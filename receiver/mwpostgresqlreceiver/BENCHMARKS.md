# Receiver benchmarks and allocation baseline

Baseline for the optimization work described in `MWPOSTGRESQL-OPTIMIZATION-PLAN.md`.
Recorded September 11, 2026. Before this commit there were no benchmarks in this
receiver or in `internal/sqlquery`, so there was nothing to measure a change against.

No optimization is claimed here. These numbers exist so that later steps can show
a difference, and so that a change which does not improve its benchmark can be
recorded as such rather than landed on the strength of its rationale.

## How to run

```
# Receiver
go test -run XXX -bench . -benchtime 1x -count 6 ./

# Shared row scanner
cd ../../internal/sqlquery && go test -run XXX -bench . -benchtime 100x -count 6 ./

# Compare against a saved baseline
go run golang.org/x/perf/cmd/benchstat@latest old.txt new.txt
```

`-benchtime 1x` is deliberate for the receiver benchmarks: each iteration is one
whole scrape of up to 1000 rows, so the default time-based loop would run for
minutes per case. Use `-count` for repetition instead, and `benchstat` to get the
variance rather than reading a single run.

Allocation and CPU profiles:

```
go test -run XXX -bench 'BenchmarkGetTopQueryRepresentative/rows=1000' \
  -benchtime 20x -memprofile mem.prof -cpuprofile cpu.prof ./
go tool pprof -sample_index=alloc_space -top mem.prof
go tool pprof -list 'getTopQuery$' mem.prof
```

## Environment

| | |
|---|---|
| Commit | `6329353373a` (after the Step 2 statement-state fixes) |
| Go | go1.26.0 linux/amd64 |
| CPU | 13th Gen Intel Core i7-1355U, 12 logical CPUs |
| Rows per scrape | 50 (low) and 1000 (`max_rows_per_query` default) |
| pg_stat_statements | 1.11 shape, matching PostgreSQL 17 |

Benchmarks drive the real code paths through `sqlmock`, so they measure this
receiver's construction cost with no server or network involved. They do not
measure PostgreSQL's own execution cost, which the plan requires to be reported
separately.

## Default configuration, for interpretation

`top_n_query` and `max_rows_per_query` both default to 1000. At defaults every
candidate row is also emitted, so the top-N heap discards nothing and deferring
work until after selection saves nothing. The low-N shape (50 of 1000) is
measured separately because Steps 6 and 7 save different amounts in each.

## Baseline: receiver

Median of 6 runs. Variance is 10–30% on wall time and under 1% on allocations, so
allocation counts are the reliable signal and timings should be compared with
`benchstat` rather than by eye.

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| GetTopQueryRepresentative/rows=50 | 2.72 ms | 553 KiB | 3,741 |
| GetTopQueryRepresentative/rows=1000 | 41.1 ms | 6.69 MiB | 72,250 |
| GetTopQueryLongSQL/rows=50 | 151 ms | 6.56 MiB | 3,731 |
| GetTopQueryLongSQL/rows=1000 | **2.93 s** | 125.5 MiB | 71,290 |
| GetTopQueryRepeatedSQL (20 distinct of 1000) | 40.8 ms | 6.69 MiB | 67,930 |
| GetTopQueryWithTraceComments | 47.7 ms | 7.64 MiB | 79,260 |
| GetTopQueryNullHeavy | 34.5 ms | 4.38 MiB | 75,230 |
| CollectTopQueryDefaultShape (1000 of 1000) | 20.1 ms | 4.84 MiB | 76,010 |
| CollectTopQueryLowN (50 of 1000) | 14.4 ms | 2.00 MiB | 47,510 |
| CollectTopQuerySmallServer (50 of 50) | 837 µs | 283 KiB | 3,805 |
| TopQueryDeltaKey (one statement, 12 counters) | 5.19 µs | 544 B | 12 |
| EmitTableResources/tables=10 | 69.0 µs | 21.2 KiB | 479 |
| EmitTableResources/tables=100 | 482 µs | 211 KiB | 4,712 |
| EmitTableResources/tables=1000 | 4.26 ms | 2.05 MiB | 47,020 |
| EmitSingleResource | 22.9 µs | 1.36 KiB | 30 |

Comment extraction, measured on its own because the CPU profile made it the
largest single consumer in the default shape:

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| ExtractSQLComments/no comment | 13.1 µs | 0 B | 0 |
| ExtractSQLComments/trace comment | 17.4 µs | 211 B | 3 |
| ExtractSQLComments/long sql no comment | **2.14 ms** | 3 B | 0 |

## Baseline: internal/sqlquery

The shared generic row scanner, 1000 rows unless stated.

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| QueryRowsNumeric/rows=50 | 320 µs | 53.6 KiB | 917 |
| QueryRowsNumeric/rows=1000 | 4.43 ms | 1.00 MiB | 17,070 |
| QueryRowsText | 4.10 ms | 2.03 MiB | 17,070 |
| QueryRowsMixed | 6.79 ms | 2.19 MiB | 23,090 |
| QueryRowsNulls | 5.27 ms | 1.22 MiB | 29,080 |
| QueryRowsTime | 3.53 ms | 1.20 MiB | 17,070 |

About 17 allocations per row for 12 columns — roughly 1.4 per column, for the
`fmt.Sprintf` result and the map entry. NULL-heavy rows cost 70% more because
each NULL builds and returns a wrapped error that the caller then joins.

## Where the allocations are

`alloc_space` profile of `GetTopQueryRepresentative/rows=1000`, 151.7 MB total
over 20 iterations:

| Site | Flat | Cumulative |
|---|---:|---:|
| `getTopQuery` | 43.9% | 91.7% |
| `sqlquery.(*rowScanner).toStringMap` | 26.4% | 30.3% |
| `bytes.(*Buffer).String` | 4.9% | 4.9% |
| `obfuscate.attemptObfuscation` | 4.0% | 8.9% |
| `fmt.Sprintf` | 4.0% | 4.0% |

Line-level inside `getTopQuery`:

| Line | Allocated | What |
|---|---:|---|
| `client.go:2158` | 40.0 MB | `currentAttributes[dbAttributePrefix+col]` — a new key string per column per row |
| `client.go:2095` | 9.0 MB | `needConversion` map rebuilt per row |
| `client.go:2156` | 8.0 MB | semantic-convention attribute assignment |
| `client.go:2113` | 7.5 MB | raw query retained for EXPLAIN |

This confirms the plan's targeting. Step 3 item 2 (hoist the per-row maps) is
worth about 9 MB of the 151.7 MB here for a one-line change. Step 6 (typed rows)
addresses the 40 MB of key construction and the 30% spent in `toStringMap`.

## Where the CPU goes

Two findings, one of which the plan does not name.

**Comment extraction dominates, not obfuscation.** In the default shape,
`extractSQLComments` accounts for **32% of CPU**, entirely inside
`regexp.FindAllString`. It runs over the full text of every candidate row on
every scrape, before any row is selected for emission, and costs nearly as much
when there is no comment to find as when there is: 13.1 µs to find nothing in an
ordinary statement, 2.14 ms to find nothing in a long one. Statement text is
unbounded, so this scales with SQL length as well as row count. In the long-SQL
shape regexp reaches 74% of CPU, which accounts for almost all of that case's
2.93 s.

The plan's Step 6 defers "comment and trace enrichment" until after selection,
which would cover this — but only if comment extraction is treated as enrichment
rather than as part of decoding. It is currently the latter: it runs inside the
row loop regardless of whether the row will ever be emitted. Worth confirming
explicitly when Step 6 is designed, because at the 1000/1000 default no row is
discarded and deferral alone saves nothing; the saving there would have to come
from not scanning for comments at all when the trace-context feature is unused.

**The obfuscator cache is inert, as the plan states.** `GetTopQueryRepeatedSQL`
presents only 20 distinct statements across 1000 rows and is no faster than the
all-distinct case (40.8 ms vs 41.1 ms, within noise). Repetition buys nothing
today, which is the direct measurement behind Step 3 item 1. It also sets the
expectation for that change: the obfuscator is roughly 9% of allocations and a
similar share of CPU, so enabling its cache should show up in
`GetTopQueryRepeatedSQL` specifically, and barely at all in the all-distinct
benchmarks.

## What dominates at the rig's shape

The Step 6 rig run was flat because the decode path is only 1.35% of the
receiver's allocation at 58 candidate rows. That left an obvious question
unanswered - what the other 98% is - and answering it before Step 7 turned out
to matter, because the benchmark profile everything was targeted from does not
describe this shape.

The host agent does not register `pprofextension` (only `kubeagent.go` does), so
rather than modify the binary under test, this is an `alloc_space` profile of
`BenchmarkCollectTopQuerySmallServer` - 50 candidates, all emitted, which is
within a couple of rows of the rig's 58. 63.8 MB profiled over 200 iterations:

| Cost | Share | Where |
|---|---:|---|
| pdata attribute construction | **44.7%** | `Map.PutDouble` 22.7%, `PutStr` 7.1%, `PutInt` 7.1%, plus `NewLogRecord`/`NewAnyValue*` |
| `deltaKey + columnName` | **24.3%** | `scraper.go:679,701,710,712` - 15.5 MB of collectTopQuery's 16.5 MB flat |
| regexp (trace-context parse) | 8.7% | `FindAllStringSubmatch` on emitted rows |

Two things follow, and they point in different directions.

**Step 7's target is real and confirmed at this shape.** The concatenated
per-counter cache key is 24.3% of allocation here, and it is 94% of what
`collectTopQuery` allocates on its own. A single typed snapshot per statement
removes it. That is worth doing and the profile supports it.

**But the larger cost is one nothing in the plan targets.** Building the emitted
attribute map - `Map.PutDouble` and friends - is 44.7%, nearly twice Step 7's
target, and it is inherent to emitting 12 counters plus identity attributes per
statement as pdata. It is not waste in the sense the earlier steps addressed; it
is the cost of the output contract. Reducing it means emitting fewer attributes
or emitting them differently, which is a product decision rather than a
refactor, and the plan's deferred-scope section is the right place for it.

The practical consequence for sequencing: Step 7 should be expected to move the
rig's whole-receiver number by roughly a quarter of the top-query path's share,
not by the 90%-shaped figures the 1000-row benchmarks produce. Predicting a
specific end-to-end delta from a benchmark measured at 20x the row count is what
made Step 6's flat result surprising; it should not be surprising twice.

## Step 6 full-agent run: no measurable effect, and why

Same-session A/B on the 52-database rig, 20 minutes per build, 71 steady-state
samples each. Artifacts in `~/pg-leak-test/measure-2026-09-11-step6/`.

Allocation 2.58 -> 2.58 MB/s, CPU 9.0% -> 9.0%, heap and connections unchanged,
identical data-point counts (45,982) and zero errors on both sides. Flat.

That sits beside a decode path measured at 65-84k allocations per scrape falling
to under 200. Both are true. The reconciliation is candidate count: the
benchmarks decode 1000 rows, which is the `max_rows_per_query` default, and this
rig decodes **58**, because `pg_stat_statements` is installed on 1 of its 52
databases. Scaling the benchmark's per-row figures to the rig puts the decode
path at **1.35%** of the receiver's allocation, so removing 90% of it moves the
total by ~1.2% - inside the noise floor.

This is the plan's second acceptable Step 6 outcome: a documented reason the path
is not dominant, with arithmetic rather than assertion. The change stays - it is
output-neutral, the cost it removes is real, and that cost scales with candidate
count, so a server with the extension installed broadly exercises it far harder
than this rig does.

The open question it raises is where the 2.58 MB/s actually lives at 58 rows.
Step 1's profile attributed 91.7% cumulative to `getTopQuery`, but that was under
a 1000-row benchmark - the shape this rig does not have. A live profile of the
agent on the rig is the missing input, and it is what Step 8's "follow the
remaining profiles" should start from.

## Full-agent runs on the 52-database rig

The plan also asks for a matched full-agent pipeline run rather than
construction cost in isolation. That was done on September 11 against the
52-database rig, 20 minutes per build, 71 steady-state samples each. Artifacts,
method and caveats are in `~/pg-leak-test/measure-2026-09-11/README.md`.

The headline is a correctness result, not a performance one. With query load
running, the server's own counters advance ~22 calls per 10-second interval.
The pre-Step-2 receiver emitted `postgresql.calls` values around 5,600 — the
cumulative lifetime total, reported every interval, overstating interval work by
roughly 250x and growing without bound. The corrected receiver emits 18–25,
matching the server's interval delta.

Resource use at equal work (identical data-point counts, zero errors on both
sides): allocation rate flat at 2.58 vs 2.60 MB/s, CPU 10.1% vs 8.8% of one
core, connections 12 vs 12, heap median identical at 31.3 MB. Step 2 was not an
optimization pass and no allocation improvement was expected; the CPU drop is a
side effect of skipping the emit path for statements that cannot produce a valid
delta, and would not appear on a server where every candidate is always
reportable.

## Step 3 result

Measured as a same-session A/B, 8 runs each, baseline and change interleaved in
one session. That matters: an earlier comparison against the numbers recorded
above appeared to show a 30–45% wall-time regression, which turned out to be
machine drift — re-running the *unmodified* baseline at that moment gave 42.9 ms
where the recorded table says 37.1 ms. Only same-session comparisons are
meaningful for wall time on this host; the allocation figures are stable enough
to compare across sessions.

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| GetTopQueryRepresentative/rows=50 | -43.2% | ~ | -11.6% |
| GetTopQueryRepresentative/rows=1000 | -45.8% | -17.6% | -9.8% |
| GetTopQueryLongSQL/rows=50 | ~ | **-68.5%** | -10.7% |
| GetTopQueryLongSQL/rows=1000 | ~ | -9.4% | +5.1% |
| GetTopQueryRepeatedSQL | -21.9% | -18.6% | -10.4% |
| GetTopQueryWithTraceComments | ~ | -4.0% | +5.8% |
| GetTopQueryNullHeavy | -32.0% | -27.2% | -9.4% |
| **geomean** | **-21.9%** | **-26.3%** | **-6.1%** |

`~` means no statistically significant difference. All other entries are
significant at p ≤ 0.01.

Allocation *count* rises ~5% in the two shapes where bytes and time both fall.
An `alloc_objects` profile attributes that to the obfuscator cache's own
bookkeeping — `ristretto.(*keyCosts).fillSample` and `setInternal` together are
about 16% of objects in the long-SQL shape. Fewer, larger allocations in place
of many small ones; the byte and time figures are what matter.

### Item 4 was not implemented

The plan's fourth item — pre-size the log-record slice through the generator —
is not achievable as specified. mdatagen's `logs.go.tmpl` has no capacity
support at all: `EnsureCapacity` appears only in `metrics.go.tmpl`, and the
generated event constructor calls `plog.NewLogRecordSlice()` with no hint. The
plan forbids hand-editing generated code, and rightly so, so this needs an
upstream mdatagen change before it can be done here. Recorded rather than
silently skipped.

## What is not covered here

Still outstanding from Step 1:

- Live-heap (`inuse_space`) profiles over a steady window. The September 11 runs
  sampled heap through the collector's own telemetry, which gives a level but
  not an attribution.
- Schema collection: cold start and forced refresh, measured separately from
  steady state.
- Query samples, which share the generic scanner with top queries but have their
  own deduplication and watermark behaviour.
- A rig running pg_stat_statements 1.11. The current rig is on 1.10, so the
  live runs exercise the pre-rename column path; the 1.11 path is covered by the
  PostgreSQL 17 container integration test instead.

## Step 6 result

Typed row decoding, deferred enrichment and the comment-marker pre-check.
Same-session A/B, 10 runs each, baseline and change interleaved in one session
for the reasons recorded under Step 3.

### Decode path

`getTopQuery` in isolation: the generic string-map scanner replaced by typed
positional scanning, and obfuscation/comment/trace work moved out to the caller.

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| GetTopQueryRepresentative/rows=50 | -71.3% | -43.8% | -94.3% |
| GetTopQueryRepresentative/rows=1000 | -91.3% | -89.7% | -99.7% |
| GetTopQueryLongSQL/rows=50 | -99.4% | -87.2% | -94.3% |
| GetTopQueryLongSQL/rows=1000 | **-99.9%** | **-99.5%** | -99.7% |
| GetTopQueryRepeatedSQL | -91.2% | -89.5% | -99.7% |
| GetTopQueryWithTraceComments | -93.9% | -92.3% | -99.8% |
| GetTopQueryNullHeavy | -94.5% | -82.2% | -99.7% |
| **geomean** | **-78.8%** | **-69.9%** | **-92.1%** |

All significant at p ≤ 0.001. Allocations per scrape fall from roughly 65-84k to
under 200 because nothing per-row is retained in a map any more: the rows are
scanned into a reused struct and appended to a pre-sized slice.

The long-SQL case is the largest because two costs compound there. Statement
text no longer drives a regex scan of every candidate, and it is no longer
rendered through `fmt.Sprintf` into an intermediate string.

### End-to-end path

`collectTopQuery`, including delta computation, selection and emission.

These three benchmarks changed fixture in this commit - their rows now carry a
representative statement instead of the two-token literal `select 1`, so that
the enrichment being measured does the work it does in production. That makes
the raw before/after numbers not comparable on wall time. The table below is the
like-for-like comparison, new code measured against the old fixture:

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| CollectTopQueryDefaultShape (1000 of 1000) | ~ | -3.7% | -14.9% |
| CollectTopQueryLowN (50 of 1000) | ~ | -11.8% | -25.8% |
| CollectTopQuerySmallServer (50 of 50) | +7.5% | -5.5% | -18.8% |

`~` means no statistically significant difference.

The low-N shape improves most, which is what deferring enrichment predicts: 950
of 1000 candidates are discarded before anything expensive touches them. The
default shape still improves because the pre-check and the typed decode help
every row regardless of selection, but it improves less, because at the defaults
no row is ever discarded.

Holding the code constant and changing only the fixture accounts for +166% and
+211% of wall time on the default and small-server shapes respectively. That is
the cost of obfuscating and scanning a real statement rather than `select 1`,
and it is the reason the fixture was changed: the previous numbers understated
the enrichment this step defers.

### What did not improve

`CollectTopQueryDefaultShape` bytes fall only 3.7%, against a 90% fall in the
decode benchmark for the same row count. The `alloc_space` profile puts the
remainder in the delta cache: `deltaKey + columnName` is built twice per counter
per row, 12 counters per row, which is about 10.5 MB of the 37 MB profile. That
concatenation is untouched here and is exactly what Step 7 removes by keying one
typed snapshot per statement instead of twelve string-keyed entries. Reporting it
rather than folding it into this step's numbers keeps the two separable.

`BenchmarkExtractSQLComments` is unchanged in all three shapes, as it must be:
it calls `extractSQLComments` directly, not the pre-checked wrapper. The
pre-check's effect appears in the `getTopQuery` benchmarks, which call the path
the receiver actually uses.

### Comment extraction: pre-check rather than deferral

Step 1 recorded `extractSQLComments` at 32% of CPU in the default shape and 74%
in the long-SQL shape, and noted that deferral alone would not help at the
defaults because no candidate is ever discarded there.

The regex is `/\*.*?\*/|--[^\n]*`. Every alternative starts with a literal
two-byte marker, so text containing neither `/*` nor `--` cannot match, and
testing for the markers first is exact rather than approximate. Measured in
isolation:

| Shape | regex only | pre-check then regex |
|---|---:|---:|
| short statement, no comment | 5.40 µs | 40 ns |
| long statement, no comment | 3.15 ms | 4.9 µs |
| statement with a comment | ~5 µs | ~5 µs, identical allocations |

`strings.Contains` is a tuned byte scan; the regex engine was backtracking over
the whole statement to find nothing. A statement that does carry a comment pays
the scan exactly as before.

This was preferred over gating comment extraction on configuration. A
configuration gate would have needed a setting expressing "trace propagation is
unused", which no existing key expresses, and the plan forbids adding one where
existing enablement already expresses the choice. The pre-check needs no
configuration reasoning, cannot surprise anyone, and helps every deployment.

Equivalence is asserted in `TestCommentPrefilterMatchesRegex` over both markers,
partial and overlapping markers, markers inside string literals and non-ASCII
text. It was additionally fuzzed against the unguarded function for 142k
executions with no divergence; the fuzz target is not kept in the tree, since
the property it checks is a property of the pattern and the table test states it
directly.

## Step 7 result

Compact statement state: one typed snapshot per statement keyed on the
`(userid, dbid, queryid, toplevel)` identity, replacing twelve string-keyed LRU
entries per statement. Same-session A/B, 10 runs each, interleaved in one
session for the reasons recorded under Step 3.

### The target

Step 6 left `deltaKey + columnName` as the dominant remaining allocation on this
path. The rig-shape profile taken after Step 6 put it at 24.3% of a 63.8 MB
profile and 94% of what `collectTopQuery` allocates on its own.

The concatenation is gone entirely. The identity is a comparable struct used
directly as the map key, so no key string is built:

| | B/op | allocs/op |
|---|---:|---:|
| before, per statement per scrape | 544 | 12 |
| after | **0** | **0** |

The two benchmarks are not measuring identical spans — the old one measured key
construction alone, the new one the whole cache access including the
subtraction — so the allocation elimination is the claim, not the ns/op ratio.

### End-to-end path

`collectTopQuery`, including delta computation, selection and emission.

| Benchmark | sec/op | B/op | allocs/op |
|---|---:|---:|---:|
| CollectTopQueryDefaultShape (1000 of 1000) | -7.7% | -27.1% | -43.1% |
| CollectTopQueryLowN (50 of 1000) | -70.8% | -79.4% | -89.0% |
| CollectTopQuerySmallServer (50 of 50) | -13.1% | -22.5% | -41.3% |

The emitted record count was asserted equal before and after for all three
shapes — 1000, 50 and 50 — so this is a comparison at equal work rather than one
where the change quietly emits less.

The low-N shape improves far more than the others, and the reason is not only
the key construction. The old cache was sized at `max_rows_per_query × 12 × 2`
entries, which is the right number of entries but the wrong shape: twelve
independent entries per statement mean the LRU's recency order interleaves
counters from different statements, so pressure evicts fragments. One entry per
statement removes that. The default shape improves less because it was the shape
least affected by fragmentation to begin with.

### Calibrating this against the whole agent

Step 6 cut its own path by 99.7% and moved the whole-agent number by nothing
measurable, because that path was 1.35% of the total at the rig's 58 rows. This
step's target was 24.3% of the same profile, so it should move something — but
the honest expectation is a fraction of the top-query path's share, not a
figure shaped like the table above. The whole-agent measurement is a separate
run and is not claimed here.

### What this does not address

The post-Step-6 profile put 44.7% in pdata attribute construction —
`Map.PutDouble` at 22.7%, `PutStr` and `PutInt` at 7.1% each. That is the cost
of emitting twelve counters plus identity per statement as pdata, and it is
inherent to the output contract rather than waste. Nothing here changes it, and
reducing it would mean changing what is emitted, which is a product decision
rather than an optimization.

### Correctness measured alongside

Every behavioral claim was verified failing-first against the unfixed code:
the plan-cache key reverted to queryid alone, the identity reduced to queryid
alone, the `stats_since` signal disabled, integer subtraction replaced with
float64, and each of the three Step 2 guards removed in turn.

Two of the Step 2 guard mutations initially did **not** fail, which is the
result the fail-first rule exists to produce. `TestTopQueryUnchangedEntryNotReported`
and `TestTopQueryRebaselinesOnCounterDecrease` were both passing for the wrong
reason: their fixtures held `total_exec_time` constant or decreasing alongside
the counter under test, and the emit path independently drops a row whose
exec-time delta is not positive. Each fixture now moves exec time in the
direction that only the named guard can account for, and both mutations then
fail as they should.

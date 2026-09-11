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

## What is not covered here

Per the plan, these are receiver-level benchmarks against a cheap sink. Still
outstanding from Step 1:

- A matched full-agent pipeline run, to show end-to-end effect rather than
  construction cost in isolation.
- Live-heap (`inuse_space`) profiles over a steady 20–30 minute window, which is
  what the plan's validation section asks for and what a short benchmark cannot
  produce.
- Schema collection: cold start and forced refresh, measured separately from
  steady state.
- Query samples, which share the generic scanner with top queries but have their
  own deduplication and watermark behaviour.

The rig for the live runs is the 52-database PostgreSQL 16 container described in
`MWPOSTGRESQL-OPTIMIZATION-PLAN.md`; the notes and sampler are under
`~/pg-leak-test/`.

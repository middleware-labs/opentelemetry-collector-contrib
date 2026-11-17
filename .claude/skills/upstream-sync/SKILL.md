---
name: upstream-sync
description: Sync the Middleware fork of opentelemetry-collector-contrib with a specific upstream release tag (e.g. v0.163.0). Resolves the tag once via a merge in a separate worktree, then rebases the fork commits onto the tag (team's chosen history) ending in a verified "Conflict Resolutions" commit; resolves conflicts while preserving every Middleware addition, regenerates mdatagen code, aligns the fork-only mw* receiver modules, builds/tests every fork-touched module, and produces a report of metrics/attributes that upstream dropped, renamed, disabled, re-united or deprecated (the backend's default dashboards and alerts depend on these). Use when asked to "sync with upstream", "rebase/upgrade the fork to 0.1xx", or "what metrics changed in upstream release X".
---

# Upstream release sync for the Middleware fork

The fork = an upstream release tag + Middleware commits. Two things must hold after a sync:
1. **No Middleware addition is lost** (metrics, attributes, config, behaviour, fork-only `mw*` receivers).
2. **Upstream changes work** (fork-touched modules build, vet and test cleanly).

And the user always needs a **metrics impact report**: anything dropped / renamed / default-disabled /
unit- or type-changed / dimension-dropped / deprecated, because the backend's default dashboards and
alerts depend on metric names, attributes and units.

Scripts live in `.claude/skills/upstream-sync/scripts/` (referred to as `$SK` below):

| script | purpose |
|---|---|
| `metrics_diff.py` | diff of every `metadata.yaml` (metrics, attrs incl. opt-in, resource attrs, events, units, types, enabled, deprecations) between two git refs/trees. Run with `uv run -q --with pyyaml`. |
| `resolve_hunks.py` | resolve conflict hunks per file with explicit choices (`o`,`t`,`ot`,`to`,`Ot`); `--show` prints hunks. |
| `align_fork_modules.sh` | bump fork-only modules (`receiver/mw*receiver`) to the new tag: contrib deps, collector core versions, go directive, drop removed in-repo modules, `go mod tidy`. |
| `check_modules.sh` | parallel `go build` + `go vet` (+ `go test` with `--test`) over module dirs, logs per module. |
| `replay_fork_commits.sh` | rebase flow: replay fork commits onto the new tag and finish with a "Conflict Resolutions" commit equal to the verified resolved tree. |

Shell gotcha: the user's shell is **zsh** — unquoted `$VAR` holding a file list is NOT word-split.
Wrap multi-file commands in `bash -c '...'`. BSD `sed` has no `\s`/`\b`; use `perl -pi -e`.

## 0. Preconditions / facts about this fork
- `origin` = middleware-labs fork. Add `upstream` if missing:
  `git remote add upstream https://github.com/open-telemetry/opentelemetry-collector-contrib.git`
- Find the current base: `git log --oneline | tail -5` → the root-most commit is
  `[chore] Prepare release vX` from upstream; verify with `git fetch upstream --no-tags tag vX` and
  `git rev-parse vX^{commit}`. History is linear (rebase flow): the commit just below the fork's oldest
  commit is upstream's release commit; the topmost fork commit is "Conflict Resolutions for vX".
- Fork-only modules: `receiver/mwmongodbreceiver`, `receiver/mwmysqlreceiver`,
  `receiver/mwpostgresqlreceiver`. They **reuse upstream module paths** (`.../receiver/mongodbreceiver`
  etc.) and component types (`mongodb`, `mysql`, `postgresql`) — the mw-host-agent swaps them in with
  `replace`. They are not touched by the merge, so they must be aligned manually (step 5).
- The user may have uncommitted/untracked work in the main checkout: **do all work in a separate
  worktree**, never in the user's checkout.

## 1. Set up
```bash
git fetch upstream --no-tags tag <NEW>          # and the old base tag if needed
git worktree add -b sync/<NEW> ../occ-sync-<NEW> main
S=<scratchpad>; mkdir -p $S
```
Focus list (components the fork customises — stars them in reports):
```bash
git diff --name-only <OLDBASE> main | awk -F/ 'NF>2 && ($1~/^(receiver|processor|exporter|extension|connector)$/){print $1"/"$2"/"}' | sort -u > $S/focus.txt
```

## 2. Upstream impact report (do this FIRST — it informs conflict resolution)
```bash
uv run -q --with pyyaml $SK/metrics_diff.py --old <OLDBASE> --new <NEW> --focus-file $S/focus.txt > $S/upstream_diff.md
git show <NEW>:CHANGELOG.md | awk '/^## .*<OLDBASE without v>/{exit} {print}' > $S/changelog.md
awk '/^## /{v=$NF} /^### 🛑 Breaking changes/{b=1;next} /^### /{b=0} b && /^- `/{print v" | "$0}' $S/changelog.md > $S/breaking.txt
grep -i -E "metric|attribute|renam|remov|unit|semconv|feature ?gate|deprecat|resource|opt-in|default" $S/breaking.txt
```
metadata.yaml does NOT show everything. Always also scan the changelog for:
- **feature-gate migrations** (old/new metric names behind gates, e.g. apache `enableNewFormatMetrics`),
  gates promoted to beta (= default ON → behaviour change, e.g. kubeletstats `cpuUsageScrapeBased`);
- **attribute requirement_level → opt_in** (dimension silently disappears, e.g. hostmetrics `cpu`);
- detector/component renames with deprecated aliases (config keys, not metric names);
- removed components (check against what the agent's builder config uses).
Also `grep -n "deprecated\|Deprecate" $S/changelog.md` for upcoming removals worth warning about.

## 3. Merge and inventory conflicts
```bash
cd ../occ-sync-<NEW>
git merge --no-ff --no-commit <NEW>
git diff --name-only --diff-filter=U
```
Dry-run alternative without touching a tree: `git merge-tree --write-tree --name-only --messages <NEW> main`.

**The team's chosen history shape is the rebase flow** (fork commits on top of the upstream tag + one
"Conflict Resolutions for <tag>" commit). The merge is only the *workspace* where conflicts are resolved
once; steps 4–8 run on it. After verification (step 8) convert it with step 9. Don't push the merge.

## 4. Resolve conflicts — rules
Classify every conflicted file:

**a) Generated files** (`documentation.md`, `internal/metadata/generated_*`, `internal/metadata/config.schema.yaml`,
`internal/metadata/testdata/config.yaml`, `generated_component_test.go`, `generated_package_test.go`):
never hand-merge. `git checkout --theirs` them, fix `metadata.yaml`, regenerate (step 6).

**b) `metadata.yaml`**: union. Keep every fork metric/attribute/resource attribute; take upstream's
new metrics, deprecations, feature_gates, formatting. Typical choices with `resolve_hunks.py`:
`ot` (fork block then upstream block), `Ot` (both re-declare the same trailing key such as
`apache.scoreboard:` vs `apache.scoreboard: # (old)` — keep upstream's), `t` for pure formatting.
Take upstream's `tests:` block (upstream removed goleak/lifecycle skips once it fixed leaks); if the
generated lifecycle test then fails because of fork code, re-add the skip and say why.
Follow upstream when it drops a key (e.g. `reaggregation_enabled`).

**c) Hand-written code**: read what the fork change does (`git diff <OLDBASE> main -- <file>`) and
re-apply it on upstream's new structure. Before porting, check whether **upstream already has the same
feature** (e.g. k8sobjects `initial_delay` was upstreamed) — then take upstream and verify
`git diff <NEW> -- <dir>` is empty or trivially explainable. Keep fork metrics emitted unconditionally
when upstream adds old/new format gating around neighbouring calls.

**d) Test fixtures** (`testdata/**/expected*.yaml`, `*.assert.yaml`): take upstream, then regenerate
from the tests (step 7). Upstream-deleted fixture files that the fork modified: delete if the test that
used them was replaced.

**e) modify/delete**: look at why each side did it. A fork deletion that breaks a test the fork still
has was accidental → restore upstream's file.

Use `python3 $SK/resolve_hunks.py FILE --show` to inspect, then `python3 $SK/resolve_hunks.py FILE t ot ...`.
After resolving hand-written files make sure no markers remain (`grep -l '^<<<<<<< \|^>>>>>>> '`), then `git add`.

## 5. Align fork-only modules (mw* receivers)
```bash
$SK/align_fork_modules.sh <NEW> receiver/mwmongodbreceiver receiver/mwmysqlreceiver receiver/mwpostgresqlreceiver
```
Then port API changes by diffing upstream's sibling: e.g.
`diff <(git show <OLDBASE>:receiver/mongodbreceiver/factory.go) <(git show <NEW>:receiver/mongodbreceiver/factory.go)`.
Seen so far (0.152→0.163): `scraperhelper.AddScraper`→`AddMetricsScraper`,
`metadata.DefaultMetricsBuilderConfig()`→`NewDefaultMetricsBuilderConfig()`,
`xconfmap.Validate`→`confmap.Validate`, `pkg/pdatautil` module removed (core `xpdata/xhash`),
mongo-driver v1 `bson`/`primitive` → `go.mongodb.org/mongo-driver/v2/bson` (`primitive.DateTime`→`bson.DateTime`).

## 6. Regenerate mdatagen code (exact upstream pipeline)
Build the tool pinned by the new release, then for every component whose `metadata.yaml` changed or
conflicted **and** for every fork-touched component (catches stale generated code):
```bash
(cd internal/tools && go build -o $S/mdatagen go.opentelemetry.io/collector/cmd/mdatagen && go build -o $S/gci github.com/daixiang0/gci)
(cd <component> && $S/mdatagen metadata.yaml)
bash -c '$S/gci write -s standard -s default -s "prefix(github.com/open-telemetry/opentelemetry-collector-contrib)" $(git ls-files -m -o --exclude-standard | grep -E "generated_.*\.go$")'
```
Do NOT run gofumpt/goimports on generated files — upstream doesn't, and it creates hundreds of noise diffs.
For components the fork didn't change semantically the result must be byte-identical to the merge
result (`git diff --stat` empty); if not, something is out of sync — investigate.

Known mdatagen gotchas:
- New validation (UCUM units since ~0.157, sorted keys): fix fork metadata the same way upstream fixed
  theirs (`responses`→`{responses}`, `microseconds`→`us`). **Metric names do not change; unit strings
  do** → list them in the report.
- mw* receivers: mdatagen names the package of root-level generated files after the directory
  (`package mwmongodbreceiver`); rewrite to the real package (`mongodbreceiver`, …) afterwards.
- Safety net: compare `func (mb *MetricsBuilder) Record*` sets in `generated_metrics.go` before
  (`git show main:...`) and after; nothing may be missing.
- **Hand edits hidden in generated code get erased by regeneration.** The fork has done this
  (mwpostgresql `generated_logs.go` set `dp.Body().SetStr(dbQueryTextAttributeValue)`; it now lives in
  `log_attrs_filter.go:setQueryTextBody`). After regenerating, list logic lines the old generated files
  had that the new ones lack and check each one:
  `comm -23 <(git diff HEAD -U0 -- F | grep '^-[^-]' | sed 's/^-//;s/^\s*//' | sort -u) <(sed 's/^\s*//' F | sort -u) | grep -E 'Body|Set|Put|if |return'`.
  Template churn (attribute helpers, reaggregation code removed in mdatagen #49242) is fine; anything
  else must be moved into hand-written code.

## 7. Build, vet, test
```bash
git diff --name-only <NEW> | xargs -n1 dirname | sort -u | while read d; do while [ "$d" != "." ] && [ ! -f "$d/go.mod" ]; do d=$(dirname "$d"); done; [ -f "$d/go.mod" ] && [ "$d" != "." ] && echo "$d"; done | sort -u > $S/modules.txt
$SK/check_modules.sh $S/build $(cat $S/modules.txt)            # build+vet
PARALLEL=4 $SK/check_modules.sh --test $S/test $(cat $S/modules.txt)   # + tests (run in background)
```
**Always run a baseline on `main`** in its own clean worktree (`git worktree add --detach ../occ-main-baseline main`)
with the same module list, then compare failing test names per module. As of v0.163.0 `main` fails tests
in most fork-touched modules because fork behaviour (extra metrics/attrs, `k8s.pod.ip`, required AWS
creds, JSON-string PMapGetter) contradicts upstream expectations. Only failures that appear in the sync
but not on `main` need a decision; new upstream tests that hit the same fork behaviour count as
pre-existing. Fork-only modules (`mw*`) must end with the same or fewer failures than on `main`.
Golden files: tests use `golden.WriteMetrics`/`pmetricassert.WriteAssertionFile` behind a comment —
uncomment, run once, re-comment, then **review the regenerated diff** (only fork metrics/upstream
additions may appear). `WriteAssertionFile` writes literal values over regex matchers
(e.g. `apache.server.port/regex: '[0-9]+'`) — restore those.
mdatagen ≥0.163 validates `MetricsBuilderConfig` (aggregation strategy) — tests that build `Config{}` by
hand need `MetricsBuilderConfig: metadata.NewDefaultMetricsBuilderConfig()`.

## 8. Verify nothing of ours was lost
```bash
git add -A && T=$(git write-tree)
uv run -q --with pyyaml $SK/metrics_diff.py --old main --new $T --focus-file $S/focus.txt > $S/fork_regression.md
grep -E "🛑|🔁|enabled: True → False|attributes: -|resource attr .* removed|event .* removed" $S/fork_regression.md
```
Every hit must be explained by an upstream change already in the upstream report.
Also `git diff main -- <fork-file>` spot checks for each fork commit area (grep fork commits:
`git log --oneline <OLDBASE>..main`).

## 9. Commit, convert to the rebase flow, report
- Commit the merge on `sync/<NEW>`: message `Sync fork with upstream <NEW>`; body lists conflict
  decisions; trailer `Assisted-by: <model>` (AGENTS.md — never `Co-authored-by`).
- Convert to the rebase history (cheap, scripted, no re-analysis):
  ```bash
  $SK/replay_fork_commits.sh <OLDBASE> main <NEW> sync/<NEW> rebase/<NEW>
  ```
  It cherry-picks every fork commit (authors/dates kept) onto `<NEW>`, auto-takes the fork commit's
  version on conflicts, then adds `Conflict Resolutions for <NEW>` whose tree is set to `sync/<NEW>`;
  it fails unless `git diff sync/<NEW> HEAD` is empty. Amend that last commit's message with the
  conflict-decision body + `Assisted-by` trailer. Intermediate commits may not build — expected; the top
  commit is the verified tree.
  Checks: `git rev-list --merges --count <NEW>..HEAD` = 0; author/date/subject lists of
  `<OLDBASE>..main` and `<NEW>..HEAD~1` are identical.
- Next sync's `<OLDBASE>` is the upstream tag the current `main` sits on (root of the fork commits).
- Leave `rebase/<NEW>` for review. Adopting it needs a **force-push of `main`** — only with explicit
  approval; keep a backup branch (e.g. `main-backup-<date>`) first, as the team did before.

Report to the user (publish as a page if they will share it):
1. **Metrics impact for dashboards/alerts** (removed / renamed / opt-in dimension / unit / type /
   default flips / deprecations / feature-gate behaviour changes) — focus components first, then any other
   component the agent ships.
2. Conflict decisions that need their call, with the option taken.
3. Fork changes absorbed by upstream (now identical) and fork changes adapted.
4. Pre-existing breakages found on `main`.
5. Build/test status per module.

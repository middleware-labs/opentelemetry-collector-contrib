#!/usr/bin/env bash
# Build + vet (and optionally test) a list of Go module dirs in parallel.
# Usage: check_modules.sh [--test] LOGDIR MODULE_DIR...
# Prints "<module> OK|FAIL" per module; full output in LOGDIR/<module>.log
set -uo pipefail
TEST=0
if [ "${1:-}" = "--test" ]; then TEST=1; shift; fi
LOGDIR="$1"; shift
mkdir -p "$LOGDIR"
ROOT="$(git rev-parse --show-toplevel)"
run_one() {
  local mod="$1" log="$LOGDIR/$(echo "$1" | tr / _).log"
  (
    cd "$ROOT/$mod" || exit 1
    go build ./... && go vet ./... && { [ "$TEST" = 0 ] || go test -count=1 ./...; }
  ) >"$log" 2>&1 && echo "$mod OK" || echo "$mod FAIL ($log)"
}
export -f run_one; export LOGDIR ROOT TEST
printf '%s\n' "$@" | xargs -P "${PARALLEL:-6}" -I{} bash -c 'run_one "$@"' _ {}

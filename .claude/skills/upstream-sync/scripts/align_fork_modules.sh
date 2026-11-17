#!/usr/bin/env bash
# Align fork-only Go modules (e.g. receiver/mw*receiver) with the upstream release
# that was just merged: bump in-repo contrib deps to the new tag, align every
# go.opentelemetry.io/collector/* dep to the version upstream modules use, drop
# replace/require entries for in-repo modules that no longer exist, set the `go`
# directive to upstream's, then `go mod tidy`.
#
# Usage (repo root, after the merge is resolved):
#   .claude/skills/upstream-sync/scripts/align_fork_modules.sh v0.163.0 receiver/mwmongodbreceiver receiver/mwmysqlreceiver ...
set -euo pipefail
NEW_TAG="$1"; shift
NEW_VER="${NEW_TAG#v}"
ROOT="$(git rev-parse --show-toplevel)"
CONTRIB="github.com/open-telemetry/opentelemetry-collector-contrib"

# Version map of collector core modules, taken from upstream-owned go.mod files only.
CORE_MAP="$(mktemp)"
git -C "$ROOT" ls-files '*/go.mod' | grep -v -E '/mw[^/]*/go.mod$' | while read -r f; do cat "$ROOT/$f"; done \
  | grep -E '^\s+go\.opentelemetry\.io/collector[/a-z0-9]* v' | awk '{print $1, $2}' \
  | sort -u -V | awk '{v[$1]=$2} END{for (k in v) print k, v[k]}' > "$CORE_MAP"
GO_DIRECTIVE="$(awk '/^go /{print $2; exit}' "$ROOT/receiver/postgresqlreceiver/go.mod")"

for MOD in "$@"; do
  echo "=== $MOD"
  cd "$ROOT/$MOD"
  go mod edit -go="$GO_DIRECTIVE"
  # In-repo contrib replaces: drop the ones whose target dir vanished upstream.
  for target in $(go mod edit -json | jq -r '.Replace[]? | select(.New.Path|startswith("../")) | .Old.Path + "=" + .New.Path'); do
    old="${target%%=*}"; dir="${target#*=}"
    if [ ! -f "$dir/go.mod" ]; then
      echo "  dropping removed module $old ($dir)"
      go mod edit -dropreplace="$old" -droprequire="$old"
    fi
  done
  # Bump contrib requires to the new tag.
  for m in $(go mod edit -json | jq -r --arg c "$CONTRIB" '.Require[]? | select(.Path|startswith($c)) | .Path'); do
    go mod edit -require="$m@v$NEW_VER"
  done
  # Align collector core modules.
  for m in $(go mod edit -json | jq -r '.Require[]? | select(.Path|startswith("go.opentelemetry.io/collector")) | .Path'); do
    v="$(awk -v k="$m" '$1==k{print $2}' "$CORE_MAP")"
    if [ -n "$v" ]; then go mod edit -require="$m@$v"; else echo "  WARN no upstream version for $m (left as is)"; fi
  done
  go mod tidy
done
rm -f "$CORE_MAP"

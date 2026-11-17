#!/usr/bin/env bash
# Replay fork commits onto a new upstream tag (rebase flow), then make the tree
# exactly equal to an already-resolved, verified tree in one final commit.
#
# Usage (inside a worktree, clean state):
#   replay_fork_commits.sh <OLD_BASE> <FORK_HEAD> <NEW_TAG> <RESOLVED_TREEISH> <BRANCH> [<MSG>]
#   e.g. replay_fork_commits.sh v0.152.0 main v0.163.0 sync/v0.163.0 rebase/v0.163.0
#
# Each fork commit is cherry-picked (author/date preserved). On conflict every
# unmerged path takes the fork commit's version (deletions included): the
# intermediate commits record fork intent and may not build; the final
# "Conflict Resolutions for <tag>" commit sets the verified resolved tree.
set -euo pipefail
OLD="$1"; HEAD_REF="$2"; NEW="$3"; RESOLVED="$4"; BRANCH="$5"
MSG="${6:-Conflict Resolutions for ${NEW}}"

git switch -q -c "$BRANCH" "$NEW"
commits=$(git rev-list --reverse --no-merges "$OLD..$HEAD_REF")
total=$(echo "$commits" | wc -l | tr -d ' '); n=0; conflicted=0
for c in $commits; do
  n=$((n+1))
  if ! git cherry-pick --allow-empty --keep-redundant-commits "$c" >/dev/null 2>&1; then
    conflicted=$((conflicted+1))
    git diff --name-only --diff-filter=U -z | while IFS= read -r -d '' f; do
      st=$(git status --porcelain -- "$f" | cut -c1-2)
      case "$st" in
        UD|DD) git rm -q --cached -- "$f" 2>/dev/null; rm -f -- "$f" ;;          # fork commit deleted it
        *)     if git checkout --theirs -- "$f" 2>/dev/null; then git add -- "$f";
               else git rm -q --cached -- "$f" 2>/dev/null; rm -f -- "$f"; fi ;;
      esac
    done
    git -c core.editor=true cherry-pick --continue >/dev/null 2>&1 \
      || git -c core.editor=true commit -q --allow-empty --no-edit
  fi
  printf '%s/%s %s %s\n' "$n" "$total" "$(git rev-parse --short HEAD)" "$(git log -1 --format=%s | cut -c1-70)"
done
echo "replayed $total commits ($conflicted auto-resolved)"

# Final commit: make the tree identical to the verified resolution.
git read-tree -u --reset "$RESOLVED"
git commit -q --allow-empty -m "$MSG"
if git diff --quiet "$RESOLVED" HEAD; then echo "OK: tree identical to $RESOLVED"; else echo "MISMATCH vs $RESOLVED"; exit 1; fi

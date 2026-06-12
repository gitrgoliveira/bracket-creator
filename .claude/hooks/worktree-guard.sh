#!/usr/bin/env bash
# PreToolUse guard for Edit|Write|NotebookEdit.
#
# Blocks edits that target the MAIN git checkout of this repo, while allowing:
#   - edits inside any worktree under <repo>/.claude/worktrees/
#   - edits anywhere OUTSIDE the repo (e.g. ~/.claude auto-memory)
#
# Fails OPEN: if anything is ambiguous (no jq, not a git repo, no path),
# it allows the edit rather than blocking all work.
#
# ACTIVATION (required — the script is inert until registered):
# .claude/settings.json is gitignored, so a fresh checkout receives this
# file but nothing invokes it. To enable the guard, add to your local
# .claude/settings.json (merge with any existing "hooks"):
#
#   "hooks": {
#     "PreToolUse": [
#       { "matcher": "Edit|Write|NotebookEdit",
#         "hooks": [ { "type": "command",
#                      "command": ".claude/hooks/worktree-guard.sh" } ] }
#     ]
#   }
#
# Then open /hooks once (or restart) so Claude Code reloads the config.

input="$(cat 2>/dev/null)"

command -v jq >/dev/null 2>&1 || exit 0   # no jq -> don't interfere

file="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"
[ -z "$file" ] && exit 0

# Resolve the main checkout root: parent of the shared .git directory.
# --path-format=absolute requires Git ≥ 2.31; cd + pwd -P resolves portably
# whether git-common-dir returns a relative or absolute path.
_raw_git="$(git rev-parse --git-common-dir 2>/dev/null)"
[ -z "$_raw_git" ] && exit 0              # not in a git repo -> allow
common_git="$(cd "$_raw_git" 2>/dev/null && pwd -P)"
[ -z "$common_git" ] && exit 0
main_root="$(cd "$(dirname "$common_git")" 2>/dev/null && pwd -P)"
[ -z "$main_root" ] && exit 0             # can't canonicalize root -> allow
worktrees_dir="$main_root/.claude/worktrees"

# Normalize target to an absolute path (file may not exist yet for Write).
case "$file" in
  /*) abs="$file" ;;
  *)  abs="$PWD/$file" ;;
esac

# Canonicalize so '..' segments and symlinks can't fool the glob matching
# below (e.g. <worktree>/../../CLAUDE.md resolving into the main checkout).
# The parent dir exists even when the Write target itself doesn't; resolve
# it with `pwd -P` (macOS/BSD has no `realpath -m`) and re-append the
# basename. Fail OPEN if the parent can't be resolved (new nested dir).
abs_parent="$(cd "$(dirname "$abs")" 2>/dev/null && pwd -P)"
[ -z "$abs_parent" ] && exit 0
abs="$abs_parent/$(basename "$abs")"

# Outside the repo entirely (e.g. ~/.claude memory) -> allow.
case "$abs" in
  "$main_root"/*) : ;;   # inside the repo tree -> keep checking
  *) exit 0 ;;
esac

# Inside a worktree -> allow.
case "$abs" in
  "$worktrees_dir"/*) exit 0 ;;
esac

# Otherwise the target is the bare main checkout -> deny.
jq -n --arg root "$main_root" '{
  hookSpecificOutput: {
    hookEventName: "PreToolUse",
    permissionDecision: "deny",
    permissionDecisionReason: ("Blocked edit to the MAIN checkout at " + $root +
      ". Work happens in a worktree under .claude/worktrees/. cd into the correct " +
      "worktree (or pass its absolute path) and retry.")
  }
}'
exit 0

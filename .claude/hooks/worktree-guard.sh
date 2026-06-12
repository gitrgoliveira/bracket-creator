#!/usr/bin/env bash
# PreToolUse guard for Edit|Write|NotebookEdit.
#
# Blocks edits that target the MAIN git checkout of this repo, while allowing:
#   - edits inside any worktree under <repo>/.claude/worktrees/
#   - edits anywhere OUTSIDE the repo (e.g. ~/.claude auto-memory)
#
# Fails OPEN: if anything is ambiguous (no jq, not a git repo, no path),
# it allows the edit rather than blocking all work.

input="$(cat 2>/dev/null)"

command -v jq >/dev/null 2>&1 || exit 0   # no jq -> don't interfere

file="$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)"
[ -z "$file" ] && exit 0

# Resolve the main checkout root: parent of the shared .git directory.
common_git="$(git rev-parse --path-format=absolute --git-common-dir 2>/dev/null)"
[ -z "$common_git" ] && exit 0            # not in a git repo -> allow
main_root="$(dirname "$common_git")"
worktrees_dir="$main_root/.claude/worktrees"

# Normalize target to an absolute path (file may not exist yet for Write).
case "$file" in
  /*) abs="$file" ;;
  *)  abs="$PWD/$file" ;;
esac

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

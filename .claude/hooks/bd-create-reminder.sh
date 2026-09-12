#!/usr/bin/env bash
# PostToolUse reminder for Bash: a bead created with `bd create` must be
# slotted into the merge order.
#
# Merge order in this repo is encoded in beads (bd memory
# "merge-concurrency-plan"): a `blocks` edge on the bead a new one must
# merge after, plus a `serial/<lane>` label when it shares a lane. A bead
# filed without either silently falls outside that plan. This hook fires
# ONLY when a command segment is `bd create ...` and that segment carries
# no --deps, and it adds a reminder to the model's context. It never blocks:
# a PostToolUse `decision: block` could make the model retry `bd create`
# and mint a duplicate bead.
#
# Fails OPEN: no jq, no command, no `bd create` segment -> no output.
#
# ACTIVATION (required, the script is inert until registered):
# .claude/settings.json is gitignored, so a fresh checkout receives this
# file but nothing invokes it. Add to your local .claude/settings.json
# (merge with any existing "hooks"):
#
#   "hooks": {
#     "PostToolUse": [
#       { "matcher": "Bash",
#         "hooks": [ { "type": "command",
#                      "command": ".claude/hooks/bd-create-reminder.sh" } ] }
#     ]
#   }
#
# Then open /hooks once (or restart) so Claude Code reloads the config.
# Live check without minting a bead: run `bd create --dry-run "x"` and
# confirm the reminder appears in the next model turn.

input="$(cat 2>/dev/null)"
command -v jq >/dev/null 2>&1 || exit 0

cmd="$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)"
[ -z "$cmd" ] && exit 0

# Split the command into segments (newline ; && || |) and look for one
# whose first word is `bd` followed by `create`, allowing global flags such
# as `-C <dir>` or `--db <path>` in between. Matching in command position
# keeps `git commit -m "bd create"` or `echo 'bd create'` from firing.
needs_reminder=0
flat="$(printf '%s' "$cmd" | tr '\n' ';' | sed -E 's/&&|\|\||\|/;/g')"
IFS=';' read -r -a segments <<<"$flat"
for seg in "${segments[@]}"; do
  # Drop leading whitespace and VAR=value assignments.
  seg="$(printf '%s' "$seg" | sed -E 's/^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*[[:space:]]+)*//')"
  printf '%s' "$seg" | grep -Eq '^bd([[:space:]]+-(-)?[A-Za-z-]+([[:space:]]+[^-[:space:]][^[:space:]]*)?)*[[:space:]]+create([[:space:]]|$)' || continue
  printf '%s' "$seg" | grep -Eq -- '(^|[[:space:]])--deps([[:space:]]|=)' && continue
  # `bd create --help` creates nothing; --dry-run stays in on purpose (live check).
  printf '%s' "$seg" | grep -Eq -- '(^|[[:space:]])(--help|-h)([[:space:]]|$)' && continue
  needs_reminder=1
  break
done
[ "$needs_reminder" = 1 ] || exit 0

# Name the bead when bd printed its id: the "Created issue:" line, or a bare
# id when the command used --silent. Any other id in stdout (e.g. from a
# `bd list` earlier in the chain) is not the new bead, so fall back to a
# generic phrase rather than name the wrong one.
id_re='(bc|mp|bracket-creator)-[a-z0-9]+(\.[0-9]+)?'
out="$(printf '%s' "$input" | jq -r '.tool_response.stdout // .tool_response // empty' 2>/dev/null)"
id="$(printf '%s' "$out" | grep -E 'Created issue:' | grep -Eo "$id_re" | head -n 1)"
if [ -z "$id" ]; then
  id="$(printf '%s' "$out" | grep -Ex "[[:space:]]*$id_re[[:space:]]*" | head -n 1 | tr -d '[:space:]')"
fi
[ -z "$id" ] && id="the new bead"

jq -n --arg id "$id" '{
  hookSpecificOutput: {
    hookEventName: "PostToolUse",
    additionalContext: ("Reminder: " + $id + " was created without --deps, so it sits outside the merge order. " +
      "Decide now and record it: if it edits a lane region (export pipeline, admin_shiaijo.jsx, server core, e2e fixtures, broad JSX text), " +
      "run `bd dep add " + $id + " <bead it must merge after>` and `bd label add " + $id + " serial/<lane>`; " +
      "otherwise state in the bead that it is independent. Lanes and the rule: `bd memories merge-concurrency-plan`.")
  }
}'
exit 0

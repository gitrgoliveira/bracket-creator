<!--
Title: conventional-commit style, e.g.
  feat(mp-xxxx): short imperative summary
  fix(mp-xxxx): ...
  chore: ...
-->

## Summary

<!-- Bullet points: what changed and why it matters. Lead with user-visible behavior. -->

-
-

<!-- Optional — include when it helps reviewers:

## Why

Motivation / root cause. For fixes, state the root cause explicitly.

-->

## Files changed

<!-- REQUIRED. One row per file touched. -->

| File | What |
|---|---|
| `path/to/file` | one-line description |

## Screenshots

<!--
REQUIRED for any change that affects the UI (web-mobile/ or web/) — including
visual, layout, copy, or behavior changes. Attach before/after images.
Delete this section only if the change has no UI impact whatsoever.

Agents: `gh gist create` rejects binaries. Push the PNG to the `pr-assets`
branch (never merged to main) and embed the raw URL:
  gh api --method PUT .../contents/pr-assets/<pr>/shot.png \
    -f branch=pr-assets -f content="$(base64 < shot.png | tr -d '\n')"
  ![desc](https://raw.githubusercontent.com/gitrgoliveira/bracket-creator/pr-assets/pr-assets/<pr>/shot.png)
A real browser/MCP screenshot is MANDATORY — there is no textual / DOM /
geometry substitute. If you have not captured one yet, the PR is not ready:
capture it (Playwright or preview_screenshot), then fill this section. Never
mark a UI PR ready with this section empty. Full recipe: `bd memories screenshot`.
-->

## Test plan

<!--
Every box must be checked before the PR is ready. Manual browser steps are NOT
optional for web-mobile / web changes — run them in a real browser, don't just
read the diff. See CONTRIBUTING and CLAUDE.md.
-->

- [ ] `make go/test` passes (lint + security scan + tests)
- [ ] New/updated unit tests cover the change
- [ ] Manual browser verification (for `web-mobile/` or `web/` changes) — describe what you exercised
- [ ] Screenshots added above (REQUIRED for any UI-affecting change)
- [ ] No new console errors or warnings

<!-- Bead reference: -->
Closes mp-xxxx

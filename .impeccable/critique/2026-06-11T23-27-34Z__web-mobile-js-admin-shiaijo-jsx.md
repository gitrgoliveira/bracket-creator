---
target: shiaijo operator view (admin_shiaijo.jsx)
total_score: 32
p0_count: 0
p1_count: 1
timestamp: 2026-06-11T23-27-34Z
slug: web-mobile-js-admin-shiaijo-jsx
---
# Design Critique — Shiaijo operator view (/admin/shiaijo/:court)

## Design Health Score — 32/40 (Good; clear, on-system, focused refinements left)

| # | Heuristic | Score | Key Issue |
|---|-----------|-------|-----------|
| 1 | Visibility of System Status | 3 | Now/Up next/Completed + ● NOW ring clear; redundant court pill noise |
| 2 | Match System / Real World | 4 | Kendo vocabulary correct (Shiaijo, Shiro/Aka, Score/Correct) |
| 3 | User Control and Freedom | 3 | Court switcher + breadcrumb back + modal close |
| 4 | Consistency and Standards | 4 | Faithful reuse of score-edit-row / page-head / badges |
| 5 | Error Prevention | 4 | key={court} remount, courtKnown guard, group-gating |
| 6 | Recognition Rather Than Recall | 3 | Names+dojos+times present; court pill is recognition noise |
| 7 | Flexibility and Efficiency | 3 | Court switcher; no list-level keyboard nav |
| 8 | Aesthetic and Minimalist Design | 2 | Redundant court column, sparse composition, 7px badges |
| 9 | Error Recovery | 3 | Unknown-court back link; empty state points to Schedule |
| 10 | Help and Documentation | 3 | Empty states teach; subhead explains the view |

## Anti-Patterns Verdict
Not AI slop. detect.mjs on admin_shiaijo.jsx → [] (exit 0). Disciplined reuse of an established design system; domain-correct; no gradient text/glassmorphism/decorative motion. Now/Up next/Completed are functional buckets, not decorative eyebrows. Failure mode is over-faithfulness to the shared row, not strangeness.

## What's Working
1. Strong consistency (H4) — same vocabulary as the rest of the console.
2. Real error prevention (H5) — key remount, courtKnown guard, group-gating, all verified.
3. Empty/unknown states teach (H9/H10).

## Priority Issues
- [P1] Redundant court column: every row in /admin/shiaijo/A shows "A" though the whole page is court A. ShiaijoMatchRow is the component's own row, so drop the court cell and rebalance the grid. Command: /impeccable layout
- [P2] Sparse horizontal composition: ~250px dead gap between time block and matchup at >=768px; tighten grid, pull Shiro/Aka left. Command: /impeccable layout
- [P2] 7px Shiro/Aka badges sub-legible for bright-hall persona; bump to ~9-10px on this view or carry side with a tinted name cell. Command: /impeccable typeset
- [P3] No loading skeleton; brief empty/unknown flash possible on slow load. Command: /impeccable harden

## Persona Red Flags
Table operator (tablet, bright hall, time pressure): redundant "A" pill wastes a glance-slot; 7px badges hard to resolve under glare (relies on left=Shiro/right=Aka position). Wins: large reachable Score button; ● NOW ring + group order surface the active match.
Spectator/first-timer: not the audience for this admin route; N/A.

## Minor Observations
- Global AdminTopbar shows "● Live" — violates the project's own no-"live" copy rule. Pre-existing/shared, not this view; separate ticket.
- .score-edit-row collapses to 1fr below 640px; verify stacked order reads on phones.

## Questions to Consider
- If the court column is always the current court, what would earn that space? Queue position? Est. start? Match number?
- Does a single-court operator need the dojo subtext at all?

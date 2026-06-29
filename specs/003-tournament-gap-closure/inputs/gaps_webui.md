# Web UI Recommendations for Tournament Setup and Management

This document summarizes UX issues identified during a full tournament setup of "London Cup 2026" using the mobile admin UI, based on test-data CSV files and the `scripts/setup_tournament.py` reference configuration.

---

## Issue 1; FIXED: Competition overview shows tournament name instead of competition name

**Where**: Admin > Competition > Overview (any competition)
**What happens**: After creating a competition called "Teams" (Pools + Knockout, Team type), the Overview page header displays "London Cup 2026" (the tournament name) instead of "Teams" (the competition name). The subtitle reads "Individual · 36 players" instead of "Team · 36 teams". The breadcrumb also shows the tournament name. The Scores; edit page has the same problem.
**Impact**: Operators cannot tell which competition they are viewing. The competition type also shows "Individual" even though it was created as "Team".
**Resolution**: The overview page now correctly displays the competition name as the title, with the tournament name shown as an eyebrow breadcrumb above it. See `admin_competition.jsx`.

---

## Issue 2; FIXED: MATCHES DONE counter shows 0/0 after scoring all 36 pool matches

**Where**: Admin > Competition > Overview
**What happens**: After starting the Teams competition (36 pool matches generated) and scoring all 36 matches through the Scores; edit page (each match shows "Final" + "Correct"), the Overview still displays "0/0 MATCHES DONE" and "0% Progress".
**Impact**: Operators cannot determine competition progress from the overview. Likely related to Issue 1 (corrupted config makes the system unable to count matches).
**Resolution**: The `compMatchStats` helper now correctly counts completed matches by checking match status and filtering out byes/unresolved slots.

---

## Issue 3; MOSTLY FIXED: Pools-to-playoffs progression through the web UI

**Where**: Admin > Competition > Overview / Scores; edit
**What happens**: Originally there was no button or flow to create a linked playoff bracket from a completed pools competition. **Update**: A "Create playoff bracket" button now exists on the Scores; edit page, and `MaybeAutoCompletePools` in `engine/competition.go` auto-transitions the pool competition to "completed" when the last pool match is scored. The normal workflow; score all pool matches, pools auto-complete, click "Create playoff bracket", start the playoff; works end-to-end through the UI.
**Remaining gap**: There is no manual "Complete pools" button if auto-completion doesn't trigger (e.g., if an admin invalidated and re-validated the competition, or if a scored match was later reverted). In such cases the operator has no way to force pool completion through the UI.
**Impact**: The normal happy path works. The edge case of needing manual pool completion is operationally rare but has no UI workaround.

---

## Issue 4; FIXED: Scoring modal title shows tournament name, not competition name

**Where**: Admin > Competition > Scores; edit > Score modal
**What happens**: The scoring modal header shows "LONDON CUP 2026 -" followed by "Shiaijo A · 09:30". It should show the competition name "TEAMS -" to identify which competition is being scored.
**Impact**: When scoring matches across multiple competitions, operators cannot confirm which competition a match belongs to from the modal alone.
**Resolution**: The modal header now displays `compName` (the competition name) instead of the tournament name. See `admin_scoring_modal.jsx`.

---

## Issue 5; FIXED: Scored matches lose their shiaijo assignment and start time

**Where**: Admin > Competition > Scores; edit (match list)
**What happens**: Before scoring, each match correctly shows "09:30 / London Cup 2026 / A ·" (time, competition, court). After finishing a match via the scoring modal, the scored match row shows ", / London Cup 2026 / · "; the time disappears (replaced by ",") and the court letter is missing. Only the (incorrect) competition name remains.
**Impact**: The match list loses scheduling context after scoring, making it impossible to audit which court a match was played on or when it started.
**Resolution**: The backend `RecordMatchResult` in `engine/scoring.go` now preserves `Court` and `ScheduledAt` fields when the scoring UI omits them from the update payload (lines 59–74 for pool matches, lines 293–298 for bracket matches). The `buildPatch()` function in the frontend intentionally sends only scoring-related fields, and the backend merges them without zeroing scheduling metadata. This applies to both single-match finish and the chained "Finish + Start Next" path, since both use the same `buildPatch()` → `RecordMatchResult` pipeline.

---

## Issue 6; FIXED: Scoring state carries over between matches

**Where**: Admin > Scoring modal, "Finish + Next" flow
**What happens**: When using "Finish + Next" to advance through matches, the scoring state (selected ippons, fouls) from the previous match carries over into the next match's modal. The new match opens with pre-filled scores from the just-completed match instead of a clean slate. This causes accidental incorrect scores (e.g., a match ending 0-3 with unintended hansoku and fouls).
**Expected**: Each new match must open with a completely clean state; zero ippons, zero fouls, no selections.
**Impact**: Operators must manually clear leftover state or risk recording incorrect results. In rapid scoring scenarios, this produces corrupted standings.
**Resolution**: State initialization now resets correctly when the `match` prop changes. Each new match opens with clean defaults (0 points, 0 fouls, no draw toggle).

---

## Issue 7; FIXED: No way to record a tie (hikiwake) in the scoring modal

**Where**: Admin > Scoring modal, between the two player panels
**What happens**: In pool matches, a match can end in a tie (hikiwake / draw) when no points are scored by either side. The "vs" label between the two player panels is static text. There is no "X" button or toggle to mark the match as a tie. Without at least one score entry, there is no way to finish the match as a draw.
**Expected**: The "vs" area should include an "X" button (or toggle) that marks the match as a tie. Recording a tie should be a valid condition for finishing the match (not requiring any ippons).
**Impact**: Operators cannot record drawn matches through the UI, making pool scoring incomplete since ties affect standings.
**Resolution**: An "X" draw toggle button now appears in the center between the two sides with `aria-label="Mark as draw (hikiwake)"`. Clicking it clears both sides' points and marks the match as a draw.

---

## Issue 8; FIXED: "H" ippon is ambiguous: Hansoku and Hantei both use "H"

**Where**: Admin > Scoring modal, ippon buttons
**What happens**: The "H" button under each player's ippon row represents Hansoku (penalty point). However, in kendo, "H" can also mean a point awarded by Hantei (judge's decision). The top tab bar offers "Ippon / Hantei / Hikiwake (Draw)" as scoring modes, but "H" in Hantei mode and "H" in Ippon mode are indistinguishable. Since Hantei simply awards a point to one side (same as any ippon), the top-level scoring mode selector (Ippon / Hantei / Hikiwake) is unnecessary; the H button already covers both meanings.
**Expected**: Remove the top scoring mode tabs (Ippon / Hantei / Hikiwake). The H button should serve for both Hansoku ippon and Hantei point. The tie/draw functionality should be handled by the "X" button described in Issue 7.
**Impact**: The extra tabs add confusion without adding functionality. Operators may not know which tab to use for Hantei decisions.
**Resolution**: The top-level scoring mode tabs (Ippon/Hantei/Hikiwake) have been removed. The modal now shows only score slots, point buttons (M/K/D/T/H), and the draw toggle.

---

## Issue 9; FIXED: White (Shiro) and Red (Aka) sides are inconsistent across the UI

**Where**: Admin > Scoring modal and match list
**What happens**: The colour assignment (Red = Aka, White = Shiro) is inconsistent. In some views, White appears on the left and Red on the right; in others, it's reversed. The scoring modal sometimes shows AKA (RED) on the left and sometimes on the right. The match list rows also swap sides.
**Expected**: Standardise across all views: **White (Shiro) always on the left, Red (Aka) always on the right**. This matches the standard kendo convention where the first-called player (white) appears on the left in draw sheets.
**Impact**: Operators and viewers cannot rely on position to identify which side is which, leading to scoring errors.
**Resolution**: Layout is now standardised: Shiro (White, sideB) always on the left, Aka (Red, sideA) always on the right. This is consistent across the scoring modal, live match panel, and pool views.

---

## Issue 10; FIXED: "Finish + Next" button should read "Finish + Start Next"

**Where**: Admin > Scoring modal, bottom action bar
**What happens**: The button label reads "Finish + Next →". This implies the current match is finished and the modal simply navigates to the next match. However, the button both completes the current match AND opens the next one for scoring.
**Expected**: Rename to "Finish + Start Next →" to make it clear that two actions happen: (1) the current match is recorded as finished, and (2) the next match is opened and ready to score.
**Impact**: Minor clarity issue. Operators may not realise the current match is being finalised when they click.
**Resolution**: Button renamed to "Finish + Start Next →" in both individual and team scoring modals.

---

## Issue 11; OPEN: Team scoring modal does not scale for large teams (10+ players)

**Where**: Admin > Scoring modal (team match)
**What happens**: The tournament spec describes teams of 10 or more players (common in kachinuki but also possible in standard team matches). The current team scoring modal renders all bout rows in a single scrollable modal view. With 10+ bouts, the modal requires significant scrolling on a tablet; the summary row at the top is no longer visible while scoring later bouts.
**Expected**: For large teams, the modal should either (a) keep the summary row sticky/fixed at the top while bouts scroll, or (b) provide a paginated or collapsible bout view so operators can focus on the active bout without losing the running tally.
**Impact**: Operators scoring large team matches on tablets lose sight of the IV/PW running totals, increasing the risk of errors and slowing down the scoring workflow.

---

## Issue 12; OPEN: Schedule view lacks quick-access for table operators during live events

**Where**: Admin > Schedule
**What happens**: During a live tournament, table operators need to quickly find "what's next on my court." The current schedule view shows a multi-court grid with all courts visible, requiring horizontal scrolling on tablets. Operators must visually scan or use filters to locate their court's upcoming matches.
**Expected**: A court-specific quick view; either a one-tap court selector that persists across the session, or a bookmarkable URL (e.g., `/admin/schedule?court=A`) that shows only that court's timeline. This is distinct from the score editor's court-scoped Prev/Next (which navigates within scoring); this is about the schedule overview before entering scoring mode.
**Impact**: During busy tournaments with many courts, operators waste time navigating to their court's section. Combined with Gap 1 (role-based access) from `gaps_tournament_spec.md`, a court-scoped operator view would address both the navigation and the access-control needs simultaneously.

---

## Issue 13; OPEN: No way to record withdrawal (kiken) or no-show (fusenpai) in scoring modal

**Where**: Admin > Scoring modal (individual and team)
**What happens**: When a competitor withdraws mid-tournament (injury, illness) or fails to appear at court call, the operator must manually score a 2–0 result and remember which competitor defaulted. There is no "Kiken" or "No-show" button, no visual distinction between a fought 2–0 and a default 2–0, and no mechanism to flag the withdrawn competitor as ineligible for subsequent matches.
**Expected**: The scoring modal should offer a "Kiken" (withdrawal) and/or "Default win" action that: (a) auto-fills the default score (2–0 regulation, 1–0 if during encho), (b) records the match decision reason, and (c) marks the withdrawn competitor as ineligible, prompting the operator to resolve their remaining scheduled matches.
**Impact**: Without this, a withdrawn competitor can silently appear in later bracket rounds. The tournament manager must manually track and intervene, which is error-prone during a busy event. See `gaps_tournament_spec.md` Gap 13.

---

## Issue 14; OPEN: No team lineup submission or position management UI

**Where**: Admin > Competition (team type)
**What happens**: In team competitions, each team must submit a fighting order (lineup) assigning members to positions (Senpo, Jiho, Chuken, Fukusho, Taisho for 5-person teams). The app generates team matches with a fixed order based on the participant list. There is no UI to submit, view, or change the lineup, and no named positions are displayed.
**Expected**: Before a competition starts (or between rounds), team managers or the tournament admin should be able to assign team members to named positions. The team scoring modal should display position names alongside player names. Between rounds, the lineup should be editable and locked once the next match begins.
**Impact**: Teams currently manage lineups on paper. The app cannot enforce position constraints (e.g., Senpo and Taisho cannot be vacant) or validate lineup changes. For tournaments that require digital lineup submission, this is a blocker. See `gaps_tournament_spec.md` Gap 14.

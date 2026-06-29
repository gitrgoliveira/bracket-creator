# Tournament Spec: Feature Gaps

Gap analysis between the requirements in `running_a_kendo_tournament.md` and the current application. Each gap describes what the tournament needs, what exists today, and what's missing.

## What's Already Covered

The application fully supports the core tournament engine:

- Court (shiaijo) configuration at tournament and competition level
- Multiple competitions with parallel execution
- Pool → playoff phase transitions with auto-complete
- Individual and team match scoring (ippons, hansoku, decisions). **Limitation**: tied team knockout matches (IV and PW equal) require a daihyosen (representative ippon-shobu bout) per the spec, but the app has no mechanism to add a dynamic extra bout; the operator must work around this manually. See also Gap 9 (kachinuki) for the general dynamic-bout-count problem.
- Court assignment and drag-drop reassignment between shiaijo
- Side display (shiro/aka); fixed and consistent across all views
- Table operator scoring workflow with court-scoped Prev/Next navigation
- Public viewer (no auth) with live matches, pools, brackets, results
- Multi-court schedule overview with court swimlanes
- Real-time updates via SSE (match scores, competition events, schedule changes)
- Per-match scheduling with auto-schedule by duration
- Excel bracket export with Time Estimator sheet
- Seeding: standard bracket ordering, pool seeding with dojo-conflict avoidance (`helper/tournament.go` for pool assignment, `helper/seed.go` for seed ordering), court-aware seed spread
- Byes: auto-resolution for empty bracket slots, 1st-place pool finishers receive byes first (`engine/bracket.go`, `helper/tree.go`)
- Court allocation: contiguous pool-to-court distribution; manual match-to-court moves via `PUT /api/competitions/:id/matches/:matchId/court`
- Rankings: engine computes 1st, 2nd, and 3rd–4th (semi-final losers) from the bracket (`engine/ranking.go`)

### Out of Scope

The following tournament operations described in the spec are physical/logistical concerns that do not require app support:

- **Equipment inspection (kensa)**: shinai measurement, bogu checks, and nafuda verification are done at a physical inspection station with official gauges. No digital tracking needed.
- **Opening and closing ceremonies**: these are fixed-schedule events managed by the tournament director. The app could model them as time blocks in schedule estimation (see Gap 6) but does not need a ceremony management feature.
- **Referee rotation and breaks**: referee group assignment and rotation are managed by the Shinpan-cho. This is a staffing/logistics concern separate from the bracket and scoring app. The Time Estimator's "padding time for rotation" already accounts for the transition time between referee sets.
- **Medical and first aid**: handled by on-site medical personnel. The app's concern is recording the match outcome (kiken/withdrawal), not managing the medical response.
- **Awards presentation**: the bracket determines the final standings (1st, 2nd, 3rd–4th). Award distribution is a physical ceremony activity.

---

## Gap 1: Role-Based Access

**Priority**: High

**Tournament need**: Four distinct roles operate during a live event; tournament manager (global control), table operator (records scores at one shiaijo), court manager (oversees flow at one shiaijo), and ribbon helpers (physical role, no app access needed). The tournament manager can change anything. A table operator should only interact with matches on their assigned court.

**Current state**: A single shared password grants full admin access. Every authenticated user sees and can modify all courts, all competitions, and all tournament settings. The score editor's Prev/Next navigation is court-scoped, but the rest of the admin UI is not.

**What's missing**:

- A role model distinguishing at least two levels: **manager** (full access) and **operator** (scoped to one or more assigned courts).
- Backend enforcement: operator API calls should be rejected if the target match is not on their assigned court.
- Frontend scoping: operator view should filter the admin UI to only show their assigned court's matches, hiding tournament settings and other courts.
- Auth mechanism: either per-role passwords, or a manager-issued session/token per operator with court assignment baked in.

---

## Gap 2: Participant and Coach Self-Lookup

**Priority**: Medium

**Tournament need**: Participants need to know their upcoming match time and how many matches remain before theirs so they can warm up. Coaches need to see when their players or teams are scheduled and review completed match results.

**Current state**: The public viewer shows all competitions, schedules, and results. A `PlayerMultiFilter` allows searching by name or dojo. However, the "my matches" feature is an unimplemented placeholder (`myPlayer = null, myUpcoming = null` in `viewer.jsx`).

**What's missing**:

- A "Look up your name" entry point in the viewer; type or select a participant name and see a personalized view.
- Personalized schedule: upcoming matches for that participant, ordered by time, with court and opponent.
- Queue position: "You are 3rd in line on Shiaijo A"; count of scheduled matches ahead of theirs on the same court.
- Match history: completed matches with scores for that participant.
- Coach mode: filter by dojo/team to see all players from that group in one view. **Note**: `PlayerMultiFilter` on the schedule page already supports dojo-based filtering with case-insensitive substring matching; this could serve as the foundation for coach mode rather than building from scratch.
- Optional: shareable URL per participant (e.g., `/viewer?player=Name`) so a QR code at registration can link directly.

---

## Gap 3: Dedicated Court TV Display

**Priority**: High

**Tournament need**: Each shiaijo may have a TV display showing the current match score and the next 2 upcoming matches. This is visible to participants, coaches, and the audience standing near that court.

**Current state**: The viewer can be filtered to a single court, but the layout is designed for mobile phones; small text, full navigation chrome, interactive elements. Not suitable for a wall-mounted monitor.

**What's missing**:

- A `/display/:court` route with a TV-optimized layout:
  - **Current match**: large-font player names, side colors (shiro/aka), live score (ippons), court name.
  - **Next 2-3 matches**: player names and scheduled times in a visible queue below.
  - Auto-advancing: when a match completes, the display shifts to the next match via SSE.
- Full-screen, no navigation, no scroll; designed for a 1080p landscape monitor.
- Auto-reconnect on SSE disconnect with a visible connection-status indicator.

---

## Gap 4: Lobby / Outside Screen Display

**Priority**: Medium

**Tournament need**: A shared display visible from outside the venue shows progress across all shiaijo; who is currently fighting, on which court, and what's coming up next.

**Current state**: The schedule viewer shows a multi-court grid but requires scrolling, has interactive filters, and uses mobile-sized text. Not designed for passive viewing on a large screen.

**What's missing**:

- A `/display/all` route with a lobby-optimized layout:
  - Fixed grid of all active courts, one card per shiaijo.
  - Each card shows: court name, current match (player names + live score), next 2 upcoming matches (player names + scheduled time).
  - Competition name and phase visible per match.
- No interaction needed; pure read-only, auto-refreshing via SSE.
- Large font, high contrast, readable from 5+ meters.
- Auto-cycle or pagination if more courts than fit on one screen.

---

## Gap 5: Video Streaming Overlay

**Priority**: Low–Medium

**Tournament need**: Tournaments with a live video stream need to overlay player names and the current score on the broadcast. The streaming software (OBS, vMix, or similar) pulls this information from the app in real time.

**Current state**: No overlay endpoints or streaming-friendly views exist. The SSE infrastructure and REST API provide all the raw data needed, but there's no presentation layer designed for capture.

**What's missing**:

- A `/overlay/:court` route returning minimal HTML suitable for OBS browser-source capture:
  - Transparent or chroma-key background.
  - Current match: player names (with optional zekken), side colors, live score.
  - Updates via SSE; no polling, no page reload.
  - Configurable layout (bottom bar, side panel, or corner) via query parameters.
- JSON endpoint alternative: `/api/overlay/:court` returning just the current match data in a simple flat structure for custom integrations (vMix data sources, custom overlays).
- Player names on the overlay should use the **zekken/display name** field when available (the `withZekkenName` competition option), not just the registration name; this matches what's printed on the physical zekken worn during matches.

---

## Gap 6: Phase-Specific Match Duration

**Priority**: Low

**Tournament need**: Match duration may differ between phases; for example, 2 minutes in pools and 3 minutes in playoffs. The schedule should reflect these different durations when estimating match times.

**Current state**: The Excel Time Estimator sheet supports separate pool and elimination durations (defaults: 3 min pools, 4 min elimination). The mobile app's auto-schedule feature only has a single "minutes per match" input that applies uniformly to all matches.

**What's missing**:

- Per-phase duration fields on the `Competition` model: `poolMatchDuration` and `playoffMatchDuration` (in minutes).
- Auto-schedule logic that uses the correct duration per phase when spacing matches.
- Admin UI: two duration inputs instead of one when the competition has both pools and playoffs.
- Optional: rotation/padding time between matches (the Excel sheet has a 30-second default, the mobile app does not).

**Note**: The Excel Time Estimator and the mobile app's auto-scheduler have divergent scheduling models; the Excel tool already supports per-phase durations with padding, while the mobile app uses a flat single value. Closing this gap should reconcile both models so they produce consistent time estimates.

---

## Gap 7: Match Queue Position

**Priority**: Low

**Tournament need**: Participants need to know how many matches remain before theirs (e.g., "10 matches left") so they can warm up in time.

**Current state**: The schedule shows ordered match lists per court, but no queue position is computed or displayed. A participant must manually count matches in the list.

**What's missing**:

- A computed `queuePosition` per scheduled match: count of non-completed matches ahead of it on the same court.
- Display in the viewer schedule: "Match 3 of 12 on Shiaijo A" or "2 matches before yours."
- Display on the court TV (Gap 3): queue numbers next to upcoming matches.
- Recalculated in real-time as matches complete (via SSE event handling).

---

## Gap 8: Partial Round-Robin for Large Pools

**Priority**: Low–Medium

**Tournament need**: In tournaments with large pools (8+ players), a partial round-robin may be used; each competitor fights only their adjacent neighbours in the pool list (e.g., position 1 vs 2, 2 vs 3, 3 vs 4) rather than every possible pairing. This significantly reduces the number of matches while still producing enough data for ranking.

**Current state**: Pool generation in `helper/tournament.go` produces full round-robin pairings only. There is no option for partial round-robin.

**What's missing**:

- A competition-level setting for round-robin mode: `full` (default) or `partial` (adjacent-neighbour pairings).
- Match generation logic that produces only adjacent-pair matches when partial is selected.
- The same pool ranking criteria apply; no changes needed to the ranking engine.

---

## Gap 9: Kachinuki (Winner-Stays-On) Format

**Priority**: Low

**Tournament need**: A team format where the winning fighter stays on to face the next opponent from the other team. Team fighting order is fixed. If a bout ends in hikiwake, both fighters retire and the next pair enters. The team that eliminates all opposing members wins. Teams can have 10+ players.

**Current state**: Not supported. The app handles standard team matches (fixed bout count, position-by-position) but not the dynamic progression of kachinuki.

**What's missing**:

- A kachinuki match type distinct from standard team matches.
- Dynamic bout progression: the next bout's matchup depends on the previous bout's result (winner stays, or both retire on draw).
- Variable bout count; the match ends when one team is eliminated, not after a fixed number of bouts.
- Scoring: team result is determined by eliminations, not IV/PW.
- UI: the scoring modal needs to handle dynamic bout addition rather than a fixed grid of bouts.

---

## Gap 10: Fusensho (Default Win for Absent Players)

**Priority**: Low

**Tournament need**: If a team is missing a player for a position, the opposing fighter wins by default (fusensho) with a score of 2–0. This counts as one individual victory and two points for the winning side.

**Current state**: No explicit fusensho handling. A missing player in a team bout must be manually scored as 2–0 by the operator, with no indication that it was a default rather than a fought result.

**What's missing**:

- A "Fusensho" / "Default win" button or toggle in the team scoring modal that auto-fills 2–0 for the bout.
- Visual distinction between fought and defaulted bouts in the scoreboard and results (e.g., "Fus." label).
- Correct accounting: fusensho 2–0 must contribute to IV and PW totals for the winning team.

---

## Gap 11: Swiss System Format

**Priority**: Low

**Tournament need**: Large individual tournaments may use Swiss pairing; competitors are paired each round based on similar records (winners face winners). After a set number of rounds, final standings are determined by cumulative results. Avoids early elimination while requiring fewer matches than full round-robin.

**Current state**: Not supported. The app supports pools (round-robin) and playoffs (single elimination) only.

**What's missing**:

- A Swiss competition format option.
- Round-by-round pairing engine that matches competitors with similar records.
- Configurable number of Swiss rounds.
- Cumulative standings computation across rounds.
- UI for managing round progression (generate next round pairings after current round completes).

---

## Gap 12: League (Full Round-Robin) Format

**Priority**: Low

**Tournament need**: The spec describes a League format where all competitors play every other competitor (or all teams play every other team) with no knockout stage. Final standings are determined entirely from cumulative results using the same pool ranking criteria. Used for smaller fields or regional leagues that run across multiple sessions.

**Current state**: The competition lifecycle already supports league-style completion. `MaybeAutoCompletePools` in `engine/competition.go` transitions a "pools" format competition directly from `CompStatusPools` to `CompStatusComplete` when every pool match is scored; no playoff bracket is required. The pool engine generates full round-robin pairings, computes standings, and the competition auto-completes. An operator who wants a league can simply create a "pools" format competition and never invoke `POST /competitions/:id/playoffs`. The "Create playoff bracket" button on the Scores; edit page is optional.

**What's missing**:

- An explicit "league" format label in the competition setup UI, so operators don't have to know that "create pools and don't create playoffs" is the league workflow.
- UI clarity: the Scores; edit page still shows the "Create playoff bracket" button after pool completion. In league mode, this button is misleading; it should be hidden or contextual.
- Results presentation: the pool standings page serves as the final result, but there is no "Final standings" header or competition-winner derivation. The UI still visually implies that playoffs are expected next.

**Note**: This is primarily a UX/labeling gap, not a lifecycle or engine gap. The core functionality works today; the missing piece is making the league path explicit and first-class rather than a side effect of not creating playoffs.

---

## Gap 13: Withdrawal (Kiken) and No-Show (Fusenpai) Handling

**Priority**: Medium

**Tournament need**: Two distinct scenarios require default-win handling beyond normal scoring:

1. **Kiken (withdrawal)**: A competitor withdraws mid-tournament due to injury, illness, or other reasons. The opponent receives 2–0 (regulation) or 1–0 (encho). The withdrawn competitor is **prohibited from participating in subsequent matches**. In team matches under FIK rules, specific positions must be vacated (Jiho for 1 withdrawal, Jiho + Fukusho for 2; Senpo and Taisho cannot be forfeited; 3+ withdrawals disqualify the team).

2. **Fusenpai (no-show)**: A competitor fails to appear within ~5 minutes of being called to court. The opponent receives 2–0. The no-show competitor is prohibited from subsequent matches; same as kiken.

**Current state**: The scoring modal records match outcomes but has no way to distinguish a fought result from a withdrawal or no-show. There is no mechanism to prevent a withdrawn/no-show competitor from appearing in later matches.

**What's missing**:

- A match decision type beyond scored/draw: `kiken` (withdrawal) and `fusenpai` (no-show), recording the reason alongside the default score.
- Auto-fill of the default score (2–0 for regulation, 1–0 for encho kiken) when the operator selects kiken/fusenpai.
- **Prohibition enforcement**: once a competitor is marked as withdrawn or no-show, the app should flag or block their subsequent scheduled matches and prompt the operator to resolve them (award default wins to opponents or remove from bracket).
- For team kiken: validation that the correct positions are vacated per FIK rules (position-specific forfeiture constraints for 5-person teams).
- Visual distinction in match results and brackets: "Kiken" or "Fus." label alongside the score.

---

## Gap 14: Team Fighting Order (Lineup) Submission

**Priority**: Low–Medium

**Tournament need**: For team competitions, each team must submit their fighting order (lineup) before the competition starts. Standard positions in a 5-person team are Senpo (1st), Jiho (2nd), Chuken (3rd), Fukusho (4th), Taisho (captain, 5th). The lineup can be revised between rounds but must be submitted immediately after the team exits the court. The Senpo and Taisho positions cannot be left vacant.

**Current state**: Team matches are generated with a fixed position order based on the participant list. There is no concept of named positions, no lineup submission workflow, and no way for teams to change their fighting order between rounds.

**What's missing**:

- Named team positions (Senpo, Jiho, Chuken, Fukusho, Taisho for 5-person teams; numbered positions for other sizes).
- A lineup submission UI: before competition start, each team submits their member-to-position assignment.
- Between-round lineup changes: after a team match completes, the team can submit a revised order for the next round. The revised order must be locked before the next match starts.
- Validation: Senpo (1st) and Taisho (last) positions cannot be left vacant.
- Display: position names shown alongside player names in the team scoring modal and match results.

---

## Gap 15: Encho Period Tracking

**Priority**: Low

**Tournament need**: When a knockout match ends in a draw, an encho (overtime) period is played under ippon-shobu rules. Some tournaments allow multiple encho periods before moving to hantei. Encho duration may differ from regulation time. Hansoku may reset between encho periods in some tournament rules.

**Current state**: The scoring modal records the final match result but does not distinguish between points scored in regulation vs. encho. An operator records the outcome without the app tracking that an encho occurred.

**What's missing**:

- Encho tracking on the match result: a flag or counter indicating how many encho periods were played.
- Optional: separate hansoku tracking per period (if the tournament rules reset fouls between encho periods).
- Display: encho indicator on match results (e.g., "(E)" suffix) so viewers and coaches know the match went to overtime.
- This is cosmetic/informational; the match outcome is already correctly determined by the operator. The gap is in record-keeping fidelity.

---

## Gap 16: Schedule Estimation Improvements

**Priority**: Low

**Tournament need**: The spec describes that a match's total elapsed time is 1.5–2x the clock time due to transitions (bowing, ribbon tying, competitor changeover). For team matches, multiply by the number of bouts plus transition time between bouts. Schedule estimation should also include ceremony time (opening: 15–30 min, closing: 20–30 min), lunch breaks, and a 10–15% buffer for the slowest court.

**Current state**: The Excel Time Estimator supports per-phase durations with a 30-second padding default and divides by court count for parallel execution. The mobile app's auto-scheduler uses a single flat duration with no padding, ceremony time, or break accounting.

**What's missing**:

- **Clock-to-elapsed ratio**: the current 30-second padding in the Excel Time Estimator underestimates transition overhead. The spec suggests 1.5–2x the clock time; for a 3-minute match, that's 4.5–6 minutes total, not 3.5. The padding should be configurable or use a multiplier rather than a fixed offset.
- **Ceremony and break blocks**: the estimator should include configurable time blocks for opening ceremony, closing ceremony, and lunch break. These are significant (30–60 min combined) and affect the projected finish time.
- **Slowest-court buffer**: the parallel-court division (`sequential time ÷ courts`) is optimistic. The spec recommends a 10–15% buffer for the slowest court.
- **Team match duration**: team matches contain multiple bouts plus inter-bout transitions. The estimator should scale duration by bout count rather than treating a team match the same as an individual match.
- **Mobile app parity**: the mobile auto-scheduler should converge with the Excel Time Estimator's model (see also Gap 6 for per-phase durations).

---

## Implementation Priority

| Gap | Priority | Effort | Dependencies |
|-----|----------|--------|--------------|
| 1. Role-based access | High | High | None; standalone |
| 2. Participant self-lookup | Medium | Medium | None; builds on existing viewer + PlayerMultiFilter |
| 3. Court TV display | High | Medium | Benefits from Gap 7 (queue position) |
| 4. Lobby screen display | Medium | Medium | Benefits from Gap 3 (shared display components) |
| 5. Video streaming overlay | Low–Medium | Medium | Benefits from Gap 3 (shared display components) |
| 6. Phase-specific duration | Low | Low | None; isolated change |
| 7. Match queue position | Low | Low | None; isolated computation |
| 8. Partial round-robin | Low–Medium | Medium | None; extends pool generation |
| 9. Kachinuki format | Low | High | New match type with dynamic bout progression |
| 10. Fusensho handling | Low | Low | None; UI addition to team scoring modal |
| 11. Swiss system | Low | High | New competition format and pairing engine |
| 12. League format | Low | Low | UX-only: lifecycle already works; needs UI labeling and "Create playoff" button hiding |
| 13. Kiken + fusenpai handling | Medium | Medium | Affects match model, bracket progression, and subsequent match scheduling |
| 14. Team fighting order | Low–Medium | Medium | Needs team position model; affects team match generation and scoring modal |
| 15. Encho period tracking | Low | Low | None; metadata addition to match results |
| 16. Schedule estimation improvements | Low | Low–Medium | Extends existing Time Estimator; benefits from Gap 6 |

Gaps 6, 7, 10, 12, 15, and 16 are small, isolated changes that can be shipped independently at any time. Gaps 3, 4, and 5 share display infrastructure and are best planned together. Gap 2 is self-contained and can leverage the existing `PlayerMultiFilter`. Gap 13 (kiken/fusenpai) is medium-priority because it affects match integrity during live events; a withdrawn competitor appearing in later rounds is an operational error the app should prevent. Gap 14 (team fighting order) is relevant for any tournament using team competitions but can be deferred if teams currently manage lineups on paper. Gap 1 is the largest effort and the most impactful for operational safety during live events. Gaps 9 and 11 are entirely new competition formats requiring significant engine work; defer unless there is concrete demand.

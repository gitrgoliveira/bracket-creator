// Package engine, kachinuki "winner-stays-on" team match advancement.
//
// FR-044, data-model §4.1.
//
// Kachinuki is a team-match format where:
//
//   - Only the first bout is scheduled up front.
//   - After each bout the winner stays on the court and faces the next
//     un-retired player from the losing team.
//   - On a hikiwake (draw) BOTH players retire and the next pair from
//     each remaining roster advance.
//   - The encounter can be won two ways: by EXHAUSTION (one side has no
//     remaining un-retired players) or by the TAISHO-DEFEATED rule (the
//     taisho -- always the last fighter -- loses, so their team loses).
//     Team sizes are unregulated and lineup vacancies are not enforced,
//     so the app's roster snapshot is advisory: the shiaijo OPERATOR
//     declares the end ("End match"), the engine never auto-finalizes
//     (mp-gmcg). Both win rules persist as
//     domain.DecisionKachinukiExhaustion.
//   - A tied final bout is a drawn encounter in pools/league; a knockout
//     tie is resolved by encho on that same bout (daihyosen does not
//     exist in kachinuki).
//
// AdvanceKachinuki encapsulates the pure decision logic. Callers
// (typically a score handler, see handlers_match.go) pass a snapshot
// of the just-completed bout plus the remaining un-retired roster per
// side, and the engine returns either the next bout to schedule or a
// MatchEnded sentinel (advisory, logged only).
package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// AdvanceKachinukiInput is the minimal snapshot AdvanceKachinuki needs.
// The engine deliberately does NOT load the full match, callers pass
// the completed bout plus the un-retired roster per team so this
// function stays free of I/O and trivially unit-testable.
//
//   - LastBout: the bout that just completed. Decision-or-Winner
//     determines the advancement path. SideA / SideB names on the bout
//     identify which physical player just played for each team.
//   - SideA, SideB: remaining un-retired competitors per team in the
//     order they will take the court. SHOULD NOT include the players
//     that just played in LastBout (callers strip retired players
//     before passing the snapshot). The team names themselves are
//     carried on the parent MatchResult, not here.
type AdvanceKachinukiInput struct {
	LastBout state.SubMatchResult
	SideA    []string
	SideB    []string
}

// AdvanceKachinukiResult is the engine's verdict. Under the operator-led
// contract (mp-gmcg) the verdict is ADVISORY: completion only ever happens
// through an explicit operator score write, never from this result.
//
//   - Next: when non-nil, the next bout to schedule. Position is set to
//     LastBout.Position + 1; SideA/SideB carry the next pair of player
//     names. Other fields are left zero, the score handler will fill
//     them as the bout is played. After a hikiwake that leaves exactly
//     ONE side without a replacement, Next keeps THE FIGHTER WHO JUST
//     TIED on that side (under the taisho rule they stay on; the
//     operator never re-types the name) paired against the surviving
//     side's next fighter. Under plain exhaustion that fighter is
//     actually out: the operator gives the survivor the per-bout
//     fusensho and Ends on that point — the walkover that expresses the
//     surviving team's win (spec 006 decision 2).
//   - MatchEnded: true when the last bout had a WINNER and the loser's
//     ADVISORY roster is empty (the decisive point already exists, so no
//     walkover slot is appended). Next is nil. WinningSide is "A" or
//     "B"; Decision is domain.DecisionKachinukiExhaustion. Team sizes
//     are unregulated, so the snapshot may be wrong; MaybeAdvanceKachinuki
//     logs this and simply appends nothing — it never finalizes the match.
//   - BothExhausted: true when a hikiwake retired the last SNAPSHOT player
//     on both teams simultaneously. MatchEnded is false. Also advisory:
//     the operator ends the encounter (pool/league tie → drawn encounter;
//     a knockout tie continues via a further bout or encho on the final
//     bout — daihyosen does not exist in kachinuki).
type AdvanceKachinukiResult struct {
	Next        *state.SubMatchResult
	MatchEnded  bool
	WinningSide string // "A" or "B" when MatchEnded; "" otherwise
	Decision    string // domain.DecisionKachinukiExhaustion when MatchEnded

	// BothExhausted is true only when a hikiwake retired the last player on
	// BOTH teams at once (no winner determinable) in the ADVISORY snapshot.
	// AdvanceKachinuki cannot pick a winner and MaybeAdvanceKachinuki takes
	// no action beyond logging; the operator decides how the encounter ends
	// (drawn in pools/league, continued via next bout or encho in a
	// knockout).
	BothExhausted bool
}

// AdvanceKachinuki computes the post-bout transition.
//
// Branches:
//
//  1. LastBout.Winner names the SideA player → SideA stays on; we pair
//     them against the head of input.SideB.
//  2. LastBout.Winner names the SideB player → SideB stays on; we pair
//     them against the head of input.SideA.
//  3. LastBout is a hikiwake (Decision == domain.DecisionHikiwake or
//     Winner == "" with a recorded decision) → both retire; pair the
//     heads of input.SideA and input.SideB.
//  4. After a WIN, the loser's queue empty → MatchEnded=true, the winner's
//     side wins by exhaustion in the ADVISORY snapshot (the decisive point
//     already exists; nothing is appended). After a HIKIWAKE, exactly one
//     queue empty → append a slot keeping the FIGHTER WHO JUST TIED on
//     the replacement-less side, against the surviving side's next
//     fighter (taisho-rule continue with nothing to re-type; plain
//     exhaustion turns it into the walkover via per-bout fusensho —
//     spec 006 decision 2). BOTH empty → BothExhausted=true and no
//     winner. In every case the caller (MaybeAdvanceKachinuki) never
//     finalizes — the match stays running until the operator ends it
//     with an explicit completed score write (mp-gmcg operator-led
//     contract).
//
// The function is pure: no I/O, no logging on the happy path. Unusual
// inputs (Winner not matching either side) log a warning so
// live-tournament operators get a breadcrumb when something downstream
// silently degraded. Simultaneous exhaustion (BothExhausted) logs a
// breadcrumb to trace the phase-dispatch flow.
func AdvanceKachinuki(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	last := in.LastBout
	// Hikiwake: explicit "hikiwake" decision is the canonical signal.
	// We deliberately don't treat "empty Winner + any decision" as a
	// draw because a non-hikiwake decision (kiken, fusenpai, …) should
	// have a Winner assigned by the score handler, an empty Winner
	// there is malformed input, not a draw.
	hikiwake := state.IsDraw(last.Decision)

	switch {
	case hikiwake:
		return advanceAfterHikiwake(in)
	case last.Winner == last.SideA && last.SideA != "":
		return advanceWinnerStays(last.SideA, last.Position, in.SideB, "A")
	case last.Winner == last.SideB && last.SideB != "":
		return advanceWinnerStays(last.SideB, last.Position, in.SideA, "B")
	default:
		// Unexpected: Winner is set but doesn't match either bout
		// side. Treat as a no-op (no advancement) so callers fall
		// back to manual scheduling instead of silently producing a
		// wrong pairing.
		log.Printf("engine.AdvanceKachinuki: unrecognized bout outcome, winner=%q sideA=%q sideB=%q decision=%q; no advancement",
			last.Winner, last.SideA, last.SideB, last.Decision)
		return AdvanceKachinukiResult{}
	}
}

// advanceWinnerStays builds the next-bout descriptor when one side's
// player stays on. The opposing side's queue (`oppQueue`) must contain
// the next un-retired opponent at index 0. The `winnerSide` param is
// just for the exhaustion-end path's WinningSide field.
func advanceWinnerStays(stayingName string, lastPos int, oppQueue []string, winnerSide string) AdvanceKachinukiResult {
	if len(oppQueue) == 0 {
		// Opposing team is exhausted, current side wins.
		return AdvanceKachinukiResult{
			MatchEnded:  true,
			WinningSide: winnerSide,
			Decision:    string(domain.DecisionKachinukiExhaustion),
		}
	}
	nextOpp := oppQueue[0]
	// Preserve the canonical SideA/SideB role from the previous bout:
	// when SideA's player stays, they remain SideA in the new bout;
	// when SideB's player stays, they remain SideB.
	var sideA, sideB string
	if winnerSide == "A" {
		sideA, sideB = stayingName, nextOpp
	} else {
		sideA, sideB = nextOpp, stayingName
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position: lastPos + 1,
			SideA:    sideA,
			SideB:    sideB,
		},
	}
}

// advanceAfterHikiwake builds the next-bout descriptor after a tie.
// Both sides have a replacement → both retire, pair the heads of each
// remaining queue. Exactly one side without a replacement → the fighter
// who just tied STAYS on the slot (under the taisho rule a drawing
// Taisho continues; the operator never re-types the name), paired
// against the surviving side's next fighter — under plain exhaustion
// the operator gives the survivor the per-bout fusensho and Ends on
// that point (spec 006 decision 2: the extra bout IS how the win is
// expressed). Both empty → BothExhausted (advisory: the operator ends
// the encounter, the engine never finalizes — mp-gmcg).
func advanceAfterHikiwake(in AdvanceKachinukiInput) AdvanceKachinukiResult {
	switch {
	case len(in.SideA) == 0 && len(in.SideB) == 0:
		// Both teams ran out simultaneously after a draw, per the ADVISORY
		// roster snapshot. The engine cannot determine a winner; flag
		// BothExhausted and append nothing. The operator ends the encounter:
		// drawn in pools/league, continued (next bout or encho) in a
		// knockout. See MaybeAdvanceKachinuki.
		log.Printf("engine.AdvanceKachinuki: hikiwake exhausted both teams simultaneously at position %d (advisory); operator ends the encounter",
			in.LastBout.Position)
		return AdvanceKachinukiResult{BothExhausted: true}
	case len(in.SideA) == 0:
		// SideA's ADVISORY roster has no replacement but SideB does: the
		// fighter who just tied STAYS ON THE SLOT (operator ruling: under
		// the taisho rule a drawing Taisho continues, and the operator must
		// not have to re-type the name), paired against SideB's next
		// fighter. The slot is advisory like every append and serves both
		// modes: under plain exhaustion the tied fighter is actually out,
		// so the operator gives the surviving fighter the per-bout fusensho
		// and Ends on that point (the walkover); under the taisho rule the
		// pairing is fought as-is. An abandoned trailing unscored slot is
		// stripped on the completed write.
		log.Printf("engine.AdvanceKachinuki: hikiwake left side A without a replacement at position %d (advisory); pairing %s against %s",
			in.LastBout.Position, in.LastBout.SideA, in.SideB[0])
		return AdvanceKachinukiResult{
			Next: &state.SubMatchResult{
				Position: in.LastBout.Position + 1,
				SideA:    in.LastBout.SideA,
				SideB:    in.SideB[0],
			},
		}
	case len(in.SideB) == 0:
		log.Printf("engine.AdvanceKachinuki: hikiwake left side B without a replacement at position %d (advisory); pairing %s against %s",
			in.LastBout.Position, in.SideA[0], in.LastBout.SideB)
		return AdvanceKachinukiResult{
			Next: &state.SubMatchResult{
				Position: in.LastBout.Position + 1,
				SideA:    in.SideA[0],
				SideB:    in.LastBout.SideB,
			},
		}
	}
	return AdvanceKachinukiResult{
		Next: &state.SubMatchResult{
			Position: in.LastBout.Position + 1,
			SideA:    in.SideA[0],
			SideB:    in.SideB[0],
		},
	}
}

// RetiredPlayersFromBoutLog walks a bout log and returns, per side,
// the set of player names that have retired (lost or hikiwake'd out)
// up to and including the supplied log. The returned maps key off
// player name; presence == retired.
//
// Helper for callers building AdvanceKachinukiInput.{SideA,SideB} from
// a roster, they subtract retired names from the initial roster to
// derive the remaining un-retired queue.
//
// teamAName / teamBName are the parent MatchResult.SideA / SideB
// (the team names), used to disambiguate which side won each bout.
func RetiredPlayersFromBoutLog(boutLog []state.SubMatchResult, teamAName, teamBName string) (retiredA, retiredB map[string]struct{}) {
	retiredA = map[string]struct{}{}
	retiredB = map[string]struct{}{}
	for _, b := range boutLog {
		if b.Position == state.DaihyosenSubPosition {
			// The daihyosen (rep bout) is not a kachinuki bout: its side
			// names are the representatives (often the team names), not
			// roster players, so it must not retire anyone.
			continue
		}
		hikiwake := state.IsDraw(b.Decision)
		if hikiwake {
			if b.SideA != "" {
				retiredA[b.SideA] = struct{}{}
			}
			if b.SideB != "" {
				retiredB[b.SideB] = struct{}{}
			}
			continue
		}
		// Map per-bout winner to the team side. A team-name match on
		// the parent (b.Winner == teamAName) is the legacy synth path
		// from quick-score; the per-player path keys on the bout's
		// SideA/SideB names.
		switch b.Winner {
		case b.SideA, teamAName:
			// SideA player stays; SideB player retires.
			if b.SideB != "" {
				retiredB[b.SideB] = struct{}{}
			}
		case b.SideB, teamBName:
			if b.SideA != "" {
				retiredA[b.SideA] = struct{}{}
			}
		}
	}
	return retiredA, retiredB
}

// FilterRemaining returns roster entries that are NOT present in the
// retired set, preserving original order. Helper for callers building
// AdvanceKachinukiInput.{SideA,SideB} from a roster and a retired set
// produced by RetiredPlayersFromBoutLog.
func FilterRemaining(roster []string, retired map[string]struct{}) []string {
	out := make([]string, 0, len(roster))
	for _, name := range roster {
		if _, gone := retired[name]; gone {
			continue
		}
		out = append(out, name)
	}
	return out
}

// describeKachinukiResult is a stringer used by the handler when
// logging an advancement decision. Pure helper, no behaviour.
func describeKachinukiResult(r AdvanceKachinukiResult) string {
	if r.MatchEnded {
		return fmt.Sprintf("MatchEnded winningSide=%s decision=%s", r.WinningSide, r.Decision)
	}
	if r.Next != nil {
		return fmt.Sprintf("Next position=%d sideA=%q sideB=%q", r.Next.Position, r.Next.SideA, r.Next.SideB)
	}
	return "no-op"
}

// appendNextKachinukiBout appends the engine-produced next bout to a
// bracket match's log, mirroring the pool mutate closure (GAP 4): the
// encounter stays running with no match-level winner or decision.
// Shared by the rounds loop and the bronze (3rd-place) branch.
func appendNextKachinukiBout(bm *state.BracketMatch, next state.SubMatchResult) {
	next.Position = len(bm.SubResults) + 1
	bm.SubResults = append(bm.SubResults, next)
	bm.Status = state.MatchStatusRunning
	bm.Winner = ""
	bm.Decision = ""
}

// MaybeAdvanceKachinuki runs the post-score side effect for a
// kachinuki team match.
//
// The score endpoint (handlers_match.go) calls this AFTER
// RecordMatchResult* has persisted the operator's bout. Steps:
//
//  1. Load the competition; bail out as no-op if it's not a kachinuki
//     team competition.
//  2. Load the just-recorded MatchResult; bail if its last SubResults
//     entry has no final outcome (still in progress).
//  3. Build the remaining-roster snapshot per side from the saved
//     TeamLineup (GAP 1/2a), falling back to the unique player names
//     seen in the bout log when no lineup is saved.
//  4. Pass to AdvanceKachinuki. When it returns Next, append the bout
//     to SubResults and persist (status stays Running). It NEVER
//     finalizes the parent match: completion is operator-led (mp-gmcg).
//     A MatchEnded/BothExhausted verdict is advisory only (team sizes
//     are unregulated, so the roster snapshot may be incomplete); the
//     operator ends the encounter with an explicit completed score
//     write from the score editor.
//
// Returns (changed, error). `changed` indicates whether SubResults or
// the parent match was mutated, handler uses it to decide whether to
// emit an additional match-updated SSE event with the freshly-derived
// bout list.
//
// FR-044, T135, T137.
func (e *Engine) MaybeAdvanceKachinuki(compID, matchID string) (bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, err
	}
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return false, nil
	}

	// Locate the parent match in either the pool or bracket store:
	// advancement runs in both (bracket bouts append via
	// appendNextKachinukiBout, with propagateBracketWinner on
	// exhaustion).
	parent, isBracket, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return false, err
	}
	if parent == nil || len(parent.SubResults) == 0 {
		return false, nil
	}
	// A completed match is final: corrections re-submit the bout log of a
	// finished match and must never re-run advancement (which would append
	// a phantom next bout onto the completed result). Defense in depth on
	// top of the handler's kachinukiBoutFinal gating.
	if parent.Status == state.MatchStatusCompleted {
		return false, nil
	}

	// Advancement is driven by the last NUMBERED bout. A daihyosen sub-result
	// (Position == DaihyosenSubPosition) is not a kachinuki bout: a bracket
	// encounter that reaches simultaneous exhaustion stays open until the
	// operator adds a daihyosen, and mergeKachinukiSubResults orders that row
	// last, so keying off the final slice element would advance off the rep
	// bout. Scan from the end past any daihyosen placeholder to the real bout.
	lastIdx := -1
	for i := len(parent.SubResults) - 1; i >= 0; i-- {
		if parent.SubResults[i].Position != state.DaihyosenSubPosition {
			lastIdx = i
			break
		}
	}
	if lastIdx < 0 {
		// Only the daihyosen placeholder is present: nothing to advance off.
		return false, nil
	}
	last := parent.SubResults[lastIdx]
	// Only act when the last bout has a final outcome. A bout written
	// with no Winner AND no Decision is still being scored; bail.
	hasOutcome := last.Winner != "" || last.Decision != ""
	if !hasOutcome {
		return false, nil
	}
	// Identity guard: retirement math needs to know WHO fought. A bout
	// carrying an outcome but no side names (e.g. a client that could not
	// resolve the lineup submitted a nameless hikiwake) retires nobody,
	// and advancing off it would append a wrong pairing and shift the
	// whole sequence by one. Refuse loudly and leave the match untouched
	// so the operator can correct the bout.
	if last.SideA == "" && last.SideB == "" {
		log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s: last bout (position %d) has an outcome but no side names; skipping advancement", compID, matchID, last.Position)
		return false, nil
	}

	// Build remaining-roster snapshot. When a TeamLineup has been saved
	// for the team, use the full ordered roster filtered by bout-log
	// retirements (A2, GAP 1 / GAP 2a). Without a lineup the function
	// degrades to the bout-log-only heuristic so existing competitions
	// without lineups continue to work.
	remainingA, remainingB, rosterAvailable := e.kachinukiRemainingRoster(compID, matchID, comp, parent, roundIdx)

	out := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: last,
		SideA:    remainingA,
		SideB:    remainingB,
	})
	log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s rosterAvailable=%t result=%s",
		compID, matchID, rosterAvailable, describeKachinukiResult(out))

	// Operator-led completion (mp-gmcg): the engine NEVER auto-finalizes a
	// kachinuki encounter. Team sizes are unregulated and lineup vacancies
	// are not enforced, so the roster snapshot above is advisory: a side
	// that looks exhausted may still have fighters the app has never seen.
	// MatchEnded/BothExhausted are logged (breadcrumb above) but not acted
	// on; the shiaijo operator ends the match explicitly from the score
	// editor ("End match"), which arrives as a normal completed score write
	// (winner from the last decisive bout, covering both win rules -- full
	// exhaustion AND taisho-defeated -- or hikiwake in pools/league; a
	// knockout tie is resolved by encho on the final bout, never a draw).
	if out.Next == nil {
		return false, nil
	}

	// Persist via the matching atomic primitive.
	mutate := func(parent *state.MatchResult) {
		// Append the next bout. The handler's broadcast carries the
		// updated subResults so SSE consumers see the new pairing.
		// Appending means the encounter continues: the parent match must
		// stay running with no match-level winner/decision.
		out.Next.Position = len(parent.SubResults) + 1
		parent.SubResults = append(parent.SubResults, *out.Next)
		parent.Status = state.MatchStatusRunning
		parent.Winner = ""
		parent.Decision = ""
	}

	if isBracket {
		if err := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
			if bracket == nil {
				return notFoundErrorf("bracket not found for competition %s", compID)
			}
			for rIdx := range bracket.Rounds {
				for mIdx := range bracket.Rounds[rIdx] {
					if bracket.Rounds[rIdx][mIdx].ID == matchID {
						appendNextKachinukiBout(&bracket.Rounds[rIdx][mIdx], *out.Next)
						return nil
					}
				}
			}
			// The Naginata 3rd-place (bronze) match is a sibling of
			// bracket.Rounds, not an element of it, so the loop above never
			// reaches it.
			if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
				appendNextKachinukiBout(bm, *out.Next)
				return nil
			}
			return notFoundErrorf("bracket match %s not found", matchID)
		}); err != nil {
			return false, err
		}
		return true, nil
	}

	if found, err := e.store.UpdatePoolMatchByID(compID, matchID, mutate); err != nil {
		return false, err
	} else if !found {
		return false, nil
	}
	return true, nil
}

// Sentinel errors for the operator-led reopen path (mp-gmcg, spec 006
// decision 4). Both map to HTTP 409 in the handler, as does the third
// reopen conflict — a busy court — which deliberately reuses the existing
// ErrCourtBusy / *CourtBusyError pair (eligibility.go) rather than minting
// a reopen-specific twin, so the "court already has a running match"
// condition has exactly ONE sentinel and one wire shape across the score
// and reopen paths.
var (
	// ErrReopenNotCompleted: only a COMPLETED kachinuki match can be
	// reopened; a running match needs no reopen and a scheduled one has
	// nothing to reopen.
	ErrReopenNotCompleted = errors.New("match is not completed; only a completed kachinuki match can be reopened")
	// ErrReopenDownstreamFought: the reopened match's winner was already
	// propagated into a downstream knockout match that has started or
	// recorded results; reopening would corrupt the bracket.
	ErrReopenDownstreamFought = errors.New("cannot reopen: a downstream knockout match has already started or recorded a result")
)

// ReopenKachinukiMatch is the sanctioned "Reopen match" path for a
// COMPLETED kachinuki team match (mp-gmcg, spec 006 decision 4): status
// back to running, match-level winner/decision cleared, the full bout log
// kept, so the operator can add more bouts and later End match again.
//
// `reason` is an OPTIONAL audit justification, persisted as the match's
// CorrectionReason when supplied. Reopening is the only way to rewrite a
// finalized kachinuki result without going through the score path's
// correction gate (which requires a correctionReason of its own), so the
// justification cannot simply be dropped — but demanding it HERE was too
// much friction: an operator who ended a match by mistake, at a shiaijo,
// mid-session, had to compose a reason before they could get back in.
// Reopen is therefore one tap, and when no reason is given the match is
// flagged ReopenPending instead: the score path then refuses to complete it
// again without a correctionReason and clears the flag once one lands
// (mp-gmcg). The audit record is written LATER than the action it
// justifies; it is never written at all only if the match is never ended
// again, in which case there is no rewritten result to justify.
//
// The flag is persisted rather than held client-side because the score
// editor mounts per match: navigating away and back would lose it.
//
// A supplied reason is trimmed here so a padded reason can never be
// persisted; the caller (the reopen handler) rejects an oversized one.
//
// COURT GATE. Reopening puts the match back in the RUNNING state, and
// court exclusivity keys purely on `status == running`
// (courtOccupiedInCompTx / checkCourtExclusivityTx). Reopening a match
// whose court already has a running match would therefore leave TWO
// running matches on one court, and the exclusivity check then rejects
// BOTH of them: the re-End of the reopened match AND every further score
// write to the genuinely live bout — i.e. reopening a past match would
// stop the operator scoring the match actually being fought. So a busy
// court REJECTS the reopen (*CourtBusyError, HTTP 409 court_busy).
// Semantically that is also the right answer: a reopen means "we need to
// fight more bouts", which really does take the court. The common case —
// a plain completed -> completed correction — is unaffected: it never
// re-enters the running state and the score handler's isCorrection skip
// already lets it through on a busy court. DO NOT remove this guard as a
// redundant-looking check.
//
// The cross-competition half runs BEFORE WithTransaction
// (CheckCrossCompCourtBusy takes read locks on other competitions, so
// calling it while holding this competition's write lock risks a
// circular wait); the same-competition half runs inside the tx off the
// same courtFreeInCompTx the score path reaches via
// checkCourtExclusivityTx. Both are skipped when the match has no court
// assigned.
//
// Kachinuki ONLY: for every other competition type the correction path
// (completed -> completed with a correctionReason) remains the sole
// sanctioned edit of a finished result, and this returns a
// *ValidationError (HTTP 400). The score path's stale-write guard
// (a plain running write against a completed match silently no-ops) is
// intentionally untouched; reopen is explicit and separate.
//
// Bracket matches: the completed result may already have been propagated
// downstream (propagateBracketWinner fills the next round's slot, and a
// semifinal feeds its loser to the bronze match). If any downstream
// target has started or recorded a result, reopen is rejected with
// ErrReopenDownstreamFought rather than corrupting the bracket. When the
// downstream slot is merely filled but unfought, the slot is reset the
// same way generation fills it: the next-round side returns to its
// "Winner of rX-mY" placeholder (the exact string propagateBracketWinner
// re-resolves on the next completion) and the bronze side to empty.
func (e *Engine) ReopenKachinukiMatch(compID, matchID, reason string) error {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return validationErrorf("reopen is only supported for kachinuki team matches; correct other results via the score editor (correctionReason)")
	}
	reason = strings.TrimSpace(reason)

	// Cross-competition court gate, deliberately OUTSIDE the transaction (see
	// the doc comment). Also surfaces the *NotFoundError for an unknown match
	// before any lock is taken.
	if err := e.CheckCrossCompCourtBusy(compID, matchID); err != nil {
		return err
	}

	var opErr error
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// guard is the SINGLE copy of the preconditions every match home
		// below (pool, bracket round, bronze) must pass: the match must be
		// completed, and its court must be free. Kept as one closure rather
		// than open-coded per branch because three hand-written copies is
		// exactly the shape that loses one when a fourth match home appears.
		//
		// The court half is the same-competition half of the reopen court
		// gate (see the COURT GATE note above): reopening flips the match
		// back to running, so a court that already has a running match would
		// end up with two, wedging the exclusivity check for BOTH. DO NOT
		// remove this guard as a redundant-looking check.
		guard := func(status state.MatchStatus, court string) error {
			if status != state.MatchStatusCompleted {
				return ErrReopenNotCompleted
			}
			return courtFreeInCompTx(tx, compID, matchID, court)
		}

		// Pool store first, mirroring findTeamMatch's lookup order.
		poolMatches, lerr := tx.LoadPoolMatches(compID)
		if lerr == nil {
			for i := range poolMatches {
				if poolMatches[i].ID != matchID {
					continue
				}
				if gerr := guard(poolMatches[i].Status, poolMatches[i].Court); gerr != nil {
					opErr = gerr
					return nil
				}
				m := &poolMatches[i]
				m.Status = state.MatchStatusRunning
				m.Winner = ""
				m.WinnerID = ""
				m.Decision = ""
				m.DecisionBy = ""
				m.DecisionReason = ""
				m.CorrectionReason = reason
				m.ReopenPending = reopenPending(reason)
				// SavePoolMatches funnels through the normal save chokepoint,
				// so standings caches invalidate via the usual version bump.
				return tx.SavePoolMatches(compID, poolMatches)
			}
		}

		bracket, lerr := tx.LoadBracket(compID)
		if lerr != nil {
			return lerr
		}
		if bracket != nil {
			for rIdx := range bracket.Rounds {
				for mIdx := range bracket.Rounds[rIdx] {
					bm := &bracket.Rounds[rIdx][mIdx]
					if bm.ID != matchID {
						continue
					}
					if gerr := guard(bm.Status, bm.Court); gerr != nil {
						opErr = gerr
						return nil
					}
					if derr := retractPropagatedWinner(bracket, rIdx, mIdx); derr != nil {
						opErr = derr
						return nil
					}
					reopenBracketMatch(bm, reason)
					return tx.SaveBracket(compID, bracket)
				}
			}
			// The bronze (3rd-place) match is a sibling of Rounds and has no
			// downstream match, so no retraction is needed.
			if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
				if gerr := guard(bm.Status, bm.Court); gerr != nil {
					opErr = gerr
					return nil
				}
				reopenBracketMatch(bm, reason)
				return tx.SaveBracket(compID, bracket)
			}
		}

		opErr = notFoundErrorf("match %s not found", matchID)
		return nil
	})
	if txErr != nil {
		return txErr
	}
	return opErr
}

// reopenBracketMatch flips a completed bracket match back to running,
// clearing the match-level outcome while keeping the bout log, and stamps
// the operator's audit reason (see ReopenKachinukiMatch).
func reopenBracketMatch(bm *state.BracketMatch, reason string) {
	bm.Status = state.MatchStatusRunning
	bm.Winner = ""
	bm.Decision = ""
	bm.DecisionBy = ""
	bm.DecisionReason = ""
	bm.CorrectionReason = reason
	bm.ReopenPending = reopenPending(reason)
}

// reopenPending reports whether a reopen still OWES an audit justification:
// true when no reason was supplied (the score path collects it on the next
// completion), false when the operator already gave one so this reopen is
// justified as it happens. One helper rather than three inline `reason == ""`
// tests, so the pool, bracket-round and bronze homes cannot drift — the same
// reason ReopenKachinukiMatch keeps its preconditions in a single `guard`
// closure.
func reopenPending(reason string) bool {
	return reason == ""
}

// retractPropagatedWinner undoes what propagateBracketWinner did for the
// match at (rIdx, mIdx): the next round's slot returns to its "Winner of
// rX-mY" placeholder and, for a semifinal feeding a bronze match, the
// bronze slot returns to empty. All downstream targets are CHECKED before
// any is mutated, so a rejection leaves the bracket untouched. A
// downstream match that has started, recorded bouts, or completed (which
// includes a bye auto-resolution off this match's winner) rejects the
// reopen with ErrReopenDownstreamFought.
func retractPropagatedWinner(bracket *state.Bracket, rIdx, mIdx int) error {
	var bronze *state.BracketMatch
	if bracket.ThirdPlaceMatch != nil && rIdx == len(bracket.Rounds)-2 {
		bronze = bracket.ThirdPlaceMatch
		if bracketMatchStartedOrScored(bronze) {
			return ErrReopenDownstreamFought
		}
	}
	var next *state.BracketMatch
	if rIdx+1 < len(bracket.Rounds) {
		next = &bracket.Rounds[rIdx+1][mIdx/2]
		if bracketMatchStartedOrScored(next) {
			return ErrReopenDownstreamFought
		}
	}
	if bronze != nil {
		// Mirror propagateBracketWinner's positional assignment: semifinal
		// mIdx 0 feeds the bronze SideA, mIdx 1 feeds SideB.
		if mIdx%2 == 0 {
			bronze.SideA = ""
		} else {
			bronze.SideB = ""
		}
	}
	if next != nil {
		// The placeholder format matches generation (bracket.go) and
		// parseWinnerOf: depth is 1-based from the final, so the source
		// match at round rIdx is depth len(Rounds)-rIdx.
		placeholder := fmt.Sprintf("Winner of r%d-m%d", len(bracket.Rounds)-rIdx, mIdx)
		if mIdx%2 == 0 {
			next.SideA = placeholder
		} else {
			next.SideB = placeholder
		}
	}
	return nil
}

// bracketMatchStartedOrScored reports whether a downstream bracket match
// is anything other than an untouched scheduled slot: running/completed
// status, a winner, recorded bouts, or a scoreline all block a reopen.
func bracketMatchStartedOrScored(bm *state.BracketMatch) bool {
	return bm.Status == state.MatchStatusRunning ||
		bm.Status == state.MatchStatusCompleted ||
		bm.Winner != "" ||
		len(bm.SubResults) > 0 ||
		bm.ScoreA != "" ||
		bm.ScoreB != ""
}

// applyKachinukiMerge merges an incoming kachinuki bout log into the stored
// prior log by position via mergeKachinukiSubResults. No-op for individual,
// fixed-format, or missing competitions. Shared by the locked and tx scoring
// paths so the merge guard cannot drift between them.
//
// On a COMPLETED write (the operator's explicit "End match", mp-gmcg) it
// additionally strips trailing UNSCORED bouts after the merge:
// MaybeAdvanceKachinuki auto-appends the next pairing after every scored
// bout, so ending the match leaves an abandoned empty pairing at the tail.
// Because the merge preserves stored entries by position, a client simply
// omitting that row cannot remove it, the server must strip it here so it
// never reaches standings/exports.
func applyKachinukiMerge(comp *state.Competition, prior, result *state.MatchResult) {
	if comp == nil || comp.TeamSize < 2 || comp.TeamMatchType != state.TeamMatchTypeKachinuki {
		return
	}
	var stored []state.SubMatchResult
	if prior != nil {
		stored = prior.SubResults
	}
	result.SubResults = mergeKachinukiSubResults(stored, result.SubResults)
	if result.Status == state.MatchStatusCompleted {
		result.SubResults = stripTrailingUnscoredKachinukiBouts(result.SubResults)
	}
}

// isUnscoredKachinukiBout reports whether a bout carries no recorded
// outcome or score at all: no winner, no decision, no real ippons on
// either side, no hansoku, no hantei, no encho marker. Such a row is the
// placeholder pairing MaybeAdvanceKachinuki appends for a bout that was
// never fought.
func isUnscoredKachinukiBout(s state.SubMatchResult) bool {
	return s.Winner == "" &&
		s.Decision == "" &&
		countScoringIppons(s.IpponsA) == 0 &&
		countScoringIppons(s.IpponsB) == 0 &&
		s.HansokuA == 0 &&
		s.HansokuB == 0 &&
		!s.DecidedByHantei &&
		s.Encho == nil
}

// stripTrailingUnscoredKachinukiBouts drops TRAILING unscored bouts from a
// kachinuki bout log (highest positions downward), stopping at the first
// scored row. Interior unscored rows are never touched (stripping them
// would renumber history), and the walk stops entirely on any row with
// Position <= 0 so a legacy daihyosen sentinel (Position -1, ordered last
// by mergeKachinukiSubResults) is never deleted or skipped over.
func stripTrailingUnscoredKachinukiBouts(subs []state.SubMatchResult) []state.SubMatchResult {
	end := len(subs)
	for end > 0 {
		s := subs[end-1]
		if s.Position <= 0 || !isUnscoredKachinukiBout(s) {
			break
		}
		end--
	}
	return subs[:end]
}

// mergeKachinukiSubResults merges an incoming kachinuki bout log into
// the stored one BY POSITION (ACID: a client whose local log is behind
// the server, a stale modal, a debounced autosave, or a second operator,
// must never destroy server-appended bouts). Incoming entries overwrite
// the stored entry at the same position; stored entries absent from the
// incoming patch are preserved, whether they are unplayed placeholders
// appended by MaybeAdvanceKachinuki, completed bouts, or the position -1
// daihyosen. Output order: numbered positions ascending, daihyosen last,
// matching the append order the advancement logic relies on (the LAST
// entry drives AdvanceKachinuki).
func mergeKachinukiSubResults(stored, incoming []state.SubMatchResult) []state.SubMatchResult {
	byPos := make(map[int]state.SubMatchResult, len(stored)+len(incoming))
	for _, s := range stored {
		byPos[s.Position] = s
	}
	for _, s := range incoming {
		byPos[s.Position] = s
	}
	numbered := make([]int, 0, len(byPos))
	hasDaihyosen := false
	for p := range byPos {
		if p == state.DaihyosenSubPosition {
			hasDaihyosen = true
			continue
		}
		if p < 0 {
			// Malformed negative position (not the daihyosen sentinel). Drop it
			// rather than preserve-and-sort-it-first, mirroring the defensive
			// Position <= DaihyosenSubPosition skip used by every aggregate
			// (state.TeamResultFrom etc.): real bouts are non-negative.
			continue
		}
		numbered = append(numbered, p)
	}
	sort.Ints(numbered)
	out := make([]state.SubMatchResult, 0, len(byPos))
	for _, p := range numbered {
		out = append(out, byPos[p])
	}
	if hasDaihyosen {
		out = append(out, byPos[state.DaihyosenSubPosition])
	}
	return out
}

// findTeamMatch locates a match by ID, returning the parent record (a
// copy), a flag indicating whether it was found in the bracket store
// rather than the pool store, and the bracket round index (0 for pool
// matches, rIdx for bracket matches, len(Rounds) for the ThirdPlaceMatch
// so round-scoped lineup resolution prefers the bronze's own stage,
// matching the client's derivedBracket.rounds.length).
func (e *Engine) findTeamMatch(compID, matchID string) (*state.MatchResult, bool, int, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				m := poolMatches[i]
				return &m, false, 0, nil
			}
		}
	}
	bracket, err := e.store.LoadBracket(compID)
	if err == nil && bracket != nil {
		for rIdx, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bracketMatchToTeamResult(bm), true, rIdx, nil
				}
			}
		}
		// The Naginata 3rd-place (bronze) match is a sibling of
		// bracket.Rounds, not an element of it; look it up here. Its
		// effective round index is len(Rounds) (one past the final round),
		// mirroring the client's derivedBracket.rounds.length so a
		// round-scoped lineup saved for the bronze stage resolves ahead of
		// an earlier round's lineup.
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
			return bracketMatchToTeamResult(*bm), true, len(bracket.Rounds), nil
		}
	}
	return nil, false, 0, nil
}

// bracketMatchToTeamResult projects a BracketMatch into the *MatchResult shape
// findTeamMatch returns for kachinuki lookups. It carries Court + ScheduledAt
// (unlike bracketMatchAsResult in bracket_result.go, which omits them and adds
// decision/encho/flag detail for the eligibility/rollback paths), so the two
// projections are deliberately distinct.
//
// SubResults is carried through by reference, same as bracketMatchAsResult:
// e.store.LoadBracket / tx.LoadBracket already deep-copy every BracketMatch
// (including SubResults, via Store.copyBracket) before handing the bracket
// back, so this projection is never aliased to the on-disk store cache. Only
// the FINDTEAMMATCH POOL branch's caller (MaybeAdvanceKachinuki's mutate
// closure) appends to a returned result's SubResults in place, and that
// closure only ever runs against the independently-loaded pool MatchResult
// from UpdatePoolMatchByID, never against a bracket-sourced result from this
// helper (the bracket branch mirrors Winner/Status directly onto the
// BracketMatch instead). If a future caller appends in place to a
// bracket-sourced result here, copy SubResults first (mirrors
// handlers_daihyosen.go's daihyosenBracketResult, which does exactly that for
// its own in-place-append call site).
func bracketMatchToTeamResult(bm state.BracketMatch) *state.MatchResult {
	return &state.MatchResult{
		ID:          bm.ID,
		SideA:       bm.SideA,
		SideB:       bm.SideB,
		Winner:      bm.Winner,
		Status:      bm.Status,
		Court:       bm.Court,
		ScheduledAt: bm.ScheduledAt,
		Decision:    bm.Decision,
		SubResults:  bm.SubResults,
	}
}

// kachinukiRemainingRoster derives the remaining un-retired roster per side
// for a team match. Returns (sideA, sideB, rosterAvailable). rosterAvailable
// is true when at least one side's roster was resolved from a saved TeamLineup;
// false means both sides fell back to the bout-log-only heuristic.
//
// Priority per side (AMENDMENT 1 / GAP 1 / GAP 2a):
//  1. Match-scoped lineup for this matchID.
//  2. Round-scoped lineup: highest round <= roundIdx.
//  3. Round-scoped lineup: highest round overall (fallback).
//  4. Bout-log-only heuristic (anyone who appeared in a bout, minus retired).
//
// The full ordered roster (from lineup.OrderedRoster) is filtered by
// RetiredPlayersFromBoutLog to produce the remaining queue.
func (e *Engine) kachinukiRemainingRoster(compID, matchID string, comp *state.Competition, parent *state.MatchResult, roundIdx int) ([]string, []string, bool) {
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)

	// Attempt lineup-based roster resolution.
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("engine.kachinukiRemainingRoster compId=%s matchId=%s: lineup load error: %v; falling back to bout-log-only", compID, matchID, err)
		lineups = nil
	}

	// The lineup editor keys lineups by the team PARTICIPANT ID
	// (player.id, a UUID) while match sides carry the team display NAME,
	// so translate each side name to its participant ID and try both keys
	// ("match on id OR name"). A participant load failure only degrades
	// the lookup to name-only; the bout-log fallback below still applies.
	var participants []domain.Player
	if len(lineups) > 0 {
		participants, err = e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
		if err != nil {
			log.Printf("engine.kachinukiRemainingRoster compId=%s matchId=%s: participant load error: %v; lineup lookup degrades to name-only", compID, matchID, err)
			participants = nil
		}
	}
	teamKeys := func(teamName string) []string {
		// Participant ID FIRST, then the display name. The lineup editor's
		// current storage key is the participant ID, so an id-keyed lineup
		// must win a same-round tie over a legacy name-keyed one:
		// FindBestLineupAny resolves same-tier ties by slice order. The name
		// stays as a fallback for lineups saved under it (older data, or a
		// team name that is not a participant id). Mirrors the id-first order
		// in kachinuki_export.go's teamKeys.
		var keys []string
		for _, p := range participants {
			if p.Name == teamName && p.ID != "" && p.ID != teamName {
				keys = append(keys, p.ID)
			}
		}
		return append(keys, teamName)
	}

	resolveRoster := func(teamName string, retired map[string]struct{}) ([]string, bool) {
		if lineups != nil {
			if lineup, found := state.FindBestLineupAny(lineups, teamKeys(teamName), matchID, roundIdx); found {
				full := lineup.OrderedRoster(comp.TeamSize)
				return FilterRemaining(full, retired), true
			}
		}
		// Preserve first-appearance order from the bout log: AdvanceKachinuki
		// treats this slice as an ordered queue (index 0 is the next fighter
		// in), so a map-iteration order would make the next pairing
		// nondeterministic when a kachinuki match runs without saved lineups.
		seen := map[string]struct{}{}
		out := make([]string, 0)
		isA := teamName == parent.SideA
		for _, b := range parent.SubResults {
			if b.Position == state.DaihyosenSubPosition {
				continue // rep bout, not a roster player (see RetiredPlayersFromBoutLog)
			}
			name := b.SideB
			if isA {
				name = b.SideA
			}
			if name == "" {
				continue
			}
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			if _, gone := retired[name]; gone {
				continue
			}
			out = append(out, name)
		}
		return out, false
	}

	remainingA, foundA := resolveRoster(parent.SideA, retiredA)
	remainingB, foundB := resolveRoster(parent.SideB, retiredB)
	return remainingA, remainingB, foundA || foundB
}

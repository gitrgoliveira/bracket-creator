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
//     finalizes the parent match: completion is operator-led (mp-gmcg,
//     the out.Next == nil path below). A MatchEnded/BothExhausted verdict
//     is advisory only (team sizes are unregulated, so the roster snapshot
//     may be incomplete); the operator ends the encounter with an explicit
//     completed score write from the score editor.
//
// Reports (advanced, postLog, err): `advanced` is whether SubResults or the
// parent match was mutated (the handler uses it to decide whether to emit an
// extra match-updated SSE event), and `postLog` is the FULL bout log AFTER the
// append when advanced is true (nil otherwise). Returning the log lets the
// caller echo the appended pairing to the open editor without re-reading the
// match from the store — the read this replaced was ~the 9th store read on a
// request already doing several, once per advancing bout, live (mp-gmcg review
// E1).
//
// FR-044, T135, T137.
func (e *Engine) MaybeAdvanceKachinuki(compID, matchID string) (bool, []state.SubMatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return false, nil, err
	}
	if !comp.IsKachinuki() {
		return false, nil, nil
	}

	// Locate the parent match in either the pool or bracket store:
	// advancement runs in both (bracket bouts append via
	// appendNextKachinukiBout, with propagateBracketWinner on
	// exhaustion).
	parent, isBracket, roundIdx, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return false, nil, err
	}
	if parent == nil || len(parent.SubResults) == 0 {
		return false, nil, nil
	}
	// A completed match is final: corrections re-submit the bout log of a
	// finished match and must never re-run advancement (which would append
	// a phantom next bout onto the completed result). Defense in depth on
	// top of the handler's kachinukiBoutFinal gating.
	if parent.Status == state.MatchStatusCompleted {
		return false, nil, nil
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
		return false, nil, nil
	}
	last := parent.SubResults[lastIdx]
	// Only act when the last bout has a final outcome. A bout written
	// with no Winner AND no Decision is still being scored; bail.
	hasOutcome := last.Winner != "" || last.Decision != ""
	if !hasOutcome {
		return false, nil, nil
	}
	// Identity guard: retirement math needs to know WHO fought. A bout
	// carrying an outcome but no side names (e.g. a client that could not
	// resolve the lineup submitted a nameless hikiwake) retires nobody,
	// and advancing off it would append a wrong pairing and shift the
	// whole sequence by one. Refuse loudly and leave the match untouched
	// so the operator can correct the bout.
	if last.SideA == "" && last.SideB == "" {
		log.Printf("engine.MaybeAdvanceKachinuki compId=%s matchId=%s: last bout (position %d) has an outcome but no side names; skipping advancement", compID, matchID, last.Position)
		return false, nil, nil
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
		return false, nil, nil
	}

	// postLog captures the FULL bout log AFTER the append, so the caller can
	// echo it without re-reading the match (E1). A fresh slice header keeps it
	// independent of the store's parse buffer.
	var postLog []state.SubMatchResult

	if isBracket {
		// UpdateBracketMatchByID owns the rounds → bronze-sibling walk, so the
		// append site no longer re-implements it (and can't forget the bronze).
		found, err := e.store.UpdateBracketMatchByID(compID, matchID, func(bm *state.BracketMatch) {
			appendNextKachinukiBout(bm, *out.Next)
			postLog = append([]state.SubMatchResult(nil), bm.SubResults...)
		})
		if err != nil {
			return false, nil, err
		}
		if !found {
			return false, nil, notFoundErrorf("bracket match %s not found", matchID)
		}
		return true, postLog, nil
	}

	found, err := e.store.UpdatePoolMatchByID(compID, matchID, func(parent *state.MatchResult) {
		// Append the next bout. Appending means the encounter continues: the
		// parent match must stay running with no match-level winner/decision.
		out.Next.Position = len(parent.SubResults) + 1
		parent.SubResults = append(parent.SubResults, *out.Next)
		parent.Status = state.MatchStatusRunning
		parent.Winner = ""
		parent.Decision = ""
		postLog = append([]state.SubMatchResult(nil), parent.SubResults...)
	})
	if err != nil {
		return false, nil, err
	}
	if !found {
		return false, nil, nil
	}
	return true, postLog, nil
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
	// ErrRemoveBoutNotRunning: bouts are removed from a RUNNING encounter only.
	// A completed kachinuki match already had its trailing unscored bouts
	// stripped on the End-match write, so there is nothing to remove; edit a
	// finished result via reopen/correction instead.
	ErrRemoveBoutNotRunning = errors.New("match is not running; reopen it before removing a bout")
	// ErrNoRemovableBout: the encounter has no trailing UNSCORED bout to drop —
	// the current bout is already scored, or there are no bouts. Surfaced so the
	// operator learns the empty-bout undo had nothing to act on rather than
	// getting a silent no-op.
	ErrNoRemovableBout = errors.New("no unscored bout to remove; only an empty appended bout can be removed")
)

// loadKachinukiComp loads the target competition and rejects it unless it is a
// kachinuki team competition — the shared prologue of ReopenKachinukiMatch and
// RequeueBlockerAndReopenKachinuki (mp-gmcg review R7: the two used to copy-paste
// this LoadCompetition → IsKachinuki → same literal operator sentence verbatim).
// It touches no court state, so callers run it OUTSIDE WithCourtExclusivityLock:
// a bad-input rejection need not serialize on the tournament-global lock. The
// reason is NOT handled here — reopenKachinukiUnderCourtLock, the single shared
// consumer, trims it (mp-gmcg review).
func (e *Engine) loadKachinukiComp(compID string) (*state.Competition, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if !comp.IsKachinuki() {
		return nil, validationErrorf("reopen is only supported for kachinuki team matches; correct other results via the score editor (correctionReason)")
	}
	return comp, nil
}

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
// (courtOccupied / checkCourtExclusivityTx). Reopening a match
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
// same courtFreeInCompTxWith the score path reaches via
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
	comp, err := e.loadKachinukiComp(compID)
	if err != nil {
		return err
	}

	// Court exclusivity is held HERE, inside the engine, so the
	// cross-competition pre-check and the same-competition in-transaction check
	// are atomic BY CONSTRUCTION — not by every caller remembering to wrap the
	// call in WithCourtExclusivityLock (mp-gmcg review A2). Without it a
	// concurrent match-start in ANOTHER competition could pass its own
	// cross-comp check and commit between our two halves, re-creating the
	// two-running-matches-on-one-court wedge the reopen court gate exists to
	// prevent. e.store owns the lock; nothing below re-takes it (non-reentrant),
	// and the ordering is courtCheckMu → per-comp lock, the same as the score
	// path. The LoadCompetition/validation above stay outside: they touch no
	// court state, so a bad-input rejection need not serialize on the lock.
	return e.store.WithCourtExclusivityLock(func() error {
		// Read-only RESULT preconditions BEFORE the court gates, so a plain reopen
		// of a permanently-unreopenable target (not completed, or its winner fed a
		// fought downstream) reports THAT — not a transient court_busy. The admin
		// remedy panel turns court_busy into an offer to requeue the court's
		// occupant (applyReopenFailure branches on code=="court_busy",
		// admin_scoring_team.jsx); without this pre-check a cross-comp court hold
		// (CheckCrossCompCourtBusy runs before the tx) would surface court_busy
		// first and steer the operator into that dead-end remedy for a target that
		// can never reopen (mp-gmcg review). checkTargetReopenable is the SAME
		// read-only pre-check the requeue path runs before its revert, and the
		// mutation below re-checks — so this only reorders which 409 wins. It lives
		// at this plain-reopen entry rather than in the shared body because the
		// requeue path already pre-checks before its revert and need not re-run it
		// (the check is read-only, so a second run would be wasted work, not a
		// hazard).
		if verr := e.checkTargetReopenable(compID, comp, matchID); verr != nil {
			return verr
		}
		return e.reopenKachinukiUnderCourtLock(compID, comp, matchID, reason)
	})
}

// reopenKachinukiUnderCourtLock runs the reopen body assuming the store's
// court-exclusivity lock is ALREADY held by the caller (mp-gmcg review A4), so a
// preceding blocker requeue and this reopen can share ONE lock section
// (RequeueBlockerAndReopenKachinuki) with no window for another match to grab
// the freed court. It MUST NOT take the court lock itself (the mutex is
// non-reentrant). comp is the kachinuki-validated target competition the caller
// already loaded. This is the SINGLE shared consumer of `reason`, so it trims
// it here (mp-gmcg review): a padded reason can never reach reopenPending /
// CorrectionReason regardless of which entry point called in.
func (e *Engine) reopenKachinukiUnderCourtLock(compID string, comp *state.Competition, matchID, reason string) error {
	reason = strings.TrimSpace(reason)
	// Cross-competition court gate, deliberately OUTSIDE the transaction (see
	// the doc comment). Its own *NotFoundError is now only a backstop: both entry
	// points surface an unknown match earlier (the plain entry via
	// checkTargetReopenable, the requeue entry via requireBlockerHoldsCourt), so
	// this gate's 404 is reachable only if the competition is deleted in the gap.
	if err := e.CheckCrossCompCourtBusy(compID, matchID); err != nil {
		return err
	}

	var opErr error
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// courtGate is the same-competition half of the reopen court gate (see
		// the COURT GATE note above): reopening flips the match back to running,
		// so a court that already has a running match would end up with two,
		// wedging the exclusivity check for BOTH. DO NOT remove it as a
		// redundant-looking check. It reuses the pool matches + bracket
		// findMatchHome already loaded under this tx for the same-comp scan,
		// instead of the re-load a nil/nil call would do (mp-gmcg review E4); a
		// nil slice (pool-load error, or the bracket on a pool home) reloads.
		courtGate := func(h matchHome, court string) error {
			return courtFreeInCompTxWith(tx, compID, matchID, court, h.PoolMatches, h.BracketRoot)
		}

		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			// Read-only RESULT preconditions (completed + downstream-not-fought),
			// the SAME check checkTargetReopenable runs, shared via
			// reopenResultPreconditionTx so neither path can add one the other
			// misses (mp-gmcg review). The pool branch's downstream refusal
			// matters because a pool finisher feeds the knockout INDIRECTLY via
			// the standings the bracket was seeded from (the score path's mp-e2k1
			// guard can't catch it: by re-End the reopened match is already out of
			// the standings baseline). These precede the SAME-competition court
			// gate below; the cross-comp gate (CheckCrossCompCourtBusy) already ran
			// before this tx, and the plain-reopen entry (ReopenKachinukiMatch)
			// pre-checks these preconditions before THAT — so an unreopenable target
			// is not masked by a transient court_busy on either gate, EXCEPT in the
			// same accepted race the requeue path documents: a /decision or
			// /bulk-score completing a downstream between the entry pre-check's tx
			// close and CheckCrossCompCourtBusy can still surface court_busy for a
			// now-unreopenable target (a retry then reports the permanent 409).
			if rerr := e.reopenResultPreconditionTx(tx, compID, comp, matchID, h); rerr != nil {
				opErr = rerr
				return nil
			}
			if h.Pool != nil {
				if cerr := courtGate(h, h.Pool.Court); cerr != nil {
					opErr = cerr
					return nil
				}
				reopenPoolMatch(h.Pool, reason)
				// SavePoolMatches funnels through the normal save chokepoint,
				// so standings caches invalidate via the usual version bump.
				return h.Save()
			}
			if cerr := courtGate(h, h.Bracket.Court); cerr != nil {
				opErr = cerr
				return nil
			}
			// A bracket ROUND winner may already be propagated downstream; the
			// bronze (3rd-place) match is a sibling with no downstream, so it
			// needs no retraction. downstreamFoughtForRound was already verified
			// above; retractPropagatedWinner re-checks it as its documented
			// check-before-mutate contract — a no-op here — then does the mutation.
			if !h.Bronze {
				if derr := retractPropagatedWinner(h.BracketRoot, h.RIdx, h.MIdx); derr != nil {
					opErr = derr
					return nil
				}
			}
			reopenBracketMatch(h.Bracket, reason)
			return h.Save()
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			opErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}
	return opErr
}

// RequeueBlockerAndReopenKachinuki atomically frees a court and reopens a
// kachinuki match onto it: under ONE hold of the court-exclusivity lock it
// requeues the blocking match and then reopens the target. A court hosts
// matches from ANY competition, and one competition spreads its matches across
// SEVERAL courts, so the blocker holding this match's court may be in the same
// competition (on this same court, a sibling of the target) or in a different
// one — either way it is the match occupying THIS court. Holding the lock
// across both steps closes the race the two-call client flow had (another
// operator could take the freed court between the requeue and the reopen —
// mp-gmcg review A4). RevertMatchToQueue takes only the blocker's
// per-competition lock, so calling it under the court lock keeps the
// courtCheckMu → per-comp ordering.
//
// The blocker id is CLIENT-SUPPLIED and RevertMatchToQueue is destructive (it
// clears the match's score), so requireBlockerHoldsCourt gates it FIRST: the
// named match must be running on the target's court, else the call is rejected
// without touching anything (mp-gmcg review R1). Without that gate a wrongly
// named bystander on a different court would be wiped AND the reopen would then
// fail on the court's real occupant, leaving the wipe committed but invisible
// behind a "court busy" response.
func (e *Engine) RequeueBlockerAndReopenKachinuki(targetComp, targetMatch, blockerComp, blockerMatch, reason string) error {
	comp, err := e.loadKachinukiComp(targetComp)
	if err != nil {
		return err
	}
	return e.store.WithCourtExclusivityLock(func() error {
		if verr := e.requireBlockerHoldsCourt(targetComp, targetMatch, blockerComp, blockerMatch); verr != nil {
			return verr
		}
		// Pre-check the TARGET's RESULT preconditions (completed +
		// downstream-not-fought) read-only BEFORE the destructive revert, so a
		// target that cannot be reopened — because its winner already fed a
		// fought knockout — does not cost the blocker its live on-court score
		// (mp-gmcg review). The court half is deliberately NOT checked here: the
		// revert is what frees the court, so the reopen's own court gate is the
		// authoritative one. This NARROWS the wipe window but does NOT close it:
		// PUT /score takes the court lock we hold, but POST /decision and POST
		// /bulk-score complete a match under the per-comp lock alone (see the
		// handler at handlers_match.go:801), so a downstream write landing between
		// this pre-check's tx close and the reopen's own in-tx re-check can still
		// make the reopen fail AFTER the revert — costing the blocker its score.
		// The reopen's re-check is the backstop that preserves bracket integrity
		// in that race regardless; closing the residual window deterministically
		// would need the pre-check, revert, and reopen under one target-comp
		// transaction, which the cross-comp revert (it takes the BLOCKER comp's
		// lock) cannot provide.
		if perr := e.checkTargetReopenable(targetComp, comp, targetMatch); perr != nil {
			return perr
		}
		if rerr := e.RevertMatchToQueue(blockerComp, blockerMatch); rerr != nil {
			return rerr
		}
		return e.reopenKachinukiUnderCourtLock(targetComp, comp, targetMatch, reason)
	})
}

// reopenResultPreconditionTx runs the read-only RESULT preconditions a reopen
// requires for one already-located match home: the match must be COMPLETED, and
// its winner must not have fed a fought downstream — a started knockout for a
// bracket round (downstreamFoughtForRound), or a started knockout seeded off this
// pool's current finisher for a pool match (checkPoolReopenDownstreamTx). It
// EXCLUDES the court gate (the requeue path frees the court itself; the plain
// reopen checks it separately) and performs NO mutation. checkTargetReopenable
// and reopenKachinukiUnderCourtLock both run it, so a RESULT precondition added to
// one path can't be missed by the other — the drift that reopens the
// wipe-for-nothing hazard (mp-gmcg review).
func (e *Engine) reopenResultPreconditionTx(tx state.StoreTx, compID string, comp *state.Competition, matchID string, h matchHome) error {
	if h.Pool != nil {
		if h.Pool.Status != state.MatchStatusCompleted {
			return ErrReopenNotCompleted
		}
		return e.checkPoolReopenDownstreamTx(tx, compID, comp, matchID)
	}
	if h.Bracket.Status != state.MatchStatusCompleted {
		return ErrReopenNotCompleted
	}
	// Bronze is a sibling of Rounds with no downstream, so it can never be
	// downstream-fought (matches reopenKachinukiUnderCourtLock's !Bronze).
	if !h.Bronze && downstreamFoughtForRound(h.BracketRoot, h.RIdx, h.MIdx) {
		return ErrReopenDownstreamFought
	}
	return nil
}

// checkTargetReopenable runs the read-only reopen RESULT preconditions (mp-gmcg
// review): it opens a target-competition tx and reports whether the match is
// completed and its winner has not fed a fought downstream, WITHOUT any court
// check and WITHOUT any mutation. Two callers run it before their court gates:
// the requeue-and-reopen path (before its destructive revert, so a target that
// can't reopen never costs the blocker its score) and the plain-reopen entry
// (before CheckCrossCompCourtBusy, so an unreopenable target reports that rather
// than a transient court_busy). The preconditions live in
// reopenResultPreconditionTx, which reopenKachinukiUnderCourtLock also runs, so
// this pre-check cannot pass a target the reopen will then reject.
func (e *Engine) checkTargetReopenable(compID string, comp *state.Competition, matchID string) error {
	var checkErr error
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			checkErr = e.reopenResultPreconditionTx(tx, compID, comp, matchID, h)
			return nil
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			checkErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return txErr
	}
	return checkErr
}

// requireBlockerHoldsCourt verifies the client-named blocker is a RUNNING match
// on targetMatch's court, the precondition RequeueBlockerAndReopenKachinuki's
// destructive requeue depends on (mp-gmcg review R1). It rejects — WITHOUT
// touching the blocker — when the target has no court, when the blocker sits on
// a different court (the bystander-wipe case), or when the blocker is not
// running (a completed/scheduled match is not what is wedging the court; retry
// the plain reopen). Each check returns its OWN operator-facing message, which
// is why this validates the client's claim directly rather than folding into a
// single store.RunningMatchOnCourt occupant lookup (that would name the true
// occupant but collapse the three messages into one).
//
// The two lookupMatchCourt reads go through the COPYING LoadPoolMatches/
// LoadBracket; MatchStatusByID is the no-copy one. The path is operator-
// initiated and rare, so the extra copies don't matter, but the doc should not
// claim they aren't made. The caller holds only the court lock, not a per-comp
// write lock, so these reads match CheckCrossCompCourtBusy's pre-tx discipline.
func (e *Engine) requireBlockerHoldsCourt(targetComp, targetMatch, blockerComp, blockerMatch string) error {
	targetCourt, err := e.lookupMatchCourt(targetComp, targetMatch)
	if err != nil {
		return err
	}
	if targetCourt == "" {
		return validationErrorf("match %s has no court assigned, so no match can be blocking its court", targetMatch)
	}
	blockerCourt, err := e.lookupMatchCourt(blockerComp, blockerMatch)
	if err != nil {
		return err
	}
	if blockerCourt != targetCourt {
		return validationErrorf("match %s is on court %q, not %s's court %q; requeuing it would not free the court", blockerMatch, blockerCourt, targetMatch, targetCourt)
	}
	status, found, err := e.store.MatchStatusByID(blockerComp, blockerMatch)
	if err != nil {
		return err
	}
	if !found {
		// lookupMatchCourt above already 404s an unknown id, so on the normal
		// path found is true here. This still fires if blockerComp is DELETED
		// between the two cached reads (DeleteCompetition takes the per-comp
		// lock, not the court lock we hold) — a clearer message than the
		// "not running (status \"\")" the check below would otherwise give.
		return notFoundErrorf("blocker match %s not found in competition %s", blockerMatch, blockerComp)
	}
	if status != state.MatchStatusRunning {
		return validationErrorf("match %s is not running (status %q); it is not blocking the court, retry the reopen", blockerMatch, status)
	}
	return nil
}

// RemoveTrailingKachinukiBout drops the SINGLE trailing UNSCORED bout from a
// RUNNING kachinuki encounter — the explicit operator undo for a pairing
// appended by mistake ([Record bout] / [Add next bout]). Its own `strip`
// closure (below), not stripTrailingUnscoredKachinukiBouts, defines
// "removable" here: never the last remaining bout, and never more than one
// per call (review F3) — a DIFFERENT, narrower rule than the completed-write
// strip's "drop the whole trailing run", which is safe there only because a
// completed match always has a scored bout to stop on. Position <= 0 is
// still refused, so a non-numbered sub is never touched — daihyosen does not
// exist in kachinuki and is not involved.
//
// Walks the same three match homes as ReopenKachinukiMatch (pool → bracket
// rounds → bronze) via the shared findMatchHome visitor (review F6), so a
// fourth match home or a lookup-order change can't be forgotten in just one
// of the two. Returns the updated match for the caller to broadcast. No
// court gate: the match is (and stays) running, so removing an empty bout
// changes no court occupancy.
func (e *Engine) RemoveTrailingKachinukiBout(compID, matchID string) (*state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if !comp.IsKachinuki() {
		return nil, validationErrorf("removing a bout is only supported for kachinuki team matches")
	}

	var (
		updated *state.MatchResult
		opErr   error
	)
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		// strip is the SINGLE precondition+mutation every match home applies:
		// the encounter must be RUNNING and must carry a trailing unscored bout.
		// One closure so pool/round/bronze cannot drift (same reasoning as
		// reopenResultPreconditionTx, the reopen's single result-precondition helper).
		strip := func(status state.MatchStatus, subs []state.SubMatchResult) ([]state.SubMatchResult, error) {
			if status != state.MatchStatusRunning {
				return nil, ErrRemoveBoutNotRunning
			}
			// Remove ONLY the single trailing bout, and never the last remaining
			// one: the operator's "× Remove this bout" is singular, and a RUNNING
			// kachinuki encounter must always keep at least one bout (mp-gmcg
			// review F3). stripTrailingUnscoredKachinukiBouts drops the whole
			// trailing run with no floor — correct for the completed-write caller
			// (a completed match always carries a scored bout, so it never
			// empties) but not here, where bout 1 could be unscored, or two
			// "Add next bout manually" pairings could be appended and one tap
			// must not strip both.
			n := len(subs)
			if n <= 1 {
				return nil, ErrNoRemovableBout
			}
			last := subs[n-1]
			if last.Position <= 0 || !isUnscoredKachinukiBout(last) {
				return nil, ErrNoRemovableBout
			}
			return subs[:n-1], nil
		}

		found, ferr := findMatchHome(tx, compID, matchID, func(h matchHome) error {
			if h.Pool != nil {
				stripped, serr := strip(h.Pool.Status, h.Pool.SubResults)
				if serr != nil {
					opErr = serr
					return nil
				}
				h.Pool.SubResults = stripped
				if werr := h.Save(); werr != nil {
					return werr
				}
				u := *h.Pool
				updated = &u
				return nil
			}
			stripped, serr := strip(h.Bracket.Status, h.Bracket.SubResults)
			if serr != nil {
				opErr = serr
				return nil
			}
			h.Bracket.SubResults = stripped
			if werr := h.Save(); werr != nil {
				return werr
			}
			updated = bracketMatchToTeamResult(*h.Bracket)
			return nil
		})
		if ferr != nil {
			return ferr
		}
		if !found {
			opErr = notFoundErrorf("match %s not found", matchID)
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	if opErr != nil {
		return nil, opErr
	}
	return updated, nil
}

// matchHome is the store location that owns a match ID, handed to a
// findMatchHome visitor. Exactly one of Pool / Bracket is non-nil. For a
// bracket ROUND match, Bracket points into BracketRoot.Rounds[RIdx][MIdx] and
// Bronze is false; for the 3rd-place match, Bracket is BracketRoot.ThirdPlaceMatch,
// Bronze is true, and RIdx/MIdx are meaningless (the bronze is a SIBLING of
// Rounds, not an element — forgetting it is the class of bug this shared walk
// exists to make impossible). Save persists the store the match lives in.
type matchHome struct {
	Pool        *state.MatchResult
	Bracket     *state.BracketMatch
	BracketRoot *state.Bracket
	RIdx, MIdx  int
	Bronze      bool
	Save        func() error
	// PoolMatches / BracketRoot are the FULL slices findMatchHome already
	// loaded under this tx, exposed so a visitor's court check can reuse them
	// instead of re-loading (mp-gmcg review E4). PoolMatches is nil when the
	// pool load errored (findMatchHome swallows that error and tries the
	// bracket), so a court check treating nil as "reload" surfaces the failure
	// rather than skipping pool matches. BracketRoot is nil for a pool home
	// (findMatchHome returns before loading the bracket).
	PoolMatches []state.MatchResult
}

// findMatchHome walks the three homes a match ID can have — pool matches, then
// bracket rounds, then the bronze (3rd-place) match — in that FIXED order, and
// invokes visit for the owning home. It is the MUTATING, in-transaction walk,
// and the engine's only copy of it, so a new caller cannot drop the bronze
// branch or the pool-load-error swallow by hand-copying the ~60-line skeleton
// (mp-gmcg review F6). found=false with a nil error means the ID is in neither
// store. A pool LOAD error is swallowed and the walk still tries the bracket
// (matching the open-coded copies this replaced); a bracket load error is
// returned. visit's own error propagates.
func findMatchHome(tx state.StoreTx, compID, matchID string, visit func(matchHome) error) (bool, error) {
	poolMatches, lerr := tx.LoadPoolMatches(compID)
	if lerr == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				return true, visit(matchHome{
					Pool:        &poolMatches[i],
					PoolMatches: poolMatches,
					Save:        func() error { return tx.SavePoolMatches(compID, poolMatches) },
				})
			}
		}
	}

	bracket, berr := tx.LoadBracket(compID)
	if berr != nil {
		return false, berr
	}
	if bracket != nil {
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				if bracket.Rounds[rIdx][mIdx].ID == matchID {
					return true, visit(matchHome{
						Bracket:     &bracket.Rounds[rIdx][mIdx],
						BracketRoot: bracket,
						PoolMatches: poolMatches,
						RIdx:        rIdx,
						MIdx:        mIdx,
						Save:        func() error { return tx.SaveBracket(compID, bracket) },
					})
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
			return true, visit(matchHome{
				Bracket:     bm,
				BracketRoot: bracket,
				PoolMatches: poolMatches,
				Bronze:      true,
				Save:        func() error { return tx.SaveBracket(compID, bracket) },
			})
		}
	}

	return false, nil
}

// checkPoolReopenDownstreamTx is the pool-match twin of the bracket branch's
// retractPropagatedWinner guard (mp-gmcg). Reopening a pool match flips it back
// to running, which drops it from the standings the knockout bracket was seeded
// from. If this pool's CURRENT qualifying finishers already sit in a started
// (running/completed) knockout match, re-ending the reopened match with a
// different result would strand a displaced finisher there. It mirrors the
// existing mp-e2k1 score-path guard exactly (top-N pool finishers vs
// hasStartedKnockoutMatchTx), and only fires for mixed competitions — the sole
// format that feeds pool finishers into a bracket; league/swiss have no bracket
// to desync. Standings are read BEFORE reopenPoolMatch mutates, so they reflect
// the finishers actually committed to the bracket.
func (e *Engine) checkPoolReopenDownstreamTx(tx state.StoreTx, compID string, comp *state.Competition, matchID string) error {
	if comp == nil || comp.Format != state.CompFormatMixed {
		return nil
	}
	pn, ok := poolNameFromMatchID(matchID)
	if !ok || !IsPoolMatchID(matchID) {
		return nil
	}
	standings, err := e.computeStandingsFrom(tx, compID)
	if err != nil {
		return fmt.Errorf("reopen: pre-reopen standings for pool %q: %w", pn, err)
	}
	ps := standings[pn]
	winners := comp.EffectivePoolWinners()
	topN := make([]string, 0, winners)
	for i := 0; i < winners && i < len(ps); i++ {
		topN = append(topN, ps[i].Player.Name)
	}
	if len(topN) == 0 {
		return nil
	}
	blockingFinisher, _, herr := e.hasStartedKnockoutMatchTx(tx, compID, topN)
	if herr != nil {
		return fmt.Errorf("reopen: checking started knockout matches: %w", herr)
	}
	if blockingFinisher != "" {
		return ErrReopenDownstreamFought
	}
	return nil
}

// reopenPoolMatch is reopenBracketMatch's twin for a pool/league match. Same
// rule, same field set, different struct: MatchResult carries the scoreline as
// IpponsA/IpponsB (BracketMatch renders it into ScoreA/ScoreB strings), adds
// WinnerID and the rep-bout nominations, and holds DecidedByHantei as a
// *bool where the bracket has a plain bool. See reopenBracketMatch for why
// each of these is verdict rather than bout record.
//
// RepPlayerA/B name who fought a pool daihyosen. That bout's own record lives
// in SubResults like any other; these two fields are the discarded verdict's
// nomination for it, so they go with the rest of the verdict.
func reopenPoolMatch(m *state.MatchResult, reason string) {
	m.Status = state.MatchStatusRunning
	m.Winner = ""
	m.WinnerID = ""
	m.IpponsA = nil
	m.IpponsB = nil
	m.Decision = ""
	m.DecisionBy = ""
	m.DecisionReason = ""
	m.Encho = nil
	m.DecidedByHantei = nil
	m.ResultSource = ""
	m.RepPlayerA = ""
	m.RepPlayerB = ""
	m.CorrectionReason = reason
	m.ReopenPending = reopenPending(reason)
}

// reopenBracketMatch discards the ENCOUNTER-LEVEL VERDICT and keeps the BOUT
// LOG. Those are separate things in the data model, which is what makes this
// safe: every fact about a bout that was actually fought — who fought it
// (SideA/SideB), what they struck, hansoku, hantei, whether it went to encho —
// lives on the SubMatchResult for that bout (state/models.go). SubResults is
// therefore untouched here, and all of it survives a reopen.
//
// Everything cleared below is the discarded verdict ABOUT those bouts, not a
// record OF them: the encounter winner, the match-level default-win scoreline
// (the FIK Art. 32 maru fill a kiken/fusenpai writes at match level), the
// decision, the match-level encho/hantei state describing the bout in progress
// when the operator ended it, and the provenance stamps for a result that no
// longer exists (IsOverridden, ResultSource). Leaving those behind produced a
// match that was running yet still advertised a winner, a scoreline, and a
// manual-override badge inherited from the result the reopen threw away.
//
// This clears the verdict fields RevertMatchToQueue clears (engine/scoring.go)
// that a kachinuki result can actually carry, MINUS SubResults (which requeue
// drops and reopen exists to preserve) and CorrectionReason/ReopenPending
// (which the reopen is itself setting). Two RevertMatchToQueue fields are
// deliberately NOT mirrored: match-level HansokuA/B and FlagsA/B are never
// populated on a kachinuki team match (hansoku rides on the sub-bouts, flags
// are engi-only, and reopen*Match run for kachinuki matches only), and
// ModifiedAt is left at the completion stamp on purpose — it still fences any
// stale pre-completion offline write via ApplyByTimestamp and never blocks the
// re-End. If you add a match-level verdict field a kachinuki result CAN carry,
// clear it here too.
func reopenBracketMatch(bm *state.BracketMatch, reason string) {
	bm.Status = state.MatchStatusRunning
	bm.Winner = ""
	bm.ScoreA = ""
	bm.ScoreB = ""
	bm.Decision = ""
	bm.DecisionBy = ""
	bm.DecisionReason = ""
	bm.Encho = nil
	bm.DecidedByHantei = false
	bm.IsOverridden = false
	bm.ResultSource = ""
	bm.CorrectionReason = reason
	bm.ReopenPending = reopenPending(reason)
}

// reopenPending reports whether a reopen still OWES an audit justification:
// true when no reason was supplied (the score path collects it on the next
// completion), false when the operator already gave one so this reopen is
// justified as it happens. One helper rather than a `reason == ""` test
// inlined at each of its two call sites (reopenPoolMatch, reopenBracketMatch
// — the latter already covers both the bracket-round and bronze homes), so a
// third caller can't drift from the rule — the same reason the reopen keeps its
// result preconditions in a single reopenResultPreconditionTx.
func reopenPending(reason string) bool {
	return reason == ""
}

// downstreamTargets returns the bracket matches this result was propagated into:
// next (the next-round slot, which received the WINNER) and bronze (the 3rd-place
// match, which received the semifinal LOSER, and only when this is a semifinal
// that feeds it). Either may be nil. This is the SINGLE derivation of downstream
// LOCATION on the REOPEN side, so the read-only downstreamFoughtForRound predicate
// and the destructive retractPropagatedWinner mutation cannot drift on WHERE a
// result went — the drift that would split "check passes" from "mutation misses"
// and wipe a blocker's live score for a reopen that then fails (mp-gmcg review).
// propagateBracketWinner (scoring.go) independently encodes the same slot rule
// (the mIdx/2 next-round index and the bronze-feeding round index), so a
// location-rule change must still land there too — as retractPropagatedWinner's
// body comment already flags ("Mirror propagateBracketWinner's positional
// assignment").
func downstreamTargets(bracket *state.Bracket, rIdx, mIdx int) (bronze, next *state.BracketMatch) {
	if bracket.ThirdPlaceMatch != nil && rIdx == len(bracket.Rounds)-2 {
		bronze = bracket.ThirdPlaceMatch
	}
	if rIdx+1 < len(bracket.Rounds) {
		next = &bracket.Rounds[rIdx+1][mIdx/2]
	}
	return bronze, next
}

// downstreamFoughtForRound reports whether the match at bracket round rIdx / slot
// mIdx has a downstream (next-round match, or the bronze it feeds) that is
// already started or scored — the read-only predicate that makes reopening it
// unsafe. It and the destructive retractPropagatedWinner both consume
// downstreamTargets, so they key on ONE definition of both WHERE the winner went
// and WHAT "downstream fought" means (mp-gmcg review).
func downstreamFoughtForRound(bracket *state.Bracket, rIdx, mIdx int) bool {
	bronze, next := downstreamTargets(bracket, rIdx, mIdx)
	return (bronze != nil && bracketMatchStartedOrScored(bronze)) ||
		(next != nil && bracketMatchStartedOrScored(next))
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
	if downstreamFoughtForRound(bracket, rIdx, mIdx) {
		return ErrReopenDownstreamFought
	}
	bronze, next := downstreamTargets(bracket, rIdx, mIdx)
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
		// winnerOfPlaceholder (bracket.go) is the ONE producer this now shares
		// with generation and parseWinnerOf: depth is 1-based from the final,
		// so the source match at round rIdx is depth len(Rounds)-rIdx.
		placeholder := winnerOfPlaceholder(len(bracket.Rounds)-rIdx, mIdx)
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
// paths (RecordMatchResultTx/RecordMatchResult AND, via
// RecordMatchResultWithIneligibilityTx, RecordDecisionTx) so the merge guard
// cannot drift between them.
//
// On a COMPLETED write (the operator's explicit "End match", mp-gmcg) it
// additionally strips trailing UNSCORED bouts after the merge:
// MaybeAdvanceKachinuki auto-appends the next pairing after every scored
// bout, so ending the match leaves an abandoned empty pairing at the tail.
// Because the merge preserves stored entries by position, a client simply
// omitting that row cannot remove it, the server must strip it here so it
// never reaches standings/exports. It then derives the winner from the
// cleaned bout log (deriveKachinukiWinner) so a plain bout-driven End write
// cannot carry an outcome the log itself does not support.
func applyKachinukiMerge(comp *state.Competition, prior, result *state.MatchResult) error {
	if !comp.IsKachinuki() {
		return nil
	}
	var stored []state.SubMatchResult
	if prior != nil {
		stored = prior.SubResults
	}
	result.SubResults = mergeKachinukiSubResults(stored, result.SubResults)
	if result.Status == state.MatchStatusCompleted {
		result.SubResults = stripTrailingUnscoredKachinukiBouts(result.SubResults)
		return deriveKachinukiWinner(result)
	}
	return nil
}

// deriveKachinukiWinner authoritatively derives result.Winner from the
// merged bout log's LAST bout carrying a recorded outcome, mirroring
// deriveKachinukiEndOutcome (admin_scoring_team.jsx): OPERATOR INPUT
// DETERMINES THE BOUT OUTCOME. This is the server-side twin of that client
// rule (mp-gmcg review C3): before this, the client simply HID the generic
// "Save correction" button that would have let an operator pick an
// unsupported winner, but nothing stopped a bulk /scores write, an offline
// flush, or any future caller from completing a kachinuki match with a
// winner the bout log does not support — the invariant was enforced by not
// rendering a button, which is not enforcement.
//
// Scoped STRICTLY to decision == kachinuki-exhaustion: that value is UNIQUE
// to the plain bout-driven End-match write (buildPatch("completed",
// {endOutcome}) in admin_scoring_team.jsx always sends it for a decisive
// end, "hikiwake" for a drawn one). kiken/fusenpai/fusensho/daihyosen
// completions reach this SAME merge point (RecordDecisionTx ->
// RecordMatchResultWithIneligibilityTx -> applyKachinukiMerge) with THEIR
// OWN decision values and their own winner rule — the non-withdrawing side,
// already set on result.Winner before this runs (see RecordDecisionTx) —
// and re-deriving from the bout log there would silently overturn a
// legitimate walkover, which is exactly the "loser kept its struck ippons"
// case FIK Art. 32 protects. "hikiwake" and "" carry no winner to derive.
//
// It also REJECTS a kachinuki-exhaustion write whose last scored bout is a DRAW
// (mp-gmcg review R2): exhaustion means the final pairing was decisive and the
// loser had no replacement, so a tied last bout contradicts the decision. C3
// closed "wrong winner when the log names one"; this closes the residue where
// the log names NO winner yet the client still sends one (for a bracket match
// validateBracketCompletion only checks the winner is non-empty, so it would
// otherwise pass). A genuine tie is "hikiwake" in pools, or goes to encho in a
// knockout — which gives the bout a winner. A completed write with NO scored
// bout at all leaves the client's winner untouched (a separate degenerate case,
// out of scope here).
func deriveKachinukiWinner(result *state.MatchResult) error {
	if result == nil || result.Decision != string(domain.DecisionKachinukiExhaustion) {
		return nil
	}
	var last *state.SubMatchResult
	for i := range result.SubResults {
		s := &result.SubResults[i]
		if s.Position <= 0 || isUnscoredKachinukiBout(*s) {
			continue
		}
		if last == nil || s.Position > last.Position {
			last = s
		}
	}
	if last == nil {
		return nil
	}
	if last.Winner == "" {
		return validationErrorf("a kachinuki match ending on a tied bout is not a decisive win: record the last bout's winner, take it to overtime, or end the encounter as a draw (hikiwake)")
	}
	switch {
	case isWinForSide(last.Winner, result.SideA, last.SideA):
		result.Winner = result.SideA
	case isWinForSide(last.Winner, result.SideB, last.SideB):
		result.Winner = result.SideB
	default:
		// The deciding bout names a winner that matches NEITHER side (mp-gmcg
		// review, R2 residue). A kachinuki bout persists the PLAYER name as its
		// winner (resolveKachinukiBoutSides never writes the team name), so the
		// only thing this can match is last.SideA/last.SideB. A payload carrying
		// a sub winner + ippons but omitting the sub's sideA/sideB — a bulk
		// PUT /scores, an offline flush, any non-editor caller — reaches here,
		// and without this guard result.Winner would keep whatever the client
		// sent (for a bracket match validateBracketCompletion only checks it is
		// non-empty). Reject the unattributable winner rather than accept it; the
		// score editor always sends the sub sides.
		return validationErrorf("a kachinuki match's deciding bout names a winner (%q) that is neither competitor: send the bout's sideA/sideB so the encounter winner can be derived", last.Winner)
	}
	return nil
}

// isUnscoredKachinukiBout reports whether a bout carries no recorded
// outcome or score at all: no winner, no decision, no real ippons on
// either side, no hansoku, no hantei, no encho marker. Such a row is the
// placeholder pairing MaybeAdvanceKachinuki appends for a bout that was
// never fought. The encho test uses the canonical EnchoMetadata.On()
// predicate (the same one validation and the (E) label share), so a
// degenerate zero-period block is NOT mistaken for a real marker that would
// wedge an unscored placeholder into a completed record.
func isUnscoredKachinukiBout(s state.SubMatchResult) bool {
	return s.Winner == "" &&
		s.Decision == "" &&
		countScoringIppons(s.IpponsA) == 0 &&
		countScoringIppons(s.IpponsB) == 0 &&
		s.HansokuA == 0 &&
		s.HansokuB == 0 &&
		!s.DecidedByHantei &&
		!s.Encho.On()
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

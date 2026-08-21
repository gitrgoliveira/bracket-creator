package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// errMatchNotFound is returned by withPoolMatch / withBracketMatch when no
// match with the given ID exists in the respective data store.
var errMatchNotFound = errors.New("match not found")

// ErrMatchSideMismatch is returned when a score payload names competitors
// (sideA/sideB) that differ from the match's stored pairing. Match identity
// is fixed at generation; a score only carries the *result*, never a new
// pairing. Rejecting prevents a malformed or buggy client from silently
// rewriting who is in a match, the live path that produced cross-pool pool
// rows (a stored Pool E bout overwritten with competitors from other pools).
// Handlers map this to HTTP 409.
var ErrMatchSideMismatch = errors.New("match side mismatch: score payload competitors differ from the stored pairing")

// ErrMatchAlreadyCompleted is returned by RevertMatchToQueue when the target
// match has already been completed. A completed bout has a recorded result
// that the operator must correct via the score editor, not discarded by a
// revert. Handlers map this to HTTP 409.
var ErrMatchAlreadyCompleted = errors.New("match already completed: use the score editor to correct a completed bout")

// errLWWDropped is a package-internal sentinel returned by the mutate callback
// inside OverrideBracketWinner when a timestamp last-write-wins check drops a
// stale incoming write. Returning this error (rather than nil) causes
// UpdateBracket to skip the disk save, avoiding a spurious write of an
// unchanged bracket (finding 8). The caller converts it to (false, nil) so
// the handler can respond 200 with applied=false without broadcasting (finding 7).
var errLWWDropped = errors.New("lww_dropped")

// reconcileSides folds the stored pairing into a score payload's result.
// An empty payload side is backfilled from the stored side (e.g. a payload
// that omits sides, or a not-yet-resolved bracket slot). A non-empty payload
// side that disagrees with a non-empty stored side is a mismatch, the caller
// must reject rather than overwrite the stored competitor. Returns true on the
// first such disagreement; result is left partially filled but is discarded by
// the caller on mismatch.
func reconcileSides(result *state.MatchResult, storedA, storedB string) (mismatch bool) {
	if result.SideA == "" {
		result.SideA = storedA
	} else if storedA != "" && result.SideA != storedA {
		mismatch = true
	}
	if result.SideB == "" {
		result.SideB = storedB
	} else if storedB != "" && result.SideB != storedB {
		mismatch = true
	}
	return mismatch
}

// withPoolMatch atomically loads pool matches, calls mutate on the one
// matching matchId, and saves the updated slice. Returns errMatchNotFound
// (unwrapped) when the ID is not present so callers can fall through to
// the bracket store.
//
// Delegates to state.Store.UpdatePoolMatchByID so the entire
// load + find + mutate + save sequence runs under the per-competition
// lock. Pre-atomic-primitive, two operators scoring different matches
// on different courts could each LoadPoolMatches into separate copies,
// mutate their target match, and SavePoolMatches in sequence, the
// later save would overwrite the earlier save's mutation with stale
// data for the OTHER match. One operator's score would be silently
// lost during a live tournament.
func (e *Engine) withPoolMatch(compId, matchId string, mutate func(*state.MatchResult)) error {
	found, err := e.store.UpdatePoolMatchByID(compId, matchId, mutate)
	if err != nil {
		return err
	}
	if !found {
		return errMatchNotFound
	}
	return nil
}

// withBracketMatch atomically loads the bracket, calls mutate on the
// match matching matchId, and saves. Returns errMatchNotFound when not
// present (so RecordMatchResult callers fall through cleanly when neither
// pool-match nor bracket-match has that ID).
//
// Delegates to state.Store.UpdateBracketMatchByID (mp-gmcg review R5), which
// holds the per-competition lock across load → walk-rounds → bronze-sibling →
// mutate → save and writes ONLY when the match is found — the exact walk this
// used to hand-roll, and the sibling of UpdatePoolMatchByID. findBracketMatchByID
// searches Rounds FIRST then the ThirdPlaceMatch sibling, so "m-bronze" resolves
// here too.
//
// NOTE: no playability gate here. withBracketMatch backs the SCHEDULING mutators
// (UpdateMatchCourt / UpdateMatchTime) and RevertMatchToQueue, which must work
// on not-yet-resolved (placeholder) knockout matches so operators can pre-arrange
// courts/times. The per-match playability gate lives only in the SCORING paths
// (recordBracketMatchResult / recordBracketMatchResultTx / OverrideBracketWinner),
// which mutate via UpdateBracket directly.
func (e *Engine) withBracketMatch(compId, matchId string, mutate func(*state.BracketMatch)) error {
	found, err := e.store.UpdateBracketMatchByID(compId, matchId, mutate)
	if err != nil {
		return err
	}
	if !found {
		return errMatchNotFound
	}
	return nil
}

// preserveSubHantei enforces the operator ruling "all results must be
// recorded into storage" at the store boundary: a writer that says NOTHING
// about the daihyosen verdict (DecidedByHantei nil - a stale editor snapshot
// opened before the verdict existed, a quick-score write, any client
// predating the field) must not erase a recorded one. The verdict travels
// (flag + winner) onto the incoming daihyosen row only when that row is
// verdict-silent, names no winner of its own, carries a decision hantei can
// coexist with, and is still tied (an untied row cannot carry a hantei).
// An EXPLICIT false (the editors' withdrawal) and a named winner both pass
// through untouched. The preserveLoserScore precedent, one bout deeper.
//
// SCOPE, deliberately narrow in two directions:
//
//   - It PATCHES an existing position -1 row; it never RE-APPENDS one the
//     incoming payload dropped. DELETE /daihyosen removes that row on purpose
//     (handlers_daihyosen.go) and writes through this same path, so
//     resurrecting a missing row would make an unscored rep bout undeletable.
//     A writer that drops the row entirely (quick-score synthesises positions
//     1..N only) therefore still discards it - that is the delete contract, not
//     an oversight.
//   - It runs in the engine, AFTER the HTTP validator, so a row it mutates is
//     never re-validated. Every field it writes must land on a row that would
//     have passed validateSubBout, which is why the decision allow-list below
//     mirrors that validator exactly: without it a verdict-silent
//     `decision:"kiken-voluntary"` row would be stamped into a bout that is
//     simultaneously a withdrawal and a judges' decision, and SideMarksLR
//     would then emit both `Kiken` and `Ht`.
//
// hanteiStillHolds reports whether a MATCH-level hantei verdict is still valid
// for the incoming result. It is the engine-side re-application of the
// DecidedByHantei block in ScoreRequest.Validate, and it must enforce that
// block's conditions IN FULL, because this runs after validation and its output
// is never re-checked: whatever it stamps is persisted unexamined.
//
// All four conditions, and what each one closed:
//
//   - A NAMED WINNER. A hantei declares one; the validator refuses the pairing
//     without it. Omitting it here let a `{status:"running", winner:null}`
//     reopen inherit the verdict, and the result was worse than losing it: the
//     pool encoding (encodeHanteiIntoIppons) cannot attribute a winner-less
//     verdict to a side, so it silently dropped the mark while the in-memory
//     cache kept the flag — every read said "hantei" until the process
//     restarted, and none did afterwards.
//   - COMPLETED (or unstated, which applyBracketMatchResult defaults to
//     completed). A match still running has not been decided by anyone.
//   - A COMPATIBLE DECISION, through the MATCH-level predicate. Using the
//     sub-bout one let a verdict-silent `decision:"daihyosen"` write inherit a
//     match-level verdict, claiming the encounter itself was judged.
//   - A TIED SCORELINE (FIK 7-5 / 29-6), the rule the verdict rests on.
//   - NO DecisionBy / DecisionReason. Those two are the withdrawal audit trail
//     (who called it, and why), so a result carrying either is recording a
//     decision someone MADE, not a tied bout the referees judged. The validator
//     refuses the pairing; this twin was missing both, so "IN FULL" above was an
//     overclaim until they were added. Reachable only through the paths that do
//     not run ScoreRequest.Validate, which is precisely why this function exists.
func hanteiStillHolds(r *state.MatchResult) bool {
	return r.Winner != "" &&
		(r.Status == "" || r.Status == state.MatchStatusCompleted) &&
		domain.IsMatchHanteiCompatibleDecisionStr(r.Decision) &&
		domain.HanteiTiedScoreline(r.IpponsA, r.IpponsB) &&
		r.DecisionBy == "" && r.DecisionReason == ""
}

// preserveMatchHantei resolves the MATCH-level verdict for a FORWARD write,
// carrying a stored one onto a verdict-silent payload — under the SAME guards
// preserveSubHantei applies one level down, which is the whole point.
//
// An unguarded inherit is worse than none. RecordDecision/RecordDecisionTx build
// their MatchResult from scratch and never set DecidedByHantei, and the decision
// handler does not run ScoreRequest.Validate, so a kiken recorded over a stored
// hantei arrives silent: a bare carry stamps the withdrawal as a judges'
// decision, and export.SideMarks (which marks Ht unconditionally, by design)
// then prints "Ht" and "Kiken" on the same encounter — the contradiction
// validation.go refuses to persist. The same applies to a silent re-score that
// turns a 1-1 hantei into a 2-1 ippon win: an untied scoreline cannot carry a
// verdict, and the wire validator would 400 the state it would leave behind.
//
// So a stored verdict that NO LONGER HOLDS is cleared explicitly rather than
// left nil: nil means "inherit" on the bracket branch, which would keep it.
// Nothing is written when there is no stored verdict and the writer said
// nothing, so an ordinary match keeps emitting an absent field.
//
// Forward only. matchWriteRestore replays a trusted snapshot verbatim;
// re-testing it there would let this rewrite the state being restored.
func preserveMatchHantei(storedHantei bool, result *state.MatchResult) {
	if result.DecidedByHantei == nil && !storedHantei {
		return
	}
	held := hanteiStillHolds(result)
	if result.DecidedByHantei == nil {
		result.DecidedByHantei = state.HanteiExplicit(held)
		return
	}
	if *result.DecidedByHantei && !held {
		result.DecidedByHantei = state.HanteiExplicit(false)
	}
}

func preserveSubHantei(stored, incoming []state.SubMatchResult) {
	var prior *state.SubMatchResult
	for i := range stored {
		if stored[i].Position == state.DaihyosenSubPosition {
			prior = &stored[i]
			break
		}
	}
	if prior == nil || !prior.HanteiDecided() || prior.Winner == "" {
		return
	}
	for i := range incoming {
		in := &incoming[i]
		if in.Position != state.DaihyosenSubPosition {
			continue
		}
		if in.DecidedByHantei != nil || in.Winner != "" {
			return // the writer addressed the verdict: its word stands
		}
		// The SAME predicate validateSubBout enforces, shared via domain so the
		// two cannot drift (this runs after validation and is never re-checked).
		if !domain.IsSubBoutHanteiCompatibleDecisionStr(in.Decision) {
			return
		}
		// Side-guarded, like the preserveLoserScore precedent: IpponsA/IpponsB
		// are POSITIONAL, so copying them onto a row whose sides are named in
		// the opposite order mirrors the letters and credits each side with the
		// other's points. reconcileSides normalises at MATCH level only, never
		// per sub-bout, so a drifted or hand-built payload can reach here
		// swapped. An unnamed incoming row (the common stale-snapshot shape)
		// inherits the names along with the scoreline.
		//
		// ABANDON on a mismatch rather than skipping only the copy. Winner is a
		// NAME, so it needs no positional guard of its own - but it names one of
		// the STORED pair, and stamping it onto a row naming a different pair
		// attributes the verdict to neither competitor present. The tie check
		// below cannot catch that: with the copy skipped the incoming row is
		// still empty, so it compares 0 against 0 and passes vacuously.
		// deriveDaihyosenWinner then matches no side and leaves the encounter
		// with a hantei-decided rep bout and no winner at all, which a bracket
		// completion rejects and pool standings score as a draw for both teams.
		if (in.SideA != "" || in.SideB != "") &&
			(in.SideA != prior.SideA || in.SideB != prior.SideB) {
			return
		}
		// A row that records no ippons of its own said nothing about the
		// SCORELINE either, so the stored one travels with the verdict it
		// rests on. Without this the verdict lands on an all-empty row and the
		// struck ippons, the outstanding fouls, the overtime marker and the
		// sub-decision are all lost: a 1-1 hantei would persist as 0-0, which
		// moves the `Ht` to the other slot (resultSlot fills outside-to-inside)
		// and drops the `(E)`.
		// Note this is also what makes the tie check below meaningful - on an
		// empty incoming row it would otherwise compare 0 against 0 and pass
		// vacuously, never once consulting what was actually stored.
		if countScoringIppons(in.IpponsA) == 0 && countScoringIppons(in.IpponsB) == 0 {
			if in.SideA == "" && in.SideB == "" {
				in.SideA, in.SideB = prior.SideA, prior.SideB
			}
			in.IpponsA = append([]string(nil), prior.IpponsA...)
			in.IpponsB = append([]string(nil), prior.IpponsB...)
			// Hansoku travels WITH the ippons, not separately: an outstanding
			// foul is part of the same scoreline, and the two are coupled
			// (every second one discharges into an "H" ippon for the opponent,
			// applyHansokuIppons). Restoring the letters but not the counts
			// left a coherent stored pair as an incoherent restored one -
			// prior's discharged H's beside the incoming zero - so the
			// referee's outstanding ▲ vanished from every scoreboard and the
			// next foul on that side no longer discharged.
			in.HansokuA, in.HansokuB = prior.HansokuA, prior.HansokuB
			in.Encho = prior.Encho.Clone()
			if in.Decision == "" {
				in.Decision = prior.Decision
			}
		}
		if !domain.HanteiTiedScoreline(in.IpponsA, in.IpponsB) {
			return // untied now: the verdict cannot stand on this scoreline
		}
		in.DecidedByHantei = state.HanteiPtr(true)
		in.Winner = prior.Winner
		return
	}
}

// preserveDaihyosenOutcome is the call every forward SubResults replacement
// makes: preserveSubHantei restores the bout-level verdict, then
// deriveDaihyosenWinner re-reads it into the ENCOUNTER's winner.
//
// Both halves are required, and the order is why they are bundled here rather
// than left as two calls. deriveDaihyosenWinner already runs earlier in each
// writer, but at that point the incoming daihyosen row is still verdict-silent
// and winner-less, so it finds nothing and leaves Winner empty; the preserve
// then stamps the row back. Without this second pass the stored state
// contradicts itself - the rep bout says "Kyoto won by hantei" while the match
// records no winner - and computeStandingsFrom, which keys W/L/T off
// MatchResult.Winner, credits BOTH teams a draw. It is idempotent: an explicit
// winner short-circuits deriveDaihyosenWinner, so a writer that genuinely
// names one is never overridden.
func preserveDaihyosenOutcome(stored []state.SubMatchResult, result *state.MatchResult) {
	if result == nil {
		return
	}
	preserveSubHantei(stored, result.SubResults)
	deriveDaihyosenWinner(result)
}

// matchWritePolicy selects how stored state is folded into an incoming result.
// It governs BOTH branches of a match write - applyPoolWrite for a pool/league
// match, applyBracketMatchResult for a knockout one - because a match id
// resolves to one or the other at run time and the caller does not know which.
// A policy that reached only the pool branch would silently revert to forward
// semantics for exactly the matches that fell through.
type matchWritePolicy int

const (
	// matchWriteForward is a client-supplied score: the payload carries only what
	// the operator entered, so stored context it omitted is inherited.
	matchWriteForward matchWritePolicy = iota
	// matchWriteRestore replays a trusted prior snapshot (the K3 rollback after a
	// concurrent-ineligibility rejection). It must land as captured, so it
	// inherits NOTHING from the rejected partial write it is undoing: an empty
	// or nil field in the snapshot means "this was empty", not "the writer said
	// nothing".
	//
	// That distinction is what makes the policy necessary rather than cosmetic.
	// lookupExistingResult collapses a stored false/empty to nil on the way out,
	// and every one of those nils is a PRESERVE trigger on the forward path, so
	// replaying a snapshot forward re-applies the very write being rolled back.
	// This used to be handled by a caller-side normalizePriorForRollback that
	// pre-mangled the snapshot into "explicit clear" shape; it had to enumerate
	// each nil-collision field of a primitive in another function, so adding a
	// fourth such field to the bracket write would have silently broken rollback.
	matchWriteRestore
)

// writeToPoolOrBracket performs one match write against whichever store the
// match id resolves to, and reports whether the payload named a pairing that is
// not this match's (a client error the caller maps to HTTP 409).
//
// A match id resolves to a pool/league match or a knockout one only at run
// time, so every score write has to try the pool store and fall through to the
// bracket on errMatchNotFound. That fall-through was hand-copied at four sites
// (both non-tx writers and both tx twins), each threading `policy` into two
// separate calls. matchWritePolicy exists precisely because a policy reaching
// only one branch silently reverts to forward semantics for the matches that
// fall through — so leaving the branch SELECTION duplicated kept the shape of
// the bug one level up: a fifth write path had to re-remember it.
//
// The tx twin is writeToPoolOrBracketTx (scoring_tx.go); they differ only in
// which store handle they take, which is why they are two functions rather than
// one with a nil check. Callers keep their own error policy: `mismatch` is
// RETURNED rather than turned into an error here, because the forward writers
// reject it while the K3 restore deliberately ignores it (a snapshot replays
// sides captured from this same match).
func (e *Engine) writeToPoolOrBracket(compId, matchId string, result *state.MatchResult, policy matchWritePolicy) (mismatch bool, err error) {
	perr := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		mismatch = applyPoolWrite(r, result, policy)
	})
	if perr == nil {
		return mismatch, nil
	}
	if !errors.Is(perr, errMatchNotFound) {
		return false, perr
	}
	// The SAME policy the pool branch would have used.
	return false, e.recordBracketMatchResult(compId, matchId, result, policy)
}

// applyPoolWrite folds the stored match into an incoming result and then
// performs the whole-struct overwrite. It reports whether the write was
// ABANDONED, in which case the stored match is left untouched. That happens two
// ways, mirroring applyBracketMatchResult: the payload names a pairing that is
// not this match's (a client error the caller maps to 409), or the timestamp
// guard drops it as stale (not an error at all, the newer result simply
// stands). Only the first is reported as a `mismatch`.
//
// It does the `*stored = *result` assignment itself rather than leaving it to
// each caller, so that the overwrite is unreachable without the merge. This is
// ONE function because the hand-copied version drifted twice, in the direction
// that hurts: preserveDaihyosenOutcome reached three of the four writers and
// missed the live /score path, and the CorrectionReason inherit existed only in
// the Tx forward writer, so a team quick-score landing after a kachinuki reopen
// blanked the operator's audit justification through the other two.
//
// SCOPE of "one home", stated precisely because the previous wording
// overclaimed: this owns the ENGINE-side preservation. Server-owned flags a
// client must not be able to clear (ReopenPending) are re-stamped at the HTTP
// boundary instead - see handlers_match.go - so a new field of that kind still
// needs a decision about which layer preserves it.
func applyPoolWrite(stored, result *state.MatchResult, policy matchWritePolicy) (mismatch bool) {
	// reconcileSides BACKFILLS omitted sides as a side effect and only reports
	// the mismatch, so it must run under both policies; hoisted out of the
	// condition below because folding it into a short-circuit would let a later
	// tidy (cheap comparison first) silently drop the backfill.
	sidesDisagree := reconcileSides(result, stored.SideA, stored.SideB)
	// Match identity is fixed at generation; a score must not rewrite it. The
	// restore policy replays sides captured from this same match, so a mismatch
	// there is not a client error.
	if sidesDisagree && policy == matchWriteForward {
		return true
	}
	// Timestamp last-write-wins, the SAME guard, the same primitive and now the
	// same call shape the bracket branch uses: a reconnecting offline court's
	// stale change loses to a newer result recorded elsewhere. The restore
	// exemption lives inside applyMatchWrite, which is load-bearing here — unlike
	// the bracket's, this branch's rollback snapshot carries a real stamp.
	if !applyMatchWrite(result, stored.ModifiedAt, policy) {
		return false
	}
	// Preserve generation-time participant ids + resolve winner id across the
	// overwrite: score requests carry side NAMES only. See backfillMatchIdentity.
	backfillMatchIdentity(result, stored)
	// Keep the stored stamp when this write is unstamped, so an un-stamped
	// client cannot reset the field to 0 and reopen the match to stale writes.
	// The whole-struct overwrite below would otherwise zero it; the bracket
	// twin needs the same rule and states it at its own assignment.
	if result.ModifiedAt == 0 {
		result.ModifiedAt = stored.ModifiedAt
	}
	if result.Court == "" {
		result.Court = stored.Court
	}
	if result.ScheduledAt == "" {
		result.ScheduledAt = stored.ScheduledAt
	}
	result.Round = stored.Round
	// Everything below is forward-only; matchWriteRestore inherits nothing more.
	if policy == matchWriteForward {
		// Set-if-empty: see the drift note on this function.
		if result.CorrectionReason == "" {
			result.CorrectionReason = stored.CorrectionReason
		}
		// The MATCH-level verdict, under preserveMatchHantei's guards. A pool
		// hantei now persists (encodeHanteiIntoIppons, state/pools.go), so
		// without this the whole-struct overwrite below would let any
		// verdict-silent re-score erase it; with a BARE carry it would instead
		// stamp a withdrawal as a judges' decision. See that function.
		preserveMatchHantei(stored.DecidedByHantei != nil && *stored.DecidedByHantei, result)
		// A pool/league team encounter can hold a daihyosen (findMatchForDaihyosen
		// accepts pool matches). Without this a verdict-silent write from a stale
		// second editor erases a recorded hantei, which in accrueTeamSubResults
		// flips the bout from a win into a draw and so moves the team's IV/IL/IT
		// tie-break figures.
		preserveDaihyosenOutcome(stored.SubResults, result)
	}
	*stored = *result
	return false
}

// applyHansokuIppons auto-awards ippons from accumulated hansoku counts per
// FIK Article 20: every 2 hansoku on one side grants 1 ippon to the opponent.
// Strips any prior 'H' entries and re-appends the correct count so that both
// increases and decreases in hansoku are handled correctly on re-scores.
func applyHansokuIppons(result *state.MatchResult) {
	if result == nil {
		return
	}
	applyOneSide := func(hansoku int, ippons *[]string) {
		expected := hansoku / 2
		if *ippons == nil && expected == 0 {
			return
		}
		filtered := make([]string, 0, len(*ippons))
		for _, v := range *ippons {
			if v != "H" {
				filtered = append(filtered, v)
			}
		}
		for range expected {
			filtered = append(filtered, "H")
		}
		*ippons = filtered
	}
	applyOneSide(result.HansokuA, &result.IpponsB)
	applyOneSide(result.HansokuB, &result.IpponsA)
	for i := range result.SubResults {
		applyOneSide(result.SubResults[i].HansokuA, &result.SubResults[i].IpponsB)
		applyOneSide(result.SubResults[i].HansokuB, &result.SubResults[i].IpponsA)
	}
}

// isWinForSide reports whether subWinner indicates a win for the given
// match-level side. It checks both the canonical match side name and the
// sub-result-level side name (which may differ when the operator used a
// player name instead of the team name). The subSide != "" guard prevents
// "" == "" false-positives when sub-bout sides are unset (quick-score).
func isWinForSide(subWinner, matchSide, subSide string) bool {
	return subWinner == matchSide || (subSide != "" && subWinner == subSide)
}

// accrueTeamSubResults folds one completed team match's sub-bout results into
// both sides' standings (IV/IL/IT and PW/PL). Shared by the pool/league core
// (computeStandingsFrom) and SwissStandings so the team tie-break accrual
// rules live in exactly one place: a future change to how draws, empty-winner
// sub-bouts, or countScoringIppons feed the tie-break cannot silently diverge
// between formats.
func accrueTeamSubResults(sA, sB *state.PlayerStanding, m state.MatchResult) {
	for _, sub := range m.SubResults {
		sideAWin := isWinForSide(sub.Winner, m.SideA, sub.SideA)
		sideBWin := isWinForSide(sub.Winner, m.SideB, sub.SideB)
		switch {
		case sideAWin:
			sA.IndividualWins++
			sB.IndividualLosses++
		case sideBWin:
			sB.IndividualWins++
			sA.IndividualLosses++
		case sub.Winner == "":
			sA.IndividualDraws++
			sB.IndividualDraws++
		}
		// countScoringIppons (not len): a completed bout can retain
		// "•" unfilled-slot placeholders or empty entries, which are
		// not scored points. This keeps the ranking PointsWon in sync
		// with the wire teamResult PW (state.TeamResultFrom uses the
		// same rule) so the displayed points and the tie-break points
		// cannot drift.
		sA.PointsWon += countScoringIppons(sub.IpponsA)
		sA.PointsLost += countScoringIppons(sub.IpponsB)
		sB.PointsWon += countScoringIppons(sub.IpponsB)
		sB.PointsLost += countScoringIppons(sub.IpponsA)
	}
}

// teamScoreSummary / individualScoreSummary render the human-readable score
// cell for a standing. One format definition each, shared by the pool/league
// and Swiss standings assembly loops so the two tables can never drift.
func teamScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d L:%d D:%d | IV:%d IL:%d IT:%d | PW:%d PL:%d",
		s.Wins, s.Losses, s.Draws,
		s.IndividualWins, s.IndividualLosses, s.IndividualDraws,
		s.PointsWon, s.PointsLost)
}

func individualScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d L:%d D:%d | P:%d-%d",
		s.Wins, s.Losses, s.Draws, s.IpponsGiven, s.IpponsTaken)
}

// deriveDaihyosenWinner fills result.Winner from a completed daihyosen
// sub-result (Position == -1) when the caller has not set it explicitly.
// Playoff team matches end in daihyosen when IV and PW are tied; the
// operator scores a single representative bout whose winner becomes the
// team match winner. The sub-result Winner may be the representative
// player's name or the team name; this function maps it back to the
// canonical team name (result.SideA / result.SideB) using the same
// side-matching logic as computeStandings.
func deriveDaihyosenWinner(result *state.MatchResult) {
	if result == nil || result.Winner != "" {
		return
	}
	for _, sub := range result.SubResults {
		if sub.Position != state.DaihyosenSubPosition || sub.Winner == "" {
			continue
		}
		sideAWin := isWinForSide(sub.Winner, result.SideA, sub.SideA)
		sideBWin := isWinForSide(sub.Winner, result.SideB, sub.SideB)
		switch {
		case sideAWin:
			result.Winner = result.SideA
		case sideBWin:
			result.Winner = result.SideB
		}
		return
	}
}

// backfillMatchIdentity preserves the participant ids stamped on a pool/league
// match at generation, and resolves the winner id. It runs inside every
// score-write closure right before the whole-struct `*r = *result` overwrite:
// score requests carry side NAMES only (no ids), so without this the overwrite
// would wipe SideAID/SideBID on the first score and break league-matrix cell
// mapping. WinnerID is resolved from an explicit WinnerSide hint when present
// (the only way to tell apart two participants who share a name), else from a
// name match (unambiguous unless both sides share a name), and as a last
// resort, for a same-name head-to-head with no side hint, from the scoreline
// (the side with more ippons is the winner). `stored` is the on-disk record
// (with the generation-time ids); `result` is the incoming score. Purely
// additive, never touches name-based scoring/standings logic.
//
// It also preserves the daihyosen/tiebreaker rep-player names (mp-62vr) the
// same preserve-on-empty way: once the operator records which player each team
// fielded, a later score write that omits them (e.g. a correction that only
// re-sends the ippons) must not wipe them. An explicit value in `result`
// always wins, so the operator can still change the rep player.
func backfillMatchIdentity(result, stored *state.MatchResult) {
	if result.RepPlayerA == "" {
		result.RepPlayerA = stored.RepPlayerA
	}
	if result.RepPlayerB == "" {
		result.RepPlayerB = stored.RepPlayerB
	}
	if result.SideAID == "" {
		result.SideAID = stored.SideAID
	}
	if result.SideBID == "" {
		result.SideBID = stored.SideBID
	}
	if result.WinnerID != "" {
		return
	}
	switch {
	case result.WinnerSide == "A":
		result.WinnerID = result.SideAID
	case result.WinnerSide == "B":
		result.WinnerID = result.SideBID
	case result.Winner != "" && result.Winner == result.SideA && result.Winner != result.SideB:
		result.WinnerID = result.SideAID
	case result.Winner != "" && result.Winner == result.SideB && result.Winner != result.SideA:
		result.WinnerID = result.SideBID
	case result.Winner != "":
		// Same-name head-to-head (Winner matches both sides) with no
		// WinnerSide hint, e.g. the admin score editor, which picks a
		// winner by name. The winning side usually has more ippons, so
		// infer from the scoreline. Equal counts (hantei/undecidable) or a
		// draw (empty Winner) leave WinnerID empty → name fallback.
		switch a, b := countScoringIppons(result.IpponsA), countScoringIppons(result.IpponsB); {
		case a > b:
			result.WinnerID = result.SideAID
		case b > a:
			result.WinnerID = result.SideBID
		}
	}
}

// preserveLoserScore implements FIK Regulations Article 32 ("Any point
// scored by the shiai-funo-sha shall remain valid") on a default-win
// result. The winner keeps the maru default-win fill already set on
// `result`; this only touches the WITHDRAWING side, which keeps whatever it
// had struck, and preserves the encounter's prior sub-bouts so a team
// withdrawal never wipes the sub-bouts already fought (both teams' results
// stand and continue to count in IV/PW standings via accrueTeamSubResults).
//
// prior is the match state before the decision; nothing is preserved unless
// its sides still match — a drifted or re-oriented prior must not
// mis-attribute points. decisionBy names the WITHDRAWING side
// ("shiro" = SideB/Shiro, "aka" = SideA/Aka). Shared by the two
// RecordDecision twins.
func preserveLoserScore(result, prior *state.MatchResult, decisionBy string) {
	if prior == nil || prior.SideA != result.SideA || prior.SideB != result.SideB {
		return
	}
	result.SubResults = prior.SubResults
	// Preserve only points the loser actually STRUCK (Art. 32 says "any point
	// scored"): strip the maru marker so a prior default-win decision's ○○
	// fill is never carried forward as if it were struck points. This matters
	// on the T103 re-decision path when decisionBy flips — the side that was
	// the prior winner holds maru, not real points, and must not inherit it as
	// the new loser.
	if decisionBy == "shiro" {
		result.IpponsB = struckIppons(prior.IpponsB) // loser = SideB (Shiro)
	} else {
		result.IpponsA = struckIppons(prior.IpponsA) // loser = SideA (Aka)
	}
}

// struckIppons returns the real struck ippon letters from a slice, dropping
// empty entries, the "•" UI placeholder, and the domain.DefaultWinIppon maru
// (an awarded default win, not a struck point). Distinct from
// countScoringIppons, which counts the maru as a scoring ippon by design.
func struckIppons(ippons []string) []string {
	var out []string
	for _, v := range ippons {
		if v != "" && v != domain.IpponPlaceholder && v != domain.DefaultWinIppon {
			out = append(out, v)
		}
	}
	return out
}

// countScoringIppons is the package-local spelling of domain.CountScoringIppons
// (real ippon marks, ignoring empties and the "•" placeholder; the default-win
// maru counts like any struck ippon). The rule itself lives in domain so the
// store's TeamResultFrom and the HTTP validator share this exact count.
func countScoringIppons(ippons []string) int {
	return domain.CountScoringIppons(ippons)
}

func (e *Engine) RecordMatchResult(compId string, matchId string, result *state.MatchResult) error {
	result.ID = matchId // normalize ID-less payloads before overwriting
	applyHansokuIppons(result)
	return e.writeMatchResult(compId, matchId, result, matchWriteForward)
}

// writeMatchResult persists the result without applying hansoku auto-award.
// RecordMatchResult calls it after applyHansokuIppons with matchWriteForward;
// the K3 rollback calls it with matchWriteRestore. The policy reaches whichever
// branch the match id resolves to — see matchWritePolicy for what each inherits.
func (e *Engine) writeMatchResult(compId string, matchId string, result *state.MatchResult, policy matchWritePolicy) error {
	sideMismatch, err := e.writeToPoolOrBracket(compId, matchId, result, policy)
	if err != nil {
		return err
	}
	if sideMismatch {
		return ErrMatchSideMismatch
	}
	// Side-effect writes are non-fatal: the match score is already on disk,
	// so propagating would cause a 500 retry that double-records the score.
	if _, err := e.recordIneligibilityFromDecision(compId, matchId, result); err != nil {
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
	}
	return nil
}

// RecordMatchResultWithIneligibility is the variant used by the score
// and decision handlers that need to broadcast the
// `competitor-status-updated` SSE event after a kiken/fusenpai is
// recorded. It returns the new CompetitorStatus (or nil when none was
// written) alongside any error.
//
// The match-score persistence semantics are identical to
// RecordMatchResult; only the side-effect status is surfaced for the
// caller's broadcast. Side-effect write failures are still non-fatal,
// the function returns (nil, nil) and logs.
//
// T085/T092.
func (e *Engine) RecordMatchResultWithIneligibility(compId string, matchId string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	result.ID = matchId

	// Engi dispatch seam: a flag-scored competition records via the engi slice
	// (pool or bracket, decided internally) and skips the kendo ippon path
	// entirely. Engi has no eligibility concept, so the status return is nil.
	comp, loadErr := e.store.LoadCompetition(compId)
	if loadErr != nil {
		return nil, fmt.Errorf("RecordMatchResultWithIneligibility: load competition %s: %w", compId, loadErr)
	}
	if comp != nil && comp.Engi {
		rec, recErr := e.recordEngiMatchResult(compId, matchId, result.FlagsA, result.FlagsB, result.CorrectionReason)
		if recErr != nil {
			return nil, recErr
		}
		backfillEngiResult(result, rec)
		return nil, nil
	}

	applyHansokuIppons(result)
	deriveDaihyosenWinner(result)

	// T105/CHK047: capture the prior result so we can rollback if the atomic
	// ineligibility write below fails with AlreadyIneligibleError.
	prior, _ := e.lookupExistingResult(compId, matchId)

	// Kachinuki bout logs merge BY POSITION rather than replace wholesale
	// (ACID: a client whose local log is behind the server must never
	// destroy server-appended bouts). Applied here at the entry point,
	// BEFORE the pool/bracket write primitives, so the rollback path
	// below (which replays `prior` through those primitives) still
	// restores the pre-write state exactly. A merge-time rejection (e.g. a
	// kachinuki-exhaustion write ending on a tied bout, mp-gmcg review R2)
	// returns BEFORE any write primitive, so nothing is persisted.
	if merr := applyKachinukiMerge(comp, prior, result); merr != nil {
		return nil, merr
	}

	sideMismatch, err := e.writeToPoolOrBracket(compId, matchId, result, matchWriteForward)
	if err != nil {
		return nil, err
	}
	if sideMismatch {
		return nil, ErrMatchSideMismatch
	}
	status, err := e.recordIneligibilityFromDecision(compId, matchId, result)
	if err != nil {
		// K2/CHK047: when the atomic check-and-set inside
		// recordIneligibilityFromDecision detects a concurrent kiken
		// (different operator already wrote ineligibility for this
		// player from another match), propagate the error so the handler
		// can return HTTP 409.
		var alreadyErr *AlreadyIneligibleError
		if errors.As(err, &alreadyErr) {
			// K3/CHK047: rollback the partial write. The match score was
			// already persisted, but the intended loser is already
			// ineligible from a different match. Revert the match score
			// to its prior state before returning 409 so the operator
			// sees a clean rejection rather than a mutated match.
			if prior != nil {
				_ = e.writeMatchResult(compId, matchId, prior, matchWriteRestore)
			}
			return nil, err
		}
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
		return nil, nil
	}
	return status, nil
}

// standingsFlightRetries bounds how many times CalculatePoolStandings will
// re-enter the single-flight path before giving up on collapsing and computing
// directly. Each retry costs two stats and a map load, so a small number is
// enough to absorb the ordinary "winner's snapshot predates my write" case.
const standingsFlightRetries = 3

func (e *Engine) CalculatePoolStandings(compId string) (map[string][]state.PlayerStanding, error) {
	// Retry as a bounded loop rather than by self-recursion: every pass
	// re-samples the tokens, so the stack stays flat, and the bound means
	// termination does not rest on the (correct but subtle) argument that the
	// flight winner always deletes its entry before releasing the losers. A spin
	// in this function would wedge a live scoring handler, which is a worse
	// failure than the stale read the retry exists to avoid.
	for attempt := 0; attempt < standingsFlightRetries; attempt++ {
		// Fast path: return cached result when neither pool-matches nor overrides
		// changed. Tokens are sampled BEFORE computeStandings below, see
		// standingsTokens for why mtime alone is not sound.
		tokens := e.sampleStandingsTokens(compId)
		if result, ok := e.cachedStandingsIfValid(compId, tokens); ok {
			return result, nil
		}

		// Single-flight: collapse concurrent cold-cache callers into one compute.
		flightV, _ := e.standingsFlight.LoadOrStore(compId, &sync.Once{})
		once := flightV.(*sync.Once)
		var (
			flightResult map[string][]state.PlayerStanding
			flightErr    error
		)
		once.Do(func() {
			defer e.standingsFlight.Delete(compId)
			flightResult, flightErr = e.computeStandings(compId)
			if flightErr == nil {
				// Stamped with the PRE-compute tokens on purpose: a write landing
				// during computeStandings then leaves a strictly newer version on
				// the next call, forcing a recompute rather than blessing a result
				// that may predate that write.
				e.standingsCache.Store(compId, &standingsCacheEntry{
					standingsTokens: tokens,
					result:          flightResult,
				})
			}
		})
		if flightErr != nil {
			return nil, flightErr
		}
		if flightResult != nil {
			return flightResult, nil
		}

		// Lost the flight race. The winner stamped its entry with the tokens IT
		// sampled, which may predate a write that had already landed before we
		// sampled ours, so its result is not automatically ours to return: the
		// caller here is typically advanceMixedPools right after saving a pool
		// score, and handing it standings that predate its own write is exactly
		// the fresh-matches-vs-stale-standings split that mp-n6ke fixed. Validate
		// against our own tokens, same check as the fast path.
		if result, ok := e.cachedStandingsIfValid(compId, tokens); ok {
			return result, nil
		}
		// Either the winner's snapshot predates our tokens, or the cache was
		// invalidated between Do completion and this Load. Either way, loop: the
		// winner has already deleted the flight, so the next pass computes fresh
		// (and any sibling losers rejoin that one compute rather than each
		// starting their own).
	}

	// Out of retries under sustained contention. Compute directly: single-flight
	// is an optimisation, freshness is not negotiable. Deliberately not cached,
	// the tokens moved under us often enough that any stamp we chose would be a
	// guess, and a guess in this cache is what mp-n6ke was.
	return e.computeStandings(compId)
}

// cachedStandingsIfValid returns the cached standings for a competition only if
// the entry was stamped with exactly the tokens passed in.
//
// This is THE read path for the standings cache: every caller goes through it,
// so there is one place where "is this entry still valid" is decided. That is
// deliberate. The bug this cache was rebuilt for was a read path that skipped
// the check (the single-flight loser returned the winner's entry unvalidated),
// and a check open-coded at each site is one a future read path can silently
// half-implement. Callers pass a snapshot from sampleStandingsTokens rather than
// having this function sample its own, so the same tokens govern the lookup and
// whatever the caller does next with the result.
func (e *Engine) cachedStandingsIfValid(compId string, tokens standingsTokens) (map[string][]state.PlayerStanding, bool) {
	v, ok := e.standingsCache.Load(compId)
	if !ok {
		return nil, false
	}
	cached := v.(*standingsCacheEntry)
	if cached.standingsTokens != tokens {
		return nil, false
	}
	return cached.result, true
}

// sampleStandingsTokens reads the current cache-validity key for a competition.
// Callers must sample ONCE and reuse the snapshot: re-reading per comparison
// would let a write slip between two reads of the same logical check.
func (e *Engine) sampleStandingsTokens(compId string) standingsTokens {
	return standingsTokens{
		poolMatchesMtime:   e.store.FileMtime(compId, "pool-matches.csv"),
		overridesMtime:     e.store.FileMtime(compId, "overrides.json"),
		poolMatchesVersion: e.store.FileVersion(compId, "pool-matches.csv"),
		overridesVersion:   e.store.FileVersion(compId, "overrides.json"),
	}
}

// poolStandingsLoader is the read surface computeStandingsFrom needs. Both
// *state.Store and state.StoreTx satisfy it (identical signatures), so the
// single scoring core below can run either against the cached/single-flight
// store path (CalculatePoolStandings) or inside a write transaction (the
// mp-e2k1 pool-rescore guard in scoring_tx.go), with NO duplicated formula.
type poolStandingsLoader interface {
	LoadCompetition(compID string) (*state.Competition, error)
	LoadPools(compID string) ([]helper.Pool, error)
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
}

// computeStandings is the non-tx standings core. It delegates to the shared
// computeStandingsFrom so the kendo scoring weights, tiebreaker/daihyosen
// grouping, and override sort live in exactly ONE place.
func (e *Engine) computeStandings(compId string) (map[string][]state.PlayerStanding, error) {
	// The engi dispatch lives inside computeStandingsFrom so it happens on a
	// single competition load shared with the kendo path, not one load here plus
	// another there.
	return e.computeStandingsFrom(e.store, compId)
}

// computeStandingsFrom is the single source of truth for pool standings. It
// reads pools/matches/competition through loader (so a transaction can pass a
// StoreTx and see its just-applied write), and reads overrides via
// e.store.LoadOverrides directly, overrides are read-only in the scoring path
// and are not part of any transaction's mutation set, so no tx variant is
// needed.
func (e *Engine) computeStandingsFrom(loader poolStandingsLoader, compId string) (map[string][]state.PlayerStanding, error) {
	comp, err := loader.LoadCompetition(compId)
	if err != nil {
		// Propagate a genuine read/parse fault rather than silently proceeding
		// with comp==nil, which would pick the wrong scoring mode (individual vs
		// team) and undermine the tx guard's fail-closed intent. A genuinely
		// absent competition maps to (nil, nil) and is left as individual mode.
		return nil, fmt.Errorf("computeStandingsFrom: load competition %s: %w", compId, err)
	}
	// Engi dispatch seam: a flag-scored competition delegates to the engi
	// standings slice; the kendo logic below is left fully unchanged. Uses the
	// same loader so a tx caller sees its just-applied write. Non-engi tx callers
	// never reach the engi branch: the tx scoring path early-returns on engi
	// before computeStandingsFrom is ever called.
	if comp != nil && comp.Engi {
		return e.computeEngiStandings(loader, compId)
	}

	pools, err := loader.LoadPools(compId)
	if err != nil {
		return nil, err
	}
	results, err := loader.LoadPoolMatches(compId)
	if err != nil {
		return nil, err
	}

	isTeam := comp != nil && comp.TeamSize > 0

	// Map match results by pool using poolNameFromMatchID so hyphenated pool
	// names (e.g. "Pool A-East") are handled correctly for all ID forms
	// ("Pool A-East-0", "Pool A-East-TB-0", "Pool A-East-DH-0").
	poolResults := make(map[string][]state.MatchResult)
	for _, r := range results {
		if pn, ok := poolNameFromMatchID(r.ID); ok {
			poolResults[pn] = append(poolResults[pn], r)
		}
	}

	allStandings := make(map[string][]state.PlayerStanding)
	for _, p := range pools {
		matches := poolResults[p.PoolName]
		playerStandings := make(map[string]*state.PlayerStanding)
		for _, player := range p.Players {
			// helper.Player is a type alias for domain.Player (NFR-007);
			// the pool player flows directly into PlayerStanding.
			playerStandings[player.Name] = &state.PlayerStanding{
				Player: player,
			}
		}

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Tiebreaker and pool-daihyosen matches don't count toward regular pool stats.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			sA := playerStandings[m.SideA]
			sB := playerStandings[m.SideB]
			if sA == nil || sB == nil {
				continue
			}

			// Team W/L/D (or individual W/L/D)
			if m.Winner == m.SideA {
				sA.Wins++
				sB.Losses++
			} else if m.Winner == m.SideB {
				sB.Wins++
				sA.Losses++
			} else if state.IsDraw(m.Decision) || m.Winner == "" {
				sA.Draws++
				sB.Draws++
			}

			if isTeam && len(m.SubResults) > 0 {
				accrueTeamSubResults(sA, sB, m)
			} else {
				// Individual scoring: ippons at match level. countScoringIppons
				// (not len) so leftover "•" placeholders / empty slots in a
				// completed match don't inflate the ippon tallies.
				sA.IpponsGiven += countScoringIppons(m.IpponsA)
				sA.IpponsTaken += countScoringIppons(m.IpponsB)
				sB.IpponsGiven += countScoringIppons(m.IpponsB)
				sB.IpponsTaken += countScoringIppons(m.IpponsA)
			}
		}

		var sorted []state.PlayerStanding
		for _, s := range playerStandings {
			if isTeam {
				// Single packed ranking score over the full team tiebreak chain
				// (W, L, T, IV, IL, IT, PW, PL). See teamStandingPoints.
				s.Points = teamStandingPoints(*s)
				s.ScoreSummary = teamScoreSummary(s)
			} else {
				// Single packed ranking score over the individual chain
				// (W, L, D, ippons given, ippons taken). See individualStandingPoints.
				s.Points = individualStandingPoints(*s)
				s.ScoreSummary = individualScoreSummary(s)
			}
			sorted = append(sorted, *s)
		}

		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Points != sorted[j].Points {
				return sorted[i].Points > sorted[j].Points
			}
			// Total-order tiebreaker. A genuine Points tie (equal on every ranking
			// criterion, no supplementary bout) must resolve deterministically
			// rather than by the iteration order of playerStandings (ranged just
			// above), or two tied qualifiers would seed into knockout slots in a
			// run-dependent order (mp-xemw). ID first (stable UUID); Name is the
			// human-unique fallback for players persisted without an ID. This only
			// reorders players already equal on Points, so applyTiebreakSort (which
			// reorders a tied group only when a decided TB/DH bout exists) and
			// markTiedStandings (which flags on Points equality) are unaffected.
			if sorted[i].Player.ID != sorted[j].Player.ID {
				return sorted[i].Player.ID < sorted[j].Player.ID
			}
			return sorted[i].Player.Name < sorted[j].Player.Name
		})

		// Apply supplementary-bout results as a secondary sort within each tied
		// group (groups located by the single detectPoolTies Points-equality
		// walk). Win counts are scoped per group, only bouts between members of
		// the same tied group count, so results from an unrelated tied group
		// never bleed across. TB (ippon-shobu) applies to all formats; DH
		// (representative) only to team competitions.
		applyTiebreakSort(sorted, matches, IsTiebreakerMatchID)
		if isTeam {
			applyTiebreakSort(sorted, matches, IsPoolDaihyosenMatchID)
		}

		// Detect ties before applying manual rank overrides. detectPoolTies walks
		// adjacent elements, so it must run while the slice is still Points-sorted.
		// Overrides only change the display order; the underlying scoring tie is real
		// regardless of how the operator chose to resolve it.
		markTiedStandings(comp, sorted, poolResults[p.PoolName])

		// Apply manual rank overrides
		overrides, _ := e.store.LoadOverrides(compId)
		if overrides != nil && overrides.PoolRanks[p.PoolName] != nil {
			poolOverrides := overrides.PoolRanks[p.PoolName]
			sort.Slice(sorted, func(i, j int) bool {
				rankI, okI := poolOverrides[sorted[i].Player.Name]
				rankJ, okJ := poolOverrides[sorted[j].Player.Name]
				if okI && okJ {
					return rankI < rankJ
				}
				if okI {
					return true
				}
				if okJ {
					return false
				}
				return sorted[i].Rank < sorted[j].Rank
			})
		}

		poolHasOverrides := overrides != nil && overrides.PoolRanks[p.PoolName] != nil
		for i := range sorted {
			sorted[i].Rank = i + 1
			if poolHasOverrides {
				if _, ok := overrides.PoolRanks[p.PoolName][sorted[i].Player.Name]; ok {
					sorted[i].IsOverridden = true
				}
			}
		}

		applyJointThirdRanks(comp, sorted, poolHasOverrides)
		allStandings[p.PoolName] = sorted
	}

	return allStandings, nil
}

// applyJointThirdRanks implements the kendo joint-3rd convention. When a league
// has LeagueTwoThirdPlaces enabled, a genuine Points-tie whose best finishing
// position is 3rd or lower is given a SHARED rank (the group's best 1-based
// position) so both the standings table and the closing-ceremony podium show two
// (or more) equal 3rd places instead of relabeling the 4th finisher. Ranks 1 and
// 2 are always kept distinct: a top-two tie is decided by a tie-breaker, never
// shared. It is a no-op for non-leagues, when the setting is off, or when the
// pool carries manual rank overrides (the operator's explicit order wins).
// Scoping to leagues keeps mixed/playoffs knockout seeding strictly sequential;
// naginata leagues leave the setting off and so keep a single 3rd.
//
// Mutates sorted in place. Callers pass a Points-sorted slice that already has
// sequential ranks assigned, so detectPoolTies groups by adjacent Points
// equality exactly as it does for the amber-tie highlight.
func applyJointThirdRanks(comp *state.Competition, sorted []state.PlayerStanding, poolHasOverrides bool) {
	if comp == nil || comp.Format != state.CompFormatLeague || !comp.LeagueTwoThirdPlaces || poolHasOverrides {
		return
	}
	for _, positions := range detectPoolTies(sorted) {
		minRank := positions[0] + 1
		if minRank < 3 {
			continue
		}
		for _, idx := range positions {
			sorted[idx].Rank = minRank
		}
	}
}

// markTiedStandings sets Tied=true on standings rows that are genuinely tied
// (same Points on every criterion) and where the tie is visible/consequential
// given the competition format and match-completion state.
//
// Gating rules by format:
//
//   - Pools (not league): mark tied groups only after ALL regular matches in
//     that pool are complete. Regular = excludes TB ("-TB-") and DH ("-DH-")
//     supplementary bouts. Pre-completion the rank is provisional, so no amber.
//
//   - League (comp.Format == state.CompFormatLeague), both team and individual:
//     EMERGING-TIE trigger. Once ANY of the top-N competitors (N =
//     effectiveTopN(comp), clamped to pool size) has completed ALL their own
//     regular fights, mark Tied=true on every tied group whose MinPosition <=
//     effectiveTopN AND that is not covered by the two-joint-3rd-places
//     exemption (LeagueTwoThirdPlaces + MinPosition >= 3).
//
// matches contains only the regular+supplementary matches for this specific
// pool (already filtered upstream by poolNameFromMatchID).
func markTiedStandings(comp *state.Competition, sorted []state.PlayerStanding, matches []state.MatchResult) {
	if len(sorted) == 0 {
		return
	}

	// Separate regular matches (exclude supplementary TB/DH bouts).
	var regularMatches []state.MatchResult
	for _, m := range matches {
		if !IsTiebreakerMatchID(m.ID) && !IsPoolDaihyosenMatchID(m.ID) {
			regularMatches = append(regularMatches, m)
		}
	}

	isLeague := comp != nil && comp.Format == state.CompFormatLeague

	if isLeague {
		markTiedStandingsLeague(comp, sorted, regularMatches)
	} else {
		markTiedStandingsPools(sorted, regularMatches)
	}
}

// markTiedStandingsPools marks tied rows in a pools (non-league) competition.
// Rows are only marked once ALL regular matches in the pool are complete.
func markTiedStandingsPools(sorted []state.PlayerStanding, regularMatches []state.MatchResult) {
	// Gate: there must be at least one regular match, and all must be completed.
	// With no matches at all the pool hasn't started, everyone is tied at 0
	// points, which must NOT surface as amber.
	if len(regularMatches) == 0 {
		return
	}
	for _, m := range regularMatches {
		if m.Status != state.MatchStatusCompleted {
			return // pool not yet finished, no amber
		}
	}

	// Pool is complete; mark every tied group.
	for _, positions := range detectPoolTies(sorted) {
		for _, idx := range positions {
			sorted[idx].Tied = true
		}
	}
}

// markTiedStandingsLeague marks tied rows in a league competition using the
// emerging-tie trigger: once ANY top-N competitor has finished all their own
// regular fights, mark consequential tied groups amber. Works for both team
// and individual leagues.
func markTiedStandingsLeague(comp *state.Competition, sorted []state.PlayerStanding, regularMatches []state.MatchResult) {
	topN := min(effectiveTopN(comp), len(sorted))

	// Build per-competitor regular match counts and completion status.
	// A competitor is "done" when every regular match they appear in is Completed.
	type compStatus struct {
		total     int
		completed int
	}
	status := make(map[string]*compStatus, len(sorted))
	for _, s := range sorted {
		status[s.Player.Name] = &compStatus{}
	}
	for _, m := range regularMatches {
		if _, okA := status[m.SideA]; okA {
			status[m.SideA].total++
			if m.Status == state.MatchStatusCompleted {
				status[m.SideA].completed++
			}
		}
		if _, okB := status[m.SideB]; okB {
			status[m.SideB].total++
			if m.Status == state.MatchStatusCompleted {
				status[m.SideB].completed++
			}
		}
	}

	// Check if ANY top-N competitor has completed all their own fights.
	triggerFired := false
	for i := range topN {
		name := sorted[i].Player.Name
		cs := status[name]
		if cs != nil && cs.total > 0 && cs.completed == cs.total {
			triggerFired = true
			break
		}
	}
	if !triggerFired {
		return
	}

	// Trigger has fired: mark tied groups that intersect the top-N band and
	// are not covered by the two-joint-3rd-places exemption.
	for _, positions := range detectPoolTies(sorted) {
		minPos := positions[0] + 1 // 1-based
		g := TiedGroup{
			Teams:       standingsAt(sorted, positions),
			MinPosition: minPos,
			MaxPosition: positions[len(positions)-1] + 1,
		}
		if !isConsequentialTie(g, comp) {
			continue
		}
		for _, idx := range positions {
			sorted[idx].Tied = true
		}
	}
}

// recordBracketMatchResult is the main bracket-side scoring path. It
// runs the entire mutation (find target match, set winner/status/
// scores, propagate winner to subsequent rounds) under the per-
// competition lock via state.Store.UpdateBracket so two operators
// scoring different elimination-round matches in the same competition
// can't lose each other's mutations through TOCTOU.
//
// Pre-atomic-primitive, LoadBracket + mutate + SaveBracket ran
// without a shared lock between Load and Save; the propagateBracketWinner
// step amplified the risk because it mutates ADJACENT bracket cells
// (the next-round match), so a concurrent save with a stale view
// could clobber another operator's propagation too.
func (e *Engine) recordBracketMatchResult(compId string, matchId string, result *state.MatchResult, policy matchWritePolicy) error {
	return e.store.UpdateBracket(compId, func(bracket *state.Bracket) error {
		return e.applyBracketResultIn(bracket, compId, matchId, result, policy)
	})
}

// applyMatchWrite reports whether a match write should apply under the
// timestamp last-write-wins guard (mp-y3nk). A deliberate operator CORRECTION
// (CorrectionReason set) always applies: it is an explicit decision made under
// the handler's correction-audit lock, not a reconnect replay, so it must never
// be dropped as "stale". Otherwise it is pure timestamp LWW.
//
// ONE primitive for both branches, because a match is a match: which store it
// lands in is an implementation detail of the phase it is in, and an operator
// cannot be expected to know that a reconnecting court's stale change is
// discarded in the knockout and applied in the pool.
//
// It used to be bracket-only, not by choice but by omission: the guard needs a
// stored stamp to compare against, and pool-matches.csv had no column for one
// (bracket.json marshals every exported field, so the bracket got it for
// free). With that column added this became symmetrical, and both callers now
// go through here.
//
// The rollout is inert on existing data: ApplyByTimestamp treats 0 on EITHER
// side as unstamped and applies, so a file written before the column existed,
// and any client that does not stamp, keep exactly their previous
// arrival-order behaviour. It only starts discriminating once a stamped write
// has landed and been persisted.
//
// The POLICY is part of the guard, not of its callers. matchWriteRestore replays
// a trusted snapshot of this same match, so it must never be weighed against the
// stamp of the write it is undoing — that write is by definition newer, and the
// rollback would lose to it every time. Stating that here rather than at each
// call site is what makes the two branches identical, and it matters because the
// two snapshot PRODUCERS differ: bracketMatchAsResult deliberately leaves
// ModifiedAt at 0 (so the bracket bypass was inert either way), while the pool
// snapshot is a straight copy of the stored MatchResult from
// lookupExistingResult and carries a REAL persisted stamp. A gate stated only at
// the pool call site was therefore load-bearing on one branch and decorative on
// the other, which is precisely the asymmetry this primitive exists to end.
func applyMatchWrite(result *state.MatchResult, storedModifiedAt int64, policy matchWritePolicy) bool {
	if policy == matchWriteRestore {
		return true
	}
	if result.CorrectionReason != "" {
		return true
	}
	return domain.ApplyByTimestamp(result.ModifiedAt, storedModifiedAt)
}

// validateBracketCompletion rejects a Completed bracket-family write with no
// winner: an elimination result must never be indeterminate. A tied fixed-
// format encounter resolves via daihyosen; a tied kachinuki final bout
// resolves via encho on that same bout (daihyosen does not exist in
// kachinuki, mp-gmcg). Applies to all bracket match types and is the single
// AMENDMENT 2 choke point. It now has a single caller, applyBracketMatchResult,
// which is itself the one per-match bracket write the twins and the bronze
// fallback all share, so there are no longer twins here to drift.
func validateBracketCompletion(matchID string, status state.MatchStatus, winner string) error {
	if status == state.MatchStatusCompleted && winner == "" {
		return validationErrorf("bracket match %s: cannot mark completed with no winner; resolve the tie first (daihyosen, or encho on the final kachinuki bout)", matchID)
	}
	return nil
}

// applyBracketMatchResult writes result into a single bracket match — a round
// match or the bronze (3rd-place) playoff, which are the same shape and take
// the same rules. It reports whether the write APPLIED: false with a nil error
// means the timestamp guard dropped it as stale, and the caller must then skip
// anything downstream (propagation).
//
// This is the whole per-match write, shared by recordBracketMatchResult, its Tx
// twin, and the bronze fallback in both. It was three copies of ~45 lines; the
// source recorded two hand-resyncs between them ("Twin parity with
// recordBracketMatchResultTx… so the non-tx write path doesn't silently drop
// it", and the AMENDMENT 2 guard), and the bronze copy had to be retrofitted
// with the mp-y3nk LWW guard the round copies already had. A new bracket-write
// rule now has one home.
//
// Note it does NOT touch bm.Court / bm.ScheduledAt: scheduling is owned
// elsewhere, and the Court/ScheduledAt handling at the end is an echo BACK into
// result for the caller's response, not a write.
func applyBracketMatchResult(bm *state.BracketMatch, result *state.MatchResult, policy matchWritePolicy) (applied bool, err error) {
	// A knockout match is playable only once both sides are resolved competitors
	// (feeder pools/matches finished). This replaces the old bracket-wide Preview
	// gate so the knockout fills in incrementally as pools qualify.
	if !bracketMatchPlayable(bm) {
		return false, validationErrorf("knockout match %s is not ready to score: a feeder pool or match has not finished", bm.ID)
	}
	// Match identity is fixed at seeding; a score must not rewrite the pairing.
	// Backfill omitted sides so deriveDaihyosenWinner can map a representative
	// player name back to the canonical team name (it must run before that, and
	// before Winner reaches the bracket); reject a non-empty payload side that
	// disagrees with the resolved pairing.
	//
	// Hoisted out of the condition for the same reason applyPoolWrite hoists it:
	// reconcileSides BACKFILLS as a side effect and only reports the mismatch,
	// so folding it into a short-circuit would let a later tidy (cheap
	// comparison first) silently drop the backfill.
	sidesDisagree := reconcileSides(result, bm.SideA, bm.SideB)
	// FORWARD only, matching the pool twin and the contract stated on
	// writeToPoolOrBracket: the restore replays sides captured from this same
	// match, so a disagreement there is not a client error to reject. This used
	// to abort regardless of policy, so one policy-driven write had two
	// behaviours - a bracket rollback would bail out (rollbackMatchResultTx only
	// LOGS the error) and leave the rejected partial write on disk, where the
	// identical pool case completed the restore.
	if sidesDisagree && policy == matchWriteForward {
		return false, ErrMatchSideMismatch
	}
	// Timestamp last-write-wins (mp-y3nk): drop a write strictly older than the
	// stored result (both stamped in server-relative time), so a reconnecting
	// offline court's stale change loses to a newer one recorded elsewhere.
	// Unstamped writes bypass it (arrival-order, as before), a deliberate
	// correction always applies, and a trusted restore is exempt (all three live
	// in applyMatchWrite); the completed-never-reverted guard stays on top.
	if !applyMatchWrite(result, bm.ModifiedAt, policy) {
		return false, nil
	}
	// Fold the stored daihyosen verdict into the incoming subs BEFORE the winner
	// is derived, validated and assigned, exactly as applyPoolWrite does it
	// before its own overwrite. Ordering matters twice here, and both bites are
	// specific to this branch because the pool twin merges then overwrites the
	// whole struct, while this one assigns bm field by field: a verdict restored
	// after `bm.Winner = result.Winner` could no longer reach bm, and one
	// restored after validateBracketCompletion could not satisfy it, so a
	// completed verdict-silent write that the pool path accepts (deriving the
	// winner from the preserved rep bout) was rejected here.
	// Forward only: see the restore note on bm.SubResults below.
	if policy == matchWriteForward && result.SubResults != nil {
		preserveDaihyosenOutcome(bm.SubResults, result)
	}
	deriveDaihyosenWinner(result)
	// Preserve incoming Status. Pre-fix this was unconditionally Completed, so
	// the scoring modal's "Start" tap (which sends `{status: "running"}`)
	// immediately persisted the bracket match as completed with no winner.
	// Default to Completed when empty, for older payloads that omitted the field.
	status := result.Status
	if status == "" {
		status = state.MatchStatusCompleted
	}
	// Validated BEFORE the first mutation of bm. The round copies used to assign
	// Winner first and lean on updateBracketLocked skipping the save when the
	// mutate callback errors — true (state/bracket.go), but a non-local property
	// nothing at the write site stated. Ordering it here makes the discard local.
	if err := validateBracketCompletion(bm.ID, status, result.Winner); err != nil {
		return false, err
	}
	bm.Winner = result.Winner
	bm.Status = status
	// Stamp the applied write's server-relative time so the next write is
	// compared against it (mp-y3nk). Preserve a prior stamp when this write is
	// unstamped, so an un-stamped correction does not reset the field to 0 and
	// reopen the match to stale writes.
	if result.ModifiedAt != 0 {
		bm.ModifiedAt = result.ModifiedAt
	}
	bm.ScoreA = formatScore(result.IpponsA, result.HansokuA)
	bm.ScoreB = formatScore(result.IpponsB, result.HansokuB)
	bm.Decision = result.Decision
	bm.DecisionBy = result.DecisionBy
	bm.DecisionReason = result.DecisionReason
	bm.Encho = result.Encho
	// Set-if-non-empty on the FORWARD path: a client payload that omits either
	// field is inheriting stored context, not clearing it. Under restore the
	// snapshot is authoritative, so both are assigned straight through — an
	// empty one means the match genuinely held no note, and leaving the
	// rejected write's audit trail behind would misattribute it to a result
	// that was rolled back. applyPoolWrite has always done this via its
	// whole-struct overwrite; the bracket twin used to skip it.
	if policy == matchWriteRestore || result.ResultSource != "" {
		bm.ResultSource = result.ResultSource
	}
	if policy == matchWriteRestore || result.CorrectionReason != "" {
		bm.CorrectionReason = result.CorrectionReason
	}
	// Forward: nil = omitted (preserve stored data), non-nil [] = explicit clear;
	// the verdict merge for this case already ran above, against the still-stored
	// bm.SubResults. Restore: the snapshot IS the truth, so nil means "there were
	// none" and is written through, and the merge is skipped for the same reason
	// applyPoolWrite skips it — it would re-derive a winner onto a snapshot whose
	// captured state had none, i.e. re-apply the write being undone.
	if policy == matchWriteRestore || result.SubResults != nil {
		bm.SubResults = result.SubResults
	}
	// Project the persisted sub-results back into result so the HTTP response and
	// SSE broadcast reflect committed state (mirrors the DecidedByHantei
	// projection below). Without this a nil-preserve re-score would keep the
	// stored bouts on disk but emit an omitted subResults payload in the same turn.
	result.SubResults = bm.SubResults
	// DecidedByHantei uses *bool so a client that omits the field (nil) preserves
	// the stored value, while an explicit true/false applies it. This prevents a
	// re-score that doesn't mention the flag from silently clearing a recorded
	// hantei win. Under restore there is no "omitted": a snapshot that carries no
	// verdict is a match that HAD no verdict, and preserving here would restore
	// the hantei the rollback is undoing.
	//
	// The forward preserve runs through preserveMatchHantei, the same guarded
	// primitive applyPoolWrite uses: an inherited verdict must still be
	// compatible with the incoming decision and rest on a tied scoreline, or a
	// kiken recorded over a stored hantei silently becomes a judges' decision.
	// It resolves result.DecidedByHantei to an explicit value, so the nil branch
	// below is now only reached when there was nothing to carry either way.
	if policy == matchWriteRestore {
		bm.DecidedByHantei = result.DecidedByHantei != nil && *result.DecidedByHantei
	} else {
		preserveMatchHantei(bm.DecidedByHantei, result)
		if result.DecidedByHantei != nil {
			bm.DecidedByHantei = *result.DecidedByHantei
		}
	}
	// Project the persisted flag back so clients (and the bracket HT chip) do not
	// see the match flip non-hantei until the next refresh. HanteiPtr returns nil
	// for false so omitempty still drops the field for non-hantei matches.
	result.DecidedByHantei = state.HanteiPtr(bm.DecidedByHantei)
	// Echo the persisted scheduling fields back into result so the caller (and
	// the SSE broadcast) sees the full match state rather than the empty
	// Court/ScheduledAt the scoring UI sends.
	if result.Court == "" {
		result.Court = bm.Court
	}
	if result.ScheduledAt == "" {
		result.ScheduledAt = bm.ScheduledAt
	}
	return true, nil
}

// applyBracketResultIn is the body BOTH bracket write twins run inside their
// UpdateBracket callback: locate the match, apply the write, propagate a
// completed winner. The twins differ only in which UpdateBracket they call
// (e.store vs tx), so that dispatch is all they contain now — before this they
// were ~105 lines that differed in two.
func (e *Engine) applyBracketResultIn(bracket *state.Bracket, compID, matchID string, result *state.MatchResult, policy matchWritePolicy) error {
	if bracket == nil {
		return notFoundErrorf("bracket not found for competition %s", compID)
	}
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			if bracket.Rounds[rIdx][mIdx].ID != matchID {
				continue
			}
			applied, err := applyBracketMatchResult(&bracket.Rounds[rIdx][mIdx], result, policy)
			if err != nil {
				return err
			}
			// Propagate only a genuinely completed result. A "running" update is
			// for live-status display, so the next round's SideA/SideB must stay
			// empty until the match has a final result; and a write the LWW guard
			// dropped changed nothing to propagate.
			if applied && bracket.Rounds[rIdx][mIdx].Status == state.MatchStatusCompleted {
				e.propagateBracketWinner(bracket, rIdx, mIdx)
			}
			return nil
		}
	}
	// The bronze (3rd-place) playoff lives in Bracket.ThirdPlaceMatch, NOT in
	// Rounds, so the scan above never finds it. There is no propagation out of
	// bronze: it has no downstream match.
	if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
		if _, err := applyBracketMatchResult(bracket.ThirdPlaceMatch, result, policy); err != nil {
			return err
		}
		return nil
	}
	return notFoundErrorf("bracket match %s not found", matchID)
}

func (e *Engine) propagateBracketWinner(bracket *state.Bracket, rIdx, mIdx int) {
	if rIdx >= len(bracket.Rounds)-1 {
		return
	}
	m := bracket.Rounds[rIdx][mIdx]
	nextMatchIdx := mIdx / 2
	nextM := &bracket.Rounds[rIdx+1][nextMatchIdx]

	if mIdx%2 == 0 {
		nextM.SideA = m.Winner
	} else {
		nextM.SideB = m.Winner
	}

	// Feed the loser of a SEMIFINAL into the bronze (3rd-place) playoff match.
	// The semifinal round index is len(Rounds)-2 (the round that feeds the
	// final). This is a pure advancement step: it moves a name only, never
	// computes a score, so it keeps propagateBracketWinner a pure helper.
	// Guarded on ThirdPlaceMatch being present (naginata brackets only).
	if bracket.ThirdPlaceMatch != nil && rIdx == len(bracket.Rounds)-2 {
		loser := ""
		switch m.Winner {
		case m.SideA:
			loser = m.SideB
		case m.SideB:
			loser = m.SideA
		}
		// Skip empty/placeholder losers (bye matches resolve with one side blank).
		if loser != "" && !strings.HasPrefix(loser, "Winner of") {
			bronze := bracket.ThirdPlaceMatch
			// Assign by semifinal POSITION, not first-empty-slot. The round
			// feeding the final always has exactly two matches (mIdx 0 and 1),
			// so each maps to a fixed bronze side. This mirrors the final's
			// positional advancement above and is re-score safe: correcting a
			// semifinal overwrites the correct bronze slot in place. A
			// fill-first-empty scheme would leave a stale loser pinned once
			// both slots are populated and a semifinal is later re-scored.
			if mIdx%2 == 0 {
				bronze.SideA = loser
			} else {
				bronze.SideB = loser
			}
		}
	}

	// Try to resolve the OTHER side if it's a "Winner of" placeholder
	if strings.HasPrefix(nextM.SideA, "Winner of") {
		// nextM.SideA is "Winner of rX-mY"
		r, m := parseWinnerOf(nextM.SideA, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				nextM.SideA = srcM.Winner
			}
		}
	}
	if strings.HasPrefix(nextM.SideB, "Winner of") {
		r, m := parseWinnerOf(nextM.SideB, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				nextM.SideB = srcM.Winner
			}
		}
	}

	// Recursive resolution
	if nextM.SideA != "" && nextM.SideB == "" && !strings.HasPrefix(nextM.SideA, "Winner of") {
		nextM.Winner = nextM.SideA
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	} else if nextM.SideA == "" && nextM.SideB != "" && !strings.HasPrefix(nextM.SideB, "Winner of") {
		nextM.Winner = nextM.SideB
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	} else if nextM.SideA == "" && nextM.SideB == "" {
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	}
}

// parseWinnerOf parses winnerOfFormat's "Winner of rX-mY" (bracket.go) and
// returns (rIdx, mIdx). Depth in the string is 1-based (root is 1). Rounds in
// bracket are 0-indexed (Round 1 is index 0). Depth d corresponds to Round
// (maxDepth - d).
func parseWinnerOf(s string, numRounds int) (int, int) {
	var depth, matchIdx int
	_, err := fmt.Sscanf(s, winnerOfFormat, &depth, &matchIdx)
	if err != nil {
		return -1, -1
	}
	// depth 1 is the final (last round).
	// rounds are 0..numRounds-1.
	// depth d = round index (numRounds - d).
	return numRounds - depth, matchIdx
}

// formatScore renders a side's ippons plus any "(HN)" hansoku suffix for the
// Excel bracket cell. Since PR #110 hansoku is 0 or 1 (outstanding undischarged
// fouls); the discharged pair appears as an "H" ippon in the opponent's slice
// instead of a redundant counter on this side. Values >1 only surface when
// reading legacy disk entries written before the shift.
//
// The package-local spelling of domain.FormatScore, whose inverse
// (domain.ParseScore) bracketMatchAsResult needs to read a stored score back.
func formatScore(ippons []string, hansoku int) string {
	return domain.FormatScore(ippons, hansoku)
}

func (e *Engine) UpdateMatchCourt(compId string, matchId string, newCourt string) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		r.Court = newCourt
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	return e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		m.Court = newCourt
	})
}

// OverrideBracketWinner atomically loads the bracket, locates the
// target match, sets the winner + IsOverridden + Status, propagates
// the winner to subsequent rounds, and saves. Same UpdateBracket
// primitive as recordBracketMatchResult and withBracketMatch, the
// entire find + mutate + propagate + save sequence runs under the
// per-competition lock, so a concurrent bracket score / court / time
// update (also under the same lock via the atomic primitives) can't
// land between our load and save and have its mutation clobbered.
//
// Uses the same UpdateBracket atomic primitive as the rest of the
// scoring path to avoid the LoadBracket + mutate + Save TOCTOU window.
// Returns (applied bool, err error). applied is true when the write landed;
// false when it was dropped by the timestamp LWW guard (stale reconnect
// replay). A false return still carries nil err so the caller can respond
// 200 with {"applied":false} without broadcasting (finding 7).
// Returning errLWWDropped from the mutate callback causes UpdateBracket to
// skip the disk save, avoiding a spurious write of an unchanged bracket (finding 8).
func (e *Engine) OverrideBracketWinner(compId string, matchId string, winnerName string, modifiedAt int64) (bool, error) {
	err := e.store.UpdateBracket(compId, func(bracket *state.Bracket) error {
		if bracket == nil {
			return notFoundErrorf("bracket not found for competition %s", compId)
		}
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				m := &bracket.Rounds[rIdx][mIdx]
				if m.ID == matchId {
					if !bracketMatchPlayable(m) {
						return validationErrorf("knockout match %s is not ready to override: a feeder pool or match has not finished", matchId)
					}
					// Timestamp last-write-wins (mp-y3nk): a reconnecting offline
					// feeder assertion older than a newer stored result is dropped.
					// Return errLWWDropped (not nil) so UpdateBracket skips the save.
					if !domain.ApplyByTimestamp(modifiedAt, m.ModifiedAt) {
						return errLWWDropped
					}
					m.Winner = winnerName
					m.IsOverridden = true
					m.Status = state.MatchStatusCompleted
					// An override is itself the operator's audited, final decision,
					// so it discharges any outstanding reopen debt: a reopened match
					// closed out this way must not keep ReopenPending set (it bypasses
					// applyCorrectionReasonUnderTx/dischargeReopenPendingUnderTx), or
					// the flag lingers until some later score write is forced to
					// invent a reason (mp-gmcg review).
					m.ReopenPending = false
					if modifiedAt != 0 {
						m.ModifiedAt = modifiedAt
					}
					e.propagateBracketWinner(bracket, rIdx, mIdx)
					return nil
				}
			}
		}
		// The bronze (3rd-place) playoff lives outside Rounds; handle it
		// here. Bronze has no downstream match, so no propagation is needed.
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchId {
			bm := bracket.ThirdPlaceMatch
			if !bracketMatchPlayable(bm) {
				return validationErrorf("knockout match %s is not ready to override: a feeder pool or match has not finished", matchId)
			}
			// Same errLWWDropped mechanism as above.
			if !domain.ApplyByTimestamp(modifiedAt, bm.ModifiedAt) {
				return errLWWDropped
			}
			bm.Winner = winnerName
			bm.IsOverridden = true
			bm.Status = state.MatchStatusCompleted
			// Mirror of the round branch: an override discharges the reopen debt.
			bm.ReopenPending = false
			if modifiedAt != 0 {
				bm.ModifiedAt = modifiedAt
			}
			return nil
		}
		return notFoundErrorf("bracket match %s not found", matchId)
	})
	if errors.Is(err, errLWWDropped) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	// Record the override for auditing. A failure here leaves the bracket
	// display correct (it was already saved atomically above); log but
	// don't surface as an error.
	if err := e.store.SaveWinnerOverride(compId, matchId, winnerName); err != nil {
		fmt.Printf("warning: failed to persist winner override audit record for %s: %v\n", matchId, err)
	}

	return true, nil
}

func (e *Engine) UpdateMatchTime(compId string, matchId string, scheduledAt string) error {
	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		r.ScheduledAt = scheduledAt
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	return e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		m.ScheduledAt = scheduledAt
	})
}

// RevertMatchToQueue reverts a running match back to the scheduled (queued)
// state, clearing any partial score so the bout can be restarted correctly.
// It is idempotent for already-scheduled matches (no-op success). Completed
// matches return ErrMatchAlreadyCompleted (HTTP 409); the operator must use
// the score editor to correct a recorded result instead.
//
// Modelled on UpdateMatchCourt: pool-match first, bracket-match fallback,
// using the same atomic withPoolMatch/withBracketMatch primitives so the
// entire load+mutate+save runs under the per-competition lock.
//
// COMPOSED UNDER THE COURT LOCK: RequeueBlockerAndReopenKachinuki calls this
// from INSIDE store.WithCourtExclusivityLock so the blocker requeue and the
// reopen share one lock section (mp-gmcg review A4/R3). This method must
// therefore take ONLY the per-competition lock (via withPoolMatch/
// withBracketMatch) and MUST NOT acquire the court-exclusivity lock — that
// mutex is tournament-global and non-reentrant, so a court check added here
// would deadlock the whole tournament under that composition. It is a
// court-FREEING operation and needs no court gate; if that ever changes, add a
// lock-free `revertMatchToQueueUnderCourtLock` core and call THAT from the
// composition, mirroring reopenKachinukiUnderCourtLock.
func (e *Engine) RevertMatchToQueue(compId, matchId string) error {
	var alreadyCompleted bool

	err := e.withPoolMatch(compId, matchId, func(r *state.MatchResult) {
		if r.Status == state.MatchStatusCompleted {
			alreadyCompleted = true
			return
		}
		// Any non-completed match (running, or an already-scheduled match that
		// still carries stale score/audit metadata from an earlier partial
		// write) is normalised to a CLEAN scheduled match. Idempotent: a
		// pristine scheduled match is left effectively unchanged.
		r.Status = state.MatchStatusScheduled
		r.Winner = ""
		r.WinnerID = ""
		r.IpponsA = nil
		r.IpponsB = nil
		r.HansokuA = 0
		r.HansokuB = 0
		r.Decision = ""
		r.DecisionBy = ""
		r.DecisionReason = ""
		r.Encho = nil
		r.SubResults = nil
		r.FlagsA = 0
		r.FlagsB = 0
		r.DecidedByHantei = nil
		r.ResultSource = ""
		r.CorrectionReason = ""
		// ReopenPending is a match-level verdict field a kachinuki result can
		// carry (reopenPoolMatch sets it), so requeue must clear it too: a
		// reopened-then-requeued match that kept the flag would keep owing an
		// audit reason for a result that no longer exists, and
		// applyCorrectionReasonUnderTx would reject its first honest
		// finalization demanding one (mp-gmcg review). reopenBracketMatch's doc
		// names this exact mirror obligation on RevertMatchToQueue.
		r.ReopenPending = false
		// Rep-bout nominations name who fought a pool/league daihyosen; they are
		// result data for that supplementary bout, so a requeued match must not
		// keep them (bracket matches have no rep fields).
		r.RepPlayerA = ""
		r.RepPlayerB = ""
	})
	if err == nil {
		if alreadyCompleted {
			return ErrMatchAlreadyCompleted
		}
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}

	// Pool match not found; try the elimination bracket. alreadyCompleted is
	// still false here (the pool closure never ran on the errMatchNotFound path).
	if err = e.withBracketMatch(compId, matchId, func(m *state.BracketMatch) {
		if m.Status == state.MatchStatusCompleted {
			alreadyCompleted = true
			return
		}
		// Same contract as the pool path: normalise any non-completed match to
		// a clean scheduled match, clearing stale score/provenance/audit fields
		// even if it was already scheduled.
		m.Status = state.MatchStatusScheduled
		m.Winner = ""
		m.ScoreA = ""
		m.ScoreB = ""
		m.Decision = ""
		m.DecisionBy = ""
		m.DecisionReason = ""
		m.Encho = nil
		m.SubResults = nil
		m.FlagsA = 0
		m.FlagsB = 0
		m.DecidedByHantei = false
		m.IsOverridden = false
		m.ResultSource = ""
		m.CorrectionReason = ""
		// Mirror of the pool branch (mp-gmcg review): clear the reopen-pending
		// audit debt so a reopened-then-requeued bracket match can be replayed
		// and finalized without applyCorrectionReasonUnderTx demanding a reason
		// for a result the requeue already discarded.
		m.ReopenPending = false
		// Revert fence (mp-y3nk): stamp now() so any pre-revert offline write
		// (T_stale < T_revert) is dropped by ApplyByTimestamp on replay.
		// Using 0 would make ApplyByTimestamp always return true (weaker).
		m.ModifiedAt = time.Now().UnixMilli()
	}); err != nil {
		// Neither pool nor bracket holds this match: surface a typed
		// NotFoundError so the handler can answer 404 (a fabricated match id
		// is a client error, not a server fault).
		if errors.Is(err, errMatchNotFound) {
			return notFoundErrorf("match %s not found in competition %s", matchId, compId)
		}
		return err
	}
	if alreadyCompleted {
		return ErrMatchAlreadyCompleted
	}
	return nil
}

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

// errPoolWriteDropped aborts withPoolMatch's mutate when applyPoolWrite has
// decided the incoming write contributes nothing — a last-write-wins supersede
// or an identity mismatch. It never escapes writeToPoolOrBracket, which
// translates it back into that function's (mismatch, err) vocabulary; it exists
// only to tell the store "do not persist this slice", which is the one thing a
// mutate closure otherwise cannot say.
//
// Distinct from errMatchNotFound ON PURPOSE. Both mean "no write happened", but
// not-found means look in the bracket, and routing a dropped POOL write into the
// bracket branch would hunt for a match that is not there.
var errPoolWriteDropped = errors.New("pool write dropped")

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

// ErrMatchSuperseded is returned by a match SCORE write that the timestamp
// last-write-wins guard dropped: a newer result for this match is already
// stored, so the incoming one is a stale reconnect replay and nothing was
// written. It is an OUTCOME, not a fault — the same shape as
// ErrMatchSideMismatch, which is likewise an engine verdict handlers map to a
// specific status rather than a 500.
//
// It exists because the drop used to be reported as (no error, nothing
// written), which every caller up to the HTTP boundary read as success: the
// score handler answered 200 with the operator's own echoed payload AND
// broadcast that discarded result over SSE, so the losing operator's board and
// every viewer showed a result the disk never held until the next refetch
// (bc-lww1). Carrying it as an error makes the drop impossible to swallow by
// omission — an unmapped path fails loudly instead of silently.
//
// Handlers MUST map it to a 2xx, never to a 4xx/5xx: the SPA's offline write
// queue retries 5xx forever (the mp-q8c6 poisoned-queue pattern) and a
// superseded write can never win a retry. Single-match handlers carry
// applied=false through respondSuperseded; the one batch endpoint (bulk-score)
// instead folds it into its per-entry errors[] inside an overall 200, which
// satisfies the 2xx rule but means a supersede is not machine-distinguishable
// from a genuine rejection there.
//
// The OverrideBracketWinner path reports the same condition through its own
// (applied bool, error) return and the package-internal errLWWDropped below;
// that one predates this and already reaches the client as {"applied": false}.
var ErrMatchSuperseded = errors.New("match write superseded: a newer result is already recorded")

// errLWWDropped is a package-internal sentinel returned by the mutate callback
// inside OverrideBracketWinner when a timestamp last-write-wins check drops a
// stale incoming write. Returning this error (rather than nil) causes
// UpdateBracket to skip the disk save, avoiding a spurious write of an
// unchanged bracket (finding 8). The caller converts it to (false, nil) so
// the handler can respond 200 with applied=false without broadcasting (finding 7).
var errLWWDropped = errors.New("lww_dropped")

// storedSides is the identity half of a stored match: who the two competitors
// are, by name and by participant id. Passed as a struct rather than as four
// bare strings because all four are the same assignable type, so a transposed
// pair would compile clean and silently reconcile a name against an id.
type storedSides struct {
	A, B     string
	AID, BID string
}

// reconcileSides folds the stored pairing into a score payload's result.
// An empty payload side is backfilled from the stored side (e.g. a payload
// that omits sides, or a not-yet-resolved bracket slot). A non-empty payload
// side that disagrees with a non-empty stored side is a mismatch, the caller
// must reject rather than overwrite the stored competitor. Returns true on the
// first such disagreement; result is left partially filled but is discarded by
// the caller on mismatch.
//
// The participant IDS get the identical rule, and must: a score write carries
// them since bc-dmsr (the client sends the same triple the server validates a
// hantei mark against), and the whole-struct overwrite in applyPoolWrite /
// applyBracketMatchResult persists whatever arrives. backfillMatchIdentity
// only fills an EMPTY id, so without this an id disagreeing with the stored
// one would be written straight through -- names guarded, ids not, on the same
// record, where the id is the authoritative half (domain.AttributeWinnerSide
// consults it FIRST, precisely because it is the only thing that separates a
// same-name pair). "Match identity is fixed at generation" has to mean both
// halves or it means neither.
func reconcileSides(result *state.MatchResult, stored storedSides) (mismatch bool) {
	if result.SideA == "" {
		result.SideA = stored.A
	} else if stored.A != "" && result.SideA != stored.A {
		mismatch = true
	}
	if result.SideB == "" {
		result.SideB = stored.B
	} else if stored.B != "" && result.SideB != stored.B {
		mismatch = true
	}
	// Backfilling the ids is backfillMatchIdentity's job (it runs after the LWW
	// guard, so an id must not be filled in on a write that is about to be
	// dropped); this only reports the disagreement.
	if result.SideAID != "" && stored.AID != "" && result.SideAID != stored.AID {
		mismatch = true
	}
	if result.SideBID != "" && stored.BID != "" && result.SideBID != stored.BID {
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
//
// mutate may ABORT by returning an error, which skips the save and propagates
// the error here — the same contract withBracketMatch's store primitive has, so
// a write that decides it contributes nothing leaves no footprint on either
// branch. The error is returned VERBATIM (not wrapped in errMatchNotFound), so a
// caller can pass its own sentinel through and recognise it on the way out.
func (e *Engine) withPoolMatch(h state.StoreTx, compId, matchId string, mutate func(*state.MatchResult) error) error {
	found, err := h.UpdatePoolMatchByID(compId, matchId, mutate)
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
// (recordBracketMatchResult / OverrideBracketWinner),
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
// about the daihyosen verdict (DecidedByHantei nil) must not erase a
// recorded one. The verdict travels (flag + winner) onto the incoming
// daihyosen row only when that row is verdict-silent, names no winner of its
// own, carries a decision hantei can coexist with, and is still tied (an
// untied row cannot carry a hantei). An EXPLICIT false and a named winner
// both pass through untouched. The preserveLoserScore precedent, one bout
// deeper.
//
// NIL vs EMPTY on IpponsA/IpponsB is the other half of "verdict-silent":
// scoreline-silence is judged on whether the writer sent an ippon array AT
// ALL (nil, the key absent from the payload), never on whether the array is
// empty of SCORING ippons. The two are not the same thing.
//
// WHO ACTUALLY SENDS NIL. The team editor (web-mobile/js/admin_scoring_team.jsx
// buildPatch) is the one client this engine has to reason about, and it is
// NOT silent by default: it states the daihyosen row's ippons explicitly
// (built from the bout's own point totals, then round-tripped through
// api_serializers.jsx's toBackendMatchResult, which folds the editor's
// decidedByHantei flag into the ippons and never forwards the flag itself
// onto the wire) whenever the operator has touched that row THIS session, or
// the row's stored verdict/score/overtime is already known locally (an
// editor that mounted after a verdict existed re-states what it was shown,
// same behaviour as before). buildPatch omits ipponsA/ipponsB entirely -
// the genuine-silence shape this function relies on - ONLY when BOTH hold:
// the row is untouched (identical to its mount-time seed, including the
// hantei arm/pick and the daihyosen encho counter) AND nothing about it is
// known locally (no recorded verdict, no scored points/fouls/draw, no
// overtime). That is exactly the stale-editor case this preserve exists for
// (an SSE gap or an offline-queue replay landing after a verdict was
// recorded elsewhere): the editor genuinely has nothing to say about the
// row, so it says nothing, and this function restores the stored verdict.
// A deliberate 0-0 withdrawal is the row-is-known-locally branch: it still
// sends an explicit `ipponsA: []` for both sides - a 0-0 scoreline the
// writer DID state, same as a 1-1 withdrawal states "D"/"T" - so it is never
// mistaken for silence.
//
// quick-score (handlers_match.go) is a SEPARATE, already-documented gap, not
// a nil-ippons case: it synthesises positions 1..N only and never emits a
// position -1 row at all, so this function's per-row loop simply never
// matches it (see the SCOPE note below on the delete contract).
//
// Testing scoring-ippon COUNT instead of nil-ness could not tell a silent
// write apart from a 0-0 withdrawal: an empty slice and a nil slice both
// count zero scoring ippons, so a 0-0 withdrawal would read as silence and
// the verdict would be copied right back onto the row the operator just
// cleared. See countScoringIppons below for the (correct, separate) job
// that helper still does: tallying points, not detecting silence.
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
//     reopen inherit the verdict, and a mark with no winner is unattributable
//     to either side — stripInvalidHantei then strips it (drop-never-guess,
//     the same policy an unattributable winner-less verdict gets everywhere
//     else in this file).
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

// stripInvalidHantei enforces the mark's validity on a FORWARD write from the
// paths that do not run ScoreRequest.Validate. The verdict rides in the
// winner's ippon slice, so a re-score replaces it atomically with the
// scoreline it rests on and there is no separate flag to carry. What remains
// is the CONTRADICTION guard preserveMatchHantei used to provide: a mark that
// no longer satisfies its own preconditions (hanteiStillHolds), or that sits
// on a side that is not the winner's, is stripped. The points stay, the
// verdict goes.
//
// WHERE AN INVALID MARK ACTUALLY COMES FROM. Not from the decision twins:
// preserveLoserScore filters the inherited slice through struckIppons, and
// domain.IsScoringIppon drops the mark, so a kiken over a stored hantei
// arrives with the mark ALREADY gone (pinned by
// TestPreserveLoserScoreDropsTheHanteiMark). This comment used to claim that
// path as the reason this function exists; it was wrong, and the test that
// pinned the guard hand-built a payload the decision path cannot produce.
//
// The reachable producer is a handler that copies the STORED match wholesale
// and changes something the verdict depended on. handlers_daihyosen.go's add
// path does exactly that - `u := *match`, then Status flips to running - so a
// stored match-level verdict arrives on a result whose status alone now fails
// hanteiStillHolds. (Its delete path strips the mark itself before writing;
// the add path relies on this guard.)
//
// WHY THIS STRIPS RATHER THAN REJECTS, and must keep doing so: the mark it
// removes is one the write INHERITED, not one the caller sent. Returning a
// validation error here would answer an operator's daihyosen with a 400 for a
// mark already on disk that their request never carried, and no endpoint could
// repair it - the same failure applyHansokuIppons' fold scoping exists to
// avoid, and the same rule: a write answers for what it introduces, not for
// what it inherited. A mark the CLIENT sent is a different matter and is
// rejected, at the boundary that can still tell whose fault it is, by
// mobileapp.validateHanteiMarkPlacement.
//
// The strip is logged. It discards an operator-visible verdict on an otherwise
// successful write, so a silent version leaves a 200 with the verdict gone and
// nothing to investigate.
//
// Forward only. matchWriteRestore replays a trusted snapshot verbatim;
// re-testing it there would let this rewrite the state being restored.
func stripInvalidHantei(result *state.MatchResult) {
	if !result.HanteiDecided() {
		return
	}
	if !hanteiStillHolds(result) {
		result.IpponsA = domain.StripHantei(result.IpponsA)
		result.IpponsB = domain.StripHantei(result.IpponsB)
		logStrippedHantei(result, "its preconditions no longer hold")
		return
	}
	// Winner-side pin: the mark names the competitor the referees chose.
	// Attribution goes through domain.AttributeWinnerSide, the one owner of
	// "which side won" (ids win over names, so a same-name pair - legal:
	// two participants from different dojos may share a name - can't send
	// the mark to the wrong side just because names alone couldn't tell
	// them apart).
	side := domain.AttributeWinnerSide(domain.WinnerAttribution{
		WinnerID: result.WinnerID, SideAID: result.SideAID, SideBID: result.SideBID,
		Winner: result.Winner, SideA: result.SideA, SideB: result.SideB,
	})
	if domain.ContainsHantei(result.IpponsA) && side != domain.MatchSideA {
		result.IpponsA = domain.StripHantei(result.IpponsA)
		logStrippedHantei(result, "it sat on a side that is not the winner's")
	}
	if domain.ContainsHantei(result.IpponsB) && side != domain.MatchSideB {
		result.IpponsB = domain.StripHantei(result.IpponsB)
		logStrippedHantei(result, "it sat on a side that is not the winner's")
	}
}

// logStrippedHantei records a discarded verdict. Every field a reader needs to
// tell an inherited mark from a client-sent one is in the line: which match,
// which precondition failed, and the winner/status/decision the mark was
// judged against.
func logStrippedHantei(result *state.MatchResult, why string) {
	log.Printf("engine: dropped the hantei mark on match %q because %s (winner=%q status=%q decision=%q); the points were kept",
		result.ID, why, result.Winner, result.Status, result.Decision)
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
		if in.Winner != "" || in.DecidedByHantei != nil ||
			domain.ContainsHantei(in.IpponsA) || domain.ContainsHantei(in.IpponsB) {
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
		// A row that supplies NEITHER ippon array said nothing about the
		// SCORELINE either, so the stored one travels with the verdict it
		// rests on. Without this the verdict lands on an all-empty row and the
		// struck ippons, the outstanding fouls, the overtime marker and the
		// sub-decision are all lost: a 1-1 hantei would persist as 0-0, which
		// moves the `Ht` to the other slot (resultSlot fills outside-to-inside)
		// and drops the `(E)`.
		//
		// A row that DOES supply an ippon array — even one that is EMPTY, and
		// even a stale second-device replay whose own scoreline happens to
		// still read tied, e.g. a markless 1-1 offline-queue replay — has
		// spoken for the scoreline itself, and the verdict must not travel
		// onto it: the mark lives IN the copied ippons (see below), so
		// stamping the winner without also copying the mark-carrying
		// scoreline would split the two permanently. The referees' record
		// would be gone from disk while its consequence, the winner, survived
		// — and once this write lands as the new stored row, a future
		// preserve has no mark left to re-attach. ABANDON here, before any
		// field is touched, rather than letting the tie check below decide:
		// that check answers "is this scoreline still tied", not "did the
		// writer supply it", so it cannot distinguish an own tied scoreline
		// from the copied one.
		//
		// This is a NIL check, deliberately not a scoring-ippon COUNT: an
		// explicit `[]` (the team editor's 0-0 daihyosen withdrawal - see the
		// function doc) and an omitted key (a genuinely silent stale
		// snapshot) both count zero scoring ippons, but only the second one
		// is silence. countScoringIppons cannot tell them apart; nil-ness
		// can, because Go's JSON decoder only produces nil for an absent key
		// (SubMatchResult.IpponsA/IpponsB carry no `omitempty`, so a present
		// `[]` always decodes to a non-nil empty slice). A bug fixed here:
		// treating an explicit 0-0 withdrawal as silence resurrected the
		// verdict the operator had just cleared, because the copy branch
		// below would then run and copy the stored Ht mark straight back.
		if in.IpponsA != nil || in.IpponsB != nil {
			return
		}
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
		if !domain.HanteiTiedScoreline(in.IpponsA, in.IpponsB) {
			return // untied now: the verdict cannot stand on this scoreline
		}
		// The verdict itself travelled with the copied scoreline: prior's
		// ippons carry the domain.HanteiMark entry, so there is no flag left
		// to raise — only the winner the mark names. Reached ONLY when the
		// scoreline above was copied wholesale from prior (the early return
		// two blocks up guards it): the mark and the winner it names move as
		// one atomic unit, never separately.
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
// THE HANDLE CONVENTION (bc-twin). h is the store surface this write runs
// against: a live StoreTx when the caller already holds the per-competition
// lock (the HTTP handlers, via WithTransaction), or e.store itself when it
// does not — *state.Store satisfies state.StoreTx by construction, each method
// then taking the lock for its own call. This function used to exist twice, a
// body per handle type, and that twinning is the pattern that produced three
// real bugs (the rollback-policy mutation that survived the suite, the
// preserveDaihyosenOutcome copy that missed the live /score path, and the
// bc-lww1 supersede report that reached one pool twin and not the other), so
// do not reintroduce a per-handle copy: pass the handle. Callers keep their
// own error policy: `mismatch` is RETURNED rather than turned into an error
// here, because the forward writers reject it while the K3 restore
// deliberately ignores it (a snapshot replays sides captured from this same
// match).
func (e *Engine) writeToPoolOrBracket(h state.StoreTx, compId, matchId string, result *state.MatchResult, policy matchWritePolicy) (mismatch bool, err error) {
	var superseded bool
	perr := e.withPoolMatch(h, compId, matchId, func(r *state.MatchResult) error {
		// The POOL branch of the path POST /score and the bulk-score endpoint
		// actually take — the site the hand-copied merge once missed.
		mismatch, superseded = applyPoolWrite(r, result, policy)
		if mismatch || superseded {
			// applyPoolWrite left the stored match untouched, so there is
			// nothing to persist. Aborting here is what makes a dropped POOL
			// write behave like a dropped BRACKET write, which has always
			// skipped its save (UpdateBracket returns early when its callback
			// errors). Without it the identical CSV row was re-serialized and
			// the standings-cache version bumped, so a rejected write left the
			// same footprint on disk as an accepted one — the branch asymmetry
			// this whole primitive exists to remove.
			return errPoolWriteDropped
		}
		return nil
	})
	// A dropped write reached the match and chose not to persist; the verdict is
	// in the flags the closure set, not in the error, so it rejoins the found
	// path here rather than being reported as a store failure.
	if perr == nil || errors.Is(perr, errPoolWriteDropped) {
		if superseded {
			return false, ErrMatchSuperseded
		}
		return mismatch, nil
	}
	if !errors.Is(perr, errMatchNotFound) {
		return false, perr
	}
	// The SAME policy the pool branch would have used.
	return false, e.recordBracketMatchResult(h, compId, matchId, result, policy)
}

// applyPoolWrite folds the stored match into an incoming result and then
// performs the whole-struct overwrite. It reports whether the write was
// ABANDONED, in which case the stored match is left untouched. That happens two
// ways, mirroring applyBracketMatchResult: the payload names a pairing that is
// not this match's (a client error the caller maps to 409), or the timestamp
// guard drops it as stale (a newer result simply stands, nobody is at fault).
// They are reported on SEPARATE returns because they are different verdicts
// about different actors, and both have to reach the operator: a drop reported
// as plain success is what bc-lww1 was.
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
func applyPoolWrite(stored, result *state.MatchResult, policy matchWritePolicy) (mismatch, superseded bool) {
	// reconcileSides BACKFILLS omitted sides as a side effect and only reports
	// the mismatch, so it must run under both policies; hoisted out of the
	// condition below because folding it into a short-circuit would let a later
	// tidy (cheap comparison first) silently drop the backfill.
	sidesDisagree := reconcileSides(result, storedSides{A: stored.SideA, B: stored.SideB, AID: stored.SideAID, BID: stored.SideBID})
	// Match identity is fixed at generation; a score must not rewrite it. The
	// restore policy replays sides captured from this same match, so a mismatch
	// there is not a client error.
	if sidesDisagree && policy == matchWriteForward {
		return true, false
	}
	// Timestamp last-write-wins, the SAME guard, the same primitive and now the
	// same call shape the bracket branch uses: a reconnecting offline court's
	// stale change loses to a newer result recorded elsewhere. The restore
	// exemption lives inside applyMatchWrite, which is load-bearing here — unlike
	// the bracket's, this branch's rollback snapshot carries a real stamp.
	if !applyMatchWrite(result, stored.ModifiedAt, policy) {
		return false, true
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
		// The MATCH-level verdict travels inside the ippons the writer sent
		// (or inherited through preserveLoserScore on the decision paths);
		// what needs enforcing here is only that an inherited mark cannot
		// contradict the incoming result. See stripInvalidHantei.
		stripInvalidHantei(result)
		// A pool/league team encounter can hold a daihyosen (findMatchForDaihyosen
		// accepts pool matches). Without this a verdict-silent write from a stale
		// second editor erases a recorded hantei, which in accrueTeamSubResults
		// flips the bout from a win into a draw and so moves the team's IV/IL/IT
		// tie-break figures.
		preserveDaihyosenOutcome(stored.SubResults, result)
	}
	// The unparsed sub-bout cell is FILE PROVENANCE, not client data: it exists
	// so a malformed hand edit survives until someone repairs it, and the
	// whole-struct overwrite below would drop it on any write to this match --
	// including one that says nothing about sub-bouts at all, such as a court
	// reassignment. Assigned unconditionally rather than set-if-empty, under
	// BOTH policies, so a client that echoes the derived flag back cannot
	// raise it on a match that reads perfectly well.
	//
	// A write that CARRIES sub-bouts clears both: the operator has re-entered
	// the encounter, so the retained bytes are superseded and the warning has
	// been answered. That is the repair path, and it is the only thing that
	// takes the flag down.
	if len(result.SubResults) == 0 {
		result.SubResultsRaw = stored.SubResultsRaw
		result.SubResultsUnreadable = stored.SubResultsUnreadable
	} else {
		result.SubResultsRaw = ""
		result.SubResultsUnreadable = false
	}
	*stored = *result
	return false, false
}

// applyHansokuIppons auto-awards ippons from accumulated hansoku counts per
// FIK Article 20: every 2 hansoku on one side grants 1 ippon to the opponent.
// Compares the count of 'H' entries a side already carries against what its
// opponent's hansoku count implies and awards ONLY the shortfall, via
// domain.AppendIppon (the derived H is a struck point, so it takes the next
// free slot before growing the slice, and a wire-legal row holding a "•"
// placeholder stays wire-legal after the award). A row carrying at least the
// implied count is left entirely alone, positions included — which is also
// why re-folding an already-folded row reports no change. See applyOneSide
// for why a surplus H is the operator's own strike and must never be
// removed.
//
// The fold and the post-fold guard used to be two steps - applyHansokuIppons
// returning a hansokuFold recording which slices it actually REWROTE, and a
// separate checkHansokuHanteiConflict taking that fold - with all three call
// sites required to chain them by hand. That is now inside this one function,
// so a new caller cannot fold without checking: there is no fold value left
// to forget to check.
//
// The check answers for what THIS fold rewrote, not for whatever was already
// on disk: a row whose award was already folded in on a previous write is
// not touched at all and counts as UNCHANGED. That scoping matters because
// the decision twins inherit the encounter's stored sub-bouts wholesale
// (preserveLoserScore) before the fold runs, so without it an operator's
// kiken would be rejected for an over-cap row that a much earlier write
// persisted and that the kiken payload never carried, with no endpoint able
// to repair it. Same reasoning as applyKachinukiMerge running AFTER the
// check.
//
// It rejects two things the auto-award can do to a scoreline that the wire
// validator, which only ever sees the payload as the client sent it, cannot
// have checked:
//
//   - The award UNTIES a scoreline a hantei mark is still riding on. A
//     payload can arrive tied (e.g. ipponsA:[M,Ht], ipponsB:[K],
//     hansokuB:2) and pass ScoreRequest.Validate's tied-scoreline
//     precondition, then the fold appends an "H" to ipponsA and the RESULT
//     is untied while the mark still stands. Falling through silently hands
//     that row to stripInvalidHantei, which discards the mark with no error
//     reaching the caller, so the operator's hansoku award would quietly
//     overrule a verdict the same payload also carried.
//   - The award pushes a side past the best-of-3 cap. A row with both slots
//     already holding entries plus a folded "H" exceeds it (a free "•"
//     placeholder is filled, not grown past), and nothing downstream rejects it, so
//     the write lands on disk and every subsequent echo save of that match
//     400s at mobileapp.validateIppons - wedging the editor on a row that was
//     never rejected at write time.
//
// Both checks apply at MATCH level and to every SubResults row, because the
// fold applies to both (a daihyosen representative bout carries its own
// hansoku counts and its own mark) and neither stripInvalidHantei nor
// preserveSubHantei runs on the sub level. The returned *ValidationError
// surfaces the same 400 the client would have gotten had it pre-computed the
// award itself and sent the post-award scoreline directly.
//
// A payload that ALREADY carries an over-cap or untied-marked row (pre-folded
// by the caller) is damage the wire validator can see for itself, and
// mobileapp.validateIppons rejects it at both levels before the engine is
// reached; what only the engine can see is the state AFTER the award, which
// is why this check lives here and covers exactly that.
//
// A no-op when the fold changed nothing, and when a changed row carries
// neither a mark nor an overflow: this must never change behaviour for the
// overwhelming majority of hansoku awards, which carry neither.
func applyHansokuIppons(result *state.MatchResult) error {
	if result == nil {
		return nil
	}
	applyOneSide := func(hansoku int, ippons *[]string) bool {
		// hansoku/2 derives the ippons a CUMULATIVE foul count implies — the
		// convention of clients that only count fouls upward and never strike
		// the "H" themselves (reconcileFoulsAtOpen in the JS editors is this
		// same derivation, run at editor-open over legacy stored counts).
		// Today's editors keep the OUTSTANDING count instead — the strike
		// resets the counter to 0 and puts the H in the opponent's slots
		// (applyFoulIncrement) — so for their payloads expected is 0 and
		// every H the row carries is the operator's own strike.
		expected := hansoku / 2
		existing := 0
		for _, v := range *ippons {
			if v == "H" {
				existing++
			}
		}
		// Award only the SHORTFALL, and never remove a surplus H: under the
		// outstanding convention every editor payload carries "more H than
		// the count explains", so a removal branch (tried once, as a
		// "hansoku reduction" feature) silently deleted the struck point on
		// every save — the editor's screen kept showing it while standings,
		// viewers and the export lost it. This restores the fold's original
		// contract: never duplicate existing H entries, never remove them.
		// An operator undoing a mistaken award edits the slots directly, and
		// the row they send IS the row that persists.
		if existing >= expected {
			return false
		}
		out := append([]string{}, *ippons...)
		for ; existing < expected; existing++ {
			// A derived hansoku ippon is a struck point, so it takes the
			// next free slot before growing the slice (domain.AppendIppon,
			// the same rule the editors and AppendHantei follow). Filling
			// rather than appending keeps a legal two-slot row with a "•"
			// placeholder legal after the award; a blind append pushed it
			// to three entries, past validateIppons' structural cap, so
			// the row 400'd here (or, before checkFoldedRow existed,
			// wedged on disk and 400'd every echo save).
			out = domain.AppendIppon(out, "H")
		}
		*ippons = out
		return true
	}
	// HansokuA awards the OPPONENT (side B) an ippon, and vice versa, so each
	// count's "changed" bool belongs to the slice it rewrote.
	matchB := applyOneSide(result.HansokuA, &result.IpponsB)
	matchA := applyOneSide(result.HansokuB, &result.IpponsA)
	if err := checkFoldedRow("", matchA, matchB, result.HanteiDecided(), result.IpponsA, result.IpponsB); err != nil {
		return err
	}
	for i := range result.SubResults {
		sr := &result.SubResults[i]
		subB := applyOneSide(sr.HansokuA, &sr.IpponsB)
		subA := applyOneSide(sr.HansokuB, &sr.IpponsA)
		if err := checkFoldedRow(fmt.Sprintf("subResults[%d]: ", i), subA, subB, sr.HanteiDecided(), sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
	}
	return nil
}

// checkFoldedRow applies both post-fold checks to ONE scoring row (the match
// itself, or one sub-bout), skipping whatever the fold left alone. Shared by
// the two levels so they cannot drift: the match level used to carry the
// hantei check only, which let the identical fold wedge an over-cap match-level
// slice on disk while the sub level rejected it.
//
// The hantei check runs first: a row that trips both (the illustrative case -
// a mark riding on a scoreline the fold both unties AND pushes over the cap)
// surfaces the more actionable message naming the verdict conflict, rather
// than a bare count error that leaves the operator to work out why.
func checkFoldedRow(prefix string, changedA, changedB, hanteiDecided bool, ipponsA, ipponsB []string) error {
	if (changedA || changedB) && hanteiDecided && !domain.HanteiTiedScoreline(ipponsA, ipponsB) {
		// The tie test spans BOTH sides, so a change to either side can be
		// what untied it.
		return validationErrorf("%shantei requires a tied scoreline after hansoku ippon award", prefix)
	}
	if (changedA && len(ipponsA) > domain.MaxIpponsPerSide) || (changedB && len(ipponsB) > domain.MaxIpponsPerSide) {
		return validationErrorf("%sat most %d ippons per side (best-of-3) after hansoku ippon award, got %d/%d", prefix, domain.MaxIpponsPerSide, len(ipponsA), len(ipponsB))
	}
	return nil
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
// match at generation, and resolves the winner id. It runs inside the pool
// score-write closure right before applyPoolWrite's whole-struct
// `*stored = *result` overwrite:
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
// empty entries, the "•" UI placeholder, the domain.DefaultWinIppon maru
// (an awarded default win, not a struck point) and the domain.HanteiMark
// (a verdict, not a strike — and one that cannot survive onto a withdrawal
// anyway; stripInvalidHantei would remove it downstream, this just keeps the
// preserved slice honest at the source). Distinct from countScoringIppons,
// which counts the maru as a scoring ippon by design.
//
// Composed from domain.IsScoringIppon (which already drops "", the
// placeholder and HanteiMark) plus the one deliberate extra exclusion, the
// maru: this keeps struckIppons in lockstep with the domain definition by
// construction, so a future non-scoring token added there (as HanteiMark
// itself once was) does not also need remembering here.
func struckIppons(ippons []string) []string {
	var out []string
	for _, v := range ippons {
		if domain.IsScoringIppon(v) && v != domain.DefaultWinIppon {
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

// RecordMatchResult is the plain score entry point (the quick-score
// PUT /result handler). It wraps the write in ONE transaction, for the same
// two reasons the main score handler wraps its own call in WithTransaction:
// the match write and the kiken/fusenpai eligibility side-effect commit under
// a single per-competition lock acquire, and recordIneligibilityFromDecision's
// K2 check-and-set is only atomic when its handle is a live tx (see the K2
// note on that function). Before bc-twin this entry wrote direct-to-store and
// leaned on an inner WithTransaction inside the ineligibility helper for K2 —
// same guarantee, now held once at the entry instead of re-acquired mid-write.
//
// NEVER call this from inside a transaction. It acquires the per-competition
// lock itself and that lock is NOT reentrant, so calling it from within a
// WithTransaction closure deadlocks the goroutine outright — no error, no
// panic, just a stuck request holding the lock every other write on that
// competition needs. Use the *Tx variant and pass the handle you already hold.
// Both entry points are on the ScoringEngine interface (mobileapp/deps.go), so
// a handler can reach them from inside its own transaction: registerScoreHandler
// already has exactly that shape. Nothing catches this at compile time and a
// test only shows it as a hang, so the rule lives here.
func (e *Engine) RecordMatchResult(compId string, matchId string, result *state.MatchResult) error {
	result.ID = matchId // normalize ID-less payloads before overwriting
	if err := applyHansokuIppons(result); err != nil {
		return err
	}
	return e.store.WithTransaction(compId, func(tx state.StoreTx) error {
		return e.writeMatchResult(tx, compId, matchId, result, matchWriteForward)
	})
}

// writeMatchResult persists the result without applying hansoku auto-award.
// RecordMatchResult calls it after applyHansokuIppons with matchWriteForward;
// the K3 rollback (rollbackMatchResultTx) replays a prior snapshot through it
// with matchWriteRestore. The policy reaches whichever branch the match id
// resolves to — see matchWritePolicy for what each inherits. h must be a live
// tx: the ineligibility side-effect's K2 atomicity is the handle's.
//
// Restore note: matchWriteRestore can never produce sideMismatch (the identity
// check in applyPoolWrite / applyBracketMatchResult is forward-only), so the
// ErrMatchSideMismatch return below is unreachable on the rollback path — the
// snapshot replays sides captured from this same match.
func (e *Engine) writeMatchResult(h state.StoreTx, compId string, matchId string, result *state.MatchResult, policy matchWritePolicy) error {
	sideMismatch, err := e.writeToPoolOrBracket(h, compId, matchId, result, policy)
	if err != nil {
		return err
	}
	if sideMismatch {
		return ErrMatchSideMismatch
	}
	// Side-effect writes are non-fatal: the match score is already staged,
	// so propagating would cause a 500 retry that double-records the score.
	if _, err := e.recordIneligibilityFromDecision(h, compId, matchId, result); err != nil {
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
	}
	return nil
}

// RecordMatchResultWithIneligibility is the variant used by callers that need
// the CompetitorStatus side-effect surfaced (for the
// `competitor-status-updated` SSE broadcast) after a kiken/fusenpai is
// recorded. It returns the new CompetitorStatus (or nil when none was written)
// alongside any error.
//
// Since bc-twin it is a WithTransaction shim over
// RecordMatchResultWithIneligibilityTx — ONE body, whichever door a caller
// enters by. Two things changed for this entry when its hand-copied body was
// deleted, both deliberate: the whole write now commits under a single
// per-comp lock acquire (previously each store call locked separately), and
// the mp-e2k1 displaced-finisher guard now applies here too — it had been
// added to the Tx twin only, which is exactly the drift this collapse exists
// to end. Side-effect write failures are still non-fatal: (nil, nil) + log.
//
// NEVER call this from inside a transaction: it takes the per-competition lock
// itself and that lock is not reentrant, so it deadlocks rather than erroring.
// See the note on RecordMatchResult, which carries the full reasoning.
//
// T085/T092, bc-twin.
func (e *Engine) RecordMatchResultWithIneligibility(compId string, matchId string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	var status *domain.CompetitorStatus
	var engErr error
	txErr := e.store.WithTransaction(compId, func(tx state.StoreTx) error {
		status, engErr = e.RecordMatchResultWithIneligibilityTx(tx, compId, matchId, result)
		// Return nil regardless: engErr is an application-level signal
		// (AlreadyIneligible → 409, validation → 400) surfaced after the tx,
		// and the K3 rollback has already replayed the prior state INSIDE the
		// tx, so committing persists exactly what the engine settled on. This
		// is the same commit contract the score handler uses around the Tx
		// variant (handlers_match.go).
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return status, engErr
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
		// Indexed by IDENTITY, not bare name: two competitors sharing a name
		// from different dojos are explicitly allowed, and a league is a
		// single pool holding every competitor, so a bare-name key here
		// silently collapses namesakes into one standings row.
		// (helper.Player is a type alias for domain.Player, NFR-007, so the
		// pool player flows straight into PlayerStanding.)
		playerStandings, order := newStandingsIndex(p.Players)

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Tiebreaker and pool-daihyosen matches don't count toward regular pool stats.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			sA := lookupStandingsPlayer(playerStandings, m.SideAID, m.SideA)
			sB := lookupStandingsPlayer(playerStandings, m.SideBID, m.SideB)
			if sA == nil || sB == nil {
				continue
			}

			// Winner by id where recorded, else by name; see resolveWinnerSide.
			winnerIsA, winnerIsB := resolveWinnerSide(m)
			switch {
			case winnerIsA:
				sA.Wins++
				sB.Losses++
			case winnerIsB:
				sB.Wins++
				sA.Losses++
			case state.IsDraw(m.Decision) || m.Winner == "":
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
		for _, s := range order {
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

		// Apply manual rank overrides. Overrides are keyed by competitor
		// IDENTITY (helper.CompetitorKey: id-preferred, name+dojo fallback),
		// not bare name (bc-cse) -- lookupPoolRankOverride also honours a
		// legacy bare-name key for an overrides.json written before this fix,
		// see its doc comment for the read-only compatibility decision.
		overrides, _ := e.store.LoadOverrides(compId)
		var poolOverrides map[string]int
		if overrides != nil {
			poolOverrides = overrides.PoolRanks[p.PoolName]
		}
		if len(poolOverrides) > 0 {
			sort.Slice(sorted, func(i, j int) bool {
				rankI, okI := lookupPoolRankOverride(poolOverrides, sorted[i].Player.ID, sorted[i].Player.Name, sorted[i].Player.Dojo)
				rankJ, okJ := lookupPoolRankOverride(poolOverrides, sorted[j].Player.ID, sorted[j].Player.Name, sorted[j].Player.Dojo)
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

		poolHasOverrides := len(poolOverrides) > 0
		for i := range sorted {
			sorted[i].Rank = i + 1
			if poolHasOverrides {
				if _, ok := lookupPoolRankOverride(poolOverrides, sorted[i].Player.ID, sorted[i].Player.Name, sorted[i].Player.Dojo); ok {
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
func (e *Engine) recordBracketMatchResult(h state.StoreTx, compId string, matchId string, result *state.MatchResult, policy matchWritePolicy) error {
	return h.UpdateBracket(compId, func(bracket *state.Bracket) error {
		return e.applyBracketResultIn(bracket, compId, matchId, result, policy)
	})
}

// applyMatchWrite reports whether a match write should apply under the
// timestamp last-write-wins guard (mp-y3nk). It is pure timestamp LWW, for
// every write including a deliberate operator CORRECTION.
//
// A correction used to bypass the guard outright, on the stated grounds that it
// "is an explicit decision made under the handler's correction-audit lock, not a
// reconnect replay". The first half is true and the second is not: the SPA
// queues a completed score as a terminal write and replays it for up to the
// 12h queue TTL, so a correction composed offline against what the operator saw
// at T0 was replayed hours later and overwrote a result recorded at T0+3h that
// they never saw — silently, with a normal 200, because a bypass reports
// nothing. That is the mirror of the bug this guard exists to prevent: bc-lww1
// stopped a dropped write from claiming success, and this stops a stale write
// from succeeding.
//
// Removing the bypass preserves every case it was protecting, because LWW
// already answers them:
//
//   - a live correction over an OLDER stored result still applies, which is the
//     whole of the original intent;
//   - a live correction over a NEWER stored result is now dropped, and that is
//     correct rather than a regression: the operator is correcting a view that
//     has already moved on, which is the one situation where their "correction"
//     is the stale party;
//   - a replayed offline correction loses on its own old stamp.
//
// What made this safe to change is bc-lww1 itself. Before it, a correction that
// lost the guard vanished with a success response, so exempting corrections was
// the only way to guarantee an operator's deliberate edit was not silently
// eaten. Now a dropped correction comes back as applied:false with "check what
// is recorded", and the operator re-enters it with a fresh stamp and wins. The
// exemption was buying safety the reporting now provides properly.
//
// The residual risk is a client whose clock runs behind losing a legitimately
// fresh correction. That is not specific to corrections — it is true of every
// stamped write already — and it now fails loudly instead of silently.
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
	// The unstamped bypass, made visible (bc-cse). An unstamped forward write
	// over a STAMPED stored result is the one remaining path that overwrites a
	// known-newer result without any comparison being possible: ApplyByTimestamp
	// reads 0 as "no opinion" and applies. The bypass STAYS — legacy clients and
	// files written before the ModifiedAt column existed depend on it, and
	// removing it would refuse writes that have always been legitimate — but it
	// must stop being invisible, because it is now the last silent-overwrite
	// path left (a client stamp far enough in the future to reach it by accident
	// is refused at the HTTP boundary instead, see modifiedAtRefuseSkewMs).
	//
	// Expected traffic, not an alarm: the server-built writes (quick-score,
	// /decision, both daihyosen paths) carry no stamp BY DESIGN, so every
	// correction made through them logs here. The line earns its keep when an
	// operator asks where a result went: it names the match whose stamped result
	// an unstamped write replaced.
	//
	// RUNNING writes are excluded, and that is a volume decision with a
	// correctness argument behind it. A legacy SPA build (no modifiedAt)
	// autosaving on the ~300ms debounce reaches this primitive once per keystroke
	// for the whole bout, which would bury the terminal line that actually
	// answers the question in hundreds of intermediate ones — precisely when
	// someone is reading these logs. Nothing diagnostic is lost, because the
	// stored stamp SURVIVES an unstamped write (applyPoolWrite copies it back
	// onto the result before the overwrite, and the bracket twin does the same),
	// so storedModifiedAt is still > 0 when that same client finally writes the
	// completed result — and THAT write logs, naming the same match and the same
	// stamp it displaced. The excluded lines are duplicates of the one kept, not
	// coverage.
	//
	// Competition and match ids are not in scope here (this primitive is handed
	// only the result and the stored stamp), so it logs what the result carries:
	// its own ID.
	if result.ModifiedAt == 0 && storedModifiedAt > 0 && result.Status != state.MatchStatusRunning {
		log.Printf("engine: match %s: unstamped write overwrites a result stamped %d (unstamped bypass, no last-write-wins comparison possible)",
			result.ID, storedModifiedAt)
	}
	return domain.ApplyByTimestamp(result.ModifiedAt, storedModifiedAt)
}

// validateBracketCompletion rejects a Completed bracket-family write with no
// winner: an elimination result must never be indeterminate. A tied fixed-
// format encounter resolves via daihyosen; a tied kachinuki final bout
// resolves via encho on that same bout (daihyosen does not exist in
// kachinuki, mp-gmcg). Applies to all bracket match types and is the single
// AMENDMENT 2 choke point. It now has a single caller, applyBracketMatchResult,
// which is itself the one per-match bracket write that both the round path and
// the bronze fallback share, so there is nothing left here to drift.
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
// This is the whole per-match write, shared by recordBracketMatchResult and the
// bronze fallback. It was three copies of ~45 lines; the
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
	// No ids: a BracketMatch persists names only, so the id half of the guard
	// has nothing to compare against here and correctly stays silent (an
	// empty stored id means "unknown", never "mismatch").
	sidesDisagree := reconcileSides(result, storedSides{A: bm.SideA, B: bm.SideB})
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
	// compared against it (mp-y3nk). On the FORWARD path, preserve a prior stamp
	// when this write is unstamped, so an un-stamped correction does not reset
	// the field to 0 and reopen the match to stale writes.
	//
	// Under RESTORE the snapshot is authoritative, stamp included, so an
	// unstamped snapshot must put the match BACK to unstamped. The snapshot is
	// taken BEFORE the forward write, so "unstamped" there means simply "this
	// match had never been written before" — i.e. every match's first score.
	// Skipping the assignment left a rolled-back first write carrying a stamp
	// the match never earned, and a later write stamped earlier (a queued
	// offline result) was then refused with "a newer result is already
	// recorded" when nothing newer was ever recorded. The pool branch already
	// behaves this way via applyPoolWrite's whole-struct overwrite; this closes
	// the remaining half of that branch asymmetry.
	if policy == matchWriteRestore || result.ModifiedAt != 0 {
		bm.ModifiedAt = result.ModifiedAt
	}
	// The verdict rides in the ippons about to be rendered; forward writes
	// from non-validated paths must not persist a mark that contradicts the
	// result (see stripInvalidHantei). Restore replays the snapshot verbatim.
	if policy == matchWriteForward {
		stripInvalidHantei(result)
	}
	// Defensive copies: bm must not alias result's slices, mirroring the
	// pattern used everywhere else a MatchResult's ippons are projected
	// onto stored state (see copyMatchResults / copyBracket).
	bm.IpponsA = append([]string(nil), result.IpponsA...)
	bm.IpponsB = append([]string(nil), result.IpponsB...)
	bm.HansokuA = result.HansokuA
	bm.HansokuB = result.HansokuB
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

// applyBracketResultIn is the body the bracket write runs inside its
// UpdateBracket callback: locate the match, apply the write, propagate a
// completed winner. It has ONE caller, recordBracketMatchResult, which passes
// its store handle straight through — before bc-twin this was two ~105-line
// twins differing only in whether they called e.store or tx, and the handle
// parameter is exactly what let them become one body.
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
			// A nil error with applied=false is the timestamp guard's drop and
			// nothing else (the other two false returns carry an error). Report
			// it: the drop reaching the handler as success was bc-lww1. Returning
			// an error here also makes UpdateBracket skip the disk save, which is
			// right — nothing changed — and is the same reason OverrideBracketWinner
			// returns errLWWDropped from its own mutate callback.
			if !applied {
				return ErrMatchSuperseded
			}
			// Propagate only a genuinely completed result. A "running" update is
			// for live-status display, so the next round's SideA/SideB must stay
			// empty until the match has a final result.
			if bracket.Rounds[rIdx][mIdx].Status == state.MatchStatusCompleted {
				e.propagateBracketWinner(bracket, rIdx, mIdx)
			}
			return nil
		}
	}
	// The bronze (3rd-place) playoff lives in Bracket.ThirdPlaceMatch, NOT in
	// Rounds, so the scan above never finds it. There is no propagation out of
	// bronze: it has no downstream match.
	if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
		applied, err := applyBracketMatchResult(bracket.ThirdPlaceMatch, result, policy)
		if err != nil {
			return err
		}
		// Same report as the round branch. Bronze used to DISCARD `applied`
		// outright, so a superseded bronze write was doubly invisible: no signal
		// to the operator and a pointless bracket re-save.
		if !applied {
			return ErrMatchSuperseded
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

func (e *Engine) UpdateMatchCourt(compId string, matchId string, newCourt string) error {
	err := e.withPoolMatch(e.store, compId, matchId, func(r *state.MatchResult) error {
		r.Court = newCourt
		return nil
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
	err := e.withPoolMatch(e.store, compId, matchId, func(r *state.MatchResult) error {
		r.ScheduledAt = scheduledAt
		return nil
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

	err := e.withPoolMatch(e.store, compId, matchId, func(r *state.MatchResult) error {
		if r.Status == state.MatchStatusCompleted {
			// Reported through the captured flag, not an abort: the caller
			// turns it into its own error AFTER the store call, and aborting
			// here would change that error's identity.
			alreadyCompleted = true
			return nil
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
		return nil
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
		m.IpponsA = nil
		m.IpponsB = nil
		m.HansokuA = 0
		m.HansokuB = 0
		m.Decision = ""
		m.DecisionBy = ""
		m.DecisionReason = ""
		m.Encho = nil
		m.SubResults = nil
		m.FlagsA = 0
		m.FlagsB = 0
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

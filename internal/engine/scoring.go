package engine

import (
	"errors"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// ErrMatchSuperseded is returned by a match write every change of which the
// merge held (mergeMatchWrite, bc-mrgc): a newer change to each group it
// changes is already stored, so nothing of it was written to the match; its
// values are kept in the match's history, and the write paths return it as
// *SupersededError naming the held groups. It is an OUTCOME, not a fault — the same shape as
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
// from a genuine rejection there (its `reason` and `heldGroups` say it is).
var ErrMatchSuperseded = errors.New("match write superseded: a newer result is already recorded")

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
	// A side whose participant id matches the stored one is the same
	// competitor whatever name the payload carries: a write queued before a
	// participant rename carries the old name and must not be refused for it
	// (bc-mrgc phase 3, "Nothing should be dropped"). It is accepted and the
	// stored, current name is kept, on the side and wherever the write names
	// that side by the old name. Only a genuine identity disagreement (an id
	// mismatch, or a name mismatch no id can settle) is refused.
	adoptCurrentSideName(result, &result.SideA, result.SideAID, stored.A, stored.AID)
	adoptCurrentSideName(result, &result.SideB, result.SideBID, stored.B, stored.BID)
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

// adoptCurrentSideName replaces a payload side name that differs from the
// stored one when the side's participant id says it is the same competitor
// (a rename since the write was made), and every place the write names that
// side by the old name: the match winner (when its id, if any, is this side's)
// and every bout row. The representative-bout row names the teams
// themselves, and a numbered row can too: a fixed-order row that names no
// fighter records the team as its winner, and an id-less kachinuki row
// likewise. Left on the old name, such a row is credited to nobody
// (state.SubBoutWinnerSide reads the match's current side names), so the
// encounter's IV and winner would move (bc-mrgc review F1).
func adoptCurrentSideName(result *state.MatchResult, side *string, sideID, storedName, storedID string) {
	old := *side
	if old == "" || storedName == "" || old == storedName || sideID == "" || sideID != storedID {
		return
	}
	*side = storedName
	if result.Winner == old && (result.WinnerID == "" || result.WinnerID == storedID) {
		result.Winner = storedName
	}
	for i := range result.SubResults {
		sub := &result.SubResults[i]
		if sub.SideA == old {
			sub.SideA = storedName
		}
		if sub.SideB == old {
			sub.SideB = storedName
		}
		if sub.Winner == old {
			sub.Winner = storedName
		}
	}
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
// Delegates to h.UpdateBracketMatchByID (mp-gmcg review R5) on the given
// state.StoreTx handle — the bare store, which holds the per-competition lock
// itself, or a transaction already holding it — across load → walk-rounds →
// bronze-sibling → mutate → save, and writes ONLY when the match is found —
// the exact walk this used to hand-roll, and the sibling of
// UpdatePoolMatchByID. findBracketMatchByID searches Rounds FIRST then the
// ThirdPlaceMatch sibling, so "m-bronze" resolves here too.
//
// NOTE: no playability gate here. withBracketMatch backs the SCHEDULING mutators
// (UpdateMatchCourt / UpdateMatchTime) and RevertMatchToQueue, which must work
// on not-yet-resolved (placeholder) knockout matches so operators can pre-arrange
// courts/times. The per-match playability gate lives only in the SCORING paths
// (recordBracketMatchResult),
// which mutate via UpdateBracket directly.
func (e *Engine) withBracketMatch(h state.StoreTx, compId, matchId string, mutate func(*state.BracketMatch)) error {
	found, err := h.UpdateBracketMatchByID(compId, matchId, mutate)
	if err != nil {
		return err
	}
	if !found {
		return errMatchNotFound
	}
	return nil
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

// ForceOptions is the optional trailing argument accepted by
// RecordMatchResultWithIneligibility(Tx) and other forced-write paths (bc-kcdg).
// It is variadic (`opts ...ForceOptions`) rather than a plain parameter
// SPECIFICALLY so the dozens of existing call sites across this codebase
// (and its tests) that predate the downstream-knockout-correction guard keep
// compiling unchanged: omitting it is exactly the pre-bc-kcdg behaviour
// (force=false, no reopen list wanted). Only callers that need to
// offer an operator confirmation (such as the score-write HTTP
// handler) pass one.
// ReopenedMatch identifies a match a forced correction reopened: its id, for
// broadcasts and for anything addressing the match, and its MATCH NUMBER, which
// is what the operator is shown.
//
// The two travel together rather than as parallel slices because only the
// number is meaningful to a person: "m-r2-0" is an internal id that appears
// nowhere on the operator's screens, while "Match 3" is the label on the score
// sheet, the bracket and the Excel tree. Number is 0 for a match that never got
// one (a bye placeholder, or a bracket saved before numbering existed), and a
// caller showing it must fall back rather than print "Match 0".
//
// DisplayRound is the match's effective round counted from the final
// (state.BracketMatch.DisplayRound: 1 = Final, 2 = Semifinals, ...), so
// MatchLabel can say which round of the KNOCKOUT the match is in. Without it
// "Match 1" collided with the pool match the operator was correcting
// ("Pool A · Match 1"), which is numbered from 1 as well. 0 when the bracket
// predates the field.
//
// PriorDecision and Restored are set only on a match forceReopenDownstreamChain
// reopened. PriorDecision is the decision the reopen cleared, and Restored is
// the eligibility that gave back when that decision was a withdrawal
// (restoreForceReopened): a withdrawal the reopen removed bars nobody, exactly
// as on the match the operator reopened or corrected. The handler broadcasts
// Restored as competitor_status_updated.
type ReopenedMatch struct {
	ID            string
	Number        int
	DisplayRound  int
	PriorDecision string
	Restored      *domain.CompetitorStatus
	// Court is the shiaijo the match is on ("" when it has none), so a
	// refusal can tell the operator where the match is being fought.
	Court string
	// RepPicksCleared lists the pick groups (state.RepPickGroup) of the sides
	// the same write that reopened the match gave another team, which took
	// that side's representative with it (seatBracketSide); nil when none
	// went. Its reopen history line then names those groups beside the
	// verdict's, and only those. markRepPicksCleared fills it from the diff the
	// door takes (repPickClears), so no second copy of the picks rides here.
	RepPicksCleared []string
}

// bracketMatchRef is the ONE way a bracket match becomes a ReopenedMatch, so
// every door that names a knockout match to the operator (the played and
// running refusals, and the matches a confirmed correction reopened) carries
// the same identity MatchLabel reads.
func bracketMatchRef(m *state.BracketMatch) ReopenedMatch {
	return ReopenedMatch{ID: m.ID, Number: m.MatchNumber, DisplayRound: m.DisplayRound, Court: m.Court}
}

// bracketMatchRefs is bracketMatchRef over a list, in order.
func bracketMatchRefs(ms []*state.BracketMatch) []ReopenedMatch {
	out := make([]ReopenedMatch, 0, len(ms))
	for _, m := range ms {
		out = append(out, bracketMatchRef(m))
	}
	return out
}

type ForceOptions struct {
	// Force bypasses the downstream-knockout-correction guard
	// (DownstreamKnockoutPlayedError): when a forward-policy completed write
	// would change a bracket match's already-propagated winner and a
	// downstream match in the propagation chain carries a result of its own,
	// the write is refused unless Force is true. A true Force also reopens
	// (reopenBracketMatch) the ONE downstream match that carries
	// its own result, since the correction just repainted their SideA/SideB
	// out from under their recorded verdicts. A POOL write in a mixed
	// competition that moves a qualifier out of a knockout match already
	// fought is refused the same way, and Force reopens those matches with
	// the new qualifier seated (requalifyAfterPoolWrite). It never gets past
	// a knockout match being fought now (DownstreamKnockoutRunningError), on a
	// pool correction or a knockout correction alike (operator decision
	// 2026-09-27).
	Force bool
	// Reopened, when non-nil, is populated with the IDs of every downstream
	// bracket match reopened by a forced correction (empty when Force is
	// false, or when nothing needed reopening). The caller uses it to
	// broadcast match_updated for each one, not just the corrected match.
	Reopened *[]ReopenedMatch
	// StartOnly marks a write that only STARTS the match (the court console's
	// Start match and Finish + Start Next, which have no score board of their
	// own): it changes the status and keeps whatever score the stored match
	// holds (keepQueuedScore). It exists because the wire cannot say "no score
	// sent": such a write carries empty ippon arrays and zero counts, the same
	// bytes as an operator clearing every mark on an editor board, which must
	// still clear them.
	StartOnly bool
	// ForceCourt is the organiser's "Start anyway" / "Reopen anyway"
	// (bc-adna): a start or a reopen into running on a court that already
	// runs a bout skips both court gates (the cross-competition one and the
	// in-competition one) and lands, so the court runs two bouts at once.
	// The sides gate (a side fighting elsewhere, a barred side) still runs.
	// Request-only, never stored; an anonymous self-run caller's is
	// stripped (enforceSelfRunPolicy). Read by the reopen; the score handler
	// judges its own gates before the write and carries it here only so a
	// forced score write says so beside StartOnly.
	ForceCourt bool
	// NoReplayHistory leaves out the history line an exact resend of a
	// recorded finish would otherwise add (answerExactScoreReplay). The score
	// handler sets it for an anonymous self-run participant, whose resend
	// changes nothing and could otherwise be looped to grow the match's
	// history file. The organiser's resend keeps its line.
	NoReplayHistory bool
	// WriteID and Basis tag a door's server-built write (a decision) with
	// the client's write id and what it had in view (bc-evts F1, Q9), for the
	// baseline it leaves in the score event log; a write the client builds
	// carries them on the MatchResult itself.
	WriteID string
	Basis   *state.ScoreBasis
	// Session is the client's event session, carried with Basis onto the
	// baseline (bc-evts S8 b) so the basis's Seq arm can count its events.
	Session string
	// ReopenReason is the reason a decision that ends a match reopened with no
	// reason keeps as the match's correction reason. It rides on the write
	// itself, so the baseline the write leaves in the score event log holds it:
	// set after the write it would be a result-group field its log did not
	// replay to. Taken only when the stored match holds no correction reason.
	ReopenReason string
	// ConfirmLots is the organiser's "Replace the lots" after a *LotsReplacedError:
	// a write that gives a drawn tie-breaker bout a winner, or reopens it, while the
	// lots they entered rule it (drawnSupplementaryRuledByChusen) lands, and the
	// bout's result decides the place. Request-only, never stored.
	ConfirmLots bool
}

// HoldReasonOlderRevision is the history reason of a previous build's running
// write the same scoring board had already followed with a newer one: a lower
// revision of a session whose higher one the score event log holds
// (olderRevisionThanLogged, bc-evts stage 2b; an in-memory guard in the score
// handler before it).
const HoldReasonOlderRevision = "older revision of this board"

// firstForceOptions returns the caller's ForceOptions, or the zero value
// (force=false, no reopen list) when the variadic slot was omitted.
func firstForceOptions(opts []ForceOptions) ForceOptions {
	if len(opts) > 0 {
		return opts[0]
	}
	return ForceOptions{}
}

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
// force (bc-kcdg) reaches ONLY the bracket branch's downstream-knockout-
// correction guard; the pool branch has no equivalent concept and ignores it.
func (e *Engine) writeToPoolOrBracket(h state.StoreTx, compId, matchId string, result *state.MatchResult, policy matchWritePolicy, force bool, comp *state.Competition) (mismatch bool, reopened []ReopenedMatch, inherited bool, err error) {
	return e.writeToPoolOrBracketLog(h, compId, matchId, result, policy, force, comp, e.snapshotScoreLog(h, compId, matchId, result, policy))
}

// writeToPoolOrBracketLog is writeToPoolOrBracket with the match's score log
// already read (snapshotScoreLog), for a caller that needed it before the
// write (the /score path's shapeScoreWrite): one read per write.
func (e *Engine) writeToPoolOrBracketLog(h state.StoreTx, compId, matchId string, result *state.MatchResult, policy matchWritePolicy, force bool, comp *state.Competition, lines []state.ScoreLogLine) (mismatch bool, reopened []ReopenedMatch, inherited bool, err error) {
	// The merge needs the competition to judge the write (engi, team,
	// kachinuki), so a
	// forward write whose caller did not load it loads it here.
	if comp == nil && policy == matchWriteForward {
		if comp, err = h.LoadCompetition(compId); err != nil {
			return false, nil, false, err
		}
	}
	var superseded bool
	perr := e.withPoolMatch(h, compId, matchId, func(r *state.MatchResult) error {
		// The POOL branch of the path POST /score and the bulk-score endpoint
		// actually take — the site the hand-copied merge once missed.
		var werr error
		mismatch, superseded, inherited, werr = applyPoolWriteLog(r, result, policy, comp, lines)
		if werr != nil {
			// A genuine validation failure (e.g. backfillMatchIdentity's
			// winnerId-names-neither-side check): propagate it AS the error,
			// distinct from errPoolWriteDropped below, so it reaches the
			// caller as a real rejection rather than being folded into the
			// silent-abandonment path. UpdatePoolMatchByID skips the save
			// when mutate returns an error, so nothing is persisted either way.
			return werr
		}
		if mismatch || superseded {
			// applyPoolWrite left the stored match untouched, so there is
			// nothing to persist. Aborting here is what makes a dropped POOL
			// write behave like a dropped BRACKET write, which has always
			// skipped its save (UpdateBracket returns early when its callback
			// errors). Without it the identical CSV row was re-serialized and
			// the standings-cache version bumped, so a rejected write left the
			// same footprint on disk as an accepted one — the branch asymmetry
			// this whole primitive exists to remove. (A fully-held write's
			// one footprint is its match history entry, written by the caller.)
			return errPoolWriteDropped
		}
		return nil
	})
	// A dropped write reached the match and chose not to persist; the verdict is
	// in the flags the closure set, not in the error, so it rejoins the found
	// path here rather than being reported as a store failure.
	if perr == nil || errors.Is(perr, errPoolWriteDropped) {
		if superseded {
			return false, nil, false, ErrMatchSuperseded
		}
		return mismatch, nil, inherited && !mismatch, nil
	}
	if !errors.Is(perr, errMatchNotFound) {
		return false, nil, false, perr
	}
	// The SAME policy the pool branch would have used.
	reopened, inherited, err = e.recordBracketMatchResultLog(h, compId, matchId, result, policy, force, comp, lines)
	return false, reopened, inherited, err
}

// snapshotScoreLog reads the match's score event log for a scoring snapshot
// the replay decides (replaysSnapshot); nil for any other write. A log that
// cannot be read is logged and read as empty: the snapshot is then replayed
// onto the stored match alone, and the door's baseline is not logged (its
// append reads the log again and fails the same way).
func (e *Engine) snapshotScoreLog(h state.StoreTx, compID, matchID string, result *state.MatchResult, policy matchWritePolicy) []state.ScoreLogLine {
	if policy != matchWriteForward || result.WriteDoor == DoorEvent || !replaysSnapshot(result) {
		return nil
	}
	lines, err := h.LoadScoreLog(compID, matchID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: score event log not read, the %s write is replayed onto the stored match alone: %v", compID, matchID, doorOf(result), err)
		return nil
	}
	return lines
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
// Returns an error when a client-supplied WinnerID names neither side once
// the row's own ids are known (see the validation block below); callers must
// treat that as a rejected write, never a silently-persisted one. That
// rejection is gated on policy == matchWriteForward, exactly like
// applyPoolWrite's sidesDisagree gate: matchWriteRestore replays a TRUSTED
// stored snapshot (the K3 rollback undoing a rejected/partial forward
// write), and rollbackMatchResultTx only LOGS a restore failure -- it never
// retries -- so rejecting a restore whose OWN stored WinnerID happens not to
// match its own side ids (legacy data predating this validation) would leave
// the very forward write the rollback exists to undo sitting on disk
// uncorrected.
func backfillMatchIdentity(result, stored *state.MatchResult, policy matchWritePolicy) error {
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
	// A client-supplied WinnerID that names NEITHER side is invalid data: it
	// would otherwise be persisted verbatim (the early return just below
	// trusted it outright) and counted for nobody in standings -- a
	// completed match with a winner nobody can find. Checked on every
	// FORWARD (client) write via domain.WinnerIDAcceptable (bc-pnum ruling
	// 1d) -- a RESTORE never rejects (see the doc comment above).
	//
	// Ids are minted for every roster row at write time and the draw itself
	// now refuses to run over a roster that still has an id-less row (bc-pnum
	// ruling 1c), so a NEWLY drawn match's side ids are never partially known
	// in current data; the tolerance this used to have for that case
	// (silently DROPPING an unattributable WinnerID instead of rejecting it,
	// added for PR #416 finding 6) is no longer warranted and has been
	// removed -- domain.WinnerIDNamesASide's own doc comment has the full
	// history.
	//
	// A competition drawn BEFORE ruling 1c existed can still have BOTH
	// SideAID and SideBID empty on a stored pool row -- ids were never
	// backfilled onto existing rows, only minted going forward -- and when
	// NEITHER side has a known id, "this winnerId names neither side" is not
	// a claim this check can support: there is no known pairing at all for it
	// to have missed. Rejecting there would 400 a legacy/queued write that
	// the SPA itself never produces this way (its serializer only sends
	// winnerId once it matches a server-supplied id), so the client discards
	// a score the operator entered offline.
	//
	// This exemption is deliberately narrow (BOTH unknown, not "at least one
	// unknown"): the PARTIALLY-stamped case -- one side id known, the other
	// not -- keeps rejecting unconditionally, exactly as PR #416 finding 6
	// pinned it (TestBackfillMatchIdentity_OneSideIDKnown,
	// TestScoreHandler_MixedIDRosterInventedWinnerIDRejected): a known side
	// id IS enough pairing information to say winnerId does not name it, and
	// a WinnerID invented from a name (the old SPA behaviour that finding
	// closed) must still be caught there. domain.WinnerIDAcceptable is the
	// ONE owner of the both-unknown exemption (bc-pnum review): do not
	// re-derive a local `bothSideIDsUnknown` here.
	return resolveWinnerIDFromSides(result, policy)
}

// resolveWinnerIDFromSides derives result.WinnerID from result.SideAID/
// SideBID (already populated by the caller from the match's fixed,
// generation-time pairing) and reports a *ValidationError when a
// client-supplied WinnerID (FORWARD policy only) names neither side once at
// least one side id is known (domain.WinnerIDAcceptable). Extracted from
// backfillMatchIdentity (bc-brid) so applyBracketMatchResult can share the
// IDENTICAL derivation rather than growing its own copy: both callers
// persist a match's generation-time SideAID/SideBID and need the same
// winner-id resolution over them (an explicit WinnerSide hint, else a name
// match against SideA/SideB, else -- a same-name head-to-head with no hint
// -- the scoreline).
//
// A no-op when result.WinnerID already carries a value (the common case: a
// restore replays a snapshot that already projected it, and a forward write
// backed by the id-aware validator on the wire already supplies it).
func resolveWinnerIDFromSides(result *state.MatchResult, policy matchWritePolicy) error {
	if policy == matchWriteForward &&
		!domain.WinnerIDAcceptable(result.WinnerID, result.SideAID, result.SideBID) {
		return validationErrorf("match %s: winnerId %q does not match sideAId %q or sideBId %q",
			result.ID, result.WinnerID, result.SideAID, result.SideBID)
	}
	if result.WinnerID != "" {
		return nil
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
		// draw (empty Winner) leave WinnerID empty -- unresolved, not a name
		// fallback: every id-only consumer (standings, tie-break, Swiss, …)
		// simply attributes this match to nobody rather than guessing from
		// the name, which is exactly what two same-name competitors made
		// ambiguous in the first place.
		switch a, b := countScoringIppons(result.IpponsA), countScoringIppons(result.IpponsB); {
		case a > b:
			result.WinnerID = result.SideAID
		case b > a:
			result.WinnerID = result.SideBID
		}
	}
	return nil
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
	var superseded error
	txErr := e.store.WithTransaction(compId, func(tx state.StoreTx) error {
		before := e.storedStatus(tx, compId, matchId)
		stamp, writeID := result.ModifiedAt, result.WriteID
		err := e.writeMatchResult(tx, compId, matchId, result, matchWriteForward)
		if err == nil {
			result.LineupWarning, err = e.afterMatchWrite(tx, compId, matchId, before, result.WriteDoor, writeID, stamp)
		}
		if errors.Is(err, ErrMatchSuperseded) {
			// Commit: the write's only footprint is its match history entry,
			// and an aborted transaction would discard it (bc-mrgc).
			superseded = err
			return nil
		}
		return err
	})
	if txErr != nil {
		return txErr
	}
	return superseded
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
	// The stored decision before the write, for the eligibility-follows-the-
	// ruling restore below (forward writes only: a restore replays a trusted
	// snapshot and its eligibility is the rolled-back write's caller's).
	var priorDecision string
	if policy == matchWriteForward {
		// An unknown match is left to writeToPoolOrBracket, which answers
		// it; a store that cannot be read is this write's error.
		prior, perr := e.lookupExistingResult(h, compId, matchId)
		var notFound *NotFoundError
		switch {
		case perr == nil:
			priorDecision = prior.Decision
		case !errors.As(perr, &notFound):
			return perr
		}
	}
	// force=false: RecordMatchResult (this function's only forward-policy
	// caller) offers no operator override, so a write that would trip the
	// downstream-knockout-correction guard is always refused here; the K3
	// rollback's matchWriteRestore call is exempt from the guard regardless.
	sideMismatch, _, inherited, err := e.writeToPoolOrBracket(h, compId, matchId, result, policy, false, nil)
	if errors.Is(err, ErrMatchSuperseded) {
		// Kept in the match's history (bc-mrgc); RecordMatchResult commits
		// the transaction on this error so the entry lands.
		e.recordSupersededWriteHistory(h, compId, matchId, result)
		return supersededBy(result)
	}
	if err != nil {
		return err
	}
	if sideMismatch {
		return ErrMatchSideMismatch
	}
	// A RESTORE touches no eligibility. It replays the snapshot of a write
	// that was refused (rollbackMatchResultTx: K3, or a pool requalification
	// refusal), and both refusals fire before the refused write recorded
	// any eligibility (recordIneligibilityFromDecision refuses K3 before its
	// SetCompetitorStatus), so there is nothing of the refused write's to
	// undo. Re-deriving eligibility from the snapshot is what this return
	// stops: a snapshot holding a withdrawal re-barred its loser, so a
	// kiken-injury competitor reinstated since was made ineligible again
	// by a correction that never landed. Nor does it leave a history entry:
	// the write it undoes never happened.
	if policy == matchWriteRestore {
		return nil
	}
	e.recordLandedWriteHistory(h, compId, matchId, result)
	// A write that kept a recorded withdrawal (preserveWithdrawalRuling), or
	// whose merge left the verdict where it was, changed no ruling, so it has
	// no eligibility consequence to record.
	if inherited || (result.Merge != nil && !result.Merge.ResultChanged) {
		return nil
	}
	// Side-effect writes are non-fatal: the match score is already staged,
	// so propagating would cause a 500 retry that double-records the score.
	loser, err := e.recordIneligibilityFromDecision(h, compId, matchId, result)
	if err != nil {
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compId, matchId, err)
	}
	// The eligibility record follows the ruling, as in
	// RecordMatchResultWithIneligibilityTx: a write that replaced a recorded
	// withdrawal, with a result that is not one or with a withdrawal by the
	// other side, restores whom the first one barred and keeps whom this one
	// barred (loser).
	e.restoreIfWithdrawalRemoved(h, compId, matchId, priorDecision, result.Decision, loser)
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
// the pool requalification check now applies here too — its predecessor had
// been added to the Tx twin only, which is exactly the drift this collapse
// exists to end. Side-effect write failures are still non-fatal: (nil, nil) + log.
//
// NEVER call this from inside a transaction: it takes the per-competition lock
// itself and that lock is not reentrant, so it deadlocks rather than erroring.
// See the note on RecordMatchResult, which carries the full reasoning.
//
// T085/T092, bc-twin.
//
// opts (bc-kcdg) is variadic ForceOptions purely to keep every pre-existing
// call site source-compatible; see ForceOptions' doc comment.
func (e *Engine) RecordMatchResultWithIneligibility(compId string, matchId string, result *state.MatchResult, opts ...ForceOptions) (*domain.CompetitorStatus, error) {
	var status *domain.CompetitorStatus
	var engErr error
	txErr := e.store.WithTransaction(compId, func(tx state.StoreTx) error {
		status, engErr = e.RecordMatchResultWithIneligibilityTx(tx, compId, matchId, result, opts...)
		// Return nil regardless: engErr is an application-level signal
		// (AlreadyIneligible → 409, validation → 400) surfaced after the tx,
		// and the K3 rollback has already replayed the prior state INSIDE the
		// tx, so committing persists exactly what the engine settled on. This
		// is the same commit contract the score handler uses around the Tx
		// variant (registerScoreHandler, mobileapp).
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return status, engErr
}

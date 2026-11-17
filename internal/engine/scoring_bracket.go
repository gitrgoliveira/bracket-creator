package engine

import (
	"fmt"
	"maps"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
//
// The matches the write re-seats without reopening get their own history line
// beside the reopen lines (recordRepPickClears), in the same transaction; a
// restore (matchWriteRestore) is not a door and records none.
func (e *Engine) recordBracketMatchResult(h state.StoreTx, compId string, matchId string, result *state.MatchResult, policy matchWritePolicy, force bool, comp *state.Competition) ([]ReopenedMatch, bool, error) {
	return e.recordBracketMatchResultLog(h, compId, matchId, result, policy, force, comp, e.snapshotScoreLog(h, compId, matchId, result, policy))
}

// recordBracketMatchResultLog is recordBracketMatchResult with the match's
// score event log already read (snapshotScoreLog).
func (e *Engine) recordBracketMatchResultLog(h state.StoreTx, compId string, matchId string, result *state.MatchResult, policy matchWritePolicy, force bool, comp *state.Competition, lines []state.ScoreLogLine) ([]ReopenedMatch, bool, error) {
	var (
		reopened  []ReopenedMatch
		inherited bool
		clears    []repPickClear
	)
	err := h.UpdateBracket(compId, func(bracket *state.Bracket) error {
		var ierr error
		reopened, inherited, clears, ierr = e.applyBracketResultIn(bracket, compId, matchId, result, policy, force, comp, lines)
		return ierr
	})
	if err == nil {
		e.restoreForceReopened(h, compId, reopened)
		e.recordRepPickClears(h, compId, clears, reopened)
	}
	return reopened, inherited && err == nil, err
}

// validateBracketCompletion rejects a Completed bracket-family write with no
// winner: an elimination result must never be indeterminate. A tied fixed-
// format encounter resolves via daihyosen; a tied kachinuki final bout
// resolves via encho on that same bout (daihyosen does not exist in
// kachinuki, mp-gmcg). Applies to all bracket match types and is the single
// AMENDMENT 2 choke point. It now has a single caller, applyBracketMatchResult,
// which is itself the one per-match bracket write that both the round path and
// the bronze fallback share, so there is nothing left here to drift.
func validateBracketCompletion(bm *state.BracketMatch, status state.MatchStatus, winner string) error {
	if status == state.MatchStatusCompleted && winner == "" {
		// MatchLabel(bracketMatchRef(bm)) is PURE (bm already carries
		// Number/DisplayRound, no store read), so it is safe to call here
		// even though this runs inside a live transaction (bc-cse item 14).
		return validationErrorf("%s: cannot mark completed with no winner; resolve the tie first (daihyosen, or encho on the final kachinuki bout)", MatchLabel(bracketMatchRef(bm)))
	}
	return nil
}

// applyBracketMatchResult writes result into a single bracket match — a round
// match or the bronze (3rd-place) knockout, which are the same shape and take
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
// sidesBeforeMerge folds a bracket match's stored pairing into the write
// before the merge (reconcileSides, as applyPoolWrite does before its own
// merge) and reports a forward write naming other competitors. A match not
// playable yet is left to applyBracketMatchResult's own refusal, which names
// what it is waiting for. applyBracketMatchResult reconciles again; the fold
// is idempotent.
func sidesBeforeMerge(bm *state.BracketMatch, result *state.MatchResult, policy matchWritePolicy) (mismatch bool) {
	if !bracketMatchPlayable(bm) {
		return false
	}
	disagree := reconcileSides(result, storedSides{A: bm.SideA, B: bm.SideB, AID: bm.SideAID, BID: bm.SideBID})
	return disagree && policy == matchWriteForward
}

func applyBracketMatchResult(bm *state.BracketMatch, result *state.MatchResult, policy matchWritePolicy) (applied bool, err error) {
	// A knockout match is playable only once both sides are resolved competitors
	// (feeder pools/matches finished). This replaces the old bracket-wide Preview
	// gate so the knockout fills in incrementally as pools qualify.
	if !bracketMatchPlayable(bm) {
		// PURE (bm already carries Number/DisplayRound): safe inside a live
		// transaction (bc-cse item 14).
		return false, validationErrorf("%s is not ready to score: a feeder pool or match has not finished", SentenceCase(MatchLabel(bracketMatchRef(bm))))
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
	// AID/BID (bc-brid): a bracket match's own pairing ids, when stamped
	// (generation, pool resolution, or propagation -- see
	// BracketMatch.SideAID's own doc comment); "" for an unrepaired legacy
	// row, which correctly stays silent here exactly as before (an empty
	// stored id means "unknown", never "mismatch").
	sidesDisagree := reconcileSides(result, storedSides{A: bm.SideA, B: bm.SideB, AID: bm.SideAID, BID: bm.SideBID})
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
	// The timestamp ordering is NOT here any more: the caller
	// (applyBracketResultIn) merges the write onto this match first, through
	// the same mergeMatchWrite the pool branch calls, so result already holds
	// the merged match and a write whose every change was held never reaches
	// this function (bc-mrgc).
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
	// The score replay's derived write (bc-evts) is the replay's own: no
	// verdict is restored onto it or derived for it here.
	if result.WriteDoor != DoorEvent {
		if policy == matchWriteForward && result.SubResults != nil {
			preserveDaihyosenOutcome(bm.SubResults, result)
		}
		deriveDaihyosenWinner(result)
	}
	// Preserve the match's fixed pairing ids (stamped at generation/pool
	// resolution/propagation, never rewritten by a score write -- see the
	// reconcileSides comment above) and resolve the winner id from them, the
	// bracket twin of backfillMatchIdentity's id half (bc-brid).
	// RepPlayerA/RepPlayerB don't apply here: BracketMatch carries no
	// rep-player fields, a bracket daihyosen is a numbered sub-bout, not a
	// team rep-player nomination. Runs before validateBracketCompletion,
	// matching that check's own "validated before the first mutation of bm"
	// ordering: a rejected winnerId must not leave bm partially written.
	if result.SideAID == "" {
		result.SideAID = bm.SideAID
	}
	if result.SideBID == "" {
		result.SideBID = bm.SideBID
	}
	if err := resolveWinnerIDFromSides(result, policy); err != nil {
		return false, err
	}
	// Preserve incoming Status. Pre-fix this was unconditionally Completed, so
	// the scoring modal's "Start" tap (which sends `{status: "running"}`)
	// immediately persisted the bracket match as completed with no winner.
	status := effectiveBracketWriteStatus(result)
	// Validated BEFORE the first mutation of bm. The round copies used to assign
	// Winner first and lean on updateBracketLocked skipping the save when the
	// mutate callback errors — true (state/bracket.go), but a non-local property
	// nothing at the write site stated. Ordering it here makes the discard local.
	if err := validateBracketCompletion(bm, status, result.Winner); err != nil {
		return false, err
	}
	bm.Winner = result.Winner
	bm.WinnerID = result.WinnerID
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
	// The per-group stamps the merge settled (forward), or the snapshot's
	// exactly (restore, nil included: a match never stamped goes back to
	// unstamped, as ModifiedAt does above).
	bm.GroupStamps = state.CloneGroupStamps(result.GroupStamps)
	// The mark ids and the event-log version, the replay's alone (bc-evts),
	// as applyPoolWrite settles them: the snapshot's under restore, the
	// derived write's (DoorEvent, the version only growing), and for any
	// other write the stored ones, carried below once the scorelines land.
	var prior *derivedScore
	switch {
	case policy == matchWriteRestore:
		bm.MarkIDs, bm.ScoreVersion = result.MarkIDs.Clone(), result.ScoreVersion
		bm.EventSeqs = state.CloneGroupStamps(result.EventSeqs)
		bm.TurnedAway = maps.Clone(result.TurnedAway)
	case result.WriteDoor == DoorEvent:
		bm.MarkIDs, bm.ScoreVersion = result.MarkIDs.Clone(), max(bm.ScoreVersion, result.ScoreVersion)
		bm.EventSeqs = mergeEventSeqs(bm.EventSeqs, result.EventSeqs)
		bm.TurnedAway = maps.Clone(result.TurnedAway)
	default:
		d := derivedScoreOf(bm.IpponsA, bm.IpponsB, bm.MarkIDs, bm.SubResults, bm.ScoreVersion, bm.EventSeqs)
		prior = &d
	}
	// The verdict rides in the ippons about to be rendered; forward writes
	// from non-validated paths must not persist a mark that contradicts the
	// result (see stripInvalidHantei). Restore replays the snapshot verbatim,
	// and the score replay's derived write already holds S3 (bc-evts).
	if policy == matchWriteForward && result.WriteDoor != DoorEvent {
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
	// Engi flags, as entered: a running engi write carries the flags so far
	// (saved as entered, operator ruling 2026-09-26), and a rollback restores
	// the snapshot's (bracketMatchAsResult). A completing engi write takes the
	// engi seam instead, and a kendo match has none.
	bm.FlagsA, bm.FlagsB = result.FlagsA, result.FlagsB
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
	if prior != nil {
		bm.MarkIDs = prior.carry(bm.IpponsA, bm.IpponsB, bm.SubResults)
		bm.ScoreVersion = prior.version
		bm.EventSeqs = state.CloneGroupStamps(prior.eventSeqs)
		result.MarkIDs, result.ScoreVersion = bm.MarkIDs.Clone(), bm.ScoreVersion
		result.EventSeqs = state.CloneGroupStamps(bm.EventSeqs)
		result.TurnedAway = maps.Clone(bm.TurnedAway)
	}
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
//
// bc-kcdg: before mutating a ROUND match (the bronze branch has no
// downstream, so it is exempt), guardDownstreamKnockoutCorrection checks
// whether this write would change an already-propagated winner while a
// downstream match in the propagation chain carries a result of its own
// (*DownstreamKnockoutPlayedError) or is being fought now
// (*DownstreamKnockoutRunningError). Either aborts before any mutation —
// UpdateBracket skips the save on a non-nil error, so a refusal leaves no
// footprint, matching the ErrMatchSuperseded contract just above it. force
// gets past the played refusal only (never the running one) and, once the
// correction and its propagation have landed, requeues the ONE downstream
// match the correction would otherwise have silently repainted; its id is
// returned so the caller can broadcast match_updated for it.
func (e *Engine) applyBracketResultIn(bracket *state.Bracket, compID, matchID string, result *state.MatchResult, policy matchWritePolicy, force bool, comp *state.Competition, lines []state.ScoreLogLine) ([]ReopenedMatch, bool, []repPickClear, error) {
	if bracket == nil {
		return nil, false, nil, notFoundErrorf("bracket not found for competition %s", compID)
	}
	mc := mergeCtx{comp: comp, knockout: true, lines: lines}
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			bm := &bracket.Rounds[rIdx][mIdx]
			if bm.ID != matchID {
				continue
			}
			// The stored pairing first, as the pool branch does it: the
			// merge judges the write against the merged match,
			// and a payload that omits its sides would otherwise read as one
			// with no sides at all (bc-mrgc review S6).
			if sidesBeforeMerge(bm, result, policy) {
				return nil, false, nil, ErrMatchSideMismatch
			}
			// The merge (bc-mrgc), the SAME owner the pool branch calls, and
			// FIRST: everything below judges the merged match. A write whose
			// every change is held is superseded -- the ordinary 200
			// {"applied":false} contract (CLAUDE.md "Write refusal and the
			// clock frame"), never a downstream refusal the guard would raise
			// against a result it is not changing (bc-cse finding 2).
			// Returning an error makes UpdateBracket skip the save.
			rep := mergeMatchWrite(bracketMatchAsResult(bm), result, policy, mc)
			if rep.Superseded() {
				return nil, false, nil, ErrMatchSuperseded
			}
			// The result lock (K1, result_lock.go): an older finish that did
			// not see the stored result is held, never refused, once the next
			// match was played or started.
			if policy == matchWriteForward && rivalDisplacesResult(rep) {
				if code := downstreamLockCode(bracket, rIdx, mIdx); code != "" {
					result.Merge = rep
					holdAsLocked(result, nil, code)
					return nil, false, nil, ErrMatchSuperseded
				}
			}
			// A bout-row correction over a recorded withdrawal keeps the
			// ruling (bc-tmfn). It must run BEFORE the downstream guard below,
			// which compares result.Winner with the stored winner: the bouts'
			// derived winner may name the other team, and the guard would then
			// refuse a correction that moves nobody, or, forced, requeue a
			// match it never changed.
			inherited := preserveWithdrawalRuling(rulingOfBracketMatch(bm), result, policy)
			// The guard runs forced too: force gets past a played later
			// match, never a running one (bc-rfsw).
			if err := guardDownstreamKnockoutCorrection(bracket, rIdx, mIdx, bm, result, policy, force); err != nil {
				return nil, false, nil, err
			}
			// Captured BEFORE the write, so the force branch below can tell a
			// correction that actually changes the winner from one that does
			// not. force skips the guard's played refusal, and an unconditional requeue there
			// cleared the next round for a write that stored the same winner --
			// including one confirmed for an unrelated reason.
			priorWinner, priorWinnerID := propagatedWinnerOf(bracket, rIdx, mIdx, bm)
			// The representatives every other match holds before the
			// propagation below can re-seat a side and take one away
			// (bracket_seat_audit.go): a played downstream match the write
			// reopens names its lost pick on its own reopen line, one it
			// leaves alone but re-seats has the door record a reseat line. A
			// restore (the K3 rollback replaying a prior) is not a door and
			// records nothing, so it takes no snapshot.
			var before map[string][2]string
			if policy == matchWriteForward {
				before = repPickSnapshot(bracket, matchID)
			}
			if _, err := applyBracketMatchResult(bm, result, policy); err != nil {
				return nil, false, nil, err
			}
			// Propagate only a genuinely completed result. A "running" update is
			// for live-status display, so the next round's SideA/SideB must stay
			// empty until the match has a final result. A correction never
			// puts a finished knockout match back to running: scoring stamped
			// after the result is turned away by the merge (the closed-match
			// rule), so the match keeps its finish and its advanced winner.
			var reopened []ReopenedMatch
			if bracket.Rounds[rIdx][mIdx].Status == state.MatchStatusCompleted {
				e.propagateBracketWinner(bracket, rIdx, mIdx)
				if force && policy == matchWriteForward &&
					winnerActuallyChanged(priorWinner, priorWinnerID, bm) {
					reopened = forceReopenDownstreamChain(bracket, rIdx, mIdx, bm.ID)
				}
			}
			clears := repPickClears(before, bracket)
			markRepPicksCleared(reopened, clears)
			return reopened, inherited, clears, nil
		}
	}
	// The bronze (3rd-place) knockout lives in Bracket.ThirdPlaceMatch, NOT in
	// Rounds, so the scan above never finds it. There is no propagation out of
	// bronze: it has no downstream match, so the downstream-correction guard
	// does not apply here either, and no other match can lose a pick to this
	// write (so, unlike the round branch, no snapshot of the picks is taken).
	if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
		if sidesBeforeMerge(bracket.ThirdPlaceMatch, result, policy) {
			return nil, false, nil, ErrMatchSideMismatch
		}
		if mergeMatchWrite(bracketMatchAsResult(bracket.ThirdPlaceMatch), result, policy, mc).Superseded() {
			return nil, false, nil, ErrMatchSuperseded
		}
		inherited := preserveWithdrawalRuling(rulingOfBracketMatch(bracket.ThirdPlaceMatch), result, policy)
		if _, err := applyBracketMatchResult(bracket.ThirdPlaceMatch, result, policy); err != nil {
			return nil, false, nil, err
		}
		return nil, inherited, nil, nil
	}
	return nil, false, nil, notFoundErrorf("bracket match %s not found", matchID)
}

// bracketMatchCarriesOwnResult reports whether bm holds a result someone
// actually recorded (closed, with any ippons/sub-results/decision/hansoku/
// flags/resultSource), as opposed to a slot merely auto-completed by
// propagateBracketWinner's bye pass-through. buildBracketFromDraw's bye
// auto-resolve and propagateBracketWinner's three empty-side arms set only
// Winner/WinnerID/Status=Completed on such a slot — never any of the fields
// checked here — so this predicate correctly lets a bye-completed downstream
// slot through (operator ruling bc-kcdg #2: a bye must never block a
// correction).
//
// This is deliberately narrower than bracketMatchStartedOrDecided
// (kachinuki.go), which asks whether a match is being or has been fought
// (running, completed, or carrying a winner), a bye completion included; the retractions no longer apply it to a bye, which
// they unwind instead (propagatedDownstreamOf). The two predicates answer
// different questions for different rules; do not merge them.
//
// Only a CLOSED match counts. A RUNNING match is not closed and is not this
// predicate's business: it is refused separately, and first, by
// propagatedDownstream.running (operator decision 2026-09-27), never
// repainted or cleared.
//
// There is deliberately no exemption for a match this bead's own forced
// correction has already dealt with. forceReopenDownstreamChain REQUEUES its
// downstream matches (reopenDisplacedBracketMatch: reopened in place as
// scheduled, no verdict, its points kept), so they fail the closed
// precondition and the operator's re-entry is never refused. An earlier fix instead left them RUNNING and exempted them by their
// ReopenPending flag, which had to be unpicked twice: the flag alone also
// exempted a match PART WAY through its re-fight, so a second correction
// silently repainted it while keeping the ippons already struck for the
// competitor being replaced -- this bead's own defect, reintroduced by its
// fix. A running match reopened through the KACHINUKI path is refused by
// propagatedDownstream.running, not by this predicate, and should be:
// someone is fighting it.
func bracketMatchCarriesOwnResult(bm *state.BracketMatch) bool {
	// CLOSED is the precondition (operator ruling 2026-09-19: "you do not
	// affect the state of the next match unless it was closed"). A scheduled
	// slot has nothing to lose. A RUNNING match is not closed either, but it
	// is no longer repainted: the operator decision of 2026-09-27 reversed the
	// 2026-09-19 trade (keep the struck ippons, repaint the name above them)
	// for a match being fought, so every correction door refuses it through
	// propagatedDownstream.running before asking this. Only a COMPLETED match
	// can be left permanently displaying a competitor its own recorded result
	// disagrees with, which is the defect this predicate exists for.
	if bm.Status != state.MatchStatusCompleted {
		return false
	}
	return len(bm.IpponsA) > 0 ||
		len(bm.IpponsB) > 0 ||
		len(bm.SubResults) > 0 ||
		bm.Decision != "" ||
		bm.HansokuA > 0 ||
		bm.HansokuB > 0 ||
		// An ENGI bout records neither ippons nor a decision: its whole result
		// is the referees' flag count. Without this arm the predicate read a
		// fought engi match as an untouched slot, so the guard found nothing
		// blocking and an engi correction repainted a played final in silence
		// -- this bead's defect surviving inside its own fix, on the format
		// its second door (engi.go's dispatch seam) serves.
		bm.FlagsA > 0 ||
		bm.FlagsB > 0 ||
		bm.ResultSource != "" ||
		// IsOverridden is the stored result shape carrying no scoreline at all:
		// an overridden match (from earlier releases) holds
		// Winner/WinnerID/Status=Completed and this flag, and nothing else. A
		// scoreline-only predicate reads that as an untouched slot and lets the
		// correction repaint it silently. It is also precisely what separates
		// an overridden match from a bye pass-through, which sets the same
		// Winner/Status pair and must NOT block.
		bm.IsOverridden
}

func (e *Engine) propagateBracketWinner(bracket *state.Bracket, rIdx, mIdx int) {
	if rIdx >= len(bracket.Rounds)-1 {
		return
	}
	m := bracket.Rounds[rIdx][mIdx]
	nextMatchIdx := mIdx / 2
	nextM := &bracket.Rounds[rIdx+1][nextMatchIdx]

	// seatBracketSide, not a bare assignment: a side given another team takes
	// its representative-bout pick away with it.
	side := feedsSide(mIdx)
	seatBracketSide(nextM, side, m.Winner, m.WinnerID)

	// Feed the loser of a SEMIFINAL into the bronze (3rd-place) knockout match.
	// The semifinal round index is len(Rounds)-2 (the round that feeds the
	// final). This is a pure advancement step: it moves a name (and, since
	// bc-brid, an id) only, never computes a score, so it keeps
	// propagateBracketWinner a pure helper. Guarded on ThirdPlaceMatch being
	// present, which bracket.go stamps whenever comp.RequiresSingleThirdPlace()
	// is true -- naginata by default, but generalised through that method, not
	// naginata-only.
	if bracket.ThirdPlaceMatch != nil && rIdx == len(bracket.Rounds)-2 {
		// bracketLoserIdentity (ranking.go), not a bare `switch m.Winner`
		// (bc-brid): a same-name semifinal (two competitors
		// sharing a display name from different dojos, legal per
		// CheckDuplicateEntriesByNameDojo) makes m.Winner equal BOTH m.SideA
		// and m.SideB, and a plain switch's first-case-wins semantics always
		// fed the WINNER's own id into the bronze match under that shape.
		// bracketLoserIdentity resolves the winner by id first
		// (domain.AttributeWinnerSide), so it names the actual loser even
		// when both sides share a name.
		loser, loserID := bracketLoserIdentity(m)
		// Skip empty/placeholder losers (bye matches resolve with one side blank).
		if loser != "" && !helper.IsWinnerOfPlaceholder(loser) {
			bronze := bracket.ThirdPlaceMatch
			// Assign by semifinal POSITION, not first-empty-slot. The round
			// feeding the final always has exactly two matches (mIdx 0 and 1),
			// so each maps to a fixed bronze side. This mirrors the final's
			// positional advancement above and is re-score safe: correcting a
			// semifinal overwrites the correct bronze slot in place. A
			// fill-first-empty scheme would leave a stale loser pinned once
			// both slots are populated and a semifinal is later re-scored.
			seatBracketSide(bronze, side, loser, loserID)
		}
	}

	// Try to resolve the OTHER side if it's a "Winner of" placeholder
	if helper.IsWinnerOfPlaceholder(nextM.SideA) {
		// nextM.SideA is "Winner of rX-mY"
		r, m := parseWinnerOf(nextM.SideA, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				seatBracketSide(nextM, domain.MatchSideA, srcM.Winner, srcM.WinnerID)
			}
		}
	}
	if helper.IsWinnerOfPlaceholder(nextM.SideB) {
		r, m := parseWinnerOf(nextM.SideB, len(bracket.Rounds))
		if r >= 0 && r < len(bracket.Rounds) && m >= 0 && m < len(bracket.Rounds[r]) {
			srcM := bracket.Rounds[r][m]
			if srcM.Status == state.MatchStatusCompleted {
				seatBracketSide(nextM, domain.MatchSideB, srcM.Winner, srcM.WinnerID)
			}
		}
	}

	// Recursive resolution
	if nextM.SideA != "" && nextM.SideB == "" && !helper.IsWinnerOfPlaceholder(nextM.SideA) {
		nextM.Winner = nextM.SideA
		nextM.WinnerID = nextM.SideAID
		nextM.Status = state.MatchStatusCompleted
		e.propagateBracketWinner(bracket, rIdx+1, nextMatchIdx)
	} else if nextM.SideA == "" && nextM.SideB != "" && !helper.IsWinnerOfPlaceholder(nextM.SideB) {
		nextM.Winner = nextM.SideB
		nextM.WinnerID = nextM.SideBID
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

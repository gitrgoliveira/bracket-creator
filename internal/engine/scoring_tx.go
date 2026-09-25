// Package engine, scoring_tx.go owns the tx-run flows of the scoring family:
// RecordMatchResultWithIneligibilityTx, RecordDecisionTx, StartMatchTx, the K3
// rollback, and the tx court/eligibility checks. They accept a state.StoreTx
// so a caller (typically a HTTP handler) can run them inside a single
// Store.WithTransaction acquire of the per-comp write lock.
//
// Why the tx flows exist. Pre-T156 the score and decision handlers called
// engine methods that each acquired their own per-comp lock via
// UpdatePoolMatchByID / UpdateBracket / SetCompetitorStatus.
// The handler's logical "score this match" operation translated to
// 3-5 separate lock acquires, with concurrent writers free to land
// mutations in the gaps. Running under one tx collapses all of those into
// ONE acquire so the entire match-write + ineligibility-write sequence
// is indivisible.
//
// Why this file no longer holds twin BODIES (bc-twin). It used to carry a
// hand-copied tx variant of each write primitive (withPoolMatchTx,
// writeToPoolOrBracketTx, recordBracketMatchResultTx, lookupExistingResultTx,
// recordIneligibilityFromDecisionTx, recordMatchResultTx), differing from the
// non-tx bodies only in the store handle. Twin drift produced real bugs three
// separate times, so the primitives now exist ONCE, taking an
// `h state.StoreTx` handle — *state.Store satisfies state.StoreTx, so the
// same body serves both doors, and the non-tx entry points are
// WithTransaction shims (see RecordMatchResult /
// RecordMatchResultWithIneligibility in scoring.go). What remains here is the
// tx-shaped orchestration, not duplicated persistence.
//
// A follow-up pass collapsed four more twins the same way: lookupMatchSides,
// checkConcurrentIneligibility, hasDownstreamMatchStarted, and (at the time)
// restoreCompetitorEligibility, all taking `h`. restoreCompetitorEligibility
// itself was later removed outright (second-Opus-pass item 3):
// RecordDecisionTx's restore-on-rescore no longer re-derives the prior
// loser's identity from the match's side names/ids at all -- it restores
// whichever competitor-status entry carries this exact MatchID and is still
// Eligible:false, which is exact by construction and needs no roster lookup.
// lookupMatchSides, checkConcurrentIneligibility and hasDownstreamMatchStarted
// remain, living in eligibility.go, taking `h`.
// RecordDecisionTx below is the last of the original hand-copied pairs to be
// resolved — unlike the others it keeps ITS name (mobileapp's ScoringEngine
// interface calls it directly), and RecordDecision (eligibility.go) is now
// the WithTransaction shim over it, the same direction as every other pair.
//
// Constraint, unchanged. Flows running under a tx MUST call only the tx
// handle, NEVER e.store directly. The per-comp lock is non-reentrant
// (sync.RWMutex is not recursive on Lock by Lock); a direct e.store.Save*
// call from inside the closure passed to WithTransaction would deadlock.
//
// T156, NFR-010, bc-twin.
package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RecordMatchResultWithIneligibilityTx is the tx-aware twin of
// RecordMatchResultWithIneligibility. The K3/CHK047 partial-write
// rollback path replays the prior result via the same tx so the
// rollback also runs inside the single lock acquire.
//
// On AlreadyIneligibleError the caller's WithTransaction body should
// propagate the error through to the handler, there's no need (and no
// way) to roll back via a separate tx here because the rollback write
// is part of THIS tx's mutations.
//
// T156.
//
// opts (bc-kcdg) is variadic ForceOptions purely to keep every pre-existing
// call site source-compatible; see ForceOptions' doc comment. Known gap: a
// forced write that force-reopens downstream matches and is THEN rolled back
// by the K3 AlreadyIneligibleError path below only restores the corrected
// match itself (rollbackMatchResultTx replays `prior` through this same
// match id) -- the downstream matches forceReopenDownstreamChain reopened
// stay reopened. The same holds for the knockout matches a forced POOL
// correction reopens (requalifyAfterPoolWrite): the rollback restores the
// pool match, not the bracket. Reaching either requires force=true on a
// decision write whose loser turns out to already be ineligible from a
// different match, a narrow intersection not covered by this bead's test
// list; recorded here rather than silently left undiscoverable.
func (e *Engine) RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult, opts ...ForceOptions) (*domain.CompetitorStatus, error) {
	fo := firstForceOptions(opts)
	result.ID = matchID

	// Engi dispatch seam (tx-aware): a flag-scored competition records via the
	// engi slice through the SAME tx so the write stays inside the caller's
	// single per-comp lock acquire. Engi has no eligibility concept, so the
	// status return is nil.
	comp, loadErr := tx.LoadCompetition(compID)
	if loadErr != nil {
		return nil, fmt.Errorf("RecordMatchResultWithIneligibilityTx: load competition %s: %w", compID, loadErr)
	}
	// Only a COMPLETING write goes through the engi recorder. Engi's flag-total
	// rule (odd, in {1,3,5}: a 3- or 5-referee panel cannot draw) can only be
	// satisfied by a real result, so routing every engi write through it
	// rejected the one write that opens a match: "Start match" sends
	// status:"running" with no flags at all, which arrived as 0+0=0 and came
	// back a 400. The effect was total -- an engi competition could not be run
	// from the court console, because its first match could never start.
	//
	// Same scoping rule as applyHansokuIppons below: a write answers for what
	// it INTRODUCES. A start write introduces no flags, so it has no flag total
	// to be judged on, and it falls through to the ordinary path that records
	// the status transition.
	// A START write is the one shape with no flag total to judge: no flags at
	// all AND not completing. Everything else still goes through the recorder,
	// including a scoring write that omits Status entirely (the engine's own
	// callers do, so gating on Status == completed would have silently stopped
	// stamping winners -- caught by TestWinnerIDInvariant_EveryWritePathStampsASideID).
	engiStartWrite := result.FlagsA == 0 && result.FlagsB == 0 && result.Status != state.MatchStatusCompleted
	if comp != nil && comp.Engi && !engiStartWrite {
		// A pool write in a mixed engi competition answers for the knockout
		// its pool feeds exactly as a kendo one does (below): prior is read
		// first, so a refusal can put the pool row back. Only a pool write in
		// a mixed competition needs it, so no other engi write pays the read.
		// A prior it cannot read is the write's error, never a nil prior: a
		// nil prior leaves the refusal below nothing to roll back to.
		var engiPrior *state.MatchResult
		if comp.Format == state.CompFormatMixed && IsPoolMatchID(matchID) {
			var lerr error
			if engiPrior, lerr = e.lookupExistingResult(tx, compID, matchID); lerr != nil {
				return nil, lerr
			}
		}
		// fo carries bc-kcdg's downstream-correction confirmation through the
		// engi seam. Without it an engi knockout correction could neither be
		// refused nor confirmed: the guard lives past this early return.
		rec, recErr := e.recordEngiMatchResult(tx, compID, matchID, result.FlagsA, result.FlagsB, result.CorrectionReason, fo)
		if recErr != nil {
			return nil, recErr
		}
		backfillEngiResult(result, rec)
		if err := e.requalifyMixedPoolWrite(tx, compID, comp, matchID, rec, engiPrior, fo); err != nil {
			return nil, err
		}
		return nil, nil
	}

	if err := applyHansokuIppons(result); err != nil {
		return nil, err
	}
	deriveDaihyosenWinner(result)

	// Capture the prior result so we can roll back the score on
	// AlreadyIneligibleError. lookupExistingResult reads directly from
	// the tx so it sees the state INSIDE the lock (the on-disk state
	// hasn't moved under us, we hold the lock). A prior it cannot read is
	// the write's error: every guard below (the kachinuki merge, K3, the
	// rollback, the pool requalification refusal) reads a nil prior as
	// "nothing stored", which would let the write through unguarded.
	prior, err := e.lookupExistingResult(tx, compID, matchID)
	if err != nil {
		return nil, err
	}

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

	// Default-win bout padding (bc-tmfn follow-up): a non-kachinuki team
	// match a default-win ruling (any kiken, fusenpai, or fusensho:
	// domain.IsDefaultWinDecisionStr) closes before every numbered bout has
	// a result of its own -- most commonly a kiken declared before the
	// match's first bout was ever scored -- gets an empty SubMatchResult row
	// (Position only) for every missing position 1..TeamSize, via
	// state.NeedsDefaultWinBoutPadding / state.PadDefaultWinBoutPositions, the
	// SAME predicate and function the legacy-load repair uses
	// (state.EnsureLegacyUpgraded). Without a row for every position,
	// state.DefaultWinCreditSide's readers (TeamResultFrom,
	// accrueTeamSubResults, the Excel export) have nothing to range over for
	// the positions nobody fought and silently credit them zero.
	//
	// Gated on PRIOR's sides and unreadable flag, never result's: a client
	// score payload commonly omits SideA/SideB and relies on reconcileSides
	// (inside applyPoolWrite/applyBracketResultIn, called later via
	// writeToPoolOrBracket) to backfill them from the stored row -- reading
	// result.SideA/SideB here would skip padding on exactly that common case
	// (a bare kiken payload). Likewise a stored SubResults cell that failed
	// to parse must never be padded over: NeedsDefaultWinBoutPadding's
	// subResultsUnreadable check keeps this branch from turning an empty
	// result.SubResults into a non-empty padded one, which would defeat
	// applyPoolWrite's own raw-bytes preservation (it only keeps
	// SubResultsRaw when the incoming SubResults is still empty) and destroy
	// the only copy of the corrupt cell. prior is guaranteed non-nil here:
	// the lookupExistingResult error above already returned on a miss.
	//
	// The decision tested is result's own, OR -- covering a CORRECTION that
	// KEEPS a stored withdrawal ruling -- prior's: such a write's own
	// Decision is either empty (a score sheet states no decision) or an
	// explicit "hikiwake" (KeepsWithdrawalRuling's own two accepted shapes),
	// and applyPoolWrite/applyBracketResultIn only reinstate the stored
	// decision AFTER this point (preserveWithdrawalRuling), so testing
	// result.Decision alone would miss it here -- and nothing downstream
	// pads again once this function returns. A correction can legitimately
	// send fewer SubResults rows than TeamSize (e.g. a bout-level edit that
	// only touches the rows it corrects), so this is the one place that
	// still needs to catch it up.
	//
	// Excluded: a bye (either side empty -- a bye never carries a
	// default-win decision in practice, but the padding shape assumes two
	// real teams either way) and a pool daihyosen/tiebreaker row
	// (IsPoolDaihyosenMatchID / IsTiebreakerMatchID), which is scored as a
	// single individual representative bout, never the team's own numbered
	// positions. Both checked inside NeedsDefaultWinBoutPadding.
	if comp != nil && comp.TeamSize >= 2 && !comp.IsKachinuki() {
		// KeepsWithdrawalRuling is checked UNCONDITIONALLY, not only when
		// result.Decision == "": it is also true for an explicit incoming
		// "hikiwake" over a stored default win (its own definition allows
		// incoming.Decision to be "" OR "hikiwake"), and that write must
		// still pad, since preserveWithdrawalRuling reinstates prior.Decision
		// downstream regardless of which of the two the correction sent.
		padDecision := result.Decision
		if KeepsWithdrawalRuling(prior.Status, prior.Decision, result) {
			padDecision = prior.Decision
		}
		if state.NeedsDefaultWinBoutPadding(result.Status, padDecision, prior.SideA, prior.SideB, matchID,
			result.SubResults, prior.SubResultsUnreadable, comp.TeamSize) {
			result.SubResults = state.PadDefaultWinBoutPositions(result.SubResults, comp.TeamSize)
		}
	}

	// K3 ahead of the write: a withdrawal whose loser a DIFFERENT match has
	// already made ineligible is refused before anything is written. The
	// post-write check (recordIneligibilityFromDecision, below) still refuses
	// it and rolls the match back, but that rollback restores the match row
	// ONLY: what the write set off in the same transaction (a mixed
	// competition's requalification reopening and repainting the knockout
	// matches the old qualifier fought, and restoring the eligibility their
	// verdicts recorded) stayed staged and committed with the refusal.
	if err := e.refuseConcurrentWithdrawal(tx, compID, matchID, result, prior); err != nil {
		return nil, err
	}

	sideMismatch, reopened, inheritedRuling, err := e.writeToPoolOrBracket(tx, compID, matchID, result, matchWriteForward, fo.Force)
	if err != nil {
		return nil, err
	}
	if fo.Reopened != nil {
		*fo.Reopened = reopened
	}
	if sideMismatch {
		// Match identity is fixed at generation; a score payload naming
		// different competitors is rejected (HTTP 409) rather than allowed to
		// overwrite the stored pairing. Returns before any side-effect write.
		return nil, ErrMatchSideMismatch
	}

	// A pool write in a mixed competition answers for what it does to the
	// knockout its pool feeds (pool_requalify.go): a place whose occupant
	// moves is repainted, a knockout match the old qualifier already fought is
	// named for the operator to confirm (force reopens it), and a match being
	// fought now refuses the write. Any refusal restores prior first. Runs for
	// every pool write, a first completion included: it only ever acts on a
	// slot that is already occupied, and ignores a pool that is not complete.
	if err := e.requalifyMixedPoolWrite(tx, compID, comp, matchID, result, prior, fo); err != nil {
		return nil, err
	}

	// A bout-row correction that kept a recorded withdrawal
	// (preserveWithdrawalRuling) changed no ruling, so it has no eligibility
	// consequence: re-recording the withdrawal would rewrite the competitor's
	// status (its RecordedAt, and undo a kiken-injury reinstatement) and
	// broadcast a change nobody made. Deliberately keyed on the helper's own
	// report, never on comparing result with prior: an unchanged
	// (decision, loser) test would also swallow a genuine re-decision.
	if inheritedRuling {
		return nil, nil
	}
	status, err := e.recordIneligibilityFromDecision(tx, compID, matchID, result)
	if err != nil {
		var alreadyErr *AlreadyIneligibleError
		if errors.As(err, &alreadyErr) {
			// K3/CHK047: roll back the partial score-write within the
			// same tx. The pool/bracket mutation already landed on disk,
			// but the intended loser is already ineligible from a
			// different match, revert before returning 409.
			if prior != nil {
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
			}
			return nil, err
		}
		log.Printf("engine: recordIneligibilityFromDecision compId=%s matchId=%s: %v", compID, matchID, err)
		return nil, nil
	}
	// The eligibility record follows the ruling (operator ruling 2026-09-24:
	// "Everything should be able to be fixed, in case of a wrong entry").
	// This write landed (a superseded, mismatched or rolled-back write
	// returned above) and REPLACED a recorded withdrawal (a kept ruling
	// returned above, via inheritedRuling): with a result that is not one
	// (fusensho or daihyosen through recordDecisionTx, or any decision
	// KeepsWithdrawalRuling does not keep the ruling for), or with a
	// withdrawal by the OTHER side. The withdrawal it replaced never
	// happened, so the competitor it barred is restored, keeping the one
	// this write barred (status), on every door alike: /score, bulk-score
	// and /decision. The restored status takes priority in the return, so
	// the handler broadcasts competitor_status_updated for it: the new
	// withdrawer's status is already carried by the match's own decision.
	// result is the post-merge value, i.e. what is now stored.
	if prior != nil {
		if restored := e.restoreIfWithdrawalRemoved(tx, compID, matchID, prior.Decision, result.Decision, status); restored != nil {
			status = restored
		}
	}
	return status, nil
}

// refuseConcurrentWithdrawal is K3's pre-write half: for a withdrawal decision
// it names the loser the write would record and refuses with
// AlreadyIneligibleError when a different match has already made them
// ineligible (checkConcurrentIneligibility, the check recordDecisionTx makes
// before its own write). The loser is read off a scratch copy with the stored
// identity folded in by backfillMatchIdentity, the same fold the write
// applies, so it is the loser the post-write check would name. A payload that
// fold rejects, or whose losing side cannot be attributed, is left to the
// write and the post-write check, which answer it as before.
func (e *Engine) refuseConcurrentWithdrawal(tx state.StoreTx, compID, matchID string, result, prior *state.MatchResult) error {
	if prior == nil || !domain.IsWithdrawalDecisionStr(result.Decision) {
		return nil
	}
	probe := *result
	if err := backfillMatchIdentity(&probe, prior, matchWriteForward); err != nil {
		return nil
	}
	loserID, loserName, ok := losingSide(&probe)
	if !ok {
		return nil
	}
	return e.checkConcurrentIneligibility(tx, compID, matchID, loserID, loserName)
}

// restoreIfWithdrawalRemoved is the one statement of "the eligibility record
// follows the ruling" for a write that replaced a recorded withdrawal (kiken,
// fusenpai). It restores every competitor-status entry this match recorded
// (restoreEligibilityRecordedByMatch) except the one the write itself has just
// recorded, loser:
//   - the decision now stored is not a withdrawal: the withdrawal was
//     removed, so nobody this match barred stays barred (loser is nil);
//   - it is a withdrawal again: it may have MOVED to the other side, so the
//     competitor the first entry barred is restored and loser, the one this
//     write barred, is kept. When the write could not resolve a loser (nil)
//     no entry is provably stale, so nothing is restored (PR #416 finding 1:
//     re-recording a withdrawal whose side name drifted must not free the
//     competitor who still withdrew).
//
// Returns the last restored status, or nil when nothing was restored. Every
// door that replaces a withdrawal calls it after its write landed:
// RecordMatchResultWithIneligibilityTx (PUT /score, bulk-score and, through
// it, POST /decision) and writeMatchResult. The reopen (reopenUnderCourtLock)
// and restoreForceReopened call it too, after their save, since clearing the
// decision removes a withdrawal just as a rescore does.
func (e *Engine) restoreIfWithdrawalRemoved(tx state.StoreTx, compID, matchID, priorDecision, storedDecision string, loser *domain.CompetitorStatus) *domain.CompetitorStatus {
	if !domain.IsWithdrawalDecisionStr(priorDecision) {
		return nil
	}
	keep := ""
	if domain.IsWithdrawalDecisionStr(storedDecision) {
		if loser == nil {
			log.Printf("engine: restoreIfWithdrawalRemoved compId=%s matchId=%s: the write recorded %q but resolved no loser; restoring nobody (no entry this match recorded is provably stale)",
				compID, matchID, storedDecision)
			return nil
		}
		keep = loser.PlayerID
	}
	return e.restoreEligibilityRecordedByMatch(tx, compID, matchID, keep)
}

// rollbackMatchResultTx restores prior over a partial score-write within the
// same transaction. Shared by the reject paths in
// RecordMatchResultWithIneligibilityTx: K3 (AlreadyIneligible) and the pool
// requalification refusals (requalifyAfterPoolWrite: a knockout match the
// move reaches is being fought, or was already fought and the operator has not
// confirmed). Within a tx, writes are in-memory WAL
// intents coalesced last-write-wins, so this restore supersedes the forward
// write before Commit applies the final state. prior must be non-nil.
//
// The nil-collision fields (SubResults, and the hantei flag at both match and
// sub-bout level) need no pre-mangling here: the snapshot replays under
// matchWriteRestore, which reads a nil as "there was nothing" rather than as
// "the writer said nothing". See matchWriteRestore for why the distinction is
// what keeps a rollback from re-applying the write it is undoing.
//
// The snapshot is restored byte-for-byte, so applyHansokuIppons is
// intentionally NOT applied; writeMatchResult is the post-hansoku write. A
// restore can never trip its ErrMatchSideMismatch return (the identity check
// is forward-only), so the replay path this used to take through a dedicated
// mismatch-discarding twin is now the shared body with nothing discarded.
func (e *Engine) rollbackMatchResultTx(tx state.StoreTx, compID, matchID string, prior *state.MatchResult) {
	prior.ID = matchID
	if rerr := e.writeMatchResult(tx, compID, matchID, prior, matchWriteRestore); rerr != nil {
		log.Printf("engine: RecordMatchResultWithIneligibilityTx rollback failed compId=%s matchId=%s: %v", compID, matchID, rerr)
	}
}

// StartMatchTx is the tx-aware FR-035 gate. Same contract as
// StartMatch: returns *IneligibleCompetitorError when any participant
// in matchID is marked ineligible from a *different* match or is
// currently Running in a different match (Phase 2c simultaneity gate).
// The undo-path is permitted (status with MatchID==matchID is skipped).
//
// The score handler wraps RecordMatchResultWithIneligibilityTx with
// this check so a fought / hikiwake score on a match whose
// participants include someone previously ineligible is rejected
// before any disk write. Kiken/fusenpai decisions go through
// RecordDecisionTx, which intentionally bypasses this gate, they ARE
// the act of recording a new withdrawal.
func (e *Engine) StartMatchTx(tx state.StoreTx, compID, matchID string) error {
	if err := e.checkCourtExclusivityTx(tx, compID, matchID); err != nil {
		return err
	}
	if err := e.checkSimultaneousMatchTx(tx, compID, matchID); err != nil {
		return err
	}
	ids, err := e.matchSideParticipantIDs(tx, compID, matchID)
	if err != nil {
		return err
	}

	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	return eligibilityErrorForSides(statuses, matchID, ids)
}

// checkSimultaneousMatchTx returns *IneligibleCompetitorError if either
// participant in matchID is currently Running in a different match within
// the same competition. Pool matches and bracket matches are both checked.
// checkSimultaneousMatch (eligibility.go) is the non-tx entry point, calling
// this with e.store as h (bc-twin: one body, two doors).
//
// matchID's own identity is resolved from the pool-matches slice FIRST, via
// findPoolMatch: a pool row already carries its own SideAID/SideBID, so
// reading them directly is strictly better than a roster name scan, which
// two different competitors can share (operator ruling bc-pnum). When
// matchID is instead a bracket row, its own SideAID/SideBID are now read the
// same direct way (bc-brid, findBracketMatchInBracket); the roster scan
// (resolvePlayerIDs) only fills a side the row itself did not stamp -- a
// bye, an unresolved "Winner of ..." feeder, or an unrepaired legacy bracket
// row (a tx read bypasses EnsureLegacyUpgraded by design, see
// legacy_upgrade.go's header, so a bracket mid-repair can still reach here
// unstamped).
//
// The pool-vs-pool comparison below stays id-only: a side with no resolvable
// id (rawIDA/rawIDB == "") never matches any other pool match, there is no
// name fallback for it (every pool match is repaired at load). The
// bracket-vs-bracket comparison (matchesBracketSide) prefers id but keeps a
// name fallback for a CANDIDATE match whose own row is still unrepaired --
// it never falls back to name once this side's id AND both of the
// candidate's ids are known and none of them match, which is exactly the
// false "already fighting" block two same-named, different-dojo competitors
// used to trip on (the live defect bc-brid fixes).
//
// Phase 2c simultaneity gate.
func (e *Engine) checkSimultaneousMatchTx(h state.StoreTx, compID, matchID string) error {
	var sideA, sideB, rawIDA, rawIDB string
	isPoolMatch := false
	// Load errors are returned as they are (see lookupExistingResult): an
	// unreadable pool-matches.csv/bracket.json must not silently drop out of
	// this check -- swallowing it let a corrupt file pass the simultaneity
	// gate and start a competitor on a second court while their first match
	// was still running.
	poolMatches, poolErr := h.LoadPoolMatches(compID)
	if poolErr != nil {
		return poolErr
	}
	if m, ok := findPoolMatch(poolMatches, matchID); ok {
		isPoolMatch = true
		sideA, sideB = m.SideA, m.SideB
		rawIDA, rawIDB = m.SideAID, m.SideBID
	}

	// Loaded once, reused below both for this match's OWN identity (bracket
	// half only) and for the bracket-vs-bracket comparison loop.
	bracket, berr := h.LoadBracket(compID)
	if berr != nil {
		return berr
	}

	if !isPoolMatch {
		if bm := findBracketMatchInBracket(bracket, matchID); bm != nil {
			sideA, sideB = bm.SideA, bm.SideB
			rawIDA, rawIDB = bm.SideAID, bm.SideBID
		}
		if sideA == "" && sideB == "" {
			// Not found in the bracket read above (a genuinely unknown
			// matchID): fall back to the general resolver, matching the
			// previous behaviour.
			var err error
			sideA, sideB, err = e.lookupMatchSides(h, compID, matchID)
			if err != nil {
				return err
			}
		}
		// The row's OWN ids are preferred (above); the roster name scan only
		// fills a side this match's own row did not stamp.
		if rawIDA == "" || rawIDB == "" {
			fallbackA, fallbackB := resolvePlayerIDs(h, compID, sideA, sideB)
			if rawIDA == "" {
				rawIDA = fallbackA
			}
			if rawIDB == "" {
				rawIDB = fallbackB
			}
		}
	}
	if sideA == "" && sideB == "" {
		return nil
	}

	// playerIDFor is the IneligibleCompetitorError.PlayerID reporting value:
	// the resolved id when there is one, else the bare name -- preferring
	// SOME identifier over a blank one.
	playerIDFor := func(raw, name string) string {
		if raw != "" {
			return raw
		}
		return name
	}

	// bc-cse: the operator sentence for the gate itself (comp/bracket are
	// already the ones this call needs to name the OTHER match, m/bm below,
	// not matchID). comp is loaded LAZILY, inside the closure, not above:
	// every caller of checkSimultaneousMatchTx returns immediately on the
	// first conflict found, so this closure runs AT MOST ONCE per call, and
	// loading comp here means the common case (no conflict) never pays for
	// it at all. comp is still best-effort: OperatorMatchLabel degrades to
	// the pool's own name on a nil comp (no League-heading override), so a
	// load failure still gates the write correctly, only the sentence's
	// match name gets plainer -- but the error is logged rather than
	// discarded, per the errcheck rule.
	simultaneousReason := func(name, otherMatchID, court string) string {
		comp, err := h.LoadCompetition(compID)
		if err != nil {
			log.Printf("engine: checkSimultaneousMatchTx: LoadCompetition compId=%s: %v (label degrades to pool-phase-only)", compID, err)
		}
		label := OperatorMatchLabel(comp, bracket, otherMatchID)
		return fmt.Sprintf("%s is fighting now in %s on Shiaijo %s. Finish that match first.", name, label, court)
	}

	for _, m := range poolMatches {
		if m.ID == matchID || m.Status != state.MatchStatusRunning {
			continue
		}
		if rawIDA != "" && (m.SideAID == rawIDA || m.SideBID == rawIDA) {
			return &IneligibleCompetitorError{
				PlayerID:     playerIDFor(rawIDA, sideA),
				Reason:       simultaneousReason(sideA, m.ID, m.Court),
				Simultaneous: true,
			}
		}
		if rawIDB != "" && (m.SideAID == rawIDB || m.SideBID == rawIDB) {
			return &IneligibleCompetitorError{
				PlayerID:     playerIDFor(rawIDB, sideB),
				Reason:       simultaneousReason(sideB, m.ID, m.Court),
				Simultaneous: true,
			}
		}
	}

	if bracket != nil {
		checkBracketMatch := func(bm *state.BracketMatch) error {
			if sideA != "" && matchesBracketSide(rawIDA, sideA, bm) {
				return &IneligibleCompetitorError{
					PlayerID:     playerIDFor(rawIDA, sideA),
					Reason:       simultaneousReason(sideA, bm.ID, bm.Court),
					Simultaneous: true,
				}
			}
			if sideB != "" && matchesBracketSide(rawIDB, sideB, bm) {
				return &IneligibleCompetitorError{
					PlayerID:     playerIDFor(rawIDB, sideB),
					Reason:       simultaneousReason(sideB, bm.ID, bm.Court),
					Simultaneous: true,
				}
			}
			return nil
		}
		for ri := range bracket.Rounds {
			for mi := range bracket.Rounds[ri] {
				bm := &bracket.Rounds[ri][mi]
				if bm.ID == matchID || bm.Status != state.MatchStatusRunning {
					continue
				}
				if err := checkBracketMatch(bm); err != nil {
					return err
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != matchID && bm.Status == state.MatchStatusRunning {
			if err := checkBracketMatch(bm); err != nil {
				return err
			}
		}
	}

	return nil
}

// findBracketMatchInBracket returns a pointer to the bracket match with the
// given ID -- rounds first, then the ThirdPlaceMatch sibling -- or nil. The
// engine package's own copy of the same walk state.findBracketMatchByID does
// internally (unexported there, so this package cannot reach it).
func findBracketMatchInBracket(bracket *state.Bracket, matchID string) *state.BracketMatch {
	if bracket == nil {
		return nil
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			if bracket.Rounds[ri][mi].ID == matchID {
				return &bracket.Rounds[ri][mi]
			}
		}
	}
	if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
		return bracket.ThirdPlaceMatch
	}
	return nil
}

// matchesBracketSide reports whether a running bracket match candidate bm
// includes the competitor identified by (rawID, name) on either of its own
// sides -- checkSimultaneousMatchTx's bracket-vs-bracket comparison
// (bc-brid).
//
// Prefers an id match when rawID and at least one of bm's own side ids are
// known. When rawID is known AND BOTH of bm's side ids are known but neither
// equals it, the two are PROVABLY different competitors and this returns
// false outright -- it never falls through to a name comparison in that
// case, which is exactly the false "already fighting" block two same-named,
// different-dojo competitors used to trip (the defect bc-brid fixes). Only
// when there is not enough id information to be sure (this side's id is
// unknown, or bm's row is not fully stamped -- an unrepaired legacy row, or
// a side that is a bye/unresolved feeder) does it fall back to comparing
// names, the same tolerance every other identity-critical bracket reader
// keeps for an unrepaired row.
func matchesBracketSide(rawID, name string, bm *state.BracketMatch) bool {
	if rawID != "" && (bm.SideAID == rawID || bm.SideBID == rawID) {
		return true
	}
	if rawID != "" && bm.SideAID != "" && bm.SideBID != "" {
		return false
	}
	return bm.SideA == name || bm.SideB == name
}

// checkCourtExclusivityTx is the court-exclusivity entry point for callers that
// know only the match id: it resolves the court via the tx, then runs the gate.
// Callers that already HOLD the court (and often the loaded slices too) skip
// this and call courtFreeInCompTxWith directly — an in-tx lookupMatchCourtTx
// walk is not free, since tx loads bypass the file cache and are therefore real
// disk reads taken under the write lock.
func (e *Engine) checkCourtExclusivityTx(tx state.StoreTx, compID, matchID string) error {
	court, err := lookupMatchCourtTx(tx, compID, matchID)
	if err != nil {
		return err
	}
	return courtFreeInCompTxWith(tx, compID, matchID, court, nil, nil)
}

// courtFreeInCompTxWith is the same-competition half of the court-exclusivity
// gate: it reports whether any match in compID's own pool or bracket OTHER than
// matchID is already running on court. Returns *CourtBusyError (HTTP 409
// court_busy) when the court is taken; a match with no court assigned is never
// gated.
//
// The cross-competition check is intentionally omitted here: calling
// store.RunningMatchOnCourt (which acquires read locks on other competitions)
// while holding compID's write lock via WithTransaction risks a circular-wait
// deadlock if another competition is simultaneously in its own WithTransaction.
// The cross-competition check is performed by CheckCrossCompCourtBusy before
// WithTransaction is entered.
//
// It REUSES pool matches and/or a bracket the caller already loaded in the same
// transaction, loading only the slice it wasn't handed (mp-gmcg review E4: the
// reopen path's findMatchHome has already loaded these under the same lock, so
// re-loading them for the court scan is pure waste). A nil argument means "load
// it" — and crucially, findMatchHome SWALLOWS a pool-load error (it still tries
// the bracket), so a nil poolMatches here forces an authoritative reload that
// SURFACES a genuine load failure rather than silently skipping pool matches in
// the scan.
//
// The reopen path needs this gate for a specific reason: reopening flips the
// match back to running, so a court that already has a running match would end
// up with TWO, wedging the exclusivity check for BOTH (the re-End of the
// reopened match and every further score write to the genuinely live bout).
// See ReopenMatch's COURT GATE note.
func courtFreeInCompTxWith(tx state.StoreTx, compID, matchID, court string, poolMatches []state.MatchResult, bracket *state.Bracket) error {
	if court == "" {
		return nil
	}
	if poolMatches == nil {
		var err error
		if poolMatches, err = tx.LoadPoolMatches(compID); err != nil {
			return err
		}
	}
	if bracket == nil {
		var err error
		if bracket, err = tx.LoadBracket(compID); err != nil {
			return err
		}
	}
	if occ := courtOccupied(poolMatches, bracket, court, matchID); occ != nil {
		// The scan is same-competition, so the occupant is in compID.
		return &CourtBusyError{Court: court, MatchID: occ.MatchID, CompID: compID}
	}
	return nil
}

func lookupMatchCourtTx(tx state.StoreTx, compID, matchID string) (string, error) {
	poolMatches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return "", err
	}
	if m, ok := findPoolMatch(poolMatches, matchID); ok {
		return m.Court, nil
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return "", err
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bm.Court, nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracket.ThirdPlaceMatch.Court, nil
		}
	}
	return "", notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// courtOccupied is the PURE court-occupancy scan (mp-gmcg review E4): given
// already-loaded pool matches and bracket, return the RUNNING match on `court`
// other than skipMatchID (pool first, then bracket rounds, then the bronze
// sibling), or nil. No I/O, so callers that already hold the loaded slices —
// the reopen path via findMatchHome — reuse them instead of re-loading. compID
// is only stamped onto the CourtOccupancy result, so it is not needed for the
// scan and is not a parameter here (the caller carries it).
func courtOccupied(poolMatches []state.MatchResult, bracket *state.Bracket, court, skipMatchID string) *state.CourtOccupancy {
	for i := range poolMatches {
		m := &poolMatches[i]
		if m.ID == skipMatchID || m.Status != state.MatchStatusRunning {
			continue
		}
		if m.Court == court {
			return &state.CourtOccupancy{MatchID: m.ID}
		}
	}
	if bracket != nil {
		for rIdx := range bracket.Rounds {
			for mIdx := range bracket.Rounds[rIdx] {
				bm := &bracket.Rounds[rIdx][mIdx]
				if bm.ID == skipMatchID || bm.Status != state.MatchStatusRunning {
					continue
				}
				if bm.Court == court {
					return &state.CourtOccupancy{MatchID: bm.ID}
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != skipMatchID && bm.Status == state.MatchStatusRunning && bm.Court == court {
			return &state.CourtOccupancy{MatchID: bm.ID}
		}
	}
	return nil
}

// resolvePlayerIDs resolves sideA/sideB (display names) against the
// competition's roster, returning "" for either side with no participant
// match (no name fallback: checkSimultaneousMatchTx's pool-vs-pool
// comparison is id-only by operator ruling bc-pnum, since a name silently
// substituted for a missing id could never legitimately equal a real
// SideAID/SideBID anyway).
func resolvePlayerIDs(h state.StoreTx, compID, sideA, sideB string) (rawIDA, rawIDB string) {
	comp, err := h.LoadCompetition(compID)
	if err != nil || comp == nil {
		return "", ""
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := h.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return "", ""
	}
	pool := combinedPlayerPool(comp.Players, participants)
	return lookupPlayerID(pool, sideA), lookupPlayerID(pool, sideB)
}

// clientWriteStamp is the caller's server-relative write stamp, or 0 when the
// caller has none to give. Variadic because the ONE caller that must always
// stamp is the /decision HTTP handler: the engine-internal pass-through and the
// suite's decision tests have no client clock, and a required parameter would
// have them all pass a meaningless 0. A stamped decision competes on timestamps
// like a score write instead of taking ApplyByTimestamp's unstamped bypass.
func clientWriteStamp(stamp []int64) int64 {
	if len(stamp) == 0 {
		return 0
	}
	return stamp[0]
}

// RecordDecisionTx auto-fills the scoreline from decision/decisionBy/encho
// and persists the result via RecordMatchResultWithIneligibilityTx. The
// canonical SideA=Aka / SideB=Shiro mapping (CLAUDE.md) is used to
// translate decisionBy → which side loses/forfeits: the winner gets the
// maru default-win fill (○○ regulation, ○ encho); the loser keeps any
// points it had already struck (FIK Art. 32, via preserveLoserScore).
//
// When the match already has a kiken/fusenpai decision recorded (the
// "undo" path, T103/CHK024) the engine enforces the
// contracts/match-decisions.md §Decision lock & undo rule: if any
// subsequent match involving either prior participant has started
// since the original decision was recorded, the engine returns
// ErrDecisionLocked unless force is true. On a successful overwrite
// where the prior loser is no longer the new loser, the prior loser's
// CompetitorStatus is restored to Eligible: true and surfaced as the
// returned status so the handler can broadcast the change.
//
// Runs the sides lookup, the T105 concurrent-kiken check, the T103
// downstream-match lock check, the match write, and the prior-loser
// eligibility restore on undo, ALL through the supplied tx, so the whole
// sequence commits under ONE per-comp lock acquire (T156).
//
// This is the canonical body: RecordDecision (eligibility.go) is a thin
// WithTransaction shim over this function — bc-twin, mirroring the
// RecordMatchResultWithIneligibility / RecordMatchResultWithIneligibilityTx
// pair in scoring.go / scoring_tx.go. Call this directly when already
// inside a WithTransaction closure (e.g. the decision HTTP handler); call
// RecordDecision otherwise.
//
// T090, T103, T156, contracts/match-decisions.md §POST /decision, bc-twin.
//
// bc-cse finding 5: force here governs ONLY the T103 downstream-match lock
// above -- a different operator confirmation from the bc-kcdg
// downstream-knockout-correction guard the underlying write applies (see
// applyBracketResultIn / guardDownstreamKnockoutCorrection). This entry
// point has always reused the SAME value for both, and its own
// RecordMatchResultWithIneligibilityTx call below still does, purely
// because every existing caller (the /decision HTTP handler, deps.go's
// ScoringEngine interface, this function's own tests) has exactly ONE
// force flag to give it and no channel to receive the reopened ids back --
// see RecordDecisionTxWithOptions for the twin that decouples the two and
// surfaces Reopened. RecordDecisionTx stays the pre-existing behaviour so
// none of those callers need to change.
func (e *Engine) RecordDecisionTx(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool, modifiedAt ...int64) (*state.MatchResult, *domain.CompetitorStatus, error) {
	return e.recordDecisionTx(tx, compID, matchID, decision, decisionBy, decisionReason, encho, force, ForceOptions{}, clientWriteStamp(modifiedAt))
}

// RecordDecisionTxWithOptions is RecordDecisionTx's bc-kcdg-aware twin
// (bc-cse finding 5). `force` still governs ONLY the T103 downstream-match
// lock (the "undo" override); `kcdgOpts` is the SEPARATE authorization for
// the bc-kcdg downstream-knockout-correction guard the underlying bracket
// write applies, and its Reopened field, when non-nil, is populated with
// the ids of every bracket match the write forced open -- the same contract
// RecordMatchResultWithIneligibility(Tx) and OverrideBracketWinner already
// give their callers, so this decision entry point no longer silently drops
// the list and leaves the caller unable to broadcast match_updated for the
// reopened matches. A caller that does not need to distinguish the two
// confirmations, or has only one flag to give, should keep calling
// RecordDecisionTx instead (source-compatible with every caller that
// predates this split).
func (e *Engine) RecordDecisionTxWithOptions(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool, kcdgOpts ForceOptions, modifiedAt ...int64) (*state.MatchResult, *domain.CompetitorStatus, error) {
	return e.recordDecisionTx(tx, compID, matchID, decision, decisionBy, decisionReason, encho, force, kcdgOpts, clientWriteStamp(modifiedAt))
}

// recordDecisionTx is the canonical body RecordDecisionTx and
// RecordDecisionTxWithOptions both delegate to, so the two never drift
// (bc-cse finding 5, mirroring bc-twin's own reasoning for keeping one write
// body per concern). kcdgOpts is the bc-kcdg ForceOptions threaded to the
// underlying RecordMatchResultWithIneligibilityTx call; modifiedAtStamp is
// the resolved clientWriteStamp value (a plain int64, since a private
// function need not preserve the exported variadic ergonomics its two
// public callers offer for their own source compatibility).
func (e *Engine) recordDecisionTx(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool, kcdgOpts ForceOptions, modifiedAtStamp int64) (*state.MatchResult, *domain.CompetitorStatus, error) {
	if decisionBy != "shiro" && decisionBy != "aka" {
		return nil, nil, validationErrorf("decisionBy must be 'shiro' or 'aka', got %q", decisionBy)
	}
	// T103: look up the prior result FIRST (ahead of the T105 concurrent
	// check below, which needs it): for a pool match this already carries
	// the generation-time SideAID/SideBID (stamped once at draw time,
	// present even before the match is ever scored, mirrors pools.go), which
	// is the identity data every id-based resolution below needs -- the
	// concurrent-kiken check, the eligibility write, and the undo-path
	// restore. Since bc-brid a bracket match's own row carries the SAME ids
	// once stamped (lookupExistingResult's bracket branch projects them via
	// bracketMatchAsResult), so this id-based resolution now applies to a
	// stamped bracket match too; only an UNSTAMPED row (a bye, an unresolved
	// feeder, or an unrepaired legacy row) still has every id below come
	// back "", falling through to each consumer's documented name-only
	// fallback.
	prior, err := e.lookupExistingResult(tx, compID, matchID)
	if err != nil {
		return nil, nil, err
	}
	sideA, sideB := prior.SideA, prior.SideB
	sideAID, sideBID := prior.SideAID, prior.SideBID

	// T105/CHK047: reject concurrent kiken, if the intended loser is
	// already ineligible from a *different* match, two operators are
	// trying to kiken the same player simultaneously. Return 409 so the
	// second operator sees the conflict before any write happens.
	//
	// Only kiken and fusenpai actually mark the loser ineligible; for
	// fusensho/daihyosen this check would surface a misleading
	// "already_ineligible" 409, the StartMatch eligibility gate is the
	// right place to reject those cases.
	//
	// loserID is resolved from the match's OWN side ids (never a name-based
	// roster scan): the shiro/aka choice already tells us definitively
	// WHICH side is withdrawing, so there is no ambiguity left to resolve --
	// unlike a name-only lookup, which would pick the first roster namesake
	// regardless of which one is actually in this match (bc-idfx repro:
	// roster Tanaka@DojoB registered before Tanaka@DojoA; Tanaka@DojoA
	// withdraws here, but a name-only check would inspect Tanaka@DojoB's
	// status instead).
	loserName := sideB
	loserID := sideBID
	if decisionBy == "aka" {
		loserName = sideA
		loserID = sideAID
	}
	if domain.IsWithdrawalDecisionStr(decision) {
		if cerr := e.checkConcurrentIneligibility(tx, compID, matchID, loserID, loserName); cerr != nil {
			return nil, nil, cerr
		}
	}
	hadPriorLoser := false
	if domain.IsWithdrawalDecisionStr(prior.Decision) {
		// losingSide, so a prior decision that itself came through
		// RecordDecisionTx -- and so already carries WinnerSide -- is
		// attributed by that authoritative hint rather than an ambiguous
		// name/ippon guess. hadPriorLoser=false (skipping the T103 lock
		// below, i.e. failing OPEN) is losingSide's answer whenever it
		// cannot attribute the loss at all; see its own doc comment for the
		// one known, narrow case that reaches.
		_, name, ok := losingSide(prior)
		hadPriorLoser = ok && name != ""
	}
	// T103: downstream-match check. The contract scope is "either
	// participant", if any subsequent match for either side has been
	// started or completed since the kiken/fusenpai, refuse the undo
	// unless force is set.
	if hadPriorLoser && !force {
		started, err := e.hasDownstreamMatchStarted(tx, compID, []string{sideA, sideB}, matchID)
		if err != nil {
			return nil, nil, err
		}
		if started {
			return nil, nil, ErrDecisionLocked
		}
	}
	// The winner gets the maru default-win fill; the withdrawing side keeps
	// whatever it had struck and the encounter keeps its prior sub-bouts
	// (FIK Art. 32 — see preserveLoserScore below).
	winIppons := domain.DefaultWinIppons(encho.On())
	result := &state.MatchResult{
		ID:             matchID,
		SideA:          sideA,
		SideB:          sideB,
		SideAID:        sideAID,
		SideBID:        sideBID,
		Decision:       decision,
		DecisionBy:     decisionBy,
		DecisionReason: decisionReason,
		Encho:          encho,
		Status:         state.MatchStatusCompleted,
		ModifiedAt:     modifiedAtStamp,
	}
	// shiro=SideB (White, left), aka=SideA (Red, right). The surviving side
	// gets the ○ default-win fill and becomes Winner. WinnerSide/WinnerID are
	// set DIRECTLY from decisionBy/the side's own id -- never inferred from
	// name or scoreline comparison -- so a same-name pairing (two "Tanaka
	// Kenji" from different dojos) is attributed by SIDE, not by a name or
	// ippon-count heuristic that goes ambiguous the instant both sides share
	// a display name (repro: Tokyo vs Osaka, 1-1 into encho, Tokyo withdraws;
	// the winner's default-win maru and the loser's one preserved struck
	// point tie the inferred ippon counts, so the old name/scoreline
	// inference credited Tokyo, the WITHDRAWER, with the win).
	if decisionBy == "shiro" {
		result.IpponsA = winIppons
		result.Winner = sideA
		result.WinnerSide = "A"
		result.WinnerID = sideAID
	} else {
		result.IpponsB = winIppons
		result.Winner = sideB
		result.WinnerSide = "B"
		result.WinnerID = sideBID
	}
	preserveLoserScore(result, prior, decisionBy)
	// kcdgOpts is the caller's own bc-kcdg authorization and is NOT derived
	// from the T103 `force` above. They answer different questions: T103's
	// force confirms undoing a kiken or fusenpai whose loser has since been
	// scheduled, while this one confirms clearing an already-played later
	// match. Feeding T103's flag in here (as the first cut did) meant an
	// operator confirming an unrelated decision-lock override silently
	// authorized a round being requeued, with no dialog naming it.
	// RecordDecisionTx therefore passes an EMPTY ForceOptions; only
	// RecordDecisionTxWithOptions callers, which have the operator's actual
	// answer, can set it, and they read back the reopened ids via
	// kcdgOpts.Reopened.
	status, err := e.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, result, kcdgOpts)
	if err != nil {
		return nil, nil, err
	}
	// T103 undo: the write above already restored whoever the withdrawal it
	// replaced had barred (restoreIfWithdrawalRemoved, called by
	// RecordMatchResultWithIneligibilityTx for every door), keeping the new
	// withdrawer, and returned the restored status in priority. A second
	// restore here read that RESTORED status as the current loser and freed
	// the new withdrawer, so there is none: the rule has one owner.
	return result, status, nil
}

// restoreEligibilityRecordedByMatch restores eligibility for every
// competitor-status ENTRY this exact match wrote (st.MatchID == matchID,
// st.Eligible == false) except keepPlayerID, the player the current write
// has just (re)confirmed ineligible for this match ("" when it confirmed
// nobody). It returns the last status it restored, or nil when it restored
// none. Its one caller is restoreIfWithdrawalRemoved, which every door that
// replaces a recorded withdrawal goes through: a withdrawal moved to the
// other side or replaced by another outcome, on /decision, /score and
// bulk-score alike, and a reopen that clears it.
//
// Restoring by the record's own MatchID -- not by re-deriving identity from
// the match's side names/ids the way an earlier version did -- is exact for
// every shape the write can take, including a same-name pairing:
// recordIneligibilityFromDecision already resolved and wrote this exact
// entry once, correctly, at kiken/fusenpai time (or refused to, for a row it
// could not resolve), so there is nothing left here to re-derive or guess.
// It also closes a starvation bug the old name/id-comparison version had:
// for a same-name pairing, the old ambiguity skip fired on every rescore of
// that match, so the prior loser stayed permanently ineligible --
// ReinstateCompetitor refuses unless Reinstateable, and neither
// kiken-voluntary nor fusenpai ever are.
//
// Every kind of withdrawal is restored, kiken-voluntary included, even
// though FIK Art. 31 bars a voluntary withdrawer from following shiai: a
// withdrawal the operator replaces or removes was a wrong entry, and a
// withdrawal that never happened bars nobody (operator ruling 2026-09-24:
// "Everything should be able to be fixed, in case of a wrong entry").
//
// PR #416 finding 2: players are visited in sorted playerID order so the
// returned status is deterministic rather than depending on Go's randomized
// map iteration order once more than one stale entry is restored in one
// call. The RESTORED player's status is what the caller returns because it
// is what the operator's UI most needs surfaced: a new loser's withdrawal is
// already conveyed by the returned MatchResult's own Decision/DecisionBy/
// Winner fields, while the restoration has no other channel.
//
// Best effort, like the eligibility write it undoes: a load or write error
// is logged and the entry skipped, never failing the score write that has
// already landed.
func (e *Engine) restoreEligibilityRecordedByMatch(tx state.StoreTx, compID, matchID, keepPlayerID string) *domain.CompetitorStatus {
	statuses, serr := tx.LoadCompetitorStatus(compID)
	if serr != nil {
		log.Printf("engine: restoreEligibilityRecordedByMatch compId=%s matchId=%s: LoadCompetitorStatus: %v", compID, matchID, serr)
		return nil
	}
	playerIDs := make([]string, 0, len(statuses))
	for playerID := range statuses {
		playerIDs = append(playerIDs, playerID)
	}
	sort.Strings(playerIDs)
	var last *domain.CompetitorStatus
	for _, playerID := range playerIDs {
		st := statuses[playerID]
		if st.MatchID != matchID || st.Eligible || playerID == keepPlayerID {
			continue
		}
		// A fresh minimal status, not the stale record with Eligible
		// flipped: Reason/Reinstateable describe why the player WAS
		// ineligible, which no longer applies once restored.
		restored := domain.CompetitorStatus{
			PlayerID:   playerID,
			Eligible:   true,
			MatchID:    matchID,
			RecordedAt: time.Now().UTC(),
		}
		if werr := tx.SetCompetitorStatus(compID, restored); werr != nil {
			log.Printf("engine: restoreEligibilityRecordedByMatch compId=%s matchId=%s: restoring playerId=%s: %v", compID, matchID, playerID, werr)
			continue
		}
		last = &restored
	}
	return last
}

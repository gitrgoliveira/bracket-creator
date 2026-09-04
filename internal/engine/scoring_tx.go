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
// checkConcurrentIneligibility, hasDownstreamMatchStarted, and
// restoreCompetitorEligibility (all live in eligibility.go now, taking `h`).
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

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// topNFinisher pairs a top-N finisher's IDENTITY key (standingsPlayerKey:
// id-preferring, name fallback) with their bare display name. The mp-e2k1
// displaced-qualifier guard below needs both: membership in the pre/post
// top-N sets must be decided by identity (two competitors sharing a display
// name from different dojos are explicitly legal, CheckDuplicateEntriesByNameDojo,
// so a bare-name set would not notice a re-score swapping WHICH namesake
// holds a qualifying rank), while hasStartedKnockoutMatchTx's bracket lookup
// only has names to match against (BracketMatch carries no per-side id), so
// the reported Finisher must still be a name.
type topNFinisher struct {
	key  string
	name string
}

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
func (e *Engine) RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	result.ID = matchID

	// Engi dispatch seam (tx-aware): a flag-scored competition records via the
	// engi slice through the SAME tx so the write stays inside the caller's
	// single per-comp lock acquire. Engi has no eligibility concept, so the
	// status return is nil.
	comp, loadErr := tx.LoadCompetition(compID)
	if loadErr != nil {
		return nil, fmt.Errorf("RecordMatchResultWithIneligibilityTx: load competition %s: %w", compID, loadErr)
	}
	if comp != nil && comp.Engi {
		rec, recErr := e.recordEngiMatchResult(tx, compID, matchID, result.FlagsA, result.FlagsB, result.CorrectionReason)
		if recErr != nil {
			return nil, recErr
		}
		backfillEngiResult(result, rec)
		return nil, nil
	}

	if err := applyHansokuIppons(result); err != nil {
		return nil, err
	}
	deriveDaihyosenWinner(result)

	// Capture the prior result so we can roll back the score on
	// AlreadyIneligibleError. lookupExistingResult reads directly from
	// the tx so it sees the state INSIDE the lock (the on-disk state
	// hasn't moved under us, we hold the lock).
	prior, _ := e.lookupExistingResult(tx, compID, matchID)

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

	// mp-e2k1: For mixed competitions, capture the pre-write standings for
	// the match's pool so we can compare after the write and detect whether
	// any qualifying finisher would be displaced from a started knockout match.
	// We only need this for re-scores (prior != nil) in mixed comps.
	// NOTE: the engi early-return above ensures this block only runs for
	// non-engi competitions, so compIsEngi is always false here and is not
	// tracked as a variable.
	var (
		poolRescoredName string         // pool this match belongs to (empty = not a pool match)
		oldTopN          []topNFinisher // qualifying finishers BEFORE the write, keyed by identity
		poolWinners      int            // EffectivePoolWinners, captured so the post-write block needn't reload the comp
	)
	if prior != nil {
		// mp-e2k1: reuse the comp already loaded (and error-checked) at the
		// engi-dispatch above rather than re-reading config.md from disk — the
		// tx sees no pending config write, so a reload would just re-parse the
		// same bytes. The load error is already returned there, so this path is
		// still fail-closed; a nil comp skips the mixed guard as before.
		if comp != nil && comp.Format == state.CompFormatMixed {
			// Only actual pool matches ("Pool X-…") can change pool finishers.
			// Gate on IsPoolMatchID so a knockout re-score ("m-rN-i"), whose ID
			// would otherwise parse as a pool via poolNameFromMatchID's trailing
			// "-<digits>" rule, skips the standings pre-read entirely.
			if pn, ok := poolNameFromMatchID(matchID); ok && IsPoolMatchID(matchID) {
				poolRescoredName = pn
				poolWinners = comp.EffectivePoolWinners()
				// Fail closed: if we can't establish the pre-write finishers we
				// can't prove the re-score is safe, so abort before writing
				// anything (nothing is staged yet, so returning aborts cleanly).
				preStandings, sErr := e.computeStandingsFrom(tx, compID)
				if sErr != nil {
					return nil, fmt.Errorf("mp-e2k1: pre-write standings for %s pool %q: %w", compID, pn, sErr)
				}
				ps := preStandings[pn]
				for i := 0; i < poolWinners && i < len(ps); i++ {
					p := ps[i].Player
					oldTopN = append(oldTopN, topNFinisher{key: standingsPlayerKey(p.ID, p.Name), name: p.Name})
				}
			}
		}
	}

	sideMismatch, err := e.writeToPoolOrBracket(tx, compID, matchID, result, matchWriteForward)
	if err != nil {
		return nil, err
	}
	if sideMismatch {
		// Match identity is fixed at generation; a score payload naming
		// different competitors is rejected (HTTP 409) rather than allowed to
		// overwrite the stored pairing. Returns before any side-effect write.
		return nil, ErrMatchSideMismatch
	}

	// mp-e2k1 guard: after the pool-match write, check whether any
	// qualifying finisher changed. If a displaced finisher already appears
	// in a started/completed bracket match, reject the re-score.
	if err == nil && poolRescoredName != "" && len(oldTopN) > 0 {
		// Fail closed on any verification-read failure past this point: the
		// forward write is already staged, so we restore prior before returning
		// the error, never silently commit a re-score we couldn't prove safe.
		postStandings, sErr := e.computeStandingsFrom(tx, compID)
		if sErr != nil {
			e.rollbackMatchResultTx(tx, compID, matchID, prior)
			return nil, fmt.Errorf("mp-e2k1: post-write standings for %s pool %q: %w", compID, poolRescoredName, sErr)
		}
		ps := postStandings[poolRescoredName]
		// Build the new top-N set and find displaced finishers, keyed by
		// IDENTITY (standingsPlayerKey: id-preferring, name fallback) rather
		// than bare name. Two competitors sharing a display name from
		// different dojos are explicitly legal (CheckDuplicateEntriesByNameDojo),
		// so a re-score that swaps WHICH namesake holds a qualifying rank
		// changes the identity at that rank without changing the bare name
		// occupying it; a name-keyed newSet would see the same string still
		// present and silently miss the swap (mp-e2k1's guard exists
		// precisely to catch a qualifying finisher changing under a started
		// knockout match). poolWinners was captured pre-write, the
		// competition record can't change within this tx, so no reload is
		// needed.
		newSet := make(map[string]struct{}, poolWinners)
		for i := 0; i < poolWinners && i < len(ps); i++ {
			p := ps[i].Player
			newSet[standingsPlayerKey(p.ID, p.Name)] = struct{}{}
		}
		// displaced carries bare NAMES (not keys): hasStartedKnockoutMatchTx
		// below matches bracket sides by name only (BracketMatch carries no
		// per-side id), so a namesake swap deliberately produces an
		// over-broad name-based lookup that can match EITHER dojo's
		// occupant of that bracket slot. That is intentional: the guard
		// fails CLOSED on a namesake collision rather than silently letting
		// an identity swap through.
		var displaced []string
		for _, f := range oldTopN {
			if _, stillIn := newSet[f.key]; !stillIn {
				displaced = append(displaced, f.name)
			}
		}
		if len(displaced) > 0 {
			blockingFinisher, knockoutMatchID, hErr := e.hasStartedKnockoutMatchTx(tx, compID, displaced)
			if hErr != nil {
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
				return nil, fmt.Errorf("mp-e2k1: checking started knockout matches for %s: %w", compID, hErr)
			}
			if blockingFinisher != "" {
				// Reject: restore the prior result so the corrupting re-score
				// never lands. Within a tx, writes are in-memory WAL intents
				// coalesced last-write-wins, so this rollback supersedes the
				// forward write before Commit applies the final state. Report the
				// finisher actually sitting in the blocking match so Finisher and
				// MatchID stay consistent (matters when poolWinners > 1).
				e.rollbackMatchResultTx(tx, compID, matchID, prior)
				return nil, &DownstreamKnockoutScoredError{
					Pool:     poolRescoredName,
					Finisher: blockingFinisher,
					MatchID:  knockoutMatchID,
				}
			}
		}
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
	return status, nil
}

// rollbackMatchResultTx restores prior over a partial score-write within the
// same transaction. Shared by the two reject paths in
// RecordMatchResultWithIneligibilityTx: K3 (AlreadyIneligible) and mp-e2k1
// (downstream knockout already scored). Within a tx, writes are in-memory WAL
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
	sideA, sideB, err := e.lookupMatchSides(tx, compID, matchID)
	if err != nil {
		return err
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return err
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	ids := []string{lookupPlayerID(pool, sideA), lookupPlayerID(pool, sideB)}

	statuses, err := tx.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range ids {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible && st.MatchID != matchID {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// checkSimultaneousMatchTx is the tx-aware twin of checkSimultaneousMatch.
// Returns *IneligibleCompetitorError if either participant in matchID is
// currently Running in a different match within the same competition.
//
// Phase 2c simultaneity gate.
func (e *Engine) checkSimultaneousMatchTx(tx state.StoreTx, compID, matchID string) error {
	sideA, sideB, err := e.lookupMatchSides(tx, compID, matchID)
	if err != nil {
		return nil
	}
	if sideA == "" && sideB == "" {
		return nil
	}

	idA, idB := resolvePlayerIDsTx(tx, compID, sideA, sideB)

	poolMatches, err := tx.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == matchID || m.Status != state.MatchStatusRunning {
				continue
			}
			if sideA != "" && (m.SideA == sideA || m.SideB == sideA) {
				return &IneligibleCompetitorError{
					PlayerID: idA,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", m.ID, m.Court),
				}
			}
			if sideB != "" && (m.SideA == sideB || m.SideB == sideB) {
				return &IneligibleCompetitorError{
					PlayerID: idB,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", m.ID, m.Court),
				}
			}
		}
	}

	bracket, berr := tx.LoadBracket(compID)
	if berr == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID || bm.Status != state.MatchStatusRunning {
					continue
				}
				if sideA != "" && (bm.SideA == sideA || bm.SideB == sideA) {
					return &IneligibleCompetitorError{
						PlayerID: idA,
						Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
					}
				}
				if sideB != "" && (bm.SideA == sideB || bm.SideB == sideB) {
					return &IneligibleCompetitorError{
						PlayerID: idB,
						Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
					}
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != matchID && bm.Status == state.MatchStatusRunning {
			if sideA != "" && (bm.SideA == sideA || bm.SideB == sideA) {
				return &IneligibleCompetitorError{
					PlayerID: idA,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
				}
			}
			if sideB != "" && (bm.SideA == sideB || bm.SideB == sideB) {
				return &IneligibleCompetitorError{
					PlayerID: idB,
					Reason:   fmt.Sprintf("already fighting in match %s on court %s", bm.ID, bm.Court),
				}
			}
		}
	}

	return nil
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
// See ReopenKachinukiMatch's COURT GATE note.
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
	for _, m := range poolMatches {
		if m.ID == matchID {
			return m.Court, nil
		}
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

func resolvePlayerIDsTx(tx state.StoreTx, compID, sideA, sideB string) (string, string) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return sideA, sideB
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := tx.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return sideA, sideB
	}
	pool := combinedPlayerPool(comp.Players, participants)
	idA := lookupPlayerID(pool, sideA)
	if idA == "" {
		idA = sideA
	}
	idB := lookupPlayerID(pool, sideB)
	if idB == "" {
		idB = sideB
	}
	return idA, idB
}

// hasStartedKnockoutMatchTx reports whether any BRACKET (knockout) match
// with status running or completed currently lists one of playerNames as a
// side. This is the bracket-only counterpart of hasDownstreamMatchStarted
// (eligibility.go, called here through tx), pool matches are intentionally
// NOT scanned because a pool finisher legitimately appears in their own
// completed pool bouts, which must NOT trip the guard.
//
// mp-e2k1.
func (e *Engine) hasStartedKnockoutMatchTx(tx state.StoreTx, compID string, playerNames []string) (matchedName, matchID string, err error) {
	wantSet := make(map[string]struct{}, len(playerNames))
	for _, n := range playerNames {
		if n != "" {
			wantSet[n] = struct{}{}
		}
	}
	if len(wantSet) == 0 {
		return "", "", nil
	}
	// matchedSide returns the displaced name found on this match (a or b), or ""
	// if neither side is one of the displaced finishers. Returning the name keeps
	// the caller's error payload consistent: the reported Finisher is the one
	// actually sitting in the blocking match, not just displaced[0].
	matchedSide := func(a, b string) string {
		if _, ok := wantSet[a]; ok {
			return a
		}
		if _, ok := wantSet[b]; ok {
			return b
		}
		return ""
	}
	isStarted := func(s state.MatchStatus) bool {
		return s == state.MatchStatusRunning || s == state.MatchStatusCompleted
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		// A genuinely absent bracket is NOT an error, LoadBracket maps a
		// missing file to an empty bracket with nil error (parseBracketFile,
		// os.IsNotExist). So a non-nil error here is a real fault (corrupt
		// bracket.json, permission/IO error). Propagate it rather than treating
		// it as "no started knockout match", which would let the caller's guard
		// fail open and allow a re-score that should be blocked.
		return "", "", err
	}
	if bracket == nil {
		return "", "", nil
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			if !isStarted(bm.Status) {
				continue
			}
			if name := matchedSide(bm.SideA, bm.SideB); name != "" {
				return name, bm.ID, nil
			}
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil && isStarted(bm.Status) {
		if name := matchedSide(bm.SideA, bm.SideB); name != "" {
			return name, bm.ID, nil
		}
	}
	return "", "", nil
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
func (e *Engine) RecordDecisionTx(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	if decisionBy != "shiro" && decisionBy != "aka" {
		return nil, nil, validationErrorf("decisionBy must be 'shiro' or 'aka', got %q", decisionBy)
	}
	// T103: look up the prior result FIRST (ahead of the T105 concurrent
	// check below, which needs it): for a pool match this already carries
	// the generation-time SideAID/SideBID (stamped once at draw time,
	// present even before the match is ever scored, mirrors pools.go), which
	// is the identity data every id-based resolution below needs -- the
	// concurrent-kiken check, the eligibility write, and the undo-path
	// restore. A bracket match carries none (BracketMatch persists no
	// per-side id), so every id below is simply "" there and each
	// consumer's documented name-only fallback applies unchanged.
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
	if domain.IsKikenDecisionStr(decision) || decision == string(domain.DecisionFusenpai) {
		if cerr := e.checkConcurrentIneligibility(tx, compID, matchID, loserID, loserName); cerr != nil {
			return nil, nil, cerr
		}
	}
	priorLoser := ""
	priorLoserID := ""
	if domain.IsKikenDecisionStr(prior.Decision) || prior.Decision == string(domain.DecisionFusenpai) {
		priorLoser = loserSideName(prior)
		priorLoserID = loserPlayerID(prior)
	}
	// T103: downstream-match check. The contract scope is "either
	// participant", if any subsequent match for either side has been
	// started or completed since the kiken/fusenpai, refuse the undo
	// unless force is set.
	if priorLoser != "" && !force {
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
	status, err := e.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, result)
	if err != nil {
		return nil, nil, err
	}
	// T103: when the prior loser is no longer the new loser (decision
	// type changed away from kiken/fusenpai, or decisionBy flipped),
	// restore the prior loser's eligibility and surface the resulting
	// status so the handler can broadcast it. If the write above just
	// wrote a *new* ineligibility for the same player, that wins (the
	// player is still ineligible). Only restore when the prior loser is
	// no longer the current loser -- compared by ID when both resolve
	// (the same same-name-collision reasoning as above), else by name.
	if priorLoser != "" || priorLoserID != "" {
		newLoser := loserSideName(result)
		newLoserID := loserPlayerID(result)
		same := priorLoser == newLoser
		if priorLoserID != "" && newLoserID != "" {
			same = priorLoserID == newLoserID
		}
		if !same {
			restored, rerr := e.restoreCompetitorEligibility(tx, compID, priorLoserID, priorLoser, matchID)
			if rerr == nil && restored != nil {
				status = restored
			}
		}
	}
	return result, status, nil
}

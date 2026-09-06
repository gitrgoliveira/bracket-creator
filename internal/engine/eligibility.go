package engine

import (
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrIneligibleCompetitor is the sentinel error matched by
// errors.Is(err, engine.ErrIneligibleCompetitor). Callers use this for
// HTTP 409 mapping; the returned concrete value is an
// *IneligibleCompetitorError that carries PlayerID/Reason for the
// response body.
//
// FR-035, contracts/match-decisions.md §409.
var ErrIneligibleCompetitor = errors.New("ineligible competitor")

// IneligibleCompetitorError wraps ErrIneligibleCompetitor with the
// player that failed the eligibility check.
type IneligibleCompetitorError struct {
	PlayerID string
	Reason   string
}

func (e *IneligibleCompetitorError) Error() string {
	return fmt.Sprintf("ineligible competitor: playerId=%q reason=%q", e.PlayerID, e.Reason)
}

func (e *IneligibleCompetitorError) Is(target error) bool {
	return target == ErrIneligibleCompetitor
}

// ErrCourtBusy is the sentinel error matched by
// errors.Is(err, engine.ErrCourtBusy). The concrete value is a
// *CourtBusyError that carries Court, MatchID, and CompID.
var ErrCourtBusy = errors.New("court already has a running match")

// CourtBusyError is returned when the target court already has a running
// match. Which competitions are scanned depends on the call site:
//   - StartMatch (non-tx): scans all competitions via store.RunningMatchOnCourt.
//   - CheckCrossCompCourtBusy (pre-tx gate): scans all competitions except compID.
//   - StartMatchTx (tx path): scans only within compID, cross-competition
//     conflicts are caught by CheckCrossCompCourtBusy before the tx begins.
//
// Courts are tournament-global: one physical shiaijo can host only one match at
// a time regardless of which competition owns it.
type CourtBusyError struct {
	Court   string
	MatchID string
	CompID  string
}

func (e *CourtBusyError) Error() string {
	return fmt.Sprintf("court %q already has running match %s (competition %s)", e.Court, e.MatchID, e.CompID)
}

func (e *CourtBusyError) Is(target error) bool {
	return target == ErrCourtBusy
}

// AlreadyIneligibleError is returned by RecordDecision when the
// intended loser already carries Eligible:false from a *different*
// match, indicating two operators on different courts concurrently
// tried to kiken/fusenpai the same player (CHK047, T105, NFR-010).
type AlreadyIneligibleError struct {
	PlayerID string
	MatchID  string
	Reason   string
}

func (e *AlreadyIneligibleError) Error() string {
	return fmt.Sprintf("competitor %q already ineligible (match %s)", e.PlayerID, e.MatchID)
}

// participantIDByName resolves name to a participant id against compID's
// roster (comp.Players plus a fresh LoadParticipants read, combinedPlayerPool
// -- the same union every other name-lookup call site in this file uses).
// PR #416 finding 8: this is the LoadCompetition -> nil check ->
// LoadParticipants -> combinedPlayerPool -> lookupPlayerID sequence that used
// to be hand-copied inline in both checkConcurrentIneligibility and
// recordIneligibilityFromDecision.
//
// Returns ("", nil) -- never an error -- for the two "nothing to resolve"
// shapes: name == "" (nothing to look up) and a genuinely missing
// competition (comp == nil, e.g. deleted mid-request; a side-effect lookup
// has no config to resolve against, which is not a fault). Any other load
// error (corrupt/unreadable competition or participants data) IS propagated,
// since it means the roster could not be read at all, not that the name is
// merely absent from it; callers decide for themselves whether that is
// fatal (recordIneligibilityFromDecision, the primary write) or a
// best-effort guard to log-and-skip (checkConcurrentIneligibility).
func participantIDByName(h state.StoreTx, compID, name string) (string, error) {
	if name == "" {
		return "", nil
	}
	comp, err := h.LoadCompetition(compID)
	if err != nil {
		return "", err
	}
	if comp == nil {
		return "", nil
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := h.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return "", err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	return lookupPlayerID(pool, name), nil
}

// checkConcurrentIneligibility returns *AlreadyIneligibleError when the
// loser (identified by loserID when the caller already resolved it from the
// match's own side ids, else by loserName) already has Eligible:false from a
// different match. Returns nil on any lookup failure (non-fatal, missing
// player IDs, store errors) so a degraded-mode run doesn't break the score
// flow.
//
// loserID is resolved by the caller from the match row's own SideAID/SideBID
// (never by a name-based roster scan here): a name-only lookup here would
// silently pick the FIRST namesake registered in the roster, which is a
// DIFFERENT competitor whenever the loser shares a display name with someone
// else (repro: roster Tanaka@DojoB registered before Tanaka@DojoA; Tanaka@A
// withdraws, but a name-only resolution here would check Tanaka@B's status
// instead). loserName is used ONLY when loserID is empty (a match row with no
// stamped side ids at all, e.g. a bracket kiken -- BracketMatch carries no
// per-side id).
//
// Takes h state.StoreTx so both the transactional (RecordDecisionTx) and
// non-transactional (test) callers share one body (bc-twin follow-up):
// this is a best-effort read-only guard, not the load-bearing K2
// check-and-set (that atomicity requirement lives in
// recordIneligibilityFromDecision), so unlike that function this one
// does not need to assert h is transactional.
//
// CHK047, T105.
func (e *Engine) checkConcurrentIneligibility(h state.StoreTx, compID, matchID, loserID, loserName string) error {
	if loserID == "" && loserName == "" {
		return nil
	}
	playerID := loserID
	if playerID == "" {
		found, err := participantIDByName(h, compID, loserName)
		if err != nil {
			log.Printf("engine: checkConcurrentIneligibility resolving %q compId=%s: %v (T105 guard skipped)", loserName, compID, err)
			return nil
		}
		if found == "" {
			return nil
		}
		playerID = found
	}
	statuses, err := h.LoadCompetitorStatus(compID)
	if err != nil {
		log.Printf("engine: checkConcurrentIneligibility LoadCompetitorStatus compId=%s: %v (T105 guard skipped)", compID, err)
		return nil
	}
	if st, ok := statuses[playerID]; ok && !st.Eligible && st.MatchID != matchID {
		return &AlreadyIneligibleError{
			PlayerID: playerID,
			MatchID:  st.MatchID,
			Reason:   st.Reason,
		}
	}
	return nil
}

// CheckEligibility consults the competitor-status store for compID and
// returns *IneligibleCompetitorError for the first playerID found with
// Eligible: false; nil when all playerIDs are eligible (or unknown to
// the store, which means default-eligible per FR-034).
//
// FR-035.
func (e *Engine) CheckEligibility(compID string, playerIDs []string) error {
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range playerIDs {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// StartMatch gates the scheduled → running transition by checking
// every participant's competitor-status and ensuring that no participant
// is already Running in a different match within the same competition
// (the simultaneity gate, Phase 2c).
//
// It returns *IneligibleCompetitorError (which matches
// errors.Is(err, ErrIneligibleCompetitor)) when any participant has
// Eligible: false or is already fighting elsewhere; nil when the match
// may proceed.
//
// The status transition itself remains with the score handler, this
// method is the pre-flight gate.
//
// FR-035, T084.
func (e *Engine) StartMatch(compID, matchID string) error {
	if err := e.checkCourtExclusivity(compID, matchID, ""); err != nil {
		return err
	}
	if err := e.checkSimultaneousMatch(compID, matchID); err != nil {
		return err
	}
	ids, err := e.resolveMatchParticipantIDs(compID, matchID)
	if err != nil {
		return err
	}
	return e.checkEligibilityExcludingMatch(compID, ids, matchID)
}

// checkCourtExclusivity rejects StartMatch when the target match's court
// already has a running match anywhere in the tournament. skipCompID is
// the competition whose data the caller already holds a write lock for
// (passed to store.RunningMatchOnCourt to avoid re-locking a non-reentrant
// mutex). Pass "" when calling outside a WithTransaction body.
func (e *Engine) checkCourtExclusivity(compID, matchID, skipCompID string) error {
	court, err := e.lookupMatchCourt(compID, matchID)
	if err != nil {
		return err
	}
	if court == "" {
		return nil
	}
	occ, err := e.store.RunningMatchOnCourt(court, skipCompID)
	if err != nil {
		return err
	}
	if occ != nil && (occ.CompID != compID || occ.MatchID != matchID) {
		return &CourtBusyError{Court: court, MatchID: occ.MatchID, CompID: occ.CompID}
	}
	return nil
}

// CheckCrossCompCourtBusy checks whether the court assigned to matchID is
// currently occupied by a running match in a different competition.
// It MUST be called before entering WithTransaction for compID: calling
// store.RunningMatchOnCourt while holding a per-comp write lock risks a
// circular-wait deadlock if another competition is simultaneously in its
// own WithTransaction (both goroutines try to read-lock each other's mutex).
func (e *Engine) CheckCrossCompCourtBusy(compID, matchID string) error {
	court, err := e.lookupMatchCourt(compID, matchID)
	if err != nil || court == "" {
		return err
	}
	crossOcc, err := e.store.RunningMatchOnCourt(court, compID)
	if err != nil {
		return err
	}
	if crossOcc != nil {
		return &CourtBusyError{Court: court, MatchID: crossOcc.MatchID, CompID: crossOcc.CompID}
	}
	return nil
}

// lookupMatchCourt returns the court assigned to matchID in compID's pool
// matches or bracket. Returns "" (not an error) when the match exists but
// has no court assigned.
func (e *Engine) lookupMatchCourt(compID, matchID string) (string, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return "", err
	}
	for _, m := range poolMatches {
		if m.ID == matchID {
			return m.Court, nil
		}
	}
	bracket, err := e.store.LoadBracket(compID)
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

// checkSimultaneousMatch returns an *IneligibleCompetitorError if either
// participant in matchID is currently Running in a different match within
// the same competition. Pool matches and bracket matches are both checked.
//
// Phase 2c simultaneity gate.
func (e *Engine) checkSimultaneousMatch(compID, matchID string) error {
	sideA, sideB, err := e.lookupMatchSides(e.store, compID, matchID)
	if err != nil {
		return nil
	}
	if sideA == "" && sideB == "" {
		return nil
	}

	idA, idB := e.resolvePlayerIDs(compID, sideA, sideB)

	poolMatches, err := e.store.LoadPoolMatches(compID)
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

	bracket, berr := e.store.LoadBracket(compID)
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

func (e *Engine) resolvePlayerIDs(compID, sideA, sideB string) (string, string) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil || comp == nil {
		return sideA, sideB
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
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

// checkEligibilityExcludingMatch is like CheckEligibility but skips
// CompetitorStatus entries whose source MatchID equals excludeMatchID.
// This lets a match be re-scored (the T103 undo path) even when its
// own prior kiken/fusenpai created the ineligibility, the status was
// recorded BY that match, so it should not block writing back to it.
func (e *Engine) checkEligibilityExcludingMatch(compID string, playerIDs []string, excludeMatchID string) error {
	statuses, err := e.store.LoadCompetitorStatus(compID)
	if err != nil {
		return err
	}
	for _, pid := range playerIDs {
		if pid == "" {
			continue
		}
		if st, ok := statuses[pid]; ok && !st.Eligible && st.MatchID != excludeMatchID {
			return &IneligibleCompetitorError{PlayerID: pid, Reason: st.Reason}
		}
	}
	return nil
}

// RecordDecision auto-fills the scoreline from decision/decisionBy/encho
// and persists the result via RecordMatchResultWithIneligibility. The
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
// Returns the persisted MatchResult and the most-recent
// CompetitorStatus change (new ineligibility OR restored eligibility),
// or nil when no status change applies.
//
// Since bc-twin it is a WithTransaction shim over RecordDecisionTx
// (scoring_tx.go) — ONE body, whichever door a caller enters by. The
// full behavioural contract above now describes RecordDecisionTx; this
// wrapper's only job is acquiring the per-comp lock once for the whole
// sides-lookup + T105 check + T103 lock check + write + eligibility-
// restore sequence, mirroring RecordMatchResultWithIneligibility.
//
// NEVER call this from inside a transaction: it takes the per-competition
// lock itself and that lock is not reentrant, so it deadlocks rather than
// erroring. See the note on RecordMatchResult (scoring.go), which carries
// the full reasoning. Call RecordDecisionTx directly when already inside
// a WithTransaction closure.
//
// T090, T103, contracts/match-decisions.md §POST /decision, bc-twin.
func (e *Engine) RecordDecision(compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	var (
		result *state.MatchResult
		status *domain.CompetitorStatus
		engErr error
	)
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		result, status, engErr = e.RecordDecisionTx(tx, compID, matchID, decision, decisionBy, decisionReason, encho, force)
		// Return nil regardless: engErr is an application-level signal
		// (validation → 400, AlreadyIneligible → 409, ErrDecisionLocked →
		// 409) surfaced after the tx, and any K3 rollback has already
		// replayed the prior state INSIDE the tx, so committing persists
		// exactly what the engine settled on. Mirrors the commit contract
		// in RecordMatchResultWithIneligibility.
		return nil
	})
	if txErr != nil {
		return nil, nil, txErr
	}
	return result, status, engErr
}

// lookupExistingResult fetches the currently-persisted MatchResult for
// compID/matchID from either the pool-matches or bracket store. For
// bracket matches the BracketMatch fields are projected onto a
// MatchResult so callers (loserSideName, etc.) see a uniform shape;
// only the fields the kiken-undo path needs are populated. Returns a
// *NotFoundError when the match is unknown.
func (e *Engine) lookupExistingResult(h state.StoreTx, compID, matchID string) (*state.MatchResult, error) {
	poolMatches, err := h.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				r := poolMatches[i]
				return &r, nil
			}
		}
	}
	bracket, err := h.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					return bracketMatchAsResult(&round[i]), nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracketMatchAsResult(bracket.ThirdPlaceMatch), nil
		}
	}
	return nil, notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

// hasDownstreamMatchStarted reports whether any pool or bracket match
// other than excludeMatchID has either SideA or SideB matching one of
// playerNames AND has status running or completed. Used by the
// kiken-undo flow (T103) to enforce the decision-lock rule.
//
// Takes h state.StoreTx so both the transactional (RecordDecisionTx) and
// non-transactional (test) callers share one body (bc-twin follow-up).
func (e *Engine) hasDownstreamMatchStarted(h state.StoreTx, compID string, playerNames []string, excludeMatchID string) (bool, error) {
	wantSet := make(map[string]struct{}, len(playerNames))
	for _, n := range playerNames {
		if n != "" {
			wantSet[n] = struct{}{}
		}
	}
	if len(wantSet) == 0 {
		return false, nil
	}
	involvesAny := func(a, b string) bool {
		if _, ok := wantSet[a]; ok {
			return true
		}
		_, ok := wantSet[b]
		return ok
	}
	isStarted := func(s state.MatchStatus) bool {
		return s == state.MatchStatusRunning || s == state.MatchStatusCompleted
	}
	poolMatches, err := h.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == excludeMatchID {
				continue
			}
			if isStarted(m.Status) && involvesAny(m.SideA, m.SideB) {
				return true, nil
			}
		}
	}
	bracket, err := h.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == excludeMatchID {
					continue
				}
				if isStarted(bm.Status) && involvesAny(bm.SideA, bm.SideB) {
					return true, nil
				}
			}
		}
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID != excludeMatchID {
			if isStarted(bm.Status) && involvesAny(bm.SideA, bm.SideB) {
				return true, nil
			}
		}
	}
	return false, nil
}

// resolveMatchParticipantIDs finds the match (pool or bracket) and
// resolves SideA/SideB names to player IDs via the competition's
// participants list.
func (e *Engine) resolveMatchParticipantIDs(compID, matchID string) ([]string, error) {
	sideA, sideB, err := e.lookupMatchSides(e.store, compID, matchID)
	if err != nil {
		return nil, err
	}
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		// This helper returns an error path already, so fail cleanly rather
		// than panic when the competition record is missing/deleted.
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	// Engi forces the zekken layout; make the effective flag explicit (Finding 10).
	participants, err := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return nil, err
	}
	pool := combinedPlayerPool(comp.Players, participants)
	return []string{lookupPlayerID(pool, sideA), lookupPlayerID(pool, sideB)}, nil
}

// lookupMatchSides resolves matchID's SideA/SideB names from the
// pool-matches store, falling back to the bracket store (rounds + the
// bronze sibling). Takes h state.StoreTx so both the transactional
// (RecordDecisionTx, StartMatchTx, checkSimultaneousMatchTx) and
// non-transactional (checkSimultaneousMatch, resolveMatchParticipantIDs,
// test) callers share one body (bc-twin follow-up).
func (e *Engine) lookupMatchSides(h state.StoreTx, compID, matchID string) (string, string, error) {
	poolMatches, err := h.LoadPoolMatches(compID)
	if err == nil {
		for _, m := range poolMatches {
			if m.ID == matchID {
				return m.SideA, m.SideB, nil
			}
		}
	}
	bracket, err := h.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if bm.ID == matchID {
					return bm.SideA, bm.SideB, nil
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracket.ThirdPlaceMatch.SideA, bracket.ThirdPlaceMatch.SideB, nil
		}
	}
	return "", "", notFoundErrorf("match %q not found in competition %q", matchID, compID)
}

func lookupPlayerID(players []domain.Player, name string) string {
	if name == "" {
		return ""
	}
	for _, p := range players {
		if p.Name == name {
			return p.ID
		}
	}
	return ""
}

// combinedPlayerPool merges comp.Players and freshly-loaded participants
// into a single []domain.Player suitable for lookupPlayerID. Several
// engine code paths need to resolve a Name → ID against both the
// in-memory competition snapshot and the participants.csv on disk
// (the two can diverge briefly during config edits).
//
// After T154, both inputs are already []domain.Player; the function
// just concatenates them (NFR-007).
func combinedPlayerPool(compPlayers []domain.Player, participants []domain.Player) []domain.Player {
	out := make([]domain.Player, 0, len(compPlayers)+len(participants))
	out = append(out, compPlayers...)
	out = append(out, participants...)
	return out
}

// recordIneligibilityFromDecision is the T085 engine-side side effect.
// When a top-level match result records a kiken or fusenpai decision,
// the losing player becomes ineligible for subsequent matches in this
// competition.
//
// The losing side is resolved by losingSide, the ONE owner of "which side
// lost" (PR #416 findings 4/5): result.WinnerSide first (the operator's own
// explicit choice, stamped directly by RecordDecisionTx), then
// domain.AttributeWinnerSide (id-preferred, name fallback -- the same
// attribution every other winner-side consumer in this codebase uses), then
// the legacy ippon-emptiness heuristic. losingSide replaced two independent,
// narrower spellings of the same question (loserSideName's name/ippon-only
// view and loserPlayerID's resolveWinnerSide-based id view) that never
// consulted WinnerSide at all, so a same-name pairing whose loser was already
// pinned by WinnerSide could still fall through to an ambiguous name/id
// tie-break that silently assumed side A won and marked the WINNER
// ineligible instead of the loser.
//
// The losing player's id is preferred straight from losingSide's own id
// return, and falls back to a name-based roster scan (lookupPlayerID)
// whenever losingSide resolves the SIDE but that side's own id field happens
// to be empty (a partially-stamped row, e.g. one side generated before ids
// existed) -- the side is not in doubt there, only its id is missing, so
// falling back to the name losingSide already returned is a legitimate
// resolution of THAT side, not a guess among candidates. A name-only
// resolution silently picks the FIRST namesake registered in the roster --
// a DIFFERENT competitor whenever the actual loser shares a display name
// with someone else (repro: roster Tanaka@DojoB registered before
// Tanaka@DojoA; Tanaka@DojoA withdraws, but the old name-only resolution
// wrote Eligible:false for Tanaka@DojoB instead, and her own next match then
// incorrectly 409'd) -- which is why the fallback is gated on the side being
// known, not merely on the id being empty.
//
// A row that DOES carry ids but whose losing SIDE is still genuinely
// unresolved (losingSide's ok=false case -- reachable via bulk-score, which
// writes a decision directly and never runs RecordDecisionTx's
// decisionBy-based WinnerSide stamp, so a same-name pairing can reach here
// with no WinnerID stamped either) is REJECTED with a *ValidationError
// rather than falling back to the same first-namesake guess: the row's own
// ids already prove there is more than one candidate and no way to tell
// which side lost, so guessing from a name-only scan would silently mark the
// wrong one exactly as in the no-ids repro above, just reached through a
// different gap.
//
// Returns the persisted CompetitorStatus when a status was written
// (so the handler layer can broadcast the corresponding
// `competitor-status-updated` SSE event), (nil, nil) when no status change
// applies (non-kiken/fusenpai decision, unresolvable loser, or unknown
// player), or a non-nil error when the row's own ids prove the loser
// ambiguous.
//
// FR-036, contracts/match-decisions.md §side-effects.
func (e *Engine) recordIneligibilityFromDecision(h state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error) {
	if result == nil {
		return nil, nil
	}
	if !domain.IsKikenDecisionStr(result.Decision) && result.Decision != string(domain.DecisionFusenpai) {
		return nil, nil
	}
	playerID, loser, ok := losingSide(result)
	if !ok {
		if result.SideAID != "" || result.SideBID != "" {
			// The row DOES carry side ids, but losingSide could not tell
			// which SIDE lost (e.g. a same-name pairing decided via
			// bulk-score, which never runs RecordDecisionTx's decisionBy-based
			// WinnerSide stamp and so never stamps a disambiguating
			// WinnerID either). Falling back to a name-only roster scan here
			// would silently pick the FIRST namesake registered -- a
			// DIFFERENT competitor whenever the actual loser is not that one
			// -- so this is rejected outright rather than guessed.
			return nil, validationErrorf("match %s: cannot resolve the losing side's identity for a %s decision (side ids present but ambiguous, e.g. a same-name pairing with no winnerId)", matchID, result.Decision)
		}
		return nil, nil
	}
	if playerID == "" {
		// losingSide resolved WHICH side lost, but that side's own id field
		// is empty on this row (a partially-stamped row, e.g. one side
		// generated before ids existed). The side is not in doubt, only its
		// id is missing, so a name-based roster scan for THAT side's name is
		// a legitimate resolution of it, not a guess among candidates.
		//
		// Unlike checkConcurrentIneligibility's best-effort guard, this is the
		// PRIMARY eligibility write, so a load/parse error is propagated
		// rather than logged-and-skipped (participantIDByName's own contract:
		// ("", nil) only for "competition/name genuinely not found").
		found, err := participantIDByName(h, compID, loser)
		if err != nil {
			return nil, err
		}
		if found == "" {
			return nil, nil
		}
		playerID = found
	}
	status := domain.CompetitorStatus{
		PlayerID:      playerID,
		Eligible:      false,
		Reinstateable: result.Decision == string(domain.DecisionKikenInjury),
		Reason:        fmt.Sprintf("%s at %s", result.Decision, matchID),
		MatchID:       matchID,
		RecordedAt:    time.Now().UTC(),
	}
	// K2/CHK047: check-and-set — the load, the check and the write MUST happen
	// under one per-competition lock acquire, or two concurrent kiken writes on
	// the same player from different matches can both pass the check (TOCTOU).
	// That atomicity is the HANDLE's: every caller reaches this inside a
	// WithTransaction (the HTTP handlers wrap their whole write; the non-tx
	// engine entry points RecordMatchResult / RecordMatchResultWithIneligibility
	// are now WithTransaction shims for exactly this reason), so h is a live tx
	// and the sequence below serializes on the already-held lock. Do NOT call
	// this with e.store as the handle: each call would take its own lock and
	// the TOCTOU window this closes (K2) would reopen.
	//
	// That rule is now CHECKED, not just stated. It used to be unpinnable:
	// unwrapping either entry shim to pass e.store bare left the whole suite
	// green, including TestRecordDecision_ConcurrentKiken and its siblings,
	// which exist for precisely this property — the damage only appears under
	// an interleaving no test can schedule. Logging instead of erroring because
	// the fault is in the CALLER's plumbing, not in this operator's write:
	// failing the write would turn a latent atomicity regression into a broken
	// score entry, while the write itself is still individually correct. The
	// log is what a test can assert on, and what a maintainer sees.
	if !state.IsTransactional(h) {
		log.Printf("engine: K2 check-and-set for competitor %q in %q is running on a NON-transactional store handle; "+
			"the load/check/set is no longer atomic and concurrent withdrawals can both pass the check. "+
			"The caller must wrap this in Store.WithTransaction (see the engine's entry-point shims)",
			playerID, compID)
	}
	statuses, err := h.LoadCompetitorStatus(compID)
	if err != nil {
		return nil, err
	}
	if st, ok := statuses[playerID]; ok && !st.Eligible && st.MatchID != matchID {
		return nil, &AlreadyIneligibleError{
			PlayerID: playerID,
			MatchID:  st.MatchID,
			Reason:   st.Reason,
		}
	}
	if err := h.SetCompetitorStatus(compID, status); err != nil {
		return nil, err
	}
	return &status, nil
}

// loserSideName returns the name of the losing side for a
// kiken/fusenpai. It prefers result.Winner (the canonical surviving
// side, set by the score handler after T077 validation) and falls
// back to the ippon-count heuristic only when Winner is unset.
//
// Returns "" when neither path is conclusive, callers must treat
// that as "no ineligibility recorded" and the operator will need to
// fix the request shape before the eligibility gate works.
func loserSideName(result *state.MatchResult) string {
	if result.Winner != "" {
		switch result.Winner {
		case result.SideA:
			return result.SideB
		case result.SideB:
			return result.SideA
		}
	}
	aEmpty := len(result.IpponsA) == 0
	bEmpty := len(result.IpponsB) == 0
	switch {
	case aEmpty && !bEmpty:
		return result.SideA
	case !aEmpty && bEmpty:
		return result.SideB
	}
	return ""
}

// losingSide is the ONE owner of "which side lost" attribution for a
// kiken/fusenpai/withdrawal decision (PR #416 findings 4/5). It replaces
// three independent spellings of the same question that used to live in
// loserSideName (name/ippon-only), loserPlayerID (id-preferred via
// resolveWinnerSide, which never consulted result.WinnerSide), and
// RecordDecisionTx's own decisionBy-derived loserName/loserID -- none of
// which ever read result.WinnerSide, even though RecordDecisionTx stamps it
// on every decision it records. On a same-name pairing whose loser was
// already pinned by WinnerSide but had no disambiguating WinnerID, the old
// loserPlayerID fell through to an ambiguous id/name tie-break that silently
// assumed side A won, so recordIneligibilityFromDecision could mark the
// actual WINNER ineligible instead of the loser.
//
// Preference order, each step trusted completely once it applies:
//
//  1. result.WinnerSide ("A" or "B") -- the one explicit, unambiguous hint a
//     producer stamps directly from the operator's own choice (e.g.
//     RecordDecisionTx from decisionBy), never inferred. Authoritative over
//     every other field on the row, including a contradicting Winner name
//     or WinnerID (which would only arise from stale/inconsistent data).
//  2. domain.AttributeWinnerSide -- the SAME id-preferred/name-fallback
//     attribution every other winner-side consumer in this codebase uses
//     (standings, hantei placement, team aggregation). Ids win over names
//     when they disagree; see that function's own doc comment.
//  3. The legacy ippon-emptiness heuristic (loserSideName's fallback: one
//     side has struck ippons, the other has none) for a row with no
//     winner-attribution data at all.
//
// ok reports whether the loss could be attributed; false replaces the old
// sideUnresolved/"" signals. A caller like recordIneligibilityFromDecision
// that needs to tell "no identity data at all" (safe no-op) from "ids
// present but genuinely ambiguous" (reject) still does so from
// result.SideAID/SideBID directly, exactly as before -- that distinction is
// about what the CALLER does with an unresolved loss, not about how the loss
// itself is attributed, so it stays out of this function.
func losingSide(result *state.MatchResult) (id, name string, ok bool) {
	switch result.WinnerSide {
	case "A":
		return result.SideBID, result.SideB, true
	case "B":
		return result.SideAID, result.SideA, true
	}
	switch domain.AttributeWinnerSide(domain.WinnerAttribution{
		WinnerID: result.WinnerID, SideAID: result.SideAID, SideBID: result.SideBID,
		Winner: result.Winner, SideA: result.SideA, SideB: result.SideB,
	}) {
	case domain.MatchSideA:
		return result.SideBID, result.SideB, true
	case domain.MatchSideB:
		return result.SideAID, result.SideA, true
	}
	switch loser := loserSideName(result); loser {
	case "":
		return "", "", false
	case result.SideA:
		return result.SideAID, result.SideA, true
	case result.SideB:
		return result.SideBID, result.SideB, true
	default:
		return "", "", false
	}
}

// ReinstateCompetitor restores eligibility for a competitor who was
// withdrawn via kiken-injury (FIK Art. 30). The status must exist,
// be Eligible: false, and have Reinstateable: true (set by
// kiken-injury). Voluntary kiken (Art. 31) and fusenpai statuses
// are not reinstateable, the endpoint returns an error.
//
// The check-and-set runs under WithTransaction (K2/CHK047) to close
// the TOCTOU window between reading the Reinstateable flag and writing
// the reinstated status.
func (e *Engine) ReinstateCompetitor(compID, playerID string) (*domain.CompetitorStatus, error) {
	if playerID == "" {
		return nil, validationErrorf("playerID is required")
	}
	var out *domain.CompetitorStatus
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		statuses, err := tx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		st, ok := statuses[playerID]
		if !ok || st.Eligible {
			return validationErrorf("competitor %q is not ineligible", playerID)
		}
		if !st.Reinstateable {
			return validationErrorf("competitor %q is not reinstateable (voluntary kiken or fusenpai)", playerID)
		}
		status := domain.CompetitorStatus{
			PlayerID:   playerID,
			Eligible:   true,
			MatchID:    st.MatchID,
			Reason:     fmt.Sprintf("reinstated (was: %s)", st.Reason),
			RecordedAt: time.Now().UTC(),
		}
		if err := tx.SetCompetitorStatus(compID, status); err != nil {
			return err
		}
		out = &status
		return nil
	})
	return out, err
}

// Package engine, match_label.go owns ONE entry point that names ANY
// match -- pool, league, Swiss round, or knockout -- the way the operator
// sees it on screen. errors.go's MatchLabel already covers the knockout
// shape (via a *ReopenedMatch* carrying Number/DisplayRound); this file
// adds the pool-phase shapes and dispatches between the two, mirroring
// web-mobile/js/viewer_utils.jsx's leagueAwareLabel/poolLabel and
// web-mobile/js/pool_ids.jsx's poolMatchNumberOf for the pool/league/Swiss
// heading, and admin_schedule_score_editor.jsx's scoreRowMatchLabel for the
// "<heading> · Match N" join. testdata/match_labels.json pins the cases
// both languages share; see its own header for the schema.
//
// bc-cse: a refusal that names a match (the ineligible-competitor sentence,
// the simultaneity gate, a reopen/court-busy refusal) needs this exact
// operator-facing spelling, not the raw match id -- "Pool A · Match 1", not
// "Pool A-0".
package engine

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// OperatorMatchLabel names matchID the way the operator sees it, PURE (no
// I/O): comp supplies Format (decides "League table" vs the pool's own
// name); bracket, when non-nil, resolves a knockout id's Number/DisplayRound
// through MatchLabel. Both may be nil/empty -- every call site here builds
// OPERATOR-FACING PROSE, never gates logic on the label, so an unresolved
// comp/bracket degrades to the bare id (MatchLabel's own fallback) rather
// than erroring.
//
// Callers already holding a live transaction (checkSimultaneousMatchTx's
// siblings, kachinuki.go's reopen/court-busy messages) load comp/bracket via
// their own `h state.StoreTx` handle and call this directly; a handler
// loads via its CompetitionStore interface (LoadCompetition/LoadBracket are
// already on it) and does the same. There is no separate "Tx" twin of this
// function: it takes no store handle at all, so bc-twin's rule (one body,
// not a hand-copied pair differing only by store handle) has nothing to
// duplicate here.
func OperatorMatchLabel(comp *state.Competition, bracket *state.Bracket, matchID string) string {
	if label, ok := poolPhaseMatchLabel(comp, matchID); ok {
		return label
	}
	return MatchLabel(bracketReopenedMatchFor(bracket, matchID))
}

// matchLabelStore is the narrow read surface OperatorMatchLabelFromStore
// needs. *state.Store satisfies it directly; mobileapp's CompetitionStore
// interface already declares both methods with the same signature, so it
// satisfies it structurally with no adapter.
type matchLabelStore interface {
	LoadCompetition(compID string) (*state.Competition, error)
	LoadBracket(compID string) (*state.Bracket, error)
}

// OperatorMatchLabelFromStore resolves matchID's operator label within
// compID by loading the competition and bracket directly from store,
// falling back to the bare matchID when the competition cannot be loaded
// (OperatorMatchLabel's own contract: operator-facing prose degrades rather
// than errors). A bracket-load failure degrades only the label (a nil
// bracket still resolves a pool-phase id; a knockout id falls back to its
// bare form) and is LOGGED rather than silently swallowed, matching every
// other read in this codebase that discards an error on purpose.
//
// The two hand-copies this replaces (mobileapp.matchLabelOrID,
// Engine.operatorMatchLabel) now both delegate here (bc-cse/11b), so the
// load-then-degrade sequence has one owner.
//
// Reads DIRECTLY from store, never through a state.StoreTx handle: do NOT
// call this from inside a Store.WithTransaction body -- a caller already
// holding compID's per-comp write lock would deadlock against the second
// acquire (the lock is non-reentrant). A caller inside a transaction
// already has its `h state.StoreTx` handle's own LoadCompetition/LoadBracket
// and should call OperatorMatchLabel directly with what it loads through
// that handle instead (see e.g. checkSimultaneousMatchTx's
// simultaneousReason closure).
func OperatorMatchLabelFromStore(store matchLabelStore, compID, matchID string) string {
	comp, err := store.LoadCompetition(compID)
	if err != nil || comp == nil {
		return matchID
	}
	bracket, err := store.LoadBracket(compID)
	if err != nil {
		log.Printf("engine: OperatorMatchLabelFromStore: LoadBracket compId=%s matchId=%s: %v (label degrades to pool-phase-only)", compID, matchID, err)
	}
	return OperatorMatchLabel(comp, bracket, matchID)
}

// poolPhaseMatchLabel handles the three pool-matches.csv-backed shapes: a
// real pool, a league table (single pool, reused generation, different
// heading -- CompFormatLeague never changes the id shape, only the
// heading), and a Swiss round (its own "Swiss-R<n>-<idx>" id shape). ok is
// false for anything else (a knockout id), which sends OperatorMatchLabel
// to the bracket branch.
func poolPhaseMatchLabel(comp *state.Competition, matchID string) (string, bool) {
	if round, isSwiss := parseSwissMatchRound(matchID); isSwiss {
		return supplementaryOrNumberedLabel(fmt.Sprintf("Round %d", round), matchID), true
	}
	if !IsPoolMatchID(matchID) {
		return "", false
	}
	heading, ok := poolNameFromMatchID(matchID)
	if !ok {
		heading = matchID
	}
	if comp != nil && comp.Format == state.CompFormatLeague {
		// leagueAwareLabel (viewer_utils.jsx): a league is one round-robin
		// table over the whole roster, so its own pool name ("Pool A", the
		// single pool the league generation reuses) is never shown.
		heading = "League table"
	}
	return supplementaryOrNumberedLabel(heading, matchID), true
}

// supplementaryOrNumberedLabel appends the daihyosen/tiebreaker qualifier
// for a supplementary bout id (scoreRowMatchLabel has no equivalent: a
// JS caller never asks for one, since poolMatchNumberOf returns 0 for a
// DH/TB id and the row simply hides its own match-number suffix -- see the
// _comment in testdata/match_labels.json), else the pool-relative match
// number ("<heading> · Match <n>"), or the bare heading when the id
// carries no numbered ordinal at all (DH/TB parse to 0 too, which is why
// the supplementary check runs FIRST).
func supplementaryOrNumberedLabel(heading, matchID string) string {
	switch {
	case IsPoolDaihyosenMatchID(matchID):
		return heading + " daihyosen"
	case IsTiebreakerMatchID(matchID):
		return heading + " tiebreaker"
	}
	n := poolPhaseMatchNumber(matchID)
	if n == 0 {
		return heading
	}
	return fmt.Sprintf("%s · Match %d", heading, n)
}

// poolPhaseMatchNumber mirrors pool_ids.jsx's poolMatchNumberOf: the bout's
// 1-based match number WITHIN its pool/round, taken from the id's own
// trailing "-<digits>" ordinal (0-based on disk, so +1 here). Returns 0 for
// a supplementary id (DH/TB) or an id with no numeric ordinal at all.
func poolPhaseMatchNumber(matchID string) int {
	if IsPoolDaihyosenMatchID(matchID) || IsTiebreakerMatchID(matchID) {
		return 0
	}
	i := strings.LastIndexByte(matchID, '-')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(matchID[i+1:])
	if err != nil {
		return 0
	}
	return n + 1
}

// bracketReopenedMatchFor resolves matchID's Number/DisplayRound through
// bracket, via the SAME bracketMatchRef every other door that names a
// knockout match to the operator uses (scoring.go), so a match this
// function names cannot disagree with a reopen/correction dialog naming the
// same match. Falls back to a bare-id ReopenedMatch (MatchLabel's own id
// fallback) when bracket is nil or does not carry matchID -- a bracket-
// shaped id from a competition with no bracket loaded, or one genuinely
// unknown to it.
func bracketReopenedMatchFor(bracket *state.Bracket, matchID string) ReopenedMatch {
	if bm := findBracketMatchInBracket(bracket, matchID); bm != nil {
		return bracketMatchRef(bm)
	}
	return ReopenedMatch{ID: matchID}
}

// operatorMatchLabel is OperatorMatchLabel's engine-internal, non-tx loader:
// callers OUTSIDE a transaction (requireBlockerHoldsCourt, which the doc
// comment on it notes holds only the court lock) resolve compID/matchID
// through the engine's own store this way rather than duplicating the
// load. Best-effort like OperatorMatchLabel's own contract: a load failure
// degrades to the bare matchID, since every call site here builds
// operator-facing prose, never gates logic on the label.
//
// There is no tx-aware twin: every current caller of this specific method
// runs outside a transaction (bc-twin's "one body, not a hand-copied pair"
// rule has nothing to duplicate until one is needed); a caller already
// holding a state.StoreTx should call OperatorMatchLabel directly with
// values loaded through that handle instead of adding one here unused.
func (e *Engine) operatorMatchLabel(compID, matchID string) string {
	return OperatorMatchLabelFromStore(e.store, compID, matchID)
}

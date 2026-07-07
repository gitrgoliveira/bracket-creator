package mobileapp

import (
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// stubCompetitionStore is a no-op implementation of CompetitionStore
// used to prove the interface compiles and is mockable in handler tests
// (T016 / NFR-002). Methods return zero values; specific tests that
// exercise behaviour will subclass or replace methods individually.
type stubCompetitionStore struct{}

func (stubCompetitionStore) LoadCompetition(string) (*state.Competition, error) {
	return nil, nil
}

func (stubCompetitionStore) LoadPoolMatches(string) ([]state.MatchResult, error) {
	return nil, nil
}

func (stubCompetitionStore) LoadBracket(string) (*state.Bracket, error) {
	return nil, nil
}

// stubTournamentLoader is a no-op implementation of TournamentLoader. Same
// rationale as stubCompetitionStore.
type stubTournamentLoader struct{}

func (stubTournamentLoader) LoadTournament() (*state.Tournament, error) {
	return nil, nil
}

// stubScoringEngine is a no-op implementation of ScoringEngine. Same
// rationale as stubCompetitionStore.
type stubScoringEngine struct{}

func (stubScoringEngine) RecordMatchResult(string, string, *state.MatchResult) error {
	return nil
}

func (stubScoringEngine) RecordMatchResultWithIneligibility(string, string, *state.MatchResult) (*domain.CompetitorStatus, error) {
	return nil, nil
}

func (stubScoringEngine) RecordMatchResultWithIneligibilityTx(state.StoreTx, string, string, *state.MatchResult) (*domain.CompetitorStatus, error) {
	return nil, nil
}

func (stubScoringEngine) StartMatchTx(state.StoreTx, string, string) error {
	return nil
}

func (stubScoringEngine) CheckCrossCompCourtBusy(string, string) error {
	return nil
}

func (stubScoringEngine) RecordDecision(string, string, string, string, string, *state.EnchoMetadata, bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	return nil, nil, nil
}

func (stubScoringEngine) RecordDecisionTx(state.StoreTx, string, string, string, string, string, *state.EnchoMetadata, bool) (*state.MatchResult, *domain.CompetitorStatus, error) {
	return nil, nil, nil
}

func (stubScoringEngine) MaybeAutoCompletePools(string) (engine.AutoCompleteOutcome, error) {
	return engine.AutoCompleteNoChange, nil
}

func (stubScoringEngine) UpdateMatchCourt(string, string, string) error {
	return nil
}

func (stubScoringEngine) OverrideBracketWinner(string, string, string, int64) error {
	return nil
}

func (stubScoringEngine) UpdateMatchTime(string, string, string) error {
	return nil
}

func (stubScoringEngine) MaybeAdvanceKachinuki(string, string) (bool, error) {
	return false, nil
}

// stubEligibilityEngine is a controllable implementation of EligibilityEngine
// for handler tests. Set Err to return an error; Status to return a status.
type stubEligibilityEngine struct {
	Status *domain.CompetitorStatus
	Err    error
}

func (s *stubEligibilityEngine) ReinstateCompetitor(string, string) (*domain.CompetitorStatus, error) {
	return s.Status, s.Err
}

// stubBroadcaster is a no-op implementation of Broadcaster. Same
// rationale as the other stubs.
type stubBroadcaster struct{}

func (stubBroadcaster) Broadcast(EventType, any) {}

// stubTeamLineupStore is a no-op implementation of TeamLineupStore.
// Same rationale as the other stubs, proves the interface is
// mockable for handler tests (Slice 7.B / T127).
type stubTeamLineupStore struct{}

func (stubTeamLineupStore) LoadTeamLineups(string) (map[string]domain.TeamLineup, error) {
	return nil, nil
}

func (stubTeamLineupStore) SetTeamLineup(string, domain.TeamLineup, int) error {
	return nil
}

func (stubTeamLineupStore) DeleteTeamLineup(string, string, int) error {
	return nil
}

func (stubTeamLineupStore) LockTeamLineupsForRound(string, int, time.Time) error {
	return nil
}

func (stubTeamLineupStore) DeleteTeamLineupForMatch(string, string, string) error {
	return nil
}

func (stubTeamLineupStore) LockTeamLineupForMatch(string, string, time.Time) error {
	return nil
}

// stubCompetitionTransactor is a no-op implementation of
// CompetitionTransactor. fn runs immediately with a nil StoreTx; tests
// that exercise the transactional path use the real *state.Store
// instead. Same rationale as the other stubs, proves the interface is
// mockable. (T156.)
type stubCompetitionTransactor struct{}

func (stubCompetitionTransactor) WithTransaction(string, func(state.StoreTx) error) error {
	return nil
}

func (stubCompetitionTransactor) WithCourtExclusivityLock(fn func() error) error {
	return fn()
}

// TestDepsInterfacesCompile is a compile-time guard that the consumer-
// boundary interfaces (deps.go) are satisfied by both the stub
// implementations above AND the production concrete types. If a method
// signature drifts on either side, this test fails to build and the
// drift is caught before any handler migration breaks at the wire.
//
// Per T016: this is the proof that the interfaces are minimal and
// mockable, any later slice that adds a method narrowly to deps.go
// must also extend the stubs above.
func TestDepsInterfacesCompile(t *testing.T) {
	// Stubs, proves the interfaces are mockable for handler tests.
	var (
		_ CompetitionStore      = stubCompetitionStore{}
		_ ScoringEngine         = stubScoringEngine{}
		_ EligibilityEngine     = &stubEligibilityEngine{}
		_ Broadcaster           = stubBroadcaster{}
		_ TeamLineupStore       = stubTeamLineupStore{}
		_ CompetitionTransactor = stubCompetitionTransactor{}
		_ TournamentLoader      = stubTournamentLoader{}
	)

	// Concrete types, proves the production types remain drop-in
	// implementations after the interface lands. (NFR-002: existing
	// concrete types must still satisfy the interfaces so wiring stays
	// drop-in across the migration.)
	var (
		_ CompetitionStore      = (*state.Store)(nil)
		_ ScoringEngine         = (*engine.Engine)(nil)
		_ EligibilityEngine     = (*engine.Engine)(nil)
		_ Broadcaster           = (*Hub)(nil)
		_ CompetitorStatusStore = (*state.Store)(nil)
		_ TeamLineupStore       = (*state.Store)(nil)
		// T156: CompetitionTransactor is the WithTransaction adapter
		// the lineup PUT migration uses. *state.Store satisfies it via
		// the Slice 6 / T155 method.
		_ CompetitionTransactor = (*state.Store)(nil)
		_ TournamentLoader      = (*state.Store)(nil)
	)
}

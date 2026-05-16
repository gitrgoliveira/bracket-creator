package mobileapp

import (
	"testing"

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

// stubScoringEngine is a no-op implementation of ScoringEngine. Same
// rationale as stubCompetitionStore.
type stubScoringEngine struct{}

func (stubScoringEngine) RecordMatchResult(string, string, *state.MatchResult) error {
	return nil
}

func (stubScoringEngine) RecordMatchResultWithIneligibility(string, string, *state.MatchResult) (*domain.CompetitorStatus, error) {
	return nil, nil
}

func (stubScoringEngine) RecordDecision(string, string, string, string, string, *state.EnchoMetadata) (*state.MatchResult, *domain.CompetitorStatus, error) {
	return nil, nil, nil
}

func (stubScoringEngine) MaybeAutoCompletePools(string) (bool, error) {
	return false, nil
}

func (stubScoringEngine) UpdateMatchCourt(string, string, string) error {
	return nil
}

func (stubScoringEngine) OverrideBracketWinner(string, string, string) error {
	return nil
}

func (stubScoringEngine) UpdateMatchTime(string, string, string) error {
	return nil
}

// stubBroadcaster is a no-op implementation of Broadcaster. Same
// rationale as the other stubs.
type stubBroadcaster struct{}

func (stubBroadcaster) Broadcast(EventType, any) {}

// TestDepsInterfacesCompile is a compile-time guard that the consumer-
// boundary interfaces (deps.go) are satisfied by both the stub
// implementations above AND the production concrete types. If a method
// signature drifts on either side, this test fails to build and the
// drift is caught before any handler migration breaks at the wire.
//
// Per T016: this is the proof that the interfaces are minimal and
// mockable — any later slice that adds a method narrowly to deps.go
// must also extend the stubs above.
func TestDepsInterfacesCompile(t *testing.T) {
	// Stubs — proves the interfaces are mockable for handler tests.
	var (
		_ CompetitionStore = stubCompetitionStore{}
		_ ScoringEngine    = stubScoringEngine{}
		_ Broadcaster      = stubBroadcaster{}
	)

	// Concrete types — proves the production types remain drop-in
	// implementations after the interface lands. (NFR-002: existing
	// concrete types must still satisfy the interfaces so wiring stays
	// drop-in across the migration.)
	var (
		_ CompetitionStore      = (*state.Store)(nil)
		_ ScoringEngine         = (*engine.Engine)(nil)
		_ Broadcaster           = (*Hub)(nil)
		_ CompetitorStatusStore = (*state.Store)(nil)
	)
}

// Package mobileapp — see deps.go for the consumer-boundary interfaces
// that handlers depend on. The intent (per Slice 0 / NFR-002) is that
// handler code reaches the store, engine, and SSE hub through these
// minimal interfaces rather than the concrete `*state.Store`,
// `*engine.Engine`, and `*Hub` types — which makes handler-level tests
// cheap (no temp dirs, no real engine wiring) and confines the
// concrete-type blast radius to the constructor.
//
// The interface methods are deliberately the SMALLEST set each handler
// family needs — adding a method here is a deliberate "I want this
// callable from an unrelated handler" signal, not a default. As more
// handlers migrate to interface-based DI in later slices, methods get
// added narrowly. Concrete types in `internal/state` and
// `internal/engine` already satisfy these interfaces by structural
// match, so wiring stays drop-in (see server.go for the constructor).
package mobileapp

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// CompetitionStore is the consumer-boundary view of state.Store used by
// handler families that need to read/write competition-level data
// (config.md, pools, brackets, schedule). Methods are added here only as
// real handler call sites need them — see server.go for the constructor
// that wires `*state.Store` through this interface.
//
// Consumers (current): handlers_match.go (the score endpoint, after T017
// migration). Other handler families continue to hold the concrete
// `*state.Store` until later slices migrate them; the concrete type
// remains a valid implementation of this interface so the migration is
// drop-in.
type CompetitionStore interface {
	// LoadCompetition returns the competition record, or (nil, nil) when
	// no record exists for this ID. Mirrors state.Store.LoadCompetition.
	LoadCompetition(id string) (*state.Competition, error)
}

// ScoringEngine is the consumer-boundary view of engine.Engine used by
// the match-score handler family. Pre-Slice-0 the handlers held a
// concrete `*engine.Engine`, which forced any handler test to spin up
// the full engine + store stack. The interface lets handler tests stub
// these calls independently.
//
// Consumers (current): handlers_match.go.
type ScoringEngine interface {
	// RecordMatchResult applies the given result to the pool match (or
	// falls through to the bracket match) for compID/matchID. Mirrors
	// engine.Engine.RecordMatchResult.
	RecordMatchResult(compID string, matchID string, result *state.MatchResult) error
	// MaybeAutoCompletePools transitions the competition's status to
	// "complete" when every pool match is done. Returns whether the
	// transition actually happened (so callers know whether to broadcast
	// EventCompetitionCompleted). Mirrors engine.Engine.MaybeAutoCompletePools.
	MaybeAutoCompletePools(compID string) (bool, error)
	// UpdateMatchCourt reassigns a match to a different court. Mirrors
	// engine.Engine.UpdateMatchCourt.
	UpdateMatchCourt(compID string, matchID string, newCourt string) error
	// OverrideBracketWinner sets the winner of a bracket match by
	// participant name (used by the admin "manual winner" flow). Mirrors
	// engine.Engine.OverrideBracketWinner.
	OverrideBracketWinner(compID string, matchID string, winnerName string) error
	// UpdateMatchTime updates a match's scheduledAt. Mirrors
	// engine.Engine.UpdateMatchTime.
	UpdateMatchTime(compID string, matchID string, scheduledAt string) error
}

// Broadcaster is the consumer-boundary view of *Hub used by handlers
// that fire SSE events on successful mutations. Defined as an interface
// so handler tests can supply a recording stub instead of running a
// real SSE hub with subscriber goroutines.
//
// Consumers (current): handlers_match.go. Other handler families will
// migrate as later slices touch them; *Hub satisfies this interface by
// structural match.
type Broadcaster interface {
	// Broadcast publishes the given event payload to every active SSE
	// subscriber. Mirrors Hub.Broadcast.
	Broadcast(eventType EventType, data any)
}

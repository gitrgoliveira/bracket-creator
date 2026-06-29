// Package mobileapp; see deps.go for the consumer-boundary interfaces
// that handlers depend on. The intent (per Slice 0 / NFR-002) is that
// handler code reaches the store, engine, and SSE hub through these
// minimal interfaces rather than the concrete `*state.Store`,
// `*engine.Engine`, and `*Hub` types; which makes handler-level tests
// cheap (no temp dirs, no real engine wiring) and confines the
// concrete-type blast radius to the constructor.
//
// The interface methods are deliberately the SMALLEST set each handler
// family needs; adding a method here is a deliberate "I want this
// callable from an unrelated handler" signal, not a default. As more
// handlers migrate to interface-based DI in later slices, methods get
// added narrowly. Concrete types in `internal/state` and
// `internal/engine` already satisfy these interfaces by structural
// match, so wiring stays drop-in (see server.go for the constructor).
package mobileapp

import (
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TournamentLoader is the consumer-boundary view of state.Store used by
// handlers that need to inspect the tournament-level configuration (e.g.
// Mode) without taking a dependency on the full store. *state.Store
// satisfies this interface by structural match.
type TournamentLoader interface {
	LoadTournament() (*state.Tournament, error)
}

// CompetitionStore is the consumer-boundary view of state.Store used by
// handler families that need to read/write competition-level data
// (config.md, pools, brackets, schedule). Methods are added here only as
// real handler call sites need them; see server.go for the constructor
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
	// LoadPoolMatches returns the pool-match results for compID.
	LoadPoolMatches(id string) ([]state.MatchResult, error)
	// LoadBracket returns the elimination bracket for compID.
	LoadBracket(id string) (*state.Bracket, error)
}

// ScoringEngine is the consumer-boundary view of engine.Engine used by
// the match-score handler family. Pre-Slice-0 the handlers held a
// concrete `*engine.Engine`, which forced any handler test to spin up
// the full engine + store stack. The interface lets handler tests stub
// these calls independently.
//
// Consumers (current): handlers_match.go, handlers_decision.go.
type ScoringEngine interface {
	// RecordMatchResult applies the given result to the pool match (or
	// falls through to the bracket match) for compID/matchID. Mirrors
	// engine.Engine.RecordMatchResult.
	RecordMatchResult(compID string, matchID string, result *state.MatchResult) error
	// RecordMatchResultWithIneligibility is the variant used by the
	// score handler when the request may have recorded a kiken or
	// fusenpai decision. The returned CompetitorStatus is non-nil only
	// when a new ineligibility was persisted; the handler uses that to
	// drive the competitor-status-updated SSE broadcast (T085/T092).
	RecordMatchResultWithIneligibility(compID string, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error)
	// RecordMatchResultWithIneligibilityTx is the tx-aware twin used by
	// the score handler under WithTransaction (T156). Same return shape
	// as RecordMatchResultWithIneligibility; calls flow through the
	// supplied StoreTx so the match-write + ineligibility-write +
	// lineup-freeze all commit under one lock acquire.
	RecordMatchResultWithIneligibilityTx(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (*domain.CompetitorStatus, error)
	// StartMatchTx is the FR-035 eligibility gate for the
	// scheduled → running transition. Returns
	// *engine.IneligibleCompetitorError when a participant is marked
	// ineligible by a *different* match. The undo path (re-scoring a
	// match that itself created the ineligibility) is permitted.
	// Score handler wraps fought/hikiwake submissions with this.
	StartMatchTx(tx state.StoreTx, compID, matchID string) error
	// CheckCrossCompCourtBusy checks whether the court for matchID is
	// already occupied by a running match in a different competition.
	// Must be called before entering WithTransaction to avoid a deadlock
	// (store.RunningMatchOnCourt acquires read locks on other competitions;
	// calling it while holding a write lock risks a circular wait).
	CheckCrossCompCourtBusy(compID, matchID string) error
	// RecordDecision auto-fills the scoreline + winner from the
	// decision/decisionBy/encho triple and persists the result. Used by
	// the dedicated POST /decision endpoint (T090). When the prior
	// result on the match already carried a kiken/fusenpai decision the
	// engine enforces the downstream-match lock (T103/CHK024); force=true
	// bypasses the lock so an operator can confirm an override.
	RecordDecision(compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error)
	// RecordDecisionTx is the tx-aware twin of RecordDecision used by
	// the decision handler under WithTransaction (T156). Same contract
	// as RecordDecision; calls flow through the supplied StoreTx.
	RecordDecisionTx(tx state.StoreTx, compID, matchID, decision, decisionBy, decisionReason string, encho *state.EnchoMetadata, force bool) (*state.MatchResult, *domain.CompetitorStatus, error)
	// MaybeAutoCompletePools transitions the competition's status to
	// "complete" when every pool match is done, or injects supplementary
	// ippon-shobu tiebreaker matches when ties are detected. Mirrors
	// engine.Engine.MaybeAutoCompletePools.
	MaybeAutoCompletePools(compID string) (engine.AutoCompleteOutcome, error)
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
	// MaybeAdvanceKachinuki runs the post-score advancement for a
	// kachinuki ("winner-stays-on") team match. No-op for non-kachinuki
	// competitions. Mirrors engine.Engine.MaybeAdvanceKachinuki.
	// FR-044, T135.
	MaybeAdvanceKachinuki(compID, matchID string) (bool, error)
}

// CompetitorStatusStore is the consumer-boundary view of state.Store
// used by handlers_eligibility.go. Mirrors the LoadCompetitorStatus /
// SetCompetitorStatus methods on *state.Store.
type CompetitorStatusStore interface {
	LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error)
	SetCompetitorStatus(compID string, status domain.CompetitorStatus) error
}

// EligibilityEngine is the consumer-boundary view of engine.Engine
// used by the reinstate handler. Separated from ScoringEngine because
// reinstatement is an eligibility concern, not a scoring concern.
type EligibilityEngine interface {
	ReinstateCompetitor(compID, playerID string) (*domain.CompetitorStatus, error)
}

// TeamLineupStore is the consumer-boundary view of state.Store used by
// handlers_lineup.go (Slice 7.B / T127). Mirrors the LoadTeamLineups /
// SetTeamLineup / DeleteTeamLineup / LockTeamLineupsForRound methods
// on *state.Store.
//
// The handler also needs the competition's TeamSize to drive
// TeamLineup.Validate, so it composes this interface with
// CompetitionStore at the registration site rather than promoting
// LoadCompetition into this minimal store interface; same pattern
// the other handler families use.
type TeamLineupStore interface {
	LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error)
	SetTeamLineup(compID string, lineup domain.TeamLineup, teamSize int) error
	DeleteTeamLineup(compID, teamID string, round int) error
	LockTeamLineupsForRound(compID string, round int, lockedAt time.Time) error
	// DeleteTeamLineupForMatch / LockTeamLineupForMatch are the
	// match-scoped twins added for per-match lineups (mp-825).
	DeleteTeamLineupForMatch(compID, teamID, matchID string) error
	LockTeamLineupForMatch(compID, matchID string, lockedAt time.Time) error
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

// CompetitionTransactor is the consumer-boundary view of
// state.Store.WithTransaction used by handler families that need to
// commit multiple file mutations under one per-comp lock acquire
// (T155/T156). Kept as a single-method interface deliberately;
// handlers that ALSO need read access compose this with the
// CompetitionStore / TeamLineupStore / etc. interfaces at the
// registration site, same pattern the other handler families use.
//
// Consumers (current): handlers_lineup.go (the PUT, T156);
// handlers_match.go (score, mp-95mg, wraps WithCourtExclusivityLock +
// WithTransaction); handlers_decision.go (decision, T156).
type CompetitionTransactor interface {
	// WithTransaction runs fn under the per-competition write lock for
	// compID. fn receives a state.StoreTx handle whose methods skip
	// re-locking; the lock is released on return (success or error).
	// Mirrors state.Store.WithTransaction.
	WithTransaction(compID string, fn func(tx state.StoreTx) error) error
	// WithCourtExclusivityLock runs fn under the store's court-exclusivity
	// mutex, which must be acquired BEFORE any per-comp lock. Use this to
	// wrap a cross-competition court-busy check + WithTransaction pair so
	// two concurrent match-starts on the same court in different
	// competitions can't both pass the check before either commits (TOCTOU).
	// Mirrors state.Store.WithCourtExclusivityLock.
	WithCourtExclusivityLock(fn func() error) error
}

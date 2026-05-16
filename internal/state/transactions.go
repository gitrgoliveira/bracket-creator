// Package state — transactions.go owns Store.WithTransaction, the
// per-competition-lock primitive that lets a handler perform several
// load + mutate + save operations against multiple files (config.md,
// pool-matches.csv, bracket.json, lineups.yaml, …) under a single
// acquire of the per-comp write lock. T155, NFR-010.
//
// Why this exists. The pre-T155 handler pattern was to call
// Update*Changed / UpdatePoolMatchByID / UpdateBracket in sequence —
// each one acquires the per-comp lock, does its work, and releases.
// Concurrent writers can sneak in between the calls and clobber a
// half-committed cross-file mutation (e.g. score writes a pool match
// AND propagates a competitor-status update AND auto-completes the
// competition: three load+save pairs that should be serialised against
// each other as one operation, not three).
//
// What "transaction" means here. Lock-level atomicity, NOT filesystem
// ACID. There is NO rollback: if fn writes file A successfully but
// fails on file B, file A stays written. The contract callers MUST
// honour is "do all your validation first, then write at the END" —
// once you start saving inside fn, finish saving inside fn. The
// trade-off is intentional: implementing real per-file rollback would
// require staging-area + commit/swap-on-success machinery that none of
// the live-tournament flows justify (the engine is the single source
// of truth and an operator can always re-key a value).
//
// Lock ordering. The per-comp lock is a sync.RWMutex; WithTransaction
// holds the WRITE lock for fn's entire duration. fn MUST call the
// load/save methods on the supplied StoreTx — those bypass re-locking.
// Calling any Store method directly from fn (e.g. s.LoadCompetition,
// s.SavePoolMatches) would deadlock because RWMutex is non-recursive.
// This mirrors the same advisory that already attaches to
// UpdateCompetitionChanged, UpdateBracket, UpdatePoolMatchByID.
package state

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// StoreTx is the transactional handle passed to fn in WithTransaction.
// Methods mirror the corresponding *Store methods but DO NOT re-acquire
// the per-competition lock — that's already held by WithTransaction.
//
// The compID parameter on each method is intentional duplication: it
// keeps StoreTx methods source-compatible with their *Store siblings,
// so the migration path for a handler is "wrap in WithTransaction +
// replace `store.` with `tx.`" with no further rewrites. The transaction
// is bound to a single competition (passed to WithTransaction), so
// passing a different compID to any StoreTx method is a programmer
// error — the implementation does NOT cross-check, but will operate on
// the requested compID without re-locking, which would mean unlocked
// I/O against a different competition's files. Don't do it.
type StoreTx interface {
	LoadCompetition(compID string) (*Competition, error)
	SaveCompetition(c *Competition) error
	LoadPoolMatches(compID string) ([]MatchResult, error)
	SavePoolMatches(compID string, matches []MatchResult) error
	LoadBracket(compID string) (*Bracket, error)
	SaveBracket(compID string, b *Bracket) error
	LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error)
	SetCompetitorStatus(compID string, status domain.CompetitorStatus) error
	LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error)
	SetTeamLineup(compID string, l domain.TeamLineup, teamSize int) error
	LoadParticipants(compID string, withZekkenName bool) ([]helper.Player, error)
}

// WithTransaction runs fn under the per-competition write lock for
// compID. fn receives a StoreTx that can call multiple load/save
// methods without re-acquiring the lock — the lock is held for the
// entire fn body and released exactly once on return (success OR
// error). Per the package-level docs: "transaction" here is
// lock-level atomicity, NOT filesystem rollback.
//
// fn MUST call methods on tx, NOT on the underlying *Store directly.
// The per-comp mutex is a non-recursive sync.RWMutex; a direct
// s.Save* call from inside fn would re-acquire and deadlock.
//
// T155, NFR-010.
func (s *Store) WithTransaction(compID string, fn func(tx StoreTx) error) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()
	return fn(&storeTx{store: s, compID: compID})
}

// storeTx implements StoreTx by delegating to the store's *Locked
// helpers — the ones that DO NOT acquire the per-comp lock. Caller
// (WithTransaction) is responsible for the lock; this type just
// dispatches.
type storeTx struct {
	store  *Store
	compID string
}

func (t *storeTx) LoadCompetition(compID string) (*Competition, error) {
	return t.store.loadCompetitionLocked(compID)
}

func (t *storeTx) SaveCompetition(c *Competition) error {
	return t.store.saveCompetitionLocked(c)
}

func (t *storeTx) LoadPoolMatches(compID string) ([]MatchResult, error) {
	return t.store.LoadPoolMatchesLocked(compID)
}

func (t *storeTx) SavePoolMatches(compID string, matches []MatchResult) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	return t.store.savePoolMatchesLocked(compID, matches)
}

func (t *storeTx) LoadBracket(compID string) (*Bracket, error) {
	return t.store.loadBracketLocked(compID)
}

func (t *storeTx) SaveBracket(compID string, b *Bracket) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	return t.store.saveBracketLocked(compID, b)
}

func (t *storeTx) LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error) {
	return t.store.loadCompetitorStatusLocked(compID)
}

func (t *storeTx) SetCompetitorStatus(compID string, status domain.CompetitorStatus) error {
	return t.store.setCompetitorStatusLocked(compID, status)
}

func (t *storeTx) LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error) {
	return t.store.loadTeamLineupsLocked(compID)
}

func (t *storeTx) SetTeamLineup(compID string, l domain.TeamLineup, teamSize int) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	return t.store.setTeamLineupLocked(compID, l, teamSize)
}

func (t *storeTx) LoadParticipants(compID string, withZekkenName bool) ([]helper.Player, error) {
	return t.store.loadParticipantsLocked(compID, withZekkenName)
}

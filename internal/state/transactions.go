// Package state, transactions.go owns Store.WithTransaction, the
// per-competition-lock primitive that lets a handler perform several
// load + mutate + save operations against multiple files (config.md,
// pool-matches.csv, bracket.json, lineups.yaml, …) under a single
// acquire of the per-comp write lock. T155, NFR-010.
//
// Why this exists. The pre-T155 handler pattern was to call
// Update*Changed / UpdatePoolMatchByID / UpdateBracket in sequence.
// each one acquires the per-comp lock, does its work, and releases.
// Concurrent writers can sneak in between the calls and clobber a
// half-committed cross-file mutation (e.g. score writes a pool match
// AND propagates a competitor-status update AND auto-completes the
// competition: three load+save pairs that should be serialised against
// each other as one operation, not three).
//
// What "transaction" means here. Lock-level atomicity AND cross-file
// crash-atomicity via a write-ahead log (T210/T211/T212). Each save
// invoked through tx is STAGED into a per-transaction WAL instead of
// landing on disk; after fn returns nil, WithTransaction commits the
// WAL (atomic-rename of the intent file into <data>/.wal/), applies
// the staged writes to their target files, then deletes the WAL.
//
// Failure modes:
//   - fn returns an error → no WAL on disk (Commit never ran);
//     intents discarded; on-disk state unchanged.
//   - Crash after fn returns but before Commit → same as fn-error:
//     intents discarded; on-disk state unchanged.
//   - Crash after Commit, before Apply completes → WAL on disk;
//     Store.NewStore Scan replays on next start; targets land.
//   - Apply returns an error mid-way → WAL stays on disk for the
//     next-start replay to finish; caller sees the error.
//
// The contract callers MUST honour is "do your validation FIRST then
// stage writes via tx", once a tx method writes, that write enters
// the WAL and will land (either at Apply time or at replay time). A
// validation failure AFTER a write returns the error AND leaves the
// staged-but-uncommitted intent to be discarded; the on-disk state
// stays unchanged. There is still NO undo log: an already-committed
// WAL can't be rolled back after Apply has partially run; the only
// recovery is forward-completion via replay.
//
// Read-your-own-writes within a tx IS supported via the WAL's
// in-memory intent map: tx-internal reads (tx.LoadCompetition,
// tx.LoadBracket, etc.) check the pending intents BEFORE going to
// disk, so a tx that saves pool-matches and then re-loads them sees
// the just-saved data, not the stale on-disk version (which won't
// update until Apply runs after fn returns). Without this, the
// existing TestWithTransaction_NestedCallDoesNotDeadlock contract
// (load-save-load round-trip) would silently see stale data.
//
// Lock ordering. The per-comp lock is a sync.RWMutex; WithTransaction
// holds the WRITE lock for fn's entire duration. fn MUST call the
// load/save methods on the supplied StoreTx, those bypass re-locking.
// Calling any Store method directly from fn (e.g. s.LoadCompetition,
// s.SavePoolMatches) would deadlock because RWMutex is non-recursive.
// This mirrors the same advisory that already attaches to
// UpdateCompetitionChanged, UpdateBracket, UpdatePoolMatchByID.
package state

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

// ErrMismatchedTxCompID is returned by StoreTx methods when the
// supplied compID (or c.ID on SaveCompetition) does not match the
// competition the transaction was opened on. The transaction holds
// the per-comp lock for one ID only, so dispatching the locked
// helpers for any other ID would perform unlocked I/O.
var ErrMismatchedTxCompID = errors.New("compID does not match transaction's competition")

// StoreTx is the transactional handle passed to fn in WithTransaction.
// Methods mirror the corresponding *Store methods but DO NOT re-acquire
// the per-competition lock, that's already held by WithTransaction.
//
// The compID parameter on each method is intentional duplication: it
// keeps StoreTx methods source-compatible with their *Store siblings,
// so the migration path for a handler is "wrap in WithTransaction +
// replace `store.` with `tx.`" with no further rewrites. The transaction
// is bound to a single competition (passed to WithTransaction); every
// StoreTx method guards the supplied compID against the bound one and
// returns ErrMismatchedTxCompID on mismatch, so a stale or wrong ID
// surfaces as a normal error instead of silently doing unlocked I/O
// against another competition's files.
type StoreTx interface {
	LoadCompetition(compID string) (*Competition, error)
	SaveCompetition(c *Competition) error
	LoadPools(compID string) ([]helper.Pool, error)
	SavePools(compID string, pools []helper.Pool) error
	LoadPoolMatches(compID string) ([]MatchResult, error)
	SavePoolMatches(compID string, matches []MatchResult) error
	LoadBracket(compID string) (*Bracket, error)
	SaveBracket(compID string, b *Bracket) error
	LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error)
	SetCompetitorStatus(compID string, status domain.CompetitorStatus) error
	LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error)
	SetTeamLineup(compID string, l domain.TeamLineup, teamSize int) error
	LoadParticipants(compID string, withZekkenName bool) ([]domain.Player, error)

	// UpdatePoolMatchByID is the tx-aware twin of
	// Store.UpdatePoolMatchByID. Same semantics, same return values; the
	// only difference is that it skips re-acquiring the per-comp lock
	// (already held by WithTransaction). Score / decision handlers (T156)
	// use this to keep their match-result write under the SAME lock
	// acquire as the competitor-status side effects.
	UpdatePoolMatchByID(compID, matchID string, mutate func(*MatchResult)) (bool, error)
	// UpdateBracket is the tx-aware twin of Store.UpdateBracket. The
	// mutate closure may modify the bracket arbitrarily and signal "match
	// not found" by returning an error (typically wrapping the engine's
	// match-not-found sentinel, see engine.withBracketMatch).
	UpdateBracket(compID string, mutate func(*Bracket) error) error
	// UpdateParticipant is the tx-aware twin of
	// Store.updateParticipantNoLock. Applies transform to the target
	// participant while the transaction lock is held, so the caller
	// can serialise a competition-status pre-check (via LoadCompetition
	// in the same tx body) and the participant write under one lock
	// acquire. Participants.csv and seeds.csv are written directly via
	// atomic rename, they are not staged through the WAL, but each
	// write is individually crash-safe, and no other WAL-staged file
	// is touched by this path, so cross-file atomicity is not required.
	UpdateParticipant(compID, pid string, withZekkenName bool, transform func(*domain.Player) error) (*domain.Player, error)
}

// WithTransaction runs fn under the per-competition write lock for
// compID. fn receives a StoreTx that can call multiple load/save
// methods without re-acquiring the lock, the lock is held for the
// entire fn body and released exactly once on return (success OR
// error).
//
// Crash-atomicity. Most saves invoked through tx are STAGED into a
// per-transaction write-ahead log (see internal/state/wal) instead
// of going straight to disk. After fn returns nil, this method
// Commits the WAL (atomic-renames the intent file into <data>/.wal/),
// then Applies the staged writes one-by-one to their target files,
// then deletes the WAL file. A crash after Commit but before all
// Applies finish leaves the WAL on disk for replay on next start
// (Store.NewStore scans the directory); a crash before Commit
// leaves no on-disk trace and the partial in-memory work is dropped.
// Multi-file transactions that previously could land file A but not
// file B now either land both or replay both, cross-file atomicity
// across a process crash (closing the v3 review A1 finding).
// Exception: UpdateParticipant is NOT WAL-staged (see WAL caveat
// below); it writes immediately and is not rolled back on error.
//
// Lock semantics. Per-comp lock is held for the entire fn body AND
// across Commit + Apply, so other writers see the WAL transition
// from "absent" → "applied" → "absent" as an atomic event.
//
// fn MUST call methods on tx, NOT on the underlying *Store directly.
// The per-comp mutex is a non-recursive sync.RWMutex; a direct
// s.Save* call from inside fn would re-acquire and deadlock.
//
// WAL caveat. StoreTx.UpdateParticipant writes directly via atomic
// rename (not through the WAL). It is crash-safe per-file but is NOT
// staged, so if fn returns an error after calling UpdateParticipant
// the participant write is NOT rolled back. Do not mix
// UpdateParticipant with WAL-staged tx saves (e.g. tx.SaveCompetition)
// and expect cross-file crash-atomicity, each write lands
// independently.
//
// fn read-after-write within the same tx. Tx-internal reads
// (tx.LoadCompetition, tx.LoadBracket, etc.) read from disk via the
// *Locked helpers and DO NOT see the WAL-staged writes, the on-disk
// file isn't updated until Apply runs after fn returns. The current
// engine paths (RecordMatchResultWithIneligibilityTx,
// RecordDecisionTx, K3 rollback) read BEFORE they write within a
// single tx and never read-after-write the same file, so this
// limitation is invisible to them. If a future tx body needs to
// read its own pending write, the WAL exposes Intents(), but that's
// a code-smell and probably indicates the load/save should be
// re-ordered.
//
// T155, NFR-010, T210/T211/T212 (A1 WAL).
func (s *Store) WithTransaction(compID string, fn func(tx StoreTx) error) error {
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	mu := s.getCompLock(compID)
	mu.Lock()
	defer mu.Unlock()

	// Begin a fresh WAL for this transaction. The WAL has no on-disk
	// footprint until Commit runs; if fn returns an error we just
	// drop the in-memory intents and return.
	w, err := wal.BeginTx(s.walDir, wal.NewWALID(), s.directWriteWAL)
	if err != nil {
		return fmt.Errorf("WithTransaction %q: BeginTx: %w", compID, err)
	}

	tx := &storeTx{store: s, compID: compID, wal: w}
	if ferr := fn(tx); ferr != nil {
		// Abort path: the WAL is in-memory only at this point
		// (Commit was never called), so there's nothing to clean
		// up on disk. But the savers DID update the file caches
		// with the staged (uncommitted) data, those caches now
		// hold a phantom value that didn't land on disk. Invalidate
		// every cache the would-be intents touched so the next
		// reader re-parses from the (untouched) on-disk file and
		// sees the pre-tx state.
		s.invalidateCachesForWALIntents(compID, w.Intents())
		return ferr
	}

	// Fast path: a tx that called nothing through the WAL writer
	// (e.g., pure read-only or a no-op save like
	// SaveCompetitionChanged returning false-changed) has no
	// intents. Skip Commit/Apply/Done, they'd just write and
	// remove an empty WAL file for nothing.
	if len(w.Intents()) == 0 {
		return nil
	}

	if err := w.Commit(); err != nil {
		return fmt.Errorf("WithTransaction %q: Commit: %w", compID, err)
	}
	if err := w.Apply(); err != nil {
		// Apply failed mid-way. The WAL is committed and remains on
		// disk; the next Store.NewStore startup will replay it.
		// Surface the error so the caller can react (e.g., HTTP
		// 500), the next process startup is what guarantees
		// completion.
		return fmt.Errorf("WithTransaction %q: Apply: %w", compID, err)
	}

	// Cache reconciliation. In WAL mode, the savers populated the
	// file caches with the in-memory result BEFORE Apply landed the
	// bytes, so cache.mtime was captured from the OLD file (or 0
	// if absent). After Apply the on-disk file has a NEW mtime, and
	// the next reader would see mtime != cache.mtime and re-parse
	// from disk, silently losing any in-memory-only fields the
	// parser doesn't know about (e.g., MatchResult.DecisionBy and
	// MatchResult.Encho, which pool-matches.csv doesn't serialize).
	// Walk the WAL intents and refresh each touched cache's mtime
	// to match the now-final file mtime so the in-memory snapshot
	// continues to win cache hits.
	s.refreshCachesAfterWALApply(compID, w.Intents())

	if err := w.Done(); err != nil {
		// Done failed but Apply succeeded, target files are in
		// the right state, the WAL file is stale. Next startup
		// will see the WAL, re-Apply (no-op for identical bytes),
		// and re-attempt Done. The transaction is effectively
		// complete; surface the error for observability but the
		// caller can treat it as success.
		slog.Warn("WithTransaction: WAL Done failed; will retry on restart",
			"comp", compID, "wal", w.ID(), "err", err)
	}
	return nil
}

// directWriteWAL is the WriteFn the WAL uses for both its OWN file
// (during Commit) and the target files (during Apply). Both go
// through Store.atomicWrite, which validates that the path stays
// inside the store's data folder before delegating to
// atomicWriteFile, so the WAL writes inherit the same write-tmp +
// fsync + rename dance AND the same path-injection sanitiser the
// rest of the package uses. Routing through s.atomicWrite (rather
// than calling atomicWriteFile directly) keeps the CodeQL
// path-injection analysis happy: the sanitiser is visible at every
// caller boundary.
func (s *Store) directWriteWAL(path string, data []byte, perm os.FileMode) error {
	return s.atomicWrite(path, data, perm)
}

// invalidateCachesForWALIntents zeroes out the file caches the
// staged-but-aborted intents would have updated, so the next reader
// re-parses from the (untouched) on-disk file. Used on the abort
// path: the savers populated the caches optimistically when fn was
// running, but Commit never happened, so the on-disk file still
// holds the pre-tx state and the cache value is now phantom.
func (s *Store) invalidateCachesForWALIntents(compID string, intents []wal.FileIntent) {
	for _, in := range intents {
		base := filepath.Base(in.Path)
		switch base {
		case "config.md",
			"pool-matches.csv",
			"bracket.json",
			competitorStatusFilename,
			teamLineupFilename:
			cache := s.getFileCache(compID, base)
			cache.mu.Lock()
			cache.data = nil
			cache.mtime = 0
			cache.mu.Unlock()
		}
	}
}

// refreshCachesAfterWALApply walks each WAL intent and re-stamps the
// matching file cache's mtime to the post-Apply on-disk mtime. Only
// touches caches that already exist (the saver populated them during
// the staged write), never creates a new cache entry from scratch.
//
// Why this is necessary. The savers update the cache with the
// in-memory struct immediately when they're called, capturing the
// PRE-write mtime. In WAL mode the actual disk write happens later
// (in Apply, after fn returns), so the recorded mtime is stale by
// the time Apply finishes. Without this refresh, the next reader
// sees cache.mtime ≠ file.mtime, re-parses from disk, and silently
// loses fields the on-disk format doesn't carry (e.g.,
// MatchResult.DecisionBy, MatchResult.Encho, pool-matches.csv
// doesn't have columns for them, so the in-memory cache is the only
// authoritative source for those fields after a save).
func (s *Store) refreshCachesAfterWALApply(compID string, intents []wal.FileIntent) {
	for _, in := range intents {
		base := filepath.Base(in.Path)
		// The cache keys we know how to refresh; if a WAL intent
		// targets a path we don't recognize, skip it (no cache to
		// refresh, the saver didn't populate one).
		switch base {
		case "config.md",
			"pool-matches.csv",
			"bracket.json",
			competitorStatusFilename,
			teamLineupFilename:
			cache := s.getFileCache(compID, base)
			cache.mu.Lock()
			if cache.data != nil {
				cache.mtime = s.FileMtime(compID, base)
			}
			cache.mu.Unlock()
		}
	}
}

// storeTx implements StoreTx by delegating to the store's *Locked
// helpers, the ones that DO NOT acquire the per-comp lock. Caller
// (WithTransaction) is responsible for the lock; this type just
// dispatches.
//
// The wal field is the per-transaction intent log: every save method
// passes wal.WriteFn() to the underlying *Locked saver, so the
// staged bytes get captured instead of landing on disk. After fn
// returns, WithTransaction Commits + Applies + Dones the WAL.
type storeTx struct {
	store  *Store
	compID string
	wal    *wal.WAL
}

// txWriteFn adapts the WAL package's WriteFn (which uses os.FileMode)
// to the state package's writeFn (which uses fs.FileMode). The two
// types are identical at the value level, fs.FileMode is an alias
// of os.FileMode, but Go's strict named-function-type rule won't
// let one pass directly where the other is expected. A trivial
// shim closure converts.
func (t *storeTx) txWriteFn() writeFn {
	walWrite := t.wal.WriteFn()
	return func(path string, data []byte, perm os.FileMode) error {
		return walWrite(path, data, perm)
	}
}

// pendingFor returns the WAL-staged bytes for the file `filename`
// under this transaction's competition directory, and ok=true; if
// no intent has been staged, ok=false. Used by the storeTx loaders
// to support read-your-own-writes within a tx body.
func (t *storeTx) pendingFor(filename string) ([]byte, bool) {
	return t.wal.PendingBytes(t.store.compPath(t.compID, filename))
}

// checkCompID enforces the transaction-bound-compID invariant. Wraps
// ErrMismatchedTxCompID with both IDs so error messages identify the
// programmer mistake unambiguously.
func (t *storeTx) checkCompID(compID string) error {
	if compID != t.compID {
		return fmt.Errorf("%w: tx=%q, got=%q", ErrMismatchedTxCompID, t.compID, compID)
	}
	return nil
}

func (t *storeTx) LoadCompetition(compID string) (*Competition, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	// Read-your-own-writes: if this tx has staged a config.md write,
	// parse the staged bytes instead of reading from disk (which
	// still holds the pre-Apply state).
	if pending, ok := t.pendingFor("config.md"); ok {
		var c Competition
		if perr := parseFrontMatter(pending, &c); perr != nil {
			return nil, perr
		}
		return t.store.copyCompetition(&c), nil
	}
	return t.store.loadCompetitionLocked(compID)
}

func (t *storeTx) SaveCompetition(c *Competition) error {
	if err := t.checkCompID(c.ID); err != nil {
		return err
	}
	return t.store.saveCompetitionLocked(c, t.txWriteFn())
}

func (t *storeTx) LoadPools(compID string) ([]helper.Pool, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	return t.store.loadPoolsLocked(compID)
}

func (t *storeTx) SavePools(compID string, pools []helper.Pool) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	return t.store.savePoolsLocked(compID, pools)
}

func (t *storeTx) LoadPoolMatches(compID string) ([]MatchResult, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	if pending, ok := t.pendingFor("pool-matches.csv"); ok {
		results, err := parsePoolMatchesBytes(pending)
		if err != nil {
			return nil, err
		}
		return t.store.copyMatchResults(results), nil
	}
	return t.store.LoadPoolMatchesLocked(compID)
}

func (t *storeTx) SavePoolMatches(compID string, matches []MatchResult) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	return t.store.savePoolMatchesLocked(compID, matches, t.txWriteFn())
}

func (t *storeTx) LoadBracket(compID string) (*Bracket, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	if pending, ok := t.pendingFor("bracket.json"); ok {
		b, err := parseBracketBytes(pending)
		if err != nil {
			return nil, err
		}
		return t.store.copyBracket(b), nil
	}
	return t.store.loadBracketLocked(compID)
}

func (t *storeTx) SaveBracket(compID string, b *Bracket) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	return t.store.saveBracketLocked(compID, b, t.txWriteFn())
}

func (t *storeTx) LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	if pending, ok := t.pendingFor(competitorStatusFilename); ok {
		return parseCompetitorStatusBytes(pending)
	}
	return t.store.loadCompetitorStatusLocked(compID)
}

func (t *storeTx) SetCompetitorStatus(compID string, status domain.CompetitorStatus) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	// Read-your-own-writes: if competitor-status.yaml is already
	// staged, merge into the staged map. Without this, two
	// SetCompetitorStatus calls in the same tx would have the
	// second one re-load the disk state (missing the first write)
	// and effectively lose the first.
	if pending, ok := t.pendingFor(competitorStatusFilename); ok {
		if err := status.Validate(); err != nil {
			return err
		}
		current, perr := parseCompetitorStatusBytes(pending)
		if perr != nil {
			return perr
		}
		if status.RecordedAt.IsZero() {
			status.RecordedAt = time.Now().UTC()
		}
		current[status.PlayerID] = status
		return t.store.saveCompetitorStatusLocked(compID, current, t.txWriteFn())
	}
	return t.store.setCompetitorStatusLocked(compID, status, t.txWriteFn())
}

func (t *storeTx) LoadTeamLineups(compID string) (map[string]domain.TeamLineup, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	if pending, ok := t.pendingFor(teamLineupFilename); ok {
		return parseTeamLineupsBytes(pending)
	}
	return t.store.loadTeamLineupsLocked(compID)
}

func (t *storeTx) SetTeamLineup(compID string, l domain.TeamLineup, teamSize int) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	// Read-your-own-writes: if lineups.yaml is already staged, merge
	// into the staged map instead of the on-disk version. Otherwise
	// the staged write would be discarded on the next save.
	if pending, ok := t.pendingFor(teamLineupFilename); ok {
		if err := l.ValidatePositions(teamSize); err != nil {
			return err
		}
		current, perr := parseTeamLineupsBytes(pending)
		if perr != nil {
			return perr
		}
		key := lineupStorageKey(l)
		l.CompetitionID = compID
		current[key] = l
		return t.store.saveTeamLineupsLocked(compID, current, t.txWriteFn())
	}
	return t.store.setTeamLineupLocked(compID, l, teamSize, t.txWriteFn())
}

func (t *storeTx) LoadParticipants(compID string, withZekkenName bool) ([]domain.Player, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	return t.store.loadParticipantsLocked(compID, withZekkenName)
}

func (t *storeTx) UpdateParticipant(compID, pid string, withZekkenName bool, transform func(*domain.Player) error) (*domain.Player, error) {
	if err := t.checkCompID(compID); err != nil {
		return nil, err
	}
	return t.store.updateParticipantNoLock(compID, pid, withZekkenName, transform)
}

// UpdatePoolMatchByID dispatches to a lock-free body that mirrors
// Store.UpdatePoolMatchByID's load + find + mutate + save sequence.
// Caller (WithTransaction) is responsible for the per-comp lock.
//
// Read-your-own-writes: if pool-matches.csv has been staged earlier
// in this tx (e.g., the K3 rollback path that writes the new score,
// fails eligibility, and rolls back), this load + mutate + save sees
// the staged version, not the stale on-disk version.
func (t *storeTx) UpdatePoolMatchByID(compID, matchID string, mutate func(*MatchResult)) (bool, error) {
	if err := t.checkCompID(compID); err != nil {
		return false, err
	}
	if err := ValidateCompetitionID(compID); err != nil {
		return false, err
	}
	if pending, ok := t.pendingFor("pool-matches.csv"); ok {
		results, perr := parsePoolMatchesBytes(pending)
		if perr != nil {
			return false, perr
		}
		for i := range results {
			if results[i].ID == matchID {
				mutate(&results[i])
				return true, t.store.savePoolMatchesLocked(compID, results, t.txWriteFn())
			}
		}
		return false, nil
	}
	return t.store.updatePoolMatchByIDLocked(compID, matchID, mutate, t.txWriteFn())
}

// UpdateBracket dispatches to a lock-free body that mirrors
// Store.UpdateBracket's load + mutate + save sequence. Caller
// (WithTransaction) is responsible for the per-comp lock.
//
// Read-your-own-writes: see UpdatePoolMatchByID for the same
// staged-vs-disk rationale.
func (t *storeTx) UpdateBracket(compID string, mutate func(*Bracket) error) error {
	if err := t.checkCompID(compID); err != nil {
		return err
	}
	if err := ValidateCompetitionID(compID); err != nil {
		return err
	}
	if pending, ok := t.pendingFor("bracket.json"); ok {
		b, perr := parseBracketBytes(pending)
		if perr != nil {
			return perr
		}
		if err := mutate(b); err != nil {
			return err
		}
		return t.store.saveBracketLocked(compID, b, t.txWriteFn())
	}
	return t.store.updateBracketLocked(compID, mutate, t.txWriteFn())
}

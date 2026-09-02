package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-lww1. A match write the timestamp guard drops must REPORT the drop, on
// every path a write can take. That is a matrix, not a single case: a match id
// resolves to a pool match or a knockout one only at run time, the knockout half
// splits again into a round match and the bronze knockout (which lives outside
// Rounds and is reached by a separate branch), and each of those is reached
// through either of the two engine entry points a caller can pick.
//
// Note what that second axis is since bc-twin, because the cell names understate
// it. It is NOT "one body through a tx and a non-tx door" any more — there is a
// single shared body, so a mutation of the write itself fails both cells of a
// pair together. What the axis still separates is the two ENTRY POINTS around
// that body: RecordMatchResult (a WithTransaction shim) and
// RecordMatchResultWithIneligibilityTx (called with a live tx by the handlers),
// which differ in their engi dispatch, kachinuki merge and ineligibility work.
// That is worth sweeping, but it is a weaker claim than "twin parity".
//
// Sweeping the matrix explicitly, rather than trusting the three unrelated tests
// that happened to cover three of the cells, is the point. An audit of this
// change found the pool/non-tx cell entirely unpinned: a mutation disabling its
// report survived the whole suite, because every pool assertion went through the
// HTTP handler (which is tx) and every non-tx assertion went through the bracket.
// That is the same failure this package has already been bitten by once — see
// TestRollback_BracketSubResults_ClearedTx, whose comment records a tx-policy
// mutation surviving the entire suite for exactly this reason.
//
// Each cell asserts BOTH directions. Reporting a supersede is only half the
// contract; a write that lands must never claim to be superseded, or the fix
// "passes" by rejecting everything.
//
// P6 (the bc-twin mutation audit's known residual) is now CLOSED, and how it
// was closed is worth recording, because "unpinnable" was wrong in an
// instructive way.
//
// The residual: nothing detected an entry shim being simplified from
// WithTransaction to a bare fn(e.store). That breaks
// recordIneligibilityFromDecision's K2 check-and-set, whose atomicity IS the
// handle's, and the damage only appears under an interleaving no test can
// schedule from outside the store. Treating it as an OBSERVATION problem was a
// dead end: both stage-then-fail paths roll back explicitly, so the
// transactional and bare-handle runs reach the same end state.
//
// It was closed by making the invariant CHECKABLE instead of observable.
// state.IsTransactional asks the handle what it is, and the K2 site logs when
// handed a bare one; TestK2ChecksItsHandleIsTransactional (eligibility_test.go)
// asserts the production paths stay silent, with a deliberate bare-handle call
// as the negative control. Both entry shims are covered — note the second took
// an extra case, because RecordMatchResult only reaches K2 when the result
// carries a kiken/fusenpai decision, so unwrapping it survived until a test
// drove that combination.
//
// If you are about to "simplify" either shim into a bare handle pass: that
// mutation now fails, which is the point. Read the K2 note on
// recordIneligibilityFromDecision first.
func TestSupersededIsReportedOnEveryWritePath(t *testing.T) {
	const storedAt, olderAt, newerAt = 2_000_000, 1_000_000, 3_000_000

	// The stored match is stamped newer while NOT completed. RevertMatchToQueue
	// leaves exactly this shape — it stamps ModifiedAt = now() so a queued
	// pre-revert result loses.
	//
	// This used to be the ONLY shape that could reach the guard, because
	// overwriting a stored COMPLETED result requires a correctionReason and a
	// correction bypassed the timestamp outright. That exemption is gone
	// (applyMatchWrite): it could not tell a live correction from one the
	// offline queue replayed hours later, so a stale correction silently
	// overwrote a newer result. A completed result is therefore reachable here
	// too now; this fixture keeps the running shape because it is the one the
	// reconnect scenario actually produces.
	incoming := func(at int64) *state.MatchResult {
		return &state.MatchResult{
			SideA: "Alice", SideB: "Bob", Winner: "Bob",
			IpponsB: []string{"M", "D"},
			Status:  state.MatchStatusCompleted, ModifiedAt: at,
		}
	}

	// Each cell seeds its own competition and returns the write under test, so a
	// cell can never pass because a sibling left usable state behind.
	cells := []struct {
		name string
		// seed installs the stored match and returns its id.
		seed func(t *testing.T, eng *Engine, store *state.Store, compID string) string
		// write performs one forward write and returns its error.
		write func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string, r *state.MatchResult) error
		// file is the competition file this cell's match lives in, so the
		// footprint assertion below can read it.
		file string
	}{
		{
			name: "pool match, plain writer",
			file: "pool-matches.csv",
			seed: seedPoolMatch(storedAt),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "pool match, tx twin",
			file: "pool-matches.csv",
			seed: seedPoolMatch(storedAt),
			write: func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string, r *state.MatchResult) error {
				return inTx(t, store, compID, func(tx state.StoreTx) error {
					_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, r)
					return err
				})
			},
		},
		{
			name: "knockout round match, plain writer",
			file: "bracket.json",
			seed: seedBracket(storedAt, false),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "knockout round match, tx twin",
			file: "bracket.json",
			seed: seedBracket(storedAt, false),
			write: func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string, r *state.MatchResult) error {
				return inTx(t, store, compID, func(tx state.StoreTx) error {
					_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, r)
					return err
				})
			},
		},
		{
			// Bronze lives in Bracket.ThirdPlaceMatch, NOT in Rounds, so the
			// round scan never finds it and it is written by its own branch —
			// the branch that used to discard `applied` outright.
			name: "bronze knockout, plain writer",
			file: "bracket.json",
			seed: seedBracket(storedAt, true),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "bronze knockout, tx twin",
			file: "bracket.json",
			seed: seedBracket(storedAt, true),
			write: func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string, r *state.MatchResult) error {
				return inTx(t, store, compID, func(tx state.StoreTx) error {
					_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, r)
					return err
				})
			},
		},
	}

	for _, cell := range cells {
		t.Run(cell.name+": a stale write is reported as superseded", func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sup-stale"
			matchID := cell.seed(t, eng, store, compID)

			err := cell.write(t, eng, store, compID, matchID, incoming(olderAt))
			require.ErrorIs(t, err, ErrMatchSuperseded,
				"a dropped write that returns nil is indistinguishable from a saved one, which is the whole bug")
		})

		// bc-cse review round: reporting the drop is half the contract; the
		// other half is that a dropped write leaves NO FOOTPRINT. A match is a
		// match, so this must hold whichever store it lands in — and it did not.
		// The bracket branch skipped its save (UpdateBracket returns early when
		// its callback errors) while the pool branch had no way for the callback
		// to say "do not persist", so the untouched slice was re-serialized and
		// the standings-cache version bumped: a rejected write left the same
		// trace on disk as an accepted one, and every stale replay in a
		// reconnect flush invalidated the cache again.
		//
		// The version counter is asserted as well as the bytes because it is the
		// half a same-bytes rewrite still moves, and it is what forces the
		// standings recompute (see the cache-invalidation rules in state).
		t.Run(cell.name+": a stale write leaves no footprint", func(t *testing.T) {
			eng, store, dir := setupTestEngine(t)
			compID := "sup-footprint"
			matchID := cell.seed(t, eng, store, compID)

			path := filepath.Join(dir, "competitions", compID, cell.file)
			before, err := os.ReadFile(path)
			require.NoError(t, err, "the seed must have written the file this cell reads")
			verBefore := store.FileVersion(compID, cell.file)

			require.ErrorIs(t,
				cell.write(t, eng, store, compID, matchID, incoming(olderAt)),
				ErrMatchSuperseded)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after), "a dropped write must not rewrite the file")
			assert.Equal(t, verBefore, store.FileVersion(compID, cell.file),
				"a dropped write must not bump the file version: that invalidates the standings cache for nothing")
		})

		t.Run(cell.name+": a write that lands reports no error", func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sup-fresh"
			matchID := cell.seed(t, eng, store, compID)

			err := cell.write(t, eng, store, compID, matchID, incoming(newerAt))
			require.NoError(t, err)
			assert.NotErrorIs(t, err, ErrMatchSuperseded)
		})
	}
}

// inTx runs fn inside a real transaction and surfaces fn's error to the caller.
// The tx itself commits (returns nil) so the cell exercises the same
// commit-whatever-the-engine-settled-on shape the score handler uses; the
// engine error is what the assertion is about.
func inTx(t *testing.T, store *state.Store, compID string, fn func(tx state.StoreTx) error) error {
	t.Helper()
	var inner error
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		inner = fn(tx)
		return nil
	}))
	return inner
}

func seedPoolMatch(storedAt int64) func(*testing.T, *Engine, *state.Store, string) string {
	return func(t *testing.T, _ *Engine, store *state.Store, compID string) string {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Sup"}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusRunning, ModifiedAt: storedAt,
		}}))
		return "Pool A-0"
	}
}

func seedBracket(storedAt int64, bronze bool) func(*testing.T, *Engine, *state.Store, string) string {
	return func(t *testing.T, _ *Engine, store *state.Store, compID string) string {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Sup", Status: state.CompStatusKnockout,
		}))
		b := &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
					Status: state.MatchStatusRunning, ModifiedAt: storedAt},
			}},
		}
		if bronze {
			b.ThirdPlaceMatch = &state.BracketMatch{
				ID: "m-bronze", SideA: "Alice", SideB: "Bob",
				Status: state.MatchStatusRunning, ModifiedAt: storedAt, DisplayRound: -1,
			}
		}
		require.NoError(t, store.SaveBracket(compID, b))
		if bronze {
			return "m-bronze"
		}
		return "m-r1-0"
	}
}

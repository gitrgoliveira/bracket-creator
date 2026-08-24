package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-lww1. A match write the timestamp guard drops must REPORT the drop, on
// every path a write can take. That is a matrix, not a single case: a match id
// resolves to a pool match or a knockout one only at run time, the knockout half
// splits again into a round match and the bronze playoff (which lives outside
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
// KNOWN RESIDUAL (P6 in the bc-twin mutation audit): nothing here pins that the
// non-tx entry shims actually WRAP their write in WithTransaction rather than
// passing e.store bare. Replacing either shim's WithTransaction with a bare
// fn(e.store) passes this package AND internal/mobileapp — including the three
// tests written for the property it breaks (TestRecordDecision_ConcurrentKiken,
// _ConcurrentKikenRace, TestRecordIneligibilityFromDecisionTx_AlreadyIneligible).
//
// What breaks is recordIneligibilityFromDecision's K2 check-and-set, which since
// bc-twin has no WithTransaction of its own — its atomicity IS the handle's, so
// a bare store handle silently makes the load/check/set three separately-locked
// operations. That needs an interleaving no test can schedule deterministically
// from outside the store, which is why it stays unpinned.
//
// The two are NOT, however, "observationally identical single-threaded", as an
// earlier version of this note claimed. WithTransaction stages saves in the WAL
// and DISCARDS them when fn returns an error, whereas a bare handle lands each
// save immediately — so on any error path that has already issued a save the two
// differ in whether that save survives, visible through Store.FileVersion even
// when the bytes are unchanged. A superseded pool write is exactly such a path
// (UpdatePoolMatchByID saves once the match is found, then the guard errors).
// That difference is real but incidental; the K2 argument above is the reason
// the wrapper must stay.
//
// If you are about to "simplify" RecordMatchResult or
// RecordMatchResultWithIneligibility into a bare handle pass: that is the
// mutation this note exists for; read the K2 note on
// recordIneligibilityFromDecision first.
func TestSupersededIsReportedOnEveryWritePath(t *testing.T) {
	const storedAt, olderAt, newerAt = 2_000_000, 1_000_000, 3_000_000

	// The stored match is stamped newer while NOT completed. That is the shape
	// the guard actually fences in practice and it is deliberate: a stored
	// COMPLETED result cannot reach the guard at all, because overwriting one
	// requires a correctionReason and a correction outranks the timestamp by
	// design (applyMatchWrite). RevertMatchToQueue leaves exactly this shape —
	// it stamps ModifiedAt = now() so a queued pre-revert result loses.
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
	}{
		{
			name: "pool match, plain writer",
			seed: seedPoolMatch(storedAt),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "pool match, tx twin",
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
			seed: seedBracket(storedAt, false),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "knockout round match, tx twin",
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
			name: "bronze playoff, plain writer",
			seed: seedBracket(storedAt, true),
			write: func(_ *testing.T, eng *Engine, _ *state.Store, compID, matchID string, r *state.MatchResult) error {
				return eng.RecordMatchResult(compID, matchID, r)
			},
		},
		{
			name: "bronze playoff, tx twin",
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
			ID: compID, Name: "Sup", Status: state.CompStatusPlayoffs,
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

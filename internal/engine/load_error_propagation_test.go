package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptCompFile overwrites one of compID's files with bytes its parser
// rejects at the structural level, the class the store reports as a located
// *state.CorruptFileError.
func corruptCompFile(t *testing.T, store *state.Store, compID, file string) {
	t.Helper()
	var raw string
	switch file {
	case "pool-matches.csv":
		raw = "a,\"unterminated\n"
	case "bracket.json":
		raw = "{not json"
	default:
		t.Fatalf("no corrupt shape for %s", file)
	}
	p := filepath.Join(store.GetFolder(), "competitions", compID, file)
	require.NoError(t, os.WriteFile(p, []byte(raw), 0600))
}

// assertLoadError is the contract every match lookup now keeps: an unreadable
// file is the caller's error, carried as the store's own located
// CorruptFileError (which the handlers turn into the corrupt_file 500 naming
// the file), and never a *NotFoundError (a 404 the offline queue drops as
// final).
func assertLoadError(t *testing.T, err error, file string) {
	t.Helper()
	require.Error(t, err)
	var notFound *NotFoundError
	assert.False(t, errors.As(err, &notFound), "an unreadable file is not a missing match: %v", err)
	cf, ok := state.AsCorruptFile(err)
	require.True(t, ok, "the load error reaches the caller as it is: %v", err)
	assert.Equal(t, file, cf.File)
}

func TestMatchLookupsPropagateLoadErrors(t *testing.T) {
	for _, file := range []string{"pool-matches.csv", "bracket.json"} {
		t.Run(file, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "load-err"
			createTestCompetition(t, store, compID, "league", 3)
			corruptCompFile(t, store, compID, file)

			// The id is in neither store, so a swallowed load error reads as
			// "not found": exactly the answer this pins against.
			const matchID = "m-r1-0"
			inTxErr := func(fn func(tx state.StoreTx) error) error {
				var got error
				require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
					got = fn(tx)
					return nil
				}))
				return got
			}

			lookups := []struct {
				name string
				call func() error
			}{
				{"lookupExistingResult", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, err := eng.lookupExistingResult(tx, compID, matchID)
						return err
					})
				}},
				{"RecordMatchResultWithIneligibilityTx", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, &state.MatchResult{Status: state.MatchStatusCompleted})
						return err
					})
				}},
				{"hasDownstreamMatchStarted", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, err := eng.hasDownstreamMatchStarted(tx, compID, []string{"Alice"}, matchID)
						return err
					})
				}},
				{"matchSideParticipantIDs", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, err := eng.matchSideParticipantIDs(tx, compID, matchID)
						return err
					})
				}},
				{"lookupMatchSides", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, _, err := eng.lookupMatchSides(tx, compID, matchID)
						return err
					})
				}},
				{"checkSimultaneousMatchTx", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						return eng.checkSimultaneousMatchTx(tx, compID, matchID)
					})
				}},
				{"findMatchHome", func() error {
					return inTxErr(func(tx state.StoreTx) error {
						_, err := findMatchHome(tx, compID, matchID, func(matchHome) error { return nil })
						return err
					})
				}},
				{"findTeamMatch", func() error {
					_, _, _, err := eng.findTeamMatch(compID, matchID)
					return err
				}},
				{"ReopenMatch", func() error {
					_, err := eng.ReopenMatch(compID, matchID, "")
					return err
				}},
			}
			for _, l := range lookups {
				t.Run(l.name, func(t *testing.T) {
					assertLoadError(t, l.call(), file)
				})
			}
		})
	}
}

// A competition that simply has no such file is not an error: a knockout-only
// competition has no pool-matches.csv and a pool-only one no bracket.json,
// and a lookup there must still answer "not found".
func TestMatchLookupsOnAMissingFileStayNotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "no-files"
	createTestCompetition(t, store, compID, "league", 3)

	var err error
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, err = eng.lookupExistingResult(tx, compID, "m-r1-0")
		return nil
	}))
	var notFound *NotFoundError
	require.ErrorAs(t, err, &notFound)

	parent, _, _, err := eng.findTeamMatch(compID, "m-r1-0")
	require.NoError(t, err)
	assert.Nil(t, parent)
}

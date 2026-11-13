package state

import (
	"errors"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// A participant edit inside a transaction stages participants.csv and
// seeds.csv in the WAL (operator decision D5 (a), 2026-11-12), so the edit
// and the draw cascade beside it commit or abort together.

func participantsTxFixture(t *testing.T) (*Store, string, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID = "participants-tx"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Participants Tx"}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
	}))
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	aliceID := ""
	for _, p := range players {
		if p.Name == "Alice" {
			aliceID = p.ID
		}
	}
	require.NotEmpty(t, aliceID)
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{{ID: aliceID, Name: "Alice", Dojo: "Dojo A", SeedRank: 1}}))
	return store, compID, aliceID
}

func renameTo(name string) func(*domain.Player) error {
	return func(p *domain.Player) error {
		p.Name = name
		return nil
	}
}

func namesOf(players []domain.Player) []string {
	out := make([]string, len(players))
	for i, p := range players {
		out[i] = p.Name
	}
	return out
}

// The round-lineup settlement a transaction runs at commit reads the roster
// the transaction staged, not the file under it: a participant edit stages
// participants.csv in the same transaction.
func TestStoreTx_RoundLineupStageReadsTheStagedRoster(t *testing.T) {
	store, compID, aliceID := participantsTxFixture(t)
	require.NoError(t, store.WithTransaction(compID, func(tx StoreTx) error {
		if _, err := tx.UpdateParticipant(compID, aliceID, false, renameTo("Alicia")); err != nil {
			return err
		}
		comp, err := tx.LoadCompetition(compID)
		if err != nil {
			return err
		}
		players, err := tx.(*storeTx).roundLineupStage().players(comp)
		if err != nil {
			return err
		}
		assert.Contains(t, namesOf(players), "Alicia", "the stage reads the roster this transaction staged")
		assert.NotContains(t, namesOf(players), "Alice")
		return nil
	}))
}

func TestStoreTx_UpdateParticipantThenAbortLeavesRosterAndSeedsUnchanged(t *testing.T) {
	store, compID, aliceID := participantsTxFixture(t)
	pPath := store.compPath(compID, "participants.csv")
	sPath := store.compPath(compID, "seeds.csv")
	pBefore, err := os.ReadFile(pPath)
	require.NoError(t, err)
	sBefore, err := os.ReadFile(sPath)
	require.NoError(t, err)
	// Warm the caches so a body published at stage time would be served.
	_, err = store.LoadParticipants(compID, false)
	require.NoError(t, err)

	var staged uint64
	sentinel := errors.New("the draw cascade failed")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if _, err := tx.UpdateParticipant(compID, aliceID, false, renameTo("Alicia")); err != nil {
			return err
		}
		staged = store.FileVersion(compID, "participants.csv")
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel)

	pAfter, err := os.ReadFile(pPath)
	require.NoError(t, err)
	assert.Equal(t, string(pBefore), string(pAfter), "an aborted transaction must leave participants.csv byte-identical")
	sAfter, err := os.ReadFile(sPath)
	require.NoError(t, err)
	assert.Equal(t, string(sBefore), string(sAfter), "an aborted transaction must leave seeds.csv byte-identical")

	assert.Greater(t, store.FileVersion(compID, "participants.csv"), staged,
		"the abort must move the version past the staged save's")

	got, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Alice", "Bob"}, namesOf(got), "the cache must not serve the aborted roster")
	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Alice", seeds[0].Name)
}

func TestStoreTx_LoadParticipantsReadsWhatTheTransactionUpdated(t *testing.T) {
	store, compID, aliceID := participantsTxFixture(t)
	pBefore, err := os.ReadFile(store.compPath(compID, "participants.csv"))
	require.NoError(t, err)

	sentinel := errors.New("abort after reading")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if _, err := tx.UpdateParticipant(compID, aliceID, false, renameTo("Alicia")); err != nil {
			return err
		}
		// Still staged, not on disk.
		onDisk, err := os.ReadFile(store.compPath(compID, "participants.csv"))
		require.NoError(t, err)
		assert.Equal(t, string(pBefore), string(onDisk), "the update must be staged, not written")

		got, err := tx.LoadParticipants(compID, false)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"Alicia", "Bob"}, namesOf(got), "a load in the transaction must answer the staged roster")
		for _, p := range got {
			if p.Name == "Alicia" {
				assert.Equal(t, 1, p.Seed, "the staged seeds.csv must be merged by the same load")
			}
		}
		// A second edit in the same transaction starts from the first.
		_, err = tx.UpdateParticipant(compID, aliceID, false, func(p *domain.Player) error {
			assert.Equal(t, "Alicia", p.Name, "a second update must read the first one's staged roster")
			p.Name = "Alicia Two"
			return nil
		})
		require.NoError(t, err)
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel)
}

func TestStoreTx_UpdateParticipantCommitLandsRosterAndSeeds(t *testing.T) {
	store, compID, aliceID := participantsTxFixture(t)
	_, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)

	require.NoError(t, store.WithTransaction(compID, func(tx StoreTx) error {
		_, err := tx.UpdateParticipant(compID, aliceID, false, renameTo("Alicia"))
		return err
	}))

	got, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"Alicia", "Bob"}, namesOf(got))
	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Alicia", seeds[0].Name, "the seed rename must land with the roster")
	assert.Equal(t, aliceID, seeds[0].ID)
}

// The floors saveParticipantsNoLock enforces hold on the transaction path, and
// a refused edit stages nothing.
func TestStoreTx_UpdateParticipantFloorsRefuseAndStageNothing(t *testing.T) {
	store, compID, aliceID := participantsTxFixture(t)
	cases := []struct {
		name    string
		mutate  func(*domain.Player) error
		wantErr error
	}{
		{"duplicate name and dojo", func(p *domain.Player) error { p.Name, p.Dojo = "Bob", "Dojo B"; return nil }, ErrDuplicateName},
		{"blank dojo", func(p *domain.Player) error { p.Dojo = " "; return nil }, ErrBlankDojo},
		{"reserved name", func(p *domain.Player) error { p.Name = "Pool A-1st"; return nil }, ErrReservedName},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stagedPaths []string
			txErr := store.WithTransaction(compID, func(tx StoreTx) error {
				_, err := tx.UpdateParticipant(compID, aliceID, false, tc.mutate)
				st := tx.(*storeTx)
				for _, in := range st.wal.Intents() {
					stagedPaths = append(stagedPaths, in.Path)
				}
				return err
			})
			require.ErrorIs(t, txErr, tc.wantErr)
			assert.Empty(t, stagedPaths, "a refused edit must stage nothing")
		})
	}
}

// The roster save's side writes (the orphaned team-member and lineup prunes,
// the Metadata member migration) go through the transaction's writer too
// ("all files in the log"): an aborted edit leaves team-members.yaml and
// lineups.yaml byte-identical, and a commit lands the prunes.
func TestStoreTx_UpdateParticipantSideWritesAreStaged(t *testing.T) {
	setup := func(t *testing.T) (*Store, string, string) {
		t.Helper()
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)
		const compID = "participants-tx-team"
		require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Team Tx", Kind: "team", TeamSize: 3, Status: CompStatusSetup}))
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{Name: "Tigers", Dojo: "Dojo A"},
			{Name: "Lions", Dojo: "Dojo B"},
		}))
		players, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		tigersID := ""
		for _, p := range players {
			if p.Name == "Tigers" {
				tigersID = p.ID
			}
		}
		require.NotEmpty(t, tigersID)
		// An orphan in each file: a team no longer on the roster.
		squads, err := store.loadSquadsLocked(compID)
		require.NoError(t, err)
		squads["orphan-team"] = []domain.TeamMember{{ID: "m-orphan", Index: 1, Name: "Ghost"}}
		require.NoError(t, store.saveSquadsLocked(compID, squads, store.directWrite))
		require.NoError(t, store.saveTeamLineupsLocked(compID, map[string]domain.TeamLineup{
			teamLineupKey("orphan-team", 1): {TeamID: "orphan-team", CompetitionID: compID, Round: 1, Positions: map[domain.Position]string{domain.PosSenpo: "Ghost"}},
		}, store.directWrite))
		return store, compID, tigersID
	}
	read := func(t *testing.T, store *Store, compID, name string) string {
		t.Helper()
		b, err := os.ReadFile(store.compPath(compID, name))
		require.NoError(t, err)
		return string(b)
	}

	t.Run("abort leaves both files untouched", func(t *testing.T) {
		store, compID, tigersID := setup(t)
		membersBefore := read(t, store, compID, teamMembersFilename)
		lineupsBefore := read(t, store, compID, teamLineupFilename)
		require.Contains(t, membersBefore, "orphan-team")
		require.Contains(t, lineupsBefore, "orphan-team")

		sentinel := errors.New("a later step failed")
		txErr := store.WithTransaction(compID, func(tx StoreTx) error {
			if _, err := tx.UpdateParticipant(compID, tigersID, false, renameTo("Tigers Two")); err != nil {
				return err
			}
			// Read-your-own-writes over the staged prune.
			squads, err := tx.LoadSquads(compID)
			require.NoError(t, err)
			assert.NotContains(t, squads, "orphan-team", "a load in the transaction must see the staged prune")
			lineups, err := tx.LoadTeamLineupsWithRemoved(compID)
			require.NoError(t, err)
			for _, l := range lineups {
				assert.NotEqual(t, "orphan-team", l.TeamID, "a load in the transaction must see the staged lineup prune")
			}
			return sentinel
		})
		require.ErrorIs(t, txErr, sentinel)
		assert.Equal(t, membersBefore, read(t, store, compID, teamMembersFilename), "an abort must leave team-members.yaml byte-identical")
		assert.Equal(t, lineupsBefore, read(t, store, compID, teamLineupFilename), "an abort must leave lineups.yaml byte-identical")
		squads, err := store.LoadSquads(compID)
		require.NoError(t, err)
		assert.Contains(t, squads, "orphan-team", "the cache must not serve the aborted prune")
	})

	t.Run("commit lands the prunes", func(t *testing.T) {
		store, compID, tigersID := setup(t)
		require.NoError(t, store.WithTransaction(compID, func(tx StoreTx) error {
			_, err := tx.UpdateParticipant(compID, tigersID, false, renameTo("Tigers Two"))
			return err
		}))
		assert.NotContains(t, read(t, store, compID, teamMembersFilename), "orphan-team")
		assert.NotContains(t, read(t, store, compID, teamLineupFilename), "orphan-team")
	})
}

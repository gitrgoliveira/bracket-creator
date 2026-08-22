package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pre-UUID participants.csv is NOT rewritten on read: the legacy no-id
// shape is byte-indistinguishable from a roster carrying non-UUID client ids
// awaiting its deferred HasParticipantIDs flip (the mp-p7n ambiguity), and a
// read-side rewrite built on that sniff persisted the shifted mis-parse.
// Roster ids convert at the WRITE boundary instead (marshalParticipantsCSV
// mints ids on every save). This pins the read side staying hands-off.
func TestLegacyParticipantsNotRewrittenOnRead(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	legacy := "Aiko Sato, Seibukan\nJun Mori, Tobukan\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "participants.csv"),
		[]byte(legacy), 0o600))

	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.Equal(t, "Aiko Sato", players[0].Name)

	b, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "participants.csv"))
	require.NoError(t, err)
	assert.Equal(t, legacy, string(b), "reads must not rewrite the roster file")
}

// A legacy seeds.csv row (no dojo) is completed from the roster on first
// read when the name is unique there; an ambiguous name is left alone
// (AssignSeeds refuses that seeding either way, and guessing would be worse).
func TestLegacySeedDojoUpgradeOnRead(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{Name: "Rin Sato", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "seeds.csv"),
		[]byte("Rank,Name\n1,Rin Sato\n2,Yuki Tanaka\n"), 0o600))

	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	_, err = fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 2)
	bySeed := map[int]string{}
	for _, sd := range seeds {
		bySeed[sd.SeedRank] = sd.Dojo
	}
	assert.Equal(t, "Seibukan", bySeed[1], "unique name: dojo completed from the roster")
	assert.Equal(t, "", bySeed[2], "ambiguous name: left legacy, never guessed")
}

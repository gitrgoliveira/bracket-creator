package state_test

// legacy_squad_resave_test.go pins the interaction between two things this
// PR introduced that used to destroy data when combined.
//
// A pre-UUID participants.csv loads with every Player.ID empty. This PR
// resolves competitors BY ID, so it detects that state and tells the
// operator the remedy in so many words: "no id on file. Save the roster
// once and the ids are assigned" (helper.MissingParticipantIDsMessage,
// shown in the console's data-issues banner and in the draw refusal).
//
// For a TEAM competition whose members still live in Player.Metadata, that
// advertised save used to be the thing that destroyed them: the pre-write
// migration skipped every row for want of an id, the write landed with
// blank Metadata, and the NEXT load seeded the team a squad of empty slots
// and marked it migrated, so nothing could ever recover the names.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacySquadSurvivesTheAdvertisedRosterResave(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", Kind: "team", TeamSize: 3}))

	// Pre-UUID shape: no id column at all, members in the trailing columns.
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "participants.csv"),
		[]byte("Tora,Tora Dojo,Sato,Kimura,Abe\nKaze,Kaze Dojo,Mori,Oda,Ito\n"), 0o600))

	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.Empty(t, loaded[0].ID, "fixture guard: a pre-UUID roster really does load id-less")
	require.Equal(t, []string{"Sato", "Kimura", "Abe"}, loaded[0].Metadata)

	// The advertised remedy, in the shape the roster PUT actually takes:
	// the operator's list, with the team members NOT echoed back.
	resaved := make([]domain.Player, 0, len(loaded))
	for _, p := range loaded {
		resaved = append(resaved, domain.Player{Name: p.Name, Dojo: p.Dojo})
	}
	require.NoError(t, fresh.SaveParticipants("c1", resaved))

	after, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, after, 2)
	byName := map[string]string{}
	for _, p := range after {
		require.NotEmpty(t, p.ID, "the save assigns the ids the banner promises")
		byName[p.Name] = p.ID
	}

	// A LATER process, which is where the loss used to become permanent.
	later, err := state.NewStore(dir)
	require.NoError(t, err)
	later.EnsureLegacyUpgraded("c1")
	squads, err := later.LoadSquads("c1")
	require.NoError(t, err)

	assert.Equal(t, []string{"Sato", "Kimura", "Abe"}, squadNames(squads[byName["Tora"]]),
		"the members carried across to the id the save minted, not to a squad of blanks")
	assert.Equal(t, []string{"Mori", "Oda", "Ito"}, squadNames(squads[byName["Kaze"]]))
}

func squadNames(members []domain.TeamMember) []string {
	out := make([]string, 0, len(members))
	for _, m := range members {
		out = append(out, m.Name)
	}
	return out
}

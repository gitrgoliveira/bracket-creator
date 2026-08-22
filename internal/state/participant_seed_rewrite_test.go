package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateParticipant_DojoOnlyEditUpdatesSeedRow pins bug (a) from the
// updateParticipantNoLock seeds rewrite: it used to gate the seeds.csv
// rewrite on oldName != newName, so a dojo-only edit never touched
// seeds.csv at all. The seed row then kept the STALE dojo, the merge
// fallback (byKey/RosterIndex) refused to match it (the row's dojo is
// non-empty, so the bare-name fallback never applies), and generate-draw
// failed "seeded participant not found in main list" for a participant
// who was plainly still on the roster.
func TestUpdateParticipant_DojoOnlyEditUpdatesSeedRow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "dojo-only-edit"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Dojo Only Edit"}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice Cooper", Dojo: "Old Dojo"},
		{Name: "Bob Marley", Dojo: "Dojo B"},
	}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Alice Cooper", Dojo: "Old Dojo", SeedRank: 1},
	}))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var aliceID string
	for _, p := range loaded {
		if p.Name == "Alice Cooper" {
			aliceID = p.ID
		}
	}
	require.NotEmpty(t, aliceID, "fixture must give Alice an id to update by")

	// Dojo-only edit: name is untouched, only Dojo changes.
	_, err = store.UpdateParticipant(compID, aliceID, false, func(p *domain.Player) error {
		p.Dojo = "New Dojo"
		return nil
	})
	require.NoError(t, err)

	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Alice Cooper", seeds[0].Name)
	assert.Equal(t, "New Dojo", seeds[0].Dojo,
		"the seed row must follow a dojo-only edit or it orphans the seed")

	// Draw-path resolution: LoadParticipants' own merge must resolve the
	// seed under the new identity, and so must domain.AssignSeeds -- the
	// same call generate-draw makes.
	reloaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var aliceSeed int
	for _, p := range reloaded {
		if p.ID == aliceID {
			aliceSeed = p.Seed
		}
	}
	assert.Equal(t, 1, aliceSeed, "the participants merge must resolve the seed under the new dojo")

	plain := make([]domain.Player, len(reloaded))
	for i, p := range reloaded {
		plain[i] = domain.Player{Name: p.Name, Dojo: p.Dojo}
	}
	assert.NoError(t, domain.AssignSeeds(plain, seeds),
		"generate-draw's own resolver must still find the seeded participant")
}

// TestUpdateParticipant_RenameOnlyTouchesEditedParticipantsSeedRow pins bug
// (b): the rewrite used to match seed rows on bare oldName alone, with no
// dojo filter, so renaming ONE of two same-named players rewrote BOTH
// rows -- crossing one competitor's seed rank onto another's identity.
func TestUpdateParticipant_RenameOnlyTouchesEditedParticipantsSeedRow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "same-name-rename"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Same Name Rename"}))

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Yuki Tanaka", Dojo: "Seibukan", SeedRank: 1},
		{Name: "Yuki Tanaka", Dojo: "Tobukan", SeedRank: 2},
	}))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var seibukanID string
	for _, p := range loaded {
		if p.Name == "Yuki Tanaka" && p.Dojo == "Seibukan" {
			seibukanID = p.ID
		}
	}
	require.NotEmpty(t, seibukanID, "fixture must give the Seibukan Yuki Tanaka an id to update by")

	_, err = store.UpdateParticipant(compID, seibukanID, false, func(p *domain.Player) error {
		p.Name = "Yuki T."
		return nil
	})
	require.NoError(t, err)

	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 2)
	byDojo := map[string]domain.SeedAssignment{}
	for _, sd := range seeds {
		byDojo[sd.Dojo] = sd
	}
	require.Contains(t, byDojo, "Seibukan")
	require.Contains(t, byDojo, "Tobukan")
	assert.Equal(t, "Yuki T.", byDojo["Seibukan"].Name, "the renamed participant's own row must update")
	assert.Equal(t, 1, byDojo["Seibukan"].SeedRank)
	assert.Equal(t, "Yuki Tanaka", byDojo["Tobukan"].Name,
		"the OTHER same-named participant's row must be left completely untouched")
	assert.Equal(t, 2, byDojo["Tobukan"].SeedRank)
}

// TestUpdateParticipant_RenameUpgradesLegacyEmptyDojoSeedRow covers the
// legacy-row branch of the fix: a seeds.csv row with no dojo, naming a
// participant whose name is unique in the roster, must still be matched
// and rewritten by a rename -- and the rewrite completes BOTH the new name
// and the dojo (upgrading the legacy row), mirroring what the merge
// fallback and the once-per-process legacy upgrade already do independently.
//
// This deliberately reaches the roster via loadParticipantsNoLock rather
// than the public LoadParticipants/LoadParticipantsOpt wrappers, which
// would trigger EnsureLegacyUpgraded and complete the row BEFORE the rename
// ever ran -- masking whether updateParticipantNoLock's OWN fallback
// matching works.
func TestUpdateParticipant_RenameUpgradesLegacyEmptyDojoSeedRow(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "legacy-seed-upgrade"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Legacy"}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Rin Sato", Dojo: "Seibukan"},
		{Name: "Bob", Dojo: "Dojo B"},
	}))
	// Hand-write a legacy (name-only, empty dojo) seed row directly to
	// disk so it stays legacy until this test's own rename touches it.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", compID, "seeds.csv"),
		[]byte("Rank,Name,Dojo\n1,Rin Sato,\n"), 0o600))

	players, err := store.loadParticipantsNoLock(compID, false, LoadParticipantsOpts{WithSeeds: false})
	require.NoError(t, err)
	var rinID string
	for _, p := range players {
		if p.Name == "Rin Sato" {
			rinID = p.ID
		}
	}
	require.NotEmpty(t, rinID, "fixture must give Rin Sato an id to update by")

	_, err = store.UpdateParticipant(compID, rinID, false, func(p *domain.Player) error {
		p.Name = "Rin S."
		return nil
	})
	require.NoError(t, err)

	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Rin S.", seeds[0].Name, "the legacy row must follow the rename")
	assert.Equal(t, "Seibukan", seeds[0].Dojo,
		"a matched legacy empty-dojo row must be completed by the rename, not just renamed")
}

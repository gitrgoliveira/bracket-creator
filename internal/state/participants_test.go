package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParticipants(t *testing.T) {
	dir, err := os.MkdirTemp("", "participants-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "comp-participants"
	err = os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700)
	require.NoError(t, err)

	// 1. Load empty participants (doesn't exist)
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	assert.Empty(t, players)

	// 2. Save participants
	playersToSave := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A", Source: "manual"},
		{Name: "Bob", Dojo: "Dojo B"},
	}
	err = store.SaveParticipants(compID, playersToSave)
	require.NoError(t, err)

	// 3. Load participants
	loadedPlayers, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPlayers, 2)
	assert.NotEmpty(t, loadedPlayers[0].ID) // UUID generated
	assert.Equal(t, "Alice", loadedPlayers[0].Name)
	assert.Equal(t, "ALICE", loadedPlayers[0].DisplayName)
	assert.Equal(t, "Dojo A", loadedPlayers[0].Dojo)
	assert.Equal(t, "manual", loadedPlayers[0].Source)

	assert.NotEmpty(t, loadedPlayers[1].ID) // UUID generated
	assert.Equal(t, "Bob", loadedPlayers[1].Name)
	assert.Equal(t, "BOB", loadedPlayers[1].DisplayName)
	assert.Equal(t, "Dojo B", loadedPlayers[1].Dojo)
	assert.Empty(t, loadedPlayers[1].Source)

	// 4. Save and load participants with existing IDs
	playersToSaveWithID := []domain.Player{
		{ID: "00000000-0000-4000-8000-000000000000", Name: "Charlie", Dojo: "Dojo C"},
	}
	err = store.SaveParticipants(compID, playersToSaveWithID)
	require.NoError(t, err)

	loadedPlayersWithID, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedPlayersWithID, 1)
	assert.Equal(t, "00000000-0000-4000-8000-000000000000", loadedPlayersWithID[0].ID)
	assert.Equal(t, "Charlie", loadedPlayersWithID[0].Name)

	// 5. Test merging seeds
	seedsPath := filepath.Join(dir, "competitions", compID, "seeds.csv")
	err = os.WriteFile(seedsPath, []byte("Name,Rank\nCharlie,1\n"), 0600)
	require.NoError(t, err)

	loadedWithSeeds, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedWithSeeds, 1)
	assert.Equal(t, 1, loadedWithSeeds[0].Seed)

	// 6. Test with old format (no IDs)
	participantsPath := filepath.Join(dir, "competitions", compID, "participants.csv")
	err = os.WriteFile(participantsPath, []byte("Dave, Dojo D\nEve, Dojo E\n"), 0600)
	require.NoError(t, err)

	loadedOldFormat, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loadedOldFormat, 2)
	assert.Empty(t, loadedOldFormat[0].ID) // No UUID
	assert.Equal(t, "Dave", loadedOldFormat[0].Name)
	assert.Empty(t, loadedOldFormat[1].ID) // No UUID
	assert.Equal(t, "Eve", loadedOldFormat[1].Name)
}

func TestParticipantsWithZekkenNameRoundTrip(t *testing.T) {
	// Regression: when WithZekkenName=true the CSV writer must always emit the
	// DisplayName column (auto-deriving from Name when blank) so the next
	// LoadParticipants(_, true) read consistently parses
	// [Name, DisplayName, Dojo, ...] regardless of which optional trailing
	// fields are present.
	dir, err := os.MkdirTemp("", "participants-zekken-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "comp-zekken"
	// SaveParticipants now consults the competition record for WithZekkenName.
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Zekken RT", WithZekkenName: true,
	}))

	// Players where DisplayName is empty (will be omitted by SaveParticipants → 2-col row)
	playersToSave := []domain.Player{
		{Name: "Alice Smith", Dojo: "Dojo A"},                         // no DisplayName
		{Name: "Bob Jones", DisplayName: "Bob Jones", Dojo: "Dojo B"}, // DisplayName == Name
		{Name: "Carol", DisplayName: "C. CAROL", Dojo: "Dojo C"},      // distinct DisplayName
	}
	err = store.SaveParticipants(compID, playersToSave)
	require.NoError(t, err)

	// Loading with withZekkenName=true must succeed (no "validation failed" error)
	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 3)

	// Alice: 2-col row → DisplayName derived from Name
	assert.Equal(t, "Alice Smith", loaded[0].Name)
	assert.NotEmpty(t, loaded[0].DisplayName)
	assert.Equal(t, "Dojo A", loaded[0].Dojo)

	// Bob: 2-col row (DisplayName == Name) → DisplayName derived from Name
	assert.Equal(t, "Bob Jones", loaded[1].Name)
	assert.NotEmpty(t, loaded[1].DisplayName)
	assert.Equal(t, "Dojo B", loaded[1].Dojo)

	// Carol: 3-col row → DisplayName preserved
	assert.Equal(t, "Carol", loaded[2].Name)
	assert.Equal(t, "C. CAROL", loaded[2].DisplayName)
	assert.Equal(t, "Dojo C", loaded[2].Dojo)
}

func TestLoadParticipantsOpt_WithSeeds(t *testing.T) {
	dir, err := os.MkdirTemp("", "participants-opt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "opt-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Opt"}))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// WithSeeds: true (default path) — must return players
	loaded, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{WithSeeds: true})
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "Alice", loaded[0].Name)
}

func TestLoadParticipantsOpt_HasIDsHint(t *testing.T) {
	dir, err := os.MkdirTemp("", "participants-opt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "opt-ids"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "IDs"}))

	// Write a UUID-prefixed participants.csv manually
	path := filepath.Join(dir, "competitions", compID, "participants.csv")
	content := "550e8400-e29b-41d4-a716-446655440000, Alice, DojoA\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	trueVal := true
	loaded, err := store.LoadParticipantsOpt(compID, false, LoadParticipantsOpts{WithSeeds: false, HasIDs: &trueVal})
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", loaded[0].ID)
}

// Regression: helper.CreatePlayers auto-populates DisplayName in the
// non-zekken branch (= SanitizeName(Name)). Before the fix, SaveParticipants
// wrote a 3-column row for that auto-derived display name, and the next
// LoadParticipants(_, withZekkenName=false) re-parsed column 2 as Dojo —
// pushing the real Dojo into Metadata and silently corrupting the roster
// (the trigger path exercised by the mobile-app import handler).
//
// The fix omits the 3rd column whenever DisplayName equals the value the
// loader would derive on its own, so the auto-derived form round-trips
// safely. Distinct user-provided display names (zekken comps) keep the
// 3-column form.
func TestParticipantsNonZekkenImportRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "import-rt"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700))

	// Simulate the import handler's pipeline: helper.CreatePlayers parses
	// the uploaded CSV in non-zekken mode, which auto-derives DisplayName.
	parsed, err := helper.CreatePlayers([]string{"Jane Doe, Enzan Dojo"}, false)
	require.NoError(t, err)
	require.Equal(t, "J. DOE", parsed[0].DisplayName, "guard: helper still auto-derives DisplayName")

	require.NoError(t, store.SaveParticipants(compID, parsed))

	// The 2-column form must land on disk so the loader doesn't shift columns.
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "participants.csv"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "J. DOE",
		"auto-derived DisplayName must not be written to disk; got %q", string(raw))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Jane Doe", loaded[0].Name)
	assert.Equal(t, "Enzan Dojo", loaded[0].Dojo, "Dojo must round-trip intact (regression)")
	assert.Empty(t, loaded[0].Metadata, "real Dojo must not leak into Metadata (regression)")
	assert.Equal(t, "J. DOE", loaded[0].DisplayName, "loader still re-derives DisplayName")
}

// Distinct user-provided display names (e.g. zekken competitions) MUST still
// round-trip the third column intact. This guards against an over-eager fix
// to TestParticipantsNonZekkenImportRoundTrip that would drop ALL display
// names instead of just the auto-derived ones.
func TestParticipantsDistinctDisplayNameRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "distinct-rt"
	// A distinct DisplayName only makes sense on a zekken competition. The new
	// CSV writer keys layout off the comp's WithZekkenName flag, so the comp
	// record must be saved before the participants are written.
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Distinct RT", WithZekkenName: true,
	}))

	players := []domain.Player{
		// SanitizeName("Carol") == "CAROL", so "C. CAROL" carries new info.
		{Name: "Carol", DisplayName: "C. CAROL", Dojo: "Dojo C"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "participants.csv"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "C. CAROL", "distinct DisplayName must be preserved on disk")

	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "C. CAROL", loaded[0].DisplayName)
	assert.Equal(t, "Dojo C", loaded[0].Dojo)
}

func TestMetadataRoundTrip(t *testing.T) {
	// Regression: saveParticipantsNoLock must write Metadata (danGrade and
	// other extra CSV columns) so they survive a save→load cycle. Previously
	// the write omitted p.Metadata entirely, silently dropping danGrade.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "meta-rt"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700))

	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A", Metadata: []string{"2d"}},
		{Name: "Bob", Dojo: "Dojo B", Metadata: []string{"3d"}, Source: "registered"},
		{Name: "Carol", Dojo: "Dojo C"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "participants.csv"))
	require.NoError(t, err)
	rawStr := string(raw)
	assert.Contains(t, rawStr, "2d", "danGrade must be written to CSV")
	assert.Contains(t, rawStr, "3d", "danGrade must be written to CSV")

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, []string{"2d"}, loaded[0].Metadata, "Alice's danGrade must round-trip")
	assert.Equal(t, []string{"3d"}, loaded[1].Metadata, "Bob's danGrade must round-trip")
	assert.Equal(t, "registered", loaded[1].Source, "Bob's source must round-trip alongside danGrade")
	assert.Empty(t, loaded[2].Metadata, "Carol with no metadata must stay empty")
}

func TestCheckedInRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "checkin-rt"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700))

	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A", CheckedIn: true},
		{Name: "Bob", Dojo: "Dojo B", CheckedIn: false},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "participants.csv"))
	require.NoError(t, err)
	rawStr := string(raw)
	assert.Contains(t, rawStr, "checked_in", "checked-in flag must be written to CSV")

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.True(t, loaded[0].CheckedIn, "Alice must round-trip as checked-in")
	assert.False(t, loaded[1].CheckedIn, "Bob must round-trip as not checked-in")
}

func TestCheckedInColumnBasedDetection(t *testing.T) {
	// Regression: checked_in must be detected by column position, not suffix match.
	// A dojo literally named "checked_in" must not be consumed.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "checkin-col"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700))

	path := filepath.Join(dir, "competitions", compID, "participants.csv")
	// Minimal "Name, Dojo, checked_in" (3-column) format.
	content := "Alice, Suigetsu, checked_in\nBob, Gyokusen\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.True(t, loaded[0].CheckedIn, "Alice must be detected as checked-in from 3-column row")
	assert.Equal(t, "Suigetsu", loaded[0].Dojo, "Dojo must not be consumed by checked_in detection")
	assert.False(t, loaded[1].CheckedIn, "Bob must not be checked-in")

	// Negative: dojo literally named "checked_in" must NOT be consumed.
	content2 := "Carol, checked_in\n"
	require.NoError(t, os.WriteFile(path, []byte(content2), 0600))
	loaded2, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded2, 1)
	assert.False(t, loaded2[0].CheckedIn, "2-column row must never trigger checked_in detection")
	assert.Equal(t, "checked_in", loaded2[0].Dojo, "dojo named checked_in must be preserved")
}

func TestUpdateParticipant(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "update-p"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Update"}))

	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	aliceID := loaded[0].ID

	// Check Alice in.
	updated, err := store.UpdateParticipant(compID, aliceID, false, func(p *domain.Player) error {
		p.CheckedIn = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, updated.CheckedIn)

	// Reload and verify persistence.
	reloaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var alice domain.Player
	for _, p := range reloaded {
		if p.ID == aliceID {
			alice = p
		}
	}
	assert.True(t, alice.CheckedIn, "check-in must persist to disk")
	assert.False(t, reloaded[1].CheckedIn, "Bob must remain unchecked")

	// Not-found case.
	_, err = store.UpdateParticipant(compID, "nonexistent-id", false, func(p *domain.Player) error {
		return nil
	})
	assert.ErrorIs(t, err, ErrParticipantNotFound)
}

func TestCheckedInColumnBasedDetectionUUIDRows(t *testing.T) {
	// Regression (Copilot review): UUID rows have format "uuid,Name,Dojo[,source][,checked_in]".
	// A 3-part UUID row "uuid,Alice,checked_in" must NOT be misclassified: "checked_in" is the Dojo.
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "checkin-uuid"
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", compID), 0700))

	path := filepath.Join(dir, "competitions", compID, "participants.csv")

	// 3-col UUID row: uuid, Name, Dojo — "checked_in" is the Dojo value, not a marker.
	require.NoError(t, os.WriteFile(path,
		[]byte("550e8400-e29b-41d4-a716-446655440000, Alice, checked_in\n"), 0600))
	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.False(t, loaded[0].CheckedIn, "3-part UUID row must NOT be misclassified as checked-in")
	assert.Equal(t, "checked_in", loaded[0].Dojo, "dojo value must be preserved for 3-part UUID row")
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", loaded[0].ID)

	// 4-col UUID row: uuid, Name, Dojo, checked_in — trailing checked_in IS a valid marker.
	require.NoError(t, os.WriteFile(path,
		[]byte("550e8400-e29b-41d4-a716-446655440001, Bob, Suigetsu, checked_in\n"), 0600))
	loaded2, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded2, 1)
	assert.True(t, loaded2[0].CheckedIn, "4-part UUID row must be detected as checked-in")
	assert.Equal(t, "Suigetsu", loaded2[0].Dojo, "Dojo must survive after checked_in token is stripped")
}

func TestAddParticipant_WhitespaceDuplicateGuard(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "ws-dup-add"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "WS Dup"}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{{Name: "Alice", Dojo: "Dojo A"}}))

	// Same name AND same dojo (normalized) must be rejected.
	_, err = store.AddParticipant(compID, domain.Player{Name: "Alice ", Dojo: "Dojo A"}, false)
	assert.ErrorIs(t, err, ErrDuplicateName, "trailing-space name variant with same dojo must be caught by duplicate guard")

	_, err = store.AddParticipant(compID, domain.Player{Name: " Alice", Dojo: "Dojo A"}, false)
	assert.ErrorIs(t, err, ErrDuplicateName, "leading-space name variant with same dojo must be caught by duplicate guard")

	_, err = store.AddParticipant(compID, domain.Player{Name: "alice", Dojo: "Dojo A"}, false)
	assert.ErrorIs(t, err, ErrDuplicateName, "case-only name variant with same dojo must be caught by duplicate guard")

	// Same name but DIFFERENT dojo is explicitly ALLOWED (two real people).
	_, err = store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo B"}, false)
	assert.NoError(t, err, "same name at a different dojo must be allowed")
}

// TestSaveParticipants_RejectsDuplicateNameDojo pins the Tier-1 guard at the
// lowest write layer (saveParticipantsNoLock). This is the path the SPA's
// PRIMARY batch/paste-import flow takes (PUT /competitions/:id →
// SaveParticipants), so enforcing here makes the perfect-duplicate reject
// non-bypassable across every persistence path, not just the single-add/edit
// handlers. (mp-ljry)
func TestSaveParticipants_RejectsDuplicateNameDojo(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "bulk-dup"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Bulk Dup"}))

	// Perfect (name, dojo) duplicate within one bulk import — including a
	// diacritic/whitespace/case variant — must be rejected.
	err = store.SaveParticipants(compID, []domain.Player{
		{Name: "Müller", Dojo: "Wakaba"},
		{Name: "muller ", Dojo: "wakaba"},
	})
	assert.ErrorIs(t, err, ErrDuplicateName, "normalized (name,dojo) collision in a bulk save must be rejected")

	// Same name at DIFFERENT dojos is allowed (two real people).
	err = store.SaveParticipants(compID, []domain.Player{
		{Name: "John Smith", Dojo: "Wakaba"},
		{Name: "John Smith", Dojo: "Tora"},
	})
	assert.NoError(t, err, "same name at different dojos must be allowed in a bulk save")

	// Two same-named teams with empty dojo collide (real duplicate).
	err = store.SaveParticipants(compID, []domain.Player{
		{Name: "Shudokan", Dojo: ""},
		{Name: "shudokan", Dojo: ""},
	})
	assert.ErrorIs(t, err, ErrDuplicateName, "identical team names with empty dojo must collide")

	// Squad variants (different names) are NOT perfect duplicates.
	err = store.SaveParticipants(compID, []domain.Player{
		{Name: "Shudokan A", Dojo: ""},
		{Name: "Shudokan B", Dojo: ""},
	})
	assert.NoError(t, err, "A/B squad teams are distinct names and must be allowed")
}

func TestSaveParticipants_RejectsReservedNames(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "reserved-names"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Reserved Names"}))

	cases := []struct {
		name string
		dojo string
	}{
		{"Pool A-1st", "Wakaba"},
		{"Pool Z-2nd", "Tora"},
		{"Winner of r1-m0", ""},
		{"Winner of r3-m10", "Dojo"},
	}
	for _, tc := range cases {
		err := store.SaveParticipants(compID, []domain.Player{{Name: tc.name, Dojo: tc.dojo}})
		assert.ErrorIs(t, err, ErrReservedName, "reserved name %q must be rejected by SaveParticipants", tc.name)
	}

	// Non-reserved names must be allowed regardless of superficial similarity.
	err = store.SaveParticipants(compID, []domain.Player{
		{Name: "Winner of the 2025 Cup", Dojo: "Wakaba"},
	})
	assert.NoError(t, err, "non-reserved name must be accepted")
}

func TestAddParticipant_RejectsReservedName(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "reserved-add"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Reserved Add"}))

	_, err = store.AddParticipant(compID, domain.Player{Name: "Pool B-3rd", Dojo: "Dojo A"}, false)
	assert.ErrorIs(t, err, ErrReservedName, "reserved pool-finalist name must be rejected by AddParticipant")

	_, err = store.AddParticipant(compID, domain.Player{Name: "Winner of r2-m5", Dojo: "Dojo A"}, false)
	assert.ErrorIs(t, err, ErrReservedName, "reserved winner-of name must be rejected by AddParticipant")
}

func TestReplaceParticipant_RejectsReservedName(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "reserved-replace"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Reserved Replace"}))

	added, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)

	_, err = store.ReplaceParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.Name = "Pool C-1st"
		p.Dojo = "Dojo A"
		return nil
	})
	assert.ErrorIs(t, err, ErrReservedName, "rename to a reserved pool-finalist name must be rejected")

	_, err = store.ReplaceParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.Name = "Winner of r1-m0"
		p.Dojo = "Dojo A"
		return nil
	})
	assert.ErrorIs(t, err, ErrReservedName, "rename to a reserved winner-of name must be rejected")

	// A non-reserved rename must succeed.
	_, err = store.ReplaceParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.Name = "Alice Renamed"
		p.Dojo = "Dojo A"
		return nil
	})
	assert.NoError(t, err, "non-reserved rename must succeed")
}

func TestUpdateParticipant_WhitespaceDuplicateGuard(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "ws-dup-upd"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "WS Dup Upd"}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo A"}, // same dojo so rename collides
	}))

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var bobID string
	for _, p := range loaded {
		if p.Name == "Bob" {
			bobID = p.ID
		}
	}
	require.NotEmpty(t, bobID)

	// Renaming Bob to "Alice " (trailing space) must be rejected — same name+dojo.
	_, err = store.UpdateParticipant(compID, bobID, false, func(p *domain.Player) error {
		p.Name = "Alice "
		return nil
	})
	assert.ErrorIs(t, err, ErrDuplicateName, "trailing-space rename colliding with existing name+dojo must be rejected")

	// Renaming Bob to "alice" (case variant) must also be rejected — same name+dojo.
	_, err = store.UpdateParticipant(compID, bobID, false, func(p *domain.Player) error {
		p.Name = "alice"
		return nil
	})
	assert.ErrorIs(t, err, ErrDuplicateName, "case-variant rename colliding with existing name+dojo must be rejected")
}

// TestZekkenWithSourceDoesNotCorruptCSV pins the marshalParticipantsCSV column-
// layout fix: a zekken competition where DisplayName equals SanitizeName(Name)
// AND Source is non-empty (e.g. the "manual" default applied by the single-add
// endpoint) used to produce a 4-field row [id, Name, Dojo, Source] that
// CreatePlayersFromRecords(_, true) misparsed as
// (Name, DisplayName=Dojo, Dojo=Source) — silently corrupting the row.
// The writer now always emits the DisplayName column for zekken comps.
func TestZekkenWithSourceDoesNotCorruptCSV(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "zekken-source"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Zekken Source", WithZekkenName: true,
	}))

	// DisplayName left blank — SaveParticipants must still write the
	// DisplayName column for zekken comps so the row is round-trip safe.
	// Source="manual" mirrors what the single-add endpoint defaults to.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Akira Tanaka", Dojo: "Gyokusen", Source: "manual"},
	}))

	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Akira Tanaka", loaded[0].Name, "Name must round-trip")
	assert.Equal(t, "Gyokusen", loaded[0].Dojo, "Dojo must NOT shift into DisplayName (regression)")
	assert.Equal(t, "manual", loaded[0].Source, "Source must NOT shift into Dojo (regression)")
	assert.NotEmpty(t, loaded[0].DisplayName, "auto-derived DisplayName must be present after reload")
}

// TestReplaceParticipant_SeedsCSVEscaping pins that seed rename writes
// participant names through encoding/csv so names containing commas or quotes
// don't produce a broken seeds.csv. Pre-fix the rewrite used
// fmt.Fprintf("%d,%s\n") and a name like "Smith, John" emitted an unescaped
// extra column that ParseSeedsFile then misparsed (silently dropping the
// seed). Use a non-zekken comp here so the name-with-comma round-trips
// through participants.csv unchanged.
func TestReplaceParticipant_SeedsCSVEscaping(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "seeds-escape"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID: compID, Name: "Seeds Escape", Status: CompStatusSetup,
	}))

	// Seed Alice with a name that requires CSV escaping when written.
	added, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}}))

	// Rename Alice → "Smith, John" — the comma MUST be CSV-escaped in seeds.csv,
	// otherwise ParseSeedsFile splits "Smith" / " John" / "1" into the wrong slots.
	_, err = store.ReplaceParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.Name = "Smith, John"
		return nil
	})
	require.NoError(t, err)

	// Verify seeds.csv reloads cleanly with the comma preserved.
	seeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1, "seed must survive the rename — a broken CSV would drop it")
	assert.Equal(t, "Smith, John", seeds[0].Name, "comma in renamed seed name must round-trip through CSV escaping")
	assert.Equal(t, 1, seeds[0].SeedRank)
}

// TestAddParticipant_RejectedAfterStart pins the in-lock status re-check.
// The HTTP handler does the same check before calling AddParticipant, but a
// concurrent POST /competitions/:id/start could land between that check and
// the per-comp lock acquisition. The store-level guard catches the racing
// write and returns ErrCompetitionNotInSetup → 409.
func TestAddParticipant_RejectedAfterStart(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "started-add"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Started", Status: CompStatusPools}))

	_, err = store.AddParticipant(compID, domain.Player{Name: "Late", Dojo: "Dojo X"}, false)
	assert.ErrorIs(t, err, ErrCompetitionNotInSetup, "AddParticipant must reject when status has advanced past setup")
}

// TestReplaceParticipant_RejectedAfterStart pins the same in-lock guard for
// the rename/replace path. UpdateParticipant (the check-in toggle path) must
// stay unconditional — that's verified by the existing UpdateParticipant
// tests, which never set Status to anything other than empty/setup.
func TestReplaceParticipant_RejectedAfterStart(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "started-replace"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Started", Status: CompStatusSetup}))

	added, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)

	// Advance the competition.
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Started", Status: CompStatusPools}))

	_, err = store.ReplaceParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.Name = "Alice Renamed"
		return nil
	})
	assert.ErrorIs(t, err, ErrCompetitionNotInSetup, "ReplaceParticipant must reject when status has advanced past setup")

	// Sanity: UpdateParticipant (the check-in path) MUST still work after start,
	// otherwise we've regressed the existing check-in toggle flow.
	_, err = store.UpdateParticipant(compID, added.ID, false, func(p *domain.Player) error {
		p.CheckedIn = true
		return nil
	})
	require.NoError(t, err, "UpdateParticipant must remain unconditional so check-in toggles work after start")
}

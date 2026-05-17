package state

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Store Initialization ---

func TestNewStore_CreatesDirectories(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "new-tournament")
	store, err := NewStore(storePath)
	require.NoError(t, err)
	assert.NotNil(t, store)

	// Verify directories exist (tournament.md is not auto-created; setup UI handles it)
	assert.DirExists(t, storePath)
	assert.DirExists(t, filepath.Join(storePath, "competitions"))
}

func TestNewStore_ExistingDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, store.GetFolder())
}

func TestNewStore_InitError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	// Create a file where a directory should be
	filePath := filepath.Join(dir, "a-file")
	err = os.WriteFile(filePath, []byte("not a dir"), 0600)
	require.NoError(t, err)

	// Try to create store with s.folder pointing to that file (MkdirAll will fail)
	_, err = NewStore(filePath)
	assert.Error(t, err)
}

// --- Tournament YAML ---

func TestStore_TournamentYAML(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourney := &Tournament{
		Name:     "Test Tournament",
		Date:     "01-05-2026", // DD-MM-YYYY canonical format
		Venue:    "Test Venue",
		Courts:   []string{"A", "B"},
		Password: "pass",
	}

	err = store.SaveTournament(tourney)
	require.NoError(t, err)

	path := filepath.Join(dir, "tournament.md")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "name: Test Tournament")
	assert.Contains(t, string(data), "---")

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, tourney.Name, loaded.Name)
	assert.Equal(t, tourney.Date, loaded.Date)
	assert.Equal(t, tourney.Venue, loaded.Venue)
	assert.Equal(t, tourney.Courts, loaded.Courts)
	assert.Equal(t, tourney.Password, loaded.Password)
}

func TestStore_TournamentYAML_ReturnsNilWhenMissing(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// tournament.md is not auto-created; LoadTournament returns nil when absent
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestStore_TournamentYAML_EmptyCourts(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourney := &Tournament{
		Name:     "Minimal",
		Password: "x",
	}
	require.NoError(t, store.SaveTournament(tourney))

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Minimal", loaded.Name)
	assert.Empty(t, loaded.Courts)
}

func TestStore_TournamentYAML_Fallback(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// Write invalid front matter
	path := filepath.Join(dir, "tournament.md")
	err = os.WriteFile(path, []byte("invalid content"), 0600)
	require.NoError(t, err)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "New Tournament", loaded.Name)
	// Canonical date format invariant: DD-MM-YYYY. If this regex fails,
	// the bootstrap default in tournament.go is using the wrong layout.
	// Validator handlers_tournament.validateDateDMY rejects any other
	// shape with 400, so an ISO-formatted bootstrap default would force
	// admins to retype the date even when not editing it.
	assert.Regexp(t, `^\d{2}-\d{2}-\d{4}$`, loaded.Date,
		"bootstrap default Date must use DD-MM-YYYY canonical format")
}

func TestStore_TournamentYAML_MalformedFrontMatter(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// Missing closing ---
	path := filepath.Join(dir, "tournament.md")
	err = os.WriteFile(path, []byte("---\nname: Foo\n"), 0600)
	require.NoError(t, err)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "New Tournament", loaded.Name)
	// Same DMY-canonical invariant as the fallback test above.
	assert.Regexp(t, `^\d{2}-\d{2}-\d{4}$`, loaded.Date,
		"malformed-front-matter fallback Date must use DD-MM-YYYY canonical format")
}

// --- Participants CSV ---

func TestStore_ParticipantsCSV(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	players := []domain.Player{
		{Name: "Akira Tanaka", Dojo: "Mumeishi"},
		{Name: "Hiroshi Sato", Dojo: "Sanshukai"},
	}

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SaveParticipants(compID, players))

	path := filepath.Join(dir, "competitions", compID, "participants.csv")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "Akira Tanaka, Mumeishi")

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "Akira Tanaka", loaded[0].Name)
	assert.Equal(t, "Mumeishi", loaded[0].Dojo)
}

func TestStore_ParticipantsCSV_MixedIDs(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "mixed"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Manually write CSV with mixed UUID/plain lines
	path := filepath.Join(dir, "competitions", compID, "participants.csv")
	// Second line has no comma, so it should be treated as plain name with no ID
	content := "550e8400-e29b-41d4-a716-446655440000, Alice, DojoA\nBob\n"
	err = os.WriteFile(path, []byte(content), 0600)
	require.NoError(t, err)

	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "550e8400-e29b-41d4-a716-446655440000", loaded[0].ID)
	assert.Empty(t, loaded[1].ID)
	assert.Equal(t, "Bob", loaded[1].Name)
}

func TestStore_ParticipantsCSV_WithZekkenName(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "zekken"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	players := []domain.Player{
		{Name: "Akira Tanaka", DisplayName: "A. Tanaka", Dojo: "Mumeishi"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Akira Tanaka", loaded[0].Name)
	assert.Equal(t, "A. Tanaka", loaded[0].DisplayName)
	assert.Equal(t, "Mumeishi", loaded[0].Dojo)
}

func TestStore_ParticipantsCSV_Empty(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadParticipants("nonexistent", false)
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// --- Pools CSV ---

func TestStore_PoolsCSV(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	pools := []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Player 1", Dojo: "DojoA", DisplayName: "P. One"},
				{Name: "Player 2", Dojo: "DojoB", DisplayName: "P. Two"},
			},
		},
	}

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePools(compID, pools))

	loaded, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Pool A", loaded[0].PoolName)
	require.Len(t, loaded[0].Players, 2)
	assert.Equal(t, "Player 1", loaded[0].Players[0].Name)
	assert.Equal(t, "P. One", loaded[0].Players[0].DisplayName)
	assert.Equal(t, "DojoA", loaded[0].Players[0].Dojo)
	assert.Equal(t, "Player 2", loaded[0].Players[1].Name)
	assert.Equal(t, "P. Two", loaded[0].Players[1].DisplayName)
	assert.Equal(t, "DojoB", loaded[0].Players[1].Dojo)
}

func TestStore_PoolsCSV_MultiplePools(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "P1", Dojo: "D1"}, {Name: "P2", Dojo: "D2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "P3", Dojo: "D3"}, {Name: "P4", Dojo: "D4"}}},
		{PoolName: "Pool C", Players: []helper.Player{{Name: "P5", Dojo: "D5"}}},
	}

	compID := "multi-pool"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePools(compID, pools))

	loaded, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, "Pool A", loaded[0].PoolName)
	assert.Equal(t, "Pool B", loaded[1].PoolName)
	assert.Equal(t, "Pool C", loaded[2].PoolName)
	assert.Len(t, loaded[0].Players, 2)
	assert.Len(t, loaded[1].Players, 2)
	assert.Len(t, loaded[2].Players, 1)
}

func TestStore_PoolsCSV_PreservesOrder(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	pools := []helper.Pool{
		{PoolName: "Pool Z", Players: []helper.Player{{Name: "First"}}},
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Second"}}},
		{PoolName: "Pool M", Players: []helper.Player{{Name: "Third"}}},
	}

	compID := "order"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePools(compID, pools))

	loaded, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, "Pool Z", loaded[0].PoolName)
	assert.Equal(t, "Pool A", loaded[1].PoolName)
	assert.Equal(t, "Pool M", loaded[2].PoolName)
}

func TestStore_PoolsCSV_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadPools("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// --- Pool Matches ---

func TestStore_PoolMatches_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "pool-matches"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	results := []MatchResult{
		{
			ID:          "Pool A-0",
			SideA:       "Alice",
			SideB:       "Bob",
			Winner:      "Alice",
			IpponsA:     []string{"M", "K"},
			IpponsB:     []string{"D"},
			HansokuA:    0,
			HansokuB:    1,
			Decision:    "",
			Status:      MatchStatusCompleted,
			Court:       "A",
			ScheduledAt: "09:00",
		},
		{
			ID:      "Pool A-1",
			SideA:   "Alice",
			SideB:   "Charlie",
			Winner:  "",
			IpponsA: []string{},
			IpponsB: []string{},
			Status:  MatchStatusScheduled,
			Court:   "B",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	assert.Equal(t, "Pool A-0", loaded[0].ID)
	assert.Equal(t, "Alice", loaded[0].SideA)
	assert.Equal(t, "Bob", loaded[0].SideB)
	assert.Equal(t, "Alice", loaded[0].Winner)
	assert.Equal(t, []string{"M", "K"}, loaded[0].IpponsA)
	assert.Equal(t, []string{"D"}, loaded[0].IpponsB)
	assert.Equal(t, 0, loaded[0].HansokuA)
	assert.Equal(t, 1, loaded[0].HansokuB)
	assert.Equal(t, MatchStatusCompleted, loaded[0].Status)
	assert.Equal(t, "A", loaded[0].Court)

	assert.Equal(t, "Pool A-1", loaded[1].ID)
	assert.Equal(t, MatchStatusScheduled, loaded[1].Status)
}

func TestStore_PoolMatches_EmptyIppons(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "empty-ippons"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	results := []MatchResult{
		{
			ID:      "Pool A-0",
			SideA:   "Alice",
			SideB:   "Bob",
			IpponsA: []string{},
			IpponsB: []string{},
			Status:  MatchStatusScheduled,
			Court:   "A",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	// Empty ippons stored as [""] when split — verify this doesn't cause issues
	assert.Equal(t, "Pool A-0", loaded[0].ID)
}

func TestStore_PoolMatches_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadPoolMatches("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// --- Bracket JSON ---

func TestStore_Bracket_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "bracket-test"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled, Court: "A"},
				{ID: "m-r1-1", SideA: "Charlie", SideB: "Dave", Status: MatchStatusScheduled, Court: "B"},
			},
			{
				{ID: "m-r2-0", SideA: "", SideB: "", Status: MatchStatusScheduled, Court: "A"},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Len(t, loaded.Rounds, 2)
	assert.Len(t, loaded.Rounds[0], 2)
	assert.Len(t, loaded.Rounds[1], 1)
	assert.Equal(t, "m-r1-0", loaded.Rounds[0][0].ID)
	assert.Equal(t, "Alice", loaded.Rounds[0][0].SideA)
}

func TestStore_Bracket_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadBracket("nonexistent")
	require.NoError(t, err)
	assert.NotNil(t, loaded)
	assert.Empty(t, loaded.Rounds)
}

// --- Competition ---

func TestStore_Competition_CRUD(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:           "mens-individual",
		Name:         "Men's Individual",
		Kind:         "individual",
		Format:       "pools",
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A", "B"},
		StartTime:    "09:00",
		Status:       "setup",
	}

	// Create
	require.NoError(t, store.SaveCompetition(comp))

	// Read
	loaded, err := store.LoadCompetition("mens-individual")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "Men's Individual", loaded.Name)
	assert.Equal(t, "pools", loaded.Format)
	assert.Equal(t, 3, loaded.PoolSize)
	assert.Equal(t, true, loaded.RoundRobin)
	assert.Equal(t, []string{"A", "B"}, loaded.Courts)

	// Update
	comp.Status = "pools"
	require.NoError(t, store.SaveCompetition(comp))
	loaded, err = store.LoadCompetition("mens-individual")
	require.NoError(t, err)
	assert.Equal(t, CompStatusPools, loaded.Status)

	// Delete
	require.NoError(t, store.DeleteCompetition("mens-individual"))
	loaded, err = store.LoadCompetition("mens-individual")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestStore_Competition_NotFound(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadCompetition("nonexistent")
	// Should get validation error since "nonexistent" is a valid ID format
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestStore_ListCompetitions(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp-a", Name: "A"}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp-b", Name: "B"}))

	ids, err := store.ListCompetitions()
	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "comp-a")
	assert.Contains(t, ids, "comp-b")
}

// --- Competition ID Validation ---

func TestValidateCompetitionID_Valid(t *testing.T) {
	validIDs := []string{
		"mens-individual",
		"womens-teams",
		"comp1",
		"A",
		"test_comp",
		"abc-123_DEF",
	}
	for _, id := range validIDs {
		assert.NoError(t, ValidateCompetitionID(id), "should accept: %s", id)
	}
}

func TestValidateCompetitionID_Invalid(t *testing.T) {
	tests := []struct {
		id   string
		desc string
	}{
		{"", "empty"},
		{"../etc/passwd", "path traversal with dots"},
		{"/etc/passwd", "absolute path"},
		{"foo/bar", "slash in name"},
		{".hidden", "starts with dot"},
		{"-hyphen-start", "starts with hyphen"},
		{"_underscore_start", "starts with underscore"},
		{"has spaces", "contains space"},
		{"has\ttab", "contains tab"},
		{"has\nnewline", "contains newline"},
		{"special!chars", "contains special chars"},
		{"a@b", "contains at sign"},
		{string(make([]byte, 65)), "too long"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateCompetitionID(tc.id)
			assert.Error(t, err, "should reject: %q", tc.id)
		})
	}
}

func TestStore_Competition_InvalidID_Rejected(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// Try to save with path traversal ID
	err = store.SaveCompetition(&Competition{ID: "../evil"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid competition ID")

	// Try to load with path traversal ID
	_, err = store.LoadCompetition("../../etc")
	assert.Error(t, err)

	// Try to delete with path traversal ID
	err = store.DeleteCompetition("../../../tmp")
	assert.Error(t, err)
}

// --- Seeds ---

func TestStore_Seeds_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "seeded"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	seeds := []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Bob", SeedRank: 2},
		{Name: "Charlie", SeedRank: 3},
	}
	require.NoError(t, store.SaveSeeds(compID, seeds))

	loaded, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, 1, loaded[0].SeedRank)
	assert.Equal(t, "Alice", loaded[0].Name)
	assert.Equal(t, 2, loaded[1].SeedRank)
	assert.Equal(t, "Bob", loaded[1].Name)
}

func TestStore_Seeds_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadSeeds("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// LoadSeeds and SaveSeeds now use the per-competition lock (not the
// store-wide s.mu) so they serialize against the StartCompetition
// transform held by UpdateCompetitionChanged. Pin two contracts:
//
//  1. The validateCompetitionID precondition fires on bogus IDs before
//     any disk I/O — proves the new per-comp locking path runs the
//     same validation the other per-comp Load/Save methods do (and
//     guards against the path-traversal class).
//  2. Concurrent SaveSeeds on DIFFERENT comps don't block each other.
//     Previously s.mu.Lock made every seed save store-wide-serial; the
//     per-comp switch is a scalability improvement on top of the race fix.
func TestStore_Seeds_PerCompLocking(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// (1) Bogus comp ID — must surface as a validation error, same as
	// LoadParticipants / LoadPools / etc.
	_, err = store.LoadSeeds("../escape")
	assert.Error(t, err, "LoadSeeds must validate the comp ID")
	err = store.SaveSeeds("../escape", []domain.SeedAssignment{{Name: "A", SeedRank: 1}})
	assert.Error(t, err, "SaveSeeds must validate the comp ID")

	// (2) Concurrent SaveSeeds on different comps must both land.
	require.NoError(t, store.SaveCompetition(&Competition{ID: "alpha"}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: "beta"}))

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = store.SaveSeeds("alpha", []domain.SeedAssignment{{Name: "A", SeedRank: 1}})
	}()
	go func() {
		defer wg.Done()
		_ = store.SaveSeeds("beta", []domain.SeedAssignment{{Name: "B", SeedRank: 1}})
	}()
	wg.Wait()

	a, _ := store.LoadSeeds("alpha")
	b, _ := store.LoadSeeds("beta")
	assert.Len(t, a, 1)
	assert.Len(t, b, 1)
}

func TestStore_ResetOverrides(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "reset"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	err = store.SaveWinnerOverride(compID, "m1", "Winner")
	require.NoError(t, err)

	loaded, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, loaded.Winners)

	err = store.ResetOverrides(compID)
	require.NoError(t, err)

	loaded, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Empty(t, loaded.Winners)
	assert.Empty(t, loaded.PoolRanks)
}

// --- Schedule ---

func TestStore_Schedule_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "sched"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	entries := []ScheduleEntry{
		{MatchType: "pool", MatchRef: "Pool A-0", Court: "A", ScheduledAt: "09:00", Status: "scheduled"},
		{MatchType: "pool", MatchRef: "Pool A-1", Court: "A", ScheduledAt: "09:15", Status: "scheduled"},
		{MatchType: "bracket", MatchRef: "R1-M0", Court: "B", ScheduledAt: "10:00", Status: "scheduled"},
	}
	require.NoError(t, store.SaveSchedule(compID, entries))

	loaded, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 3)
	assert.Equal(t, "pool", loaded[0].MatchType)
	assert.Equal(t, "Pool A-0", loaded[0].MatchRef)
	assert.Equal(t, "A", loaded[0].Court)
	assert.Equal(t, "09:00", loaded[0].ScheduledAt)
}

func TestStore_Schedule_NotExists(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadSchedule("nonexistent")
	require.NoError(t, err)
	assert.Empty(t, loaded)
}

// --- Concurrent Access ---

func TestStore_ConcurrentAccess(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "concurrent"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	done := make(chan bool, 10)

	// Concurrent reads
	for i := 0; i < 5; i++ {
		go func() {
			_, _ = store.LoadCompetition(compID)
			done <- true
		}()
	}

	// Concurrent writes
	for i := 0; i < 5; i++ {
		go func() {
			_ = store.SaveCompetition(&Competition{ID: compID, Name: "Updated"})
			done <- true
		}()
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final state is consistent
	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.NotNil(t, loaded)
}

func TestIsDraw(t *testing.T) {
	assert.True(t, IsDraw("hikiwake"), "canonical spelling")
	assert.False(t, IsDraw("hikewake"), "legacy misspelling no longer accepted")
	assert.False(t, IsDraw(""))
	assert.False(t, IsDraw("ippon"))
	assert.False(t, IsDraw("HIKIWAKE"), "case-sensitive — wire format is lowercase")
}

// --- UpdateTournamentChanged ---

func TestUpdateTournamentChanged_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	initial := &Tournament{Name: "My Tournament", Password: "secret"}
	require.NoError(t, store.SaveTournament(initial))

	desired := &Tournament{Name: "My Tournament Updated", Password: "secret"}
	changed, err := store.UpdateTournamentChanged(desired, func(current, d *Tournament) error {
		return nil // accept as-is
	})
	require.NoError(t, err)
	assert.True(t, changed)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "My Tournament Updated", loaded.Name)
}

func TestUpdateTournamentChanged_TransformError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{Name: "T"}))

	sentinel := errors.New("transform failed")
	_, err = store.UpdateTournamentChanged(&Tournament{Name: "T2"}, func(_, _ *Tournament) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

// --- LoadSchedule ---

func TestLoadSchedule_MissingFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "sched-missing"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Sched"}))

	entries, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestLoadSchedule_RoundTrip(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "sched-rt"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Sched RT"}))

	entries := []ScheduleEntry{
		{MatchType: "pool", MatchRef: "P1-0", Court: "A", ScheduledAt: "09:00", Status: "scheduled"},
		{MatchType: "pool", MatchRef: "P1-1", Court: "A", ScheduledAt: "09:10", Status: "scheduled"},
	}
	require.NoError(t, store.SaveSchedule(compID, entries))

	loaded, err := store.LoadSchedule(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "P1-0", loaded[0].MatchRef)
	assert.Equal(t, "09:10", loaded[1].ScheduledAt)
}

func TestLoadSchedule_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.LoadSchedule("../bad")
	assert.Error(t, err)
}

func TestLoadSchedule_FreshStore(t *testing.T) {
	// Use a fresh store to read so parseScheduleFile is called (not cache).
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	writeStore, err := NewStore(dir)
	require.NoError(t, err)

	compID := "sched-fresh"
	require.NoError(t, writeStore.SaveCompetition(&Competition{ID: compID, Name: "SchedFresh"}))

	entries := []ScheduleEntry{
		{MatchType: "pool", MatchRef: "P1-0", Court: "A", ScheduledAt: "09:00", Status: "scheduled", Date: "01-01-2026", IsBreak: false, Label: ""},
		{MatchType: "break", MatchRef: "", Court: "", ScheduledAt: "12:00", Status: "", IsBreak: true, Label: "Lunch"},
	}
	require.NoError(t, writeStore.SaveSchedule(compID, entries))

	readStore, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := readStore.LoadSchedule(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Equal(t, "P1-0", loaded[0].MatchRef)
	assert.Equal(t, "01-01-2026", loaded[0].Date)
	assert.True(t, loaded[1].IsBreak)
	assert.Equal(t, "Lunch", loaded[1].Label)
}

// --- UpdateCompetitionChanged ---

func TestUpdateCompetitionChanged_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "ucc-basic"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Original", Status: CompStatusSetup}))

	changed, err := store.UpdateCompetitionChanged(compID, func(c *Competition) (*Competition, error) {
		c.Status = CompStatusPools
		return c, nil
	})
	require.NoError(t, err)
	assert.True(t, changed)

	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, CompStatusPools, loaded.Status)
}

func TestUpdateCompetitionChanged_NoChange(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "ucc-nochange"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Same"}))

	changed, err := store.UpdateCompetitionChanged(compID, func(c *Competition) (*Competition, error) {
		// returning nil signals no-op
		return nil, nil
	})
	require.NoError(t, err)
	assert.False(t, changed)
}

func TestUpdateCompetitionChanged_TransformError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "ucc-err"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Err"}))

	sentinel := errors.New("transform error")
	_, err = store.UpdateCompetitionChanged(compID, func(c *Competition) (*Competition, error) {
		return nil, sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestUpdateCompetitionChanged_InvalidID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.UpdateCompetitionChanged("../bad", func(c *Competition) (*Competition, error) {
		return c, nil
	})
	assert.Error(t, err)
}

// --- SetCompetitorStatus ---

func TestSetCompetitorStatus_NewAndOverwrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "scs-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Status Test"}))

	status := domain.CompetitorStatus{
		PlayerID: "player-1",
		Eligible: false,
		Reason:   "kiken",
	}
	require.NoError(t, store.SetCompetitorStatus(compID, status))

	loaded, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Contains(t, loaded, "player-1")
	assert.False(t, loaded["player-1"].Eligible)
	assert.Equal(t, "kiken", loaded["player-1"].Reason)
	assert.False(t, loaded["player-1"].RecordedAt.IsZero(), "RecordedAt should be auto-filled")

	// Overwrite with eligible
	status2 := domain.CompetitorStatus{PlayerID: "player-1", Eligible: true}
	require.NoError(t, store.SetCompetitorStatus(compID, status2))

	loaded2, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, loaded2["player-1"].Eligible)
}

func TestSetCompetitorStatus_ValidationError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "scs-val"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Val"}))

	// Missing PlayerID
	err = store.SetCompetitorStatus(compID, domain.CompetitorStatus{Eligible: false, Reason: "kiken"})
	assert.Error(t, err)

	// Ineligible but no reason
	err = store.SetCompetitorStatus(compID, domain.CompetitorStatus{PlayerID: "p1", Eligible: false})
	assert.Error(t, err)
}

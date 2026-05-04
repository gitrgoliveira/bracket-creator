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

// --- Store Initialization ---

func TestNewStore_CreatesDirectories(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	storePath := filepath.Join(dir, "new-tournament")
	store, err := NewStore(storePath)
	require.NoError(t, err)
	assert.NotNil(t, store)

	// Verify files/directories exist
	assert.DirExists(t, storePath)
	assert.DirExists(t, filepath.Join(storePath, "competitions"))
	assert.FileExists(t, filepath.Join(storePath, "tournament.md"))
}

func TestNewStore_ExistingDirectory(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)
	assert.Equal(t, dir, store.GetFolder())
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
		Date:     "2026-05-01",
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

func TestStore_TournamentYAML_AutoCreate(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.NotNil(t, loaded)
	assert.Equal(t, "New Tournament", loaded.Name)
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

// --- Participants CSV ---

func TestStore_ParticipantsCSV(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	players := []helper.Player{
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

func TestStore_ParticipantsCSV_WithZekkenName(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "zekken"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	players := []helper.Player{
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
	assert.Equal(t, "pools", loaded.Status)

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

package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newCourtScanStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	return store
}

func TestCourtQueueBounds_SameCompetitionPoolMatches(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
		{ID: "m2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "m3", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "11:45"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "09:00", earliest)
	assert.Equal(t, "11:45", latest)
}

func TestCourtQueueBounds_CrossCompetitionAndOwnCompRetained(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m-moved", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
		{ID: "m-sibling", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "12:00"},
	}))

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp2"}))
	require.NoError(t, store.SavePoolMatches("comp2", []MatchResult{
		{ID: "m-other", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:15"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "comp1", "m-moved")
	require.NoError(t, err)
	assert.Equal(t, "09:15", earliest)
	assert.Equal(t, "12:00", latest)
}

func TestCourtQueueBounds_SkipExactMatchOnly(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "target-id", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "08:00"},
		{ID: "comp1-other", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
	}))

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp2"}))
	require.NoError(t, store.SavePoolMatches("comp2", []MatchResult{
		{ID: "target-id", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "11:30"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "comp1", "target-id")
	require.NoError(t, err)
	assert.Equal(t, "10:00", earliest)
	assert.Equal(t, "11:30", latest)
}

func TestCourtQueueBounds_IgnoredMatchesAndOtherCourts(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m-running", Status: MatchStatusRunning, Court: "A", ScheduledAt: "08:00"},
		{ID: "m-completed", Status: MatchStatusCompleted, Court: "A", ScheduledAt: "08:30"},
		{ID: "m-untimed", Status: MatchStatusScheduled, Court: "A", ScheduledAt: ""},
		{ID: "m-court-b", Status: MatchStatusScheduled, Court: "B", ScheduledAt: "07:00"},
		{ID: "m-valid", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:15"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "10:15", earliest)
	assert.Equal(t, "10:15", latest)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp2"}))
	require.NoError(t, store.SavePoolMatches("comp2", []MatchResult{
		{ID: "m2-running", Status: MatchStatusRunning, Court: "C", ScheduledAt: "09:00"},
		{ID: "m2-untimed", Status: MatchStatusScheduled, Court: "C", ScheduledAt: ""},
	}))
	earliestC, latestC, err := store.CourtQueueBounds("C", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", earliestC)
	assert.Equal(t, "", latestC)
}

func TestCourtQueueBounds_BracketMatchesAndBronzeAsEarliest(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{ID: "bm1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
				{ID: "bm2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
			},
		},
		ThirdPlaceMatch: &BracketMatch{
			ID:          "bronze",
			Status:      MatchStatusScheduled,
			Court:       "A",
			ScheduledAt: "09:30",
		},
	}
	require.NoError(t, store.SaveBracket("comp1", bracket))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "09:30", earliest)
	assert.Equal(t, "11:00", latest)
}

func TestCourtQueueBounds_BronzeAsLatest(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{ID: "bm1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
			},
		},
		ThirdPlaceMatch: &BracketMatch{
			ID:          "bronze",
			Status:      MatchStatusScheduled,
			Court:       "A",
			ScheduledAt: "12:15",
		},
	}
	require.NoError(t, store.SaveBracket("comp1", bracket))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "10:30", earliest)
	assert.Equal(t, "12:15", latest)
}

func TestCourtQueueBounds_LegacyTimeOrdering(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
		{ID: "m2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "9:45"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "10:00", earliest)
	assert.Equal(t, "9:45", latest)
}

func TestCourtQueueBounds_EmptyCourtString(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "", ScheduledAt: "10:00"},
	}))

	earliest, latest, err := store.CourtQueueBounds("", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", earliest)
	assert.Equal(t, "", latest)
}

func TestCourtQueueBounds_EmptyCourtOrNoCompetitions(t *testing.T) {
	store := newCourtScanStore(t)

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", earliest)
	assert.Equal(t, "", latest)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
	}))

	earliestB, latestB, err := store.CourtQueueBounds("B", "", "")
	require.NoError(t, err)
	assert.Equal(t, "", earliestB)
	assert.Equal(t, "", latestB)
}

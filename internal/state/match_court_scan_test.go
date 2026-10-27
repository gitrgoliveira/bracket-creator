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
				{ID: "bm1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
				{ID: "bm2", SideA: "Carol", SideB: "Dave", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
			},
		},
		ThirdPlaceMatch: &BracketMatch{
			ID:          "bronze",
			SideA:       "Eve",
			SideAID:     "eve",
			SideB:       "Frank",
			SideBID:     "frank",
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
				{ID: "bm1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
			},
		},
		ThirdPlaceMatch: &BracketMatch{
			ID:          "bronze",
			SideA:       "Eve",
			SideAID:     "eve",
			SideB:       "Frank",
			SideBID:     "frank",
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

// A legacy "9:45" cannot be ordered against "10:00" as a string, so it is not
// a bound: it would read as the latest and turn placement off for the court.
func TestCourtQueueBounds_NonCanonicalTimesAreSkipped(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
		{ID: "m2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "9:45"},
		{ID: "m3", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "25:00"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "10:00", earliest)
	assert.Equal(t, "10:00", latest)
}

// A knockout match whose sides are not both competitors yet is not a queue
// entry, and neither is a pool bout's own knockout; another competition's
// knockout match with its competitors known is.
func TestCourtQueueBounds_UnresolvedAndOwnKnockoutRowsAreSkipped(t *testing.T) {
	store := newCourtScanStore(t)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "mixed"}))
	require.NoError(t, store.SavePoolMatches("mixed", []MatchResult{
		{ID: "Pool A-0", SideA: "P1", SideAID: "p1", SideB: "P2", SideBID: "p2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
	}))
	require.NoError(t, store.SaveBracket("mixed", &Bracket{
		Rounds: [][]BracketMatch{{
			{ID: "r1-m0", SideA: "Q1", SideAID: "q1", SideB: "Q2", SideBID: "q2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
			{ID: "r1-m1", SideA: "Pool A-1st", SideB: "Pool B-2nd", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:05"},
		}},
	}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: "ko"}))
	require.NoError(t, store.SaveBracket("ko", &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "r1-m0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "08:30"}},
			{{ID: "r2-m0", SideA: "Winner of r1-m0", SideB: "Winner of r1-m1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "12:00"}},
		},
		ThirdPlaceMatch: &BracketMatch{ID: "bronze", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "12:30"},
	}))

	earliest, latest, err := store.CourtQueueBounds("A", "", "")
	require.NoError(t, err)
	assert.Equal(t, "08:30", earliest)
	assert.Equal(t, "10:00", latest, "the resolved knockout row counts, the placeholders do not")

	earliest, latest, err = store.CourtQueueBounds("A", "ko", "r1-m0")
	require.NoError(t, err)
	assert.Equal(t, "09:00", earliest, "a knockout match moved skips itself only")
	assert.Equal(t, "10:00", latest, "a knockout match moved keeps the other knockout rows")

	require.NoError(t, store.SavePoolMatches("mixed", []MatchResult{
		{ID: "Pool A-0", SideA: "P1", SideAID: "p1", SideB: "P2", SideBID: "p2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool B-0", SideA: "P3", SideAID: "p3", SideB: "P4", SideBID: "p4", Status: MatchStatusScheduled, Court: "B", ScheduledAt: "09:00"},
	}))
	earliest, latest, err = store.CourtQueueBounds("A", "mixed", "Pool B-0")
	require.NoError(t, err)
	assert.Equal(t, "08:30", earliest)
	assert.Equal(t, "09:00", latest, "a pool bout's own knockout is not its queue neighbour")
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

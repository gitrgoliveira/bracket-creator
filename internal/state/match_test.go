package state

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunningMatchOnCourt_FreeCourt(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A"},
	}))

	occ, err := store.RunningMatchOnCourt("A", "")
	require.NoError(t, err)
	assert.Nil(t, occ, "scheduled match should not block the court")
}

func TestRunningMatchOnCourt_BusySameCourt(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))

	occ, err := store.RunningMatchOnCourt("A", "")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "m1", occ.MatchID)
}

func TestRunningMatchOnCourt_SkipCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))

	// With skipCompID=comp1, the running match in comp1 should be ignored.
	occ, err := store.RunningMatchOnCourt("A", "comp1")
	require.NoError(t, err)
	assert.Nil(t, occ, "skipCompID competition should be excluded from scan")
}

func TestRunningMatchOnCourt_CrossCompetition(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// comp1 has a running match on court A; comp2 wants to start on court A.
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp2"}))
	require.NoError(t, store.SavePoolMatches("comp2", []MatchResult{
		{ID: "m2", Status: MatchStatusScheduled, Court: "A"},
	}))

	// Scanning from comp2's perspective (skipCompID=comp2) should find comp1's match.
	occ, err := store.RunningMatchOnCourt("A", "comp2")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "m1", occ.MatchID)
}

func TestRunningMatchOnCourt_BracketMatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "bm1", Status: MatchStatusRunning, Court: "B"}},
		},
	}
	require.NoError(t, store.SaveBracket("comp1", bracket))

	occ, err := store.RunningMatchOnCourt("B", "")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "bm1", occ.MatchID)
}

// TestRunningMatchOnCourt_BronzeMatch pins the branch a rounds-only loop never
// reaches: the 3rd-place match is a SIBLING of Rounds, not an element of it, so
// a bronze running on a court must still make that court busy. Added with the
// no-copy rewrite of the scan (mp-gmcg review R9) — the branch it protects is
// the one this repo has repeatedly lost to hand-copied bracket walks.
func TestRunningMatchOnCourt_BronzeMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SaveBracket("comp1", &Bracket{
		Rounds:          [][]BracketMatch{{{ID: "bm1", Status: MatchStatusCompleted, Court: "B"}}},
		ThirdPlaceMatch: &BracketMatch{ID: "bronze", Status: MatchStatusRunning, Court: "B"},
	}))

	occ, err := store.RunningMatchOnCourt("B", "")
	require.NoError(t, err)
	require.NotNil(t, occ, "a running bronze match must occupy its court")
	assert.Equal(t, "bronze", occ.MatchID)
}

func TestRunningMatchOnCourt_EmptyCourtIsNeverBusy(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: ""},
	}))

	occ, err := store.RunningMatchOnCourt("", "")
	require.NoError(t, err)
	assert.Nil(t, occ, "empty court string should never be considered busy")
}

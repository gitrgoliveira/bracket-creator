package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func drawMatchesFixture() ([]MatchResult, *Bracket) {
	pool := []MatchResult{
		{ID: "Pool A-0", SideA: "A", SideB: "B", SideAID: "a", SideBID: "b",
			SubResults: []SubMatchResult{{Position: 1, IpponsA: []string{"M"}, Winner: "A"}}},
		{ID: "Pool A-DH-0", SideAID: "a", SideBID: "b"},
	}
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "r0-m0", SideAID: "a", SideBID: "x"}, {ID: "r0-m1", SideAID: "b", SideBID: "c"}},
			{{ID: "r1-m0"}},
		},
		ThirdPlaceMatch: &BracketMatch{ID: "bronze", SideAID: "b", SideBID: "c"},
	}
	return pool, bracket
}

// TestDrawMatchesFrom pins the walk: pool matches, then each bracket round in
// stored order, then the 3rd-place match, which sits at len(Rounds) because it
// is a sibling of the rounds and not one of them.
func TestDrawMatchesFrom(t *testing.T) {
	pool, bracket := drawMatchesFixture()

	t.Run("pool matches, then the rounds, then the 3rd-place match", func(t *testing.T) {
		assert.Equal(t, []DrawMatch{
			{ID: "Pool A-0", SideAID: "a", SideBID: "b"},
			{ID: "Pool A-DH-0", SideAID: "a", SideBID: "b"},
			{ID: "r0-m0", SideAID: "a", SideBID: "x", Knockout: true, Round: 0, Index: 0},
			{ID: "r0-m1", SideAID: "b", SideBID: "c", Knockout: true, Round: 0, Index: 1},
			{ID: "r1-m0", Knockout: true, Round: 1, Index: 0},
			{ID: "bronze", SideAID: "b", SideBID: "c", Knockout: true, Round: 2, Index: 0},
		}, DrawMatchesFrom(pool, bracket))
	})

	t.Run("a nil bracket leaves the knockout out", func(t *testing.T) {
		got := DrawMatchesFrom(pool, nil)

		require.Len(t, got, 2)
		assert.False(t, got[0].Knockout)
	})

	t.Run("no matches and no bracket is an empty draw", func(t *testing.T) {
		assert.Empty(t, DrawMatchesFrom(nil, nil))
		assert.Empty(t, DrawMatchesFrom([]MatchResult{}, &Bracket{Rounds: [][]BracketMatch{}}))
	})

	t.Run("keeps no reference into its arguments", func(t *testing.T) {
		got := DrawMatchesFrom(pool, bracket)
		got[0].SideAID = "changed"

		assert.Equal(t, "a", pool[0].SideAID)
	})
}

func newDrawMatchesStore(t *testing.T) (*Store, string) {
	t.Helper()
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID = "draw-matches"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))
	return store, compID
}

func writeCompFile(t *testing.T, store *Store, compID, file, body string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(store.GetFolder(), "competitions", compID, file), []byte(body), 0o600))
}

// TestStoreDrawMatches covers the no-copy read of what the store holds.
func TestStoreDrawMatches(t *testing.T) {
	t.Run("agrees with the full-copy loaders", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		pool, bracket := drawMatchesFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool))
		require.NoError(t, store.SaveBracket(compID, bracket))

		loadedPool, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		loadedBracket, err := store.LoadBracket(compID)
		require.NoError(t, err)

		got, err := store.DrawMatches(compID)

		require.NoError(t, err)
		assert.Equal(t, DrawMatchesFrom(loadedPool, loadedBracket), got)
		assert.Len(t, got, 6)
	})

	t.Run("a competition with no draw has none", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)

		got, err := store.DrawMatches(compID)

		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("sees every write at once", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		pool, bracket := drawMatchesFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool[:1]))
		first, err := store.DrawMatches(compID)
		require.NoError(t, err)
		require.Len(t, first, 1)

		require.NoError(t, store.SavePoolMatches(compID, pool))
		require.NoError(t, store.SaveBracket(compID, bracket))
		found, err := store.UpdatePoolMatchByID(compID, "Pool A-0", func(m *MatchResult) error {
			m.SideBID = "rewritten"
			return nil
		})
		require.NoError(t, err)
		require.True(t, found)

		got, err := store.DrawMatches(compID)

		require.NoError(t, err)
		require.Len(t, got, 6)
		assert.Equal(t, "rewritten", got[0].SideBID, "the cache every writer refreshes is the cache it reads")
	})

	t.Run("an unreadable bracket.json is named and the pool matches are still returned", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		pool, _ := drawMatchesFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool))
		writeCompFile(t, store, compID, "bracket.json", "{not json")

		got, err := store.DrawMatches(compID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "bracket.json")
		assert.NotContains(t, err.Error(), "pool-matches.csv")
		assert.Equal(t, DrawMatchesFrom(pool, nil), got)
	})

	t.Run("an unreadable pool-matches.csv is named and the bracket is still returned", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		_, bracket := drawMatchesFixture()
		require.NoError(t, store.SaveBracket(compID, bracket))
		writeCompFile(t, store, compID, "pool-matches.csv", "\"unterminated")

		got, err := store.DrawMatches(compID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool-matches.csv")
		assert.NotContains(t, err.Error(), "bracket.json")
		assert.Equal(t, DrawMatchesFrom(nil, bracket), got)
	})

	t.Run("both files unreadable name both", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		writeCompFile(t, store, compID, "bracket.json", "{not json")
		writeCompFile(t, store, compID, "pool-matches.csv", "\"unterminated")

		got, err := store.DrawMatches(compID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "bracket.json")
		assert.Contains(t, err.Error(), "pool-matches.csv")
		assert.Empty(t, got)
	})

	t.Run("invalid compID errors", func(t *testing.T) {
		store, _ := newDrawMatchesStore(t)

		_, err := store.DrawMatches("../traversal")

		assert.Error(t, err)
	})

	t.Run("what it returns is its own: changing it leaves the next read alone", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		pool, bracket := drawMatchesFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool))
		require.NoError(t, store.SaveBracket(compID, bracket))
		got, err := store.DrawMatches(compID)
		require.NoError(t, err)
		got[0].SideAID = "changed"

		again, err := store.DrawMatches(compID)

		require.NoError(t, err)
		assert.Equal(t, "a", again[0].SideAID)
	})

	t.Run("does not deep-clone the matches the loaders clone", func(t *testing.T) {
		store, compID := newDrawMatchesStore(t)
		var pool []MatchResult
		for i := 0; i < 60; i++ {
			subs := make([]SubMatchResult, 4)
			for j := range subs {
				subs[j] = SubMatchResult{Position: j + 1, IpponsA: []string{"M", "K"}, IpponsB: []string{"D"}, Winner: "A"}
			}
			pool = append(pool, MatchResult{ID: "Pool A-" + string(rune('a'+i%26)) + string(rune('a'+i/26)), SideAID: "a", SideBID: "b", SubResults: subs})
		}
		require.NoError(t, store.SavePoolMatches(compID, pool))
		_, err := store.DrawMatches(compID) // warm the cache and the one-time legacy upgrade
		require.NoError(t, err)

		projected := testing.AllocsPerRun(20, func() { _, _ = store.DrawMatches(compID) })
		cloned := testing.AllocsPerRun(20, func() { _, _ = store.LoadPoolMatches(compID) })

		t.Logf("allocations per read: projection %.0f, deep-copying loader %.0f", projected, cloned)
		assert.Less(t, projected*4, cloned,
			"the projection allocates %.0f per read against the loader's %.0f: it must not copy each match's results", projected, cloned)
	})
}

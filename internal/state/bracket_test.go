package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBracketBytes(t *testing.T) {
	t.Run("empty bytes returns empty rounds", func(t *testing.T) {
		b, err := parseBracketBytes(nil)
		require.NoError(t, err)
		require.NotNil(t, b)
		assert.Empty(t, b.Rounds)
	})

	t.Run("zero-length slice returns empty rounds", func(t *testing.T) {
		b, err := parseBracketBytes([]byte{})
		require.NoError(t, err)
		require.NotNil(t, b)
		assert.Empty(t, b.Rounds)
	})

	t.Run("valid JSON parses correctly", func(t *testing.T) {
		raw := []byte(`{"rounds":[[{"id":"M1","sideA":"Alice","sideB":"Bob","status":"scheduled"}]]}`)
		b, err := parseBracketBytes(raw)
		require.NoError(t, err)
		require.Len(t, b.Rounds, 1)
		require.Len(t, b.Rounds[0], 1)
		assert.Equal(t, "M1", b.Rounds[0][0].ID)
		assert.Equal(t, "Alice", b.Rounds[0][0].SideA)
	})

	t.Run("malformed JSON returns error", func(t *testing.T) {
		_, err := parseBracketBytes([]byte(`{not valid json}`))
		assert.Error(t, err)
	})

	// Regression (Copilot PR #326 round 2): a corrupted/hand-edited bracket.json
	// must not load negative engi flag counts (validated non-negative at the
	// HTTP boundary). parseBracketBytes clamps negatives to 0 on both the rounds
	// and the bronze match, symmetric with the pool CSV parser.
	t.Run("negative engi flag counts are clamped to 0", func(t *testing.T) {
		raw := []byte(`{"rounds":[[{"id":"m1","flagsA":-3,"flagsB":2}]],"thirdPlaceMatch":{"id":"m-bronze","flagsA":1,"flagsB":-5}}`)
		b, err := parseBracketBytes(raw)
		require.NoError(t, err)
		require.Len(t, b.Rounds, 1)
		require.Len(t, b.Rounds[0], 1)
		assert.Equal(t, 0, b.Rounds[0][0].FlagsA, "negative round FlagsA clamped to 0")
		assert.Equal(t, 2, b.Rounds[0][0].FlagsB, "non-negative FlagsB preserved")
		require.NotNil(t, b.ThirdPlaceMatch)
		assert.Equal(t, 1, b.ThirdPlaceMatch.FlagsA, "non-negative bronze FlagsA preserved")
		assert.Equal(t, 0, b.ThirdPlaceMatch.FlagsB, "negative bronze FlagsB clamped to 0")
	})
}

func TestUpdateBracket_Basic(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	// Save an initial bracket
	initial := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{ID: "M1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
				{ID: "M2", SideA: "Charlie", SideB: "Dave", Status: MatchStatusScheduled},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, initial))

	// Mutate M1's winner
	err = store.UpdateBracket(compID, func(b *Bracket) error {
		for i := range b.Rounds[0] {
			if b.Rounds[0][i].ID == "M1" {
				b.Rounds[0][i].Winner = "Alice"
				b.Rounds[0][i].Status = MatchStatusCompleted
			}
		}
		return nil
	})
	require.NoError(t, err)

	updated, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", updated.Rounds[0][0].Winner)
	assert.Equal(t, MatchStatusCompleted, updated.Rounds[0][0].Status)
	// M2 untouched
	assert.Equal(t, "", updated.Rounds[0][1].Winner)
}

func TestUpdateBracket_MutateError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	initial := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "Alice", SideB: "Bob"}},
		},
	}
	require.NoError(t, store.SaveBracket(compID, initial))

	sentinel := errors.New("not found")
	err = store.UpdateBracket(compID, func(b *Bracket) error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	// Bracket on disk must be unchanged
	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "", loaded.Rounds[0][0].Winner)
}

func TestUpdateBracket_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	err = store.UpdateBracket("../traversal", func(b *Bracket) error { return nil })
	assert.Error(t, err)
}

func TestUpdateBracket_MissingFile_EmptyBracket(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "fresh-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Fresh"}))

	// No bracket.json exists yet; mutate sees empty rounds, not nil
	called := false
	err = store.UpdateBracket(compID, func(b *Bracket) error {
		called = true
		require.NotNil(t, b)
		assert.Empty(t, b.Rounds)
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)
}

// TestUpdateBracketMatchByID covers the bracket-match analogue of
// UpdatePoolMatchByID (mp-gmcg review): find one match by id across rounds AND
// the bronze sibling, mutate, save; found=false and NO write on a miss.
func TestUpdateBracketMatchByID(t *testing.T) {
	newStore := func(t *testing.T) (*Store, string) {
		t.Helper()
		dir, err := os.MkdirTemp("", "state-bracket-updbyid-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(dir) })
		store, err := NewStore(dir)
		require.NoError(t, err)
		compID := "test-comp"
		require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))
		require.NoError(t, store.SaveBracket(compID, &Bracket{
			Rounds: [][]BracketMatch{{
				{ID: "M1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
				{ID: "M2", SideA: "Charlie", SideB: "Dave", Status: MatchStatusScheduled},
			}},
			ThirdPlaceMatch: &BracketMatch{ID: "BRONZE", SideA: "Eve", SideB: "Frank", Status: MatchStatusScheduled},
		}))
		return store, compID
	}

	t.Run("mutates a round match, leaves siblings untouched", func(t *testing.T) {
		store, compID := newStore(t)
		found, err := store.UpdateBracketMatchByID(compID, "M1", func(bm *BracketMatch) {
			bm.Winner = "Alice"
			bm.Status = MatchStatusCompleted
		})
		require.NoError(t, err)
		assert.True(t, found)

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
		assert.Equal(t, MatchStatusCompleted, b.Rounds[0][0].Status)
		assert.Equal(t, MatchStatusScheduled, b.Rounds[0][1].Status, "sibling round match untouched")
		assert.Equal(t, MatchStatusScheduled, b.ThirdPlaceMatch.Status, "bronze untouched")
	})

	t.Run("finds the BRONZE match, which a rounds-only walk would miss", func(t *testing.T) {
		store, compID := newStore(t)
		found, err := store.UpdateBracketMatchByID(compID, "BRONZE", func(bm *BracketMatch) {
			bm.Winner = "Eve"
		})
		require.NoError(t, err)
		assert.True(t, found)

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Eve", b.ThirdPlaceMatch.Winner)
	})

	t.Run("not found: mutate never called, no write", func(t *testing.T) {
		store, compID := newStore(t)
		called := false
		found, err := store.UpdateBracketMatchByID(compID, "nope", func(bm *BracketMatch) { called = true })
		require.NoError(t, err)
		assert.False(t, found)
		assert.False(t, called)
	})

	t.Run("invalid compID errors", func(t *testing.T) {
		store, _ := newStore(t)
		_, err := store.UpdateBracketMatchByID("../traversal", "M1", func(bm *BracketMatch) {})
		assert.Error(t, err)
	})

	t.Run("tx twin mutates and persists", func(t *testing.T) {
		store, compID := newStore(t)
		var found bool
		require.NoError(t, store.WithTransaction(compID, func(tx StoreTx) error {
			var err error
			found, err = tx.UpdateBracketMatchByID(compID, "M2", func(bm *BracketMatch) {
				bm.Winner = "Dave"
			})
			return err
		}))
		assert.True(t, found)
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Dave", b.Rounds[0][1].Winner)
	})
}

// TestMatchStatusByID covers the no-copy status reader (mp-gmcg review E5):
// pool first, then bracket rounds, then the bronze sibling; found=false
// otherwise; and its status must AGREE with the full-copy LoadPoolMatches /
// LoadBracket path it replaces.
func TestMatchStatusByID(t *testing.T) {
	newStore := func(t *testing.T) (*Store, string) {
		t.Helper()
		dir, err := os.MkdirTemp("", "state-match-status-*")
		require.NoError(t, err)
		t.Cleanup(func() { os.RemoveAll(dir) })
		store, err := NewStore(dir)
		require.NoError(t, err)
		compID := "test-comp"
		require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))
		require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
			{ID: "P1-0", Status: MatchStatusCompleted, Winner: "Alice",
				SubResults: []SubMatchResult{{Position: 1, IpponsA: []string{"M"}, Winner: "Alice"}}},
			{ID: "P1-1", Status: MatchStatusRunning},
		}))
		require.NoError(t, store.SaveBracket(compID, &Bracket{
			Rounds:          [][]BracketMatch{{{ID: "B1", Status: MatchStatusScheduled}}},
			ThirdPlaceMatch: &BracketMatch{ID: "BRONZE", Status: MatchStatusRunning},
		}))
		return store, compID
	}

	t.Run("finds a pool match's status", func(t *testing.T) {
		store, compID := newStore(t)
		st, found, err := store.MatchStatusByID(compID, "P1-0")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, MatchStatusCompleted, st)
	})

	t.Run("finds a bracket round match's status", func(t *testing.T) {
		store, compID := newStore(t)
		st, found, err := store.MatchStatusByID(compID, "B1")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, MatchStatusScheduled, st)
	})

	t.Run("finds the BRONZE sibling's status (a rounds-only walk would miss it)", func(t *testing.T) {
		store, compID := newStore(t)
		st, found, err := store.MatchStatusByID(compID, "BRONZE")
		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, MatchStatusRunning, st)
	})

	t.Run("found=false for an unknown match", func(t *testing.T) {
		store, compID := newStore(t)
		_, found, err := store.MatchStatusByID(compID, "nope")
		require.NoError(t, err)
		assert.False(t, found)
	})

	t.Run("invalid compID errors", func(t *testing.T) {
		store, _ := newStore(t)
		_, _, err := store.MatchStatusByID("../traversal", "P1-0")
		assert.Error(t, err)
	})

	t.Run("agrees with the full-copy load path for every match", func(t *testing.T) {
		store, compID := newStore(t)
		// Build the authoritative status map from the deep-copy loaders.
		want := map[string]MatchStatus{}
		pm, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range pm {
			want[m.ID] = m.Status
		}
		br, err := store.LoadBracket(compID)
		require.NoError(t, err)
		for _, round := range br.Rounds {
			for _, bm := range round {
				want[bm.ID] = bm.Status
			}
		}
		if br.ThirdPlaceMatch != nil {
			want[br.ThirdPlaceMatch.ID] = br.ThirdPlaceMatch.Status
		}
		for id, wantStatus := range want {
			got, found, err := store.MatchStatusByID(compID, id)
			require.NoError(t, err)
			require.Truef(t, found, "MatchStatusByID must find %q", id)
			assert.Equalf(t, wantStatus, got, "status mismatch for %q", id)
		}
	})

	t.Run("does not mutate cached data: a status read leaves later loads intact", func(t *testing.T) {
		store, compID := newStore(t)
		_, _, err := store.MatchStatusByID(compID, "P1-0")
		require.NoError(t, err)
		// The SubResults must still be intact on a subsequent full load (the
		// no-copy read must never have aliased or truncated the cached slice).
		pm, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, pm, 2)
		require.Len(t, pm[0].SubResults, 1, "cached SubResults untouched by the status read")
		assert.Equal(t, "Alice", pm[0].SubResults[0].Winner)
	})
}

func TestLoadBracketLocked_ViaTransaction(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-locked-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Tx"}))

	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "A", SideB: "B"}},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	// Exercise loadBracketLocked via WithTransaction
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		b, err := tx.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, b)
		assert.Len(t, b.Rounds, 1)
		return nil
	})
	require.NoError(t, txErr)
}

func TestLoadBracket_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	_, err = store.LoadBracket("../bad")
	assert.Error(t, err)
}

func TestSaveBracket_InvalidCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	err = store.SaveBracket("../bad", &Bracket{})
	assert.Error(t, err)
}

func TestParseBracketFile_MissingFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	result, err := parseBracketFile(filepath.Join(dir, "nonexistent.json"))
	require.NoError(t, err)
	b := result.(*Bracket)
	require.NotNil(t, b)
	assert.Empty(t, b.Rounds)
}

func TestCopyBracket_Nil(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-bracket-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	result := store.copyBracket(nil)
	assert.Nil(t, result)
}

// TestLoadBracket_DeepCopyIsolation guards copyBracket's deep-copy of the
// reference-type fields on BracketMatch (Encho pointer, SubResults slice and
// each SubMatchResult's IpponsA/IpponsB/Encho). A shallow copy would alias the
// cached backing array/pointers, so a caller mutating a returned match in place
// could silently corrupt cached state without going through Save/UpdateBracket.
// Mirrors the pool-match copy path (copyMatchResults / cloneSubResults).
func TestLoadBracket_DeepCopyIsolation(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	compID := "test-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Test"}))

	initial := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{
					ID:    "M1",
					SideA: "Team A",
					SideB: "Team B",
					Encho: &EnchoMetadata{PeriodCount: 1},
					SubResults: []SubMatchResult{
						{
							SideA:           "A1",
							SideB:           "B1",
							IpponsA:         []string{"M"},
							IpponsB:         []string{"K"},
							Encho:           &EnchoMetadata{PeriodCount: 2},
							DecidedByHantei: HanteiPtr(true),
						},
					},
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, initial))

	// First load, then mutate every reference field on the returned copy in
	// place; without calling Save/UpdateBracket.
	first, err := store.LoadBracket(compID)
	require.NoError(t, err)
	first.Rounds[0][0].Encho.PeriodCount = 99
	// Scalar field on an existing element: only stays isolated if the
	// SubResults backing array itself was copied (a shallow slice copy would
	// share the element and leak this write). append() can't prove this; it
	// reallocates when cap==len, so an aliased slice's length wouldn't change.
	first.Rounds[0][0].SubResults[0].SideA = "MUTATED"
	first.Rounds[0][0].SubResults[0].IpponsA[0] = "MUTATED"
	first.Rounds[0][0].SubResults[0].IpponsB[0] = "MUTATED"
	first.Rounds[0][0].SubResults[0].Encho.PeriodCount = 99
	// The *bool hantei flag is the newest reference field on the sub, and it
	// needs its own mutation: a shallow struct copy would carry the SAME
	// pointer, so writing through it corrupts the cached bracket while every
	// other assertion here still passes.
	*first.Rounds[0][0].SubResults[0].DecidedByHantei = false

	// A fresh load must reflect the saved state, not the in-place mutation.
	second, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, second.Rounds[0][0].SubResults, 1)
	assert.Equal(t, 1, second.Rounds[0][0].Encho.PeriodCount)
	assert.Equal(t, "A1", second.Rounds[0][0].SubResults[0].SideA)
	assert.Equal(t, []string{"M"}, second.Rounds[0][0].SubResults[0].IpponsA)
	assert.Equal(t, []string{"K"}, second.Rounds[0][0].SubResults[0].IpponsB)
	assert.Equal(t, 2, second.Rounds[0][0].SubResults[0].Encho.PeriodCount)
	assert.True(t, second.Rounds[0][0].SubResults[0].HanteiDecided())
}

// TestParseBracketFile_MalformedJSON covers the parseBracketBytes error
// branch inside parseBracketFile.
func TestParseBracketFile_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bracket.json")
	require.NoError(t, os.WriteFile(path, []byte(`{not valid json`), 0o600))
	_, err := parseBracketFile(path)
	assert.Error(t, err)
}

// TestLoadBracketLocked_InvalidCompID covers the ValidateCompetitionID
// error branch in loadBracketLocked (called without holding a lock in
// unit-test context; safe because no concurrent writer exists).
func TestLoadBracketLocked_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.loadBracketLocked("")
	assert.Error(t, err)
}

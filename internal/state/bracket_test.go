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
							SideA:   "A1",
							SideB:   "B1",
							IpponsA: []string{"M"},
							IpponsB: []string{"K"},
							Encho:   &EnchoMetadata{PeriodCount: 2},
						},
					},
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, initial))

	// First load, then mutate every reference field on the returned copy in
	// place — without calling Save/UpdateBracket.
	first, err := store.LoadBracket(compID)
	require.NoError(t, err)
	first.Rounds[0][0].Encho.PeriodCount = 99
	first.Rounds[0][0].SubResults[0].IpponsA[0] = "MUTATED"
	first.Rounds[0][0].SubResults[0].IpponsB[0] = "MUTATED"
	first.Rounds[0][0].SubResults[0].Encho.PeriodCount = 99
	first.Rounds[0][0].SubResults = append(first.Rounds[0][0].SubResults, SubMatchResult{SideA: "leak"})

	// A fresh load must reflect the saved state, not the in-place mutation.
	second, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, second.Rounds[0][0].SubResults, 1, "appended sub-result must not leak into cache")
	assert.Equal(t, 1, second.Rounds[0][0].Encho.PeriodCount)
	assert.Equal(t, []string{"M"}, second.Rounds[0][0].SubResults[0].IpponsA)
	assert.Equal(t, []string{"K"}, second.Rounds[0][0].SubResults[0].IpponsB)
	assert.Equal(t, 2, second.Rounds[0][0].SubResults[0].Encho.PeriodCount)
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
// unit-test context — safe because no concurrent writer exists).
func TestLoadBracketLocked_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.loadBracketLocked("")
	assert.Error(t, err)
}

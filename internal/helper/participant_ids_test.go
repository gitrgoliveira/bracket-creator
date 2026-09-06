package helper

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMissingParticipantIDsMessage(t *testing.T) {
	t.Run("empty when every row already has an id", func(t *testing.T) {
		players := []Player{
			{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
			{ID: "00000000-0000-4000-8000-000000000001", Name: "Bob", Dojo: "Dojo B"},
		}
		assert.Empty(t, MissingParticipantIDsMessage(players))
	})

	t.Run("names a single missing row and states the remedy", func(t *testing.T) {
		players := []Player{
			{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
			{Name: "Bob", Dojo: "Dojo B"},
		}
		msg := MissingParticipantIDsMessage(players)
		assert.Contains(t, msg, "Bob (Dojo B)")
		assert.NotContains(t, msg, "Alice", "a row that already has an id is not named")
		assert.Contains(t, msg, "Save the roster once and the ids are assigned.")
	})

	t.Run("names every row up to three, without a count prefix", func(t *testing.T) {
		players := []Player{
			{Name: "Alice", Dojo: "Dojo A"},
			{Name: "Bob", Dojo: "Dojo B"},
			{Name: "Carol", Dojo: "Dojo C"},
		}
		msg := MissingParticipantIDsMessage(players)
		assert.Contains(t, msg, "Alice (Dojo A)")
		assert.Contains(t, msg, "Bob (Dojo B)")
		assert.Contains(t, msg, "Carol (Dojo C)")
		assert.NotContains(t, msg, "competitors, including", "at or under the threshold, no count prefix is used")
	})

	t.Run("names only the first three and states the total count for a large roster", func(t *testing.T) {
		players := []Player{
			{Name: "Alice", Dojo: "Dojo A"},
			{Name: "Bob", Dojo: "Dojo B"},
			{Name: "Carol", Dojo: "Dojo C"},
			{Name: "Dave", Dojo: "Dojo D"},
			{Name: "Eve", Dojo: "Dojo E"},
		}
		msg := MissingParticipantIDsMessage(players)
		assert.Contains(t, msg, "5 competitors, including Alice (Dojo A), Bob (Dojo B), Carol (Dojo C)")
		assert.NotContains(t, msg, "Dave", "only the first few are named for a large roster")
		assert.NotContains(t, msg, "Eve")
	})

	t.Run("a dojo-less name is not parenthesized", func(t *testing.T) {
		players := []Player{{Name: "Alice"}}
		msg := MissingParticipantIDsMessage(players)
		assert.Contains(t, msg, "Alice: no id on file")
		assert.NotContains(t, msg, "(")
	})
}

func TestValidateNoMissingParticipantIDs(t *testing.T) {
	t.Run("nil when every row has an id", func(t *testing.T) {
		players := []Player{{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"}}
		assert.NoError(t, ValidateNoMissingParticipantIDs(players))
	})

	t.Run("wraps ErrMissingParticipantIDsInDraw and names the offending row", func(t *testing.T) {
		players := []Player{{Name: "Alice", Dojo: "Dojo A"}}
		err := ValidateNoMissingParticipantIDs(players)
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrMissingParticipantIDsInDraw))
		assert.Contains(t, err.Error(), "Alice (Dojo A)")
	})
}

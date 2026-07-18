package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignPlayerNumbers(t *testing.T) {
	t.Run("basic numbering with prefix", func(t *testing.T) {
		players := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
			{Name: "Carol"},
		}

		next := AssignPlayerNumbers(players, "A", 1)

		require.Equal(t, 4, next)
		assert.Equal(t, "A1", players[0].Number)
		assert.Equal(t, "A2", players[1].Number)
		assert.Equal(t, "A3", players[2].Number)
	})

	t.Run("empty prefix produces bare numbers", func(t *testing.T) {
		players := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
		}

		next := AssignPlayerNumbers(players, "", 1)

		require.Equal(t, 3, next)
		assert.Equal(t, "1", players[0].Number)
		assert.Equal(t, "2", players[1].Number)
	})

	t.Run("empty slice returns start unchanged and mutates nothing", func(t *testing.T) {
		var players []Player

		next := AssignPlayerNumbers(players, "A", 5)

		assert.Equal(t, 5, next)
		assert.Empty(t, players)
	})

	t.Run("chaining continues sequence across slices without gaps or duplicates", func(t *testing.T) {
		pool1 := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
			{Name: "Carol"},
		}
		pool2 := []Player{
			{Name: "Dave"},
			{Name: "Erin"},
		}

		next := AssignPlayerNumbers(pool1, "A", 1)
		require.Equal(t, 4, next)

		next = AssignPlayerNumbers(pool2, "A", next)
		require.Equal(t, 6, next)

		assert.Equal(t, "A1", pool1[0].Number)
		assert.Equal(t, "A2", pool1[1].Number)
		assert.Equal(t, "A3", pool1[2].Number)
		assert.Equal(t, "A4", pool2[0].Number)
		assert.Equal(t, "A5", pool2[1].Number)
	})

	t.Run("non-1 start value", func(t *testing.T) {
		players := []Player{
			{Name: "Alice"},
			{Name: "Bob"},
		}

		next := AssignPlayerNumbers(players, "K", 10)

		require.Equal(t, 12, next)
		assert.Equal(t, "K10", players[0].Number)
		assert.Equal(t, "K11", players[1].Number)
	})
}

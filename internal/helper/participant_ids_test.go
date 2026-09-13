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

func TestPoolsMissingParticipantIDsMessage(t *testing.T) {
	t.Run("empty when every pool member already has an id", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{
				{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
				{ID: "00000000-0000-4000-8000-000000000001", Name: "Bob", Dojo: "Dojo B"},
			}},
		}
		assert.Empty(t, PoolsMissingParticipantIDsMessage(pools))
	})

	t.Run("empty for no pools at all", func(t *testing.T) {
		assert.Empty(t, PoolsMissingParticipantIDsMessage(nil))
		assert.Empty(t, PoolsMissingParticipantIDsMessage([]Pool{}))
	})

	// A nameless row is an empty slot, not a competitor missing an id. This
	// pins the narrowing that made the notice agree with the load-time
	// repair's own scan (poolMemberMissingID, internal/state/legacy_upgrade.go):
	// the repair CANNOT attempt such a row, because a pools row's only
	// resolution key is its name, so naming it told the operator the app had
	// tried and failed when it never looked. It also read badly -- playerLabel
	// renders a nameless row as "" or " (Dojo)", so the notice emitted a
	// sentence beginning with a bare colon. Not reachable through any app
	// write path (every draw fills the name); a hand-edited pools.csv line is
	// the only producer, which is exactly why it needs a test rather than an
	// argument.
	t.Run("a nameless row is an empty slot, not a competitor to name", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{
				{Name: "", Dojo: "Dojo A"},
				{Name: "Bob", Dojo: "Dojo B"},
			}},
		}
		msg := PoolsMissingParticipantIDsMessage(pools)
		assert.Contains(t, msg, "Bob (Dojo B)", "the named id-less member is still reported")
		assert.NotContains(t, msg, "(Dojo A)",
			"the nameless row must not be named: playerLabel renders it as a bare dojo")
		assert.NotContains(t, msg, "2 competitors", "the nameless row must not inflate the count")
	})

	t.Run("a pool of nothing but nameless rows reports nothing at all", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{{Name: "", Dojo: "Dojo A"}, {Name: ""}}},
		}
		assert.Empty(t, PoolsMissingParticipantIDsMessage(pools),
			"empty slots are not competitors, so there is nothing to tell the operator")
	})

	t.Run("names a single missing member across two pools and states the remedy", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{
				{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
			}},
			{PoolName: "Pool B", Players: []Player{
				{Name: "Bob", Dojo: "Dojo B"},
			}},
		}
		msg := PoolsMissingParticipantIDsMessage(pools)
		assert.Contains(t, msg, "Bob (Dojo B)")
		assert.NotContains(t, msg, "Alice", "a member that already has an id is not named")
		assert.Contains(t, msg, "no id in the pool draw")
		assert.Contains(t, msg, "could not be matched to a participant automatically")
	})

	t.Run("names only the first three and states the total count across pools", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{
				{Name: "Alice", Dojo: "Dojo A"},
				{Name: "Bob", Dojo: "Dojo B"},
			}},
			{PoolName: "Pool B", Players: []Player{
				{Name: "Carol", Dojo: "Dojo C"},
				{Name: "Dave", Dojo: "Dojo D"},
				{Name: "Eve", Dojo: "Dojo E"},
			}},
		}
		msg := PoolsMissingParticipantIDsMessage(pools)
		assert.Contains(t, msg, "5 competitors, including Alice (Dojo A), Bob (Dojo B), Carol (Dojo C)")
		assert.NotContains(t, msg, "Dave", "only the first few are named for a large draw")
		assert.NotContains(t, msg, "Eve")
	})

	t.Run("a member with an id in one pool and none in another only names the id-less one", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: []Player{
				{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
			}},
			{PoolName: "Pool B", Players: []Player{
				{Name: "Alice", Dojo: "Dojo A"},
			}},
		}
		msg := PoolsMissingParticipantIDsMessage(pools)
		assert.Contains(t, msg, "Alice (Dojo A): no id in the pool draw")
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

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateAssignments(t *testing.T) {
	assignments := []SeedAssignment{
		{Name: "Jane Doe", SeedRank: 1},
		{Name: "John Smith", SeedRank: 2},
	}
	err := ValidateAssignments(assignments)
	assert.NoError(t, err)

	assignments = []SeedAssignment{
		{Name: "Jane Doe", SeedRank: 1},
		{Name: "John Smith", SeedRank: 1},
	}
	err = ValidateAssignments(assignments)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate seed rank detected")
}

func TestAssignSeeds(t *testing.T) {
	players := []Player{
		{Name: "Jane Doe"},
		{Name: "John Smith"},
		{Name: "Alice"},
	}

	assignments := []SeedAssignment{
		{Name: "Jane Doe", SeedRank: 2},
		{Name: "John Smith", SeedRank: 1},
	}

	err := AssignSeeds(players, assignments)
	assert.NoError(t, err)

	assert.Equal(t, 2, players[0].Seed) // Jane Doe
	assert.Equal(t, 1, players[1].Seed) // John Smith
	assert.Equal(t, 0, players[2].Seed) // Alice
}

func TestAssignSeeds_MissingParticipant(t *testing.T) {
	players := []Player{
		{Name: "Jane Doe"},
	}

	assignments := []SeedAssignment{
		{Name: "Bob", SeedRank: 1},
	}

	err := AssignSeeds(players, assignments)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seeded participant not found")
}

func TestAssignSeeds_CollisionSwap(t *testing.T) {
	players := []Player{
		{Name: "Jane Doe", Seed: 1},
		{Name: "John Smith", Seed: 2},
	}

	// We assign rank 1 to John
	assignments := []SeedAssignment{
		{Name: "John Smith", SeedRank: 1},
	}

	err := AssignSeeds(players, assignments)
	assert.NoError(t, err)

	// Since John took 1, Jane should be swapped to 2
	assert.Equal(t, 2, players[0].Seed) // Jane Doe
	assert.Equal(t, 1, players[1].Seed) // John Smith
}

func TestMatches_CaseSensitive(t *testing.T) {
	p := Player{Name: "Jane Doe"}

	assert.True(t, p.Matches("Jane Doe"))
	assert.False(t, p.Matches("jane doe"))
	assert.False(t, p.Matches("Jane doe"))
}

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateAssignments(t *testing.T) {
	tests := []struct {
		name        string
		assignments []SeedAssignment
		wantErr     bool
		errContains string
	}{
		{
			name: "valid assignments",
			assignments: []SeedAssignment{
				{Name: "Jane Doe", SeedRank: 1},
				{Name: "John Smith", SeedRank: 2},
			},
			wantErr: false,
		}, {
			name: "valid assignments",
			assignments: []SeedAssignment{
				{Name: "Jane Doe", SeedRank: 2},
				{Name: "John Smith", SeedRank: 1},
			},
			wantErr: false,
		},
		{
			name: "duplicate seed rank",
			assignments: []SeedAssignment{
				{Name: "Jane Doe", SeedRank: 1},
				{Name: "John Smith", SeedRank: 1},
			},
			wantErr:     true,
			errContains: "duplicate seed rank detected",
		},
		{
			name: "gap in sequence",
			assignments: []SeedAssignment{
				{Name: "Jane Doe", SeedRank: 1},
				{Name: "John Smith", SeedRank: 2},
				{Name: "Alice", SeedRank: 4},
			},
			wantErr:     true,
			errContains: "seed ranks must be sequential without gaps",
		},
		{
			name: "invalid seed rank",
			assignments: []SeedAssignment{
				{Name: "Jane Doe", SeedRank: 0},
			},
			wantErr:     true,
			errContains: "seed rank must be greater than 0",
		},
		{
			name: "empty name",
			assignments: []SeedAssignment{
				{Name: "", SeedRank: 1},
			},
			wantErr:     true,
			errContains: "name cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAssignments(tt.assignments)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				assert.NoError(t, err)
			}
		})
	}
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

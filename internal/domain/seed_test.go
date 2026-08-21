package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestAssignSeeds_SameNameDifferentDojo(t *testing.T) {
	players := []Player{
		{Name: "John Doe", Dojo: "DojoA"},
		{Name: "John Doe", Dojo: "DojoB"},
	}

	assignments := []SeedAssignment{
		{Name: "John Doe", Dojo: "DojoA", SeedRank: 1},
		{Name: "John Doe", Dojo: "DojoB", SeedRank: 2},
	}

	err := AssignSeeds(players, assignments)
	assert.NoError(t, err)
	assert.Equal(t, 1, players[0].Seed)
	assert.Equal(t, 2, players[1].Seed)
}

func TestAssignSeeds_BackwardCompatEmptyDojo(t *testing.T) {
	players := []Player{
		{Name: "Alice", Dojo: "SomeDojo"},
		{Name: "Bob", Dojo: "OtherDojo"},
	}

	assignments := []SeedAssignment{
		{Name: "Alice", SeedRank: 1},
	}

	err := AssignSeeds(players, assignments)
	assert.NoError(t, err)
	assert.Equal(t, 1, players[0].Seed)
}

func TestAssignSeeds_AmbiguousNameNoDojo(t *testing.T) {
	players := []Player{
		{Name: "John Doe", Dojo: "DojoA"},
		{Name: "John Doe", Dojo: "DojoB"},
	}

	assignments := []SeedAssignment{
		{Name: "John Doe", SeedRank: 1},
	}

	err := AssignSeeds(players, assignments)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestRosterIndex_Lookup pins the ONE shared fallback rule (see SeedKey's and
// RosterIndex's doc comments) directly against the primitive itself, rather
// than only indirectly through AssignSeeds/ApplySeeds/the state-package
// matchers that now all call it: exact (name, dojo) match; a legacy row
// with no dojo falls back to a bare-name match ONLY when that name is
// unique in the roster; anything else resolves to false rather than
// guessing.
func TestRosterIndex_Lookup(t *testing.T) {
	t.Run("exact name and dojo match", func(t *testing.T) {
		players := []Player{
			{Name: "Yuki Tanaka", Dojo: "Seibukan"},
			{Name: "Yuki Tanaka", Dojo: "Tobukan"},
		}
		idx := NewRosterIndex(players)

		p, ok := idx.Lookup("Yuki Tanaka", "Tobukan")
		require.True(t, ok)
		assert.Equal(t, "Tobukan", p.Dojo)

		p, ok = idx.Lookup("Yuki Tanaka", "Seibukan")
		require.True(t, ok)
		assert.Equal(t, "Seibukan", p.Dojo)
	})

	t.Run("empty-dojo row falls back to bare name when unique", func(t *testing.T) {
		players := []Player{
			{Name: "Rin Sato", Dojo: "Seibukan"},
			{Name: "Other Player", Dojo: "Tobukan"},
		}
		idx := NewRosterIndex(players)

		p, ok := idx.Lookup("Rin Sato", "")
		require.True(t, ok, "a legacy row naming a unique roster player must resolve")
		assert.Equal(t, "Seibukan", p.Dojo)
	})

	t.Run("empty-dojo row stays unresolved when the name is ambiguous", func(t *testing.T) {
		players := []Player{
			{Name: "Yuki Tanaka", Dojo: "Seibukan"},
			{Name: "Yuki Tanaka", Dojo: "Tobukan"},
		}
		idx := NewRosterIndex(players)

		_, ok := idx.Lookup("Yuki Tanaka", "")
		assert.False(t, ok, "an ambiguous bare name must never be guessed")
	})

	t.Run("non-empty dojo mismatch never falls back to bare name", func(t *testing.T) {
		// The fallback is documented as applying ONLY when the row's dojo is
		// empty; a row that names a real (but wrong) dojo must not silently
		// match some other player sharing the name.
		players := []Player{
			{Name: "Rin Sato", Dojo: "Seibukan"},
		}
		idx := NewRosterIndex(players)

		_, ok := idx.Lookup("Rin Sato", "Some Other Dojo")
		assert.False(t, ok)
	})

	t.Run("unknown name never matches", func(t *testing.T) {
		players := []Player{
			{Name: "Rin Sato", Dojo: "Seibukan"},
		}
		idx := NewRosterIndex(players)

		_, ok := idx.Lookup("Nobody Here", "")
		assert.False(t, ok)
	})
}

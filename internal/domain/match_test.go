package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestMatch(t *testing.T) {
	// Create test players
	player1 := domain.Player{
		ID:           "player1",
		Name:         "John Doe",
		DisplayName:  "J. Doe",
		Dojo:         "Test Dojo",
		PoolPosition: 1,
	}

	player2 := domain.Player{
		ID:           "player2",
		Name:         "Jane Smith",
		DisplayName:  "J. Smith",
		Dojo:         "Another Dojo",
		PoolPosition: 2,
	}

	// Create a test match with no winner
	match := domain.Match{
		ID:    "match1",
		SideA: &player1,
		SideB: &player2,
	}

	// Verify match properties
	assert.Equal(t, "match1", match.ID)
	assert.Equal(t, "player1", match.SideA.ID)
	assert.Equal(t, "player2", match.SideB.ID)
	assert.Nil(t, match.Winner)

	// Set a winner
	match.Winner = &player1

	assert.Equal(t, "player1", match.Winner.ID)
}

// TestEnchoMetadataPersists verifies FR-032: encho (overtime / sudden-death)
// metadata round-trips through YAML on a domain.MatchResult.
//
// This is a Red test — domain.MatchResult and domain.EnchoMetadata do
// not yet exist. The build must fail until the Green implementation
// (T034 family) lands.
func TestEnchoMetadataPersists(t *testing.T) {
	r := domain.MatchResult{
		Encho: &domain.EnchoMetadata{PeriodCount: 1},
	}

	data, err := yaml.Marshal(r)
	require.NoError(t, err)

	var got domain.MatchResult
	require.NoError(t, yaml.Unmarshal(data, &got))

	require.NotNil(t, got.Encho, "Encho metadata must survive YAML round-trip")
	assert.Equal(t, 1, got.Encho.PeriodCount)
}

package helper_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
)

func TestPlayerToDomainAndBack(t *testing.T) {
	hp := helper.Player{
		ID:           "uuid-1",
		Name:         "Alice",
		DisplayName:  "A. Smith",
		Dojo:         "Dojo A",
		Metadata:     []string{"5-dan", "captain"},
		Tag:          "registered",
		PoolPosition: 7,
		Seed:         2,
		Number:       "K1",
	}
	dp := helper.PlayerToDomain(hp)
	assert.Equal(t, hp.ID, dp.ID)
	assert.Equal(t, hp.Name, dp.Name)
	assert.Equal(t, hp.DisplayName, dp.DisplayName)
	assert.Equal(t, hp.Dojo, dp.Dojo)
	assert.Equal(t, hp.Metadata, dp.Metadata)
	assert.Equal(t, hp.Tag, dp.Tag)
	assert.Equal(t, hp.PoolPosition, dp.PoolPosition)
	assert.Equal(t, hp.Seed, dp.Seed)
	assert.Equal(t, hp.Number, dp.Number)

	// Round trip back to helper.Player.
	hp2 := helper.PlayerFromDomain(dp)
	assert.Equal(t, hp, hp2)
}

func TestPlayerToDomainMetadataAliasing(t *testing.T) {
	// Mutating the source slice after conversion must not affect the
	// converted value (shallow copy semantics).
	src := helper.Player{Metadata: []string{"a", "b"}}
	dp := helper.PlayerToDomain(src)
	src.Metadata[0] = "MUTATED"
	assert.Equal(t, "a", dp.Metadata[0])
}

func TestPlayerToDomainEmptyMetadata(t *testing.T) {
	hp := helper.Player{Name: "Solo"}
	dp := helper.PlayerToDomain(hp)
	assert.Nil(t, dp.Metadata, "empty Metadata should remain nil (no alloc)")
}

func TestPlayersToDomainNilAndEmpty(t *testing.T) {
	assert.Nil(t, helper.PlayersToDomain(nil))
	assert.Nil(t, helper.PlayersFromDomain(nil))

	out := helper.PlayersToDomain([]helper.Player{})
	assert.NotNil(t, out)
	assert.Len(t, out, 0)
}

func TestPlayersToDomainPreservesOrder(t *testing.T) {
	hps := []helper.Player{
		{Name: "First"},
		{Name: "Second"},
		{Name: "Third"},
	}
	dps := helper.PlayersToDomain(hps)
	assert.Equal(t, []domain.Player{
		{Name: "First"},
		{Name: "Second"},
		{Name: "Third"},
	}, dps)
}

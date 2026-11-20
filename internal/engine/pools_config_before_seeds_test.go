package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGeneratePools_SettingsRefusalComesBeforeSeedError pins the order of the
// draw's refusals: a stale seed row is a plain error, so it must not hide the
// 400 that names the setting to fix.
func TestGeneratePools_SettingsRefusalComesBeforeSeedError(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID:       "seed-after-settings",
		Kind:     "individual",
		Format:   state.CompFormatMixed,
		PoolSize: 0,
		Courts:   []string{"A"},
	}
	players := []domain.Player{
		{ID: "p1", Name: "Alice", Dojo: "North"},
		{ID: "p2", Name: "Bob", Dojo: "South"},
	}
	staleSeed := []domain.SeedAssignment{{Name: "Nobody Here", Dojo: "Nowhere", SeedRank: 1}}

	_, err := eng.generatePools(comp, players, staleSeed, nil)

	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "the pool size refusal must win over the seed error")
	assert.Contains(t, err.Error(), "pool size must be at least 1")
}

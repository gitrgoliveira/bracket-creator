package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-qual review round: generatePools' fill-bracket branch dispatched on
// comp.ExtraQualifiers alone, while its own comment cited a
// state.ValidateExtraQualifiers call as the gate that kept it to
// minimum-players-per-pool sizing. That call ran AFTER pool formation, and only
// inside the `mixed` arm -- so it protected neither the branch that named it nor
// the other format generatePools serves.
//
// These two tests pin the two halves of the fix. Both are ORDERING tests: the
// old code reached the same eventual verdict in the first case (via a different,
// misleading message) and the same eventual pool count in the second (only by a
// coincidence of the caller's), so each fixture is chosen specifically so the
// two orders produce observably different results.

// A competition configured with fill-bracket AND maximum pool sizing is invalid
// by rule (state.ValidateExtraQualifiers), and the operator must be told THAT.
// Running formation first hands them FillBracketPoolCount's complaint about
// entrant counts instead -- a true statement about arithmetic they never asked
// for, pointing at the wrong setting.
//
// The fixture makes the two messages distinguishable: 7 entrants at pool size 5
// has no fill-bracket formation (the search range is empty, ceil(7/6)=2 > 1 =
// floor(7/5)), so the pre-fix order fails inside FillBracketPoolCount and never
// reaches the sizing rule at all.
//
// Fault injection (verified): moving the ValidateExtraQualifiers call back below
// the formation dispatch turns this red with
// "no pool count fits 7 entrants at minimum pool size 5".
func TestStartCompetition_FillBracket_MaxSizing_ReportsTheSettingNotTheFormation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-max-sizing"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 5, func(c *state.Competition) {
		c.PoolSizeMode = "max"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, compID, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank", "Grace",
	})

	err := eng.StartCompetition(compID)
	require.Error(t, err)
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "must surface as a ValidationError (-> HTTP 400)")
	assert.Contains(t, ve.Error(), "requires minimum-players-per-pool sizing",
		"the operator must be told which SETTING is wrong")
	assert.NotContains(t, ve.Error(), "no pool count fits",
		"formation ran before the setting was validated: the operator is handed an entrant-count complaint for a sizing-mode mistake")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status, "a rejected draw must not transition the competition")
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Empty(t, pools, "nothing may be persisted before the setting is validated")
}

// generatePools serves BOTH mixed and league. ExtraQualifiers means nothing for
// a league (no knockout for a pool to feed), and the HTTP layer zeroes it for
// that format -- but a hand-edited config.md, or a format changed below the HTTP
// layer, can still present one. Pool FORMATION must ignore it.
//
// Called directly rather than through StartCompetition, deliberately: the
// pre-fix code survived only because runDrawPipeline happens to pin a league's
// PoolSize to len(players) just before calling this, which makes
// FillBracketPoolCount return exactly one pool whatever it is handed. That is
// the caller's coincidence, not this function's guard, and a test routed
// through the caller could not tell the two apart. 18 entrants at pool size 3
// is chosen because the two formation objectives genuinely disagree there:
// standard min-mode cuts floor(18/3) = 6 pools, fill-bracket cuts 5 (6 needs 2
// drafted 2nds and has 0 oversized pools to take them from).
//
// Fault injection (verified): dropping `poolFedKnockout &&` from the formation
// dispatch turns this red with 5 pools.
func TestGeneratePools_LeagueIgnoresFillBracketFormation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-with-stale-fill-bracket"

	createTestCompetition(t, store, compID, state.CompFormatLeague, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B"}
	})

	players := make([]domain.Player, 18)
	for i := range players {
		players[i] = domain.Player{
			Name: fmt.Sprintf("Player%02d", i),
			Dojo: fmt.Sprintf("Dojo%02d", i),
		}
	}

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NoError(t, eng.generatePools(comp, players, nil))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, pools, 6,
		"a league formed its pools through the fill-bracket objective: the setting has no meaning without a pool-fed knockout, and reading PoolSize as that objective's minimum is not what a league's PoolSize means")
}

package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// A competition with no lineups at all has nothing to move, whether the
// caller read an empty file or none: the settlement answers a map it can write
// into, changes nothing and leaves nothing waiting.
func TestSettleRoundLineups_NoLineups(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatKnockout}
	players := []domain.Player{{ID: "t", Name: "Tora"}}

	for name, lineups := range map[string]map[string]domain.TeamLineup{"nil": nil, "empty": {}} {
		t.Run(name, func(t *testing.T) {
			for _, onLoad := range []bool{true, false} {
				res := settleRoundLineups(comp, players, lineups, nil, onLoad)

				assert.NotNil(t, res.lineups)
				assert.Empty(t, res.lineups)
				assert.False(t, res.changed)
				assert.Zero(t, res.waiting)
				assert.Empty(t, res.notes)
			}
		})
	}
}

// settleRoundLineups reads only its arguments: the lineups it was given are
// never modified, the map it answers is its own.
func TestSettleRoundLineups_NeverModifiesWhatItWasGiven(t *testing.T) {
	comp := &Competition{ID: "c", Kind: "team", TeamSize: 3, Format: CompFormatKnockout}
	players := []domain.Player{{ID: "t", Name: "Tora"}}
	round1 := domain.TeamLineup{TeamID: "t", CompetitionID: "c", Round: 1,
		Positions: map[domain.Position]string{"1": "Sato"}, MemberIDs: map[domain.Position]string{"1": "m"}}
	given := map[string]domain.TeamLineup{teamLineupKey("t", 1): round1}
	draw := []DrawMatch{{ID: "r1-m0", SideAID: "t", SideBID: "o", Knockout: true, Round: 1}}

	res := settleRoundLineups(comp, players, given, draw, true)

	assert.True(t, res.changed)
	assert.Len(t, given, 1, "the map it was given still holds its lineup")
	assert.Contains(t, given, teamLineupKey("t", 1))
	moved := res.lineups[teamLineupMatchKey("t", "r1-m0")]
	moved.Positions["1"] = "changed"
	assert.Equal(t, "Sato", round1.Positions["1"], "a lineup it made carries its own copy of the maps")
	assert.Equal(t, "Sato", given[teamLineupKey("t", 1)].Positions["1"])
}

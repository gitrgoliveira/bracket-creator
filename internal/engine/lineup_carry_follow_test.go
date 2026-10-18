package engine

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingTx counts the lineup writes a follow stages, and fails them on
// demand, as a store that refuses the write would.
type countingTx struct {
	state.StoreTx
	single, batch int
	fail          error
}

func (c *countingTx) SetTeamLineup(compID string, l domain.TeamLineup, teamSize int) error {
	c.single++
	if c.fail != nil {
		return c.fail
	}
	return c.StoreTx.SetTeamLineup(compID, l, teamSize)
}

func (c *countingTx) SetTeamLineups(compID string, ls []domain.TeamLineup, teamSize int) error {
	c.batch++
	if c.fail != nil {
		return c.fail
	}
	return c.StoreTx.(interface {
		SetTeamLineups(string, []domain.TeamLineup, int) error
	}).SetTeamLineups(compID, ls, teamSize)
}

// followFixture: team ta started two kachinuki matches at start, so a change
// to its starting lineup stamped before that moves both pins.
func followFixture(t *testing.T) (*Engine, *state.Store, *state.Competition, int64) {
	eng, store, _ := setupTestEngine(t)
	const c = "follow-batch"
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled},
		state.MatchResult{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled})
	startingLineup(t, store, c, "ta", "Aoki", "m-aoki")
	startingLineup(t, store, c, "tb", "Fujii", "m-fujii")
	const start = int64(1_000_000)
	startKachinuki(t, eng, c, "P1-0", start)
	startKachinuki(t, eng, c, "P1-1", start+5)
	comp, err := store.LoadCompetition(c)
	require.NoError(t, err)
	return eng, store, comp, start
}

// A follow writes every lineup it moved in ONE staged write of the lineups
// file, however many moved (stage 6 review 7: one per copy re-parsed and
// rewrote the whole file under the competition's lock).
func TestFollowLineupSave_StagesTheLineupsFileOnce(t *testing.T) {
	eng, store, comp, start := followFixture(t)
	p1 := domain.PositionNumbered(1)
	var ct *countingTx
	var moved []string
	require.NoError(t, store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		saved := carryTestStartingSave(t, tx, comp.ID, "ta", p1, "Kato", "m-Kato", start-10)
		ct = &countingTx{StoreTx: tx}
		var err error
		moved, err = eng.FollowLineupSave(ct, comp, saved, []domain.Position{p1})
		return err
	}))
	assert.ElementsMatch(t, []string{"P1-0", "P1-1"}, moved)
	assert.Zero(t, ct.single, "no lineup is written on its own")
	assert.Equal(t, 1, ct.batch)
	for _, m := range []string{"P1-0", "P1-1"} {
		in, err := eng.LineupInForce(comp.ID, "ta", m)
		require.NoError(t, err)
		assert.Equal(t, "Kato", in.Lineup.Positions[p1], m)
	}
}

// A lineup the follow cannot store fails the save's transaction, as a pin
// that cannot be kept fails the start: nothing is left half carried.
func TestFollowLineupSave_AStoreThatRefusesFailsTheSave(t *testing.T) {
	eng, store, comp, start := followFixture(t)
	p1 := domain.PositionNumbered(1)
	refused := errors.New("disk refuses")
	err := store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		saved := carryTestStartingSave(t, tx, comp.ID, "ta", p1, "Kato", "m-Kato", start-10)
		_, err := eng.FollowLineupSave(&countingTx{StoreTx: tx, fail: refused}, comp, saved, []domain.Position{p1})
		return err
	})
	require.ErrorIs(t, err, refused)
	for _, m := range []string{"P1-0", "P1-1"} {
		in, err := eng.LineupInForce(comp.ID, "ta", m)
		require.NoError(t, err)
		assert.Equal(t, "Aoki", in.Lineup.Positions[p1], "%s: the transaction rolled back whole", m)
	}
	start0, err := store.LoadTeamLineups(comp.ID)
	require.NoError(t, err)
	for _, l := range start0 {
		if l.TeamID == "ta" && l.MatchID == "" {
			assert.Equal(t, "Aoki", l.Positions[p1], "the save itself rolled back with it")
		}
	}
}

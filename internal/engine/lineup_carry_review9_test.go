package engine

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// lineupReadFailing is a store handle whose read of the lineups in force
// fails, every other call reaching the transaction it wraps.
type lineupReadFailing struct{ state.StoreTx }

func (lineupReadFailing) LoadTeamLineups(string) (map[string]domain.TeamLineup, error) {
	return nil, errors.New("lineups.yaml: read refused")
}

// A start that anchors the match's own lineup reads the lineups back for the
// rest of the start (the bout 1 seat and the team's later matches): a read
// that fails fails the start, never leaves it working from the lineups as
// they were before the anchor (stage 6 review 9, G5).
func TestKachinukiOpener_AnAnchorWhoseLineupsCannotBeReadBackFailsTheStart(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	const c = "rv9anchor"
	p1 := domain.PositionNumbered(1)
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled})
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{p1: "Sato"}, Stamps: map[domain.Position]int64{p1: 100}}, 3))
	// The match's own lineup, a copy made at 500: a start at 300 anchors it.
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Doi"}, Stamps: map[domain.Position]int64{p1: 500},
		CopiedAt: 500, Own: []domain.Position{p1}}, 3))
	var anchored bool
	var anchorErr error
	require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(c)
		require.NoError(t, err)
		d, ok := readOpenerDraw(tx, comp, c)
		require.True(t, ok)
		anchored, anchorErr = anchorStartedCopy(lineupReadFailing{tx}, &d, c, "P1-0", "ta", 300)
		return nil
	}))
	require.Error(t, anchorErr, "the read back failed, so the anchor fails (anchored=%v)", anchored)
	require.Contains(t, anchorErr.Error(), "read refused")
}

// A save on a match's own lineup that is a live carried copy is landed whole:
// the positions it carries stay on the stored lineup, so a follow that does
// not work that lineup out again (nothing it carries moved) never leaves it
// stored with its own positions alone. Only a removed record keeps its own
// positions alone (stage 6 review 9, G6).
func TestLineupCarry_ASaveOnALiveCopyIsLandedWithItsCarriedPositions(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	const c = "rv9land"
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled})
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{p1: "Sato", p2: "Ueda"}, Stamps: map[domain.Position]int64{p1: 100, p2: 100}}, 3))
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Doi", p2: "Ueda"}, Stamps: map[domain.Position]int64{p1: 500, p2: 100},
		CopiedAt: 500, Own: []domain.Position{p1}}, 3))
	lineups, err := store.LoadTeamLineupsWithRemoved(c)
	require.NoError(t, err)
	pool, err := store.LoadPoolMatches(c)
	require.NoError(t, err)
	landed, err := LandMatchLineupSave(lineups, pool, nil, false, 3, "ta", "P1-0",
		[]domain.Position{p1}, map[domain.Position]string{p1: "Mori"}, nil, map[domain.Position]int64{p1: 600}, 1_000_000)
	require.NoError(t, err)
	require.True(t, landed.Live)
	require.Equal(t, "Mori", landed.Lineup.Positions[p1], "the change lands")
	require.Equal(t, "Ueda", landed.Lineup.Positions[p2], "the carried position stays on the lineup landed")
	for _, r := range landed.Records {
		if !r.IsRemoved() {
			require.Equal(t, "Ueda", r.Positions[p2], "the live record staged is whole")
		}
	}
}

// A carried lineup the domain refuses as worked out is named to the operator
// as the operator knows the match (OperatorMatchLabel), in plain words, with
// the lead of the change that was made, and never with the domain's own error
// text (stage 6 review 9, F5).
func TestLineupCarry_ARefusedCarriedLineupIsNamedAsTheOperatorKnowsIt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "rv9words"
	p1, p5 := domain.PositionNumbered(1), domain.PositionNumbered(5)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: c, Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{p1: "Sato"}, Stamps: map[domain.Position]int64{p1: 100}}, 3))
	// Kept from older data: a position the team size has no place for.
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", MatchID: "Pool A-1",
		Positions: map[domain.Position]string{p1: "Sato", p5: "Endo"}, Stamps: map[domain.Position]int64{p1: 100, p5: 500},
		CopiedAt: 500, Own: []domain.Position{p5}}, 6))
	var derived *DerivedLineupError
	require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(c)
		require.NoError(t, err)
		saved := carryTestStartingSave(t, tx, c, "ta", p1, "Mori", "", 200)
		_, err = eng.FollowLineupSave(tx, comp, saved, []domain.Position{p1})
		require.ErrorAs(t, err, &derived)
		return nil
	}))
	require.Equal(t, []string{"Pool A-1"}, derived.MatchIDs)
	require.Equal(t, "The lineup was saved, but the lineup carried to Pool A · Match 2 could not be worked out again "+
		"(a position the team size has no place for); it was kept as it was. Check that match's lineup.", derived.Warning())
	require.Equal(t, "The lineup was removed, but the lineup carried to Pool A · Match 2 could not be worked out again "+
		"(a position the team size has no place for); it was kept as it was. Check that match's lineup.", derived.RemovalWarning())
	require.Equal(t, "The match was started, but the lineup carried to Pool A · Match 2 could not be worked out again "+
		"(a position the team size has no place for); it was kept as it was. Check that match's lineup.", derived.StartWarning())
	require.Contains(t, derived.Error(), "team_lineup", "the log keeps the domain's own error")
}

// Several matches are named together, and a lineup with one member at two
// positions says so.
func TestLineupCarry_ARefusalOfSeveralCarriedLineupsNamesThemAll(t *testing.T) {
	d := &DerivedLineupError{
		MatchIDs: []string{"Pool A-1", "Pool A-2", "Pool A-3"},
		Labels:   []string{"Pool A · Match 2", "Pool A · Match 3", ""},
		Err:      fmt.Errorf("team_lineup: %w", domain.ErrLineupDuplicateMember),
	}
	require.Equal(t, "The lineup was removed, but the lineups carried to Pool A · Match 2, Pool A · Match 3 and Pool A-3 "+
		"could not be worked out again (a team member placed at two positions); they were kept as they were. "+
		"Check those matches' lineups.", d.RemovalWarning())
}

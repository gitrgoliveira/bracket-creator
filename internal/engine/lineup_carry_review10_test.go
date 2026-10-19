package engine

import (
	"errors"
	"fmt"
	"maps"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// olderDataLineups is a store handle whose lineups read gives match's lineup
// member at pos as well: a shape only older data holds, which the store
// refuses to write.
type olderDataLineups struct {
	state.StoreTx
	match  string
	pos    domain.Position
	member string
}

func (o olderDataLineups) LoadTeamLineupsWithRemoved(compID string) (map[string]domain.TeamLineup, error) {
	ls, err := o.StoreTx.LoadTeamLineupsWithRemoved(compID)
	for k, l := range ls {
		if l.MatchID == o.match && !l.IsRemoved() {
			l.MemberIDs = maps.Clone(l.MemberIDs)
			l.MemberIDs[o.pos] = o.member
			ls[k] = l
		}
	}
	return ls, err
}

// Two carried lineups refused for different reasons each keep their own
// reason, in the error and in the operator's sentence (stage 6 review 10, m9):
// the reason of the first refusal is never given for the others.
func TestLineupCarry_EachRefusedCarriedLineupKeepsItsOwnReason(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "rv10why"
	p1, p2, p3, p5 := domain.PositionNumbered(1), domain.PositionNumbered(2), domain.PositionNumbered(3), domain.PositionNumbered(5)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: c, Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tw", Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{p1: "Sato"}, MemberIDs: map[domain.Position]string{p1: "m-sato"},
		Stamps: map[domain.Position]int64{p1: 100}}, 3))
	// Pool A-1: a position the team size has no place for, kept from older data.
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", MatchID: "Pool A-1",
		Positions: map[domain.Position]string{p1: "Sato", p5: "Endo"}, MemberIDs: map[domain.Position]string{p1: "m-sato"},
		Stamps: map[domain.Position]int64{p1: 100, p5: 500}, CopiedAt: 500, Own: []domain.Position{p5}}, 6))
	// Pool A-2, made at 400 (before Pool A-1's lineup was); read below with
	// one member at its own second and third positions, kept from older data
	// (the store refuses to write that shape).
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", MatchID: "Pool A-2",
		Positions: map[domain.Position]string{p1: "Sato", p2: "Endo", p3: "Endo"},
		MemberIDs: map[domain.Position]string{p1: "m-sato", p2: "m-endo"},
		Stamps:    map[domain.Position]int64{p1: 100, p2: 400, p3: 400}, CopiedAt: 400, Own: []domain.Position{p2, p3}}, 3))
	var derived *DerivedLineupError
	require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(c)
		require.NoError(t, err)
		saved := carryTestStartingSave(t, tx, c, "ta", p1, "Mori", "m-mori", 200)
		_, err = eng.FollowLineupSave(olderDataLineups{StoreTx: tx, match: "Pool A-2", pos: p3, member: "m-endo"}, comp, saved, []domain.Position{p1})
		require.ErrorAs(t, err, &derived)
		return nil
	}))
	require.Equal(t, []string{"Pool A-1", "Pool A-2"}, derived.MatchIDs)
	require.True(t, errors.Is(derived, domain.ErrLineupDuplicateMember), "Pool A-2's own reason is kept: %v", derived)
	require.Equal(t, "The lineup was saved, but the lineups carried to Pool A · Match 2 (a position the team size has no place for) "+
		"and Pool A · Match 3 (a team member placed at two positions) could not be worked out again; "+
		"they were kept as they were. Check those matches' lineups.", derived.Warning())
}

// Matches refused for the same reason are named together under it; a
// different reason names its own matches (stage 6 review 10, m9).
func TestLineupCarry_RefusalsAreGroupedByReason(t *testing.T) {
	size := fmt.Errorf("team_lineup: position %q not allowed", "5")
	dup := fmt.Errorf("team_lineup: %w", domain.ErrLineupDuplicateMember)
	d := &DerivedLineupError{
		MatchIDs: []string{"Pool A-1", "Pool A-2", "Pool A-3"},
		Labels:   []string{"Pool A · Match 2", "Pool A · Match 3", "Pool A · Match 4"},
		Errs:     []error{size, dup, size},
	}
	require.Equal(t, "The lineup was removed, but the lineups carried to Pool A · Match 2 and Pool A · Match 4 "+
		"(a position the team size has no place for) and Pool A · Match 3 (a team member placed at two positions) "+
		"could not be worked out again; they were kept as they were. Check those matches' lineups.", d.RemovalWarning())
}

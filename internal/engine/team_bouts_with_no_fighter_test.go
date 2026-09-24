package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-tmfn: TeamBoutsWithNoFighter names the bouts the team finish gate
// exempts. It resolves each side's lineup in force the way the kachinuki
// roster does (match-scoped first, then the round index of the match).
func TestTeamBoutsWithNoFighter(t *testing.T) {
	const aID, bID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	setup := func(t *testing.T, teamSize int) (*Engine, *state.Store) {
		t.Helper()
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c", Kind: "team", Format: state.CompFormatKnockout, TeamSize: teamSize}))
		require.NoError(t, store.SaveParticipants("c", []domain.Player{
			{ID: aID, Name: "Ryu", Dojo: "R"}, {ID: bID, Name: "Tora", Dojo: "T"},
		}))
		require.NoError(t, store.SaveBracket("c", &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "R0M0", SideA: "X", SideB: "Y"}},
			{{ID: "R1M0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID}},
		}}))
		return New(store), store
	}
	lineup := func(teamID string, round int, matchID string, occupied ...int) domain.TeamLineup {
		positions := map[domain.Position]string{}
		for _, b := range occupied {
			pos, _ := domain.PositionForBout(3, b)
			positions[pos] = "f" + string(pos)
		}
		return domain.TeamLineup{TeamID: teamID, Round: round, MatchID: matchID, Positions: positions}
	}

	t.Run("both sides vacant at a position", func(t *testing.T) {
		e, store := setup(t, 3)
		require.NoError(t, store.SetTeamLineup("c", lineup(aID, 1, "", 1, 2), 3))
		require.NoError(t, store.SetTeamLineup("c", lineup(bID, 1, "", 1), 3))
		got, err := e.TeamBoutsWithNoFighter("c", "R1M0")
		require.NoError(t, err)
		assert.Equal(t, map[int]bool{3: true}, got, "bout 2 is vacant for Tora only, so it is still a bout")
	})

	t.Run("the lineup in force is the match's round, not an earlier one", func(t *testing.T) {
		e, store := setup(t, 3)
		// Round 0 leaves bout 3 open on both sides; round 1 (this match's
		// round) fields everyone, so nothing is exempt.
		require.NoError(t, store.SetTeamLineup("c", lineup(aID, 0, "", 1, 2), 3))
		require.NoError(t, store.SetTeamLineup("c", lineup(bID, 0, "", 1, 2), 3))
		require.NoError(t, store.SetTeamLineup("c", lineup(aID, 1, "", 1, 2, 3), 3))
		got, err := e.TeamBoutsWithNoFighter("c", "R1M0")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a match-scoped lineup wins", func(t *testing.T) {
		e, store := setup(t, 3)
		require.NoError(t, store.SetTeamLineup("c", lineup(aID, 1, "", 1, 2, 3), 3))
		require.NoError(t, store.SetTeamLineup("c", lineup(aID, 0, "R1M0", 1), 3))
		require.NoError(t, store.SetTeamLineup("c", lineup(bID, 0, "R1M0", 1), 3))
		got, err := e.TeamBoutsWithNoFighter("c", "R1M0")
		require.NoError(t, err)
		assert.Equal(t, map[int]bool{2: true, 3: true}, got)
	})

	t.Run("a side with no saved lineup is occupied everywhere", func(t *testing.T) {
		e, store := setup(t, 3)
		require.NoError(t, store.SetTeamLineup("c", lineup(bID, 1, ""), 3))
		got, err := e.TeamBoutsWithNoFighter("c", "R1M0")
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("nothing to answer", func(t *testing.T) {
		e, store := setup(t, 3)
		got, err := e.TeamBoutsWithNoFighter("c", "no-such-match")
		require.NoError(t, err)
		assert.Empty(t, got)
		got, err = e.TeamBoutsWithNoFighter("missing", "R1M0")
		require.NoError(t, err)
		assert.Empty(t, got)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "ind", Format: state.CompFormatKnockout}))
		got, err = e.TeamBoutsWithNoFighter("ind", "R1M0")
		require.NoError(t, err)
		assert.Empty(t, got)
		_, err = e.TeamBoutsWithNoFighter("bad/id", "R1M0")
		assert.Error(t, err)
	})
}

// lineupRoundOfTeamMatch mirrors the client's resolveRoundIndex: a bracket
// match's round index, else a pool/league/Swiss match's own Round, else 0.
func TestLineupRoundOfTeamMatch(t *testing.T) {
	assert.Equal(t, 2, lineupRoundOfTeamMatch(&state.MatchResult{Round: 7}, true, 2), "a bracket match uses its round index")
	assert.Equal(t, 3, lineupRoundOfTeamMatch(&state.MatchResult{Round: 3}, false, 0), "a pool match uses its own round")
	assert.Equal(t, 0, lineupRoundOfTeamMatch(&state.MatchResult{Round: -1}, false, 0), "a pool with no rounds reads as round 0")
}

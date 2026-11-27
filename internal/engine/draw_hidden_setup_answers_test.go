package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The BeforeDraw reads are the answers a draft whose draw is hidden from its
// caller gets (bc-drpb): the same read as the competition gives in setup, with
// the refusals of the real read, so a hidden draw shows in no difference.

func TestKachinukiFieldedPlacesBeforeDraw(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-before-draw", state.MatchStatusScheduled)

	t.Run("a kachinuki draft fills nothing although its match is held", func(t *testing.T) {
		got, err := f.eng.KachinukiFieldedPlacesBeforeDraw(f.compID, domain.TeamLineup{})
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
		real, err := f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, domain.TeamLineup{})
		require.NoError(t, err)
		assert.NotEmpty(t, real, "the real read fills the places, so the hidden answer is the setup one")
	})

	t.Run("a position the team size has no place for is the real read's refusal", func(t *testing.T) {
		bad := domain.TeamLineup{Positions: map[domain.Position]string{domain.PositionNumbered(9): "X"}}
		_, err := f.eng.KachinukiFieldedPlacesBeforeDraw(f.compID, bad)
		require.Error(t, err)
		assert.True(t, strings.HasPrefix(err.Error(), "team_lineup:"), err.Error())
		_, realErr := f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, bad)
		assert.Equal(t, realErr.Error(), err.Error())
	})

	t.Run("an unknown competition is a NotFoundError", func(t *testing.T) {
		_, err := f.eng.KachinukiFieldedPlacesBeforeDraw("no-such-comp", domain.TeamLineup{})
		var notFound *NotFoundError
		assert.True(t, errors.As(err, &notFound), "got %v", err)
	})

	t.Run("a competition that is not kachinuki fills nothing and refuses nothing", func(t *testing.T) {
		eng, _, comp := setupKachinukiComp(t, "fielded-before-draw-fixed", 3, func(c *state.Competition) {
			c.TeamMatchType = state.TeamMatchTypeFixed
		})
		bad := domain.TeamLineup{Positions: map[domain.Position]string{domain.PositionNumbered(9): "X"}}
		got, err := eng.KachinukiFieldedPlacesBeforeDraw(comp.ID, bad)
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Empty(t, got)
	})
}

func TestKachinukiRosterBeforeDraw(t *testing.T) {
	f := newFieldedPlacesFixture(t, "roster-before-draw", state.MatchStatusScheduled)

	t.Run("a kachinuki draft does not find the match, in the real read's words", func(t *testing.T) {
		_, err := f.eng.KachinukiRosterBeforeDraw(f.compID, fieldedMatch)
		var notFound *NotFoundError
		require.True(t, errors.As(err, &notFound), "got %v", err)
		_, realErr := f.eng.KachinukiRoster(f.compID, "Pool A-99")
		require.Error(t, realErr)
		assert.Equal(t, strings.ReplaceAll(realErr.Error(), "Pool A-99", fieldedMatch), err.Error())
	})

	t.Run("an unknown competition is a NotFoundError", func(t *testing.T) {
		_, err := f.eng.KachinukiRosterBeforeDraw("no-such-comp", fieldedMatch)
		var notFound *NotFoundError
		assert.True(t, errors.As(err, &notFound), "got %v", err)
	})

	t.Run("a competition that is not kachinuki is ErrNotKachinuki", func(t *testing.T) {
		eng, _, comp := setupKachinukiComp(t, "roster-before-draw-fixed", 3, func(c *state.Competition) {
			c.TeamMatchType = state.TeamMatchTypeFixed
		})
		_, err := eng.KachinukiRosterBeforeDraw(comp.ID, fieldedMatch)
		assert.ErrorIs(t, err, ErrNotKachinuki)
	})
}

func TestLeagueBeforeDrawReads(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "lg", Format: state.CompFormatLeague, TeamSize: 3}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "ko", Format: state.CompFormatKnockout}))

	standings, err := eng.LeagueStandingsBeforeDraw("lg")
	require.NoError(t, err)
	assert.NotNil(t, standings)
	assert.Empty(t, standings)

	_, err = eng.LeagueStandingsBeforeDraw("ko")
	var notFound *NotFoundError
	require.True(t, errors.As(err, &notFound), "a competition that is not a league is the real read's 404: %v", err)
	_, realErr := eng.LeagueStandings("ko")
	assert.Equal(t, realErr.Error(), err.Error())

	_, err = eng.LeagueStandingsBeforeDraw("nope")
	assert.True(t, errors.As(err, &notFound), "got %v", err)

	cands, err := eng.LeagueTiebreakCandidatesBeforeDraw("lg")
	require.NoError(t, err)
	assert.Empty(t, cands)
	_, err = eng.LeagueTiebreakCandidatesBeforeDraw("nope")
	assert.True(t, errors.As(err, &notFound), "got %v", err)
}

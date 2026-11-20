package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fieldedPlacesFixture is a five-person kachinuki competition with Ryu (side A)
// and Tora (side B) in one scheduled pool match, each team's members seeded by
// number with no names (team size plus two reserves), and Ryu's seven member ids
// in member number order.
type fieldedPlacesFixture struct {
	eng      *Engine
	store    *state.Store
	compID   string
	aID, bID string
	a        []string
}

const fieldedMatch = "Pool A-0"

func newFieldedPlacesFixture(t *testing.T, id string, status state.MatchStatus) fieldedPlacesFixture {
	t.Helper()
	eng, store, comp := setupKachinukiComp(t, id, 5, func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: fieldedMatch, SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: status,
	}}))
	return fieldedPlacesFixture{
		eng: eng, store: store, compID: comp.ID, aID: aID, bID: bID,
		a: teamMemberIDs(t, store, comp.ID, aID, 7),
	}
}

func (f fieldedPlacesFixture) places(t *testing.T, form domain.TeamLineup) map[domain.Position]string {
	t.Helper()
	got, err := f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, form)
	require.NoError(t, err)
	require.NotNil(t, got, "an answer is a map, never nil")
	return got
}

// An empty form, nothing saved: every place is filled by member number, members
// 1 to 5 (nameless, so by id alone); the reserves are never fielded by number.
func TestKachinukiFieldedPlaces_AnEmptyFormFillsEveryPlaceByNumber(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-empty", state.MatchStatusScheduled)

	got := f.places(t, domain.TeamLineup{})

	assert.Equal(t, map[domain.Position]string{
		domain.PosSenpo: f.a[0], domain.PosJiho: f.a[1], domain.PosChuken: f.a[2],
		domain.PosFukusho: f.a[3], domain.PosTaisho: f.a[4],
	}, got)
}

// The form is what the panel holds, saved or not: member 3 picked at senpo
// leaves members 1, 2, 4 and 5 for the other places (the worked example of the
// plan), and the place the form fills is not in the answer.
func TestKachinukiFieldedPlaces_AShortFormFillsOnlyItsEmptyPlaces(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-short", state.MatchStatusScheduled)

	got := f.places(t, domain.TeamLineup{
		Positions: map[domain.Position]string{domain.PosSenpo: ""},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: f.a[2]},
	})

	assert.Equal(t, map[domain.Position]string{
		domain.PosJiho: f.a[0], domain.PosChuken: f.a[1], domain.PosFukusho: f.a[3], domain.PosTaisho: f.a[4],
	}, got)
}

// A form whose every place is filled leaves nothing to fill: an empty map.
func TestKachinukiFieldedPlaces_AFullFormAnswersAnEmptyMap(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-full", state.MatchStatusScheduled)
	form := domain.TeamLineup{MemberIDs: map[domain.Position]string{
		domain.PosSenpo: f.a[4], domain.PosJiho: f.a[3], domain.PosChuken: f.a[2],
		domain.PosFukusho: f.a[1], domain.PosTaisho: f.a[0],
	}}

	assert.Empty(t, f.places(t, form))
}

// A typed name with no id holds its place and blocks no member; a reserve
// placed in the form holds its place and blocks no regular from the others.
func TestKachinukiFieldedPlaces_ATypedNameOrAReserveHoldsItsPlaceOnly(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-typed", state.MatchStatusScheduled)

	typed := f.places(t, domain.TeamLineup{Positions: map[domain.Position]string{domain.PosSenpo: "Ishida"}})
	assert.Equal(t, map[domain.Position]string{
		domain.PosJiho: f.a[0], domain.PosChuken: f.a[1], domain.PosFukusho: f.a[2], domain.PosTaisho: f.a[3],
	}, typed)

	reserve := f.places(t, domain.TeamLineup{MemberIDs: map[domain.Position]string{domain.PosTaisho: f.a[5]}})
	assert.Equal(t, map[domain.Position]string{
		domain.PosSenpo: f.a[0], domain.PosJiho: f.a[1], domain.PosChuken: f.a[2], domain.PosFukusho: f.a[3],
	}, reserve)
}

// The form wins over what is saved: on a started match whose team carries a
// full starting lineup, the form (one place left empty) is read as the match's
// own lineup, never the carried one and never the empty lineup a started match
// with nothing of its own fields. A named member fills with its id.
func TestKachinukiFieldedPlaces_TheFormStandsForTheSavedLineup(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-form-wins", state.MatchStatusRunning)
	ishida, err := f.store.RenameTeamMember(f.compID, f.aID, f.a[0], "Ishida")
	require.NoError(t, err)
	require.NoError(t, f.store.SetTeamLineup(f.compID, domain.TeamLineup{
		TeamID: f.aID, Positions: map[domain.Position]string{
			domain.PosSenpo: "R1", domain.PosJiho: "R2", domain.PosChuken: "R3", domain.PosFukusho: "R4", domain.PosTaisho: "R5",
		},
	}, 5))

	got := f.places(t, domain.TeamLineup{Positions: map[domain.Position]string{
		domain.PosSenpo: "R1", domain.PosJiho: "R2", domain.PosChuken: "R3", domain.PosFukusho: "R4",
	}})

	assert.Equal(t, map[domain.Position]string{domain.PosTaisho: ishida.ID}, got,
		"the form's one empty place, filled by member 1, now named")
}

// The read writes nothing: lineups.yaml is the same before and after.
func TestKachinukiFieldedPlaces_WritesNothing(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-read-only", state.MatchStatusScheduled)
	require.NoError(t, f.store.SetTeamLineup(f.compID, domain.TeamLineup{
		TeamID: f.aID, MatchID: fieldedMatch, Positions: map[domain.Position]string{domain.PosSenpo: "R1"},
	}, 5))
	before, err := f.store.LoadTeamLineups(f.compID)
	require.NoError(t, err)

	_ = f.places(t, domain.TeamLineup{})

	after, err := f.store.LoadTeamLineups(f.compID)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// Every case where the server would fill nothing answers an empty map, never an
// error: a fixed-order competition, an unknown match, a team that is not a side
// of the match (kachinukiFielded would fill for any team id).
func TestKachinukiFieldedPlaces_NothingToFillIsAnEmptyMap(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-nothing", state.MatchStatusScheduled)

	got, err := f.eng.KachinukiFieldedPlaces(f.compID, f.aID, "Pool A-9", domain.TeamLineup{})
	require.NoError(t, err)
	assert.Empty(t, got, "an unknown match")
	assert.NotNil(t, got)

	got, err = f.eng.KachinukiFieldedPlaces(f.compID, helper.NewUUID4(), fieldedMatch, domain.TeamLineup{})
	require.NoError(t, err)
	assert.Empty(t, got, "a team that is not a side of the match")
	assert.NotNil(t, got)

	comp, err := f.store.LoadCompetition(f.compID)
	require.NoError(t, err)
	comp.TeamMatchType = state.TeamMatchTypeFixed
	require.NoError(t, f.store.SaveCompetition(comp))
	got, err = f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, domain.TeamLineup{})
	require.NoError(t, err)
	assert.Empty(t, got, "a fixed-order competition")
	assert.NotNil(t, got)
}

// team-members.yaml that cannot be read: nothing is filled by number, as every
// kachinuki reader then fields it, the ERROR is logged naming the file, and the
// read answers an empty map.
func TestKachinukiFieldedPlaces_UnreadableMembersAnswerAnEmptyMap(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-members-unread", state.MatchStatusScheduled)
	makeTeamMembersUnreadable(t, f.store, f.compID)

	var got map[domain.Position]string
	var err error
	logs := captureLog(t, func() {
		got, err = f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, domain.TeamLineup{})
	})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got)
	assert.Contains(t, logs, "engine: ERROR: engine.KachinukiFieldedPlaces compId="+f.compID+": team-members.yaml load error:")
}

// lineups.yaml that cannot be read is an error, as the lineup-in-force read
// answers it: a fill promised over a lineup the server could not read would be
// one it never makes.
func TestKachinukiFieldedPlaces_UnreadableLineupsAreAnError(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-lineups-unread", state.MatchStatusScheduled)
	require.NoError(t, os.WriteFile(
		filepath.Join(f.store.GetFolder(), "competitions", f.compID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))

	_, err := f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, domain.TeamLineup{})
	require.Error(t, err)
	var notFound *NotFoundError
	assert.False(t, errors.As(err, &notFound))
}

// An unknown competition is a NotFoundError; a form position the team size has
// no place for is the domain's own refusal (the "team_lineup:" prefix the
// handlers answer 400).
func TestKachinukiFieldedPlaces_RefusesAnUnknownCompetitionAndABadPosition(t *testing.T) {
	f := newFieldedPlacesFixture(t, "fielded-places-refusals", state.MatchStatusScheduled)

	_, err := f.eng.KachinukiFieldedPlaces("no-such-comp", f.aID, fieldedMatch, domain.TeamLineup{})
	var notFound *NotFoundError
	require.True(t, errors.As(err, &notFound), "got %v", err)

	_, err = f.eng.KachinukiFieldedPlaces(f.compID, f.aID, fieldedMatch, domain.TeamLineup{
		Positions: map[domain.Position]string{domain.PositionNumbered(9): "X"},
	})
	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "team_lineup:"), err.Error())
}

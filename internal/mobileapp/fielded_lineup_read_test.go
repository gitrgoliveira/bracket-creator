package mobileapp

// POST /api/competitions/:id/teams/:tid/fielded-lineup/:matchId (bc-kno1,
// operator decisions D2 and D3): the lineup panel sends its form, saved or not,
// and is answered the places the server fills by member number, from the one
// engine read every kachinuki reader seats from. Public, read-only.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fieldedReadFixture is a five-person kachinuki competition served through the
// whole router, with two teams in one scheduled pool match and each team's
// members seeded by number with no names, the way the app seeds them.
type fieldedReadFixture struct {
	r      *gin.Engine
	store  *state.Store
	teamA  string
	teamB  string
	membA  []string // team A's member ids, member number order (7: size + 2 reserves)
	compID string
}

const fieldedReadMatch = "PoolA-0"

func newFieldedReadFixture(t *testing.T, selfRun bool, matchType state.TeamMatchType) fieldedReadFixture {
	t.Helper()
	store := newTempStore(t)
	if selfRun {
		seedSelfRunTournament(t, store)
	} else {
		seedOfficiatedTournament(t, store)
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "k1", Name: "Teams", Kind: "team", TeamSize: 5, TeamMatchType: matchType,
	}))
	require.NoError(t, store.SaveParticipants("k1", []domain.Player{
		{Name: "Ryu", Dojo: "Ryu Dojo"},
		{Name: "Tora", Dojo: "Tora Dojo"},
	}))
	teams, err := store.LoadParticipants("k1", false)
	require.NoError(t, err)
	require.Len(t, teams, 2)
	a, b := teams[0], teams[1]
	require.NoError(t, store.SavePoolMatches("k1", []state.MatchResult{
		{ID: fieldedReadMatch, SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusScheduled},
	}))
	store.EnsureLegacyUpgraded("k1")
	squads, err := store.LoadSquads("k1")
	require.NoError(t, err)
	members := slices.Clone(squads[a.ID])
	slices.SortFunc(members, func(x, y domain.TeamMember) int { return x.Index - y.Index })
	ids := make([]string, 0, len(members))
	for _, m := range members {
		require.Empty(t, m.Name, "seeded members carry no name")
		ids = append(ids, m.ID)
	}
	require.GreaterOrEqual(t, len(ids), 5, "the team is seeded with a member per place")
	return fieldedReadFixture{
		r: setupSelfRunRouter(t, store, NewFileVerifier(store)), store: store,
		teamA: a.ID, teamB: b.ID, membA: ids, compID: "k1",
	}
}

func (f fieldedReadFixture) path(teamID, matchID string) string {
	return "/api/competitions/" + f.compID + "/teams/" + teamID + "/fielded-lineup/" + matchID
}

// read posts body with NO password header at all, as a caller who never signed
// in, and returns the recorder.
func (f fieldedReadFixture) read(teamID, matchID string, body any) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, jsonReq(http.MethodPost, f.path(teamID, matchID), body))
	return w
}

// fielded decodes a 200 answer's fieldedMemberIds from the RAW body, so a
// missing key or a null map fails here rather than reading as empty.
func fielded(t *testing.T, w *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	got, ok := raw["fieldedMemberIds"]
	require.True(t, ok, "fieldedMemberIds is always present: %s", w.Body.String())
	require.NotEqual(t, "null", string(got), "an empty answer is {}, never null")
	var out map[string]string
	require.NoError(t, json.Unmarshal(got, &out))
	return out
}

// The headline case: no lineup saved, the form empty, nameless members. Every
// place is filled by member number, members 1 to 5, read in self-run without a
// password. A pick of member 3 at senpo, unsaved, moves the others along
// (D3: the panel asks again after every change).
func TestFieldedLineupRead_AnswersTheFormsEmptyPlacesByNumber(t *testing.T) {
	f := newFieldedReadFixture(t, true, state.TeamMatchTypeKachinuki)
	a := f.membA

	assert.Equal(t, map[string]string{
		"senpo": a[0], "jiho": a[1], "chuken": a[2], "fukusho": a[3], "taisho": a[4],
	}, fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{"positions": map[string]string{}})))

	assert.Equal(t, map[string]string{
		"jiho": a[0], "chuken": a[1], "fukusho": a[3], "taisho": a[4],
	}, fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{
		"positions": map[string]string{"senpo": ""},
		"memberIds": map[string]string{"senpo": a[2]},
	})), "member 3 picked at senpo: the others take 1, 2, 4 and 5")

	assert.Equal(t, map[string]string{
		"jiho": a[0], "chuken": a[1], "fukusho": a[2], "taisho": a[3],
	}, fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{
		"positions": map[string]string{"senpo": "Ishida"},
	})), "a typed name holds its place and blocks no member")

	assert.Empty(t, fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{
		"memberIds": map[string]string{"senpo": a[4], "jiho": a[3], "chuken": a[2], "fukusho": a[1], "taisho": a[0]},
	})), "a full form leaves nothing to fill")
}

// A read writes nothing: lineups.yaml is the same before and after, and no
// lineup is saved for the match.
func TestFieldedLineupRead_WritesNothing(t *testing.T) {
	f := newFieldedReadFixture(t, true, state.TeamMatchTypeKachinuki)
	before, err := f.store.LoadTeamLineups(f.compID)
	require.NoError(t, err)

	_ = fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{
		"positions": map[string]string{"senpo": "Ishida"},
	}))

	after, err := f.store.LoadTeamLineups(f.compID)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

// Public in an officiated tournament too: the main password is set and none is
// sent, as for every lineup read.
func TestFieldedLineupRead_NeedsNoPasswordWhenOfficiated(t *testing.T) {
	f := newFieldedReadFixture(t, false, state.TeamMatchTypeKachinuki)

	assert.Len(t, fielded(t, f.read(f.teamA, fieldedReadMatch, map[string]any{"positions": map[string]string{}})), 5)
}

// Everything the server would not fill answers 200 with an empty map: a
// fixed-order competition, an unknown match, a team the match does not hold,
// and a team-members.yaml that cannot be read.
func TestFieldedLineupRead_NothingToFillIsAnEmptyMap(t *testing.T) {
	empty := map[string]any{"positions": map[string]string{}}

	fixed := newFieldedReadFixture(t, true, state.TeamMatchTypeFixed)
	assert.Empty(t, fielded(t, fixed.read(fixed.teamA, fieldedReadMatch, empty)), "a fixed-order match")

	f := newFieldedReadFixture(t, true, state.TeamMatchTypeKachinuki)
	assert.Empty(t, fielded(t, f.read(f.teamA, "PoolA-9", empty)), "an unknown match")
	assert.Empty(t, fielded(t, f.read("not-in-the-match", fieldedReadMatch, empty)), "a team the match does not hold")
	assert.Len(t, fielded(t, f.read(f.teamB, fieldedReadMatch, empty)), 5, "the other side is filled too")

	path := filepath.Join(f.store.GetFolder(), "competitions", f.compID, "team-members.yaml")
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.Mkdir(path, 0o755))
	assert.Empty(t, fielded(t, f.read(f.teamA, fieldedReadMatch, empty)), "team members that cannot be read")
}

// A body that is not JSON, or a form position the team size has no place for,
// is a 400; an unknown competition is the 404 every lineup read answers.
func TestFieldedLineupRead_Refusals(t *testing.T) {
	f := newFieldedReadFixture(t, true, state.TeamMatchTypeKachinuki)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, f.path(f.teamA, fieldedReadMatch), nil)
	req.Header.Set("Content-Type", "application/json")
	f.r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "no body: %s", w.Body.String())

	w = f.read(f.teamA, fieldedReadMatch, map[string]any{"positions": map[string]string{"9": "X"}})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "team_lineup:")

	w = httptest.NewRecorder()
	f.r.ServeHTTP(w, jsonReq(http.MethodPost, "/api/competitions/no-such-comp/teams/x/fielded-lineup/m1",
		map[string]any{"positions": map[string]string{}}))
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// lineups.yaml that cannot be read is a 500, as the lineup-in-force read
// answers it: over lineups it could not read the server fills nothing.
func TestFieldedLineupRead_UnreadableLineupsAre500(t *testing.T) {
	f := newFieldedReadFixture(t, true, state.TeamMatchTypeKachinuki)
	require.NoError(t, os.WriteFile(
		filepath.Join(f.store.GetFolder(), "competitions", f.compID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))

	w := f.read(f.teamA, fieldedReadMatch, map[string]any{"positions": map[string]string{}})
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterPublicLineupHandlers(r.Group("/api"), f.store, f.store, failingLineupEngine{})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, jsonReq(http.MethodPost, f.path(f.teamA, fieldedReadMatch), map[string]any{"positions": map[string]string{}}))
	assert.Equal(t, http.StatusInternalServerError, w.Code, "an engine read that fails: %s", w.Body.String())
}

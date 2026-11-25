package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The per-competition public reads of a draft whose draw is hidden (bc-drpb):
// each is answered to the public as the same competition was answered before
// its draw, and to the organiser as the draw stands.

const pdrComp = "kachi-league"

// pdrWorld is an officiated tournament with one kachinuki team league of two
// teams, so its draw is one match both teams fight, mounted on the real router
// (NewRouterWithHub), whose public reads carry the audience judge.
type pdrWorld struct {
	r      *gin.Engine
	store  *state.Store
	eng    *engine.Engine
	teamA  string
	teamB  string
	player string
}

func newPDRWorld(t *testing.T) pdrWorld {
	t.Helper()
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Password: dhPassword, Courts: []string{"A"}, Mode: state.TournamentModeOfficiated,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: pdrComp, Name: "Kachinuki league", Kind: "team", Format: state.CompFormatLeague,
		TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(pdrComp, []domain.Player{
		{Name: "Tora", Dojo: "Dojo One"}, {Name: "Washi", Dojo: "Dojo Two"},
	}))
	teams, err := store.LoadParticipants(pdrComp, false)
	require.NoError(t, err)
	require.Len(t, teams, 2)
	eng := engine.New(store)
	hub := NewHub()
	t.Cleanup(hub.Close)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html></html>")}})
	r, _, limiter := NewRouterWithHub(store, eng, res, nil, hub, false)
	t.Cleanup(limiter.Close)
	w := pdrWorld{r: r, store: store, eng: eng, teamA: teams[0].ID, teamB: teams[1].ID, player: teams[0].ID}
	// What setup already holds: the team's starting lineup (round 0) and a
	// status naming no match.
	require.NoError(t, store.SetTeamLineup(pdrComp, domain.TeamLineup{
		TeamID: w.teamA, Round: 0, Positions: map[domain.Position]string{domain.PositionNumbered(1): "Aoki"},
	}, 3))
	require.NoError(t, store.SetCompetitorStatus(pdrComp, domain.CompetitorStatus{
		PlayerID: w.teamB, Eligible: true, RecordedAt: time.Date(2030, 6, 1, 9, 0, 0, 0, time.UTC),
	}))
	return w
}

func (w pdrWorld) do(t *testing.T, method, path string, body any, headers map[string]string) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, &buf)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// pdrRead is one public read of the competition, by method and path.
type pdrRead struct {
	name, method, path string
	body               any
}

// reads are every per-competition public read this task covers, at match
// matchID of the draw and the first team.
func (w pdrWorld) reads(matchID string) []pdrRead {
	base := "/api/competitions/" + pdrComp
	team := base + "/teams/" + w.teamA
	matchID = url.PathEscape(matchID)
	return []pdrRead{
		{name: "competitor-status", method: http.MethodGet, path: base + "/competitor-status"},
		{name: "round lineup", method: http.MethodGet, path: team + "/lineups/0"},
		{name: "match lineup", method: http.MethodGet, path: team + "/match-lineups/" + matchID},
		{name: "lineup in force", method: http.MethodGet, path: team + "/lineup-in-force/" + matchID},
		{name: "fielded lineup", method: http.MethodPost, path: team + "/fielded-lineup/" + matchID,
			body: fieldedLineupRequest{Positions: map[domain.Position]string{}}},
		{name: "kachinuki roster", method: http.MethodGet, path: base + "/matches/" + matchID + "/kachinuki-roster"},
		{name: "league standings", method: http.MethodGet, path: base + "/league/standings"},
		{name: "tie-break candidates", method: http.MethodGet, path: base + "/league-tiebreak/candidates"},
	}
}

type pdrAnswer struct {
	code int
	body string
}

func (w pdrWorld) answers(t *testing.T, matchID string, headers map[string]string) map[string]pdrAnswer {
	t.Helper()
	out := map[string]pdrAnswer{}
	for _, rd := range w.reads(matchID) {
		code, body := w.do(t, rd.method, rd.path, rd.body, headers)
		out[rd.name] = pdrAnswer{code: code, body: body}
	}
	return out
}

// drawMatch generates the draw and gives its one match's id.
func (w pdrWorld) drawMatch(t *testing.T) string {
	t.Helper()
	require.NoError(t, w.eng.GenerateDraw(pdrComp))
	matches, err := w.store.LoadPoolMatches(pdrComp)
	require.NoError(t, err)
	require.Len(t, matches, 1, "premise: two teams fight one league match")
	return matches[0].ID
}

func TestPublicDrawReads_HiddenDraftIsAnsweredAsBeforeItsDraw(t *testing.T) {
	w := newPDRWorld(t)
	// The match id the draw gives: generated once to learn it, then discarded
	// so the answers before the draw can be read at the same id.
	matchID := w.drawMatch(t)
	require.NoError(t, w.eng.DiscardDraw(pdrComp))
	before := w.answers(t, matchID, dhPublic)

	require.Equal(t, matchID, w.drawMatch(t), "premise: the draw gives the same match id again")
	comp, err := w.store.LoadCompetition(pdrComp)
	require.NoError(t, err)
	require.True(t, comp.DrawHidden, "premise: a generated draw is stored hidden")
	// What the draw adds: the match's own lineup and a status naming it.
	require.NoError(t, w.store.SetTeamLineup(pdrComp, domain.TeamLineup{
		TeamID: w.teamA, MatchID: matchID, Positions: map[domain.Position]string{domain.PositionNumbered(1): "Ishida"},
	}, 3))
	require.NoError(t, w.store.SetCompetitorStatus(pdrComp, domain.CompetitorStatus{
		PlayerID: w.teamA, Eligible: false, Reason: "fusenpai", MatchID: matchID,
		RecordedAt: time.Date(2030, 6, 1, 10, 0, 0, 0, time.UTC),
	}))

	hidden := w.answers(t, matchID, dhPublic)
	for name, want := range before {
		got := hidden[name]
		assert.Equal(t, want.code, got.code, "%s: status", name)
		if json.Valid([]byte(want.body)) {
			assert.JSONEq(t, want.body, got.body, "%s: the public reads the draft as before its draw", name)
		} else {
			assert.Equal(t, want.body, got.body, name)
		}
	}
	// The setup answers themselves, so a read that answered the draw both
	// times could not pass.
	assert.Equal(t, http.StatusNotFound, before["kachinuki roster"].code)
	assert.JSONEq(t, `[]`, before["league standings"].body)
	assert.NotContains(t, hidden["competitor-status"].body, matchID)
	assert.NotContains(t, hidden["lineup in force"].body, "Ishida")

	// The organiser reads the draw as it stands.
	organiser := w.answers(t, matchID, dhOrganiser)
	assert.Equal(t, http.StatusOK, organiser["kachinuki roster"].code, organiser["kachinuki roster"].body)
	assert.NotEqual(t, `[]`, organiser["league standings"].body)
	assert.Contains(t, organiser["competitor-status"].body, matchID)
	assert.Contains(t, organiser["match lineup"].body, "Ishida")
	assert.Contains(t, organiser["lineup in force"].body, "Ishida")

	// Published, the public reads what the organiser reads.
	changed, err := w.eng.SetDrawHidden(pdrComp, false)
	require.NoError(t, err)
	require.True(t, changed)
	published := w.answers(t, matchID, dhPublic)
	for name, want := range organiser {
		assert.Equal(t, want.code, published[name].code, "%s: status once published", name)
		assert.JSONEq(t, want.body, published[name].body, "%s: once published", name)
	}
}

func TestPublicDrawReads_WrongPasswordIs401(t *testing.T) {
	w := newPDRWorld(t)
	matchID := w.drawMatch(t)
	for _, rd := range w.reads(matchID) {
		code, body := w.do(t, rd.method, rd.path, rd.body, dhWrong)
		assert.Equal(t, http.StatusUnauthorized, code, "%s: %s", rd.name, body)
	}
}

func TestFieldedPlacesAsSetup(t *testing.T) {
	kachi := &state.Competition{TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}
	fixed := &state.Competition{TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed}
	tooFar := domain.TeamLineup{Positions: map[domain.Position]string{domain.PositionNumbered(9): "X"}}

	places, err := fieldedPlacesAsSetup(fixed, tooFar)
	require.NoError(t, err)
	assert.Empty(t, places, "a competition that is not kachinuki fills nothing")
	assert.NotNil(t, places)

	_, err = fieldedPlacesAsSetup(kachi, tooFar)
	require.Error(t, err, "a kachinuki form position the team size has no place for is refused, as in setup")

	places, err = fieldedPlacesAsSetup(kachi, domain.TeamLineup{})
	require.NoError(t, err)
	assert.Empty(t, places)
	assert.NotNil(t, places)
}

func TestKachinukiRosterAsSetup(t *testing.T) {
	err := kachinukiRosterAsSetup(&state.Competition{TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed}, "Pool A-0")
	assert.ErrorIs(t, err, engine.ErrNotKachinuki)
	err = kachinukiRosterAsSetup(&state.Competition{TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}, "Pool A-0")
	var notFound *engine.NotFoundError
	require.ErrorAs(t, err, &notFound)
	assert.Equal(t, "match Pool A-0 not found", notFound.Error())
}

// TestPublicDrawReads_HiddenSwissDraftStandingsAreAsBeforeItsDraw pins the
// Swiss standings read: three entrants, so round 1 gives one of them a bye the
// engine counts as a win while draw-ready. The public reads one zero row per
// entrant, as before the draw; the organiser reads the bye's win.
func TestPublicDrawReads_HiddenSwissDraftStandingsAreAsBeforeItsDraw(t *testing.T) {
	w := newPDRWorld(t)
	const swissComp = "swiss-draft"
	require.NoError(t, w.store.SaveCompetition(&state.Competition{
		ID: swissComp, Name: "Swiss", Kind: "individual", Format: state.CompFormatSwiss,
		SwissRounds: 2, Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))
	require.NoError(t, w.store.SaveParticipants(swissComp, []domain.Player{
		{Name: "Aoki", Dojo: "Dojo One"}, {Name: "Baba", Dojo: "Dojo Two"}, {Name: "Chiba", Dojo: "Dojo Three"},
	}))
	path := "/api/competitions/" + swissComp + "/swiss/standings"
	code, before := w.do(t, http.MethodGet, path, nil, dhPublic)
	require.Equal(t, http.StatusOK, code, before)

	require.NoError(t, w.eng.GenerateDraw(swissComp))
	comp, err := w.store.LoadCompetition(swissComp)
	require.NoError(t, err)
	require.True(t, comp.DrawHidden, "premise: a generated draw is stored hidden")

	code, hidden := w.do(t, http.MethodGet, path, nil, dhPublic)
	require.Equal(t, http.StatusOK, code, hidden)
	assert.JSONEq(t, before, hidden, "the public reads the hidden Swiss draft as before its draw")
	var rows []state.PlayerStanding
	require.NoError(t, json.Unmarshal([]byte(hidden), &rows))
	require.Len(t, rows, 3, "one row per entrant")
	for _, row := range rows {
		assert.Zero(t, row.Wins, "%s: no round-1 bye counted", row.Player.Name)
	}

	code, organiser := w.do(t, http.MethodGet, path, nil, dhOrganiser)
	require.Equal(t, http.StatusOK, code, organiser)
	assert.NotEqual(t, before, organiser, "premise: the organiser reads the bye's win")

	changed, err := w.eng.SetDrawHidden(swissComp, false)
	require.NoError(t, err)
	require.True(t, changed)
	code, published := w.do(t, http.MethodGet, path, nil, dhPublic)
	require.Equal(t, http.StatusOK, code, published)
	assert.JSONEq(t, organiser, published, "once published the public reads what the organiser reads")
}

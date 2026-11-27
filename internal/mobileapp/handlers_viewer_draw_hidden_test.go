package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A draft draw kept off the public pages (bc-drpb) on the viewer list and
// detail: the public is answered the same competition as before its draw, the
// organiser the draw, and both the drawHidden marker.

const (
	dhComp     = "draft-cup"
	dhPassword = "secret"
)

// drawHiddenWorld is an officiated tournament on one shiaijo with one
// individual competition in setup, six competitors and a number prefix, so
// its draw puts numbers on the roster.
type drawHiddenWorld struct {
	r     *gin.Engine
	store *state.Store
	eng   *engine.Engine
}

func newDrawHiddenWorld(t *testing.T, format string) drawHiddenWorld {
	t.Helper()
	r, store, eng, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Cup", Password: dhPassword, Courts: []string{"A"}, Mode: state.TournamentModeOfficiated,
	}))
	addDrawHiddenCompetition(t, store, dhComp, format)
	return drawHiddenWorld{r: r, store: store, eng: eng}
}

func addDrawHiddenCompetition(t *testing.T, store *state.Store, id, format string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: id, Name: "Cup " + id, Kind: "individual", Format: format,
		Courts: []string{"A"}, Status: state.CompStatusSetup, NumberPrefix: "K",
		PoolSize: 3, PoolWinners: 1,
	}))
	players := make([]domain.Player, 6)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Player %d", i+1), Dojo: fmt.Sprintf("Dojo %d", i+1)}
	}
	require.NoError(t, store.SaveParticipants(id, players))
}

// dhHeaders are the three callers of every route: the public sends no
// password, the organiser the right one, and a stale tab a wrong one.
var (
	dhPublic    = map[string]string{}
	dhOrganiser = map[string]string{"X-Tournament-Password": dhPassword}
	dhWrong     = map[string]string{"X-Tournament-Password": "not-it"}
)

func (w drawHiddenWorld) get(t *testing.T, path string, headers map[string]string) (int, []byte) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w.r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (w drawHiddenWorld) getJSON(t *testing.T, path string, headers map[string]string) map[string]any {
	t.Helper()
	code, body := w.get(t, path, headers)
	require.Equal(t, http.StatusOK, code, string(body))
	var out map[string]any
	require.NoError(t, json.Unmarshal(body, &out))
	return out
}

// listEntry is the viewer list's payload of id, nil when it is not listed.
func (w drawHiddenWorld) listEntry(t *testing.T, id string, headers map[string]string) map[string]any {
	t.Helper()
	code, body := w.get(t, "/api/viewer/competitions", headers)
	require.Equal(t, http.StatusOK, code, string(body))
	var list []map[string]any
	require.NoError(t, json.Unmarshal(body, &list))
	return payloadOf(list, id)
}

func payloadOf(list []map[string]any, id string) map[string]any {
	for _, entry := range list {
		if cfg, ok := entry["config"].(map[string]any); ok && cfg["id"] == id {
			return entry
		}
	}
	return nil
}

func (w drawHiddenWorld) detail(t *testing.T, headers map[string]string) map[string]any {
	t.Helper()
	return w.getJSON(t, "/api/viewer/competitions/"+dhComp, headers)
}

func (w drawHiddenWorld) setHidden(t *testing.T, hidden bool) {
	t.Helper()
	changed, err := w.eng.SetDrawHidden(dhComp, hidden)
	require.NoError(t, err)
	require.True(t, changed)
}

// asSetup is a payload with the config fields a draw itself writes taken
// out (the status, the version that hashes the config, and a league's one pool
// of everyone, poolSize and roundRobin), the flag the first roster load
// repairs (hasParticipantIDs) and the drawHidden marker, so a hidden draft's
// answer can be compared with its answer before the draw.
func asSetup(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	require.NotNil(t, payload)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(raw, &out))
	cfg := out["config"].(map[string]any)
	for _, k := range []string{"status", "configVersion", "drawHidden", "poolSize", "roundRobin", "hasParticipantIDs"} {
		delete(cfg, k)
	}
	return out
}

func configOf(payload map[string]any) map[string]any {
	return payload["config"].(map[string]any)
}

func numbersOf(payload map[string]any) []any {
	var out []any
	players, _ := configOf(payload)["players"].([]any)
	for _, p := range players {
		if n, ok := p.(map[string]any)["number"]; ok {
			out = append(out, n)
		}
	}
	return out
}

// hasDraw reports whether a payload carries anything of the draw: matches,
// a bracket, pools or a number on the roster.
func hasDraw(payload map[string]any) bool {
	if matches, _ := payload["poolMatches"].([]any); len(matches) > 0 {
		return true
	}
	if bracket, _ := payload["bracket"].(map[string]any); bracket != nil {
		if rounds, _ := bracket["rounds"].([]any); len(rounds) > 0 {
			return true
		}
	}
	if pools, _ := payload["pools"].([]any); len(pools) > 0 {
		return true
	}
	return len(numbersOf(payload)) > 0
}

var drawHiddenFormats = []string{state.CompFormatMixed, state.CompFormatKnockout, state.CompFormatLeague}

func TestViewerList_HiddenDraftIsAnsweredToThePublicAsSetup(t *testing.T) {
	for _, format := range drawHiddenFormats {
		t.Run(format, func(t *testing.T) {
			w := newDrawHiddenWorld(t, format)
			before := w.listEntry(t, dhComp, dhPublic)
			require.NotNil(t, before)
			require.False(t, hasDraw(before), "premise: no draw before it is made")

			require.NoError(t, w.eng.GenerateDraw(dhComp))

			public := w.listEntry(t, dhComp, dhPublic)
			require.NotNil(t, public, "the hidden draft stays on the list")
			assert.Equal(t, asSetup(t, before), asSetup(t, public), "the public reads the competition as it was before its draw")
			assert.Equal(t, string(state.CompStatusDrawReady), configOf(public)["status"])
			assert.Equal(t, true, configOf(public)["drawHidden"])
			assert.NotContains(t, public, "dataIssues")

			organiser := w.listEntry(t, dhComp, dhOrganiser)
			assert.True(t, hasDraw(organiser), "the organiser reads the draw")
			assert.NotEmpty(t, numbersOf(organiser), "the organiser reads the draw's numbers")
			assert.Equal(t, true, configOf(organiser)["drawHidden"], "the organiser reads the same marker")

			code, body := w.get(t, "/api/viewer/competitions", dhWrong)
			assert.Equal(t, http.StatusUnauthorized, code, string(body))
		})
	}
}

func TestViewerDetail_HiddenDraftIsAnsweredToThePublicAsSetup(t *testing.T) {
	for _, format := range drawHiddenFormats {
		t.Run(format, func(t *testing.T) {
			w := newDrawHiddenWorld(t, format)
			before := w.detail(t, dhPublic)
			require.False(t, hasDraw(before), "premise: no draw before it is made")

			require.NoError(t, w.eng.GenerateDraw(dhComp))

			public := w.detail(t, dhPublic)
			assert.Equal(t, asSetup(t, before), asSetup(t, public), "the public reads the competition as it was before its draw")
			assert.Equal(t, true, configOf(public)["drawHidden"])
			assert.NotContains(t, public, "poolQualifiers")
			assert.NotContains(t, public, "dataIssues")

			organiser := w.detail(t, dhOrganiser)
			assert.True(t, hasDraw(organiser), "the organiser reads the draw")
			assert.NotEmpty(t, numbersOf(organiser), "the organiser reads the draw's numbers")
			assert.Equal(t, true, configOf(organiser)["drawHidden"])
			if format == state.CompFormatMixed {
				assert.Contains(t, organiser, "poolQualifiers")
			}

			code, body := w.get(t, "/api/viewer/competitions/"+dhComp, dhWrong)
			assert.Equal(t, http.StatusUnauthorized, code, string(body))
		})
	}
}

// A public read made straight after a publish or an unpublish answers the
// new state, on the list and on the detail alike.
func TestViewerReads_FollowPublishAndUnpublishAtOnce(t *testing.T) {
	w := newDrawHiddenWorld(t, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(dhComp))
	require.False(t, hasDraw(w.listEntry(t, dhComp, dhPublic)))

	w.setHidden(t, false)
	list, detail := w.listEntry(t, dhComp, dhPublic), w.detail(t, dhPublic)
	assert.True(t, hasDraw(list), "the list shows the published draw")
	assert.True(t, hasDraw(detail), "the detail shows the published draw")
	assert.NotContains(t, configOf(list), "drawHidden")
	assert.NotContains(t, configOf(detail), "drawHidden")
	assert.Contains(t, detail, "poolQualifiers")

	w.setHidden(t, true)
	list, detail = w.listEntry(t, dhComp, dhPublic), w.detail(t, dhPublic)
	assert.False(t, hasDraw(list), "hidden again, the list shows no draw")
	assert.False(t, hasDraw(detail), "hidden again, the detail shows no draw")
	assert.Equal(t, true, configOf(list)["drawHidden"])
	assert.Equal(t, true, configOf(detail)["drawHidden"])
}

// A self-run tournament shows its draw as soon as it is made, whatever is
// stored: the public reads it and no answer carries the marker.
func TestViewerReads_SelfRunShowsAStoredHiddenDraw(t *testing.T) {
	w := newDrawHiddenWorld(t, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(dhComp))
	stored, err := w.store.LoadCompetition(dhComp)
	require.NoError(t, err)
	require.True(t, stored.DrawHidden, "premise: the draft is stored hidden")
	tour, err := w.store.LoadTournament()
	require.NoError(t, err)
	selfRun := *tour
	selfRun.Mode = state.TournamentModeSelfRun
	require.NoError(t, w.store.SaveTournament(&selfRun))

	list, detail := w.listEntry(t, dhComp, dhPublic), w.detail(t, dhPublic)
	assert.True(t, hasDraw(list))
	assert.True(t, hasDraw(detail))
	assert.NotContains(t, configOf(list), "drawHidden")
	assert.NotContains(t, configOf(detail), "drawHidden")
}

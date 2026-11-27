package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drawVisibilityPost sends Publish draw ("publish-draw") or Unpublish
// ("unpublish-draw") for a competition on the test router, which runs no
// sign-in gate (setupTestRouter).
func drawVisibilityPost(t *testing.T, r *gin.Engine, compID, route string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/"+route, nil)
	r.ServeHTTP(w, req)
	return w
}

// seedDrawReadyKnockout saves an eight-player knockout competition and
// generates its draw through the route, so it is a draft as the console
// leaves it: draw-ready and hidden.
func seedDrawReadyKnockout(t *testing.T, r *gin.Engine, store *state.Store, compID string) {
	t.Helper()
	players := make([]domain.Player, 8)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Visibility", Format: state.CompFormatKnockout, Kind: "individual",
		Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, players))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/generate-draw", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "generate-draw: %s", w.Body.String())
}

// visibilityEvents drains the events the hub has already fanned out to ch
// (Broadcast sends synchronously, before the handler answers) and returns the
// data of each draw_visibility_changed among them.
func visibilityEvents(t *testing.T, ch chan historyEntry) []map[string]any {
	t.Helper()
	var out []map[string]any
	for {
		select {
		case e := <-ch:
			if e.typ != EventDrawVisibilityChanged {
				continue
			}
			var ev struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal([]byte(e.payload), &ev))
			out = append(out, ev.Data)
		default:
			return out
		}
	}
}

func refusalCode(t *testing.T, w *httptest.ResponseRecorder) (code, message string) {
	t.Helper()
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Code, body.Error
}

// Publish and Unpublish flip the stored field, answer the record as stored,
// and broadcast draw_visibility_changed only when something changed: a tap
// repeated on a draft already in that state is 200 with nothing written and
// nothing sent.
func TestDrawVisibility_PublishAndUnpublish(t *testing.T) {
	r, store, _, hub, _ := setupTestRouter(t)
	const cid = "vis-flip"
	seedDrawReadyKnockout(t, r, store, cid)
	require.True(t, storedDrawHidden(t, store, cid), "a generated draw starts hidden")

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	steps := []struct {
		route      string
		wantHidden bool
		wantEvent  bool
	}{
		{"unpublish-draw", true, false}, // already hidden
		{"publish-draw", false, true},
		{"publish-draw", false, false}, // already published
		{"unpublish-draw", true, true},
	}
	for i, st := range steps {
		w := drawVisibilityPost(t, r, cid, st.route)
		require.Equalf(t, http.StatusOK, w.Code, "step %d %s: %s", i, st.route, w.Body.String())

		var answer state.Competition
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answer))
		assert.Equal(t, cid, answer.ID, "step %d answers the competition record", i)
		assert.Equal(t, state.CompStatusDrawReady, answer.Status, "step %d leaves the draft waiting to start", i)
		assert.Equal(t, st.wantHidden, answer.DrawHidden, "step %d answer", i)
		assert.Equal(t, st.wantHidden, storedDrawHidden(t, store, cid), "step %d stored before the answer", i)

		events := visibilityEvents(t, ch)
		if !st.wantEvent {
			assert.Empty(t, events, "step %d %s changed nothing, so it broadcasts nothing", i, st.route)
			continue
		}
		require.Len(t, events, 1, "step %d %s broadcasts once", i, st.route)
		assert.Equal(t, cid, events[0]["competitionId"])
		assert.Equal(t, st.wantHidden, events[0]["hidden"])
	}
}

// Off draw-ready there is no draft to show or hide: setup has no draw and a
// started competition is public whatever is stored. Both routes answer 409
// draw_not_ready and change nothing; an unknown competition is 404.
func TestDrawVisibility_RefusedOffDrawReady(t *testing.T) {
	r, store, _, hub, _ := setupTestRouter(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "vis-setup", Name: "Setup", Format: state.CompFormatKnockout, Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))
	const started = "vis-started"
	seedDrawReadyKnockout(t, r, store, started)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+started+"/start", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "start: %s", w.Body.String())

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	for _, cid := range []string{"vis-setup", started} {
		before, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		for _, route := range []string{"publish-draw", "unpublish-draw"} {
			t.Run(cid+"/"+route, func(t *testing.T) {
				w := drawVisibilityPost(t, r, cid, route)
				require.Equalf(t, http.StatusConflict, w.Code, "%s", w.Body.String())
				code, message := refusalCode(t, w)
				assert.Equal(t, drawNotReadyCode, code)
				assert.Equal(t, drawNotReadyMessage, message)

				after, err := store.LoadCompetition(cid)
				require.NoError(t, err)
				assert.Equal(t, before.Status, after.Status)
				assert.Equal(t, before.DrawHidden, after.DrawHidden, "a refusal writes nothing")
				assert.Empty(t, visibilityEvents(t, ch), "a refusal broadcasts nothing")
			})
		}
	}

	for _, route := range []string{"publish-draw", "unpublish-draw"} {
		w := drawVisibilityPost(t, r, "no-such-comp", route)
		assert.Equalf(t, http.StatusNotFound, w.Code, "%s unknown competition: %s", route, w.Body.String())
	}
}

// A self-run tournament shows its draw as soon as it is made (DrawHiddenIn
// judges the mode at read time), so both routes refuse the organiser there
// with 409 draw_visibility_self_run and leave the stored field as it is. The
// anonymous caller's 401 is the gate's (middleware_selfrun_test.go).
func TestDrawVisibility_RefusedInSelfRun(t *testing.T) {
	r, store, _, hub, _ := setupTestRouter(t)
	const cid = "vis-self-run"
	seedDrawReadyKnockout(t, r, store, cid)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Self Run", Date: "01-06-2026", Courts: []string{"A"}, Mode: state.TournamentModeSelfRun,
	}))
	stored := storedDrawHidden(t, store, cid)

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	for _, route := range []string{"publish-draw", "unpublish-draw"} {
		w := drawVisibilityPost(t, r, cid, route)
		require.Equalf(t, http.StatusConflict, w.Code, "%s: %s", route, w.Body.String())
		code, message := refusalCode(t, w)
		assert.Equal(t, drawVisibilitySelfRunCode, code)
		assert.Equal(t, drawVisibilitySelfRunMessage, message)
		assert.Equal(t, stored, storedDrawHidden(t, store, cid), "%s writes nothing in self-run", route)
		assert.Empty(t, visibilityEvents(t, ch), "%s broadcasts nothing in self-run", route)
	}
}

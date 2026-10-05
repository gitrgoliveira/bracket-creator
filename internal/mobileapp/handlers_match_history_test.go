package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/competitions/:id/matches/:mid/history (bc-mrgc): a match's write
// history in the order the changes were made, which the score editors show.

func newMatchHistoryFixture(t *testing.T) (*state.Store, func(path string) *httptest.ResponseRecorder) {
	t.Helper()
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "Individuals"}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Yamada", Dojo: "Kyoto"},
		{Name: "Tanaka", Dojo: "Osaka"},
	}))
	players, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	a, b := players[0], players[1]
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Status: state.MatchStatusScheduled},
	}))
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	get := func(path string) *httptest.ResponseRecorder {
		req := jsonReq(http.MethodGet, path, nil)
		req.Header.Set("X-Tournament-Password", "main-pw")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	return store, get
}

func TestMatchHistory_EntriesInStampOrder(t *testing.T) {
	store, get := newMatchHistoryFixture(t)
	// Arrival order is not stamp order: the offline write made first arrives
	// last. An unstamped write is placed at the time the server took it.
	for _, e := range []state.MatchHistoryEntry{
		{MatchID: "Pool A-0", Door: "score", Stamp: 3000, ReceivedAt: 3100, Changed: []string{"points"}, Outcomes: map[string]string{"points": "applied"}},
		{MatchID: "Pool A-0", Door: "quick-score", Stamp: 0, ReceivedAt: 2500, Changed: []string{"result"}, Outcomes: map[string]string{"result": "applied"}},
		{MatchID: "Pool A-0", Door: "score", Stamp: 1000, ReceivedAt: 4000, Changed: []string{"points"}, Outcomes: map[string]string{"points": "held"},
			Held: map[string]json.RawMessage{"points": json.RawMessage(`{"ipponsA":["M"],"ipponsB":[],"hansokuA":0,"hansokuB":0}`)}},
	} {
		require.NoError(t, store.AppendMatchHistory("c1", e))
	}

	w := get("/api/competitions/c1/matches/Pool%20A-0/history")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []state.MatchHistoryEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 3)
	assert.Equal(t, []int64{1000, 0, 3000}, []int64{got[0].Stamp, got[1].Stamp, got[2].Stamp},
		"oldest change first; the unstamped one at the time it was taken")
	assert.Equal(t, "held", got[0].Outcomes["points"])
	assert.JSONEq(t, `{"ipponsA":["M"],"ipponsB":[],"hansokuA":0,"hansokuB":0}`, string(got[0].Held["points"]),
		"a held change is answered with its value")
}

func TestMatchHistory_NoneIsAnEmptyList(t *testing.T) {
	_, get := newMatchHistoryFixture(t)
	w := get("/api/competitions/c1/matches/Pool%20A-1/history")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `[]`, w.Body.String())
}

func TestMatchHistory_UnknownCompetitionOrMatchIs404(t *testing.T) {
	_, get := newMatchHistoryFixture(t)
	assert.Equal(t, http.StatusNotFound, get("/api/competitions/nope/matches/Pool%20A-0/history").Code)
	assert.Equal(t, http.StatusNotFound, get("/api/competitions/c1/matches/Pool%20Z-9/history").Code)
}

// A real score write lands in the history the route answers with, naming the
// groups it changed as the client sent them.
func TestMatchHistory_AScoreWriteIsListed(t *testing.T) {
	store, get := newMatchHistoryFixture(t)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	req := jsonReq(http.MethodPut, "/api/competitions/c1/matches/Pool%20A-0/score", map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"modifiedAt": 1, "changed": []string{"points"},
	})
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = get("/api/competitions/c1/matches/Pool%20A-0/history")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got []state.MatchHistoryEntry
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "score", got[0].Door)
	assert.Equal(t, []string{"points"}, got[0].Changed)
	assert.Equal(t, "applied", got[0].Outcomes["points"])
}

func TestMatchHistoryInStampOrder_NeverNil(t *testing.T) {
	assert.NotNil(t, matchHistoryInStampOrder(nil))
}

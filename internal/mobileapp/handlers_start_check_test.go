package mobileapp

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startCheckIDs are the roster ids startCheckFixture saves.
type startCheckIDs struct {
	alice, bob, carol, dave, eve string
}

// startCheckFixture saves a mixed competition whose court A runs Alice v Bob
// while Alice v Carol and Bob v Dave wait on the same court, and Dave v Eve
// runs on court B. It returns the store's directory so a test can compare its files.
func startCheckFixture(t *testing.T, compID string) (*state.Store, string, startCheckIDs) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	ids := startCheckIDs{helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: ids.alice, Name: "Alice", Dojo: "A"},
		{ID: ids.bob, Name: "Bob", Dojo: "B"},
		{ID: ids.carol, Name: "Carol", Dojo: "C"},
		{ID: ids.dave, Name: "Dave", Dojo: "D"},
		{ID: ids.eve, Name: "Eve", Dojo: "E"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: ids.alice, SideBID: ids.bob, Status: state.MatchStatusRunning, Court: "A"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: ids.alice, SideBID: ids.carol, Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "Pool A-2", SideA: "Dave", SideB: "Eve", SideAID: ids.dave, SideBID: ids.eve, Status: state.MatchStatusRunning, Court: "B"},
		{ID: "Pool A-3", SideA: "Bob", SideB: "Dave", SideAID: ids.bob, SideBID: ids.dave, Status: state.MatchStatusCompleted, Winner: "Bob", WinnerID: ids.bob, Court: "A"},
		{ID: "Pool A-4", SideA: "Bob", SideB: "Dave", SideAID: ids.bob, SideBID: ids.dave, Status: state.MatchStatusScheduled, Court: "A"},
	}))
	return store, dir, ids
}

// startCheckRouter registers the match handlers on /api, as the app does.
func startCheckRouter(store *state.Store) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterMatchHandlers(r.Group("/api"), engine.New(store), store, store, NewHub(), NewFileVerifier(store), store)
	return r
}

func getStartCheck(t *testing.T, r *gin.Engine, compID, mid, court string, header ...string) *httptest.ResponseRecorder {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "/api/competitions/"+compID+"/matches/"+mid+"/start-check?court="+url.QueryEscape(court), nil)
	require.NoError(t, err)
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// treeBytes reads every file under dir, keyed by its path.
func treeBytes(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	require.NoError(t, filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = b
		return nil
	}))
	return out
}

func TestStartCheck_AcceptedWhenOnlyTheBoutOnItsCourtHoldsItsCompetitor(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-ok")
	r := startCheckRouter(store)

	w := getStartCheck(t, r, "sc-ok", "Pool A-1", "A")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"ok": true}`, w.Body.String())
}

func TestStartCheck_RefusesACompetitorFightingOnAnotherCourt(t *testing.T) {
	store, _, ids := startCheckFixture(t, "sc-simul")
	r := startCheckRouter(store)

	// Bob v Dave waits on court A; Bob's bout there is skipped, Dave's on
	// court B is not.
	w := getStartCheck(t, r, "sc-simul", "Pool A-4", "A")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ineligible_competitor", body["error"])
	assert.Equal(t, ids.dave, body["playerId"])
	assert.Equal(t, "Dave is fighting now in Pool A · Match 3 on Shiaijo B. Finish that match first.", body["reasonHuman"])
}

func TestStartCheck_RefusesAPickNoLongerScheduled(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-not-sched")
	r := startCheckRouter(store)

	for mid, status := range map[string]string{"Pool A-0": "running", "Pool A-3": "completed"} {
		w := getStartCheck(t, r, "sc-not-sched", mid, "A")
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, "match_not_scheduled", body["error"])
		assert.Equal(t, mid, body["matchId"])
		assert.Equal(t, status, body["status"])
		assert.Equal(t, "This match has already started or finished on another device. Check the court's list.", body["reasonHuman"])
	}
}

func TestStartCheck_UnknownMatchOrCompetitionIs404(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-404")
	r := startCheckRouter(store)

	assert.Equal(t, http.StatusNotFound, getStartCheck(t, r, "sc-404", "no-such", "A").Code)
	assert.Equal(t, http.StatusNotFound, getStartCheck(t, r, "no-such-comp", "Pool A-1", "A").Code)
}

// TestStartCheck_IneligibleBodyIsTheScoreStartsOwn pins that the check and
// the /score start answer a barred side with the same bytes: the console
// shows either the same way (respondStartRefusal).
func TestStartCheck_IneligibleBodyIsTheScoreStartsOwn(t *testing.T) {
	for _, decision := range []string{"kiken-voluntary", "kiken-injury", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			compID := "sc-same-body-" + decision
			r, _, _, _ := barredCompetitorFixture(t, compID, true)
			// Alice is barred by Pool A-0; Pool A-1 (Alice v Carol) has no
			// court, so the /score start answers for the side, not the court.
			resp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{
				Decision: decision, DecisionBy: "aka", DecisionReason: "test",
			})
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

			check := getStartCheck(t, r, compID, "Pool A-1", "")
			start := putScoreJSON(t, r, compID, "Pool A-1", state.MatchResult{
				ID: "Pool A-1", Status: state.MatchStatusRunning,
			})
			require.Equal(t, http.StatusConflict, start.Code, start.Body.String())
			require.Equal(t, http.StatusConflict, check.Code, check.Body.String())
			assert.Contains(t, check.Body.String(), `"error":"ineligible_competitor"`)
			assert.Equal(t, start.Body.String(), check.Body.String())
		})
	}
}

func TestStartCheck_WritesNothing(t *testing.T) {
	store, dir, _ := startCheckFixture(t, "sc-no-write")
	r := startCheckRouter(store)
	// A first read may upgrade legacy shapes on disk; that is the store's,
	// not the check's, so it happens before the snapshot.
	_, err := store.LoadPoolMatches("sc-no-write")
	require.NoError(t, err)
	before := treeBytes(t, dir)

	for _, mid := range []string{"Pool A-0", "Pool A-1", "no-such"} {
		getStartCheck(t, r, "sc-no-write", mid, "A")
	}
	assert.Equal(t, before, treeBytes(t, dir))
}

// TestStartCheck_OpenToCourtOperatorsInSelfRun pins that the check is
// operational play: an anonymous caller (the public page sends an EMPTY
// password) reaches it in self-run, like start and send back.
func TestStartCheck_OpenToCourtOperatorsInSelfRun(t *testing.T) {
	assert.False(t, isSelfRunMainGatedConfigRoute(http.MethodGet, "/api/competitions/:id/matches/:mid/start-check"))

	store, _, _ := startCheckFixture(t, "sc-self-run")
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	w := getStartCheck(t, r, "sc-self-run", "Pool A-1", "A", "X-Tournament-Password", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"ok": true}`, w.Body.String())
}

// bothBarredKnockoutFixture builds a competition where Alice is barred by a
// withdrawal on Pool A-0 and Dave by one on Pool A-1, leaving bracket match
// m-r1-0 scheduled with both sides already barred.
func bothBarredKnockoutFixture(t *testing.T, compID string) *gin.Engine {
	t.Helper()
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusKnockout,
	}))
	aliceID, bobID, carolID, daveID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: daveID, Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Dave", SideB: "Carol", SideAID: daveID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Alice", SideAID: aliceID, SideB: "Dave", SideBID: daveID,
			Status: state.MatchStatusScheduled, DisplayRound: 1, MatchNumber: 1}},
	}}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	require.Equal(t, http.StatusOK, postDecisionJSON(t, r, compID, "Pool A-0",
		DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"}).Code)
	require.Equal(t, http.StatusOK, postDecisionJSON(t, r, compID, "Pool A-1",
		DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"}).Code)

	return r
}

func TestStartCheck_BothSidesBarred(t *testing.T) {
	t.Run("pool match", func(t *testing.T) {
		compID := "sc-both-barred-pool"
		r, _, _ := bothBarredPoolFixture(t, compID)

		check := getStartCheck(t, r, compID, "Pool A-2", "")
		require.Equal(t, http.StatusConflict, check.Code, check.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(check.Body.Bytes(), &body))
		assert.Equal(t, "ineligible_competitor", body["error"])
		reasonHuman, _ := body["reasonHuman"].(string)
		assert.Contains(t, reasonHuman, "Neither")
		assert.Contains(t, reasonHuman, "Alice")
		assert.Contains(t, reasonHuman, "Dave")
		assert.Contains(t, reasonHuman, "correct the earlier withdrawal or the draw")

		start := putScoreJSON(t, r, compID, "Pool A-2", state.MatchResult{
			ID: "Pool A-2", Status: state.MatchStatusRunning,
		})
		require.Equal(t, http.StatusConflict, start.Code, start.Body.String())
		assert.Equal(t, start.Body.String(), check.Body.String())
	})

	t.Run("bracket match", func(t *testing.T) {
		compID := "sc-both-barred-knockout"
		r := bothBarredKnockoutFixture(t, compID)

		check := getStartCheck(t, r, compID, "m-r1-0", "")
		require.Equal(t, http.StatusConflict, check.Code, check.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(check.Body.Bytes(), &body))
		assert.Equal(t, "ineligible_competitor", body["error"])
		reasonHuman, _ := body["reasonHuman"].(string)
		assert.Contains(t, reasonHuman, "Neither")
		assert.Contains(t, reasonHuman, "Alice")
		assert.Contains(t, reasonHuman, "Dave")
		assert.Contains(t, reasonHuman, "correct the earlier withdrawal or the draw")

		start := putScoreJSON(t, r, compID, "m-r1-0", state.MatchResult{
			ID: "m-r1-0", Status: state.MatchStatusRunning,
		})
		require.Equal(t, http.StatusConflict, start.Code, start.Body.String())
		assert.Equal(t, start.Body.String(), check.Body.String())
	})
}

func TestStartCheck_OversizedMatchIDIs400(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-oversized")
	r := startCheckRouter(store)

	mid := strings.Repeat("x", MaxLenMatchID+1)
	w := getStartCheck(t, r, "sc-oversized", mid, "A")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "matchId: must be <= 128 characters", body["error"])
}

func TestStartCheck_OversizedCourtIs400(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-oversized-court")
	r := startCheckRouter(store)

	court := strings.Repeat("x", MaxLenMatchCourt+1)
	w := getStartCheck(t, r, "sc-oversized-court", "Pool A-1", court)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "court: must be <= 32 characters", body["error"])
}

func TestStartCheck_MissingCourtIs400(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-missing-court")
	r := startCheckRouter(store)

	req, err := http.NewRequest(http.MethodGet, "/api/competitions/sc-missing-court/matches/Pool A-1/start-check", nil)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "court is required", body["error"])
}

func TestStartCheck_CourtMismatchIs409(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-mismatch")
	r := startCheckRouter(store)

	// Pool A-1 is stored on Court "A"; console operating Court "B" asks.
	w := getStartCheck(t, r, "sc-mismatch", "Pool A-1", "B")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "match_on_other_court", body["error"])
	assert.Equal(t, "Pool A-1", body["matchId"])
	assert.Equal(t, "A", body["court"])
	assert.Equal(t, matchOnOtherCourtReasonHuman, body["reasonHuman"])
}

func TestStartCheck_EmptyCourtForUnassignedMatch(t *testing.T) {
	store, _, _ := startCheckFixture(t, "sc-unassigned")
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("sc-unassigned", []domain.Player{
		{ID: aliceID, Name: "UnassignedAlice", Dojo: "A"},
		{ID: bobID, Name: "UnassignedBob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches("sc-unassigned", []state.MatchResult{
		{ID: "Pool-Free", SideA: "UnassignedAlice", SideB: "UnassignedBob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled, Court: ""},
	}))
	r := startCheckRouter(store)

	w := getStartCheck(t, r, "sc-unassigned", "Pool-Free", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"ok": true}`, w.Body.String())
}

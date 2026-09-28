package mobileapp

// A self-run tournament lets the participants run the representative bout
// (daihyosen) of a tied knockout team match like any bout (operator decision,
// bc-dhas): the public page adds it, scores it, finishes the match on the
// winner it decides, and removes one added by mistake, all with no organiser
// password. A hantei stays the organiser's: an anonymous write may not record,
// move or clear the judges' decision on the representative bout, and may only
// send back the one the organiser recorded (409 hantei_organiser_only
// otherwise). Officiated tournaments are unchanged.
//
// Every request is built the way the public page sends it: an EMPTY
// X-Tournament-Password header, not a missing one.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const repBoutMatchPath = "/api/competitions/c1/matches/B1"

// repBoutFixture is a knockout team competition whose one match, B1, is
// running with its three bouts fought and tied: one win each and a draw.
type repBoutFixture struct {
	r     *gin.Engine
	store *state.Store
	now   int64
}

func newRepBoutFixture(t *testing.T, selfRun bool) *repBoutFixture {
	t.Helper()
	store := newTempStore(t)
	if selfRun {
		seedSelfRunTournament(t, store, "admin-pw")
	} else {
		seedOfficiatedTournament(t, store)
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "Teams", Kind: "team", Format: state.CompFormatKnockout,
		TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
	}))
	teamA := domain.Player{ID: "11111111-1111-4111-1111-111111111111", Name: "TeamA", Dojo: "A"}
	teamB := domain.Player{ID: "22222222-2222-4222-2222-222222222222", Name: "TeamB", Dojo: "B"}
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{teamA, teamB}))
	now := time.Now().UnixMilli()
	require.NoError(t, store.SaveBracket("c1", &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "B1", SideA: teamA.Name, SideB: teamB.Name, SideAID: teamA.ID, SideBID: teamB.ID,
		Status: state.MatchStatusRunning, ModifiedAt: now - 60_000,
		SubResults: []state.SubMatchResult{
			{Position: 1, IpponsA: []string{"M"}, Winner: teamA.Name},
			{Position: 2, IpponsB: []string{"K"}, Winner: teamB.Name},
			{Position: 3, Decision: "hikiwake"},
		},
	}}}}))
	return &repBoutFixture{r: setupSelfRunRouter(t, store, NewFileVerifier(store)), store: store, now: now}
}

// send serves one request with the given main password; "" is the public page.
func (f *repBoutFixture) send(method, path, password string, body any) *httptest.ResponseRecorder {
	req := jsonReq(method, path, body)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	f.r.ServeHTTP(w, req)
	return w
}

func (f *repBoutFixture) addRepBout(t *testing.T) {
	t.Helper()
	w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now})
	require.Equal(t, http.StatusOK, w.Code, "adding the representative bout: %s", w.Body.String())
}

// score sends the team sheet the way the public editor does: the three fought
// bouts as they stand, then the representative bout (none when repBout is
// nil, which drops the row).
func (f *repBoutFixture) score(password string, status state.MatchStatus, winner string, at int64, repBout map[string]any) *httptest.ResponseRecorder {
	subs := []any{
		map[string]any{"position": 1, "sideA": "", "sideB": "", "ipponsA": []string{"M"}, "ipponsB": []string{}, "winner": "TeamA", "decision": ""},
		map[string]any{"position": 2, "sideA": "", "sideB": "", "ipponsA": []string{}, "ipponsB": []string{"K"}, "winner": "TeamB", "decision": ""},
		map[string]any{"position": 3, "sideA": "", "sideB": "", "ipponsA": []string{}, "ipponsB": []string{}, "winner": "", "decision": "hikiwake"},
	}
	if repBout != nil {
		subs = append(subs, repBout)
	}
	return f.send(http.MethodPut, repBoutMatchPath+"/score", password, map[string]any{
		"sideA": "TeamA", "sideB": "TeamB", "status": status, "winner": winner,
		"ipponsA": []string{}, "ipponsB": []string{}, "modifiedAt": at,
		"subResults": subs,
	})
}

// repBoutRow is the representative bout's row as the team editor writes it:
// it keeps the team names and always carries decision "daihyosen".
func repBoutRow(ipponsA, ipponsB []string, winner string) map[string]any {
	return map[string]any{
		"position": state.DaihyosenSubPosition, "sideA": "TeamA", "sideB": "TeamB",
		"ipponsA": ipponsA, "ipponsB": ipponsB, "winner": winner, "decision": "daihyosen",
	}
}

func (f *repBoutFixture) storedRepBout(t *testing.T) *state.SubMatchResult {
	t.Helper()
	subs := storedB1(t, f.store, "c1").SubResults
	for i := range subs {
		if subs[i].Position == state.DaihyosenSubPosition {
			return &subs[i]
		}
	}
	t.Fatal("B1 carries no representative bout")
	return nil
}

func TestSelfRun_ParticipantsRunTheRepresentativeBout(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)

	w := f.score("", state.MatchStatusRunning, "", f.now+100, repBoutRow([]string{"M"}, []string{}, "TeamA"))
	require.Equal(t, http.StatusOK, w.Code, "scoring it: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), `"applied":false`)
	assert.Equal(t, []string{"M"}, f.storedRepBout(t).IpponsA)

	w = f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, repBoutRow([]string{"M"}, []string{}, "TeamA"))
	require.Equal(t, http.StatusOK, w.Code, "finishing on the winner it decides: %s", w.Body.String())
	bm := storedB1(t, f.store, "c1")
	assert.Equal(t, state.MatchStatusCompleted, bm.Status)
	assert.Equal(t, "TeamA", bm.Winner)
	assert.Equal(t, "self-reported", bm.ResultSource)

	w = f.score("", state.MatchStatusCompleted, "TeamB", f.now+300, repBoutRow([]string{}, []string{"M"}, "TeamB"))
	require.Equal(t, http.StatusConflict, w.Code, "a finished match stays finished: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), `"result_finalized"`)
	assert.Equal(t, "TeamA", storedB1(t, f.store, "c1").Winner)
}

// The editor saves the sheet before it removes the row, and that save carries
// the row, so the save has to land for the remove to be sent at all. With the
// row still unscored the editor leaves its points out, as it sends it.
func TestSelfRun_ParticipantsRemoveARepresentativeBoutAddedByMistake(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)

	unscored := repBoutRow(nil, nil, "")
	delete(unscored, "ipponsA")
	delete(unscored, "ipponsB")
	w := f.score("", state.MatchStatusRunning, "", f.now+100, unscored)
	require.Equal(t, http.StatusOK, w.Code, "the save before the remove: %s", w.Body.String())

	w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 200})
	require.Equal(t, http.StatusOK, w.Code, "removing it: %s", w.Body.String())
	assert.False(t, carriesDaihyosenRow(storedB1(t, f.store, "c1").SubResults))
}

func TestSelfRun_HanteiOnTheRepresentativeBoutStaysTheOrganisers(t *testing.T) {
	legacyFlag := repBoutRow([]string{}, []string{}, "TeamA")
	legacyFlag["decidedByHantei"] = true
	cases := []struct {
		name    string
		repBout map[string]any
	}{
		{"the judges'-decision mark", hanteiRow()},
		{"the legacy decidedByHantei flag", legacyFlag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)

			w := f.score("", state.MatchStatusRunning, "", f.now+100, tc.repBout)
			requireHanteiRefusal(t, w, "Only the tournament organizer can record a judges' decision (hantei).")
			assert.False(t, f.storedRepBout(t).HanteiDecided(), "nothing is written")

			w = f.score("main-pw", state.MatchStatusRunning, "", f.now+200, tc.repBout)
			require.Equal(t, http.StatusOK, w.Code, "the organiser records it: %s", w.Body.String())
			assert.True(t, f.storedRepBout(t).HanteiDecided())
		})
	}
}

// hanteiRow is the representative bout the judges decided for TeamA, as the
// team editor writes it: the organiser's, or a participant's editor sending
// back the verdict it took up.
func hanteiRow() map[string]any {
	return repBoutRow([]string{domain.HanteiMark}, []string{}, "TeamA")
}

// recordHantei has the organiser record the judges' decision for TeamA while
// the match is running, as the operator console saves it.
func (f *repBoutFixture) recordHantei(t *testing.T) {
	t.Helper()
	w := f.score("main-pw", state.MatchStatusRunning, "", f.now+100, hanteiRow())
	require.Equal(t, http.StatusOK, w.Code, "the organiser records the hantei: %s", w.Body.String())
	require.True(t, f.storedRepBout(t).HanteiDecided())
}

// requireHanteiRefusal checks a participant's write was refused the way a
// finished match's is (409), with the sentence the public score sheet shows.
func requireHanteiRefusal(t *testing.T, w *httptest.ResponseRecorder, message string) {
	t.Helper()
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	assert.Equal(t, "hantei_organiser_only", body["error"])
	assert.Equal(t, message, body["message"])
}

// A participant cannot clear, move or score over the organiser's verdict, by
// any shape of write the score route accepts (operator decision, bc-dhas).
func TestSelfRun_ParticipantsCannotChangeTheOrganisersHantei(t *testing.T) {
	swapped := hanteiRow()
	swapped["sideA"], swapped["sideB"], swapped["winner"] = "TeamB", "TeamA", "TeamB"
	legacyMove := repBoutRow([]string{}, []string{}, "TeamB")
	legacyMove["decidedByHantei"] = true
	cases := []struct {
		name    string
		repBout map[string]any // nil drops the row from the list
	}{
		{"clearing it", repBoutRow([]string{}, []string{}, "")},
		{"moving it to the other side", repBoutRow([]string{}, []string{domain.HanteiMark}, "TeamB")},
		{"handing it over by swapping the side names", swapped},
		{"moving it with the legacy decidedByHantei flag", legacyMove},
		{"scoring over it", repBoutRow([]string{}, []string{"M"}, "TeamB")},
		{"dropping the row", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)
			f.recordHantei(t)

			w := f.score("", state.MatchStatusRunning, "", f.now+200, tc.repBout)
			requireHanteiRefusal(t, w, "The judges decided this representative bout (hantei). Ask the tournament organizer to change it.")
			row := f.storedRepBout(t)
			assert.Equal(t, []string{domain.HanteiMark}, row.IpponsA, "the verdict stands")
			assert.Equal(t, "TeamA", row.Winner)
		})
	}
}

// A participant's editor that took up the organiser's verdict keeps saving:
// sending it back, leaving the row's points out, or sending no bouts at all
// all keep it, and the match finishes on it.
func TestSelfRun_ParticipantsKeepTheOrganisersHantei(t *testing.T) {
	legacyEcho := repBoutRow([]string{}, []string{}, "TeamA")
	legacyEcho["decidedByHantei"] = true
	silent := repBoutRow(nil, nil, "")
	delete(silent, "ipponsA")
	delete(silent, "ipponsB")
	cases := []struct {
		name    string
		repBout map[string]any
	}{
		{"sending it back as it is", hanteiRow()},
		{"sending it back with the legacy decidedByHantei flag", legacyEcho},
		{"leaving the row's points out", silent},
	}
	requireKept := func(t *testing.T, f *repBoutFixture, w *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.NotContains(t, w.Body.String(), `"applied":false`)
		row := f.storedRepBout(t)
		assert.Equal(t, []string{domain.HanteiMark}, row.IpponsA, "the verdict stands")
		assert.Equal(t, "TeamA", row.Winner)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)
			f.recordHantei(t)
			requireKept(t, f, f.score("", state.MatchStatusRunning, "", f.now+200, tc.repBout))
		})
	}

	t.Run("sending no bouts at all", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		f.recordHantei(t)
		requireKept(t, f, f.send(http.MethodPut, repBoutMatchPath+"/score", "", map[string]any{
			"sideA": "TeamA", "sideB": "TeamB", "status": state.MatchStatusRunning, "modifiedAt": f.now + 200,
		}))
	})

	t.Run("finishing the match on it", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		f.recordHantei(t)
		requireKept(t, f, f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, hanteiRow()))
		bm := storedB1(t, f.store, "c1")
		assert.Equal(t, state.MatchStatusCompleted, bm.Status)
		assert.Equal(t, "TeamA", bm.Winner)
		assert.Equal(t, "self-reported", bm.ResultSource)
	})
}

// The organiser, with the password, changes their own verdict as before.
func TestSelfRun_TheOrganiserChangesTheirHantei(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	f.recordHantei(t)

	w := f.score("main-pw", state.MatchStatusRunning, "", f.now+200, repBoutRow([]string{}, []string{}, ""))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, f.storedRepBout(t).HanteiDecided(), "the organiser cleared it")
}

// Adding a second representative bout used to pass the add's tie check (which
// counts none) and put a finished match back to running beside the verdict
// recorded on the first. A participant can reach that route, so it matters
// most here.
func TestSelfRun_ASecondRepresentativeBoutCannotReopenTheMatch(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	f.recordHantei(t)
	w := f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, hanteiRow())
	require.Equal(t, http.StatusOK, w.Code, "finishing on the verdict: %s", w.Body.String())

	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 300})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"daihyosen_exists"`)
	bm := storedB1(t, f.store, "c1")
	assert.Equal(t, state.MatchStatusCompleted, bm.Status, "the match stays finished")
	assert.Equal(t, "TeamA", bm.Winner)
	rows := 0
	for _, s := range bm.SubResults {
		if s.Position == state.DaihyosenSubPosition {
			rows++
		}
	}
	assert.Equal(t, 1, rows, "the representative bout stays the only one")
}

func TestOfficiated_RepresentativeBoutNeedsThePassword(t *testing.T) {
	f := newRepBoutFixture(t, false)

	w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now})
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	w = f.score("", state.MatchStatusRunning, "", f.now+100, repBoutRow([]string{"M"}, []string{}, "TeamA"))
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.False(t, carriesDaihyosenRow(storedB1(t, f.store, "c1").SubResults), "nothing is written")
}

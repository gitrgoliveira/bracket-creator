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
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	repBoutMatchPath = "/api/competitions/c1/matches/B1"
	repBoutTeamAID   = "11111111-1111-4111-1111-111111111111"
	repBoutTeamBID   = "22222222-2222-4222-2222-222222222222"
)

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
	teamA := domain.Player{ID: repBoutTeamAID, Name: "TeamA", Dojo: "A"}
	teamB := domain.Player{ID: repBoutTeamBID, Name: "TeamB", Dojo: "B"}
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
	return f.send(http.MethodPut, repBoutMatchPath+"/score", password, scoreSheet(status, winner, at, repBout))
}

// scoreSheet is the body score sends, for a test that adds to it.
func scoreSheet(status state.MatchStatus, winner string, at int64, repBout map[string]any) map[string]any {
	subs := []any{
		map[string]any{"position": 1, "sideA": "", "sideB": "", "ipponsA": []string{"M"}, "ipponsB": []string{}, "winner": "TeamA", "decision": ""},
		map[string]any{"position": 2, "sideA": "", "sideB": "", "ipponsA": []string{}, "ipponsB": []string{"K"}, "winner": "TeamB", "decision": ""},
		map[string]any{"position": 3, "sideA": "", "sideB": "", "ipponsA": []string{}, "ipponsB": []string{}, "winner": "", "decision": "hikiwake"},
	}
	if repBout != nil {
		subs = append(subs, repBout)
	}
	return map[string]any{
		"sideA": "TeamA", "sideB": "TeamB", "status": status, "winner": winner,
		"ipponsA": []string{}, "ipponsB": []string{}, "modifiedAt": at,
		"subResults": subs,
	}
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
	i := state.DaihyosenSubIndex(subs)
	require.GreaterOrEqual(t, i, 0, "B1 carries no representative bout")
	return &subs[i]
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
			requireHanteiRefusal(t, w, false)
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
// finished match's is (409), with the sentence the public score sheet shows:
// the shared table's recorded one when the organiser recorded a decision.
func requireHanteiRefusal(t *testing.T, w *httptest.ResponseRecorder, recorded bool) {
	t.Helper()
	table := loadRepBoutHanteiTable(t)
	message := table.NotRecorded
	if recorded {
		message = table.Recorded
	}
	requireRefusal(t, w, http.StatusConflict, "hantei_organiser_only", message)
}

// repBoutHanteiTable is what a participant is told about the judges' decision
// on a representative bout, the table the Go and JS halves share
// (testdata/rep_bout_hantei_messages.json).
type repBoutHanteiTable struct {
	Recorded    string `json:"recorded"`
	NotRecorded string `json:"notRecorded"`
}

func loadRepBoutHanteiTable(t *testing.T) repBoutHanteiTable {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "rep_bout_hantei_messages.json"))
	require.NoError(t, err)
	var table repBoutHanteiTable
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Recorded)
	require.NotEmpty(t, table.NotRecorded)
	return table
}

// requireRefusal checks a participant's write was refused with this status and
// code, and with the sentence the public page shows as it is.
func requireRefusal(t *testing.T, w *httptest.ResponseRecorder, status int, code, message string) {
	t.Helper()
	require.Equal(t, status, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	assert.Equal(t, code, body["error"])
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
			requireHanteiRefusal(t, w, true)
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
// recorded on the first. A participant's add no longer reaches a finished
// match at all (TestSelfRun_ParticipantsAddARepresentativeBoutOnlyToARunningMatch);
// the organiser's does, and this is what stops it.
func TestSelfRun_ASecondRepresentativeBoutCannotReopenTheMatch(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	f.recordHantei(t)
	w := f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, hanteiRow())
	require.Equal(t, http.StatusOK, w.Code, "finishing on the verdict: %s", w.Body.String())

	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now + 300})
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

const resultFinalizedSentence = "This match result has already been reported. Contact the tournament organizer to correct it."

// setB1 rewrites the stored B1, for a match the fixture does not start with.
func (f *repBoutFixture) setB1(t *testing.T, edit func(bm *state.BracketMatch)) {
	t.Helper()
	b, err := f.store.LoadBracket("c1")
	require.NoError(t, err)
	edit(&b.Rounds[0][0])
	require.NoError(t, f.store.SaveBracket("c1", b))
}

// Two fouls on a row that leaves its ippons out fold into an ippon for the
// other side before the write is stored, so the row is not silent: it would
// replace the organiser's verdict with a scoreline that has none.
func TestSelfRun_FoulsOnARowWithoutIpponsCannotEraseTheHantei(t *testing.T) {
	for _, fouls := range []string{"hansokuA", "hansokuB"} {
		t.Run(fouls, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)
			f.recordHantei(t)
			recorded := *f.storedRepBout(t)

			row := repBoutRow(nil, nil, "")
			delete(row, "ipponsA")
			delete(row, "ipponsB")
			row[fouls] = 2
			w := f.score("", state.MatchStatusRunning, "", f.now+200, row)
			requireHanteiRefusal(t, w, true)
			assert.Equal(t, recorded, *f.storedRepBout(t), "the decided bout is as the organiser recorded it")
		})
	}
}

// A participant's write that keeps the organiser's verdict is stored with the
// decided bout exactly as recorded, whatever it sent for that bout: a new
// scoreline around the same verdict, fouls, overtime, or no sub-decision on a
// row that leaves its ippons out (the only other one a participant may send
// there, IsSelfRunReportableSubDecision).
func TestSelfRun_TheDecidedRepresentativeBoutIsStoredAsRecorded(t *testing.T) {
	rescored := repBoutRow([]string{"M", domain.HanteiMark}, []string{"K"}, "TeamA")
	rescored["hansokuB"] = 1
	rescored["encho"] = map[string]any{"periodCount": 2}
	redecided := repBoutRow(nil, nil, "")
	delete(redecided, "ipponsA")
	delete(redecided, "ipponsB")
	redecided["decision"] = ""
	cases := []struct {
		name    string
		repBout map[string]any
	}{
		{"a new scoreline, fouls and overtime around the same verdict", rescored},
		{"another sub-decision on a row without ippons", redecided},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)
			f.recordHantei(t)
			recorded := *f.storedRepBout(t)

			w := f.score("", state.MatchStatusRunning, "", f.now+200, tc.repBout)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.NotContains(t, w.Body.String(), `"applied":false`)
			assert.Equal(t, recorded, *f.storedRepBout(t), "the decided bout is as the organiser recorded it")
		})
	}
}

// A participant finishes the match on the organiser's judges' decision, never
// against it: a winner the write names, by name or by id, must be the side
// the decided bout names, even when the write sends that bout back as it is.
func TestSelfRun_TheMatchFinishesOnlyOnTheDecidedSide(t *testing.T) {
	// finish sends the finishing write and reports B1 as it stood before it.
	finish := func(t *testing.T, winner, winnerID string) (*repBoutFixture, state.BracketMatch, *httptest.ResponseRecorder) {
		t.Helper()
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		f.recordHantei(t)
		before := storedB1(t, f.store, "c1")
		body := scoreSheet(state.MatchStatusCompleted, winner, f.now+200, hanteiRow())
		body["winnerId"] = winnerID
		return f, before, f.send(http.MethodPut, repBoutMatchPath+"/score", "", body)
	}
	cases := []struct {
		name, winner, winnerID string
	}{
		{"naming the other team", "TeamB", ""},
		{"naming the other team by name and id", "TeamB", repBoutTeamBID},
		{"naming the other team by id alone", "", repBoutTeamBID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, before, w := finish(t, tc.winner, tc.winnerID)
			requireHanteiRefusal(t, w, true)
			assert.Equal(t, before, storedB1(t, f.store, "c1"), "nothing is written")
		})
	}

	// A name and an id that disagree never reach the judge: the request's
	// own validation refuses them first.
	t.Run("naming the decided team with the other team's id", func(t *testing.T) {
		f, before, w := finish(t, "TeamA", repBoutTeamBID)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Equal(t, before, storedB1(t, f.store, "c1"), "nothing is written")
	})
}

// A start keeps the stored bouts whatever it sends (engine.keepQueuedScore),
// so the match winner a start derives must come from the bouts it stores. A
// crafted start whose own representative bout names the other team used to
// make that team the winner beside the organiser's decision for TeamA.
func TestSelfRun_AStartDerivesItsWinnerFromTheBoutsItStores(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	f.recordHantei(t)
	recorded := *f.storedRepBout(t)

	body := scoreSheet(state.MatchStatusRunning, "", f.now+200, repBoutRow([]string{}, []string{"M"}, "TeamB"))
	body["startOnly"] = true
	w := f.send(http.MethodPut, repBoutMatchPath+"/score", "", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bm := storedB1(t, f.store, "c1")
	assert.Equal(t, "TeamA", bm.Winner, "the winner is the side the stored representative bout names")
	assert.Equal(t, repBoutTeamAID, bm.WinnerID)
	assert.Equal(t, recorded, *f.storedRepBout(t), "the decided bout is as the organiser recorded it")
}

// The score route and the representative bout's add answer a wrong password
// as the team writes do (TestSelfRun_ScoreSheetTeamWrites_AWrongPasswordIsRefused),
// while an empty header is still a participant.
func TestSelfRun_AWrongPasswordIsRefusedOnTheScoreAndTheRepresentativeBout(t *testing.T) {
	t.Run("the score", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		before := storedB1(t, f.store, "c1")
		requireInvalidPassword(t, f.score("stale-pw", state.MatchStatusCompleted, "TeamB", f.now+100, nil))
		assert.Equal(t, before, storedB1(t, f.store, "c1"), "nothing is written")

		w := f.score("", state.MatchStatusRunning, "", f.now+200, nil)
		require.Equal(t, http.StatusOK, w.Code, "an empty header is still a participant: %s", w.Body.String())
		assert.Equal(t, "self-reported", storedB1(t, f.store, "c1").ResultSource)
	})

	t.Run("adding a representative bout", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		requireInvalidPassword(t, f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "stale-pw", map[string]any{"modifiedAt": f.now + 100}))
		assert.False(t, carriesDaihyosenRow(storedB1(t, f.store, "c1").SubResults), "nothing is written")
		f.addRepBout(t) // an empty header, as a participant
	})
}

// A participant scores the representative bout the match already has. Only
// the add route creates one, with its tie, pool, kachinuki, engi and
// eligibility checks. A write can still carry one the match does not have: a
// sheet a moment behind a remove made on another device, or a queued write
// replayed after it. It inherited that row rather than introduced it, so the
// row is dropped and the rest of the write is judged and written as usual,
// the edit it carries included. An encounter has one representative bout, so
// a write listing two is refused.
func TestSelfRun_AScoreWriteNeverCreatesARepresentativeBout(t *testing.T) {
	won := repBoutRow([]string{"M"}, []string{}, "TeamA")
	// sheetWithEdit is the sheet with bout 3 rescored: the edit the write
	// carries beside the row it inherited.
	sheetWithEdit := func(at int64) map[string]any {
		body := scoreSheet(state.MatchStatusRunning, "", at, won)
		body["subResults"].([]any)[2] = map[string]any{
			"position": 3, "sideA": "", "sideB": "", "ipponsA": []string{"K"}, "ipponsB": []string{}, "winner": "TeamA", "decision": "",
		}
		return body
	}
	requireRowDropped := func(t *testing.T, f *repBoutFixture, w *httptest.ResponseRecorder) {
		t.Helper()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.NotContains(t, w.Body.String(), `"applied":false`)
		subs := storedB1(t, f.store, "c1").SubResults
		assert.False(t, carriesDaihyosenRow(subs), "the representative bout is not written")
		edited := false
		for _, s := range subs {
			if s.Position == 3 {
				edited = true
				assert.Equal(t, []string{"K"}, s.IpponsA, "the edit beside it is written")
			}
		}
		assert.True(t, edited, "bout 3 is stored")
	}

	t.Run("on a knockout match without one", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		requireRowDropped(t, f, f.send(http.MethodPut, repBoutMatchPath+"/score", "", sheetWithEdit(f.now+100)))
	})

	t.Run("on a kachinuki match, which has none", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		require.NoError(t, f.store.SaveCompetition(&state.Competition{
			ID: "c1", Name: "Teams", Kind: "team", Format: state.CompFormatKnockout,
			TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		requireRowDropped(t, f, f.send(http.MethodPut, repBoutMatchPath+"/score", "", sheetWithEdit(f.now+100)))
	})

	t.Run("two in one write", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		body := scoreSheet(state.MatchStatusRunning, "", f.now+100, won)
		body["subResults"] = append(body["subResults"].([]any), repBoutRow([]string{}, []string{"M"}, "TeamB"))
		w := f.send(http.MethodPut, repBoutMatchPath+"/score", "", body)
		requireRefusal(t, w, http.StatusBadRequest, "duplicate_daihyosen",
			"A team match has one representative bout, and this score lists more than one. Check the scores and try again.")
		rows := 0
		for _, s := range storedB1(t, f.store, "c1").SubResults {
			if s.Position == state.DaihyosenSubPosition {
				rows++
				assert.Empty(t, s.Winner, "nothing is written")
			}
		}
		assert.Equal(t, 1, rows)
	})
}

// A finish cannot rest on a representative bout the match no longer has. A
// sheet a moment behind a remove made on another device scores the stale row
// and finishes on the winner it names; dropping the row and keeping the finish
// used to store the tied encounter as completed for that winner, with no
// representative bout behind it, for the bracket to advance. The finish is
// refused and nothing is stored; a running write still only loses the row
// (TestSelfRun_AScoreWriteNeverCreatesARepresentativeBout).
func TestSelfRun_AFinishOnARemovedRepresentativeBoutIsRefused(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	w := f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 100})
	require.Equal(t, http.StatusOK, w.Code, "another device removes it: %s", w.Body.String())
	before := storedB1(t, f.store, "c1")

	w = f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, repBoutRow([]string{"M"}, []string{}, "TeamA"))
	requireRefusal(t, w, http.StatusConflict, "no_daihyosen",
		"This match's representative bout was removed on another device. Check the scores and finish again.")
	assert.Equal(t, before, storedB1(t, f.store, "c1"), "nothing is stored: the match is still running, with no winner")
}

// A participant adds the representative bout of the match being fought. On a
// finished match the add would put it back to running with the organiser's
// result still on it, for the next score write to overturn; on one not started
// it would start it past the court and eligibility checks a start runs.
func TestSelfRun_ParticipantsAddARepresentativeBoutOnlyToARunningMatch(t *testing.T) {
	t.Run("a finished match", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		w := f.score("main-pw", state.MatchStatusCompleted, "TeamA", f.now+100, nil)
		require.Equal(t, http.StatusOK, w.Code, "the organiser finishes it: %s", w.Body.String())

		w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 200})
		requireRefusal(t, w, http.StatusConflict, "result_finalized", resultFinalizedSentence)
		bm := storedB1(t, f.store, "c1")
		assert.Equal(t, state.MatchStatusCompleted, bm.Status, "the match stays finished")
		assert.Equal(t, "TeamA", bm.Winner)
		assert.False(t, carriesDaihyosenRow(bm.SubResults), "nothing is written")

		// The organiser's add is not the participant's: it may be their
		// correction, since a fixed-order knockout match has no reopen.
		w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now + 300})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("a match not started yet", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.setB1(t, func(bm *state.BracketMatch) {
			bm.Status = state.MatchStatusScheduled
			bm.SubResults = nil
		})
		w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 100})
		requireRefusal(t, w, http.StatusConflict, "match_not_running",
			"This match has not started. Start it before adding a representative bout.")
		bm := storedB1(t, f.store, "c1")
		assert.Equal(t, state.MatchStatusScheduled, bm.Status, "the match is not started")
		assert.False(t, carriesDaihyosenRow(bm.SubResults), "nothing is written")
	})
}

// A participant removes a representative bout added by mistake from the match
// being fought. On a finished match the remove would put it back to running
// and clear the organiser's result, a withdrawal's bar with it.
func TestSelfRun_ParticipantsRemoveARepresentativeBoutOnlyFromARunningMatch(t *testing.T) {
	t.Run("a finished match", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		// TeamB cannot field a representative, so the organiser records its
		// withdrawal; the unfought representative bout stays on the match.
		w := f.send(http.MethodPost, repBoutMatchPath+"/decision", "main-pw", map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": f.now + 100,
		})
		require.Equal(t, http.StatusOK, w.Code, "the organiser records the withdrawal: %s", w.Body.String())
		bm := storedB1(t, f.store, "c1")
		require.Equal(t, state.MatchStatusCompleted, bm.Status)
		require.True(t, carriesDaihyosenRow(bm.SubResults))

		w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 200})
		requireRefusal(t, w, http.StatusConflict, "result_finalized", resultFinalizedSentence)
		bm = storedB1(t, f.store, "c1")
		assert.Equal(t, state.MatchStatusCompleted, bm.Status, "the match stays finished")
		assert.Equal(t, "TeamA", bm.Winner)
		assert.Equal(t, "kiken-voluntary", bm.Decision)
		assert.True(t, carriesDaihyosenRow(bm.SubResults), "nothing is written")
		statuses, err := f.store.LoadCompetitorStatus("c1")
		require.NoError(t, err)
		require.Contains(t, statuses, repBoutTeamBID)
		assert.False(t, statuses[repBoutTeamBID].Eligible, "the withdrawal still bars TeamB")
	})

	t.Run("a match not started yet", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		f.setB1(t, func(bm *state.BracketMatch) {
			bm.Status = state.MatchStatusScheduled
			bm.SubResults = append(bm.SubResults, state.SubMatchResult{
				Position: state.DaihyosenSubPosition, Decision: string(domain.DecisionDaihyosen),
			})
		})
		w := f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 100})
		requireRefusal(t, w, http.StatusConflict, "match_not_running",
			"This match has not started. Start it before removing its representative bout.")
		bm := storedB1(t, f.store, "c1")
		assert.Equal(t, state.MatchStatusScheduled, bm.Status, "the match is not started")
		assert.True(t, carriesDaihyosenRow(bm.SubResults), "nothing is written")
	})
}

// A representative bout breaks a tie between teams. An individual match has
// no bouts, so the add's tie check read it as tied at nothing each and
// appended one; every caller is refused now, the organiser included.
func TestRepresentativeBoutIsForTeamCompetitionsOnly(t *testing.T) {
	for _, caller := range []struct{ name, password string }{
		{"the organiser", "main-pw"},
		{"a participant", ""},
	} {
		t.Run(caller.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			require.NoError(t, f.store.SaveCompetition(&state.Competition{
				ID: "c1", Name: "Singles", Kind: "individual", Format: state.CompFormatKnockout,
			}))
			f.setB1(t, func(bm *state.BracketMatch) { bm.SubResults = nil })

			w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", caller.password, map[string]any{"modifiedAt": f.now + 100})
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
			assert.Equal(t, "individual competitions do not support daihyosen; a representative bout breaks a tie between teams", body["error"])
			bm := storedB1(t, f.store, "c1")
			assert.Equal(t, state.MatchStatusRunning, bm.Status)
			assert.False(t, carriesDaihyosenRow(bm.SubResults), "nothing is written")
		})
	}
}

// TestHoldSelfReportedWriteUnderTx pins the judge on its own, against a stored
// match: a participant may send the organiser's verdict back or say nothing
// about it, never record, move or clear one, and the decided bout they send
// back is stored as the organiser recorded it (operator decision, bc-dhas).
// The handler tests above pin the same rules through the score route.
func TestHoldSelfReportedWriteUnderTx(t *testing.T) {
	row := func(winner string, ipponsA, ipponsB []string) state.SubMatchResult {
		return state.SubMatchResult{
			Position: state.DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB",
			IpponsA: ipponsA, IpponsB: ipponsB, Winner: winner, Decision: "daihyosen",
		}
	}
	bout := state.SubMatchResult{Position: 1, IpponsA: []string{"M"}, Winner: "TeamA"}
	decided := row("TeamA", []string{domain.HanteiMark}, []string{})
	unscored := row("", []string{}, []string{})
	scoredRow := row("TeamA", []string{"M"}, []string{})
	silent := row("", nil, nil)
	fouled := silent
	fouled.HansokuB = 2
	swapped := row("TeamB", []string{domain.HanteiMark}, []string{})
	swapped.SideA, swapped.SideB = "TeamB", "TeamA"
	recorded, notRecorded := repBoutHanteiRefusal(true), repBoutHanteiRefusal(false)

	tests := []struct {
		name              string
		stored            *state.SubMatchResult
		incoming          []state.SubMatchResult
		winner, winnerID  string
		status            state.MatchStatus
		startOnly         bool
		want              *selfRunRefusal
		wantStoredRepBout bool // the write's row is replaced by the stored one
		wantRepBoutGone   bool // the write's row is dropped, the rest kept as sent
		wantRepBoutKept   bool // the stored row is appended to the rows sent
	}{
		{name: "scoring with no verdict anywhere", stored: &unscored, incoming: []state.SubMatchResult{bout, row("TeamA", []string{"M"}, []string{})}},
		{name: "recording a verdict", stored: &unscored, incoming: []state.SubMatchResult{bout, decided}, want: notRecorded},
		{name: "scoring a representative bout the match does not have", incoming: []state.SubMatchResult{bout, decided}, wantRepBoutGone: true},
		{name: "finishing on a representative bout the match does not have", incoming: []state.SubMatchResult{bout, row("TeamA", []string{"M"}, []string{})}, winner: "TeamA", status: state.MatchStatusCompleted, want: errRepBoutRemoved},
		{name: "listing the bouts without the scored representative bout keeps it", stored: &scoredRow, incoming: []state.SubMatchResult{bout}, wantRepBoutKept: true},
		{name: "an empty list keeps an unscored one", stored: &unscored, incoming: []state.SubMatchResult{}, wantRepBoutKept: true},
		{name: "finishing without the representative bout the match has", stored: &scoredRow, incoming: []state.SubMatchResult{bout}, winner: "TeamA", status: state.MatchStatusCompleted, want: errRepBoutAdded},
		{name: "repeating the recorded verdict", stored: &decided, incoming: []state.SubMatchResult{bout, decided}, wantStoredRepBout: true},
		{name: "moving the verdict to the other side", stored: &decided, incoming: []state.SubMatchResult{bout, row("TeamB", []string{}, []string{domain.HanteiMark})}, want: recorded},
		{name: "handing the verdict over by swapping the side names", stored: &decided, incoming: []state.SubMatchResult{bout, swapped}, want: recorded},
		{name: "clearing the verdict", stored: &decided, incoming: []state.SubMatchResult{bout, unscored}, want: recorded},
		{name: "scoring over the verdict", stored: &decided, incoming: []state.SubMatchResult{bout, row("TeamB", []string{}, []string{"M"})}, want: recorded},
		{name: "leaving the row's points out", stored: &decided, incoming: []state.SubMatchResult{bout, silent}, wantStoredRepBout: true},
		{name: "fouls that fold into a point on a row without points", stored: &decided, incoming: []state.SubMatchResult{bout, fouled}, want: recorded},
		{name: "dropping the row", stored: &decided, incoming: []state.SubMatchResult{bout}, want: recorded},
		{name: "an empty list drops it too", stored: &decided, incoming: []state.SubMatchResult{}, want: recorded},
		{name: "sending no sub-results at all", stored: &decided},
		{name: "a second row behind an echo", stored: &decided, incoming: []state.SubMatchResult{bout, decided, unscored}, want: errDuplicateRepBout},
		{name: "finishing on the decided side", stored: &decided, incoming: []state.SubMatchResult{bout, decided}, winner: "TeamA", winnerID: repBoutTeamAID, wantStoredRepBout: true},
		{name: "finishing on the other side", stored: &decided, incoming: []state.SubMatchResult{bout, decided}, winner: "TeamB", want: recorded},
		{name: "finishing on the other side's id", stored: &decided, winnerID: repBoutTeamBID, want: recorded},
		{name: "finishing on a name neither side has", stored: &decided, winner: "TeamC", want: recorded},
		{name: "a start judges no bouts", stored: &decided, incoming: []state.SubMatchResult{bout}, startOnly: true},
		{name: "a start still judges its winner", stored: &decided, winner: "TeamB", startOnly: true, want: recorded},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newTempStore(t)
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID: "c1", Name: "Teams", Kind: "team", Format: state.CompFormatKnockout,
				TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
			}))
			subs := []state.SubMatchResult{bout}
			if tc.stored != nil {
				subs = append(subs, *tc.stored)
			}
			require.NoError(t, store.SaveBracket("c1", &state.Bracket{Rounds: [][]state.BracketMatch{{{
				ID: "B1", SideA: "TeamA", SideB: "TeamB", SideAID: repBoutTeamAID, SideBID: repBoutTeamBID,
				Status: state.MatchStatusRunning, SubResults: subs,
			}}}}))
			sent := state.CloneSubResults(tc.incoming)
			result := &state.MatchResult{SubResults: tc.incoming, Winner: tc.winner, WinnerID: tc.winnerID, Status: tc.status}

			err := store.WithTransaction("c1", func(stx state.StoreTx) error {
				return holdSelfReportedWriteUnderTx(stx, "c1", "B1", result, tc.startOnly)
			})
			if tc.want != nil {
				var refusal *selfRunRefusal
				require.ErrorAs(t, err, &refusal)
				assert.Equal(t, tc.want, refusal)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, sent, tc.incoming, "the request's own rows are never written into")
			switch {
			case tc.wantStoredRepBout:
				assert.Equal(t, *tc.stored, result.SubResults[state.DaihyosenSubIndex(result.SubResults)])
			case tc.wantRepBoutKept:
				assert.Equal(t, append(slices.Clone(sent), *tc.stored), result.SubResults)
			case tc.wantRepBoutGone:
				kept := slices.DeleteFunc(slices.Clone(sent), func(s state.SubMatchResult) bool {
					return s.Position == state.DaihyosenSubPosition
				})
				assert.Equal(t, kept, result.SubResults)
			default:
				assert.Equal(t, sent, result.SubResults)
			}
		})
	}
}

// TestRepBoutHanteiRefusal_SharedTable is the Go half of the shared sentence
// table; the JS half reads the same file (rep_bout_hantei_note.test.jsx).
func TestRepBoutHanteiRefusal_SharedTable(t *testing.T) {
	table := loadRepBoutHanteiTable(t)
	assert.Equal(t, table.Recorded, repBoutHanteiRefusal(true).message)
	assert.Equal(t, table.NotRecorded, repBoutHanteiRefusal(false).message)
}

// A participant's score write never removes a representative bout either:
// only the remove route does, with its unscored check. A sheet a moment behind
// an add made on another device lists the bouts without it; a running write
// keeps the stored row and the rest of the write, and a finish is refused,
// since it was decided on a sheet that never saw the row.
func TestSelfRun_AScoreWriteNeverRemovesARepresentativeBout(t *testing.T) {
	scored := func(t *testing.T) *repBoutFixture {
		f := newRepBoutFixture(t, true)
		f.addRepBout(t)
		w := f.score("", state.MatchStatusRunning, "", f.now+100, repBoutRow([]string{"M"}, []string{}, "TeamA"))
		require.Equal(t, http.StatusOK, w.Code, "another device scores it: %s", w.Body.String())
		return f
	}

	t.Run("a running write keeps it", func(t *testing.T) {
		f := scored(t)
		body := scoreSheet(state.MatchStatusRunning, "", f.now+200, nil)
		body["subResults"].([]any)[2] = map[string]any{
			"position": 3, "sideA": "", "sideB": "", "ipponsA": []string{"K"}, "ipponsB": []string{}, "winner": "TeamA", "decision": "",
		}
		w := f.send(http.MethodPut, repBoutMatchPath+"/score", "", body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.NotContains(t, w.Body.String(), `"applied":false`)
		assert.Equal(t, []string{"M"}, f.storedRepBout(t).IpponsA, "the scored representative bout is kept")
		for _, s := range storedB1(t, f.store, "c1").SubResults {
			if s.Position == 3 {
				assert.Equal(t, []string{"K"}, s.IpponsA, "the edit beside it is written")
			}
		}
	})

	t.Run("a finish is refused", func(t *testing.T) {
		f := scored(t)
		before := storedB1(t, f.store, "c1")
		w := f.score("", state.MatchStatusCompleted, "TeamA", f.now+200, nil)
		requireRefusal(t, w, http.StatusConflict, "daihyosen_added",
			"A representative bout was added to this match on another device. Check the scores and finish again.")
		assert.Equal(t, before, storedB1(t, f.store, "c1"), "nothing is stored")
	})

	t.Run("the organiser's write is not judged", func(t *testing.T) {
		f := scored(t)
		w := f.score("main-pw", state.MatchStatusRunning, "", f.now+200, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.False(t, carriesDaihyosenRow(storedB1(t, f.store, "c1").SubResults))
	})
}

// The representative bout is sudden death: a participant scores it, and never
// records a draw or a default win on it (those decide the encounter through
// deriveDaihyosenWinner). The organiser's write is unchanged.
func TestSelfRun_RepresentativeBoutTakesOnlyItsOwnDecision(t *testing.T) {
	for _, decision := range []string{"fusensho", "hikiwake"} {
		t.Run(decision, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.addRepBout(t)
			row := repBoutRow([]string{"○", "○"}, []string{}, "TeamA")
			row["decision"] = decision
			w := f.score("", state.MatchStatusRunning, "", f.now+100, row)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Empty(t, f.storedRepBout(t).Winner, "nothing is written")

			w = f.score("main-pw", state.MatchStatusRunning, "", f.now+200, row)
			require.Equal(t, http.StatusOK, w.Code, "the organiser's write: %s", w.Body.String())
		})
	}
}

// requireNoAuditFields checks a 200 body's result carries none of the
// operator-only fields the public broadcast strips.
func requireNoAuditFields(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var body map[string]map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	require.NotNil(t, body["result"], w.Body.String())
	for _, key := range []string{"decisionReason", "correctionReason", "rev", "revSession"} {
		assert.NotContains(t, body["result"], key)
	}
	return body["result"]
}

// Both representative-bout routes are public in self-run, so their answer
// carries the match as the public broadcast does, without the audit notes.
func TestDaihyosenResponses_CarryNoAuditFields(t *testing.T) {
	for _, password := range []string{"", "main-pw"} {
		t.Run("add, password "+password, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			f.setB1(t, func(bm *state.BracketMatch) { bm.DecisionReason = "private note" })
			result := requireNoAuditFields(t, f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", password, map[string]any{"modifiedAt": f.now}))
			assert.Contains(t, result, "teamResult", "the match keeps the shape the editor adopts")
		})
	}
	t.Run("remove", func(t *testing.T) {
		f := newRepBoutFixture(t, true)
		require.NoError(t, f.store.SavePoolMatches("c1", []state.MatchResult{{
			ID: "Pool A-1", SideA: "TeamA", SideB: "TeamB", SideAID: repBoutTeamAID, SideBID: repBoutTeamBID,
			Status: state.MatchStatusRunning, CorrectionReason: "private note", DecisionReason: "private note",
			SubResults: []state.SubMatchResult{{Position: state.DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB", Decision: "daihyosen"}},
		}}))
		requireNoAuditFields(t, f.send(http.MethodDelete, "/api/competitions/c1/matches/Pool%20A-1/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now}))
	})
}

// bc-dhrp: a participant's pick of a side's representative names a member of
// THAT side's team, the same rule a lineup places a member by (400
// team_member_not_in_team). The organiser's pick is not judged here.
func TestSelfRun_RepresentativeMembersMustBeOnTheirTeam(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	squads, err := f.store.LoadSquads("c1")
	require.NoError(t, err)
	membersA, membersB := squads[repBoutTeamAID], squads[repBoutTeamBID]
	require.NotEmpty(t, membersA, "team A is seeded with members")
	require.NotEmpty(t, membersB, "team B is seeded with members")

	pick := func(sideAID, sideBID string) map[string]any {
		row := repBoutRow([]string{}, []string{}, "")
		row["sideAMemberId"] = sideAID
		row["sideBMemberId"] = sideBID
		return row
	}

	w := f.score("", state.MatchStatusRunning, "", f.now+100, pick(membersA[0].ID, membersA[0].ID))
	requireRefusal(t, w, http.StatusBadRequest, "team_member_not_in_team", "The representative chosen is not on this team. Pick again from the list.")
	assert.Empty(t, f.storedRepBout(t).SideBMemberID, "a refused pick writes nothing")

	w = f.score("", state.MatchStatusRunning, "", f.now+200, pick(membersA[0].ID, membersB[0].ID))
	require.Equal(t, http.StatusOK, w.Code, "each side's own member is accepted: %s", w.Body.String())
	stored := f.storedRepBout(t)
	assert.Equal(t, membersA[0].ID, stored.SideAMemberID)
	assert.Equal(t, membersB[0].ID, stored.SideBMemberID)

	w = f.score("main-pw", state.MatchStatusRunning, "", f.now+300, pick(membersB[0].ID, membersB[0].ID))
	assert.Equal(t, http.StatusOK, w.Code, "the organiser's pick is not judged on membership: %s", w.Body.String())
}

// A password sent but wrong is answered 401 before the body is read, so a
// stale organiser is not told about their clock or their body instead.
func TestDaihyosen_AWrongPasswordIsRefusedBeforeTheStamp(t *testing.T) {
	farFuture := map[string]any{"modifiedAt": time.Now().Add(time.Hour).UnixMilli()}
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method+" with a stamp far ahead", func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			requireInvalidPassword(t, f.send(method, repBoutMatchPath+"/daihyosen", "stale-pw", farFuture))
			w := f.send(method, repBoutMatchPath+"/daihyosen", "", farFuture)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"clock_skew"`, "a participant's stamp is still judged")
		})
		t.Run(method+" with a malformed body", func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			req := httptest.NewRequest(method, repBoutMatchPath+"/daihyosen", strings.NewReader("{not json"))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Tournament-Password", "stale-pw")
			w := httptest.NewRecorder()
			f.r.ServeHTTP(w, req)
			requireInvalidPassword(t, w)
		})
	}
}

package mobileapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const seMatch = "Pool A-0"

// seServer is mergeServer's league (Alice and Bob in a running Pool A-0, Pool
// A-1 scheduled), with its hub and data folder.
func seServer(t *testing.T, compID string) (http.Handler, *state.Store, *Hub, string) {
	t.Helper()
	r, store, _, hub, dir := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: mgAliceID, Name: mgAlice, Dojo: "D1"}, {ID: mgBobID, Name: mgBob, Dojo: "D2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: seMatch, SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID, Court: "A", Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: mgBob, SideAID: mgBobID, SideB: mgAlice, SideBID: mgAliceID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	return r, store, hub, dir
}

type seAnswer struct {
	Match    state.MatchResult   `json:"match"`
	Outcomes []scoreEventOutcome `json:"outcomes"`
	Gaps     []int64             `json:"gaps"`
}

func sePath(compID, matchID string) string {
	return "/api/competitions/" + compID + "/matches/" + matchID + "/score-events"
}

func sePost(t *testing.T, r http.Handler, compID, matchID string, events ...map[string]any) seAnswer {
	t.Helper()
	w := serveJSON(r, http.MethodPost, sePath(compID, matchID), map[string]any{"session": "evs", "events": events})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out seAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func seTap(id string, seq, stamp int64, side, letter string) map[string]any {
	return map[string]any{"id": id, "seq": seq, "stamp": stamp, "kind": "ippon.add", "side": side, "letter": letter}
}

func TestScoreEventsDoor_MixedBatchAnswersEveryEvent(t *testing.T) {
	const compID = "se-mixed"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()

	w := serveJSON(r, http.MethodPost, sePath(compID, seMatch), map[string]any{"session": "evs", "events": []map[string]any{
		seTap("e1", 1, now-3000, "a", "M"),
		seTap("e2", 2, now+60_000, "a", "K"),
		seTap("e3", 3, now-2000, "b", "D"),
		{"id": "e4", "seq": 4, "stamp": now - 1000, "kind": "ippon.remove", "target": "no-such-mark"},
	}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out seAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Outcomes, 4)
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
	assert.Equal(t, domain.ScoreOutcomeClockSkew, out.Outcomes[1].Outcome, "a tap stamped too far ahead is refused alone")
	assert.Positive(t, out.Outcomes[1].ServerNowMs)
	assert.Greater(t, out.Outcomes[1].AheadMs, int64(50_000))
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[2].Outcome)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, out.Outcomes[3].Outcome)
	assert.Equal(t, []string{"M"}, out.Match.IpponsA)
	assert.Equal(t, []string{"D"}, out.Match.IpponsB)
	require.NotNil(t, out.Match.MarkIDs)
	assert.Equal(t, []string{"e1"}, out.Match.MarkIDs.A)
	assert.Equal(t, 4, out.Match.ScoreVersion, "the migration baseline and three events")
	assert.Equal(t, []int64{2}, out.Gaps, "the skewed tap is a gap the device is told about")

	lines, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	for _, l := range lines {
		if l.Event != nil {
			assert.NotEqual(t, "e2", l.Event.ID, "a clock_skew tap is never logged")
		}
	}
}

func TestScoreEventsDoor_AllDuplicateBatchWritesNoBytes(t *testing.T) {
	const compID = "se-dup"
	r, store, _, dir := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-1000, "a", "M"))
	logPath := filepath.Join(dir, "competitions", compID, "events", "Pool%20A-0.jsonl")
	before, err := os.ReadFile(logPath)
	require.NoError(t, err)
	version := store.FileVersion(compID, "pool-matches.csv")

	out := sePost(t, r, compID, seMatch, seTap("e1", 1, now-1000, "a", "M"))
	assert.Equal(t, domain.ScoreOutcomeDuplicate, out.Outcomes[0].Outcome)
	after, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, version, store.FileVersion(compID, "pool-matches.csv"))
}

func TestScoreEventsDoor_RefusesABadBatchWhole(t *testing.T) {
	const compID = "se-bad"
	r, _, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	many := make([]map[string]any, MaxScoreEventsPerBatch+1)
	for i := range many {
		many[i] = seTap(fmt.Sprintf("m%d", i), int64(i+1), now, "a", "M")
	}
	for name, body := range map[string]any{
		"no events":     map[string]any{"session": "evs", "events": []any{}},
		"too many":      map[string]any{"session": "evs", "events": many},
		"no session":    map[string]any{"events": []any{seTap("e1", 1, now, "a", "M")}},
		"unknown kind":  map[string]any{"session": "evs", "events": []any{map[string]any{"id": "x", "stamp": now, "kind": "ippon.steal", "side": "a"}}},
		"other session": map[string]any{"session": "evs", "events": []any{map[string]any{"id": "x", "session": "zzz", "stamp": now, "kind": "foul.add", "side": "a"}}},
	} {
		w := serveJSON(r, http.MethodPost, sePath(compID, seMatch), body)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s: %s", name, w.Body.String())
	}
	w := serveJSON(r, http.MethodPost, sePath(compID, "Pool Z-9"), map[string]any{"session": "evs", "events": []any{seTap("e1", 1, now, "a", "M")}})
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestScoreEventsDoor_NotStartedSaysWhy(t *testing.T) {
	const compID = "se-queue"
	r, _, _, _ := seServer(t, compID)
	out := sePost(t, r, compID, "Pool A-1", seTap("q1", 1, time.Now().UnixMilli(), "a", "M"))
	assert.Equal(t, domain.ScoreOutcomeNotStarted, out.Outcomes[0].Outcome)
	assert.Equal(t, engine.EventReasonNotStarted, out.Outcomes[0].Reason)
	assert.Equal(t, engine.EventMessageNotStarted, out.Outcomes[0].Message)
}

func TestScoreEventsDoor_UnreadableLogIsARefusalNotA5xx(t *testing.T) {
	const compID = "se-unreadable"
	r, _, _, dir := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-1000, "a", "M"))
	logPath := filepath.Join(dir, "competitions", compID, "events", "Pool%20A-0.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte("garbage\n"), 0o600))
	out := sePost(t, r, compID, seMatch, seTap("e2", 2, now, "a", "K"))
	assert.Equal(t, domain.ScoreOutcomeRefused, out.Outcomes[0].Outcome)
	assert.Equal(t, engine.EventReasonLogUnreadable, out.Outcomes[0].Reason)
	assert.NotEmpty(t, out.Outcomes[0].Message)
}

// The SSE match_updated carries the replay's mark ids and version, and no
// mark id is a scoring board's revSession (the public stream strips it).
func TestScoreEventsDoor_BroadcastCarriesMarksNotTheRevSession(t *testing.T) {
	const compID = "se-sse"
	r, _, hub, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScoreAt(t, r, compID, map[string]any{"status": "running", "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 5000, "rev": 1, "revSession": "rev-secret"})
	// Past the coalescer's window for the running write above.
	time.Sleep(300 * time.Millisecond)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-1000, "b", "K"))

	var payload string
	select {
	case msg := <-ch:
		payload = msg.payload
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
	assert.Contains(t, payload, `"match_updated"`)
	assert.Contains(t, payload, `"markIds"`)
	assert.Contains(t, payload, `"scoreVersion"`)
	assert.NotContains(t, payload, "rev-secret")
}

func mergeScoreAt(t *testing.T, r http.Handler, compID string, body map[string]any) map[string]any {
	t.Helper()
	body["sideA"], body["sideB"] = mgAlice, mgBob
	w := serveJSON(r, http.MethodPut, "/api/competitions/"+compID+"/matches/"+seMatch+"/score", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

// F1: a finish resent with its writeId after a later tap (R3) answers as
// recorded, with the tap in it, and adds no history line.
func TestScoreEventsDoor_WriteIDResendAnswersRecorded(t *testing.T) {
	const compID = "se-resend"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	finish := func() map[string]any {
		return map[string]any{"status": "completed", "winner": mgAlice, "ipponsA": []string{"M"}, "ipponsB": []string{},
			"modifiedAt": now - 8000, "writeId": "w-fin", "basis": map[string]any{"seen": 2}}
	}
	mergeScoreAt(t, r, compID, finish())
	out := sePost(t, r, compID, seMatch, seTap("e2", 2, now-7000, "a", "K"))
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, out.Outcomes[0].Outcome, "stamped after the finish")
	history, err := store.LoadMatchHistory(compID, seMatch)
	require.NoError(t, err)

	again := mergeScoreAt(t, r, compID, finish())
	assert.Equal(t, []any{"M"}, again["ipponsA"], "answered with the stored match")
	assert.NotContains(t, again, "applied")
	assert.NotContains(t, again, "heldGroups")
	after, err := store.LoadMatchHistory(compID, seMatch)
	require.NoError(t, err)
	assert.Len(t, after, len(history), "a recorded resend leaves no history line")
}

// S8 b: a /score write carrying the device's event session and basis.seq
// leaves a baseline with that session, so the device's own taps up to seq are
// recorded though the basis's seen and own name none of them; a session longer
// than the batch's limit is a 400.
func TestScoreDoor_SessionCarriesTheBasisSeq(t *testing.T) {
	const compID = "se-session"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"), seTap("e2", 2, now-8500, "b", "K"))

	long := map[string]any{"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"modifiedAt": now - 8200, "session": strings.Repeat("s", domain.MaxScoreEventFieldLen+1),
		"sideA": mgAlice, "sideB": mgBob}
	w := serveJSON(r, http.MethodPut, "/api/competitions/"+compID+"/matches/"+seMatch+"/score", long)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "session")

	mergeScoreAt(t, r, compID, map[string]any{"status": "completed", "winner": mgAlice, "ipponsA": []string{"M"},
		"ipponsB": []string{}, "modifiedAt": now - 8000, "writeId": "w-fin", "session": "evs",
		"basis": map[string]any{"seen": 0, "seq": 1}})
	lines, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	var fin *state.ScoreBaseline
	for _, l := range lines {
		if l.Baseline != nil && l.Baseline.WriteID == "w-fin" {
			fin = l.Baseline
		}
	}
	require.NotNil(t, fin, "the finish left its baseline")
	assert.Equal(t, "evs", fin.Session)
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	rp := engine.ReplayScoring(&ms[0], lines, engine.ReplayShape{Comp: comp})
	assert.Equal(t, domain.ScoreOutcomeRecorded, rp.Outcome("e1"), "seq 1 is within the basis's seq")
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, rp.Outcome("e2"), "seq 2 is past it")
}

// Round-4 finding 7: a tap and a scoring write turned away because the match
// is finished are answered in the same words on both doors, for a tie and for a
// winner-giving point alike, and the answer never claims a newer result exists
// when none does.
func TestMatchClosedIsAnsweredAlikeOnBothDoors(t *testing.T) {
	const compID = "se-closed-both"
	r, _, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScoreAt(t, r, compID, map[string]any{"status": "completed", "winner": mgAlice,
		"ipponsA": []string{"M"}, "ipponsB": []string{}, "modifiedAt": now - 8000})

	// The events door: a tying tap.
	out := sePost(t, r, compID, seMatch, seTap("e1", 1, now-7000, "b", "M"))
	require.Len(t, out.Outcomes, 1)
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, out.Outcomes[0].Outcome)
	assert.Equal(t, state.HeldReasonMatchClosed, out.Outcomes[0].Reason)
	assert.Equal(t, MatchClosedMessage, out.Outcomes[0].Message)

	// The score door: the same tie, as a running snapshot.
	tie := mergeScoreAt(t, r, compID, map[string]any{"status": "running",
		"ipponsA": []string{"M"}, "ipponsB": []string{"M"}, "modifiedAt": now - 6000})
	assert.Equal(t, false, tie["applied"])
	assert.Equal(t, state.HeldReasonMatchClosed, tie["heldReason"])
	assert.Equal(t, MatchClosedMessage, tie["message"], "not 'a newer result is already recorded': there is none")

	// The score door: a point that would change the winner.
	win := mergeScoreAt(t, r, compID, map[string]any{"status": "running",
		"ipponsA": []string{"M"}, "ipponsB": []string{"K", "D"}, "modifiedAt": now - 5000})
	assert.Equal(t, state.HeldReasonMatchClosed, win["heldReason"])
	assert.Equal(t, MatchClosedMessage, win["message"])
}

// F3: a previous build's running snapshot (no writeId, no events) is a
// baseline; taps made after it fold on top.
func TestScoreEventsDoor_OldBuildSnapshotIsABaseline(t *testing.T) {
	const compID = "se-oldbuild"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	mergeScoreAt(t, r, compID, map[string]any{"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{"D"},
		"changed": []string{"points"}, "modifiedAt": now - 8000})
	lines, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	require.NotNil(t, lines[len(lines)-1].Baseline, "the snapshot is logged as a baseline")
	assert.Equal(t, engine.DoorScore, lines[len(lines)-1].Baseline.Door)

	out := sePost(t, r, compID, seMatch, seTap("e2", 2, now-7000, "a", "K"))
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
	assert.Equal(t, []string{"M", "K"}, out.Match.IpponsA)
	assert.Equal(t, []string{"D"}, out.Match.IpponsB)
}

// Bulk-score lands a baseline too; a tap after it folds on top.
func TestScoreEventsDoor_BulkScoreIsABaseline(t *testing.T) {
	const compID = "se-bulk"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	w := serveJSON(r, http.MethodPost, "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{{
		"id": seMatch, "sideA": mgAlice, "sideB": mgBob, "status": "running",
		"ipponsA": []string{"D"}, "ipponsB": []string{}, "modifiedAt": now - 8000, "writeId": "w-bulk",
	}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	lines, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	last := lines[len(lines)-1].Baseline
	require.NotNil(t, last)
	assert.Equal(t, engine.DoorBulkScore, last.Door)
	assert.Equal(t, "w-bulk", last.WriteID)

	out := sePost(t, r, compID, seMatch, seTap("e2", 2, now-7000, "b", "K"))
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
	assert.Equal(t, []string{"D"}, out.Match.IpponsA)
	assert.Equal(t, []string{"K"}, out.Match.IpponsB)
}

// Self-run: the door is public, a participant is held to the score path's
// limits, and a wrong password is 401.
func TestScoreEventsDoor_SelfRunParticipantLimits(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "C", Kind: "individual",
		Format: state.CompFormatLeague, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: mgAlice, SideB: mgBob, Status: state.MatchStatusRunning},
		{ID: "PoolA-1", SideA: mgAlice, SideB: mgBob, Status: state.MatchStatusCompleted, Winner: mgAlice, IpponsA: []string{"M"}, IpponsB: []string{}},
	}))
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	now := time.Now().UnixMilli()
	send := func(matchID, password string, events ...map[string]any) (int, seAnswer) {
		req := jsonReq(http.MethodPost, sePath("c1", matchID), map[string]any{"session": "evs", "events": events})
		req.Header.Set("X-Tournament-Password", password)
		w := httptestRecorder(r, req)
		var out seAnswer
		if w.Code == http.StatusOK {
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		}
		return w.Code, out
	}

	req := jsonReq(http.MethodPost, sePath("c1", "PoolA-0"), map[string]any{"session": "evs", "events": []any{seTap("p0", 1, now-1100, "a", "M")}})
	req.Header.Set("X-Tournament-Password", "")
	w := httptestRecorder(r, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, AppBuild(), w.Header().Get(BuildHeader), "every answer names the build")

	code, out := send("PoolA-0", "", seTap("p1", 2, now-1000, "b", "K"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome, "a participant scores a running match")

	code, out = send("PoolA-1", "", seTap("p2", 3, now-900, "b", "K"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, domain.ScoreOutcomeRefused, out.Outcomes[0].Outcome)
	assert.Equal(t, "result_finalized", out.Outcomes[0].Reason)
	assert.Equal(t, errResultFinalized.message, out.Outcomes[0].Message)

	code, out = send("PoolA-0", "", map[string]any{"id": "p3", "seq": 4, "stamp": now - 800, "kind": "daihyosen.verdict", "pos": -1, "side": "a"})
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "hantei_organiser_only", out.Outcomes[0].Reason)

	code, out = send("PoolA-1", "main-pw", seTap("o1", 1, now-700, "b", "K"))
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, out.Outcomes[0].Outcome, "the organiser's tap on a finished match is turned away: Correct is the way")

	code, _ = send("PoolA-0", "wrong", seTap("x1", 9, now, "a", "M"))
	assert.Equal(t, http.StatusUnauthorized, code)
}

// bc-evts stage 4: the engi editor scores a running match by flag taps, on the
// public score sheet too (password ""). A participant's flag lands on a running
// engi match and is refused once the match has finished, as a strike is.
func TestScoreEventsDoor_SelfRunEngiFlags(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "C", Kind: "individual", Engi: true,
		Format: state.CompFormatLeague, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePoolMatches("c1", []state.MatchResult{
		{ID: "PoolA-0", SideA: mgAlice, SideB: mgBob, Status: state.MatchStatusRunning},
		{ID: "PoolA-1", SideA: mgAlice, SideB: mgBob, Status: state.MatchStatusCompleted, Winner: mgAlice, FlagsA: 3, FlagsB: 0},
	}))
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	now := time.Now().UnixMilli()
	flag := func(id string, seq, stamp int64, kind, side string) map[string]any {
		return map[string]any{"id": id, "seq": seq, "stamp": stamp, "kind": kind, "side": side}
	}
	send := func(matchID string, events ...map[string]any) seAnswer {
		req := jsonReq(http.MethodPost, sePath("c1", matchID), map[string]any{"session": "evs", "events": events})
		req.Header.Set("X-Tournament-Password", "")
		w := httptestRecorder(r, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out seAnswer
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}

	out := send("PoolA-0", flag("f1", 1, now-1000, "flag.add", "b"), flag("f2", 2, now-900, "flag.add", "b"),
		flag("f3", 3, now-800, "flag.add", "a"), flag("f4", 4, now-700, "flag.remove", "b"))
	require.Len(t, out.Outcomes, 4)
	for _, o := range out.Outcomes {
		assert.Equal(t, domain.ScoreOutcomeApplied, o.Outcome, o.ID)
	}
	assert.Equal(t, 1, out.Match.FlagsA)
	assert.Equal(t, 1, out.Match.FlagsB)
	stored, err := store.LoadPoolMatches("c1")
	require.NoError(t, err)
	assert.Equal(t, [2]int{1, 1}, [2]int{stored[0].FlagsA, stored[0].FlagsB}, "the flags are stored as they are tapped")

	out = send("PoolA-1", flag("f5", 5, now-600, "flag.add", "b"))
	require.Len(t, out.Outcomes, 1)
	assert.Equal(t, domain.ScoreOutcomeRefused, out.Outcomes[0].Outcome)
	assert.Equal(t, "result_finalized", out.Outcomes[0].Reason)
	assert.Equal(t, errResultFinalized.message, out.Outcomes[0].Message)
}

// The SSE stream's first data frame names the build.
func TestSSEHelloNamesTheBuild(t *testing.T) {
	hub := NewHub()
	srv := newSSETestServer(t, hub)
	resp, err := http.Get(srv + "/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	buf := make([]byte, 512)
	got := ""
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(got, `"hello":true`) {
		n, rerr := resp.Body.Read(buf)
		got += string(buf[:n])
		if rerr != nil {
			break
		}
	}
	assert.Contains(t, got, `"hello":true`)
	assert.Contains(t, got, `"build":"`+AppBuild()+`"`)
}

func httptestRecorder(r http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// newSSETestServer serves hub's stream at /events on a real listener.
func newSSETestServer(t *testing.T, hub *Hub) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/events", hub.HandleEvents())
	srv := httptest.NewServer(r)
	t.Cleanup(func() {
		hub.Close()
		srv.Close()
	})
	return srv.URL
}

// assertLogReplaysToStored pins "baselines are total" from the handler side:
// the stored match is what its score event log replays to.
func assertLogReplaysToStored(t *testing.T, store *state.Store, compID, matchID string, knockout bool) []state.ScoreLogLine {
	t.Helper()
	lines, err := store.LoadScoreLog(compID, matchID)
	require.NoError(t, err)
	var stored state.MatchResult
	if knockout {
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := b.MatchByID(matchID)
		require.NotNil(t, bm)
		stored = state.MatchResult{Status: bm.Status, Winner: bm.Winner, WinnerID: bm.WinnerID, Decision: bm.Decision,
			DecisionBy: bm.DecisionBy, IpponsA: bm.IpponsA, IpponsB: bm.IpponsB, HansokuA: bm.HansokuA, HansokuB: bm.HansokuB,
			Encho: bm.Encho, SubResults: bm.SubResults, GroupStamps: bm.GroupStamps, ModifiedAt: bm.ModifiedAt,
			ScoreVersion: bm.ScoreVersion, CorrectionReason: bm.CorrectionReason, ResultSource: bm.ResultSource,
			SideA: bm.SideA, SideB: bm.SideB, SideAID: bm.SideAID, SideBID: bm.SideBID, FlagsA: bm.FlagsA, FlagsB: bm.FlagsB}
	} else {
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == matchID {
				stored = m
			}
		}
	}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	rp := engine.ReplayScoring(&stored, lines, engine.ReplayShape{Comp: comp, Knockout: knockout})
	groups := append(append([]string{}, state.ScalarGroups...), state.ServerGroups...)
	for _, p := range state.SubPositions(append(append([]state.SubMatchResult{}, stored.SubResults...), rp.Match.SubResults...)) {
		groups = append(groups, state.BoutGroup(p))
	}
	for _, g := range groups {
		assert.JSONEq(t, string(state.GroupValue(&stored, g)), string(state.GroupValue(&rp.Match, g)), "group %s", g)
	}
	assert.Equal(t, len(lines), stored.ScoreVersion)
	for _, l := range lines {
		if l.Baseline != nil {
			assert.NotEqual(t, engine.DoorDrift, l.Baseline.Door, "no write needed the drift net")
		}
	}
	return lines
}

// The representative bout's add and remove are baselines, tagged with the
// writeId the request carried; taps on the bout fold on top.
func TestScoreEventsDoor_RepBoutAddAndRemoveAreBaselines(t *testing.T) {
	f := newRepBoutFixture(t, false)
	post := func(events ...map[string]any) seAnswer {
		t.Helper()
		w := f.send(http.MethodPost, repBoutMatchPath+"/score-events", "main-pw", map[string]any{"session": "evs", "events": events})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out seAnswer
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		return out
	}
	out := post(map[string]any{"id": "r1", "seq": 1, "stamp": f.now - 50_000, "kind": "foul.add", "pos": 3, "side": "a"})
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
	assertLogReplaysToStored(t, f.store, "c1", "B1", true)

	w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now - 40_000, "writeId": "w-add"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	lines := assertLogReplaysToStored(t, f.store, "c1", "B1", true)
	last := lines[len(lines)-1].Baseline
	require.NotNil(t, last)
	assert.Equal(t, engine.DoorDaihyosenAdd, last.Door)
	assert.Equal(t, "w-add", last.WriteID)

	out = post(map[string]any{"id": "r2", "seq": 2, "stamp": f.now - 30_000, "kind": "ippon.add", "pos": -1, "side": "b", "letter": "M"})
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome, "a tap lands on the representative bout")
	assertLogReplaysToStored(t, f.store, "c1", "B1", true)

	// A scored representative bout is not removed (daihyosen_scored): the
	// tap is taken back first, naming its mark by the tap's id.
	out = post(map[string]any{"id": "r2x", "seq": 4, "stamp": f.now - 25_000, "kind": "ippon.remove", "pos": -1, "target": "r2"})
	assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
	w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now - 20_000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assertLogReplaysToStored(t, f.store, "c1", "B1", true)
	out = post(map[string]any{"id": "r3", "seq": 3, "stamp": f.now - 10_000, "kind": "ippon.add", "pos": -1, "side": "a", "letter": "K"})
	assert.Equal(t, domain.ScoreOutcomeNoTarget, out.Outcomes[0].Outcome, "a tap on the removed bout has no target")
}

// A decision resent with the writeId its first send logged answers as
// recorded, with no history line.
func TestScoreEventsDoor_DecisionWriteIDResend(t *testing.T) {
	const compID = "se-decision"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	decide := func() *httptest.ResponseRecorder {
		return serveJSON(r, http.MethodPost, "/api/competitions/"+compID+"/matches/"+seMatch+"/decision", map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 8000, "writeId": "w-dec"})
	}
	w := decide()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	lines, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	last := lines[len(lines)-1].Baseline
	require.NotNil(t, last)
	assert.Equal(t, engine.DoorDecision, last.Door)
	assert.Equal(t, "w-dec", last.WriteID)
	history, err := store.LoadMatchHistory(compID, seMatch)
	require.NoError(t, err)

	w = decide()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	after, err := store.LoadMatchHistory(compID, seMatch)
	require.NoError(t, err)
	assert.Len(t, after, len(history), "a recorded resend leaves no history line")
	again, err := store.LoadScoreLog(compID, seMatch)
	require.NoError(t, err)
	assert.Len(t, again, len(lines), "nor a log line")
}

// A decision that ends a match reopened with no reason keeps the decision's
// reason as the correction reason. That reason is part of the result group, so
// it must be on the write the baseline is taken from: set after the engine
// wrote, the stored match carried a field its log did not replay to, and the
// next tap fired the drift net.
func TestScoreEventsDoor_DecisionOnAReopenedMatchCarriesItsReasonInTheBaseline(t *testing.T) {
	const compID = "se-reopen-reason"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	base := "/api/competitions/" + compID + "/matches/" + seMatch
	w := serveJSON(r, http.MethodPost, base+"/decision", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 8000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(r, http.MethodPost, base+"/reopen", map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(r, http.MethodPost, base+"/decision", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro", "decisionReason": "left the court",
		"modifiedAt": time.Now().UnixMilli() + 100})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Equal(t, "left the court", ms[0].CorrectionReason, "the reason is the match's correction reason")

	sePost(t, r, compID, seMatch, seTap("e2", 2, time.Now().UnixMilli()+1000, "b", "K"))
	assertLogReplaysToStored(t, store, compID, seMatch, false)
}

// Two taps inside the coalescer's window both reach the stream: the second is
// held to the window's end and carries the last state.
func TestScoreEventsDoor_SecondTapInTheWindowIsBroadcastAtItsEnd(t *testing.T) {
	const compID = "se-trailing"
	r, _, hub, _ := seServer(t, compID)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-2000, "a", "M"))
	sePost(t, r, compID, seMatch, seTap("e2", 2, now-1900, "b", "K"))

	var payloads []string
	deadline := time.After(3 * time.Second)
	for len(payloads) < 2 {
		select {
		case msg := <-ch:
			if strings.Contains(msg.payload, `"match_updated"`) {
				payloads = append(payloads, msg.payload)
			}
		case <-deadline:
			t.Fatalf("only %d match_updated broadcast(s) arrived", len(payloads))
		}
	}
	assert.NotContains(t, payloads[0], `"K"`, "the first broadcast is the first tap's state")
	assert.Contains(t, payloads[1], `"K"`, "the held broadcast carries the last tap")
}

// The door's write refusal mapping, and what a refusal leaves: the taps
// answered refused with the refusal's code, one history line per event id
// however often they are sent again, and no log line (the transaction wrote
// no match, so it wrote no baseline for it either).
// A tap on a finished knockout match whose winner already plays on is turned
// away by the closed-match rule before any write is tried: the downstream guard
// never sees it, because a tap can no longer change a finished match. The tap
// is a line of the log all the same (its outcome is the replay's), once however
// often it is sent.
func TestScoreEventsDoor_WriteRefusalIsAnAnswerNotALogLine(t *testing.T) {
	const compID = "se-write-refused"
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A", "B"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID, Kind: "individual", Status: state.CompStatusKnockout}))
	// A finished first round whose winner already plays the final, which is
	// being fought.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob", Winner: "Alice", WinnerID: "alice",
			Status: state.MatchStatusCompleted, IpponsA: []string{"M"}, MatchNumber: 1, DisplayRound: 2, Court: "B"}},
		{{ID: "m-r2-0", SideA: "Alice", SideB: "Charlie", SideAID: "alice", SideBID: "charlie",
			Status: state.MatchStatusRunning, MatchNumber: 2, DisplayRound: 1, Court: "A"}},
	}}))
	now := time.Now().UnixMilli()
	send := func() seAnswer {
		return sePost(t, r, compID, "m-r1-0", seTap("w1", 1, now-3000, "b", "K"), seTap("w2", 2, now-2900, "b", "K"))
	}
	for i := range 3 {
		out := send()
		require.Len(t, out.Outcomes, 2)
		for _, o := range out.Outcomes {
			if i == 0 {
				assert.Equal(t, domain.ScoreOutcomeMatchClosed, o.Outcome)
				assert.Equal(t, state.HeldReasonMatchClosed, o.Reason)
				assert.Equal(t, MatchClosedMessage, o.Message, "the score door's words")
			} else {
				assert.Equal(t, domain.ScoreOutcomeDuplicate, o.Outcome, "a resend is quiet")
			}
		}
	}
	lines, err := store.LoadScoreLog(compID, "m-r1-0")
	require.NoError(t, err)
	events := 0
	for _, l := range lines {
		if l.Event != nil {
			events++
		}
	}
	assert.Equal(t, 2, events, "each tap is one line of the log, however often it is resent")
	m, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", m.Rounds[0][0].Winner, "the finished match is as it was")
	assert.Empty(t, m.Rounds[0][0].IpponsB)
}

// A fault that is not a refusal (here a stand-in engine) is a 500 the device
// retries, never a refused answer.
type faultyScoreEvents struct{ ScoringEngine }

func (faultyScoreEvents) RecordScoreEventsTx(_ state.StoreTx, _, _ string, req engine.ScoreEventsRequest) (*engine.ScoreEventsResult, error) {
	return &engine.ScoreEventsResult{Answers: make([]engine.ScoreEventAnswer, len(req.Events)),
		WriteErr: errors.New("disk fault"), Match: &state.MatchResult{}}, nil
}

func TestScoreEventsDoor_NonRefusalWriteFaultIs500(t *testing.T) {
	const compID = "se-fault"
	_, store, hub, _ := seServer(t, compID)
	g := gin.New()
	c := newMatchBroadcastCoalescer()
	t.Cleanup(c.Close)
	registerScoreEventsHandler(g.Group("/api"), faultyScoreEvents{engine.New(store)}, store, store, hub, NewFileVerifier(store), store, c)
	w := serveJSON(g, http.MethodPost, sePath(compID, seMatch), map[string]any{"session": "evs",
		"events": []map[string]any{seTap("f1", 1, time.Now().UnixMilli()-1000, "a", "M")}})
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}

func TestEventWriteRefusalMapsEveryRefusal(t *testing.T) {
	for want, err := range map[string]error{
		"downstream_knockout_played":  &engine.DownstreamKnockoutPlayedError{},
		"downstream_knockout_running": &engine.DownstreamKnockoutRunningError{},
		"already_ineligible":          &engine.AlreadyIneligibleError{},
		"ineligible_competitor":       &engine.IneligibleCompetitorError{},
		"side_mismatch":               engine.ErrMatchSideMismatch,
		"invalid":                     &engine.ValidationError{Msg: "bad scoreline"},
	} {
		reason, message, ok := eventWriteRefusal(fmt.Errorf("wrapped: %w", err))
		assert.True(t, ok, want)
		assert.Equal(t, want, reason)
		assert.NotEmpty(t, message, want)
	}
	_, _, ok := eventWriteRefusal(errors.New("disk fault"))
	assert.False(t, ok, "anything else is a fault, not a refusal")
}

// A tap that clears a withdrawal (R2) gives the competitor their eligibility
// back in the same transaction, and the log still replays to the stored match.
// A tap after a withdrawal no longer clears it (the closed-match rule): the
// competitor stays barred, and the tap is turned away.
func TestScoreEventsDoor_TapClearingAWithdrawalRestoresEligibility(t *testing.T) {
	const compID = "se-clears"
	r, store, _, _ := seServer(t, compID)
	now := time.Now().UnixMilli()
	sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"))
	w := serveJSON(r, http.MethodPost, "/api/competitions/"+compID+"/matches/"+seMatch+"/decision",
		map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 8000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	barred, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.NotEmpty(t, barred, "the withdrawal bars the competitor")
	for _, s := range barred {
		require.False(t, s.Eligible)
	}

	out := sePost(t, r, compID, seMatch, seTap("e2", 2, now-1000, "b", "K"))
	require.Equal(t, domain.ScoreOutcomeMatchClosed, out.Outcomes[0].Outcome)
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, "kiken-voluntary", ms[0].Decision, "the withdrawal stands")
	after, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	for _, s := range after {
		assert.False(t, s.Eligible, "the competitor stays barred")
	}
	assertLogReplaysToStored(t, store, compID, seMatch, false)
}

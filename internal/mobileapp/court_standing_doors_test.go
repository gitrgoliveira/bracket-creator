package mobileapp

// bc-sseq FR8 through the doors: /score and /decision judge a write's
// courtControl {tabId, deviceId} on arrival against who runs the match's
// shiaijo (courtWrite.judgeCourtStanding, CourtControls.WriteStanding). Until
// the court console stamps its writes (p5-client) these tests post the
// stamp by hand. The court's standing shows in the outcome: a court result
// replaces an ordinary result it never saw, where any other later result is
// its rival and is turned away match_closed (I1).

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// standingRig is mergeServer's running pool match (Pool A-0 on shiaijo A,
// Alice v Bob) behind a router whose hub carries a court control registry,
// as the server's does.
type standingRig struct {
	r      http.Handler
	store  *state.Store
	courts *courtControlRegistry
	compID string
}

func newStandingRig(t *testing.T, compID string, mode string) *standingRig {
	t.Helper()
	r, store, _, hub, _ := setupTestRouter(t)
	password := ""
	if mode == state.TournamentModeSelfRun {
		password = "main-pw"
	}
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: password, Courts: []string{"A"}, Mode: mode}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: mgAliceID, Name: mgAlice, Dojo: "D1"},
		{ID: mgBobID, Name: mgBob, Dojo: "D2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID, Court: "A", Status: state.MatchStatusRunning},
		// A second match keeps the league from finishing on the first one's
		// result, which would hold a court result downstream_built_on.
		{ID: "Pool A-1", SideA: mgBob, SideAID: mgBobID, SideB: mgAlice, SideBID: mgAliceID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	courts := newCourtControlRegistry(store, hub)
	hub.attachCourtControl(courts)
	return &standingRig{r: r, store: store, courts: courts, compID: compID}
}

func (rig *standingRig) claim(t *testing.T, tab, tablet, page string, take bool, takenAt int64) {
	t.Helper()
	_, err := rig.courts.Claim(CourtClaim{Court: "A", TabID: tab, DeviceID: tablet, PageID: page, Device: "ipad", Take: take, TakenAt: takenAt})
	require.NoError(t, err)
}

// send is one write to a door of Pool A-0 with the password given ("" as the
// public page sends it), answering the status and the decoded body.
func (rig *standingRig) send(t *testing.T, method, door, password string, body map[string]any) (int, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	require.NoError(t, err)
	req, err := http.NewRequest(method, "/api/competitions/"+rig.compID+"/matches/Pool A-0/"+door, bytes.NewReader(raw))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	rig.r.ServeHTTP(w, req)
	var out map[string]any
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	}
	return w.Code, out
}

// finish is a finished result of Pool A-0 with winner, stamped at stamp, from
// a device that saw no result (a basis holding nothing), stamped with the
// court control cc when cc is not nil.
func (rig *standingRig) finish(t *testing.T, password, winner string, stamp int64, cc map[string]any) (int, map[string]any) {
	t.Helper()
	body := map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": winner,
		"modifiedAt": stamp, "basis": map[string]any{"seen": 0},
	}
	if winner == mgAlice {
		body["ipponsA"], body["ipponsB"] = []string{"M"}, []string{}
	} else {
		body["ipponsA"], body["ipponsB"] = []string{}, []string{"K"}
	}
	if cc != nil {
		body["courtControl"] = cc
	}
	return rig.send(t, "PUT", "score", password, body)
}

func (rig *standingRig) stored(t *testing.T) state.MatchResult {
	t.Helper()
	return mergeStored(t, rig.store, rig.compID)
}

func (rig *standingRig) hold(t *testing.T) state.CourtHold {
	t.Helper()
	return rig.store.LoadCourtControl().Courts["A"]
}

func courtStamp(tab, tablet string) map[string]any {
	return map[string]any{"tabId": tab, "deviceId": tablet}
}

// assertTurnedAway is the ordinary later result: a rival of the first one,
// kept in the history, the first result standing.
func assertTurnedAway(t *testing.T, rig *standingRig, code int, out map[string]any) {
	t.Helper()
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["applied"], out)
	assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
	assert.Equal(t, mgAlice, rig.stored(t).Winner, "the first recorded result stands")
}

// assertCourtCounts is the court's later result: it replaces the ordinary one.
func assertCourtCounts(t *testing.T, rig *standingRig, code int, out map[string]any) {
	t.Helper()
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEqual(t, false, out["applied"], out)
	m := rig.stored(t)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, mgBob, m.Winner, "the court's result counts")
	assert.Equal(t, mgBobID, m.WinnerID)
}

func TestCourtStanding_AWriteClaimsAFreeShiaijoAndCounts(t *testing.T) {
	rig := newStandingRig(t, "standing-free", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code)
	require.False(t, rig.hold(t).Held(), "an ordinary write claims nothing")

	// A console opened offline: nobody runs shiaijo A when its result arrives.
	code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
	assertCourtCounts(t, rig, code, out)
	h := rig.hold(t)
	require.True(t, h.Held(), "the write claimed the free shiaijo")
	assert.Equal(t, state.CourtControlKey(ccTab1), h.HolderKey, "the writing tab runs it")
	assert.Equal(t, state.CourtControlKey(ccTablet1), h.DeviceKey)
	assert.Zero(t, h.StandingFrom, "a free shiaijo's standing starts at 0")
}

func TestCourtStanding_AnotherTabOfTheHoldingTabletCountsAndTakesNothing(t *testing.T) {
	rig := newStandingRig(t, "standing-same-tablet", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
	code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code)

	// A new tab of the same tablet (opened from the home screen icon, offline).
	code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab2, ccTablet1))
	assertCourtCounts(t, rig, code, out)
	assert.Equal(t, state.CourtControlKey(ccTab1), rig.hold(t).HolderKey, "the holding tab keeps the bar")
}

func TestCourtStanding_AnotherTabletsWriteIsOrdinary(t *testing.T) {
	rig := newStandingRig(t, "standing-other-tablet", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
	code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code)

	code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab2, ccTablet2))
	assertTurnedAway(t, rig, code, out)
	assert.Equal(t, state.CourtControlKey(ccTab1), rig.hold(t).HolderKey, "an ordinary write takes nothing")
}

func TestCourtStanding_AWriteWithNoCourtControlIsOrdinary(t *testing.T) {
	rig := newStandingRig(t, "standing-none", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
	code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code)

	// The holding tablet's Scores tab sends no court control.
	code, out := rig.finish(t, "", mgBob, now-1_000, nil)
	assertTurnedAway(t, rig, code, out)
}

// A take by another tablet starts the standing at the take: what the taking
// tablet made before it is ordinary, what it makes after it counts.
func TestCourtStanding_TheStandingStartsAtTheTake(t *testing.T) {
	now := time.Now().UnixMilli()
	takeAt := now - 3_000
	setup := func(t *testing.T, compID string) *standingRig {
		rig := newStandingRig(t, compID, state.TournamentModeOfficiated)
		rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
		rig.claim(t, ccTab2, ccTablet2, ccPage2, true, takeAt)
		code, _ := rig.finish(t, "", mgAlice, now-6_000, nil)
		require.Equal(t, http.StatusOK, code)
		return rig
	}
	t.Run("made before the take", func(t *testing.T) {
		rig := setup(t, "standing-before-take")
		code, out := rig.finish(t, "", mgBob, now-4_000, courtStamp(ccTab2, ccTablet2))
		assertTurnedAway(t, rig, code, out)
	})
	t.Run("made after the take", func(t *testing.T) {
		rig := setup(t, "standing-after-take")
		code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab2, ccTablet2))
		assertCourtCounts(t, rig, code, out)
	})
	t.Run("the displaced tablet's later write", func(t *testing.T) {
		rig := setup(t, "standing-displaced")
		code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
		assertTurnedAway(t, rig, code, out)
		assert.Equal(t, state.CourtControlKey(ccTab2), rig.hold(t).HolderKey)
	})
}

// A court move between the write and its arrival: the write is judged by the
// shiaijo the match is on when it arrives, which the tablet does not run.
func TestCourtStanding_JudgedByTheShiaijoTheMatchIsOnNow(t *testing.T) {
	rig := newStandingRig(t, "standing-moved", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
	code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code)
	tour, err := rig.store.LoadTournament()
	require.NoError(t, err)
	tour.Courts = []string{"A", "B"}
	require.NoError(t, rig.store.SaveTournament(tour))
	// Another tablet runs shiaijo B, where the match now is.
	_, err = rig.courts.Claim(CourtClaim{Court: "B", TabID: ccTab3, DeviceID: ccTablet2, PageID: ccPage3, Device: "ipad"})
	require.NoError(t, err)
	ms, err := rig.store.LoadPoolMatches(rig.compID)
	require.NoError(t, err)
	ms[0].Court = "B"
	require.NoError(t, rig.store.SavePoolMatches(rig.compID, ms))

	code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
	assertTurnedAway(t, rig, code, out)
	assert.Equal(t, state.CourtControlKey(ccTab3), rig.store.LoadCourtControl().Courts["B"].HolderKey, "B keeps its holder")
}

func TestCourtStanding_NeverInSelfRun(t *testing.T) {
	t.Run("a participant's court control is dropped", func(t *testing.T) {
		rig := newStandingRig(t, "standing-self-run-public", state.TournamentModeSelfRun)
		now := time.Now().UnixMilli()
		code, out := rig.finish(t, "", mgAlice, now-1_000, courtStamp(ccTab1, ccTablet1))
		require.Equal(t, http.StatusOK, code, out)
		assert.False(t, rig.hold(t).Held(), "a participant's write claims no shiaijo")
	})
	t.Run("the organiser's write has no standing either", func(t *testing.T) {
		rig := newStandingRig(t, "standing-self-run-organiser", state.TournamentModeSelfRun)
		now := time.Now().UnixMilli()
		code, _ := rig.finish(t, "main-pw", mgAlice, now-5_000, nil)
		require.Equal(t, http.StatusOK, code)
		code, out := rig.finish(t, "main-pw", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
		assertTurnedAway(t, rig, code, out)
		assert.False(t, rig.hold(t).Held(), "no write claims a shiaijo in self-run")
	})
}

func TestCourtStanding_AMalformedCourtControlIsRefused(t *testing.T) {
	rig := newStandingRig(t, "standing-malformed", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	bad := []map[string]any{
		{"tabId": ccTab1},
		{"tabId": "not-a-uuid", "deviceId": ccTablet1},
		{"tabId": ccTab1, "deviceId": "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA"},
	}
	for _, cc := range bad {
		code, out := rig.finish(t, "", mgBob, now-1_000, cc)
		assert.Equal(t, http.StatusBadRequest, code, out)
		code, out = rig.send(t, "POST", "decision", "", map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": now - 1_000, "courtControl": cc,
		})
		assert.Equal(t, http.StatusBadRequest, code, out)
	}
	assert.False(t, rig.hold(t).Held(), "a refused write claims nothing")
	assert.Equal(t, state.MatchStatusRunning, rig.stored(t).Status)
}

func TestCourtStanding_TheDecisionDoor(t *testing.T) {
	now := time.Now().UnixMilli()
	// Shiro (Bob) withdraws: aka (Alice) wins.
	kiken := func(cc map[string]any) map[string]any {
		body := map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 1_000,
			"basis": map[string]any{"seen": 0},
		}
		if cc != nil {
			body["courtControl"] = cc
		}
		return body
	}
	setup := func(t *testing.T, compID string) *standingRig {
		rig := newStandingRig(t, compID, state.TournamentModeOfficiated)
		code, _ := rig.finish(t, "", mgBob, now-5_000, nil)
		require.Equal(t, http.StatusOK, code)
		return rig
	}
	t.Run("the court's decision claims the free shiaijo and counts", func(t *testing.T) {
		rig := setup(t, "standing-decision-court")
		code, out := rig.send(t, "POST", "decision", "", kiken(courtStamp(ccTab1, ccTablet1)))
		require.Equal(t, http.StatusOK, code, out)
		assert.NotEqual(t, false, out["applied"], out)
		m := rig.stored(t)
		assert.Equal(t, "kiken-voluntary", m.Decision, "the court's decision counts")
		assert.Equal(t, mgAliceID, m.WinnerID)
		assert.Equal(t, state.CourtControlKey(ccTab1), rig.hold(t).HolderKey)
	})
	t.Run("another tablet's decision is ordinary", func(t *testing.T) {
		rig := setup(t, "standing-decision-other")
		rig.claim(t, ccTab1, ccTablet1, ccPage1, false, 0)
		code, out := rig.send(t, "POST", "decision", "", kiken(courtStamp(ccTab2, ccTablet2)))
		require.Equal(t, http.StatusOK, code, out)
		assert.Equal(t, false, out["applied"], out)
		assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
		m := rig.stored(t)
		assert.Empty(t, m.Decision, "the first recorded result stands")
		assert.Equal(t, mgBobID, m.WinnerID)
	})
}

// The built-on hold through the door: a league that finished on the ordinary
// result holds the court's result downstream_built_on, kept in the history,
// where an ordinary later result is a rival as ever (match_closed).
func TestCourtStanding_ACourtResultOverAFinishedLeagueIsHeldBuiltOn(t *testing.T) {
	now := time.Now().UnixMilli()
	setup := func(t *testing.T, compID string) *standingRig {
		rig := newStandingRig(t, compID, state.TournamentModeOfficiated)
		ms, err := rig.store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.NoError(t, rig.store.SavePoolMatches(compID, ms[:1]), "a league of one match")
		code, _ := rig.finish(t, "", mgAlice, now-5_000, nil)
		require.Equal(t, http.StatusOK, code)
		return rig
	}
	t.Run("the court's result", func(t *testing.T) {
		rig := setup(t, "standing-built-on-court")
		code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
		require.Equal(t, http.StatusOK, code, out)
		assert.Equal(t, false, out["applied"], out)
		assert.Equal(t, state.HeldReasonBuiltOn, out["heldReason"])
		assert.Contains(t, out["message"], "the competition already finished", "the sentence names what was built on the result")
		assert.Contains(t, out["message"], "tap Correct")
		assert.Equal(t, mgAlice, rig.stored(t).Winner, "the result the league finished on stands")
	})
	t.Run("the court's decision", func(t *testing.T) {
		rig := setup(t, "standing-built-on-decision")
		code, out := rig.send(t, "POST", "decision", "", map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": now - 1_000,
			"basis": map[string]any{"seen": 0}, "courtControl": courtStamp(ccTab1, ccTablet1),
		})
		require.Equal(t, http.StatusOK, code, out)
		assert.Equal(t, false, out["applied"], out)
		assert.Equal(t, state.HeldReasonBuiltOn, out["heldReason"])
		assert.Contains(t, out["message"], "the competition already finished", "the decision door words it as the score door does")
		assert.Equal(t, mgAlice, rig.stored(t).Winner, "the result the league finished on stands")
	})
	t.Run("an ordinary result", func(t *testing.T) {
		rig := setup(t, "standing-built-on-ordinary")
		code, out := rig.finish(t, "", mgBob, now-1_000, nil)
		assertTurnedAway(t, rig, code, out)
	})
}

// An ordinary result that arrives after a later-stamped court result is held
// court_result, kept in the history: the court's result stands.
func TestCourtStanding_AnOrdinaryResultArrivingAfterTheCourtsIsHeld(t *testing.T) {
	rig := newStandingRig(t, "standing-court-first", state.TournamentModeOfficiated)
	now := time.Now().UnixMilli()
	code, out := rig.finish(t, "", mgBob, now-1_000, courtStamp(ccTab1, ccTablet1))
	assertCourtCounts(t, rig, code, out)

	code, out = rig.finish(t, "", mgAlice, now-5_000, nil)
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["applied"], out)
	assert.Equal(t, state.HeldReasonCourtResult, out["heldReason"])
	stored := rig.stored(t)
	assert.Equal(t, engine.CourtResultMessage(stored.Court), out["message"], "the sentence names the court's result")
	assert.Equal(t, mgBob, stored.Winner, "the court's result stands")
}

// The sentence for each FR8 code, on a path that knows the stored match and on
// bulk-score's, which knows only the code: never the generic newer-change one.
func TestCourtStanding_HeldReasonSentences(t *testing.T) {
	assert.Equal(t, engine.CourtResultMessage("B"), heldReasonMessage(state.HeldReasonCourtResult, &state.MatchResult{Court: "B"}))
	assert.Equal(t, engine.CourtResultMessage(""), heldReasonMessage(state.HeldReasonCourtResult, nil))
	assert.Equal(t, engine.CourtResultMessage(""), messageForHeldReason(state.HeldReasonCourtResult))
	assert.Equal(t, MatchClosedMessage, heldReasonMessage(state.HeldReasonMatchClosed, &state.MatchResult{Court: "B"}))
}

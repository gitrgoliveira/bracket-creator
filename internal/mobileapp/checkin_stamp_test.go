package mobileapp

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The three check-in doors take the tap's stamp ({modifiedAt}) and the later
// tap wins: an older or equal stamp is answered 200 superseded with the
// participant as stored, and writes nothing.

var (
	checkInStampMu   sync.Mutex
	checkInStampLast int64
)

// nextCheckInStamp is a check-in tap's stamp for a test: the clock now, and
// always later than the one handed out before, so two taps in one
// millisecond still order.
func nextCheckInStamp() int64 {
	checkInStampMu.Lock()
	defer checkInStampMu.Unlock()
	at := time.Now().UnixMilli()
	if at <= checkInStampLast {
		at = checkInStampLast + 1
	}
	checkInStampLast = at
	return at
}

// stampedBody is the JSON body a check-in door takes, stamped now.
func stampedBody() map[string]any { return map[string]any{"modifiedAt": nextCheckInStamp()} }

// stampedBytes is stampedBody encoded, for the tests that send raw bytes.
func stampedBytes(t *testing.T) []byte {
	t.Helper()
	b, err := json.Marshal(stampedBody())
	require.NoError(t, err)
	return b
}

func checkInStampCalls(pid string, stamp int64) (put, del, bulk checkInCall) {
	base := "/api/competitions/" + noCheckInComp + "/participants"
	body := map[string]any{"modifiedAt": stamp}
	return checkInCall{"PUT checkin", http.MethodPut, base + "/" + pid + "/checkin", body},
		checkInCall{"DELETE checkin", http.MethodDelete, base + "/" + pid + "/checkin", body},
		checkInCall{"POST checkin-bulk", http.MethodPost, base + "/checkin-bulk", map[string]any{"participantIds": []string{pid}, "modifiedAt": stamp}}
}

func TestCheckInDoors_RequireAStamp(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"

	for _, stamp := range []int64{0, -5} {
		put, del, bulk := checkInStampCalls(pid, stamp)
		for _, c := range []checkInCall{put, del, bulk} {
			w := serve(r, c, &organiser)
			assert.Equal(t, http.StatusBadRequest, w.Code, "%s at %d: %s", c.name, stamp, w.Body.String())
		}
	}
	put, del, bulk := checkInStampCalls(pid, 1)
	for _, c := range []checkInCall{put, del, bulk} {
		c.body = nil
		w := serve(r, c, &organiser)
		assert.Equal(t, http.StatusBadRequest, w.Code, "%s with no body: %s", c.name, w.Body.String())
		// The same sentence the announcement doors give, not the decoder's "EOF".
		assert.JSONEq(t, `{"error":"Invalid request payload"}`, w.Body.String(), "%s with no body", c.name)
	}

	players, err := store.LoadParticipants(noCheckInComp, false)
	require.NoError(t, err)
	assert.False(t, players[0].CheckedIn)
	assert.Zero(t, players[0].CheckInAt, "a refused check-in writes nothing")
}

func TestCheckInDoors_AClockAheadIsAnsweredClockSkew(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"

	put, del, bulk := checkInStampCalls(pid, time.Now().UnixMilli()+60_000)
	for _, c := range []checkInCall{put, del, bulk} {
		w := serve(r, c, &organiser)
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", c.name, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		assert.Equal(t, false, body["applied"], c.name)
		assert.Equal(t, "clock_skew", body["reason"], c.name)
	}
	players, err := store.LoadParticipants(noCheckInComp, false)
	require.NoError(t, err)
	assert.False(t, players[0].CheckedIn)
	assert.Zero(t, players[0].CheckInAt)
}

func TestCheckInDoors_TheLaterTapWins(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"
	now := time.Now().UnixMilli()

	stored := func() domain.Player {
		players, err := store.LoadParticipants(noCheckInComp, false)
		require.NoError(t, err)
		return players[0]
	}

	// Desk B takes the competitor back at now; desk A's check-in, tapped
	// earlier, arrives after it and loses.
	_, del, _ := checkInStampCalls(pid, now)
	w := serve(r, del, &organiser)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var applied domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &applied))
	assert.False(t, applied.CheckedIn)
	assert.Equal(t, now, applied.CheckInAt)

	put, _, bulk := checkInStampCalls(pid, now-1000)
	for _, c := range []checkInCall{put, bulk} {
		w = serve(r, c, &organiser)
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", c.name, w.Body.String())
		assert.False(t, stored().CheckedIn, "%s: an older tap changes nothing", c.name)
	}
	w = serve(r, put, &organiser)
	var superseded struct {
		Applied     bool          `json:"applied"`
		Reason      string        `json:"reason"`
		Participant domain.Player `json:"participant"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &superseded))
	assert.False(t, superseded.Applied)
	assert.Equal(t, "superseded", superseded.Reason)
	assert.Equal(t, pid, superseded.Participant.ID)
	assert.False(t, superseded.Participant.CheckedIn, "answered as stored")
	assert.Equal(t, now, superseded.Participant.CheckInAt)

	w = serve(r, bulk, &organiser)
	var res struct {
		CheckedIn        int             `json:"checkedIn"`
		AlreadyCheckedIn int             `json:"alreadyCheckedIn"`
		NotFound         []string        `json:"notFound"`
		Superseded       []domain.Player `json:"superseded"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Zero(t, res.CheckedIn)
	require.Len(t, res.Superseded, 1)
	assert.Equal(t, pid, res.Superseded[0].ID)
	assert.Equal(t, now, res.Superseded[0].CheckInAt)

	// The same stamp again (a resend) is superseded too: the first to arrive
	// at a stamp holds.
	_, del, _ = checkInStampCalls(pid, now)
	w = serve(r, del, &organiser)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &superseded))
	assert.Equal(t, "superseded", superseded.Reason)

	// A later tap wins, through either door.
	put, _, _ = checkInStampCalls(pid, now+1)
	w = serve(r, put, &organiser)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &applied))
	assert.True(t, applied.CheckedIn)
	assert.Equal(t, now+1, stored().CheckInAt)

	_, _, bulk = checkInStampCalls(pid, now+2)
	w = serve(r, bulk, &organiser)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	assert.Equal(t, 1, res.AlreadyCheckedIn)
	assert.Empty(t, res.Superseded)
	assert.Equal(t, now+2, stored().CheckInAt, "an already checked-in competitor takes the later stamp")
}

func TestCheckInStamp_SurvivesTheBatchRosterPost(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"
	now := time.Now().UnixMilli()

	put, _, _ := checkInStampCalls(pid, now)
	require.Equal(t, http.StatusOK, serve(r, put, &organiser).Code)

	post := checkInCall{"POST participants", http.MethodPost, "/api/competitions/" + noCheckInComp + "/participants", map[string]any{
		"players": []map[string]any{{"name": "Alice", "dojo": "Suigetsu"}, {"name": "Bob", "dojo": "Kobe"}},
	}}
	w := serve(r, post, &organiser)
	require.Less(t, w.Code, 300, w.Body.String())

	players, err := store.LoadParticipants(noCheckInComp, false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.True(t, players[0].CheckedIn)
	assert.Equal(t, now, players[0].CheckInAt, "the roster replace keeps the stamp")

	// So a late, older take-back still loses after the replace.
	_, del, _ := checkInStampCalls(pid, now-1)
	w = serve(r, del, &organiser)
	assert.Contains(t, w.Body.String(), `"superseded"`)
}

package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementHandlers(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "announcement-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	eng := engine.New(store)
	mockFS := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
	}
	res := resources.NewResources(nil, mockFS)

	tourney := state.Tournament{
		Name:     "Test Tournament",
		Password: "secret-password",
	}
	err = store.SaveTournament(&tourney)
	require.NoError(t, err)

	router, _, limiter := NewRouter(store, eng, res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)

	// 1. GET /api/tournament/announcement - initially empty (204 No Content)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament/announcement", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// 2. GET /api/tournament/announcements - initially empty list
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var emptyList []state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &emptyList))
	assert.Empty(t, emptyList)

	// 3. POST /api/tournament/announce - unauthorized without header
	payload := newCall("Lunch break for 30 minutes", 30)
	body, _ := json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 4. POST /api/tournament/announce - wrong password
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "wrong-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 5. POST /api/tournament/announce - empty message
	badPayload := newCall("   ", 30)
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot be empty")

	// 6. POST /api/tournament/announce - message too long (>200 chars)
	badPayload = newCall(strings.Repeat("A", 201), 30)
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot exceed 200 characters")

	// 7. POST /api/tournament/announce - invalid duration
	badPayload = newCall("Valid message", 7)
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Duration must be 5, 10, 15, or 30 minutes")

	// 8. POST /api/tournament/announce - oversized body (>AnnouncementMaxBodyBytes)
	hugeMsg := strings.Repeat("A", int(AnnouncementMaxBodyBytes)+10)
	body, _ = json.Marshal(newCall(hugeMsg, 30))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equalf(t, http.StatusRequestEntityTooLarge, w.Code, "expected 413 for body over %d bytes", AnnouncementMaxBodyBytes)

	// 9. POST first announcement, happy path
	body, _ = json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var first state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))
	assert.Equal(t, "Lunch break for 30 minutes", first.Message)
	assert.NotEmpty(t, first.ID)
	assert.False(t, first.SentAt.IsZero())
	assert.True(t, first.ExpiresAt.After(first.SentAt))

	// 10. POST second announcement, both should coexist
	body, _ = json.Marshal(newCall("Court 3 paused", 5))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var second state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &second))
	assert.Equal(t, "Court 3 paused", second.Message)
	assert.NotEmpty(t, second.ID)
	assert.NotEqual(t, first.ID, second.ID)

	// 11. GET /api/tournament/announcements, should list both
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 2)
	assert.Equal(t, first.ID, list[0].ID)
	assert.Equal(t, second.ID, list[1].ID)

	// 12. GET /api/tournament/announcement, legacy endpoint returns most recent
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcement", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var single state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &single))
	assert.Equal(t, second.ID, single.ID)

	// 13. DELETE /api/announcements/:id, dismiss first
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements/"+first.ID, nil)
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 1)
	assert.Equal(t, second.ID, list[0].ID)

	// 14. DELETE /api/announcements/:id, not found
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements/doesnotexist", nil)
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 15. DELETE /api/announcements, clear all made at or before the clear
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements", clearBody())
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Empty(t, list)

	// 16. DELETE admin endpoints require auth
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements/someid", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestAnnouncementHandlers_IdenticalMessageReplaces pins bc-cdbl: posting the
// text that is already showing replaces its banner rather than stacking a
// second one, so a double tap on Call to court is one announcement from any
// device.
func TestAnnouncementHandlers_IdenticalMessageReplaces(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test Tournament", Password: "secret-password"}))

	eng := engine.New(store)
	res := resources.NewResources(nil, fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
	})
	router, _, limiter := NewRouter(store, eng, res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)

	post := func(msg string) state.Announcement {
		t.Helper()
		body, _ := json.Marshal(newCall(msg, 5))
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
		req.Header.Set("X-Tournament-Password", "secret-password")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var ann state.Announcement
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ann))
		return ann
	}

	first := post("Now calling Yamada and Tanaka to Shiaijo A.")
	second := post("Now calling Yamada and Tanaka to Shiaijo A.")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	var list []state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list, 1)
	assert.Equal(t, second.ID, list[0].ID)
	assert.NotEqual(t, first.ID, list[0].ID)
}

// TestAnnouncementHandlers_DeleteThroughAReplacedID pins the follow-up to
// bc-cdbl: a court console that holds the id of a call a later identical call
// replaced (another device called the same match, or its own re-call's answer
// was lost) still takes the call down. The DELETE answers 200 and broadcasts
// the list without the announcement that replaced the id, instead of a 404 that
// left the banner up until it expired. The server still tags no call with a
// match: it only knows which id replaced which.
func TestAnnouncementHandlers_DeleteThroughAReplacedID(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test Tournament", Password: "secret-password"}))

	eng := engine.New(store)
	res := resources.NewResources(nil, fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
	})
	router, hub, limiter := NewRouter(store, eng, res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)

	post := func(msg string) state.Announcement {
		t.Helper()
		body, _ := json.Marshal(newCall(msg, 5))
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
		req.Header.Set("X-Tournament-Password", "secret-password")
		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var ann state.Announcement
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ann))
		return ann
	}
	del := func(id string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/announcements/"+id, nil)
		req.Header.Set("X-Tournament-Password", "secret-password")
		router.ServeHTTP(w, req)
		return w
	}

	const call = "Now calling Yamada and Tanaka to Shiaijo A."
	first := post(call)
	second := post(call)
	require.NotEqual(t, first.ID, second.ID)

	// Subscribe after the POSTs, so the next event is the DELETE's broadcast.
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	w := del(first.ID)
	assert.Equal(t, http.StatusOK, w.Code, "the replaced id must withdraw the announcement that replaced it")

	select {
	case msg := <-ch:
		env := decodeHubEvent(t, msg)
		assert.Equal(t, EventAnnouncement, env.Type)
		list, ok := env.Data.([]any)
		require.Truef(t, ok, "the broadcast carries the announcement list, got %T", env.Data)
		assert.Empty(t, list, "the withdrawn call must leave the broadcast list")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the announcement broadcast after the withdrawal")
	}

	w = httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	var list []state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Empty(t, list)

	// Both ids are spent: nothing is left to take down through either.
	assert.Equal(t, http.StatusNotFound, del(first.ID).Code)
	assert.Equal(t, http.StatusNotFound, del(second.ID).Code)
}

var (
	callStampMu   sync.Mutex
	callLastStamp int64
)

// callStamp is a made-at for a test call: the server clock, kept strictly
// increasing, so two calls in a row are ordered (an identical call is ordered
// by made-at, and a clear takes what was made at or before it).
func callStamp() int64 {
	callStampMu.Lock()
	defer callStampMu.Unlock()
	ms := time.Now().UnixMilli()
	if ms <= callLastStamp {
		ms = callLastStamp + 1
	}
	callLastStamp = ms
	return ms
}

// newCall is a court call as a device makes it: a minted id and a made-at.
func newCall(msg string, minutes int) announcementRequest {
	return announcementRequest{ID: helper.NewUUID4(), Message: msg, DurationMinutes: minutes, ModifiedAt: callStamp()}
}

// clearBody is a clear made now.
func clearBody() *bytes.Reader {
	body, _ := json.Marshal(announcementClearRequest{ModifiedAt: callStamp()})
	return bytes.NewReader(body)
}

// announcementRig is a router with a tournament password and its hub.
type announcementRig struct {
	t      *testing.T
	router http.Handler
	hub    *Hub
}

func newAnnouncementRig(t *testing.T) *announcementRig {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test Tournament", Password: "secret-password"}))
	res := resources.NewResources(nil, fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
	})
	router, hub, limiter := NewRouter(store, engine.New(store), res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)
	return &announcementRig{t: t, router: router, hub: hub}
}

func (r *announcementRig) do(method, path string, body any) *httptest.ResponseRecorder {
	r.t.Helper()
	var reader *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		require.NoError(r.t, err)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(method, path, reader)
	req.Header.Set("X-Tournament-Password", "secret-password")
	r.router.ServeHTTP(w, req)
	return w
}

func (r *announcementRig) post(call announcementRequest) map[string]any {
	r.t.Helper()
	w := r.do("POST", "/api/tournament/announce", call)
	require.Equal(r.t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(r.t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func (r *announcementRig) list() []state.Announcement {
	r.t.Helper()
	w := r.do("GET", "/api/tournament/announcements", nil)
	require.Equal(r.t, http.StatusOK, w.Code)
	var list []state.Announcement
	require.NoError(r.t, json.Unmarshal(w.Body.Bytes(), &list))
	return list
}

// noBroadcast fails when an event reaches ch within a short wait.
func noBroadcast(t *testing.T, ch chan historyEntry, why string) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Errorf("%s, got %v", why, decodeHubEvent(t, msg))
	case <-time.After(100 * time.Millisecond):
	}
}

// TestAnnouncementHandlers_CallNeedsItsIDAndMadeAt: the id is how a resend is
// known and the made-at sets the expiry, so a call or a clear without them is
// refused rather than given a server-made stand-in.
func TestAnnouncementHandlers_CallNeedsItsIDAndMadeAt(t *testing.T) {
	rig := newAnnouncementRig(t)

	noID := newCall("Call A", 5)
	noID.ID = ""
	upper := newCall("Call A", 5)
	upper.ID = strings.ToUpper(upper.ID)
	noStamp := newCall("Call A", 5)
	noStamp.ModifiedAt = 0

	for name, call := range map[string]announcementRequest{"no id": noID, "uppercase id": upper, "no made-at": noStamp} {
		w := rig.do("POST", "/api/tournament/announce", call)
		assert.Equalf(t, http.StatusBadRequest, w.Code, "%s: %s", name, w.Body.String())
	}
	assert.Equal(t, http.StatusBadRequest, rig.do("DELETE", "/api/announcements", nil).Code, "a clear needs its made-at")
	assert.Equal(t, http.StatusBadRequest, rig.do("DELETE", "/api/announcements", announcementClearRequest{}).Code, "a clear needs its made-at")
	assert.Empty(t, rig.list())
}

// TestAnnouncementHandlers_ClockAheadIsTheSkewAnswer: a made-at far ahead of
// the server is the clock_skew answer the outbox re-stamps, and shows nothing.
func TestAnnouncementHandlers_ClockAheadIsTheSkewAnswer(t *testing.T) {
	rig := newAnnouncementRig(t)
	call := newCall("Call A", 5)
	call.ModifiedAt = time.Now().Add(time.Minute).UnixMilli()

	out := rig.post(call)
	assert.Equal(t, false, out["applied"])
	assert.Equal(t, "clock_skew", out["reason"])
	assert.Empty(t, rig.list())

	ahead := rig.do("DELETE", "/api/announcements", announcementClearRequest{ModifiedAt: time.Now().Add(time.Minute).UnixMilli()})
	assert.Equal(t, http.StatusOK, ahead.Code)
	assert.Contains(t, ahead.Body.String(), "clock_skew")
}

// TestAnnouncementHandlers_ResendIsRecordedSilently: the outbox sending a call
// again (its answer was lost) is answered as recorded with the call as stored,
// and the venue screens hear nothing new.
func TestAnnouncementHandlers_ResendIsRecordedSilently(t *testing.T) {
	rig := newAnnouncementRig(t)
	call := newCall("Now calling Yamada and Tanaka to Shiaijo A.", 5)
	first := rig.post(call)
	assert.Equal(t, call.ID, first["id"])
	assert.Nil(t, first["recorded"], "a call shown for the first time is not recorded")

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)

	again := rig.post(call)
	assert.Equal(t, true, again["recorded"])
	assert.Equal(t, first["id"], again["id"])
	assert.Equal(t, first["expiresAt"], again["expiresAt"])
	noBroadcast(t, ch, "a resend must not be broadcast")
	assert.Len(t, rig.list(), 1)
}

// TestAnnouncementHandlers_LateCall pins decision Q1 (floor): a call made
// offline that arrives with 15 seconds left shows for a minute, and one whose
// time is over is answered expired and shows nothing.
func TestAnnouncementHandlers_LateCall(t *testing.T) {
	rig := newAnnouncementRig(t)

	late := newCall("Call late", 5)
	late.ModifiedAt = time.Now().Add(-(4*time.Minute + 45*time.Second)).UnixMilli()
	out := rig.post(late)
	assert.Nil(t, out["expired"])
	expiresAt, err := time.Parse(time.RFC3339Nano, out["expiresAt"].(string))
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(state.AnnouncementMinShow), expiresAt, 2*time.Second)

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)

	over := newCall("Call over", 5)
	over.ModifiedAt = time.Now().Add(-6 * time.Minute).UnixMilli()
	out = rig.post(over)
	assert.Equal(t, true, out["expired"])
	assert.Equal(t, over.ID, out["id"])
	noBroadcast(t, ch, "an expired call must not be broadcast")
	list := rig.list()
	require.Len(t, list, 1)
	assert.Equal(t, late.ID, list[0].ID)
}

// TestAnnouncementHandlers_WithdrawnCallStaysDown: a late copy of a send the
// console already withdrew is recorded and never brings the call back.
func TestAnnouncementHandlers_WithdrawnCallStaysDown(t *testing.T) {
	rig := newAnnouncementRig(t)
	call := newCall("Call A", 5)
	rig.post(call)
	assert.Equal(t, http.StatusOK, rig.do("DELETE", "/api/announcements/"+call.ID, map[string]int64{"modifiedAt": callStamp()}).Code)

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)
	out := rig.post(call)
	assert.Equal(t, true, out["recorded"])
	noBroadcast(t, ch, "a late copy of a withdrawn call must not be broadcast")
	assert.Empty(t, rig.list())
	assert.Equal(t, http.StatusNotFound, rig.do("DELETE", "/api/announcements/"+call.ID, nil).Code, "withdrawing it again finds nothing")
}

// TestAnnouncementHandlers_ClearKeepsItsFloor: a clear takes down what was
// made at or before it; a call made before it that arrives later shows
// nothing, and a call made after it shows whatever the arrival order.
func TestAnnouncementHandlers_ClearKeepsItsFloor(t *testing.T) {
	rig := newAnnouncementRig(t)
	before := newCall("Made before the clear", 5)
	clearAt := callStamp()
	after := newCall("Made after the clear", 5)

	rig.post(after)
	assert.Equal(t, http.StatusOK, rig.do("DELETE", "/api/announcements", announcementClearRequest{ModifiedAt: clearAt}).Code)
	list := rig.list()
	require.Len(t, list, 1, "a call made after the clear stays")
	assert.Equal(t, after.ID, list[0].ID)

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)
	out := rig.post(before)
	assert.Equal(t, true, out["recorded"])
	noBroadcast(t, ch, "a call made before the clear must not be broadcast")
	assert.Len(t, rig.list(), 1)
}

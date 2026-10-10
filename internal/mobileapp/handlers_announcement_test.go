package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
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
	payload := announcementRequest{
		Message:         "Lunch break for 30 minutes",
		DurationMinutes: 30,
	}
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
	badPayload := announcementRequest{Message: "   ", DurationMinutes: 30}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot be empty")

	// 6. POST /api/tournament/announce - message too long (>200 chars)
	badPayload = announcementRequest{Message: strings.Repeat("A", 201), DurationMinutes: 30}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot exceed 200 characters")

	// 7. POST /api/tournament/announce - invalid duration
	badPayload = announcementRequest{Message: "Valid message", DurationMinutes: 7}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Duration must be 5, 10, 15, or 30 minutes")

	// 8. POST /api/tournament/announce - oversized body (>AnnouncementMaxBodyBytes)
	hugeMsg := strings.Repeat("A", int(AnnouncementMaxBodyBytes)+10)
	body, _ = json.Marshal(announcementRequest{Message: hugeMsg, DurationMinutes: 30})
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
	body, _ = json.Marshal(announcementRequest{Message: "Court 3 paused", DurationMinutes: 5})
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
	assert.Equal(t, http.StatusNoContent, w.Code)

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

	// 15. DELETE /api/announcements, clear all
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements", nil)
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

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
		body, _ := json.Marshal(announcementRequest{Message: msg, DurationMinutes: 5})
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
// was lost) still takes the call down. The DELETE answers 204 and broadcasts
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
		body, _ := json.Marshal(announcementRequest{Message: msg, DurationMinutes: 5})
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
	assert.Equal(t, http.StatusNoContent, w.Code, "the replaced id must withdraw the announcement that replaced it")

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

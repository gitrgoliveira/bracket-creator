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

	router, _, _ := NewRouter(store, eng, res, NewFileVerifier(store))

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

	// 9. POST first announcement — happy path
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

	// 10. POST second announcement — both should coexist
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

	// 11. GET /api/tournament/announcements — should list both
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcements", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var list []state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Len(t, list, 2)
	assert.Equal(t, first.ID, list[0].ID)
	assert.Equal(t, second.ID, list[1].ID)

	// 12. GET /api/tournament/announcement — legacy endpoint returns most recent
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcement", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var single state.Announcement
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &single))
	assert.Equal(t, second.ID, single.ID)

	// 13. DELETE /api/announcements/:id — dismiss first
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

	// 14. DELETE /api/announcements/:id — not found
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/announcements/doesnotexist", nil)
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// 15. DELETE /api/announcements — clear all
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

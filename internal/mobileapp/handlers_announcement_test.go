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

	// Save tournament config with a password so AuthMiddleware doesn't run in bootstrap mode
	tourney := state.Tournament{
		Name:     "Test Tournament",
		Password: "secret-password",
	}
	err = store.SaveTournament(&tourney)
	require.NoError(t, err)

	// Construct router using NewRouter so we test full middleware integration
	router := NewRouter(store, eng, res, NewFileVerifier(store))

	// 1. GET /api/tournament/announcement - initially empty (204 No Content)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament/announcement", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// 2. POST /api/tournament/announce - unauthorized without header
	payload := announcementRequest{
		Message:         "Lunch break for 30 minutes",
		DurationMinutes: 30,
	}
	body, _ := json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 3. POST /api/tournament/announce - unauthorized with wrong password
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "wrong-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// 4. POST /api/tournament/announce - bad request with empty message
	badPayload := announcementRequest{
		Message:         "   ",
		DurationMinutes: 30,
	}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot be empty")

	// 5. POST /api/tournament/announce - bad request with too long message (>200 chars)
	tooLongMsg := strings.Repeat("A", 201)
	badPayload = announcementRequest{
		Message:         tooLongMsg,
		DurationMinutes: 30,
	}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "cannot exceed 200 characters")

	// 6. POST /api/tournament/announce - bad request with invalid duration (e.g. 7 minutes)
	badPayload = announcementRequest{
		Message:         "Valid message",
		DurationMinutes: 7,
	}
	body, _ = json.Marshal(badPayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Duration must be 5, 10, 15, or 30 minutes")

	// 7. POST /api/tournament/announce - oversized body (>maxAnnounceBodyBytes)
	// returns 400 from MaxBytesReader unwinding through ShouldBindJSON.
	// Build a JSON body whose `message` field alone exceeds the 4096-byte
	// cap so we exercise the body-cap path (not the post-bind 200-char
	// validation).
	hugeMsg := strings.Repeat("A", maxAnnounceBodyBytes+10)
	hugePayload := announcementRequest{
		Message:         hugeMsg,
		DurationMinutes: 30,
	}
	body, _ = json.Marshal(hugePayload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "expected 400 for body over %d bytes", maxAnnounceBodyBytes)

	// 8. POST /api/tournament/announce - happy path (200 OK)
	body, _ = json.Marshal(payload)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament/announce", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "secret-password")
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var response state.Announcement
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	assert.Equal(t, "Lunch break for 30 minutes", response.Message)
	assert.False(t, response.SentAt.IsZero())
	assert.False(t, response.ExpiresAt.IsZero())
	assert.True(t, response.ExpiresAt.After(response.SentAt))

	// 9. GET /api/tournament/announcement - should now return the active announcement (200 OK)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament/announcement", nil)
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var retrieved state.Announcement
	err = json.Unmarshal(w.Body.Bytes(), &retrieved)
	require.NoError(t, err)
	assert.Equal(t, "Lunch break for 30 minutes", retrieved.Message)
}

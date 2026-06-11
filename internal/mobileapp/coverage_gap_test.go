package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRecoveredPanicError covers the Error() method on recoveredPanic (0%).
func TestRecoveredPanicError(t *testing.T) {
	p := &recoveredPanic{value: "kaboom"}
	assert.Contains(t, p.Error(), "panic: kaboom")

	// Non-string value.
	p2 := &recoveredPanic{value: 42}
	assert.Contains(t, p2.Error(), "panic: 42")
}

// TestNewHubWithLimits_ZeroHistorySize covers the historySize<=0 fallback (66.7%→100%).
func TestNewHubWithLimits_ZeroHistorySize(t *testing.T) {
	h := NewHubWithLimits(0, 5)
	assert.Equal(t, DefaultHistorySize, h.HistorySize)
	assert.Equal(t, 5, h.MaxClients)
	h.Close()

	h2 := NewHubWithLimits(-1, 0)
	assert.Equal(t, DefaultHistorySize, h2.HistorySize)
	h2.Close()
}

// errWriter always returns an error on Write.
type errWriter struct{}

func (errWriter) Write([]byte) (int, error) {
	return 0, fmt.Errorf("write error")
}

// TestWriteSSEEnvelope_ErrorPath covers the fmt.Printf branch in writeSSEEnvelope (50%).
func TestWriteSSEEnvelope_ErrorPath(t *testing.T) {
	// Should not panic even when the write fails.
	assert.NotPanics(t, func() {
		writeSSEEnvelope(errWriter{}, 99, `{"seq":99}`)
	})
}

// TestWriteSSEEnvelope_HappyPath verifies the SSE output format.
func TestWriteSSEEnvelope_HappyPath(t *testing.T) {
	var buf bytes.Buffer
	writeSSEEnvelope(&buf, 7, `{"seq":7,"type":"match_updated"}`)
	got := buf.String()
	assert.Contains(t, got, "id: 7")
	assert.Contains(t, got, "event: message")
	assert.Contains(t, got, `"seq":7`)
}

// TestExtractSeq covers the missing-seq and valid-seq paths (75%→100%).
func TestExtractSeq(t *testing.T) {
	assert.Equal(t, int64(0), extractSeq(""))
	assert.Equal(t, int64(0), extractSeq("not json"))
	assert.Equal(t, int64(0), extractSeq(`{"other":1}`))
	assert.Equal(t, int64(42), extractSeq(`{"seq":42}`))
	assert.Equal(t, int64(0), extractSeq(`{"seq":0}`))
}

// TestValidateCompetitionLengths_ErrorCases covers the error-return branches (55.6%→100%).
func TestValidateCompetitionLengths_ErrorCases(t *testing.T) {
	// All fields valid.
	require.NoError(t, validateCompetitionLengths(&state.Competition{
		Name:         "Short",
		NumberPrefix: "A",
		StartTime:    "09:00",
		Date:         "2026-01-01",
	}))

	// Name too long.
	require.Error(t, validateCompetitionLengths(&state.Competition{
		Name: strings.Repeat("x", MaxLenCompetitionName+1),
	}))

	// NumberPrefix too long.
	require.Error(t, validateCompetitionLengths(&state.Competition{
		Name:         "OK",
		NumberPrefix: strings.Repeat("x", MaxLenCompetitionNumberPrefix+1),
	}))

	// StartTime too long.
	require.Error(t, validateCompetitionLengths(&state.Competition{
		Name:         "OK",
		NumberPrefix: "OK",
		StartTime:    strings.Repeat("x", MaxLenCompetitionStartTime+1),
	}))

	// Date too long.
	require.Error(t, validateCompetitionLengths(&state.Competition{
		Name:         "OK",
		NumberPrefix: "OK",
		StartTime:    "OK",
		Date:         strings.Repeat("x", MaxLenCompetitionDate+1),
	}))
}

// TestBracketMatchToResult covers the bracketMatchToResult helper (0%).
func TestBracketMatchToResult(t *testing.T) {
	bm := &state.BracketMatch{
		ID:       "match-1",
		Winner:   "Alice",
		Decision: string(domain.DecisionFought),
		Status:   state.MatchStatusCompleted,
	}
	got := bracketMatchToResult(bm)
	require.NotNil(t, got)
	assert.Equal(t, bm.ID, got.ID)
	assert.Equal(t, bm.Winner, got.Winner)
	assert.Equal(t, bm.Decision, got.Decision)
	assert.Equal(t, bm.Status, got.Status)
}

// TestWriteSSEEnvelope_Discard ensures no panic writing to io.Discard.
func TestWriteSSEEnvelope_Discard(t *testing.T) {
	assert.NotPanics(t, func() {
		writeSSEEnvelope(io.Discard, 1, `{"seq":1}`)
	})
}

// TestValidateTournamentLengths_ErrorCases covers error-return branches in
// validateTournamentLengths (75% → higher).
func TestValidateTournamentLengths_ErrorCases(t *testing.T) {
	base := &state.Tournament{
		Name:     "OK",
		Venue:    "OK",
		Date:     "2026-01-01",
		Password: "pw",
	}

	// Happy path: empty struct passes all checks.
	require.NoError(t, validateTournamentLengths(&state.Tournament{}))

	// Name too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name: strings.Repeat("x", MaxLenTournamentName+1),
	}))

	// Venue too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:  base.Name,
		Venue: strings.Repeat("x", MaxLenTournamentVenue+1),
	}))

	// Date too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:  base.Name,
		Venue: base.Venue,
		Date:  strings.Repeat("x", MaxLenTournamentDate+1),
	}))

	// Password too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: strings.Repeat("x", MaxLenTournamentPassword+1),
	}))

	// OpeningBlock too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:         base.Name,
		Venue:        base.Venue,
		Date:         base.Date,
		Password:     base.Password,
		OpeningBlock: strings.Repeat("x", MaxLenCeremonyBlock+1),
	}))

	// LunchBlock too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:       base.Name,
		Venue:      base.Venue,
		Date:       base.Date,
		Password:   base.Password,
		LunchBlock: strings.Repeat("x", MaxLenCeremonyBlock+1),
	}))

	// ClosingBlock too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:         base.Name,
		Venue:        base.Venue,
		Date:         base.Date,
		Password:     base.Password,
		ClosingBlock: strings.Repeat("x", MaxLenCeremonyBlock+1),
	}))

	// PublicURL too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:      base.Name,
		Venue:     base.Venue,
		Date:      base.Date,
		Password:  base.Password,
		PublicURL: strings.Repeat("x", MaxLenPublicURL+1),
	}))

	// PublicURL invalid scheme (not http/https).
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:      base.Name,
		Venue:     base.Venue,
		Date:      base.Date,
		Password:  base.Password,
		PublicURL: "ftp://example.com",
	}))

	// PublicURL no host.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:      base.Name,
		Venue:     base.Venue,
		Date:      base.Date,
		Password:  base.Password,
		PublicURL: "https://",
	}))

	// VenueAddress too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:         base.Name,
		Venue:        base.Venue,
		Date:         base.Date,
		Password:     base.Password,
		VenueAddress: strings.Repeat("x", MaxLenVenueAddress+1),
	}))

	// VenueMapURL invalid scheme.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:        base.Name,
		Venue:       base.Venue,
		Date:        base.Date,
		Password:    base.Password,
		VenueMapURL: "ftp://example.com",
	}))

	// OpeningTime too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:        base.Name,
		Venue:       base.Venue,
		Date:        base.Date,
		Password:    base.Password,
		OpeningTime: strings.Repeat("x", MaxLenDisplayTime+1),
	}))

	// ClosingTime too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:        base.Name,
		Venue:       base.Venue,
		Date:        base.Date,
		Password:    base.Password,
		ClosingTime: strings.Repeat("x", MaxLenDisplayTime+1),
	}))

	// RulesURL invalid scheme.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: base.Password,
		RulesURL: "ftp://example.com",
	}))

	// VenueMapURL too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:        base.Name,
		Venue:       base.Venue,
		Date:        base.Date,
		Password:    base.Password,
		VenueMapURL: strings.Repeat("x", MaxLenVenueMapURL+1),
	}))

	// RulesURL too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: base.Password,
		RulesURL: strings.Repeat("x", MaxLenRulesURL+1),
	}))

	// AwardsNote too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:       base.Name,
		Venue:      base.Venue,
		Date:       base.Date,
		Password:   base.Password,
		AwardsNote: strings.Repeat("x", MaxLenAwardsNote+1),
	}))

	// InfoNotes too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:      base.Name,
		Venue:     base.Venue,
		Date:      base.Date,
		Password:  base.Password,
		InfoNotes: strings.Repeat("x", MaxLenInfoNotes+1),
	}))

	// Contacts exceeds max count.
	contacts := make([]state.TournamentContact, MaxTournamentContacts+1)
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: base.Password,
		Contacts: contacts,
	}))

	// Contact label too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: base.Password,
		Contacts: []state.TournamentContact{{Label: strings.Repeat("x", MaxLenContactLabel+1)}},
	}))

	// Contact value too long.
	require.Error(t, validateTournamentLengths(&state.Tournament{
		Name:     base.Name,
		Venue:    base.Venue,
		Date:     base.Date,
		Password: base.Password,
		Contacts: []state.TournamentContact{{Label: "ok", Value: strings.Repeat("x", MaxLenContactValue+1)}},
	}))
}

// outcomeEngine is a minimal ScoringEngine that returns a fixed
// AutoCompleteOutcome so tryAutoCompletePools can be tested for each branch.
type outcomeEngine struct {
	stubScoringEngine
	outcome engine.AutoCompleteOutcome
}

func (e outcomeEngine) MaybeAutoCompletePools(string) (engine.AutoCompleteOutcome, error) {
	return e.outcome, nil
}

// TestTryAutoCompletePools_Outcomes exercises every switch-case branch in
// tryAutoCompletePools (except the error path, which is already covered by
// TestTryAutoCompletePools_SanitizesErrorHeader in handlers_match_test.go).
func TestTryAutoCompletePools_Outcomes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name    string
		outcome engine.AutoCompleteOutcome
	}{
		{"Transitioned", engine.AutoCompleteTransitioned},
		{"TiebreakInjected", engine.AutoCompleteTiebreakInjected},
		{"KnockoutStarted", engine.AutoCompleteKnockoutStarted},
		{"PoolsResolved", engine.AutoCompletePoolsResolved},
		{"NoChange", engine.AutoCompleteNoChange},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			hub := stubBroadcaster{}
			eng := outcomeEngine{outcome: tc.outcome}
			assert.NotPanics(t, func() {
				tryAutoCompletePools(c, eng, hub, "comp-1")
			})
		})
	}
}

// failingCompetitorStatusStore is a stub CompetitorStatusStore that always
// returns an error from both methods.
type failingCompetitorStatusStore struct{}

func (failingCompetitorStatusStore) LoadCompetitorStatus(string) (map[string]domain.CompetitorStatus, error) {
	return nil, fmt.Errorf("store unavailable")
}

func (failingCompetitorStatusStore) SetCompetitorStatus(string, domain.CompetitorStatus) error {
	return fmt.Errorf("store unavailable")
}

// jsonBody encodes v as JSON and returns a *bytes.Reader for use as a request body.
func jsonBody(v any) *bytes.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}

// TestRegisterPublicEligibilityHandlers_StoreError covers the
// store.LoadCompetitorStatus error path (75% → 100%).
func TestRegisterPublicEligibilityHandlers_StoreError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterPublicEligibilityHandlers(api, failingCompetitorStatusStore{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/competitions/c1/competitor-status", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, 500, w.Code)
}

// TestRegisterEligibilityHandlers_SetStatusError covers the store error
// path in RegisterEligibilityHandlers (60.7% → higher).
func TestRegisterEligibilityHandlers_SetStatusError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterEligibilityHandlers(admin, failingCompetitorStatusStore{}, stubBroadcaster{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/competitions/c1/competitor-status",
		jsonBody(map[string]any{"playerID": "p1", "eligible": false, "reason": "kiken"}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, 500, w.Code)
}

// ---------------------------------------------------------------------------
// Schedule endpoint — unparsable multiplier/courts + queryIntDefault fallback
// ---------------------------------------------------------------------------

// TestScheduleEstimate_UnparsableMultiplier covers the "multiplier must be a number"
// branch (line 64-67 in handlers_schedule.go).
func TestScheduleEstimate_UnparsableMultiplier(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/schedule/estimate?matchDuration=3&multiplier=abc&courts=2", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "multiplier")
}

// TestScheduleEstimate_UnparsableCourts covers the "courts must be an integer"
// branch (line 73-76 in handlers_schedule.go).
func TestScheduleEstimate_UnparsableCourts(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=abc", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "courts")
}

// TestScheduleEstimate_InvalidOptionalParam covers the queryIntDefault error
// fallback (line 118-120 in handlers_schedule.go) — an unparsable optional
// param silently falls back to the default and the request still returns 200.
func TestScheduleEstimate_InvalidOptionalParam(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&numMatches=abc", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Announcement handler — bad JSON body
// ---------------------------------------------------------------------------

// TestAnnouncement_BadJSON covers the ShouldBindJSON error branch
// (line 25-28 in handlers_announcement.go).
func TestAnnouncement_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "ann-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	hub := NewHub()
	t.Cleanup(hub.Close)
	r := gin.New()
	api := r.Group("/api")
	RegisterAnnouncementHandlers(api, store, hub)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tournament/announce", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Registration GET — invalid comp ID
// ---------------------------------------------------------------------------

// TestRegistration_GET_InvalidCompID covers the requireValidCompID !ok branch
// (line 25-27 in handlers_registration.go).
func TestRegistration_GET_InvalidCompID(t *testing.T) {
	r, _, _, _ := setupRegistrationRouter(t, selfRunTournament())
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/register/competitions/.invalid", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestRegistration_POST_BadJSON covers the ShouldBindJSON error branch
// (line 96-100 in handlers_registration.go).
func TestRegistration_POST_BadJSON(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/register/competitions/c1", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Admin password endpoint — input validation gaps
// ---------------------------------------------------------------------------

// TestAdminPassword_BadJSON covers the ShouldBindJSON error path
// (line 50-53 in handlers_auth_admin.go).
func TestAdminPassword_BadJSON(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))
	req := httptest.NewRequest(http.MethodPut, "/api/auth/admin-password", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminPassword_NewPasswordTooLong covers the validateMaxLen("newPassword")
// error path (line 62-65 in handlers_auth_admin.go).
func TestAdminPassword_NewPasswordTooLong(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))
	w := putAdminPassword(r, map[string]string{"newPassword": strings.Repeat("x", MaxLenTournamentPassword+1)})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestAdminPassword_CurrentPasswordTooLong covers the validateMaxLen("currentPassword")
// error path (line 66-69 in handlers_auth_admin.go).
func TestAdminPassword_CurrentPasswordTooLong(t *testing.T) {
	store := setupVerifierTestStore(t)
	seedTournament(t, store)
	r, _ := elevatedHandlerRouter(t, store, NewFileVerifier(store))
	w := putAdminPassword(r, map[string]string{
		"newPassword":     "valid",
		"currentPassword": strings.Repeat("x", MaxLenTournamentPassword+1),
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Eligibility POST — bad JSON and validate-length error paths
// ---------------------------------------------------------------------------

// TestEligibilityPOST_BadJSON covers the ShouldBindJSON error path
// (lines 100-103 in handlers_eligibility.go).
func TestEligibilityPOST_BadJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterEligibilityHandlers(api, failingCompetitorStatusStore{}, stubBroadcaster{})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/competitions/c1/competitor-status", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestEligibilityPOST_ValidateLengthError covers the Validate() error path
// (lines 104-112 in handlers_eligibility.go) via an over-length playerId.
func TestEligibilityPOST_ValidateLengthError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterEligibilityHandlers(api, failingCompetitorStatusStore{}, stubBroadcaster{})
	body := jsonBody(map[string]any{
		"playerId": strings.Repeat("x", MaxLenEntityID+1),
		"eligible": false,
		"reason":   "kiken",
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/competitions/c1/competitor-status", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Reinstate handler — internal error → 500
// ---------------------------------------------------------------------------

// TestReinstateHandler_InternalError covers the non-ValidationError error path
// (lines 165-166 in handlers_eligibility.go) where the engine returns a plain error.
func TestReinstateHandler_InternalError(t *testing.T) {
	eng := &stubEligibilityEngine{Err: fmt.Errorf("unexpected db failure")}
	r, store := setupReinstateTestRouter(t, eng)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "pw"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))
	req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitors/p1/reinstate", nil)
	req.Header.Set("X-Tournament-Password", "pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// ---------------------------------------------------------------------------
// Viewer — competitions with HasParticipantIDs=true
// ---------------------------------------------------------------------------

// TestViewerCompetitions_HasParticipantIDs covers the hasIDsHint-set branch
// (lines 67-70 in handlers_viewer.go) via GET /api/viewer/competitions and
// (lines 158-161) via GET /api/viewer/competitions/:id.
func TestViewerCompetitions_HasParticipantIDs(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: ""}))
	comp := &state.Competition{
		ID:                "has-ids-comp",
		Name:              "Has IDs Comp",
		HasParticipantIDs: true,
	}
	require.NoError(t, store.SaveCompetition(comp))

	t.Run("list covers HasParticipantIDs branch", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("detail covers HasParticipantIDs branch", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/viewer/competitions/has-ids-comp", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// ---------------------------------------------------------------------------
// Reset handler — bad JSON body
// ---------------------------------------------------------------------------

// TestReset_BadJSON covers the ShouldBindJSON error path
// (lines 160-163 in handlers_reset.go) with no Origin header (same-origin passes).
func TestReset_BadJSON(t *testing.T) {
	dir, err := os.MkdirTemp("", "reset-badjson-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	_, r, hub := setupResetTest(t, NewFileVerifier(store))
	t.Cleanup(hub.Close)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/tournament/reset", bytes.NewBufferString("{invalid"))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Server nil verifier
// ---------------------------------------------------------------------------

// TestNewRouterWithHub_NilVerifier covers the nil-verifier fallback
// (line 60-62 in server.go).
func TestNewRouterWithHub_NilVerifier(t *testing.T) {
	dir, err := os.MkdirTemp("", "nil-verifier-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()
	t.Cleanup(hub.Close)
	res := resources.NewResources(nil, fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html></html>")},
	})
	// nil verifier is allowed — falls back to NewFileVerifier(store)
	r, _ := NewRouterWithHub(store, eng, res, nil, hub)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Display handler — match on different court and non-running match
// ---------------------------------------------------------------------------

// TestCourtLive_MatchOnDifferentCourt covers the !strings.EqualFold(m.Court, court)
// continue branch (line 69-70 in handlers_display.go).
func TestCourtLive_MatchOnDifferentCourt(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A", "B"},
	}))
	comp := &state.Competition{ID: "dc-comp", Status: state.CompStatusPools, Courts: []string{"A", "B"}}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches("dc-comp", []state.MatchResult{
		{ID: "m1", SideA: "P1", SideB: "P2", Status: state.MatchStatusRunning, Court: "A"},
	}))
	// Request court B — match is on court A, so the court-mismatch continue fires.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/B/live", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestCourtLive_MatchNotRunning covers the m.Status != MatchStatusRunning
// continue branch (line 72-73 in handlers_display.go).
func TestCourtLive_MatchNotRunning(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))
	comp := &state.Competition{ID: "nr-comp", Status: state.CompStatusPools, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches("nr-comp", []state.MatchResult{
		{ID: "m1", SideA: "P1", SideB: "P2", Status: state.MatchStatusCompleted, Court: "A"},
	}))
	// Match exists on court A but is completed, not running.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/live", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestCourtLive_HasParticipantIDs covers the comp.HasParticipantIDs hasIDsHint
// branch (lines 80-83 in handlers_display.go) when finding a running match.
func TestCourtLive_HasParticipantIDs(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))
	comp := &state.Competition{
		ID:                "hid-comp",
		Status:            state.CompStatusPools,
		Courts:            []string{"A"},
		HasParticipantIDs: true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches("hid-comp", []state.MatchResult{
		{ID: "m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
	}))
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/live", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// ---------------------------------------------------------------------------
// Swiss generate-round and standings — invalid comp ID
// ---------------------------------------------------------------------------

// TestSwissGenerateRound_InvalidCompID covers the requireValidCompID !ok branch
// (line 50-52 in handlers_swiss.go).
func TestSwissGenerateRound_InvalidCompID(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/.invalid/swiss/generate-round", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSwissStandings_InvalidCompID covers the requireValidCompID !ok branch
// (line 110-112 in handlers_swiss.go).
func TestSwissStandings_InvalidCompID(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/.invalid/swiss/standings", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// Competition POST — validation error paths
// ---------------------------------------------------------------------------

// TestCompetitionPOST_NameTooLong covers the validateCompetitionLengths error path
// (lines 278-281 in handlers_competition.go).
func TestCompetitionPOST_NameTooLong(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	comp := map[string]any{"name": strings.Repeat("x", MaxLenCompetitionName+1)}
	b, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompetitionPOST_PlayerNameTooLong covers the validatePlayerLengths error path
// (lines 287-290 in handlers_competition.go).
func TestCompetitionPOST_PlayerNameTooLong(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	comp := map[string]any{
		"name": "OK",
		"players": []map[string]any{
			{"name": strings.Repeat("x", MaxLenPlayerName+1), "dojo": "Dojo"},
		},
	}
	b, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompetitionPOST_NegativeDuration covers the validateCompetitionDurations
// error path (lines 337-340 in handlers_competition.go).
func TestCompetitionPOST_NegativeDuration(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	comp := map[string]any{"name": "OK", "matchDuration": -1}
	b, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompetitionPOST_InvalidFormat covers the validateCompetitionFormat
// error path (lines 345-348 in handlers_competition.go).
func TestCompetitionPOST_InvalidFormat(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	comp := map[string]any{"name": "OK", "format": "unknown_format"}
	b, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompetitionPOST_InvalidTeamMatchType covers the ValidateTeamMatchType
// error path (lines 358-361 in handlers_competition.go).
func TestCompetitionPOST_InvalidTeamMatchType(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	comp := map[string]any{"name": "OK", "teamMatchType": "unknown_type"}
	b, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompetitionGET_NotFound covers the comp == nil 404 path
// (lines 446-449 in handlers_competition.go).
func TestCompetitionGET_NotFound(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/nonexistent-comp", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------------
// Quick-score — sideA and sideB length validation
// ---------------------------------------------------------------------------

// TestQuickScore_SideATooLong covers the validateMaxLen("sideA") error path
// (lines 330-333 in handlers_match.go).
func TestQuickScore_SideATooLong(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "qs-len", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches("qs-len", []state.MatchResult{
		{ID: "m1", SideA: "T1", SideB: "T2"},
	}))
	body, _ := json.Marshal(map[string]any{
		"sideA":     strings.Repeat("x", MaxLenMatchSide+1),
		"sideB":     "T2",
		"teamAWins": 1, "teamBWins": 0, "draws": 0,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/qs-len/matches/m1/quick-score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestQuickScore_SideBTooLong covers the validateMaxLen("sideB") error path
// (lines 334-337 in handlers_match.go).
func TestQuickScore_SideBTooLong(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "qs-len2", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches("qs-len2", []state.MatchResult{
		{ID: "m1", SideA: "T1", SideB: "T2"},
	}))
	body, _ := json.Marshal(map[string]any{
		"sideA":     "T1",
		"sideB":     strings.Repeat("x", MaxLenMatchSide+1),
		"teamAWins": 1, "teamBWins": 0, "draws": 0,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/qs-len2/matches/m1/quick-score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

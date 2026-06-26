package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupRegistrationRouter builds a minimal Gin engine with only the
// public registration handlers wired up. The store is pre-seeded with
// the tournament passed in (may be nil to skip seeding).
func setupRegistrationRouter(t *testing.T, tour *state.Tournament) (*gin.Engine, *state.Store, *spyBroadcaster, string) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "reg-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	if tour != nil {
		require.NoError(t, store.SaveTournament(tour))
	}

	spy := &spyBroadcaster{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	RegisterPublicRegistrationHandlers(api, store, spy)

	return r, store, spy, tempDir
}

func selfRunTournament() *state.Tournament {
	return &state.Tournament{
		Name:     "Self-Run Test",
		Password: "pw",
		Courts:   []string{"A"},
		Mode:     state.TournamentModeSelfRun,
	}
}

func officiatedTournament() *state.Tournament {
	return &state.Tournament{
		Name:     "Officiated Test",
		Password: "pw",
		Courts:   []string{"A"},
		Mode:     state.TournamentModeOfficiated,
	}
}

// doRegister is a helper that POSTs a registration request and returns the recorder.
func doRegister(r *gin.Engine, compID string, body map[string]any) *httptest.ResponseRecorder {
	b, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/register/competitions/"+compID, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// doGetMeta is a helper that GETs registration metadata.
func doGetMeta(r *gin.Engine, compID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/register/competitions/"+compID, nil)
	r.ServeHTTP(w, req)
	return w
}

// ---------------------------------------------------------------------------
// GET /register/competitions/:id — metadata
// ---------------------------------------------------------------------------

func TestRegistration_GET_SelfRun_ReturnsMetadata(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-get-meta"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             compID,
		Name:           "Open Men",
		Status:         state.CompStatusSetup,
		WithZekkenName: true,
	}))

	w := doGetMeta(r, compID)
	require.Equal(t, http.StatusOK, w.Code)

	var meta map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &meta))
	assert.Equal(t, compID, meta["id"])
	assert.Equal(t, "Open Men", meta["name"])
	assert.Equal(t, true, meta["withZekkenName"])
	assert.Equal(t, string(state.CompStatusSetup), meta["status"])
}

func TestRegistration_GET_Officiated_Returns404(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, officiatedTournament())

	const compID = "comp-off"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Men's",
		Status: state.CompStatusSetup,
	}))

	w := doGetMeta(r, compID)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not available")
}

func TestRegistration_GET_NoTournament_Returns404(t *testing.T) {
	// No tournament seeded.
	r, _, _, _ := setupRegistrationRouter(t, nil)
	w := doGetMeta(r, "any-comp")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegistration_GET_CompNotFound_Returns404(t *testing.T) {
	r, _, _, _ := setupRegistrationRouter(t, selfRunTournament())
	w := doGetMeta(r, "nonexistent")
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestRegistration_GET_TeamComp_Returns404(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-team-get"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Team Comp",
		Status: state.CompStatusSetup,
		Kind:   "team",
	}))

	w := doGetMeta(r, compID)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not available")
}

func TestRegistration_POST_TeamComp_Returns404(t *testing.T) {
	r, store, spy, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-team-post"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Team Comp",
		Status: state.CompStatusSetup,
		Kind:   "team",
	}))

	w := doRegister(r, compID, map[string]any{
		"name": "Alice", "dojo": "Dojo",
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not available")
	assert.Equal(t, 0, spy.count())
}

func TestRegistration_POST_WhitespaceDanGrade_NotPersisted(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-ws-dan"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Whitespace Dan",
		Status: state.CompStatusSetup,
	}))

	w := doRegister(r, compID, map[string]any{
		"name": "Bob Smith", "dojo": "Dojo", "danGrade": "   ",
	})
	require.Equal(t, http.StatusOK, w.Code)

	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Empty(t, players[0].Metadata, "whitespace-only danGrade should not persist")
}

// ---------------------------------------------------------------------------
// POST /register/competitions/:id — happy path
// ---------------------------------------------------------------------------

func TestRegistration_POST_SelfRun_Setup_CreatesParticipant(t *testing.T) {
	r, store, spy, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-reg-happy"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Open Men",
		Status: state.CompStatusSetup,
	}))

	w := doRegister(r, compID, map[string]any{
		"name":     "Alice Tanaka",
		"dojo":     "Raizan",
		"danGrade": "3 Dan",
	})

	require.Equal(t, http.StatusOK, w.Code)

	var player domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &player))
	assert.Equal(t, "Alice Tanaka", player.Name)
	assert.Equal(t, "Raizan", player.Dojo)
	assert.Equal(t, "registered", player.Source)
	assert.NotEmpty(t, player.ID)

	// Verify persisted
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "registered", players[0].Source)
	assert.Equal(t, []string{"3 Dan"}, players[0].Metadata)

	// Verify broadcast
	assert.Equal(t, 1, spy.count(), "one EventParticipantsUpdated broadcast expected")
}

func TestRegistration_POST_SelfRun_ZekkenComp_PersistsDisplayName(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-zekken-reg"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             compID,
		Name:           "Naginata Women",
		Status:         state.CompStatusSetup,
		WithZekkenName: true,
	}))

	w := doRegister(r, compID, map[string]any{
		"name":        "Yuki Sato",
		"dojo":        "Gyokusen",
		"displayName": "SATO",
	})

	require.Equal(t, http.StatusOK, w.Code)

	players, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "SATO", players[0].DisplayName)
}

func TestRegistration_POST_NonZekkenComp_DisplayNameStripped(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-no-zekken-reg"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             compID,
		Name:           "Men's Individual",
		Status:         state.CompStatusSetup,
		WithZekkenName: false,
	}))

	w := doRegister(r, compID, map[string]any{
		"name":        "Kenji Smith",
		"dojo":        "Suigetsu",
		"displayName": "SMITH", // should be stripped
	})

	require.Equal(t, http.StatusOK, w.Code)

	// Reload without zekken — displayName should NOT be persisted as-is
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	// Stored displayName should be auto-derived (SanitizeName), not the raw "SMITH"
	assert.NotEqual(t, "SMITH", players[0].DisplayName,
		"operator-supplied displayName must be stripped for non-zekken comps")
}

// ---------------------------------------------------------------------------
// POST — officiated mode → 404
// ---------------------------------------------------------------------------

func TestRegistration_POST_Officiated_Returns404(t *testing.T) {
	r, store, spy, _ := setupRegistrationRouter(t, officiatedTournament())

	const compID = "comp-off-post"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Men's",
		Status: state.CompStatusSetup,
	}))

	w := doRegister(r, compID, map[string]any{
		"name": "Bob", "dojo": "Dojo",
	})

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "not available")
	assert.Equal(t, 0, spy.count(), "no broadcast when registration rejected")
}

// ---------------------------------------------------------------------------
// POST — competition not in setup → 409
// ---------------------------------------------------------------------------

func TestRegistration_POST_CompNotInSetup_Returns409(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	for _, tc := range []struct {
		status state.CompetitionStatus
		label  string
	}{
		{state.CompStatusPools, "pools"},
		{state.CompStatusPlayoffs, "playoffs"},
		{state.CompStatusComplete, "complete"},
	} {
		t.Run(tc.label, func(t *testing.T) {
			compID := "comp-started-" + tc.label
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:     compID,
				Name:   "Started " + tc.label,
				Status: tc.status,
			}))

			w := doRegister(r, compID, map[string]any{
				"name": "Alice", "dojo": "Dojo",
			})

			assert.Equal(t, http.StatusConflict, w.Code)
			assert.Contains(t, w.Body.String(), "closed")
		})
	}
}

// ---------------------------------------------------------------------------
// POST — duplicate name → 409 with user-friendly message
// ---------------------------------------------------------------------------

func TestRegistration_POST_DuplicateName_Returns409WithFriendlyMessage(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-dup-reg"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Duplicate Test",
		Status: state.CompStatusSetup,
	}))

	// Register Alice once.
	w1 := doRegister(r, compID, map[string]any{
		"name": "Alice Yamamoto", "dojo": "Raizan",
	})
	require.Equal(t, http.StatusOK, w1.Code)

	// Try to register Alice again with the SAME dojo — same (name,dojo) pair
	// is a duplicate under the new name+dojo dedup key.
	w2 := doRegister(r, compID, map[string]any{
		"name": "Alice Yamamoto", "dojo": "Raizan",
	})
	assert.Equal(t, http.StatusConflict, w2.Code)
	// Friendly message (not the raw internal error).
	assert.Contains(t, w2.Body.String(), "already registered")
	assert.Contains(t, w2.Body.String(), "no action needed")
}

// ---------------------------------------------------------------------------
// POST — missing required fields → 400
// ---------------------------------------------------------------------------

func TestRegistration_POST_MissingRequiredFields(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-missing-fields"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Missing Fields Test",
		Status: state.CompStatusSetup,
	}))

	cases := []struct {
		desc string
		body map[string]any
	}{
		{"missing name", map[string]any{"dojo": "Dojo A"}},
		{"blank name", map[string]any{"name": "   ", "dojo": "Dojo A"}},
		{"missing dojo", map[string]any{"name": "Alice"}},
		{"blank dojo", map[string]any{"name": "Alice", "dojo": "   "}},
		{"both missing", map[string]any{}},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := doRegister(r, compID, tc.body)
			assert.Equalf(t, http.StatusBadRequest, w.Code, "expected 400 for: %s", tc.desc)
		})
	}
}

// ---------------------------------------------------------------------------
// POST — field length validation → 400
// ---------------------------------------------------------------------------

func TestRegistration_POST_FieldLengthValidation(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-length-val"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             compID,
		Name:           "Length Validation Test",
		Status:         state.CompStatusSetup,
		WithZekkenName: true,
	}))

	cases := []struct {
		desc string
		body map[string]any
	}{
		{
			"name over cap",
			map[string]any{
				"name": strings.Repeat("n", MaxLenPlayerName+1),
				"dojo": "Dojo",
			},
		},
		{
			"dojo over cap",
			map[string]any{
				"name": "Alice",
				"dojo": strings.Repeat("d", MaxLenPlayerDojo+1),
			},
		},
		{
			"displayName over cap",
			map[string]any{
				"name":        "Alice",
				"dojo":        "Dojo",
				"displayName": strings.Repeat("x", MaxLenPlayerDisplayName+1),
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.desc, func(t *testing.T) {
			w := doRegister(r, compID, tc.body)
			assert.Equalf(t, http.StatusBadRequest, w.Code, "expected 400 for: %s", tc.desc)
		})
	}
}

// ---------------------------------------------------------------------------
// POST — path traversal defense
// ---------------------------------------------------------------------------

func TestRegistration_POST_InvalidCompID_Returns400(t *testing.T) {
	r, _, _, _ := setupRegistrationRouter(t, selfRunTournament())

	for _, tc := range []struct {
		id   string
		desc string
	}{
		{".invalid", "dot-prefixed ID rejected by alphanumeric regex"},
		{"has%20spaces", "URL-encoded spaces rejected by alphanumeric regex"},
		{"a@b", "special char rejected by alphanumeric regex"},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/register/competitions/"+tc.id, bytes.NewBufferString(`{"name":"A","dojo":"D"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"invalid comp ID %q must return 400, got %d", tc.id, w.Code)
		})
	}
}

// ---------------------------------------------------------------------------
// POST — competition not found → 404
// ---------------------------------------------------------------------------

func TestRegistration_POST_CompNotFound_Returns404(t *testing.T) {
	r, _, _, _ := setupRegistrationRouter(t, selfRunTournament())

	w := doRegister(r, "nonexistent-comp", map[string]any{
		"name": "Alice", "dojo": "Dojo",
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ---------------------------------------------------------------------------
// POST — source is always "registered" regardless of caller
// ---------------------------------------------------------------------------

func TestRegistration_POST_SourceAlwaysRegistered(t *testing.T) {
	r, store, _, _ := setupRegistrationRouter(t, selfRunTournament())

	const compID = "comp-source-check"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Source Check",
		Status: state.CompStatusSetup,
	}))

	w := doRegister(r, compID, map[string]any{
		"name": "Bob", "dojo": "Dojo B",
		// Even if a caller somehow included a 'source' field, our handler
		// hardcodes "registered" and doesn't read it.
	})
	require.Equal(t, http.StatusOK, w.Code)

	var player domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &player))
	assert.Equal(t, "registered", player.Source)
}

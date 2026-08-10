package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-draw R9 UAT gap 3: shrinking the tournament's shiaijo count orphaned a
// competition's courts. The competition kept the removed shiaijo, the draw
// scheduled matches onto it, and no operator view existed for it.
//
// The chosen mechanism is REFUSAL, not pruning: dropping a court a competition
// is actively using mid-event is the worst of the three options, and pruning
// on read leaves the stored value (which the draw uses) wrong. The refusal
// names the blocking competitions so the operator can fix them in one pass.

// membershipPutTournament PUTs a tournament body and returns the recorder.
func membershipPutTournament(t *testing.T, r *gin.Engine, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// membershipVenue saves a 4-shiaijo tournament plus one competition using
// the courts given.
func membershipVenue(t *testing.T, store *state.Store, compCourts []string, status state.CompetitionStatus) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Venue Cup", Password: "pw", Courts: []string{"A", "B", "C", "D"},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "mudansha", Name: "Mudansha", Kind: "individual", Format: state.CompFormatPlayoffs,
		Courts: compCourts, StartTime: "09:00", Status: status,
	}))
}

func TestPutTournamentRefusesToOrphanACompetitionsShiaijo(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	membershipVenue(t, store, []string{"A", "B", "C", "D"}, state.CompStatusSetup)

	w := membershipPutTournament(t, r, map[string]any{
		"name": "Venue Cup", "password": "pw", "courts": []string{"A", "B", "C"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "cannot set the tournament's shiaijo to A, B, C")
	assert.Contains(t, w.Body.String(), "Mudansha")
	assert.Contains(t, w.Body.String(), "still runs on shiaijo D")

	// Nothing changed on disk: the operator's venue keeps its 4 shiaijo, and
	// the competition keeps the allocation it is running on.
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C", "D"}, tourn.Courts)
	comp, err := store.LoadCompetition("mudansha")
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C", "D"}, comp.Courts,
		"the competition's courts must never be pruned behind the operator's back")
}

func TestPutTournamentAllowsAShrinkNothingDependsOn(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	membershipVenue(t, store, []string{"A", "B"}, state.CompStatusSetup)

	w := membershipPutTournament(t, r, map[string]any{
		"name": "Venue Cup", "password": "pw", "courts": []string{"A", "B", "C"},
	})

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, []string{"A", "B", "C"}, tourn.Courts)
}

func TestPutTournamentIgnoresFinishedCompetitions(t *testing.T) {
	// A completed or invalidated competition is history: no new draw or
	// schedule can be built for it. Blocking on one would wedge the common
	// multi-day case (morning divisions on 4 shiaijo, afternoon on 2) with no
	// remedy but deleting finished results.
	for _, status := range []state.CompetitionStatus{state.CompStatusComplete, state.CompStatusInvalid} {
		t.Run(string(status), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			membershipVenue(t, store, []string{"A", "B", "C", "D"}, status)

			w := membershipPutTournament(t, r, map[string]any{
				"name": "Venue Cup", "password": "pw", "courts": []string{"A", "B"},
			})
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		})
	}
}

func TestPutTournamentNamesEveryBlocker(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	membershipVenue(t, store, []string{"A", "C"}, state.CompStatusSetup)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "yudansha", Name: "Yudansha", Kind: "individual", Format: state.CompFormatLeague,
		Courts: []string{"D"}, StartTime: "13:00", Status: state.CompStatusPools,
	}))

	w := membershipPutTournament(t, r, map[string]any{
		"name": "Venue Cup", "password": "pw", "courts": []string{"A", "B"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, "Mudansha")
	assert.Contains(t, body, "Yudansha")
	assert.Contains(t, body, "shiaijo C")
	assert.Contains(t, body, "shiaijo D")
}

func TestPostTournamentRefusesToOrphanACompetitionsShiaijo(t *testing.T) {
	// POST is not create-only (the SPA re-POSTs against an existing record),
	// so the same guard has to sit on both write paths.
	r, store, _, _, _ := setupTestRouter(t)
	membershipVenue(t, store, []string{"A", "B", "C", "D"}, state.CompStatusSetup)

	b, err := json.Marshal(map[string]any{
		"name": "Venue Cup", "password": "pw", "courts": []string{"A", "B"},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "pw")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "still runs on shiaijo")
}

// competitionsBlockingCourtRemoval is the pure half; table-drive it so the
// carve-outs are pinned independently of the HTTP wiring.
func TestCompetitionsBlockingCourtRemoval(t *testing.T) {
	comp := func(name string, courts []string, status state.CompetitionStatus) *state.Competition {
		return &state.Competition{ID: name, Name: name, Courts: courts, Status: status}
	}
	tests := []struct {
		desc    string
		comps   []*state.Competition
		courts  []string
		blocked bool
	}{
		{"no competitions", nil, []string{"A"}, false},
		{"nil entry is skipped", []*state.Competition{nil}, []string{"A"}, false},
		{"subset is fine", []*state.Competition{comp("X", []string{"A"}, state.CompStatusSetup)}, []string{"A", "B"}, false},
		{"orphaned live competition blocks", []*state.Competition{comp("X", []string{"A", "B"}, state.CompStatusSetup)}, []string{"A"}, true},
		{"draw-ready blocks", []*state.Competition{comp("X", []string{"B"}, state.CompStatusDrawReady)}, []string{"A"}, true},
		{"running blocks", []*state.Competition{comp("X", []string{"B"}, state.CompStatusPlayoffs)}, []string{"A"}, true},
		{"completed does not block", []*state.Competition{comp("X", []string{"B"}, state.CompStatusComplete)}, []string{"A"}, false},
		{"invalid does not block", []*state.Competition{comp("X", []string{"B"}, state.CompStatusInvalid)}, []string{"A"}, false},
		// An empty allocation means "inherit the tournament's courts", which
		// follows the tournament by definition and can never be orphaned.
		{"inheriting competition does not block", []*state.Competition{comp("X", nil, state.CompStatusSetup)}, []string{"A"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := competitionsBlockingCourtRemoval(tc.comps, tc.courts)
			if tc.blocked {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestCompetitionsBlockingCourtRemovalFallsBackToTheID(t *testing.T) {
	// A record with no display name still has to be identifiable in the
	// message, otherwise the operator is told to fix "".
	err := competitionsBlockingCourtRemoval(
		[]*state.Competition{{ID: "mudansha", Courts: []string{"D"}, Status: state.CompStatusSetup}},
		[]string{"A"},
	)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mudansha")
}

func TestCreateCompetitionRejectsShiaijoTheTournamentLacks(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Venue Cup", Courts: []string{"A", "B"}}))

	w := pairingPostComp(t, r, map[string]any{
		"name":   "Ghost Courts",
		"format": "mixed",
		"courts": []string{"C", "D"},
	}, "")

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "shiaijo C, D are not part of this tournament")
}

func TestUpdateCompetitionRejectsAChangeOntoAMissingShiaijo(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Venue Cup", Courts: []string{"A", "B"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "mudansha", Name: "Mudansha", Kind: "individual", Format: state.CompFormatPlayoffs,
		Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
	}))

	w := pairingPutComp(t, r, "mudansha", map[string]any{
		"name": "Mudansha", "format": "playoffs", "startTime": "09:00",
		"courts": []string{"C", "D"},
	})

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "not part of this tournament")
}

func TestUpdateCompetitionStaysEditableWhileOrphaned(t *testing.T) {
	// The competition holding an orphaned court must stay editable: that
	// screen is the operator's route back to a valid allocation, and the
	// settings form renders the orphan as a deselectable pill. Only a CHANGE
	// that still names a missing shiaijo is refused, exactly like the
	// pairing rule's write-time behaviour.
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Venue Cup", Courts: []string{"A", "B", "C"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "mudansha", Name: "Mudansha", Kind: "individual", Format: state.CompFormatPlayoffs,
		Courts: []string{"A", "B", "C", "D"}, StartTime: "09:00", Status: state.CompStatusSetup,
	}))

	t.Run("an unrelated edit succeeds", func(t *testing.T) {
		w := pairingPutComp(t, r, "mudansha", map[string]any{
			"name": "Mudansha Renamed", "format": "playoffs", "startTime": "10:00",
			"courts": []string{"A", "B", "C", "D"},
		})
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})

	t.Run("dropping the orphan succeeds", func(t *testing.T) {
		w := pairingPutComp(t, r, "mudansha", map[string]any{
			"name": "Mudansha Renamed", "format": "playoffs", "startTime": "10:00",
			"courts": []string{"A", "B"},
		})
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		comp, err := store.LoadCompetition("mudansha")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, comp.Courts)
	})
}

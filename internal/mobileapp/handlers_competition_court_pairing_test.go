package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pairingCourts returns n single-character court labels, A, B, C, ...
func pairingCourts(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = string(rune('A' + i))
	}
	return out
}

// pairingPostComp POSTs a competition body and returns the recorder. password
// is sent as the admin header when non-empty (the venue-total test configures
// one; the rest run against a password-less tournament).
func pairingPostComp(t *testing.T, r *gin.Engine, body map[string]any, password string) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	r.ServeHTTP(w, req)
	return w
}

// pairingPutComp PUTs a settings-only body (no players field) and returns the
// recorder.
func pairingPutComp(t *testing.T, r *gin.Engine, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+id, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// TestCreateCompetitionCourtPairing sweeps the shiaijo-count rule on the
// create path: a competition that draws a bracket takes 1 shiaijo or an even
// number, and an odd count above 1 is a 400 naming the nearest valid counts
// (and 1, so the rule never reads as "at least 2 courts").
func TestCreateCompetitionCourtPairing(t *testing.T) {
	for n := 1; n <= 8; n++ {
		valid := n == 1 || n%2 == 0
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store)

			w := pairingPostComp(t, r, map[string]any{
				"name":   fmt.Sprintf("Comp %d", n),
				"format": "mixed",
				"courts": pairingCourts(n),
			}, "")
			if valid {
				require.Equalf(t, http.StatusCreated, w.Code, "%d shiaijo must be accepted: %s", n, w.Body.String())
				return
			}
			require.Equalf(t, http.StatusBadRequest, w.Code, "%d shiaijo must be rejected", n)
			assert.Contains(t, w.Body.String(), "courts must be 1 or an even number")
			assert.Contains(t, w.Body.String(), fmt.Sprintf("use %d or %d, or 1", n-1, n+1))
		})
	}
}

// TestCreateCompetitionCourtPairingScope pins that league and Swiss are out
// of scope: their courts are parallel mats, not bracket regions.
func TestCreateCompetitionCourtPairingScope(t *testing.T) {
	for _, format := range []string{"league", "swiss"} {
		t.Run(format+" accepts 3 shiaijo", func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store)
			body := map[string]any{
				"name":   "Odd " + format,
				"format": format,
				"courts": []string{"A", "B", "C"},
			}
			if format == "swiss" {
				body["swissRounds"] = 3
			}
			w := pairingPostComp(t, r, body, "")
			require.Equalf(t, http.StatusCreated, w.Code, "resp: %s", w.Body.String())
		})
	}
}

// TestTournamentCourtsMayBeOdd pins the boundary of the rule: it constrains a
// COMPETITION's allocation, never the venue. A 5-shiaijo tournament is legal
// and simply splits its courts across competitions, 4 + 1.
func TestTournamentCourtsMayBeOdd(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tournament",
		bytes.NewBufferString(`{"name":"Five Court Cup","password":"secret","courts":["A","B","C","D","E"]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "a 5-shiaijo venue must be accepted: %s", w.Body.String())

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.Len(t, tourn.Courts, 5)

	// 4 + 1 across two competitions is the intended shape, and both are legal.
	for _, tc := range []struct {
		name   string
		courts []string
	}{
		{"Big Comp", []string{"A", "B", "C", "D"}},
		{"Small Comp", []string{"E"}},
	} {
		w := pairingPostComp(t, r, map[string]any{
			"name": tc.name, "format": "mixed", "courts": tc.courts,
		}, "secret")
		require.Equalf(t, http.StatusCreated, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// TestUpdateCompetitionCourtPairing covers the PUT nuance that the whole
// design turns on: existing data is validated on WRITE only.
func TestUpdateCompetitionCourtPairing(t *testing.T) {
	// saveLegacyOddComp writes a competition with an unpairable allocation
	// straight to disk, standing in for a record saved before the rule
	// existed (or one that inherited an odd venue court list).
	saveLegacyOddComp := func(t *testing.T, store *state.Store, id string) {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:           id,
			Name:         "Legacy Odd",
			Kind:         "individual",
			Format:       "mixed",
			PoolSize:     4,
			PoolWinners:  2,
			PoolSizeMode: "min",
			Courts:       []string{"A", "B", "C"},
			StartTime:    "09:00",
			Status:       state.CompStatusSetup,
		}))
	}

	t.Run("unrelated edit leaving an odd allocation unchanged succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		saveLegacyOddComp(t, store, "legacy-odd")

		// The operator renames the competition and changes its start time.
		// Courts are round-tripped untouched, which is exactly what the real
		// settings screen PUTs. Blocking this would lock them out of every
		// unrelated edit on a competition that is already running.
		w := pairingPutComp(t, r, "legacy-odd", map[string]any{
			"id":        "legacy-odd",
			"name":      "Legacy Odd Renamed",
			"format":    "mixed",
			"courts":    []string{"A", "B", "C"},
			"startTime": "10:30",
			"poolSize":  4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-odd")
		require.NoError(t, err)
		assert.Equal(t, "Legacy Odd Renamed", got.Name, "the unrelated edit must land")
		assert.Equal(t, "10:30", got.StartTime)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts, "the allocation is preserved as-is")
	})

	t.Run("changing to another odd allocation is rejected", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		saveLegacyOddComp(t, store, "legacy-odd-2")

		w := pairingPutComp(t, r, "legacy-odd-2", map[string]any{
			"id": "legacy-odd-2", "name": "Legacy Odd", "format": "mixed",
			"courts": []string{"A", "B", "C", "D", "E"}, "poolSize": 4,
		})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "use 4 or 6, or 1")

		got, err := store.LoadCompetition("legacy-odd-2")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts, "the rejected write must not land")
	})

	t.Run("fixing an odd allocation to an even one succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		saveLegacyOddComp(t, store, "legacy-odd-3")

		w := pairingPutComp(t, r, "legacy-odd-3", map[string]any{
			"id": "legacy-odd-3", "name": "Legacy Odd", "format": "mixed",
			"courts": []string{"A", "B"}, "poolSize": 4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-odd-3")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, got.Courts)
	})

	t.Run("sweep: changing the allocation must land on a valid count", func(t *testing.T) {
		for n := 1; n <= 8; n++ {
			valid := n == 1 || n%2 == 0
			t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
				r, store, _, _, _ := setupTestRouter(t)
				saveTournamentCourts(t, store)
				// Start from an even allocation so every case below is a change.
				require.NoError(t, store.SaveCompetition(&state.Competition{
					ID: "sweep", Name: "Sweep", Kind: "individual", Format: "mixed",
					PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
					Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
				}))

				w := pairingPutComp(t, r, "sweep", map[string]any{
					"id": "sweep", "name": "Sweep", "format": "mixed",
					"courts": pairingCourts(n), "poolSize": 4,
				})
				if valid {
					require.Equalf(t, http.StatusOK, w.Code, "%d shiaijo: %s", n, w.Body.String())
					return
				}
				require.Equalf(t, http.StatusBadRequest, w.Code, "%d shiaijo must be rejected", n)
				assert.Contains(t, w.Body.String(), fmt.Sprintf("use %d or %d, or 1", n-1, n+1))
			})
		}
	})

	// The rule is scoped by format, so a format change can make a
	// stored-and-valid allocation unpairable without the court list moving at
	// all. The trigger has to watch both fields or this slips through and the
	// operator ends up with a competition that silently cannot draw.
	t.Run("switching a 3-shiaijo league to a bracket format is rejected", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "league-to-mixed", Name: "Was League", Kind: "individual", Format: "league",
			PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
			Courts: []string{"A", "B", "C"}, StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := pairingPutComp(t, r, "league-to-mixed", map[string]any{
			"id": "league-to-mixed", "name": "Was League", "format": "mixed",
			"courts": []string{"A", "B", "C"}, "poolSize": 4,
		})
		require.Equalf(t, http.StatusBadRequest, w.Code, "resp: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "use 2 or 4, or 1")

		got, err := store.LoadCompetition("league-to-mixed")
		require.NoError(t, err)
		assert.Equal(t, "league", got.Format, "the rejected write must not land")
	})

	// The mirror image: dropping a bracket format is how an operator legitimately
	// keeps an odd allocation, so it must be allowed.
	t.Run("switching a 3-shiaijo mixed competition to league succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		saveLegacyOddComp(t, store, "mixed-to-league")

		w := pairingPutComp(t, r, "mixed-to-league", map[string]any{
			"id": "mixed-to-league", "name": "Legacy Odd", "format": "league",
			"courts": []string{"A", "B", "C"}, "poolSize": 4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("mixed-to-league")
		require.NoError(t, err)
		assert.Equal(t, "league", got.Format)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts)
	})

	t.Run("league keeps an odd allocation editable and changeable", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "league-put", Name: "League", Kind: "individual", Format: "league",
			PoolSize: 6, PoolWinners: 1, PoolSizeMode: "min",
			Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := pairingPutComp(t, r, "league-put", map[string]any{
			"id": "league-put", "name": "League", "format": "league",
			"courts": []string{"A", "B", "C"}, "poolSize": 6,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("league-put")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts)
	})
}

// saveTournamentCourts seeds an 8-shiaijo venue (A..H) so every competition
// court count in these tests has real labels to pick from. Written straight to
// the store, with no password, so the admin requests under test need no auth
// header (same pattern as TestCompetitionCourtsInvariant).
func saveTournamentCourts(t *testing.T, store *state.Store) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Pairing Cup", Courts: pairingCourts(8),
	}))
}

package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// An incomplete seeding is refused at every boundary that takes one as a
// FINISHED artefact, not only at the draw.
//
// The asymmetry with PUT /competitions/:id is deliberate and is pinned by
// TestRosterPutStillAcceptsAHalfTypedSeeding below: that endpoint carries the
// roster and the admin console sends it on every keystroke in the seeding
// panel, so refusing it would make typing a 4th seed before a 1st impossible.
// This one carries a seeding on its own, and there is nothing half-typed to
// protect.
func seedEnforcementFixture(t *testing.T) (*state.Store, func(string, string, []byte) *httptest.ResponseRecorder) {
	t.Helper()
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2,
	}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"},
		{Name: "Carol", Dojo: "E"}, {Name: "Dave", Dojo: "E"},
	}))
	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, err := http.NewRequest(method, path, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	return store, do
}

func TestPutSeedsRefusesAnIncompleteSeeding(t *testing.T) {
	cases := []struct {
		name        string
		seeds       []domain.SeedAssignment
		wantMessage []string
		wantAbsent  string
	}{
		{
			name:        "typing seed 4 first",
			seeds:       []domain.SeedAssignment{{Name: "Dave", SeedRank: 4}},
			wantMessage: []string{"seed ranks 1, 2 and 3 have not been set", "rank 4 has", "Send the complete seeding"},
		},
		{
			name:        "one rank skipped",
			seeds:       []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}, {Name: "Carol", SeedRank: 3}},
			wantMessage: []string{"seed rank 2 has not been set", "rank 3 has"},
		},
		{
			// A duplicate is refused too, but it is not a gap: the message must
			// keep the validator's precise words rather than invent a missing
			// rank for it.
			name:        "duplicate rank keeps its own reason",
			seeds:       []domain.SeedAssignment{{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 1}},
			wantMessage: []string{"duplicate seed rank"},
			wantAbsent:  "have not been set",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, do := seedEnforcementFixture(t)
			body, err := json.Marshal(tc.seeds)
			require.NoError(t, err)

			w := do("PUT", "/api/competitions/c1/seeds", body)
			require.Equal(t, http.StatusBadRequest, w.Code,
				"an incomplete seeding must be refused with a reason, not stored: %s", w.Body.String())

			var got map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
			for _, want := range tc.wantMessage {
				assert.Contains(t, got["error"], want)
			}
			if tc.wantAbsent != "" {
				assert.NotContains(t, got["error"], tc.wantAbsent)
			}

			// Refused means NOT WRITTEN. A 400 over a file that was updated
			// anyway is the worst of both.
			stored, err := store.LoadSeedsRaw("c1")
			require.NoError(t, err)
			assert.Empty(t, stored, "a refused seeding must not reach seeds.csv")
		})
	}
}

func TestPutSeedsAcceptsACompleteSeedingAndAnEmptyOne(t *testing.T) {
	store, do := seedEnforcementFixture(t)

	complete, err := json.Marshal([]domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 2},
	})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, do("PUT", "/api/competitions/c1/seeds", complete).Code)
	stored, err := store.LoadSeeds("c1")
	require.NoError(t, err)
	assert.Len(t, stored, 2)

	// The remedy the refusal offers ("or an empty list to clear it") has to
	// actually work, or the message sends the operator into a second refusal.
	require.Equal(t, http.StatusOK, do("PUT", "/api/competitions/c1/seeds", []byte(`[]`)).Code)
	stored, err = store.LoadSeeds("c1")
	require.NoError(t, err)
	assert.Empty(t, stored)
}

// The keystroke path stays open, ON PURPOSE.
//
// The seeding panel PUTs the whole roster every time a rank is typed, so an
// operator entering their 4th seed first sends {4} and would be refused before
// they could type the 1st. What that path owes the operator instead is that the
// rank comes BACK (so the console can warn about it) and that nothing draws
// with it, both of which are pinned elsewhere:
// state.TestIncompleteSeedingStaysVisibleToTheOperator and
// engine.TestGenerateDrawReportsMalformedSeedsAsValidation.
func TestRosterPutStillAcceptsAHalfTypedSeeding(t *testing.T) {
	store, do := seedEnforcementFixture(t)

	body, err := json.Marshal(map[string]any{
		"id":     "c1",
		"name":   "C1",
		"format": string(state.CompFormatMixed),
		"players": []map[string]any{
			{"name": "Alice", "dojo": "D"}, {"name": "Bob", "dojo": "D"},
			{"name": "Carol", "dojo": "E"}, {"name": "Dave", "dojo": "E", "seed": 4},
		},
	})
	require.NoError(t, err)

	w := do("PUT", "/api/competitions/c1", body)
	require.Equal(t, http.StatusOK, w.Code,
		"refusing here would make it impossible to type a 4th seed before a 1st: %s", w.Body.String())

	stored, err := store.LoadSeedsRaw("c1")
	require.NoError(t, err)
	assert.Equal(t, []domain.SeedAssignment{{Name: "Dave", SeedRank: 4}}, stored,
		"the typed rank must persist, or the console shows 0 seeded and cannot warn")
}

// A rank assigned to nobody is not a seeding.
//
// The ranks are validated as a SET (contiguous from 1, no duplicates) and that
// check never looked at the roster, so a name with no participant behind it
// stored cleanly -- and then the same seeding read two different ways. seeds.csv
// held a valid 1..N and the draw ran; every reader that merges seeds onto
// players by name saw only the survivors, read the ghost's rank as a gap, and
// the console disabled "Generate draw" with a message naming a rank no row owned
// and no edit could close.
func TestPutSeedsRefusesARankForSomeoneNotOnTheRoster(t *testing.T) {
	t.Run("a ghost name is refused and named", func(t *testing.T) {
		store, do := seedEnforcementFixture(t)
		body, err := json.Marshal([]domain.SeedAssignment{
			{Name: "Alice", SeedRank: 1},
			{Name: "Ghost", SeedRank: 2},
			{Name: "Carol", SeedRank: 3},
		})
		require.NoError(t, err)

		w := do("PUT", "/api/competitions/c1/seeds", body)
		require.Equal(t, http.StatusBadRequest, w.Code,
			"a seeding whose ranks are contiguous but reference nobody must still be refused: %s", w.Body.String())

		var got map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		assert.Contains(t, got["error"], `"Ghost"`, "the operator has to be told WHICH name has no participant")
		assert.Contains(t, got["error"], "rank 2")
		assert.NotContains(t, got["error"], "Alice", "a name that IS on the roster must not be blamed")

		stored, err := store.LoadSeedsRaw("c1")
		require.NoError(t, err)
		assert.Empty(t, stored, "a refused seeding must not reach seeds.csv")
	})

	t.Run("every name on the roster still saves", func(t *testing.T) {
		store, do := seedEnforcementFixture(t)
		body, err := json.Marshal([]domain.SeedAssignment{
			{Name: "Alice", SeedRank: 1}, {Name: "Bob", SeedRank: 2},
		})
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, do("PUT", "/api/competitions/c1/seeds", body).Code)
		stored, err := store.LoadSeeds("c1")
		require.NoError(t, err)
		assert.Len(t, stored, 2, "the guard must not refuse an ordinary seeding")
	})

	t.Run("no roster yet is not every name being a ghost", func(t *testing.T) {
		// Seeds saved before participants are written have nothing to
		// contradict them, so the roster check stays out of the way and the
		// draw's own validation remains the backstop.
		r, store, _, _, _ := setupTestRouter(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "empty", Name: "Empty", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2,
		}))
		body, err := json.Marshal([]domain.SeedAssignment{{Name: "Alice", SeedRank: 1}})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		req, err := http.NewRequest("PUT", "/api/competitions/empty/seeds", bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "an empty roster must not turn every seed into a ghost: %s", w.Body.String())
	})
}

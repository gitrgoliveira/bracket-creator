package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shiaijoLabels returns n single-character court labels, A, B, C, ...
func shiaijoLabels(n int) []string {
	out := make([]string, n)
	for i := range n {
		out[i] = helper.CourtLabel(i)
	}
	return out
}

// shiaijoPostComp POSTs a competition body and returns the recorder. password
// is sent as the admin header when non-empty (the venue-total test configures
// one; the rest run against a password-less tournament).
func shiaijoPostComp(t *testing.T, r *gin.Engine, body map[string]any, password string) *httptest.ResponseRecorder {
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

// shiaijoPutComp PUTs a settings-only body (no players field) and returns the
// recorder.
func shiaijoPutComp(t *testing.T, r *gin.Engine, id string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+id, bytes.NewBuffer(b))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// TestCreateCompetitionShiaijoCount sweeps the shiaijo-count rule on the
// create path across 1..17: a competition that draws a bracket takes a power
// of two (1, 2, 4, 8 or 16), and everything else is a 400 naming the nearest
// valid counts (and 1, so the rule never reads as "at least 2 courts"). The
// sweep spans the counts the retired "1 or an even number" rule wrongly
// ACCEPTED (6, 10, 12, 14) as well as the odd ones.
func TestCreateCompetitionShiaijoCount(t *testing.T) {
	for n := 1; n <= 17; n++ {
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store, 20)

			w := shiaijoPostComp(t, r, map[string]any{
				"name":   fmt.Sprintf("Comp %d", n),
				"format": "mixed",
				"courts": shiaijoLabels(n),
			}, "")
			if bctest.LegalShiaijoCount(n) {
				require.Equalf(t, http.StatusCreated, w.Code, "%d shiaijo must be accepted: %s", n, w.Body.String())
				return
			}
			require.Equalf(t, http.StatusBadRequest, w.Code, "%d shiaijo must be rejected", n)
			assert.Contains(t, w.Body.String(), "shiaijo count must be a power of two")
			assert.Contains(t, w.Body.String(), ", or 1",
				"the message must always offer a single shiaijo")
		})
	}
}

// TestCreateCompetitionRejectsEvenNonPowerOfTwo is the regression for the rule
// change on the create path. 6 shiaijo was ACCEPTED by the old rule.
func TestCreateCompetitionRejectsEvenNonPowerOfTwo(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	saveTournamentCourts(t, store, 8)

	w := shiaijoPostComp(t, r, map[string]any{
		"name": "Six Shiaijo", "format": "mixed", "courts": shiaijoLabels(6),
	}, "")
	require.Equalf(t, http.StatusBadRequest, w.Code,
		"6 is even but not a power of two: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "use 4 or 8, or 1")
}

// TestCreateCompetitionShiaijoCountScope pins that league and Swiss are out
// of scope: their courts run in parallel and are not bracket blocks.
func TestCreateCompetitionShiaijoCountScope(t *testing.T) {
	for _, format := range []string{"league", "swiss"} {
		t.Run(format+" accepts 3 shiaijo", func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store, 8)
			body := map[string]any{
				"name":   "Odd " + format,
				"format": format,
				"courts": []string{"A", "B", "C"},
			}
			if format == "swiss" {
				body["swissRounds"] = 3
			}
			w := shiaijoPostComp(t, r, body, "")
			require.Equalf(t, http.StatusCreated, w.Code, "resp: %s", w.Body.String())
		})
	}
}

// TestCreateCompetitionInheritedCourtsMatchExplicit is the inherit-hole
// regression, and the reason the create handler resolves before it validates.
//
// The defect: the rule ran on the PRE-resolution court list and returned early
// when that list was empty, so on a 3-shiaijo venue POST with
// courts:["A","B","C"] returned 400 while the SAME allocation reached by
// OMITTING the key returned 201 and persisted courts: [A B C]. The operator who
// stated their intent was refused and the one who said nothing succeeded.
//
// The ruling is that a 3-shiaijo venue does not entitle a competition to all
// three, so the omitted form is rejected too, with the identical message and
// nothing persisted. Rejecting rather than silently picking 2 of their 3
// shiaijo is deliberate: choosing WHICH two courts a competition runs on is the
// operator's call, not the server's.
func TestCreateCompetitionInheritedCourtsMatchExplicit(t *testing.T) {
	// Every venue size that is not itself a legal competition allocation, so
	// the fix cannot be special-cased to 3.
	for _, venue := range []int{3, 5, 6, 7, 10} {
		t.Run(fmt.Sprintf("venue=%d", venue), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store, venue)

			explicit := shiaijoPostComp(t, r, map[string]any{
				"name": "Explicit", "format": "mixed", "courts": shiaijoLabels(venue),
			}, "")
			inherited := shiaijoPostComp(t, r, map[string]any{
				"name": "Inherited", "format": "mixed",
			}, "")

			assert.Equalf(t, http.StatusBadRequest, explicit.Code,
				"stating all %d shiaijo must be refused: %s", venue, explicit.Body.String())
			assert.Equalf(t, explicit.Code, inherited.Code,
				"omitting courts must reach the SAME outcome as stating them: %s", inherited.Body.String())
			assert.Equal(t, explicit.Body.String(), inherited.Body.String(),
				"both forms must produce the identical message")

			// Neither form may leave a competition on disk.
			for _, id := range []string{"explicit", "inherited"} {
				comp, err := store.LoadCompetition(id)
				require.NoError(t, err)
				assert.Nilf(t, comp, "a refused create must persist nothing (%s)", id)
			}
		})
	}
}

// TestCreateCompetitionInheritedCourtsAcceptedWhenLegal is the other half of
// the inherit fix: closing the hole must not break the ordinary case. A venue
// whose court count IS a legal allocation still lets a competition inherit it.
func TestCreateCompetitionInheritedCourtsAcceptedWhenLegal(t *testing.T) {
	for _, venue := range []int{1, 2, 4, 8} {
		t.Run(fmt.Sprintf("venue=%d", venue), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store, venue)

			w := shiaijoPostComp(t, r, map[string]any{
				"name": "Inherited", "format": "mixed",
			}, "")
			require.Equalf(t, http.StatusCreated, w.Code, "resp: %s", w.Body.String())

			comp, err := store.LoadCompetition("inherited")
			require.NoError(t, err)
			require.NotNil(t, comp)
			assert.Equal(t, shiaijoLabels(venue), comp.Courts,
				"the inherited allocation must still be the tournament's courts")
		})
	}
}

// TestImportCompetitionInheritedCourtsMatchExplicit is the same inherit hole at
// the manifest importer, which is the fully operator-reachable half: a manifest
// that simply omits the optional courts key is the ORDINARY case and needs no
// API client at all.
func TestImportCompetitionInheritedCourtsMatchExplicit(t *testing.T) {
	newStore := func(t *testing.T, venue int) *state.Store {
		t.Helper()
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Date: "11-06-2026", Courts: shiaijoLabels(venue),
		}))
		return store
	}

	for _, venue := range []int{3, 5, 6, 7} {
		t.Run(fmt.Sprintf("venue=%d", venue), func(t *testing.T) {
			store := newStore(t, venue)

			explicit := importCompetition(store, ImportManifestComp{
				ID: "imp-explicit", Name: "Explicit", Date: "11-06-2026",
				Format: "mixed", Courts: shiaijoLabels(venue),
			}, map[string][]byte{})
			inherited := importCompetition(store, ImportManifestComp{
				ID: "imp-inherited", Name: "Inherited", Date: "11-06-2026",
				Format: "mixed",
			}, map[string][]byte{})

			require.NotEmptyf(t, explicit.Error,
				"stating all %d shiaijo must be refused", venue)
			assert.Equal(t, explicit.Error, inherited.Error,
				"omitting courts: must reach the SAME outcome as stating them")
			assert.Contains(t, inherited.Error, "shiaijo count must be a power of two")

			for _, id := range []string{"imp-explicit", "imp-inherited"} {
				comp, err := store.LoadCompetition(id)
				require.NoError(t, err)
				assert.Nilf(t, comp, "a refused import must persist nothing (%s)", id)
			}
		})
	}

	t.Run("a legal venue count is still inherited", func(t *testing.T) {
		store := newStore(t, 2)
		res := importCompetition(store, ImportManifestComp{
			ID: "imp-ok", Name: "OK", Date: "11-06-2026", Format: "mixed",
		}, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("imp-ok")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, comp.Courts)
	})

	t.Run("a league inherits a 3-shiaijo venue", func(t *testing.T) {
		// The format scope survives the reordering: a league has no bracket
		// blocks to merge, so it may inherit any venue.
		store := newStore(t, 3)
		res := importCompetition(store, ImportManifestComp{
			ID: "imp-league", Name: "League", Date: "11-06-2026", Format: "league",
		}, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("imp-league")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, comp.Courts)
	})
}

// TestImportCompetitionRejectsIllegalShiaijoCount sweeps the importer over
// 1..17 with an explicit court list, so the manifest path carries the same
// table as the REST create.
func TestImportCompetitionRejectsIllegalShiaijoCount(t *testing.T) {
	for n := 1; n <= 17; n++ {
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			store, err := state.NewStore(t.TempDir())
			require.NoError(t, err)
			require.NoError(t, store.SaveTournament(&state.Tournament{
				Name: "T", Date: "11-06-2026", Courts: shiaijoLabels(20),
			}))

			res := importCompetition(store, ImportManifestComp{
				ID: "imp", Name: "Imp", Date: "11-06-2026",
				Format: "mixed", Courts: shiaijoLabels(n),
			}, map[string][]byte{})

			if bctest.LegalShiaijoCount(n) {
				require.Emptyf(t, res.Error, "%d shiaijo must be accepted: %s", n, res.Error)
				return
			}
			require.NotEmptyf(t, res.Error, "%d shiaijo must be rejected", n)
			assert.Contains(t, res.Error, "shiaijo count must be a power of two")
		})
	}
}

// TestTournamentCourtsMayBeAnyCount pins the boundary of the rule: it
// constrains a COMPETITION's allocation, never the venue. A 3-, 5- or
// 7-shiaijo tournament is legal and simply splits its courts across
// competitions.
func TestTournamentCourtsMayBeAnyCount(t *testing.T) {
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
		w := shiaijoPostComp(t, r, map[string]any{
			"name": tc.name, "format": "mixed", "courts": tc.courts,
		}, "secret")
		require.Equalf(t, http.StatusCreated, w.Code, "%s: %s", tc.name, w.Body.String())
	}
}

// TestUpdateCompetitionShiaijoCount covers the PUT nuance that the whole
// design turns on: existing data is validated on WRITE only.
func TestUpdateCompetitionShiaijoCount(t *testing.T) {
	// saveLegacyThreeComp writes a competition with an illegal allocation
	// straight to disk, standing in for a record saved before the rule
	// existed (or one that inherited a 3-shiaijo venue's court list).
	saveLegacyThreeComp := func(t *testing.T, store *state.Store, id string) {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:           id,
			Name:         "Legacy Three",
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

	t.Run("unrelated edit leaving an illegal allocation unchanged succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		saveLegacyThreeComp(t, store, "legacy-three")

		// The operator renames the competition and changes its start time.
		// Courts are round-tripped untouched, which is exactly what the real
		// settings screen PUTs. Blocking this would lock them out of every
		// unrelated edit on a competition that is already running.
		w := shiaijoPutComp(t, r, "legacy-three", map[string]any{
			"id":        "legacy-three",
			"name":      "Legacy Three Renamed",
			"format":    "mixed",
			"courts":    []string{"A", "B", "C"},
			"startTime": "10:30",
			"poolSize":  4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-three")
		require.NoError(t, err)
		assert.Equal(t, "Legacy Three Renamed", got.Name, "the unrelated edit must land")
		assert.Equal(t, "10:30", got.StartTime)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts, "the allocation is preserved as-is")
	})

	t.Run("changing to another illegal allocation is rejected", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		saveLegacyThreeComp(t, store, "legacy-three-2")

		w := shiaijoPutComp(t, r, "legacy-three-2", map[string]any{
			"id": "legacy-three-2", "name": "Legacy Three", "format": "mixed",
			"courts": []string{"A", "B", "C", "D", "E"}, "poolSize": 4,
		})
		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "use 4 or 8, or 1")

		got, err := store.LoadCompetition("legacy-three-2")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts, "the rejected write must not land")
	})

	t.Run("changing to an even non-power-of-two is rejected", func(t *testing.T) {
		// 6 passed the retired rule, so this is the PUT-side regression.
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		saveLegacyThreeComp(t, store, "legacy-three-6")

		w := shiaijoPutComp(t, r, "legacy-three-6", map[string]any{
			"id": "legacy-three-6", "name": "Legacy Three", "format": "mixed",
			"courts": shiaijoLabels(6), "poolSize": 4,
		})
		require.Equalf(t, http.StatusBadRequest, w.Code, "resp: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "use 4 or 8, or 1")
	})

	t.Run("fixing an illegal allocation to a legal one succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		saveLegacyThreeComp(t, store, "legacy-three-3")

		w := shiaijoPutComp(t, r, "legacy-three-3", map[string]any{
			"id": "legacy-three-3", "name": "Legacy Three", "format": "mixed",
			"courts": []string{"A", "B"}, "poolSize": 4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-three-3")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, got.Courts)
	})

	t.Run("sweep: changing the allocation must land on a legal count", func(t *testing.T) {
		for n := 1; n <= 17; n++ {
			t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
				r, store, _, _, _ := setupTestRouter(t)
				saveTournamentCourts(t, store, 20)
				// Start from a legal allocation so every case below is a change.
				require.NoError(t, store.SaveCompetition(&state.Competition{
					ID: "sweep", Name: "Sweep", Kind: "individual", Format: "mixed",
					PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
					Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
				}))

				w := shiaijoPutComp(t, r, "sweep", map[string]any{
					"id": "sweep", "name": "Sweep", "format": "mixed",
					"courts": shiaijoLabels(n), "poolSize": 4,
				})
				if bctest.LegalShiaijoCount(n) {
					require.Equalf(t, http.StatusOK, w.Code, "%d shiaijo: %s", n, w.Body.String())
					return
				}
				require.Equalf(t, http.StatusBadRequest, w.Code, "%d shiaijo must be rejected", n)
				assert.Contains(t, w.Body.String(), "shiaijo count must be a power of two")
			})
		}
	})

	// A PUT that OMITS courts inherits the tournament's, exactly like a create,
	// and the inherited list is what the rule sees. On a 3-shiaijo venue that
	// is a change away from a legal [A B], so it must be refused rather than
	// silently widening the competition onto all three courts.
	t.Run("omitting courts inherits the venue and is judged on the inherited list", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 3)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "inherit-put", Name: "Inherit", Kind: "individual", Format: "mixed",
			PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
			Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := shiaijoPutComp(t, r, "inherit-put", map[string]any{
			"id": "inherit-put", "name": "Inherit", "format": "mixed", "poolSize": 4,
		})
		require.Equalf(t, http.StatusBadRequest, w.Code, "resp: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "use 2 or 4, or 1")

		got, err := store.LoadCompetition("inherit-put")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, got.Courts, "the rejected write must not land")
	})

	// The Group B tolerance, reached by inheritance: a legacy record already
	// holding all 3 of its venue's shiaijo must still save an unrelated edit
	// even when the PUT omits courts, because the inherited list equals the
	// stored one and so is not a change.
	t.Run("a legacy record that already holds the whole venue still saves", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 3)
		saveLegacyThreeComp(t, store, "legacy-inherit")

		w := shiaijoPutComp(t, r, "legacy-inherit", map[string]any{
			"id": "legacy-inherit", "name": "Renamed By Inheritance",
			"format": "mixed", "poolSize": 4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-inherit")
		require.NoError(t, err)
		assert.Equal(t, "Renamed By Inheritance", got.Name)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts)
	})

	// A record stored with NO courts key at all is the same tolerance case
	// reached from the other side, and it is the one the raw-vs-resolved
	// comparison broke: the incoming list is RESOLVED before the transform
	// runs, so comparing it against a nil stored list said "changed" for a
	// PUT that never mentioned courts, and the rename died on a 400 naming a
	// count the operator never chose. The SPA compares the raw lists, so it
	// renders Save as enabled and the operator just sees the refusal.
	//
	// All three ways of saying "I am not touching the courts" are swept: the
	// SPA omits the key, and direct API callers send null or [].
	for _, tc := range []struct {
		desc  string
		body  map[string]any
		nulls bool
	}{
		{desc: "omitted"},
		{desc: "null", body: map[string]any{"courts": nil}},
		{desc: "empty", body: map[string]any{"courts": []string{}}},
	} {
		t.Run("a record with no stored courts still saves an unrelated edit ("+tc.desc+")", func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			saveTournamentCourts(t, store, 3)
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID: "no-courts-" + tc.desc, Name: "No Courts", Kind: "individual", Format: "mixed",
				PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
				StartTime: "09:00", Status: state.CompStatusSetup,
			}))

			body := map[string]any{
				"id": "no-courts-" + tc.desc, "name": "Renamed", "format": "mixed", "poolSize": 4,
			}
			for k, v := range tc.body {
				body[k] = v
			}
			w := shiaijoPutComp(t, r, "no-courts-"+tc.desc, body)

			require.Equalf(t, http.StatusOK, w.Code,
				"a PUT that changes nothing about courts must land: %s", w.Body.String())
			got, err := store.LoadCompetition("no-courts-" + tc.desc)
			require.NoError(t, err)
			assert.Equal(t, "Renamed", got.Name)
			assert.Equal(t, []string{"A", "B", "C"}, got.Courts,
				"the inherited allocation is materialised, exactly as it always was on save")
		})
	}

	// The same raw-vs-resolved comparison also drives the draw-ready gate, so
	// the identical PUT produced a spurious 409 there. A cosmetic edit on a
	// draw-ready competition with no stored courts must not be told to
	// discard its draw.
	t.Run("a draw-ready record with no stored courts still saves a cosmetic edit", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 3)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "draw-ready-no-courts", Name: "Pending Draw", Kind: "individual", Format: "mixed",
			PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
			StartTime: "09:00", Status: state.CompStatusDrawReady,
		}))

		w := shiaijoPutComp(t, r, "draw-ready-no-courts", map[string]any{
			"id": "draw-ready-no-courts", "name": "Pending Draw Renamed", "format": "mixed",
			"kind": "individual", "poolSize": 4, "poolWinners": 2, "poolSizeMode": "min",
		})

		require.Equalf(t, http.StatusOK, w.Code,
			"nothing output-affecting changed, so the draw is still valid: %s", w.Body.String())
		got, err := store.LoadCompetition("draw-ready-no-courts")
		require.NoError(t, err)
		assert.Equal(t, "Pending Draw Renamed", got.Name)
		assert.Equal(t, state.CompStatusDrawReady, got.Status)
	})

	// The gate must still bite when the courts really do move on a
	// draw-ready record, or this fix would have opened a hole.
	t.Run("a draw-ready record still refuses a real court change", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 4)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "draw-ready-move", Name: "Pending Draw", Kind: "individual", Format: "mixed",
			PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
			Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusDrawReady,
		}))

		w := shiaijoPutComp(t, r, "draw-ready-move", map[string]any{
			"id": "draw-ready-move", "name": "Pending Draw", "format": "mixed",
			"kind": "individual", "poolSize": 4, "poolWinners": 2, "poolSizeMode": "min",
			"courts": []string{"C", "D"},
		})

		require.Equalf(t, http.StatusConflict, w.Code, "resp: %s", w.Body.String())
		got, err := store.LoadCompetition("draw-ready-move")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, got.Courts, "the rejected write must not land")
	})

	// The rule is scoped by format, so a format change can make a
	// stored-and-valid allocation illegal without the court list moving at
	// all. The trigger has to watch both fields or this slips through and the
	// operator ends up with a competition that silently cannot draw.
	t.Run("switching a 3-shiaijo league to a bracket format is rejected", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "league-to-mixed", Name: "Was League", Kind: "individual", Format: "league",
			PoolSize: 4, PoolWinners: 2, PoolSizeMode: "min",
			Courts: []string{"A", "B", "C"}, StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := shiaijoPutComp(t, r, "league-to-mixed", map[string]any{
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
	// keeps a 3-shiaijo allocation, so it must be allowed.
	t.Run("switching a 3-shiaijo mixed competition to league succeeds", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		saveLegacyThreeComp(t, store, "mixed-to-league")

		w := shiaijoPutComp(t, r, "mixed-to-league", map[string]any{
			"id": "mixed-to-league", "name": "Legacy Three", "format": "league",
			"courts": []string{"A", "B", "C"}, "poolSize": 4,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("mixed-to-league")
		require.NoError(t, err)
		assert.Equal(t, "league", got.Format)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts)
	})

	t.Run("league keeps a 3-shiaijo allocation editable and changeable", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		saveTournamentCourts(t, store, 8)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "league-put", Name: "League", Kind: "individual", Format: "league",
			PoolSize: 6, PoolWinners: 1, PoolSizeMode: "min",
			Courts: []string{"A", "B"}, StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := shiaijoPutComp(t, r, "league-put", map[string]any{
			"id": "league-put", "name": "League", "format": "league",
			"courts": []string{"A", "B", "C"}, "poolSize": 6,
		})
		require.Equalf(t, http.StatusOK, w.Code, "resp: %s", w.Body.String())

		got, err := store.LoadCompetition("league-put")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C"}, got.Courts)
	})
}

// saveTournamentCourts seeds an n-shiaijo venue (A, B, C, ...) so every
// competition court count under test has real labels to pick from: a
// competition may only be allocated shiaijo the tournament actually has, so the
// venue has to be at least as large as the biggest allocation swept. Written
// straight to the store, with no password, so the admin requests under test
// need no auth header (same pattern as TestCompetitionCourtsInvariant).
func saveTournamentCourts(t *testing.T, store *state.Store, n int) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Shiaijo Cup", Courts: shiaijoLabels(n),
	}))
}

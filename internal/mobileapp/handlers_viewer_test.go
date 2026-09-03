package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMergePoolNumbersIntoPlayers, mp-13y: numbers from pools.csv must be
// merged onto comp.Players so the viewer API carries the numberPrefix-derived
// "K1", "K2", … on every player. The merge is the bridge that lets the TV
// display / streaming overlay / viewer card render the prefix at all
// (participants.csv does NOT persist Number).
func TestMergePoolNumbersIntoPlayers(t *testing.T) {
	t.Run("no-op when numberPrefix is empty", func(t *testing.T) {
		comp := &state.Competition{
			Players: []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number, "no numberPrefix → never merge")
	})

	t.Run("no-op when pools is empty and format is not playoffs", func(t *testing.T) {
		// Before the draw a mixed competition has no pools.csv and NO assigned
		// number: the check-in desk's provisional number travels separately
		// (Competition.ProvisionalNumbers), never in Player.Number, so a
		// public surface cannot show a number the draw has not assigned.
		comp := &state.Competition{
			NumberPrefix: "K",
			Format:       state.CompFormatMixed,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "", comp.Players[0].Number)
	})

	t.Run("assigns sequential numbers for playoffs-only with no pools", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Players: []domain.Player{
				{ID: "p1", Name: "Rossi Marco", Dojo: "Dojo Rossi Marco"},
				{ID: "p2", Name: "Dubois Claire", Dojo: "Dojo Dubois Claire"},
				{ID: "p3", Name: "Santos Ana", Dojo: "Dojo Santos Ana"},
			},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "D1", comp.Players[0].Number)
		assert.Equal(t, "D2", comp.Players[1].Number)
		assert.Equal(t, "D3", comp.Players[2].Number)
	})

	// bc-pnum G8: this subtest used to assert the opposite -- that an
	// existing non-empty Number survived the merge untouched. That guard
	// (the playoffs-only branch's own `if players[i].Number == ""`) was
	// RETIRED: participants.csv never persists Number, so the only Number
	// this branch could ever see already set was one THIS SAME function had
	// just assigned on an earlier call in the request; the preserve was
	// unreachable in production, and preserving a stale value on purpose
	// (rather than a competition's CURRENT prefix) is exactly the partial-
	// preserve fallback D1 forbids. helper.AssignPlayerNumbers now runs
	// unconditionally here, same as generatePlayoffs itself, so a
	// NumberPrefix changed after a playoffs-only draw is reflected
	// immediately on read -- there is no pools.csv for playoffs-only, so
	// there is nothing to rewrite either (acceptance criterion 4).
	t.Run("playoffs-only: re-derives unconditionally, overwriting any existing Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "STALE", Dojo: "Dojo Tanaka"}},
		}
		mergePoolNumbersIntoPlayers(comp, nil)
		assert.Equal(t, "D1", comp.Players[0].Number, "must overwrite a stale Number with the current prefix, not preserve it")
	})

	t.Run("merges by id when HasParticipantIDs", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"},
				{ID: "p2", Name: "Suzuki", Dojo: "Dojo Suzuki"},
				{ID: "p3", Name: "Yamada", Dojo: "Dojo Yamada"},
			},
		}
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []domain.Player{
				{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"},
				{ID: "p3", Name: "Yamada", Number: "K2", Dojo: "Dojo Yamada"},
			}},
			{PoolName: "Pool B", Players: []domain.Player{
				{ID: "p2", Name: "Suzuki", Number: "K3", Dojo: "Dojo Suzuki"},
			}},
		}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K3", comp.Players[1].Number)
		assert.Equal(t, "K2", comp.Players[2].Number)
	})

	t.Run("falls back to name when id is empty (legacy roster)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players: []domain.Player{
				{Name: "Tanaka", Dojo: "Dojo Tanaka"}, // no ID
				{Name: "Suzuki", Dojo: "Dojo Suzuki"},
			},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{
			{Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"},
			{Name: "Suzuki", Number: "K2", Dojo: "Dojo Suzuki"},
		}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K2", comp.Players[1].Number)
	})

	t.Run("preserves existing non-empty Number (idempotent)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "EXISTING", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "EXISTING", comp.Players[0].Number, "must not overwrite an existing Number")
	})

	t.Run("skips pool players with empty Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayers(comp, pools)
		assert.Equal(t, "", comp.Players[0].Number)
	})
}

func TestViewerHandlers_Standalone(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. GET /api/viewer/tournament - No tournament case: 200 with a null
	// body (a normal bootstrap state, not a 404, so the SPA doesn't log a
	// console error).
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "null", w.Body.String())

	// 2. GET /api/viewer/tournament - With tournament
	tourney := state.Tournament{Name: "Test Tourney", Password: "secret"}
	require.NoError(t, store.SaveTournament(&tourney))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var respTourney state.Tournament
	json.Unmarshal(w.Body.Bytes(), &respTourney)
	assert.Equal(t, "Test Tourney", respTourney.Name)
	assert.Equal(t, "", respTourney.Password) // Password should be stripped

	// 3. GET /api/viewer/competitions - Empty case
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", w.Body.String())

	// 4. GET /api/viewer/competitions - With competitions
	comp1 := state.Competition{ID: "c1", Name: "Comp 1"}
	require.NoError(t, store.SaveCompetition(&comp1))
	require.NoError(t, store.SaveParticipants("c1", nil))

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &comps)
	assert.Len(t, comps, 1)
	config := comps[0]["config"].(map[string]interface{})
	assert.Equal(t, "c1", config["id"])

	// 5. GET /api/viewer/competitions/:id - Success
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var detail map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &detail)
	assert.NotNil(t, detail["config"])
	assert.Contains(t, detail, "pools")
	assert.Contains(t, detail, "bracket")

	// 6. GET /api/viewer/competitions/:id - Not Found
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/nonexistent", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

}

// TestViewerAggregator_StripsPreviewBracket asserts that a Preview bracket
// (pool-origin placeholder leaves on a mixed source competition) is REMOVED
// from the aggregate /api/viewer/competitions payload so the SPA doesn't
// surface "Pool A-1st vs Pool B-2nd" as upcoming matches in Find-My-Matches /
// Watchlist / schedule / TV displays. The per-competition detail endpoint
// (/api/viewer/competitions/:id) must still return it for the Bracket-tab UI.
// Regression guard for mp-9dz.
func TestViewerAggregator_StripsPreviewBracket(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))
	comp := state.Competition{ID: "mixed", Name: "Mixed", Format: state.CompFormatMixed}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("mixed", nil))

	preview := &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{
				ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Court: "A",
				Status: state.MatchStatusScheduled, ScheduledAt: "09:30",
				IpponsA: []string{"M"}, HansokuB: 1,
			},
		}},
	}
	require.NoError(t, store.SaveBracket("mixed", preview))

	// Aggregate endpoint MUST strip the preview bracket.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var comps []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	require.Len(t, comps, 1)
	assert.Nil(t, comps[0]["bracket"], "aggregate endpoint must strip Preview brackets (mp-9dz)")

	// Detail endpoint MUST still return it so the Bracket-tab UI renders.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/mixed", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detail map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	bracketField, ok := detail["bracket"].(map[string]any)
	require.True(t, ok, "detail endpoint must return the preview bracket for the Bracket-tab UI")
	assert.Equal(t, true, bracketField["preview"], "preview flag must be present on the detail payload")
	rounds, _ := bracketField["rounds"].([]any)
	assert.NotEmpty(t, rounds, "preview bracket rounds must be present on the detail payload")
	// Wire contract: a bracket match carries ipponsA/hansokuB directly, never
	// the legacy scoreA/scoreB rendered-string fields.
	round0, _ := rounds[0].([]any)
	require.NotEmpty(t, round0)
	match0, _ := round0[0].(map[string]any)
	assert.Equal(t, []any{"M"}, match0["ipponsA"])
	assert.Equal(t, float64(1), match0["hansokuB"])
	assert.NotContains(t, match0, "scoreA")
	assert.NotContains(t, match0, "scoreB")
}

// TestViewerCompetitionDetail_NumbersBeforeAndAfterTheDraw pins the payload
// the operator console's check-in list reads: before a draw exists Number is
// EMPTY and the competitors' registration-order numbers travel as
// provisionalNumbers, index-aligned with players; once pools.csv exists its
// numbers fill Number and provisionalNumbers is gone. The reverts this pins:
// composing provisional numbers INTO Number (a public surface would show a
// number the draw never assigned), and dropping the provisional array (the
// check-in desk would have nothing to call before the draw).
func TestViewerCompetitionDetail_NumbersBeforeAndAfterTheDraw(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "viewer-numbers"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Viewer Numbers", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup,
		NumberPrefix: "K", HasParticipantIDs: true,
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Bob"},
	}))

	payload := func(t *testing.T) (numbers, provisional []string) {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+cid, nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		var body struct {
			Config state.Competition `json:"config"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		numbers = make([]string, 0, len(body.Config.Players))
		for _, p := range body.Config.Players {
			numbers = append(numbers, p.Number)
		}
		return numbers, body.Config.ProvisionalNumbers
	}

	numbers, provisional := payload(t)
	assert.Equal(t, []string{"", ""}, numbers, "pre-draw: no ASSIGNED number")
	assert.Equal(t, []string{"K1", "K2"}, provisional, "pre-draw: provisional registration-order numbers, index-aligned with players")

	// The draw puts Bob first: pools.csv wins, and the provisional array goes.
	require.NoError(t, store.SavePools(cid, []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Bob", Number: "K1"},
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice", Number: "K2"},
	}}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Viewer Numbers", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
		NumberPrefix: "K", HasParticipantIDs: true,
	}))
	numbers, provisional = payload(t)
	assert.Equal(t, []string{"K2", "K1"}, numbers, "post-draw: pools.csv numbers")
	assert.Empty(t, provisional, "post-draw: nothing provisional")
}

// TestViewerCompetitionsList_CorruptPoolsShowsNoNumbers pins D1 on the read
// side: a drawn competition whose pools.csv will not parse shows MISSING
// numbers on the public list (and no provisional ones), never numbers
// composed from registration order that would contradict the draw on disk.
// The revert this pins: passing a nil pools slice to the merge on a read
// error, which the merge reads as "no draw yet". The fixture is a
// playoffs-only competition on purpose: for that format "no pools" DOES
// compose numbers (participant order is its assigned number), so it is the
// one format where an unreadable file handed to the merge as "no pools" is
// observable as invented numbers rather than as silence.
func TestViewerCompetitionsList_CorruptPoolsShowsNoNumbers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "corrupt-pools-list"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Corrupt Pools", Format: state.CompFormatPlayoffs, Kind: "individual",
		Courts: []string{"A"}, Status: state.CompStatusPools, NumberPrefix: "K", HasParticipantIDs: true,
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Bob"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "competitions", cid, "pools.csv"), []byte("a,b\na,\"bad\nquote"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	// The aggregate is a list of {config, poolMatches, bracket} items.
	var items []struct {
		Config state.Competition `json:"config"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
	var found bool
	for _, item := range items {
		comp := item.Config
		if comp.ID != cid {
			continue
		}
		found = true
		require.NotEmpty(t, comp.Players, "the roster must still be served")
		for _, p := range comp.Players {
			assert.Emptyf(t, p.Number, "competitor %q must show NO number over an unreadable pools.csv, got %q", p.Name, p.Number)
		}
		assert.Empty(t, comp.ProvisionalNumbers, "a drawn competition never carries provisional numbers")
		assert.Contains(t, w.Body.String(), `"file":"pools.csv"`, "the unreadable file must be named in the item's dataIssues, not only in the server log")
	}
	assert.True(t, found, "the competition must still be listed")
}

// TestProvisionalCompetitorNumbers pins where the separate provisional list
// exists: only for a competition a draw can still be generated from, with a
// prefix and a roster, and never for Swiss, whose draw assigns no number that
// could replace it.
func TestProvisionalCompetitorNumbers(t *testing.T) {
	roster := []domain.Player{{ID: "p1", Name: "A", Dojo: "D"}, {ID: "p2", Name: "B", Dojo: "D"}}
	for _, tc := range []struct {
		name string
		comp *state.Competition
		want []string
	}{
		{"setup mixed", &state.Competition{Format: state.CompFormatMixed, Status: state.CompStatusSetup, NumberPrefix: "K", Players: roster}, []string{"K1", "K2"}},
		{"legacy empty status", &state.Competition{Format: state.CompFormatMixed, Status: "", NumberPrefix: "K", Players: roster}, []string{"K1", "K2"}},
		{"draw-ready", &state.Competition{Format: state.CompFormatMixed, Status: state.CompStatusDrawReady, NumberPrefix: "K", Players: roster}, nil},
		{"running", &state.Competition{Format: state.CompFormatMixed, Status: state.CompStatusPools, NumberPrefix: "K", Players: roster}, nil},
		{"no prefix", &state.Competition{Format: state.CompFormatMixed, Status: state.CompStatusSetup, Players: roster}, nil},
		{"swiss", &state.Competition{Format: state.CompFormatSwiss, Status: state.CompStatusSetup, NumberPrefix: "S", Players: roster}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := provisionalCompetitorNumbers(tc.comp)
			assert.Equal(t, tc.want, got)
			for _, p := range tc.comp.Players {
				assert.Empty(t, p.Number, "Player.Number itself must stay untouched")
			}
		})
	}
}

package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
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

// TestMergePoolNumbersIntoPlayersSlice, mp-13y: numbers from pools.csv must be
// merged onto comp.Players so the viewer API carries the numberPrefix-derived
// "K1", "K2", … on every player. The merge is the bridge that lets the TV
// display / streaming overlay / viewer card render the prefix at all
// (participants.csv does NOT persist Number).
//
// bc-pnum ruling 2: mergePoolNumbersIntoPlayersSlice no longer has a
// playoffs-only branch (a knockout-only competition's number now comes from
// the bracket's DrawOrder, exercised by TestApplyDrawNumbers below, not
// pools.csv) and no longer falls back to (name, dojo): identity is the
// participant id, ONLY. A pools.csv row or a roster row with no id
// contributes/receives no number.
func TestMergePoolNumbersIntoPlayersSlice(t *testing.T) {
	t.Run("no-op when numberPrefix is empty", func(t *testing.T) {
		comp := &state.Competition{
			Players: []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
		assert.Equal(t, "", comp.Players[0].Number, "no numberPrefix → never merge")
	})

	t.Run("no-op when pools is empty", func(t *testing.T) {
		// Before the draw a mixed competition has no pools.csv and NO assigned
		// number: a pooled competition's competitors carry no number at all
		// until the draw runs (bc-pnum operator ruling), so a public surface
		// cannot show a number the draw has not assigned.
		comp := &state.Competition{
			NumberPrefix: "K",
			Format:       state.CompFormatMixed,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, nil)
		assert.Equal(t, "", comp.Players[0].Number)
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
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
		assert.Equal(t, "K1", comp.Players[0].Number)
		assert.Equal(t, "K3", comp.Players[1].Number)
		assert.Equal(t, "K2", comp.Players[2].Number)
	})

	// bc-pnum ruling 2: a legacy roster (or pool row) with no id contributes
	// no number at all -- there is no (name, dojo) fallback tier any more.
	t.Run("no number when id is empty (legacy roster), even on an exact name/dojo match", func(t *testing.T) {
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
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
		assert.Equal(t, "", comp.Players[0].Number, "an id-less roster row must get no number, not a name-matched one")
		assert.Equal(t, "", comp.Players[1].Number)
	})

	t.Run("preserves existing non-empty Number (idempotent)", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Number: "EXISTING", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
		assert.Equal(t, "EXISTING", comp.Players[0].Number, "must not overwrite an existing Number")
	})

	t.Run("skips pool players with empty Number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "", Dojo: "Dojo Tanaka"}}}}
		mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
		assert.Equal(t, "", comp.Players[0].Number)
	})
}

// TestApplyDrawNumbers pins bc-pnum ruling 2's format switch: a playoffs
// (knockout-only) competition is numbered from the bracket's DrawOrder
// (engine.NumberKnockoutParticipants), never from pools.csv or participant
// order; every other format is numbered from pools.csv, unchanged.
func TestApplyDrawNumbers(t *testing.T) {
	t.Run("playoffs: numbers from bracket.DrawOrder, in draw-position order", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Status:       state.CompStatusDrawReady,
			Players: []domain.Player{
				{ID: "p1", Name: "Rossi Marco", Dojo: "Dojo Rossi Marco"},
				{ID: "p2", Name: "Dubois Claire", Dojo: "Dojo Dubois Claire"},
				{ID: "p3", Name: "Santos Ana", Dojo: "Dojo Santos Ana"},
			},
		}
		bracket := &state.Bracket{DrawOrder: []string{"p3", "p1", "p2"}}
		applyDrawNumbers(comp, comp.Players, nil, bracket)
		assert.Equal(t, "D2", comp.Players[0].Number, "p1 is DrawOrder[1] -> D2")
		assert.Equal(t, "D3", comp.Players[1].Number, "p2 is DrawOrder[2] -> D3")
		assert.Equal(t, "D1", comp.Players[2].Number, "p3 is DrawOrder[0] -> D1")
	})

	// mp-yuy8: an unset Format ("") is standalone playoffs too (the draw
	// pipeline's default branch calls generatePlayoffs for it exactly as it
	// does for the literal "playoffs" value), so this must go through
	// comp.EffectiveFormat(), not comp.Format.
	t.Run("unset (empty) Format numbers from DrawOrder too, same as playoffs", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       "",
			Status:       state.CompStatusDrawReady,
			Players: []domain.Player{
				{ID: "p1", Name: "Rossi Marco", Dojo: "Dojo Rossi Marco"},
				{ID: "p2", Name: "Dubois Claire", Dojo: "Dojo Dubois Claire"},
			},
		}
		bracket := &state.Bracket{DrawOrder: []string{"p1", "p2"}}
		applyDrawNumbers(comp, comp.Players, nil, bracket)
		assert.Equal(t, "D1", comp.Players[0].Number)
		assert.Equal(t, "D2", comp.Players[1].Number)
	})

	t.Run("playoffs pre-draw: no bracket, no DrawOrder -> no numbers at all", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Status:       state.CompStatusSetup,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		applyDrawNumbers(comp, comp.Players, nil, nil)
		assert.Equal(t, "", comp.Players[0].Number, "pre-draw: no number even though a NumberPrefix is configured")
	})

	t.Run("playoffs: a player absent from DrawOrder (excluded from the draw) gets no number", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Status:       state.CompStatusDrawReady,
			Players: []domain.Player{
				{ID: "p1", Name: "Rossi Marco", Dojo: "Dojo Rossi Marco"},
				{ID: "p2", Name: "Not Checked In", Dojo: "Dojo Absent"},
			},
		}
		bracket := &state.Bracket{DrawOrder: []string{"p1"}}
		applyDrawNumbers(comp, comp.Players, nil, bracket)
		assert.Equal(t, "D1", comp.Players[0].Number)
		assert.Equal(t, "", comp.Players[1].Number, "excluded from the draw -> no number")
	})

	t.Run("playoffs: a prefix change shows on the next call with no rewrite", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "D",
			Format:       state.CompFormatPlayoffs,
			Status:       state.CompStatusDrawReady,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		bracket := &state.Bracket{DrawOrder: []string{"p1"}}
		applyDrawNumbers(comp, comp.Players, nil, bracket)
		assert.Equal(t, "D1", comp.Players[0].Number)

		comp.NumberPrefix = "Z"
		comp.Players[0].Number = "" // simulate a fresh read, Number is never persisted
		applyDrawNumbers(comp, comp.Players, nil, bracket)
		assert.Equal(t, "Z1", comp.Players[0].Number, "the same DrawOrder under a new prefix relabels immediately")
	})

	t.Run("mixed: still numbers from pools.csv, unaffected by the playoffs branch", func(t *testing.T) {
		comp := &state.Competition{
			NumberPrefix: "K",
			Format:       state.CompFormatMixed,
			Status:       state.CompStatusDrawReady,
			Players:      []domain.Player{{ID: "p1", Name: "Tanaka", Dojo: "Dojo Tanaka"}},
		}
		pools := []helper.Pool{{PoolName: "Pool A", Players: []domain.Player{{ID: "p1", Name: "Tanaka", Number: "K1", Dojo: "Dojo Tanaka"}}}}
		applyDrawNumbers(comp, comp.Players, pools, nil)
		assert.Equal(t, "K1", comp.Players[0].Number)
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
// the operator console's check-in list reads (bc-pnum operator ruling:
// numbers are assigned pool by pool at the draw, and nothing is shown
// before it): before a draw exists, players carry no "number" field at all
// and the payload carries no "provisionalNumbers" key whatsoever; once
// pools.csv exists, the draw's pool-order numbers fill Number. The revert
// this pins: reintroducing a pre-draw number of any kind, provisional or
// otherwise.
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

	rawGet := func(t *testing.T) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+cid, nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	body := rawGet(t)
	config, ok := body["config"].(map[string]any)
	require.True(t, ok, "payload must carry a config object")
	_, hasProvisionalKey := config["provisionalNumbers"]
	assert.False(t, hasProvisionalKey, "pre-draw: the payload must not carry a provisionalNumbers key at all")
	players, ok := config["players"].([]any)
	require.True(t, ok, "payload must carry a players array")
	require.Len(t, players, 2)
	for _, raw := range players {
		p, ok := raw.(map[string]any)
		require.True(t, ok)
		_, hasNumber := p["number"]
		assert.False(t, hasNumber, "pre-draw: player %v must carry no number field at all", p["name"])
	}

	// The draw puts Bob first: pools.csv wins.
	require.NoError(t, store.SavePools(cid, []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Bob", Number: "K1"},
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice", Number: "K2"},
	}}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Viewer Numbers", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
		NumberPrefix: "K", HasParticipantIDs: true,
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+cid, nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	var typed struct {
		Config state.Competition `json:"config"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &typed))
	numbers := make([]string, 0, len(typed.Config.Players))
	for _, p := range typed.Config.Players {
		numbers = append(numbers, p.Number)
	}
	assert.Equal(t, []string{"K2", "K1"}, numbers, "post-draw: pool-order numbers from pools.csv")
}

// TestViewerCompetitionDetail_PlayoffsNumbersFollowBracketPosition pins
// bc-pnum ruling 2 end to end, through the REAL draw pipeline
// (eng.GenerateDraw / eng.DiscardDraw) and the real HTTP viewer detail
// endpoint, not just the applyDrawNumbers unit (TestApplyDrawNumbers
// above). Seeds are set so bracket position genuinely differs from
// registration order -- Dan (seed 1) and Alice (seed 2) land in opposite
// halves of the draw -- so a wrong implementation that silently fell back
// to participant order would print the WRONG numbers, not merely omit
// them. Eve is not checked in, so the draw excludes her (bc-pnum ruling 2's
// "absent from DrawOrder" case, exercised here over check-in specifically
// rather than the synthetic bracket fixture TestApplyDrawNumbers used).
func TestViewerCompetitionDetail_PlayoffsNumbersFollowBracketPosition(t *testing.T) {
	r, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "playoffs-viewer-numbers"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Playoffs Viewer Numbers", Format: state.CompFormatPlayoffs, Kind: "individual",
		Courts: []string{"A"}, Status: state.CompStatusSetup, NumberPrefix: "K",
		CheckInEnabled: true,
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice", CheckedIn: true},
		{Name: "Bob", Dojo: "Dojo Bob", CheckedIn: true},
		{Name: "Cleo", Dojo: "Dojo Cleo", CheckedIn: true},
		{Name: "Dan", Dojo: "Dojo Dan", CheckedIn: true},
		{Name: "Eve", Dojo: "Dojo Eve", CheckedIn: false}, // excluded: not checked in
	}))
	require.NoError(t, store.SaveSeeds(cid, []domain.SeedAssignment{
		{Name: "Dan", SeedRank: 1},
		{Name: "Alice", SeedRank: 2},
	}))

	getDetail := func(t *testing.T) map[string]any {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+cid, nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}
	playersOf := func(body map[string]any) []map[string]any {
		config := body["config"].(map[string]any)
		raw := config["players"].([]any)
		out := make([]map[string]any, len(raw))
		for i, r := range raw {
			out[i] = r.(map[string]any)
		}
		return out
	}

	// Pre-draw: no number field at all, per every competitor.
	for _, p := range playersOf(getDetail(t)) {
		_, hasNumber := p["number"]
		assert.False(t, hasNumber, "pre-draw: player %v must carry no number field at all", p["name"])
	}

	// Generate (not start, so a later DiscardDraw is legal) the real draw.
	require.NoError(t, eng.GenerateDraw(cid))

	bracket, err := store.LoadBracket(cid)
	require.NoError(t, err)
	require.NotEmpty(t, bracket.DrawOrder, "premise: the draw stamped a DrawOrder")
	require.Len(t, bracket.DrawOrder, 4, "Eve (not checked in) must be excluded from the draw")

	roster, err := store.LoadParticipantsOpt(cid, false, state.LoadParticipantsOpts{})
	require.NoError(t, err)
	idToName := make(map[string]string, len(roster))
	for _, p := range roster {
		idToName[p.ID] = p.Name
	}
	wantNumberByName := make(map[string]string, len(bracket.DrawOrder))
	for i, id := range bracket.DrawOrder {
		wantNumberByName[idToName[id]] = fmt.Sprintf("K%d", i+1)
	}
	// The independent oracle (bracket.DrawOrder) must actually differ from
	// plain registration order, or this test could not distinguish the
	// bracket-position rule from a participant-order fallback.
	assert.NotEqual(t, []string{"K1", "K2", "K3", "K4"},
		[]string{wantNumberByName["Alice"], wantNumberByName["Bob"], wantNumberByName["Cleo"], wantNumberByName["Dan"]},
		"premise: seeding must make bracket position differ from registration order")

	for _, p := range playersOf(getDetail(t)) {
		name := p["name"].(string)
		if name == "Eve" {
			_, hasNumber := p["number"]
			assert.False(t, hasNumber, "Eve was excluded from the draw and must carry no number")
			continue
		}
		assert.Equal(t, wantNumberByName[name], p["number"], "post-draw: %s's number must follow bracket position", name)
	}

	// Discard the draw: bracket.json is deleted and status reverts to Setup,
	// so numbers must vanish again, exactly like pre-draw.
	require.NoError(t, eng.DiscardDraw(cid))
	for _, p := range playersOf(getDetail(t)) {
		_, hasNumber := p["number"]
		assert.False(t, hasNumber, "after discarding the draw: player %v must carry no number field at all", p["name"])
	}
}

// TestViewerCompetitionsList_CorruptBracketShowsNoNumbers pins D1 on the
// read side for a knockout-only competition (bc-pnum ruling 2's successor to
// TestViewerCompetitionsList_CorruptPoolsShowsNoNumbers, retired below): a
// drawn playoffs competition whose bracket.json will not parse shows MISSING
// numbers on the public list, never numbers invented from participant order
// or any other fallback. Under ruling 2 a playoffs competition's number
// comes from bracket.DrawOrder, so the analogous read-error risk moved from
// pools.csv to bracket.json; a corrupt pools.csv is no longer even read for
// this format (see TestCourtCurrentUnreadablePoolsShowsNoNumbers in
// handlers_display_test.go for that boundary, exercised there instead over a
// pooled format).
func TestViewerCompetitionsList_CorruptBracketShowsNoNumbers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "corrupt-bracket-list"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Corrupt Bracket", Format: state.CompFormatPlayoffs, Kind: "individual",
		Courts: []string{"A"}, Status: state.CompStatusPools, NumberPrefix: "K", HasParticipantIDs: true,
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Bob"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "competitions", cid, "bracket.json"), []byte("{not valid json"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	// The aggregate is a list of {config, poolMatches, bracket, dataIssues} items.
	var items []struct {
		Config     state.Competition `json:"config"`
		DataIssues []struct {
			File string `json:"file"`
		} `json:"dataIssues"`
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
			assert.Emptyf(t, p.Number, "competitor %q must show NO number over an unreadable bracket.json, got %q", p.Name, p.Number)
		}
		// Exactly ONE dataIssues entry, not two: buildViewerCompetitionPayload
		// loads the bracket once (for the court-feed check) and the numbering
		// merge must reuse that same read/error rather than attempting a
		// second bracket.json load and reporting the identical corrupt-file
		// failure a second time (numbersFromDrawWithBracket, not
		// numbersFromDraw, is what closes that door).
		var bracketIssues int
		for _, di := range item.DataIssues {
			if di.File == "bracket.json" {
				bracketIssues++
			}
		}
		assert.Equal(t, 1, bracketIssues, "bracket.json's unreadable-file issue must be reported exactly once, not duplicated")
	}
	assert.True(t, found, "the competition must still be listed")
}

// TestViewerCompetitionsList_SetupCompetitionSkipsPoolsRead pins numbersFromPools'
// setup-status skip (PR #416 finding 3): a competition that has never drawn
// cannot legitimately have a pools.csv, so the read must not even be
// attempted -- garbage bytes left at that path (a stray fixture/leftover, not
// an operator-actionable file) must surface as neither a dataIssue nor a log
// line, unlike TestViewerCompetitionsList_CorruptPoolsShowsNoNumbers's DRAWN
// competition, where the identical bytes DO produce both.
func TestViewerCompetitionsList_SetupCompetitionSkipsPoolsRead(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "setup-skips-pools-read"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Setup Skips Pools Read", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, Status: state.CompStatusSetup, NumberPrefix: "K", HasParticipantIDs: true,
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo Alice"},
	}))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "competitions", cid, "pools.csv"), []byte("a,b\na,\"bad\nquote"), 0o600))

	var logBuf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	assert.NotContains(t, w.Body.String(), "pools.csv", "a setup competition must never attempt the pools.csv read, so garbage bytes there produce no dataIssues entry")
	assert.NotContains(t, logBuf.String(), "load pools", "a setup competition must never attempt the pools.csv read, so garbage bytes there produce no log line either")

	var items []struct {
		Config state.Competition `json:"config"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &items))
	var found bool
	for _, item := range items {
		if item.Config.ID != cid {
			continue
		}
		found = true
		require.NotEmpty(t, item.Config.Players, "the roster must still be served")
		assert.Empty(t, item.Config.Players[0].Number, "pre-draw: no number even though a NumberPrefix is configured")
	}
	assert.True(t, found, "the competition must still be listed")
}

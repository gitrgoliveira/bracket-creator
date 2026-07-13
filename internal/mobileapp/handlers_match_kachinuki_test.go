// Package mobileapp, handlers_match_kachinuki_test.go pins the kachinuki
// bout-submission contract on the score endpoint:
//
//   - Advancement (MaybeAdvanceKachinuki) fires ONLY on a write flagged
//     with the transient kachinukiBoutFinal request field. Unflagged
//     running writes (autosave, where a 1-0 lead already sets the sub
//     winner mid-bout) and completed writes (corrections) never advance.
//   - A bout-1 hikiwake is recordable in knockout kachinuki via a flagged
//     running write: both players retire and the next pair is appended.
//   - A premature status=completed write (roster not exhausted, no
//     daihyosen sub-result, not a correction) is rejected 409, never
//     silently accepted or dropped.
//   - A daihyosen completion (position -1 sub with winner) completes the
//     bracket match and propagates the winner via the normal path.
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupKachinukiScoreServer builds a kachinuki team competition (size 3)
// with two team participants (Ryu, Tora) whose lineups are keyed by the
// team PARTICIPANT ID, mirroring how the lineup editor saves them.
func setupKachinukiScoreServer(t *testing.T, compID string) (*gin.Engine, *state.Store) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		Format:        state.CompFormatMixed,
		Status:        state.CompStatusPools,
		TeamSize:      3,
		TeamMatchType: state.TeamMatchTypeKachinuki,
	}))

	ryuID := helper.NewUUID4()
	toraID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: ryuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: toraID, Name: "Tora", Dojo: "DojoT"},
	}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: ryuID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: toraID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	return r, store
}

// putScore PUTs a raw JSON payload to the score endpoint.
func putScore(t *testing.T, r *gin.Engine, compID, matchID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+matchID+"/score", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// kachinukiSub is a shorthand builder for a sub-result JSON object.
func kachinukiSub(position int, sideA, sideB string, ipponsA []string, winner, decision string) map[string]any {
	if ipponsA == nil {
		ipponsA = []string{}
	}
	return map[string]any{
		"position": position,
		"sideA":    sideA,
		"sideB":    sideB,
		"ipponsA":  ipponsA,
		"ipponsB":  []string{},
		"winner":   winner,
		"decision": decision,
	}
}

// TestScoreHandler_KachinukiUnflaggedRunningWriteDoesNotAdvance: a
// mid-bout autosave (running, no kachinukiBoutFinal) already carries a
// sub winner whenever one side leads 1-0. It must NOT trigger
// advancement: the bout is still being fought.
func TestScoreHandler_KachinukiUnflaggedRunningWriteDoesNotAdvance(t *testing.T) {
	compID := "kachinuki-unflagged-autosave"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "running",
		"subResults": []map[string]any{
			// 1-0 lead mid-bout: client autosave sets the sub winner.
			kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", ""),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 1, "unflagged autosave must not append the next bout")
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
	assert.Empty(t, matches[0].Winner)
	assert.Empty(t, matches[0].Decision)
}

// TestScoreHandler_KachinukiBoutFinalAppendsNextBout: the explicit
// "record bout" submit (running + kachinukiBoutFinal) appends the next
// bout and leaves the parent match running with no match-level
// winner/decision (contract D regression).
func TestScoreHandler_KachinukiBoutFinalAppendsNextBout(t *testing.T) {
	compID := "kachinuki-bout-final"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "flagged bout-final write must append bout 2")
	assert.Equal(t, "R-1", matches[0].SubResults[1].SideA, "winner stays on")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "next from lineup")
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "match must stay running")
	assert.Empty(t, matches[0].Winner, "no match-level winner while bouts remain")
	assert.Empty(t, matches[0].Decision, "no match-level decision while bouts remain")
}

// TestScoreHandler_KachinukiPartialWritePreservesAppendedBout: ACID
// data-loss guard (UAT: a recorded bout-1 draw was lost and the
// server-appended bout-2 placeholder silently destroyed). A running
// write whose SubResults carry ONLY bout 1 (stale modal, autosave,
// second operator) must merge by position: bout 1 is overwritten by the
// incoming entry, the stored bout-2 placeholder is preserved.
func TestScoreHandler_KachinukiPartialWritePreservesAppendedBout(t *testing.T) {
	compID := "kachinuki-partial-write-merge"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				// Bout 1 recorded as hikiwake; the engine appended the bout-2
				// placeholder (both senpo retire, next pair steps up).
				{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
				{Position: 2, SideA: "R-2", SideB: "W-2"},
			},
		},
	}))

	// Stale client rewrite: only bout 1, now as an R-1 win (the UAT repro).
	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "running",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "the appended bout-2 placeholder must survive a partial write")
	assert.Equal(t, 1, matches[0].SubResults[0].Position)
	assert.Equal(t, "R-1", matches[0].SubResults[0].Winner, "incoming bout 1 overwrites the stored entry")
	assert.Equal(t, "fought", matches[0].SubResults[0].Decision)
	assert.Equal(t, 2, matches[0].SubResults[1].Position)
	assert.Equal(t, "R-2", matches[0].SubResults[1].SideA, "stored placeholder preserved")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB)
}

// TestScoreHandler_KachinukiOmittedCompletedBoutSurvives: a stored
// COMPLETED bout absent from the incoming patch must be preserved on a
// kachinuki match (bracket path: the write falls through to the bracket
// twin).
func TestScoreHandler_KachinukiOmittedCompletedBoutSurvives(t *testing.T) {
	compID := "kachinuki-omitted-bout-survives"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
						{Position: 2, SideA: "R-1", SideB: "W-2"},
					},
				},
			},
			{
				{ID: "R2M0"},
			},
		},
	}))

	// Patch carries only bout 2 (the current bout): bout 1 must survive.
	w := putScore(t, r, compID, "R1M0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "running",
		"subResults": []map[string]any{
			kachinukiSub(2, "R-1", "W-2", []string{"D"}, "R-1", ""),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	bm := bracket.Rounds[0][0]
	require.Len(t, bm.SubResults, 2, "the stored completed bout 1 must survive a patch that omits it")
	assert.Equal(t, 1, bm.SubResults[0].Position)
	assert.Equal(t, "R-1", bm.SubResults[0].Winner, "completed bout 1 untouched")
	assert.Equal(t, []string{"M", "K"}, bm.SubResults[0].IpponsA)
	assert.Equal(t, 2, bm.SubResults[1].Position)
	assert.Equal(t, []string{"D"}, bm.SubResults[1].IpponsA, "incoming bout 2 applied")
}

// TestScoreHandler_KachinukiBootstrappedBout1: a fresh kachinuki match
// has NO server bout log (the server only appends bouts 2+); the client
// bootstraps bout 1 in the UI (kachinukiVisiblePositions) and submits it
// with the flag. The write must persist bout 1 and append bout 2.
func TestScoreHandler_KachinukiBootstrappedBout1(t *testing.T) {
	compID := "kachinuki-bootstrap-bout1"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "bout 1 persisted and bout 2 appended")
	assert.Equal(t, 1, matches[0].SubResults[0].Position)
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
}

// saveKachinukiBracket persists a 2-round bracket whose first match is
// the running kachinuki encounter Ryu vs Tora with bout 1 open.
func saveKachinukiBracket(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
				},
				{ID: "R1M1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusScheduled},
			},
			{
				{ID: "R2M0"},
			},
		},
	}))
}

// TestScoreHandler_KachinukiBout1HikiwakeKnockout: a bout-level hikiwake
// in knockout kachinuki is a legitimate flagged running write (the match
// is not being completed, so the no-draw rule for knockout completion
// does not apply). Both players retire and the next pair steps up.
func TestScoreHandler_KachinukiBout1HikiwakeKnockout(t *testing.T) {
	compID := "kachinuki-ko-hikiwake"
	r, store := setupKachinukiScoreServer(t, compID)
	saveKachinukiBracket(t, store, compID)

	w := putScore(t, r, compID, "R1M0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", nil, "", "hikiwake"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	bm := bracket.Rounds[0][0]
	require.Len(t, bm.SubResults, 2, "hikiwake retires both; next pair must be appended")
	assert.Equal(t, "R-2", bm.SubResults[1].SideA)
	assert.Equal(t, "W-2", bm.SubResults[1].SideB)
	assert.Equal(t, state.MatchStatusRunning, bm.Status, "match must stay running")
	assert.Empty(t, bm.Winner)
}

// TestScoreHandler_KachinukiPrematureCompletionRejected: a
// non-correction completed write while both rosters still have players
// and the patch carries no daihyosen sub-result must be rejected 409
// (ACID: never silently accepted, never silently dropped).
func TestScoreHandler_KachinukiPrematureCompletionRejected(t *testing.T) {
	compID := "kachinuki-premature-complete"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"winner": "Ryu",
		"status": "completed",
		"subResults": []map[string]any{
			// Bout 1 won by R-1: Tora still has W-2 and W-3 queued, so the
			// encounter is nowhere near decided.
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "kachinuki_premature_completion")

	// The match on disk is untouched.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
	assert.Empty(t, matches[0].Winner)
}

// TestScoreHandler_KachinukiDaihyosenCompletionPropagates: a completed
// write that carries a daihyosen sub-result (position -1 with a winner)
// is the sanctioned tie-after-exhaustion resolution. It must complete
// the bracket match and propagate the winner to the next round via the
// normal completion path.
func TestScoreHandler_KachinukiDaihyosenCompletionPropagates(t *testing.T) {
	compID := "kachinuki-daihyosen-complete"
	r, store := setupKachinukiScoreServer(t, compID)
	saveKachinukiBracket(t, store, compID)

	w := putScore(t, r, compID, "R1M0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"winner": "Ryu",
		"status": "completed",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", nil, "", "hikiwake"),
			{
				"position": -1,
				"sideA":    "Ryu",
				"sideB":    "Tora",
				"ipponsA":  []string{"M"},
				"ipponsB":  []string{},
				"winner":   "Ryu",
				"decision": "daihyosen",
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	bm := bracket.Rounds[0][0]
	assert.Equal(t, state.MatchStatusCompleted, bm.Status)
	assert.Equal(t, "Ryu", bm.Winner)
	assert.Equal(t, "Ryu", bracket.Rounds[1][0].SideA, "winner must propagate to the next round")
}

// TestScoreHandler_KachinukiCompletedCorrectionDoesNotAdvance: a
// correction (completed -> completed, with a reason) must never re-run
// advancement even when flagged, the engine's completed-status guard is
// the defense in depth.
func TestScoreHandler_KachinukiCompletedCorrectionDoesNotAdvance(t *testing.T) {
	compID := "kachinuki-correction-no-advance"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
			Winner: "Ryu", Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"winner":             "Ryu",
		"status":             "completed",
		"correctionReason":   "scoring error",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 1, "a correction must never append a new bout")
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
}

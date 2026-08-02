// Package mobileapp, handlers_match_kachinuki_test.go pins the kachinuki
// bout-submission contract on the score endpoint:
//
//   - Advancement (MaybeAdvanceKachinuki) fires ONLY on a write flagged
//     with the transient kachinukiBoutFinal request field. Unflagged
//     running writes (autosave, where a 1-0 lead already sets the sub
//     winner mid-bout) and completed writes (corrections) never advance.
//   - A bout-1 hikiwake is recordable in knockout kachinuki via a flagged
//     running write: both players retire and the next pair is appended.
//   - Completion is operator-led (mp-gmcg): an explicit completed write is
//     ACCEPTED even while the roster snapshot shows unfought players (the
//     taisho-defeated rule ends a match early, and team sizes are
//     unregulated), and the server strips the trailing auto-appended
//     unscored bout so it never reaches standings/exports.
//   - A daihyosen completion (position -1 sub with winner) completes the
//     bracket match and propagates the winner via the normal path
//     (legacy data only; new daihyosen on kachinuki is rejected 400).
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	// The response must echo the POST-advance bout log (mp-gmcg): the open
	// score editor adopts it to render the appended pairing without a
	// close/reopen; the pre-advance result would hide the new bout.
	var echoed state.MatchResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &echoed))
	require.Len(t, echoed.SubResults, 2, "response must carry the appended bout")
	assert.Equal(t, "R-1", echoed.SubResults[1].SideA)
	assert.Equal(t, "W-2", echoed.SubResults[1].SideB)

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

// TestScoreHandler_KachinukiHikiwakeAppendsWalkoverSlot pins spec 006
// decision 2 + the stays-on ruling through the wire: a hikiwake that
// leaves one side without a replacement while the other side still has
// fighters appends the next bout KEEPING THE FIGHTER WHO JUST TIED on
// that side (under the taisho rule they continue, with nothing to
// re-type) against the surviving side's next fighter. Under plain
// exhaustion the operator gives the survivor the per-bout fusensho and
// Ends on that point (the walkover), or abandons the slot (trailing
// unscored bouts are stripped on the completed write). Record bout —
// not a manual add — is the documented flow.
func TestScoreHandler_KachinukiHikiwakeAppendsWalkoverSlot(t *testing.T) {
	compID := "kachinuki-walkover-slot"
	r, store := setupKachinukiScoreServer(t, compID)
	// W-1 beat R-1 and R-2; bout 3 pairs W-1 against R-3 (Ryu's last).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsB: []string{"M", "K"}, Winner: "W-1", Decision: "fought"},
				{Position: 2, SideA: "R-2", SideB: "W-1", IpponsB: []string{"M"}, Winner: "W-1", Decision: "fought"},
				{Position: 3, SideA: "R-3", SideB: "W-1"},
			},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			{"position": 1, "sideA": "R-1", "sideB": "W-1", "ipponsA": []string{}, "ipponsB": []string{"M", "K"}, "winner": "W-1", "decision": "fought"},
			{"position": 2, "sideA": "R-2", "sideB": "W-1", "ipponsA": []string{}, "ipponsB": []string{"M"}, "winner": "W-1", "decision": "fought"},
			// The hikiwake retires R-3 (Ryu's last known fighter) AND W-1.
			kachinukiSub(3, "R-3", "W-1", nil, "", "hikiwake"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 4, "the next-bout slot must be appended")
	slot := matches[0].SubResults[3]
	assert.Equal(t, 4, slot.Position)
	assert.Equal(t, "R-3", slot.SideA, "the fighter who just tied stays on the slot")
	assert.Equal(t, "W-2", slot.SideB, "surviving side's next fighter comes up")
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "operator-led: the match stays running")
	assert.Empty(t, matches[0].Winner)
	assert.Empty(t, matches[0].Decision)
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

// TestScoreHandler_KachinukiEarlyCompletionAccepted pins the operator-led
// completion contract (mp-gmcg): an explicit "End match" (completed write
// with decision kachinuki-exhaustion and a winner) is ACCEPTED even while
// the roster snapshot says the losing side still has unfought players —
// the taisho-defeated rule legitimately ends a match early, and team
// sizes are unregulated so the snapshot is advisory. The former 409
// premature-completion gate is gone. The server also strips the trailing
// auto-appended unscored bout so the abandoned pairing never reaches
// standings/exports.
func TestScoreHandler_KachinukiEarlyCompletionAccepted(t *testing.T) {
	compID := "kachinuki-early-complete"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				// Bout 1 scored; the engine auto-appended the bout-2 pairing.
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-1", SideB: "W-2"},
			},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":    "Ryu",
		"sideB":    "Tora",
		"winner":   "Ryu",
		"status":   "completed",
		"decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			// Taisho-defeat early end: the operator knows W-1 was Tora's
			// taisho even though the app's snapshot still queues W-2/W-3.
			// The patch omits the appended bout 2; the merge would restore
			// it, so the completed-write strip must remove it server-side.
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the operator's End match must be accepted as-is")
	assert.Equal(t, "Ryu", matches[0].Winner, "winner persisted")
	assert.Equal(t, "kachinuki-exhaustion", matches[0].Decision, "decision persisted")
	require.Len(t, matches[0].SubResults, 1, "the abandoned auto-appended bout must be stripped")
	assert.Equal(t, 1, matches[0].SubResults[0].Position)
	assert.Equal(t, "R-1", matches[0].SubResults[0].Winner)
}

// TestScoreHandler_KachinukiEnchoFinalBoutPersists: a knockout kachinuki
// tie on the FINAL bout resolves by encho on that same bout (the same
// pair keeps fighting; daihyosen does not exist in kachinuki, mp-gmcg).
// A completed write whose final bout carries the encho marker and a
// winner must round-trip the marker + winner through the store and
// propagate the match winner to the next round. The fixture uses
// production-shaped bracket IDs ("m-r{N}-{POS}", buildBracketFromLeaves).
func TestScoreHandler_KachinukiEnchoFinalBoutPersists(t *testing.T) {
	compID := "kachinuki-encho-final-bout"
	r, store := setupKachinukiScoreServer(t, compID)
	// Taisho vs taisho: bouts 1-2 drawn, bout 3 (the final pair) was tied
	// in regulation and went to encho.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
						{Position: 2, SideA: "R-2", SideB: "W-2", Decision: "hikiwake"},
						{Position: 3, SideA: "R-3", SideB: "W-3"},
					},
				},
			},
			{
				{ID: "m-r2-0"},
			},
		},
	}))

	w := putScore(t, r, compID, "m-r1-0", map[string]any{
		"sideA":    "Ryu",
		"sideB":    "Tora",
		"winner":   "Ryu",
		"status":   "completed",
		"decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", nil, "", "hikiwake"),
			kachinukiSub(2, "R-2", "W-2", nil, "", "hikiwake"),
			{
				"position": 3,
				"sideA":    "R-3",
				"sideB":    "W-3",
				"ipponsA":  []string{"M"},
				"ipponsB":  []string{},
				"winner":   "R-3",
				"decision": "fought",
				"encho":    map[string]any{"periodCount": 2},
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
	assert.Equal(t, "kachinuki-exhaustion", bm.Decision)
	require.Len(t, bm.SubResults, 3, "the full bout log must persist")
	final := bm.SubResults[2]
	assert.Equal(t, "R-3", final.Winner, "encho winner persisted on the final bout")
	require.NotNil(t, final.Encho, "encho marker must round-trip through the store")
	assert.Equal(t, 2, final.Encho.PeriodCount)
	assert.Equal(t, "Ryu", bracket.Rounds[1][0].SideA, "winner must propagate to the next round")
}

// TestScoreHandler_KachinukiPoolBoutEnchoAccepted pins the SCOPE of the
// kachinuki bout-level encho exception (allowNumberedEnchoFor): it applies
// in EVERY phase, pools included. Whether the final pairing must produce a
// result (e.g. the taisho must be defeated) is OPERATOR DISCRETION — the
// operator may fight a tied pool pairing on in overtime rather than accept
// the draw, and the app must never hard-code that rule by phase (operator
// ruling superseding an earlier bracket-only scoping).
func TestScoreHandler_KachinukiPoolBoutEnchoAccepted(t *testing.T) {
	compID := "kachinuki-pool-encho-accepted"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool 1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "Pool 1-0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "running",
		"subResults": []map[string]any{
			{
				"position": 1,
				"sideA":    "R-1",
				"sideB":    "W-1",
				"ipponsA":  []string{"M"},
				"ipponsB":  []string{"K"},
				"encho":    map[string]any{"periodCount": 1},
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 1)
	require.NotNil(t, matches[0].SubResults[0].Encho,
		"the overtime marker must persist on the pool bout")
	assert.Equal(t, 1, matches[0].SubResults[0].Encho.PeriodCount)
}

// TestScoreHandler_KachinukiSimultaneousExhaustionNoWinnerIs400: a
// completed winnerless write on a bracket kachinuki match whose rosters
// are BOTH exhausted passes the handler's premature-completion pre-check
// (nothing premature: everyone has fought) and is rejected by the
// engine's validateBracketCompletion instead. That rejection is an
// *engine.ValidationError and must surface as HTTP 400 with the
// "resolve via daihyosen" message: pre-fix the handler only mapped the
// handler-layer ValidationError type, so this surfaced as a 500, which
// the client write-queue treats as transient and retries (mp-q8c6).
func TestScoreHandler_KachinukiSimultaneousExhaustionNoWinnerIs400(t *testing.T) {
	compID := "kachinuki-both-exhausted-400"
	r, store := setupKachinukiScoreServer(t, compID)
	// Size-3 teams with all three bouts drawn: every fighter on both sides
	// has retired, so the encounter is tied and only a daihyosen can end it.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{
				ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
					{Position: 2, SideA: "R-2", SideB: "W-2", Decision: "hikiwake"},
					{Position: 3, SideA: "R-3", SideB: "W-3", Decision: "hikiwake"},
				},
			}},
			{{ID: "R2M0"}},
		},
	}))

	w := putScore(t, r, compID, "R1M0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "completed",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", nil, "", "hikiwake"),
			kachinukiSub(2, "R-2", "W-2", nil, "", "hikiwake"),
			kachinukiSub(3, "R-3", "W-3", nil, "", "hikiwake"),
		},
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "daihyosen", "operator must be told to resolve via daihyosen")

	// The match on disk is untouched.
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusRunning, bracket.Rounds[0][0].Status)
	assert.Empty(t, bracket.Rounds[0][0].Winner)
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

// postReopenRaw POSTs an arbitrary body to the reopen endpoint.
func postReopenRaw(t *testing.T, r *gin.Engine, compID, matchID string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/"+matchID+"/reopen", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// postReopen POSTs to the reopen endpoint with the mandatory audit reason.
func postReopen(t *testing.T, r *gin.Engine, compID, matchID, reason string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]any{"reason": reason})
	require.NoError(t, err)
	return postReopenRaw(t, r, compID, matchID, body)
}

// completedKachinukiPoolMatch is the canonical finished pool encounter
// used by the reopen tests: one decisive bout, ended by the operator.
func completedKachinukiPoolMatch() state.MatchResult {
	return state.MatchResult{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
		Winner: "Ryu", Decision: "kachinuki-exhaustion",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
		},
	}
}

// TestReopenHandler_KachinukiPoolMatch pins the sanctioned reopen path
// (mp-gmcg, spec 006 decision 4): a completed pool kachinuki match goes
// back to running with winner/decision cleared and the bout log intact,
// and a subsequent explicit End match (completed write) works without a
// correction reason (the match is no longer completed).
func TestReopenHandler_KachinukiPoolMatch(t *testing.T) {
	compID := "kachinuki-reopen-pool"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	w := postReopen(t, r, compID, "P1-0", "wrong winner recorded")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "reopen must set the match running")
	assert.Empty(t, matches[0].Winner, "winner cleared")
	assert.Empty(t, matches[0].Decision, "decision cleared")
	require.Len(t, matches[0].SubResults, 1, "the bout log must stay intact")
	assert.Equal(t, "R-1", matches[0].SubResults[0].Winner)

	// The operator adds one more bout and ends the match the other way.
	w = putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":    "Ryu",
		"sideB":    "Tora",
		"winner":   "Tora",
		"status":   "completed",
		"decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
			{
				"position": 2,
				"sideA":    "R-1",
				"sideB":    "W-2",
				"ipponsA":  []string{},
				"ipponsB":  []string{"D", "K"},
				"winner":   "W-2",
				"decision": "fought",
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "a fresh End match after reopen must complete normally")
	assert.Equal(t, "Tora", matches[0].Winner)
	assert.Len(t, matches[0].SubResults, 2)
}

// TestReopenHandler_NonKachinukiRejected: reopen exists ONLY for
// kachinuki. Any other competition keeps the correction path
// (completed -> completed + correctionReason) as its sole sanctioned edit.
func TestReopenHandler_NonKachinukiRejected(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()
	compID := "fixed-team-reopen"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Format:   state.CompFormatMixed,
		TeamSize: 3, // fixed-format team competition (no kachinuki)
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	w := postReopen(t, r, compID, "P1-0", "wrong winner recorded")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "kachinuki")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the finished result must be untouched")
	assert.Equal(t, "Ryu", matches[0].Winner)
}

// TestReopenHandler_NotCompleted409: only a completed match can be
// reopened.
func TestReopenHandler_NotCompleted409(t *testing.T) {
	compID := "kachinuki-reopen-running"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := postReopen(t, r, compID, "P1-0", "wrong winner recorded")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "not completed")
}

// TestReopenHandler_BracketDownstreamStates covers the bracket reopen
// contract: a propagated-but-unfought downstream slot is retracted to its
// "Winner of rX-mY" placeholder; a downstream match that already recorded
// anything rejects the reopen with 409 and leaves the bracket untouched.
func TestReopenHandler_BracketDownstreamStates(t *testing.T) {
	completedR1M0 := func() state.BracketMatch {
		return state.BracketMatch{
			ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
			Winner: "Ryu", Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
	}

	t.Run("downstream filled but unfought: reopened, slot retracted to placeholder", func(t *testing.T) {
		compID := "kachinuki-reopen-bracket-ok"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					completedR1M0(),
					{ID: "R1M1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusScheduled},
				},
				{
					// Winner already propagated into the final, which is untouched.
					{ID: "R2M0", SideA: "Ryu", SideB: "Winner of r2-m1", Status: state.MatchStatusScheduled},
				},
			},
		}))

		w := postReopen(t, r, compID, "R1M0", "semifinal must be re-fought")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := bracket.Rounds[0][0]
		assert.Equal(t, state.MatchStatusRunning, bm.Status)
		assert.Empty(t, bm.Winner)
		assert.Empty(t, bm.Decision)
		require.Len(t, bm.SubResults, 1, "bout log kept")
		assert.Equal(t, "Winner of r2-m0", bracket.Rounds[1][0].SideA,
			"the downstream slot must return to the generation placeholder")
		assert.Equal(t, "Winner of r2-m1", bracket.Rounds[1][0].SideB, "the sibling slot is untouched")
	})

	t.Run("downstream already fought: 409, bracket untouched", func(t *testing.T) {
		compID := "kachinuki-reopen-bracket-blocked"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					completedR1M0(),
					{
						ID: "R1M1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted,
						Winner: "Kuma",
					},
				},
				{
					{
						ID: "R2M0", SideA: "Ryu", SideB: "Kuma", Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1", IpponsA: []string{"M"}}},
					},
				},
			},
		}))

		w := postReopen(t, r, compID, "R1M0", "semifinal must be re-fought")
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "downstream")

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "the reopen must not land")
		assert.Equal(t, "Ryu", bracket.Rounds[0][0].Winner)
		assert.Equal(t, "Ryu", bracket.Rounds[1][0].SideA, "the downstream pairing must be untouched")
	})
}

// TestScoreHandler_KachinukiCompletedToRunningStillNoOps pins that the
// reopen endpoint did NOT weaken the score path's stale-write guard: a
// plain status "running" write against a completed match is still
// silently discarded (stale) rather than reverting the finished result.
// Reopen is the only sanctioned way back to running.
func TestScoreHandler_KachinukiCompletedToRunningStillNoOps(t *testing.T) {
	compID := "kachinuki-stale-guard-survives"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":  "Ryu",
		"sideB":  "Tora",
		"status": "running",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", ""),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"stale":true`, "the write must be discarded as stale, not applied")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the finished result must survive")
	assert.Equal(t, "Ryu", matches[0].Winner)
	assert.Equal(t, "kachinuki-exhaustion", matches[0].Decision)
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

// loadPoolMatch is a small lookup helper for the reopen tests below.
func loadPoolMatch(t *testing.T, store *state.Store, compID, matchID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == matchID {
			return m
		}
	}
	t.Fatalf("match %s not found in %s", matchID, compID)
	return state.MatchResult{}
}

// TestReopenHandler_ReasonRequired pins the reopen audit gate (mp-gmcg).
//
// Every other format requires a non-empty correctionReason to overwrite a
// finalized result, and the reopen endpoint is the only other way to rewrite
// one — so it must demand the same justification, otherwise it is simply the
// way around the audit trail.
func TestReopenHandler_ReasonRequired(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{"no reason field", []byte(`{}`), "reason is required"},
		{"empty reason", []byte(`{"reason":""}`), "reason is required"},
		{"whitespace-only reason", []byte(`{"reason":"   \t "}`), "reason is required"},
		// gin's JSON encoder escapes "<", so match on the tail of the message.
		{"oversized reason", []byte(`{"reason":"` + strings.Repeat("x", MaxLenCorrectionReason+1) + `"}`), "200 characters"},
		{"malformed body", []byte(`{nope`), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compID := "kachinuki-reopen-reason"
			r, store := setupKachinukiScoreServer(t, compID)
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

			w := postReopenRaw(t, r, compID, "P1-0", tc.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			if tc.want != "" {
				assert.Contains(t, w.Body.String(), tc.want)
			}
			m := loadPoolMatch(t, store, compID, "P1-0")
			assert.Equal(t, state.MatchStatusCompleted, m.Status, "a rejected reopen must leave the result finalized")
			assert.Equal(t, "Ryu", m.Winner)
		})
	}
}

// TestReopenHandler_ReasonSurvivesTheReEnd is the audit-trail regression
// (mp-gmcg): the reopen reason is recorded on the match as its
// CorrectionReason, and it must still be there after the operator has
// recorded further bouts and Ended the match again.
//
// Two things used to erase it. (1) The pool write is a whole-struct
// overwrite (`*r = *result` in engine/scoring_tx.go), so every running write
// with an empty CorrectionReason wiped the stored one. (2) The re-End is a
// FIRST finalization (the match is running, not completed), and that branch
// executed `result.CorrectionReason = ""` unconditionally. Net effect: a
// finished kachinuki result could be rewritten with no record of who changed
// it or why — exactly what the correction gate exists to prevent.
func TestReopenHandler_ReasonSurvivesTheReEnd(t *testing.T) {
	compID := "kachinuki-reopen-audit"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	const reason = "bout 1 scored on the wrong sheet"
	w := postReopen(t, r, compID, "P1-0", "  "+reason+"  ")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, reason, loadPoolMatch(t, store, compID, "P1-0").CorrectionReason,
		"the reopen reason must be persisted (trimmed) as the match's correction reason")

	// The operator records the next bout (a plain running write).
	w = putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, reason, loadPoolMatch(t, store, compID, "P1-0").CorrectionReason,
		"a running write must not wipe the reopen audit reason")

	// The operator ends the match again. This is a FIRST finalization (the
	// match is running), so no client reason is supplied — the stored one must
	// survive it.
	w = putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":    "Ryu",
		"sideB":    "Tora",
		"winner":   "Tora",
		"status":   "completed",
		"decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
			{
				"position": 2, "sideA": "R-1", "sideB": "W-2",
				"ipponsA": []string{}, "ipponsB": []string{"D", "K"},
				"winner": "W-2", "decision": "fought",
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, "Tora", m.Winner)
	assert.Equal(t, reason, m.CorrectionReason,
		"the reopen reason is the audit record for this rewrite and must survive the re-End")
}

// TestScoreHandler_FirstFinalizationDropsClientReason is the other half of
// the contract carrying the reopen reason forward: only a STORED reason is
// honoured on a first finalization. A client that volunteers a
// correctionReason on a match that was never completed must still have it
// dropped, so the field keeps meaning "this rewrote a finalized result".
func TestScoreHandler_FirstFinalizationDropsClientReason(t *testing.T) {
	compID := "kachinuki-first-final-reason"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA":            "Ryu",
		"sideB":            "Tora",
		"winner":           "Ryu",
		"status":           "completed",
		"decision":         "kachinuki-exhaustion",
		"correctionReason": "invented by the client",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Empty(t, loadPoolMatch(t, store, compID, "P1-0").CorrectionReason,
		"a client-supplied reason on a genuine first finalization must be dropped")
}

// TestReopenHandler_CourtBusy409 is the two-running-matches-on-one-court
// regression (mp-gmcg).
//
// NOTE ON THE FIXTURES: every other reopen test builds its matches with NO
// Court set, which is precisely why this bug survived — the court gate never
// engaged. Both matches here carry Court "A" deliberately.
//
// Reopen puts the match back in the RUNNING state and court exclusivity keys
// purely on `status == running`, so reopening onto a busy court leaves TWO
// running matches there and the exclusivity check then rejects BOTH: the
// re-End of the reopened match AND — the damaging half — every further score
// write to the match actually being fought. So the reopen is refused, and the
// live match must remain scoreable afterwards.
func TestReopenHandler_CourtBusy409(t *testing.T) {
	compID := "kachinuki-reopen-court-busy"
	r, store := setupKachinukiScoreServer(t, compID)
	finished := completedKachinukiPoolMatch()
	finished.Court = "A"
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		finished,
		// The live match on the same court. Different teams, so only the COURT
		// dimension can reject anything here.
		{ID: "P1-1", SideA: "Kuma", SideB: "Washi", Court: "A", Status: state.MatchStatusRunning},
	}))
	runningRevStore.Delete(compID + ":P1-1")

	w := postReopen(t, r, compID, "P1-0", "need more bouts")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "court_busy")
	assert.Contains(t, w.Body.String(), "P1-1", "the response must name the match holding the court")

	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status, "the refused reopen must not land")
	assert.Equal(t, "Ryu", m.Winner)
	assert.Empty(t, m.CorrectionReason, "a refused reopen must not stamp an audit reason")

	// The damaging half: the genuinely live match on that court must still be
	// scoreable. With the reopen applied, this write returned 409 court_busy
	// and the operator could not score the bout in front of them.
	w = putScore(t, r, compID, "P1-1", map[string]any{
		"sideA": "Kuma", "sideB": "Washi",
		"ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "running",
	})
	require.Equal(t, http.StatusOK, w.Code, "the live match on that court must stay scoreable: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, loadPoolMatch(t, store, compID, "P1-1").Status)
}

// TestScoreHandler_KachinukiBronzeBoutFinalEchoesAppendedBout: the bronze
// (3rd-place) match is a SIBLING of bracket.Rounds, not an element of it.
// MaybeAdvanceKachinuki appends bouts to it like any other bracket match, so
// the handler's post-advance echo must look there too — otherwise "Record
// bout" appends server-side but the response carries the PRE-advance log and
// the new pairing never reaches the open score editor.
func TestScoreHandler_KachinukiBronzeBoutFinalEchoesAppendedBout(t *testing.T) {
	compID := "kachinuki-bronze-echo"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "F0", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusScheduled}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: "B0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
		},
	}))

	w := putScore(t, r, compID, "B0", map[string]any{
		"sideA":              "Ryu",
		"sideB":              "Tora",
		"status":             "running",
		"kachinukiBoutFinal": true,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.ThirdPlaceMatch.SubResults, 2, "the bout must be appended on disk")

	var echoed state.MatchResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &echoed))
	require.Len(t, echoed.SubResults, 2, "the response must echo the appended bronze pairing")
	assert.Equal(t, "R-1", echoed.SubResults[1].SideA, "winner stays on")
	assert.Equal(t, "W-2", echoed.SubResults[1].SideB, "next from the lineup")
}

// TestAnyNumberedBoutHasEncho pins the encho predicate used by both score
// paths to decide whether the kachinuki numbered-bout exception needs the
// competition loaded. It must route through state.EnchoMetadata.On() — the
// single "did this happen in encho" predicate — so a degenerate
// {periodCount: 0} block reads the same here as in validateSubBout.
func TestAnyNumberedBoutHasEncho(t *testing.T) {
	tests := []struct {
		name string
		subs []state.SubMatchResult
		want bool
	}{
		{"empty log", nil, false},
		{"no encho block", []state.SubMatchResult{{Position: 1}}, false},
		{"degenerate zero-period block is not encho", []state.SubMatchResult{{Position: 1, Encho: &state.EnchoMetadata{}}}, false},
		{"real encho on a numbered bout", []state.SubMatchResult{{Position: 2, Encho: &state.EnchoMetadata{PeriodCount: 1}}}, true},
		{
			"encho on the daihyosen only is not a numbered bout",
			[]state.SubMatchResult{{Position: state.DaihyosenSubPosition, Encho: &state.EnchoMetadata{PeriodCount: 1}}},
			false,
		},
		{
			"mixed: the numbered bout still counts",
			[]state.SubMatchResult{
				{Position: state.DaihyosenSubPosition, Encho: &state.EnchoMetadata{PeriodCount: 2}},
				{Position: 1},
				{Position: 2, Encho: &state.EnchoMetadata{PeriodCount: 1}},
			},
			true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, anyNumberedBoutHasEncho(tc.subs))
		})
	}
}

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
	// POST /decision is the OTHER way to finalize a match, so the reopen-audit
	// tests below need it wired against the same store as /score: the whole
	// point is that an obligation created on one endpoint cannot be walked
	// around by picking the other.
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	return r, store
}

// postDecision POSTs a decision payload to the decision endpoint.
func postDecision(t *testing.T, r *gin.Engine, compID, matchID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/"+matchID+"/decision", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
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
// kachinuki bout-level encho exception (allowNumberedEnchoFromStore): it applies
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

func postRequeueAndReopen(t *testing.T, r *gin.Engine, compID, matchID string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/"+matchID+"/requeue-blocker-and-reopen", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// TestRequeueBlockerAndReopenHandler covers the atomic court-busy remedy
// endpoint (mp-gmcg review A4).
func TestRequeueBlockerAndReopenHandler(t *testing.T) {
	setup := func(t *testing.T) (*gin.Engine, *state.Store, string) {
		compID := "requeue-reopen-handler"
		r, store := setupKachinukiScoreServer(t, compID)
		target := completedKachinukiPoolMatch() // P1-0, completed
		target.Court = "A"
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			target,
			{ID: "P1-1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: "A"},
		}))
		return r, store, compID
	}

	t.Run("frees the blocker and reopens the target (200)", func(t *testing.T) {
		r, store, compID := setup(t)

		// A plain reopen is refused (court A busy).
		require.Equal(t, http.StatusConflict, postReopen(t, r, compID, "P1-0", "").Code)

		w := postRequeueAndReopen(t, r, compID, "P1-0", map[string]any{
			"blockerCompId": compID, "blockerMatchId": "P1-1",
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		assert.Equal(t, state.MatchStatusScheduled, loadPoolMatch(t, store, compID, "P1-1").Status, "blocker requeued")
		reopened := loadPoolMatch(t, store, compID, "P1-0")
		assert.Equal(t, state.MatchStatusRunning, reopened.Status, "target reopened")
		assert.True(t, reopened.ReopenPending, "reason-less reopen leaves the audit obligation")
	})

	// mp-gmcg review R1: a COMPLETED match never holds the court as "running",
	// so it is not the blocker (a plain reopen is the correct remedy for a free
	// court). The guard rejects it (400 "not running") rather than destructively
	// requeuing it.
	t.Run("a completed blocker is not running: 400 and the target stays finished", func(t *testing.T) {
		r, store, compID := setup(t)
		w := postRequeueAndReopen(t, r, compID, "P1-0", map[string]any{
			"blockerCompId": compID, "blockerMatchId": "P1-0", // the (completed) target as its own blocker
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "is not running")
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatch(t, store, compID, "P1-0").Status)
	})

	// mp-gmcg review R1: the blocker id is client-supplied. Naming a bystander
	// running on a DIFFERENT court must be a 400 that wipes nothing — otherwise
	// the requeue commits and the reopen then 409s on the court's real occupant.
	t.Run("a blocker on a different court is a 400 and the bystander is untouched", func(t *testing.T) {
		r, store, compID := setup(t)
		matches, _ := store.LoadPoolMatches(compID)
		matches = append(matches, state.MatchResult{
			ID: "P1-2", SideA: "Taka", SideB: "Oni", Status: state.MatchStatusRunning, Court: "B",
			IpponsA: []string{"M"}, // a partial score the wipe would clear
		})
		require.NoError(t, store.SavePoolMatches(compID, matches))

		w := postRequeueAndReopen(t, r, compID, "P1-0", map[string]any{
			"blockerCompId": compID, "blockerMatchId": "P1-2", // wrong court
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		bystander := loadPoolMatch(t, store, compID, "P1-2")
		assert.Equal(t, state.MatchStatusRunning, bystander.Status, "the bystander must not be requeued")
		assert.Equal(t, []string{"M"}, bystander.IpponsA, "the bystander's score must not be wiped")
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatch(t, store, compID, "P1-0").Status, "target not reopened")
	})

	t.Run("an unknown blocker is a 404", func(t *testing.T) {
		r, _, compID := setup(t)
		w := postRequeueAndReopen(t, r, compID, "P1-0", map[string]any{
			"blockerCompId": compID, "blockerMatchId": "no-such-match",
		})
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("a missing blockerMatchId is a 400", func(t *testing.T) {
		r, _, compID := setup(t)
		w := postRequeueAndReopen(t, r, compID, "P1-0", map[string]any{"blockerCompId": compID})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
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

// TestDecisionHandler_KachinukiReopenPendingRequiresReason pins E3's kachinuki
// branch (mp-gmcg review): the /decision handler keeps the reopen-pending
// snapshot read ONLY for kachinuki, and a decision that completes a REOPENED
// kachinuki match must still carry the audit reason. If E3 wrongly skipped the
// read for kachinuki too, the obligation would silently vanish and the kiken
// would land unaudited instead of 400-ing on the missing reason.
func TestDecisionHandler_KachinukiReopenPendingRequiresReason(t *testing.T) {
	compID := "kachinuki-decision-reopen"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	// Reason-less reopen: the match goes back to running with ReopenPending set,
	// the audit obligation now owed by whatever finalizes it next.
	require.Equal(t, http.StatusOK, postReopen(t, r, compID, "P1-0", "").Code)
	require.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending)

	// A kiken decision with NO reason must be refused: /decision is the OTHER
	// way to finalize, so it collects the reopen reason too.
	w := postDecision(t, r, compID, "P1-0", map[string]any{"decision": "kiken", "decisionBy": "aka"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "decisionReason")
	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.True(t, m.ReopenPending, "a blocked decision leaves the obligation outstanding")
	assert.Equal(t, state.MatchStatusRunning, m.Status)

	// With a reason it lands and the obligation is discharged.
	w = postDecision(t, r, compID, "P1-0", map[string]any{"decision": "kiken", "decisionBy": "aka", "decisionReason": "Ryu withdrew, injured"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m = loadPoolMatch(t, store, compID, "P1-0")
	assert.False(t, m.ReopenPending, "the reason discharged the obligation")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
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

// TestReopenHandler_ReasonRejections pins what the reopen endpoint STILL
// rejects (mp-gmcg). The reason itself is optional now (see
// TestReopenHandler_ReasonOptional), but a supplied one must fit the same
// 200-character audit cap as correctionReason, and a body that is neither
// absent nor valid JSON is a client bug, not a one-tap reopen.
func TestReopenHandler_ReasonRejections(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
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
			assert.False(t, m.ReopenPending, "a rejected reopen leaves nothing outstanding")
		})
	}
}

// TestReopenHandler_ReasonOptional is the one-tap regression (mp-gmcg).
//
// Reopen used to demand a justification up front, which was exactly the wrong
// moment to ask for one: the operator is at a shiaijo, mid-session, and ended
// the match by mistake. So an absent body, an absent field, an empty string
// and a whitespace-only string all reopen the match — and every one of them
// leaves ReopenPending set, which is what carries the audit obligation
// forward to the next completion (TestReopenHandler_PendingReasonEnforcedOnReEnd).
func TestReopenHandler_ReasonOptional(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"absent body", nil},
		{"no reason field", []byte(`{}`)},
		{"empty reason", []byte(`{"reason":""}`)},
		{"whitespace-only reason", []byte(`{"reason":"   \t "}`)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compID := "kachinuki-reopen-one-tap"
			r, store := setupKachinukiScoreServer(t, compID)
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

			w := postReopenRaw(t, r, compID, "P1-0", tc.body)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			m := loadPoolMatch(t, store, compID, "P1-0")
			assert.Equal(t, state.MatchStatusRunning, m.Status, "the match must be reopened")
			assert.Empty(t, m.Winner, "winner cleared")
			assert.Empty(t, m.CorrectionReason, "no reason was given, so none is recorded yet")
			assert.True(t, m.ReopenPending,
				"a reason-less reopen must record that a justification is still outstanding")
			require.Len(t, m.SubResults, 1, "the bout log must stay intact")
		})
	}
}

// TestReopenHandler_ReasonSuppliedLeavesNothingOutstanding: a reopen that DID
// carry a reason is already justified at the moment it happens, so it must not
// also demand one on the next End — that would make volunteering a reason
// strictly worse than staying silent.
func TestReopenHandler_ReasonSuppliedLeavesNothingOutstanding(t *testing.T) {
	compID := "kachinuki-reopen-justified"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	w := postReopen(t, r, compID, "P1-0", "ended one bout too early")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, "ended one bout too early", m.CorrectionReason)
	assert.False(t, m.ReopenPending, "a justified reopen owes nothing")

	// The re-End therefore needs no reason of its own.
	w = putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu",
		"status": "completed", "decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.MatchStatusCompleted, loadPoolMatch(t, store, compID, "P1-0").Status)
}

// endKachinukiPoolMatch is the operator's "End match" write for the canonical
// pool fixture, optionally carrying a correctionReason.
func endKachinukiPoolMatch(reason string) map[string]any {
	payload := map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu",
		"status": "completed", "decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	}
	if reason != "" {
		payload["correctionReason"] = reason
	}
	return payload
}

// TestReopenHandler_PendingReasonEnforcedOnReEnd is the audit regression that
// makes the one-tap reopen safe (mp-gmcg).
//
// Reopen no longer collects a justification, so the ONLY thing standing
// between a discarded finalized result and no audit record at all is this
// gate: a match flagged ReopenPending cannot be completed again without a
// correctionReason. Losing it would mean a finished result could be rewritten
// with no record of who changed it or why — the exact hole the correction gate
// exists to close.
func TestReopenHandler_PendingReasonEnforcedOnReEnd(t *testing.T) {
	compID := "kachinuki-reopen-pending"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "P1-0", nil).Code)
	require.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending)

	// A running write in between must NOT discharge the obligation: the whole
	// -struct pool overwrite would otherwise blank the flag.
	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "status": "running",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending,
		"a running write must not discharge the outstanding justification")

	// A client cannot clear its own obligation by sending the flag.
	w = putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "status": "running",
		"reopenPending": false,
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending,
		"reopenPending is server-owned: a client-supplied false must be ignored")

	// Ending it again without a reason is refused, and the match stays open so
	// the operator can retry with one.
	w = putScore(t, r, compID, "P1-0", endKachinukiPoolMatch(""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "correctionReason")
	assert.Contains(t, w.Body.String(), "reopened",
		"the message must name the cause; the operator did not knowingly perform a 'correction'")
	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status, "a refused End must leave the match open")
	assert.True(t, m.ReopenPending, "the obligation survives a refused End")

	// With a reason it completes, the reason is recorded, and nothing is
	// outstanding any more.
	const reason = "Ended by mistake: bout 2 was still to be fought"
	w = putScore(t, r, compID, "P1-0", endKachinukiPoolMatch(reason))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m = loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, reason, m.CorrectionReason, "the justification must be persisted")
	assert.False(t, m.ReopenPending, "the flag must be cleared once the record exists")
}

// TestReopenHandler_PendingReasonEnforcedOnBulkScore pins the SAME rule on the
// bulk-score path. Both paths share applyCorrectionReasonUnderTx precisely so
// the audit gate cannot be walked around by picking the other endpoint.
func TestReopenHandler_PendingReasonEnforcedOnBulkScore(t *testing.T) {
	compID := "kachinuki-reopen-bulk"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))
	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "P1-0", nil).Code)

	bulk := func(reason string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal([]state.MatchResult{{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Winner: "Ryu",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			CorrectionReason: reason,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
			},
		}})
		require.NoError(t, err)
		w := httptest.NewRecorder()
		req, err := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/bulk-score", bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	w := bulk("")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var noReason struct {
		Succeeded int `json:"succeeded"`
		Errors    []struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &noReason))
	assert.Zero(t, noReason.Succeeded, "a reason-less completion of a reopened match must not land")
	require.Len(t, noReason.Errors, 1)
	assert.Contains(t, noReason.Errors[0].Error, "reopened")
	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.True(t, m.ReopenPending)

	w = bulk("Ended by mistake: bulk correction")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var withReason struct {
		Succeeded int `json:"succeeded"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &withReason))
	assert.Equal(t, 1, withReason.Succeeded)
	m = loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, "Ended by mistake: bulk correction", m.CorrectionReason)
	assert.False(t, m.ReopenPending, "the flag must be cleared once the record exists")
}

// TestReopenHandler_PendingReasonEnforcedOnBracketMatch is the bracket half of
// the same contract. It is a genuinely separate path: the bracket score write
// copies the payload into the stored BracketMatch field by field and
// deliberately never reads ReopenPending off the client-supplied body, so
// without dischargeReopenPendingUnderTx the flag would survive the justified End
// and demand a fresh reason on every subsequent write.
func TestReopenHandler_PendingReasonEnforcedOnBracketMatch(t *testing.T) {
	compID := "kachinuki-reopen-bracket-pending"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
					Winner: "Ryu", Decision: "kachinuki-exhaustion",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
					},
				},
			},
		},
	}))

	loadBM := func() state.BracketMatch {
		t.Helper()
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, b)
		return b.Rounds[0][0]
	}

	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "R1M0", nil).Code)
	bm := loadBM()
	require.Equal(t, state.MatchStatusRunning, bm.Status)
	assert.True(t, bm.ReopenPending, "a reason-less bracket reopen must flag the justification as outstanding")

	end := func(reason string) map[string]any {
		payload := map[string]any{
			"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu",
			"status": "completed", "decision": "kachinuki-exhaustion",
			"subResults": []map[string]any{
				kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", "fought"),
			},
		}
		if reason != "" {
			payload["correctionReason"] = reason
		}
		return payload
	}

	w := putScore(t, r, compID, "R1M0", end(""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "reopened")
	assert.Equal(t, state.MatchStatusRunning, loadBM().Status, "a refused End must leave the match open")

	const reason = "Ended by mistake: semifinal was still live"
	w = putScore(t, r, compID, "R1M0", end(reason))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bm = loadBM()
	assert.Equal(t, state.MatchStatusCompleted, bm.Status)
	assert.Equal(t, reason, bm.CorrectionReason)
	assert.False(t, bm.ReopenPending, "the bracket flag must be cleared once the record exists")

	// And the next ordinary correction is gated only by the normal
	// completed -> completed rule, not by a stale reopen obligation.
	w = putScore(t, r, compID, "R1M0", end("Scoring error: wrong waza"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, loadBM().ReopenPending)
}

// TestReopenHandler_PendingReasonEnforcedOnDecisionPoolMatch closes the hole
// the one-tap reopen opened: POST /decision is the OTHER way to finalize a
// match, so an outstanding justification has to be collected there too.
//
// The POOL failure was a SILENT DISCHARGE. RecordDecisionTx builds its own
// MatchResult and the pool write is a whole-struct overwrite (`*r = *result`),
// so ReopenPending was blanked as a side effect: the match completed, the flag
// vanished, and no audit record was ever written. An operator could reopen a
// finalized encounter with one tap, record a kiken, and leave no trace that
// the earlier result had been discarded.
func TestReopenHandler_PendingReasonEnforcedOnDecisionPoolMatch(t *testing.T) {
	compID := "kachinuki-reopen-decision-pool"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedKachinukiPoolMatch()}))

	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "P1-0", nil).Code)
	require.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending)

	// Reason-less decision is refused, and the match stays open to retry.
	w := postDecision(t, r, compID, "P1-0", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro",
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "decisionReason",
		"the endpoint must name its OWN audit field, not /score's correctionReason")
	assert.Contains(t, w.Body.String(), "reopened", "the message must name the cause")
	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusRunning, m.Status, "a refused decision must leave the match open")
	assert.True(t, m.ReopenPending, "the obligation survives a refused decision")

	// Whitespace is not a justification.
	w = postDecision(t, r, compID, "P1-0", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro", "decisionReason": "   ",
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.True(t, loadPoolMatch(t, store, compID, "P1-0").ReopenPending)

	// With a reason it lands, and the reason reaches CorrectionReason so the
	// audit field means the same thing whichever endpoint finalized the match.
	const reason = "Ended by mistake: Ryu withdrew before bout 2"
	w = postDecision(t, r, compID, "P1-0", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro", "decisionReason": reason,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m = loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, reason, m.CorrectionReason, "the justification must be persisted, not just accepted")
	assert.False(t, m.ReopenPending, "the flag must be cleared once the record exists")
	// CorrectionReason is not a nicety here, it is the ONLY durable home for
	// the justification on this path: participants.csv-style pool storage has
	// no DecisionReason column (see the header in state/pools.go), so a pool
	// match's decisionReason lives only in the response and the SSE broadcast
	// and is gone on the next load. Requiring the reason WITHOUT copying it
	// into a persisted field would have re-created the very bug this closes,
	// just one reload later.
	assert.Empty(t, m.DecisionReason,
		"pool storage has no DecisionReason column; if that ever changes this assertion should flip, "+
			"but the CorrectionReason copy above must stay either way")
	assert.NotEmpty(t, m.SubResults, "the bout log survives the decision (FIK Art. 32)")
}

// TestReopenHandler_PendingReasonEnforcedOnDecisionBracketMatch is the same
// contract on the bracket, where the pre-fix bug was the OPPOSITE one. The
// bracket write is field-by-field and never copies ReopenPending, so a
// decision left the flag set on a COMPLETED match: no audit record, and every
// later write refused for an obligation that could no longer be discharged.
// One shared discharge pass fixes both, which is why both are pinned.
func TestReopenHandler_PendingReasonEnforcedOnDecisionBracketMatch(t *testing.T) {
	compID := "kachinuki-reopen-decision-bracket"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "R1M0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
					Winner: "Ryu", Decision: "kachinuki-exhaustion",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
					},
				},
			},
		},
	}))
	loadBM := func() state.BracketMatch {
		t.Helper()
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, b)
		return b.Rounds[0][0]
	}

	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "R1M0", nil).Code)
	require.True(t, loadBM().ReopenPending)

	w := postDecision(t, r, compID, "R1M0", map[string]any{
		"decision": "fusenpai", "decisionBy": "aka",
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, loadBM().Status)

	const reason = "Ended by mistake: Tora did not answer the call"
	w = postDecision(t, r, compID, "R1M0", map[string]any{
		"decision": "fusenpai", "decisionBy": "aka", "decisionReason": reason,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bm := loadBM()
	assert.Equal(t, state.MatchStatusCompleted, bm.Status)
	assert.Equal(t, reason, bm.CorrectionReason)
	assert.False(t, bm.ReopenPending, "a completed match must never keep an undischargeable obligation")
	assert.NotEmpty(t, bm.SubResults, "the bout log survives the decision")
}

// TestDecisionHandler_UnreopenedMatchNeedsNoReason guards the blast radius: the
// gate keys on ReopenPending ALONE, so the ordinary kiken/fusenpai flow (by far
// the common case, and the one RemainingMatchesPanel drives in bulk) must stay
// reasonless. A gate that fired on every decision would be a worse bug than the
// hole it closed.
func TestDecisionHandler_UnreopenedMatchNeedsNoReason(t *testing.T) {
	compID := "kachinuki-decision-no-reopen"
	r, store := setupKachinukiScoreServer(t, compID)
	m := completedKachinukiPoolMatch()
	m.Status = state.MatchStatusRunning
	m.Winner = ""
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{m}))

	w := postDecision(t, r, compID, "P1-0", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, stored.Status)
	assert.Empty(t, stored.CorrectionReason, "a first finalization is not a correction")
}

// TestReopenHandler_PendingReasonEnforcedOnBronzeMatch runs the same contract
// against the bronze (3rd-place) match, which is a SIBLING of bracket.Rounds
// rather than an element of it. A rounds-only loop never reaches it, so a
// bronze that was reopened would keep its outstanding justification forever
// and every later write would be refused — the exact branch-omission bug
// lookupMatchSnapshot's single traversal exists to prevent.
func TestReopenHandler_PendingReasonEnforcedOnBronzeMatch(t *testing.T) {
	compID := "kachinuki-reopen-bronze-pending"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "F0", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusScheduled}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: "B0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusCompleted,
			Winner: "Ryu", Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	loadBronze := func() state.BracketMatch {
		t.Helper()
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, b)
		require.NotNil(t, b.ThirdPlaceMatch)
		return *b.ThirdPlaceMatch
	}

	require.Equal(t, http.StatusOK, postReopenRaw(t, r, compID, "B0", nil).Code)
	assert.True(t, loadBronze().ReopenPending)

	end := func(reason string) map[string]any {
		payload := map[string]any{
			"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu",
			"status": "completed", "decision": "kachinuki-exhaustion",
			"subResults": []map[string]any{
				kachinukiSub(1, "R-1", "W-1", []string{"M"}, "R-1", "fought"),
			},
		}
		if reason != "" {
			payload["correctionReason"] = reason
		}
		return payload
	}

	w := putScore(t, r, compID, "B0", end(""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "reopened")
	assert.Equal(t, state.MatchStatusRunning, loadBronze().Status)

	const reason = "Ended by mistake: bronze was still live"
	w = putScore(t, r, compID, "B0", end(reason))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	bronze := loadBronze()
	assert.Equal(t, state.MatchStatusCompleted, bronze.Status)
	assert.Equal(t, reason, bronze.CorrectionReason)
	assert.False(t, bronze.ReopenPending, "the bronze flag must be cleared once the record exists")
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

// deleteKachinukiBout DELETEs the kachinuki empty-bout removal endpoint.
func deleteKachinukiBout(t *testing.T, r *gin.Engine, compID, matchID string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodDelete, "/api/competitions/"+compID+"/matches/"+matchID+"/kachinuki-bout", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	return w
}

// TestRemoveKachinukiBoutHandler covers the DELETE .../kachinuki-bout wiring
// (mp-gmcg): the operator undo for a pairing appended by mistake. 200 strips a
// trailing unscored bout; a scored trailing bout and a completed match both 409;
// a non-kachinuki competition 400s; an unknown match 404s.
func TestRemoveKachinukiBoutHandler(t *testing.T) {
	scored := state.SubMatchResult{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"}
	appended := state.SubMatchResult{Position: 2, SideA: "R-1", SideB: "W-2"}

	t.Run("200 strips a trailing unscored bout", func(t *testing.T) {
		compID := "rm-bout-ok"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{scored, appended}},
		}))

		w := deleteKachinukiBout(t, r, compID, "P1-0")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches[0].SubResults, 1, "the appended empty bout is gone")
	})

	t.Run("409 when the trailing bout is scored", func(t *testing.T) {
		compID := "rm-bout-scored"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{scored}},
		}))

		w := deleteKachinukiBout(t, r, compID, "P1-0")
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})

	t.Run("409 on a completed match", func(t *testing.T) {
		compID := "rm-bout-done"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusCompleted, SubResults: []state.SubMatchResult{scored, appended}},
		}))

		w := deleteKachinukiBout(t, r, compID, "P1-0")
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})

	t.Run("404 for an unknown match", func(t *testing.T) {
		compID := "rm-bout-nf"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

		w := deleteKachinukiBout(t, r, compID, "nope")
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	t.Run("400 for a non-kachinuki competition", func(t *testing.T) {
		compID := "rm-bout-fixed"
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		eng := engine.New(store)
		hub := NewHub()
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{scored, appended}},
		}))
		gin.SetMode(gin.TestMode)
		r := gin.New()
		RegisterMatchHandlers(r.Group("/api"), eng, store, store, hub, NewFileVerifier(store), store)

		w := deleteKachinukiBout(t, r, compID, "P1-0")
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})

	t.Run("400 for a matchId over the length cap", func(t *testing.T) {
		// Parity with the sibling reopen/score routes: an over-long matchId is
		// rejected up front by validateMaxLen (mp-gmcg review), before the
		// engine lookup that would otherwise 404 it.
		compID := "rm-bout-longmid"
		r, _ := setupKachinukiScoreServer(t, compID)

		w := deleteKachinukiBout(t, r, compID, strings.Repeat("x", MaxLenMatchID+1))
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "matchId")
	})
}

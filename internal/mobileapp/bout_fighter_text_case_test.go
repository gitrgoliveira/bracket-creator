package mobileapp

import (
	"net/http"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fighter name typed on a team bout row is stored title-cased as the write
// enters (operator decision D14), and a bout already fought keeps the name it
// was recorded with (D18): only a name the row introduces is changed.

func TestTitleCaseTypedFighters(t *testing.T) {
	stored := &state.MatchResult{SubResults: []state.SubMatchResult{
		{Position: 1, SideA: "ito ken", SideB: "Ueno", Winner: "ito ken", IpponsA: []string{"M"}},
		{Position: state.DaihyosenSubPosition, SideA: "tora", SideB: "kuma"},
	}}
	rows := []state.SubMatchResult{
		// An echo of the stored row: left as recorded, winner included.
		{Position: 1, SideA: "ito ken", SideB: "Ueno", Winner: "ito ken", IpponsA: []string{"M"}},
		// A new row: both names title-cased, the winner carried with its side.
		{Position: 2, SideA: "ito ken", SideB: "kato jun", Winner: "kato jun"},
		// The representative bout names the teams and is never touched.
		{Position: state.DaihyosenSubPosition, SideA: "tora", SideB: "kuma"},
	}

	titleCaseTypedFighters(rows, stored)

	assert.Equal(t, "ito ken", rows[0].SideA, "a bout already fought keeps its name")
	assert.Equal(t, "ito ken", rows[0].Winner)
	assert.Equal(t, "Ito Ken", rows[1].SideA)
	assert.Equal(t, "Kato Jun", rows[1].SideB)
	assert.Equal(t, "Kato Jun", rows[1].Winner, "the winner follows its side's new spelling")
	assert.Equal(t, "tora", rows[2].SideA)
	assert.Equal(t, "kuma", rows[2].SideB)

	// A changed name on a stored row is one the row introduces.
	changed := []state.SubMatchResult{{Position: 1, SideA: "mori jun", SideB: "Ueno", Winner: "mori jun"}}
	titleCaseTypedFighters(changed, stored)
	assert.Equal(t, "Mori Jun", changed[0].SideA)
	assert.Equal(t, "Mori Jun", changed[0].Winner)

	// No stored match: every numbered row is new.
	fresh := []state.SubMatchResult{{Position: 1, SideA: "sato", SideB: ""}}
	titleCaseTypedFighters(fresh, nil)
	assert.Equal(t, "Sato", fresh[0].SideA)
	assert.Empty(t, fresh[0].SideB)
}

func TestScoreHandler_TypedFighterIsStoredTitleCased(t *testing.T) {
	compID := "kachinuki-typed-fighter"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "old name", SideB: "W-1", IpponsA: []string{"M"}, Winner: "old name"}},
	}}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "status": "running",
		"subResults": []map[string]any{
			kachinukiSub(1, "old name", "W-1", []string{"M"}, "old name", ""),
			kachinukiSub(2, "old name", "kato jun", []string{}, "", ""),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	require.Len(t, m.SubResults, 2)
	assert.Equal(t, "old name", m.SubResults[0].SideA, "the bout already fought keeps its recorded name")
	assert.Equal(t, "old name", m.SubResults[0].Winner)
	assert.Equal(t, "Old Name", m.SubResults[1].SideA, "a name the new row introduces is title-cased")
	assert.Equal(t, "Kato Jun", m.SubResults[1].SideB)
}

// bulk-score stores a typed fighter as /score does: a name a row introduces is
// title-cased, a bout already fought keeps its recorded name.
func TestBulkScore_TypedFighterIsStoredTitleCased(t *testing.T) {
	compID := "kachinuki-bulk-typed-fighter"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "old name", SideB: "W-1", IpponsA: []string{"M"}, Winner: "old name"}},
	}}))

	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{{
		"id": "P1-0", "sideA": "Ryu", "sideB": "Tora", "status": "running",
		"subResults": []map[string]any{
			kachinukiSub(1, "old name", "W-1", []string{"M"}, "old name", ""),
			kachinukiSub(2, "old name", "kato jun", []string{}, "", ""),
		},
	}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"succeeded":1`, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	require.Len(t, m.SubResults, 2)
	assert.Equal(t, "old name", m.SubResults[0].SideA, "the bout already fought keeps its recorded name")
	assert.Equal(t, "Old Name", m.SubResults[1].SideA, "a name the new row introduces is title-cased")
	assert.Equal(t, "Kato Jun", m.SubResults[1].SideB)
}

// A finish that typed a new fighter lands title-cased; its exact resend, still
// typed, is title-cased again before the correction-reason check and so reads
// as the finish already recorded, never as a correction needing a reason.
func TestScoreHandler_ResendOfAFinishWithATypedFighterIsAnsweredAsRecorded(t *testing.T) {
	const compID = "typed-fighter-resend"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
	}}))
	at := time.Now().UnixMilli() - 5_000
	finish := func() map[string]any {
		return map[string]any{
			"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu", "status": "completed", "decision": "kachinuki-exhaustion",
			"modifiedAt": at,
			"subResults": []map[string]any{
				kachinukiSub(1, "kato jun", "W-1", []string{"M", "K"}, "kato jun", "fought"),
			},
		}
	}
	w := putScore(t, r, compID, "P1-0", finish())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := loadPoolMatch(t, store, compID, "P1-0")
	require.Len(t, m.SubResults, 1)
	assert.Equal(t, "Kato Jun", m.SubResults[0].SideA)
	assert.Equal(t, "Kato Jun", m.SubResults[0].Winner)
	versionBefore := store.FileVersion(compID, "pool-matches.csv")

	requireAnsweredAsRecorded(t, putScore(t, r, compID, "P1-0", finish()), store, compID, "pool-matches.csv", versionBefore)
}

func TestScoreEventsBody_FighterNameIsTitleCased(t *testing.T) {
	body := scoreEventsBody{Session: "dev-1", Events: []domain.ScoreEvent{
		{ID: "e1", Kind: domain.ScoreBoutFighter, Pos: 2, Side: domain.ScoreSideA, Name: "  kato jun ", Stamp: 1},
	}}
	require.NoError(t, validScoreEventsBody(&body))
	assert.Equal(t, "Kato Jun", body.Events[0].Name)
}

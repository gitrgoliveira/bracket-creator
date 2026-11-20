package mobileapp

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kachinukiMembersUnreadSentence is the roster's warning when team-members.yaml
// cannot be read (operator decision Q5 (a)). Spelled out here, not taken from
// the engine, so a change to what the operator reads is a change to this test.
const kachinukiMembersUnreadSentence = "Team members could not be read, so fighters are not seated by member number. Add each bout's pairing by hand."

// corruptCompetitionFile overwrites a competition file with bytes no YAML
// reader accepts.
func (w *noLineupWorld) corruptCompetitionFile(name string) {
	w.t.Helper()
	require.NoError(w.t, os.WriteFile(filepath.Join(w.store.GetFolder(), "competitions", w.comp, name), []byte("not: [valid yaml"), 0o600))
}

// pointOnBoutOne scores bout 1 for side A through the score-events door, the
// court's tap, and answers the raw answer.
func (w *noLineupWorld) pointOnBoutOne() []byte {
	w.t.Helper()
	rec := w.send(http.MethodPost, w.path("/score-events"), map[string]any{
		"session": "dev-1",
		"events": []domain.ScoreEvent{{ID: "unread-point", Stamp: time.Now().UnixMilli() - 500,
			Kind: domain.ScoreIpponAdd, Pos: 1, Side: domain.ScoreSideA, Letter: "M"}},
	})
	require.Equal(w.t, http.StatusOK, rec.Code, "the point: %s", rec.Body.String())
	return rec.Body.Bytes()
}

// A no-lineup kachinuki encounter fields its members by number, so the roster
// needs team-members.yaml. When that file cannot be read the roster read still
// answers 200 (Q5 (a)): it logs an ERROR naming the file, fills no place by
// number, and carries the members sentence, and nobody reads as having no
// fighters left. The court keeps scoring: a tap and Record bout both answer 200,
// and the tap's answer carries no warning (the roster read alone tells the
// operator).
func TestKachinukiRoster_MembersUnreadableWarnsAndScoringGoesOn(t *testing.T) {
	w := newNoLineupWorld(t, false)
	w.start()
	logs := captureLog(t)
	w.corruptCompetitionFile("team-members.yaml")

	rec := w.send(http.MethodGet, w.path("/kachinuki-roster"), nil)
	require.Equal(t, http.StatusOK, rec.Code, "an unread file is no error: %s", rec.Body.String())
	raw := rec.Body.String()
	assert.Contains(t, raw, `"sideA":`)
	assert.Contains(t, raw, `"sideB":`)
	assert.Contains(t, raw, `"remaining":[]`, "nobody is queued, and the list is present, never null")
	r := w.roster()
	assert.Equal(t, kachinukiMembersUnreadSentence, r.Warning)
	assert.False(t, r.SideA.Exhausted, "never no fighters left under a warning")
	assert.False(t, r.SideB.Exhausted, "never no fighters left under a warning")
	assert.Empty(t, r.SideA.Remaining)
	assert.Empty(t, r.SideB.Remaining)
	assert.Contains(t, logs.String(), "ERROR")
	assert.Contains(t, logs.String(), "team-members.yaml", "the ERROR names the file")

	answer := w.pointOnBoutOne()
	assert.NotContains(t, string(answer), `"warning"`, "the tap's answer carries no warning")
	assert.Equal(t, []string{"M"}, state.SubResultAt(w.stored().SubResults, 1).IpponsA, "the point lands")

	// Nobody is seated by number, so bout 1 names no fighter until the operator
	// adds one by hand. Record bout over a bout that names no fighter answers
	// 200 (recordBout requires it), keeps the bout's points and appends nothing;
	// the advance skips a bout with an outcome and no side names, so it is not
	// marked recorded either.
	m := w.recordBout(false)
	require.Len(t, m.SubResults, 1, "nothing is appended")
	bout1 := state.SubResultAt(m.SubResults, 1)
	assert.Equal(t, []string{"M", "K"}, bout1.IpponsA)
	assert.Empty(t, bout1.SideAMemberID+bout1.SideBMemberID, "nobody is seated by number")
	assert.False(t, bout1.Recorded, "a bout naming no fighter is not recorded")

	// The warning's way on: the operator types bout 1's fighters by hand and
	// taps Record bout again. The typed names are stored and the bout is
	// recorded.
	w.stamp = max(w.next(), w.stored().ModifiedAt+1)
	rec = w.send(http.MethodPut, w.path("/score"), map[string]any{
		"sideA": "Tora", "sideB": "Washi", "sideAId": w.teamA, "sideBId": w.teamB,
		"status": "running", "kachinukiBoutFinal": true, "modifiedAt": w.stamp,
		"ipponsA": []string{}, "ipponsB": []string{},
		"subResults": []map[string]any{{
			"position": 1, "sideA": "Kato", "sideB": "Ueno", "ipponsA": []string{"M", "K"}, "ipponsB": []string{},
			"winner": "Kato", "decision": "fought",
		}},
	})
	require.Equal(t, http.StatusOK, rec.Code, "Record bout by hand: %s", rec.Body.String())
	m = w.stored()
	bout1 = state.SubResultAt(m.SubResults, 1)
	assert.Equal(t, "Kato", bout1.SideA, "the typed fighter is stored: %+v", m.SubResults)
	assert.Equal(t, "Ueno", bout1.SideB)
	assert.True(t, bout1.Recorded, "the bout is recorded: %+v", m.SubResults)
}

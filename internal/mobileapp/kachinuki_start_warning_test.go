package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A kachinuki start that keeps the match's own lineup at the start (a copy
// made after the start moves back to it) works the team's later matches out
// again, as a lineup save does. A later match's carried lineup the domain
// refuses as worked out stays as stored and the start stands, as for a save,
// and the operator is told on the start's own answer (`warning`), never only
// in the log (stage 6 review 9, G1).
func TestKachinukiStart_ACarriedLineupThatCannotFollowTheStartIsTold(t *testing.T) {
	e := newCarryEnv(t, true)
	p1, p2, p5 := domain.PositionNumbered(1), domain.PositionNumbered(2), domain.PositionNumbered(5)
	e.save("", 1, "Mori", 35)
	// P1-1's own lineup, a copy made at 40 holding a position the team size
	// has no place for (kept from older data).
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: e.id["Taka"], MatchID: "P1-1",
		Positions: map[domain.Position]string{p1: "Mori", p2: "Ueda", p5: "Endo"},
		Stamps:    map[domain.Position]int64{p1: e.base + 35, p2: e.base - 1000, p5: e.base + 40},
		CopiedAt:  e.base + 40, Own: []domain.Position{p5}}, 6))
	// P1-0's own lineup, a copy made at 50: after P1-1's, so P1-1 does not
	// read it yet.
	e.save("P1-0", 2, "Doi", 50)
	before, ok := e.lineup("P1-1")
	require.True(t, ok)
	// The start at 30 keeps P1-0's lineup from 30 on: P1-1 at 40 now reads it.
	rec := e.do(http.MethodPut, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape("P1-0")+"/score", map[string]any{
		"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": e.base + 30, "writeId": "start-P1-0",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, state.MatchStatusRunning, e.stored("P1-0").Status, "the start stands")
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "The match was started, but the lineup carried to P1-1 could not be worked out again "+
		"(a position the team size has no place for); it was kept as it was. Check that match's lineup.", body["warning"],
		"the operator is told on the start's answer: %s", rec.Body.String())
	after, ok := e.lineup("P1-1")
	require.True(t, ok)
	require.Equal(t, before.Positions, after.Positions, "the refused lineup stays as stored")
}

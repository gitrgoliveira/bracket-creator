package mobileapp

// The representative bout's row keeps the team names, and it keeps them from the
// moment it exists. The hantei mark is placed on the winner's side by comparing
// the winner against THAT row's sideA/sideB, so a row that names nobody between
// the add and the first score write is a row nothing can attribute. The add
// stores the match's own names (engine.AddDaihyosen); the first write restates
// the same strings, so it lands as an echo, not as a side mismatch or a verdict
// the organiser alone may touch.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDaihyosenAdd_TheRowCarriesTheMatchsTeamNames(t *testing.T) {
	for _, tc := range []struct {
		name     string
		selfRun  bool
		password string
	}{
		{"the organiser's add", false, "main-pw"},
		{"a participant's add", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, tc.selfRun)
			w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", tc.password, map[string]any{"modifiedAt": f.now})
			require.Equal(t, http.StatusOK, w.Code, "adding the representative bout: %s", w.Body.String())

			bm := storedB1(t, f.store, "c1")
			row := f.storedRepBout(t)
			assert.Equal(t, bm.SideA, row.SideA, "the stored row names the match's side A the moment it exists")
			assert.Equal(t, bm.SideB, row.SideB, "the stored row names the match's side B the moment it exists")
			assert.Equal(t, "TeamA", row.SideA)
			assert.Equal(t, "TeamB", row.SideB)

			var body struct {
				SubResult state.SubMatchResult `json:"subResult"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "TeamA", body.SubResult.SideA, "the answer carries the same names")
			assert.Equal(t, "TeamB", body.SubResult.SideB, "the answer carries the same names")
		})
	}
}

// The first write after the add restates the names the add stored. It lands
// whole: no side_mismatch, no hantei_organiser_only, the scoreline is written
// and the names stay what the match's are.
func TestSelfRun_TheFirstWriteRestatingTheStoredNamesLands(t *testing.T) {
	f := newRepBoutFixture(t, true)
	f.addRepBout(t)
	stored := f.storedRepBout(t)
	require.Equal(t, "TeamA", stored.SideA, "precondition: the add stored the names")

	w := f.score("", state.MatchStatusRunning, "", f.now+100, repBoutRow([]string{"M"}, []string{}, "TeamA"))
	require.Equal(t, http.StatusOK, w.Code, "scoring the row that restates the names: %s", w.Body.String())
	assert.NotContains(t, w.Body.String(), `"applied":false`)
	assert.NotContains(t, w.Body.String(), "side_mismatch")
	assert.NotContains(t, w.Body.String(), "hantei_organiser_only")

	row := f.storedRepBout(t)
	assert.Equal(t, []string{"M"}, row.IpponsA, "the scoreline is written")
	assert.Equal(t, "TeamA", row.SideA)
	assert.Equal(t, "TeamB", row.SideB)
}

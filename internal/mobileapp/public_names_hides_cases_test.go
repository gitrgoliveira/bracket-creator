package mobileapp

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicNamesHides_SharedTable runs publicNames.hides over the cases the
// browser's competitionHidesNames reads too (web-mobile/js/__tests__/
// competition_hides_names.test.jsx), so the client mirror cannot drift from
// the server's judge.
func TestPublicNamesHides_SharedTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/public_names_hides_cases.json")
	require.NoError(t, err)
	var table struct {
		Cases []struct {
			Name        string `json:"name"`
			PublicNames string `json:"publicNames"`
			Mode        string `json:"mode"`
			Kind        string `json:"kind"`
			TeamSize    int    `json:"teamSize"`
			Hides       bool   `json:"hides"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases)

	f := newPNFixture(t, state.PublicNamesShown)
	tour, err := f.store.LoadTournament()
	require.NoError(t, err)
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			tour.Mode = c.Mode
			require.NoError(t, f.store.SaveTournament(tour))
			comp := *f.comp
			comp.PublicNames = c.PublicNames
			if c.Kind != "" {
				comp.Kind = c.Kind
			}
			comp.TeamSize = c.TeamSize
			assert.Equal(t, c.Hides, f.pn.hides(&comp))
		})
	}
}

// TestStampNamesHidden_ListsWhatTheCallersLinksReveal: a masked payload names
// the participants the caller's own per-person links reveal, so the reveal page
// can follow them even when a revealed team has no named member to show.
func TestStampNamesHidden_ListsWhatTheCallersLinksReveal(t *testing.T) {
	masked := maskView{m: &nameMask{}, reveal: map[string]bool{"t2": true, "t1": true, "t3": false}}
	payload := gin.H{}
	stampNamesHidden(payload, masked)
	assert.Equal(t, true, payload["namesHidden"])
	assert.Equal(t, []string{"t1", "t2"}, payload["revealedCompetitors"])

	none := gin.H{}
	stampNamesHidden(none, maskView{m: &nameMask{}})
	assert.Equal(t, true, none["namesHidden"])
	assert.NotContains(t, none, "revealedCompetitors")

	shown := gin.H{}
	stampNamesHidden(shown, maskView{})
	assert.Empty(t, shown, "a payload that is not masked carries neither marker")
}

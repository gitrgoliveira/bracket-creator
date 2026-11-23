package mobileapp

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMaskViewSideReading_SharedTable runs maskView.sideReading over the cases
// the court bridge's browser mask reads too (web-mobile/js/__tests__/
// court_bridge_side_cases.test.jsx), so the two copies of the mask cannot
// drift apart.
func TestMaskViewSideReading_SharedTable(t *testing.T) {
	raw, err := os.ReadFile("testdata/public_names_side_cases.json")
	require.NoError(t, err)
	var table struct {
		Roster []struct {
			ID     string `json:"id"`
			Number string `json:"number"`
		} `json:"roster"`
		Cases []struct {
			Name    string `json:"name"`
			Side    string `json:"side"`
			ID      string `json:"id"`
			Reading string `json:"reading"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases)

	numbers := map[string]string{}
	for _, r := range table.Roster {
		numbers[r.ID] = r.Number
	}
	view := maskView{m: &nameMask{numbers: numbers}}
	for _, c := range table.Cases {
		t.Run(c.Name, func(t *testing.T) {
			assert.Equal(t, c.Reading, view.side(c.Side, c.ID))
		})
	}
}

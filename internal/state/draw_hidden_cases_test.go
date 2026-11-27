package state

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDrawHiddenIn_SharedTable runs Competition.DrawHiddenIn over the cases
// the browser's drawHiddenFromPublic reads too (web-mobile/js/__tests__/
// draw_visibility.test.jsx), so the client twin cannot drift from the
// server's rule. Each case's competition is decoded from the wire shape the
// page reads (`status`, `drawHidden`), so a case with no drawHidden key is
// the competition a stored config without the field decodes to.
func TestDrawHiddenIn_SharedTable(t *testing.T) {
	raw, err := os.ReadFile("../mobileapp/testdata/draw_visibility_cases.json")
	require.NoError(t, err)
	var table struct {
		Cases []map[string]json.RawMessage `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases)

	for _, c := range table.Cases {
		var name, mode string
		var hidden bool
		require.NoError(t, json.Unmarshal(c["name"], &name))
		require.NoError(t, json.Unmarshal(c["mode"], &mode))
		require.NoError(t, json.Unmarshal(c["hidden"], &hidden))
		t.Run(name, func(t *testing.T) {
			wire := map[string]json.RawMessage{"status": c["status"]}
			if v, ok := c["drawHidden"]; ok {
				wire["drawHidden"] = v
			}
			body, err := json.Marshal(wire)
			require.NoError(t, err)
			var comp Competition
			require.NoError(t, json.Unmarshal(body, &comp))
			assert.Equal(t, hidden, comp.DrawHiddenIn(&Tournament{Mode: mode}))
		})
	}
}

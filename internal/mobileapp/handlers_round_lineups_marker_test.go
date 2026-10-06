package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A competition created by this release starts with the round-lineup marker
// set, whatever its kind: it has no lineup for a round to move onto a match,
// and no writer left that creates one (PUT .../lineups/:round refuses a round
// above 0), so the load repair and the writes that seat a team have nothing to
// settle for it and skip it.
func TestCreateCompetition_StartsWithTheRoundLineupMarker(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	for _, body := range []map[string]any{
		{"id": "create-fixed-team", "name": "Create Fixed Team", "kind": "team", "teamSize": 3, "format": state.CompFormatKnockout},
		{"id": "create-kachinuki-team", "name": "Create Kachinuki Team", "kind": "team", "teamSize": 3,
			"teamMatchType": state.TeamMatchTypeKachinuki, "format": state.CompFormatMixed},
		{"id": "create-individual", "name": "Create Individual", "kind": "individual", "format": state.CompFormatKnockout},
	} {
		id := body["id"].(string)
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(raw))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

		comp, err := store.LoadCompetition(id)
		require.NoError(t, err)
		require.NotNil(t, comp)
		assert.True(t, comp.RoundLineupsConverted, id)
	}
}

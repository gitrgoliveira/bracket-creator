package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

// The settings PUT merges what a client sends onto the stored record, and the
// round-lineup conversion's own fields are off the wire: a settings save made
// while the conversion is under way keeps the pairs it has settled (or its
// marker), so it cannot give a lineup the operator removed back.
func TestPUTCompetition_KeepsWhatTheRoundLineupConversionRecorded(t *testing.T) {
	for _, tc := range []struct {
		name      string
		given     map[string][]string
		converted bool
	}{
		{"the pairs settled", map[string][]string{"team-a": {"r0-m0", "r1-m0"}}, false},
		{"the marker", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, tempDir := setupTestRouter(t)
			defer os.RemoveAll(tempDir)
			const compID = "round-lineups-record"
			seed := state.Competition{
				ID: compID, Name: "Round Lineups", Kind: "team", TeamSize: 3, Format: state.CompFormatKnockout,
				PoolSize: 3, Courts: []string{"A"}, RoundLineupsGiven: tc.given, RoundLineupsConverted: tc.converted,
			}
			require.NoError(t, store.SaveCompetition(&seed))
			// A team with a lineup for a later round: the conversion has it to wait
			// for, so it does not clear the pairs the seed records.
			tora := helper.NewUUID4()
			require.NoError(t, store.SaveParticipants(compID, []domain.Player{{ID: tora, Name: "Tora", Dojo: "A"}}))
			require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
				TeamID: tora, Round: 1, Positions: map[domain.Position]string{domain.PositionNumbered(1): "Sato"},
			}, 3))

			update := seed
			update.Name = "Round Lineups Renamed"
			body, err := json.Marshal(update) // both fields are json:"-": a client never sends them
			require.NoError(t, err)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+compID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())

			stored, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, "Round Lineups Renamed", stored.Name)
			assert.Equal(t, tc.given, stored.RoundLineupsGiven)
			assert.Equal(t, tc.converted, stored.RoundLineupsConverted)
		})
	}
}

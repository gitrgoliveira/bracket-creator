package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateParticipant_DuplicateTeamMember409 pins the HTTP-boundary half of
// bc-tmdup's acceptance criteria: the single-participant PUT endpoint (which
// wraps state.Store.UpdateParticipant, saveParticipantsNoLock's caller) must
// surface state.ErrDuplicateTeamMember as a 409, the same status
// state.ErrDuplicateName already gets via classifyRosterWriteError
// (internal/mobileapp/errors.go), naming the team and the repeated member in
// the response body rather than falling through to a generic 500.
func TestUpdateParticipant_DuplicateTeamMember409(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "dup-team-member-put"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Dup Team Member PUT", Kind: "team", TeamSize: 3, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}},
	}))
	stored, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, stored, 1)

	body, _ := json.Marshal(map[string]interface{}{
		"name":     "Tora A",
		"dojo":     "Tora Dojo",
		"metadata": []string{"Alice", "Bob", "Alice"},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+stored[0].ID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code,
		"a duplicate team member is a data conflict on the write, not a server fault")
	assert.Contains(t, w.Body.String(), "Tora A", "the response must name the offending team")
	assert.Contains(t, w.Body.String(), "Alice", "the response must name the repeated member")
}

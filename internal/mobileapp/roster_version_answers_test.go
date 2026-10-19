package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Every answer that carries the roster names its version (stage 6 review 13,
// m6): the competition answers as the `rosterVersion` field, the two list
// answers (GET and the batch POST of /participants), which are bare arrays, in
// the X-Roster-Version header. A page that reads the list from either builds
// its next roster save on that version.
func TestParticipantListAnswersNameTheRosterVersion(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-roster-version-header"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Version", Status: state.CompStatusSetup}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice Smith", Dojo: "Wakaba"},
		{Name: "Bob Jones", Dojo: "Tora"},
	}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{{Name: "Alice Smith", Dojo: "Wakaba", SeedRank: 1}}))

	t.Run("GET", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, "/api/competitions/"+compID+"/participants", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var listed []domain.Player
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &listed))
		require.Len(t, listed, 2)
		assert.Equal(t, state.RosterVersion(mustLoad(t, store, compID)), w.Header().Get("X-Roster-Version"),
			"the version of the roster the list holds, seeds merged, as a roster save names it")
	})

	t.Run("batch POST", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"players": []map[string]string{
			{"name": "Alice Smith", "dojo": "Wakaba"},
			{"name": "Bob Jones", "dojo": "Tora"},
			{"name": "Carol Ito", "dojo": "Hana"},
		}})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, state.RosterVersion(mustLoad(t, store, compID)), w.Header().Get("X-Roster-Version"),
			"the list the POST answers is the stored one, and so is the version it names")
	})
}

// The seeds a roster replace prunes are the ones stored when its locked step
// runs: a seed another desk set after the caller read them is kept (stage 6
// review 13, m7), where a list read before the lock written back over them
// lost it.
func TestReplaceRosterKeepingCheckIns_PrunesTheSeedsStoredWhenItRuns(t *testing.T) {
	_, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-replace-prunes-current-seeds"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Prune", Status: state.CompStatusSetup}))
	alice := domain.Player{Name: "Alice Smith", Dojo: "Wakaba"}
	bob := domain.Player{Name: "Bob Jones", Dojo: "Tora"}
	carol := domain.Player{Name: "Carol Ito", Dojo: "Hana"}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{alice, bob, carol}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: alice.Name, Dojo: alice.Dojo, SeedRank: 1},
		{Name: bob.Name, Dojo: bob.Dojo, SeedRank: 2},
	}))

	// The caller's read, taken before the lock.
	stale, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, stale, 2)

	// Another desk seeds Carol after that read.
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: alice.Name, Dojo: alice.Dojo, SeedRank: 1},
		{Name: bob.Name, Dojo: bob.Dojo, SeedRank: 2},
		{Name: carol.Name, Dojo: carol.Dojo, SeedRank: 3},
	}))

	// Alice is taken off the list; her seed goes with her, Carol's stays.
	remaining := []domain.Player{bob, carol}
	seedsErr, err := store.ReplaceRosterKeepingCheckIns(compID, remaining,
		func(_ []domain.Player, current []domain.SeedAssignment) []domain.SeedAssignment {
			_, kept := seedsOrphanedByRosterReplace([]domain.Player{alice, bob, carol}, remaining, current)
			return kept
		})
	require.NoError(t, err)
	require.NoError(t, seedsErr)

	seeds, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	byName := map[string]int{}
	for _, s := range seeds {
		byName[s.Name] = s.SeedRank
	}
	assert.NotContains(t, byName, alice.Name, "the removed competitor's seed is dropped")
	assert.Equal(t, 2, byName[bob.Name])
	assert.Equal(t, 3, byName[carol.Name], "the seed set after the caller's read is kept")
}

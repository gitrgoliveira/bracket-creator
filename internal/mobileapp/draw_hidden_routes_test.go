package mobileapp

import (
	"net/http"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DrawHidden is the draw routes' alone (state.Competition.DrawHidden): neither
// the create nor the settings PUT stores or changes it, whatever the body says.

func storedDrawHidden(t *testing.T, store *state.Store, id string) bool {
	t.Helper()
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	return comp.DrawHidden
}

func TestDrawHidden_CreateNeverSetsIt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	newShuffleComp(t, r, store, "create-hidden", map[string]any{"drawHidden": true})
	assert.False(t, storedDrawHidden(t, store, "create-hidden"), "a new competition has no draw to hide, whatever the body says")
}

func TestDrawHidden_SettingsPutNeitherSetsNorClearsIt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	const id = "settings-hidden"
	newShuffleComp(t, r, store, id, nil)

	put := func(hidden bool) {
		t.Helper()
		comp, err := store.LoadCompetition(id)
		require.NoError(t, err)
		comp.Name += "!"
		comp.DrawHidden = hidden
		w := shuffleServe(t, r, http.MethodPut, "/api/competitions/"+id, comp)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	put(true)
	assert.False(t, storedDrawHidden(t, store, id), "a settings PUT never sets the field")

	_, err := store.UpdateCompetitionChanged(id, func(c *state.Competition) (*state.Competition, error) {
		c.DrawHidden = true
		return c, nil
	})
	require.NoError(t, err)
	put(false)
	assert.True(t, storedDrawHidden(t, store, id), "a settings PUT never clears the field")
}

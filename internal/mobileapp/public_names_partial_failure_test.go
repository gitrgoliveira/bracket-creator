package mobileapp

// A tournament save that sets competitions' public names but cannot change
// every one of them: the tournament default's apply (PUT and POST) answers 409
// public_names_apply_failed (a 5xx would be retried unseen by the offline
// queue) naming the competitions left as they were, tells open pages about the
// ones that did change, and finishes the rest when the same save is sent again.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// partialFailureFixture is the full router over a store in a known folder,
// with its hub in hand so a test can read what was broadcast.
type partialFailureFixture struct {
	publicNamesFixture
	hub *Hub
	dir string
}

func newPartialFailureFixture(t *testing.T, mode string) partialFailureFixture {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("a read-only folder does not stop root from writing")
	}
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	switch mode {
	case state.TournamentModeOfficiated:
		seedOfficiatedTournament(t, store)
	}
	hub := NewHub()
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html>test</html>")}})
	r, _, limiter := NewRouterWithHub(store, engine.New(store), res, NewFileVerifier(store), hub, false)
	t.Cleanup(limiter.Close)
	return partialFailureFixture{publicNamesFixture: publicNamesFixture{r: r, store: store}, hub: hub, dir: dir}
}

// lockCompetition makes the competition's folder refuse writes until the
// returned function puts it back; the cleanup puts it back too, so the
// temporary folder can be removed.
func (f partialFailureFixture) lockCompetition(t *testing.T, id string) (unlock func()) {
	t.Helper()
	path := filepath.Join(f.dir, "competitions", id)
	require.NoError(t, os.Chmod(path, 0o555))
	unlock = func() { require.NoError(t, os.Chmod(path, 0o755)) }
	t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
	return unlock
}

// tournamentUpdates counts the tournament_updated events the hub has sent
// to a subscriber since the last call.
func tournamentUpdates(ch chan historyEntry) int {
	n := 0
	for {
		select {
		case e := <-ch:
			if strings.Contains(e.payload, fmt.Sprint(EventTournamentUpdated)) {
				n++
			}
		default:
			return n
		}
	}
}

func failedCompetitions(t *testing.T, body []byte) []string {
	t.Helper()
	var out struct {
		Competitions []string `json:"competitions"`
	}
	require.NoError(t, json.Unmarshal(body, &out))
	return out.Competitions
}

func TestPublicNames_DefaultApplyPartialFailureIsA409AndSendingAgainFinishesIt(t *testing.T) {
	f := newPartialFailureFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
	f.saveComp(t, &state.Competition{ID: "c2", Name: "Shiai", Kind: "individual", Format: state.CompFormatKnockout})
	unlock := f.lockCompetition(t, "c2")
	events := f.hub.Subscribe()
	defer f.hub.Unsubscribe(events)

	body := func() map[string]any {
		return tournamentPutBody(t, f.publicNamesFixture, map[string]any{"defaultPublicNames": "person", "confirmPublicNames": true})
	}
	w := f.admin(http.MethodPut, "/api/tournament", body())
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, codePublicNamesApplyFailed, codeOf(t, w))
	assert.Equal(t, []string{"c2"}, failedCompetitions(t, w.Body.Bytes()))
	assert.Contains(t, w.Body.String(), "Save again to retry")
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, state.PublicNamesPerson, stored.DefaultPublicNames, "the settings themselves were saved")
	assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c1").PublicNames)
	assert.Empty(t, f.comp(t, "c2").PublicNames, "the competition that could not be written keeps its option")
	assert.Positive(t, tournamentUpdates(events), "open pages are told what changed")

	// The default now equals the stored one, so this save moves nothing: the
	// confirmation alone sends it over the competitions still to follow.
	unlock()
	w = f.admin(http.MethodPut, "/api/tournament", body())
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c2").PublicNames)
}

// The POST door runs the same apply after its save, so a competition it cannot
// write is answered the same 409 naming it, and sending the POST again
// finishes the rest.
func TestPublicNames_PostDefaultApplyPartialFailureIsA409AndSendingAgainFinishesIt(t *testing.T) {
	f := newPartialFailureFixture(t, state.TournamentModeOfficiated)
	f.saveComp(t, &state.Competition{ID: "c1", Name: "Kata", Kind: "individual", Format: state.CompFormatKnockout})
	f.saveComp(t, &state.Competition{ID: "c2", Name: "Shiai", Kind: "individual", Format: state.CompFormatKnockout})
	unlock := f.lockCompetition(t, "c2")
	events := f.hub.Subscribe()
	defer f.hub.Unsubscribe(events)
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	body := map[string]any{
		"name": stored.Name, "date": stored.Date, "courts": stored.Courts, "password": publicNamesPassword,
		"mode": stored.Mode, "defaultPublicNames": state.PublicNamesPerson, "confirmPublicNames": true,
	}

	w := f.admin(http.MethodPost, "/api/tournament", body)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, codePublicNamesApplyFailed, codeOf(t, w))
	assert.Equal(t, []string{"c2"}, failedCompetitions(t, w.Body.Bytes()))
	assert.Contains(t, w.Body.String(), "Save again to retry")
	after, err := f.store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, state.PublicNamesPerson, after.DefaultPublicNames, "the settings themselves were saved")
	assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c1").PublicNames)
	assert.Empty(t, f.comp(t, "c2").PublicNames, "the competition that could not be written keeps its option")
	assert.Positive(t, tournamentUpdates(events), "open pages are told what changed")

	unlock()
	w = f.admin(http.MethodPost, "/api/tournament", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, state.PublicNamesPerson, f.comp(t, "c2").PublicNames)
}

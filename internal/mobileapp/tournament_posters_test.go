package mobileapp

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func postersSaveBody(version string, posters any) map[string]any {
	body := tournamentSaveBody("Taikai", version)
	body["courts"] = []string{"A", "B"}
	if posters != nil {
		body["posters"] = posters
	}
	return body
}

func storedPosters(t *testing.T, store *state.Store) *state.Posters {
	t.Helper()
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)
	return tourn.Posters
}

func savePostersTournament(t *testing.T, store *state.Store, posters *state.Posters) {
	t.Helper()
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Taikai", Date: "01-03-2027", Venue: "Hall", Password: "pw",
		Courts: []string{"A", "B"}, Posters: posters,
	}))
}

func TestTournamentPostersSaveLandsTrimmedAndMovesTheVersion(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, nil)
	before := tournamentVersionOf(t, r)

	w := versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(before, map[string]any{
		"viewer": map[string]any{"heading": "  Follow the taikai  ", "logo": "centre"},
		"courts": map[string]any{
			"B": map[string]any{"callToAction": "Scan for Shiaijo B", "hideSponsors": true},
			"A": map[string]any{},
		},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	answered, _ := decodeMap(t, w)["tournamentVersion"].(string)
	assert.NotEqual(t, before, answered, "a poster choice is part of the version")
	assert.Equal(t, answered, tournamentVersionOf(t, r))

	assert.Equal(t, &state.Posters{
		Viewer: state.PosterChoices{Heading: "Follow the taikai", Logo: state.PosterLogoCentre},
		Courts: map[string]state.PosterChoices{"B": {CallToAction: "Scan for Shiaijo B", HideSponsors: true}},
	}, storedPosters(t, store), "trimmed, with the empty entry dropped")

	got := decodeMap(t, versionDo(t, r, "GET", "/api/tournament", nil))
	posters, ok := got["posters"].(map[string]any)
	require.True(t, ok, "the GET serves the block: %v", got)
	assert.Equal(t, map[string]any{"heading": "Follow the taikai", "logo": "centre"}, posters["viewer"])
}

// The Edit tournament form, or any client, that leaves the key out keeps the
// stored block: only a body that sends it replaces it.
func TestTournamentSaveWithoutPostersKeepsTheStoredBlock(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	kept := &state.Posters{Courts: map[string]state.PosterChoices{"A": {Heading: "Main court"}}}
	savePostersTournament(t, store, kept)
	v := tournamentVersionOf(t, r)

	body := postersSaveBody(v, nil)
	body["name"] = "Renamed"
	w := versionDo(t, r, "PUT", "/api/tournament", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, kept, storedPosters(t, store))
	answered := decodeMap(t, w)
	assert.NotNil(t, answered["posters"], "the answer carries the kept block")
}

// Clearing the last choice sends an empty object, which removes the block
// from tournament.md.
func TestTournamentSaveWithEmptyPostersRemovesTheBlock(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, &state.Posters{Viewer: state.PosterChoices{Heading: "Follow us"}})
	v := tournamentVersionOf(t, r)

	w := versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(v, map[string]any{}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Nil(t, storedPosters(t, store))
	raw, err := os.ReadFile(filepath.Join(store.GetFolder(), "tournament.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "posters")
}

func TestTournamentSaveRefusesABadPostersBlockWithItsSentence(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, nil)
	v := tournamentVersionOf(t, r)

	courts := map[string]any{}
	for i := 0; i < state.MaxPosterCourtEntries+1; i++ {
		courts[string(rune('A'+i))] = map[string]any{"hideSponsors": true}
	}
	cases := map[string]struct {
		posters any
		want    string
	}{
		"heading": {map[string]any{"courts": map[string]any{"B": map[string]any{"heading": strings.Repeat("x", 81)}}},
			"The heading on the Shiaijo B poster is longer than 80 characters."},
		"call to action": {map[string]any{"viewer": map[string]any{"callToAction": strings.Repeat("y", 121)}},
			"The call to action on the viewer home poster is longer than 120 characters."},
		"logo": {map[string]any{"viewer": map[string]any{"logo": "left"}},
			"The logo place on the viewer home poster must be in the centre of the code or above it."},
		"key": {map[string]any{"courts": map[string]any{"AB": map[string]any{"heading": "x"}}},
			`The poster choices name a shiaijo "AB", which is not a shiaijo label: a label is one letter or character.`},
		"33 entries": {map[string]any{"courts": courts},
			"Poster choices can be kept for at most 32 shiaijo; this save holds 33."},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			w := versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(v, tc.posters))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Equal(t, tc.want, decodeMap(t, w)["error"])
			assert.Nil(t, storedPosters(t, store), "a refused save writes nothing")
		})
	}
}

// A court labelled outside A to P that was removed keeps its entry, and the
// entry never refuses the next save of the block.
func TestTournamentSaveKeepsAStaleCourtEntry(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, &state.Posters{Courts: map[string]state.PosterChoices{"Z": {Heading: "Old court"}}})
	v := tournamentVersionOf(t, r)

	w := versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(v, map[string]any{
		"courts": map[string]any{"Z": map[string]any{"heading": "Old court"}, "B": map[string]any{"logo": "above"}},
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, map[string]state.PosterChoices{"Z": {Heading: "Old court"}, "B": {Logo: state.PosterLogoAbove}}, storedPosters(t, store).Courts)
}

// Two devices saving posters from the same copy: the second is told the
// settings changed, by the rule every settings save has.
func TestTournamentPostersSaveFromAnOlderCopyIsRefused(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, nil)
	old := tournamentVersionOf(t, r)

	w := versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(old, map[string]any{"viewer": map[string]any{"heading": "From the desk"}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(old, map[string]any{"viewer": map[string]any{"heading": "From the hall"}}))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "tournament_changed", decodeMap(t, w)["code"])
	assert.Equal(t, "From the desk", storedPosters(t, store).Viewer.Heading)

	// The resend of the save that landed, its answer lost, is not stale.
	w = versionDo(t, r, "PUT", "/api/tournament", postersSaveBody(old, map[string]any{"viewer": map[string]any{"heading": "From the desk"}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// The setup form re-POSTs the tournament without the block: it keeps the
// stored poster text, and a POST that sends a block is judged like the PUT.
func TestTournamentRePostKeepsThePostersBlock(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	kept := &state.Posters{Viewer: state.PosterChoices{CallToAction: "Scan me"}}
	savePostersTournament(t, store, kept)

	repost := map[string]any{"name": "Taikai", "date": "01-03-2027", "venue": "Hall", "password": "pw", "courts": []string{"A", "B"}}
	w := versionDo(t, r, "POST", "/api/tournament", repost)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, kept, storedPosters(t, store))

	repost["posters"] = map[string]any{"courts": map[string]any{"A": map[string]any{"logo": "behind"}}}
	w = versionDo(t, r, "POST", "/api/tournament", repost)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "The logo place on the Shiaijo A poster must be in the centre of the code or above it.", decodeMap(t, w)["error"])
	assert.Equal(t, kept, storedPosters(t, store))

	repost["posters"] = map[string]any{"courts": map[string]any{"A": map[string]any{"heading": " Main "}}}
	w = versionDo(t, r, "POST", "/api/tournament", repost)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.Equal(t, &state.Posters{Courts: map[string]state.PosterChoices{"A": {Heading: "Main"}}}, storedPosters(t, store))
}

// The court list keeps its own wording for a label of more than one
// character, now judged by the rule the posters' keys share.
func TestTournamentCourtsStillRefuseALongLabel(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	savePostersTournament(t, store, nil)
	body := postersSaveBody(tournamentVersionOf(t, r), nil)
	body["courts"] = []string{"A", "BB"}
	w := versionDo(t, r, "PUT", "/api/tournament", body)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "must be a single character")
}

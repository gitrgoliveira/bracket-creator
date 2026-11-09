package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func versionDo(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		data, err := json.Marshal(body)
		require.NoError(t, err)
		buf = bytes.NewBuffer(data)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req, err := http.NewRequest(method, path, buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeMap(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &m), w.Body.String())
	return m
}

// tournamentVersionOf reads the version the app gets: the viewer GET it loads
// the tournament from, and the admin GET, which must agree.
func tournamentVersionOf(t *testing.T, r *gin.Engine) string {
	t.Helper()
	viewer := versionDo(t, r, "GET", "/api/viewer/tournament", nil)
	require.Equal(t, http.StatusOK, viewer.Code, viewer.Body.String())
	vm := decodeMap(t, viewer)
	admin := versionDo(t, r, "GET", "/api/tournament", nil)
	require.Equal(t, http.StatusOK, admin.Code, admin.Body.String())
	am := decodeMap(t, admin)
	v, _ := vm["tournamentVersion"].(string)
	require.NotEmpty(t, v, "the viewer GET answers tournamentVersion")
	assert.Equal(t, v, am["tournamentVersion"], "both GETs answer the same version")
	assert.Equal(t, "", vm["password"], "the viewer GET never serves the password")
	return v
}

func tournamentSaveBody(name, version string) map[string]any {
	body := map[string]any{"name": name, "date": "01-03-2027", "venue": "Hall", "courts": []string{"A"}}
	if version != "" {
		body["tournamentVersion"] = version
	}
	return body
}

func TestTournamentSaveNeedsAVersion(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}}))

	w := versionDo(t, r, "PUT", "/api/tournament", tournamentSaveBody("Renamed", ""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "tournamentVersion")

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Taikai", stored.Name, "a save with no version writes nothing")
}

func TestTournamentSaveWithTheCurrentVersionLandsAndAnswersTheNext(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}}))
	before := tournamentVersionOf(t, r)

	// Empty lists the store writes back as absent must not make the answer's
	// version disagree with the next read.
	body := tournamentSaveBody("Renamed", before)
	body["pauses"] = []any{}
	body["contacts"] = []any{}
	body["sponsors"] = []any{}
	w := versionDo(t, r, "PUT", "/api/tournament", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	answered, _ := decodeMap(t, w)["tournamentVersion"].(string)
	require.NotEmpty(t, answered, "the 200 answers the new version")
	assert.NotEqual(t, before, answered)

	after := tournamentVersionOf(t, r)
	assert.Equal(t, answered, after, "the version the save answered is the one the next read serves")

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Renamed", stored.Name)
	assert.Equal(t, "pw", stored.Password, "an empty password keeps the stored one")

	// A second save from the answered version lands too.
	w = versionDo(t, r, "PUT", "/api/tournament", tournamentSaveBody("Renamed again", answered))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestTournamentSaveFromAnOlderCopyIsRefusedWithTheCurrentOne(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}}))
	old := tournamentVersionOf(t, r)

	// Another device saves first.
	w := versionDo(t, r, "PUT", "/api/tournament", tournamentSaveBody("From the desk", old))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	current := tournamentVersionOf(t, r)

	w = versionDo(t, r, "PUT", "/api/tournament", tournamentSaveBody("From the court", old))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	m := decodeMap(t, w)
	assert.Equal(t, "tournament_changed", m["code"])
	assert.NotEmpty(t, m["error"])
	assert.Equal(t, current, m["tournamentVersion"])
	copyOf, ok := m["tournament"].(map[string]any)
	require.True(t, ok, "the refusal carries the current copy")
	assert.Equal(t, "From the desk", copyOf["name"])
	assert.Equal(t, current, copyOf["tournamentVersion"])

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "From the desk", stored.Name, "the refused save wrote nothing")
}

// A sponsor added or removed while the Edit Tournament form is open does not
// make the form's Save stale, and the save keeps the stored list rather than
// writing the form's older copy over it.
func TestTournamentSaveAfterASponsorChangeLandsAndKeepsTheSponsors(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}}))
	opened := tournamentVersionOf(t, r)

	withSponsor, err := store.LoadTournament()
	require.NoError(t, err)
	withSponsor.Sponsors = []state.Sponsor{{Name: "Shop", File: "s.png"}}
	require.NoError(t, store.SaveTournament(withSponsor))
	assert.Equal(t, opened, tournamentVersionOf(t, r), "a sponsor change leaves the version alone")

	body := tournamentSaveBody("Renamed", opened)
	body["sponsors"] = []any{}
	w := versionDo(t, r, "PUT", "/api/tournament", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Renamed", stored.Name)
	assert.Equal(t, []state.Sponsor{{Name: "Shop", File: "s.png"}}, stored.Sponsors)
}

// A password change alone does not move the version: it is never hashed, so
// the version is safe to serve on the public viewer GET.
func TestTournamentVersionIgnoresAPasswordChange(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}, Date: "01-03-2027", Venue: "Hall"}))
	before := tournamentVersionOf(t, r)
	body := tournamentSaveBody("Taikai", before)
	body["password"] = "new-pw"
	w := versionDo(t, r, "PUT", "/api/tournament", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, before, decodeMap(t, w)["tournamentVersion"])
}

// A tournament.md written before the defaulted keys existed: the GET serves it
// with the defaults, while the save's transform reads it as parsed. The
// version the GET served must still be accepted.
func TestTournamentSaveAcceptsTheVersionOfALegacyFile(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	legacy := "---\nname: Legacy\ndate: 01-03-2027\nvenue: Hall\ncourts: []\npassword: pw\nrules_url: https://example.org\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tournament.md"), []byte(legacy), 0o600))

	v := tournamentVersionOf(t, r)
	w := versionDo(t, r, "PUT", "/api/tournament", tournamentSaveBody("Legacy renamed", v))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Legacy renamed", stored.Name)
}

func TestTournamentCreateAnswersAVersion(t *testing.T) {
	r, _, _, _, _ := setupTestRouter(t)
	body := tournamentSaveBody("New", "")
	body["password"] = "pw"
	w := versionDo(t, r, "POST", "/api/tournament", body)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	created, _ := decodeMap(t, w)["tournamentVersion"].(string)
	assert.Equal(t, tournamentVersionOf(t, r), created)
}

func awardsFixture(t *testing.T) (*gin.Engine, *state.Store) {
	t.Helper()
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Taikai", Password: "pw", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2}))
	return r, store
}

// awardsVersionOf reads the version from the competition detail the console
// loads, and checks the admin GET agrees.
func awardsVersionOf(t *testing.T, r *gin.Engine) string {
	t.Helper()
	detail := versionDo(t, r, "GET", "/api/viewer/competitions/c1", nil)
	require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
	v, _ := decodeMap(t, detail)["awardsVersion"].(string)
	require.NotEmpty(t, v, "the competition detail answers awardsVersion")
	admin := versionDo(t, r, "GET", "/api/competitions/c1", nil)
	require.Equal(t, http.StatusOK, admin.Code, admin.Body.String())
	am := decodeMap(t, admin)
	assert.Equal(t, v, am["awardsVersion"], "the admin GET answers the same version")
	assert.Equal(t, "c1", am["id"], "the admin GET still answers the competition's own fields")
	return v
}

func awardsBody(recipient, version string) map[string]any {
	body := map[string]any{"fightingSpiritAwards": []map[string]any{{"title": "Fighting spirit", "recipientName": recipient}}}
	if version != "" {
		body["awardsVersion"] = version
	}
	return body
}

func TestAwardsSaveNeedsAVersion(t *testing.T) {
	r, store := awardsFixture(t)
	w := versionDo(t, r, "PUT", "/api/competitions/c1/awards", awardsBody("Sato", ""))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "awardsVersion")
	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	assert.Empty(t, comp.FightingSpiritAwards, "a save with no version writes nothing")
}

func TestAwardsSaveWithTheCurrentVersionLandsAndAnswersTheNext(t *testing.T) {
	r, store := awardsFixture(t)
	before := awardsVersionOf(t, r)

	w := versionDo(t, r, "PUT", "/api/competitions/c1/awards", awardsBody("Sato", before))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	m := decodeMap(t, w)
	answered, _ := m["awardsVersion"].(string)
	require.NotEmpty(t, answered)
	assert.NotEqual(t, before, answered)
	assert.Equal(t, "c1", m["id"], "the 200 still answers the competition")
	assert.Equal(t, answered, awardsVersionOf(t, r), "the version the save answered is the one the next read serves")

	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	require.Len(t, comp.FightingSpiritAwards, 1)
	assert.Equal(t, "Sato", comp.FightingSpiritAwards[0].RecipientName)

	// Clearing the list from the answered version lands, and the empty list
	// reads back with the version an absent one has.
	w = versionDo(t, r, "PUT", "/api/competitions/c1/awards", map[string]any{"fightingSpiritAwards": []any{}, "awardsVersion": answered})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, before, decodeMap(t, w)["awardsVersion"])
	assert.Equal(t, before, awardsVersionOf(t, r))
}

func TestAwardsSaveFromAnOlderCopyIsRefusedWithTheCurrentOne(t *testing.T) {
	r, store := awardsFixture(t)
	old := awardsVersionOf(t, r)
	w := versionDo(t, r, "PUT", "/api/competitions/c1/awards", awardsBody("Sato", old))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	current := awardsVersionOf(t, r)

	w = versionDo(t, r, "PUT", "/api/competitions/c1/awards", awardsBody("Tanaka", old))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	m := decodeMap(t, w)
	assert.Equal(t, "awards_changed", m["code"])
	assert.NotEmpty(t, m["error"])
	assert.Equal(t, current, m["awardsVersion"])
	list, ok := m["fightingSpiritAwards"].([]any)
	require.True(t, ok, "the refusal carries the current list")
	require.Len(t, list, 1)
	assert.Equal(t, "Sato", list[0].(map[string]any)["recipientName"])

	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	require.Len(t, comp.FightingSpiritAwards, 1)
	assert.Equal(t, "Sato", comp.FightingSpiritAwards[0].RecipientName, "the refused save wrote nothing")
}

// A settings change elsewhere on the competition does not make an awards save
// stale: the version covers the awards only.
func TestAwardsVersionIgnoresOtherSettings(t *testing.T) {
	r, store := awardsFixture(t)
	v := awardsVersionOf(t, r)
	comp, err := store.LoadCompetition("c1")
	require.NoError(t, err)
	comp.Name = "Renamed"
	require.NoError(t, store.SaveCompetition(comp))
	assert.Equal(t, v, awardsVersionOf(t, r))
	w := versionDo(t, r, "PUT", "/api/competitions/c1/awards", awardsBody("Sato", v))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestAwardsSaveOnAnUnknownCompetitionIsNotFound(t *testing.T) {
	r, _ := awardsFixture(t)
	w := versionDo(t, r, "PUT", "/api/competitions/nope/awards", awardsBody("Sato", "0123456789abcdef"))
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

// versionedTournamentBody returns body, a JSON object for PUT /api/tournament,
// with the tournamentVersion the app would send: the version of the settings
// stored now. A body that already names a version keeps it. With no
// tournament stored yet it sends the version of empty settings, which the
// server does not compare (there is nothing to write over).
func versionedTournamentBody(t testing.TB, store *state.Store, body []byte) []byte {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m), string(body))
	if _, ok := m["tournamentVersion"]; !ok {
		stored, err := store.LoadTournament()
		require.NoError(t, err)
		if stored == nil {
			stored = &state.Tournament{}
		}
		m["tournamentVersion"] = state.TournamentVersion(stored)
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

// versionedAwardsBody returns body, a JSON object for PUT .../awards, with the
// awardsVersion of competition id's stored awards. A body that already names a
// version keeps it. An unknown competition gets the version of no awards.
func versionedAwardsBody(t testing.TB, store *state.Store, id string, body []byte) []byte {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(body, &m), string(body))
	if _, ok := m["awardsVersion"]; !ok {
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			comp = &state.Competition{}
		}
		m["awardsVersion"] = state.AwardsVersion(comp)
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return out
}

// tournamentBodyVia returns body, a JSON object for PUT /api/tournament, with
// the tournamentVersion the admin GET on h serves now (signed in with
// password), for tests that hold a router but not its store.
func tournamentBodyVia(t testing.TB, h http.Handler, password, body string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var current map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &current))
	version, _ := current["tournamentVersion"].(string)
	require.NotEmpty(t, version, "the admin GET answers tournamentVersion")

	var m map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &m), body)
	if _, ok := m["tournamentVersion"]; !ok {
		m["tournamentVersion"] = version
	}
	out, err := json.Marshal(m)
	require.NoError(t, err)
	return string(out)
}

// withStoredTournamentVersion returns a copy of body, a PUT /api/tournament
// body, carrying the tournamentVersion of the settings stored now.
func withStoredTournamentVersion(t testing.TB, store *state.Store, body map[string]any) map[string]any {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	var out map[string]any
	require.NoError(t, json.Unmarshal(versionedTournamentBody(t, store, data), &out))
	return out
}

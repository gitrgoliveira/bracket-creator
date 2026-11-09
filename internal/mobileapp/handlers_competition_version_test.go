package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cvSettingsBody builds a settings PUT body the way the Settings page sends
// one: every field of c except the roster, the given version (none when
// empty), then edits on top.
func cvSettingsBody(t *testing.T, c *state.Competition, version string, edits map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(c)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	delete(m, "players")
	delete(m, "rosterVersion")
	delete(m, "configVersion")
	if version != "" {
		m["configVersion"] = version
	}
	for k, v := range edits {
		m[k] = v
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return b
}

func cvPut(t *testing.T, r *gin.Engine, cid string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPut, "/api/competitions/"+cid, bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func cvGet(t *testing.T, r *gin.Engine, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, path, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "GET %s: %s", path, w.Body.String())
	return w
}

// cvAnswerVersion reads configVersion off a competition answer.
func cvAnswerVersion(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var ans struct {
		ConfigVersion string `json:"configVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ans))
	return ans.ConfigVersion
}

func cvConfigBytes(t *testing.T, dir, cid string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "competitions", cid, "config.md"))
	require.NoError(t, err)
	return b
}

// cvSeed stores a competition, then saves it once through the settings PUT
// so the server's own normalisation (the number prefix among it) is on disk,
// and returns the stored record and its version.
func cvSeed(t *testing.T, r *gin.Engine, store *state.Store, cid string, tweak func(*state.Competition)) (*state.Competition, string) {
	t.Helper()
	c := &state.Competition{
		ID:                           cid,
		Name:                         "Version " + cid,
		Date:                         "10-06-2026",
		StartTime:                    "09:00",
		PoolSize:                     3,
		PoolWinners:                  1,
		PoolSizeMode:                 "max",
		Courts:                       []string{"A", "B"},
		Format:                       state.CompFormatMixed,
		Kind:                         "individual",
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		Status:                       state.CompStatusSetup,
	}
	if tweak != nil {
		tweak(c)
	}
	require.NoError(t, store.SaveCompetition(c))
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	w := cvPut(t, r, cid, cvSettingsBody(t, stored, "", nil))
	require.Equalf(t, http.StatusOK, w.Code, "settling PUT: %s", w.Body.String())
	stored, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	v := state.ConfigVersion(stored)
	require.Equal(t, v, cvAnswerVersion(t, w), "the PUT answer names the stored version")
	return stored, v
}

// The team-size cap is judged where a write sets the size: a competition stored
// past it before the cap existed still saves an unrelated setting, while a
// create or a change of the size past it is refused.
func TestTeamSizeCap_JudgedOnlyWhereTheSizeIsSet(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	big := &state.Competition{
		ID: "big", Name: "Big Teams", Date: "10-06-2026", StartTime: "09:00",
		PoolSize: 3, PoolWinners: 1, PoolSizeMode: "max", Courts: []string{"A", "B"},
		Format: state.CompFormatMixed, Kind: "team", TeamSize: 40,
		PoolMatchDurationSeconds: 180, KnockoutMatchDurationSeconds: 180,
		Status: state.CompStatusSetup,
	}
	require.NoError(t, store.SaveCompetition(big))
	stored, err := store.LoadCompetition("big")
	require.NoError(t, err)

	w := cvPut(t, r, "big", cvSettingsBody(t, stored, "", map[string]any{"name": "Bigger Teams"}))
	require.Equalf(t, http.StatusOK, w.Code, "rename: %s", w.Body.String())
	stored, err = store.LoadCompetition("big")
	require.NoError(t, err)
	assert.Equal(t, "Bigger Teams", stored.Name)
	assert.Equal(t, 40, stored.TeamSize)

	w = cvPut(t, r, "big", cvSettingsBody(t, stored, "", map[string]any{"teamSize": 31}))
	require.Equalf(t, http.StatusBadRequest, w.Code, "resize: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "teamSize must be at most 30")

	post := *big
	post.ID, post.Name, post.TeamSize, post.Status = "new-big", "New Big Teams", 31, ""
	body, err := json.Marshal(post)
	require.NoError(t, err)
	wPost := httptest.NewRecorder()
	reqPost, err := http.NewRequest(http.MethodPost, "/api/competitions", bytes.NewReader(body))
	require.NoError(t, err)
	reqPost.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wPost, reqPost)
	require.Equalf(t, http.StatusBadRequest, wPost.Code, "POST: %s", wPost.Body.String())
	assert.Contains(t, wPost.Body.String(), "teamSize must be at most 30")
}

func TestConfigVersion_EveryReadAndAnswerAgrees(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	cid := "version-agree"

	post := state.Competition{
		ID:                           cid,
		Name:                         "Version Agreement",
		Date:                         "10-06-2026",
		StartTime:                    "09:00",
		PoolSize:                     3,
		PoolWinners:                  1,
		PoolSizeMode:                 "max",
		Courts:                       []string{"A", "B"},
		Format:                       state.CompFormatMixed,
		Kind:                         "individual",
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		Players: []domain.Player{
			{Name: "Competitor 1", Dojo: "Dojo 1"},
			{Name: "Competitor 2", Dojo: "Dojo 2"},
		},
	}
	body, err := json.Marshal(post)
	require.NoError(t, err)
	wPost := httptest.NewRecorder()
	reqPost, err := http.NewRequest(http.MethodPost, "/api/competitions", bytes.NewReader(body))
	require.NoError(t, err)
	reqPost.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wPost, reqPost)
	require.Equalf(t, http.StatusCreated, wPost.Code, "POST: %s", wPost.Body.String())
	created := cvAnswerVersion(t, wPost)
	require.Len(t, created, 16)

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ConfigVersion(stored), created, "POST answer")

	readAll := func() map[string]string {
		got := map[string]string{}
		got["admin single"] = cvAnswerVersion(t, cvGet(t, r, "/api/competitions/"+cid))
		var list []struct {
			ID            string `json:"id"`
			ConfigVersion string `json:"configVersion"`
		}
		require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/competitions").Body.Bytes(), &list))
		for _, item := range list {
			if item.ID == cid {
				got["admin list"] = item.ConfigVersion
			}
		}
		var single struct {
			Config struct {
				ConfigVersion string `json:"configVersion"`
			} `json:"config"`
		}
		require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/viewer/competitions/"+cid).Body.Bytes(), &single))
		got["viewer single"] = single.Config.ConfigVersion
		var vlist []struct {
			ID            string `json:"id"`
			ConfigVersion string `json:"configVersion"`
		}
		require.NoError(t, json.Unmarshal(cvGet(t, r, "/api/viewer/competitions").Body.Bytes(), &vlist))
		for _, item := range vlist {
			if item.ID == cid {
				got["viewer list"] = item.ConfigVersion
			}
		}
		return got
	}

	for where, v := range readAll() {
		assert.Equalf(t, created, v, "%s after the POST", where)
	}

	w := cvPut(t, r, cid, cvSettingsBody(t, stored, created, map[string]any{"poolMatchDurationSeconds": 240}))
	require.Equalf(t, http.StatusOK, w.Code, "PUT: %s", w.Body.String())
	saved := cvAnswerVersion(t, w)
	require.Len(t, saved, 16)
	assert.NotEqual(t, created, saved)
	for where, v := range readAll() {
		assert.Equalf(t, saved, v, "%s after the PUT", where)
	}
}

func TestConfigVersion_FieldCoverage(t *testing.T) {
	no := false
	cases := []struct {
		name  string
		seed  func(*state.Competition)
		edits map[string]any
	}{
		{"name", nil, map[string]any{"name": "Coverage Renamed"}},
		{"date", nil, map[string]any{"date": "20-07-2026"}},
		{"startTime", nil, map[string]any{"startTime": "11:15"}},
		{"poolSize", nil, map[string]any{"poolSize": 5}},
		{"poolWinners", nil, map[string]any{"poolWinners": 2}},
		{"poolSizeMode", nil, map[string]any{"poolSizeMode": "min"}},
		{"extraQualifiers", func(c *state.Competition) { c.PoolSizeMode = "min" }, map[string]any{"extraQualifiers": state.ExtraQualifiersLargerPools}},
		{"courts", nil, map[string]any{"courts": []string{"A", "B", "C", "D"}}},
		{"roundRobin", nil, map[string]any{"roundRobin": true}},
		{"withZekkenName", nil, map[string]any{"withZekkenName": true}},
		{"teamSize", func(c *state.Competition) { c.Kind = "team"; c.TeamSize = 3 }, map[string]any{"teamSize": 5}},
		{"numberPrefix", nil, map[string]any{"numberPrefix": "CV"}},
		{"format", nil, map[string]any{"format": state.CompFormatKnockout}},
		{"poolFormat", nil, map[string]any{"poolFormat": state.PoolFormatPartial}},
		{"kind", nil, map[string]any{"kind": "team", "teamSize": 3}},
		{"poolMatchDurationSeconds", nil, map[string]any{"poolMatchDurationSeconds": 300}},
		{"knockoutMatchDurationSeconds", nil, map[string]any{"knockoutMatchDurationSeconds": 360}},
		{"swissRounds", func(c *state.Competition) { c.Format = state.CompFormatSwiss; c.SwissRounds = 3 }, map[string]any{"swissRounds": 4}},
		{"naginata", nil, map[string]any{"naginata": true}},
		{"engi", nil, map[string]any{"engi": true}},
		{"teamMatchType", func(c *state.Competition) { c.Kind = "team"; c.TeamSize = 3 }, map[string]any{"teamMatchType": string(state.TeamMatchTypeKachinuki)}},
		{"leagueTiebreakTopN", func(c *state.Competition) { c.Format = state.CompFormatLeague }, map[string]any{"leagueTiebreakTopN": 3}},
		{"twoThirdPlaces", nil, map[string]any{"twoThirdPlaces": &no}},
		{"expectedParticipants", nil, map[string]any{"expectedParticipants": 24}},
		{"checkInEnabled", nil, map[string]any{"checkInEnabled": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, dir := setupTestRouter(t)
			cid := "cov-" + tc.name
			stored, before := cvSeed(t, r, store, cid, tc.seed)
			diskBefore := cvConfigBytes(t, dir, cid)

			w := cvPut(t, r, cid, cvSettingsBody(t, stored, before, tc.edits))
			require.Equalf(t, http.StatusOK, w.Code, "PUT %s: %s", tc.name, w.Body.String())

			require.NotEqualf(t, string(diskBefore), string(cvConfigBytes(t, dir, cid)),
				"the case must change config.md, or it proves nothing about %s", tc.name)
			after := cvAnswerVersion(t, w)
			assert.NotEqualf(t, before, after, "config.md changed with %s, so the version must move", tc.name)
			reloaded, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, state.ConfigVersion(reloaded), after)
		})
	}
}

func TestConfigVersion_RuntimeWritesDoNotRefuseASettingsEdit(t *testing.T) {
	cases := []struct {
		name  string
		seed  func(*state.Competition)
		write func(*state.Competition)
	}{
		{"swiss round", func(c *state.Competition) { c.Format = state.CompFormatSwiss; c.SwissRounds = 3 },
			func(c *state.Competition) { c.SwissCurrentRound = 1 }},
		{"status flip", nil, func(c *state.Competition) { c.Status = state.CompStatusDrawReady }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			cid := "runtime-write"
			page, loaded := cvSeed(t, r, store, cid, tc.seed)

			current, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			tc.write(current)
			require.NoError(t, store.SaveCompetition(current))

			w := cvPut(t, r, cid, cvSettingsBody(t, page, loaded, map[string]any{"knockoutMatchDurationSeconds": 240}))
			require.Equalf(t, http.StatusOK, w.Code, "a duration edit after a %s: %s", tc.name, w.Body.String())
			reloaded, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, 240, reloaded.KnockoutMatchDurationSeconds)
		})
	}
}

func TestConfigVersion_StaleSettingsSaveAfterCheckInToggleIsRefused(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	cid := "stale-checkin"
	page, v1 := cvSeed(t, r, store, cid, nil)

	w := cvPut(t, r, cid, cvSettingsBody(t, page, v1, map[string]any{"checkInEnabled": true}))
	require.Equalf(t, http.StatusOK, w.Code, "the check-in toggle saves: %s", w.Body.String())
	v2 := cvAnswerVersion(t, w)
	require.NotEqual(t, v1, v2)

	w = cvPut(t, r, cid, cvSettingsBody(t, page, v1, map[string]any{"checkInEnabled": false, "poolMatchDurationSeconds": 240}))
	require.Equalf(t, http.StatusConflict, w.Code, "stale save after check-in toggle is refused: %s", w.Body.String())
	var refusal struct {
		Code          string `json:"code"`
		Error         string `json:"error"`
		ConfigVersion string `json:"configVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "config_changed", refusal.Code)
	assert.Equal(t, "This competition changed, reload it.", refusal.Error)
	assert.Equal(t, v2, refusal.ConfigVersion)

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.True(t, stored.CheckInEnabled, "CheckInEnabled remains true")
}

func TestConfigVersion_StaleVersionRefused(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	cid := "stale-version"
	page, v1 := cvSeed(t, r, store, cid, nil)
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{{Name: "Alice", Dojo: "Wakaba"}}))

	w := cvPut(t, r, cid, cvSettingsBody(t, page, v1, map[string]any{"poolMatchDurationSeconds": 240}))
	require.Equalf(t, http.StatusOK, w.Code, "the current version saves: %s", w.Body.String())
	v2 := cvAnswerVersion(t, w)
	require.NotEqual(t, v1, v2)
	diskAtV2 := cvConfigBytes(t, dir, cid)

	w = cvPut(t, r, cid, cvSettingsBody(t, page, v1, map[string]any{"startTime": "13:00"}))
	require.Equalf(t, http.StatusConflict, w.Code, "an older version is refused: %s", w.Body.String())
	var refusal struct {
		Code          string `json:"code"`
		Error         string `json:"error"`
		ConfigVersion string `json:"configVersion"`
		Competition   struct {
			StartTime                string          `json:"startTime"`
			PoolMatchDurationSeconds int             `json:"poolMatchDurationSeconds"`
			ConfigVersion            string          `json:"configVersion"`
			RosterVersion            string          `json:"rosterVersion"`
			Players                  []domain.Player `json:"players"`
		} `json:"competition"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "config_changed", refusal.Code)
	assert.Equal(t, "This competition changed, reload it.", refusal.Error)
	assert.Equal(t, v2, refusal.ConfigVersion)
	assert.Equal(t, v2, refusal.Competition.ConfigVersion)
	assert.Equal(t, 240, refusal.Competition.PoolMatchDurationSeconds, "the refusal carries the current settings")
	assert.Equal(t, "09:00", refusal.Competition.StartTime)
	assert.NotEmpty(t, refusal.Competition.RosterVersion)
	assert.Len(t, refusal.Competition.Players, 1)
	assert.Equal(t, string(diskAtV2), string(cvConfigBytes(t, dir, cid)), "a refused PUT writes nothing")

	w = cvPut(t, r, cid, cvSettingsBody(t, page, "", map[string]any{"poolMatchDurationSeconds": 300}))
	require.Equalf(t, http.StatusOK, w.Code, "no version is unchecked: %s", w.Body.String())
	reloaded, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, 300, reloaded.PoolMatchDurationSeconds)
}

func TestConfigVersion_RosterPUTIsNotChecked(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	cid := "roster-put"
	page, v1 := cvSeed(t, r, store, cid, nil)
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{{Name: "Alice", Dojo: "Wakaba"}}))

	w := cvPut(t, r, cid, cvSettingsBody(t, page, v1, map[string]any{"poolMatchDurationSeconds": 240}))
	require.Equalf(t, http.StatusOK, w.Code, "settings PUT: %s", w.Body.String())

	raw, err := json.Marshal(page)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	m["configVersion"] = v1
	m["players"] = []map[string]any{
		{"name": "Alice", "dojo": "Wakaba"},
		{"name": "Bob", "dojo": "Tora"},
	}
	body, err := json.Marshal(m)
	require.NoError(t, err)
	w = cvPut(t, r, cid, body)
	require.Equalf(t, http.StatusOK, w.Code, "a roster PUT with an older config version: %s", w.Body.String())

	players, err := store.LoadParticipants(cid, false)
	require.NoError(t, err)
	assert.Len(t, players, 2)
}

func TestConfigVersion_ConcurrentSettingsPUTs(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	cid := "concurrent-put"
	page, v := cvSeed(t, r, store, cid, nil)

	bodies := [][]byte{
		cvSettingsBody(t, page, v, map[string]any{"poolMatchDurationSeconds": 240}),
		cvSettingsBody(t, page, v, map[string]any{"poolMatchDurationSeconds": 300}),
	}
	answers := make([]*httptest.ResponseRecorder, len(bodies))
	var wg sync.WaitGroup
	for i, b := range bodies {
		wg.Add(1)
		go func(i int, b []byte) {
			defer wg.Done()
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/api/competitions/"+cid, bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			answers[i] = w
		}(i, b)
	}
	wg.Wait()

	var ok, refused int
	for _, w := range answers {
		switch w.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			refused++
			assert.Equal(t, "config_changed", decodeErrorCode(t, w))
		default:
			t.Errorf("unexpected answer %d: %s", w.Code, w.Body.String())
		}
	}
	assert.Equal(t, 1, ok, "one PUT lands")
	assert.Equal(t, 1, refused, "the other is refused config_changed")
}

func decodeErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body.Code
}

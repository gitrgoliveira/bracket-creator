package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The aggregate deliberately swallows a per-file load failure and serves what
// it got, so one unreadable file cannot blank a whole competition view. That
// left the operator with a silently half-empty competition and no way to learn
// why: "the bracket is missing" and "the bracket file will not parse" looked
// identical. These pin the located reason travelling with the payload.
func TestViewerAggregateReportsACorruptFile(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools,
	}))

	broken := "{\n  \"rounds\": [\n    [{\"id\": \"R1-1\"x}]\n  ]\n}\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "bracket.json"), []byte(broken), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"one unreadable file must not blank the whole view")

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)

	issues, ok := payload[0]["dataIssues"].([]any)
	require.True(t, ok, "the competition carries its data issues, got %#v", payload[0]["dataIssues"])
	require.Len(t, issues, 1)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "bracket.json", issue["file"])
	assert.EqualValues(t, 3, issue["line"], "the line the operator has to open")
	assert.NotEmpty(t, issue["detail"])
}

func TestViewerAggregateHasNoIssuesWhenTheFilesAreFine(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools,
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)
	_, present := payload[0]["dataIssues"]
	assert.False(t, present, "no banner when there is nothing to repair")
}

func TestDataIssuesFromKeepsOnlyRepairableFailures(t *testing.T) {
	// A missing file, a permissions problem or a nil is not something an
	// operator fixes in a text editor, so it gets no banner.
	issues := dataIssuesFrom(
		nil,
		os.ErrNotExist,
		&state.CorruptFileError{File: "pool-matches.csv", Line: 9, Column: 4, Detail: "bare \" in field"},
	)
	require.Len(t, issues, 1)
	assert.Equal(t, "pool-matches.csv", issues[0]["file"])
	assert.Equal(t, 9, issues[0]["line"])
}

// bc-pnum ruling 1b: a legacy participants.csv that predates the id-minting
// write path loads fine (no parse error) but leaves rows with no stable id.
// The viewer aggregate must still report it, distinct from a corrupt file
// (nothing failed to parse here), naming the affected competitors and the
// remedy.
func TestViewerAggregateReportsParticipantsMissingIDs(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools,
	}))

	// Legacy, id-less rows: no leading UUID column.
	legacy := "Dave, Dojo D\nEve, Dojo E\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "participants.csv"), []byte(legacy), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)

	issues, ok := payload[0]["dataIssues"].([]any)
	require.True(t, ok, "the competition carries its data issues, got %#v", payload[0]["dataIssues"])
	require.Len(t, issues, 1)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "missing-ids", issue["kind"])
	assert.Equal(t, "participants.csv", issue["file"])
	detail, _ := issue["detail"].(string)
	assert.Contains(t, detail, "Dave")
	assert.Contains(t, detail, "Eve")
	assert.Contains(t, detail, "Save the roster once and the ids are assigned.")
}

// A fully-stamped roster (every row already has a UUID from a prior save)
// must NOT trip the missing-ids issue.
func TestViewerAggregateNoMissingIDsIssueWhenRosterIsStamped(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{Name: "Dave", Dojo: "Dojo D"},
		{Name: "Eve", Dojo: "Dojo E"},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)
	_, present := payload[0]["dataIssues"]
	assert.False(t, present, "a fully-stamped roster raises no missing-ids issue")
}

func TestMissingParticipantIDsIssue_ManyRowsNamesFirstFewPlusCount(t *testing.T) {
	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
		{Name: "Carol", Dojo: "Dojo C"},
		{Name: "Dave", Dojo: "Dojo D"},
		{Name: "Eve", Dojo: "Dojo E"},
	}
	issue := missingParticipantIDsIssue(players)
	require.NotNil(t, issue)
	detail, _ := (*issue)["detail"].(string)
	assert.Contains(t, detail, "5 competitors, including")
	assert.Contains(t, detail, "Alice")
	assert.Contains(t, detail, "Bob")
	assert.Contains(t, detail, "Carol")
	assert.NotContains(t, detail, "Dave", "only the first few are named for a large roster")
}

func TestMissingParticipantIDsIssue_NilWhenEveryRowHasAnID(t *testing.T) {
	players := []domain.Player{
		{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
	}
	assert.Nil(t, missingParticipantIDsIssue(players))
}

// bc-pnum ruling 1e follow-up: the single-competition detail endpoint
// (GET /api/viewer/competitions/:id) used to compute no dataIssues at all,
// so a competition whose OWN aggregate list entry named a missing-ids or
// corrupt-file issue lost that entry the moment the admin console's
// competition Overview loaded the detail (admin.jsx renders
// `detail?.config || c`, and `detail.config.dataIssues` did not exist).
// These two pin that the detail endpoint now reports the identical issues
// the aggregate does, via the shared viewerDataIssues.
func TestViewerDetail_ReportsParticipantsMissingIDs(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools,
	}))
	legacy := "Dave, Dojo D\nEve, Dojo E\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "participants.csv"), []byte(legacy), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions/kendo", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	issues, ok := payload["dataIssues"].([]any)
	require.True(t, ok, "the detail payload carries dataIssues, got %#v", payload["dataIssues"])
	require.Len(t, issues, 1)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "missing-ids", issue["kind"])
	detail, _ := issue["detail"].(string)
	assert.Contains(t, detail, "Dave")
	assert.Contains(t, detail, "Eve")
}

// TestViewerDetail_ReportsCorruptPoolsAndDoesNotFail is the corrupt-file
// half of the same pin: an unparseable pools.csv used to fail the WHOLE
// detail request (the pre-fix abort loop returned on ANY of the five
// concurrent loads' errors, including engine.CalculatePoolStandings' own
// internal LoadPools failing on the identical file), so the operator saw a
// 500 with no reason rather than a named, repairable fault. It now
// degrades, matching the aggregate's own resilience, and reports the
// located fault.
func TestViewerDetail_ReportsCorruptPoolsAndDoesNotFail(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "corrupt-pools-detail", Name: "Corrupt Pools", Status: state.CompStatusPools,
		Kind: "individual", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants("corrupt-pools-detail", []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice"},
		{Name: "Bob", Dojo: "Dojo Bob"},
	}))
	// The same corrupting bytes handlers_viewer_test.go's own corrupt-pools
	// fixture uses (a bare, unterminated quote): a genuine csv.Reader parse
	// failure, not a hand-built error.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "corrupt-pools-detail", "pools.csv"),
		[]byte("a,b\na,\"bad\nquote"), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions/corrupt-pools-detail", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"an unparseable pools.csv must degrade, not fail the whole detail request; body: %s", w.Body.String())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	issues, ok := payload["dataIssues"].([]any)
	require.True(t, ok, "the detail payload carries dataIssues, got %#v", payload["dataIssues"])
	require.Len(t, issues, 1, "poolsErr and standingsErr report the identical fault and must not double up: %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "pools.csv", issue["file"])
	assert.NotEmpty(t, issue["detail"])
}

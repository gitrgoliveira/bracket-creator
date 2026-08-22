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

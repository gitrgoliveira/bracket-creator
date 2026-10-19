package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// lineupWarningTable is the server's lineup warning as a write's answer
// carries it, shared with the outbox's test
// (web-mobile/js/__tests__/outbox_score_warning.test.jsx), so the client pins
// the sentence the server sends (stage 6 review 11, item 7).
type lineupWarningTable struct {
	MatchStarted string `json:"matchStarted"`
}

func loadLineupWarningTable(t *testing.T) lineupWarningTable {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "lineup_warnings.json"))
	require.NoError(t, err)
	var table lineupWarningTable
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.MatchStarted)
	return table
}

// The shared sentence is the one a real start answers with.
func TestLineupWarning_SharedTableIsTheServersSentence(t *testing.T) {
	table := loadLineupWarningTable(t)
	e := newCarryEnv(t, true)
	refusedCarryAfterP10(t, e)
	rec := e.do(http.MethodPut, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape("P1-0")+"/score", map[string]any{
		"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{"result"}, "modifiedAt": e.base + 30, "writeId": "start-P1-0",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, table.MatchStarted, warningOf(t, rec.Body))
}

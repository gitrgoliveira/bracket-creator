package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

type recordedResultCase struct {
	Name        string              `json:"name"`
	PoolMatches []state.MatchResult `json:"poolMatches"`
	Bracket     *state.Bracket      `json:"bracket"`
	Expected    bool                `json:"expected"`
}

// TestRecordedResultIn_SharedTable runs the table the browser's mirror
// (competitionHasRecordedResult) is pinned to by too.
func TestRecordedResultIn_SharedTable(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "recorded_result_cases.json"))
	require.NoError(t, err)
	var table struct {
		Cases []recordedResultCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(data, &table))
	require.NotEmpty(t, table.Cases)
	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			assert.Equal(t, tc.Expected, recordedResultIn(tc.PoolMatches, tc.Bracket))
		})
	}
}

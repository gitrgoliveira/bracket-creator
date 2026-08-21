package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Go half of the shared Go/JS golden table for "does this format draw a
// bracket?": see the `_comment` in testdata/format_draws_bracket.json for why
// the table is shared and what the default branch means in it. JS half:
// web-mobile/js/__tests__/format_draws_bracket.test.jsx.
//
// Until this existed, formatDrawsBracket was the one Go/JS mirror in the
// shiaijo-count rule with nothing pinning it: the message is pinned by
// TestShiaijoRuleJSMirrorsMatchTheGoMessage and the seed-gap wording by its own
// shared table, but the SCOPE -- which formats the rule applies to at all -- was
// hand-copied. Both suites stayed green through a divergence that would disable
// the console's Create and Start buttons for an allocation the server accepts.

type formatDrawsBracketCase struct {
	Why          string `json:"why"`
	Format       string `json:"format"`
	DrawsBracket bool   `json:"drawsBracket"`
}

func loadFormatDrawsBracketTable(t *testing.T) []formatDrawsBracketCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "format_draws_bracket.json"))
	require.NoError(t, err, "reading the shared Go/JS golden table")
	var table struct {
		Cases []formatDrawsBracketCase `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table), "parsing the shared Go/JS golden table")
	// Load-bearing: ranging over an empty slice asserts nothing and stays green,
	// so a degraded table needs its own failure.
	require.NotEmpty(t, table.Cases,
		"testdata/format_draws_bracket.json parsed to zero cases: the mirror would assert nothing")
	return table.Cases
}

func TestCompetitionDrawsBracket_GoldenTable(t *testing.T) {
	t.Parallel()

	for _, tc := range loadFormatDrawsBracketTable(t) {
		t.Run(tc.Why, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.DrawsBracket, CompetitionDrawsBracket(tc.Format),
				"CompetitionDrawsBracket(%q); the JS mirror formatDrawsBracket reads the same table, "+
					"so a change here has to be made there too", tc.Format)
		})
	}
}

// TestCompetitionDrawsBracket_TableCoversEveryFormat is what stops a new format
// being added without anyone deciding this question. Without it the table would
// keep passing while the new format silently took the default branch on the Go
// side and whatever the console's hand-written list happened to say on the other.
func TestCompetitionDrawsBracket_TableCoversEveryFormat(t *testing.T) {
	t.Parallel()

	covered := make(map[string]bool)
	for _, tc := range loadFormatDrawsBracketTable(t) {
		covered[tc.Format] = true
	}

	for _, format := range []string{
		state.CompFormatPlayoffs,
		state.CompFormatMixed,
		state.CompFormatLeague,
		state.CompFormatSwiss,
	} {
		assert.True(t, covered[format],
			"format %q has no case in testdata/format_draws_bracket.json: add one saying whether "+
				"its draw builds a bracket, so the operator console is told too", format)
	}
}

// ValidateCompetitionShiaijoCount is the composite the API and the draw pipeline
// share, and the format scope is one of its two exemptions. Pinning it against
// the same table keeps the exemption from being satisfied by a second, drifting
// copy of the format switch.
func TestValidateCompetitionShiaijoCountAppliesTheTablesScope(t *testing.T) {
	t.Parallel()

	// 3 is the smallest illegal count and the one the app's own league hint
	// recommends, so it is exactly where a format-blind rule shows up.
	illegal := []string{"A", "B", "C"}

	for _, tc := range loadFormatDrawsBracketTable(t) {
		t.Run(tc.Why, func(t *testing.T) {
			t.Parallel()
			err := ValidateCompetitionShiaijoCount(illegal, tc.Format)
			if tc.DrawsBracket {
				assert.Error(t, err, "format %q draws a bracket, so 3 shiaijo must be refused", tc.Format)
			} else {
				assert.NoError(t, err, "format %q draws no bracket, so any shiaijo count is fine", tc.Format)
			}
			// The empty-list exemption is unconditional: it means "inherit the
			// tournament's courts" and carries no count of its own.
			assert.NoError(t, ValidateCompetitionShiaijoCount(nil, tc.Format),
				"an empty allocation carries no count to validate, whatever the format")
		})
	}
}

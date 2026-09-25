package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// UAT (bc-tmfn): correcting "Pool A · Match 1" in a mixed competition raised
// a confirm about "Match 1", the first KNOCKOUT match, which read as the pool
// match on screen: pool matches are numbered from 1 inside each pool. A
// knockout match is therefore named with its round, the heading of the
// bracket column the operator finds it under.
func TestMatchLabel_NamesTheKnockoutRound(t *testing.T) {
	assert.Equal(t, "Match 1 (Semifinals)", MatchLabel(ReopenedMatch{ID: "m-r2-0", Number: 1, DisplayRound: 2}))
	assert.Equal(t, "Match 3 (Final)", MatchLabel(ReopenedMatch{ID: "m-r3-0", Number: 3, DisplayRound: 1}))
	assert.Equal(t, "Match 5 (R16)", MatchLabel(ReopenedMatch{ID: "m-r1-4", Number: 5, DisplayRound: 4}))
	// A bracket saved before DisplayRound existed has no round to name, and
	// still must not read as a pool match.
	assert.Equal(t, "knockout Match 3", MatchLabel(ReopenedMatch{ID: "m-r3-0", Number: 3}))
	// The bronze carries DisplayRound -1 and no number: it keeps its name.
	assert.Equal(t, "the 3rd-place match", MatchLabel(ReopenedMatch{ID: state.BronzeMatchID, DisplayRound: -1}))
}

// Every construction site goes through bracketMatchRef, so the round a stored
// bracket match carries reaches the label.
func TestBracketMatchRef_CarriesTheRound(t *testing.T) {
	ref := bracketMatchRef(&state.BracketMatch{ID: "m-r2-1", MatchNumber: 2, DisplayRound: 2})
	assert.Equal(t, ReopenedMatch{ID: "m-r2-1", Number: 2, DisplayRound: 2}, ref)
	assert.Equal(t, "Match 2 (Semifinals)", MatchLabel(ref))
}

// TestOperatorMatchLabel_GoldenTable is the Go half of the shared Go/JS
// table of operator-facing match labels -- see the `_comment` in
// testdata/match_labels.json for the schema and why the pool-phase and
// knockout rows are built differently below.
func TestOperatorMatchLabel_GoldenTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "match_labels.json"))
	require.NoError(t, err, "shared Go/JS golden table is missing")

	var table struct {
		Cases []struct {
			ID           string `json:"id"`
			Format       string `json:"format"`
			Label        string `json:"label"`
			MatchNumber  int    `json:"matchNumber"`
			DisplayRound int    `json:"displayRound"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases, "golden table parsed to zero cases: it would assert nothing")

	for _, tc := range table.Cases {
		t.Run(tc.ID, func(t *testing.T) {
			t.Parallel()
			comp := &state.Competition{Format: tc.Format}
			var bracket *state.Bracket
			if tc.Format == "knockout" {
				bracket = &state.Bracket{}
				switch {
				case tc.MatchNumber > 0:
					bracket.Rounds = [][]state.BracketMatch{{{
						ID: tc.ID, MatchNumber: tc.MatchNumber, DisplayRound: tc.DisplayRound,
					}}}
				case tc.ID == state.BronzeMatchID:
					bracket.ThirdPlaceMatch = &state.BracketMatch{ID: tc.ID}
				}
				// Anything else (the "unresolved knockout id" case) is left
				// out of the bracket entirely, so OperatorMatchLabel falls
				// through to MatchLabel's own bare-id fallback.
			}
			assert.Equal(t, tc.Label, OperatorMatchLabel(comp, bracket, tc.ID),
				"Go label disagrees with the shared table; update BOTH spellings, not just this one")
		})
	}
}

// TestRoundLabelFromEnd_GoldenTable is the Go half of the shared Go/JS table
// of knockout round names -- see the `_comment` in testdata/round_labels.json.
// JS half: the "round label Go/JS mirror" describe in
// web-mobile/js/__tests__/write_result_downstream_knockout.test.jsx.
func TestRoundLabelFromEnd_GoldenTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "round_labels.json"))
	require.NoError(t, err, "shared Go/JS golden table is missing")

	var table struct {
		Cases []struct {
			DisplayRound int    `json:"displayRound"`
			Label        string `json:"label"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases, "golden table parsed to zero cases: it would assert nothing")

	for _, tc := range table.Cases {
		t.Run(fmt.Sprintf("displayRound=%d", tc.DisplayRound), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.Label, roundLabelFromEnd(tc.DisplayRound-1),
				"Go round name disagrees with the shared table; update BOTH spellings, not just this one")
		})
	}
}

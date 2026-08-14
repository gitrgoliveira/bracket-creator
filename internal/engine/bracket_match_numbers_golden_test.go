package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Bracket match numbers are a CROSS-LANGUAGE contract, and until this file
// nothing pinned it.
//
// assignBracketMatchNumbers (bracket.go) stamps MatchNumber on every real match
// and the SPA re-derives the same numbering independently, in buildDisplayModel
// (web-mobile/js/bracket.jsx), to label its cards "M1", "M2". A referee holding
// the printed Excel sheet and looking at the operator's screen has to read the
// same number for the same bout, so the two walks must agree exactly -- and they
// have drifted before: bc-draw fixed a case where the app and the sheet named
// different bouts "Match 12", by patching the JS tie-break to match Go.
//
// The agreement rests on two conventions that no compiler checks:
//
//  1. The two REAL-match filters coincide. Go keeps a match when it is not
//     Hidden and not empty-vs-empty; the JS keeps it when it is not hidden and
//     displayRound > 0. Those are different predicates over the same data, so a
//     match that ever satisfies one and not the other shifts every number after
//     it on one side only.
//  2. A match id's last segment equals its index within its round. Go takes the
//     position from the slice index; the JS parses it out of the id string.
//
// testdata/bracket_match_numbers.json is the pin, in the shape this repo already
// uses for cross-language contracts (internal/export/testdata/encho_labels.json,
// internal/helper/testdata/seed_gap_messages.json): THIS test writes what the
// engine actually persists, and web-mobile/js/__tests__/bracket_match_numbers.test.jsx
// asserts the SPA derives the same numbers from the same brackets. A drift on
// either side fails on that side, naming the case.
//
// Regeneration (the repo's convention):
//
//	UPDATE_GOLDEN=1 go test ./internal/engine/ -run TestBracketMatchNumbersGolden
//
// Do NOT hand-edit a value: a diff here means the numbering moved, and the
// reason has to be stated. Regeneration is deterministic -- the entrants are
// generated names, there is no seeding, and no clock or map iteration reaches
// the output.
//
// bracketNumberMatch is the subset of state.BracketMatch the numbering depends
// on. Deliberately not the whole struct: winner/status/court/scheduledAt change
// with unrelated work and would churn the golden for no contract reason. The
// json tags are state.BracketMatch's own, so the fixture is the wire shape the
// SPA really receives.
type bracketNumberMatch struct {
	ID           string   `json:"id"`
	SideA        string   `json:"sideA"`
	SideB        string   `json:"sideB"`
	DisplayRound int      `json:"displayRound,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	Feeders      []string `json:"feeders,omitempty"`
	MatchNumber  int      `json:"matchNumber,omitempty"`
}

type bracketNumberCase struct {
	Name     string `json:"name"`
	Entrants int    `json:"entrants"`
	// Discriminating marks a case whose bracket can tell the correct ordering
	// (by leaf slot) apart from the pre-fix one (by position within the round).
	// The JS mirror requires at least one, so the table cannot decay into a set
	// of shapes that passes against the bug.
	Discriminating bool                   `json:"discriminating"`
	Rounds         [][]bracketNumberMatch `json:"rounds"`
}

type bracketNumberGolden struct {
	Comment []string            `json:"_comment"`
	Cases   []bracketNumberCase `json:"cases"`
}

// The cases worth pinning.
//
// DISCRIMINATING is the load-bearing column, and it is why this list is not
// just a handful of round numbers. Both walks sort on effective round first and
// then LEFT TO RIGHT, where "left to right" is the match's leftmost first-round
// leaf slot (pos << (backendRound+1)), not its position within its round. On
// most bracket shapes the two orderings coincide, so a case that is not marked
// discriminating cannot tell a correct implementation from one that sorts on
// position alone -- which is precisely the drift bc-draw had to fix. A golden
// made only of those cases passes against the bug.
//
// The marked cases were found by sweeping formats, entrant counts and shiaijo
// counts for a bracket where the two orderings genuinely disagree; each one has
// an effective round holding a DEEP match at a small position alongside a
// SHALLOW match at a larger position. 9 entrants is the smallest.
//
// The rest are kept for coverage of the ordinary shapes: exact powers of two
// have no byes at all, and 5/11/13 put byes in different places. Byes matter
// because they are exactly the matches the two REAL-match filters disagree
// about.
var bracketNumberCases = []struct {
	format         string
	entrants       int
	courts         int
	discriminating bool
}{
	{state.CompFormatPlayoffs, 4, 1, false},
	{state.CompFormatPlayoffs, 5, 1, false},
	{state.CompFormatPlayoffs, 8, 1, false},
	{state.CompFormatPlayoffs, 9, 1, true},
	{state.CompFormatPlayoffs, 11, 1, false},
	{state.CompFormatPlayoffs, 13, 1, false},
	{state.CompFormatPlayoffs, 16, 1, false},
	{state.CompFormatPlayoffs, 19, 1, true},
	// Mixed preview brackets: pool-origin placeholders rather than players, and
	// the shape the court-region draw actually produces on several shiaijo.
	{state.CompFormatMixed, 40, 2, true},
	{state.CompFormatMixed, 40, 4, true},
}

func buildBracketNumberCases(t *testing.T) []bracketNumberCase {
	t.Helper()
	cases := make([]bracketNumberCase, 0, len(bracketNumberCases))
	for _, c := range bracketNumberCases {
		eng, store, _ := setupTestEngine(t)
		compID := "match-numbers"
		createTestCompetition(t, store, compID, c.format, 4, func(comp *state.Competition) {
			comp.Courts = courtLabels(c.courts)
		})
		require.NoError(t, store.SaveParticipants(compID, makePlayers(c.entrants)))
		require.NoError(t, eng.GenerateDraw(compID))

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, bracket)

		rounds := make([][]bracketNumberMatch, 0, len(bracket.Rounds))
		for _, round := range bracket.Rounds {
			out := make([]bracketNumberMatch, 0, len(round))
			for _, m := range round {
				out = append(out, bracketNumberMatch{
					ID:           m.ID,
					SideA:        m.SideA,
					SideB:        m.SideB,
					DisplayRound: m.DisplayRound,
					Hidden:       m.Hidden,
					Feeders:      m.Feeders,
					MatchNumber:  m.MatchNumber,
				})
			}
			rounds = append(rounds, out)
		}
		name := fmt.Sprintf("%s on %d shiaijo", c.format, c.courts)
		if c.discriminating {
			name += ", orderings disagree here"
		}
		cases = append(cases, bracketNumberCase{
			Name:           name,
			Entrants:       c.entrants,
			Discriminating: c.discriminating,
			Rounds:         rounds,
		})
	}
	return cases
}

func TestBracketMatchNumbersGolden(t *testing.T) {
	cases := buildBracketNumberCases(t)

	// Without a discriminating case the whole table passes against the very bug
	// it exists to catch, so its absence is a failure in its own right.
	discriminating := 0
	for _, c := range cases {
		if c.Discriminating {
			discriminating++
		}
	}
	require.Positive(t, discriminating,
		"no case distinguishes ordering by leaf slot from ordering by position: this table would pass against the pre-fix numbering")

	// Every case must actually number something, or the JS mirror would assert
	// nothing and pass on an empty file.
	for _, c := range cases {
		numbered := 0
		for _, round := range c.Rounds {
			for _, m := range round {
				if m.MatchNumber > 0 {
					numbered++
				}
			}
		}
		require.Positivef(t, numbered, "%d entrants: no match carries a number, the golden would pin nothing", c.Entrants)
	}

	golden := bracketNumberGolden{
		Comment: []string{
			"Bracket match numbers as internal/engine persists them, and the pin for the",
			"SPA's independent re-derivation in web-mobile/js/bracket.jsx (buildDisplayModel).",
			"A card labelled M<n> and the printed Excel sheet's Match <n> must name the same",
			"bout. Both sides read this file: Go in bracket_match_numbers_golden_test.go,",
			"JS in web-mobile/js/__tests__/bracket_match_numbers.test.jsx.",
			"Do NOT hand-edit. Regenerate with:",
			"UPDATE_GOLDEN=1 go test ./internal/engine/ -run TestBracketMatchNumbersGolden",
		},
		Cases: cases,
	}

	encoded, err := json.MarshalIndent(golden, "", "  ")
	require.NoError(t, err)
	encoded = append(encoded, '\n')

	path := filepath.Join("testdata", "bracket_match_numbers.json")
	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		t.Logf("wrote %s (%d cases)", path, len(cases))
		return
	}

	want, err := os.ReadFile(path) // #nosec G304 -- fixed test-local path
	require.NoError(t, err,
		"golden file missing; regenerate with: UPDATE_GOLDEN=1 go test ./internal/engine/ -run TestBracketMatchNumbersGolden")

	var parsed bracketNumberGolden
	require.NoError(t, json.Unmarshal(want, &parsed))
	require.Len(t, parsed.Cases, len(cases), "case count moved; regenerate the golden")

	for i, got := range cases {
		expected := parsed.Cases[i]
		assert.Equalf(t, expected.Entrants, got.Entrants, "case %d: entrant count moved", i)
		assert.Equalf(t, expected.Rounds, got.Rounds,
			"%d entrants: the engine's bracket numbering moved. If that is intentional, "+
				"regenerate with UPDATE_GOLDEN=1 and check the JS mirror still passes: "+
				"the SPA re-derives these numbers and the printed sheet has to agree",
			got.Entrants)
	}
}

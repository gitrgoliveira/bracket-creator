package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// PoolCourtByName tells the workbook writers which shiaijo a pool is ACTUALLY
// being fought on, and its contract is that a pool is reported only when EVERY
// one of its matches agrees. The whole block moves on that answer: the Pool
// Matches header, its matches, its standings table and its "Names to Print"
// roster sheet.
//
// Rows with no court recorded used to be SKIPPED rather than counted as
// disagreement, so one bout carrying a court spoke for a pool whose others
// carried none. Unknown is not agreement -- the same rule helper.CourtPlan.
// PageCourt applies to a tree page.
func TestPoolCourtByNameRequiresEveryMatchToAgree(t *testing.T) {
	t.Parallel()

	m := func(id, court string) state.MatchResult {
		return state.MatchResult{ID: id, Court: court}
	}

	cases := []struct {
		name    string
		matches []state.MatchResult
		want    map[string]string
	}{
		{
			name:    "every match on one shiaijo reports the move",
			matches: []state.MatchResult{m("Pool A-0", "D"), m("Pool A-1", "D"), m("Pool A-2", "D")},
			want:    map[string]string{"Pool A": "D"},
		},
		{
			name:    "one bout moved elsewhere keeps the drawn band",
			matches: []state.MatchResult{m("Pool A-0", "D"), m("Pool A-1", "A"), m("Pool A-2", "D")},
			want:    map[string]string{},
		},
		{
			name:    "one bout with a court and the rest without is not a move",
			matches: []state.MatchResult{m("Pool A-0", "D"), m("Pool A-1", ""), m("Pool A-2", "")},
			want:    map[string]string{},
		},
		{
			name:    "no courts at all says nothing",
			matches: []state.MatchResult{m("Pool A-0", ""), m("Pool A-1", "")},
			want:    map[string]string{},
		},
		{
			name: "one pool agreeing does not lose its answer to another that does not",
			matches: []state.MatchResult{
				m("Pool A-0", "D"), m("Pool A-1", "D"),
				m("Pool B-0", "B"), m("Pool B-1", ""),
			},
			want: map[string]string{"Pool A": "D"},
		},
		{
			// A hyphenated pool name and the -TB- suffix both go through
			// poolNameFromMatchID, not a first-hyphen split, so neither folds
			// two pools into one key and reports the pair as split.
			name:    "a tiebreaker bout counts toward its own pool",
			matches: []state.MatchResult{m("Pool A-0", "C"), m("Pool A-TB-0", "C")},
			want:    map[string]string{"Pool A": "C"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := PoolCourtByName(tc.matches)
			if len(tc.want) == 0 {
				assert.Empty(t, got, "nothing may be reported as moved here; the writers read that as 'use the draw'")
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

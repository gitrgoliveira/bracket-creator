package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestedMaxCourts(t *testing.T) {
	tests := []struct {
		players  int
		expected int
		why      string
	}{
		{3, 1, "floor(3/2)-1 = 0, clamped to 1"},
		{4, 1, "floor(4/2)-1 = 1"},
		{5, 1, "floor(5/2)-1 = 1"},
		{6, 2, "floor(6/2)-1 = 2"},
		{8, 3, "floor(8/2)-1 = 3"},
		{10, 4, "floor(10/2)-1 = 4"},
		{16, 7, "floor(16/2)-1 = 7"},
		{2, 1, "minimum clamp"},
	}

	for _, tc := range tests {
		t.Run(tc.why, func(t *testing.T) {
			got := SuggestedMaxCourts(tc.players)
			assert.Equalf(t, tc.expected, got, "players=%d: %s", tc.players, tc.why)
		})
	}
}

func TestValidateCourtCount(t *testing.T) {
	tests := []struct {
		players int
		courts  int
		wantErr bool
		desc    string
	}{
		// 8 players: hardCap=4
		{8, 3, false, "8p/3c: ok (below cap)"},
		{8, 4, false, "8p/4c: ok (== floor(8/2), warning is frontend-only)"},
		{8, 5, true, "8p/5c: error (> floor(8/2))"},

		// 6 players: hardCap=3
		{6, 2, false, "6p/2c: ok"},
		{6, 3, false, "6p/3c: ok (== floor(6/2))"},
		{6, 4, true, "6p/4c: error"},

		// 3 players: hardCap=1
		{3, 1, false, "3p/1c: ok"},
		{3, 2, true, "3p/2c: error"},

		// 4 players: hardCap=2
		{4, 1, false, "4p/1c: ok"},
		{4, 2, false, "4p/2c: ok (== floor(4/2))"},
		{4, 3, true, "4p/3c: error"},

		// 2 players: hardCap=1
		{2, 1, false, "2p/1c: ok"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateCourtCount(tc.players, tc.courts)
			if tc.wantErr {
				require.Errorf(t, err, "expected error for players=%d courts=%d", tc.players, tc.courts)
			} else {
				require.NoErrorf(t, err, "unexpected error for players=%d courts=%d", tc.players, tc.courts)
			}
		})
	}
}

// TestCourtsOutsideTournament pins the orphaned-shiaijo predicate: a
// competition's courts are a SUBSET of the tournament's, and reducing the
// venue's court count leaves the competition holding one that no longer
// exists. Mirrored client-side by
// web-mobile/js/__tests__/court_membership.test.jsx.
func TestCourtsOutsideTournament(t *testing.T) {
	tests := []struct {
		desc   string
		comp   []string
		tourn  []string
		expect []string
	}{
		{"every court still exists", []string{"A", "B"}, []string{"A", "B", "C"}, nil},
		{"one dropped court", []string{"A", "B", "C", "D"}, []string{"A", "B", "C"}, []string{"D"}},
		{"reports in the competition's order", []string{"A", "D", "B"}, []string{"A"}, []string{"D", "B"}},
		{"identical lists", []string{"A", "B"}, []string{"A", "B"}, nil},
		// Empty comp courts mean "inherit the tournament's", which is
		// trivially a subset (resolveCompetitionCourts materialises it).
		{"no allocation yet", nil, []string{"A", "B"}, nil},
		// Empty tournament courts mean "not known yet" (bootstrap), NOT
		// "the venue has no courts": flagging here would reject everything.
		{"tournament courts unknown", []string{"A"}, nil, nil},
		{"duplicate orphan reported once", []string{"D", "D"}, []string{"A"}, []string{"D"}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.expect, CourtsOutsideTournament(tc.comp, tc.tourn))
		})
	}
}

// TestValidateCourtsInTournament pins the operator-facing message: it names
// the missing shiaijo, the courts that DO exist, and both ways out.
func TestValidateCourtsInTournament(t *testing.T) {
	t.Run("accepts a subset", func(t *testing.T) {
		require.NoError(t, ValidateCourtsInTournament([]string{"A", "B"}, []string{"A", "B", "C"}))
	})

	t.Run("names a single missing shiaijo", func(t *testing.T) {
		err := ValidateCourtsInTournament([]string{"A", "B", "C", "D"}, []string{"A", "B", "C"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shiaijo D is not part of this tournament")
		assert.Contains(t, err.Error(), "the tournament has A, B, C")
		assert.Contains(t, err.Error(), "reassign")
	})

	t.Run("agrees in the plural", func(t *testing.T) {
		err := ValidateCourtsInTournament([]string{"A", "C", "D"}, []string{"A"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "shiaijo C, D are not part of this tournament")
	})

	t.Run("silent when the tournament courts are unknown", func(t *testing.T) {
		require.NoError(t, ValidateCourtsInTournament([]string{"A"}, nil))
	})
}

// TestCourtsStillInUse pins the competition-level twin of the tournament's
// orphan guard: a shiaijo cannot be dropped while this competition's own live
// matches are still assigned to it.
func TestCourtsStillInUse(t *testing.T) {
	t.Parallel()

	poolMatches := []state.MatchResult{
		{ID: "Pool A-1", Court: "A", Status: state.MatchStatusCompleted},
		{ID: "Pool B-1", Court: "B", Status: state.MatchStatusScheduled},
	}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", Court: "C", Status: state.MatchStatusRunning},
		{ID: "m-r1-1", Court: "D", Status: state.MatchStatusCompleted},
	}}}

	t.Run("keeping every court is always fine", func(t *testing.T) {
		t.Parallel()
		assert.Empty(t, CourtsStillInUse([]string{"A", "B", "C", "D"}, poolMatches, bracket))
	})

	t.Run("a scheduled pool match blocks its shiaijo", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"B"}, CourtsStillInUse([]string{"A", "C", "D"}, poolMatches, bracket))
	})

	t.Run("a running knockout bout blocks its shiaijo", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"C"}, CourtsStillInUse([]string{"A", "B", "D"}, poolMatches, bracket))
	})

	t.Run("completed matches never block, so a court frees up as it finishes", func(t *testing.T) {
		t.Parallel()
		// A and D carry ONLY completed matches. Refusing on those would make a
		// shiaijo unremovable for the rest of the tournament.
		assert.Empty(t, CourtsStillInUse([]string{"B", "C"}, poolMatches, bracket),
			"a shiaijo whose bouts are all fought is free to drop")
	})

	t.Run("several blocked shiaijo are reported in allocation order", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"B", "C"}, CourtsStillInUse([]string{"A", "D"}, poolMatches, bracket))
	})

	t.Run("no bracket yet is not a failure", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, []string{"B"}, CourtsStillInUse([]string{"A"}, poolMatches, nil))
	})
}

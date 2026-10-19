package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/require"
)

// Stage 6 review 11, items 3 to 5: the warning names each match once, with
// every reason its carried lineups were refused for; counts the lineups, not
// the matches, so two teams' lineups at one match are plural; and separates
// the groups of matches refused for different reasons so no "and" nests.

var (
	rv11Size = fmt.Errorf("team_lineup: position %q not allowed", "5")
	rv11Dup  = fmt.Errorf("team_lineup: %w", domain.ErrLineupDuplicateMember)
)

// Item 3: both teams' carried lineups at one match, refused for different
// reasons, name that match once with both reasons.
func TestLineupWarning_OneMatchRefusedForTwoReasonsIsNamedOnce(t *testing.T) {
	d := &DerivedLineupError{
		MatchIDs: []string{"Final", "Final"},
		Labels:   []string{"Match 3 (Final)", "Match 3 (Final)"},
		Errs:     []error{rv11Size, rv11Dup},
	}
	require.Equal(t, "The match was started, but the lineups carried to Match 3 (Final) could not be worked out again "+
		"(a position the team size has no place for and a team member placed at two positions); "+
		"they were kept as they were. Check that match's lineups.", d.warning("The match was started"))
}

// Item 4: both teams' carried lineups at one match, refused for the same
// reason, are two lineups.
func TestLineupWarning_TwoTeamsAtOneMatchArePlural(t *testing.T) {
	d := &DerivedLineupError{
		MatchIDs: []string{"Final", "Final"},
		Labels:   []string{"Match 3 (Final)", "Match 3 (Final)"},
		Errs:     []error{rv11Size, rv11Size},
	}
	require.Equal(t, "The match was started, but the lineups carried to Match 3 (Final) could not be worked out again "+
		"(a position the team size has no place for); they were kept as they were. Check that match's lineups.",
		d.warning("The match was started"))
}

// Item 5: matches refused for different reasons are separate groups, so the
// "and" naming matches of one group never runs into the next group.
func TestLineupWarning_GroupsOfMatchesAreSeparated(t *testing.T) {
	d := &DerivedLineupError{
		MatchIDs: []string{"Pool A-2", "Pool A-3", "Pool A-4", "Pool A-5", "Pool A-5"},
		Labels:   []string{"Match 3", "Match 4", "Match 5", "Match 6", "Match 6"},
		Errs:     []error{rv11Size, rv11Size, rv11Dup, rv11Size, rv11Dup},
	}
	require.Equal(t, "The lineup was saved, but the lineups carried to these matches could not be worked out again "+
		"and were kept as they were: Match 3 and Match 4 (a position the team size has no place for); "+
		"Match 5 (a team member placed at two positions); Match 6 (a position the team size has no place for "+
		"and a team member placed at two positions). Check those matches' lineups.", d.Warning())
}

package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

// tiedStanding builds a standing with an explicit name and Points value.
// markTiedStandings only reads Player.Name and Points, so the packed-points
// chain is irrelevant here — equal Points means tied, by construction.
func tiedStanding(name string, points int) state.PlayerStanding {
	return state.PlayerStanding{Player: domain.Player{Name: name}, Points: points}
}

// completedMatch / scheduledMatch build regular pool matches (ID "Pool A-N")
// between two named competitors with the given completion status.
func completedMatch(idx int, a, b string) state.MatchResult {
	return state.MatchResult{ID: fmt.Sprintf("Pool A-%d", idx), SideA: a, SideB: b, Status: state.MatchStatusCompleted}
}

func scheduledMatch(idx int, a, b string) state.MatchResult {
	return state.MatchResult{ID: fmt.Sprintf("Pool A-%d", idx), SideA: a, SideB: b, Status: state.MatchStatusScheduled}
}

// tiedNames returns the set of names flagged Tied, for order-independent asserts.
func tiedNames(sorted []state.PlayerStanding) map[string]bool {
	out := map[string]bool{}
	for _, s := range sorted {
		if s.Tied {
			out[s.Player.Name] = true
		}
	}
	return out
}

// TestMarkTiedStandings_Pools covers the pools (non-league) gate: amber appears
// only once every regular match in the pool is complete.
func TestMarkTiedStandings_Pools(t *testing.T) {
	comp := &state.Competition{Format: state.CompFormatMixed}

	t.Run("no matches yet → no amber even when all tied at 0", func(t *testing.T) {
		sorted := []state.PlayerStanding{tiedStanding("A", 0), tiedStanding("B", 0), tiedStanding("C", 0)}
		markTiedStandings(comp, sorted, nil)
		assert.Empty(t, tiedNames(sorted), "pre-start pool must not surface amber")
	})

	t.Run("pool complete with a tie → tied rows marked", func(t *testing.T) {
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50)}
		matches := []state.MatchResult{completedMatch(0, "A", "B"), completedMatch(1, "A", "C"), completedMatch(2, "B", "C")}
		markTiedStandings(comp, sorted, matches)
		assert.Equal(t, map[string]bool{"A": true, "B": true}, tiedNames(sorted))
	})

	t.Run("pool incomplete → no amber even if currently tied", func(t *testing.T) {
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50)}
		matches := []state.MatchResult{completedMatch(0, "A", "B"), scheduledMatch(1, "A", "C")}
		markTiedStandings(comp, sorted, matches)
		assert.Empty(t, tiedNames(sorted), "an unscored match must suppress amber")
	})

	t.Run("pool complete, no tie → nothing marked", func(t *testing.T) {
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 60), tiedStanding("C", 50)}
		matches := []state.MatchResult{completedMatch(0, "A", "B"), completedMatch(1, "A", "C"), completedMatch(2, "B", "C")}
		markTiedStandings(comp, sorted, matches)
		assert.Empty(t, tiedNames(sorted))
	})

	t.Run("supplementary TB/DH bouts don't block the all-complete gate", func(t *testing.T) {
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50)}
		matches := []state.MatchResult{
			completedMatch(0, "A", "B"), completedMatch(1, "A", "C"), completedMatch(2, "B", "C"),
			{ID: "Pool A-TB-0", SideA: "A", SideB: "B", Status: state.MatchStatusScheduled},
		}
		markTiedStandings(comp, sorted, matches)
		assert.Equal(t, map[string]bool{"A": true, "B": true}, tiedNames(sorted),
			"a pending TB bout is supplementary and must not gate the highlight")
	})
}

// leagueMatchesAllDoneFor builds a round-robin among `names` where every
// listed competitor's matches are completed. Used to fire the emerging trigger.
func leagueRoundRobin(names []string, completed bool) []state.MatchResult {
	var out []state.MatchResult
	idx := 0
	st := state.MatchStatusScheduled
	if completed {
		st = state.MatchStatusCompleted
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			out = append(out, state.MatchResult{
				ID: fmt.Sprintf("Pool A-%d", idx), SideA: names[i], SideB: names[j], Status: st,
			})
			idx++
		}
	}
	return out
}

// TestMarkTiedStandings_League covers the emerging-tie trigger for league
// format, for both team and individual leagues.
func TestMarkTiedStandings_League(t *testing.T) {
	// Team and individual leagues share the same trigger path; only TeamSize differs.
	for _, tc := range []struct {
		name     string
		teamSize int
	}{
		{"individual league", 0},
		{"team league", 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp := &state.Competition{Format: state.CompFormatLeague, TeamSize: tc.teamSize}

			t.Run("trigger not fired (no top-N competitor finished) → no amber", func(t *testing.T) {
				sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50), tiedStanding("D", 40)}
				// All matches scheduled → nobody has finished their fixtures.
				matches := leagueRoundRobin([]string{"A", "B", "C", "D"}, false)
				markTiedStandings(comp, sorted, matches)
				assert.Empty(t, tiedNames(sorted))
			})

			t.Run("a top-N competitor finished all fights → consequential tie marked", func(t *testing.T) {
				sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50), tiedStanding("D", 40)}
				// A (rank 1, within top-3) has completed every fixture; B still has one pending.
				matches := []state.MatchResult{
					completedMatch(0, "A", "B"), completedMatch(1, "A", "C"), completedMatch(2, "A", "D"),
					scheduledMatch(3, "B", "C"), scheduledMatch(4, "B", "D"), scheduledMatch(5, "C", "D"),
				}
				markTiedStandings(comp, sorted, matches)
				assert.Equal(t, map[string]bool{"A": true, "B": true}, tiedNames(sorted),
					"emerging trigger fires once a top-N competitor is done, even though B isn't")
			})

			t.Run("tie outside the top-N band → not marked", func(t *testing.T) {
				// effectiveTopN defaults to 3. Tie sits at positions 4-5 (MinPosition 4 > 3).
				sorted := []state.PlayerStanding{
					tiedStanding("A", 100), tiedStanding("B", 90), tiedStanding("C", 80),
					tiedStanding("D", 50), tiedStanding("E", 50),
				}
				matches := leagueRoundRobin([]string{"A", "B", "C", "D", "E"}, true)
				markTiedStandings(comp, sorted, matches)
				assert.Empty(t, tiedNames(sorted), "a tie below the top-N band is not consequential")
			})

			t.Run("no tie → nothing marked even after trigger", func(t *testing.T) {
				sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 90), tiedStanding("C", 80)}
				matches := leagueRoundRobin([]string{"A", "B", "C"}, true)
				markTiedStandings(comp, sorted, matches)
				assert.Empty(t, tiedNames(sorted))
			})
		})
	}
}

// TestMarkTiedStandings_TwoThirdPlacesExemption verifies the two-joint-3rd
// exemption: a tie sitting entirely at positions >= 3 is not marked when
// LeagueTwoThirdPlaces is enabled (both teams share bronze, no decider needed).
func TestMarkTiedStandings_TwoThirdPlacesExemption(t *testing.T) {
	matches := leagueRoundRobin([]string{"A", "B", "C", "D"}, true)

	t.Run("exemption on → pure 3rd/4th tie not marked", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague, LeagueTiebreakTopN: 4, LeagueTwoThirdPlaces: true}
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 90), tiedStanding("C", 50), tiedStanding("D", 50)}
		markTiedStandings(comp, sorted, matches)
		assert.Empty(t, tiedNames(sorted), "joint-3rd tie needs no decider when two-third-places is enabled")
	})

	t.Run("exemption off → same 3rd/4th tie IS marked", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague, LeagueTiebreakTopN: 4, LeagueTwoThirdPlaces: false}
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 90), tiedStanding("C", 50), tiedStanding("D", 50)}
		markTiedStandings(comp, sorted, matches)
		assert.Equal(t, map[string]bool{"C": true, "D": true}, tiedNames(sorted))
	})

	t.Run("exemption on but tie straddles 2nd/3rd → still marked", func(t *testing.T) {
		comp := &state.Competition{Format: state.CompFormatLeague, LeagueTiebreakTopN: 3, LeagueTwoThirdPlaces: true}
		// Tie at positions 2-3 (MinPosition 2 < 3): a decider IS needed for 2nd.
		sorted := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 80), tiedStanding("C", 80), tiedStanding("D", 40)}
		markTiedStandings(comp, sorted, matches)
		assert.Equal(t, map[string]bool{"B": true, "C": true}, tiedNames(sorted))
	})
}

// TestMarkTiedStandings_AutoClear confirms that once a tie resolves (Points
// differ), the rows are no longer flagged — the highlight clears on its own.
func TestMarkTiedStandings_AutoClear(t *testing.T) {
	comp := &state.Competition{Format: state.CompFormatMixed}
	matches := []state.MatchResult{completedMatch(0, "A", "B"), completedMatch(1, "A", "C"), completedMatch(2, "B", "C")}

	// Tied first.
	tied := []state.PlayerStanding{tiedStanding("A", 100), tiedStanding("B", 100), tiedStanding("C", 50)}
	markTiedStandings(comp, tied, matches)
	assert.Equal(t, map[string]bool{"A": true, "B": true}, tiedNames(tied))

	// A later result separates A and B → no rows flagged.
	resolved := []state.PlayerStanding{tiedStanding("A", 110), tiedStanding("B", 100), tiedStanding("C", 50)}
	markTiedStandings(comp, resolved, matches)
	assert.Empty(t, tiedNames(resolved), "resolved tie must clear the highlight")
}

// TestMarkTiedStandings_EmptyStandings guards the empty-input path: an empty
// standings slice must be a no-op (no panic, nothing marked), regardless of
// format. League is used here as the non-trivial gating branch.
func TestMarkTiedStandings_EmptyStandings(t *testing.T) {
	comp := &state.Competition{Format: state.CompFormatLeague}
	var sorted []state.PlayerStanding
	markTiedStandings(comp, sorted, nil) // must not panic
	assert.Empty(t, tiedNames(sorted))
}

package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Guard for the load-time restamp (bc-tmfn). Generation and the restamp share
// ONE producer of rounds and match numbers, state.Bracket.StampRoundsFromFeeders
// (each real bout's round is its distance from the final along the Feeders
// computeBracketDisplayMetadata stamps), so on a FRESH bracket a restamp being
// a no-op is true by construction. What that half still catches is a second
// producer creeping back into generation after the shared stamp: anything that
// rewrites a round or a number there (the pow2 provisional rounds this
// replaced, say) makes every load renumber a freshly drawn bracket, and the
// sweep below goes red on it. The Excel parity suites
// (excel_draw_parity_test.go, match_numbering_parity_test.go) and
// bracket_match_numbers_golden_test.go are what prove the shared producer
// matches the printed sheet.
//
// The PLAYED halves are not tautological: they pin that fighting a knockout
// out, and resolving pool finishers into it (ResolveQualifiedPools), never
// rewrite the Feeders or Hidden flags nor empty a side, which is what lets the
// restamp walk a bracket in play exactly as generation walked it.

// assertRestampIsNoOp restamps an independent copy of compID's stored bracket
// and requires it to come back identical, rounds, match numbers and scheduled
// times included.
func assertRestampIsNoOp(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	stored, err := store.LoadBracket(compID)
	require.NoError(t, err)
	restamped, err := store.LoadBracket(compID) // LoadBracket returns a copy
	require.NoError(t, err)

	real := 0
	for _, round := range stored.Rounds {
		for _, m := range round {
			if m.Hidden {
				continue
			}
			real++
			require.Positivef(t, m.DisplayRound, "%s: generation stamps every real bout's round", m.ID)
			require.Positivef(t, m.MatchNumber, "%s: generation numbers every real bout", m.ID)
		}
	}
	require.NotZero(t, real, "the shape must draw at least one real bout")

	changes, err := restamped.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Empty(t, changes, "the restamp moved a round, number or time generation stamped")
	assert.Equal(t, stored, restamped)
}

func TestRestampRoundsFromFeeders_LeavesFreshBracketsUnchanged(t *testing.T) {
	t.Run("knockout", func(t *testing.T) {
		for n := 2; n <= 40; n++ {
			for _, courts := range []int{1, 2, 4} {
				for _, seeds := range []int{0, 4, 8} {
					if seeds > 0 && n <= seeds {
						continue // not a seeded draw: every entrant would be a seed
					}
					t.Run(fmt.Sprintf("%d_entrants_%d_shiaijo_%d_seeds", n, courts, seeds), func(t *testing.T) {
						eng, store, _ := setupTestEngine(t)
						const compID = "restamp-knockout"
						createTestCompetition(t, store, compID, state.CompFormatKnockout, 0, func(c *state.Competition) {
							c.Courts = courtLabels(courts)
						})
						players := makeSeededPlayers(n, seeds)
						require.NoError(t, store.SaveParticipants(compID, makePlayers(n)))
						var assignments []domain.SeedAssignment
						for _, p := range players {
							if p.Seed > 0 {
								assignments = append(assignments, domain.SeedAssignment{Name: p.Name, Dojo: p.Dojo, SeedRank: p.Seed})
							}
						}
						if len(assignments) > 0 {
							require.NoError(t, store.SaveSeeds(compID, assignments))
						}
						require.NoError(t, eng.GenerateDraw(compID))
						assertRestampIsNoOp(t, store, compID)
					})
				}
			}
		}
	})

	// Pool-fed brackets: the court-region draw (buildPoolFedDraw) with
	// pool-origin placeholders, on the pool counts, qualifier counts and
	// shiaijo counts the Excel parity sweep covers (unbalanced pool counts
	// included), plus larger rosters at the default pool settings.
	t.Run("mixed", func(t *testing.T) {
		for _, numPools := range []int{2, 3, 4, 6, 7, 8} {
			for _, winners := range []int{1, 2, 3} {
				for _, courts := range []int{1, 2, 4} {
					t.Run(fmt.Sprintf("%d_pools_%d_qualifiers_%d_shiaijo", numPools, winners, courts), func(t *testing.T) {
						eng, store, _ := setupTestEngine(t)
						const compID = "restamp-mixed"
						createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
							c.PoolSizeMode = "max"
							c.PoolWinners = winners
							c.Courts = courtLabels(courts)
						})
						require.NoError(t, store.SaveParticipants(compID, mixedParityRoster(numPools)))
						require.NoError(t, eng.GenerateDraw(compID))
						pools, err := store.LoadPools(compID)
						require.NoError(t, err)
						require.Lenf(t, pools, numPools, "the case must actually draw %d pools", numPools)
						assertRestampIsNoOp(t, store, compID)
					})
				}
			}
		}
		for _, n := range []int{6, 11, 20, 33, 50, 80} {
			for _, courts := range []int{1, 2, 4} {
				t.Run(fmt.Sprintf("%d_entrants_%d_shiaijo", n, courts), func(t *testing.T) {
					eng, store, _ := setupTestEngine(t)
					const compID = "restamp-mixed-default"
					createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
						c.Courts = courtLabels(courts)
					})
					require.NoError(t, store.SaveParticipants(compID, makePlayers(n)))
					require.NoError(t, eng.GenerateDraw(compID))
					assertRestampIsNoOp(t, store, compID)
				})
			}
		}
	})
}

// A bracket in play walks exactly like a fresh one: results resolve the
// "Winner of" sides into names, but Feeders and Hidden are never rewritten, so
// the restamp still changes nothing once every bout has been fought.
func TestRestampRoundsFromFeeders_LeavesPlayedBracketsUnchanged(t *testing.T) {
	for _, n := range []int{5, 9, 13, 19, 27, 40} {
		for _, courts := range []int{1, 4} {
			t.Run(fmt.Sprintf("%d_entrants_%d_shiaijo", n, courts), func(t *testing.T) {
				eng, store, _ := setupTestEngine(t)
				const compID = "restamp-played"
				createTestCompetition(t, store, compID, state.CompFormatKnockout, 0, func(c *state.Competition) {
					c.Courts = courtLabels(courts)
				})
				require.NoError(t, store.SaveParticipants(compID, makePlayers(n)))
				require.NoError(t, eng.StartCompetition(compID))
				playKnockoutInNumberOrder(t, eng, store, compID)
				assertRestampIsNoOp(t, store, compID)
			})
		}
	}
}

// The pool-fed case a v2.0.0/v2.1.0 upgrade most often meets mid-event: every
// pool finished, the finishers resolved into the knockout in place of the
// "Pool A-1st" placeholders (ResolveQualifiedPools), and then the knockout
// fought out. Neither step may disturb the stored Feeders or Hidden.
func TestRestampRoundsFromFeeders_LeavesPlayedPoolFedBracketsUnchanged(t *testing.T) {
	for _, numPools := range []int{3, 6, 7} {
		for _, winners := range []int{1, 2, 3} {
			for _, courts := range []int{1, 4} {
				t.Run(fmt.Sprintf("%d_pools_%d_qualifiers_%d_shiaijo", numPools, winners, courts), func(t *testing.T) {
					eng, store, _ := setupTestEngine(t)
					const compID = "restamp-played-mixed"
					createTestCompetition(t, store, compID, state.CompFormatMixed, 4, func(c *state.Competition) {
						c.PoolSizeMode = "max"
						c.PoolWinners = winners
						c.Courts = courtLabels(courts)
					})
					require.NoError(t, store.SaveParticipants(compID, mixedParityRoster(numPools)))
					require.NoError(t, eng.StartCompetition(compID))
					completeAllPoolMatches(t, eng, store, compID)

					resolved := loadBracket(t, store, compID)
					for _, round := range resolved.Rounds {
						for _, m := range round {
							require.Falsef(t, helper.IsPoolFinalistPlaceholder(m.SideA) || helper.IsPoolFinalistPlaceholder(m.SideB),
								"%s still waits on a pool (%s v %s): the fixture must resolve every finisher", m.ID, m.SideA, m.SideB)
						}
					}
					assertRestampIsNoOp(t, store, compID)

					playKnockoutInNumberOrder(t, eng, store, compID)
					assertRestampIsNoOp(t, store, compID)
				})
			}
		}
	}
}

// playKnockoutInNumberOrder fights every numbered bout, side A winning. Match
// numbers run deepest round first, so fighting them in number order always
// finds both sides already decided.
func playKnockoutInNumberOrder(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()
	numbered := 0
	for _, round := range loadBracket(t, store, compID).Rounds {
		for _, m := range round {
			if m.MatchNumber > 0 {
				numbered++
			}
		}
	}
	require.NotZero(t, numbered)
	for num := 1; num <= numbered; num++ {
		var m *state.BracketMatch
		b := loadBracket(t, store, compID)
		for ri := range b.Rounds {
			for mi := range b.Rounds[ri] {
				if b.Rounds[ri][mi].MatchNumber == num {
					m = &b.Rounds[ri][mi]
				}
			}
		}
		require.NotNilf(t, m, "Match %d", num)
		require.Falsef(t, strings.HasPrefix(m.SideA, "Winner of") || strings.HasPrefix(m.SideB, "Winner of"),
			"Match %d (%s) is fought before its feeders: %s v %s", num, m.ID, m.SideA, m.SideB)
		scoreBracketMatch(t, eng, store, compID, m.ID, m.SideA)
	}
	final := loadBracket(t, store, compID)
	require.Equal(t, state.MatchStatusCompleted, final.Rounds[len(final.Rounds)-1][0].Status)
}

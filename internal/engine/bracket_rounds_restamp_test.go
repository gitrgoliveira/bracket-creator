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

// Drift guard for the load-time restamp (bc-tmfn). Generation stamps each
// bout's round from the DRAW TREE (computeBracketDisplayMetadata, then
// applySlotDisplayRounds); state.Bracket.RestampRoundsFromFeeders, which
// corrects brackets drawn by v2.0.0 and v2.1.0 on load, recomputes it from the
// STORED FEEDERS as the distance from the final. The two routes must give the
// same rounds, or every load would renumber a freshly drawn bracket away from
// the Excel export. These tests pin that the restamp changes nothing on a
// bracket the current generator wrote, fresh and in play.

// assertRestampIsNoOp restamps an independent copy of compID's stored bracket
// and requires it to come back identical, rounds and match numbers included.
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
	assert.Empty(t, changes, "the stored feeders and the draw tree disagree about a round")
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

package engine

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) generatePools(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment) error {
	// PoolSize is the divisor in CreatePools (and is fed to PoolSeeding); a
	// zero/negative value would otherwise reach helper.CreatePools and the
	// guard there returns a plain error mapped to HTTP 500. Validate up front
	// so a competition started with an unset PoolSize fails as a clean 400
	// (validationErrorf → *ValidationError) with an actionable message. (mp-ebgz)
	if comp.PoolSize <= 0 {
		return validationErrorf("competition %s cannot start: pool size must be at least 1, got %d, set a pool size before starting", comp.ID, comp.PoolSize)
	}

	isMax := comp.PoolSizeMode == "max"

	// numCourts is the modulus for BOTH the seed spread and the pool
	// deinterleave, so it is derived once here. An unset court list means one
	// unnamed court; helper.PoolSeeding and helper.ReorderPoolsForCourts each
	// treat anything below 1 as 1, and normalising up front keeps them and the
	// AssignPoolsToCourts call below reading off a single value.
	numCourts := len(comp.Courts)
	if numCourts == 0 {
		numCourts = 1
	}

	// helper.Player is a type alias for domain.Player (NFR-007); the
	// Excel-coupled helpers accept domain values directly.
	if len(seeds) > 0 {
		if err := helper.ApplySeeds(players, seeds); err != nil {
			return fmt.Errorf("applying seeds: %w", err)
		}
	}

	// Mirrors cmd/create-pools.go exactly (bc-draw Phase 2a). Two things about
	// this call had drifted from the CLI:
	//
	//  1. The second argument is the pool COUNT, not the pool SIZE. Passing
	//     comp.PoolSize put every seed in the wrong pool whenever the two
	//     differ, which is almost always. helper.PoolCount is the same function
	//     CreatePools uses to size its own pool slice, so the two cannot drift.
	//  2. It runs UNCONDITIONALLY, not only when seeds exist. With zero seeds
	//     PoolSeeding still clusters players by dojo so that CreatePools'
	//     round-robin fill lands club-mates in different pools; gating it on
	//     seeds disabled that for every unseeded competition, which is the
	//     common case in the app and never the case in the CLI.
	players = helper.PoolSeeding(players, helper.PoolCount(len(players), comp.PoolSize, isMax), numCourts)

	pools, err := helper.CreatePools(players, comp.PoolSize, isMax)
	if err != nil {
		return err
	}

	// Deinterleave pools into court blocks, at the same point in the sequence
	// the CLI does it (seed, create, reorder). PoolSeeding's placement maths
	// assumes this has run (see the doc comment on helper.PoolSeeding), and
	// without it helper.AssignPoolsToCourts' contiguous blocks piled every
	// oversized pool onto the first court: 26 players at PoolSize 4 in "max"
	// mode on 2 courts gave court A 16 players (pools 0-3, all oversized) and
	// court B 10. Everything below that reads pool order or pool names runs
	// after this call, so they all see the reordered, realphabetised list: the
	// mixed-format validation messages, AssignPlayerNumbers, SavePools, the
	// MatchResult ID prefix and AssignPoolsToCourts.
	pools = helper.ReorderPoolsForCourts(pools, numCourts)

	// A "mixed" competition is "Pools + Knockout" by definition, a single
	// pool collapses to a round-robin with a tacked-on 2-player "final", which
	// is the same shape as `league` and is NOT what an operator picking
	// "mixed" intends. Refuse to start a mixed competition whose participant
	// count + PoolSize would produce fewer than 2 pools, so the operator can
	// either reduce PoolSize, add participants, or switch to `league` format.
	// (league/swiss legitimately produce 1 pool, exempted.)
	if comp.Format == state.CompFormatMixed {
		if len(pools) < 2 {
			return validationErrorf("mixed (Pools + Knockout) competition %s requires at least 2 pools, got %d with %d participants at PoolSize=%d; reduce PoolSize, add participants, or change format to league", comp.ID, len(pools), len(players), comp.PoolSize)
		}
		// Every pool must be able to supply PoolWinners finishers to the knockout.
		// In "max" mode an odd participant count can leave an under-filled last
		// pool (e.g. PoolSize=2 with 3 players → pools of 2 and 1); with the
		// default PoolWinners=2 that 1-player pool could never produce a 2nd-place
		// finisher, and ResolveQualifiedPools would later fail mid-tournament
		// ("only N ranked finishers"). Catch it here so the operator gets an
		// actionable error BEFORE any match is played.
		poolWinners := comp.EffectivePoolWinners()
		for _, p := range pools {
			if len(p.Players) < poolWinners {
				return validationErrorf("mixed (Pools + Knockout) competition %s: pool %q has only %d participant(s) but %d advance to the knockout (PoolWinners=%d), every pool needs at least PoolWinners participants; reduce PoolWinners, adjust PoolSize/pool-size-mode, or add participants", comp.ID, p.PoolName, len(p.Players), poolWinners, poolWinners)
			}
		}
	}

	if comp.NumberPrefix != "" {
		counter := 1
		for i := range pools {
			counter = helper.AssignPlayerNumbers(pools[i].Players, comp.NumberPrefix, counter)
		}
	}

	hasRounds := false
	switch comp.PoolFormat {
	case state.PoolFormatPartial:
		helper.CreatePartialPoolMatches(pools)
		hasRounds = true
	default:
		// PoolFormatFull (default / unset) and any unrecognized value fall
		// through to the legacy code path. RoundRobin remains the inner
		// switch for backward compatibility (FR-052, R9).
		if comp.RoundRobin {
			helper.CreatePoolRoundRobinMatches(pools)
			hasRounds = true
		} else {
			helper.CreatePoolMatches(pools)
		}
	}

	// Save pools
	if err := e.store.SavePools(comp.ID, pools); err != nil {
		return err
	}

	if len(pools) == 1 && numCourts > 1 {
		if err := ValidateCourtCount(len(players), numCourts); err != nil {
			return err
		}
	}

	courtAssign, err := helper.AssignPoolsToCourts(len(pools), numCourts)
	if err != nil {
		return fmt.Errorf("assigning pools to courts: %w", err)
	}

	var results []state.MatchResult
	for pi, p := range pools {
		poolCourts := []string{""}
		if len(comp.Courts) > 0 {
			poolCourts = []string{comp.Courts[courtAssign[pi]]}
			// When there is only one pool (league format) and multiple
			// courts, seed each match with a court so none is left blank.
			// For League this is provisional: the rest-aware scheduler
			// below reassigns courts per slot. It matters for the
			// non-league else branch's round-position spread.
			if len(pools) == 1 && len(comp.Courts) > 1 {
				poolCourts = comp.Courts
			}
		}
		for i, m := range p.Matches {
			round := m.Round
			if !hasRounds {
				round = -1
			}
			results = append(results, state.MatchResult{
				ID:      p.PoolName + "-" + strconv.Itoa(i),
				SideA:   m.SideA.Name,
				SideB:   m.SideB.Name,
				SideAID: m.SideA.ID,
				SideBID: m.SideB.ID,
				Status:  state.MatchStatusScheduled,
				Court:   poolCourts[i%len(poolCourts)],
				Round:   round,
				// ScheduledAt is populated below by
				// assignPoolMatchSlots, uniform start times were
				// retired in T150.
			})
		}
	}

	// Per-court slot assignment (T150) + ceremony-block skipping
	// (T151). Loads the tournament-level tuning (multiplier,
	// opening / lunch blocks) so a missing tournament.md falls back
	// to the function's documented defaults without aborting the
	// pipeline.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return err
	}

	if comp.Format == state.CompFormatLeague {
		// League scheduling (mp-sjaz): spread every player's matches so
		// nobody fights two slots in a row, and keep all courts in a slot
		// strictly time-aligned. This replaces the round-position court
		// assignment + per-court slot cursors used by the other single-pool
		// paths. The runtime simultaneity gate (checkSimultaneousMatch)
		// remains the defense-in-depth backstop at match start.
		// scheduleLeagueSlots treats an empty court list as one unnamed court.
		var slots []int
		results, slots = scheduleLeagueSlots(results, comp.Courts)
		results, _ = assignLeagueSlotTimes(results, slots, comp, tournament)
	} else {
		// For single-pool multi-court, distribute each round's matches across
		// courts so that simultaneous matches never share a participant.
		// When a round has more matches than courts (e.g. 6 players / 2
		// courts → 3 matches per round), the extra match is queued on the
		// same court and runs sequentially; the runtime simultaneity gate
		// (checkSimultaneousMatch) prevents double-booking at match start.
		if len(pools) == 1 && len(comp.Courts) > 1 {
			sort.SliceStable(results, func(i, j int) bool {
				return results[i].Round < results[j].Round
			})
			roundStart := 0
			currentRound := -1
			for i := range results {
				if results[i].Round != currentRound {
					currentRound = results[i].Round
					roundStart = 0
				}
				results[i].Court = comp.Courts[roundStart%len(comp.Courts)]
				roundStart++
			}
		}
		results, _ = assignPoolMatchSlots(results, comp, tournament)
	}

	return e.store.SavePoolMatches(comp.ID, results)
}

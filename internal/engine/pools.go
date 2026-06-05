package engine

import (
	"fmt"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) generatePools(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment) error {
	// helper.Player is a type alias for domain.Player (NFR-007); the
	// Excel-coupled helpers accept domain values directly.
	if len(seeds) > 0 {
		if err := helper.ApplySeeds(players, seeds); err != nil {
			return fmt.Errorf("applying seeds: %w", err)
		}
		players = helper.PoolSeeding(players, comp.PoolSize, len(comp.Courts))
	}

	isMax := comp.PoolSizeMode == "max"
	pools, err := helper.CreatePools(players, comp.PoolSize, isMax)
	if err != nil {
		return err
	}

	// A "mixed" competition is "Pools + Knockout" by definition — a single
	// pool collapses to a round-robin with a tacked-on 2-player "final", which
	// is the same shape as `league` and is NOT what an operator picking
	// "mixed" intends. Refuse to start a mixed competition whose participant
	// count + PoolSize would produce fewer than 2 pools, so the operator can
	// either reduce PoolSize, add participants, or switch to `league` format.
	// (league/swiss legitimately produce 1 pool — exempted.)
	if comp.Format == state.CompFormatMixed && len(pools) < 2 {
		return validationErrorf("mixed (Pools + Knockout) competition %s requires at least 2 pools — got %d with %d participants at PoolSize=%d; reduce PoolSize, add participants, or change format to league", comp.ID, len(pools), len(players), comp.PoolSize)
	}

	if comp.NumberPrefix != "" {
		counter := 1
		for i := range pools {
			counter = helper.AssignPlayerNumbers(pools[i].Players, comp.NumberPrefix, counter)
		}
	}

	switch comp.PoolFormat {
	case state.PoolFormatPartial:
		helper.CreatePartialPoolMatches(pools)
	default:
		// PoolFormatFull (default / unset) and any unrecognized value fall
		// through to the legacy code path. RoundRobin remains the inner
		// switch for backward compatibility (FR-052, R9).
		if comp.RoundRobin {
			helper.CreatePoolRoundRobinMatches(pools)
		} else {
			helper.CreatePoolMatches(pools)
		}
	}

	// Save pools
	if err := e.store.SavePools(comp.ID, pools); err != nil {
		return err
	}

	numCourts := len(comp.Courts)
	if numCourts == 0 {
		numCourts = 1
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
			// courts, spread that pool's matches round-robin across all
			// competition courts so no court sits idle.
			if len(pools) == 1 && len(comp.Courts) > 1 {
				poolCourts = comp.Courts
			}
		}
		for i, m := range p.Matches {
			results = append(results, state.MatchResult{
				ID:     p.PoolName + "-" + strconv.Itoa(i),
				SideA:  m.SideA.Name,
				SideB:  m.SideB.Name,
				Status: state.MatchStatusScheduled,
				Court:  poolCourts[i%len(poolCourts)],
				// ScheduledAt is populated below by
				// assignPoolMatchSlots — uniform start times were
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
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	results, _ = assignPoolMatchSlots(results, comp, tournament)

	return e.store.SavePoolMatches(comp.ID, results)
}

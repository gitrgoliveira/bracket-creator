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
		court := ""
		if len(comp.Courts) > 0 {
			court = comp.Courts[courtAssign[pi]]
		}
		for i, m := range p.Matches {
			results = append(results, state.MatchResult{
				ID:     p.PoolName + "-" + strconv.Itoa(i),
				SideA:  m.SideA.Name,
				SideB:  m.SideB.Name,
				Status: state.MatchStatusScheduled,
				Court:  court,
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

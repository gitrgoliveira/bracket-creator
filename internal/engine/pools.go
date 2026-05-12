package engine

import (
	"fmt"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) generatePools(comp *state.Competition, players []helper.Player, seeds []domain.SeedAssignment) error {
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

	if comp.RoundRobin {
		helper.CreatePoolRoundRobinMatches(pools)
	} else {
		helper.CreatePoolMatches(pools)
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
				ID:          p.PoolName + "-" + strconv.Itoa(i),
				SideA:       m.SideA.Name,
				SideB:       m.SideB.Name,
				Status:      state.MatchStatusScheduled,
				Court:       court,
				ScheduledAt: comp.StartTime,
			})
		}
	}

	return e.store.SavePoolMatches(comp.ID, results)
}

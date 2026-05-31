package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// EstimateScheduleForCompetition returns a pre-draw ScheduleEstimate for the
// competition identified by compID. It is a *Engine method (rather than a
// free function) because it needs the engine's store to load the competition,
// tournament, and participant roster — matching the pattern of other stateful
// engine entry points such as GenerateSchedule and StartCompetition.
// (EstimateForCounts is a free function because it takes pre-derived counts
// and has no store dependency.)
//
// Algorithm:
//  1. Load the competition; return *NotFoundError if it does not exist.
//  2. Load the tournament (nil is tolerated — EstimateForCounts and
//     ApplyTournamentDefaults both handle a nil *Tournament safely).
//  3. Derive the participant count:
//     - Normal competitions: len(LoadParticipants(compID)).
//     - Source-linked playoffs (comp.SourceCompID != "" and comp has an
//     empty roster): derive from the SOURCE competition's pool count ×
//     comp.PoolWinners.  If the source has no pools yet (draw not
//     generated), returns a zero ScheduleEstimate without an error —
//     the estimate is genuinely unknown at that stage.
//  4. Map comp.Format → helper.EstimateMatchCountsInput and call
//     helper.EstimateMatchCounts to obtain pool and playoff counts.
//  5. Delegate to EstimateForCounts(poolCount, playoffCount, comp, tournament).
//
// Format mapping (state constant → helper format string):
//
//	state.CompFormatPlayoffs ("playoffs") → "playoffs"
//	state.CompFormatMixed    ("mixed")    → "mixed"
//	state.CompFormatLeague   ("league")   → "league"
//	state.CompFormatSwiss    ("swiss")    → "swiss"
//
// They are the same strings — the constants are defined in state/models.go and
// the helper uses the same literal values, so no translation is needed.
func (e *Engine) EstimateScheduleForCompetition(compID string) (ScheduleEstimate, error) {
	// Step 1: load competition.
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if comp == nil {
		return ScheduleEstimate{}, notFoundErrorf("competition %s not found", compID)
	}

	// Step 2: load tournament (nil is safe — EstimateForCounts handles it).
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return ScheduleEstimate{}, err
	}

	// Step 3: derive participant count.
	playerCount, err := e.estimateParticipantCount(comp)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if playerCount == 0 {
		// No participants yet (or source pools not generated) — return a zero
		// estimate rather than an error; the caller can display "—" or "unknown".
		return EstimateForCounts(0, 0, comp, tournament), nil
	}

	// Step 4: derive match counts via the Phase 1 helper.
	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2 // mirrors engine/ranking.go:169 default
	}
	in := helper.EstimateMatchCountsInput{
		Format:       comp.Format,
		PlayerCount:  playerCount,
		PoolSize:     comp.PoolSize,
		PoolSizeMode: comp.PoolSizeMode,
		PoolWinners:  poolWinners,
		RoundRobin:   comp.RoundRobin,
		PoolFormat:   comp.PoolFormat,
		SwissRounds:  comp.SwissRounds,
	}
	poolCount, playoffCount, err := helper.EstimateMatchCounts(in)
	if err != nil {
		return ScheduleEstimate{}, err
	}

	// Step 5: delegate to EstimateForCounts.
	return EstimateForCounts(poolCount, playoffCount, comp, tournament), nil
}

// estimateParticipantCount returns the number of participants for a
// competition, handling the source-linked playoffs case.
//
// For source-linked playoffs (comp.SourceCompID != "" and roster is empty),
// the participant count is derived from the SOURCE competition's pool count ×
// comp.PoolWinners. This mirrors resolvePoolWinners in ranking.go, which uses
// len(pools) × winnersPerPool as the authoritative finalist count (not
// recomputed from participant count + pool size, because CreatePools may
// choose a different pool count in "min" vs "max" mode).
//
// If the source competition's pools have not been generated yet (len(pools)==0),
// returns (0, nil) — the caller treats this as "not enough data to estimate"
// and returns a zero ScheduleEstimate.
func (e *Engine) estimateParticipantCount(comp *state.Competition) (int, error) {
	// Source-linked playoffs: roster on disk is empty until StartCompetition.
	// Detect by checking SourceCompID and a zero-length roster (mirrors the
	// guard in runDrawPipeline: only auto-resolve when roster is empty).
	if comp.Format == state.CompFormatPlayoffs && comp.SourceCompID != "" {
		players, err := e.store.LoadParticipants(comp.ID, comp.WithZekkenName)
		if err != nil {
			return 0, err
		}
		if len(players) == 0 {
			// Roster not yet populated — derive from source pools.
			return e.estimateFinalistCount(comp.SourceCompID, comp.PoolWinners)
		}
		// Roster already populated (competition already started) — use it.
		return len(players), nil
	}

	// Normal path: load roster from disk.
	players, err := e.store.LoadParticipants(comp.ID, comp.WithZekkenName)
	if err != nil {
		return 0, err
	}
	return len(players), nil
}

// estimateFinalistCount returns the number of pool winners that will advance
// from a source competition (identified by srcCompID) to a linked playoffs
// competition. It reads the SOURCE competition's pools.csv to get the
// authoritative pool count, then multiplies by the effective winners-per-pool.
//
// Returns (0, nil) when the source has no pools yet.
func (e *Engine) estimateFinalistCount(srcCompID string, poolWinners int) (int, error) {
	pools, err := e.store.LoadPools(srcCompID)
	if err != nil {
		return 0, err
	}
	if len(pools) == 0 {
		// Source draw not generated yet — can't estimate.
		return 0, nil
	}
	winnersPerPool := poolWinners
	if winnersPerPool <= 0 {
		winnersPerPool = 2 // default mirrors resolvePoolWinners / ranking.go:169
	}
	return len(pools) * winnersPerPool, nil
}

package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// EstimateScheduleForCompetition returns a pre-draw ScheduleEstimate for the
// competition identified by compID. It loads the competition and participant
// roster, derives pool + playoff match counts via helper.EstimateMatchCounts,
// and delegates to EstimateForCounts.
func (e *Engine) EstimateScheduleForCompetition(compID string) (ScheduleEstimate, error) {
	// Step 1: load competition.
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if comp == nil {
		return ScheduleEstimate{}, notFoundErrorf("competition %s not found", compID)
	}

	// Step 2: load tournament (nil is safe, EstimateForCounts handles it).
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
		// No participants yet (or source pools not generated), return a zero
		// estimate rather than an error; the caller can display a dash or "unknown".
		return EstimateForCounts(0, 0, comp, tournament), nil
	}

	// Step 4: derive match counts via the Phase 1 helper.
	poolWinners := comp.EffectivePoolWinners()
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
		// EstimateMatchCounts errors are config-caused (unknown format, zero
		// pool size, player count < pool size), surface as ValidationError
		// so the HTTP handler returns 400, not 500.
		return ScheduleEstimate{}, validationErrorf("%s", err)
	}

	// Step 5: delegate to EstimateForCounts.
	return EstimateForCounts(poolCount, playoffCount, comp, tournament), nil
}

// estimateParticipantCount returns the number of participants for a
// competition, applying the check-in filter when enabled (mirroring
// runDrawPipeline's filterCheckedIn opt-in semantics).
func (e *Engine) estimateParticipantCount(comp *state.Competition) (int, error) {
	players, err := e.store.LoadParticipants(comp.ID, comp.WithZekkenName)
	if err != nil {
		return 0, err
	}
	if comp.CheckInEnabled {
		players = filterCheckedIn(players)
	}
	return len(players), nil
}

package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxEstimateParticipants is the largest entry count an estimate is asked
// for: the cap on a competition's ExpectedParticipants.
const MaxEstimateParticipants = 1024

// Where the count on ScheduleEstimate.Participants came from.
const (
	// ParticipantsSourceRoster: the roster (checked in, when check-in applies).
	ParticipantsSourceRoster = "roster"
	// ParticipantsSourceExpected: the organiser's expected count.
	ParticipantsSourceExpected = "expected"
)

// EstimateScheduleForCompetition returns a pre-draw ScheduleEstimate for the
// competition identified by compID. It loads the competition and participant
// roster, takes the entry count from estimateParticipantCount, derives pool +
// knockout match counts via helper.EstimateMatchCounts, and delegates to
// EstimateForCounts. The answer carries the count it used and its source
// (Participants, ParticipantsSource) whenever that count is above 0.
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
	playerCount, source, err := e.estimateParticipantCount(comp, tournament)
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
	poolCount, knockoutCount, err := helper.EstimateMatchCounts(in)
	if err != nil {
		// EstimateMatchCounts errors are config-caused (unknown format, zero
		// pool size, player count < pool size), surface as ValidationError
		// so the HTTP handler returns 400, not 500.
		return ScheduleEstimate{}, validationErrorf("%s", err)
	}

	// Step 5: delegate to EstimateForCounts.
	est := EstimateForCounts(poolCount, knockoutCount, comp, tournament)
	est.Participants = playerCount
	est.ParticipantsSource = source
	return est, nil
}

// estimateParticipantCount returns the entry count a competition's estimate
// uses and where it came from. The counted roster is the participants file,
// filtered to those checked in when check-in applies (mirroring
// runDrawPipeline's filterCheckedIn opt-in semantics). While the competition
// is in setup, countForEstimate weighs the organiser's expected count against
// it; from draw ready on the draw's entrants are the count and the source is
// the roster, whatever the expected number. A team competition's rows are
// teams, so the same rule counts teams.
func (e *Engine) estimateParticipantCount(comp *state.Competition, tournament *state.Tournament) (int, string, error) {
	players, err := e.store.LoadParticipants(comp.ID, comp.EffectiveWithZekkenName())
	if err != nil {
		return 0, "", err
	}
	if comp.CheckInApplies(tournament) {
		players = filterCheckedIn(players)
	}
	counted := len(players)
	if comp.Status != state.CompStatusSetup && comp.Status != "" {
		return counted, ParticipantsSourceRoster, nil
	}
	n, source := countForEstimate(comp.ExpectedParticipants, counted)
	return n, source, nil
}

// countForEstimate is the one rule for the entry count a setup competition's
// estimate uses: the larger of the organiser's expected count and the counted
// roster, so the estimate does not collapse while registrations (or check-ins)
// trickle in, and the real count once more than expected have come. The
// source is ParticipantsSourceExpected only while the expected count is the
// larger; an equal count is the roster's.
func countForEstimate(expected, counted int) (int, string) {
	if expected > counted {
		return expected, ParticipantsSourceExpected
	}
	return counted, ParticipantsSourceRoster
}

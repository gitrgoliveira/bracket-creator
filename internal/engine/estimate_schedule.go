package engine

import (
	"errors"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// DraftBreak describes a scheduled pause window during the competition day.
// Start and End are formatted as "HH:MM".
type DraftBreak struct {
	Name  string `json:"name"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// DraftTiming holds the timing configuration applied to a draft schedule estimate.
type DraftTiming struct {
	Multiplier   float64      `json:"multiplier"`
	AllowancePct int          `json:"allowancePct"`
	Breaks       []DraftBreak `json:"breaks"`
}

// DraftEstimate holds the schedule estimate for a draft competition along with
// match counts and timing parameters. ScheduleEstimate is embedded so its JSON
// fields are inlined.
type DraftEstimate struct {
	ScheduleEstimate
	PoolMatches     int         `json:"poolMatches"`
	KnockoutMatches int         `json:"knockoutMatches"`
	Timing          DraftTiming `json:"timing"`
}

// EstimateScheduleForCompetition returns a pre-draw ScheduleEstimate for the
// competition identified by compID. It loads the competition and participant
// roster, takes the entry count from estimateParticipantCount, delegates to
// estimateForPlayers, and records the source. The answer carries the count it
// used and its source (Participants, ParticipantsSource) whenever that count
// is above 0.
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

	// Step 3: derive participant count and counted roster seed ranks.
	playerCount, source, seedRanks, err := e.estimateParticipantCount(comp, tournament)
	if err != nil {
		return ScheduleEstimate{}, err
	}

	est, _, _, err := estimateForPlayers(comp, tournament, playerCount, seedRanks, false)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if playerCount > 0 {
		est.ParticipantsSource = source
	}
	return est, nil
}

// EstimateDraft computes a schedule estimate for an unstored draft competition
// with the given participant count and tournament configuration.
func (e *Engine) EstimateDraft(comp *state.Competition, tournament *state.Tournament, participants int) (DraftEstimate, error) {
	if comp == nil {
		return DraftEstimate{}, validationErrorf("competition is required")
	}
	if participants < 1 || participants > MaxEstimateParticipants {
		return DraftEstimate{}, validationErrorf("Participants must be a whole number from 1 to %d.", MaxEstimateParticipants)
	}

	est, poolMatches, knockoutMatches, err := estimateForPlayers(comp, tournament, participants, nil, true)
	if err != nil {
		return DraftEstimate{}, err
	}

	return DraftEstimate{
		ScheduleEstimate: est,
		PoolMatches:      poolMatches,
		KnockoutMatches:  knockoutMatches,
		Timing: DraftTiming{
			Multiplier:   tournamentMultiplier(tournament),
			AllowancePct: tournamentAllowancePct(tournament),
			Breaks:       draftBreaks(tournament, comp.Date),
		},
	}, nil
}

// estimateForPlayers derives match counts and a ScheduleEstimate for n players.
func estimateForPlayers(comp *state.Competition, tournament *state.Tournament, n int, seedRanks []int, draft bool) (est ScheduleEstimate, poolMatches, knockoutMatches int, err error) {
	if n == 0 {
		return EstimateForCounts(0, 0, comp, tournament), 0, 0, nil
	}

	poolWinners := comp.EffectivePoolWinners()
	in := helper.EstimateMatchCountsInput{
		Format:           comp.Format,
		PlayerCount:      n,
		PoolSize:         comp.PoolSize,
		PoolSizeMode:     comp.PoolSizeMode,
		PoolWinners:      poolWinners,
		RoundRobin:       comp.RoundRobin,
		PoolFormat:       comp.PoolFormat,
		SwissRounds:      comp.SwissRounds,
		ExtraQualifiers:  comp.ExtraQualifiers,
		SeedRanks:        seedRanks,
		SingleThirdPlace: comp.RequiresSingleThirdPlace(),
		Draft:            draft,
	}
	poolCount, knockoutCount, err := helper.EstimateMatchCounts(in)
	if err != nil {
		// EstimateMatchCounts errors are config-caused (unknown format, zero
		// pool size, player count < pool size), surface as ValidationError
		// so the HTTP handler returns 400, not 500, while preserving the cause
		// for errors.As / errors.Is detection.
		return ScheduleEstimate{}, 0, 0, wrapValidationErrorf(err, "%s", err)
	}

	est = EstimateForCounts(poolCount, knockoutCount, comp, tournament)
	est.Participants = n
	return est, poolCount, knockoutCount, nil
}

// tournamentMultiplier returns tournament.ClockToElapsedMultiplier when set
// and positive, or 1.5 (the default).
func tournamentMultiplier(tournament *state.Tournament) float64 {
	if tournament != nil && tournament.ClockToElapsedMultiplier > 0 {
		return tournament.ClockToElapsedMultiplier
	}
	return 1.5
}

// tournamentAllowancePct mirrors tournamentMultiplier for the slowest-court allowance: with no tournament configured the estimate still applies the default buffer.
func tournamentAllowancePct(tournament *state.Tournament) int {
	var t state.Tournament
	if tournament != nil {
		t = *tournament
	}
	state.ApplyTournamentDefaults(&t)
	return t.SlowestCourtBufferPct
}

// draftBreaks returns the list of draft breaks for the given date, preserving
// pause names while delegating day and duration filtering to dayBreaks.
func draftBreaks(tournament *state.Tournament, date string) []DraftBreak {
	if tournament == nil || len(tournament.Pauses) == 0 {
		return []DraftBreak{}
	}
	out := make([]DraftBreak, 0, len(tournament.Pauses))
	for _, p := range tournament.Pauses {
		tCopy := *tournament
		tCopy.Pauses = []state.Pause{p}
		windows := dayBreaks(&tCopy, date)
		if len(windows) == 1 {
			w := windows[0]
			out = append(out, DraftBreak{
				Name:  p.Name,
				Start: w.start.Format(scheduleClockLayout),
				End:   w.end.Format(scheduleClockLayout),
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Start != out[j].Start {
			return out[i].Start < out[j].Start
		}
		return out[i].End < out[j].End
	})
	return out
}

// estimateSeedRanks returns the seed ranks of the counted roster for a mixed
// competition. Other formats do not use seed ranks for match count estimation,
// so seeds are not loaded for them.
func (e *Engine) estimateSeedRanks(comp *state.Competition, players []domain.Player) ([]int, error) {
	if comp.EffectiveFormat() != state.CompFormatMixed {
		return nil, nil
	}
	seeds, err := e.store.LoadSeeds(comp.ID)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidSeedAssignments) {
			return nil, wrapValidationErrorf(err, "%s", e.seedingProblem(comp.ID, err))
		}
		return nil, err
	}

	idx := domain.NewRosterIndex(players)
	var seedRanks []int
	for _, s := range seeds {
		if p, ok := idx.LookupSeed(s); ok && p != nil {
			seedRanks = append(seedRanks, s.SeedRank)
		}
	}
	return seedRanks, nil
}

// estimateParticipantCount returns the entry count a competition's estimate
// uses, where it came from, and the seed ranks of the counted roster (for a mixed
// competition only). The counted roster is the participants file, filtered to those
// checked in when check-in applies (mirroring runDrawPipeline's filterCheckedIn
// opt-in semantics). While the competition is in setup, countForEstimate weighs the
// organiser's expected count against it; from draw ready on the draw's entrants are
// the count and the source is the roster, whatever the expected number. A team
// competition's rows are teams, so the same rule counts teams.
func (e *Engine) estimateParticipantCount(comp *state.Competition, tournament *state.Tournament) (int, string, []int, error) {
	players, err := e.store.LoadParticipants(comp.ID, comp.EffectiveWithZekkenName())
	if err != nil {
		return 0, "", nil, err
	}
	if comp.CheckInApplies(tournament) {
		players = filterCheckedIn(players)
	}
	counted := len(players)

	seedRanks, err := e.estimateSeedRanks(comp, players)
	if err != nil {
		return 0, "", nil, err
	}

	if comp.Status != state.CompStatusSetup && comp.Status != "" {
		return counted, ParticipantsSourceRoster, seedRanks, nil
	}
	n, source := countForEstimate(comp.ExpectedParticipants, counted)
	return n, source, seedRanks, nil
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

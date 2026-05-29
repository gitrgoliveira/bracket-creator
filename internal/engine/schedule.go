package engine

import (
	"fmt"
	"math"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxCourts is the hard upper bound on the `courts` parameter accepted
// by EstimateSchedule. Mirrors the CLI's A–Z (26) cap (CLAUDE.md) and
// is also enforced by the handler so a hostile query-string cannot
// trigger an excessive allocation (CodeQL go/uncontrolled-allocation-size).
const MaxCourts = 26

// ScheduleEstimate is the wire response for GET /api/schedule/estimate
// and the return type of EstimateSchedule. All durations are in minutes,
// rounded to the nearest integer.
//
// PerCourtMinutes has length == in.NumCourts (>=1 after clamping) and
// each entry is the estimated elapsed minutes that one court runs match
// play (excluding ceremonies). TotalDurationMinutes is the slowest court
// plus CeremonyMinutes — i.e. the earliest the operator can expect the
// final medal ceremony to begin.
//
// data-model §5/§6.
type ScheduleEstimate struct {
	TotalDurationMinutes int   `json:"totalDurationMinutes"`
	PerCourtMinutes      []int `json:"perCourtMinutes"`
	CeremonyMinutes      int   `json:"ceremonyMinutes"`
}

// EstimateInput holds the parameters EstimateSchedule consumes. All
// fields are required except SlowestCourtBufferPct (no buffer when 0)
// and CeremonyMinutes (no ceremony when 0). TeamSize=0 selects the
// individual-match branch; TeamSize>0 with BoutsPerTeamMatch>0 selects
// the team-match branch and scales per-match duration by bouts plus an
// inter-bout transition allowance (~1 minute per switch).
//
// FR-055, FR-058: the multiplier converts on-clock minutes to elapsed
// minutes; team-match duration scales linearly with bouts plus a
// per-switch transition.
type EstimateInput struct {
	MatchDurationClockMinutes float64
	Multiplier                float64
	NumMatches                int
	NumCourts                 int
	TeamSize                  int
	BoutsPerTeamMatch         int
	SlowestCourtBufferPct     int
	CeremonyMinutes           int
}

// perMatchElapsed returns the un-rounded elapsed minutes for a single
// match given the on-clock duration, the multiplier, and the number of
// bouts (0 = individual match; >0 = team match with that many bouts).
//
// Formula (FR-055, FR-058):
//
//	bouts == 0: clockMin * multiplier
//	bouts > 0:  bouts * clockMin * multiplier + (bouts-1) * 1
//	            (the +1 per switch covers rotation/transition between bouts)
//
// This is the single source of truth shared by EstimateSchedule and
// perMatchElapsedMinutes (scheduler_slots.go). Both callers delegate
// here — satisfying the FR-059 "MUST agree" constraint without manual
// synchronisation.
func perMatchElapsed(clockMin, multiplier float64, bouts int) float64 {
	if bouts > 0 {
		return float64(bouts)*clockMin*multiplier + float64(bouts-1)*1.0
	}
	return clockMin * multiplier
}

// EstimateSchedule computes the total elapsed-minute estimate for a
// match set given clock duration, multiplier, court count, optional
// team-match bout count, slowest-court buffer %, and ceremony block.
//
// Algorithm:
//  1. perMatchMin = perMatchElapsed(clockMin, multiplier, bouts)
//  2. totalMin = perMatchMin * numMatches
//  3. perCourt = totalMin / numCourts  (clamped numCourts >= 1)
//  4. perCourt *= (1 + buffer/100)
//  5. total = round(perCourt) + ceremonyMinutes
//
// FR-055, FR-057, FR-058, FR-059, data-model §5.
//
// Breaks (OpeningBlock, LunchBlock, ClosingBlock) are NOT modelled
// here because EstimateInput carries only raw scalars — the stateless
// handler has no competition/tournament context. Use EstimateForCounts
// when per-comp, break-aware estimation is needed.
func EstimateSchedule(in EstimateInput) ScheduleEstimate {
	// Per-match elapsed minutes via the shared pure core.
	bouts := 0
	if in.TeamSize > 0 && in.BoutsPerTeamMatch > 0 {
		bouts = in.BoutsPerTeamMatch
	}
	perMatchMin := perMatchElapsed(in.MatchDurationClockMinutes, in.Multiplier, bouts)

	// Total clock time across all matches, distributed evenly across
	// courts. Courts is clamped to [1, MaxCourts] so a malformed or
	// hostile input cannot trigger a giant slice allocation downstream
	// (CodeQL go/uncontrolled-allocation-size) nor divide by zero.
	// MaxCourts matches the CLI's A–Z hard cap (CLAUDE.md, FR limit).
	totalMin := perMatchMin * float64(in.NumMatches)
	courts := in.NumCourts
	if courts < 1 {
		courts = 1
	}
	if courts > MaxCourts {
		courts = MaxCourts
	}
	perCourt := totalMin / float64(courts)

	// Slowest-court buffer (10–15% typical). Skipped when 0.
	if in.SlowestCourtBufferPct > 0 {
		perCourt *= 1.0 + float64(in.SlowestCourtBufferPct)/100.0
	}

	perCourtInt := int(math.Round(perCourt))
	perCourtList := make([]int, courts)
	for i := range perCourtList {
		perCourtList[i] = perCourtInt
	}

	total := perCourtInt + in.CeremonyMinutes
	return ScheduleEstimate{
		TotalDurationMinutes: total,
		PerCourtMinutes:      perCourtList,
		CeremonyMinutes:      in.CeremonyMinutes,
	}
}

// EstimateForCounts returns a ScheduleEstimate for a pre-draw competition
// (no generated matches yet) given the expected number of pool matches and
// playoff matches. It uses the slot-model primitives (perMatchElapsedMinutes,
// skipCeremonyBlocks) so it stays in exact agreement with the post-draw path.
//
// Unit reconciliation: the slot model advances clock times (time.Time), while
// ScheduleEstimate.TotalDurationMinutes is a duration in minutes. This function
// defines TotalDurationMinutes = round(maxCourtCursor − dayStart), where dayStart
// is comp.StartTime (the same anchor the slot assigners use). Each court's cursor
// is initialised to dayStart+OpeningBlock, so the returned duration INCLUDES the
// opening-block offset. PerCourtMinutes entries are each court's individual
// duration.
//
// Buffer divergence (intentional): EstimateForCounts applies
// tournament.SlowestCourtBufferPct because it is a predictive, pre-draw estimate
// — the slowest court will likely run over the mean. The post-draw slot assigners
// (assignPoolMatchSlots / assignBracketMatchSlots) do NOT apply the buffer because
// a real, assigned schedule needs no extra padding. Do NOT assert cross-regime
// equality for buffered inputs.
//
// CeremonyMinutes is populated from tournament.ClosingBlock. The OpeningBlock is
// applied as a pre-loop per-court start offset (cursor initialised to
// dayStart+OpeningBlock); the LunchBlock is applied per match via the shared
// skipCeremonyBlocks helper. ClosingBlock is not entered by the cursor — it is
// surfaced only as CeremonyMinutes.
//
// Phase sequencing (intentional, and a SECOND divergence from the post-draw
// path): each court runs its pool matches and THEN its playoff matches on the
// same advancing cursor — pools-then-playoffs, which is the realistic order
// (playoff seeding needs pool results). The post-draw slot assigners
// (assignPoolMatchSlots / assignBracketMatchSlots) are invoked as two separate
// calls that EACH re-anchor to dayStart+OpeningBlock, so they OVERLAP the two
// phases in clock time. A post-draw estimate must therefore SEQUENCE (sum) the
// two assigner cursors rather than max() them to match EstimateForCounts for a
// mixed-format competition. See mp-zoh.
//
// Returns a zero ScheduleEstimate when comp is nil or has no courts.
func EstimateForCounts(poolCount, playoffCount int, comp *state.Competition, tournament *state.Tournament) ScheduleEstimate {
	if comp == nil {
		return ScheduleEstimate{}
	}

	courts := comp.Courts
	numCourts := len(courts)
	if numCourts == 0 {
		return ScheduleEstimate{}
	}

	// Work on shallow copies so the caller's structs are not mutated by
	// ApplyTournamentDefaults / ApplyCompetitionDefaults. The slot
	// assigners are called via the engine and always have defaults applied
	// before they run; we mirror that here without the side-effect.
	compCopy := *comp
	comp = &compCopy
	var tournCopy state.Tournament
	if tournament != nil {
		tournCopy = *tournament
	}
	tournament = &tournCopy
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)

	// Common ceremony parameters (same as the slot assigners).
	// tournament is always non-nil here (copies from caller or zero-value tournCopy).
	dayStart := parseClockHHMM(comp.StartTime)
	openingMin := parseDurationMinutes(tournament.OpeningBlock)
	lunchMin := parseDurationMinutes(tournament.LunchBlock)
	lunchStart := parseClockHHMM(defaultLunchStartClock)

	// Phase durations via the shared slot-model helper.
	poolPerMatch := perMatchElapsedMinutes(comp, tournament, false /*isPlayoff*/)
	playoffPerMatch := perMatchElapsedMinutes(comp, tournament, true /*isPlayoff*/)

	// Distribute pool matches evenly across courts, then advance each
	// court's cursor by poolPerMatch per match (with lunch skipping).
	// We use integer division; the remainder matches are spread across
	// the first courts, mirroring the round-robin distribution that
	// assignPoolMatchSlots uses in practice.
	courtCursor := make([]time.Time, numCourts)
	for i := range courtCursor {
		courtCursor[i] = dayStart.Add(time.Duration(openingMin) * time.Minute)
	}

	// --- Pool phase ---
	base := poolCount / numCourts
	rem := poolCount % numCourts
	for ci := range courtCursor {
		n := base
		if ci < rem {
			n++
		}
		for range n {
			courtCursor[ci] = skipCeremonyBlocks(courtCursor[ci], lunchStart, lunchMin)
			courtCursor[ci] = courtCursor[ci].Add(time.Duration(poolPerMatch) * time.Minute)
		}
	}

	// --- Playoff phase ---
	base = playoffCount / numCourts
	rem = playoffCount % numCourts
	for ci := range courtCursor {
		n := base
		if ci < rem {
			n++
		}
		for range n {
			courtCursor[ci] = skipCeremonyBlocks(courtCursor[ci], lunchStart, lunchMin)
			courtCursor[ci] = courtCursor[ci].Add(time.Duration(playoffPerMatch) * time.Minute)
		}
	}

	// Convert clock times back to durations from dayStart.
	// tournament is always non-nil here (see copy above).
	bufferMultiplier := 1.0
	if tournament.SlowestCourtBufferPct > 0 {
		bufferMultiplier = 1.0 + float64(tournament.SlowestCourtBufferPct)/100.0
	}

	perCourtList := make([]int, numCourts)
	var maxDuration float64
	for ci, cur := range courtCursor {
		raw := cur.Sub(dayStart).Minutes()
		buffered := raw * bufferMultiplier
		perCourtList[ci] = int(math.Round(buffered))
		if buffered > maxDuration {
			maxDuration = buffered
		}
	}

	ceremonyMin := parseDurationMinutes(tournament.ClosingBlock)

	total := int(math.Round(maxDuration)) + ceremonyMin
	return ScheduleEstimate{
		TotalDurationMinutes: total,
		PerCourtMinutes:      perCourtList,
		CeremonyMinutes:      ceremonyMin,
	}
}

func (e *Engine) GenerateSchedule(compID string) error {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", compID)
	}

	var entries []state.ScheduleEntry

	if comp.Format == state.CompFormatMixed || comp.Format == state.CompFormatLeague || comp.Format == state.CompFormatSwiss {
		matches, err := e.store.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			entries = append(entries, state.ScheduleEntry{
				MatchType:   "pool",
				MatchRef:    m.ID,
				Court:       m.Court,
				ScheduledAt: m.ScheduledAt,
				Status:      string(m.Status),
			})
		}
	} else {
		bracket, err := e.store.LoadBracket(compID)
		if err != nil {
			return err
		}
		if bracket != nil {
			for rIdx, round := range bracket.Rounds {
				for _, m := range round {
					court := m.Court
					if court == "" {
						court = "A" // Default court
					}
					entries = append(entries, state.ScheduleEntry{
						MatchType:   "bracket",
						MatchRef:    fmt.Sprintf("R%d-M%s", rIdx+1, m.ID),
						Court:       court,
						ScheduledAt: m.ScheduledAt,
						Status:      string(m.Status),
					})
				}
			}
		}
	}

	return e.store.SaveSchedule(compID, entries)
}

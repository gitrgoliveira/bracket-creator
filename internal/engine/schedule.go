package engine

import (
	"fmt"
	"math"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
	MatchDurationClockMinutes int
	Multiplier                float64
	NumMatches                int
	NumCourts                 int
	TeamSize                  int
	BoutsPerTeamMatch         int
	SlowestCourtBufferPct     int
	CeremonyMinutes           int
}

// EstimateSchedule computes the total elapsed-minute estimate for a
// match set given clock duration, multiplier, court count, optional
// team-match bout count, slowest-court buffer %, and ceremony block.
//
// Algorithm:
//  1. perMatchMin = clockMin * multiplier (individual)
//     or            bouts * clockMin * multiplier + (bouts-1) * 1
//     (team — the +1 per switch covers rotation/transition between bouts).
//  2. totalMin = perMatchMin * numMatches
//  3. perCourt = totalMin / numCourts  (clamped numCourts >= 1)
//  4. perCourt *= (1 + buffer/100)
//  5. total = round(perCourt) + ceremonyMinutes
//
// FR-055, FR-057, FR-058, FR-059, data-model §5.
//
// TODO(T150, T151): wire this into Engine.GenerateSchedule so each
// scheduled match's expectedDuration comes from EstimateSchedule and
// the slot assigner skips ceremony blocks. Tracked separately from
// this slice — the calculator + endpoint are sufficient for the
// estimator surface; the auto-scheduler integration is deferred so a
// regression in slot assignment doesn't take down the whole estimator.
func EstimateSchedule(in EstimateInput) ScheduleEstimate {
	// Per-match elapsed minutes.
	perMatchMin := float64(in.MatchDurationClockMinutes) * in.Multiplier
	if in.TeamSize > 0 && in.BoutsPerTeamMatch > 0 {
		perMatchMin = float64(in.BoutsPerTeamMatch) * float64(in.MatchDurationClockMinutes) * in.Multiplier
		// Inter-bout transition: ~1 minute per switch between bouts.
		perMatchMin += float64(in.BoutsPerTeamMatch-1) * 1.0
	}

	// Total clock time across all matches, distributed evenly across
	// courts. Courts < 1 is clamped to 1 so a malformed input doesn't
	// divide-by-zero or yield a negative per-court estimate.
	totalMin := perMatchMin * float64(in.NumMatches)
	courts := in.NumCourts
	if courts < 1 {
		courts = 1
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

func (e *Engine) GenerateSchedule(compID string) error {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", compID)
	}

	var entries []state.ScheduleEntry

	if comp.Format == state.CompFormatPools {
		matches, err := e.store.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			entries = append(entries, state.ScheduleEntry{
				MatchType: "pool",
				MatchRef:  m.ID,
				Court:     m.Court,
				Status:    string(m.Status),
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
						MatchType: "bracket",
						MatchRef:  fmt.Sprintf("R%d-M%s", rIdx+1, m.ID),
						Court:     court,
						Status:    string(m.Status),
					})
				}
			}
		}
	}

	return e.store.SaveSchedule(compID, entries)
}

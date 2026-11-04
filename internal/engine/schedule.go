package engine

import (
	"errors"
	"log"
	"math"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxCourts is the hard upper bound on the `courts` parameter accepted by
// EstimateSchedule. It IS helper.MaxCourts rather than a second copy of the
// number: this endpoint estimates a schedule for a tournament the rest of the
// system has to be able to run, so a count it accepts but no tournament can
// hold would be a planning answer nobody could act on. Also enforced by the
// handler so a hostile query-string cannot trigger an excessive allocation
// (CodeQL go/uncontrolled-allocation-size).
const MaxCourts = helper.MaxCourts

// MaxTeamSize is a defensive upper bound on the `teamSize` /
// `boutsPerTeamMatch` parameters. It is NOT a domain rule — kachinuki team
// sizes are unregulated, and standard kendo teams are 3/5/7 — but a public,
// stateless endpoint must not let an unbounded value drive the bout math:
// `worst = 2n-1` and `bouts * clock * multiplier` overflow int64 for a large
// n, and the final int(math.Round(...)) conversion of an out-of-range float is
// implementation-defined in Go, yielding a negative duration or an inverted
// best/worst range (mp-gmcg review). 100 is ~14× the largest real team, so no
// legitimate planning number is rejected. Clamped in EstimateSchedule (all
// callers) and 400'd by the handler (matching the MaxCourts precedent). The
// stateless workbook generator (POST /create, cmd/create_handler.go) 400s a
// team size past it too, since that size sizes every team block's bout rows.
const MaxTeamSize = 100

// MaxScheduleCount and MaxSchedulePct are the same class of defensive bound for
// the OTHER scalars that scale the estimate — numMatches and ceremonyMinutes
// (counts/minutes) and the slowest-court buffer (a percentage). Without them a
// hostile query-string overflows the same duration math MaxTeamSize guards
// (int(math.Round(perCourt)) or the ceremony addition) on this public endpoint
// (mp-gmcg review). Both are far beyond any real tournament — a million matches
// or minutes, a 100 000 % buffer — while keeping the arithmetic exact.
const (
	MaxScheduleCount = 1_000_000
	MaxSchedulePct   = 100_000
)

// maxEstimateMinutes is the ceiling clamped onto per-court minutes right before
// the float→int conversion. It closes the overflow class the int clamps cannot:
// matchDuration and multiplier are floats (the handler only checks them
// positive-finite), so a hostile matchDuration=1e19 drives perCourt past int64
// and int(math.Round(...)) yields the implementation-defined min-int (mp-gmcg
// review). 1e15 is far beyond any real tournament, exactly representable in
// float64 (< 2^53), and leaves ~4 orders of magnitude below int64 max for the
// ceremony addition — so no input combination can produce a negative duration.
//
// The min() keeps the clamp correct on a 32-bit int too, where a bare 1e15 would
// overflow and re-trigger the very conversion this ceiling exists to prevent:
// min/max over constant operands is itself a constant expression (Go >= 1.21), so
// this stays a compile-time const, and float64(math.MaxInt/2) folds to a value
// far above 1e15 on every 64-bit platform (~4.6e18) — bit-identical to 1e15
// today, and automatically correct if the package's current 32-bit compile break
// (tiebreaker.go's 100_000_000_000 overflows a 32-bit int; .goreleaser.yaml ships
// no 32-bit target) is ever fixed.
const maxEstimateMinutes = min(1e15, float64(math.MaxInt/2))

// ScheduleEstimate is the wire response for GET /api/schedule/estimate
// and the return type of EstimateSchedule. All durations are in minutes,
// rounded to the nearest integer.
//
// PerCourtMinutes has length == in.NumCourts (>=1 after clamping) and
// each entry is the estimated elapsed minutes that one court runs match
// play (excluding ceremonies). TotalDurationMinutes is the slowest court
// plus CeremonyMinutes, i.e. the earliest the operator can expect the
// final medal ceremony to begin.
//
// NOTE the "excluding ceremonies" wording above describes the EstimateSchedule
// producer (raw scalars, no break model). The other producer, EstimateForCounts,
// fills PerCourtMinutes from a per-court clock cursor that INCLUDES any
// pause dead-time from the day's pauses, see its doc comment.
//
// Best/worst/average (mp-gmcg): kachinuki team matches have a variable
// bout count, so the estimate is a RANGE. With nominal team size N
// (sizes are unregulated; N is the configured planning number):
//
//	best  = N bouts per match  (one fighter sweeps the opposing team)
//	worst = 2N-1 bouts         (every bout retires exactly one player)
//	avg   = (3N-1)/2 bouts     (midpoint)
//
// TotalDurationMinutes is the AVERAGE scenario (the headline number);
// BestCaseMinutes / WorstCaseMinutes bracket it. For individual and
// fixed-format team matches the bout count is constant, so all three
// fields carry the same value. PerCourtMinutes reflects the average
// scenario.
//
// data-model §5/§6.
type ScheduleEstimate struct {
	TotalDurationMinutes int   `json:"totalDurationMinutes"`
	BestCaseMinutes      int   `json:"bestCaseMinutes"`
	WorstCaseMinutes     int   `json:"worstCaseMinutes"`
	PerCourtMinutes      []int `json:"perCourtMinutes"`
	CeremonyMinutes      int   `json:"ceremonyMinutes"`
	// CourtEnds maps each court to the planned end ("HH:MM") of this competition's
	// last slot there; only set on the per-competition estimate of a drawn competition.
	CourtEnds map[string]string `json:"courtEnds,omitempty"`
	// Participants is the entry count the per-competition estimate used and
	// ParticipantsSource where it came from (ParticipantsSourceRoster or
	// ParticipantsSourceExpected); both are left empty by EstimateForCounts and
	// when the count is 0.
	Participants       int    `json:"participants,omitempty"`
	ParticipantsSource string `json:"participantsSource,omitempty"`
}

// EstimateInput holds the parameters EstimateSchedule consumes. All
// fields are required except SlowestCourtBufferPct (no buffer when 0),
// CeremonyMinutes (no ceremony when 0) and BoutsPerTeamMatch (defaults
// to TeamSize). TeamSize=0 selects the individual-match branch;
// TeamSize>0 selects the team-match branch and scales per-match duration
// by bouts plus an inter-bout transition allowance (~1 minute per switch).
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
	// BoutsPerTeamMatch OVERRIDES the bout count for a team match. Leave 0
	// for the normal case: a team match is worth TeamSize bouts, which is
	// the rule every other producer runs on (perMatchElapsedMinutes,
	// EstimateForCounts) and the only one an actual generated schedule can
	// reproduce. Set it only to price a hypothetical the scheduler will not
	// lay out.
	BoutsPerTeamMatch     int
	SlowestCourtBufferPct int
	CeremonyMinutes       int
}

// perMatchElapsedBouts returns the un-rounded elapsed minutes for a single
// match given the on-clock duration, the multiplier, and the number of
// bouts (0 = individual match; >0 = team match with that many bouts).
//
// Formula (FR-055, FR-058):
//
//	bouts == 0: clockMin * multiplier
//	bouts > 0:  bouts * clockMin * multiplier + (bouts-1) * 1
//	            (the +1 per switch covers rotation/transition between bouts)
//
// bouts is a float because the kachinuki AVERAGE scenario has a fractional
// bout count ((3N-1)/2 for odd expressions), which must not be truncated
// before the duration math (mp-gmcg).
//
// This is the single source of truth shared by EstimateSchedule and
// perMatchElapsedMinutes (scheduler_slots.go). Both callers delegate
// here, satisfying the FR-059 "MUST agree" constraint without manual
// synchronisation.
func perMatchElapsedBouts(clockMin, multiplier, bouts float64) float64 {
	if bouts > 0 {
		return bouts*clockMin*multiplier + (bouts-1)*1.0
	}
	return clockMin * multiplier
}

// kachinukiBoutRange returns the (best, average, worst) bout counts for
// a kachinuki team match with nominal team size n (mp-gmcg):
//
//	best  = n     one fighter sweeps the whole opposing team
//	worst = 2n-1  every bout retires exactly one player
//	avg   = (best+worst)/2, the midpoint planning number
//
// Team sizes are unregulated, so n is the configured planning number,
// not a guarantee; the range brackets realistic outcomes rather than
// bounding them absolutely.
func kachinukiBoutRange(n int) (best, avg, worst float64) {
	if n <= 0 {
		return 0, 0, 0
	}
	best = float64(n)
	worst = float64(domain.KachinukiMaxBouts(n))
	avg = (best + worst) / 2
	return best, avg, worst
}

// clampNonNeg returns v bounded to [0, max]: a negative value becomes 0 and a
// value above max is capped. Used to keep the schedule bout drivers in a range
// the duration arithmetic cannot overflow (see MaxTeamSize).
func clampNonNeg(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}

// EstimateSchedule computes the total elapsed-minute estimate for a
// match set given clock duration, multiplier, court count, optional
// team-match bout count, slowest-court buffer %, and ceremony block.
//
// Algorithm:
//  1. perMatchMin = perMatchElapsedBouts(clockMin, multiplier, bouts)
//  2. totalMin = perMatchMin * numMatches
//  3. perCourt = totalMin / numCourts  (clamped numCourts >= 1)
//  4. perCourt *= (1 + buffer/100)
//  5. total = round(perCourt) + ceremonyMinutes
//
// FR-055, FR-057, FR-058, FR-059, data-model §5.
//
// Pauses (pauses configured on the tournament) are NOT modelled
// here because EstimateInput carries only raw scalars, the stateless
// handler has no competition/tournament context. Use EstimateForCounts
// when per-comp, pause-aware estimation is needed.
func EstimateSchedule(in EstimateInput) ScheduleEstimate {
	// Per-match elapsed minutes via the shared pure core. The stateless
	// estimator keeps a constant bout count for all scenarios (the three
	// range fields collapse to one value).
	//
	// A team match is worth TeamSize bouts unless the caller overrides it.
	// That default lives HERE rather than in each caller because it is the
	// rule the rest of the system already runs on: perMatchElapsedMinutes
	// (scheduler_slots.go) derives `bouts = comp.TeamSize` with no caller
	// input, and it is the function the REAL schedule is laid out with.
	// Requiring the caller to restate it made "team match" and "how long a
	// team match takes" two separate things a caller had to get right
	// together, and getting it wrong was silent: TeamSize>0 with the bouts
	// field omitted fell through to the INDIVIDUAL formula and answered a
	// team question with a one-bout number, no error.
	// Clamp the bout drivers to [0, MaxTeamSize] BEFORE the arithmetic so a
	// hostile query-string cannot overflow the duration math (mp-gmcg review):
	// the handler already 400s these, but clamping here protects every other
	// caller and keeps the int(math.Round(...)) conversion in range. See
	// MaxTeamSize.
	teamSize := clampNonNeg(in.TeamSize, MaxTeamSize)
	boutsOverride := clampNonNeg(in.BoutsPerTeamMatch, MaxTeamSize)
	bouts := 0
	if teamSize > 0 {
		bouts = teamSize
		if boutsOverride > 0 {
			bouts = boutsOverride
		}
	}
	bestBouts, avgBouts, worstBouts := float64(bouts), float64(bouts), float64(bouts)

	// Courts is clamped to [1, MaxCourts] so a malformed or hostile
	// input cannot trigger a giant slice allocation downstream (CodeQL
	// go/uncontrolled-allocation-size) nor divide by zero. MaxCourts
	// matches the CLI's hard court cap (helper.MaxCourts).
	courts := in.NumCourts
	if courts < 1 {
		courts = 1
	}
	if courts > MaxCourts {
		courts = MaxCourts
	}

	// numMatches, the buffer %, and the ceremony block scale the same duration
	// math as the bout count, so they get the same defensive clamp — otherwise
	// a hostile numMatches/ceremonyMinutes overflows int(math.Round(...)) / the
	// ceremony addition, and a hostile buffer blows up the multiply (mp-gmcg
	// review). See MaxScheduleCount / MaxSchedulePct.
	numMatches := clampNonNeg(in.NumMatches, MaxScheduleCount)
	bufferPct := clampNonNeg(in.SlowestCourtBufferPct, MaxSchedulePct)
	ceremonyMinutes := clampNonNeg(in.CeremonyMinutes, MaxScheduleCount)

	// One scenario = total clock time across all matches, distributed
	// evenly across courts, with the slowest-court buffer applied.
	perCourtFor := func(boutsF float64) int {
		perMatchMin := perMatchElapsedBouts(in.MatchDurationClockMinutes, in.Multiplier, boutsF)
		perCourt := perMatchMin * float64(numMatches) / float64(courts)
		// Slowest-court buffer (10–15% typical). Skipped when 0.
		if bufferPct > 0 {
			perCourt *= 1.0 + float64(bufferPct)/100.0
		}
		// Clamp to [0, maxEstimateMinutes] before the int conversion so a hostile
		// float input (matchDuration/multiplier, unbounded by the int clamps) or
		// a NaN can't produce a negative min-int. NaN fails every comparison, so
		// test it explicitly. See maxEstimateMinutes.
		if math.IsNaN(perCourt) || perCourt > maxEstimateMinutes {
			perCourt = maxEstimateMinutes
		} else if perCourt < 0 {
			perCourt = 0
		}
		return int(math.Round(perCourt))
	}

	avgPerCourt := perCourtFor(avgBouts)
	perCourtList := make([]int, courts)
	for i := range perCourtList {
		perCourtList[i] = avgPerCourt
	}

	// perCourtFor is a pure closure over `in` and `courts`. In this stateless
	// path (bestBouts == avgBouts == worstBouts) it returns bit-identically
	// avgPerCourt for all scenarios.
	bestPerCourt, worstPerCourt := perCourtFor(bestBouts), perCourtFor(worstBouts)

	return ScheduleEstimate{
		TotalDurationMinutes: avgPerCourt + ceremonyMinutes,
		BestCaseMinutes:      bestPerCourt + ceremonyMinutes,
		WorstCaseMinutes:     worstPerCourt + ceremonyMinutes,
		PerCourtMinutes:      perCourtList,
		CeremonyMinutes:      ceremonyMinutes,
	}
}

// estimateCourtCount returns the effective court count for comp: len(comp.Courts),
// defaulting to 1 when empty, clamped to MaxCourts.
func estimateCourtCount(comp *state.Competition) int {
	if comp == nil {
		return 1
	}
	numCourts := len(comp.Courts)
	if numCourts == 0 {
		// Empty courts list defaults to a single court, consistent with the
		// assigners (pools.go / bracket.go) and EstimateSchedule's NumCourts
		// clamp. Returning zero here would under-estimate a competition that
		// relies on that 1-court default.
		numCourts = 1
	}
	if numCourts > MaxCourts {
		// Clamp to the court cap, the same defensive bound EstimateSchedule
		// applies. A malformed/hostile Competition with an oversized Courts
		// slice would otherwise drive large per-court allocations
		// (courtCursor/matchMin/perCourtList), CodeQL go/uncontrolled-allocation-size.
		numCourts = MaxCourts
	}
	return numCourts
}

// courtShare returns court ci's share of count matches across numCourts.
// It is the estimate's intentional even spread, remainder to the first courts,
// and the post-draw assigners do no spread of their own because their matches
// already carry a court.
func courtShare(count, numCourts, ci int) int {
	if numCourts <= 0 || count <= 0 || ci < 0 || ci >= numCourts {
		return 0
	}
	share := count / numCourts
	if ci < count%numCourts {
		share++
	}
	return share
}

// normalizedEstimateInputs returns defensive shallow copies of comp and tournament
// with tournament defaults, competition defaults, and the team-size default applied.
func normalizedEstimateInputs(comp *state.Competition, tournament *state.Tournament) (*state.Competition, *state.Tournament) {
	// EstimateForCounts is the one engine entry point that normalizes its own
	// inputs, and deliberately so: it is exported and takes raw structs rather
	// than loading them, so it has no store guarantee to lean on. A caller
	// handing it a Tournament with SlowestCourtBufferPct unset genuinely needs
	// the 10% default filled in (0 means "unset" for that field, not "no
	// buffer"). Everywhere else in this package the values came from
	// Store.LoadCompetition / LoadTournament, both of which normalize on the way
	// out, so re-applying defaults there was dead weight and has been removed.
	//
	// Shallow-copy first so normalizing does not mutate the caller's structs.
	var compCopy state.Competition
	if comp != nil {
		compCopy = *comp
	}
	normComp := &compCopy
	var tournCopy state.Tournament
	if tournament != nil {
		tournCopy = *tournament
	}
	normTourn := &tournCopy
	state.ApplyTournamentDefaults(normTourn)
	state.ApplyCompetitionDefaults(normComp)
	// Team-size default, same guard as competition.go (StartCompetition) and
	// swiss.go: a team competition with an omitted TeamSize uses the FIK
	// 5-person default. Without this, perMatchElapsedMinutes would see
	// TeamSize==0 and fall through to the individual-match formula,
	// under-estimating a team competition's duration.
	if normComp.IsTeam() && normComp.TeamSize == 0 {
		normComp.TeamSize = 5
	}
	return normComp, normTourn
}

// estimateScenarios returns best, avg, worst for kachinuki with the avg the headline,
// else the one nominal scenario. Inputs must already be normalized.
func estimateScenarios(comp *state.Competition, tournament *state.Tournament) []schedScenario {
	if comp.IsKachinuki() {
		bestBouts, avgBouts, worstBouts := kachinukiBoutRange(comp.TeamSize)
		// Order: best, avg, worst. Each scenario's per-match minutes come from
		// kachinukiBoutRange's own bout counts, NOT from the nominal walk: the
		// two are equal today only because perMatchElapsedMinutes uses
		// bouts = TeamSize, and leaning on that coincidence meant retuning the
		// nominal rule could silently stop BestCaseMinutes being the best case
		// (or render Best > Average) while avg and worst stayed right.
		return []schedScenario{
			{perMatchElapsedMinutesBouts(comp, tournament, false, bestBouts), perMatchElapsedMinutesBouts(comp, tournament, true, bestBouts)},
			{perMatchElapsedMinutesBouts(comp, tournament, false, avgBouts), perMatchElapsedMinutesBouts(comp, tournament, true, avgBouts)},
			{perMatchElapsedMinutesBouts(comp, tournament, false, worstBouts), perMatchElapsedMinutesBouts(comp, tournament, true, worstBouts)},
		}
	}
	return []schedScenario{{
		perMatchElapsedMinutes(comp, tournament, false /*isKnockout*/),
		perMatchElapsedMinutes(comp, tournament, true /*isKnockout*/),
	}}
}

// EstimateForCounts returns a ScheduleEstimate for a pre-draw competition
// (no generated matches yet) given the expected number of pool matches and
// knockout matches. It reuses the slot-model primitives (perMatchElapsedMinutes,
// skipBreaks) so per-match and in-day pause math match the post-draw path, but the
// aggregate intentionally diverges in two documented ways (the buffer and
// phase-sequencing notes below), so it is NOT a byte-for-byte equal.
//
// Unit reconciliation: the slot model advances clock times (time.Time), while
// ScheduleEstimate.TotalDurationMinutes is a duration in minutes. This function
// defines TotalDurationMinutes = round(maxCourtCursor - dayStart), where dayStart
// is comp.StartTime (the same anchor the slot assigners use). Each court's cursor
// is initialised to dayStart. PerCourtMinutes entries are each court's individual
// elapsed duration, and, NOTE, unlike the ScheduleEstimate struct docstring
// ("match play (excluding ceremonies)", which describes the EstimateSchedule
// producer), these entries INCLUDE any pause dead-time. They are NOT match-time-only.
// (Only the slowest-court buffer is applied to match time alone; see below.)
//
// Buffer divergence (intentional): EstimateForCounts applies
// tournament.SlowestCourtBufferPct because it is a predictive, pre-draw estimate,
// the slowest court will likely run over the mean. The buffer is applied to
// MATCH time only (matchMin), not to pause dead-time, which has no runtime
// variance to pad, matching EstimateSchedule's semantics. The post-draw slot
// assigners (assignPoolMatchSlotsFrom / assignBracketMatchSlots) do NOT apply
// the buffer at all because a real, assigned schedule needs no extra padding.
// So do NOT assert RAW cross-regime equality, but the buffered relationship
// (EstimateForCounts ≈ slot duration × (1 + buffer/100)) IS valid, and is exactly what
// TestEstimateForCountsVsSlotAssigner_Balanced checks.
//
// CeremonyMinutes is 0 on this path; pauses are applied per match via the shared
// skipBreaks helper.
//
// Phase sequencing: each court runs its pool matches and THEN its knockout
// matches on the same advancing cursor, pools-then-knockout, which is the
// realistic order (knockout seeding needs pool results). The post-draw path
// sequences the phases per court the same way: the knockout slot assigner
// (assignBracketMatchSlots) starts each court's cursor at the end of that
// court's stored pool schedule (poolPhaseEndByCourt, bc-kosc), while a
// knockout-only competition keeps the day start. A post-draw estimate must
// therefore SEQUENCE the two phases rather than max() them.
//
// Negative counts are clamped to 0, the helper is exported and likely fed
// derived/user inputs (mp-zoh), and a negative count would otherwise make
// matchMin negative and yield a nonsensical (even negative) duration.
//
// Returns a zero ScheduleEstimate only when comp is nil; an empty courts list
// defaults to a single court (matching the assigners and EstimateSchedule).
func EstimateForCounts(poolCount, knockoutCount int, comp *state.Competition, tournament *state.Tournament) ScheduleEstimate {
	if comp == nil {
		return ScheduleEstimate{}
	}
	if poolCount < 0 {
		poolCount = 0
	}
	if knockoutCount < 0 {
		knockoutCount = 0
	}

	numCourts := estimateCourtCount(comp)
	comp, tournament = normalizedEstimateInputs(comp, tournament)

	// Common pause parameters (same as the slot assigners).
	// tournament is always non-nil here (copies from caller or zero-value tournCopy).
	dayStart, breaks := parseCeremonyParams(comp, tournament)

	// Convert clock times back to durations from dayStart.
	// tournament is always non-nil here (see copy above).
	bufferPct := 0
	if tournament.SlowestCourtBufferPct > 0 {
		bufferPct = tournament.SlowestCourtBufferPct
	}

	// walk runs the per-court cursor simulation for one or more per-match
	// scenarios via replayCourt, returning each scenario's (perCourtList,
	// slowest-court buffered duration), positionally.
	walk := func(scenarios []schedScenario) ([][]int, []float64) {
		ns := len(scenarios)
		perCourtLists := make([][]int, ns)
		maxDurations := make([]float64, ns)
		for s := range scenarios {
			perCourtLists[s] = make([]int, numCourts)
		}

		for ci := 0; ci < numCourts; ci++ {
			nPool := courtShare(poolCount, numCourts, ci)
			nKO := courtShare(knockoutCount, numCourts, ci)
			for s := range scenarios {
				runs := []ReplayRun{
					{Count: nPool, Minutes: scenarios[s].poolPerMatch},
					{Count: nKO, Minutes: scenarios[s].knockoutPerMatch},
				}
				_, buffered := replayCourt(dayStart, runs, breaks, bufferPct)
				perCourtLists[s][ci] = int(math.Round(buffered))
				if buffered > maxDurations[s] {
					maxDurations[s] = buffered
				}
			}
		}
		return perCourtLists, maxDurations
	}

	scenarios := estimateScenarios(comp, tournament)
	perCourts, maxes := walk(scenarios)

	// Kachinuki (mp-gmcg): the bout count is variable, so price three
	// scenarios from estimateScenarios. The headline TotalDurationMinutes / PerCourtMinutes is
	// the AVERAGE; best and worst bracket it.
	// IsKachinuki requires TeamSize >= 2 (review: this used to accept
	// TeamSize > 0, so a TeamSize == 1 competition priced a fake best/avg/worst
	// range here while every engine kachinuki function refused it as
	// non-kachinuki — a single fighter per side has no "winner stays on" to
	// range-price).
	if comp.IsKachinuki() {
		return ScheduleEstimate{
			BestCaseMinutes:      int(math.Round(maxes[0])),
			TotalDurationMinutes: int(math.Round(maxes[1])), // the AVERAGE is the headline
			WorstCaseMinutes:     int(math.Round(maxes[2])),
			PerCourtMinutes:      perCourts[1], // avg
			CeremonyMinutes:      0,
		}
	}

	total := int(math.Round(maxes[0]))
	return ScheduleEstimate{
		TotalDurationMinutes: total,
		BestCaseMinutes:      total,
		WorstCaseMinutes:     total,
		PerCourtMinutes:      perCourts[0],
		CeremonyMinutes:      0,
	}
}

// ReplayRun describes a run of count matches of duration minutes each.
type ReplayRun struct {
	Count   int `json:"count"`
	Minutes int `json:"minutes"`
}

// replayCourt simulates a single court's match execution starting at start,
// advancing past any breakWindow in breaks via skipBreaks at the start of each slot,
// and buffering the match time only by bufferPct (slowest court buffer).
// An unbuffered fixed overhead (pause dead-time) is added back without multiplier.
// Returns the final time and the total duration in buffered minutes.
func replayCourt(start time.Time, runs []ReplayRun, breaks []breakWindow, bufferPct int) (time.Time, float64) {
	cur := start
	var matchMin float64
	for _, r := range runs {
		if r.Count <= 0 {
			continue
		}
		for i := 0; i < r.Count; i++ {
			cur = skipBreaks(cur, breaks)
			cur = cur.Add(time.Duration(r.Minutes) * time.Minute)
		}
		matchMin += float64(r.Count * r.Minutes)
	}
	raw := cur.Sub(start).Minutes()
	fixedOverhead := raw - matchMin
	multiplier := 1.0
	if bufferPct > 0 {
		multiplier += float64(bufferPct) / 100.0
	}
	buffered := fixedOverhead + matchMin*multiplier
	end := start.Add(time.Duration(math.Round(buffered)) * time.Minute)
	return end, buffered
}

// schedScenario is one per-match timing (pool + knockout minutes) that
// EstimateForCounts's walk prices. Kachinuki passes three (best/avg/worst);
// every other format passes one.
type schedScenario struct{ poolPerMatch, knockoutPerMatch int }

func slotMinutesFor(tournament *state.Tournament) state.SlotMinutes {
	return func(c *state.Competition, ko bool) int {
		// A team competition with an omitted TeamSize uses the FIK 5-person
		// default, as EstimateForCounts does.
		if c != nil && c.IsTeam() && c.TeamSize == 0 {
			compCopy := *c
			compCopy.TeamSize = 5
			c = &compCopy
		}
		return perMatchElapsedMinutes(c, tournament, ko)
	}
}

// courtFloorFor resolves the per-court earliest start times for comp based on
// other same-day competitions sharing its courts. A store read error is logged
// and floors nothing, since the floor is advisory; a court another competition
// holds until midnight or later is returned as state.ErrCourtFloorPastMidnight,
// which refuses the draw (no "HH:MM" slot is left on that court that day).
func (e *Engine) courtFloorFor(comp *state.Competition, tournament *state.Tournament) (map[string]time.Time, error) {
	if comp == nil {
		return nil, nil
	}
	courts := InheritedDrawCourts(comp.Courts, tournament)
	floors, err := e.store.CourtFloor(comp, courts, slotMinutesFor(tournament))
	if errors.Is(err, state.ErrCourtFloorPastMidnight) {
		return nil, err
	}
	if err != nil {
		log.Printf("courtFloorFor %s: %v", comp.ID, err)
		return nil, nil
	}
	if len(floors) == 0 {
		return nil, nil
	}
	out := make(map[string]time.Time, len(floors))
	for court, floorStr := range floors {
		t := parseClockHHMM(floorStr)
		if !t.IsZero() {
			out[court] = t
		}
	}
	return out, nil
}

// CourtEnds returns the planned end ("HH:MM") of compID's last slot on each court
// it holds matches on.
func (e *Engine) CourtEnds(compID string) (map[string]string, error) {
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	return e.store.CourtEnds(compID, slotMinutesFor(tournament))
}

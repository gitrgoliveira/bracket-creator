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

// MaxTeamSize is a defensive upper bound on the `teamSize` /
// `boutsPerTeamMatch` parameters. It is NOT a domain rule — kachinuki team
// sizes are unregulated, and standard kendo teams are 3/5/7 — but a public,
// stateless endpoint must not let an unbounded value drive the bout math:
// `worst = 2n-1` and `bouts * clock * multiplier` overflow int64 for a large
// n, and the final int(math.Round(...)) conversion of an out-of-range float is
// implementation-defined in Go, yielding a negative duration or an inverted
// best/worst range (mp-gmcg review). 100 is ~14× the largest real team, so no
// legitimate planning number is rejected. Clamped in EstimateSchedule (all
// callers) and 400'd by the handler (matching the MaxCourts precedent).
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
// fills PerCourtMinutes from a per-court clock cursor that INCLUDES the
// OpeningBlock offset and any LunchBlock dead-time, see its doc comment.
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
	// Kachinuki widens the estimate into a best/average/worst range
	// (mp-gmcg): the resolved bout count is the nominal team size N,
	// best = N bouts, worst = 2N-1, average = midpoint. False (fixed
	// format or individual) keeps a constant bout count, so the three
	// range fields collapse to one value.
	Kachinuki bool
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
	worst = float64(2*n - 1)
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
// Breaks (OpeningBlock, LunchBlock, ClosingBlock) are NOT modelled
// here because EstimateInput carries only raw scalars, the stateless
// handler has no competition/tournament context. Use EstimateForCounts
// when per-comp, break-aware estimation is needed.
func EstimateSchedule(in EstimateInput) ScheduleEstimate {
	// Per-match elapsed minutes via the shared pure core. Kachinuki
	// (mp-gmcg) widens the constant bout count into a best/avg/worst
	// range; fixed/individual keeps one value for all three scenarios.
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
	if in.Kachinuki && bouts > 0 {
		bestBouts, avgBouts, worstBouts = kachinukiBoutRange(bouts)
	}

	// Courts is clamped to [1, MaxCourts] so a malformed or hostile
	// input cannot trigger a giant slice allocation downstream (CodeQL
	// go/uncontrolled-allocation-size) nor divide by zero. MaxCourts
	// matches the CLI's A–Z hard cap (CLAUDE.md, FR limit).
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

	// perCourtFor is a pure closure over `in` and `courts`, so for a
	// non-kachinuki request (bestBouts == avgBouts == worstBouts) it returns
	// bit-identically avgPerCourt — no guard needed to "avoid recomputation",
	// and an unconditional call is structurally unable to disagree about which
	// requests have a range (mp-gmcg review: R10 replaced one restated condition
	// with two derived ones; zero is available and carries no drift hazard).
	bestPerCourt, worstPerCourt := perCourtFor(bestBouts), perCourtFor(worstBouts)

	return ScheduleEstimate{
		TotalDurationMinutes: avgPerCourt + ceremonyMinutes,
		BestCaseMinutes:      bestPerCourt + ceremonyMinutes,
		WorstCaseMinutes:     worstPerCourt + ceremonyMinutes,
		PerCourtMinutes:      perCourtList,
		CeremonyMinutes:      ceremonyMinutes,
	}
}

// EstimateForCounts returns a ScheduleEstimate for a pre-draw competition
// (no generated matches yet) given the expected number of pool matches and
// playoff matches. It reuses the slot-model primitives (perMatchElapsedMinutes,
// skipCeremonyBlocks) so per-match and in-day break (OpeningBlock/LunchBlock)
// math match the post-draw path, but the aggregate intentionally diverges in
// two documented ways (the buffer and phase-sequencing notes below), so it is
// NOT a byte-for-byte equal. (ClosingBlock is handled differently, surfaced as
// CeremonyMinutes here, ignored by the assigners; see below.)
//
// Unit reconciliation: the slot model advances clock times (time.Time), while
// ScheduleEstimate.TotalDurationMinutes is a duration in minutes. This function
// defines TotalDurationMinutes = round(maxCourtCursor - dayStart), where dayStart
// is comp.StartTime (the same anchor the slot assigners use). Each court's cursor
// is initialised to dayStart+OpeningBlock, so the returned duration INCLUDES the
// opening-block offset. PerCourtMinutes entries are each court's individual
// elapsed duration, and, NOTE, unlike the ScheduleEstimate struct docstring
// ("match play (excluding ceremonies)", which describes the EstimateSchedule
// producer), these entries INCLUDE the OpeningBlock offset and any LunchBlock
// dead-time. They are NOT match-time-only. (Only the slowest-court buffer is
// applied to match time alone; see below.)
//
// Buffer divergence (intentional): EstimateForCounts applies
// tournament.SlowestCourtBufferPct because it is a predictive, pre-draw estimate,
// the slowest court will likely run over the mean. The buffer is applied to
// MATCH time only (matchMin), not to the fixed OpeningBlock offset or LunchBlock
// dead-time, which have no runtime variance to pad, matching EstimateSchedule's
// semantics. The post-draw slot assigners (assignPoolMatchSlots /
// assignBracketMatchSlots) do NOT apply the buffer at all because a real,
// assigned schedule needs no extra padding. So do NOT assert RAW cross-regime
// equality, but the buffered relationship (EstimateForCounts ≈ slot duration ×
// (1 + buffer/100)) IS valid, and is exactly what
// TestEstimateForCountsVsSlotAssigner_Balanced checks.
//
// CeremonyMinutes is populated from tournament.ClosingBlock. The OpeningBlock is
// applied as a pre-loop per-court start offset (cursor initialised to
// dayStart+OpeningBlock); the LunchBlock is applied per match via the shared
// skipCeremonyBlocks helper. ClosingBlock is not entered by the cursor, it is
// surfaced only as CeremonyMinutes.
//
// Phase sequencing (intentional, and a SECOND divergence from the post-draw
// path): each court runs its pool matches and THEN its playoff matches on the
// same advancing cursor, pools-then-playoffs, which is the realistic order
// (playoff seeding needs pool results). The post-draw slot assigners
// (assignPoolMatchSlots / assignBracketMatchSlots) are invoked as two separate
// calls that EACH re-anchor to dayStart+OpeningBlock, so they OVERLAP the two
// phases in clock time. A post-draw estimate must therefore SEQUENCE the two
// phases rather than max() them, but it must add the OpeningBlock offset ONCE,
// not once per phase. Summing the raw cursor durations would double-count
// OpeningBlock (each cursor = dayStart + OpeningBlock + match-time); instead sum
// the per-phase match durations and add OpeningBlock a single time. See mp-zoh.
//
// Negative counts are clamped to 0, the helper is exported and likely fed
// derived/user inputs (mp-zoh), and a negative count would otherwise make
// matchMin negative and yield a nonsensical (even negative) duration.
//
// Returns a zero ScheduleEstimate only when comp is nil; an empty courts list
// defaults to a single court (matching the assigners and EstimateSchedule).
func EstimateForCounts(poolCount, playoffCount int, comp *state.Competition, tournament *state.Tournament) ScheduleEstimate {
	if comp == nil {
		return ScheduleEstimate{}
	}
	if poolCount < 0 {
		poolCount = 0
	}
	if playoffCount < 0 {
		playoffCount = 0
	}

	courts := comp.Courts
	numCourts := len(courts)
	if numCourts == 0 {
		// Empty courts list defaults to a single court, consistent with the
		// assigners (pools.go / bracket.go) and EstimateSchedule's NumCourts
		// clamp. Returning zero here would under-estimate a competition that
		// relies on that 1-court default.
		numCourts = 1
	}
	if numCourts > MaxCourts {
		// Clamp to the A–Z (26) cap, the same defensive bound EstimateSchedule
		// applies. A malformed/hostile Competition with an oversized Courts
		// slice would otherwise drive large per-court allocations
		// (courtCursor/matchMin/perCourtList), CodeQL go/uncontrolled-allocation-size.
		numCourts = MaxCourts
	}

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
	compCopy := *comp
	comp = &compCopy
	var tournCopy state.Tournament
	if tournament != nil {
		tournCopy = *tournament
	}
	tournament = &tournCopy
	state.ApplyTournamentDefaults(tournament)
	state.ApplyCompetitionDefaults(comp)
	// Team-size default, same guard as competition.go (StartCompetition) and
	// swiss.go: a team competition with an omitted TeamSize uses the FIK
	// 5-person default. Without this, perMatchElapsedMinutes would see
	// TeamSize==0 and fall through to the individual-match formula,
	// under-estimating a team competition's duration.
	if comp.Kind == "team" && comp.TeamSize == 0 {
		comp.TeamSize = 5
	}

	// Common ceremony parameters (same as the slot assigners).
	// tournament is always non-nil here (copies from caller or zero-value tournCopy).
	dayStart := parseClockHHMM(comp.StartTime)
	openingMin := parseDurationMinutes(tournament.OpeningBlock)
	lunchMin := parseDurationMinutes(tournament.LunchBlock)
	lunchStart := parseClockHHMM(defaultLunchStartClock)

	// Convert clock times back to durations from dayStart.
	// tournament is always non-nil here (see copy above).
	bufferMultiplier := 1.0
	if tournament.SlowestCourtBufferPct > 0 {
		bufferMultiplier = 1.0 + float64(tournament.SlowestCourtBufferPct)/100.0
	}

	// walk runs the per-court cursor simulation for one or more per-match
	// scenarios in a SINGLE pass over the match distribution, returning each
	// scenario's (perCourtList, slowest-court buffered duration), positionally.
	// The distribution (base/rem per court) is identical across scenarios —
	// pool matches spread evenly across courts, remainder matches to the first
	// courts, an intentional even-distribution heuristic for this pre-draw
	// estimate (the post-draw assigner does NO distribution of its own: it
	// schedules matches that already carry a Court assignment) — only the
	// per-match minutes, and thus how each cursor lands relative to the lunch
	// block, differ, so a cursor per scenario advances together court-by-court
	// (mp-gmcg review F7). This shares the distribution derivation across
	// scenarios; the kachinuki caller below still discards two of the three
	// perCourtList builds (it only surfaces the average one) — negligible on
	// this admin page-load endpoint, so left as is (review E7), but the
	// sharing is scoped to the distribution, not a claim that nothing is
	// discarded.
	//
	// Pure match minutes per court are tracked separately from the cursor so
	// the slowest-court buffer applies to match time ONLY, never to the fixed
	// OpeningBlock offset or LunchBlock dead-time (those have no runtime
	// variance to pad). Mirrors EstimateSchedule, which buffers match time
	// alone.
	walk := func(scenarios []schedScenario) ([][]int, []float64) {
		ns := len(scenarios)
		courtCursor := make([][]time.Time, ns)
		matchMin := make([][]float64, ns)
		start := dayStart.Add(time.Duration(openingMin) * time.Minute)
		for s := range scenarios {
			courtCursor[s] = make([]time.Time, numCourts)
			for i := range courtCursor[s] {
				courtCursor[s][i] = start
			}
			matchMin[s] = make([]float64, numCourts)
		}

		for _, spec := range []struct {
			count    int
			perMatch func(schedScenario) int
		}{
			{poolCount, func(sc schedScenario) int { return sc.poolPerMatch }},
			{playoffCount, func(sc schedScenario) int { return sc.playoffPerMatch }},
		} {
			base := spec.count / numCourts
			rem := spec.count % numCourts
			for ci := 0; ci < numCourts; ci++ {
				n := base
				if ci < rem {
					n++
				}
				for s := range scenarios {
					pm := spec.perMatch(scenarios[s])
					for range n {
						courtCursor[s][ci] = skipCeremonyBlocks(courtCursor[s][ci], lunchStart, lunchMin)
						courtCursor[s][ci] = courtCursor[s][ci].Add(time.Duration(pm) * time.Minute)
					}
					matchMin[s][ci] += float64(n * pm)
				}
			}
		}

		perCourtLists := make([][]int, ns)
		maxDurations := make([]float64, ns)
		for s := range scenarios {
			perCourtLists[s] = make([]int, numCourts)
			for ci, cur := range courtCursor[s] {
				raw := cur.Sub(dayStart).Minutes()
				// fixedOverhead = OpeningBlock offset + any LunchBlock dead-time
				// (raw minus pure match time). Buffer the match time only; add the
				// fixed overhead back unbuffered.
				fixedOverhead := raw - matchMin[s][ci]
				buffered := fixedOverhead + matchMin[s][ci]*bufferMultiplier
				perCourtLists[s][ci] = int(math.Round(buffered))
				if buffered > maxDurations[s] {
					maxDurations[s] = buffered
				}
			}
		}
		return perCourtLists, maxDurations
	}

	ceremonyMin := parseDurationMinutes(tournament.ClosingBlock)

	// Kachinuki (mp-gmcg): the bout count is variable, so price three
	// scenarios. The headline TotalDurationMinutes / PerCourtMinutes is
	// the AVERAGE; best (= the nominal walk) and worst bracket it.
	// IsKachinuki requires TeamSize >= 2 (review: this used to accept
	// TeamSize > 0, so a TeamSize == 1 competition priced a fake best/avg/worst
	// range here while every engine kachinuki function refused it as
	// non-kachinuki — a single fighter per side has no "winner stays on" to
	// range-price).
	if comp.IsKachinuki() {
		bestBouts, avgBouts, worstBouts := kachinukiBoutRange(comp.TeamSize)
		// Order: best, avg, worst. Each scenario's per-match minutes come from
		// kachinukiBoutRange's own bout counts, NOT from the nominal walk: the
		// two are equal today only because perMatchElapsedMinutes uses
		// bouts = TeamSize, and leaning on that coincidence meant retuning the
		// nominal rule could silently stop BestCaseMinutes being the best case
		// (or render Best > Average) while avg and worst stayed right.
		perCourts, maxes := walk([]schedScenario{
			{perMatchElapsedMinutesBouts(comp, tournament, false, bestBouts), perMatchElapsedMinutesBouts(comp, tournament, true, bestBouts)},
			{perMatchElapsedMinutesBouts(comp, tournament, false, avgBouts), perMatchElapsedMinutesBouts(comp, tournament, true, avgBouts)},
			{perMatchElapsedMinutesBouts(comp, tournament, false, worstBouts), perMatchElapsedMinutesBouts(comp, tournament, true, worstBouts)},
		})
		return ScheduleEstimate{
			BestCaseMinutes:      int(math.Round(maxes[0])) + ceremonyMin,
			TotalDurationMinutes: int(math.Round(maxes[1])) + ceremonyMin, // the AVERAGE is the headline
			WorstCaseMinutes:     int(math.Round(maxes[2])) + ceremonyMin,
			PerCourtMinutes:      perCourts[1], // avg
			CeremonyMinutes:      ceremonyMin,
		}
	}

	// Nominal per-match minutes via the shared slot-model helper (bouts =
	// TeamSize). Computed after the kachinuki early-return above, which prices
	// its own three scenarios and never reads these.
	perCourts, maxes := walk([]schedScenario{{
		perMatchElapsedMinutes(comp, tournament, false /*isPlayoff*/),
		perMatchElapsedMinutes(comp, tournament, true /*isPlayoff*/),
	}})
	total := int(math.Round(maxes[0])) + ceremonyMin
	return ScheduleEstimate{
		TotalDurationMinutes: total,
		BestCaseMinutes:      total,
		WorstCaseMinutes:     total,
		PerCourtMinutes:      perCourts[0],
		CeremonyMinutes:      ceremonyMin,
	}
}

// schedScenario is one per-match timing (pool + playoff minutes) that
// EstimateForCounts's walk prices. Kachinuki passes three (best/avg/worst);
// every other format passes one.
type schedScenario struct{ poolPerMatch, playoffPerMatch int }

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
			// Bronze (3rd-place) playoff is a sibling field, not a row in Rounds;
			// count it as one extra bracket bout when present (naginata only).
			if bracket.ThirdPlaceMatch != nil {
				bm := bracket.ThirdPlaceMatch
				court := bm.Court
				if court == "" {
					court = "A"
				}
				entries = append(entries, state.ScheduleEntry{
					MatchType: "bracket",
					// Bronze is not in a numbered round, so it can't use the
					// "R{n}-M{id}" round-match ref. Use the bare match id so
					// patchScheduleCourt's exact-match clause keeps the
					// schedule court in sync when the bronze court changes.
					MatchRef:    bm.ID,
					Court:       court,
					ScheduledAt: bm.ScheduledAt,
					Status:      string(bm.Status),
				})
			}
		}
	}

	return e.store.SaveSchedule(compID, entries)
}

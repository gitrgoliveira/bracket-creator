package engine

import (
	"math"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// scheduleClockLayout is the only time format used here. Wire format
// is HH:MM (24h), with a zero-valued date so we operate purely on
// minutes-of-day arithmetic. T150 / T151.
const scheduleClockLayout = "15:04"

// defaultLunchStartClock is the fallback start-of-lunch when a
// tournament defines LunchBlock duration but does not (yet) carry a
// start time. The struct in internal/state/models.go currently models
// only the duration string; data-model §6 envisions a TimeBlock with
// {StartTime, Duration} but the Go shape has not caught up.
//
// We pick 12:00 because it matches operator convention for kendo
// tournaments scheduled around midday, and because the only consumer
// that actually cares (the slot-skip loop) needs a definite window.
// When the tournament struct gains a typed TimeBlock the resolution
// changes, and the test suite documents this assumption explicitly
// so a future refactor is forced to update the convention together
// with the type.
const defaultLunchStartClock = "12:00"

// defaultPerMatchClockMinutes is the fallback on-clock minutes per
// match when a competition carries neither PoolMatchDuration nor
// PlayoffMatchDuration nor a legacy MatchDuration (e.g. an
// unconfigured comp loaded under ApplyCompetitionDefaults). 3 minutes
// is a reasonable nominal value that keeps the slot loop progressing
// rather than collapsing to zero-duration steps; it is only an estimate
// anchor, not a regulation match time.
const defaultPerMatchClockMinutes = 3

// perMatchElapsedMinutes returns the elapsed minutes a single match
// should occupy on a court given the competition/tournament tuning.
// Pool and playoff matches read their own per-phase clock duration
// (FR-053); team matches scale by bout count plus inter-bout
// transitions (FR-058).
//
// The kachinuki branch uses comp.TeamSize as the worst-case bout
// count for slot planning, a kachinuki match may finish earlier in
// practice if one side gets exhausted, but the operator must reserve
// the upper-bound block so the auto-scheduler does not double-book
// the court. Documented trade-off; T150.
func perMatchElapsedMinutes(comp *state.Competition, tournament *state.Tournament, isPlayoff bool) int {
	if comp == nil {
		return defaultPerMatchClockMinutes
	}

	clockMin := comp.PoolMatchDuration
	if isPlayoff {
		clockMin = comp.PlayoffMatchDuration
	}
	if clockMin <= 0 && comp.MatchDuration > 0 {
		clockMin = comp.MatchDuration
	}
	if clockMin <= 0 {
		clockMin = defaultPerMatchClockMinutes
	}

	multiplier := 1.5
	if tournament != nil && tournament.ClockToElapsedMultiplier > 0 {
		multiplier = tournament.ClockToElapsedMultiplier
	}

	// Team match branch. comp.TeamSize == 0 means individual; >0
	// means a per-bout calculation. For kachinuki we still use
	// TeamSize as the upper bound (see function doc).
	bouts := 0
	if comp.Kind == "team" && comp.TeamSize > 0 {
		bouts = comp.TeamSize
	}

	// Delegate to the shared pure core so this function and
	// EstimateSchedule stay in exact agreement (FR-059).
	return int(math.Round(perMatchElapsed(float64(clockMin), multiplier, bouts)))
}

// parseDurationMinutes converts a Go-style duration string ("30m",
// "1h", "1h30m") to whole minutes. Empty string returns 0 (no block
// configured). Unparseable strings return 0, we treat invalid
// tournament configs as "no block" rather than failing the whole
// match-generation pipeline; the operator notices the missing skip
// in the rendered schedule. T151.
func parseDurationMinutes(s string) int {
	if s == "" {
		return 0
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0
	}
	if d <= 0 {
		return 0
	}
	// Round to nearest minute. ParseDuration accepts sub-minute
	// units but we operate at minute resolution on the wire.
	return int(math.Round(d.Minutes()))
}

// parseClockHHMM parses the "HH:MM" wire format used by
// Competition.StartTime into a time.Time anchored to the zero date.
// The date doesn't matter for the loop, we only consume the time-of-
// day on output via t.Format(scheduleClockLayout). Falls back to
// 09:00 when the input is empty or malformed; this matches the test
// fixture defaults and keeps slot assignment from collapsing.
func parseClockHHMM(s string) time.Time {
	if s == "" {
		t, _ := time.Parse(scheduleClockLayout, "09:00")
		return t
	}
	t, err := time.Parse(scheduleClockLayout, s)
	if err != nil {
		t, _ = time.Parse(scheduleClockLayout, "09:00")
		return t
	}
	return t
}

// skipCeremonyBlocks pushes t past any ceremony window it falls
// inside. Currently models LunchBlock as the only mid-day block,
// OpeningBlock is applied as a pre-loop offset to the per-court
// start (see assignPoolMatchSlots / assignBracketMatchSlots) and
// ClosingBlock is treated as an end-of-day reservation that the
// forward-marching slot loop will not naturally enter unless the
// operator over-fills the day. T151.
//
// dayStart is the parsed StartTime used as the day anchor. lunchStart
// is the time at which LunchBlock begins; lunchDurationMin is its
// length. When lunchDurationMin <= 0 the function is a no-op.
//
// The push is "to the END of the block", never partial. If t lands
// at 12:15 with a 13:00 lunch end, ScheduledAt becomes 13:00. The
// subsequent match on that court therefore can start at 13:00 + this
// match's elapsed minutes, which is correct: the operator does not
// have to manually advance past the block.
func skipCeremonyBlocks(t, lunchStart time.Time, lunchDurationMin int) time.Time {
	if lunchDurationMin <= 0 {
		return t
	}
	lunchEnd := lunchStart.Add(time.Duration(lunchDurationMin) * time.Minute)
	if !t.Before(lunchStart) && t.Before(lunchEnd) {
		return lunchEnd
	}
	return t
}

// assignPoolMatchSlots walks the pool-match list and writes a per-
// court time slot into each match's ScheduledAt field. Pool matches
// are grouped by Court; matches on the same court are sequential,
// matches on different courts run in parallel.
//
// The first match on each court starts at `comp.StartTime +
// OpeningBlock`. Subsequent matches start at the previous match's
// end. Any match whose start would fall inside LunchBlock is pushed
// past the block before its ScheduledAt is recorded. T150, T151.
//
// Mutates `matches` in place: the slice header is passed by value, but
// element writes via indexing reach the caller's underlying array. Returns
// the same slice for ergonomic chaining, and the maximum per-court end-cursor
// (i.e. the clock time when the last match on the busiest court
// finishes). The end-cursor lets a post-draw consumer derive a real schedule
// duration, the mp-zoh per-comp endpoint (future) and
// TestEstimateForCountsVsSlotAssigner_Balanced (today). Note EstimateForCounts
// itself is pre-draw and does NOT call this assigner; it computes its own
// per-court cursors. Callers that only want the mutated slice may discard the
// second return value.
//
// The end-cursor is the per-court start anchor (comp.StartTime + OpeningBlock)
// when there are no matches, matching where the first match on each court
// would have started, and consistent with EstimateForCounts(0,…). So a
// post-draw consumer computing cursor.Sub(dayStart) gets the opening offset (or
// 0 with no OpeningBlock), never a bogus year-from-zero value. A zero time.Time
// is returned ONLY when comp is nil (dayStart cannot be derived).
func assignPoolMatchSlots(matches []state.MatchResult, comp *state.Competition, tournament *state.Tournament) ([]state.MatchResult, time.Time) {
	if comp == nil {
		return matches, time.Time{}
	}
	dayStart := parseClockHHMM(comp.StartTime)
	openingMin := 0
	lunchMin := 0
	var lunchStart time.Time
	if tournament != nil {
		openingMin = parseDurationMinutes(tournament.OpeningBlock)
		lunchMin = parseDurationMinutes(tournament.LunchBlock)
		lunchStart = parseClockHHMM(defaultLunchStartClock)
	}
	if len(matches) == 0 {
		return matches, dayStart.Add(time.Duration(openingMin) * time.Minute)
	}

	courtCursor := map[string]time.Time{}
	perMatchMin := perMatchElapsedMinutes(comp, tournament, false /*isPlayoff*/)

	// Pre-anchor each known court to dayStart+OpeningBlock so a court
	// with N>1 matches gets the opening offset applied to the first
	// slot consistently. Courts not present in the comp config but
	// present on matches (defensive) are anchored on first sight.
	for _, court := range comp.Courts {
		courtCursor[court] = dayStart.Add(time.Duration(openingMin) * time.Minute)
	}

	for i := range matches {
		court := matches[i].Court
		cursor, ok := courtCursor[court]
		if !ok {
			cursor = dayStart.Add(time.Duration(openingMin) * time.Minute)
		}
		cursor = skipCeremonyBlocks(cursor, lunchStart, lunchMin)
		matches[i].ScheduledAt = cursor.Format(scheduleClockLayout)
		cursor = cursor.Add(time.Duration(perMatchMin) * time.Minute)
		courtCursor[court] = cursor
	}

	// Find the maximum end-cursor across all courts (the busiest court
	// determines the soonest the tournament phase can finish).
	// Seed with dayStart so the comparison baseline stays in the same
	// date epoch as the cursors produced by parseClockHHMM (year 0000).
	maxCursor := dayStart
	for _, c := range courtCursor {
		if c.After(maxCursor) {
			maxCursor = c
		}
	}
	return matches, maxCursor
}

// assignBracketMatchSlots is the bracket analogue of
// assignPoolMatchSlots. Bracket matches carry the same Court field
// as pool matches; matches are walked round-by-round, court-by-
// court, and each court's cursor advances by perMatchElapsedMinutes
// after every assignment. T150, T151.
//
// Auto-resolved bye matches (Status == Completed at generation time)
// still receive a ScheduledAt for UI consistency, the operator-
// facing schedule lists them even though no play happens. The court
// cursor is NOT advanced for byes (they consume no court time).
//
// Returns the maximum per-court end-cursor (the clock time when the
// last match on the busiest court finishes). Callers that only want
// the in-place mutation may discard the return value.
//
// As with assignPoolMatchSlots, the end-cursor is the per-court start anchor
// (comp.StartTime + OpeningBlock) when there are no rounds, and a zero
// time.Time only when comp is nil.
func assignBracketMatchSlots(rounds [][]state.BracketMatch, comp *state.Competition, tournament *state.Tournament) time.Time {
	if comp == nil {
		return time.Time{}
	}
	dayStart := parseClockHHMM(comp.StartTime)
	openingMin := 0
	lunchMin := 0
	var lunchStart time.Time
	if tournament != nil {
		openingMin = parseDurationMinutes(tournament.OpeningBlock)
		lunchMin = parseDurationMinutes(tournament.LunchBlock)
		lunchStart = parseClockHHMM(defaultLunchStartClock)
	}
	if len(rounds) == 0 {
		return dayStart.Add(time.Duration(openingMin) * time.Minute)
	}

	courtCursor := map[string]time.Time{}
	for _, court := range comp.Courts {
		courtCursor[court] = dayStart.Add(time.Duration(openingMin) * time.Minute)
	}

	perMatchMin := perMatchElapsedMinutes(comp, tournament, true /*isPlayoff*/)

	for rIdx := range rounds {
		round := rounds[rIdx]
		for mIdx := range round {
			m := &round[mIdx]
			court := m.Court
			cursor, ok := courtCursor[court]
			if !ok {
				cursor = dayStart.Add(time.Duration(openingMin) * time.Minute)
			}
			cursor = skipCeremonyBlocks(cursor, lunchStart, lunchMin)
			m.ScheduledAt = cursor.Format(scheduleClockLayout)

			// Don't advance the court cursor for auto-resolved byes.
			// They occupy no real time on the court, the next round
			// would otherwise inherit a phantom delay.
			if m.Status != state.MatchStatusCompleted {
				cursor = cursor.Add(time.Duration(perMatchMin) * time.Minute)
			}
			courtCursor[court] = cursor
		}
	}

	// Find the maximum end-cursor across all courts. Seed with dayStart
	// so the comparison stays in the same date epoch as parseClockHHMM
	// cursors (year 0000), avoiding false comparisons against the Go
	// zero Time (year 0001).
	maxCursor := dayStart
	for _, c := range courtCursor {
		if c.After(maxCursor) {
			maxCursor = c
		}
	}
	return maxCursor
}

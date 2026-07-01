package engine

import (
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// scheduleLeagueSlots assigns each match a global slot index and a court,
// spreading every player's matches so they never fight two slots in a row.
// Returns the input matches with Court set, plus a parallel slice giving the
// slot index of each returned match (result[i] is in slot slots[i]). Players
// are identified by SideA/SideB (names). numPlayers is the league size (n);
// courts is the ordered court-label list (len>=1).
//
// Guarantees (see mp-sjaz):
//   - G1 (no simultaneity): within any single slot, no player appears in two matches.
//   - G2 (no back-to-back): NO player ever has matches in two adjacent slots, for
//     ANY court count. This is a HARD invariant: when every remaining match would
//     put a player who just fought (in slot-1) straight back in, the current slot
//     is left empty (an idle rest band) and scheduling advances to the next slot,
//     where the just-played players are two slots clear and eligible again.
//   - Completeness: every match in the input appears exactly once in the output.
//
// Court count therefore affects only schedule LENGTH (fewer courts -> more slots,
// i.e. more idle/rest time), never correctness. A full round-robin cannot pack
// every player's matches non-adjacently into the minimum slot count, so idle
// slots are the deliberate price of the rest guarantee.
func scheduleLeagueSlots(matches []state.MatchResult, numPlayers int, courts []string) (ordered []state.MatchResult, slots []int) {
	if len(courts) == 0 {
		courts = []string{""}
	}
	numCourts := len(courts)

	// sentinel: a player not yet scheduled has lastSlot = math.MinInt/2 so
	// (lastSlot[x] == slot-1) is false for any non-negative slot.
	const absent = -(1 << 30)
	lastSlot := make(map[string]int, numPlayers)
	getLastSlot := func(player string) int {
		if v, ok := lastSlot[player]; ok {
			return v
		}
		return absent
	}

	remaining := make([]state.MatchResult, len(matches))
	copy(remaining, matches)

	slot := 0
	for len(remaining) > 0 {
		// Candidate order for G2-aware pass:
		// sort by (min(lastSlot[a],lastSlot[b]) asc, Round asc, ID asc)
		sorted := make([]state.MatchResult, len(remaining))
		copy(sorted, remaining)
		sort.SliceStable(sorted, func(i, j int) bool {
			mi, mj := sorted[i], sorted[j]
			lsi := min(getLastSlot(mi.SideA), getLastSlot(mi.SideB))
			lsj := min(getLastSlot(mj.SideA), getLastSlot(mj.SideB))
			if lsi != lsj {
				return lsi < lsj
			}
			if mi.Round != mj.Round {
				return mi.Round < mj.Round
			}
			return mi.ID < mj.ID
		})

		var slotMatches []state.MatchResult
		used := make(map[string]bool)

		// Fill this slot with matches whose players did NOT fight in slot-1
		// (hard G2) and are not already placed in this slot (G1). Preferring
		// the least-recently-played matches keeps every player's fights spread
		// out. If nothing qualifies (every remaining match reuses a slot-1
		// player), slotMatches stays empty: the slot becomes an idle rest band
		// and we advance. On the next slot the just-played players are two
		// slots clear, so at least one match always qualifies, guaranteeing
		// termination.
		for _, cand := range sorted {
			if len(slotMatches) == numCourts {
				break
			}
			a, b := cand.SideA, cand.SideB
			if used[a] || used[b] {
				continue // G1 violation
			}
			if getLastSlot(a) == slot-1 || getLastSlot(b) == slot-1 {
				continue // G2: never place a player who fought last slot
			}
			slotMatches = append(slotMatches, cand)
			used[a] = true
			used[b] = true
		}

		// Assign courts and record slot index; update lastSlot.
		placed := make(map[string]bool)
		for i, m := range slotMatches {
			m.Court = courts[i]
			placed[m.ID] = true
			lastSlot[m.SideA] = slot
			lastSlot[m.SideB] = slot
			ordered = append(ordered, m)
			slots = append(slots, slot)
		}

		// Remove placed matches from remaining.
		next := remaining[:0]
		for _, m := range remaining {
			if !placed[m.ID] {
				next = append(next, m)
			}
		}
		remaining = next
		slot++
	}

	return ordered, slots
}

// assignLeagueSlotTimes writes ScheduledAt on each match according to its slot:
// all matches in slot k share one start time; slot k+1 starts one match-duration
// after slot k (lunch/opening blocks respected via the same rules as
// assignPoolMatchSlots). Returns matches and the max end cursor.
//
// A single global cursor is used: slot k's start = dayStart + OpeningBlock +
// k*perMatchMin, pushed past LunchBlock via skipCeremonyBlocks. This keeps all
// courts in the same slot strictly time-aligned, unlike the per-court cursors of
// assignPoolMatchSlots (which is the defect G2 scheduling is designed to fix).
//
// Returns the input slice with ScheduledAt populated and the cursor position
// immediately after the last slot ends. Returns an unmodified slice and zero
// time.Time when comp is nil.
func assignLeagueSlotTimes(matches []state.MatchResult, slots []int, comp *state.Competition, tournament *state.Tournament) ([]state.MatchResult, time.Time) {
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

	perMatchMin := perMatchElapsedMinutes(comp, tournament, false /*isPlayoff*/)

	// Build a map from slot index to start time by walking slot 0..maxSlot
	// with a single cursor, skipping ceremony blocks.
	maxSlot := 0
	for _, s := range slots {
		if s > maxSlot {
			maxSlot = s
		}
	}

	slotTime := make([]time.Time, maxSlot+1)
	cursor := dayStart.Add(time.Duration(openingMin) * time.Minute)
	for k := range maxSlot + 1 {
		cursor = skipCeremonyBlocks(cursor, lunchStart, lunchMin)
		slotTime[k] = cursor
		cursor = cursor.Add(time.Duration(perMatchMin) * time.Minute)
	}

	for i := range matches {
		if i < len(slots) {
			s := slots[i]
			if s >= 0 && s < len(slotTime) {
				matches[i].ScheduledAt = slotTime[s].Format(scheduleClockLayout)
			}
		}
	}

	// Return cursor as the end time after the last slot.
	return matches, cursor
}

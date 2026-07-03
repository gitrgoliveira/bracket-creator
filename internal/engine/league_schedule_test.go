package engine

import (
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// buildLeagueMatches builds a []state.MatchResult for all n*(n-1)/2 pairs
// using CircleMethodRounds so each match carries its Round number. Players
// are named "P0".."P{n-1}".
func buildLeagueMatches(n int) []state.MatchResult {
	rounds := helper.CircleMethodRounds(n)
	var matches []state.MatchResult
	k := 0
	for ri, round := range rounds {
		for _, pair := range round {
			matches = append(matches, state.MatchResult{
				ID:     fmt.Sprintf("m%03d", k),
				SideA:  fmt.Sprintf("P%d", pair.A),
				SideB:  fmt.Sprintf("P%d", pair.B),
				Round:  ri,
				Status: state.MatchStatusScheduled,
			})
			k++
		}
	}
	return matches
}

// courtLabels returns the first n uppercase letter labels starting from "A".
func courtLabels(n int) []string {
	labels := make([]string, n)
	for i := range n {
		labels[i] = string(rune('A' + i))
	}
	return labels
}

// verifyCompleteness checks that every (i,j) pair with i<j appears exactly
// once in the output and that the total match count equals n*(n-1)/2.
func verifyCompleteness(t *testing.T, ordered []state.MatchResult, n int) {
	t.Helper()
	expected := n * (n - 1) / 2
	require.Len(t, ordered, expected, "total match count: want %d, got %d", expected, len(ordered))

	type pair struct{ a, b string }
	seen := make(map[pair]int, expected)
	for _, m := range ordered {
		a, b := m.SideA, m.SideB
		if a > b {
			a, b = b, a
		}
		seen[pair{a, b}]++
	}
	assert.Len(t, seen, expected, "unique pairs count mismatch")
	for p, count := range seen {
		assert.Equal(t, 1, count, "pair (%s,%s) appears %d times, want 1", p.a, p.b, count)
	}
}

// verifyG1 checks that within every slot no player appears in two matches.
func verifyG1(t *testing.T, ordered []state.MatchResult, slots []int) {
	t.Helper()
	// group match indices by slot
	bySlot := make(map[int][]int)
	for i, s := range slots {
		bySlot[s] = append(bySlot[s], i)
	}
	for s, idxs := range bySlot {
		seen := make(map[string]bool)
		for _, idx := range idxs {
			m := ordered[idx]
			assert.Falsef(t, seen[m.SideA], "slot %d: player %q appears in multiple matches (G1 violation)", s, m.SideA)
			assert.Falsef(t, seen[m.SideB], "slot %d: player %q appears in multiple matches (G1 violation)", s, m.SideB)
			seen[m.SideA] = true
			seen[m.SideB] = true
		}
	}
}

// verifyG2 checks that no player has matches in two adjacent slots.
func verifyG2(t *testing.T, ordered []state.MatchResult, slots []int) {
	t.Helper()
	// lastSlot[player] -> the most recent slot index seen
	lastSlot := make(map[string]int)
	// Walk in slot order so we can detect adjacency.
	// Build (slot, matchIdx) pairs sorted by slot.
	type entry struct {
		slot int
		idx  int
	}
	entries := make([]entry, len(slots))
	for i, s := range slots {
		entries[i] = entry{s, i}
	}
	// Sort by slot asc, then by original index for determinism.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].slot != entries[j].slot {
			return entries[i].slot < entries[j].slot
		}
		return entries[i].idx < entries[j].idx
	})

	for _, e := range entries {
		m := ordered[e.idx]
		for _, player := range []string{m.SideA, m.SideB} {
			if prev, ok := lastSlot[player]; ok {
				assert.NotEqual(t, e.slot-1, prev,
					"G2 violation: player %q is in slot %d and slot %d (adjacent)", player, prev, e.slot)
			}
			lastSlot[player] = e.slot
		}
	}
}

// --- Completeness + G1 + G2 at SuggestedMaxCourts ---

func TestScheduleLeagueSlots_CompletenessG1G2(t *testing.T) {
	t.Parallel()

	sizes := []int{2, 3, 5, 6, 7, 8, 10, 12, 16}
	for _, n := range sizes {
		t.Run(fmt.Sprintf("n=%d", n), func(t *testing.T) {
			t.Parallel()
			numCourts := SuggestedMaxCourts(n)
			courts := courtLabels(numCourts)
			matches := buildLeagueMatches(n)

			ordered, slots := scheduleLeagueSlots(matches, courts)

			require.Len(t, slots, len(ordered), "slots and ordered must be same length")

			verifyCompleteness(t, ordered, n)
			verifyG1(t, ordered, slots)
			verifyG2(t, ordered, slots)
		})
	}
}

// --- Warning zone: numCourts = floor(n/2) (one above SuggestedMaxCourts) ---
// G2 is a hard invariant (idle slots are inserted when needed), so it holds
// here too regardless of court count. This test asserts correctness (G1/G2),
// not schedule length; the court count changes how the matches pack into slots
// but never whether the rest guarantee holds.

func TestScheduleLeagueSlots_WarningZone_StillG2(t *testing.T) {
	t.Parallel()

	sizes := []int{4, 6, 8, 10, 12, 16}
	for _, n := range sizes {
		t.Run(fmt.Sprintf("n=%d_courts=%d", n, n/2), func(t *testing.T) {
			t.Parallel()
			numCourts := n / 2 // warning zone: one more than SuggestedMaxCourts
			courts := courtLabels(numCourts)
			matches := buildLeagueMatches(n)

			ordered, slots := scheduleLeagueSlots(matches, courts)

			require.Len(t, slots, len(ordered), "slots and ordered must be same length")
			verifyCompleteness(t, ordered, n)
			verifyG1(t, ordered, slots)
			verifyG2(t, ordered, slots)
		})
	}
}

// --- numCourts == 1 ---

func TestScheduleLeagueSlots_SingleCourt(t *testing.T) {
	t.Parallel()

	// With one court each slot holds one match, so ordering alone must space
	// every player out: G1 is trivial and G2 (no back-to-back) still holds
	// for all n via idle-slot insertion.
	for _, n := range []int{3, 4, 5, 6, 8} {
		t.Run(fmt.Sprintf("n=%d_singlecourt", n), func(t *testing.T) {
			t.Parallel()
			courts := []string{"A"}
			matches := buildLeagueMatches(n)

			ordered, slots := scheduleLeagueSlots(matches, courts)

			require.Len(t, slots, len(ordered))
			verifyCompleteness(t, ordered, n)
			verifyG1(t, ordered, slots) // trivially holds with 1 court
			verifyG2(t, ordered, slots)
		})
	}
}

// --- Empty input ---

func TestScheduleLeagueSlots_Empty(t *testing.T) {
	ordered, slots := scheduleLeagueSlots(nil, []string{"A"})
	assert.Empty(t, ordered)
	assert.Empty(t, slots)
}

// --- assignLeagueSlotTimes ---

func TestAssignLeagueSlotTimes_SameSlotSameTime(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 3,
		Courts:            []string{"A", "B"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.5,
	}

	// Two matches in slot 0, one in slot 1.
	matches := []state.MatchResult{
		{ID: "m0", SideA: "P0", SideB: "P1"},
		{ID: "m1", SideA: "P2", SideB: "P3"},
		{ID: "m2", SideA: "P0", SideB: "P2"},
	}
	slots := []int{0, 0, 1}

	out, cursor := assignLeagueSlotTimes(matches, slots, comp, tournament)

	require.Len(t, out, 3)

	// Slot 0 matches must share the same ScheduledAt.
	assert.Equal(t, out[0].ScheduledAt, out[1].ScheduledAt,
		"both slot-0 matches must share the same start time")

	// Slot 1 must be exactly perMatchMin later than slot 0.
	perMatch := perMatchElapsedMinutes(comp, tournament, false)
	gap := minutesBetween(t, out[0].ScheduledAt, out[2].ScheduledAt)
	assert.Equal(t, perMatch, gap, "slot 1 must start exactly perMatchMin after slot 0")

	// Cursor must be non-zero.
	assert.False(t, cursor.IsZero(), "cursor must not be zero")
}

func TestAssignLeagueSlotTimes_NilComp(t *testing.T) {
	matches := []state.MatchResult{{ID: "m0"}}
	slots := []int{0}
	out, cursor := assignLeagueSlotTimes(matches, slots, nil, nil)
	require.Len(t, out, 1)
	assert.True(t, cursor.IsZero(), "nil comp must return zero cursor")
}

func TestAssignLeagueSlotTimes_EmptyMatches(t *testing.T) {
	comp := &state.Competition{
		StartTime:         "09:00",
		PoolMatchDuration: 3,
	}
	out, cursor := assignLeagueSlotTimes(nil, nil, comp, nil)
	assert.Empty(t, out)
	assert.False(t, cursor.IsZero(), "cursor for empty input should be dayStart offset")
}

func TestAssignLeagueSlotTimes_LunchSkip(t *testing.T) {
	// Start at 11:57, 3-minute matches -> slot 0 at 11:57, slot 1 would be 12:02
	// but lunch is 12:00-13:00, so slot 1 should be pushed to 13:00.
	comp := &state.Competition{
		StartTime:         "11:57",
		PoolMatchDuration: 3,
		Courts:            []string{"A"},
	}
	tournament := &state.Tournament{
		ClockToElapsedMultiplier: 1.0, // 3 min * 1.0 = 3 min elapsed
		LunchBlock:               "1h",
	}
	matches := []state.MatchResult{
		{ID: "m0", SideA: "P0", SideB: "P1"},
		{ID: "m1", SideA: "P0", SideB: "P2"},
	}
	slots := []int{0, 1}

	out, _ := assignLeagueSlotTimes(matches, slots, comp, tournament)
	require.Len(t, out, 2)

	// Slot 1 must be at or after 13:00 (past the lunch block).
	slot1Time := parseClockHHMM(out[1].ScheduledAt)
	lunchEnd := parseClockHHMM("13:00")
	assert.False(t, slot1Time.Before(lunchEnd),
		"slot 1 (%s) should be at or after lunch end (13:00)", out[1].ScheduledAt)
}

// --- Integration: generatePools produces G1/G2-compliant League schedule ---

func TestGeneratePools_League_G1G2(t *testing.T) {
	t.Parallel()

	// Every case must satisfy G1 and G2: G2 is a hard invariant at any court
	// count, so a 1-court league is included alongside the SuggestedMaxCourts
	// cases. Court count only changes schedule length, not the guarantee. The
	// lunch case exercises the ceremony-block skip so a lunch break between two
	// of a player's matches never reads as a false adjacency.
	tests := []struct {
		n         int
		courts    []string
		startTime string
		lunch     bool
	}{
		{n: 6, courts: courtLabels(SuggestedMaxCourts(6)), startTime: "09:00"},
		{n: 8, courts: courtLabels(SuggestedMaxCourts(8)), startTime: "09:00"},
		{n: 10, courts: courtLabels(SuggestedMaxCourts(10)), startTime: "09:00"},
		{n: 6, courts: []string{"A"}, startTime: "09:00"},              // single court still gets G2 via idle slots
		{n: 8, courts: []string{"A"}, startTime: "11:50", lunch: true}, // slots straddle the noon lunch block
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("n=%d_courts=%d_lunch=%v", tt.n, len(tt.courts), tt.lunch), func(t *testing.T) {
			t.Parallel()

			eng, store, _ := setupTestEngine(t)
			compID := fmt.Sprintf("league-g1g2-%d-%d-%v", tt.n, len(tt.courts), tt.lunch)

			if tt.lunch {
				require.NoError(t, store.SaveTournament(&state.Tournament{LunchBlock: "1h"}))
			}

			comp := &state.Competition{
				ID:           compID,
				Name:         "League G1G2 Test",
				Kind:         "individual",
				Format:       state.CompFormatLeague,
				PoolSize:     tt.n,
				PoolSizeMode: "min",
				PoolWinners:  1,
				RoundRobin:   true,
				Courts:       tt.courts,
				StartTime:    tt.startTime,
				Status:       "setup",
			}
			require.NoError(t, store.SaveCompetition(comp))

			saveTestParticipants(t, store, compID, names(tt.n))

			require.NoError(t, eng.StartCompetition(compID))

			matches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.NotEmpty(t, matches)

			expectedTotal := tt.n * (tt.n - 1) / 2
			assert.Len(t, matches, expectedTotal, "wrong total match count")

			// G1: matches sharing a start time (one slot) must not share a player.
			byTime := make(map[string][]state.MatchResult)
			for _, m := range matches {
				byTime[m.ScheduledAt] = append(byTime[m.ScheduledAt], m)
			}
			for timeStr, group := range byTime {
				seen := make(map[string]bool)
				for _, m := range group {
					assert.Falsef(t, seen[m.SideA], "time %s: player %q in multiple simultaneous matches (G1)", timeStr, m.SideA)
					assert.Falsef(t, seen[m.SideB], "time %s: player %q in multiple simultaneous matches (G1)", timeStr, m.SideB)
					seen[m.SideA] = true
					seen[m.SideB] = true
				}
			}

			// G2: no player appears in two adjacent slots. Wall-clock minute gaps
			// cannot detect this across a lunch/ceremony boundary (a skip inflates
			// the gap of an adjacent pair), so map each ScheduledAt back to its
			// slot INDEX by replaying the same cursor loop assignLeagueSlotTimes
			// uses (parseCeremonyParams + skipCeremonyBlocks + perMatch steps),
			// then assert no player has two matches in consecutive slot indices.
			loadedComp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			tournament, err := store.LoadTournament()
			require.NoError(t, err)
			state.ApplyTournamentDefaults(tournament)
			state.ApplyCompetitionDefaults(loadedComp)
			perMatch := perMatchElapsedMinutes(loadedComp, tournament, false)
			require.Positive(t, perMatch)

			dayStart, openingMin, lunchMin, lunchStart := parseCeremonyParams(loadedComp, tournament)
			timeToSlot := make(map[string]int)
			cursor := dayStart.Add(time.Duration(openingMin) * time.Minute)
			for k := 0; k < len(matches)*3+2; k++ { // generous bound: idle slots at most ~1 per match
				cursor = skipCeremonyBlocks(cursor, lunchStart, lunchMin)
				timeToSlot[cursor.Format(scheduleClockLayout)] = k
				cursor = cursor.Add(time.Duration(perMatch) * time.Minute)
			}

			byPlayer := make(map[string][]int)
			for _, m := range matches {
				s, ok := timeToSlot[m.ScheduledAt]
				require.Truef(t, ok, "match time %s not found in replayed slot grid", m.ScheduledAt)
				byPlayer[m.SideA] = append(byPlayer[m.SideA], s)
				byPlayer[m.SideB] = append(byPlayer[m.SideB], s)
			}
			for player, slotIdxs := range byPlayer {
				sort.Ints(slotIdxs)
				for i := 1; i < len(slotIdxs); i++ {
					assert.Greaterf(t, slotIdxs[i]-slotIdxs[i-1], 1,
						"G2 violation: player %q in adjacent slots %d and %d", player, slotIdxs[i-1], slotIdxs[i])
				}
			}
		})
	}
}

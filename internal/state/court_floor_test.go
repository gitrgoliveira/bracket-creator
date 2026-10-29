package state

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func dummySlotMinutes(poolMin, koMin int) SlotMinutes {
	return func(c *Competition, isKnockout bool) int {
		if isKnockout {
			return koMin
		}
		return poolMin
	}
}

func TestStore_CourtEnds_EmptyOrUndrawn(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:        "c1",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(comp))

	ends, err := store.CourtEnds("c1", dummySlotMinutes(10, 15))
	require.NoError(t, err)
	assert.Empty(t, ends, "undrawn competition should have no court ends")
}

func TestStore_CourtEnds_PoolAndBracket(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:        "c1",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A", "B"},
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Pool matches:
	// Court A: 09:00 + 10m = 09:10, 09:20 + 10m = 09:30
	// Court B: 09:00 + 10m = 09:10
	poolMatches := []MatchResult{
		{ID: "p1", Court: "A", ScheduledAt: "09:00", Status: MatchStatusScheduled},
		{ID: "p2", Court: "A", ScheduledAt: "09:20", Status: MatchStatusScheduled},
		{ID: "p3", Court: "B", ScheduledAt: "09:00", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches("c1", poolMatches))

	// Bracket matches:
	// Court A: 09:40 + 15m = 09:55
	// Court A: bye match completed at 10:00 (empty SideB) -> start 10:00 + 0m = 10:00
	// Court B: 3rd place match at 09:30 + 15m = 09:45
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{
					ID:          "b1",
					Court:       "A",
					ScheduledAt: "09:40",
					Status:      MatchStatusScheduled,
					SideA:       "Player 1",
					SideB:       "Player 2",
				},
				{
					ID:          "b-bye",
					Court:       "A",
					ScheduledAt: "10:00",
					Status:      MatchStatusCompleted,
					SideA:       "Player 3",
					SideB:       "", // Bye: empty competitor side
				},
			},
		},
		ThirdPlaceMatch: &BracketMatch{
			ID:          "b-tp",
			Court:       "B",
			ScheduledAt: "09:30",
			Status:      MatchStatusScheduled,
			SideA:       "Player 4",
			SideB:       "Player 5",
		},
	}
	require.NoError(t, store.SaveBracket("c1", bracket))

	ends, err := store.CourtEnds("c1", dummySlotMinutes(10, 15))
	require.NoError(t, err)

	// Court A latest end: bye is 10:00 (10:00 + 0), earlier was 09:40 + 15 = 09:55 -> latest is 10:00
	assert.Equal(t, "10:00", ends["A"])
	// Court B latest end: pool was 09:10, 3rd place is 09:30 + 15 = 09:45 -> latest is 09:45
	assert.Equal(t, "09:45", ends["B"])
}

func TestStore_CourtFloor_Rules(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	// compEarly: 2026-11-01, StartTime 09:00, Court A ends at 10:00, Court B ends at 09:40
	compEarly := &Competition{
		ID:        "c-early",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A", "B"},
	}
	require.NoError(t, store.SaveCompetition(compEarly))
	require.NoError(t, store.SavePoolMatches("c-early", []MatchResult{
		{ID: "e1", Court: "A", ScheduledAt: "09:45", Status: MatchStatusScheduled}, // + 15 = 10:00
		{ID: "e2", Court: "B", ScheduledAt: "09:25", Status: MatchStatusScheduled}, // + 15 = 09:40
	}))

	// compSameTime: 2026-11-01, StartTime 09:00, Court A ends at 10:15
	compSameTime := &Competition{
		ID:        "c-same",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(compSameTime))
	require.NoError(t, store.SavePoolMatches("c-same", []MatchResult{
		{ID: "s1", Court: "A", ScheduledAt: "10:00", Status: MatchStatusScheduled}, // + 15 = 10:15
	}))

	// compLater: 2026-11-01, StartTime 11:00, Court A ends at 11:30
	compLater := &Competition{
		ID:        "c-later",
		Date:      "2026-11-01",
		StartTime: "11:00",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(compLater))
	require.NoError(t, store.SavePoolMatches("c-later", []MatchResult{
		{ID: "l1", Court: "A", ScheduledAt: "11:15", Status: MatchStatusScheduled}, // + 15 = 11:30
	}))

	// compOtherDay: 2026-11-02, StartTime 08:30, Court A ends at 09:30
	compOtherDay := &Competition{
		ID:        "c-other-day",
		Date:      "2026-11-02",
		StartTime: "08:30",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(compOtherDay))
	require.NoError(t, store.SavePoolMatches("c-other-day", []MatchResult{
		{ID: "od1", Court: "A", ScheduledAt: "09:15", Status: MatchStatusScheduled},
	}))

	// compEmptyDate: Date "", StartTime 08:30, Court A
	compEmptyDate := &Competition{
		ID:        "c-empty-date",
		Date:      "",
		StartTime: "08:30",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(compEmptyDate))
	require.NoError(t, store.SavePoolMatches("c-empty-date", []MatchResult{
		{ID: "ed1", Court: "A", ScheduledAt: "09:15", Status: MatchStatusScheduled},
	}))

	slotMin := dummySlotMinutes(15, 15)

	t.Run("earlier and equal start times floor, later does not, latest floor wins", func(t *testing.T) {
		// Target comp: 2026-11-01, StartTime 09:00, requests Court A and Court C
		target := &Competition{
			ID:        "target-1",
			Date:      "2026-11-01",
			StartTime: "09:00",
			Courts:    []string{"A", "C"},
		}
		require.NoError(t, store.SaveCompetition(target))

		floor, err := store.CourtFloor(target, []string{"A", "C"}, slotMin)
		require.NoError(t, err)

		// Court A is shared with:
		// - compEarly (start 09:00 <= 09:00, end 10:00)
		// - compSameTime (start 09:00 <= 09:00, end 10:15)
		// - compLater (start 11:00 > 09:00 -> skipped)
		// - compOtherDay (different date -> skipped)
		// - compEmptyDate (empty date -> skipped)
		// Latest end on Court A is 10:15
		assert.Equal(t, "10:15", floor["A"])

		// Court C nobody floors -> absent
		_, ok := floor["C"]
		assert.False(t, ok, "court C should not be floored")
	})

	t.Run("target competition is never its own floor", func(t *testing.T) {
		floor, err := store.CourtFloor(compEarly, []string{"A", "B"}, slotMin)
		require.NoError(t, err)

		// On Court A: compSameTime has end 10:15, compEarly excluded. compLater is 11:00 > 09:00.
		assert.Equal(t, "10:15", floor["A"])
		// On Court B: only compEarly had matches on B; with compEarly excluded, nobody floors B.
		_, ok := floor["B"]
		assert.False(t, ok, "compEarly must not floor its own court B")
	})

	t.Run("target competition with empty or invalid start time takes no floor", func(t *testing.T) {
		noStart := &Competition{
			ID:        "target-no-start",
			Date:      "2026-11-01",
			StartTime: "",
			Courts:    []string{"A"},
		}
		require.NoError(t, store.SaveCompetition(noStart))

		floor, err := store.CourtFloor(noStart, []string{"A"}, slotMin)
		require.NoError(t, err)
		assert.Empty(t, floor, "competition with empty StartTime should take no floor")
	})

	t.Run("target competition with empty date takes no floor", func(t *testing.T) {
		noDate := &Competition{
			ID:        "target-no-date",
			Date:      "",
			StartTime: "09:00",
			Courts:    []string{"A"},
		}
		require.NoError(t, store.SaveCompetition(noDate))

		floor, err := store.CourtFloor(noDate, []string{"A"}, slotMin)
		require.NoError(t, err)
		assert.Empty(t, floor, "competition with empty Date should take no floor")
	})
}

func TestStore_CourtFloor_LogsAndSkipsCorruptNeighbour(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	target := &Competition{
		ID:        "target-comp",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(target))

	readable := &Competition{
		ID:        "readable-comp",
		Date:      "2026-11-01",
		StartTime: "09:00",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(readable))
	require.NoError(t, store.SavePoolMatches("readable-comp", []MatchResult{
		{ID: "r1", Court: "A", ScheduledAt: "09:15", Status: MatchStatusScheduled},
	}))

	corruptDir := filepath.Join(dir, "competitions", "corrupt-comp")
	require.NoError(t, os.MkdirAll(corruptDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(corruptDir, "config.md"), []byte("---:\n:invalid yaml\n"), 0o644))

	var buf bytes.Buffer
	origOutput := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(origOutput)

	slotMin := dummySlotMinutes(15, 15)
	floor, err := store.CourtFloor(target, []string{"A"}, slotMin)
	require.NoError(t, err)
	assert.Equal(t, "09:30", floor["A"])
	assert.Contains(t, buf.String(), "corrupt-comp")
}

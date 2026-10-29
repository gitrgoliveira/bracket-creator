package state_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type goldenCase struct {
	Name      string                  `json:"name"`
	Entries   []state.CourtQueueEntry `json:"entries"`
	Queues    []goldenCourtQueue      `json:"queues"`
	Positions map[string]int          `json:"positions"`
}

type goldenCourtQueue struct {
	Date  string   `json:"date"`
	Court string   `json:"court"`
	Order []string `json:"order"`
}

type goldenFile struct {
	Cases []goldenCase `json:"cases"`
}

func TestCourtQueue_GoldenTable(t *testing.T) {
	bytes, err := os.ReadFile("testdata/court_queue.json")
	require.NoError(t, err)

	var gf goldenFile
	require.NoError(t, json.Unmarshal(bytes, &gf))
	require.NotEmpty(t, gf.Cases)

	for _, tc := range gf.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			actualQueues := state.CourtQueues(tc.Entries)

			expectedQueueMap := make(map[state.CourtKey][]string)
			for _, q := range tc.Queues {
				key := state.CourtKey{Date: q.Date, Court: q.Court}
				expectedQueueMap[key] = q.Order
			}

			actualQueueMap := make(map[state.CourtKey][]string)
			for key, idxs := range actualQueues {
				names := make([]string, len(idxs))
				for i, idx := range idxs {
					names[i] = fmt.Sprintf("%s/%s", tc.Entries[idx].CompetitionID, tc.Entries[idx].MatchID)
				}
				actualQueueMap[key] = names
			}

			assert.Equal(t, expectedQueueMap, actualQueueMap, "queues mismatch in case: %s", tc.Name)

			actualPositions := state.CourtQueuePositions(tc.Entries)
			require.Equal(t, len(tc.Entries), len(actualPositions))

			actualPosMap := make(map[string]int)
			for i, pos := range actualPositions {
				name := fmt.Sprintf("%s/%s", tc.Entries[i].CompetitionID, tc.Entries[i].MatchID)
				expectedPos := tc.Positions[name]
				assert.Equal(t, expectedPos, pos, "position for %s in case: %s", name, tc.Name)
				if pos != 0 {
					actualPosMap[name] = pos
				}
			}
			assert.Equal(t, tc.Positions, actualPosMap, "positions map mismatch in case: %s", tc.Name)
		})
	}
}

func TestCourtQueueEntriesOf_DrawIndex(t *testing.T) {
	comp := &state.Competition{
		ID:        "c1",
		StartTime: "09:00",
		Date:      "2026-11-01",
	}

	poolMatches := []state.MatchResult{
		{
			ID:     "p0",
			Court:  "A",
			Status: state.MatchStatusScheduled,
		},
		{
			ID:              "p1",
			Court:           "A",
			Status:          state.MatchStatusScheduled,
			IneligibleSides: &state.IneligibleSidesAnnotation{A: "kiken-injury"},
		},
	}

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:              "r0m0",
					Court:           "A",
					Status:          state.MatchStatusCompleted,
					IneligibleSides: &state.IneligibleSidesAnnotation{A: "kiken-injury"},
				},
				{
					ID:     "r0m1",
					Court:  "A",
					Status: state.MatchStatusScheduled,
				},
			},
			{
				{
					ID:     "r1m0",
					Court:  "A",
					Status: state.MatchStatusScheduled,
				},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     state.BronzeMatchID,
			Court:  "A",
			Status: state.MatchStatusScheduled,
		},
	}

	entries := state.CourtQueueEntriesOf(comp, poolMatches, bracket)
	require.Len(t, entries, 6)

	expected := []struct {
		matchID   string
		drawIndex int
		barred    bool
	}{
		{matchID: "p0", drawIndex: 0, barred: false},
		{matchID: "p1", drawIndex: 1, barred: true},
		{matchID: "r0m0", drawIndex: 2, barred: false},
		{matchID: "r0m1", drawIndex: 3, barred: false},
		{matchID: state.BronzeMatchID, drawIndex: 4, barred: false},
		{matchID: "r1m0", drawIndex: 5, barred: false},
	}

	for i, exp := range expected {
		e := entries[i]
		assert.Equal(t, "c1", e.CompetitionID, "entry %d CompetitionID", i)
		assert.Equal(t, "09:00", e.CompetitionStartTime, "entry %d CompetitionStartTime", i)
		assert.Equal(t, "2026-11-01", e.Date, "entry %d Date", i)
		assert.Equal(t, exp.matchID, e.MatchID, "entry %d MatchID", i)
		assert.Equal(t, exp.drawIndex, e.DrawIndex, "entry %d DrawIndex", i)
		assert.Equal(t, exp.barred, e.Barred, "entry %d Barred", i)
	}
}

func TestCourtQueue_PoolAndBracketShareOnePosition(t *testing.T) {
	comp := &state.Competition{
		ID:        "c1",
		StartTime: "09:00",
		Date:      "2026-11-01",
	}

	poolMatches := []state.MatchResult{
		{
			ID:          "p0",
			Court:       "A",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "",
		},
	}

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "b0",
					Court:       "A",
					Status:      state.MatchStatusScheduled,
					ScheduledAt: "",
				},
			},
		},
	}

	entries := state.CourtQueueEntriesOf(comp, poolMatches, bracket)
	require.Len(t, entries, 2)

	positions := state.CourtQueuePositions(entries)
	require.Equal(t, []int{1, 2}, positions)
}

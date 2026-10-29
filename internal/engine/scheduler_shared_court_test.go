package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func TestSharedCourt_PoolsDrawnSecondStartsAtFirstPlannedEnd(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Date: "2026-11-01",
	}))

	// Comp 1: a 3-player league on Court A, 180s bout -> 5m per bout; the league
	// scheduler keeps a competitor from fighting twice in a row, so its last
	// bout starts at 09:20 -> ends 09:25
	createTestCompetition(t, store, "comp-first", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-first", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-first"))

	ends1, err := eng.CourtEnds("comp-first")
	require.NoError(t, err)
	assert.Equal(t, "09:25", ends1["A"])

	// Comp 2: drawn second on Court A on the same day with same StartTime
	createTestCompetition(t, store, "comp-second", state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-second", []string{"Dave", "Eve", "Frank", "Grace", "Heidi", "Ivan"})
	require.NoError(t, eng.GenerateDraw("comp-second"))

	pm2, err := store.LoadPoolMatches("comp-second")
	require.NoError(t, err)
	require.NotEmpty(t, pm2)

	assert.Equal(t, "09:25", pm2[0].ScheduledAt, "second competition on Court A must start at the first competition's planned end")
}

func TestSharedCourt_KnockoutDrawnSecondStartsAtFirstPlannedEnd(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Date: "2026-11-01",
	}))

	// Comp 1: on Court A, ends at 09:25
	createTestCompetition(t, store, "comp-pools", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-pools", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-pools"))

	// Comp KO: knockout only on Court A
	createTestCompetition(t, store, "comp-ko", state.CompFormatKnockout, 0, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-ko", []string{"Dave", "Eve", "Frank", "Grace"})
	require.NoError(t, eng.GenerateDraw("comp-ko"))

	b, err := store.LoadBracket("comp-ko")
	require.NoError(t, err)
	require.NotNil(t, b)

	var earliest string
	for _, round := range b.Rounds {
		for _, m := range round {
			if m.Court == "A" && m.ScheduledAt != "" {
				if earliest == "" || m.ScheduledAt < earliest {
					earliest = m.ScheduledAt
				}
			}
		}
	}
	assert.Equal(t, "09:25", earliest, "knockout matches must start at the floor left by the first competition")
}

// A floor on one court never moves the knockout on another court of the same
// competition (assignBracketMatchSlots sends a court with no entry in its map
// to the latest start in it, which would otherwise be the floor).
func TestSharedCourt_KnockoutFloorOnOneCourtLeavesTheOtherAlone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Date:   "2026-11-01",
		Courts: []string{"A", "B"},
	}))

	createTestCompetition(t, store, "comp-pools", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-pools", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-pools"))

	createTestCompetition(t, store, "comp-ko", state.CompFormatKnockout, 0, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-ko", []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"})
	require.NoError(t, eng.GenerateDraw("comp-ko"))

	b, err := store.LoadBracket("comp-ko")
	require.NoError(t, err)
	require.NotNil(t, b)

	earliest := map[string]string{}
	for _, round := range b.Rounds {
		for _, m := range round {
			if m.ScheduledAt == "" || m.Status == state.MatchStatusCompleted {
				continue
			}
			if cur, ok := earliest[m.Court]; !ok || m.ScheduledAt < cur {
				earliest[m.Court] = m.ScheduledAt
			}
		}
	}
	require.Contains(t, earliest, "A")
	require.Contains(t, earliest, "B")
	assert.Equal(t, "09:25", earliest["A"], "the shared court starts at the floor")
	assert.Equal(t, "09:00", earliest["B"], "the court nobody else uses keeps the day start")
}

func TestSharedCourt_LeagueDrawnSecondShiftsWholeGridToLatestFloor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Date:   "2026-11-01",
		Courts: []string{"A", "B"},
	}))

	// Comp 1 on Court A only, ends at 09:25
	createTestCompetition(t, store, "comp-first", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-first", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-first"))

	// Comp League uses Courts A and B. Court A is floored at 09:25, Court B has no floor (09:00).
	// The league grid must anchor the entire grid at max(09:00, 09:25) = 09:25.
	createTestCompetition(t, store, "comp-league", state.CompFormatLeague, 0, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-league", []string{"Dave", "Eve", "Frank", "Grace"})
	require.NoError(t, eng.GenerateDraw("comp-league"))

	lm, err := store.LoadPoolMatches("comp-league")
	require.NoError(t, err)
	require.NotEmpty(t, lm)

	// The grid moves as a whole: the first slot on BOTH courts is the floor,
	// so no competitor can be on two courts at once.
	firstOn := map[string]string{}
	for _, m := range lm {
		require.NotEmpty(t, m.ScheduledAt, "league match %s has no time", m.ID)
		assert.GreaterOrEqual(t, m.ScheduledAt, "09:25", "league match %s at %s must not be before 09:25", m.ID, m.ScheduledAt)
		if cur, ok := firstOn[m.Court]; !ok || m.ScheduledAt < cur {
			firstOn[m.Court] = m.ScheduledAt
		}
	}
	assert.Equal(t, "09:25", firstOn["A"], "floored court A starts at the floor")
	assert.Equal(t, "09:25", firstOn["B"], "unfloored court B moves with the league grid")
}

func TestSharedCourt_EqualStartTimeDrawnInEitherOrder(t *testing.T) {
	// Case 1: Draw A then B -> B goes after A
	t.Run("draw A then B", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{Date: "2026-11-01"}))

		createTestCompetition(t, store, "cA", state.CompFormatLeague, 3, func(c *state.Competition) {
			c.Date = "2026-11-01"
			c.Courts = []string{"A"}
			c.StartTime = "09:00"
			c.PoolMatchDurationSeconds = 180
		})
		saveTestParticipants(t, store, "cA", []string{"A1", "A2", "A3"})

		createTestCompetition(t, store, "cB", state.CompFormatLeague, 3, func(c *state.Competition) {
			c.Date = "2026-11-01"
			c.Courts = []string{"A"}
			c.StartTime = "09:00"
			c.PoolMatchDurationSeconds = 180
		})
		saveTestParticipants(t, store, "cB", []string{"B1", "B2", "B3"})

		require.NoError(t, eng.GenerateDraw("cA"))
		require.NoError(t, eng.GenerateDraw("cB"))

		pmA, err := store.LoadPoolMatches("cA")
		require.NoError(t, err)
		pmB, err := store.LoadPoolMatches("cB")
		require.NoError(t, err)

		assert.Equal(t, "09:00", pmA[0].ScheduledAt)
		assert.Equal(t, "09:25", pmB[0].ScheduledAt)
	})

	// Case 2: Draw B then A -> A goes after B
	t.Run("draw B then A", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{Date: "2026-11-01"}))

		createTestCompetition(t, store, "cA", state.CompFormatLeague, 3, func(c *state.Competition) {
			c.Date = "2026-11-01"
			c.Courts = []string{"A"}
			c.StartTime = "09:00"
			c.PoolMatchDurationSeconds = 180
		})
		saveTestParticipants(t, store, "cA", []string{"A1", "A2", "A3"})

		createTestCompetition(t, store, "cB", state.CompFormatLeague, 3, func(c *state.Competition) {
			c.Date = "2026-11-01"
			c.Courts = []string{"A"}
			c.StartTime = "09:00"
			c.PoolMatchDurationSeconds = 180
		})
		saveTestParticipants(t, store, "cB", []string{"B1", "B2", "B3"})

		require.NoError(t, eng.GenerateDraw("cB"))
		require.NoError(t, eng.GenerateDraw("cA"))

		pmA, err := store.LoadPoolMatches("cA")
		require.NoError(t, err)
		pmB, err := store.LoadPoolMatches("cB")
		require.NoError(t, err)

		assert.Equal(t, "09:00", pmB[0].ScheduledAt)
		assert.Equal(t, "09:25", pmA[0].ScheduledAt)
	})
}

func TestSharedCourt_AnotherDayDoesNotFloor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Date: "2026-11-01"}))

	// Comp on 2026-11-02
	createTestCompetition(t, store, "comp-day2", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-02"
		c.Courts = []string{"A"}
		c.StartTime = "08:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-day2", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-day2"))

	// Comp on 2026-11-01
	createTestCompetition(t, store, "comp-day1", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-day1", []string{"Dave", "Eve", "Frank"})
	require.NoError(t, eng.GenerateDraw("comp-day1"))

	pm1, err := store.LoadPoolMatches("comp-day1")
	require.NoError(t, err)
	assert.Equal(t, "09:00", pm1[0].ScheduledAt, "competition on 2026-11-01 must not be floored by 2026-11-02")
}

func TestSharedCourt_UnsharedCourtUnchanged(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Date: "2026-11-01", Courts: []string{"A", "B"}}))

	// Comp 1 on Court A only
	createTestCompetition(t, store, "comp-first", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-first", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.GenerateDraw("comp-first"))

	// Comp 2 on Courts A and B (2 pools of 3, 6 players total)
	createTestCompetition(t, store, "comp-second", state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180
	})
	saveTestParticipants(t, store, "comp-second", []string{"P1", "P2", "P3", "P4", "P5", "P6"})
	require.NoError(t, eng.GenerateDraw("comp-second"))

	pm2, err := store.LoadPoolMatches("comp-second")
	require.NoError(t, err)

	var firstA, firstB string
	for _, m := range pm2 {
		if m.Court == "A" && firstA == "" {
			firstA = m.ScheduledAt
		}
		if m.Court == "B" && firstB == "" {
			firstB = m.ScheduledAt
		}
	}
	assert.Equal(t, "09:25", firstA, "shared court A must start at floor")
	assert.Equal(t, "09:00", firstB, "unshared court B must start at standard day start")
}

func TestSharedCourt_AppendWithSlotsPlacesInjectedRowAfterOwnLastRow(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	tourn := &state.Tournament{Date: "2026-11-01"}
	comp := &state.Competition{
		ID:                       "comp-inj",
		Date:                     "2026-11-01",
		Courts:                   []string{"A"},
		StartTime:                "09:00",
		PoolMatchDurationSeconds: 180, // 5 min per match
	}
	require.NoError(t, store.SaveCompetition(comp))

	// Competition already has matches on Court A: 10:00 and 10:05 (ends 10:10)
	prior := []state.MatchResult{
		{ID: "m1", Court: "A", ScheduledAt: "10:00", Status: state.MatchStatusScheduled},
		{ID: "m2", Court: "A", ScheduledAt: "10:05", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches("comp-inj", prior))

	injected := []state.MatchResult{
		{
			ID:    "m-rep",
			Court: "A",
		},
	}

	result := appendWithSlots(prior, injected, comp, tourn)
	require.Len(t, result, 3)
	assert.Equal(t, "10:10", result[2].ScheduledAt, "injected match must be placed after the competition's own last row on court A")
}

func TestSharedCourt_SwissRoundTwoLandsAfterRoundOne(t *testing.T) {
	eng, store, compID, _ := setupSwissCompetition(t, []string{"Alice", "Bob", "Charlie", "Dave"}, nil, 3)
	require.NoError(t, store.SaveTournament(&state.Tournament{Date: "2026-11-01"}))

	// Set date on competition
	_, err := store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.Date = "2026-11-01"
		c.Courts = []string{"A"}
		c.StartTime = "09:00"
		c.PoolMatchDurationSeconds = 180 // 5m per match
		return c, nil
	})
	require.NoError(t, err)

	// Round 1: 4 players on 1 court -> 2 matches: 09:00, 09:05 (ends 09:10)
	ms1, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, ms1))
	_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
		c.SwissCurrentRound = 1
		return c, nil
	})
	require.NoError(t, err)

	for _, m := range ms1 {
		completeSwissMatch(t, store, compID, m.ID, m.SideA)
	}

	// Advance to round 2
	ms2, nextRound, err := eng.AdvanceSwissRound(compID)
	require.NoError(t, err)
	assert.Equal(t, 2, nextRound)
	require.NotEmpty(t, ms2)

	assert.Equal(t, "09:10", ms2[0].ScheduledAt, "round 2 match on court A must land after round 1's end")
}

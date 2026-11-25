package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

// drawSeededComp draws a pools + knockout (or knockout-only) competition of n
// competitors, one per dojo, the first `seeds` of them seeded 1, 2, ... in
// roster order, and returns its pools (nil for a knockout-only draw).
func drawSeededComp(t *testing.T, eng *Engine, store *state.Store, compID, format string, n, seeds int, opts ...func(*state.Competition)) []helper.Pool {
	t.Helper()
	createTestCompetition(t, store, compID, format, 3, opts...)
	players := make([]domain.Player, n)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: fmt.Sprintf("Dojo %02d", i+1)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	var assignments []domain.SeedAssignment
	for r := 1; r <= seeds; r++ {
		assignments = append(assignments, domain.SeedAssignment{Name: players[r-1].Name, Dojo: players[r-1].Dojo, SeedRank: r})
	}
	require.NoError(t, store.SaveSeeds(compID, assignments))
	require.NoError(t, eng.GenerateDraw(compID))
	if format == state.CompFormatKnockout {
		return nil
	}
	return loadSwapPools(t, store, compID)
}

// seededRow finds the pools.csv row of seed rank in pools.
func seededRow(t *testing.T, pools []helper.Pool, rank int) (pool, row int) {
	t.Helper()
	for i, p := range pools {
		for j, pl := range p.Players {
			if pl.Seed == rank {
				return i, j
			}
		}
	}
	require.Failf(t, "no seed", "seed %d is in no pool", rank)
	return 0, 0
}

// unseededRow is the last unseeded row of pools[pool].
func unseededRow(t *testing.T, pools []helper.Pool, pool int) int {
	t.Helper()
	for j := len(pools[pool].Players) - 1; j >= 0; j-- {
		if pools[pool].Players[j].Seed == 0 {
			return j
		}
	}
	require.Failf(t, "no unseeded row", "pool %s holds seeds only", pools[pool].PoolName)
	return 0
}

// freshPreview builds the knockout preview a draw would make of the pools now
// on disk, with no court floor (the fixtures share no court with another
// competition).
func freshPreview(t *testing.T, eng *Engine, store *state.Store, compID string) *state.Bracket {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	tournament, err := store.LoadTournament()
	require.NoError(t, err)
	b, err := eng.buildPoolPreviewBracket(comp, loadSwapPools(t, store, compID), loadSwapMatches(t, store, compID), nil, tournament)
	require.NoError(t, err)
	require.NotNil(t, b)
	return b
}

type slotTime struct{ court, at string }

// scheduleOf maps each bracket match id to its court and time.
func scheduleOf(b *state.Bracket) map[string]slotTime {
	out := map[string]slotTime{}
	for _, m := range bracketMatches(b) {
		out[m.ID] = slotTime{m.Court, m.ScheduledAt}
	}
	return out
}

// sides maps each bracket match id to its two side labels.
func sides(b *state.Bracket) map[string][2]string {
	out := map[string][2]string{}
	for _, m := range bracketMatches(b) {
		out[m.ID] = [2]string{m.SideA, m.SideB}
	}
	return out
}

// toPlayChanged says some match id is a bye in one bracket and a match to
// fight in the other.
func toPlayChanged(a, b *state.Bracket) bool {
	before := map[string]bool{}
	for _, m := range bracketMatches(a) {
		before[m.ID] = toPlay(m)
	}
	for _, m := range bracketMatches(b) {
		if before[m.ID] != toPlay(m) {
			return true
		}
	}
	return false
}

// --- pools + knockout -------------------------------------------------------

// A seeded competitor swapped into another pool rebuilds the knockout from the
// new pools: the bracket the swap stores is the one a draw of those pools
// builds, the answer warns with the seed and where it goes, and, when no
// match changes between bye and bout, every match keeps its stored court and
// time, an organiser's move included.
func TestSwapDrawPlaces_SeededSwapRebuildsTheKnockout(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-seeded"
	pools := drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 15, 2, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
	})
	require.GreaterOrEqual(t, len(pools), 3)
	sp, sr := seededRow(t, pools, 1)
	p2, _ := seededRow(t, pools, 2)
	op := (sp + 1) % len(pools)
	if op == p2 {
		op = (op + 1) % len(pools)
	}
	a, b := poolSwapSide(pools, sp, sr), poolSwapSide(pools, op, unseededRow(t, pools, op))
	seeded := pools[sp].Players[sr]

	// The organiser moved one knockout match before reviewing the draw.
	stored, err := store.LoadBracket(compID)
	require.NoError(t, err)
	var moved string
	for _, m := range bracketMatches(stored) {
		if toPlay(m) {
			m.ScheduledAt, m.QueuePosition, moved = "17:45", 9, m.ID
			break
		}
	}
	require.NotEmpty(t, moved)
	require.NoError(t, store.SaveBracket(compID, stored))
	before, err := store.LoadBracket(compID)
	require.NoError(t, err)
	filesBefore := drawFiles(t, dir, compID)

	dry, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b, DryRun: true})
	require.NoError(t, err)
	assert.True(t, dry.BracketRebuilt, "the dry run says the knockout is rebuilt")
	assert.Equal(t, filesBefore, drawFiles(t, dir, compID), "a dry run saves nothing")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.True(t, got.BracketRebuilt)
	assert.Equal(t, []SwapSeedWarning{{ID: seeded.ID, Name: seeded.Name, Seed: 1, To: b.Place}}, got.SeededWarning,
		"the seed, and the place they go")
	assert.Equal(t, dry.SeededWarning, got.SeededWarning)
	assert.Equal(t, dry.TimesRescheduled, got.TimesRescheduled)
	assert.Equal(t, dry.Separation, got.Separation, "the dry run answers the swap it would make")

	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, sides(freshPreview(t, eng, store, compID)), sides(after), "the stored knockout is the one the swapped pools draw")
	assert.NotEqual(t, sides(before), sides(after), "the seed's move reorders the knockout's pool places")
	assert.True(t, after.Preview)
	require.False(t, toPlayChanged(before, after), "the fixture keeps every bye where it was")
	assert.False(t, got.TimesRescheduled)
	assert.Equal(t, scheduleOf(before), scheduleOf(after), "every match keeps its court and time by id")
	assert.Equal(t, 9, after.MatchByID(moved).QueuePosition, "an organiser's move stands")

	// The seed went with the competitor; the place kept its number.
	newPools := loadSwapPools(t, store, compID)
	assert.Equal(t, seeded.ID, newPools[op].Players[b.Place.Position-1].ID)
	assert.Equal(t, 1, newPools[op].Players[b.Place.Position-1].Seed)
	assert.Equal(t, 0, newPools[sp].Players[sr].Seed)
	assert.Equal(t, pools[sp].Players[sr].Number, newPools[sp].Players[sr].Number)
	seeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	assert.Len(t, seeds, 2, "seeds.csv is the competitor's, not the place's")

	// A resend changes nothing and rebuilds nothing.
	again, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	assert.Equal(t, SwapAlreadySwapped, again.Reason)
	assert.False(t, again.BracketRebuilt)
	assert.Empty(t, again.SeededWarning)
}

// Export parity: after a seeded swap that rebuilt the knockout, the workbook
// the export re-derives from the stored pools prints the stored bracket.
func TestSwapDrawPlaces_SeededSwapKeepsExportParity(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-seeded-export"
	pools := drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 15, 1, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
	})
	sp, sr := seededRow(t, pools, 1)
	op := (sp + 1) % len(pools)
	before, err := store.LoadBracket(compID)
	require.NoError(t, err)
	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: poolSwapSide(pools, sp, sr), B: poolSwapSide(pools, op, unseededRow(t, pools, op))})
	require.NoError(t, err)
	require.True(t, got.BracketRebuilt)
	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEqual(t, sides(before), sides(after), "the fixture reorders the knockout")

	assertWorkbookMatchesBracket(t, eng, store, compID)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	draw := poolDraw(comp, loadSwapPools(t, store, compID), len(comp.Courts))
	require.NotNil(t, draw)
	stored, err := store.LoadBracket(compID)
	require.NoError(t, err)
	var firstRound []string
	for _, m := range stored.Rounds[0] {
		firstRound = append(firstRound, m.PlaceholderA, m.PlaceholderB)
	}
	assert.Equal(t, helper.SlotArray(draw.Root), firstRound, "the export's draw is the stored first round")
}

// When a seed changing pool moves which match is a bye, the builder's whole
// new schedule is taken, never a mix of the old and the new, and the answer
// says so.
func TestSwapDrawPlaces_SeededSwapThatMovesAByeReschedules(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-seeded-bye"
	pools := drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 7, 1, func(c *state.Competition) {
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
	})
	require.Len(t, pools, 2)
	before, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sp, sr := seededRow(t, pools, 1)
	op := 1 - sp

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: poolSwapSide(pools, sp, sr), B: poolSwapSide(pools, op, unseededRow(t, pools, op))})
	require.NoError(t, err)
	assert.True(t, got.BracketRebuilt)
	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, toPlayChanged(before, after), "the fixture moves a bye")
	assert.True(t, got.TimesRescheduled)

	assert.Equal(t, scheduleOf(freshPreview(t, eng, store, compID)), scheduleOf(after), "the new schedule whole")
	seen := map[slotTime]string{}
	for _, m := range bracketMatches(after) {
		if !toPlay(m) || m.ScheduledAt == "" {
			continue
		}
		at := slotTime{m.Court, m.ScheduledAt}
		assert.Empty(t, seen[at], "%s and %s share a court and a time", seen[at], m.ID)
		seen[at] = m.ID
	}
}

// A swap inside one pool, a seed included, and a swap of two unseeded
// competitors in different pools leave the knockout and warn of nothing.
func TestSwapDrawPlaces_SeedStayingInItsPoolRebuildsNothing(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-seeded-within"
	pools := drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 9, 1)
	require.Len(t, pools, 3)
	sp, sr := seededRow(t, pools, 1)
	bracketBefore := drawFiles(t, dir, compID)["bracket.json"]

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: poolSwapSide(pools, sp, sr), B: poolSwapSide(pools, sp, unseededRow(t, pools, sp))})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.False(t, got.BracketRebuilt)
	assert.False(t, got.TimesRescheduled)
	assert.Empty(t, got.SeededWarning, "the seed stays in their pool")
	assert.Equal(t, bracketBefore, drawFiles(t, dir, compID)["bracket.json"])

	pools = loadSwapPools(t, store, compID)
	op, o2 := (sp+1)%len(pools), (sp+2)%len(pools)
	got, err = eng.SwapDrawPlaces(compID, SwapRequest{A: poolSwapSide(pools, op, unseededRow(t, pools, op)), B: poolSwapSide(pools, o2, unseededRow(t, pools, o2))})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.False(t, got.BracketRebuilt)
	assert.Empty(t, got.SeededWarning)
	assert.Equal(t, bracketBefore, drawFiles(t, dir, compID)["bracket.json"])
}

// A knockout-only seeded swap warns, and trades the two competitors in the
// first round without rebuilding anything.
func TestSwapDrawPlaces_SeededKnockoutSwapWarns(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-seeded-ko"
	drawSeededComp(t, eng, store, compID, state.CompFormatKnockout, 8, 1)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	roster, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var seeded domain.Player
	for _, p := range roster {
		if p.Seed == 1 {
			seeded = p
		}
	}
	require.NotEmpty(t, seeded.ID)
	atSeed, ok := LeafSlotOf(b, seeded.ID)
	require.True(t, ok)
	var other SwapSide
	for _, id := range b.DrawOrder {
		if id == seeded.ID {
			continue
		}
		place, ok := LeafSlotOf(b, id)
		require.True(t, ok)
		other = SwapSide{ID: id, Place: place}
	}

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: seeded.ID, Place: atSeed}, B: other})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.False(t, got.BracketRebuilt)
	assert.False(t, got.TimesRescheduled)
	assert.Equal(t, []SwapSeedWarning{{ID: seeded.ID, Name: seeded.Name, Seed: 1, To: other.Place}}, got.SeededWarning)
}

// --- the schedule rule ------------------------------------------------------

func TestSwapDrawPlaces_KeepBracketSchedule(t *testing.T) {
	stamped := func(id string, status state.MatchStatus, court, at string) state.BracketMatch {
		return state.BracketMatch{ID: id, Status: status, Court: court, ScheduledAt: at, ModifiedAt: 7, GroupStamps: map[string]int64{"result": 7}}
	}
	stored := func() *state.Bracket {
		return &state.Bracket{Rounds: [][]state.BracketMatch{
			{stamped("m-r1-0", state.MatchStatusScheduled, "A", "10:00"), stamped("m-r1-1", state.MatchStatusCompleted, "B", "")},
			{stamped("m-r2-0", state.MatchStatusScheduled, "A", "11:00")},
		}}
	}
	rebuilt := func() *state.Bracket {
		return &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:30"}, {ID: "m-r1-1", Status: state.MatchStatusCompleted, Court: "B"}},
			{{ID: "m-r2-0", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:30"}},
		}}
	}

	t.Run("same byes keep the stored schedule", func(t *testing.T) {
		r := rebuilt()
		assert.False(t, keepBracketSchedule(stored(), r))
		assert.Equal(t, scheduleOf(stored()), scheduleOf(r))
		assert.Equal(t, int64(7), r.Rounds[0][0].ModifiedAt, "the stamps stay")
		assert.Equal(t, map[string]int64{"result": 7}, r.Rounds[1][0].GroupStamps)
	})
	t.Run("a moved bye takes the new schedule whole", func(t *testing.T) {
		r := rebuilt()
		r.Rounds[0][1].Status, r.Rounds[0][1].ScheduledAt = state.MatchStatusScheduled, "09:45"
		want := scheduleOf(r)
		assert.True(t, keepBracketSchedule(stored(), r))
		assert.Equal(t, want, scheduleOf(r), "never a mix")
		assert.Equal(t, int64(7), r.Rounds[0][0].ModifiedAt)
	})
	t.Run("a match that no longer exists is dropped", func(t *testing.T) {
		r := rebuilt()
		r.Rounds[0] = r.Rounds[0][:1]
		want := scheduleOf(r)
		assert.True(t, keepBracketSchedule(stored(), r))
		assert.Equal(t, want, scheduleOf(r))
	})
	t.Run("a new match is scheduled as the draw schedules it", func(t *testing.T) {
		r := rebuilt()
		r.ThirdPlaceMatch = &state.BracketMatch{ID: state.BronzeMatchID, Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:15"}
		want := scheduleOf(r)
		assert.True(t, keepBracketSchedule(stored(), r))
		assert.Equal(t, want, scheduleOf(r))
	})
}

package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

func swapSlot(i int) DrawPlace { return DrawPlace{Slot: &i} }

// poolSwapSide names the competitor at pools[pool].Players[row] at the place
// the store holds them.
func poolSwapSide(pools []helper.Pool, pool, row int) SwapSide {
	return SwapSide{ID: pools[pool].Players[row].ID, Place: DrawPlace{Pool: pools[pool].PoolName, Position: row + 1}}
}

// drawFiles reads the three draw files of compID as they are on disk, so a
// test can say a refused or dry-run swap wrote nothing.
func drawFiles(t *testing.T, dir, compID string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, name := range []string{"pools.csv", "pool-matches.csv", "bracket.json"} {
		raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, name))
		if err != nil && !os.IsNotExist(err) {
			require.NoError(t, err)
		}
		out[name] = string(raw)
	}
	return out
}

func loadSwapPools(t *testing.T, store *state.Store, compID string) []helper.Pool {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools)
	return pools
}

func loadSwapMatches(t *testing.T, store *state.Store, compID string) []state.MatchResult {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	return matches
}

func requireSwapRefused(t *testing.T, err error, code string) *SwapRefusedError {
	t.Helper()
	var refused *SwapRefusedError
	require.True(t, errors.As(err, &refused), "want a %s refusal, got %v", code, err)
	assert.Equal(t, code, refused.Code, refused.Message)
	assert.NotEmpty(t, refused.Message)
	return refused
}

// exchanged is id with a and b traded.
func exchanged(id, a, b string) string {
	switch id {
	case a:
		return b
	case b:
		return a
	}
	return id
}

// assertPoolMatchesExchanged checks that every side and winner of after is
// before's with a and b traded, by id and by the name the pools give them,
// and that nothing else of a match moved.
func assertPoolMatchesExchanged(t *testing.T, before, after []state.MatchResult, pools []helper.Pool, a, b string) {
	t.Helper()
	names := map[string]string{}
	for _, p := range pools {
		for _, pl := range p.Players {
			names[pl.ID] = pl.Name
		}
	}
	require.Len(t, after, len(before))
	for i := range before {
		bm, am := before[i], after[i]
		assert.Equal(t, bm.ID, am.ID)
		assert.Equal(t, bm.Court, am.Court, bm.ID)
		assert.Equal(t, bm.ScheduledAt, am.ScheduledAt, bm.ID)
		for _, s := range []struct{ id, name, gotID, gotName string }{
			{bm.SideAID, bm.SideA, am.SideAID, am.SideA},
			{bm.SideBID, bm.SideB, am.SideBID, am.SideB},
			{bm.WinnerID, bm.Winner, am.WinnerID, am.Winner},
		} {
			want := exchanged(s.id, a, b)
			assert.Equal(t, want, s.gotID, bm.ID)
			if want != s.id {
				assert.Equal(t, names[want], s.gotName, bm.ID)
			} else {
				assert.Equal(t, s.name, s.gotName, bm.ID)
			}
		}
	}
}

// bracketSlots lists every (match, slot) id and name of a bracket in
// forEachBracketSlot's order.
func bracketSlots(b *state.Bracket) (ids, names []string) {
	forEachBracketSlot(b, func(_ *state.BracketMatch, _ domain.MatchSide, name, id *string) {
		ids = append(ids, *id)
		names = append(names, *name)
	})
	return ids, names
}

// --- pools draw -------------------------------------------------------------

func TestSwapDrawPlaces_PoolsTradeTheCompetitorAndKeepThePlace(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-pools"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6), func(c *state.Competition) { c.NumberPrefix = "K" })
	pools := loadSwapPools(t, store, compID)
	require.GreaterOrEqual(t, len(pools), 2)
	matchesBefore := loadSwapMatches(t, store, compID)
	bracketBefore := drawFiles(t, dir, compID)["bracket.json"]
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 1)
	rowA, rowB := pools[0].Players[0], pools[1].Players[1]

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.Empty(t, got.Reason)
	assert.False(t, got.DryRun)
	assert.Equal(t, SwapPlaces{A: b.Place, B: a.Place}, got.Places)
	require.Len(t, got.Moves, 2)
	assert.Equal(t, SwapMove{ID: a.ID, Name: rowA.Name, DisplayName: rowA.DisplayName, From: a.Place, To: b.Place}, got.Moves[0])
	assert.Equal(t, SwapMove{ID: b.ID, Name: rowB.Name, DisplayName: rowB.DisplayName, From: b.Place, To: a.Place}, got.Moves[1])
	assert.Equal(t, []SwapNumber{
		{ID: a.ID, Name: rowA.Name, From: rowA.Number, To: rowB.Number},
		{ID: b.ID, Name: rowB.Name, From: rowB.Number, To: rowA.Number},
	}, got.Numbers, "numbers stay with the place, so the two trade tags")

	after := loadSwapPools(t, store, compID)
	placeA, placeB := after[0].Players[0], after[1].Players[1]
	assert.Equal(t, rowB.ID, placeA.ID)
	assert.Equal(t, rowB.Name, placeA.Name)
	assert.Equal(t, rowB.Dojo, placeA.Dojo)
	assert.Equal(t, rowA.Number, placeA.Number, "the place keeps its number")
	assert.Equal(t, rowA.ID, placeB.ID)
	assert.Equal(t, rowA.Dojo, placeB.Dojo)
	assert.Equal(t, rowB.Number, placeB.Number)
	for i := range pools {
		assert.Len(t, after[i].Players, len(pools[i].Players), "no row is added or lost")
	}

	assertPoolMatchesExchanged(t, matchesBefore, loadSwapMatches(t, store, compID), after, a.ID, b.ID)
	assert.Equal(t, bracketBefore, drawFiles(t, dir, compID)["bracket.json"], "the knockout's places are pool places, not competitors")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusDrawReady, comp.Status)
	for _, id := range []string{a.ID, b.ID} {
		history, err := store.LoadMatchHistory(compID, id)
		require.NoError(t, err)
		assert.Empty(t, history)
	}
}

func TestSwapDrawPlaces_WithinOnePoolFlipsTheMatchHoldingBoth(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-within"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6))
	pools := loadSwapPools(t, store, compID)
	matchesBefore := loadSwapMatches(t, store, compID)
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 0, 2)

	both := -1
	for i, m := range matchesBefore {
		if (m.SideAID == a.ID && m.SideBID == b.ID) || (m.SideAID == b.ID && m.SideBID == a.ID) {
			both = i
		}
	}
	require.GreaterOrEqual(t, both, 0, "a round robin of three holds the two")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	assert.True(t, got.Applied)

	after := loadSwapPools(t, store, compID)
	assert.Equal(t, b.ID, after[0].Players[0].ID)
	assert.Equal(t, a.ID, after[0].Players[2].ID)

	matchesAfter := loadSwapMatches(t, store, compID)
	assertPoolMatchesExchanged(t, matchesBefore, matchesAfter, after, a.ID, b.ID)
	assert.Equal(t, matchesBefore[both].SideAID, matchesAfter[both].SideBID, "both sides flip")
	assert.Equal(t, matchesBefore[both].SideBID, matchesAfter[both].SideAID)
}

// --- knockout only ----------------------------------------------------------

func TestSwapDrawPlaces_KnockoutIntoAByeSlot(t *testing.T) {
	// Six entrants in an eight-slot bracket: two byes the draw resolved when
	// it was generated, which do not count as a result.
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-ko-bye"
	drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(6), func(c *state.Competition) { c.NumberPrefix = "K" })
	before := loadBracket(t, store, compID)
	holders := byeHolders(before)
	require.NotEmpty(t, holders, "fixture must hold a bye")

	var x, y string
	var sx, sy int
	for mi, m := range before.Rounds[0] {
		for side, id := range [2]string{m.SideAID, m.SideBID} {
			switch {
			case id == "":
			case holders[id] && x == "":
				x, sx = id, 2*mi+side
			case !holders[id] && y == "":
				y, sy = id, 2*mi+side
			}
		}
	}
	require.NotEmpty(t, x)
	require.NotEmpty(t, y)
	byeMatch := before.Rounds[0][sx/2]
	require.Equal(t, x, byeMatch.WinnerID, "the draw resolved the bye")
	numbers := placeNumbers(&state.Competition{NumberPrefix: "K"}, nil, before, DrawInBracket)
	idsBefore, _ := bracketSlots(before)

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: x, Place: swapSlot(sx)}, B: SwapSide{ID: y, Place: swapSlot(sy)}})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	assert.Equal(t, SwapPlaces{A: swapSlot(sy), B: swapSlot(sx)}, got.Places)
	assert.ElementsMatch(t, []SwapBye{{ID: x, Name: got.Moves[0].Name, Bye: false}, {ID: y, Name: got.Moves[1].Name, Bye: true}}, got.Byes)
	assert.Equal(t, []SwapNumber{
		{ID: x, Name: got.Moves[0].Name, From: numbers[x], To: numbers[y]},
		{ID: y, Name: got.Moves[1].Name, From: numbers[y], To: numbers[x]},
	}, got.Numbers)

	after := loadBracket(t, store, compID)
	idsAfter, namesAfter := bracketSlots(after)
	require.Len(t, idsAfter, len(idsBefore))
	nameOf := map[string]string{x: got.Moves[0].Name, y: got.Moves[1].Name}
	for i := range idsBefore {
		want := exchanged(idsBefore[i], x, y)
		assert.Equal(t, want, idsAfter[i], "slot %d", i)
		if want != idsBefore[i] {
			assert.Equal(t, nameOf[want], namesAfter[i], "slot %d", i)
		}
	}
	m, side, ok := FirstRoundLeaf(after, sx)
	require.True(t, ok)
	assert.Equal(t, y, map[domain.MatchSide]string{domain.MatchSideA: m.SideAID, domain.MatchSideB: m.SideBID}[side])
	assert.Equal(t, y, m.WinnerID, "the bye's winner follows the competitor")
	assert.Equal(t, nameOf[y], m.Winner)
	assert.Equal(t, before.Rounds[0][sx/2].Court, m.Court, "courts do not move")
	assert.Equal(t, before.Rounds[0][sx/2].ScheduledAt, m.ScheduledAt, "times do not move")
	assert.Equal(t, numbers[x], placeNumbers(&state.Competition{NumberPrefix: "K"}, nil, after, DrawInBracket)[y], "the number stays with the place")

	pos := drawPositions(before.DrawOrder)
	assert.Equal(t, y, after.DrawOrder[pos[x]])
	assert.Equal(t, x, after.DrawOrder[pos[y]])
}

func TestSwapDrawPlaces_KnockoutOpponentsFlip(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-ko-opp"
	drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(8))
	before := loadBracket(t, store, compID)
	m := before.Rounds[0][0]
	require.NotEmpty(t, m.SideAID)
	require.NotEmpty(t, m.SideBID)

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: m.SideAID, Place: swapSlot(0)}, B: SwapSide{ID: m.SideBID, Place: swapSlot(1)}})
	require.NoError(t, err)
	assert.True(t, got.Applied)
	require.Len(t, got.Numbers, 2)
	assert.Equal(t, got.Numbers[0].From, got.Numbers[1].To, "the two trade the numbers of their places")
	assert.Equal(t, got.Numbers[1].From, got.Numbers[0].To)

	after := loadBracket(t, store, compID).Rounds[0][0]
	assert.Equal(t, m.SideBID, after.SideAID)
	assert.Equal(t, m.SideB, after.SideA)
	assert.Equal(t, m.SideAID, after.SideBID)
	assert.Equal(t, m.SideA, after.SideB)
}

// --- idempotency and the dry run ----------------------------------------------

func TestSwapDrawPlaces_ResendAndAMovedPlace(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-resend"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6))
	pools := loadSwapPools(t, store, compID)
	a, b, c := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 0), poolSwapSide(pools, 1, 1)
	req := SwapRequest{A: a, B: b}

	_, err := eng.SwapDrawPlaces(compID, req)
	require.NoError(t, err)
	landed := drawFiles(t, dir, compID)

	again, err := eng.SwapDrawPlaces(compID, req)
	require.NoError(t, err, "a resend of a swap that landed is not a refusal")
	assert.False(t, again.Applied)
	assert.Equal(t, SwapAlreadySwapped, again.Reason)
	assert.Equal(t, SwapPlaces{A: b.Place, B: a.Place}, again.Places)
	assert.Equal(t, landed, drawFiles(t, dir, compID), "a resend changes nothing")

	// a is now at b's old place, so a request naming a at its old place is
	// a draw that changed.
	_, err = eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: c})
	refused := requireSwapRefused(t, err, SwapDrawChanged)
	require.NotNil(t, refused.Places)
	assert.Equal(t, SwapPlaces{A: b.Place, B: c.Place}, *refused.Places, "the refusal says where they are now")
	assert.Equal(t, landed, drawFiles(t, dir, compID))

	// Swap back gives the draw as it was.
	back, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: a.ID, Place: b.Place}, B: SwapSide{ID: b.ID, Place: a.Place}})
	require.NoError(t, err)
	assert.True(t, back.Applied)
	assert.Equal(t, pools, loadSwapPools(t, store, compID))
}

func TestSwapDrawPlaces_DryRunSavesNothingAndAnswersTheSwap(t *testing.T) {
	// Two of Mumeishi kept apart by the draw: moving one into the other's
	// pool makes them share it, which the separation says.
	dojos := append([]string{"Mumeishi", "Mumeishi"}, uniqueDojos(4)...)
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-dry"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, dojos)
	pools := loadSwapPools(t, store, compID)
	var mover SwapSide
	var mate helper.Player
	target := -1
	for pi, p := range pools {
		for ri, pl := range p.Players {
			if pl.Dojo == "Mumeishi" && mover.ID == "" {
				mover = poolSwapSide(pools, pi, ri)
			} else if pl.Dojo == "Mumeishi" {
				mate, target = pl, pi
			}
		}
	}
	require.NotEmpty(t, mover.ID)
	require.GreaterOrEqual(t, target, 0)
	require.NotEqual(t, mover.Place.Pool, pools[target].PoolName, "the draw keeps the two apart")
	var other SwapSide
	for ri, pl := range pools[target].Players {
		if pl.ID != mate.ID {
			other = poolSwapSide(pools, target, ri)
			break
		}
	}
	files := drawFiles(t, dir, compID)

	dry, err := eng.SwapDrawPlaces(compID, SwapRequest{A: mover, B: other, DryRun: true})
	require.NoError(t, err)
	assert.True(t, dry.DryRun)
	assert.False(t, dry.Applied, "a dry run applies nothing")
	assert.Equal(t, files, drawFiles(t, dir, compID), "a dry run saves nothing")

	require.NotEmpty(t, dry.Separation.Before)
	require.NotEmpty(t, dry.Separation.After)
	findPair := func(rows []DojoMeeting) DojoMeeting {
		for _, r := range rows {
			if (r.A.ID == mover.ID && r.B.ID == mate.ID) || (r.A.ID == mate.ID && r.B.ID == mover.ID) {
				return r
			}
		}
		t.Fatalf("no Mumeishi pair in %v", rows)
		return DojoMeeting{}
	}
	assert.Empty(t, findPair(dry.Separation.Before).SamePool)
	assert.Equal(t, pools[target].PoolName, findPair(dry.Separation.After).SamePool)

	swapped, err := eng.SwapDrawPlaces(compID, SwapRequest{A: mover, B: other})
	require.NoError(t, err)
	assert.True(t, swapped.Applied)
	swapped.Applied, swapped.DryRun = false, true
	assert.Equal(t, *dry, *swapped, "the dry run answers what the swap does")
}

// --- refusals ---------------------------------------------------------------

func TestSwapDrawPlaces_Refusals(t *testing.T) {
	drawn := func(t *testing.T, compID, format string, opts ...func(*state.Competition)) (*Engine, *state.Store, string) {
		eng, store, dir := setupTestEngine(t)
		drawDojoComp(t, eng, store, compID, format, 3, uniqueDojos(6), opts...)
		return eng, store, dir
	}
	poolsReq := func(t *testing.T, store *state.Store, compID string) SwapRequest {
		pools := loadSwapPools(t, store, compID)
		return SwapRequest{A: poolSwapSide(pools, 0, 0), B: poolSwapSide(pools, 1, 0)}
	}

	t.Run("same competitor", func(t *testing.T) {
		eng, store, _ := drawn(t, "ref-same", state.CompFormatMixed)
		req := poolsReq(t, store, "ref-same")
		req.B = req.A
		_, err := eng.SwapDrawPlaces("ref-same", req)
		requireSwapRefused(t, err, SwapSameCompetitor)
	})

	t.Run("the same place for both competitors", func(t *testing.T) {
		eng, store, dir := drawn(t, "ref-sameplace", state.CompFormatMixed)
		req := poolsReq(t, store, "ref-sameplace")
		req.B.Place = req.A.Place
		files := drawFiles(t, dir, "ref-sameplace")
		_, err := eng.SwapDrawPlaces("ref-sameplace", req)
		refused := requireSwapRefused(t, err, SwapInvalid)
		assert.Contains(t, refused.Message, "same place")
		assert.Equal(t, files, drawFiles(t, dir, "ref-sameplace"))
	})

	t.Run("invalid places", func(t *testing.T) {
		eng, store, _ := drawn(t, "ref-inv", state.CompFormatMixed)
		base := poolsReq(t, store, "ref-inv")
		for name, mutate := range map[string]func(*SwapRequest){
			"no place":      func(r *SwapRequest) { r.A.Place = DrawPlace{} },
			"both shapes":   func(r *SwapRequest) { r.A.Place.Slot = new(int) },
			"slot on pools": func(r *SwapRequest) { r.B.Place = swapSlot(0) },
			"no position":   func(r *SwapRequest) { r.A.Place.Position = 0 },
			"no pool":       func(r *SwapRequest) { r.A.Place.Pool = "" },
			"no id":         func(r *SwapRequest) { r.A.ID = "" },
		} {
			t.Run(name, func(t *testing.T) {
				req := base
				mutate(&req)
				_, err := eng.SwapDrawPlaces("ref-inv", req)
				requireSwapRefused(t, err, SwapInvalid)
			})
		}

		eng, store, _ = drawn(t, "ref-inv-ko", state.CompFormatKnockout)
		b := loadBracket(t, store, "ref-inv-ko")
		m := b.Rounds[0][0]
		_, err := eng.SwapDrawPlaces("ref-inv-ko", SwapRequest{
			A: SwapSide{ID: m.SideAID, Place: DrawPlace{Pool: "Pool A", Position: 1}},
			B: SwapSide{ID: m.SideBID, Place: swapSlot(1)},
		})
		requireSwapRefused(t, err, SwapInvalid)
		_, err = eng.SwapDrawPlaces("ref-inv-ko", SwapRequest{
			A: SwapSide{ID: m.SideAID, Place: swapSlot(-1)},
			B: SwapSide{ID: m.SideBID, Place: swapSlot(1)},
		})
		requireSwapRefused(t, err, SwapInvalid)
	})

	t.Run("no draw", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "ref-nodraw", state.CompFormatMixed, 3)
		saveTestParticipants(t, store, "ref-nodraw", []string{"A", "B", "C", "D"})
		_, err := eng.SwapDrawPlaces("ref-nodraw", SwapRequest{
			A: SwapSide{ID: "x", Place: DrawPlace{Pool: "Pool A", Position: 1}},
			B: SwapSide{ID: "y", Place: DrawPlace{Pool: "Pool A", Position: 2}},
		})
		requireSwapRefused(t, err, SwapNoDraw)
	})

	t.Run("unknown competition", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.SwapDrawPlaces("ref-missing", SwapRequest{})
		var nf *NotFoundError
		assert.True(t, errors.As(err, &nf), "got %v", err)
	})

	for _, format := range []string{state.CompFormatLeague, state.CompFormatSwiss} {
		t.Run("not supported: "+format, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "ref-" + format
			createTestCompetition(t, store, compID, format, 3)
			saveTestParticipants(t, store, compID, []string{"A", "B", "C", "D"})
			if format == state.CompFormatLeague {
				require.NoError(t, eng.GenerateDraw(compID))
			}
			_, err := eng.SwapDrawPlaces(compID, SwapRequest{
				A: SwapSide{ID: "x", Place: DrawPlace{Pool: "Pool A", Position: 1}},
				B: SwapSide{ID: "y", Place: DrawPlace{Pool: "Pool A", Position: 2}},
			})
			requireSwapRefused(t, err, SwapNotSupported)
		})
	}

	t.Run("not in the draw", func(t *testing.T) {
		eng, store, dir := drawn(t, "ref-notin", state.CompFormatMixed)
		req := poolsReq(t, store, "ref-notin")
		files := drawFiles(t, dir, "ref-notin")

		unknown := req
		unknown.B.ID = "00000000-0000-4000-8000-000000000000"
		_, err := eng.SwapDrawPlaces("ref-notin", unknown)
		refused := requireSwapRefused(t, err, SwapNotInDraw)
		assert.Contains(t, refused.Message, "roster")

		comp, err := store.LoadCompetition("ref-notin")
		require.NoError(t, err)
		roster, err := store.LoadParticipants("ref-notin", comp.EffectiveWithZekkenName())
		require.NoError(t, err)
		roster = append(roster, domain.Player{Name: "Late Comer", Dojo: "Dojo 99"})
		require.NoError(t, store.SaveParticipants("ref-notin", roster))
		roster, err = store.LoadParticipants("ref-notin", comp.EffectiveWithZekkenName())
		require.NoError(t, err)
		late := ""
		for _, p := range roster {
			if p.Name == "Late Comer" {
				late = p.ID
			}
		}
		require.NotEmpty(t, late)
		added := req
		added.B.ID = late
		_, err = eng.SwapDrawPlaces("ref-notin", added)
		refused = requireSwapRefused(t, err, SwapNotInDraw)
		assert.Contains(t, refused.Message, "Late Comer")
		assert.Equal(t, files, drawFiles(t, dir, "ref-notin"))
	})

	t.Run("started", func(t *testing.T) {
		eng, store, dir := drawn(t, "ref-started", state.CompFormatMixed)
		req := poolsReq(t, store, "ref-started")
		require.NoError(t, eng.StartCompetition("ref-started"))
		files := drawFiles(t, dir, "ref-started")
		_, err := eng.SwapDrawPlaces("ref-started", req)
		refused := requireSwapRefused(t, err, SwapDrawStarted)
		assert.Equal(t, swapDrawStartedHuman, refused.Message)
		req.DryRun = true
		_, err = eng.SwapDrawPlaces("ref-started", req)
		requireSwapRefused(t, err, SwapDrawStarted)
		assert.Equal(t, files, drawFiles(t, dir, "ref-started"))
	})

	t.Run("a result recorded while still draw-ready", func(t *testing.T) {
		eng, store, dir := drawn(t, "ref-result", state.CompFormatMixed)
		req := poolsReq(t, store, "ref-result")
		m := loadSwapMatches(t, store, "ref-result")[0]
		_, err := store.UpdatePoolMatchByID("ref-result", m.ID, func(r *state.MatchResult) error {
			r.Status = state.MatchStatusRunning
			return nil
		})
		require.NoError(t, err)
		files := drawFiles(t, dir, "ref-result")
		_, err = eng.SwapDrawPlaces("ref-result", req)
		requireSwapRefused(t, err, SwapDrawStarted)
		assert.Equal(t, files, drawFiles(t, dir, "ref-result"))
	})

	t.Run("a scheduled match that holds something fought", func(t *testing.T) {
		for name, mark := range map[string]func(t *testing.T, store *state.Store, compID, matchID string){
			"bout rows": func(t *testing.T, store *state.Store, compID, matchID string) {
				_, err := store.UpdatePoolMatchByID(compID, matchID, func(r *state.MatchResult) error {
					r.SubResults = []state.SubMatchResult{{Position: 1}}
					return nil
				})
				require.NoError(t, err)
			},
			"a history file": func(t *testing.T, store *state.Store, compID, matchID string) {
				require.NoError(t, store.AppendMatchHistory(compID, state.MatchHistoryEntry{MatchID: matchID, Door: "score", Stamp: 1}))
			},
			"a score log file": func(t *testing.T, store *state.Store, compID, matchID string) {
				tap := domain.ScoreEvent{ID: "e1", Session: "s", Seq: 1, Stamp: 10, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "M"}
				require.NoError(t, store.AppendScoreLog(compID, matchID, state.ScoreLogLine{Event: &tap}))
			},
			"a score held on a scheduled match": func(t *testing.T, store *state.Store, compID, matchID string) {
				_, err := store.UpdatePoolMatchByID(compID, matchID, func(r *state.MatchResult) error {
					r.IpponsA = []string{"M"}
					return nil
				})
				require.NoError(t, err)
			},
		} {
			t.Run(name, func(t *testing.T) {
				eng, store, _ := drawn(t, "ref-fought", state.CompFormatMixed)
				req := poolsReq(t, store, "ref-fought")
				m := loadSwapMatches(t, store, "ref-fought")[0]
				require.Equal(t, state.MatchStatusScheduled, m.Status)
				mark(t, store, "ref-fought", m.ID)
				_, err := eng.SwapDrawPlaces("ref-fought", req)
				requireSwapRefused(t, err, SwapDrawStarted)
			})
		}
	})
}

// --- stale writes -------------------------------------------------------------

// A decision names a side by colour alone (kiken, fusenpai, fusensho, hantei),
// so what refuses one after a swap is the competitors its writer saw
// (ForceOptions.Seen), not the score payload's own sides.
func TestSwapDrawPlaces_StaleDecisionIsRefused(t *testing.T) {
	assertRefused := func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string, seenA, seenB string) {
		t.Helper()
		for _, decision := range []string{"kiken", "fusenpai", "fusensho"} {
			_, _, err := eng.RecordDecisionWithOptions(compID, matchID, decision, "aka", "", nil,
				ForceOptions{Seen: SeenSides{AID: seenA, BID: seenB}}, serverNowMs())
			assert.ErrorIs(t, err, ErrMatchSideMismatch, decision)
		}
		statuses, err := store.LoadCompetitorStatus(compID)
		require.NoError(t, err)
		assert.Empty(t, statuses, "a refused decision bars nobody")
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, comp.Status, "a refused decision does not start the competition")
		history, err := store.LoadMatchHistory(compID, matchID)
		require.NoError(t, err)
		assert.Empty(t, history, "a refused decision leaves no history line")
	}

	t.Run("pool match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "stale-decision-pool"
		drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6))
		pools := loadSwapPools(t, store, compID)
		a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 0)
		var loaded state.MatchResult
		for _, m := range loadSwapMatches(t, store, compID) {
			if m.SideAID == a.ID || m.SideBID == a.ID {
				loaded = m
				break
			}
		}
		require.NotEmpty(t, loaded.ID)
		_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
		require.NoError(t, err)
		assertRefused(t, eng, store, compID, loaded.ID, loaded.SideAID, loaded.SideBID)
	})

	t.Run("bracket match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "stale-decision-ko"
		drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(8))
		before := loadBracket(t, store, compID)
		m0, m1 := before.Rounds[0][0], before.Rounds[0][1]
		_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: m0.SideAID, Place: swapSlot(0)}, B: SwapSide{ID: m1.SideAID, Place: swapSlot(2)}})
		require.NoError(t, err)
		assertRefused(t, eng, store, compID, m0.ID, m0.SideAID, m0.SideBID)
	})

	t.Run("a writer that saw the current pairing is judged as before", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "stale-decision-current"
		drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(8))
		m0 := loadBracket(t, store, compID).Rounds[0][0]
		_, _, err := eng.RecordDecisionWithOptions(compID, m0.ID, "kiken-voluntary", "aka", "", nil,
			ForceOptions{Seen: SeenSides{AID: m0.SideAID, BID: m0.SideBID}}, serverNowMs())
		require.NoError(t, err)
	})
}

// TestSwapDrawPlaces_StaleWritesAreRefused pins FR11: a device that loaded a
// match before the swap and sends a start or a result after it (a queued
// offline write included) names the old pairing, so the engine refuses it as
// a side mismatch, writes nothing, and the competition does not start.
func TestSwapDrawPlaces_StaleWritesAreRefused(t *testing.T) {
	staleWrites := func(m state.MatchResult) map[string]*state.MatchResult {
		return map[string]*state.MatchResult{
			"start": {SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusRunning, ModifiedAt: serverNowMs()},
			"result": {SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusCompleted,
				Winner: m.SideA, WinnerID: m.SideAID, IpponsA: []string{"M"}, ModifiedAt: serverNowMs()},
			"start by name only": {SideA: m.SideA, SideB: m.SideB, Status: state.MatchStatusRunning, ModifiedAt: serverNowMs()},
		}
	}
	assertStillDrawReady := func(t *testing.T, eng *Engine, store *state.Store, compID, matchID string) {
		t.Helper()
		_, err := eng.MaybeAutoCompletePools(compID)
		require.NoError(t, err)
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, comp.Status, "a refused write does not start the competition")
		history, err := store.LoadMatchHistory(compID, matchID)
		require.NoError(t, err)
		assert.Empty(t, history, "a refused write leaves no history line")
	}

	t.Run("pool match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "stale-pool"
		drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6))
		pools := loadSwapPools(t, store, compID)
		a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 0)
		var loaded state.MatchResult
		for _, m := range loadSwapMatches(t, store, compID) {
			if m.SideAID == a.ID || m.SideBID == a.ID {
				loaded = m
				break
			}
		}
		require.NotEmpty(t, loaded.ID)
		_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
		require.NoError(t, err)

		for name, w := range staleWrites(loaded) {
			t.Run(name, func(t *testing.T) {
				_, err := eng.RecordMatchResultWithIneligibility(compID, loaded.ID, w)
				assert.ErrorIs(t, err, ErrMatchSideMismatch)
				assertStillDrawReady(t, eng, store, compID, loaded.ID)
			})
		}
		// Nothing was recorded, so the swap can still be undone.
		back, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: a.ID, Place: b.Place}, B: SwapSide{ID: b.ID, Place: a.Place}})
		require.NoError(t, err)
		assert.True(t, back.Applied)
	})

	t.Run("bracket match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "stale-ko"
		drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(8))
		before := loadBracket(t, store, compID)
		m0, m1 := before.Rounds[0][0], before.Rounds[0][1]
		_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: m0.SideAID, Place: swapSlot(0)}, B: SwapSide{ID: m1.SideAID, Place: swapSlot(2)}})
		require.NoError(t, err)

		loaded := state.MatchResult{SideA: m0.SideA, SideB: m0.SideB, SideAID: m0.SideAID, SideBID: m0.SideBID}
		for name, w := range staleWrites(loaded) {
			t.Run(name, func(t *testing.T) {
				_, err := eng.RecordMatchResultWithIneligibility(compID, m0.ID, w)
				assert.ErrorIs(t, err, ErrMatchSideMismatch)
				assertStillDrawReady(t, eng, store, compID, m0.ID)
			})
		}
		after := loadBracket(t, store, compID).Rounds[0][0]
		assert.Equal(t, state.MatchStatusScheduled, after.Status)
		assert.Equal(t, m1.SideAID, after.SideAID)
	})
}

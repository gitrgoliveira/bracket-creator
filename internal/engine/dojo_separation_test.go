package engine

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

// drawDojoComp creates a competition of format with one participant per dojo
// in dojos (named P01, P02, ...), generates its draw and returns the summary
// the draw-ready competition answers.
func drawDojoComp(t *testing.T, eng *Engine, store *state.Store, compID, format string, poolSize int, dojos []string, opts ...func(*state.Competition)) []DojoMeeting {
	t.Helper()
	createTestCompetition(t, store, compID, format, poolSize, opts...)
	players := make([]domain.Player, len(dojos))
	for i, d := range dojos {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: d}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)
	meetings, err := eng.DojoSeparationFor(comp)
	require.NoError(t, err)
	return meetings
}

// uniqueDojos returns n distinct dojo names, the rest of a roster around the
// dojo a test is about.
func uniqueDojos(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("Dojo %02d", i+1)
	}
	return out
}

// placeRank reads the rank out of a qualifying-place label ("Pool A-2nd" -> 2).
func placeRank(t *testing.T, label string) int {
	t.Helper()
	i := strings.LastIndexByte(label, '-')
	require.Positive(t, i, label)
	digits := strings.TrimRight(label[i+1:], "stndrh")
	n, err := strconv.Atoi(digits)
	require.NoError(t, err, label)
	return n
}

// scorePoolMatchFor completes pool match m through the engine's score door,
// winnerID winning one men.
func scorePoolMatchFor(t *testing.T, eng *Engine, compID string, m state.MatchResult, winnerID string) {
	t.Helper()
	r := &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusCompleted}
	switch winnerID {
	case m.SideAID:
		r.Winner, r.WinnerID, r.IpponsA = m.SideA, m.SideAID, []string{"M"}
	case m.SideBID:
		r.Winner, r.WinnerID, r.IpponsB = m.SideB, m.SideBID, []string{"M"}
	default:
		t.Fatalf("%s is not in %s", winnerID, m.ID)
	}
	_, err := eng.RecordMatchResultWithIneligibility(compID, m.ID, r)
	require.NoError(t, err, m.ID)
}

// finishPoolsAt plays every pool bout through the engine so that each
// competitor in ranks finishes their pool at that rank: the pool's order is
// its draw order with each ranked competitor moved to its rank, and in every
// bout the competitor higher in that order wins. A strict order leaves no tie
// for a tiebreaker to hold the pools open. Then the pools are resolved into
// the knockout.
func finishPoolsAt(t *testing.T, eng *Engine, store *state.Store, compID string, ranks map[string]int) {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	order := map[string]int{}
	for _, p := range pools {
		ids := make([]string, 0, len(p.Players))
		var ranked []string
		for _, pl := range p.Players {
			if _, ok := ranks[pl.ID]; ok {
				ranked = append(ranked, pl.ID)
				continue
			}
			ids = append(ids, pl.ID)
		}
		for _, id := range ranked {
			at := ranks[id] - 1
			require.LessOrEqual(t, at, len(ids), "rank %d does not exist in %s", ranks[id], p.PoolName)
			ids = append(ids[:at], append([]string{id}, ids[at:]...)...)
		}
		for i, id := range ids {
			order[id] = i
		}
	}
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for _, m := range matches {
		winner := m.SideAID
		if order[m.SideBID] < order[m.SideAID] {
			winner = m.SideBID
		}
		scorePoolMatchFor(t, eng, compID, m, winner)
	}
	_, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
}

// playUntilTheyMeet fights the knockout through the engine's score door, a
// and b winning every bout they are in (side A wins every other bout), until
// one match holds both of them, and returns that match's id. It is the
// bracket's own advancement that brings them together, so the summary's
// arithmetic is checked against the engine rather than against itself.
func playUntilTheyMeet(t *testing.T, eng *Engine, store *state.Store, compID, a, b string) string {
	t.Helper()
	for step := 0; step < 256; step++ {
		bracket := loadBracket(t, store, compID)
		var next *state.BracketMatch
		for ri := range bracket.Rounds {
			for mi := range bracket.Rounds[ri] {
				m := &bracket.Rounds[ri][mi]
				ids := map[string]bool{m.SideAID: true, m.SideBID: true}
				if ids[a] && ids[b] {
					return m.ID
				}
				if next == nil && !m.Hidden && m.Status == state.MatchStatusScheduled && m.SideAID != "" && m.SideBID != "" {
					next = m
				}
			}
		}
		require.NotNil(t, next, "no match left to fight and %s and %s never met", a, b)
		winner := next.SideA
		if next.SideBID == a || next.SideBID == b {
			winner = next.SideB
		}
		scoreBracketMatch(t, eng, store, compID, next.ID, winner)
	}
	t.Fatalf("%s and %s never met", a, b)
	return ""
}

// assertPoolsMeetingHolds drives a pools + knockout competition so that the
// pair finishes at the places the meeting names, then fights the knockout and
// asserts the two meet in the meeting's match.
func assertPoolsMeetingHolds(t *testing.T, eng *Engine, store *state.Store, compID string, dm DojoMeeting, m *BracketMeeting) {
	t.Helper()
	require.NotNil(t, m)
	require.NotEmpty(t, m.PlaceA)
	require.NotEmpty(t, m.PlaceB)
	require.Equal(t, dm.A.Pool, qualifierLabelPool(m.PlaceA))
	require.Equal(t, dm.B.Pool, qualifierLabelPool(m.PlaceB))
	require.NoError(t, eng.StartCompetition(compID))
	finishPoolsAt(t, eng, store, compID, map[string]int{
		dm.A.ID: placeRank(t, m.PlaceA),
		dm.B.ID: placeRank(t, m.PlaceB),
	})
	assert.Equal(t, m.MatchID, playUntilTheyMeet(t, eng, store, compID, dm.A.ID, dm.B.ID),
		"%s (%s) and %s (%s) must meet in the match the summary names", dm.A.Name, m.PlaceA, dm.B.Name, m.PlaceB)
}

// firstCrossPool returns the first meeting of two different pools.
func firstCrossPool(t *testing.T, meetings []DojoMeeting) DojoMeeting {
	t.Helper()
	for _, dm := range meetings {
		if dm.SamePool == "" {
			return dm
		}
	}
	t.Fatal("the draw put no pair of one dojo in different pools")
	return DojoMeeting{}
}

// --- pools + knockout ---------------------------------------------------------

func TestDojoSeparation_PoolsOneQualifier(t *testing.T) {
	// Two pools of four, one qualifier each: the two 1st places meet in the
	// final, which is both the meeting if both win and the earliest.
	dojos := append([]string{"Mumeishi", "Mumeishi"}, uniqueDojos(6)...)
	eng, store, _ := setupTestEngine(t)
	meetings := drawDojoComp(t, eng, store, "one-q", state.CompFormatMixed, 4, dojos, func(c *state.Competition) { c.PoolWinners = 1 })
	require.Len(t, meetings, 1)
	dm := meetings[0]
	assert.Equal(t, "Mumeishi", dm.Dojo)
	require.Empty(t, dm.SamePool, "the draw keeps the two apart")
	require.NotNil(t, dm.IfBothWin)
	require.NotNil(t, dm.Earliest)
	assert.Equal(t, *dm.IfBothWin, *dm.Earliest, "with one qualifier a pool has no earlier meeting")
	assert.Equal(t, dm.A.Pool+"-1st", dm.Earliest.PlaceA)
	assert.Equal(t, dm.B.Pool+"-1st", dm.Earliest.PlaceB)
	assert.Equal(t, 1, dm.Earliest.DisplayRound, "the final")
	assertPoolsMeetingHolds(t, eng, store, "one-q", dm, dm.Earliest)
}

func TestDojoSeparation_PoolsTwoQualifiers(t *testing.T) {
	dojos := append([]string{"Mumeishi", "Mumeishi"}, uniqueDojos(6)...)
	setup := func(t *testing.T, compID string) (*Engine, *state.Store, DojoMeeting) {
		eng, store, _ := setupTestEngine(t)
		meetings := drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 4, dojos)
		require.Len(t, meetings, 1)
		dm := meetings[0]
		require.Empty(t, dm.SamePool)
		require.NotNil(t, dm.IfBothWin)
		require.NotNil(t, dm.Earliest)
		assert.Less(t, dm.Earliest.Round, dm.IfBothWin.Round,
			"a 1st and a 2nd of two pools meet before the two 1st places do")
		assert.Equal(t, dm.A.Pool+"-1st", dm.IfBothWin.PlaceA)
		assert.Equal(t, dm.B.Pool+"-1st", dm.IfBothWin.PlaceB)
		assert.Equal(t, 1, dm.IfBothWin.DisplayRound, "the two winners meet in the final")
		return eng, store, dm
	}
	t.Run("if both win", func(t *testing.T) {
		eng, store, dm := setup(t, "two-q-win")
		assertPoolsMeetingHolds(t, eng, store, "two-q-win", dm, dm.IfBothWin)
	})
	t.Run("earliest", func(t *testing.T) {
		eng, store, dm := setup(t, "two-q-early")
		assertPoolsMeetingHolds(t, eng, store, "two-q-early", dm, dm.Earliest)
	})
}

func TestDojoSeparation_PoolsExtraQualifiers(t *testing.T) {
	// 11 entrants at minimum 3 with fill-bracket: three pools and a drafted
	// 2nd, so a pool owns more than one place of a one-winner bracket.
	setup := func(t *testing.T, compID string) (*Engine, *state.Store, []DojoMeeting) {
		eng, store, _ := setupTestEngine(t)
		dojos := make([]string, 11)
		for i := range dojos {
			dojos[i] = "Dojo" + string(rune('A'+i%5))
		}
		meetings := drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, dojos, func(c *state.Competition) {
			c.PoolWinners = 1
			c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		})
		return eng, store, meetings
	}
	_, store, meetings := setup(t, "extra-q-shape")
	drafted := false
	for _, m := range loadBracket(t, store, "extra-q-shape").Rounds[0] {
		drafted = drafted || strings.HasSuffix(m.PlaceholderA, "-2nd") || strings.HasSuffix(m.PlaceholderB, "-2nd")
	}
	require.True(t, drafted, "fixture must seat a drafted 2nd, or this test does not exercise extra qualifiers")
	for _, dm := range meetings {
		if dm.SamePool != "" {
			continue
		}
		require.NotNil(t, dm.Earliest, "%s v %s", dm.A.Name, dm.B.Name)
		if dm.IfBothWin != nil {
			assert.LessOrEqual(t, compareMeetings(dm.Earliest, dm.IfBothWin), 0, "nothing is earlier than the earliest")
		}
	}

	t.Run("the earliest meeting holds", func(t *testing.T) {
		eng, store, meetings := setup(t, "extra-q-drive")
		// Prefer a pair whose earliest meeting runs through the drafted 2nd.
		pick := firstCrossPool(t, meetings)
		for _, dm := range meetings {
			if dm.SamePool == "" && (strings.HasSuffix(dm.Earliest.PlaceA, "-2nd") || strings.HasSuffix(dm.Earliest.PlaceB, "-2nd")) {
				pick = dm
				break
			}
		}
		assertPoolsMeetingHolds(t, eng, store, "extra-q-drive", pick, pick.Earliest)
	})
}

func TestDojoSeparation_SamePool(t *testing.T) {
	// Three of one dojo over two pools of three: two of them must share one.
	dojos := append([]string{"Mumeishi", "Mumeishi", "Mumeishi"}, uniqueDojos(3)...)
	eng, store, _ := setupTestEngine(t)
	meetings := drawDojoComp(t, eng, store, "same-pool", state.CompFormatMixed, 3, dojos)
	require.Len(t, meetings, 3, "three of one dojo make three pairs")
	same := 0
	for i, dm := range meetings {
		if dm.SamePool == "" {
			continue
		}
		same++
		assert.Equal(t, i, same-1, "same-pool pairs come first")
		assert.Equal(t, dm.A.Pool, dm.SamePool)
		assert.Equal(t, dm.B.Pool, dm.SamePool)
		assert.Nil(t, dm.Earliest, "they meet in the pool")
		assert.Nil(t, dm.IfBothWin)
	}
	assert.Positive(t, same)

	pools, err := store.LoadPools("same-pool")
	require.NoError(t, err)
	for _, dm := range meetings {
		for _, s := range []DojoSide{dm.A, dm.B} {
			place, ok := PoolPlaceOf(pools, s.ID)
			require.True(t, ok)
			assert.Equal(t, s.Pool, place.Pool)
		}
	}
}

// --- knockout only ------------------------------------------------------------

func TestDojoSeparation_KnockoutWithByes(t *testing.T) {
	// Six entrants in an eight-slot bracket: two byes, and a dojo of three.
	dojos := append([]string{"Mumeishi", "Mumeishi", "Mumeishi"}, uniqueDojos(3)...)
	draw := func(t *testing.T, compID string) (*Engine, *state.Store, []DojoMeeting) {
		eng, store, _ := setupTestEngine(t)
		return eng, store, drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, dojos)
	}
	_, store, meetings := draw(t, "ko-shape")
	require.Len(t, meetings, 3, "three of one dojo make three pairs")
	seen := map[string]bool{}
	bracket := loadBracket(t, store, "ko-shape")
	for i, dm := range meetings {
		assert.Empty(t, dm.SamePool)
		assert.Nil(t, dm.IfBothWin)
		require.NotNil(t, dm.Earliest)
		assert.Empty(t, dm.A.Pool)
		assert.Empty(t, dm.Earliest.PlaceA)
		assert.Equal(t, len(bracket.Rounds), dm.Earliest.Rounds)
		if i > 0 {
			assert.LessOrEqual(t, compareMeetings(meetings[i-1].Earliest, dm.Earliest), 0, "earliest meeting first")
		}
		seen[dm.A.ID+"|"+dm.B.ID] = true
		seen[dm.B.ID+"|"+dm.A.ID] = true
		_, okA := LeafSlotOf(bracket, dm.A.ID)
		_, okB := LeafSlotOf(bracket, dm.B.ID)
		assert.True(t, okA && okB)
	}
	assert.Len(t, seen, 6, "every pair of the three, once")

	for k := range meetings {
		k := k
		t.Run(fmt.Sprintf("pair %d meets where reported", k), func(t *testing.T) {
			compID := fmt.Sprintf("ko-drive-%d", k)
			eng, store, meetings := draw(t, compID)
			dm := meetings[k]
			require.NoError(t, eng.StartCompetition(compID))
			assert.Equal(t, dm.Earliest.MatchID, playUntilTheyMeet(t, eng, store, compID, dm.A.ID, dm.B.ID))
		})
	}
}

// --- no summary -----------------------------------------------------------------

func TestDojoSeparation_FormatsAndDrawsWithoutOne(t *testing.T) {
	pools := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: "a1", Name: "A1", Dojo: "Mumeishi"},
		{ID: "a2", Name: "A2", Dojo: "Mumeishi"},
	}}}
	for _, tc := range []struct {
		name   string
		format string
		status state.CompetitionStatus
	}{
		{"league", state.CompFormatLeague, state.CompStatusDrawReady},
		{"swiss", state.CompFormatSwiss, state.CompStatusDrawReady},
		{"no draw yet", state.CompFormatMixed, state.CompStatusSetup},
		{"knockout with no draw yet", state.CompFormatKnockout, state.CompStatusSetup},
	} {
		t.Run(tc.name, func(t *testing.T) {
			comp := &state.Competition{ID: "c", Format: tc.format, Status: tc.status}
			got := DojoSeparation(comp, pools, nil, nil)
			require.NotNil(t, got, "an empty list, never null")
			assert.Empty(t, got)
		})
	}
	assert.Empty(t, DojoSeparation(nil, pools, nil, nil))

	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "setup-only", state.CompFormatMixed, 3)
	saveTestParticipants(t, store, "setup-only", []string{"A", "B", "C", "D", "E", "F"})
	comp, err := store.LoadCompetition("setup-only")
	require.NoError(t, err)
	got, err := eng.DojoSeparationFor(comp)
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
	got, err = eng.DojoSeparationFor(nil)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// --- the pure function on a hand-built draw ---------------------------------------

// handBracket is a four-leaf pools bracket with the given first-round labels,
// numbered as the draw numbers it (first round 1-2, the final 3).
func handBracket(labels [4]string) *state.Bracket {
	return &state.Bracket{Preview: true, Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", SideA: labels[0], SideB: labels[1], PlaceholderA: labels[0], PlaceholderB: labels[1], DisplayRound: 2, MatchNumber: 1},
			{ID: "m-r1-1", SideA: labels[2], SideB: labels[3], PlaceholderA: labels[2], PlaceholderB: labels[3], DisplayRound: 2, MatchNumber: 2},
		},
		{
			{ID: "m-r2-0", SideA: "Winner of r1-m0", SideB: "Winner of r1-m1", DisplayRound: 1, MatchNumber: 3},
		},
	}}
}

func TestDojoSeparation_ExtraQualifierPlaceIsEarliest(t *testing.T) {
	// Pool C owns a 1st and a drafted 2nd. A's winner meets C's 2nd in the
	// first round and C's winner only in the final.
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{ID: "a1", Name: "A1", Dojo: "Mumeishi"}, {ID: "a2", Name: "A2", Dojo: "Other A"}}},
		{PoolName: "Pool B", Players: []helper.Player{{ID: "b1", Name: "B1", Dojo: "Other B"}}},
		{PoolName: "Pool C", Players: []helper.Player{{ID: "c1", Name: "C1", Dojo: "Other C"}, {ID: "c2", Name: "C2", Dojo: "mumeishi "}}},
	}
	bracket := handBracket([4]string{"Pool A-1st", "Pool C-2nd", "Pool B-1st", "Pool C-1st"})
	comp := &state.Competition{ID: "c", Format: state.CompFormatMixed, Status: state.CompStatusDrawReady}
	got := DojoSeparation(comp, pools, bracket, nil)
	require.Len(t, got, 1, "a spelling that differs only in case and spacing is the same dojo")
	dm := got[0]
	assert.Equal(t, "Mumeishi", dm.Dojo, "the group reads under its first member's spelling")
	assert.Equal(t, DojoSide{ID: "a1", Name: "A1", Pool: "Pool A"}, dm.A)
	assert.Equal(t, DojoSide{ID: "c2", Name: "C2", Pool: "Pool C"}, dm.B)
	assert.Equal(t, &BracketMeeting{MatchID: "m-r2-0", DisplayRound: 1, Round: 1, Rounds: 2, MatchNumber: 3, PlaceA: "Pool A-1st", PlaceB: "Pool C-1st"}, dm.IfBothWin)
	assert.Equal(t, &BracketMeeting{MatchID: "m-r1-0", DisplayRound: 2, Round: 0, Rounds: 2, MatchNumber: 1, PlaceA: "Pool A-1st", PlaceB: "Pool C-2nd"}, dm.Earliest)

	// Once the pools resolve the live sides carry names, but the places are
	// read from the draw-time labels, so the answer does not move.
	resolved := handBracket([4]string{"Pool A-1st", "Pool C-2nd", "Pool B-1st", "Pool C-1st"})
	for mi := range resolved.Rounds[0] {
		resolved.Rounds[0][mi].SideA, resolved.Rounds[0][mi].SideB = "X", "Y"
	}
	assert.Equal(t, got, DojoSeparation(comp, pools, resolved, nil))
}

func TestDojoSeparation_BlankDojoFormsNoGroup(t *testing.T) {
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{ID: "a1", Name: "A1", Dojo: ""}, {ID: "a2", Name: "A2", Dojo: "  "}}},
		{PoolName: "Pool B", Players: []helper.Player{{ID: "b1", Name: "B1", Dojo: "\t"}, {ID: "b2", Name: "B2", Dojo: "Real"}}},
	}
	bracket := handBracket([4]string{"Pool A-1st", "Pool B-2nd", "Pool B-1st", "Pool A-2nd"})
	mixed := &state.Competition{ID: "c", Format: state.CompFormatMixed, Status: state.CompStatusDrawReady}
	assert.Empty(t, DojoSeparation(mixed, pools, bracket, nil))

	ko := &state.Competition{ID: "k", Format: state.CompFormatKnockout, Status: state.CompStatusDrawReady}
	koBracket := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "A1", SideAID: "a1", SideB: "A2", SideBID: "a2"},
	}}}
	roster := []domain.Player{{ID: "a1", Name: "A1", Dojo: " "}, {ID: "a2", Name: "A2", Dojo: ""}}
	assert.Empty(t, DojoSeparation(ko, nil, koBracket, roster))
}

func TestDojoSeparation_DeterministicOrder(t *testing.T) {
	// An eight-leaf knockout, filled top to bottom: Kodokan holds slots 0, 1
	// and 4; Mumeishi slots 2 and 3; Hokushin slots 5 and 7.
	roster := []domain.Player{
		{ID: "id-k0", Name: "K0", Dojo: "Kodokan"},
		{ID: "id-k1", Name: "K1", Dojo: "Kodokan"},
		{ID: "id-m2", Name: "M2", Dojo: "Mumeishi"},
		{ID: "id-m3", Name: "M3", DisplayName: "M3 zekken", Dojo: "Mumeishi"},
		{ID: "id-k4", Name: "K4", Dojo: "kodokan"},
		{ID: "id-h5", Name: "H5", Dojo: "Hokushin"},
		{ID: "id-x6", Name: "X6", Dojo: "Solo"},
		{ID: "id-h7", Name: "H7", Dojo: "Hokushin"},
	}
	leaf := func(i int) (string, string) { return roster[i].Name, roster[i].ID }
	var r0 []state.BracketMatch
	for m := 0; m < 4; m++ {
		an, aid := leaf(2 * m)
		bn, bid := leaf(2*m + 1)
		r0 = append(r0, state.BracketMatch{ID: fmt.Sprintf("m-r1-%d", m), SideA: an, SideAID: aid, SideB: bn, SideBID: bid, DisplayRound: 3, MatchNumber: m + 1})
	}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		r0,
		{{ID: "m-r2-0", DisplayRound: 2, MatchNumber: 5}, {ID: "m-r2-1", DisplayRound: 2, MatchNumber: 6}},
		{{ID: "m-r3-0", DisplayRound: 1, MatchNumber: 7}},
	}}
	comp := &state.Competition{ID: "k", Format: state.CompFormatKnockout, Status: state.CompStatusDrawReady}
	got := DojoSeparation(comp, nil, bracket, roster)

	type row struct{ a, b, match string }
	var rows []row
	for _, dm := range got {
		rows = append(rows, row{dm.A.ID, dm.B.ID, dm.Earliest.MatchID})
	}
	assert.Equal(t, []row{
		{"id-k0", "id-k1", "m-r1-0"}, // first round, match 1
		{"id-m2", "id-m3", "m-r1-1"}, // first round, match 2
		{"id-h5", "id-h7", "m-r2-1"}, // semi-final, match 6
		{"id-k0", "id-k4", "m-r3-0"}, // final: dojo, then a's id, then b's id
		{"id-k1", "id-k4", "m-r3-0"},
	}, rows)
	assert.Equal(t, "M3 zekken", got[1].B.DisplayName, "a knockout side is read from the roster by id")
	assert.Equal(t, got, DojoSeparation(comp, nil, bracket, roster), "the same draw answers the same list")

	// A leaf whose id the roster does not hold names nobody.
	assert.Len(t, DojoSeparation(comp, nil, bracket, roster[:6]), 4, "H7 left the roster, so the Hokushin pair is gone")
}

// --- numbers ------------------------------------------------------------------

func TestDojoSeparation_SidesCarryTheirPlaceNumber(t *testing.T) {
	dojos := append([]string{"Mumeishi", "Mumeishi", "Mumeishi"}, uniqueDojos(5)...)
	withPrefix := func(c *state.Competition) { c.NumberPrefix = "K" }

	t.Run("pools draw: the pools.csv row's Number", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		meetings := drawDojoComp(t, eng, store, "num-pools", state.CompFormatMixed, 3, dojos, withPrefix)
		require.NotEmpty(t, meetings)
		pools, err := store.LoadPools("num-pools")
		require.NoError(t, err)
		byID := map[string]string{}
		for _, p := range pools {
			for _, pl := range p.Players {
				byID[pl.ID] = pl.Number
			}
		}
		for _, dm := range meetings {
			for _, s := range []DojoSide{dm.A, dm.B} {
				require.NotEmpty(t, byID[s.ID], "the draw numbered %s", s.ID)
				assert.Equal(t, byID[s.ID], s.Number)
			}
		}
	})

	t.Run("knockout: the number composed from DrawOrder", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		meetings := drawDojoComp(t, eng, store, "num-ko", state.CompFormatKnockout, 3, dojos, withPrefix)
		require.NotEmpty(t, meetings)
		comp, err := store.LoadCompetition("num-ko")
		require.NoError(t, err)
		bracket := loadBracket(t, store, "num-ko")
		require.NotEmpty(t, bracket.DrawOrder)
		roster, err := store.LoadParticipants("num-ko", comp.EffectiveWithZekkenName())
		require.NoError(t, err)
		NumberKnockoutParticipants(comp, bracket.DrawOrder, roster)
		byID := map[string]string{}
		for _, p := range roster {
			byID[p.ID] = p.Number
		}
		for _, dm := range meetings {
			for _, s := range []DojoSide{dm.A, dm.B} {
				require.NotEmpty(t, byID[s.ID], "the draw numbered %s", s.ID)
				assert.Equal(t, byID[s.ID], s.Number)
			}
		}
	})

	t.Run("no prefix: no number, whatever the records carry", func(t *testing.T) {
		pools := []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{{ID: "a1", Name: "A1", Dojo: "Mumeishi", Number: "K1"}}},
			{PoolName: "Pool B", Players: []helper.Player{{ID: "b1", Name: "B1", Dojo: "Mumeishi", Number: "K2"}}},
		}
		bracket := handBracket([4]string{"Pool A-1st", "Pool B-2nd", "Pool B-1st", "Pool A-2nd"})
		mixed := &state.Competition{ID: "c", Format: state.CompFormatMixed, Status: state.CompStatusDrawReady}
		got := DojoSeparation(mixed, pools, bracket, nil)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].A.Number)
		assert.Empty(t, got[0].B.Number)

		mixed.NumberPrefix = "K"
		got = DojoSeparation(mixed, pools, bracket, nil)
		require.Len(t, got, 1)
		assert.Equal(t, "K1", got[0].A.Number)
		assert.Equal(t, "K2", got[0].B.Number)

		roster := []domain.Player{
			{ID: "x1", Name: "X1", Dojo: "Kodokan", Number: "stale"},
			{ID: "x2", Name: "X2", Dojo: "Kodokan", Number: "stale"},
		}
		koBracket := &state.Bracket{Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "X1", SideAID: "x1", SideB: "X2", SideBID: "x2"},
		}}}
		ko := &state.Competition{ID: "k", Format: state.CompFormatKnockout, Status: state.CompStatusDrawReady}
		got = DojoSeparation(ko, nil, koBracket, roster)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].A.Number, "no prefix")

		ko.NumberPrefix = "K"
		got = DojoSeparation(ko, nil, koBracket, roster)
		require.Len(t, got, 1)
		assert.Empty(t, got[0].A.Number, "a bracket with no DrawOrder carries no number")

		koBracket.DrawOrder = []string{"x1", "x2"}
		got = DojoSeparation(ko, nil, koBracket, roster)
		require.Len(t, got, 1)
		assert.Equal(t, "K1", got[0].A.Number)
		assert.Equal(t, "K2", got[0].B.Number)
		assert.Equal(t, "stale", roster[0].Number, "the caller's roster is not written to")
	})
}

func TestDojoSeparation_PlaceLookups(t *testing.T) {
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{ID: "a1"}, {ID: "a2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{ID: "b1"}, {ID: "b2"}, {ID: "b3"}}},
	}
	place, ok := PoolPlaceOf(pools, "b3")
	require.True(t, ok)
	assert.Equal(t, DrawPlace{Pool: "Pool B", Position: 3}, place)
	_, ok = PoolPlaceOf(pools, "nobody")
	assert.False(t, ok)
	_, ok = PoolPlaceOf([]helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{Name: "No id"}}}}, "")
	assert.False(t, ok, "an empty id is in no place")

	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideAID: "x0", SideBID: "x1"}, {ID: "m-r1-1", SideAID: "x2"}},
		{{ID: "m-r2-0"}},
	}}
	place, ok = LeafSlotOf(bracket, "x0")
	require.True(t, ok)
	require.NotNil(t, place.Slot)
	assert.Equal(t, 0, *place.Slot)
	place, ok = LeafSlotOf(bracket, "x2")
	require.True(t, ok)
	assert.Equal(t, 2, *place.Slot)
	_, ok = LeafSlotOf(bracket, "")
	assert.False(t, ok, "the empty side of a bye is nobody's slot")
	_, ok = LeafSlotOf(nil, "x0")
	assert.False(t, ok)

	m, side, ok := FirstRoundLeaf(bracket, 3)
	require.True(t, ok)
	assert.Equal(t, "m-r1-1", m.ID)
	assert.Equal(t, domain.MatchSideB, side)
	m, side, ok = FirstRoundLeaf(bracket, 0)
	require.True(t, ok)
	assert.Equal(t, "m-r1-0", m.ID)
	assert.Equal(t, domain.MatchSideA, side)
	_, _, ok = FirstRoundLeaf(bracket, 4)
	assert.False(t, ok)
	_, _, ok = FirstRoundLeaf(bracket, -1)
	assert.False(t, ok)

	assert.Nil(t, leafMeeting(bracket, 1, 1), "one leaf does not meet itself")
	assert.Nil(t, leafMeeting(bracket, 0, 4), "a slot the bracket does not hold")
	assert.Equal(t, "m-r2-0", leafMeeting(bracket, 1, 3).MatchID)
}

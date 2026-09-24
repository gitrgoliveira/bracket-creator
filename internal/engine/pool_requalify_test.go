package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// rqFix is a mixed (pools then knockout) individual competition built the way
// a real draw builds it: round-robin pool matches with ids stamped, and a
// knockout from buildBracketFromDraw, so every slot carries its draw label.
type rqFix struct {
	t      *testing.T
	eng    *Engine
	store  *state.Store
	compID string
}

func rqID(name string) string { return "id-" + name }

// newRQFixture builds pools named "Pool A", "Pool B", ... from names, each
// player "id-<name>" from "Dojo <name>". The knockout is built from the draw;
// a test needing labels this draw builder does not seat saves its own over it.
func newRQFixture(t *testing.T, compID string, poolWinners int, names [][]string, opts ...func(*state.Competition)) *rqFix {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: poolWinners,
	}
	for _, o := range opts {
		o(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))

	var pools []helper.Pool
	var roster []domain.Player
	var matches []state.MatchResult
	for pi, pn := range names {
		pool := helper.Pool{PoolName: fmt.Sprintf("Pool %c", 'A'+pi)}
		for _, n := range pn {
			p := domain.Player{ID: rqID(n), Name: n, Dojo: "Dojo " + n}
			pool.Players = append(pool.Players, p)
			roster = append(roster, p)
		}
		k := 0
		for i := 0; i < len(pn); i++ {
			for j := i + 1; j < len(pn); j++ {
				matches = append(matches, state.MatchResult{
					ID:    fmt.Sprintf("%s-%d", pool.PoolName, k),
					SideA: pn[i], SideB: pn[j], SideAID: rqID(pn[i]), SideBID: rqID(pn[j]),
					Status: state.MatchStatusScheduled,
				})
				k++
			}
		}
		pools = append(pools, pool)
	}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, roster))
	require.NoError(t, store.SavePoolMatches(compID, matches))

	draw := helper.BuildKnockoutDraw(pools, poolWinners, 1)
	stored, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := eng.buildBracketFromDraw(stored, draw, nil)
	require.NoError(t, err)
	require.NoError(t, store.SaveBracket(compID, bracket))
	return &rqFix{t: t, eng: eng, store: store, compID: compID}
}

// poolResult is a completed result for a stored pool match, won by winner
// with one men, stamped with ids from the stored row.
func (f *rqFix) poolResult(matchID, winner string) *state.MatchResult {
	f.t.Helper()
	m := loadPoolMatchByID(f.t, f.store, f.compID, matchID)
	require.NotNil(f.t, m, matchID)
	r := &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusCompleted}
	switch winner {
	case m.SideA:
		r.Winner, r.WinnerID, r.IpponsA = m.SideA, m.SideAID, []string{"M"}
	case m.SideB:
		r.Winner, r.WinnerID, r.IpponsB = m.SideB, m.SideBID, []string{"M"}
	default:
		f.t.Fatalf("%s is not in %s", winner, matchID)
	}
	return r
}

// write runs one score write through the transaction door every handler uses.
func (f *rqFix) write(matchID string, r *state.MatchResult, fo ...ForceOptions) error {
	f.t.Helper()
	var werr error
	require.NoError(f.t, f.store.WithTransaction(f.compID, func(tx state.StoreTx) error {
		_, werr = f.eng.RecordMatchResultWithIneligibilityTx(tx, f.compID, matchID, r, fo...)
		return nil
	}))
	return werr
}

func (f *rqFix) scorePool(matchID, winner string) {
	f.t.Helper()
	require.NoError(f.t, f.write(matchID, f.poolResult(matchID, winner)))
}

func (f *rqFix) resolve() {
	f.t.Helper()
	_, _, err := f.eng.ResolveQualifiedPools(f.compID)
	require.NoError(f.t, err)
}

func (f *rqFix) bracket() *state.Bracket {
	f.t.Helper()
	b, err := f.store.LoadBracket(f.compID)
	require.NoError(f.t, err)
	return b
}

// slot finds the round-0 match seated for label and which side holds it.
func (f *rqFix) slot(label string) (state.BracketMatch, string) {
	f.t.Helper()
	for _, m := range f.bracket().Rounds[0] {
		if m.PlaceholderA == label {
			return m, "A"
		}
		if m.PlaceholderB == label {
			return m, "B"
		}
	}
	f.t.Fatalf("no round-0 slot for %s", label)
	return state.BracketMatch{}, ""
}

func sideOf(m state.BracketMatch, side string) (string, string) {
	if side == "A" {
		return m.SideA, m.SideAID
	}
	return m.SideB, m.SideBID
}

// scoreKO completes a knockout match for winner (a name on one of its sides).
func (f *rqFix) scoreKO(matchID, winner string) error {
	f.t.Helper()
	m := findBracketMatchInBracket(f.bracket(), matchID)
	require.NotNil(f.t, m, matchID)
	r := &state.MatchResult{SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID, Status: state.MatchStatusCompleted}
	switch winner {
	case m.SideA:
		r.Winner, r.WinnerID, r.IpponsA = m.SideA, m.SideAID, []string{"M"}
	case m.SideB:
		r.Winner, r.WinnerID, r.IpponsB = m.SideB, m.SideBID, []string{"M"}
	default:
		f.t.Fatalf("%s is not in %s (%s vs %s)", winner, matchID, m.SideA, m.SideB)
	}
	return f.write(matchID, r)
}

// A 1st/2nd swap leaves the same two competitors qualified, so a comparison of
// the qualifying SET before and after the write saw no change at all, and both
// knockout slots went on showing the old places. Measured against the
// bracket, both places move and the played match is named; confirmed, it is
// reopened, both slots are repainted, and the untouched final no longer shows
// the old winner as if it were playable.
func TestRequalify_FirstSecondSwap_WarnsThenReopensAndRepaints(t *testing.T) {
	f := newRQFixture(t, "rq-swap", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()

	m1, s1 := f.slot("Pool A-1st")
	name, _ := sideOf(m1, s1)
	require.Equal(t, "A1", name)
	require.NoError(t, f.scoreKO(m1.ID, "A1"))

	err := f.write("Pool A-0", f.poolResult("Pool A-0", "A2"))
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	assert.Equal(t, "Pool A-0", played.MatchID)
	require.Len(t, played.Blocking, 1, "only the match A1 already fought is named; the scheduled one is simply repainted")
	assert.Equal(t, m1.ID, played.Blocking[0].ID)
	require.Len(t, played.QualifierChange, 2)
	assert.Equal(t, QualifierChange{Pool: "Pool A", Rank: 1, Place: "1st", From: QualifierIdentity{Name: "A1", ID: rqID("A1")}, To: QualifierIdentity{Name: "A2", ID: rqID("A2")}}, played.QualifierChange[0])
	assert.Equal(t, QualifierChange{Pool: "Pool A", Rank: 2, Place: "2nd", From: QualifierIdentity{Name: "A2", ID: rqID("A2")}, To: QualifierIdentity{Name: "A1", ID: rqID("A1")}}, played.QualifierChange[1])
	assert.Equal(t, "A1", loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Winner, "refused until confirmed")

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Len(t, reopened, 1)
	assert.Equal(t, m1.ID, reopened[0].ID)
	assert.Equal(t, m1.MatchNumber, reopened[0].Number)

	b := f.bracket()
	got := findBracketMatchInBracket(b, m1.ID)
	assert.Equal(t, state.MatchStatusScheduled, got.Status)
	assert.Empty(t, got.Winner)
	assert.Empty(t, got.IpponsA)
	n, id := sideOf(*got, s1)
	assert.Equal(t, "A2", n)
	assert.Equal(t, rqID("A2"), id)
	m2, s2 := f.slot("Pool A-2nd")
	n, id = sideOf(m2, s2)
	assert.Equal(t, "A1", n)
	assert.Equal(t, rqID("A1"), id)

	final := b.Rounds[1][0]
	assert.Equal(t, state.MatchStatusScheduled, final.Status)
	for _, side := range []string{final.SideA, final.SideB} {
		assert.NotEqual(t, "A1", side, "the untouched final must not keep the old qualifier as if playable")
	}
}

// When only the 2nd-place match was played, the competitor named as displaced
// is the one sitting in THAT match (the old 2nd), not the old 1st: the
// refusal and the queued-replay copy say "<displaced> already played <match>".
func TestRequalify_FirstSecondSwap_DisplacedIsWhoPlayed(t *testing.T) {
	f := newRQFixture(t, "rq-swap2", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m2, _ := f.slot("Pool A-2nd")
	require.NoError(t, f.scoreKO(m2.ID, "A2"))

	err := f.write("Pool A-0", f.poolResult("Pool A-0", "A2"))
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, m2.ID, played.BlockingMatchID)
	assert.Equal(t, "A2", played.Displaced)
	assert.Contains(t, played.Error(), "was already fought")
}

// Under the extra-qualifier modes the knockout seats "-2nd" slots while
// PoolWinners stays 1, so a guard counting only the top PoolWinners never saw
// a 2nd-place change. The bracket-baseline check sees every label the draw
// seated.
func TestRequalify_ExtraQualifierSecond_Warns(t *testing.T) {
	f := newRQFixture(t, "rq-extra", 1, [][]string{{"A1", "A2", "A3"}, {"B1", "B2", "B3"}}, func(c *state.Competition) {
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
	})
	// Seat the "-2nd" labels by hand: this draw builder only seats winners.
	require.NoError(t, f.store.SaveBracket(f.compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", MatchNumber: 1, SideA: "Pool A-1st", SideB: "Pool B-2nd", PlaceholderA: "Pool A-1st", PlaceholderB: "Pool B-2nd"},
			{ID: "m-r1-1", MatchNumber: 2, SideA: "Pool B-1st", SideB: "Pool A-2nd", PlaceholderA: "Pool B-1st", PlaceholderB: "Pool A-2nd"},
		},
		{{ID: "m-r2-0", MatchNumber: 3, SideA: winnerOfPlaceholder(2, 0), SideB: winnerOfPlaceholder(2, 1), PlaceholderA: winnerOfPlaceholder(2, 0), PlaceholderB: winnerOfPlaceholder(2, 1)}},
	}}))
	// A1 > A2 > A3; B1 > B2 > B3.
	for _, w := range [][2]string{{"Pool A-0", "A1"}, {"Pool A-1", "A1"}, {"Pool A-2", "A2"}, {"Pool B-0", "B1"}, {"Pool B-1", "B1"}, {"Pool B-2", "B2"}} {
		f.scorePool(w[0], w[1])
	}
	f.resolve()
	require.Equal(t, "A2", findBracketMatchInBracket(f.bracket(), "m-r1-1").SideB)
	require.NoError(t, f.scoreKO("m-r1-1", "B1"))

	// A3 now beats A2: A3 is 2nd.
	err := f.write("Pool A-2", f.poolResult("Pool A-2", "A3"))
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.QualifierChange, 1)
	assert.Equal(t, 2, played.QualifierChange[0].Rank)
	assert.Equal(t, "A2", played.QualifierChange[0].From.Name)
	assert.Equal(t, "A3", played.QualifierChange[0].To.Name)
	assert.Equal(t, "m-r1-1", played.BlockingMatchID)
}

// A match decided by a withdrawal and then reopened by a pool correction no
// longer carries that withdrawal, so the competitor it barred is restored and
// reported for the broadcast.
func TestRequalify_ForcedReopenRestoresWithdrawalEligibility(t *testing.T) {
	f := newRQFixture(t, "rq-kiken", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko := f.bracket().Rounds[0][0]
	// B1 withdraws from the knockout match against A1. decisionBy names the
	// withdrawing side: aka is SideA, shiro is SideB.
	decisionBy := "shiro"
	if ko.SideA == "B1" {
		decisionBy = "aka"
	}
	_, st, err := f.eng.RecordDecision(f.compID, ko.ID, "kiken-voluntary", decisionBy, "", nil, false)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.Equal(t, rqID("B1"), st.PlayerID)

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Len(t, reopened, 1)
	require.NotNil(t, reopened[0].Restored, "the withdrawal the reopen removed bars nobody")
	assert.Equal(t, rqID("B1"), reopened[0].Restored.PlayerID)
	assert.True(t, reopened[0].Restored.Eligible)
	statuses, err := f.store.LoadCompetitorStatus(f.compID)
	require.NoError(t, err)
	assert.True(t, statuses[rqID("B1")].Eligible)
	assert.Empty(t, findBracketMatchInBracket(f.bracket(), ko.ID).Decision)
}

// A qualifier seated through a bye sits in two slots: the bye match's winner
// and the next round's side. A correction made once the competition reached
// knockout status repaints both.
func TestRequalify_ByeLeafRepaintsNextRound(t *testing.T) {
	f := newRQFixture(t, "rq-bye", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}, {"C1", "C2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.scorePool("Pool C-0", "C1")
	f.resolve()
	setCompStatus(t, f.store, f.compID, state.CompStatusKnockout)

	var bye state.BracketMatch
	for _, m := range f.bracket().Rounds[0] {
		if m.SideA == "" || m.SideB == "" {
			bye = m
		}
	}
	require.NotEmpty(t, bye.ID, "three qualifiers in a four-slot draw leave one bye")
	label := bye.PlaceholderWinner
	require.NotEmpty(t, label)
	pool := label[:len("Pool X")]
	winner, loser := string(pool[5])+"1", string(pool[5])+"2"
	require.Equal(t, winner, bye.Winner)

	require.NoError(t, f.write(pool+"-0", f.poolResult(pool+"-0", loser)))

	b := f.bracket()
	gotBye := findBracketMatchInBracket(b, bye.ID)
	assert.Equal(t, loser, gotBye.Winner)
	assert.Equal(t, rqID(loser), gotBye.WinnerID)
	final := b.Rounds[1][0]
	seated := final.SideA
	seatedID := final.SideAID
	if final.PlaceholderB == label {
		seated, seatedID = final.SideB, final.SideBID
	}
	assert.Equal(t, loser, seated)
	assert.Equal(t, rqID(loser), seatedID)
}

// A correction that leaves a qualifying place tied does not pick a finisher by
// sort order: the slot returns to its draw label until the tie-break is
// fought, which is then injected and seated even though the competition had
// already reached knockout status.
func TestRequalify_TieRevertsToLabelThenTieBreakSeats(t *testing.T) {
	f := newRQFixture(t, "rq-tie", 1, [][]string{{"A1", "A2", "A3"}, {"B1", "B2"}})
	// Pool A-0: A1 v A2, A-1: A1 v A3, A-2: A2 v A3.
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool A-1", "A1")
	f.scorePool("Pool A-2", "A2")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	setCompStatus(t, f.store, f.compID, state.CompStatusKnockout)
	m, side := f.slot("Pool A-1st")
	name, _ := sideOf(m, side)
	require.Equal(t, "A1", name)

	// A3 beats A1: a three-way tie on one win each.
	require.NoError(t, f.write("Pool A-1", f.poolResult("Pool A-1", "A3")))
	m, _ = f.slot("Pool A-1st")
	name, id := sideOf(m, side)
	assert.Equal(t, "Pool A-1st", name, "a tied place is not a finisher")
	assert.Empty(t, id)

	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	var tbs []state.MatchResult
	matches, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for _, pm := range matches {
		if IsTiebreakerMatchID(pm.ID) {
			tbs = append(tbs, pm)
		}
	}
	require.NotEmpty(t, tbs, "the tie-break is injected in knockout status")
	for _, tb := range tbs {
		winner := tb.SideA
		if tb.SideB == "A3" || (tb.SideA != "A3" && tb.SideB == "A1") {
			winner = tb.SideB
		}
		f.scorePool(tb.ID, winner)
	}
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	m, _ = f.slot("Pool A-1st")
	name, id = sideOf(m, side)
	assert.Equal(t, "A3", name, "the tie-break winner is seated")
	assert.Equal(t, rqID("A3"), id)
}

// Reopening a withdrawal-decided pool match is allowed even though its
// finisher has played the knockout: the reopen moves no slot. The write that
// finishes it decides: a different result names the played match, the same
// result is silent.
func TestRequalify_ReopenThenFinish(t *testing.T) {
	f := newRQFixture(t, "rq-reopen", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	// A2 (shiro, SideB) withdraws from Pool A-0, so A1 qualifies.
	_, _, err := f.eng.RecordDecision(f.compID, "Pool A-0", "kiken-voluntary", "shiro", "", nil, false)
	require.NoError(t, err)
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko := f.bracket().Rounds[0][0]
	require.NoError(t, f.scoreKO(ko.ID, "B1"))

	_, err = f.eng.ReopenMatch(f.compID, "Pool A-0", "withdrawal recorded by mistake")
	require.NoError(t, err, "a pool reopen moves no knockout slot")

	err = f.write("Pool A-0", f.poolResult("Pool A-0", "A2"))
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played, "finishing with a different result must name the played match")
	assert.Equal(t, ko.ID, played.BlockingMatchID)
	assert.Equal(t, state.MatchStatusRunning, loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Status, "the refused finish leaves the reopened match open")

	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A1")), "the same result moves nobody")
	assert.Equal(t, state.MatchStatusCompleted, findBracketMatchInBracket(f.bracket(), ko.ID).Status, "the played match is untouched")
}

// A queued write replayed after a newer result was stored is superseded before
// anything looks at the knockout, forced or not: nothing is reopened.
func TestRequalify_StaleReplayIsSuperseded(t *testing.T) {
	f := newRQFixture(t, "rq-stale", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	r := f.poolResult("Pool A-0", "A1")
	r.ModifiedAt = 5000
	require.NoError(t, f.write("Pool A-0", r))
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko := f.bracket().Rounds[0][0]
	require.NoError(t, f.scoreKO(ko.ID, "A1"))

	stale := f.poolResult("Pool A-0", "A2")
	stale.ModifiedAt = 1000
	err := f.write("Pool A-0", stale, ForceOptions{Force: true})
	require.ErrorIs(t, err, ErrMatchSuperseded)
	got := findBracketMatchInBracket(f.bracket(), ko.ID)
	assert.Equal(t, state.MatchStatusCompleted, got.Status)
	assert.Equal(t, "A1", got.Winner)
}

// The bare-store resolver never repaints a slot in a match that was already
// fought: standings can move without a correcting write (a pool-rank override,
// or a file edited by hand here), and a played match must not have its
// competitor swapped under its own verdict.
func TestResolveQualifiedPools_SkipsPlayedMatch(t *testing.T) {
	f := newRQFixture(t, "rq-skip", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko := f.bracket().Rounds[0][0]
	require.NoError(t, f.scoreKO(ko.ID, "A1"))

	matches, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == "Pool A-0" {
			matches[i].Winner, matches[i].WinnerID = "A2", rqID("A2")
			matches[i].IpponsA, matches[i].IpponsB = nil, []string{"M"}
		}
	}
	require.NoError(t, f.store.SavePoolMatches(f.compID, matches))
	f.resolve()

	got := findBracketMatchInBracket(f.bracket(), ko.ID)
	assert.Contains(t, []string{got.SideA, got.SideB}, "A1", "a played match keeps the competitor who fought it")
	assert.NotContains(t, []string{got.SideA, got.SideB}, "A2")
}

// The resolver's safety net must be all-or-nothing PER POOL. Skipping only the
// locked slot while repainting the pool's others seated one competitor twice:
// a 1st/2nd swap that reached the bare-store door (a rank override, a file
// edited by hand) kept A1 in the played "Pool A-1st" match and also painted
// A1 into the scheduled "Pool A-2nd" one. When any slot of a pool is locked
// and would get a different competitor, none of that pool's slots move.
func TestResolveQualifiedPools_LockedSlotFreezesItsWholePool(t *testing.T) {
	f := newRQFixture(t, "rq-freeze", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m1, _ := f.slot("Pool A-1st")
	m2, s2 := f.slot("Pool A-2nd")
	require.NotEqual(t, m1.ID, m2.ID, "the two places of one pool are seated in different matches")
	require.NoError(t, f.scoreKO(m1.ID, "A1"))

	matches, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == "Pool A-0" {
			matches[i].Winner, matches[i].WinnerID = "A2", rqID("A2")
			matches[i].IpponsA, matches[i].IpponsB = nil, []string{"M"}
		}
	}
	require.NoError(t, f.store.SavePoolMatches(f.compID, matches))
	f.resolve()

	seated := 0
	for _, m := range f.bracket().Rounds[0] {
		for _, id := range []string{m.SideAID, m.SideBID} {
			if id == rqID("A1") {
				seated++
			}
		}
	}
	assert.Equal(t, 1, seated, "A1 is seated once, never in the played match AND the 2nd-place slot")
	m2, _ = f.slot("Pool A-2nd")
	name, id := sideOf(m2, s2)
	assert.Equal(t, "A2", name, "the scheduled slot of a frozen pool is left as it was")
	assert.Equal(t, rqID("A2"), id)
}

// Once a mixed competition is in its knockout, almost every write is a
// knockout match or its autosave, and none of them can move who holds a pool
// place. The after-write auto-complete must not pay the pool pass for them
// (standings, both tie-break injectors, a bracket parse, under the
// per-competition lock). The standings cache is the observable: every pass
// computes standings, so an entry deleted before the call and still absent
// after it means none ran. The completed pool write at the end is the control
// that proves the observable is live.
func TestAutoCompleteAfterWrite_KnockoutWriteSkipsThePoolPass(t *testing.T) {
	f := newRQFixture(t, "rq-cost", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	comp, err := f.store.LoadCompetition(f.compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusKnockout, comp.Status, "the last pool seated flips the competition to knockout")
	ko := f.bracket().Rounds[0][0]
	require.NoError(t, f.scoreKO(ko.ID, "A1"))

	cached := func() bool {
		_, ok := f.eng.standingsCache.Load(f.compID)
		return ok
	}
	for _, w := range []state.MatchResult{
		{ID: ko.ID, Status: state.MatchStatusCompleted},
		{ID: ko.ID, Status: state.MatchStatusRunning},
		{ID: "Pool A-0", Status: state.MatchStatusRunning},
		{ID: "Pool A-0", Status: state.MatchStatusScheduled},
	} {
		f.eng.standingsCache.Delete(f.compID)
		outcome, err := f.eng.MaybeAutoCompletePoolsAfterWrite(f.compID, w)
		require.NoError(t, err)
		assert.Equal(t, AutoCompleteNoChange, outcome, "%s left %s", w.ID, w.Status)
		assert.False(t, cached(), "%s left %s reads no pool standings", w.ID, w.Status)
	}

	f.eng.standingsCache.Delete(f.compID)
	_, err = f.eng.MaybeAutoCompletePoolsAfterWrite(f.compID, state.MatchResult{ID: "Pool A-0", Status: state.MatchStatusCompleted})
	require.NoError(t, err)
	assert.True(t, cached(), "a completed pool write in knockout status still runs the pool pass")
}

// K3 must refuse BEFORE anything is written. A forced pool correction that is
// also a withdrawal, whose loser a different match has already made
// ineligible, used to land the pool row, reopen and repaint the knockout match
// the old qualifier had fought (restoring any eligibility its verdict
// recorded), and only then meet AlreadyIneligibleError: the rollback restored
// the pool row alone, and the refusal committed with the knockout reopened.
func TestRequalify_ForcedWithdrawalOfAnIneligibleLoserStagesNothing(t *testing.T) {
	f := newRQFixture(t, "rq-k3", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko := f.bracket().Rounds[0][0]
	require.NoError(t, f.scoreKO(ko.ID, "A1"))
	require.NoError(t, f.store.SetCompetitorStatus(f.compID, domain.CompetitorStatus{
		PlayerID: rqID("A1"), Eligible: false, MatchID: "elsewhere", Reason: "kiken-voluntary at elsewhere",
	}))
	bracketVersion := f.store.FileVersion(f.compID, "bracket.json")

	// A1 (aka, SideA) withdraws from Pool A-0, so A2 would take 1st place.
	r := f.poolResult("Pool A-0", "A2")
	r.Decision = string(domain.DecisionKikenVoluntary)
	r.DecisionBy = "aka"
	var reopened []ReopenedMatch
	err := f.write("Pool A-0", r, ForceOptions{Force: true, Reopened: &reopened})
	var already *AlreadyIneligibleError
	require.ErrorAs(t, err, &already)
	assert.Equal(t, "elsewhere", already.MatchID)

	assert.Empty(t, reopened, "nothing was reopened")
	got := findBracketMatchInBracket(f.bracket(), ko.ID)
	require.NotNil(t, got)
	assert.Equal(t, state.MatchStatusCompleted, got.Status, "the knockout match keeps its result")
	assert.Equal(t, "A1", got.Winner)
	assert.Contains(t, []string{got.SideA, got.SideB}, "A1")
	assert.Equal(t, bracketVersion, f.store.FileVersion(f.compID, "bracket.json"), "the bracket was not written")
	assert.Equal(t, "A1", loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Winner)
}

// An engi (kata) competition with pools and a knockout answers a pool
// correction exactly as a kendo one does. Its pool writes go through the engi
// recorder, which returned before the requalification check, so a 1st/2nd swap
// after the old 1st had fought silently left the played match standing on the
// old qualifier. Engi has no supplementary bouts and all its standings carry
// Points 0, so the tie check must not run for it: every place would read tied.
func TestRequalify_Engi_FirstSecondSwap_WarnsThenReopens(t *testing.T) {
	f := newRQFixture(t, "rq-engi", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}}, func(c *state.Competition) { c.Engi = true })
	engi := func(matchID string, flagsA, flagsB int, fo ...ForceOptions) error {
		t.Helper()
		return f.write(matchID, &state.MatchResult{FlagsA: flagsA, FlagsB: flagsB, Status: state.MatchStatusCompleted}, fo...)
	}
	require.NoError(t, engi("Pool A-0", 3, 0)) // A1 (side A) wins
	require.NoError(t, engi("Pool B-0", 3, 0))
	f.resolve()
	m1, s1 := f.slot("Pool A-1st")
	name, _ := sideOf(m1, s1)
	require.Equal(t, "A1", name)
	flagsA, flagsB := 3, 0
	if s1 == "B" {
		flagsA, flagsB = 0, 3
	}
	require.NoError(t, engi(m1.ID, flagsA, flagsB), "A1 fights and wins the 1st-place match")

	err := engi("Pool A-0", 0, 3)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played, "the correction names the match A1 already fought")
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, m1.ID, played.Blocking[0].ID)
	require.Len(t, played.QualifierChange, 2)
	for _, c := range played.QualifierChange {
		assert.False(t, c.Tied, "an engi place is never read as tied: %+v", c)
	}
	assert.Equal(t, "A2", played.QualifierChange[0].To.Name)
	stored := loadPoolMatchByID(t, f.store, f.compID, "Pool A-0")
	assert.Equal(t, "A1", stored.Winner, "refused until confirmed")
	assert.Equal(t, 3, stored.FlagsA, "the refusal restores the flags too")
	assert.Equal(t, 0, stored.FlagsB)

	var reopened []ReopenedMatch
	require.NoError(t, engi("Pool A-0", 0, 3, ForceOptions{Force: true, Reopened: &reopened}))
	require.Len(t, reopened, 1)
	assert.Equal(t, m1.ID, reopened[0].ID)
	got := findBracketMatchInBracket(f.bracket(), m1.ID)
	require.NotNil(t, got)
	assert.Equal(t, state.MatchStatusScheduled, got.Status)
	assert.Empty(t, got.Winner)
	name, id := sideOf(*got, s1)
	assert.Equal(t, "A2", name)
	assert.Equal(t, rqID("A2"), id)
	seated := map[string]int{}
	for _, m := range f.bracket().Rounds[0] {
		seated[m.SideAID]++
		seated[m.SideBID]++
	}
	assert.Equal(t, 1, seated[rqID("A1")], "A1 moves to the 2nd-place slot, seated once")
	assert.Equal(t, 1, seated[rqID("A2")])
}

// A pool-rank override (the chusen door) moves a pool's order without a match
// write, so it answers for the knockout exactly as a pool correction does:
// named and refused while the old 1st has fought, the override taken back; a
// knockout match being fought refuses it outright; confirmed, the fought match
// is reopened and both places are repainted, with nobody seated twice.
func TestOverridePoolRank_AnswersForTheKnockout(t *testing.T) {
	f := newRQFixture(t, "rq-rank", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	m1, s1 := f.slot("Pool A-1st")
	m2, s2 := f.slot("Pool A-2nd")
	ranks := func() map[string]int {
		o, err := f.store.LoadOverrides(f.compID)
		require.NoError(t, err)
		return o.PoolRanks["Pool A"]
	}

	// Running: terminal, never confirmable, override taken back.
	require.NoError(t, f.store.WithTransaction(f.compID, func(tx state.StoreTx) error {
		return tx.UpdateBracket(f.compID, func(b *state.Bracket) error {
			findBracketMatchInBracket(b, m1.ID).Status = state.MatchStatusRunning
			return nil
		})
	}))
	_, err := f.eng.OverridePoolRank(f.compID, "Pool A", rqID("A2"), 1, ForceOptions{Force: true})
	var running *DownstreamKnockoutRunningError
	require.ErrorAs(t, err, &running)
	assert.Empty(t, running.MatchID, "no match is being corrected")
	assert.Empty(t, ranks(), "the refused override is taken back")
	require.NoError(t, f.scoreKO(m1.ID, "A1"))

	// Played: named, with the places that move, and taken back.
	changed, err := f.eng.OverridePoolRank(f.compID, "Pool A", rqID("A2"), 1)
	assert.False(t, changed)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	assert.Empty(t, played.MatchID)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, m1.ID, played.Blocking[0].ID)
	require.Len(t, played.QualifierChange, 2)
	assert.Equal(t, QualifierIdentity{Name: "A2", ID: rqID("A2")}, played.QualifierChange[0].To)
	assert.Contains(t, played.Error(), "changing the ranking of Pool A")
	assert.Empty(t, ranks(), "the refused override is taken back")
	got := findBracketMatchInBracket(f.bracket(), m1.ID)
	assert.Equal(t, state.MatchStatusCompleted, got.Status, "nothing reopened before the operator confirms")

	// Confirmed: reopened, repainted, nobody twice.
	var reopened []ReopenedMatch
	changed, err = f.eng.OverridePoolRank(f.compID, "Pool A", rqID("A2"), 1, ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, 1, ranks()[helper.CompetitorKey(rqID("A2"), "", "")])
	require.Len(t, reopened, 1)
	assert.Equal(t, m1.ID, reopened[0].ID)
	b := f.bracket()
	got = findBracketMatchInBracket(b, m1.ID)
	assert.Equal(t, state.MatchStatusScheduled, got.Status)
	assert.Equal(t, "reopened: the ranking of Pool A was changed", got.CorrectionReason)
	name, _ := sideOf(*got, s1)
	assert.Equal(t, "A2", name)
	name, _ = sideOf(*findBracketMatchInBracket(b, m2.ID), s2)
	assert.Equal(t, "A1", name)
	seated := map[string]int{}
	for _, m := range b.Rounds[0] {
		seated[m.SideAID]++
		seated[m.SideBID]++
	}
	assert.Equal(t, 1, seated[rqID("A1")])
	assert.Equal(t, 1, seated[rqID("A2")])
}

// Re-entering a match a pool correction reopened, with the SAME winner the
// next round already holds, moves nobody there, so it must not warn. A
// different winner still does.
func TestRequalify_ReenterReopenedSameWinnerDoesNotWarn(t *testing.T) {
	f := newRQFixture(t, "rq-reenter", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	mA1, _ := f.slot("Pool A-1st") // A1 v B2
	mA2, _ := f.slot("Pool A-2nd") // B1 v A2
	require.NoError(t, f.scoreKO(mA1.ID, "B2"))
	require.NoError(t, f.scoreKO(mA2.ID, "B1"))
	final := f.bracket().Rounds[1][0]
	require.NoError(t, f.scoreKO(final.ID, "B2"))

	var reopened []ReopenedMatch
	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A2"), ForceOptions{Force: true, Reopened: &reopened}))
	require.Len(t, reopened, 2)
	require.Equal(t, state.MatchStatusCompleted, findBracketMatchInBracket(f.bracket(), final.ID).Status, "one hop: the played final is left for its own turn")

	// A2 v B2 again, B2 wins again: the final already holds B2.
	require.NoError(t, f.scoreKO(mA1.ID, "B2"), "the same winner moves nobody in the final")
	// B1 v A1 again, A1 wins: the final holds B1, so that one must warn.
	err := f.scoreKO(mA2.ID, "A1")
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	assert.Equal(t, final.ID, played.BlockingMatchID)
	assert.Empty(t, played.QualifierChange, "a knockout correction moves no pool place")
}

// Sending a running pool match back to the queue stamps it, as the bracket
// branch always did, so an older queued write replayed afterwards cannot
// resurrect a result the operator discarded.
func TestRevertMatchToQueue_PoolStampsRevertFence(t *testing.T) {
	f := newRQFixture(t, "rq-revert", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	now := time.Now().UnixMilli()
	running := f.poolResult("Pool A-0", "A1")
	running.Status, running.Winner, running.WinnerID = state.MatchStatusRunning, "", ""
	running.ModifiedAt = now - 10_000
	require.NoError(t, f.write("Pool A-0", running))

	require.NoError(t, f.eng.RevertMatchToQueue(f.compID, "Pool A-0"))
	reverted := loadPoolMatchByID(t, f.store, f.compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusScheduled, reverted.Status)
	assert.GreaterOrEqual(t, reverted.ModifiedAt, now, "the requeue is stamped with the server's now")

	stale := f.poolResult("Pool A-0", "A1")
	stale.ModifiedAt = now - 5_000 // written before the requeue, replayed after it
	require.ErrorIs(t, f.write("Pool A-0", stale), ErrMatchSuperseded)
	assert.Equal(t, state.MatchStatusScheduled, loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Status)
}

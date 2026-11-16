package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The participant ids the lineup fixtures seat RedTeam and WhiteTeam under: a
// lineup is a team's by its participant id, so a fixture that saves one stamps
// the same id on the match's side.
const (
	redTeamID   = "red-id"
	whiteTeamID = "white-id"
)

// fighters builds an []kachinukiFighter queue from bare names, with no
// member id on any entry -- the shape every pre-bc-tmid test literal used
// (a plain []string) now needs to take, since AdvanceKachinukiInput's
// SideA/SideB carry the fighter's possible member id alongside the name.
func fighters(names ...string) []kachinukiFighter {
	out := make([]kachinukiFighter, len(names))
	for i, n := range names {
		out[i] = kachinukiFighter{Name: n}
	}
	return out
}

// setupKachinukiComp builds an engine + store and saves a kachinuki
// competition with an empty pool-matches file, the setup every kachinuki
// advancement/export test shares. opts mutate the competition before it is
// saved (e.g. teamSize, Naginata, Format).
func setupKachinukiComp(t *testing.T, id string, teamSize int, opts ...func(*state.Competition)) (*Engine, *state.Store, *state.Competition) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID:            id,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      teamSize,
	}
	for _, o := range opts {
		o(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{}))
	return eng, store, comp
}

// reopenErr is ReopenMatch for a test that asserts only the error;
// the restored status is pinned by kachinuki_reopen_withdrawal_test.go.
func reopenErr(eng *Engine, compID, matchID, reason string) error {
	_, err := eng.ReopenMatch(compID, matchID, reason)
	return err
}

// requeueReopenErr is RequeueBlockerAndReopen for a test that
// asserts only the error.
func requeueReopenErr(eng *Engine, targetComp, targetMatch, blockerComp, blockerMatch, reason string) error {
	_, err := eng.RequeueBlockerAndReopen(targetComp, targetMatch, blockerComp, blockerMatch, reason)
	return err
}

// saveMixedKachinukiCompForReopenTest builds a mixed KACHINUKI competition (two
// 1-winner pools feeding a single knockout leaf) for exercising the pool reopen
// downstream guard. Standings derive W/L straight from the match Winner
// (computeStandingsFrom), so A1/B1 rank first in their pools once their pool
// matches complete, regardless of the (absent) sub-bout detail.
func saveMixedKachinukiCompForReopenTest(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-reopen-guard"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"}}},
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		Name:          compID,
		Kind:          "team",
		Format:        state.CompFormatMixed,
		Status:        state.CompStatusPools,
		Courts:        []string{"A"},
		StartTime:     "09:00",
		PoolWinners:   1,
		TeamSize:      2,
		TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	bctest.StampPoolIDs(pools)
	require.NoError(t, store.SavePools(compID, pools))
	players := []domain.Player{
		{Name: "A1", Dojo: "Dojo A1"}, {Name: "A2", Dojo: "Dojo A2"}, {Name: "B1", Dojo: "Dojo B1"}, {Name: "B2", Dojo: "Dojo B2"},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	draw := helper.BuildKnockoutDraw(pools, 1, 1)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := eng.buildBracketFromDraw(comp, draw, nil, nil, nil)
	require.NoError(t, err)
	bracket.Preview = true
	require.NoError(t, store.SaveBracket(compID, bracket))
	return eng, store, compID
}

func loadPoolMatchByID(t *testing.T, store *state.Store, compID, matchID string) *state.MatchResult {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == matchID {
			return &matches[i]
		}
	}
	return nil
}

// runningOnCourt builds a RUNNING team match (no score yet) holding court — the
// bare "this match occupies the court" fixture shared across the reopen/requeue
// tests. completedOnCourt is its finished counterpart; scoredBlocker adds a score.
func runningOnCourt(id, court string) state.MatchResult {
	return state.MatchResult{
		ID: id, SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: court,
	}
}

// completedOnCourt builds a COMPLETED kachinuki team match (RedTeam over WhiteTeam
// by exhaustion, one scored bout) holding court.
func completedOnCourt(id, court string) state.MatchResult {
	return state.MatchResult{
		ID: id, SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
		Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: court,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
		},
	}
}

// scoredBlocker is runningOnCourt plus a live score (one ippon) and a single
// scored bout — the state a destructive requeue would wipe. Composing on
// runningOnCourt keeps the blocker fixture in lockstep with the plain running
// fixture. assertBlockerIntact checks exactly this shape survives.
func scoredBlocker(id, court string) state.MatchResult {
	m := runningOnCourt(id, court)
	m.IpponsA = []string{"M"}
	m.SubResults = []state.SubMatchResult{{Position: 1, SideA: "K-1", SideB: "Wa-1", IpponsA: []string{"M"}}}
	return m
}

// foughtDownstreamBracket returns a two-round bracket whose semifinal SF0
// (completed on court A, RedTeam by exhaustion) has already fed the RUNNING final
// F0 — SF0's winner sits in a fought downstream, so reopening SF0 is refused.
func foughtDownstreamBracket() *state.Bracket {
	return &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
					Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A"},
				{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted, Winner: "Kuma"},
			},
			{
				// The final is already being fought — SF0's winner fed it.
				{ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1"}}},
			},
		},
	}
}

// assertBlockerIntact verifies the named match still holds the running status,
// score, and single-bout log that a destructive requeue would have cleared — the
// "blocker must NOT be wiped" invariant the requeue pre-checks protect. Pairs
// with scoredBlocker, which builds that exact shape.
func assertBlockerIntact(t *testing.T, store *state.Store, compID, matchID string) {
	t.Helper()
	b := loadPoolMatchByID(t, store, compID, matchID)
	require.NotNil(t, b)
	assert.Equal(t, state.MatchStatusRunning, b.Status, "blocker must NOT be requeued")
	assert.Equal(t, []string{"M"}, b.IpponsA, "blocker's score must NOT be wiped")
	assert.Len(t, b.SubResults, 1, "blocker's bout log must NOT be wiped")
}

// startedMixedKnockout extends saveMixedKachinukiCompForReopenTest by scoring
// both pools (A1, B1 advance), resolving them, and starting the single knockout
// leaf (A1 vs B1) RUNNING — so A1, Pool A-0's finisher, is committed to a fought
// downstream. It is the shared fixture for the reopen-refusal cases that need a
// pool finisher already sitting in a started bracket match.
func startedMixedKnockout(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, compID := saveMixedKachinukiCompForReopenTest(t)
	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")
	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, b.Rounds, 1)
	require.Len(t, b.Rounds[0], 1)
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, b.Rounds[0][0].ID, &state.MatchResult{
			SideA: "A1", SideB: "B1", Status: state.MatchStatusRunning,
		})
		return e
	}))
	return eng, store, compID
}

// kachinukiRemainingRoster is a TEST helper: it composes the advance's own
// queues (kachinukiRosterQueues) with RetiredPlayersFromBoutLog the way the
// advance does, so the tests below pin that composition. It derives the
// remaining un-retired roster per side
// for a team match. Returns (sideA, sideB, foundA, foundB): found is per
// side, true when that side's roster was resolved from a saved TeamLineup and
// false when it fell back to the bout-log-only heuristic. The advance logs
// foundA || foundB. The queues come from kachinukiRosterQueues, which the
// advisory read (KachinukiRoster) builds from too.
//
// A lineup in force that fields nobody (a match's own empty lineup,
// positions: {}) is FOUND here and yields an empty queue, so the advance reads
// that side as exhausted and appends nothing; the advisory read instead
// reports such a side as having no lineup (see KachinukiRosterSide).
//
// Priority per side (GAP 1 / GAP 2a):
//  1. The lineup in force for the side's team at this match (see
//     lineup_in_force.go: the match's own lineup, else the one the team
//     carries from its previous match or round), matched by the side's
//     participant id; a side with no id has none.
//  2. Bout-log-only heuristic (anyone who appeared in a bout, minus retired).
//
// The full ordered roster (from lineup.OrderedMembers, bc-tmid pass 2) is
// filtered by IsMemberRetired against RetiredPlayersFromBoutLog's sets to
// produce the remaining queue, so a lineup-resolved fighter's member id
// (when the slot has one) rather than their possibly-stale name decides
// whether they have retired -- falling back to the name only where the
// retiring bout row carried no id to match against and the name belongs to
// one member of this roster alone. See IsMemberRetired for why both tiers
// are needed and what the gate protects.
func kachinukiRemainingRoster(comp *state.Competition, parent *state.MatchResult, rule *lineupRule) (remainingA, remainingB []kachinukiFighter, foundA, foundB bool) {
	queueA, queueB := kachinukiRosterQueues(comp, parent, rule)
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)
	return queueA.remaining(retiredA), queueB.remaining(retiredB), queueA.found, queueB.found
}

package engine

// bc-tmfn follow-up: a team match a default-win ruling (any kiken, fusenpai,
// or fusensho) closes before every numbered bout has a result credits each
// unfought bout to the ruling's winning side (state.DefaultWinCreditSide /
// SubBoutEffectiveResult), and the write path pads the stored SubResults with
// an empty row per missing position so a reader has something to credit
// (state.PadDefaultWinBoutPositions, engine.RecordMatchResultWithIneligibilityTx).
// These pin the padding at write time (pool and bracket), its exclusion for
// kachinuki, the legacy-load repair, and the standings/wire consequence end
// to end.

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDefaultWinBoutCredit_PoolStandings is the scenario from the operator
// ruling: team size 3, a pool of three teams. Yama C v Tora A is decided by
// Kiken-Voluntary against Yama C before any bout is fought; Umi E v Tora A is
// fought to two draws and one Tora A win (IV 0-1, PW 0-1). Tora A's kiken
// match alone must credit it IV 3 / PW 6; Yama C's only completed match must
// leave it IL 3 / PL 6; and Yama C, tied with Umi E on team W/L and IV, must
// rank BELOW Umi E on the lower-IL tiebreak (CLAUDE.md Team Tournaments
// tie-break #5).
func TestDefaultWinBoutCredit_PoolStandings(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-pool"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Default Win Bout Credit Pool",
		Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 3, PoolWinners: 3,
	}))
	players := []helper.Player{{Name: "Yama C"}, {Name: "Tora A"}, {Name: "Umi E"}}
	matches := []state.MatchResult{
		// Yama C v Tora A: running, no bout fought yet. Decided below via the
		// real engine decision path, which pads bouts 1-3 (see
		// engine.RecordMatchResultWithIneligibilityTx).
		{ID: "Pool A-0", SideA: "Yama C", SideB: "Tora A", Court: "A", Status: state.MatchStatusRunning},
		// Umi E v Tora A: fought. Bouts 1-2 draw (hikiwake, no points), bout 3
		// Tora A's fighter wins 1-0. Team-level Winner/WinnerID mirror the
		// FIK team-match rule (highest IV, CLAUDE.md "Team Match Winning
		// Criteria"): Tora A's 1 individual win beats Umi E's 0.
		{ID: "Pool A-1", SideA: "Umi E", SideB: "Tora A", Court: "A", Status: state.MatchStatusCompleted,
			Winner: "Tora A",
			SubResults: []state.SubMatchResult{
				{Position: 1, Decision: "hikiwake"},
				{Position: 2, Decision: "hikiwake"},
				{Position: 3, Winner: "Tora A", IpponsB: []string{"M"}},
			},
		},
		// Yama C v Umi E: not yet played. Left out of every completed-match
		// tally on purpose, so Yama C's and Umi E's stats below come from
		// exactly the one match each the scenario describes.
		{ID: "Pool A-2", SideA: "Yama C", SideB: "Umi E", Court: "A", Status: state.MatchStatusScheduled},
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))

	// Yama C (aka = SideA) withdraws; Tora A (shiro = SideB) is credited.
	_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "aka", "no-show", nil, false)
	require.NoError(t, err)

	// The kiken match's OWN contribution, isolated: bouts 1-3 padded and
	// credited to Tora A.
	kikenMatch, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var pool0 *state.MatchResult
	for i := range kikenMatch {
		if kikenMatch[i].ID == "Pool A-0" {
			pool0 = &kikenMatch[i]
		}
	}
	require.NotNil(t, pool0)
	require.Len(t, pool0.SubResults, 3, "bouts 1-3 padded")
	line := pool0.TeamResult()
	require.NotNil(t, line)
	assert.Equal(t, 3, line.ShiroIV, "Tora A (shiro) credited all 3 bouts")
	assert.Equal(t, 6, line.ShiroPW, "3 bouts x 2 maru points each")
	assert.Equal(t, 0, line.AkaIV)
	assert.Equal(t, 0, line.AkaPW)

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	byName := make(map[string]state.PlayerStanding, len(standings["Pool A"]))
	for _, s := range standings["Pool A"] {
		byName[s.Player.Name] = s
	}
	require.Contains(t, byName, "Yama C")
	require.Contains(t, byName, "Tora A")
	require.Contains(t, byName, "Umi E")

	yamaC := byName["Yama C"]
	assert.Equal(t, 0, yamaC.Wins)
	assert.Equal(t, 1, yamaC.Losses)
	assert.Equal(t, 0, yamaC.IndividualWins, "IV")
	assert.Equal(t, 3, yamaC.IndividualLosses, "IL: all 3 bouts credited to Tora A")
	assert.Equal(t, 0, yamaC.PointsWon, "PW")
	assert.Equal(t, 6, yamaC.PointsLost, "PL: 3 bouts x 2 maru points")

	toraA := byName["Tora A"]
	assert.Equal(t, 2, toraA.Wins, "both matches")
	assert.Equal(t, 0, toraA.Losses)
	// 3 credited (kiken) + 1 real (bout 3 v Umi E) = 4; 6 credited + 1 real = 7.
	assert.Equal(t, 4, toraA.IndividualWins, "IV across both matches")
	assert.Equal(t, 7, toraA.PointsWon, "PW across both matches")

	umiE := byName["Umi E"]
	assert.Equal(t, 0, umiE.Wins)
	assert.Equal(t, 1, umiE.Losses)
	assert.Equal(t, 0, umiE.IndividualWins)
	assert.Equal(t, 1, umiE.IndividualLosses, "only bout 3 was lost; bouts 1-2 are draws (IT), not losses")
	assert.Equal(t, 2, umiE.IndividualDraws, "IT: bouts 1-2")

	// Yama C (IL 3) ranks below Umi E (IL 1): tied on W/L/IV, the lower-IL
	// tiebreak decides (CLAUDE.md Team Tournaments tie-break #5).
	order := poolOrder(standings["Pool A"])
	assert.Equal(t, []string{"Tora A", "Umi E", "Yama C"}, order)
}

// TestDefaultWinBoutCredit_BracketTeamResult is the knockout variant: a
// bracket team match decided by kiken before any bout is fought must credit
// the wire teamResult the same way a pool match does.
func TestDefaultWinBoutCredit_BracketTeamResult(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "dwb", Kind: "team", TeamSize: 3, Status: state.CompStatusKnockout,
	}))
	players := []helper.Player{{Name: "Ryu"}, {Name: "Tora"}}
	b := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning},
	}}}
	matches := []state.MatchResult{{ID: "m-r1-0", SideA: "Ryu", SideB: "Tora"}}
	bctest.StampIDs(players, matches)
	b.Rounds[0][0].SideAID = matches[0].SideAID
	b.Rounds[0][0].SideBID = matches[0].SideBID
	require.NoError(t, store.SaveBracket(compID, b))

	// Ryu (shiro = SideB) withdraws; Tora (aka = SideA) is credited.
	_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "shiro", "no-show", nil, false)
	require.NoError(t, err)

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := loaded.Rounds[0][0]
	require.Len(t, m.SubResults, 3, "bouts 1-3 padded")
	line := m.TeamResult()
	require.NotNil(t, line)
	assert.Equal(t, 3, line.AkaIV, "Tora (aka) credited all 3 bouts")
	assert.Equal(t, 6, line.AkaPW)
	assert.Equal(t, 0, line.ShiroIV)
	assert.Equal(t, 0, line.ShiroPW)
}

// TestDefaultWinBoutCredit_PoolWritePads pins the write-time padding
// (engine.RecordMatchResultWithIneligibilityTx) on the pool branch in
// isolation from any standings computation: a kiken before any bout is
// scored leaves one empty row per missing position.
func TestDefaultWinBoutCredit_PoolWritePads(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-pool-pad"
	createTestCompetition(t, store, compID, "league", 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
	})
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Red", SideB: "White", Status: state.MatchStatusRunning,
	}}))
	result, _, err := eng.RecordDecision(compID, "Pool A-0", "fusenpai", "aka", "no-show", nil, false)
	require.NoError(t, err)

	// Assert on the WRITE's own returned result first, in memory, before any
	// further store.Load* call: store.LoadPoolMatches below would ALSO pad a
	// stale file via the legacy-load repair (state.EnsureLegacyUpgraded), so
	// checking only the reloaded copy could not tell write-time padding
	// (engine.RecordMatchResultWithIneligibilityTx) apart from that safety
	// net catching up on read.
	require.Len(t, result.SubResults, 3, "the write's own returned result is already padded")
	for i, wantPos := range []int{1, 2, 3} {
		assert.Equal(t, wantPos, result.SubResults[i].Position)
		assert.False(t, result.SubResults[i].HasResult(), "padded row has no result")
	}

	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, ms, 1)
	require.Len(t, ms[0].SubResults, 3, "and it persisted to disk")
	for i, wantPos := range []int{1, 2, 3} {
		assert.Equal(t, wantPos, ms[0].SubResults[i].Position)
		assert.False(t, ms[0].SubResults[i].HasResult())
	}
}

// TestDefaultWinBoutCredit_BracketWritePads is the bracket branch twin of
// TestDefaultWinBoutCredit_PoolWritePads.
func TestDefaultWinBoutCredit_BracketWritePads(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "dwb-bracket-pad"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "dwb", Kind: "team", TeamSize: 3, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "Red", SideB: "White", Status: state.MatchStatusRunning},
	}}}))
	result, _, err := eng.RecordDecision(compID, "m-r1-0", "kiken-injury", "shiro", "injured", nil, false)
	require.NoError(t, err)

	// Assert on the WRITE's own returned result first, in memory -- see
	// TestDefaultWinBoutCredit_PoolWritePads for why this must not be
	// skipped in favour of only the reloaded copy.
	require.Len(t, result.SubResults, 3, "the write's own returned result is already padded")
	for i, wantPos := range []int{1, 2, 3} {
		assert.Equal(t, wantPos, result.SubResults[i].Position)
		assert.False(t, result.SubResults[i].HasResult())
	}

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := b.Rounds[0][0]
	require.Len(t, m.SubResults, 3, "and it persisted to disk")
	for i, wantPos := range []int{1, 2, 3} {
		assert.Equal(t, wantPos, m.SubResults[i].Position)
		assert.False(t, m.SubResults[i].HasResult())
	}
}

// TestDefaultWinBoutCredit_KachinukiNotPadded pins that a kachinuki match is
// never padded: kachinuki's completed write strips trailing unfought
// pairings on its own (applyKachinukiMerge), and the default-win padding
// must not add any back.
func TestDefaultWinBoutCredit_KachinukiNotPadded(t *testing.T) {
	eng, store, _ := setupKachinukiComp(t, "dwb-kachinuki", 3)
	require.NoError(t, store.SavePoolMatches("dwb-kachinuki", []state.MatchResult{{
		ID: "Pool A-0", SideA: "Red", SideB: "White", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideAMemberID: "r1", SideBMemberID: "w1", WinnerMemberID: "r1", Winner: "r1", IpponsA: []string{"M"}},
		},
	}}))
	_, _, err := eng.RecordDecision("dwb-kachinuki", "Pool A-0", "kiken-voluntary", "shiro", "withdrew", nil, false)
	require.NoError(t, err)

	ms, err := store.LoadPoolMatches("dwb-kachinuki")
	require.NoError(t, err)
	require.Len(t, ms, 1)
	// Never padded to teamSize (3): kachinuki is excluded from
	// state.PadDefaultWinBoutPositions entirely (comp.IsKachinuki() guard in
	// RecordMatchResultWithIneligibilityTx).
	assert.Less(t, len(ms[0].SubResults), 3, "kachinuki bouts are never padded up to TeamSize")
}

// TestDefaultWinBoutCredit_LegacyLoadPads pins the load-time repair
// (state.EnsureLegacyUpgraded / upgradeTeamDefaultWinBoutPaddingLocked): a
// completed default-win team match saved directly (bypassing the engine
// write path entirely, as a pre-padding file on disk would have been) is
// padded the first time it is loaded.
func TestDefaultWinBoutCredit_LegacyLoadPads(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	const compID = "dwb-legacy"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "dwb legacy", Format: state.CompFormatLeague, Status: state.CompStatusPools,
		Courts: []string{"A"}, Kind: "team", TeamSize: 3, PoolWinners: 2,
	}))
	// Saved directly, bypassing RecordDecision entirely: a completed kiken
	// with only ONE bout row (won by Red, the side that goes on to
	// withdraw), the on-disk shape a file predating write-time padding
	// would have. DecisionBy "aka" credits White (shiro) for bouts 2-3.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Red", SideB: "White", Status: state.MatchStatusCompleted,
		Decision: "kiken-voluntary", DecisionBy: "aka", Winner: "White",
		SubResults: []state.SubMatchResult{{Position: 1, Winner: "Red", IpponsA: []string{"M"}}},
	}}))

	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, ms, 1)
	require.Len(t, ms[0].SubResults, 3, "bouts 2 and 3 padded on load")
	assert.Equal(t, 1, ms[0].SubResults[0].Position)
	assert.Equal(t, []string{"M"}, ms[0].SubResults[0].IpponsA, "the real bout is untouched")
	assert.Equal(t, "Red", ms[0].SubResults[0].Winner, "bout 1's own winner survives, even though Red goes on to withdraw")
	assert.Equal(t, 2, ms[0].SubResults[1].Position)
	assert.False(t, ms[0].SubResults[1].HasResult())
	assert.Equal(t, 3, ms[0].SubResults[2].Position)
	assert.False(t, ms[0].SubResults[2].HasResult())

	// The credit follows through to the wire teamResult too: bout 1 keeps
	// its own result (Red/aka won it), bouts 2-3 are credited to White
	// (shiro) by the match-level kiken.
	line := ms[0].TeamResult()
	require.NotNil(t, line)
	assert.Equal(t, 1, line.AkaIV, "bout 1's own result: Red won it")
	assert.Equal(t, 1, line.AkaPW)
	assert.Equal(t, 2, line.ShiroIV, "White (shiro) credited bouts 2-3")
	assert.Equal(t, 4, line.ShiroPW)
}

// TestDefaultWinBoutCredit_PoolDaihyosenRowNeverPadded pins that a pool
// daihyosen/tiebreaker row (a single individual representative bout, never a
// team's own numbered positions -- IsPoolDaihyosenMatchID / IsTiebreakerMatchID)
// is never padded, on the write path or the legacy-load repair, even when it
// carries a completed default-win decision at TeamSize >= 2.
func TestDefaultWinBoutCredit_PoolDaihyosenRowNeverPadded(t *testing.T) {
	for i, matchID := range []string{"Pool A-DH-1", "Pool A-TB-1"} {
		t.Run(matchID, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := fmt.Sprintf("dwb-dh-%d", i)
			createTestCompetition(t, store, compID, "league", 3, func(c *state.Competition) {
				c.Kind = "team"
				c.TeamSize = 3
			})
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: matchID, SideA: "Red", SideB: "White", Status: state.MatchStatusRunning,
			}}))
			result, _, err := eng.RecordDecision(compID, matchID, "kiken-voluntary", "aka", "no-show", nil, false)
			require.NoError(t, err)
			assert.Empty(t, result.SubResults, "the write path never pads a pool DH/TB row")

			ms, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, ms, 1)
			assert.Empty(t, ms[0].SubResults, "the legacy-load repair never pads a pool DH/TB row either")
		})
	}
}

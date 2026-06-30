package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Validation -------------------------------------------------------------

func TestEngiValidTotal(t *testing.T) {
	cases := []struct {
		a, b int
		ok   bool
	}{
		// Accepted: odd total in {1,3,5}, no draw.
		{1, 0, true},
		{0, 1, true},
		{3, 0, true},
		{2, 1, true},
		{1, 2, true},
		{5, 0, true},
		{4, 1, true},
		{3, 2, true},
		// Rejected: zero total.
		{0, 0, false},
		// Rejected: even totals (would allow a draw).
		{2, 0, false},
		{1, 1, false},
		{4, 2, false},
		{2, 2, false},
		// Rejected: totals over 5.
		{7, 0, false},
		{6, 1, false},
		{4, 3, false},
		// Rejected: negative.
		{-1, 2, false},
	}
	for _, c := range cases {
		assert.Equalf(t, c.ok, engiValidTotal(c.a, c.b), "engiValidTotal(%d,%d)", c.a, c.b)
	}
}

func TestEngiWinnerSide(t *testing.T) {
	assert.Equal(t, "A", engiWinnerSide(3, 2))
	assert.Equal(t, "B", engiWinnerSide(2, 3))
	assert.Equal(t, "A", engiWinnerSide(5, 0))
	assert.Equal(t, "B", engiWinnerSide(0, 1))
}

// The wins-then-flags ranking is exercised end-to-end by
// TestComputeEngiStandings_PoolWinsThenFlags and
// TestComputeEngiStandings_FlagTiebreak.

// --- Helpers ---------------------------------------------------------------

func createEngiCompetition(t *testing.T, store *state.Store, id, format string, poolSize int) {
	t.Helper()
	comp := &state.Competition{
		ID:           id,
		Name:         "Engi Test",
		Kind:         "individual",
		Format:       format,
		PoolSize:     poolSize,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
		Engi:         true,
	}
	require.NoError(t, store.SaveCompetition(comp))
}

// --- Pool match recording --------------------------------------------------

func TestRecordEngiMatchResult_PoolFlagMajority(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-pool"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	first := matches[0]

	res, err := eng.recordEngiMatchResult(compID, first.ID, 3, 2, "")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, first.SideA, res.Winner, "3-2 → SideA wins")
	assert.Equal(t, 3, res.FlagsA)
	assert.Equal(t, 2, res.FlagsB)
	assert.Equal(t, state.MatchStatusCompleted, res.Status)

	// Persisted to disk.
	matches, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == first.ID {
			assert.Equal(t, 3, m.FlagsA)
			assert.Equal(t, 2, m.FlagsB)
			assert.Equal(t, first.SideA, m.Winner)
		}
	}
}

func TestRecordEngiMatchResult_InvalidRejected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-invalid"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	first := matches[0]

	for _, bad := range [][2]int{{0, 0}, {2, 0}, {1, 1}, {4, 2}, {7, 0}} {
		_, err := eng.recordEngiMatchResult(compID, first.ID, bad[0], bad[1], "")
		assert.Errorf(t, err, "expected %d-%d to be rejected", bad[0], bad[1])
	}
}

// --- Bracket match recording -----------------------------------------------

func TestRecordEngiMatchResult_BracketAdvances(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-bracket"

	comp := &state.Competition{
		ID: compID, Name: "Engi Knockout", Kind: "individual",
		Format: state.CompFormatPlayoffs, PoolSize: 3, PoolWinners: 2,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup", Engi: true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)

	// SF0: 3-2 → SideA advances.
	res, err := eng.recordEngiMatchResult(compID, sf[0].ID, 3, 2, "")
	require.NoError(t, err)
	assert.Equal(t, sf[0].SideA, res.Winner)

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	// Final's SideA should now be SF0's winner.
	final := bracket.Rounds[len(bracket.Rounds)-1][0]
	assert.Equal(t, sf[0].SideA, final.SideA, "flag-decided winner propagated to final")
	// Flag counts persisted in bracket.json.
	assert.Equal(t, 3, bracket.Rounds[sfIdx][0].FlagsA)
	assert.Equal(t, 2, bracket.Rounds[sfIdx][0].FlagsB)
}

// --- Standings -------------------------------------------------------------

// TestComputeEngiStandings_PoolWinsThenFlags builds a 3-player round-robin and
// scores it so the ranking is decided first by wins, then by accumulated flags.
func TestComputeEngiStandings_PoolWinsThenFlags(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-standings"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 3, "3-player round-robin → 3 matches")

	// Score every match. We give Alice both her wins, Bob beats Charlie.
	// Standings: Alice 2 wins, Bob 1 win, Charlie 0 wins.
	for _, m := range matches {
		var fa, fb int
		// Alice always wins her matches; otherwise the alphabetically-first side wins.
		switch {
		case m.SideA == "Alice":
			fa, fb = 3, 2
		case m.SideB == "Alice":
			fa, fb = 2, 3
		case m.SideA == "Bob":
			fa, fb = 5, 0
		default:
			fa, fb = 0, 5
		}
		_, err := eng.recordEngiMatchResult(compID, m.ID, fa, fb, "")
		require.NoError(t, err)
	}

	standings, err := eng.computeStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 1)
	var rows []state.PlayerStanding
	for _, v := range standings {
		rows = v
	}
	require.Len(t, rows, 3)

	assert.Equal(t, "Alice", rows[0].Player.Name, "2 wins → 1st")
	assert.Equal(t, 2, rows[0].Wins)
	assert.Equal(t, "Bob", rows[1].Player.Name, "1 win → 2nd")
	assert.Equal(t, 1, rows[1].Wins)
	assert.Equal(t, "Charlie", rows[2].Player.Name, "0 wins → 3rd")
	assert.Equal(t, 0, rows[2].Wins)

	// Flag accrual: winner AND loser both accumulate own-side flags. Alice won
	// 3-2 and 2-3-as-SideB → accrued flags from both her bouts.
	assert.Greater(t, rows[0].Flags, 0, "winner accrues own-side flags")
	assert.Greater(t, rows[2].Flags, 0, "loser also accrues own-side flags")
}

// TestComputeEngiStandings_FlagTiebreak verifies that when wins tie, the higher
// accumulated flag count ranks first.
func TestComputeEngiStandings_FlagTiebreak(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-flagtie"

	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)
	// Use 2 pools is not desired; one league pool of 4.
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// Make Alice and Bob each win 2 (tie on wins), but Alice wins by larger
	// margins (more own-side flags) so she ranks above Bob.
	for _, m := range matches {
		var fa, fb int
		win := func(side string) (int, int) {
			// big margin for Alice, small for Bob
			if side == "Alice" {
				return 5, 0
			}
			if side == "Bob" {
				return 3, 2
			}
			return 3, 0
		}
		switch {
		case m.SideA == "Alice":
			fa, fb = win("Alice")
		case m.SideB == "Alice":
			fb, fa = win("Alice")
		case m.SideA == "Bob":
			fa, fb = win("Bob")
		case m.SideB == "Bob":
			fb, fa = win("Bob")
		default:
			fa, fb = 3, 0
		}
		_, err := eng.recordEngiMatchResult(compID, m.ID, fa, fb, "")
		require.NoError(t, err)
	}

	standings, err := eng.computeStandings(compID)
	require.NoError(t, err)
	var rows []state.PlayerStanding
	for _, v := range standings {
		rows = v
	}
	require.Len(t, rows, 4)
	// Find Alice and Bob.
	var aliceRank, bobRank, aliceFlags, bobFlags int
	for _, r := range rows {
		if r.Player.Name == "Alice" {
			aliceRank, aliceFlags = r.Rank, r.Flags
		}
		if r.Player.Name == "Bob" {
			bobRank, bobFlags = r.Rank, r.Flags
		}
	}
	assert.Greater(t, aliceFlags, bobFlags, "Alice has more own-side flags")
	assert.Less(t, aliceRank, bobRank, "more flags ranks above on a wins tie")
}

// --- Regression: kendo path unchanged --------------------------------------

// TestEngiDispatch_DoesNotAffectKendo proves the engi seam is branched-around:
// a non-engi (kendo) competition's standings are computed via the kendo path
// and are identical whether or not engi support is compiled in.
func TestEngiDispatch_DoesNotAffectKendo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kendo-regression"

	createTestCompetition(t, store, compID, state.CompFormatLeague, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &state.MatchResult{
			Winner:  m.SideA,
			IpponsA: []string{"M", "K"},
			Status:  state.MatchStatusCompleted,
		}))
	}

	standings, err := eng.computeStandings(compID)
	require.NoError(t, err)
	var rows []state.PlayerStanding
	for _, v := range standings {
		rows = v
	}
	require.NotEmpty(t, rows)
	// Kendo standings populate IpponsGiven; engi never touches that field. The
	// winning side accrued ippons, proving the kendo path ran (not the engi one).
	total := 0
	for _, r := range rows {
		total += r.IpponsGiven
		assert.Zero(t, r.Flags, "kendo standings must not populate the engi Flags field")
	}
	assert.Greater(t, total, 0, "kendo standings still record ippons")
}

// TestEngi_PairParticipantRoundTrip verifies an engi-pair participant row
// (member 1, member 2, dojo) round-trips through the store with member 2 stored
// in the DisplayName column.
func TestEngi_PairParticipantRoundTrip(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	compID := "engi-pair"
	createEngiCompetition(t, store, compID, state.CompFormatLeague, 4)

	pair := domain.Player{Name: "Yamada Taro", DisplayName: "Suzuki Hanako", Dojo: "Tokyo Dojo"}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{pair}))

	// Load with the caller passing false; the engi flag must still force the
	// 4-column zekken layout so member 2 reads back from DisplayName.
	loaded, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Yamada Taro", loaded[0].Name, "member 1 → Name")
	assert.Equal(t, "Suzuki Hanako", loaded[0].DisplayName, "member 2 → DisplayName")
	assert.Equal(t, "Tokyo Dojo", loaded[0].Dojo, "shared dojo")
}

// --- Regression: Finding 2 - LoadCompetition errors must propagate ----------

// corruptCompetitionConfig writes invalid YAML front-matter into the
// competition's config.md, forcing LoadCompetition to return a parse error.
// The competition directory must already exist (e.g. via SaveCompetition).
// The mtime change from WriteFile ensures the store's mtime-based cache
// bypasses any in-memory hit and re-parses from disk on the next call.
func corruptCompetitionConfig(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	path := filepath.Join(store.GetFolder(), "competitions", compID, "config.md")
	require.NoError(t, os.WriteFile(path, []byte("---\n: : :\n---\n"), 0o600))
}

// TestRecordMatchResultWithIneligibility_LoadCompetitionErrorPropagates pins
// Finding 2: when LoadCompetition returns a parse error, the non-tx path must
// propagate the error rather than silently falling through to kendo scoring.
// Before the fix, the single-if guard swallowed the error (loadErr == nil check
// was the gate, so a non-nil error caused it to skip the engi path and invoke
// the kendo path instead of returning an error).
func TestRecordMatchResultWithIneligibility_LoadCompetitionErrorPropagates(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-load-err"

	// Seed a minimal competition so the directory exists, then corrupt the config.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Engi Load Error",
		Kind:   "individual",
		Format: state.CompFormatPlayoffs,
		Status: "setup",
	}))
	corruptCompetitionConfig(t, store, compID)

	// Any call through the engi dispatch seam must return the load error, not
	// proceed with kendo scoring.
	result := &state.MatchResult{
		SideA:  "Alice",
		SideB:  "Bob",
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	}
	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", result)
	require.Error(t, err, "LoadCompetition error must propagate")
	assert.Contains(t, err.Error(), fmt.Sprintf("load competition %s", compID),
		"error message must identify the failing competition")
}

// TestRecordMatchResultWithIneligibilityTx_LoadCompetitionErrorPropagates is
// the tx-aware twin of the above regression test (Finding 2, tx path).
// Before the fix, the same single-if guard in RecordMatchResultWithIneligibilityTx
// swallowed LoadCompetition errors.
func TestRecordMatchResultWithIneligibilityTx_LoadCompetitionErrorPropagates(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-load-err-tx"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Engi Load Error Tx",
		Kind:   "individual",
		Format: state.CompFormatPlayoffs,
		Status: "setup",
	}))
	// SavePoolMatches so the tx has something to open.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))
	corruptCompetitionConfig(t, store, compID)

	result := &state.MatchResult{
		SideA:  "Alice",
		SideB:  "Bob",
		Winner: "Alice",
		Status: state.MatchStatusCompleted,
	}
	var txErr error
	_ = store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, txErr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", result)
		return nil
	})
	require.Error(t, txErr, "LoadCompetition error must propagate (tx path)")
	assert.Contains(t, txErr.Error(), fmt.Sprintf("load competition %s", compID),
		"tx error message must identify the failing competition")
}

// --- Regression: Findings 4/5/6 - Bronze match reachable via engi dispatch ---

// TestRecordEngiMatchResult_BronzeMatchReachable pins Finding 4/5/6 (engi
// variant): "m-bronze" must be reachable through RecordEngiMatchResult after
// both semifinals are scored. Before the ThirdPlaceMatch fix, the bracket-stage
// iteration in recordEngiMatch only scanned Rounds and never found "m-bronze",
// returning notFoundError.
func TestRecordEngiMatchResult_BronzeMatchReachable(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-bronze"

	// Engi + Naginata so the bronze match is generated.
	comp := &state.Competition{
		ID:       compID,
		Name:     "Engi Bronze",
		Kind:     "individual",
		Format:   state.CompFormatPlayoffs,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2,
		Courts:    []string{"A"},
		StartTime: "09:00",
		Status:    "setup",
		Engi:      true,
		Naginata:  true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch, "engi+naginata must generate a bronze match")

	// Score both semifinals via engi (flag counts) so losers populate bronze.
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)

	_, err = eng.recordEngiMatchResult(compID, sf[0].ID, 3, 2, "")
	require.NoError(t, err, "SF0 engi score must succeed")
	_, err = eng.recordEngiMatchResult(compID, sf[1].ID, 3, 2, "")
	require.NoError(t, err, "SF1 engi score must succeed")

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, bracketMatchPlayable(bracket.ThirdPlaceMatch),
		"bronze must be playable after both SFs decided")

	// Score the bronze match; this exercises the ThirdPlaceMatch branch that was
	// missing before the fix.
	bronzeRes, err := eng.recordEngiMatchResult(compID, "m-bronze", 5, 0, "")
	require.NoError(t, err, "m-bronze must be scoreable via RecordEngiMatchResult")
	require.NotNil(t, bronzeRes)
	assert.Equal(t, state.MatchStatusCompleted, bronzeRes.Status, "bronze status must be completed")
	assert.NotEmpty(t, bronzeRes.Winner, "bronze result must have a winner")
	assert.Equal(t, 5, bronzeRes.FlagsA, "FlagsA must be stored")
	assert.Equal(t, 0, bronzeRes.FlagsB, "FlagsB must be stored")

	// Verify persistence in bracket.json.
	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status,
		"bronze ThirdPlaceMatch.Status must persist as completed")
	assert.NotEmpty(t, bracket.ThirdPlaceMatch.Winner, "bronze winner must persist")
	assert.Equal(t, 5, bracket.ThirdPlaceMatch.FlagsA, "bronze FlagsA must persist")
	assert.Equal(t, 0, bracket.ThirdPlaceMatch.FlagsB, "bronze FlagsB must persist")
}

// TestRecordMatchResultWithIneligibility_EngiBronzeDispatch verifies that the
// engi dispatch seam in RecordMatchResultWithIneligibility correctly routes
// "m-bronze" scoring to the engi path (not kendo scoring). This exercises the
// ThirdPlaceMatch fix in both recordEngiMatch AND the scoring.go dispatch
// (Finding 2 error path is not triggered here; comp.Engi == true so engi wins).
func TestRecordMatchResultWithIneligibility_EngiBronzeDispatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-bronze-dispatch"

	comp := &state.Competition{
		ID:       compID,
		Name:     "Engi Bronze Dispatch",
		Kind:     "individual",
		Format:   state.CompFormatPlayoffs,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2,
		Courts:    []string{"A"},
		StartTime: "09:00",
		Status:    "setup",
		Engi:      true,
		Naginata:  true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]

	// Score both SFs to populate bronze sides.
	_, err = eng.RecordMatchResultWithIneligibility(compID, sf[0].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err, "SF0 via RecordMatchResultWithIneligibility must succeed")
	_, err = eng.RecordMatchResultWithIneligibility(compID, sf[1].ID, &state.MatchResult{
		FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err, "SF1 via RecordMatchResultWithIneligibility must succeed")

	// Score bronze via the engi dispatch seam.
	status, err := eng.RecordMatchResultWithIneligibility(compID, "m-bronze", &state.MatchResult{
		FlagsA: 1, FlagsB: 0, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err, "m-bronze via RecordMatchResultWithIneligibility (engi) must succeed")
	assert.Nil(t, status, "engi has no eligibility concept, status must be nil")

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status)
	assert.NotEmpty(t, bracket.ThirdPlaceMatch.Winner)
}

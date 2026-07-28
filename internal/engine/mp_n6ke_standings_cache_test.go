package engine

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mp-n6ke: the standings cache used to be keyed solely on the FILE MTIME of
// pool-matches.csv and overrides.json. Filesystem mtimes come from the kernel
// coarse clock (measured at 1ms granularity on this tree), so two writes inside
// one tick left the key unchanged, the validity check passed, and pre-write
// standings were served afterwards.
//
// These tests write twice back to back with NO sleep between the saves, which is
// what makes them a real regression guard: they only pass because the store's
// monotonic FileVersion counter moves on every write, independently of timestamp
// granularity. A sleep would restore the mtime difference and make them vacuous
// (the mp-p7n fix removed exactly that crutch, see
// internal/state/mp_p7n_repro_test.go).

// TestMpN6keStandingsCacheInvalidatedOnSameTickPoolMatchSave is the direct unit
// guard: consecutive saves with no delay must be reflected in standings.
//
// The mtime collision is FORCED with os.Chtimes rather than raced for. Simply
// saving twice back to back reproduces the bug only when the two writes happen
// to land in one tick (~4% of runs on this tree), which would make this test as
// unreliable as the flake it guards. Pinning both writes to one timestamp makes
// the coarse clock the test's premise instead of its luck: pre-fix this fails
// every run, post-fix it passes every run.
func TestMpN6keStandingsCacheInvalidatedOnSameTickPoolMatchSave(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "n6ke"

	// A fixed instant standing in for "both writes shared one clock tick".
	frozen := time.Unix(1700000000, 0)
	poolMatchesPath := filepath.Join(dir, "competitions", compID, "pool-matches.csv")
	freezeMtime := func() {
		require.NoError(t, os.Chtimes(poolMatchesPath, frozen, frozen))
	}

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"},
	}))

	// First save: the bout is still scheduled, so both players sit at 0 wins.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
	}))
	freezeMtime()
	first, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, first["Pool A"], 2)
	for _, ps := range first["Pool A"] {
		require.Zero(t, ps.Wins, "no bout scored yet, so nobody may have a win")
	}

	// Second save in the SAME millisecond: A1 now beat A2. Pre-fix this write
	// was invisible because pool-matches.csv kept its mtime.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))
	freezeMtime()

	// Sanity-check the premise: if the two writes were distinguishable by mtime
	// the assertions below would prove nothing about the cache key.
	require.Equal(t, frozen.UnixNano(), store.FileMtime(compID, "pool-matches.csv"),
		"both writes must present one identical mtime for this test to exercise the bug")

	second, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, second["Pool A"], 2)

	wins := map[string]int{}
	for _, ps := range second["Pool A"] {
		wins[ps.Player.Name] = ps.Wins
	}
	assert.Equal(t, 1, wins["A1"],
		"A1's win must be visible immediately, a same-millisecond save must not be masked by an mtime-keyed cache")
	assert.Equal(t, 0, wins["A2"])
}

// TestMpN6keLoserOfFlightRaceRejectsStaleSnapshot covers the second way the
// cache could hand back pre-write standings: the single-flight loser path.
//
// A caller that arrives while another goroutine is mid-compute does not run the
// compute itself, it reads whatever the winner stored. The winner stamps its
// entry with tokens it sampled BEFORE its compute, so that entry can predate a
// write which had already landed when the loser sampled. Returning it hands the
// loser (typically advanceMixedPools, right after saving a pool score) standings
// that predate its own write, which is the same fresh-matches-vs-stale-standings
// split mp-n6ke fixed on the fast path.
//
// The race is forced, not waited for: the flight entry is pre-loaded with an
// already-consumed sync.Once so this goroutine's once.Do is a no-op and it lands
// on the loser branch every run. Real winners delete their flight entry before
// releasing losers, so a consumed Once never lingers in production, but leaving
// it here is what makes the branch reachable deterministically instead of a few
// percent of the time.
func TestMpN6keLoserOfFlightRaceRejectsStaleSnapshot(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "n6ke-flight"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual",
		Format: state.CompFormatMixed, Status: state.CompStatusPools,
		Courts: []string{"A"}, StartTime: "09:00", PoolWinners: 2,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"},
	}))

	// Warm the cache while the bout is unscored. This entry stands in for the
	// snapshot a flight winner would have stamped.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
	}))
	warm, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, warm["Pool A"], 2)

	// Our write: A1 won. The cache now holds a snapshot older than our tokens.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Winner: "A1",
			IpponsA: []string{"M"}, Status: state.MatchStatusCompleted},
	}))

	consumed := &sync.Once{}
	consumed.Do(func() {})
	eng.standingsFlight.Store(compID, consumed)

	got, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	require.Len(t, got["Pool A"], 2)

	wins := map[string]int{}
	for _, ps := range got["Pool A"] {
		wins[ps.Player.Name] = ps.Wins
	}
	assert.Equal(t, 1, wins["A1"],
		"losing the single-flight race must not return a snapshot that predates our own write")
	assert.Equal(t, 0, wins["A2"])
}

// TestMpN6keFileVersionAdvancesOnEveryWrite pins the store-level invariant the
// fix rests on: the version counter moves on EVERY write, even when a burst of
// writes shares one filesystem timestamp. Asserting a strictly increasing
// counter is what keeps this independent of clock granularity.
func TestMpN6keFileVersionAdvancesOnEveryWrite(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	compID := "n6ke-ver"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID}))

	const writes = 25
	sameMtime := 0
	prevMtime := store.FileMtime(compID, "pool-matches.csv")
	prevVersion := store.FileVersion(compID, "pool-matches.csv")

	for i := 0; i < writes; i++ {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		}))

		version := store.FileVersion(compID, "pool-matches.csv")
		assert.Greater(t, version, prevVersion,
			"FileVersion must strictly increase on every write (write %d)", i)

		if mtime := store.FileMtime(compID, "pool-matches.csv"); mtime == prevMtime {
			sameMtime++
			prevMtime = mtime
		} else {
			prevMtime = mtime
		}
		prevVersion = version
	}

	// Not asserted as a hard requirement: whether a burst actually collides
	// depends on the host clock, and the counter has to hold regardless. Logged
	// so a future reader can see the granularity this guards against.
	t.Logf("writes sharing an identical mtime: %d/%d", sameMtime, writes)
}

// TestMpN6keOverridesSaveInvalidatesStandings covers the second cache input.
// overrides.json has no fileCache of its own, so before the fix its mtime was
// the only staleness signal and a same-tick override save was equally invisible.
func TestMpN6keOverridesSaveInvalidatesStandings(t *testing.T) {
	_, store, _ := setupTestEngine(t)
	compID := "n6ke-ov"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: compID}))

	before := store.FileVersion(compID, "overrides.json")
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "A1", 1))
	mid := store.FileVersion(compID, "overrides.json")
	assert.Greater(t, mid, before, "an overrides write must advance the version")

	// Immediately again, no sleep: still must be distinguishable.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "A2", 2))
	assert.Greater(t, store.FileVersion(compID, "overrides.json"), mid,
		"a same-millisecond second overrides write must advance the version again")
}

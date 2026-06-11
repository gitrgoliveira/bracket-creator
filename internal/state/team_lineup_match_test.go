// internal/state/team_lineup_match_test.go covers mp-825: match-scoped
// team lineups. A team may field a different order/roster for each
// encounter (e.g. successive pool matches), so a lineup keyed by MatchID
// must lock independently of other matches and of the legacy
// round-scoped sweep. The headline regression test is
// TestMatchLineup_LockMatch1LeavesMatch2Editable — the exact behavior
// the bug report asked for.
package state

import (
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fiveStarterForMatch is the match-scoped twin of fiveStarter: a fully
// populated 5-person lineup pinned to a specific matchID.
func fiveStarterForMatch(teamID, matchID string) domain.TeamLineup {
	l := fiveStarter(teamID, 0)
	l.MatchID = matchID
	return l
}

// TestMatchLineup_RoundTrip: a match-scoped Set round-trips through Load
// keyed independently of any round-scoped entry for the same team.
func TestMatchLineup_RoundTrip(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-rt"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)

	persisted, ok := got[teamLineupMatchKey("team-alpha", "P1")]
	require.True(t, ok, "lineup must be keyed by the match-scoped key")
	assert.Equal(t, "P1", persisted.MatchID)
	assert.Equal(t, compID, persisted.CompetitionID, "CompetitionID auto-stamped")
	assert.Nil(t, persisted.LockedAt)
}

// TestMatchLineup_CoexistsWithRoundLineup: a match-scoped and a
// round-scoped lineup for the same team live in disjoint namespaces and
// neither clobbers the other.
func TestMatchLineup_CoexistsWithRoundLineup(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-coexist"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 2, "round-scoped and match-scoped entries coexist")
	assert.Contains(t, got, teamLineupKey("team-alpha", 0))
	assert.Contains(t, got, teamLineupMatchKey("team-alpha", "P1"))
}

// TestMatchLineup_LockMatch1LeavesMatch2Editable is the headline mp-825
// regression: in a pool a team plays several encounters. Starting (and
// thereby locking) match 1 must NOT freeze the still-unstarted match-2
// lineup. Under the old round-0 collapse this test would fail because
// both lineups shared one round-0 key.
func TestMatchLineup_LockMatch1LeavesMatch2Editable(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-isolation"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, TeamSize: 5}))

	// Two pool encounters for the same team, each with its own lineup.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P2"), 5))

	// Match 1 starts and is frozen.
	require.NoError(t, store.LockTeamLineupForMatch(compID, "P1", time.Now().UTC()))

	// Match 1 is now locked: editing it must be refused.
	swapped1 := fiveStarterForMatch("team-alpha", "P1")
	swapped1.Positions[domain.PosJiho] = "p2-sub"
	require.ErrorIs(t, store.SetTeamLineup(compID, swapped1, 5), ErrLineupLocked,
		"match 1 is locked, its lineup must be frozen")

	// Match 2 is STILL editable — the whole point of the change.
	swapped2 := fiveStarterForMatch("team-alpha", "P2")
	swapped2.Positions[domain.PosJiho] = "p2-sub"
	require.NoError(t, store.SetTeamLineup(compID, swapped2, 5),
		"match 2 has not started — its lineup must remain editable after match 1 locks")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotNil(t, got[teamLineupMatchKey("team-alpha", "P1")].LockedAt, "match 1 locked")
	assert.Nil(t, got[teamLineupMatchKey("team-alpha", "P2")].LockedAt, "match 2 still open")
	assert.Equal(t, "p2-sub", got[teamLineupMatchKey("team-alpha", "P2")].Positions[domain.PosJiho],
		"match 2 edit must have persisted")
}

// TestMatchLineup_KeyNoHyphenCollision guards the Copilot-found bug
// (PR #197): joining teamID and matchID with "-" was ambiguous because
// both routinely contain hyphens (pool match IDs are "PoolA-0"). The
// pairs ("a-b","c") and ("a","b-c") both produced "m:a-b-c" and one
// lineup silently overwrote the other. With the NUL-byte delimiter they
// must remain distinct entries.
func TestMatchLineup_KeyNoHyphenCollision(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-collision"
	l1 := fiveStarterForMatch("a-b", "c")
	l1.Positions[domain.PosSenpo] = "team-ab-senpo"
	l2 := fiveStarterForMatch("a", "b-c")
	l2.Positions[domain.PosSenpo] = "team-a-senpo"

	require.NoError(t, store.SetTeamLineup(compID, l1, 5))
	require.NoError(t, store.SetTeamLineup(compID, l2, 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 2, "(a-b,c) and (a,b-c) must be distinct keys, not a collision")
	assert.Equal(t, "team-ab-senpo",
		got[teamLineupMatchKey("a-b", "c")].Positions[domain.PosSenpo])
	assert.Equal(t, "team-a-senpo",
		got[teamLineupMatchKey("a", "b-c")].Positions[domain.PosSenpo])
}

// TestMatchLineup_RoundLockSkipsMatchScoped: the legacy round-0 sweep
// must NOT freeze match-scoped lineups (their Round defaults to 0).
// Otherwise a live round-0 pool match would re-introduce the bug.
func TestMatchLineup_RoundLockSkipsMatchScoped(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-roundsweep"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P2"), 5))

	// Engine fires the legacy round-0 lock when some match starts.
	require.NoError(t, store.LockTeamLineupsForRound(compID, 0, time.Now().UTC()))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Nil(t, got[teamLineupMatchKey("team-alpha", "P2")].LockedAt,
		"round-0 sweep must NOT freeze a match-scoped lineup")
}

// TestMatchLineup_SetGuardedByOwnMatchStatus: SetTeamLineup for a
// match-scoped lineup is refused only when THAT match is running, and is
// allowed when a DIFFERENT match is running.
func TestMatchLineup_SetGuardedByOwnMatchStatus(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-toctou"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, TeamSize: 5}))
	// Pool match IDs are persisted as "PoolName-Idx" (pools.go), so the
	// test must use that form for the ID to survive the CSV round-trip
	// and be found by matchIsRunningOrCompletedLocked.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "PoolA-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
		{ID: "PoolA-1", SideA: "TeamA", SideB: "TeamC", Status: MatchStatusScheduled},
	}))

	// PoolA-0 is running → its match-scoped lineup is blocked even with
	// no LockedAt stamp yet (TOCTOU guard).
	require.ErrorIs(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "PoolA-0"), 5),
		ErrLineupLocked, "running match PoolA-0 must block its own lineup")

	// PoolA-1 is only scheduled → its lineup is freely settable.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "PoolA-1"), 5),
		"scheduled match PoolA-1 must allow its lineup")
}

// TestLockTeamLineupForMatch_OnlyTargetMatch: locking match P1 stamps
// every team's P1 lineup but leaves other matches untouched.
func TestLockTeamLineupForMatch_OnlyTargetMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-locktarget"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-beta", "P1"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P3"), 5))

	require.NoError(t, store.LockTeamLineupForMatch(compID, "P1", time.Now().UTC()))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotNil(t, got[teamLineupMatchKey("team-alpha", "P1")].LockedAt, "both P1 teams locked")
	assert.NotNil(t, got[teamLineupMatchKey("team-beta", "P1")].LockedAt, "both P1 teams locked")
	assert.Nil(t, got[teamLineupMatchKey("team-alpha", "P3")].LockedAt, "P3 untouched")
}

// TestDeleteTeamLineupForMatch: idempotent delete, and frozen entries
// refuse deletion.
func TestDeleteTeamLineupForMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-delete"

	// Idempotent on a missing entry.
	require.NoError(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "P9"))

	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))
	require.NoError(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "P1"))
	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Empty(t, got, "match lineup gone after delete")

	// A locked entry refuses deletion.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P2"), 5))
	require.NoError(t, store.LockTeamLineupForMatch(compID, "P2", time.Now().UTC()))
	require.ErrorIs(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "P2"), ErrLineupLocked,
		"locked match lineup must refuse delete")
}

// TestDeleteTeamLineupForMatch_RefusesWhenMatchLive guards the
// delete-side TOCTOU window (PR #197 review): even with no LockedAt
// stamp yet, a running/completed match must block DELETE so it cannot
// reopen a live encounter's lineup — mirroring SetTeamLineup's guard.
func TestDeleteTeamLineupForMatch_RefusesWhenMatchLive(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-delete-live"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, TeamSize: 5}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "PoolA-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusScheduled},
	}))
	// Set the lineup while the match is still scheduled.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "PoolA-0"), 5))

	// The match starts WITHOUT the lock stamp landing (the TOCTOU
	// window). DELETE must still be refused.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "PoolA-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))
	require.ErrorIs(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "PoolA-0"), ErrLineupLocked,
		"DELETE must be refused once the match is running, even before the lock stamp lands")
}

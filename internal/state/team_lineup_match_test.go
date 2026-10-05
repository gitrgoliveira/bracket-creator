// internal/state/team_lineup_match_test.go covers mp-825: match-scoped
// team lineups. A team may field a different order/roster for each
// encounter (e.g. successive pool matches), so a lineup keyed by MatchID
// must be stored and loaded independently of other matches and of the
// legacy round-scoped entries.
package state

import (
	"os"
	"path/filepath"
	"testing"

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
	// The competition must exist: saving lineups no longer creates the
	// competition directory (it let a write after DeleteCompetition resurrect it).
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 1)

	persisted, ok := got[teamLineupMatchKey("team-alpha", "P1")]
	require.True(t, ok, "lineup must be keyed by the match-scoped key")
	assert.Equal(t, "P1", persisted.MatchID)
	assert.Equal(t, compID, persisted.CompetitionID, "CompetitionID auto-stamped")
}

// TestMatchLineup_CoexistsWithRoundLineup: a match-scoped and a
// round-scoped lineup for the same team live in disjoint namespaces and
// neither clobbers the other.
func TestMatchLineup_CoexistsWithRoundLineup(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-coexist"
	// The competition must exist: saving lineups no longer creates the
	// competition directory (it let a write after DeleteCompetition resurrect it).
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 2, "round-scoped and match-scoped entries coexist")
	assert.Contains(t, got, teamLineupKey("team-alpha", 0))
	assert.Contains(t, got, teamLineupMatchKey("team-alpha", "P1"))
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
	// The competition must exist: saving lineups no longer creates the
	// competition directory (it let a write after DeleteCompetition resurrect it).
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
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

// TestDeleteTeamLineupForMatch: idempotent delete and happy-path delete.
func TestDeleteTeamLineupForMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-delete"
	// The competition must exist: saving lineups no longer creates the
	// competition directory (it let a write after DeleteCompetition resurrect it).
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Idempotent on a missing entry.
	require.NoError(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "P9"))

	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))
	require.NoError(t, store.DeleteTeamLineupForMatch(compID, "team-alpha", "P1"))
	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Empty(t, got, "match lineup gone after delete")
}

// TestDeleteMatchScopedTeamLineups: every match-scoped lineup goes, whichever
// team it is for, and the round-scoped ones stay (a team's starting lineup is
// its round-0 entry). A discarded draw relies on this: the regenerated draw
// reuses the match ids, so a lineup left behind for one would become a team's
// own at the reused id.
func TestDeleteMatchScopedTeamLineups(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-match-delete-all"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	t.Run("nothing saved is not an error and writes no file", func(t *testing.T) {
		require.NoError(t, store.DeleteMatchScopedTeamLineups(compID))

		_, err := os.Stat(filepath.Join(store.GetFolder(), "competitions", compID, teamLineupFilename))
		assert.True(t, os.IsNotExist(err), "no lineups.yaml was there to rewrite")
	})

	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 1), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P1"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-alpha", "P2"), 5))
	require.NoError(t, store.SetTeamLineup(compID, fiveStarterForMatch("team-beta", "P1"), 5))

	t.Run("drops every match-scoped lineup and keeps the round-scoped ones", func(t *testing.T) {
		require.NoError(t, store.DeleteMatchScopedTeamLineups(compID))

		got, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Contains(t, got, teamLineupKey("team-alpha", 0), "the starting lineup stays")
		assert.Contains(t, got, teamLineupKey("team-alpha", 1), "a later round's lineup stays")
	})

	t.Run("is idempotent", func(t *testing.T) {
		require.NoError(t, store.DeleteMatchScopedTeamLineups(compID))

		got, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("never creates the competition directory", func(t *testing.T) {
		require.NoError(t, store.DeleteMatchScopedTeamLineups("never-created"))

		_, err := os.Stat(filepath.Join(store.GetFolder(), "competitions", "never-created"))
		assert.True(t, os.IsNotExist(err), "only competition creation makes the directory")
	})

	t.Run("an invalid competition id is an error", func(t *testing.T) {
		assert.Error(t, store.DeleteMatchScopedTeamLineups("../traversal"))
	})
}

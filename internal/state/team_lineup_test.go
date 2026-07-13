// internal/state/team_lineup_test.go covers Slice 7.B / T122: sanity
// round-trip tests confirming that LoadTeamLineups returns what
// SetTeamLineup wrote, and that validation rules are enforced.
package state

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestStore returns a Store rooted at a temp dir and a teardown
// callback for the caller's defer.
func newTestStore(t *testing.T) (*Store, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("", "state-lineup-test-*")
	require.NoError(t, err)
	store, err := NewStore(dir)
	require.NoError(t, err)
	return store, func() { _ = os.RemoveAll(dir) }
}

// fiveStarter is a fully-populated 5-person lineup, the canonical
// "no vacancies" case used as the baseline in most tests.
func fiveStarter(teamID string, round int) domain.TeamLineup {
	return domain.TeamLineup{
		TeamID: teamID,
		Round:  round,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "p1",
			domain.PosJiho:    "p2",
			domain.PosChuken:  "p3",
			domain.PosFukusho: "p4",
			domain.PosTaisho:  "p5",
		},
	}
}

// TestLineupRoundTrip is the sanity check: a fresh Set should round-trip
// through Load with all fields intact, including the auto-stamped
// CompetitionID.
func TestLineupRoundTrip(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-1"
	lineup := fiveStarter("team-alpha", 0)

	require.NoError(t, store.SetTeamLineup(compID, lineup, 5))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got, 1, "exactly one persisted lineup")

	key := teamLineupKey("team-alpha", 0)
	persisted, ok := got[key]
	require.True(t, ok, "lineup must be keyed by teamID-round")
	assert.Equal(t, "team-alpha", persisted.TeamID)
	assert.Equal(t, compID, persisted.CompetitionID,
		"CompetitionID is auto-stamped by Set so the file is self-describing")
	assert.Equal(t, 0, persisted.Round)
	assert.Equal(t, "p1", persisted.Positions[domain.PosSenpo])
	assert.Equal(t, "p5", persisted.Positions[domain.PosTaisho])
}

// TestLineupSetValidatesShape proves that SetTeamLineup rejects INVALID
// position keys, a lineup with an unrecognised position must never reach
// disk. Under the new partial-lineup contract, a lineup that has only valid
// keys but is incomplete (e.g. only Senpo + Taisho, missing the three middle
// positions) MUST be accepted, completeness is a non-blocking UI warning,
// not a write-time gate.
func TestLineupSetValidatesShape(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-4"

	t.Run("invalid position key is rejected", func(t *testing.T) {
		bad := domain.TeamLineup{
			TeamID: "team-alpha",
			Round:  0,
			Positions: map[domain.Position]string{
				"chudan": "p1", // not a valid FIK position name
			},
		}
		err := store.SetTeamLineup(compID, bad, 5)
		require.Error(t, err, "an invalid position key must be rejected")
		assert.NotErrorIs(t, err, domain.ErrLineupTooManyMissing,
			"key validation must fire before the completeness check")

		got, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		assert.Empty(t, got, "rejected Set must not leave a partial file")
	})

	t.Run("partial lineup with valid keys is accepted", func(t *testing.T) {
		partial := domain.TeamLineup{
			TeamID: "team-alpha",
			Round:  0,
			Positions: map[domain.Position]string{
				domain.PosSenpo:  "p1",
				domain.PosTaisho: "p5",
				// Jiho, Chuken, Fukusho missing, partial, but keys are valid.
			},
		}
		require.NoError(t, store.SetTeamLineup(compID, partial, 5),
			"a partial lineup with valid position keys must persist, completeness is not enforced at write time")

		got, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		require.Len(t, got, 1, "partial lineup must be on disk")
		key := teamLineupKey("team-alpha", 0)
		assert.Equal(t, "p1", got[key].Positions[domain.PosSenpo])
		assert.Equal(t, "p5", got[key].Positions[domain.PosTaisho])
	})
}

// TestLoadTeamLineupsMissingFile confirms the missing-file case maps
// to an empty map, matching the LoadCompetitorStatus contract, the
// handler returns "no lineups submitted" rather than 500.
func TestLoadTeamLineupsMissingFile(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	got, err := store.LoadTeamLineups("comp-never-touched")
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestParseTeamLineupsBytes_MalformedYAML covers the error path in
// parseTeamLineupsBytes for invalid YAML input.
func TestParseTeamLineupsBytes_MalformedYAML(t *testing.T) {
	_, err := parseTeamLineupsBytes([]byte(":\t:bad yaml:"))
	assert.Error(t, err)
}

// TestParseTeamLineupsBytes_Empty confirms empty bytes return an empty map.
func TestParseTeamLineupsBytes_Empty(t *testing.T) {
	m, err := parseTeamLineupsBytes(nil)
	require.NoError(t, err)
	assert.Empty(t, m)
}

// TestDeleteTeamLineup_NotFound verifies that deleting a lineup that does not
// exist returns nil without error (idempotent).
func TestDeleteTeamLineup_NotFound(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-delete-notfound"
	// No lineup ever written; delete must succeed silently.
	err := store.DeleteTeamLineup(compID, "team-ghost", 0)
	require.NoError(t, err, "deleting a non-existent lineup must not error")
}

// TestDeleteTeamLineup_Success verifies the happy path: set a lineup, then
// delete it, and confirm it is gone from the persisted map.
func TestDeleteTeamLineup_Success(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-delete-ok"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))

	// Confirm it exists.
	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Len(t, got, 1)

	// Delete it.
	require.NoError(t, store.DeleteTeamLineup(compID, "team-alpha", 0))

	// Must be gone.
	got, err = store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Empty(t, got, "lineup must be absent after successful delete")
}

// TestSetTeamLineup_WhileMatchRunning verifies that lineups are always
// editable, including while a pool match is running or completed.
func TestSetTeamLineup_WhileMatchRunning(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-editable-while-live"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	// New lineup while match is running must succeed.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5),
		"new lineup while match is running must succeed")

	// Changing an already-recorded position while match is running must also succeed.
	changed := fiveStarter("team-alpha", 0)
	changed.Positions[domain.PosJiho] = "p2-substitute"
	require.NoError(t, store.SetTeamLineup(compID, changed, 5),
		"changing a recorded position while live must succeed (no freeze)")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	key := teamLineupKey("team-alpha", 0)
	require.Contains(t, got, key)
	assert.Equal(t, "p2-substitute", got[key].Positions[domain.PosJiho],
		"substitution must have persisted")
}

// TestSetTeamLineup_WhileBracketMatchRunning verifies that lineups are
// always editable, including while a bracket match is running.
func TestSetTeamLineup_WhileBracketMatchRunning(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-editable-bracket-live"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SaveBracket(compID, &Bracket{
		Rounds: [][]BracketMatch{{
			{ID: "M1", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
		}},
	}))

	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5),
		"saving a lineup while the round is live must succeed")

	// Changing must also succeed.
	changed := fiveStarter("team-alpha", 0)
	changed.Positions[domain.PosJiho] = "p2-sub"
	require.NoError(t, store.SetTeamLineup(compID, changed, 5),
		"changing a recorded position while round is live must succeed (no freeze)")
}

// TestDeleteTeamLineup_WhileLive verifies that lineups can be deleted
// even while a match is running or completed.
func TestDeleteTeamLineup_WhileLive(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-delete-while-live"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))
	require.NoError(t, store.DeleteTeamLineup(compID, "team-alpha", 0),
		"deleting a lineup while the match is live must succeed (no freeze)")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Empty(t, got, "lineup must be gone after delete")
}

// TestLoadTeamLineups_ReturnsDeepCopy confirms that the map returned by
// LoadTeamLineups is a distinct copy: mutating the returned map or the
// Positions inside it must not affect a subsequent load, guarding against
// cache aliasing (the cache stores its own copy after B3/B4).
func TestLoadTeamLineups_ReturnsDeepCopy(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-deepcopy"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))

	// First load: mutate both the returned map and a Positions entry inside it.
	got1, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	key := teamLineupKey("team-alpha", 0)
	got1[key].Positions[domain.PosSenpo] = "mutated"
	got1["injected"] = domain.TeamLineup{TeamID: "ghost"}

	// Second load must not reflect either mutation.
	got2, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotContains(t, got2, "injected", "injected key must not appear in second load")
	persisted, ok := got2[key]
	require.True(t, ok)
	assert.Equal(t, "p1", persisted.Positions[domain.PosSenpo],
		"Positions mutation in first load must not affect second load")
}

// TestLoadTeamLineups_CacheRefreshedOnSave confirms that a SetTeamLineup call
// that follows a LoadTeamLineups (which warms the cache) is visible on the
// next LoadTeamLineups, guarding against a stale-cache regression.
func TestLoadTeamLineups_CacheRefreshedOnSave(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-cache-refresh"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))

	// Warm the cache.
	got1, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	require.Len(t, got1, 1, "one lineup after initial save")

	// Add a second lineup; saveTeamLineupsLocked must refresh the cache.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-beta", 0), 5))

	// The new entry must be visible without any file-mtime tricks.
	got2, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Len(t, got2, 2, "both lineups must be visible after cache refresh")
	assert.Contains(t, got2, teamLineupKey("team-beta", 0),
		"newly added lineup must appear in the second load")
}

// TestFindBestLineupAny covers the multi-key lookup used when a match
// side name must also be tried as the team's participant ID ("match on
// id OR name"): the priority tiers apply across the whole key set, and
// within a tier the first key in the slice wins.
func TestFindBestLineupAny(t *testing.T) {
	pid := "8d5a1b1e-1111-4222-8333-444455556666"
	name := "RedTeam"

	matchScoped := domain.TeamLineup{
		TeamID: pid, MatchID: "SF-1",
		Positions: map[domain.Position]string{domain.PosSenpo: "MatchScoped"},
	}
	roundScopedName := domain.TeamLineup{
		TeamID: name, Round: 0,
		Positions: map[domain.Position]string{domain.PosSenpo: "RoundName"},
	}
	roundScopedPid := domain.TeamLineup{
		TeamID: pid, Round: 1,
		Positions: map[domain.Position]string{domain.PosSenpo: "RoundPid"},
	}
	lineups := map[string]domain.TeamLineup{
		lineupStorageKey(matchScoped):     matchScoped,
		lineupStorageKey(roundScopedName): roundScopedName,
		lineupStorageKey(roundScopedPid):  roundScopedPid,
	}

	t.Run("match-scoped under id beats round-scoped under name", func(t *testing.T) {
		got, found := FindBestLineupAny(lineups, []string{name, pid}, "SF-1", 1)
		require.True(t, found)
		assert.Equal(t, "MatchScoped", got.Positions[domain.PosSenpo])
	})

	t.Run("round tier picks highest round <= maxRound across keys", func(t *testing.T) {
		got, found := FindBestLineupAny(lineups, []string{name, pid}, "other-match", 1)
		require.True(t, found)
		assert.Equal(t, "RoundPid", got.Positions[domain.PosSenpo],
			"round 1 pid-keyed entry outranks round 0 name-keyed entry")
	})

	t.Run("single name key still resolves", func(t *testing.T) {
		got, found := FindBestLineupAny(lineups, []string{name}, "", 0)
		require.True(t, found)
		assert.Equal(t, "RoundName", got.Positions[domain.PosSenpo])
	})

	t.Run("no candidate keys match", func(t *testing.T) {
		_, found := FindBestLineupAny(lineups, []string{"UnknownTeam"}, "", 5)
		assert.False(t, found)
	})
}

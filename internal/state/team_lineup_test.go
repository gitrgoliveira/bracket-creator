// internal/state/team_lineup_test.go covers Slice 7.B / T122: a lineup
// that's been frozen for a round (LockTeamLineupsForRound stamped its
// LockedAt) must reject further SetTeamLineup calls with
// ErrLineupLocked. Plus a sanity round-trip test confirming
// LoadTeamLineups returns what SetTeamLineup wrote.
package state

import (
	"os"
	"testing"
	"time"

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
// through Load with all fields intact, including the
// auto-stamped CompetitionID and the absence of LockedAt.
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
	assert.Nil(t, persisted.LockedAt, "freshly-set lineup is not locked")
}

// TestLineupLockedAfterLive is the core Slice 7.B / T122 contract:
// once LockTeamLineupsForRound has stamped a round's lineups, further
// SetTeamLineup calls for any team in that round must return
// ErrLineupLocked, the freeze is the entire point of the round-start
// transition.
func TestLineupLockedAfterLive(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-2"

	// 1. Submit a lineup pre-live.
	original := fiveStarter("team-alpha", 0)
	require.NoError(t, store.SetTeamLineup(compID, original, 5))

	// 2. Freeze the round (engine would call this when the round's
	//    first match transitions to running).
	require.NoError(t, store.LockTeamLineupsForRound(compID, 0, time.Now().UTC()))

	// 3. Subsequent Set must refuse with ErrLineupLocked. We try a
	//    DIFFERENT players-map so a green test can't be the result of
	//    "no actual difference to persist", the refusal must come from
	//    the lock check, not from idempotent equality.
	swapped := fiveStarter("team-alpha", 0)
	swapped.Positions[domain.PosJiho] = "p2-substitute"
	err := store.SetTeamLineup(compID, swapped, 5)
	require.ErrorIs(t, err, ErrLineupLocked,
		"Set after Lock must surface ErrLineupLocked so the handler can map to 409")

	// And the persisted lineup is unchanged: the rejected Set must NOT
	// have leaked partial state.
	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	persisted, ok := got[teamLineupKey("team-alpha", 0)]
	require.True(t, ok)
	assert.Equal(t, "p2", persisted.Positions[domain.PosJiho],
		"locked lineup must retain its original Jiho")
	assert.NotNil(t, persisted.LockedAt, "lock timestamp survives reload")

	// 4. Delete is also rejected; freezing applies to every mutation,
	//    not just replacement.
	err = store.DeleteTeamLineup(compID, "team-alpha", 0)
	require.ErrorIs(t, err, ErrLineupLocked,
		"Delete after Lock must also surface ErrLineupLocked")
}

// TestLineupLockOtherRoundUnaffected exercises the multi-round isolation
// promise: locking round 0 must NOT freeze round 1 lineups, since
// each elimination round has its own first-match-going-live moment.
func TestLineupLockOtherRoundUnaffected(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-3"

	r0 := fiveStarter("team-alpha", 0)
	r1 := fiveStarter("team-alpha", 1)
	require.NoError(t, store.SetTeamLineup(compID, r0, 5))
	require.NoError(t, store.SetTeamLineup(compID, r1, 5))

	require.NoError(t, store.LockTeamLineupsForRound(compID, 0, time.Now().UTC()))

	// Round 1 must remain editable.
	r1.Positions[domain.PosJiho] = "p2-changed"
	assert.NoError(t, store.SetTeamLineup(compID, r1, 5),
		"locking round 0 must not freeze round 1 ,the next round's lineup is still mutable")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotNil(t, got[teamLineupKey("team-alpha", 0)].LockedAt, "round 0 locked")
	assert.Nil(t, got[teamLineupKey("team-alpha", 1)].LockedAt, "round 1 still open")
}

// TestLineupSetValidatesShape proves that SetTeamLineup rejects INVALID
// position keys ,a lineup with an unrecognised position must never reach
// disk. Under the new partial-lineup contract, a lineup that has only valid
// keys but is incomplete (e.g. only Senpo + Taisho, missing the three middle
// positions) MUST be accepted ,completeness is a non-blocking UI warning,
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
				// Jiho, Chuken, Fukusho missing ,partial, but keys are valid.
			},
		}
		require.NoError(t, store.SetTeamLineup(compID, partial, 5),
			"a partial lineup with valid position keys must persist ,completeness is not enforced at write time")

		got, err := store.LoadTeamLineups(compID)
		require.NoError(t, err)
		require.Len(t, got, 1, "partial lineup must be on disk")
		key := teamLineupKey("team-alpha", 0)
		assert.Equal(t, "p1", got[key].Positions[domain.PosSenpo])
		assert.Equal(t, "p5", got[key].Positions[domain.PosTaisho])
	})
}

// TestLineupLockIdempotent locks the same round twice; the second call
// must preserve the original LockedAt (first-live-match time is the
// canonical freeze moment, not the last write).
func TestLineupLockIdempotent(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-5"
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5))

	first := time.Date(2026, 5, 16, 10, 0, 0, 0, time.UTC)
	second := time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC)
	require.NoError(t, store.LockTeamLineupsForRound(compID, 0, first))
	require.NoError(t, store.LockTeamLineupsForRound(compID, 0, second))

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	persisted := got[teamLineupKey("team-alpha", 0)]
	require.NotNil(t, persisted.LockedAt)
	assert.True(t, persisted.LockedAt.Equal(first),
		"re-locking must preserve the original LockedAt; first-live wins")
}

// TestLoadTeamLineupsMissingFile confirms the missing-file case maps
// to an empty map, matching the LoadCompetitorStatus contract ,the
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

// TestSetTeamLineup_RoundHasLiveBracketMatch verifies the new partial-lineup
// contract: saving a NEW lineup (no prior entry) while the round is live
// SUCCEEDS ,the lock only applies to writes that CHANGE an already-recorded
// position. Adding the first lineup at the table while bouts run is the
// normal live-entry case.
func TestSetTeamLineup_RoundHasLiveBracketMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-live-bracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Bracket round 0 has a running match.
	require.NoError(t, store.SaveBracket(compID, &Bracket{
		Rounds: [][]BracketMatch{{
			{ID: "M1", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
		}},
	}))

	// NEW lineup (no prior entry) while the round is live → must succeed.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5),
		"saving a new lineup while the round is live must succeed ,lock only blocks changing a recorded position")
}

// TestSetTeamLineup_RoundHasLivePoolMatch verifies the new partial-lineup
// contract for pool matches: saving a NEW lineup while a pool match is running
// must SUCCEED. The lock only blocks changing an already-recorded position.
func TestSetTeamLineup_RoundHasLivePoolMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-live-pool"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Pool match running → NEW lineup (no prior) must still succeed.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5),
		"saving a new lineup while a pool match is live must succeed ,only changing a recorded position is blocked")
}

// TestSetTeamLineup_RoundHasCompletedMatch verifies the new partial-lineup
// contract: a completed bracket match does NOT block a NEW lineup submission
// (no prior entry). Only changing an already-recorded position is blocked.
func TestSetTeamLineup_RoundHasCompletedMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-completed-bracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	require.NoError(t, store.SaveBracket(compID, &Bracket{
		Rounds: [][]BracketMatch{{
			{ID: "M1", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusCompleted, Winner: "TeamA"},
		}},
	}))

	// NEW lineup (no prior) while the round is completed → must succeed.
	require.NoError(t, store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5),
		"saving a new lineup while the round is completed must succeed ,lock only blocks changing a recorded position")
}

// TestSetTeamLineup_PartialAllowedWhileLive verifies that a partial lineup
// (only senpo set, other positions empty) persists successfully while a pool
// match is running ,the normal "enter the order at the table" flow.
func TestSetTeamLineup_PartialAllowedWhileLive(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-partial-live"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	partial := domain.TeamLineup{
		TeamID: "team-alpha",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1",
		},
	}
	require.NoError(t, store.SetTeamLineup(compID, partial, 5),
		"partial {senpo:X} save while the match is running must succeed")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	key := teamLineupKey("team-alpha", 0)
	require.Contains(t, got, key)
	assert.Equal(t, "p1", got[key].Positions[domain.PosSenpo])
}

// TestSetTeamLineup_ChangeRecordedWhileLiveLocked verifies that changing an
// already-recorded position while the round is live returns ErrLineupLocked
// (non-force path). Adding to empty slots is allowed; only substitutions are
// blocked.
func TestSetTeamLineup_ChangeRecordedWhileLiveLocked(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-change-live-locked"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Pre-live: save a partial lineup while the match is scheduled.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusScheduled},
	}))
	initial := domain.TeamLineup{
		TeamID: "team-alpha",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1",
		},
	}
	require.NoError(t, store.SetTeamLineup(compID, initial, 5))

	// Match goes live.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	// Changing the already-recorded senpo must be blocked.
	change := domain.TeamLineup{
		TeamID: "team-alpha",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1-substitute", // CHANGE to a recorded position
		},
	}
	require.ErrorIs(t, store.SetTeamLineup(compID, change, 5), ErrLineupLocked,
		"changing a recorded position while live must return ErrLineupLocked")

	// Adding jiho to the empty slot must still succeed.
	add := domain.TeamLineup{
		TeamID: "team-alpha",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "p1",     // unchanged
			domain.PosJiho:  "p2-new", // ADD to empty slot
		},
	}
	require.NoError(t, store.SetTeamLineup(compID, add, 5),
		"adding a player to an empty slot while live must succeed")
}

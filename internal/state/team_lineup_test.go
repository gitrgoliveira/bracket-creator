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
// ErrLineupLocked — the freeze is the entire point of the round-start
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
	//    "no actual difference to persist" — the refusal must come from
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

	// 4. Delete is also rejected — freezing applies to every mutation,
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
		"locking round 0 must not freeze round 1 — the next round's lineup is still mutable")

	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotNil(t, got[teamLineupKey("team-alpha", 0)].LockedAt, "round 0 locked")
	assert.Nil(t, got[teamLineupKey("team-alpha", 1)].LockedAt, "round 1 still open")
}

// TestLineupSetValidatesShape proves that SetTeamLineup runs
// TeamLineup.Validate before persisting — a malformed lineup must
// never reach disk. Reuses the FIK back-fill rule the domain test
// already covers (3+ missing positions disqualifies a 5-person team).
func TestLineupSetValidatesShape(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-comp-4"
	bad := domain.TeamLineup{
		TeamID: "team-alpha",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:  "p1",
			domain.PosTaisho: "p5",
			// Jiho, Chuken, Fukusho all missing → 3+ vacancies → DQ.
		},
	}
	err := store.SetTeamLineup(compID, bad, 5)
	require.ErrorIs(t, err, domain.ErrLineupTooManyMissing,
		"shape errors must propagate from Validate so the handler returns 400")

	// Nothing on disk.
	got, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.Empty(t, got, "rejected Set must not leave a partial file")
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
// to an empty map, matching the LoadCompetitorStatus contract — the
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

// TestSetTeamLineup_RoundHasLiveBracketMatch verifies the T128a guard:
// when a bracket match for the same round is already running, SetTeamLineup
// must refuse with ErrLineupLocked even if no LockedAt stamp exists yet.
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

	err := store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5)
	require.ErrorIs(t, err, ErrLineupLocked,
		"running bracket match in round 0 must block SetTeamLineup with ErrLineupLocked")
}

// TestSetTeamLineup_RoundHasLivePoolMatch verifies the T128a guard for pool
// matches: running pool matches collapse to round 0.
func TestSetTeamLineup_RoundHasLivePoolMatch(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const compID = "team-live-pool"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Pool match running → should block round 0 lineup submissions.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusRunning},
	}))

	err := store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5)
	require.ErrorIs(t, err, ErrLineupLocked,
		"running pool match must block round 0 SetTeamLineup with ErrLineupLocked")
}

// TestSetTeamLineup_RoundHasCompletedMatch verifies that a COMPLETED bracket
// match in the round also blocks the lineup submission.
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

	err := store.SetTeamLineup(compID, fiveStarter("team-alpha", 0), 5)
	require.ErrorIs(t, err, ErrLineupLocked)
}

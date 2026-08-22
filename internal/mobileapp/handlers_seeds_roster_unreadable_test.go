package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-qual review round: rejectSeedsOffRoster's own comment said that returning
// early on a load failure "would let an unvalidated seeding through on a
// transient read error", and the next line did exactly that --
// `if err != nil || len(players) == 0 { return nil }` -- so a participants.csv
// the server could not read meant the ghost-name check simply did not run and
// PUT /seeds answered 200.
//
// The two halves of that condition are NOT the same case and must not share a
// branch:
//
//   - an EMPTY roster is a legitimate state (state.loadParticipants maps a
//     missing file to an empty slice, and a client may save seeds before the
//     roster). Nothing contradicts the seeding, so it is accepted, and the
//     draw's own validation stays the backstop.
//   - an UNREADABLE roster is the server's failure. Accepting the write there
//     persists a seeding nobody checked, into the exact state the check exists
//     to prevent: seeds.csv holding a valid 1..N that draws, while every reader
//     merging seeds onto players by name sees an unclosable gap and Generate
//     draw is disabled permanently, with no row the operator can edit to fix it.
//
// The failure is forced with a DIRECTORY where participants.csv belongs, which
// os.Open accepts and the first Read rejects. That is a genuine I/O error on
// the real load path -- not a stub -- and unlike chmod it behaves the same when
// the suite runs as root.

func seedsRosterFixture(t *testing.T) (*state.Store, string, func(string, string, []byte) *httptest.ResponseRecorder) {
	t.Helper()
	r, store, _, _, tempDir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "c1", Name: "C1", Format: state.CompFormatMixed, PoolSize: 3, PoolWinners: 2,
	}))
	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, err := http.NewRequest(method, path, bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	return store, tempDir, do
}

func completeSeeding(t *testing.T) []byte {
	t.Helper()
	body, err := json.Marshal([]domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Bob", SeedRank: 2},
	})
	require.NoError(t, err)
	return body
}

// Fault injection (verified): restoring `if err != nil || len(players) == 0 {
// return nil }` turns this red with a 200 and a persisted seeds.csv.
func TestPutSeeds_UnreadableRosterFailsClosed(t *testing.T) {
	store, tempDir, do := seedsRosterFixture(t)

	// A directory where the roster file belongs: os.Open succeeds, Read fails.
	rosterPath := filepath.Join(tempDir, "competitions", "c1", "participants.csv")
	require.NoError(t, os.MkdirAll(rosterPath, 0o750))

	w := do("PUT", "/api/competitions/c1/seeds", completeSeeding(t))
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"an unreadable roster is the server's failure, not a bad seeding: it must not be reported as a client error, and it must not be accepted")
	assert.NotEqual(t, http.StatusOK, w.Code,
		"the seeding was accepted without ever being checked against a roster")

	seeds, err := store.LoadSeeds("c1")
	require.NoError(t, err)
	assert.Empty(t, seeds, "nothing may be persisted when the check could not run")
}

// The other half of the retired condition still behaves as documented: an empty
// roster is a legitimate state, not a failure, and the seeding is accepted.
func TestPutSeeds_EmptyRosterStillAccepts(t *testing.T) {
	store, _, do := seedsRosterFixture(t)

	w := do("PUT", "/api/competitions/c1/seeds", completeSeeding(t))
	assert.Equal(t, http.StatusOK, w.Code,
		"seeds saved before the roster must still work: the draw's own validation is the backstop")

	seeds, err := store.LoadSeeds("c1")
	require.NoError(t, err)
	assert.Len(t, seeds, 2)
}

// And a readable roster still refuses a name nobody carries, so failing closed
// on a read error did not weaken the check it guards.
func TestPutSeeds_GhostNameStillRefused(t *testing.T) {
	store, _, do := seedsRosterFixture(t)
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"},
	}))

	body, err := json.Marshal([]domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Nobody", SeedRank: 2},
	})
	require.NoError(t, err)

	w := do("PUT", "/api/competitions/c1/seeds", body)
	assert.Equal(t, http.StatusBadRequest, w.Code, "a ghost name is the client's error")
	assert.Contains(t, w.Body.String(), "not on this competition's roster")
}

// TestPutSeeds_DojoMismatchRefused is the regression guard for the bc-389
// review finding: rejectSeedsOffRoster used to check Name alone, coarser than
// the (name, dojo) key the merge (state.loadParticipants / domain.RosterIndex)
// actually resolves seeds by. A row naming a roster name under the WRONG dojo
// used to pass this gate -- seeds.csv saved with 200 -- yet the merge could
// never attach it (the exact key misses, and the bare-name fallback is
// disabled whenever the row carries a non-empty dojo), so the rank rode in
// seeds.csv unattached and only surfaced, much later, as generate-draw's
// "seeded participant not found in main list". The roster carries Alice from
// "Wakaba"; the seed names Alice from "Cooper Dojo" instead.
func TestPutSeeds_DojoMismatchRefused(t *testing.T) {
	store, _, do := seedsRosterFixture(t)
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Alice", Dojo: "Wakaba"}, {Name: "Bob", Dojo: "Wakaba"},
	}))

	body, err := json.Marshal([]domain.SeedAssignment{
		{Name: "Alice", Dojo: "Cooper Dojo", SeedRank: 1},
		{Name: "Bob", Dojo: "Wakaba", SeedRank: 2},
	})
	require.NoError(t, err)

	w := do("PUT", "/api/competitions/c1/seeds", body)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"a seed row naming a roster name under the wrong dojo must be refused, not silently persisted unattached")
	assert.Contains(t, w.Body.String(), "not on this competition's roster")

	seeds, loadErr := store.LoadSeeds("c1")
	require.NoError(t, loadErr)
	assert.Empty(t, seeds, "nothing may be persisted when the dojo-mismatch row was refused")
}

// TestPutSeeds_LegacyEmptyDojoStillAccepted pins that the fallback the gate
// now shares with the merge (domain.RosterIndex's unique-bare-name match) is
// preserved: a legacy seed row with NO dojo naming a unique roster name must
// still be accepted, exactly as the merge would still attach it.
func TestPutSeeds_LegacyEmptyDojoStillAccepted(t *testing.T) {
	store, _, do := seedsRosterFixture(t)
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{
		{Name: "Alice", Dojo: "Wakaba"}, {Name: "Bob", Dojo: "Wakaba"},
	}))

	body, err := json.Marshal([]domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1}, // legacy row: no dojo, "Alice" is unique
		{Name: "Bob", SeedRank: 2},
	})
	require.NoError(t, err)

	w := do("PUT", "/api/competitions/c1/seeds", body)
	assert.Equal(t, http.StatusOK, w.Code, "a legacy no-dojo row naming a unique roster name must still be accepted")

	seeds, loadErr := store.LoadSeeds("c1")
	require.NoError(t, loadErr)
	assert.Len(t, seeds, 2)
}

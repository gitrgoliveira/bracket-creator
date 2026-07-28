package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSaveThenLoadTournamentIsNormalized pins the invariant that consumers rely
// on: anything Store.LoadTournament hands out is canonical.
//
// It was NOT true. LoadTournament serves s.cachedTourn verbatim on a cache hit,
// and both save paths seeded that cache with the caller's raw struct while also
// bumping the mtime, so the very next load returned
// ClockToElapsedMultiplier=0 / SlowestCourtBufferPct=0 / Mode="". Engine masked
// it by re-applying ApplyTournamentDefaults after every load; when those
// redundant-looking calls were removed, the hole became reachable. A zero
// multiplier collapses perMatchElapsed to 0, which the 1-minute slot floor then
// pins at 1 minute per match: silently wrong schedule estimates, no error.
//
// The whole Go and JS suites passed with the hole open, because engine tests
// build tournaments with explicit values rather than round-tripping one.
func TestSaveThenLoadTournamentIsNormalized(t *testing.T) {
	for _, tc := range []struct {
		name string
		save func(*Store, *Tournament) error
	}{
		{"SaveTournament", func(s *Store, tn *Tournament) error { return s.SaveTournament(tn) }},
		{"UpdateTournamentChanged", func(s *Store, tn *Tournament) error {
			_, err := s.UpdateTournamentChanged(tn, func(current, desired *Tournament) error { return nil })
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			store, err := NewStore(dir)
			require.NoError(t, err)

			// Schedule fields deliberately left unset, as an API caller may send.
			require.NoError(t, tc.save(store, &Tournament{
				Name: "Probe", Date: "01-08-2026", Venue: "V", Courts: []string{"A"},
			}))

			// This read is a cache hit: the save updated both the cache and the
			// mtime. It must still come back normalized.
			got, err := store.LoadTournament()
			require.NoError(t, err)
			require.NotNil(t, got)

			assert.Equal(t, 1.5, got.ClockToElapsedMultiplier,
				"a zero multiplier silently collapses every schedule estimate")
			assert.Equal(t, 10, got.SlowestCourtBufferPct)
			assert.Equal(t, TournamentModeOfficiated, got.Mode)
			assert.Equal(t, 1, got.DurationDays)
		})
	}
}

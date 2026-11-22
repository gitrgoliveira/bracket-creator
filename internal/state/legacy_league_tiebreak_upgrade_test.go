package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// releasedLeagueConfig returns the config.md the current code keeps for a
// team league, and the same file as v2.4.2 (and every release from v0.17.0)
// wrote it with the retired keys set: league_tiebreak_top_n in its place
// before league_two_third_places, and league_tiebreak_finalized after
// two_third_places. keys picks which of the two the released file carries.
func releasedLeagueConfig(t *testing.T, keys ...string) (current, released []byte) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	on := true
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "league-x", Name: "League X", Kind: "team", TeamSize: 3,
		Format: state.CompFormatLeague, Status: state.CompStatusComplete,
		Courts: []string{"A"}, LeagueTwoThirdPlaces: true, TwoThirdPlaces: &on,
	}))
	// A second start lets every other load-time step converge the file first,
	// so current is the file as a running server keeps it.
	_, err = state.NewStore(dir)
	require.NoError(t, err)
	current, err = os.ReadFile(filepath.Join(dir, "competitions", "league-x", "config.md"))
	require.NoError(t, err)

	text := string(current)
	for _, key := range keys {
		switch key {
		case "top_n":
			require.Contains(t, text, "\nleague_two_third_places:")
			text = strings.Replace(text, "\nleague_two_third_places:", "\nleague_tiebreak_top_n: 4\nleague_two_third_places:", 1)
		case "finalized":
			i := strings.Index(text, "\ntwo_third_places:")
			require.GreaterOrEqual(t, i, 0)
			end := i + 1 + strings.Index(text[i+1:], "\n")
			text = text[:end] + "\nleague_tiebreak_finalized: true" + text[end:]
		default:
			t.Fatalf("unknown key %q", key)
		}
	}
	return current, []byte(text)
}

// writeConfig puts a config.md for league-x into a fresh data folder.
func writeConfig(t *testing.T, raw []byte) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	compDir := filepath.Join(dir, "competitions", "league-x")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	path = filepath.Join(compDir, "config.md")
	require.NoError(t, os.WriteFile(path, raw, 0o600))
	return dir, path
}

// Opening a data folder a released version wrote drops the two retired league
// tie-break keys from config.md and changes nothing else: the file then reads
// byte for byte as the current code writes the same competition.
func TestLegacyUpgrade_DropsRetiredLeagueTiebreakKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		keys []string
	}{
		{"both keys", []string{"top_n", "finalized"}},
		{"Break ties for top only", []string{"top_n"}},
		{"accepted shared ranks only", []string{"finalized"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, released := releasedLeagueConfig(t, tc.keys...)
			require.NotEqual(t, string(current), string(released), "the fixture must carry the retired keys")
			dir, path := writeConfig(t, released)

			store, err := state.NewStore(dir) // the startup sweep runs inside this call
			require.NoError(t, err)

			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(after), "league_tiebreak_top_n")
			assert.NotContains(t, string(after), "league_tiebreak_finalized")
			assert.Equal(t, string(current), string(after), "nothing else in config.md changes")

			comp, err := store.LoadCompetition("league-x")
			require.NoError(t, err)
			require.NotNil(t, comp)
			assert.Equal(t, state.CompStatusComplete, comp.Status, "a finished league keeps its status at upgrade")
			assert.True(t, comp.LeagueTwoThirdPlaces)
			assert.Equal(t, 3, comp.TeamSize)
		})
	}
}

// A second start writes nothing: the converged file holds neither key.
func TestLegacyUpgrade_RetiredLeagueTiebreakKeysSecondLoadWritesNothing(t *testing.T) {
	_, released := releasedLeagueConfig(t, "top_n", "finalized")
	dir, path := writeConfig(t, released)
	_, err := state.NewStore(dir)
	require.NoError(t, err)

	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(path, old, old))
	before, err := os.ReadFile(path)
	require.NoError(t, err)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	_, err = store.LoadCompetition("league-x")
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(old), "config.md was not written again")
}

// A config.md with neither key is never written by this step.
func TestLegacyUpgrade_ConfigWithoutRetiredLeagueTiebreakKeysIsUntouched(t *testing.T) {
	current, _ := releasedLeagueConfig(t)
	dir, path := writeConfig(t, current)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(path, old, old))

	_, err := state.NewStore(dir)
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(current), string(after))
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(old), "config.md was not written")
}

// Opening a data folder a released version wrote drops its pool_format key
// from config.md and changes nothing else: the round_robin value the operator
// chose is what the pool shape reads, whatever pool_format said.
func TestLegacyUpgrade_DropsRetiredPoolFormatKey(t *testing.T) {
	for _, value := range []string{"full", "partial"} {
		t.Run(value, func(t *testing.T) {
			dir := t.TempDir()
			store, err := state.NewStore(dir)
			require.NoError(t, err)
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID: "mixed-x", Name: "Mixed X", Format: state.CompFormatMixed,
				Status: state.CompStatusSetup, Courts: []string{"A"},
				PoolSize: 4, PoolWinners: 2, RoundRobin: false,
			}))
			_, err = state.NewStore(dir)
			require.NoError(t, err)
			path := filepath.Join(dir, "competitions", "mixed-x", "config.md")
			current, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Contains(t, string(current), "\nround_robin: false\n")

			released := strings.Replace(string(current), "\nround_robin: false\n",
				"\npool_format: "+value+"\nround_robin: false\n", 1)
			require.NoError(t, os.WriteFile(path, []byte(released), 0o600))

			store, err = state.NewStore(dir) // the startup sweep runs inside this call
			require.NoError(t, err)
			after, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.NotContains(t, string(after), "pool_format")
			assert.Equal(t, string(current), string(after), "nothing else in config.md changes")

			comp, err := store.LoadCompetition("mixed-x")
			require.NoError(t, err)
			require.NotNil(t, comp)
			assert.False(t, comp.RoundRobin, "the stored ring stays the ring")
		})
	}
}

package state_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The expected entry count (Competition.ExpectedParticipants) is stored in
// config.md as expected_participants. No earlier release writes the key, so a
// config.md without it must load as "not set" (0), and a competition with no
// expected count must not start writing it.

func expectedTestCompetition(id string, expected int) *state.Competition {
	return &state.Competition{
		ID:                   id,
		Name:                 "Expected " + id,
		Format:               state.CompFormatKnockout,
		Kind:                 "individual",
		Courts:               []string{"A"},
		StartTime:            "09:00",
		Status:               state.CompStatusSetup,
		ExpectedParticipants: expected,
	}
}

func readCompetitionConfig(t *testing.T, dir, id string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", id, "config.md"))
	require.NoError(t, err)
	return string(raw)
}

func TestExpectedParticipants_RoundTripsThroughConfig(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(expectedTestCompetition("expected-40", 40)))

	assert.Contains(t, readCompetitionConfig(t, dir, "expected-40"), "expected_participants: 40",
		"the expected count is stored in config.md under its snake_case key")

	reopened, err := state.NewStore(dir)
	require.NoError(t, err)
	comp, err := reopened.LoadCompetition("expected-40")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, 40, comp.ExpectedParticipants)
}

func TestExpectedParticipants_ZeroWritesNoKey(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(expectedTestCompetition("expected-none", 0)))

	assert.NotContains(t, readCompetitionConfig(t, dir, "expected-none"), "expected_participants",
		"a competition with no expected count writes config.md exactly as before the field existed")
}

// TestExpectedParticipants_ConfigWithoutKeyLoadsAsZero takes a config.md that
// holds the key, removes the line (leaving the file an earlier release would
// have written), and reloads it in a fresh store: the field reads as 0.
func TestExpectedParticipants_ConfigWithoutKeyLoadsAsZero(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(expectedTestCompetition("older-config", 24)))

	path := filepath.Join(dir, "competitions", "older-config", "config.md")
	text := readCompetitionConfig(t, dir, "older-config")
	require.Contains(t, text, "expected_participants: 24\n")
	older := strings.Replace(text, "expected_participants: 24\n", "", 1)
	require.NotContains(t, older, "expected_participants")
	require.NoError(t, os.WriteFile(path, []byte(older), 0o600))

	reopened, err := state.NewStore(dir)
	require.NoError(t, err)
	comp, err := reopened.LoadCompetition("older-config")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, 0, comp.ExpectedParticipants)
	assert.Equal(t, "Expected older-config", comp.Name, "the rest of the file loads unchanged")
}

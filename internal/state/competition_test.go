package state

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestCompetitionLoadsPerPhaseDurations verifies FR-053 / NFR-025:
// the Competition struct round-trips per-phase match durations
// (pool_match_duration and playoff_match_duration) through YAML.
func TestCompetitionLoadsPerPhaseDurations(t *testing.T) {
	original := Competition{
		ID:                   "test-comp",
		Name:                 "Per-Phase Durations",
		PoolMatchDuration:    2,
		PlayoffMatchDuration: 3,
	}

	data, err := yaml.Marshal(&original)
	require.NoError(t, err)

	var loaded Competition
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, 2, loaded.PoolMatchDuration, "PoolMatchDuration should round-trip")
	assert.Equal(t, 3, loaded.PlayoffMatchDuration, "PlayoffMatchDuration should round-trip")
}

// TestCompetitionLegacyMatchDurationFallback verifies FR-054 / NFR-025 / R9:
// when YAML lacks the per-phase fields but carries the legacy match_duration
// field, ApplyCompetitionDefaults populates BOTH per-phase durations with the
// legacy value so older tournaments continue to schedule correctly.
func TestCompetitionLegacyMatchDurationFallback(t *testing.T) {
	legacyYAML := []byte(`id: legacy-comp
name: Legacy Comp
match_duration: 5
`)

	var c Competition
	err := yaml.Unmarshal(legacyYAML, &c)
	require.NoError(t, err)

	// Before applying defaults, per-phase fields are zero.
	require.Equal(t, 0, c.PoolMatchDuration)
	require.Equal(t, 0, c.PlayoffMatchDuration)

	ApplyCompetitionDefaults(&c)

	assert.Equal(t, 5, c.PoolMatchDuration, "legacy match_duration should fall through to PoolMatchDuration")
	assert.Equal(t, 5, c.PlayoffMatchDuration, "legacy match_duration should fall through to PlayoffMatchDuration")
}

// TestSwissRoundsFieldPersists verifies FR-050a / NFR-025:
// the Competition struct round-trips the Swiss-format fields
// (swissRounds and swissCurrentRound) through YAML so a paused
// Swiss tournament can resume with its round budget intact.
func TestSwissRoundsFieldPersists(t *testing.T) {
	original := Competition{
		ID:                "swiss-comp",
		Name:              "Swiss Persistence",
		Format:            CompFormatSwiss,
		SwissRounds:       4,
		SwissCurrentRound: 2,
	}

	data, err := yaml.Marshal(&original)
	require.NoError(t, err)

	// Sanity-check the on-disk key naming — the YAML wire format must
	// use snake_case (existing competition.go convention) so older
	// loaders that key-match by snake_case continue to work.
	yamlText := string(data)
	assert.Contains(t, yamlText, "swiss_rounds: 4")
	assert.Contains(t, yamlText, "swiss_current_round: 2")

	var loaded Competition
	err = yaml.Unmarshal(data, &loaded)
	require.NoError(t, err)

	assert.Equal(t, 4, loaded.SwissRounds, "SwissRounds should round-trip")
	assert.Equal(t, 2, loaded.SwissCurrentRound, "SwissCurrentRound should round-trip")
	assert.Equal(t, CompFormatSwiss, loaded.Format, "Format should round-trip")
}

// TestLeagueFormatHidesPlayoffs verifies FR-050 / FR-051:
// IsPlayoffEnabled() reports whether the competition's format includes a
// playoff phase. League and pure-pools formats return false; playoffs and
// mixed return true.
func TestLeagueFormatHidesPlayoffs(t *testing.T) {
	cases := []struct {
		format string
		want   bool
	}{
		{format: "league", want: false},
		{format: "pools", want: false},
		{format: "playoffs", want: true},
		{format: "mixed", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			c := Competition{Format: tc.format}
			assert.Equal(t, tc.want, c.IsPlayoffEnabled(), "format=%q", tc.format)
		})
	}
}

// TestCopyCompetition_WithPlayersAndCourts exercises the Players and
// Courts slice-copy branches in copyCompetition so mutations to the
// copy don't alias back to the original.
func TestCopyCompetition_WithPlayersAndCourts(t *testing.T) {
	dir, err := os.MkdirTemp("", "copy-comp-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:     "copy-test",
		Name:   "Copy Test",
		Courts: []string{"A", "B"},
		Players: []domain.Player{
			{Name: "Alice", Dojo: "DojoA"},
			{Name: "Bob", Dojo: "DojoB"},
		},
	}

	cp := store.copyCompetition(comp)
	require.NotNil(t, cp)

	// Mutate the copy's slice — original must be unaffected.
	cp.Courts[0] = "Z"
	assert.Equal(t, "A", comp.Courts[0], "original Courts must not be aliased")

	cp.Players[0].Name = "Modified"
	assert.Equal(t, "Alice", comp.Players[0].Name, "original Players must not be aliased")
}

// TestCopyCompetition_Nil verifies that copyCompetition(nil) returns nil
// without panicking.
func TestCopyCompetition_Nil(t *testing.T) {
	dir, err := os.MkdirTemp("", "copy-nil-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	cp := store.copyCompetition(nil)
	assert.Nil(t, cp)
}

// TestApplyCompetitionDefaults_MatchDurationPromotion verifies that a
// legacy MatchDuration value is promoted to the per-phase fields when they
// are zero.
func TestApplyCompetitionDefaults_MatchDurationPromotion(t *testing.T) {
	c := &Competition{MatchDuration: 5}
	ApplyCompetitionDefaults(c)
	assert.Equal(t, 5, c.PoolMatchDuration, "MatchDuration should promote to PoolMatchDuration")
	assert.Equal(t, 5, c.PlayoffMatchDuration, "MatchDuration should promote to PlayoffMatchDuration")
}

// TestApplyCompetitionDefaults_NoPromotionWhenAlreadySet verifies that
// existing per-phase durations are NOT overwritten by the legacy value.
func TestApplyCompetitionDefaults_NoPromotionWhenAlreadySet(t *testing.T) {
	c := &Competition{PoolMatchDuration: 4, PlayoffMatchDuration: 6, MatchDuration: 5}
	ApplyCompetitionDefaults(c)
	assert.Equal(t, 4, c.PoolMatchDuration)
	assert.Equal(t, 6, c.PlayoffMatchDuration)
}

// TestLoadCompetitionLocked_InvalidCompID covers the ValidateCompetitionID
// error branch inside loadCompetitionLocked.
func TestLoadCompetitionLocked_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.loadCompetitionLocked("")
	assert.Error(t, err)
}

// TestLoadCompetitionLocked_MalformedYAML covers the parseCompetitionFile
// error branch: a config.md with invalid YAML front-matter returns an error.
func TestLoadCompetitionLocked_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	compID := "bad-yaml"
	store, err := NewStore(dir)
	require.NoError(t, err)

	// Create the competition directory and write a malformed config.md.
	compDir := dir + "/competitions/" + compID
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	require.NoError(t, os.WriteFile(compDir+"/config.md", []byte("---\n: : :\n---\n"), 0o600))

	_, err = store.loadCompetitionLocked(compID)
	assert.Error(t, err)
}

// TestSaveCompetitionLocked_InvalidCompID covers the ValidateCompetitionID
// error branch inside saveCompetitionLocked.
func TestSaveCompetitionLocked_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	err = store.saveCompetitionLocked(&Competition{ID: ""}, store.directWrite)
	assert.Error(t, err)
}

// TestParseCompetitionFile_MalformedYAML covers the parseFrontMatter error
// path in parseCompetitionFile.
func TestParseCompetitionFile_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/config.md"
	require.NoError(t, os.WriteFile(path, []byte("---\n: : :\n---\n"), 0o600))
	_, err := parseCompetitionFile(path)
	assert.Error(t, err)
}

// TestSaveCompetitionChanged_NoChange verifies the bytes.Equal early-exit path
// in saveCompetitionChangedLocked: saving an identical Competition twice must
// return changed=false on the second call without touching the file.
func TestSaveCompetitionChanged_NoChange(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)

	comp := &Competition{
		ID:   "no-change-comp",
		Name: "Same Struct",
	}

	// First save — file doesn't exist yet, must report changed.
	changed1, err := store.SaveCompetitionChanged(comp)
	require.NoError(t, err)
	assert.True(t, changed1, "first save must report changed=true")

	// Second save with identical struct — bytes.Equal path, must report false.
	changed2, err := store.SaveCompetitionChanged(comp)
	require.NoError(t, err)
	assert.False(t, changed2, "second identical save must report changed=false")
}

// TestLoadReservedSlots_InvalidCompID covers the ValidateCompetitionID
// error branch in LoadReservedSlots.
func TestLoadReservedSlots_InvalidCompID(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	_, err = store.LoadReservedSlots("")
	assert.Error(t, err)
}

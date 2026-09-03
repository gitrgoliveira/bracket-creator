package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A pre-UUID participants.csv is NOT rewritten on read: the legacy no-id
// shape is byte-indistinguishable from a roster carrying non-UUID client ids
// awaiting its deferred HasParticipantIDs flip (the mp-p7n ambiguity), and a
// read-side rewrite built on that sniff persisted the shifted mis-parse.
// Roster ids convert at the WRITE boundary instead (marshalParticipantsCSV
// mints ids on every save). This pins the read side staying hands-off.
func TestLegacyParticipantsNotRewrittenOnRead(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	legacy := "Aiko Sato, Seibukan\nJun Mori, Tobukan\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "participants.csv"),
		[]byte(legacy), 0o600))

	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	players, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.Equal(t, "Aiko Sato", players[0].Name)

	b, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "participants.csv"))
	require.NoError(t, err)
	assert.Equal(t, legacy, string(b), "reads must not rewrite the roster file")
}

// A legacy seeds.csv row (no dojo) is completed from the roster on first
// read when the name is unique there; an ambiguous name is left alone
// (AssignSeeds refuses that seeding either way, and guessing would be worse).
func TestLegacySeedDojoUpgradeOnRead(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{Name: "Rin Sato", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "seeds.csv"),
		[]byte("Rank,Name\n1,Rin Sato\n2,Yuki Tanaka\n"), 0o600))

	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	_, err = fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 2)
	bySeed := map[int]string{}
	for _, sd := range seeds {
		bySeed[sd.SeedRank] = sd.Dojo
	}
	assert.Equal(t, "Seibukan", bySeed[1], "unique name: dojo completed from the roster")
	assert.Equal(t, "", bySeed[2], "ambiguous name: left legacy, never guessed")
}

// TestLegacyPlayoffsConfigConvergesOnKnockout pins the bc-terminology commit 1
// migration against a REAL pre-rename config.md (testdata/legacy_playoffs_config.md
// is byte-copied, unmodified, from tournament-data/competitions/bracket-court-d
// in the main checkout, which carries format: playoffs, status: playoffs, AND
// the legacy whole-minute playoff_match_duration key all at once).
//
// It goes through state.NewStore rather than calling the upgrade function
// directly, so this also exercises the EAGER startup sweep (SweepLegacyUpgrades):
// the file must already be converted by the time NewStore returns, not lazily
// on first touch.
func TestLegacyPlayoffsConfigConvergesOnKnockout(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))

	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), fixture, 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format, "format: playoffs must convert to knockout")
	assert.Equal(t, state.CompStatusKnockout, comp.Status, "status: playoffs must convert to knockout")
	assert.Equal(t, 300, comp.KnockoutMatchDurationSeconds, "the legacy playoff_match_duration: 5 (minutes) must still fold to 300s")
	assert.Equal(t, 180, comp.PoolMatchDurationSeconds, "the legacy pool_match_duration: 3 (minutes) must still fold to 180s")
	// Untouched fields survive the rewrite.
	assert.Equal(t, "Bracket Court D", comp.Name)
	assert.Equal(t, "D", comp.NumberPrefix)
	assert.Equal(t, []string{"D"}, comp.Courts)

	// The ON-DISK file converges too, not just the in-memory read: the next
	// operator to open this folder in a text editor sees the canonical shape.
	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "format: knockout")
	assert.Contains(t, string(raw), "status: knockout")
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 300")
	assert.NotContains(t, string(raw), "playoffs")
	assert.NotContains(t, string(raw), "playoff_match_duration")

	// A second sweep over the now-converted file is a no-op: idempotent, no
	// error, nothing flips back.
	require.NoError(t, s.SweepLegacyUpgrades())
	comp2, err := s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)
	assert.Equal(t, state.CompFormatKnockout, comp2.Format)
}

// TestEnsureLegacyUpgradedRetriesAfterFailure pins the bc-terminology
// commit 2 fix: a FAILED format/status conversion must keep being reported
// on every subsequent call, not just the first. The bug stamped the
// legacyUpgraded once-map on the failure path too, so a second call saw
// `done` and returned nil while config.md was still unconverted -- silently
// mis-classifying the competition, exactly what EnsureLegacyUpgraded's own
// doc comment says the lazy caller refuses to do.
//
// The conversion is forced to fail by making the competition directory
// read-only: os.MkdirAll on an already-existing directory is a no-op (it
// does not chmod), so the read-only mode only bites once the rewrite tries
// to create its atomic-write temp file, which needs write permission on the
// directory itself.
func TestEnsureLegacyUpgradedRetriesAfterFailure(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)

	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), fixture, 0o600))

	require.NoError(t, os.Chmod(compDir, 0o500))
	defer func() { _ = os.Chmod(compDir, 0o700) }() // let t.TempDir() clean up afterwards

	err1 := s.EnsureLegacyUpgraded("bracket-court-d")
	require.Error(t, err1, "the config.md rewrite must fail against a read-only directory")

	err2 := s.EnsureLegacyUpgraded("bracket-court-d")
	require.Error(t, err2, "a second call must keep reporting the failure, not silently report success")

	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "format: playoffs", "the file must still be unconverted after the masked-success bug")

	// Restore write access: the once-map must not have been poisoned by the
	// failure, so the conversion is retried and this time succeeds.
	require.NoError(t, os.Chmod(compDir, 0o700))
	require.NoError(t, s.EnsureLegacyUpgraded("bracket-court-d"))
	comp, err := s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)
	assert.Equal(t, state.CompFormatKnockout, comp.Format)
}

// TestLegacyPlayoffMatchDurationSecondsFoldsOntoKnockoutKey covers the
// post-rename-retired playoff_match_duration_seconds key: unlike the
// whole-minute playoff_match_duration (whose yaml tag is unchanged, so
// ApplyCompetitionDefaults' existing fold already picks it up on every
// load), this key's Go field was RENAMED, so an old file carrying only this
// key is invisible to the ordinary parse path and would silently lose its
// configured duration without upgradeCompetitionFormatLocked's raw-YAML
// recovery. No such file exists on disk today (the seconds field is newer
// than any real legacy config.md this repo has), but current pre-rename
// builds write it, so a config.md carrying it will exist in the wild.
func TestLegacyPlayoffMatchDurationSecondsFoldsOntoKnockoutKey(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "seconds-legacy")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	legacy := "---\n" +
		"id: seconds-legacy\n" +
		"name: Seconds Legacy\n" +
		"kind: individual\n" +
		"format: mixed\n" +
		"courts:\n" +
		"    - A\n" +
		"status: pools\n" +
		"playoff_match_duration_seconds: 180\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("seconds-legacy")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, "mixed", comp.Format, "format was already canonical and must be left alone")
	assert.Equal(t, 180, comp.KnockoutMatchDurationSeconds,
		"the pre-rename seconds key must fold onto the renamed field, not silently vanish")

	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 180")
	assert.NotContains(t, string(raw), "playoff_match_duration_seconds")
}

// TestLoadCompetitionUpgradesLegacyFormatOnLoad pins the gap closed by wiring
// EnsureLegacyUpgraded into Store.LoadCompetition itself: before this,
// LoadCompetition read straight through loadCached and could serve a
// config.md still carrying the retired "playoffs" wire values with no error
// at all, since Format/Status are bare strings with no load-time validator.
//
// The legacy config.md is written to disk AFTER NewStore returns, so the
// eager startup sweep (SweepLegacyUpgrades) never sees it and cannot be what
// converts it -- this isolates LoadCompetition's OWN call to
// EnsureLegacyUpgraded from the sweep's.
func TestLoadCompetitionUpgradesLegacyFormatOnLoad(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)

	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), fixture, 0o600))

	comp, err := s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format, "format: playoffs must convert to knockout on load, not just via the startup sweep")
	assert.Equal(t, state.CompStatusKnockout, comp.Status, "status: playoffs must convert to knockout on load")
	assert.Equal(t, 300, comp.KnockoutMatchDurationSeconds, "the legacy playoff_match_duration: 5 (minutes) must still fold to 300s")

	// The on-disk file converges too, exactly as the sweep path does.
	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "format: knockout")
	assert.Contains(t, string(raw), "status: knockout")
	assert.NotContains(t, string(raw), "playoffs")
}

// TestLoadCompetitionMissingReturnsNilNil pins that wiring
// EnsureLegacyUpgraded into LoadCompetition must not change its contract for
// a competition that does not exist on disk: EnsureLegacyUpgraded's own
// conversion (upgradeCompetitionFormatLocked) already returns nil when
// config.md is missing (nothing to convert), and this confirms that holds
// through LoadCompetition too, rather than the new call turning a plain
// "not found" into an error.
func TestLoadCompetitionMissingReturnsNilNil(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("does-not-exist")
	require.NoError(t, err)
	assert.Nil(t, comp)
}

// TestNewStoreSurvivesSweepFailureButLoadCompetitionRefuses pins the other
// half of the gap closure: now that LoadCompetition enforces the format/
// status conversion itself, the eager startup sweep no longer needs to be
// fatal. A single competition whose config.md cannot be rewritten (its
// directory is read-only) must not take down store construction for every
// other competition and court -- but it must also not be silently served:
// LoadCompetition still refuses it.
//
// Same failure-forcing technique as TestEnsureLegacyUpgradedRetriesAfterFailure:
// os.MkdirAll on an already-existing directory is a no-op, so making the
// directory read-only only bites once the rewrite tries to create its
// atomic-write temp file. The fixture needs a valid `id:` in its front
// matter (bracket-court-d, matching the directory name) to reach that write
// at all.
func TestNewStoreSurvivesSweepFailureButLoadCompetitionRefuses(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), fixture, 0o600))

	require.NoError(t, os.Chmod(compDir, 0o500))
	defer func() { _ = os.Chmod(compDir, 0o700) }() // let t.TempDir() clean up afterwards

	// The sweep hits this competition during construction and fails to
	// convert it (read-only directory), but that failure must be logged, not
	// propagated: store construction must still succeed so every other
	// competition and court can start.
	s, err := state.NewStore(dir)
	require.NoError(t, err, "a single competition's sweep failure must not fail store construction")

	// The broken competition must still refuse to be served in its
	// unconverted shape: LoadCompetition enforces what the sweep couldn't.
	comp, err := s.LoadCompetition("bracket-court-d")
	require.Error(t, err, "an unconverted legacy competition must not be silently served")
	assert.Nil(t, comp)

	// Restore write access and confirm the competition recovers: the
	// once-map must not have been poisoned by the earlier failure.
	require.NoError(t, os.Chmod(compDir, 0o700))
	comp2, err := s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)
	require.NotNil(t, comp2)
	assert.Equal(t, state.CompFormatKnockout, comp2.Format)
}

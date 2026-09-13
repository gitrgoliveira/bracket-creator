package state_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

// TestLoadCompetitionFoldsLegacyPlayoffsFormat pins the bc-terminology
// commit 1 migration against a REAL pre-rename config.md
// (testdata/legacy_playoffs_config.md is byte-copied, unmodified, from
// tournament-data/competitions/bracket-court-d in the main checkout, which
// carries format: playoffs, status: playoffs, AND the legacy whole-minute
// playoff_match_duration key all at once).
//
// The fold happens IN MEMORY inside parseCompetitionFile, the single funnel
// every competition read goes through, and needs no per-competition write
// lock of its own. These assertions hold regardless of whether NewStore's
// startup sweep (sweepLegacyUpgrades, legacy_upgrade.go) has already
// converged this file on disk by the time LoadCompetition runs here: either
// way the returned struct is folded. See
// TestLegacyUpgradeSweepConvergesConfigOnDisk for that write-side half of the
// contract, and TestLoadCompetitionDoesNotRewriteConfigOnDisk for the
// narrower claim that LoadCompetition ITSELF never writes.
func TestLoadCompetitionFoldsLegacyPlayoffsFormat(t *testing.T) {
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
	// Untouched fields survive the fold.
	assert.Equal(t, "Bracket Court D", comp.Name)
	assert.Equal(t, "D", comp.NumberPrefix)
	assert.Equal(t, []string{"D"}, comp.Courts)
}

// TestLoadCompetitionDoesNotRewriteConfigOnDisk pins that LoadCompetition
// ITSELF never writes to config.md: the format/status/duration fold it
// returns is purely an in-memory transform of the parsed struct.
// LoadCompetition is deliberately not one of EnsureLegacyUpgraded's six
// callers (legacy_upgrade.go's own doc comment enumerates them), so calling
// it can never trigger the write-side convergence pass.
//
// The competition directory is created and populated AFTER this test's own
// Store is already open, specifically so NewStore's startup sweep (which
// also converges config.md -- see TestLegacyUpgradeSweepConvergesConfigOnDisk
// -- and would otherwise convert this exact fixture before LoadCompetition
// ever ran) cannot be what's being observed here. That would still leave
// config.md byte-stable across the LoadCompetition call, but for the wrong
// reason: it would prove nothing about LoadCompetition itself.
func TestLoadCompetitionDoesNotRewriteConfigOnDisk(t *testing.T) {
	dir := t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)

	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	configPath := filepath.Join(compDir, "config.md")
	require.NoError(t, os.WriteFile(configPath, fixture, 0o600))
	before, err := os.ReadFile(configPath)
	require.NoError(t, err)

	_, err = s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)

	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "LoadCompetition itself must never rewrite config.md")
}

// TestSaveConvergesLegacyConfigOnDisk covers the other half: the on-disk file
// converges onto the canonical values once something actually saves the
// competition. SaveCompetition re-serialises whatever LoadCompetition handed
// back, which is already folded, so no retired key or value survives.
//
// Like TestLoadCompetitionDoesNotRewriteConfigOnDisk, the competition
// directory is populated AFTER this test's Store is already open, so it is
// genuinely SaveCompetition proving this, and not NewStore's startup sweep
// (TestLegacyUpgradeSweepConvergesConfigOnDisk) having already converged the
// file before LoadCompetition/SaveCompetition ever ran.
func TestSaveConvergesLegacyConfigOnDisk(t *testing.T) {
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
	require.NoError(t, s.SaveCompetition(comp))

	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "format: knockout")
	assert.Contains(t, string(raw), "status: knockout")
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 300")
	assert.NotContains(t, string(raw), "playoffs")
	assert.NotContains(t, string(raw), "playoff_match_duration")
}

// TestLoadCompetitionWithMismatchedIDDoesNotCorruptAnotherCompetition is a
// regression test for bug 1 in the deleted migration:
// upgradeCompetitionFormatLocked read a competition by its DIRECTORY
// (compID) but saved it back via saveCompetitionLocked, which paths off
// comp.ID -- the "id:" front-matter field -- and documents that its caller
// must already hold THAT id's lock. A directory whose id: didn't match its
// own directory name was therefore converted INTO the other competition's
// directory, under the wrong lock, overwriting it.
//
// The new design never writes anything on a read, so this class of bug
// cannot recur: loading the mismatched directory must leave every file on
// disk -- including the victim's -- byte for byte as it was, and must not
// create any new directory.
func TestLoadCompetitionWithMismatchedIDDoesNotCorruptAnotherCompetition(t *testing.T) {
	dir := t.TempDir()

	// The real "victim" competition: its own id: matches its directory name.
	victimDir := filepath.Join(dir, "competitions", "victim")
	require.NoError(t, os.MkdirAll(victimDir, 0o700))
	victimConfig := "---\n" +
		"id: victim\n" +
		"name: Victim Competition\n" +
		"kind: individual\n" +
		"format: mixed\n" +
		"status: pools\n" +
		"---\n"
	victimPath := filepath.Join(victimDir, "config.md")
	require.NoError(t, os.WriteFile(victimPath, []byte(victimConfig), 0o600))
	victimBefore, err := os.ReadFile(victimPath)
	require.NoError(t, err)

	// The "mismatched" competition: it lives in its own directory, but its
	// id: front-matter field names the VICTIM directory instead.
	mismatchedDir := filepath.Join(dir, "competitions", "mismatched")
	require.NoError(t, os.MkdirAll(mismatchedDir, 0o700))
	mismatchedConfig := "---\n" +
		"id: victim\n" +
		"name: Mismatched Competition\n" +
		"kind: individual\n" +
		"format: playoffs\n" +
		"status: playoffs\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(mismatchedDir, "config.md"), []byte(mismatchedConfig), 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("mismatched")
	require.NoError(t, err, "a mismatched id: must still load without error")
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format, "the retired value still folds in memory")

	victimAfter, err := os.ReadFile(victimPath)
	require.NoError(t, err)
	assert.Equal(t, victimBefore, victimAfter, "the victim competition's file must be byte-unchanged")

	entries, err := os.ReadDir(filepath.Join(dir, "competitions"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"victim", "mismatched"}, names, "no new directory must be created")
}

// TestLoadCompetitionWithNoIDStillLoads is a regression test for bug 2 in the
// deleted migration: upgradeCompetitionFormatLocked's save path validated
// comp.ID via ValidateCompetitionID, so a config.md with a blank or missing
// id: field failed that validation and every subsequent load returned an
// error forever -- even though the exact same file loaded fine before this
// migration existed. The new design never validates or writes comp.ID on a
// read, so a missing id: must still load.
func TestLoadCompetitionWithNoIDStillLoads(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "no-id-comp")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	legacy := "---\n" +
		"name: No ID Competition\n" +
		"kind: individual\n" +
		"format: playoffs\n" +
		"status: playoffs\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("no-id-comp")
	require.NoError(t, err, "a config.md with no id: must still load")
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format)
	assert.Equal(t, state.CompStatusKnockout, comp.Status)
	// The directory IS the identity (ids are name slugs, and every other
	// store path keys off the folder), so a file with no id: has its ID
	// adopted from the directory on load rather than reaching callers empty.
	assert.Equal(t, "no-id-comp", comp.ID, "a blank id: is adopted from the directory on load")
}

// TestLoadCompetitionKnockoutSecondsWinOverLegacyMinutes is a regression test
// for bug 3 in the deleted migration: the old guard checked
// KnockoutMatchDurationSeconds == 0 AFTER ApplyCompetitionDefaults had
// already back-filled it from the whole-minute key, so a config.md carrying
// BOTH playoff_match_duration: 5 (-> 300s) and
// playoff_match_duration_seconds: 150 resolved to 300 -- and the rewrite
// then deleted the more precise seconds key permanently. The fold now lives
// entirely inside ApplyCompetitionDefaults, where the retired seconds key is
// checked BEFORE the whole-minute one, so the explicit value wins.
func TestLoadCompetitionKnockoutSecondsWinOverLegacyMinutes(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "both-duration-keys")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	legacy := "---\n" +
		"id: both-duration-keys\n" +
		"name: Both Duration Keys\n" +
		"kind: individual\n" +
		"format: knockout\n" +
		"status: setup\n" +
		"courts:\n" +
		"    - A\n" +
		"playoff_match_duration: 5\n" +
		"playoff_match_duration_seconds: 150\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("both-duration-keys")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, 150, comp.KnockoutMatchDurationSeconds,
		"the explicit seconds value must win over the whole-minute key rounded up to 300")
}

// TestLegacyPlayoffMatchDurationSecondsFoldsOntoKnockoutKey covers the
// post-rename-retired playoff_match_duration_seconds key on its own (no
// whole-minute key present): unlike playoff_match_duration (whose yaml tag
// is unchanged, so ApplyCompetitionDefaults' ordinary fold already picks it
// up), this key's Go field was renamed, so it needs its own struct field
// (KnockoutMatchDurationSecondsLegacy) or an old file carrying only this key
// would silently lose its configured duration.
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

	// On-disk convergence happens once something saves the record -- either
	// explicitly, as here, or via NewStore's startup sweep, which may have
	// already converged this exact file before NewStore even returned above
	// (see TestLegacyUpgradeSweepConvergesConfigOnDisk). Either way the
	// assertions below hold.
	require.NoError(t, s.SaveCompetition(comp))
	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 180")
	assert.NotContains(t, string(raw), "playoff_match_duration_seconds")
}

// TestLegacyUpgradeSweepConvergesConfigOnDisk pins the write-side half of the
// bc-terminology commit 1 migration: NewStore's startup sweep
// (sweepLegacyUpgrades, legacy_upgrade.go) converges a legacy config.md on
// disk without anything ever explicitly calling LoadCompetition or
// SaveCompetition -- opening the store is enough. Uses the same real,
// unmodified fixture as TestLoadCompetitionFoldsLegacyPlayoffsFormat.
func TestLegacyUpgradeSweepConvergesConfigOnDisk(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), fixture, 0o600))

	_, err = state.NewStore(dir) // the startup sweep runs synchronously, inside this call
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "format: knockout")
	assert.Contains(t, string(raw), "status: knockout")
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 300")
	assert.NotContains(t, string(raw), "playoffs")
	assert.NotContains(t, string(raw), "playoff_match_duration")
}

// TestLegacyUpgradeSweepSkipsMismatchedCompetitionID is BUG 1's regression
// test against the startup-sweep path specifically.
// (TestLoadCompetitionWithMismatchedIDDoesNotCorruptAnotherCompetition pins
// the same guard against a direct LoadCompetition call, which never wrote
// anything at all even before this migration existed -- that test cannot
// tell a correct guard apart from "nothing ever writes", since both leave
// the files alone.) ListCompetitions surfaces BOTH directories below, so
// the sweep actually attempts to converge "mismatched" too; the guard
// inside upgradeCompetitionFormatLocked must refuse to save because the
// loaded id: ("victim") does not match the directory it was read from
// ("mismatched"), rather than saving into competitions/victim/config.md
// under the wrong lock and destroying it.
func TestLegacyUpgradeSweepSkipsMismatchedCompetitionID(t *testing.T) {
	dir := t.TempDir()

	// The real "victim" competition: its own id: matches its directory name,
	// and it is already fully canonical (nothing for the sweep to converge).
	victimDir := filepath.Join(dir, "competitions", "victim")
	require.NoError(t, os.MkdirAll(victimDir, 0o700))
	victimConfig := "---\n" +
		"id: victim\n" +
		"name: Victim Competition\n" +
		"kind: individual\n" +
		"format: mixed\n" +
		"status: pools\n" +
		"---\n"
	victimPath := filepath.Join(victimDir, "config.md")
	require.NoError(t, os.WriteFile(victimPath, []byte(victimConfig), 0o600))
	victimBefore, err := os.ReadFile(victimPath)
	require.NoError(t, err)

	// The "mismatched" competition: it lives in its own directory, carries
	// retired format/status values (so the sweep DOES attempt to converge
	// it), but its id: front-matter field names the VICTIM directory instead.
	mismatchedDir := filepath.Join(dir, "competitions", "mismatched")
	require.NoError(t, os.MkdirAll(mismatchedDir, 0o700))
	mismatchedConfig := "---\n" +
		"id: victim\n" +
		"name: Mismatched Competition\n" +
		"kind: individual\n" +
		"format: playoffs\n" +
		"status: playoffs\n" +
		"---\n"
	mismatchedPath := filepath.Join(mismatchedDir, "config.md")
	require.NoError(t, os.WriteFile(mismatchedPath, []byte(mismatchedConfig), 0o600))
	mismatchedBefore, err := os.ReadFile(mismatchedPath)
	require.NoError(t, err)

	_, err = state.NewStore(dir) // runs the startup sweep over both directories
	require.NoError(t, err)

	victimAfter, err := os.ReadFile(victimPath)
	require.NoError(t, err)
	assert.Equal(t, victimBefore, victimAfter, "the victim competition's file must be byte-unchanged")

	mismatchedAfter, err := os.ReadFile(mismatchedPath)
	require.NoError(t, err)
	assert.Equal(t, mismatchedBefore, mismatchedAfter,
		"the mismatched file itself is also left unconverted: the guard skips the save entirely rather than writing it back under its own directory")

	entries, err := os.ReadDir(filepath.Join(dir, "competitions"))
	require.NoError(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	assert.ElementsMatch(t, []string{"victim", "mismatched"}, names, "no new directory must be created for the foreign id")
}

// TestLegacyUpgradeSweepConvergesBlankCompetitionID covers BUG 2's territory
// after the blank-id fix. The original bug was that a config.md with no id:
// became permanently UNREADABLE, because the save's ValidateCompetitionID("")
// error propagated out through every load path; that must never return.
//
// It is no longer merely tolerated, though. adoptDirectoryID (competition.go)
// fills the ID from the directory on load, so the record reaches the sweep
// with a valid identity, the id/directory guard passes, and the file
// CONVERGES -- gaining the id: it was missing along with the canonical
// format/status. A MISMATCHED id is still skipped; that is two competing
// claims, whereas a blank one has none.
func TestLegacyUpgradeSweepConvergesBlankCompetitionID(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "no-id-comp")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	legacy := "---\n" +
		"name: No ID Competition\n" +
		"kind: individual\n" +
		"format: playoffs\n" +
		"status: playoffs\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	s, err := state.NewStore(dir) // must succeed even though this file has no id:
	require.NoError(t, err)

	comp, err := s.LoadCompetition("no-id-comp")
	require.NoError(t, err, "a config.md with no id: must still load after the sweep has run over it")
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format)
	assert.Equal(t, state.CompStatusKnockout, comp.Status)
	assert.Equal(t, "no-id-comp", comp.ID, "the ID is adopted from the directory")

	// And the file itself converges, id included, instead of being skipped.
	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "id: no-id-comp", "the missing id: is written back")
	assert.Contains(t, string(raw), "format: knockout")
	assert.Contains(t, string(raw), "status: knockout")
	assert.NotContains(t, string(raw), "playoffs")
}

// TestLegacyUpgradeSweepKnockoutSecondsWinOverGlobalMinutes is BUG 3's
// regression test against the startup-sweep path: a config.md carrying the
// retired per-phase seconds key (playoff_match_duration_seconds: 150)
// ALONGSIDE the global whole-minute fallback (match_duration: 3, which would
// round up to 180) must converge to the precise 150s value, never the
// coarser 180s one. The deleted migration's bug 3 re-derived the duration
// itself and checked its own zero-guard AFTER ApplyCompetitionDefaults had
// already back-filled from the whole-minute key; this migration does no
// duration arithmetic of its own at all (see upgradeCompetitionFormatLocked's
// doc comment), so there is no guard-ordering mistake left to make.
func TestLegacyUpgradeSweepKnockoutSecondsWinOverGlobalMinutes(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "global-minutes-comp")
	require.NoError(t, os.MkdirAll(compDir, 0o700))
	legacy := "---\n" +
		"id: global-minutes-comp\n" +
		"name: Global Minutes Comp\n" +
		"kind: individual\n" +
		"format: knockout\n" +
		"status: setup\n" +
		"courts:\n" +
		"    - A\n" +
		"match_duration: 3\n" +
		"playoff_match_duration_seconds: 150\n" +
		"---\n"
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	s, err := state.NewStore(dir)
	require.NoError(t, err)

	comp, err := s.LoadCompetition("global-minutes-comp")
	require.NoError(t, err)
	require.NotNil(t, comp)
	assert.Equal(t, 150, comp.KnockoutMatchDurationSeconds,
		"the retired per-phase seconds key must win over the global whole-minute fallback, which would round up to 180")

	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 150")
	assert.NotContains(t, string(raw), "playoff_match_duration_seconds")
	assert.NotContains(t, string(raw), "match_duration: 3")
}

// TestLegacyUpgradeSweepFailureIsolatedPerCompetition: when the on-disk
// convergence write fails for one competition (its directory is read-only),
// NewStore must still succeed, and a subsequent LoadCompetition for that
// same competition must still return the folded values -- the in-memory
// safety net in parseCompetitionFile does not depend on the write ever
// landing. This is the "best-effort, never a safety mechanism" property the
// whole migration rests on.
func TestLegacyUpgradeSweepFailureIsolatedPerCompetition(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 isn't enforced on Windows the same way")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}

	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "locked-comp")
	require.NoError(t, os.MkdirAll(compDir, 0o700))

	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	// The fixture's own id: is "bracket-court-d"; give this copy an id:
	// matching ITS OWN directory instead, so bug 1's guard cannot be what
	// blocks the write -- this test needs the write to fail for a
	// PERMISSION reason, not because the migration correctly declined an
	// unrelated mismatched id.
	legacy := strings.Replace(string(fixture), "id: bracket-court-d", "id: locked-comp", 1)
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "config.md"), []byte(legacy), 0o600))

	require.NoError(t, os.Chmod(compDir, 0500))
	defer func() { _ = os.Chmod(compDir, 0700) }() // let t.TempDir() clean up

	s, err := state.NewStore(dir)
	require.NoError(t, err, "one broken competition directory must not stop NewStore from succeeding")

	comp, err := s.LoadCompetition("locked-comp")
	require.NoError(t, err, "a load must still succeed and fold in memory even though the on-disk convergence write failed")
	require.NotNil(t, comp)
	assert.Equal(t, state.CompFormatKnockout, comp.Format)
	assert.Equal(t, state.CompStatusKnockout, comp.Status)
	assert.Equal(t, 300, comp.KnockoutMatchDurationSeconds)
}

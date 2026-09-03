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

// TestLoadCompetitionFoldsLegacyPlayoffsFormat pins the bc-terminology
// commit 1 migration against a REAL pre-rename config.md
// (testdata/legacy_playoffs_config.md is byte-copied, unmodified, from
// tournament-data/competitions/bracket-court-d in the main checkout, which
// carries format: playoffs, status: playoffs, AND the legacy whole-minute
// playoff_match_duration key all at once).
//
// The fold happens IN MEMORY inside parseCompetitionFile, the single funnel
// every competition read goes through -- there is no separate upgrade pass
// to warm up, and no per-competition write lock is involved. See
// TestLoadCompetitionDoesNotRewriteConfigOnDisk for the other half of the
// contract: this call leaves config.md itself untouched.
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

// TestLoadCompetitionDoesNotRewriteConfigOnDisk pins the core property of the
// replacement design: unlike the deleted write-on-read migration
// (upgradeCompetitionFormatLocked), a load NEVER touches config.md. The
// format/status fold is purely an in-memory transform of the parsed struct;
// the on-disk bytes still read "playoffs" until something actually SAVES the
// competition (TestSaveConvergesLegacyConfigOnDisk covers that half).
func TestLoadCompetitionDoesNotRewriteConfigOnDisk(t *testing.T) {
	dir := t.TempDir()
	compDir := filepath.Join(dir, "competitions", "bracket-court-d")
	require.NoError(t, os.MkdirAll(compDir, 0o700))

	fixture, err := os.ReadFile(filepath.Join("testdata", "legacy_playoffs_config.md"))
	require.NoError(t, err)
	configPath := filepath.Join(compDir, "config.md")
	require.NoError(t, os.WriteFile(configPath, fixture, 0o600))
	before, err := os.ReadFile(configPath)
	require.NoError(t, err)

	s, err := state.NewStore(dir)
	require.NoError(t, err)
	_, err = s.LoadCompetition("bracket-court-d")
	require.NoError(t, err)

	after, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a load must never rewrite config.md")
}

// TestSaveConvergesLegacyConfigOnDisk covers the other half: the on-disk file
// converges onto the canonical values once something actually saves the
// competition. SaveCompetition re-serialises whatever LoadCompetition handed
// back, which is already folded, so no retired key or value survives.
func TestSaveConvergesLegacyConfigOnDisk(t *testing.T) {
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
	assert.Equal(t, "", comp.ID, "the file carries no id:, and a load must not invent or validate one")
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

	// On-disk convergence only happens once something actually saves the
	// record (see TestLoadCompetitionDoesNotRewriteConfigOnDisk).
	require.NoError(t, s.SaveCompetition(comp))
	raw, err := os.ReadFile(filepath.Join(compDir, "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "knockout_match_duration_seconds: 180")
	assert.NotContains(t, string(raw), "playoff_match_duration_seconds")
}

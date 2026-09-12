package state_test

// legacy_upgrade_seed_ids_test.go pins the bc-sdid widening of the seeds.csv
// load-time repair (upgradeSeedRowsLocked, legacy_upgrade.go): a row missing
// its participant id is stamped in the SAME pass that already backfills a
// missing dojo, using newLegacyUpgradeFixture/freshLegacyUpgradeStore from
// this package's legacy_upgrade_pool_ids_test.go sibling.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacySeedIDUpgrade_DojoPresentIDMissing: a seeds.csv row that already
// carries a dojo (written after the Dojo column existed but before the ID
// column did) gets its id stamped by an exact (name, dojo) match -- no
// ambiguity guard needed, since the roster refuses to save that pair twice.
func TestLegacySeedIDUpgrade_DojoPresentIDMissing(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
	}))

	// Pre-ID-column shape: Rank,Name,Dojo (no ID column at all).
	seedsPath := filepath.Join(dir, "competitions", "c1", "seeds.csv")
	require.NoError(t, os.WriteFile(seedsPath, []byte("Rank,Name,Dojo\n1,Rin Sato,Seibukan\n"), 0o600))

	// LoadSeedsRaw is not itself one of the five entry points that runs
	// EnsureLegacyUpgraded (see that function's own doc comment); a plain
	// LoadParticipants call is what actually triggers the repair pass here,
	// exactly as the pre-existing TestLegacySeedDojoUpgradeOnRead does.
	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, rinID, seeds[0].ID, "the id is stamped from an exact (name, dojo) match")

	raw, err := os.ReadFile(seedsPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), rinID, "the repair lands on disk, not just in the returned copy")
}

// TestLegacySeedIDUpgrade_NoDojoUniqueNameRepairsBoth: a name-only legacy row
// (no dojo, no id -- the oldest seeds.csv shape) gets BOTH fields completed
// in the same pass when the name is unique in the roster.
func TestLegacySeedIDUpgrade_NoDojoUniqueNameRepairsBoth(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: helper.NewUUID4(), Name: "Other Player", Dojo: "Tobukan"},
	}))

	seedsPath := filepath.Join(dir, "competitions", "c1", "seeds.csv")
	require.NoError(t, os.WriteFile(seedsPath, []byte("Rank,Name\n1,Rin Sato\n"), 0o600))

	// LoadSeedsRaw is not itself one of the five entry points that runs
	// EnsureLegacyUpgraded (see that function's own doc comment); a plain
	// LoadParticipants call is what actually triggers the repair pass here,
	// exactly as the pre-existing TestLegacySeedDojoUpgradeOnRead does.
	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Seibukan", seeds[0].Dojo, "the dojo is backfilled from the unique-name match")
	assert.Equal(t, rinID, seeds[0].ID, "the id is stamped from the SAME resolved participant")
}

// TestLegacySeedIDUpgrade_AmbiguousNameLeftAlone: two roster entries share
// the bare name a no-dojo seed row carries. Neither the dojo nor the id can
// be completed without guessing, so both stay empty and the row is left
// exactly as it was read -- the same residue policy every sibling upgrade in
// this file applies.
func TestLegacySeedIDUpgrade_AmbiguousNameLeftAlone(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{ID: helper.NewUUID4(), Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))

	seedsPath := filepath.Join(dir, "competitions", "c1", "seeds.csv")
	require.NoError(t, os.WriteFile(seedsPath, []byte("Rank,Name\n1,Yuki Tanaka\n"), 0o600))

	// LoadSeedsRaw is not itself one of the five entry points that runs
	// EnsureLegacyUpgraded (see that function's own doc comment); a plain
	// LoadParticipants call is what actually triggers the repair pass here,
	// exactly as the pre-existing TestLegacySeedDojoUpgradeOnRead does.
	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Empty(t, seeds[0].Dojo, "an ambiguous name must never be guessed at")
	assert.Empty(t, seeds[0].ID, "nor stamped with either namesake's id")

	raw, err := os.ReadFile(seedsPath)
	require.NoError(t, err)
	assert.Equal(t, "Rank,Name\n1,Yuki Tanaka\n", string(raw), "an unresolvable row is left byte-for-byte alone")
}

// TestLegacySeedIDUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard is the
// seeds.csv twin of TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard in
// legacy_upgrade_pool_ids_test.go: domain.RosterIndex.Lookup(name, "") tries
// the EXACT key "name|" first and only THEN falls back to the
// unique-bare-name match, so a roster carrying a BLANK-DOJO entry under a
// name a SECOND, non-blank-dojo entry also uses scores an exact hit on that
// first branch, bypassing NameCount's uniqueness guard entirely -- unless
// the guard is checked EXPLICITLY before Lookup ever runs, which is what
// upgradeSeedRowsLocked's dojo=="" branch does. Blank dojos are refused on
// save (state.ErrBlankDojo) but tolerated on load, so a legacy roster
// reaches this code with one intact; this fixture writes participants.csv
// directly for exactly that reason, since SaveParticipants cannot produce it.
func TestLegacySeedIDUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	blankDojoID := helper.NewUUID4()
	tobukanID := helper.NewUUID4()
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	rawRoster := blankDojoID + ",Yuki Tanaka,\n" + tobukanID + ",Yuki Tanaka,Tobukan\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(rawRoster), 0o600))

	seedsPath := filepath.Join(dir, "competitions", "c1", "seeds.csv")
	require.NoError(t, os.WriteFile(seedsPath, []byte("Rank,Name\n1,Yuki Tanaka\n"), 0o600))

	_, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := s.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Empty(t, seeds[0].ID,
		"Yuki Tanaka is ambiguous (a blank-dojo entry AND a Tobukan entry both carry that name): the row must be left alone, never resolved to the blank-dojo entry by coincidence")
}

// TestLegacySeedRowUpgrade_NoOpWhenNothingToRepair: a seeds.csv already
// carrying both the dojo and the id for every row (written through the
// normal SaveSeeds path) must not be rewritten by the repair -- same bytes,
// same Store.FileVersion, across a load. Mirrors
// TestLegacyPoolUpgrade_AlreadyStampedNotRewritten in this package's
// legacy_upgrade_pool_ids_test.go sibling.
func TestLegacySeedRowUpgrade_NoOpWhenNothingToRepair(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{Name: "Rin Sato", Dojo: "Seibukan"},
	}))
	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{
		{Name: "Rin Sato", Dojo: "Seibukan", SeedRank: 1},
	}))

	seedsPath := filepath.Join(dir, "competitions", "c1", "seeds.csv")
	before, err := os.ReadFile(seedsPath)
	require.NoError(t, err)

	fresh := freshLegacyUpgradeStore(t, dir)
	verBefore := fresh.FileVersion("c1", "seeds.csv")

	// LoadSeedsRaw is not itself one of the five entry points that runs
	// EnsureLegacyUpgraded; a plain LoadParticipants call is what actually
	// triggers the (here, no-op) repair pass.
	_, err = fresh.LoadParticipants("c1", false)
	require.NoError(t, err)

	seeds, err := fresh.LoadSeedsRaw("c1")
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	require.NotEmpty(t, seeds[0].ID, "SaveSeeds must have stamped the id already")

	after, err := os.ReadFile(seedsPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "an already-repaired seeds.csv must not be rewritten")
	assert.Equal(t, verBefore, fresh.FileVersion("c1", "seeds.csv"), "no version bump for an untouched file")
}

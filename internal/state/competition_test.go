package state

import (
	"encoding/json"
	"os"
	"path/filepath"
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

// TestCompetitionLegacyMatchDurationMigration verifies FR-054 / NFR-025 / R9:
// a config.md carrying only the retired whole-minute match_duration is migrated
// onto BOTH per-phase seconds fields, so older tournaments keep their schedule
// estimates instead of silently reverting to the default.
func TestCompetitionLegacyMatchDurationMigration(t *testing.T) {
	legacyYAML := []byte(`id: legacy-comp
name: Legacy Comp
match_duration: 5
`)

	var c Competition
	err := yaml.Unmarshal(legacyYAML, &c)
	require.NoError(t, err)
	require.Equal(t, 0, c.PoolMatchDurationSeconds, "seconds are unset before migration")

	ApplyCompetitionDefaults(&c)

	assert.Equal(t, 300, c.PoolMatchDurationSeconds, "5 min must migrate to 300s for the pool phase")
	assert.Equal(t, 300, c.PlayoffMatchDurationSeconds, "5 min must migrate to 300s for the playoff phase")
	// The retired keys are cleared so omitempty drops them on the next save and
	// the file converges on the seconds-only schema.
	assert.Zero(t, c.MatchDuration)
	assert.Zero(t, c.PoolMatchDuration)
	assert.Zero(t, c.PlayoffMatchDuration)
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

	// Sanity-check the on-disk key naming; the YAML wire format must
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
// playoff phase. League returns false; playoffs and mixed return true.
func TestLeagueFormatHidesPlayoffs(t *testing.T) {
	cases := []struct {
		format string
		want   bool
	}{
		{format: "league", want: false},
		{format: "playoffs", want: true},
		{format: "mixed", want: true},
	}

	for _, tc := range cases {
		t.Run(tc.format, func(t *testing.T) {
			c := Competition{Format: tc.format}
			assert.Equalf(t, tc.want, c.IsPlayoffEnabled(), "format=%q", tc.format)
		})
	}
}

// EffectivePoolWinners() returns the configured PoolWinners, defaulting to 2 when
// unset (<=0). Single source of truth for the knockout qualifier count.
func TestEffectivePoolWinners(t *testing.T) {
	cases := []struct {
		name        string
		poolWinners int
		want        int
	}{
		{name: "unset defaults to 2", poolWinners: 0, want: 2},
		{name: "negative defaults to 2", poolWinners: -1, want: 2},
		{name: "explicit 1", poolWinners: 1, want: 1},
		{name: "explicit 2", poolWinners: 2, want: 2},
		{name: "explicit 4", poolWinners: 4, want: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Competition{PoolWinners: tc.poolWinners}
			assert.Equal(t, tc.want, c.EffectivePoolWinners())
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

	// Mutate the copy's slice; original must be unaffected.
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
	assert.Equal(t, 300, c.PoolMatchDurationSeconds, "MatchDuration should migrate to pool seconds")
	assert.Equal(t, 300, c.PlayoffMatchDurationSeconds, "MatchDuration should migrate to playoff seconds")
}

// TestApplyCompetitionDefaults_PerPhaseWinsOverSingleField verifies that the
// per-phase retired field takes precedence over the single-field one when both
// are present in an old config.md.
func TestApplyCompetitionDefaults_PerPhaseWinsOverSingleField(t *testing.T) {
	c := &Competition{PoolMatchDuration: 4, PlayoffMatchDuration: 6, MatchDuration: 5}
	ApplyCompetitionDefaults(c)
	assert.Equal(t, 240, c.PoolMatchDurationSeconds)
	assert.Equal(t, 360, c.PlayoffMatchDurationSeconds)
}

// TestApplyCompetitionDefaults_ClampsIntoBand pins the clamp that makes every
// other layer able to assume a stored duration is in range. A legacy value
// outside the band is pinned to the nearest bound rather than dropped (silent
// reset) or kept (a value the UI can display but not re-submit).
func TestApplyCompetitionDefaults_ClampsIntoBand(t *testing.T) {
	// A realistic legacy value now sits INSIDE the band and survives intact.
	realistic := &Competition{MatchDuration: 15} // 900s, well under the 3600s ceiling
	ApplyCompetitionDefaults(realistic)
	assert.Equal(t, 900, realistic.PoolMatchDurationSeconds, "15 minutes is in band and must not be clamped")

	// Only an absurd stored value is pinned.
	tooLong := &Competition{MatchDuration: 90} // 5400s, above the 3600s ceiling
	ApplyCompetitionDefaults(tooLong)
	assert.Equal(t, MaxMatchDurationSeconds, tooLong.PoolMatchDurationSeconds)
	assert.Equal(t, MaxMatchDurationSeconds, tooLong.PlayoffMatchDurationSeconds)

	// The floor is 60s and the retired fields are whole MINUTES, so the smallest
	// value they can express already sits exactly on it. Only a direct sub-band
	// seconds value can land under.
	assert.Equal(t, MinMatchDurationSeconds, ClampMatchSeconds(3))
	assert.Equal(t, MaxMatchDurationSeconds, ClampMatchSeconds(99999))
	assert.Equal(t, 150, ClampMatchSeconds(150), "an in-band value is untouched")
	assert.Equal(t, 0, ClampMatchSeconds(0), "unset passes through as unset")
}

// TestApplyCompetitionDefaults_Idempotent guards the fact that it runs on every
// read: a second pass must not re-migrate or disturb an already-canonical value.
func TestApplyCompetitionDefaults_Idempotent(t *testing.T) {
	c := &Competition{MatchDuration: 4}
	ApplyCompetitionDefaults(c)
	first := *c
	ApplyCompetitionDefaults(c)
	assert.Equal(t, first.PoolMatchDurationSeconds, c.PoolMatchDurationSeconds)
	assert.Equal(t, first.PlayoffMatchDurationSeconds, c.PlayoffMatchDurationSeconds)
}

// mp-m5kf: sub-minute (seconds) per-match durations.

// TestApplyCompetitionDefaults_SecondsBackfillFromMinutes verifies that a
// competition carrying only the legacy whole-minute fields gets its canonical
// *Seconds fields back-filled (minutes*60) so the scheduler and UI read a
// single source of truth.
func TestApplyCompetitionDefaults_SecondsBackfillFromMinutes(t *testing.T) {
	c := &Competition{PoolMatchDuration: 3, PlayoffMatchDuration: 5}
	ApplyCompetitionDefaults(c)
	assert.Equal(t, 180, c.PoolMatchDurationSeconds, "3 min should migrate to 180s")
	assert.Equal(t, 300, c.PlayoffMatchDurationSeconds, "5 min should migrate to 300s")
}

// TestApplyCompetitionDefaults_SecondsWinOverMinutes verifies that an explicit
// sub-minute seconds value (e.g. 150s = 2m30s) is never clobbered by the
// whole-minute back-fill, even when a stale minute field is also present.
func TestApplyCompetitionDefaults_SecondsWinOverMinutes(t *testing.T) {
	c := &Competition{PoolMatchDuration: 3, PoolMatchDurationSeconds: 150}
	ApplyCompetitionDefaults(c)
	assert.Equal(t, 150, c.PoolMatchDurationSeconds, "explicit 2m30s must survive migration")
	assert.Zero(t, c.PoolMatchDuration, "the retired field is cleared either way")
}

// TestCompetitionSecondsRoundTrip verifies the *Seconds fields persist through
// YAML with their snake_case tags.
func TestCompetitionSecondsRoundTrip(t *testing.T) {
	original := Competition{
		ID:                          "sec-comp",
		Name:                        "Seconds",
		PoolMatchDurationSeconds:    150,
		PlayoffMatchDurationSeconds: 210,
	}
	data, err := yaml.Marshal(&original)
	require.NoError(t, err)
	assert.Contains(t, string(data), "pool_match_duration_seconds: 150")
	assert.Contains(t, string(data), "playoff_match_duration_seconds: 210")

	var loaded Competition
	require.NoError(t, yaml.Unmarshal(data, &loaded))
	assert.Equal(t, 150, loaded.PoolMatchDurationSeconds)
	assert.Equal(t, 210, loaded.PlayoffMatchDurationSeconds)
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

	// First save; file doesn't exist yet, must report changed.
	changed1, err := store.SaveCompetitionChanged(comp)
	require.NoError(t, err)
	assert.True(t, changed1, "first save must report changed=true")

	// Second save with identical struct; bytes.Equal path, must report false.
	changed2, err := store.SaveCompetitionChanged(comp)
	require.NoError(t, err)
	assert.False(t, changed2, "second identical save must report changed=false")
}

// TestNaginataFieldPersists verifies that the Naginata bool field
// round-trips correctly through YAML front-matter. When naginata: true
// is present the field is set; when absent (kendo competitions) it stays
// false.
func TestNaginataFieldPersists(t *testing.T) {
	t.Run("naginata true round-trips", func(t *testing.T) {
		original := Competition{
			ID:       "naginata-comp",
			Name:     "Naginata Test",
			Naginata: true,
		}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		assert.Contains(t, string(data), "naginata: true", "YAML must contain naginata: true")

		var loaded Competition
		require.NoError(t, parseFrontMatter(data, &loaded))
		assert.True(t, loaded.Naginata, "Naginata should round-trip to true")
	})

	t.Run("naginata absent defaults to false", func(t *testing.T) {
		yamlText := []byte("---\nid: kendo-comp\nname: Kendo Comp\n---\n")
		var c Competition
		require.NoError(t, parseFrontMatter(yamlText, &c))
		assert.False(t, c.Naginata, "Naginata should default to false when absent from YAML")
	})

	t.Run("naginata false omitted from YAML", func(t *testing.T) {
		original := Competition{
			ID:       "kendo-comp",
			Name:     "Kendo Comp",
			Naginata: false,
		}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "naginata", "omitempty: naginata=false must not appear in YAML")
	})
}

// TestNaginataJSONAlwaysPresent verifies that the Naginata field uses
// json:"naginata" WITHOUT omitempty so false is always serialised in
// JSON API responses. This is intentionally asymmetric with the YAML tag
// (which keeps omitempty so Kendo config.md files stay clean). The JSON
// no-omitempty prevents stale client state: the admin UI merges PUT
// responses via { ...c, ...updated }, so if the server omits naginata
// when false, toggling back to Kendo leaves a stale naginata:true in the
// client until a full page reload.
func TestNaginataJSONAlwaysPresent(t *testing.T) {
	t.Run("naginata false serialises to json:false (not omitted)", func(t *testing.T) {
		c := Competition{ID: "kendo", Name: "Kendo Comp", Naginata: false}
		data, err := json.Marshal(&c)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"naginata":false`, "json tag must NOT have omitempty: false must appear explicitly")
	})

	t.Run("naginata true serialises to json:true", func(t *testing.T) {
		c := Competition{ID: "naginata", Name: "Naginata Comp", Naginata: true}
		data, err := json.Marshal(&c)
		require.NoError(t, err)
		assert.Contains(t, string(data), `"naginata":true`)
	})

	t.Run("yaml false still omitted (omitempty on YAML tag)", func(t *testing.T) {
		c := Competition{ID: "kendo", Name: "Kendo Comp", Naginata: false}
		data, err := writeFrontMatter(&c)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "naginata", "YAML tag keeps omitempty: false must not appear in config.md")
	})
}

// TestFightingSpiritAwardsRoundTrip verifies that FightingSpiritAwards
// round-trip through YAML correctly: N awards survive, absent field loads
// as nil, and an empty slice omits the key from YAML output.
func TestFightingSpiritAwardsRoundTrip(t *testing.T) {
	t.Run("N awards round-trip through YAML front-matter", func(t *testing.T) {
		original := Competition{
			ID:   "fs-comp",
			Name: "FS Awards",
			FightingSpiritAwards: []FightingSpiritAward{
				{Title: "Fighting Spirit", RecipientName: "Alice Yamada", RecipientDojo: "Shinjuku"},
				{Title: "Best Technique", RecipientName: "Bob Tanaka"},
			},
		}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)

		var loaded Competition
		require.NoError(t, parseFrontMatter(data, &loaded))

		require.Len(t, loaded.FightingSpiritAwards, 2, "both awards must survive the round-trip")
		assert.Equal(t, "Fighting Spirit", loaded.FightingSpiritAwards[0].Title)
		assert.Equal(t, "Alice Yamada", loaded.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Shinjuku", loaded.FightingSpiritAwards[0].RecipientDojo)
		assert.Equal(t, "Best Technique", loaded.FightingSpiritAwards[1].Title)
		assert.Equal(t, "Bob Tanaka", loaded.FightingSpiritAwards[1].RecipientName)
		assert.Equal(t, "", loaded.FightingSpiritAwards[1].RecipientDojo, "absent dojo must be empty string")
	})

	t.Run("absent field loads as nil (legacy config)", func(t *testing.T) {
		legacyYAML := []byte("---\nid: legacy\nname: Legacy Comp\n---\n")
		var c Competition
		require.NoError(t, parseFrontMatter(legacyYAML, &c))
		assert.Nil(t, c.FightingSpiritAwards, "absent field must load as nil")
	})

	t.Run("empty slice omits the key from YAML output", func(t *testing.T) {
		c := Competition{
			ID:                   "empty-fs",
			Name:                 "No Awards",
			FightingSpiritAwards: []FightingSpiritAward{},
		}
		data, err := writeFrontMatter(&c)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "fighting_spirit_awards", "omitempty: empty slice must not appear in YAML")
	})

	t.Run("dojo optional: omitempty omits it from YAML when empty", func(t *testing.T) {
		original := Competition{
			ID:   "no-dojo-comp",
			Name: "No Dojo",
			FightingSpiritAwards: []FightingSpiritAward{
				{Title: "Spirit", RecipientName: "Carol Ito"},
			},
		}
		data, err := writeFrontMatter(&original)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "recipient_dojo", "empty dojo must not appear in YAML")
	})

	t.Run("round-trips via store SaveCompetition + LoadCompetition", func(t *testing.T) {
		store, err := NewStore(t.TempDir())
		require.NoError(t, err)

		comp := &Competition{
			ID:     "store-fs-comp",
			Name:   "Store FS Test",
			Status: CompStatusComplete,
			FightingSpiritAwards: []FightingSpiritAward{
				{Title: "Fighting Spirit", RecipientName: "Dan Watanabe", RecipientDojo: "Osaka"},
			},
		}
		require.NoError(t, store.SaveCompetition(comp))

		loaded, err := store.LoadCompetition("store-fs-comp")
		require.NoError(t, err)
		require.Len(t, loaded.FightingSpiritAwards, 1)
		assert.Equal(t, "Dan Watanabe", loaded.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Osaka", loaded.FightingSpiritAwards[0].RecipientDojo)
	})
}

// TestNormalizeStoredDurations_ClampsAlreadyPopulatedValue is the regression
// guard for the tri-review finding that ApplyCompetitionDefaults only clamped a
// value it had just derived from the retired whole-minute fields, leaving an
// already-populated out-of-band seconds value untouched.
//
// That mattered because the HTTP validator is a flat band check that trusts
// "no stored duration is out of band". A config.md hand-edited to
// pool_match_duration_seconds: 99999 would load as-is and then fail every
// subsequent settings save with 400, making the competition uneditable.
func TestNormalizeStoredDurations_ClampsAlreadyPopulatedValue(t *testing.T) {
	t.Run("above the ceiling", func(t *testing.T) {
		c := &Competition{PoolMatchDurationSeconds: 99999, PlayoffMatchDurationSeconds: 99999}
		normalizeStoredDurations(c)
		assert.Equal(t, MaxMatchDurationSeconds, c.PoolMatchDurationSeconds)
		assert.Equal(t, MaxMatchDurationSeconds, c.PlayoffMatchDurationSeconds)
	})

	t.Run("below the floor", func(t *testing.T) {
		c := &Competition{PoolMatchDurationSeconds: 3, PlayoffMatchDurationSeconds: 59}
		normalizeStoredDurations(c)
		assert.Equal(t, MinMatchDurationSeconds, c.PoolMatchDurationSeconds)
		assert.Equal(t, MinMatchDurationSeconds, c.PlayoffMatchDurationSeconds)
	})

	t.Run("an in-band value and an unset value are untouched", func(t *testing.T) {
		c := &Competition{PoolMatchDurationSeconds: 150}
		normalizeStoredDurations(c)
		assert.Equal(t, 150, c.PoolMatchDurationSeconds)
		assert.Zero(t, c.PlayoffMatchDurationSeconds, "0 stays 0: unset means use the default")
	})

	t.Run("nil is safe", func(t *testing.T) {
		assert.NotPanics(t, func() { normalizeStoredDurations(nil) })
	})

	// The plain migration entry point deliberately does NOT clamp a
	// pre-populated value: handlers call it on an inbound request body, and POST
	// does so before validation, so clamping there would silently rewrite a
	// value the API must reject with 400.
	t.Run("ApplyCompetitionDefaults leaves a pre-populated value alone", func(t *testing.T) {
		c := &Competition{PoolMatchDurationSeconds: 99999}
		ApplyCompetitionDefaults(c)
		assert.Equal(t, 99999, c.PoolMatchDurationSeconds,
			"clamping here would let an out-of-band POST body pass validation")
	})
}

// TestStore_HandEditedOutOfBandDurationIsPinnedOnLoad proves the invariant end
// to end through the real store, not just the helper: a config.md carrying an
// absurd seconds value is pinned when it is read.
func TestStore_HandEditedOutOfBandDurationIsPinnedOnLoad(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", "hand-edited"), 0o700))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "hand-edited", "config.md"),
		[]byte("---\nid: hand-edited\nname: Hand Edited\npool_match_duration_seconds: 99999\n---\n"),
		0o600))

	got, err := store.LoadCompetition("hand-edited")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, MaxMatchDurationSeconds, got.PoolMatchDurationSeconds,
		"a hand-edited out-of-band value must be pinned on load, or every later save 400s")
}

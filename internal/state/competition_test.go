package state

import (
	"encoding/json"
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

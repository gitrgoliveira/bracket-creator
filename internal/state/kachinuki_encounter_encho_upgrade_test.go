package state

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-kheb (operator ruling 2026-09-24): a kachinuki encounter carries no
// match-level overtime; (E) lives on the bout's own row. An older release
// mirrored a bout's encho into the encounter's Encho, so the load repair
// clears it on every pool and bracket match (bronze included), once, keyed
// on the competition's persisted marker.

func encounterEncho() *EnchoMetadata { return &EnchoMetadata{PeriodCount: 1} }

func enchoBout() []SubMatchResult {
	return []SubMatchResult{{
		Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1",
		Encho: &EnchoMetadata{PeriodCount: 1},
	}}
}

// seedEncounterEncho writes, through an older release's shape (no marker),
// a pool match and a bracket (one round plus the bronze) each carrying a
// match-level Encho, then returns the data folder.
func seedEncounterEncho(t *testing.T, comp *Competition) string {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(comp))
	require.NoError(t, s.SavePoolMatches(comp.ID, []MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideB: "Tora", Status: MatchStatusCompleted,
		Winner: "Ryu", Decision: "kachinuki-exhaustion", Encho: encounterEncho(), SubResults: enchoBout(),
	}}))
	require.NoError(t, s.SaveBracket(comp.ID, &Bracket{
		Rounds: [][]BracketMatch{{{
			ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Status: MatchStatusCompleted,
			Winner: "Ryu", Decision: "kachinuki-exhaustion", Encho: encounterEncho(), SubResults: enchoBout(),
		}}},
		ThirdPlaceMatch: &BracketMatch{
			ID: "m-3rd", SideA: "Kuma", SideB: "Saru", Status: MatchStatusCompleted,
			Winner: "Kuma", Decision: "kachinuki-exhaustion", Encho: encounterEncho(), SubResults: enchoBout(),
		},
	}))
	return dir
}

func kachinukiComp(id string) *Competition {
	return &Competition{ID: id, Name: "K", TeamSize: 3, TeamMatchType: TeamMatchTypeKachinuki}
}

func TestLegacyKachinukiEncounterEnchoIsCleared(t *testing.T) {
	dir := seedEncounterEncho(t, kachinukiComp("k"))

	fresh, err := NewStore(dir) // the startup sweep runs EnsureLegacyUpgraded
	require.NoError(t, err)

	pool, err := fresh.LoadPoolMatches("k")
	require.NoError(t, err)
	require.Len(t, pool, 1)
	assert.Nil(t, pool[0].Encho, "the pool encounter's (E) is cleared")
	require.Len(t, pool[0].SubResults, 1)
	assert.Equal(t, encounterEncho(), pool[0].SubResults[0].Encho, "the bout keeps its own overtime")

	b, err := fresh.LoadBracket("k")
	require.NoError(t, err)
	require.NotNil(t, b)
	assert.Nil(t, b.Rounds[0][0].Encho, "the knockout encounter's (E) is cleared")
	assert.Equal(t, encounterEncho(), b.Rounds[0][0].SubResults[0].Encho)
	require.NotNil(t, b.ThirdPlaceMatch)
	assert.Nil(t, b.ThirdPlaceMatch.Encho, "the bronze encounter's (E) is cleared")
	assert.Equal(t, encounterEncho(), b.ThirdPlaceMatch.SubResults[0].Encho)

	assert.NotZero(t, fresh.FileVersion("k", "pool-matches.csv"), "saved through the version-bumping writer")
	assert.NotZero(t, fresh.FileVersion("k", "bracket.json"), "saved through the version-bumping writer")

	comp, err := fresh.LoadCompetition("k")
	require.NoError(t, err)
	assert.True(t, comp.KachinukiEncounterEnchoCleared, "the marker is recorded once both files are saved")
	raw, err := os.ReadFile(fresh.compPath("k", "config.md")) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(raw), "kachinuki_encounter_encho_cleared: true", "and persisted")
}

// The repair is keyed on the marker, not on the data's shape: a competition
// that already carries it is left alone.
func TestLegacyKachinukiEncounterEnchoMarkerStopsTheRepair(t *testing.T) {
	comp := kachinukiComp("k")
	comp.KachinukiEncounterEnchoCleared = true
	dir := seedEncounterEncho(t, comp)

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	pool, err := fresh.LoadPoolMatches("k")
	require.NoError(t, err)
	require.Len(t, pool, 1)
	assert.Equal(t, encounterEncho(), pool[0].Encho, "a marked competition is not repaired again")
}

// A match file the repair cannot read is not "nothing to repair": the marker
// stays unset so a later start, once the file reads, still clears it.
func TestLegacyKachinukiEncounterEnchoUnreadableFileLeavesTheMarkerUnset(t *testing.T) {
	dir := seedEncounterEncho(t, kachinukiComp("k"))
	seed, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(seed.compPath("k", "bracket.json"), []byte("{not json"), 0o600))
	raw, err := os.ReadFile(seed.compPath("k", "config.md")) // #nosec G304
	require.NoError(t, err)
	// The seeding store's own startup sweep already ran the repair: put the
	// older release's marker-less config back.
	require.NoError(t, os.WriteFile(seed.compPath("k", "config.md"),
		[]byte(strings.ReplaceAll(string(raw), "kachinuki_encounter_encho_cleared: true\n", "")), 0o600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	raw, err = os.ReadFile(fresh.compPath("k", "config.md")) // #nosec G304
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "kachinuki_encounter_encho_cleared", "an unreadable bracket leaves the marker unset")
}

// Scoping guard: an individual competition and a fixed-order team
// competition keep their match-level overtime, bytes untouched, and get no
// marker.
func TestLegacyKachinukiEncounterEnchoLeavesOtherCompetitions(t *testing.T) {
	for name, comp := range map[string]*Competition{
		"individual":  {ID: "i", Name: "I"},
		"fixed-order": {ID: "f", Name: "F", TeamSize: 3, TeamMatchType: TeamMatchTypeFixed},
	} {
		t.Run(name, func(t *testing.T) {
			dir := seedEncounterEncho(t, comp)
			s, err := NewStore(dir)
			require.NoError(t, err)
			poolPath := s.compPath(comp.ID, "pool-matches.csv")
			bracketPath := s.compPath(comp.ID, "bracket.json")
			poolBefore, err := os.ReadFile(poolPath) // #nosec G304
			require.NoError(t, err)
			bracketBefore, err := os.ReadFile(bracketPath) // #nosec G304
			require.NoError(t, err)

			fresh, err := NewStore(dir)
			require.NoError(t, err)
			pool, err := fresh.LoadPoolMatches(comp.ID)
			require.NoError(t, err)
			require.Len(t, pool, 1)
			assert.Equal(t, encounterEncho(), pool[0].Encho)
			b, err := fresh.LoadBracket(comp.ID)
			require.NoError(t, err)
			assert.Equal(t, encounterEncho(), b.Rounds[0][0].Encho)
			assert.Equal(t, encounterEncho(), b.ThirdPlaceMatch.Encho)

			poolAfter, err := os.ReadFile(poolPath) // #nosec G304
			require.NoError(t, err)
			bracketAfter, err := os.ReadFile(bracketPath) // #nosec G304
			require.NoError(t, err)
			assert.Equal(t, poolBefore, poolAfter)
			assert.Equal(t, bracketBefore, bracketAfter)
			loaded, err := fresh.LoadCompetition(comp.ID)
			require.NoError(t, err)
			assert.False(t, loaded.KachinukiEncounterEnchoCleared)
		})
	}
}

// A pool representative bout or tie-break bout is ONE individual bout, whose
// overtime lives at match level because the match is the bout (owner review,
// GH-T1). The repair clears an encounter's (E), never one of these: a played
// "Pool A-DH-1" in encho keeps it, in a kachinuki competition too, while the
// encounter beside it is still cleared.
func TestLegacyKachinukiEncounterEnchoKeepsAPoolRepBoutsOwn(t *testing.T) {
	comp := kachinukiComp("k")
	dir := seedEncounterEncho(t, comp)
	s, err := NewStore(dir)
	require.NoError(t, err)
	pool, err := s.LoadPoolMatches("k")
	require.NoError(t, err)
	// Seeded again through the older shape (no marker): the store above ran
	// the sweep already, so the marker is put back to unset.
	comp.KachinukiEncounterEnchoCleared = false
	require.NoError(t, s.SaveCompetition(comp))
	pool[0].Encho = encounterEncho()
	pool = append(pool,
		MatchResult{ID: "Pool A-DH-1", SideA: "R-1", SideB: "W-1", Status: MatchStatusCompleted, Winner: "R-1", IpponsA: []string{"M"}, Encho: encounterEncho()},
		MatchResult{ID: "Pool A-TB-1", SideA: "R-2", SideB: "W-2", Status: MatchStatusCompleted, Winner: "W-2", IpponsB: []string{"K"}, Encho: encounterEncho()},
	)
	require.NoError(t, s.SavePoolMatches("k", pool))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	got, err := fresh.LoadPoolMatches("k")
	require.NoError(t, err)
	byID := map[string]MatchResult{}
	for _, m := range got {
		byID[m.ID] = m
	}
	assert.Nil(t, byID["Pool A-0"].Encho, "the encounter's (E) is still cleared")
	assert.Equal(t, encounterEncho(), byID["Pool A-DH-1"].Encho, "the representative bout keeps its overtime")
	assert.Equal(t, encounterEncho(), byID["Pool A-TB-1"].Encho, "and so does the tie-break bout")
}

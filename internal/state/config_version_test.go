package state

import (
	"regexp"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigVersion_Nil(t *testing.T) {
	assert.Equal(t, "", ConfigVersion(nil))
}

func TestConfigVersion_HexLength(t *testing.T) {
	twoThirds := true
	comp := &Competition{
		ID:                           "comp-1",
		Name:                         "Individual Championship",
		Date:                         "12-05-2026",
		StartTime:                    "09:30",
		PoolSize:                     4,
		PoolWinners:                  2,
		PoolSizeMode:                 "max",
		ExtraQualifiers:              "larger-pools",
		Courts:                       []string{"A", "B"},
		RoundRobin:                   true,
		WithZekkenName:               true,
		TeamSize:                     0,
		NumberPrefix:                 "K",
		Format:                       CompFormatMixed,
		PoolFormat:                   PoolFormatFull,
		Kind:                         "individual",
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 240,
		SwissRounds:                  3,
		Naginata:                     false,
		Engi:                         false,
		TeamMatchType:                TeamMatchTypeFixed,
		LeagueTiebreakTopN:           3,
		TwoThirdPlaces:               &twoThirds,
		ExpectedParticipants:         16,
	}
	ver := ConfigVersion(comp)
	assert.Len(t, ver, 16)
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{16}$`), ver)
}

func TestConfigVersion_StableAcrossSaveLoad(t *testing.T) {
	twoThirds := true
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:                           "comp-stable",
		Name:                         "Stable Championship",
		Date:                         "15-06-2026",
		StartTime:                    "10:00",
		PoolSize:                     3,
		PoolWinners:                  1,
		PoolSizeMode:                 "exact",
		ExtraQualifiers:              "",
		Courts:                       []string{"A", "B"},
		RoundRobin:                   false,
		WithZekkenName:               true,
		TeamSize:                     0,
		NumberPrefix:                 "S",
		Format:                       CompFormatKnockout,
		PoolFormat:                   PoolFormatFull,
		Kind:                         "individual",
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 180,
		SwissRounds:                  0,
		Naginata:                     false,
		Engi:                         false,
		TeamMatchType:                TeamMatchTypeFixed,
		LeagueTiebreakTopN:           3,
		TwoThirdPlaces:               &twoThirds,
		ExpectedParticipants:         32,
	}

	initialVersion := ConfigVersion(comp)
	require.NotEmpty(t, initialVersion)

	err = store.SaveCompetition(comp)
	require.NoError(t, err)

	loaded, err := store.LoadCompetition("comp-stable")
	require.NoError(t, err)
	require.NotNil(t, loaded)

	loadedVersion := ConfigVersion(loaded)
	assert.Equal(t, initialVersion, loadedVersion)
}

func TestConfigVersion_ChangesWhenAllowlistedFieldChanges(t *testing.T) {
	twoThirdsTrue := true
	twoThirdsFalse := false

	base := func() *Competition {
		return &Competition{
			ID:                           "base-comp",
			Name:                         "Original Name",
			Date:                         "01-01-2026",
			StartTime:                    "09:00",
			PoolSize:                     3,
			PoolWinners:                  1,
			PoolSizeMode:                 "max",
			ExtraQualifiers:              "",
			Courts:                       []string{"A", "B"},
			RoundRobin:                   false,
			WithZekkenName:               false,
			TeamSize:                     0,
			NumberPrefix:                 "A",
			Format:                       CompFormatKnockout,
			PoolFormat:                   PoolFormatFull,
			Kind:                         "individual",
			PoolMatchDurationSeconds:     180,
			KnockoutMatchDurationSeconds: 240,
			SwissRounds:                  3,
			Naginata:                     false,
			Engi:                         false,
			TeamMatchType:                TeamMatchTypeFixed,
			LeagueTiebreakTopN:           3,
			TwoThirdPlaces:               &twoThirdsTrue,
			ExpectedParticipants:         12,
		}
	}

	baseVer := ConfigVersion(base())

	tests := []struct {
		name   string
		mutate func(c *Competition)
	}{
		{"Name", func(c *Competition) { c.Name = "New Name" }},
		{"Date", func(c *Competition) { c.Date = "02-01-2026" }},
		{"StartTime", func(c *Competition) { c.StartTime = "11:00" }},
		{"PoolSize", func(c *Competition) { c.PoolSize = 5 }},
		{"PoolWinners", func(c *Competition) { c.PoolWinners = 2 }},
		{"PoolSizeMode", func(c *Competition) { c.PoolSizeMode = "exact" }},
		{"ExtraQualifiers", func(c *Competition) { c.ExtraQualifiers = "larger-pools" }},
		{"Courts", func(c *Competition) { c.Courts = []string{"A", "B", "C", "D"} }},
		{"RoundRobin", func(c *Competition) { c.RoundRobin = true }},
		{"WithZekkenName", func(c *Competition) { c.WithZekkenName = true }},
		{"TeamSize", func(c *Competition) { c.TeamSize = 5 }},
		{"NumberPrefix", func(c *Competition) { c.NumberPrefix = "B" }},
		{"Format", func(c *Competition) { c.Format = CompFormatMixed }},
		{"PoolFormat", func(c *Competition) { c.PoolFormat = PoolFormatPartial }},
		{"Kind", func(c *Competition) { c.Kind = "team" }},
		{"PoolMatchDurationSeconds", func(c *Competition) { c.PoolMatchDurationSeconds = 300 }},
		{"KnockoutMatchDurationSeconds", func(c *Competition) { c.KnockoutMatchDurationSeconds = 300 }},
		{"SwissRounds", func(c *Competition) { c.SwissRounds = 5 }},
		{"Naginata", func(c *Competition) { c.Naginata = true }},
		{"Engi", func(c *Competition) { c.Engi = true }},
		{"TeamMatchType", func(c *Competition) { c.TeamMatchType = TeamMatchTypeKachinuki }},
		{"LeagueTiebreakTopN", func(c *Competition) { c.LeagueTiebreakTopN = 4 }},
		{"TwoThirdPlaces", func(c *Competition) { c.TwoThirdPlaces = &twoThirdsFalse }},
		{"ExpectedParticipants", func(c *Competition) { c.ExpectedParticipants = 24 }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			mutatedVer := ConfigVersion(c)
			assert.NotEqual(t, baseVer, mutatedVer, "version must change when %s changes", tc.name)
		})
	}
}

func TestConfigVersion_DoesNotChangeForRuntimeFields(t *testing.T) {
	twoThirds := true
	base := func() *Competition {
		return &Competition{
			ID:                           "runtime-comp",
			Name:                         "Runtime Competition",
			Date:                         "10-10-2026",
			StartTime:                    "09:00",
			PoolSize:                     3,
			PoolWinners:                  1,
			PoolSizeMode:                 "max",
			ExtraQualifiers:              "",
			Courts:                       []string{"A", "B"},
			RoundRobin:                   false,
			WithZekkenName:               false,
			TeamSize:                     0,
			NumberPrefix:                 "R",
			Format:                       CompFormatKnockout,
			PoolFormat:                   PoolFormatFull,
			Kind:                         "individual",
			PoolMatchDurationSeconds:     180,
			KnockoutMatchDurationSeconds: 180,
			SwissRounds:                  3,
			Naginata:                     false,
			Engi:                         false,
			TeamMatchType:                TeamMatchTypeFixed,
			LeagueTiebreakTopN:           3,
			TwoThirdPlaces:               &twoThirds,
			ExpectedParticipants:         10,
		}
	}

	baseVer := ConfigVersion(base())

	tests := []struct {
		name   string
		mutate func(c *Competition)
	}{
		{"Status", func(c *Competition) { c.Status = CompStatusPools }},
		{"HasParticipantIDs", func(c *Competition) { c.HasParticipantIDs = true }},
		{"SwissCurrentRound", func(c *Competition) { c.SwissCurrentRound = 2 }},
		{"CheckInEnabled", func(c *Competition) { c.CheckInEnabled = true }},
		{"FightingSpiritAwards", func(c *Competition) {
			c.FightingSpiritAwards = []FightingSpiritAward{{Title: "Award", RecipientName: "Fighter"}}
		}},
		{"LeagueTiebreakFinalized", func(c *Competition) { c.LeagueTiebreakFinalized = true }},
		{"KachinukiEncounterEnchoCleared", func(c *Competition) { c.KachinukiEncounterEnchoCleared = true }},
		{"RoundLineupsConverted", func(c *Competition) { c.RoundLineupsConverted = true }},
		{"RoundLineupsGiven", func(c *Competition) { c.RoundLineupsGiven = map[string][]string{"team-1": {"r0-m0"}} }},
		{"RoundLineupsLegacy", func(c *Competition) { c.RoundLineupsLegacy = []string{"team-1"} }},
		{"Players", func(c *Competition) {
			c.Players = []domain.Player{{ID: "p1", Name: "Alice", Dojo: "Wakaba"}}
		}},
		{"RosterVersion", func(c *Competition) { c.RosterVersion = "1234567890abcdef" }},
		{"ConfigVersion", func(c *Competition) { c.ConfigVersion = "fedcba0987654321" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := base()
			tc.mutate(c)
			assert.Equal(t, baseVer, ConfigVersion(c), "version must NOT change when runtime field %s changes", tc.name)
		})
	}
}

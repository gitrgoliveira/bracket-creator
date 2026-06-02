package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestApplyTournamentDefaults_ZeroValues(t *testing.T) {
	tour := &Tournament{}
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 1.5, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 10, tour.SlowestCourtBufferPct)
}

func TestApplyTournamentDefaults_NonZeroPreserved(t *testing.T) {
	tour := &Tournament{
		ClockToElapsedMultiplier: 2.0,
		SlowestCourtBufferPct:    20,
	}
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 2.0, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 20, tour.SlowestCourtBufferPct)
}

func TestApplyTournamentDefaults_Nil(t *testing.T) {
	// Must not panic
	ApplyTournamentDefaults(nil)
}

func TestApplyTournamentDefaults_Idempotent(t *testing.T) {
	tour := &Tournament{}
	ApplyTournamentDefaults(tour)
	ApplyTournamentDefaults(tour)
	assert.Equal(t, 1.5, tour.ClockToElapsedMultiplier)
	assert.Equal(t, 10, tour.SlowestCourtBufferPct)
}

func TestHanteiPtr(t *testing.T) {
	assert.Nil(t, HanteiPtr(false), "false should map to nil so omitempty omits the field")
	got := HanteiPtr(true)
	require.NotNil(t, got)
	assert.True(t, *got, "true should map to a non-nil pointer to true")
}

// TestMatchResult_HanteiOmitempty pins the wire contract: a MatchResult
// projected from a non-hantei BracketMatch using HanteiPtr must NOT emit
// the field. Regression for the bug where `&bm.DecidedByHantei` (always
// non-nil) leaked `"decidedByHantei": false` into every SSE payload and
// every HTTP response, defeating the omitempty contract.
func TestMatchResult_HanteiOmitempty(t *testing.T) {
	t.Run("non-hantei projection omits field", func(t *testing.T) {
		mr := &MatchResult{ID: "m1", DecidedByHantei: HanteiPtr(false)}
		b, err := json.Marshal(mr)
		require.NoError(t, err)
		assert.NotContains(t, string(b), "decidedByHantei", "wire payload must omit the field for non-hantei matches")
	})
	t.Run("hantei projection emits true", func(t *testing.T) {
		mr := &MatchResult{ID: "m1", DecidedByHantei: HanteiPtr(true)}
		b, err := json.Marshal(mr)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"decidedByHantei":true`)
	})
}

// TestSubMatchResult_HanteiRoundTrip pins the wire/storage contract for the
// per-bout hantei flag the viewer reads (mp-8sw). Unlike MatchResult, the
// SubMatchResult flag is a plain bool, so omitempty omits it when false and
// emits it when true — across both the JSON HTTP path and the YAML config.md
// persistence path.
func TestSubMatchResult_HanteiRoundTrip(t *testing.T) {
	t.Run("true survives JSON round-trip", func(t *testing.T) {
		sub := SubMatchResult{Position: -1, DecidedByHantei: true}
		b, err := json.Marshal(sub)
		require.NoError(t, err)
		assert.Contains(t, string(b), `"decidedByHantei":true`)
		var got SubMatchResult
		require.NoError(t, json.Unmarshal(b, &got))
		assert.True(t, got.DecidedByHantei)
	})
	t.Run("false is omitted from JSON", func(t *testing.T) {
		b, err := json.Marshal(SubMatchResult{Position: 1})
		require.NoError(t, err)
		assert.NotContains(t, string(b), "decidedByHantei")
	})
	t.Run("true survives YAML round-trip", func(t *testing.T) {
		sub := SubMatchResult{Position: -1, DecidedByHantei: true}
		b, err := yaml.Marshal(sub)
		require.NoError(t, err)
		assert.Contains(t, string(b), "decided_by_hantei: true")
		var got SubMatchResult
		require.NoError(t, yaml.Unmarshal(b, &got))
		assert.True(t, got.DecidedByHantei)
	})
}

// --- Tournament.Days() ---

func TestTournament_Days(t *testing.T) {
	tests := []struct {
		name         string
		date         string
		durationDays int
		want         []string
	}{
		{
			name:         "single day",
			date:         "05-06-2026",
			durationDays: 1,
			want:         []string{"05-06-2026"},
		},
		{
			name:         "three days",
			date:         "05-06-2026",
			durationDays: 3,
			want:         []string{"05-06-2026", "06-06-2026", "07-06-2026"},
		},
		{
			name:         "month boundary",
			date:         "30-06-2026",
			durationDays: 3,
			want:         []string{"30-06-2026", "01-07-2026", "02-07-2026"},
		},
		{
			name:         "year boundary",
			date:         "31-12-2025",
			durationDays: 2,
			want:         []string{"31-12-2025", "01-01-2026"},
		},
		{
			name:         "empty date returns nil",
			date:         "",
			durationDays: 3,
			want:         nil,
		},
		{
			name:         "unparseable date returns nil",
			date:         "not-a-date",
			durationDays: 1,
			want:         nil,
		},
		{
			name:         "durationDays zero returns nil",
			date:         "05-06-2026",
			durationDays: 0,
			want:         nil,
		},
		{
			name:         "durationDays negative returns nil",
			date:         "05-06-2026",
			durationDays: -1,
			want:         nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tour := &Tournament{Date: tc.date, DurationDays: tc.durationDays}
			got := tour.Days()
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestTournament_Days_NilReceiver(t *testing.T) {
	var tour *Tournament
	// Must not panic
	got := tour.Days()
	assert.Nil(t, got)
}

// --- ApplyTournamentDefaults DurationDays ---

func TestApplyTournamentDefaults_DurationDays(t *testing.T) {
	t.Run("zero defaults to 1", func(t *testing.T) {
		tour := &Tournament{}
		ApplyTournamentDefaults(tour)
		assert.Equal(t, 1, tour.DurationDays)
	})
	t.Run("non-zero preserved", func(t *testing.T) {
		tour := &Tournament{DurationDays: 5}
		ApplyTournamentDefaults(tour)
		assert.Equal(t, 5, tour.DurationDays)
	})
}

func TestValidateTeamMatchType(t *testing.T) {
	tests := []struct {
		name     string
		t        TeamMatchType
		teamSize int
		wantErr  bool
	}{
		{"empty is fixed default", "", 0, false},
		{"fixed explicit", TeamMatchTypeFixed, 0, false},
		{"kachinuki with size 2", TeamMatchTypeKachinuki, 2, false},
		{"kachinuki with size 5", TeamMatchTypeKachinuki, 5, false},
		{"kachinuki with size 1 errors", TeamMatchTypeKachinuki, 1, true},
		{"kachinuki with size 0 errors", TeamMatchTypeKachinuki, 0, true},
		{"unknown value errors", "bogus", 5, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateTeamMatchType(tc.t, tc.teamSize)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- Sponsors (mp-c38) ---

// TestTournament_SponsorsRoundTrip pins the YAML round-trip contract:
// populated sponsors must survive marshal/unmarshal with name, file, and
// link preserved. omitempty on Link must omit the key for sponsors with
// no link set.
func TestTournament_SponsorsRoundTrip(t *testing.T) {
	original := Tournament{
		Name: "Round-Trip Cup",
		Sponsors: []Sponsor{
			{Name: "Acme Corp", File: "8a3f9c12d7b6e041.png", Link: "https://acme.example"},
			{Name: "BetaCo", File: "1f2e3d4c5b6a7080.jpg"}, // no link
		},
	}
	b, err := yaml.Marshal(&original)
	require.NoError(t, err)
	// omitempty on Link → second sponsor must not emit a `link:` key.
	assert.Contains(t, string(b), "name: Acme Corp")
	assert.Contains(t, string(b), "link: https://acme.example")

	var got Tournament
	require.NoError(t, yaml.Unmarshal(b, &got))
	require.Len(t, got.Sponsors, 2)
	assert.Equal(t, "Acme Corp", got.Sponsors[0].Name)
	assert.Equal(t, "8a3f9c12d7b6e041.png", got.Sponsors[0].File)
	assert.Equal(t, "https://acme.example", got.Sponsors[0].Link)
	assert.Equal(t, "BetaCo", got.Sponsors[1].Name)
	assert.Empty(t, got.Sponsors[1].Link, "omitempty link must round-trip as empty")
}

// TestTournament_NoSponsorsKey_LegacyParse ensures legacy tournament.md
// files (predating mp-c38) deserialize with an empty/nil Sponsors slice
// rather than failing. Strict-mode unmarshal would break older configs.
func TestTournament_NoSponsorsKey_LegacyParse(t *testing.T) {
	legacy := []byte(`name: Legacy Cup
date: "01-06-2026"
courts: ["A", "B"]
`)
	var got Tournament
	require.NoError(t, yaml.Unmarshal(legacy, &got))
	assert.Empty(t, got.Sponsors, "missing sponsors key must deserialize as empty slice")
}

// TestTournament_EmptySponsors_OmitsKey pins the reverse direction: a
// tournament with no sponsors must NOT emit `sponsors: []` so existing
// files round-trip byte-for-equivalent when no sponsors are configured.
func TestTournament_EmptySponsors_OmitsKey(t *testing.T) {
	tour := Tournament{Name: "No-Sponsor Cup"}
	b, err := yaml.Marshal(&tour)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "sponsors:", "empty Sponsors must be omitted from YAML")
}

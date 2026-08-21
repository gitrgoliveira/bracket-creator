package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

func TestFormatScore(t *testing.T) {
	tests := []struct {
		name    string
		ippons  []string
		hansoku int
		want    string
	}{
		{"empty", nil, 0, ""},
		{"one ippon", []string{"M"}, 0, "M"},
		{"two ippons", []string{"M", "K"}, 0, "MK"},
		{"hansoku only", nil, 1, "(H1)"},
		{"ippons and hansoku", []string{"M", "K"}, 1, "MK (H1)"},
		{"legacy multi hansoku", []string{"M"}, 2, "M (H2)"},
		{"default-win maru", []string{"○", "○"}, 0, "○○"},
		{"naginata sune", []string{"S"}, 0, "S"},
		{"hansoku ippon is a letter, not a counter", []string{"H"}, 0, "H"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.FormatScore(tc.ippons, tc.hansoku))
		})
	}
}

func TestParseScore(t *testing.T) {
	tests := []struct {
		name        string
		in          string
		wantIppons  []string
		wantHansoku int
	}{
		{"empty", "", nil, 0},
		{"one ippon", "M", []string{"M"}, 0},
		{"two ippons", "MK", []string{"M", "K"}, 0},
		{"hansoku only", "(H1)", nil, 1},
		{"ippons and hansoku", "MK (H1)", []string{"M", "K"}, 1},
		{"legacy multi hansoku", "M (H2)", []string{"M"}, 2},
		{"multi-byte maru survives the rune split", "○○", []string{"○", "○"}, 0},
		{"surrounding whitespace", "  MK  ", []string{"M", "K"}, 0},
		{"interior spaces are not ippons", "M K", []string{"M", "K"}, 0},
		// Persisted data on a live path: an unreadable count degrades to 0
		// rather than failing the read and blanking the match.
		{"malformed hansoku count", "M (Hx)", []string{"M"}, 0},
		// Pinning the byte-lookahead rewrite: the mark sits between two bare
		// letters/points, so a wrong skip index would eat or duplicate a
		// neighbour.
		{"mark between two hansoku ippons", "HtH", []string{"Ht", "H"}, 0},
		{"mark sandwiched between points", "MHtM", []string{"M", "Ht", "M"}, 0},
		// Non-ASCII passthrough: a multi-byte maru next to the 'H' lookahead
		// must not desync the byte offsets the lookahead relies on.
		{"maru before a bare hansoku ippon", "○H", []string{"○", "H"}, 0},
		{"bare hansoku ippon before a maru", "H○", []string{"H", "○"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ippons, hansoku := domain.ParseScore(tc.in)
			assert.Equal(t, tc.wantIppons, ippons)
			assert.Equal(t, tc.wantHansoku, hansoku)
		})
	}
}

// The property that matters: the two are a genuine inverse pair. They used to
// live in different packages (engine and mobileapp) with only a comment
// claiming the relationship, so nothing caught a change to one alone.
func TestScoreCodecRoundTrip(t *testing.T) {
	cases := []struct {
		ippons  []string
		hansoku int
	}{
		{nil, 0},
		{[]string{"M"}, 0},
		{[]string{"M", "K"}, 0},
		{[]string{"M", "K"}, 1},
		{nil, 1},
		{[]string{"D", "T"}, 2},
		{[]string{"○", "○"}, 0},
		{[]string{"H"}, 1},
		{[]string{"S"}, 0},
		// The codec's one two-rune token: the hantei mark, alone, after a
		// point, and adjacent to a real "H" ippon in both orders (the
		// no-lone-"t" lookahead must not eat a neighbouring hansoku letter).
		{[]string{"Ht"}, 0},
		{[]string{"K", "Ht"}, 0},
		{[]string{"H", "Ht"}, 0},
		{[]string{"Ht", "H"}, 1},
		{[]string{"Ht", "H"}, 0},
		{[]string{"M", "Ht", "M"}, 0},
	}
	for _, tc := range cases {
		encoded := domain.FormatScore(tc.ippons, tc.hansoku)
		ippons, hansoku := domain.ParseScore(encoded)
		assert.Equal(t, tc.ippons, ippons, "ippons survive %q", encoded)
		assert.Equal(t, tc.hansoku, hansoku, "hansoku survives %q", encoded)
	}
}

// The one documented normalisation: an empty entry renders as nothing, so it
// cannot come back. It is not a scoring ippon either way (CountScoringIppons
// drops it), so the round trip is lossless in MEANING, not in slice length.
func TestScoreCodecDropsEmptyEntries(t *testing.T) {
	encoded := domain.FormatScore([]string{"M", ""}, 0)
	require.Equal(t, "M", encoded)
	ippons, _ := domain.ParseScore(encoded)
	assert.Equal(t, []string{"M"}, ippons)
	assert.Equal(t, domain.CountScoringIppons([]string{"M", ""}), domain.CountScoringIppons(ippons))
}

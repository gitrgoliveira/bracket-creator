package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// TestIsScoringIppon pins the one predicate CountScoringIppons and
// export.IpponsScore both draw through: an empty cell, the unfilled-slot
// placeholder, and the judges'-decision mark are all NOT points.
func TestIsScoringIppon(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{"a waza letter scores", "M", true},
		{"the default-win maru scores", "○", true},
		{"an empty cell does not score", "", false},
		{"the unfilled-slot placeholder does not score", domain.IpponPlaceholder, false},
		{"the hantei mark does not score", domain.HanteiMark, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.IsScoringIppon(tc.in))
		})
	}
}

// TestCountScoringIppons pins the drop rule: placeholders, empty entries, and
// the hantei mark are all excluded from the count, so a tied scoreline with
// an unfilled slot or a recorded verdict still reads as tied.
func TestCountScoringIppons(t *testing.T) {
	tests := []struct {
		name   string
		ippons []string
		want   int
	}{
		{"nil scores zero", nil, 0},
		{"two real ippons", []string{"M", "K"}, 2},
		{"placeholder is dropped", []string{"M", domain.IpponPlaceholder}, 1},
		{"hantei mark is dropped", []string{"M", domain.HanteiMark}, 1},
		{"empty entry is dropped", []string{"M", ""}, 1},
		{"mixed drops", []string{"M", domain.IpponPlaceholder, domain.HanteiMark, ""}, 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.CountScoringIppons(tc.ippons))
		})
	}
}

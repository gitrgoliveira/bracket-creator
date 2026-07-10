package export

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func encho(periods int) *state.EnchoMetadata {
	return &state.EnchoMetadata{PeriodCount: periods}
}

func TestDecisionSuffix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision string
		encho    *state.EnchoMetadata
		hantei   bool
		want     string
	}{
		// Base: no suffix
		{name: "empty decision, no encho, no hantei", decision: "", encho: nil, hantei: false, want: ""},
		{name: "fought, no extras", decision: "fought", encho: nil, hantei: false, want: ""},

		// Kiken variants
		{name: "kiken-voluntary", decision: "kiken-voluntary", encho: nil, hantei: false, want: "Kiken"},
		{name: "kiken-injury", decision: "kiken-injury", encho: nil, hantei: false, want: "Kiken"},
		{name: "kiken (legacy)", decision: "kiken", encho: nil, hantei: false, want: "Kiken"},

		// Fusenpai / fusensho
		{name: "fusenpai", decision: "fusenpai", encho: nil, hantei: false, want: "Fus."},
		{name: "fusensho", decision: "fusensho", encho: nil, hantei: false, want: "Fus."},

		// Daihyosen
		{name: "daihyosen", decision: "daihyosen", encho: nil, hantei: false, want: "DH"},

		// Encho only
		{name: "encho only (fought)", decision: "fought", encho: encho(1), hantei: false, want: "(E)"},
		{name: "encho nil vs zero periods", decision: "fought", encho: encho(0), hantei: false, want: ""},

		// Hantei only
		{name: "hantei only (fought)", decision: "fought", encho: nil, hantei: true, want: "Ht"},

		// Encho + hantei
		{name: "encho + hantei (fought)", decision: "fought", encho: encho(2), hantei: true, want: "(E) Ht"},

		// Base label + encho
		{name: "Kiken + encho", decision: "kiken-voluntary", encho: encho(1), hantei: false, want: "Kiken (E)"},
		{name: "Fus. + encho", decision: "fusenpai", encho: encho(1), hantei: false, want: "Fus. (E)"},
		{name: "DH + encho", decision: "daihyosen", encho: encho(1), hantei: false, want: "DH (E)"},

		// Base label + hantei
		{name: "Kiken + hantei", decision: "kiken-voluntary", encho: nil, hantei: true, want: "Kiken Ht"},
		{name: "DH + hantei", decision: "daihyosen", encho: nil, hantei: true, want: "DH Ht"},

		// Full composition: base + encho + hantei
		{name: "Kiken + encho + hantei", decision: "kiken-voluntary", encho: encho(1), hantei: true, want: "Kiken (E) Ht"},
		{name: "DH + encho + hantei", decision: "daihyosen", encho: encho(3), hantei: true, want: "DH (E) Ht"},

		// Hikiwake (draw) produces no base label; suffix still applies
		{name: "hikiwake + hantei", decision: "hikiwake", encho: nil, hantei: true, want: "Ht"},
		{name: "hikiwake + encho", decision: "hikiwake", encho: encho(1), hantei: false, want: "(E)"},
		{name: "hikiwake + encho + hantei", decision: "hikiwake", encho: encho(1), hantei: true, want: "(E) Ht"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := DecisionSuffix(tc.decision, tc.encho, tc.hantei)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestMiddleCellText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		decision string
		suffix   string
		want     string
	}{
		// Non-draw: just the suffix (may be empty).
		{name: "fought, no suffix", decision: "fought", suffix: "", want: ""},
		{name: "fought, kiken suffix", decision: "fought", suffix: "Kiken", want: "Kiken"},
		{name: "daihyosen suffix only", decision: "daihyosen", suffix: "DH", want: "DH"},

		// Draw alone -> bare "X".
		{name: "draw, no suffix", decision: "hikiwake", suffix: "", want: "X"},

		// The regression this helper fixes: draw AND suffix must be COMBINED,
		// not overwritten. Previously the code wrote "X" then replaced it with
		// the suffix, dropping the draw marker.
		{name: "draw + encho", decision: "hikiwake", suffix: "(E)", want: "X (E)"},
		{name: "draw + hantei", decision: "hikiwake", suffix: "Ht", want: "X Ht"},
		{name: "draw + encho + hantei", decision: "hikiwake", suffix: "(E) Ht", want: "X (E) Ht"},
		{name: "draw + DH", decision: "hikiwake", suffix: "DH", want: "X DH"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, MiddleCellText(tc.decision, tc.suffix))
		})
	}
}

func TestFlagsScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n    int
		want string
	}{
		{0, ""},
		{1, "1"},
		{3, "3"},
		{5, "5"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("flags_%d", tc.n), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, FlagsScore(tc.n))
		})
	}
}

func TestIpponsScore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ippons []string
		want   string
	}{
		{name: "nil slice", ippons: nil, want: ""},
		{name: "empty slice", ippons: []string{}, want: ""},
		{name: "single ippon", ippons: []string{"M"}, want: "M"},
		{name: "two ippons", ippons: []string{"M", "K"}, want: "MK"},
		{name: "skips dot placeholders", ippons: []string{"•", "M"}, want: "M"},
		{name: "skips empty strings", ippons: []string{"", "K"}, want: "K"},
		{name: "all placeholders", ippons: []string{"•", "•"}, want: ""},
		{name: "preserves order", ippons: []string{"D", "T", "H"}, want: "DTH"},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, IpponsScore(tc.ippons))
		})
	}
}

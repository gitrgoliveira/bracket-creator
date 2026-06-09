package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSuggestedMaxCourts(t *testing.T) {
	tests := []struct {
		players  int
		expected int
		why      string
	}{
		{3, 1, "floor(3/2)-1 = 0, clamped to 1"},
		{4, 1, "floor(4/2)-1 = 1"},
		{5, 1, "floor(5/2)-1 = 1"},
		{6, 2, "floor(6/2)-1 = 2"},
		{8, 3, "floor(8/2)-1 = 3"},
		{10, 4, "floor(10/2)-1 = 4"},
		{16, 7, "floor(16/2)-1 = 7"},
		{2, 1, "minimum clamp"},
	}

	for _, tc := range tests {
		t.Run(tc.why, func(t *testing.T) {
			got := SuggestedMaxCourts(tc.players)
			assert.Equalf(t, tc.expected, got, "players=%d: %s", tc.players, tc.why)
		})
	}
}

func TestValidateCourtCount(t *testing.T) {
	tests := []struct {
		players int
		courts  int
		wantErr bool
		desc    string
	}{
		// 8 players: hardCap=4
		{8, 3, false, "8p/3c: ok (below cap)"},
		{8, 4, false, "8p/4c: ok (== floor(8/2), warning is frontend-only)"},
		{8, 5, true, "8p/5c: error (> floor(8/2))"},

		// 6 players: hardCap=3
		{6, 2, false, "6p/2c: ok"},
		{6, 3, false, "6p/3c: ok (== floor(6/2))"},
		{6, 4, true, "6p/4c: error"},

		// 3 players: hardCap=1
		{3, 1, false, "3p/1c: ok"},
		{3, 2, true, "3p/2c: error"},

		// 4 players: hardCap=2
		{4, 1, false, "4p/1c: ok"},
		{4, 2, false, "4p/2c: ok (== floor(4/2))"},
		{4, 3, true, "4p/3c: error"},

		// 2 players: hardCap=1
		{2, 1, false, "2p/1c: ok"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := ValidateCourtCount(tc.players, tc.courts)
			if tc.wantErr {
				require.Errorf(t, err, "expected error for players=%d courts=%d", tc.players, tc.courts)
			} else {
				require.NoErrorf(t, err, "unexpected error for players=%d courts=%d", tc.players, tc.courts)
			}
		})
	}
}

package domain_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKachinukiTaishoPairingGolden is the Go half of the shared table for the
// kachinuki encho rule (bc-kten): only taisho against taisho may go to encho.
// The JS half, kachinuki_taisho_golden.test.jsx, loads the SAME file, so the
// editor that offers Encho and the server that refuses it cannot disagree
// about who a team's taisho is.
func TestKachinukiTaishoPairingGolden(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "kachinuki_taisho.json"))
	require.NoError(t, err)
	var table struct {
		Cases []struct {
			Name     string             `json:"name"`
			TeamSize int                `json:"teamSize"`
			LineupA  *domain.TeamLineup `json:"lineupA"`
			LineupB  *domain.TeamLineup `json:"lineupB"`
			A        struct {
				Name     string `json:"name"`
				MemberID string `json:"memberId"`
			} `json:"a"`
			B struct {
				Name     string `json:"name"`
				MemberID string `json:"memberId"`
			} `json:"b"`
			FoughtA []domain.BoutFighter `json:"foughtA"`
			FoughtB []domain.BoutFighter `json:"foughtB"`
			Taisho  bool                 `json:"taisho"`
			Known   bool                 `json:"known"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases, "the shared table parsed to zero cases: the mirror would assert nothing")

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			taisho, known := domain.KachinukiTaishoPairing(tc.TeamSize, tc.LineupA, tc.LineupB,
				domain.BoutFighter{Name: tc.A.Name, MemberID: tc.A.MemberID},
				domain.BoutFighter{Name: tc.B.Name, MemberID: tc.B.MemberID},
				tc.FoughtA, tc.FoughtB)
			assert.Equal(t, tc.Known, known, "known")
			assert.Equal(t, tc.Taisho, taisho, "taisho")
		})
	}
}

package state_test

// bracket_rounds_v110_test.go pins what the load-time restamp does to a bracket
// drawn before v2.0.0 (bc-tmfn). v1.1.0 already stored each bout's round as its
// distance from the final, so its rounds are left alone; but it numbered the
// bouts of one round by their position inside their own pow2 round, not by
// their first-round slot. Where a round draws bouts from two pow2 rounds (it
// takes byes) the two orders differ, the Excel export numbers by the slot, and
// the restamp renumbers the stored bracket to agree with it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v110NineEntrantFixture is a nine-entrant knockout on one shiaijo exactly as
// a v1.1.0 binary wrote it through its API (tournament, competition, nine
// participants Player01..Player09, generate-draw; nothing fought), bytes
// unchanged. v110NineEntrantRoster is the participants.csv it wrote alongside.
// In the third round from the final, P5 v P6 (m-r1-4, first-round slot 8)
// and P7 v the winner of P8 v P9 (m-r2-3, slot 12) share a round; v1.1.0
// numbered them 5 and 4 by their positions in their own pow2 rounds (4 and
// 3), the sheet numbers them 4 and 5 left to right.
const (
	v110NineEntrantFixture = "testdata/bracket_v1.1.0_knockout9.json"
	v110NineEntrantRoster  = "testdata/participants_v1.1.0_knockout9.csv"
)

func loadV110NineEntrant(t *testing.T) *state.Bracket {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(v110NineEntrantFixture))
	require.NoError(t, err)
	var b state.Bracket
	require.NoError(t, json.Unmarshal(raw, &b))
	require.False(t, b.TimesSettled, "fixture: written by v1.1.0, before the times were settled")
	return &b
}

// v110NineEntrantCorrected sets on b the rounds, numbers and times the
// restamp gives the v1.1.0 bracket. The rounds are v1.1.0's own. The numbers
// follow the sheet's left-to-right order. v1.1.0 also scheduled the court in
// storage order (its Match 1, P8 v P9, was the fourth bout on the court), and
// nothing has been fought, so the court's eight times are handed out again in
// match-number order.
func v110NineEntrantCorrected(t *testing.T, b *state.Bracket) {
	t.Helper()
	for id, want := range map[string]struct {
		round, number int
		at            string
	}{
		"m-r1-7": {4, 1, "09:00"}, // P8 v P9, the one bout of the deepest round
		"m-r1-0": {3, 2, "09:05"}, // P1 v P2
		"m-r1-1": {3, 3, "09:10"}, // P3 v P4
		"m-r1-4": {3, 4, "09:15"}, // P5 v P6: 5 in v1.1.0
		"m-r2-3": {3, 5, "09:20"}, // P7 v W(P8 v P9): 4 in v1.1.0
		"m-r2-0": {2, 6, "09:25"},
		"m-r3-1": {2, 7, "09:30"},
		"m-r4-0": {1, 8, "09:35"}, // the final keeps the last time
	} {
		setRoundNumberTime(t, b, id, want.round, want.number, want.at)
	}
	b.TimesSettled = true
}

func TestRestampRoundsFromFeeders_RenumbersTheV110NineEntrantBracket(t *testing.T) {
	got := loadV110NineEntrant(t)
	require.Equal(t, 5, matchByID(t, got, "m-r1-4").MatchNumber, "fixture must carry v1.1.0's numbering")
	require.Equal(t, 4, matchByID(t, got, "m-r2-3").MatchNumber, "fixture must carry v1.1.0's numbering")

	changes, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	for _, c := range changes {
		assert.Equalf(t, c.OldRound, c.NewRound, "%s: v1.1.0 already stored the distance from the final", c.ID)
	}

	want := loadV110NineEntrant(t)
	v110NineEntrantCorrected(t, want)
	assert.Equal(t, want, got)

	again, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Empty(t, again, "a second pass finds nothing to move")
}

// Through the load path: the v1.1.0 bracket predates side ids too, so its
// first load also stamps them from the roster. Both repairs land in ONE write
// of bracket.json (the file version moves once), and a later process writes
// nothing.
func TestLegacyBracketUpgrade_V110BracketTakesIdsAndNumbersInOneWrite(t *testing.T) {
	dir, _ := newLegacyUpgradeFixture(t)
	v110, err := os.ReadFile(filepath.FromSlash(v110NineEntrantFixture))
	require.NoError(t, err)
	roster, err := os.ReadFile(filepath.FromSlash(v110NineEntrantRoster))
	require.NoError(t, err)
	compDir := filepath.Join(dir, "competitions", "c1")
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "participants.csv"), roster, 0o600))
	path := filepath.Join(compDir, "bracket.json")
	require.NoError(t, os.WriteFile(path, v110, 0o600))

	first := freshLegacyUpgradeStore(t, dir)
	served, err := first.LoadBracket("c1")
	require.NoError(t, err)
	assert.Equal(t, uint64(1), first.FileVersion("c1", "bracket.json"),
		"the side ids and the numbers are saved together, in one write")

	for _, m := range []struct{ id, sideAID string }{
		{"m-r1-0", "61f84cfd-791c-455f-b57a-94fcaf820d87"}, // Player01, from the roster
		{"m-r1-7", "eb0e30d1-27f2-4dab-9701-305544e26b0c"}, // Player08
	} {
		assert.Equalf(t, m.sideAID, matchByID(t, served, m.id).SideAID, "%s: side ids stamped by the same load", m.id)
	}
	corrected := loadV110NineEntrant(t)
	v110NineEntrantCorrected(t, corrected)
	for ri := range corrected.Rounds {
		for mi, want := range corrected.Rounds[ri] {
			got := served.Rounds[ri][mi]
			assert.Equalf(t, [3]any{want.DisplayRound, want.MatchNumber, want.ScheduledAt},
				[3]any{got.DisplayRound, got.MatchNumber, got.ScheduledAt}, "%s", want.ID)
		}
	}

	upgraded, err := os.ReadFile(path)
	require.NoError(t, err)
	var onDisk state.Bracket
	require.NoError(t, json.Unmarshal(upgraded, &onDisk))
	assert.Equal(t, served, &onDisk, "the read serves exactly what was written")

	second := freshLegacyUpgradeStore(t, dir)
	_, err = second.LoadBracket("c1")
	require.NoError(t, err)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(upgraded), string(again), "an upgraded bracket must not be rewritten")
	assert.Zero(t, second.FileVersion("c1", "bracket.json"), "no write, so no version bump")
}

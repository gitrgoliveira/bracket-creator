package state_test

// legacy_upgrade_unreadable_files_test.go pins what each step of
// EnsureLegacyUpgraded gives up when a file it opens will not parse: only
// the work that genuinely needed that file, and never in silence.
//
// squads.yaml: both side-id repairs open it for ONE branch -- resolving a
// team bout log's member ids -- and used to return its error, abandoning
// the MATCH-level SideAID/SideBID/WinnerID stamping in the same breath.
// That stamping needs no squad at all, and since bc-pnum standings resolve
// BY ID ONLY, every row left unstamped contributes nothing to anyone's
// record.
//
// lineups.yaml: the member-id repair discarded its load error outright,
// alone among the six steps this pass logs, so a corrupt lineup file was
// the one repair failure nothing anywhere reported.

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeUnreadableSquads replaces c1's squads.yaml with bytes no parser can
// accept. A MISSING file is deliberately NOT this case: parseSquadsFile
// reads that as an empty map with no error, which is the ordinary "no
// squads yet" state every individual competition is in.
func writeUnreadableSquads(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "competitions", "c1", "squads.yaml")
	require.NoError(t, os.WriteFile(path, []byte("\tthis: [is not\n  valid: yaml\n"), 0o600))
	_, err := state.NewStore(dir)
	require.NoError(t, err)
}

// TestPoolMatchSideIDsRepairedDespiteUnreadableSquads: the match-level
// triple is stamped from the roster even though the squad file this pass
// also opens cannot be parsed.
func TestPoolMatchSideIDsRepairedDespiteUnreadableSquads(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	ids := legacyUpgradeTeams(t, s, "Rin Sato", "Yuki Tanaka")
	rinID, yukiID := ids[0], ids[1]

	// Legacy-shaped: both sides named, no ids anywhere on the row.
	require.NoError(t, s.SavePoolMatches("c1", []state.MatchResult{
		{ID: "P1-0", SideA: "Rin Sato", SideB: "Yuki Tanaka", Winner: "Rin Sato"},
	}))
	writeUnreadableSquads(t, dir)

	fresh := freshLegacyUpgradeStore(t, dir)
	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)

	assert.Equal(t, rinID, matches[0].SideAID, "side A is stamped from the roster, which needs no squad")
	assert.Equal(t, yukiID, matches[0].SideBID)
	assert.Equal(t, rinID, matches[0].WinnerID, "the winner follows the sides this pass just resolved")
}

// TestBracketSideIDsRepairedDespiteUnreadableSquads is the bracket.json
// twin: the same eager load sat in front of the same match-level repair, so
// the same file had to be pinned on both branches rather than one standing
// in for the other.
func TestBracketSideIDsRepairedDespiteUnreadableSquads(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	ids := legacyUpgradeTeams(t, s, "Rin Sato", "Yuki Tanaka")
	rinID, yukiID := ids[0], ids[1]

	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "R1-M1", SideA: "Rin Sato", SideB: "Yuki Tanaka", Winner: "Rin Sato", Status: state.MatchStatusCompleted},
		}},
	}))
	writeUnreadableSquads(t, dir)

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.Len(t, bracket.Rounds, 1)
	require.Len(t, bracket.Rounds[0], 1)

	m := bracket.Rounds[0][0]
	assert.Equal(t, rinID, m.SideAID, "side A is stamped from the roster, which needs no squad")
	assert.Equal(t, yukiID, m.SideBID)
	assert.Equal(t, rinID, m.WinnerID)
}

// captureStateLog swaps the default logger's sink for the duration of fn
// and returns everything logged during it, mirroring internal/engine's
// captureLog. Not parallel-safe (it mutates the package-level logger), which
// is why no test here calls t.Parallel.
func captureStateLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
	fn()
	return buf.String()
}

// TestUnreadableLineupsIsReported: a lineups.yaml that will not parse is
// named in the log rather than swallowed. A MISSING file stays silent --
// parseTeamLineupsFile reads that as an empty map, the ordinary state of
// every competition that has never set a lineup, and logging it would bury
// the real failure this test pins.
func TestUnreadableLineupsIsReported(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	legacyUpgradeTeams(t, s, "Tora", "Kaze")

	path := filepath.Join(dir, "competitions", "c1", "lineups.yaml")
	require.NoError(t, os.WriteFile(path, []byte("\tnot: [valid\n  yaml: here\n"), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	logged := captureStateLog(t, func() { fresh.EnsureLegacyUpgraded("c1") })
	assert.Contains(t, logged, "legacy lineup-member-id upgrade for c1",
		"a lineup file that will not parse must reach the log this pass already writes for its five siblings")
}

// TestMissingLineupsIsNotReported is the other half: silence is correct for
// the file simply not being there, so the assertion above cannot pass by
// logging on every competition.
func TestMissingLineupsIsNotReported(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	legacyUpgradeTeams(t, s, "Tora", "Kaze")

	fresh := freshLegacyUpgradeStore(t, dir)
	logged := captureStateLog(t, func() { fresh.EnsureLegacyUpgraded("c1") })
	assert.NotContains(t, logged, "legacy lineup-member-id upgrade",
		"no lineups.yaml is the ordinary state, not a repair failure")
}

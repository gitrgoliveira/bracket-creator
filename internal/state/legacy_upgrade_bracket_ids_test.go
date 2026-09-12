package state_test

// legacy_upgrade_bracket_ids_test.go pins the bc-brid load-time repair for
// bracket.json: a legacy (pre-bc-brid) bracket carries SideA/SideB/Winner
// names but no SideAID/SideBID/WinnerID. Two resolution paths, exactly as
// legacy_upgrade.go's header documents: DrawOrder positionally for a
// standalone knockout's round 0 (exact even for a duplicated name, since it
// never compares names at all), and the unique-bare-name fallback
// everywhere else (a mixed/pool-fed bracket's every round, and any round-0
// side DrawOrder didn't reach), with WinnerID derived from each row's own
// resolved sides. Both convert ON READ, under the per-comp write lock,
// exactly like the seeds.csv/pools.csv/pool-matches.csv upgrades this
// file's siblings pin -- see newLegacyUpgradeFixture and
// freshLegacyUpgradeStore in legacy_upgrade_pool_ids_test.go.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyBracketSideIDUpgrade_DrawOrderResolvesDuplicatedName pins the
// standalone-knockout resolution path: DrawOrder is positional (bc-brid), so
// it resolves round-0 sides EXACTLY even when two different competitors
// share the display name "Sam" (different dojos, legal per
// CheckDuplicateEntriesByNameDojo) -- the unique-bare-name fallback the
// mixed path uses could never do this (NameCount("Sam") == 2 would leave
// BOTH round-0 occurrences unresolved).
func TestLegacyBracketSideIDUpgrade_DrawOrderResolvesDuplicatedName(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	samSouthID := helper.NewUUID4()
	samNorthID := helper.NewUUID4()
	op1ID := helper.NewUUID4()
	op2ID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: samSouthID, Name: "Sam", Dojo: "South"},
		{ID: op1ID, Name: "Op1", Dojo: "O1"},
		{ID: samNorthID, Name: "Sam", Dojo: "North"},
		{ID: op2ID, Name: "Op2", Dojo: "O2"},
	}))

	// Legacy shape: DrawOrder is present (this is a standalone knockout) but
	// SideAID/SideBID/WinnerID are all empty -- the exact shape a
	// pre-bc-brid bracket.json carries (omitempty drops the keys entirely on
	// marshal, so this is byte-identical to a hand-written legacy file).
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		DrawOrder: []string{samSouthID, op1ID, samNorthID, op2ID},
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Sam", SideB: "Op1", Winner: "Sam", Status: state.MatchStatusCompleted},
				{ID: "m-r1-1", SideA: "Sam", SideB: "Op2", Winner: "Op2", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "m-r2-0", SideA: "Winner of r2-0", SideB: "Winner of r2-1", Status: state.MatchStatusScheduled},
			},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 2)

	m0 := bracket.Rounds[0][0]
	assert.Equal(t, samSouthID, m0.SideAID, "position 0 of DrawOrder is Sam-South, not Sam-North, despite the identical name")
	assert.Equal(t, op1ID, m0.SideBID)
	assert.Equal(t, samSouthID, m0.WinnerID, "WinnerID derived from this row's OWN resolved sides")

	m1 := bracket.Rounds[0][1]
	assert.Equal(t, samNorthID, m1.SideAID, "position 2 of DrawOrder is Sam-North, correctly distinguished from Sam-South above")
	assert.Equal(t, op2ID, m1.SideBID)
	assert.Equal(t, op2ID, m1.WinnerID)

	// The repair lands on disk, not just in the returned copy.
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "bracket.json"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), samSouthID)
	assert.Contains(t, string(raw), samNorthID)
}

// TestLegacyBracketSideIDUpgrade_UniqueNameFallback_BlankDojoRowStillRepairs
// pins the mixed (pool-fed) resolution path: no DrawOrder at all (a mixed
// bracket never carries it), so every side resolves via the unique-bare-name
// fallback -- the SAME rule the pools.csv/pool-matches.csv upgrades apply. A
// roster row whose OWN dojo is blank (tolerated on load, see
// legacy_upgrade.go's header) still repairs as long as the name is unique
// in the roster.
func TestLegacyBracketSideIDUpgrade_UniqueNameFallback_BlankDojoRowStillRepairs(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	// Written directly to disk (SaveParticipants refuses a blank dojo,
	// state.ErrBlankDojo -- loading stays tolerant on purpose so a legacy
	// roster like this can still be repaired, per CLAUDE.md's blank-dojo
	// note). Rin Sato's own dojo is blank; Yuki Tanaka's is not.
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	rawRoster := rinID + ",Rin Sato,\n" + yukiID + ",Yuki Tanaka,Tobukan\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(rawRoster), 0o600))

	// Mixed/pool-fed shape: no DrawOrder at all.
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Rin Sato", SideB: "Yuki Tanaka", Winner: "Rin Sato", Status: state.MatchStatusCompleted}},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 1)
	m := bracket.Rounds[0][0]
	assert.Equal(t, rinID, m.SideAID, "a unique name resolves even when the roster's own dojo is blank")
	assert.Equal(t, yukiID, m.SideBID)
	assert.Equal(t, rinID, m.WinnerID, "WinnerID derived from the row's own resolved sides")
}

// TestLegacyBracketSideIDUpgrade_AmbiguousNameLeftAlone is the refusing
// direction paired with the two resolving tests above: two roster entries
// share the exact name "Yuki Tanaka" (different dojos), so the
// unique-bare-name resolution cannot pick one. That side's id is left alone
// rather than guessed; the OTHER side (a unique name) still resolves, and
// WinnerID stays unresolved since it cannot be derived without both sides
// known.
func TestLegacyBracketSideIDUpgrade_AmbiguousNameLeftAlone(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	yuki1 := helper.NewUUID4()
	yuki2 := helper.NewUUID4()
	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: yuki1, Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{ID: yuki2, Name: "Yuki Tanaka", Dojo: "Tobukan"},
		{ID: rinID, Name: "Rin Sato", Dojo: "Kobukan"},
	}))

	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Yuki Tanaka", SideB: "Rin Sato", Winner: "Yuki Tanaka", Status: state.MatchStatusCompleted}},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 1)
	m := bracket.Rounds[0][0]
	assert.Empty(t, m.SideAID, "an ambiguous name is left alone, never guessed")
	assert.Equal(t, rinID, m.SideBID, "the unambiguous side still resolves")
	assert.Empty(t, m.WinnerID, "WinnerID cannot be derived without a resolved SideAID")
}

// TestLegacyBracketSideIDUpgrade_SameNameRoundZeroWinnerIDLeftEmpty pins
// bc-brid: StampRoundZeroSideIDsFromDrawOrder's WinnerID
// derivation called domain.AttributeWinnerSide on names with NO SideA !=
// SideB guard, unlike its sibling deriveWinner (legacy_upgrade.go) which
// carries that guard explicitly. DrawOrder resolves round-0 POSITIONALLY, so
// it correctly tells the two same-name competitors apart for SideAID/SideBID
// even though they share a display name -- but the row's own Winner name
// ("Sam") matches BOTH sides identically, so a WinnerID derived from names
// alone cannot legitimately pick one. Before the fix this invented a
// coin-flip WinnerID (always SideA's, domain.AttributeWinnerSide's own
// name-tier tie-break) and persisted it; after the fix it is left empty,
// exactly like every other unresolvable same-name row this file's siblings
// pin.
func TestLegacyBracketSideIDUpgrade_SameNameRoundZeroWinnerIDLeftEmpty(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	samSouthID := helper.NewUUID4()
	samNorthID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: samSouthID, Name: "Sam", Dojo: "South"},
		{ID: samNorthID, Name: "Sam", Dojo: "North"},
	}))

	// Legacy shape: DrawOrder is present (standalone knockout) but
	// SideAID/SideBID/WinnerID are all empty. The row's Winner name ("Sam")
	// matches BOTH SideA and SideB identically -- the exact shape a
	// same-name round-0 meeting takes.
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		DrawOrder: []string{samSouthID, samNorthID},
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Sam", SideB: "Sam", Winner: "Sam", Status: state.MatchStatusCompleted}},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 1)
	m := bracket.Rounds[0][0]
	assert.Equal(t, samSouthID, m.SideAID, "DrawOrder resolves round-0 positionally, even for a duplicated name")
	assert.Equal(t, samNorthID, m.SideBID)
	assert.Empty(t, m.WinnerID, "a same-name round-0 winner cannot be told apart by name alone; it must be left empty, never a coin-flip guess")
}

// TestLegacyBracketUpgrade_AlreadyStampedNotRewritten pins the
// no-write-when-unchanged rule the pools/pool-matches repairs already
// follow (see TestLegacyPoolUpgrade_AlreadyStampedNotRewritten): a
// bracket.json whose every side already carries its id must not be
// rewritten on load, and the cached file version must not bump.
func TestLegacyBracketUpgrade_AlreadyStampedNotRewritten(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Rin Sato", SideAID: rinID, SideB: "Yuki Tanaka", SideBID: yukiID,
				Winner: "Rin Sato", WinnerID: rinID, Status: state.MatchStatusCompleted}},
		},
	}))

	bracketPath := filepath.Join(dir, "competitions", "c1", "bracket.json")
	before, err := os.ReadFile(bracketPath)
	require.NoError(t, err)

	fresh := freshLegacyUpgradeStore(t, dir)
	verBefore := fresh.FileVersion("c1", "bracket.json")

	_, err = fresh.LoadBracket("c1")
	require.NoError(t, err)

	after, err := os.ReadFile(bracketPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a fully-stamped bracket.json must not be rewritten")
	assert.Equal(t, verBefore, fresh.FileVersion("c1", "bracket.json"), "no version bump for an untouched file")
}

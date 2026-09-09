package state_test

// legacy_upgrade_pool_ids_test.go pins the bc-pnum load-time repair for the
// two on-disk shapes legacy_upgrade_test.go's header comment documents
// alongside the seeds.csv one: pools.csv rows with an empty id (pre-append-
// column builds) and pool-matches.csv rows with an empty SideAID/SideBID/
// WinnerID (same vintage). Both convert ON READ, under the per-comp write
// lock, exactly like the seeds.csv upgrade this file's sibling pins.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLegacyUpgradeFixture creates a fresh Store rooted at a fresh t.TempDir()
// with competition "c1" already saved -- the setup every test below builds
// on before writing its own legacy-shaped pools.csv/pool-matches.csv/
// participants.csv fixture.
func newLegacyUpgradeFixture(t *testing.T) (dir string, s *state.Store) {
	t.Helper()
	dir = t.TempDir()
	s, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1"}))
	return dir, s
}

// freshLegacyUpgradeStore opens a NEW Store instance rooted at dir. Most
// tests below need one after using `s` (or another Store) to seed the
// fixture: EnsureLegacyUpgraded's once-map is per-process (here, per Store),
// so reading "c1" through a store that already touched it first would
// silently skip the repair -- see the once-per-comp contract in
// legacy_upgrade.go. A second, fresh Store instance is therefore
// load-bearing wherever it appears below, not an artifact of copy-paste.
func freshLegacyUpgradeStore(t *testing.T, dir string) *state.Store {
	t.Helper()
	fresh, err := state.NewStore(dir)
	require.NoError(t, err)
	return fresh
}

// TestLegacyPoolParticipantIDUpgradeOnRead: a legacy 7-column pools.csv (no
// id column at all) gets ids stamped from participants.csv by an exact
// name+dojo match after a plain LoadPools, and the file on disk carries them
// afterward -- not just the returned copy.
func TestLegacyPoolParticipantIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))

	// Pre-append-column shape: PoolName,Name,Position,DisplayName,Dojo,Seed,Number.
	legacy := "Pool A,Rin Sato,0,,Seibukan,,\nPool A,Yuki Tanaka,1,,Tobukan,,\n"
	poolsPath := filepath.Join(dir, "competitions", "c1", "pools.csv")
	require.NoError(t, os.WriteFile(poolsPath, []byte(legacy), 0o600))

	// A fresh Store instance: see freshLegacyUpgradeStore's own doc comment.
	fresh := freshLegacyUpgradeStore(t, dir)
	pools, err := fresh.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)

	byName := map[string]string{}
	for _, p := range pools[0].Players {
		byName[p.Name] = p.ID
	}
	assert.Equal(t, rinID, byName["Rin Sato"], "id stamped from the roster by an exact name+dojo match")
	assert.Equal(t, yukiID, byName["Yuki Tanaka"])
	assert.Empty(t, helper.PoolsMissingParticipantIDsMessage(pools), "a fully-repaired draw raises no notice")

	raw, err := os.ReadFile(poolsPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), rinID, "the repair lands on disk, not just in the returned copy")
	assert.Contains(t, string(raw), yukiID)
}

// TestLegacyPoolMatchSideIDUpgradeOnRead: a legacy 16-column pool-matches.csv
// (through Round, no SideAID/SideBID/WinnerID columns at all) gets
// SideAID/SideBID stamped from the roster (both names are unique) after a
// plain LoadPoolMatches, and WinnerID follows from the row's OWN
// just-resolved SideAID (Winner == SideA), not a fresh roster lookup.
func TestLegacyPoolMatchSideIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))

	// Pre-append-column shape (16 columns, through Round): PoolName,MatchIdx,
	// SideA,SideB,Winner,IpponsA,IpponsB,HansokuA,HansokuB,Decision,Status,
	// Court,SubResults,ScheduledAt,ResultSource,Round -- no SideAID, SideBID
	// or WinnerID columns at all.
	legacy := "Pool A,0,Rin Sato,Yuki Tanaka,Rin Sato,,,0,0,,completed,1,,,,1\n"
	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	require.NoError(t, os.WriteFile(matchesPath, []byte(legacy), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	m := matches[0]
	assert.Equal(t, rinID, m.SideAID)
	assert.Equal(t, yukiID, m.SideBID)
	assert.Equal(t, rinID, m.WinnerID, "WinnerID is derived from the row's own SideA match, not a fresh roster lookup")
	assert.Empty(t, engine.PoolMatchesMissingSideIDsMessage(matches), "a fully-repaired row raises no notice")

	raw, err := os.ReadFile(matchesPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), rinID, "the repair lands on disk, not just in the returned copy")
	assert.Contains(t, string(raw), yukiID)
}

// TestLegacyPoolMatchSideIDUpgrade_AmbiguousNameLeftAlone is the residue
// case: two roster entries share the exact name "Yuki Tanaka" (different
// dojos), so the unique-bare-name resolution the repair relies on (the same
// fallback upgradeSeedRowsLocked already uses for seeds.csv) cannot pick
// one. That side's id is left alone rather than guessed, the row still
// carries the operator-facing notice, and the OTHER side (a unique name)
// still resolves normally.
func TestLegacyPoolMatchSideIDUpgrade_AmbiguousNameLeftAlone(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	yuki1 := helper.NewUUID4()
	yuki2 := helper.NewUUID4()
	rinID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: yuki1, Name: "Yuki Tanaka", Dojo: "Seibukan"},
		{ID: yuki2, Name: "Yuki Tanaka", Dojo: "Tobukan"},
		{ID: rinID, Name: "Rin Sato", Dojo: "Kobukan"},
	}))

	legacy := "Pool A,0,Yuki Tanaka,Rin Sato,Yuki Tanaka,,,0,0,,completed,1,,,,1\n"
	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	require.NoError(t, os.WriteFile(matchesPath, []byte(legacy), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	m := matches[0]
	assert.Empty(t, m.SideAID, "an ambiguous name is left alone, never guessed")
	assert.Equal(t, rinID, m.SideBID, "the unambiguous side still resolves")
	assert.Empty(t, m.WinnerID, "WinnerID cannot be derived without a resolved SideAID")

	msg := engine.PoolMatchesMissingSideIDsMessage(matches)
	assert.NotEmpty(t, msg, "the residue keeps its operator-facing notice")
	assert.Contains(t, msg, "Yuki Tanaka vs Rin Sato")
}

// TestLegacyPoolMatchSideIDUpgrade_RunsOnceThenSkips: the repair runs once
// per competition per Store instance. A second load on the same Store must
// not rewrite the (already-repaired) file again.
func TestLegacyPoolMatchSideIDUpgrade_RunsOnceThenSkips(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	legacy := "Pool A,0,Rin Sato,Yuki Tanaka,Rin Sato,,,0,0,,completed,1,,,,1\n"
	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	require.NoError(t, os.WriteFile(matchesPath, []byte(legacy), 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	_, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	verAfterRepair := fresh.FileVersion("c1", "pool-matches.csv")

	// Put the file back into a legacy (needs-work) shape, as if an external
	// process reverted it. If the once-per-comp-per-Store gate (the
	// legacyUpgraded map in legacy_upgrade.go) is doing its job, a second
	// LoadPoolMatches on the SAME Store instance must not even inspect this
	// file again, let alone repair it -- proving "runs once" is a real gate,
	// not just a side effect of there being nothing left to repair the
	// second time (which TestLegacyPoolUpgrade_AlreadyStampedNotRewritten
	// covers separately, for a Store that has never touched the comp before).
	require.NoError(t, os.WriteFile(matchesPath, []byte(legacy), 0o600))

	_, err = fresh.LoadPoolMatches("c1")
	require.NoError(t, err)

	bytesAfterSecondLoad, err := os.ReadFile(matchesPath)
	require.NoError(t, err)
	assert.Equal(t, legacy, string(bytesAfterSecondLoad),
		"a second load on the same Store must not re-inspect, let alone rewrite, a file it already upgraded once")
	assert.Equal(t, verAfterRepair, fresh.FileVersion("c1", "pool-matches.csv"),
		"a second load must not bump the file version")
}

// TestLegacyPoolUpgrade_AlreadyStampedNotRewritten: a competition whose
// pools.csv and pool-matches.csv are already fully stamped (written through
// the normal SavePools/SavePoolMatches API, exactly like any competition
// drawn after bc-pnum's id-stamping went live) is NOT rewritten by the
// repair: same bytes, same Store.FileVersion, across a load. Mirrors the
// shape TestSupersededIsReportedOnEveryWritePath/"a stale write leaves no
// footprint" in internal/engine/superseded_matrix_test.go uses to pin the
// same "an unaffected write leaves no footprint" property.
func TestLegacyPoolUpgrade_AlreadyStampedNotRewritten(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	rinID := helper.NewUUID4()
	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
	}))
	require.NoError(t, s.SavePools("c1", []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: rinID, Name: "Rin Sato", Dojo: "Seibukan"},
			{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Tobukan"},
		}},
	}))
	require.NoError(t, s.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Rin Sato", SideAID: rinID, SideB: "Yuki Tanaka", SideBID: yukiID,
			Status: state.MatchStatusCompleted, Winner: "Rin Sato", WinnerID: rinID},
	}))

	poolsPath := filepath.Join(dir, "competitions", "c1", "pools.csv")
	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	poolsBefore, err := os.ReadFile(poolsPath)
	require.NoError(t, err)
	matchesBefore, err := os.ReadFile(matchesPath)
	require.NoError(t, err)

	fresh := freshLegacyUpgradeStore(t, dir)
	poolsVerBefore := fresh.FileVersion("c1", "pools.csv")
	matchesVerBefore := fresh.FileVersion("c1", "pool-matches.csv")

	_, err = fresh.LoadPools("c1")
	require.NoError(t, err)
	_, err = fresh.LoadPoolMatches("c1")
	require.NoError(t, err)

	poolsAfter, err := os.ReadFile(poolsPath)
	require.NoError(t, err)
	matchesAfter, err := os.ReadFile(matchesPath)
	require.NoError(t, err)
	assert.Equal(t, string(poolsBefore), string(poolsAfter), "a fully-stamped pools.csv must not be rewritten")
	assert.Equal(t, string(matchesBefore), string(matchesAfter), "a fully-stamped pool-matches.csv must not be rewritten")
	assert.Equal(t, poolsVerBefore, fresh.FileVersion("c1", "pools.csv"), "no version bump for an untouched file")
	assert.Equal(t, matchesVerBefore, fresh.FileVersion("c1", "pool-matches.csv"), "no version bump for an untouched file")
}

// TestLegacyUpgrade_ReArmsOnParticipantsSave pins bc-pnum review blocker 1:
// EnsureLegacyUpgraded's once-map is stamped per competition per process on
// the FIRST read, and (pre-fix) only DeleteCompetition ever cleared it. But
// the ids the pools.csv/pool-matches.csv repairs copy are minted when the
// ROSTER is saved. For a GENUINELY legacy competition, participants.csv has
// no ids either, so the first read finds nothing to copy, repairs nothing,
// and marked the competition done for the life of the Store. The operator
// then applies the participant list, exactly as the setup notice already
// tells them to -- which mints the ids the repair needed all along -- but
// pre-fix, pools.csv/pool-matches.csv were never repaired until the app
// restarted. saveParticipantsNoLock now re-arms the gate on every roster
// write, so the very next read retries. This test saves a legacy
// competition whose roster ALSO has no ids, loads it (asserting nothing is
// repaired), saves the roster so ids are minted, and loads again on the
// SAME Store instance, asserting pools.csv AND pool-matches.csv are now
// repaired -- no restart, no second Store.
func TestLegacyUpgrade_ReArmsOnParticipantsSave(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	// A genuinely legacy competition: participants.csv, pools.csv AND
	// pool-matches.csv all predate their id columns.
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	require.NoError(t, os.WriteFile(participantsPath, []byte("Rin Sato,Seibukan\nYuki Tanaka,Tobukan\n"), 0o600))

	poolsPath := filepath.Join(dir, "competitions", "c1", "pools.csv")
	require.NoError(t, os.WriteFile(poolsPath, []byte("Pool A,Rin Sato,0,,Seibukan,,\nPool A,Yuki Tanaka,1,,Tobukan,,\n"), 0o600))

	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	require.NoError(t, os.WriteFile(matchesPath, []byte("Pool A,0,Rin Sato,Yuki Tanaka,Rin Sato,,,0,0,,completed,1,,,,1\n"), 0o600))

	// First read: participants.csv is ITSELF legacy, so there is nothing to
	// copy an id FROM. EnsureLegacyUpgraded's first pass repairs nothing.
	pools, err := s.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	for _, p := range pools[0].Players {
		assert.Empty(t, p.ID, "nothing to copy from a roster that is itself legacy")
	}
	matches, err := s.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Empty(t, matches[0].SideAID, "same reason: the roster has no ids yet")
	assert.Empty(t, matches[0].WinnerID)

	// The operator applies the participant list, exactly as the Overview
	// notice says: a plain roster save, which mints an id for every id-less
	// row (marshalParticipantsCSV, participants.csv's one write chokepoint).
	loaded, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	require.NoError(t, s.SaveParticipants("c1", loaded))

	minted, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	byName := map[string]string{}
	for _, p := range minted {
		require.NotEmpty(t, p.ID, "the roster save mints an id for every id-less row")
		byName[p.Name] = p.ID
	}

	// Load again, on the SAME Store: the save above must have re-armed the
	// once-per-process gate (saveParticipantsNoLock's
	// s.legacyUpgraded.Delete), so THIS read retries the repair against a
	// roster that now carries ids -- no restart, no new Store.
	repairedPools, err := s.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, repairedPools, 1)
	for _, p := range repairedPools[0].Players {
		assert.Equal(t, byName[p.Name], p.ID, "pools.csv is repaired on the very next read after the roster save")
	}
	assert.Empty(t, helper.PoolsMissingParticipantIDsMessage(repairedPools))

	repairedMatches, err := s.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, repairedMatches, 1)
	assert.Equal(t, byName["Rin Sato"], repairedMatches[0].SideAID)
	assert.Equal(t, byName["Yuki Tanaka"], repairedMatches[0].SideBID)
	assert.Equal(t, byName["Rin Sato"], repairedMatches[0].WinnerID, "WinnerID is derived once SideAID resolves")
	assert.Empty(t, engine.PoolMatchesMissingSideIDsMessage(repairedMatches))
}

// TestLegacyUpgrade_SameNameWinnerLeftUnresolved pins two review rounds'
// findings against the SAME reproduction: a pool-matches.csv row whose
// SideA and SideB hold the exact same name.
//
// Round 1 (bc-pnum review blocker 2): comparing Winner against SideA first
// and stopping there (the pre-fix hand-rolled `case m.Winner == m.SideA:
// ...`) is true for BOTH sides whenever SideA and SideB hold the exact same
// name, so it always credited side A and PERSISTED that id, silently
// removing the row's operator-facing notice even though the winner was
// never actually resolved. internal/engine/bc_idfx_test.go already fixed
// this exact mechanism once for the runtime resolver (resolveWinnerSide);
// this pins the load-time repair's own copy of the same bug.
//
// Round 2 (second review, MEDIUM): the m.SideA != m.SideB gate that fixed
// round 1 guarded ONLY the winner derivation. The roster below holds a
// SINGLE "Yuki Tanaka" -- a name that IS unique -- so the per-side stamping
// loop (which runs BEFORE the winner gate) individually passes its own
// NameCount(m.SideA) == 1 / NameCount(m.SideB) == 1 checks for BOTH sides
// and would (pre-fix) resolve BOTH SideAID and SideBID to that ONE
// competitor's id: a half-repair strictly worse than doing nothing. Before
// this repair existed, an id-less row contributed nothing to standings (the
// standings builder skips a row when either side fails to resolve); after a
// half-repair like this, both sides resolve to the SAME person and the row
// silently double-counts them against themselves, with no equal-sides guard
// downstream to catch it. The fix is a whole-row skip at the top of the
// loop (m.SideA != "" && m.SideA == m.SideB), so this row is left ENTIRELY
// alone: no side id, no winner id, exactly as if the repair had never run.
// (An ambiguous name across two different dojos -- NameCount == 2, not 1 --
// is the separate scenario TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard
// pins below; a unique name is what actually exercises the per-side
// stamping loop this round's bug lived in.)
func TestLegacyUpgrade_SameNameWinnerLeftUnresolved(t *testing.T) {
	_, s := newLegacyUpgradeFixture(t)

	yukiID := helper.NewUUID4()
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: yukiID, Name: "Yuki Tanaka", Dojo: "Seibukan"},
	}))

	require.NoError(t, s.SavePoolMatches("c1", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Yuki Tanaka", SideB: "Yuki Tanaka",
			Winner: "Yuki Tanaka", Status: state.MatchStatusCompleted},
	}))

	// Same Store: it has not yet called LoadPoolMatches for "c1", so the
	// once-map has not been stamped and EnsureLegacyUpgraded still runs.
	matches, err := s.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Empty(t, matches[0].SideAID,
		"same-name sides can never be told apart by name alone; the row must be left ENTIRELY alone, never half-repaired by stamping both sides to the one roster match")
	assert.Empty(t, matches[0].SideBID,
		"the other side must stay empty too: stamping only one side to the shared id would still leave the row wrongly resolved")
	assert.Empty(t, matches[0].WinnerID,
		"the winner must be left unattributed rather than defaulting to side A")

	msg := engine.PoolMatchesMissingSideIDsMessage(matches)
	assert.NotEmpty(t, msg, "the row keeps its operator-facing notice since it was never actually resolved")
}

// TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard pins bc-pnum
// review blocker 3: domain.RosterIndex.Lookup(name, "") tries the EXACT key
// "name|" first and only THEN falls back to the unique-bare-name match, so
// a roster carrying a BLANK-DOJO entry under a name that a SECOND,
// non-blank-dojo entry also uses scores an exact hit on that first branch,
// bypassing the fallback's own NameCount==1 uniqueness guard entirely.
// Blank dojos are refused on save (ErrBlankDojo) but tolerated on load,
// deliberately, so a legacy roster reaches this code with one intact -- this
// fixture writes participants.csv directly for exactly that reason;
// SaveParticipants cannot produce it.
//
// A pool-matches.csv side has no dojo column at all, so the "" passed to
// Lookup here is a stand-in for "unknown", not a recorded blank value (the
// pools.csv case is different: see upgradePoolParticipantIDsLocked's own
// doc comment). Treating "unknown" as an exact match to a coincidentally
// blank-dojo roster entry would silently attribute an unknown-dojo side to
// the WRONG competitor whenever a second, differently-dojo'd namesake also
// exists, exactly as reproduced here.
func TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	blankDojoID := helper.NewUUID4()
	tobukanID := helper.NewUUID4()
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	rawRoster := blankDojoID + ",Yuki Tanaka,\n" + tobukanID + ",Yuki Tanaka,Tobukan\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(rawRoster), 0o600))

	row := strings.Join([]string{"Pool A", "0", "Yuki Tanaka", "", "", "", "", "0", "0", "", "completed", "1", "", "", "", "1"}, ",") + "\n"
	matchesPath := filepath.Join(dir, "competitions", "c1", "pool-matches.csv")
	require.NoError(t, os.WriteFile(matchesPath, []byte(row), 0o600))

	matches, err := s.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Empty(t, matches[0].SideAID,
		"Yuki Tanaka is ambiguous (a blank-dojo entry AND a Tobukan entry both carry that name): the side must be left alone, never resolved to the blank-dojo entry by coincidence")
}

// TestLegacyUpgrade_PoolParticipantID_BlankDojoDoesNotDefeatAmbiguityGuard is
// the pools.csv twin of TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard
// above, pinning the second review round's HIGH finding:
// upgradePoolParticipantIDsLocked called idx.Lookup(p.Name, p.Dojo) with the
// ROW'S OWN blank dojo passed straight through, unguarded. That hits
// domain.RosterIndex.Lookup's first branch, the exact key "name|", whenever
// some roster entry happens to carry the row's name under a blank dojo too
// -- bypassing the NameCount uniqueness fallback entirely and silently
// stamping the row with a specific blank-dojo competitor's id by
// coincidence, even though a second, non-blank-dojo namesake also exists.
// This is the identical hole the pool-matches side already guards against
// (TestLegacyUpgrade_BlankDojoDoesNotDefeatAmbiguityGuard above); the fix
// mirrors that guard here: an explicit idx.NameCount(p.Name) != 1 check
// before Lookup ever runs, whenever the row's own dojo is blank.
//
// Blank dojos are refused on save (state.ErrBlankDojo) but tolerated on
// load, deliberately, so a legacy roster can be repaired rather than
// rejected outright -- this fixture writes participants.csv directly for
// exactly that reason; SaveParticipants cannot produce it.
func TestLegacyUpgrade_PoolParticipantID_BlankDojoDoesNotDefeatAmbiguityGuard(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	blankDojoID := helper.NewUUID4()
	tobukanID := helper.NewUUID4()
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	rawRoster := blankDojoID + ",Yuki Tanaka,\n" + tobukanID + ",Yuki Tanaka,Tobukan\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(rawRoster), 0o600))

	// Pre-append-column shape (PoolName,Name,Position,DisplayName,Dojo,Seed,
	// Number -- no id column), and the row's own Dojo column (index 4) is
	// ALSO blank, same as the pool-matches repro above.
	legacy := "Pool A,Yuki Tanaka,0,,,,\n"
	poolsPath := filepath.Join(dir, "competitions", "c1", "pools.csv")
	require.NoError(t, os.WriteFile(poolsPath, []byte(legacy), 0o600))

	pools, err := s.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 1)
	assert.Empty(t, pools[0].Players[0].ID,
		"Yuki Tanaka is ambiguous (a blank-dojo entry AND a Tobukan entry both carry that name): the row must be left alone, never resolved to the blank-dojo entry by coincidence")

	msg := helper.PoolsMissingParticipantIDsMessage(pools)
	assert.NotEmpty(t, msg, "the unresolved row keeps its operator-facing notice")
	assert.Contains(t, msg, "Yuki Tanaka")
}

// TestLegacyUpgrade_PoolParticipantID_BlankDojoUniqueNameStillRepairs is the
// CORRECT-DIRECTION twin of the guard above. The guard reads
// `p.Dojo == "" && idx.NameCount(p.Name) != 1`, and only its refusal half was
// pinned: replacing it with a blanket `if p.Dojo == "" { continue }` left the
// whole suite green, so nothing proved a blank-dojo row still repairs when its
// name IS unique on the roster. That is the common shape for the legacy data
// this repair exists for (a pre-Dojo pools.csv, one competitor per name), so
// over-skipping it would quietly turn the feature off for its main population
// while every ambiguity test kept passing.
func TestLegacyUpgrade_PoolParticipantID_BlankDojoUniqueNameStillRepairs(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	yukiID := helper.NewUUID4()
	participantsPath := filepath.Join(dir, "competitions", "c1", "participants.csv")
	// ONE Yuki Tanaka on the roster, so the name is unambiguous even though
	// the pools row below records no dojo to match on.
	rawRoster := yukiID + ",Yuki Tanaka,Tobukan\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(rawRoster), 0o600))

	legacy := "Pool A,Yuki Tanaka,0,,,,\n"
	poolsPath := filepath.Join(dir, "competitions", "c1", "pools.csv")
	require.NoError(t, os.WriteFile(poolsPath, []byte(legacy), 0o600))

	pools, err := s.LoadPools("c1")
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 1)
	assert.Equal(t, yukiID, pools[0].Players[0].ID,
		"a blank-dojo row whose name is unique on the roster must still be repaired: the guard refuses AMBIGUITY, not every blank dojo")

	assert.Empty(t, helper.PoolsMissingParticipantIDsMessage(pools),
		"a repaired row carries no notice")
}

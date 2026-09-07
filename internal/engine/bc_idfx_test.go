// Package engine, bc_idfx_test.go: regression tests for the bc-idfx fix-and-
// polish pass (recall-mode review of PR #408, all findings CONFIRMED by
// execution against 3bc4711c). Each test is named after the bead's finding
// number and reproduces the exact scenario the review executed.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// corruptOverridesFile writes unparseable JSON into the competition's
// overrides.json, forcing LoadOverrides to return a parse error. The
// competition directory must already exist (e.g. via SaveCompetition).
// Mirrors corruptCompetitionConfig (engi_test.go).
func corruptOverridesFile(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	path := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))
}

// --- Finding 1: groupMemberIDs must not fall through a foreign id to the name index ---

// TestGroupMemberIDs_NonMemberIDDoesNotFallThroughToName pins the
// membership-set contract directly: a NON-EMPTY id that does not belong to
// any member of the group must not resolve, never fall through to a name
// match. An EMPTY id ALSO does not resolve (operator ruling bc-pnum,
// converted from the pre-ruling behaviour: an empty id used to take a
// bare-name path; there is no name path left at all now).
func TestGroupMemberIDs_NonMemberIDDoesNotFallThroughToName(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-x-a", Name: "X", Dojo: "Dojo A"}},
		{Player: domain.Player{ID: "id-y", Name: "Y", Dojo: "Dojo Y"}},
	}
	ids := groupMemberIDs(group)

	// id-x-b is a REAL id, but belongs to X@DojoB, who is NOT a member of
	// this specific tied group (e.g. a stale TB row from before a score
	// correction moved the consequential tie). It must not resolve
	// outright rather than silently attributing it to X@DojoA merely
	// because they share the display name "X".
	assert.False(t, ids["id-x-b"], "a non-member id must not resolve, even when the name matches a group member")

	// An EMPTY id resolves to nothing (operator ruling bc-pnum: no name
	// fallback survives, even for a group that DOES carry ids).
	assert.False(t, ids[""], "an empty id must resolve to nothing, never to a name match")
}

// TestApplyTiebreakSort_ForeignIDNeverCreditsWrongMember is the end-to-end
// reproduction from the bead: a pool has X@A(id1), X@B(id3) and Y(id2). A TB-0
// bout is stamped between X@B and Y and played (Y wins). A score correction
// then moves the CONSEQUENTIAL tie to X@A + Y (X@B is no longer part of this
// tied group). Before the fix, resolving X@B's foreign id (id3) against the
// NEW group {X@A, Y} fell through to the bare-name index and silently
// attributed the TB-0 bout to X@A -- who never played it -- reordering Y
// above her on a bout she never fought. After the fix, the foreign id fails
// to resolve, the bout is skipped entirely for this group, and the original
// order is preserved (no supplementary result to sort on).
func TestApplyTiebreakSort_ForeignIDNeverCreditsWrongMember(t *testing.T) {
	sorted := []state.PlayerStanding{
		{Player: domain.Player{ID: "id1", Name: "X", Dojo: "Dojo A"}, Points: 100},
		{Player: domain.Player{ID: "id2", Name: "Y", Dojo: "Dojo Y"}, Points: 100},
	}
	matches := []state.MatchResult{
		{
			ID: "Pool P-TB-0",
			// Stale bout: really X@B (id3) vs Y (id2), Y won. X@B is NOT a
			// member of the group being sorted here (only X@A/id1 and Y/id2 are).
			SideA: "X", SideAID: "id3",
			SideB: "Y", SideBID: "id2",
			Winner: "Y", WinnerID: "id2",
			Status: state.MatchStatusCompleted,
		},
	}

	applyTiebreakSort(sorted, matches, IsTiebreakerMatchID)

	require.Len(t, sorted, 2)
	assert.Equal(t, "id1", sorted[0].Player.ID, "X@A must keep her original position; the TB-0 bout was never hers to be sorted on")
	assert.Equal(t, "id2", sorted[1].Player.ID)
}

// TestNewGroupKeyResolver_FullyLegacyGroupFallsThroughForeignIDToName pinned
// the bc-idfx review's nit 20 fix (a fully id-less group fell through to a
// bare-name index). That name-fallback subject no longer exists: the
// operator ruling bc-pnum removed newGroupKeyResolver's byName index
// entirely, so a fully id-less group now resolves NOTHING, by id or by
// name. Deleted (not converted) because there is no fallback path left for
// a replacement test to exercise; see
// TestApplyTiebreakSort_IDlessGroupNeverResolvesBout for the group's new,
// opposite behaviour end-to-end.

// TestApplyTiebreakSort_IDlessGroupNeverResolvesBout is the converted twin
// of the deleted TestApplyTiebreakSort_FullyLegacyGroupResolvesBoutWithStrayID
// (which pinned the pre-bc-pnum name-fallback: a fully id-less group's
// supplementary bout resolved by name despite the bout row carrying a stray
// id). Under the operator ruling, a group with no id-carrying members has
// nothing to resolve a bout side against, by id or otherwise -- there is no
// name fallback left -- so the bout is skipped entirely and the input order
// stands, even though the bout names a winner.
func TestApplyTiebreakSort_IDlessGroupNeverResolvesBout(t *testing.T) {
	sorted := []state.PlayerStanding{
		{Player: domain.Player{Name: "X", Dojo: "Dojo A"}, Points: 100},
		{Player: domain.Player{Name: "Y", Dojo: "Dojo Y"}, Points: 100},
	}
	matches := []state.MatchResult{
		{
			ID:    "Pool P-TB-0",
			SideA: "X", SideAID: "some-stray-id", // stray id, group has none
			SideB:  "Y",
			Winner: "Y", WinnerID: "some-stray-id-2",
			Status: state.MatchStatusCompleted,
		},
	}

	applyTiebreakSort(sorted, matches, IsTiebreakerMatchID)

	require.Len(t, sorted, 2)
	assert.Equal(t, "X", sorted[0].Player.Name, "neither group member carries an id, so the bout cannot resolve for either side and the input order is unchanged")
	assert.Equal(t, "Y", sorted[1].Player.Name)
}

// --- Finding 2: eligibility resolution must use side ids, not the first namesake ---

// TestRecordDecision_KikenResolvesLoserByID_NotFirstRegisteredNamesake is the
// bead's repro: roster has Tanaka@DojoB registered BEFORE Tanaka@DojoA.
// Tanaka@DojoA withdraws (kiken) against Sato. Before the fix,
// recordIneligibilityFromDecision resolved the loser purely by NAME via
// lookupPlayerID, which returns the FIRST namesake in roster order
// (Tanaka@DojoB) -- so the wrong competitor was marked ineligible, and
// Tanaka@DojoB's own next match would incorrectly 409. After the fix, the
// match's own SideAID/SideBID (stamped at pool generation) resolve the loser
// unambiguously.
func TestRecordDecision_KikenResolvesLoserByID_NotFirstRegisteredNamesake(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-namesake"
	createTestCompetition(t, store, compID, "league", 4)

	tanakaB := helper.NewUUID4() // registered FIRST
	tanakaA := helper.NewUUID4() // registered SECOND; the one who actually withdraws
	satoID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: tanakaB, Name: "Tanaka", Dojo: "DojoB"},
		{ID: tanakaA, Name: "Tanaka", Dojo: "DojoA"},
		{ID: satoID, Name: "Sato", Dojo: "DojoS"},
	}))
	// Pool matches: Tanaka@A (SideA) vs Sato on Pool A-0 (the one that gets
	// the kiken); Tanaka@B (SideA) vs Sato on Pool A-1 (must remain startable).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideAID: tanakaA, SideB: "Sato", SideBID: satoID, Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "Pool A-1", SideA: "Tanaka", SideAID: tanakaB, SideB: "Sato", SideBID: satoID, Status: state.MatchStatusScheduled, Court: "A"},
	}))

	// decisionBy "aka" => SideA (Tanaka@A) is the withdrawing/losing side.
	_, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", nil, false)
	require.NoError(t, err)
	require.NotNil(t, status, "a CompetitorStatus must be written for the kiken")
	assert.Equal(t, tanakaA, status.PlayerID, "the ineligibility must land on Tanaka@A, the competitor who actually withdrew")

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	if st, ok := statuses[tanakaB]; ok {
		assert.True(t, st.Eligible, "Tanaka@B must remain eligible; she never withdrew")
	}

	// Tanaka@B's own match must not be blocked by the eligibility gate.
	require.NoError(t, eng.StartMatch(compID, "Pool A-1"))
}

// TestRecordDecision_PartiallyStampedRow_StillRecordsIneligibility is the
// second-Opus-pass repro for item 1: loserPlayerID's second return used to
// conflate "the SIDE is unresolved" with "the resolved side's own id field
// happens to be empty" (a partially-stamped row: SideAID unset, SideBID
// set). decisionBy="aka" names SideA (Tanaka) as the loser; the WINNER
// (Sato, SideB) carries a real id, which is enough for resolveWinnerSide to
// resolve winnerIsB via the id branch -- so the losing SIDE (SideA) is not
// remotely ambiguous, only her own id field on THIS row was never stamped.
// Before the fix this was indistinguishable from a genuinely unresolved
// side and was rejected outright, logging "cannot resolve" and recording no
// ineligibility at all -- so Tanaka's own next match would incorrectly
// start unblocked and the withdrawal would go unenforced. The name fallback
// (loserSideName, already computed independently) must still resolve her.
func TestRecordDecision_PartiallyStampedRow_StillRecordsIneligibility(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-partial-stamp"
	createTestCompetition(t, store, compID, "league", 4)

	tanakaID := helper.NewUUID4()
	satoID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: tanakaID, Name: "Tanaka", Dojo: "DojoA"},
		{ID: satoID, Name: "Sato", Dojo: "DojoS"},
	}))
	// Tanaka's own id was never stamped on this row (SideAID ""), even
	// though she has a real participant id in the roster -- the shape a row
	// generated before ids existed, or written by an older client, takes.
	// Sato's id IS stamped.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideAID: "", SideB: "Sato", SideBID: satoID, Status: state.MatchStatusScheduled, Court: "A"},
	}))

	// decisionBy "aka" => SideA (Tanaka) is the withdrawing/losing side.
	_, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", nil, false)
	require.NoError(t, err)
	require.NotNil(t, status, "a CompetitorStatus must be written -- the losing SIDE is known even though her id field on this row is empty")
	assert.Equal(t, tanakaID, status.PlayerID, "the ineligibility must land on Tanaka, resolved by name fallback for the known losing side")
	assert.False(t, status.Eligible)
}

// --- Second Opus pass, item 3: RecordDecisionTx's restore-on-rescore must
// key on the competitor-status RECORD (MatchID + Eligible), not re-derive
// identity from the match's side names/ids ---

// TestRecordDecisionTx_SameNamePairing_RescoreAsFoughtRestoresEligibility is
// the item 3 repro: two "Tanaka" from different dojos meet directly (a
// same-name pairing). Recording a kiken correctly marks the specific
// withdrawing side ineligible (item 2/round-2's own fix), but the OLD
// restore-on-rescore logic re-derived "was the prior loser resolved
// unambiguously" via loserPlayerID on the SAME same-name row -- which is
// ALWAYS ambiguous for a same-name pairing with no WinnerID stamped on the
// rescore -- and skipped the restore outright, logging and leaving Tanaka@A
// ineligible FOREVER (kiken-voluntary is not Reinstateable, so
// ReinstateCompetitor refuses too; only this restore path could ever clear
// it). The fix restores by matching the competitor-status record's own
// MatchID against a matchID key ONLY the engine itself ever wrote (exact
// regardless of any name collision), so a same-name pairing restores exactly
// like any other rescore.
func TestRecordDecisionTx_SameNamePairing_RescoreAsFoughtRestoresEligibility(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "same-name-restore"
	createTestCompetition(t, store, compID, "league", 3)

	tanakaAID := helper.NewUUID4()
	tanakaBID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: tanakaAID, Name: "Tanaka", Dojo: "DojoA"},
		{ID: tanakaBID, Name: "Tanaka", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideAID: tanakaAID, SideB: "Tanaka", SideBID: tanakaBID, Status: state.MatchStatusScheduled, Court: "A"},
	}))

	// decisionBy "aka" => SideA (Tanaka@DojoA) withdraws.
	_, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", nil, false)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, tanakaAID, status.PlayerID)
	assert.False(t, status.Eligible)

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.False(t, statuses[tanakaAID].Eligible, "precondition: Tanaka@DojoA must be ineligible before the rescore")

	// Rescore the SAME match as a normal fought result: decision no longer
	// kiken/fusenpai, so recordIneligibilityFromDecision writes nothing new
	// for this match -- the stale Pool A-0 ineligibility entry must be
	// restored by the record itself, not re-derived from the row's (still
	// same-name, still id-less-on-WinnerID) sides.
	_, restoredStatus, err := eng.RecordDecision(compID, "Pool A-0", "fought", "aka", "", nil, false)
	require.NoError(t, err)
	require.NotNil(t, restoredStatus, "the stale ineligibility for THIS match must be restored, not silently skipped as ambiguous")
	assert.Equal(t, tanakaAID, restoredStatus.PlayerID)
	assert.True(t, restoredStatus.Eligible)

	statuses, err = store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[tanakaAID].Eligible, "Tanaka@DojoA must be eligible again after the rescore")
}

// --- Finding 3: ReplaceParticipantInDraw must match by participant id when available ---

// TestReplaceParticipantInDraw_MatchesByID_NotBareName is the bead's repro:
// Tanaka Kenji/Osaka and Tanaka Kenji/Tokyo sit in one pool with distinct
// participant ids stamped on both pools.csv and pool-matches.csv (exactly as
// the real tree-aware draw stamps them). Renaming Osaka's entry must rewrite
// ONLY her rows -- keyed by participant id -- never Tokyo's, even though both
// currently share the display name "Tanaka Kenji".
func TestReplaceParticipantInDraw_MatchesByID_NotBareName(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-by-id"
	comp := &state.Competition{
		ID: compID, Name: "ID Match Test", Kind: "individual",
		Format: state.CompFormatLeague, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}
	require.NoError(t, store.SaveCompetition(comp))

	osakaID := helper.NewUUID4()
	tokyoID := helper.NewUUID4()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: osakaID, Name: "Tanaka Kenji", Dojo: "Osaka"},
			{ID: tokyoID, Name: "Tanaka Kenji", Dojo: "Tokyo"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka Kenji", SideAID: osakaID, SideB: "Tanaka Kenji", SideBID: tokyoID,
			Winner: "Tanaka Kenji", WinnerID: osakaID, Status: state.MatchStatusScheduled, Court: "A"},
	}))

	warnings, err := eng.ReplaceParticipantInDraw(compID, osakaID, "Tanaka Kenji", "Osaka", "", "Tanaka K.", "Osaka", "")
	require.NoError(t, err)
	assert.Empty(t, warnings)

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 1)
	byID := map[string]helper.Player{}
	for _, p := range poolsAfter[0].Players {
		byID[p.ID] = p
	}
	require.Contains(t, byID, osakaID)
	require.Contains(t, byID, tokyoID)
	assert.Equal(t, "Tanaka K.", byID[osakaID].Name, "Osaka's own row must be renamed")
	assert.Equal(t, "Osaka", byID[osakaID].Dojo)
	assert.Equal(t, "Tanaka Kenji", byID[tokyoID].Name, "Tokyo's row must be untouched")
	assert.Equal(t, "Tokyo", byID[tokyoID].Dojo, "Tokyo's dojo must NOT be rewritten to Osaka")

	matchesAfter, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matchesAfter, 1)
	m := matchesAfter[0]
	assert.Equal(t, "Tanaka K.", m.SideA, "the match side carrying Osaka's id must be renamed")
	assert.Equal(t, "Tanaka Kenji", m.SideB, "the match side carrying Tokyo's id must be untouched")
	assert.Equal(t, "Tanaka K.", m.Winner, "the winner (Osaka, by id) must be renamed")
}

// --- Finding 4: computeStandingsFrom override sort: unstable + override-first-unconditional ---

// TestComputeStandingsFrom_OverrideSort_LargePoolDoesNotScrambleNaturalOrder
// is the bead's first repro: a 14-row pool with one override applied
// somewhere in the middle must NOT scramble the other 13 rows' natural
// (points-descending) order. Before the fix the fallback comparator compared
// every non-overridden pair by Rank, which is not assigned until AFTER this
// sort runs (always reads the zero value), and sort.Slice is not stable, so a
// >12-row pool could reorder equal-comparator rows arbitrarily.
func TestComputeStandingsFrom_OverrideSort_LargePoolDoesNotScrambleNaturalOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-large-pool"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Large Pool", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	const n = 14
	players := make([]helper.Player, n)
	for i := 0; i < n; i++ {
		players[i] = helper.Player{ID: fmt.Sprintf("id-%02d", i), Name: fmt.Sprintf("P%02d", i), Dojo: fmt.Sprintf("Dojo%02d", i)}
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))

	// P00 beats every other player, giving her a strictly distinct (highest)
	// points total; every other player has a losing record against her and
	// nothing else, so the natural order is P00 first, then P01..P13 (their
	// relative order among themselves doesn't matter for this assertion --
	// only that P00 stays on top).
	var matches []state.MatchResult
	for i := 1; i < n; i++ {
		matches = append(matches, state.MatchResult{
			ID: fmt.Sprintf("Pool A-%d", i-1), SideA: players[0].Name, SideAID: players[0].ID,
			SideB: players[i].Name, SideBID: players[i].ID,
			Winner: players[0].Name, WinnerID: players[0].ID, Status: state.MatchStatusCompleted,
		})
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	// A single override on the LAST-placed player (an operator chusen that
	// has nothing to do with the points leader) is enough to enter the
	// override-sort code path.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", players[n-1].ID, players[n-1].Name, players[n-1].Dojo, n))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, n)

	assert.Equal(t, players[0].ID, poolA[0].Player.ID, "the undefeated points leader must stay rank 1, not be scrambled by the override sort")
	assert.Equal(t, 1, poolA[0].Rank)
}

// TestComputeStandingsFrom_OverrideSort_NaturalRankBeatsUnrankedOverride is the
// bead's second repro: Alpha (2-0, undefeated, no override) is tied with
// Yank/Xray only insofar as they need a chusen; once Yank=2 and Xray=3 are
// recorded, the expected final order is Alpha 1, Yank 2, Xray 3. Before the
// fix, ANY overridden row sorted ahead of EVERY non-overridden row regardless
// of its recorded rank, so Alpha (undefeated, no override) was demoted below
// both Yank and Xray. A PARTIAL chusen (only Yank=2 recorded) must still
// produce Alpha 1, Yank 2, Xray 3 -- the group stays adjacent.
func TestComputeStandingsFrom_OverrideSort_NaturalRankBeatsUnrankedOverride(t *testing.T) {
	setup := func(t *testing.T) (*Engine, *state.Store, string) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		compID := "override-natural-vs-override"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Natural vs Override", Format: state.CompFormatMixed,
			Status: state.CompStatusPools, Courts: []string{"A"},
		}))
		alpha := helper.Player{ID: "id-alpha", Name: "Alpha", Dojo: "DojoAlpha"}
		yank := helper.Player{ID: "id-1-yank", Name: "Yank", Dojo: "DojoYank"}
		xray := helper.Player{ID: "id-2-xray", Name: "Xray", Dojo: "DojoXray"}
		require.NoError(t, store.SavePools(compID, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{alpha, yank, xray}},
		}))
		// Alpha beats both Yank and Xray (2-0, undefeated, ranks naturally
		// first). Yank and Xray draw each other, tying on every criterion.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: alpha.Name, SideAID: alpha.ID, SideB: yank.Name, SideBID: yank.ID,
				Winner: alpha.Name, WinnerID: alpha.ID, Status: state.MatchStatusCompleted},
			{ID: "Pool A-1", SideA: alpha.Name, SideAID: alpha.ID, SideB: xray.Name, SideBID: xray.ID,
				Winner: alpha.Name, WinnerID: alpha.ID, Status: state.MatchStatusCompleted},
			{ID: "Pool A-2", SideA: yank.Name, SideAID: yank.ID, SideB: xray.Name, SideBID: xray.ID,
				Winner: "", Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
		}))
		return eng, store, compID
	}

	t.Run("full chusen: Alpha 1, Yank 2, Xray 3", func(t *testing.T) {
		eng, store, compID := setup(t)
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "id-1-yank", "Yank", "DojoYank", 2))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "id-2-xray", "Xray", "DojoXray", 3))

		standings, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)
		poolA := standings["Pool A"]
		require.Len(t, poolA, 3)
		names := []string{poolA[0].Player.Name, poolA[1].Player.Name, poolA[2].Player.Name}
		assert.Equal(t, []string{"Alpha", "Yank", "Xray"}, names, "Alpha must stay rank 1 despite having no override")
	})

	t.Run("partial chusen (only Yank=2 recorded): group stays adjacent", func(t *testing.T) {
		eng, store, compID := setup(t)
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", "id-1-yank", "Yank", "DojoYank", 2))

		standings, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)
		poolA := standings["Pool A"]
		require.Len(t, poolA, 3)
		names := []string{poolA[0].Player.Name, poolA[1].Player.Name, poolA[2].Player.Name}
		assert.Equal(t, []string{"Alpha", "Yank", "Xray"}, names, "a partial chusen must not demote the undefeated Alpha or scramble the still-tied Xray")
	})
}

// TestComputeStandingsFrom_OverrideSort_NamesakesDoNotCollideOnNaturalRank
// pins the rule computeStandingsFrom's pairing struct enforces: a
// competitor's natural rank travels glued to their own standing through the
// sort (a per-element field, never a map keyed by identity), so two
// same-name-different-dojo competitors (legal, CheckDuplicateEntriesByNameDojo)
// can never have one's natural rank silently overwritten by the other's.
//
// Standings resolution is id-only (operator ruling bc-pnum): an id-less
// match row does not contribute to anyone's record at all (it is simply
// skipped). The fixture stamps both Tanakas with real, distinct ids
// (bctest.StampIDs) so their matches resolve and accrue wins exactly as
// production data would, against a same-name-different-dojo pair, which is
// the scenario this rule protects, independent of whether the pair happens
// to carry ids.
//
// Fixture: TanakaGhost (0-0-0, registered FIRST in roster order) and
// TanakaReal (2-0, undefeated leader, registered LAST) share the name
// "Tanaka" across different dojos. The two "Tanaka"-named match rows'
// SideAID is stamped BY HAND to TanakaReal's id, exactly as production data
// would (a real draw's SideAID names one specific competitor, never an
// ambiguous bare name) -- TanakaGhost genuinely never appears in a match
// and stays at 0-0-0. This is deliberately NOT bctest.StampIDs's own
// same-name resolution: that helper now panics if a match row needs its
// ambiguous byName lookup to resolve a duplicate roster name (bc-pnum
// review finding 9), precisely because silently picking "whichever
// namesake was registered last" is the class of bug this fixture exists to
// rule out, not a mechanism to lean on. Carol carries an override unrelated
// to either Tanaka. Before the original fix this dropped the 2-0 leader to
// rank 3 (probe-verified); after it she is rank 1.
func TestComputeStandingsFrom_OverrideSort_NamesakesDoNotCollideOnNaturalRank(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-namesake-collision"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Namesake Collision", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	// Roster order matters: TanakaGhost registered BEFORE TanakaReal.
	players := []helper.Player{
		{Name: "Tanaka", Dojo: "DojoGhost"},
		{Name: "Bob", Dojo: "DojoBob"},
		{Name: "Carol", Dojo: "DojoCarol"},
		{Name: "Tanaka", Dojo: "DojoReal"},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideB: "Bob", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Tanaka", SideB: "Carol", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Carol", Winner: "Bob", Status: state.MatchStatusCompleted},
	}
	// Stamp player ids first (nil matches: nothing to reconcile yet, so this
	// cannot hit the ambiguous-name panic). Then hand-stamp the two "Tanaka"
	// rows' SideAID directly to TanakaReal (players[3]) before the second
	// call, which fills in Bob's/Carol's non-ambiguous ids as usual -- a row
	// whose SideAID is already set never reaches byName at all.
	bctest.StampIDs(players, nil)
	matches[0].SideAID = players[3].ID
	matches[1].SideAID = players[3].ID
	bctest.StampIDs(players, matches)

	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	// Carol's override is unrelated to either Tanaka; its mere presence is
	// enough to enter the override-sort code path.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", players[2].ID, "Carol", "DojoCarol", 2))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 4)

	byDojo := make(map[string]state.PlayerStanding, len(poolA))
	for _, s := range poolA {
		byDojo[s.Player.Dojo] = s
	}
	require.Contains(t, byDojo, "DojoGhost")
	require.Contains(t, byDojo, "DojoReal")
	assert.Equal(t, 0, byDojo["DojoGhost"].Wins, "the ghost Tanaka never appears in a match and must stay 0-0-0")
	assert.Equal(t, 2, byDojo["DojoReal"].Wins, "the real Tanaka is the 2-0 undefeated leader")
	assert.Equal(t, 1, byDojo["DojoReal"].Rank, "the undefeated leader must be rank 1, not collide with her namesake's natural rank")
}

// --- Finding 5: RecordDecisionTx must attribute WinnerID by side, not by name ---

// TestRecordDecisionTx_SameNamePairing_AttributesWinnerBySide is the bead's
// repro: Tanaka Kenji/Tokyo (SideA) vs Tanaka Kenji/Osaka (SideB), 1-1 into
// encho, SideA (aka) withdraws. Before the fix, RecordDecisionTx never set
// WinnerID (or WinnerSide), and backfillMatchIdentity's same-name scoreline
// inference tied 1-1 (one real struck point on the loser vs one default-win
// maru on the winner) and gave up, leaving WinnerID empty; resolveWinnerSide
// then fell back to name comparison, which is true for BOTH sides on a
// same-name pairing, and the switch's first-match-wins semantics always
// credited SideA (Tokyo) -- even though SideB (Osaka) is the actual survivor.
func TestRecordDecisionTx_SameNamePairing_AttributesWinnerBySide(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "decision-samename"
	createTestCompetition(t, store, compID, "league", 2)

	tokyoID := helper.NewUUID4()
	osakaID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: tokyoID, Name: "Tanaka Kenji", Dojo: "Tokyo"},
		{ID: osakaID, Name: "Tanaka Kenji", Dojo: "Osaka"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: tokyoID, Name: "Tanaka Kenji", Dojo: "Tokyo"},
			{ID: osakaID, Name: "Tanaka Kenji", Dojo: "Osaka"},
		}},
	}))
	// 1-1 into encho: both sides have one real struck point recorded.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka Kenji", SideAID: tokyoID, SideB: "Tanaka Kenji", SideBID: osakaID,
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			Encho:  &state.EnchoMetadata{PeriodCount: 1},
			Status: state.MatchStatusRunning, Court: "A"},
	}))

	// decisionBy "aka" => SideA (Tokyo) withdraws; SideB (Osaka) survives.
	result, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "injury", &state.EnchoMetadata{PeriodCount: 1}, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, osakaID, result.WinnerID, "WinnerID must name Osaka (SideB), the actual survivor")
	assert.Equal(t, "B", result.WinnerSide)

	require.NotNil(t, status)
	assert.Equal(t, tokyoID, status.PlayerID, "the withdrawing competitor (Tokyo) must be the one marked ineligible")

	// The standings computation must credit the win to Osaka, not Tokyo.
	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	byID := map[string]state.PlayerStanding{}
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	assert.Equal(t, 1, byID[osakaID].Wins, "Osaka (the survivor) must be credited the win")
	assert.Equal(t, 0, byID[tokyoID].Wins, "Tokyo (the withdrawer) must not be credited a win")
}

// --- Finding 6: groupNeedsChusen must attribute daihyosen wins exactly as applyTiebreakSort does ---

// TestGroupNeedsChusen_HanteiDHWithNoWinnerID_AttributesBySide pinned, before
// the operator ruling bc-pnum, that a daihyosen bout with an unstamped
// WinnerID could still be attributed to a winner "by side" (resolveWinnerSide
// used to fall back to a name/side comparison when WinnerID was empty). That
// fallback no longer exists: resolveWinnerSide is id-only now (engi.go) --
// `if m.WinnerID == "" { return false, false }`, full stop, regardless of
// what Winner/SideA/SideB say. Converted to assert the opposite: a fully
// decisive round whose bouts all lack a WinnerID contributes NO wins to
// anyone, so every member ties at 0 and the group correctly reads as still
// needing a chusen -- an empty id resolves to nothing, even when the round
// was genuinely decisive on paper.
func TestGroupNeedsChusen_HanteiDHWithNoWinnerID_NeedsChusen(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-dojo-a", Name: "Team X", Dojo: "Dojo A"}},
		{Player: domain.Player{ID: "id-dojo-b", Name: "Team X", Dojo: "Dojo B"}},
		{Player: domain.Player{ID: "id-dojo-c", Name: "Team X", Dojo: "Dojo C"}},
	}
	// Every bout: WinnerID empty, Winner name ambiguously equal to BOTH
	// sides (same display name "Team X" throughout) -- the exact
	// unstamped-hantei shape. On paper Dojo A wins both her bouts and Dojo B
	// wins hers (a strict 2-1-0 order), but none of that reaches WinnerID.
	dh := func(idx int, sideAID, sideBID string) state.MatchResult {
		return state.MatchResult{
			ID:    fmt.Sprintf("Pool A-DH-%d", idx),
			SideA: "Team X", SideAID: sideAID,
			SideB: "Team X", SideBID: sideBID,
			Winner: "Team X", WinnerID: "",
			Status: state.MatchStatusCompleted,
		}
	}
	matches := []state.MatchResult{
		dh(0, "id-dojo-a", "id-dojo-b"), // A beats B on paper
		dh(1, "id-dojo-a", "id-dojo-c"), // A beats C on paper
		dh(2, "id-dojo-b", "id-dojo-c"), // B beats C on paper
	}
	// No WinnerID anywhere: every member accrues 0 wins and ties, so a
	// chusen is (correctly) still needed despite the round being complete.
	assert.True(t, groupNeedsChusen(group, matches, nil),
		"a daihyosen round with no WinnerID anywhere resolves no wins at all, so every member ties and still needs a chusen")
}

// --- Finding 7: GenerateSwissRound must tally a resolvable winner independently of the other side ---

// TestGenerateSwissRound_WinnerTalliedEvenWhenOpponentRemoved is the bead's
// repro: round 1 pairs A vs B (B wins), C vs D (C wins), and gives E a bye.
// The roster is hand-edited to remove A before round 2 is generated. Before
// the fix, the prior-match loop's `if !okA { continue }` skipped the ENTIRE
// row the instant side A failed to resolve against the (now A-less) current
// roster -- including the winner tally -- so B's round-1 win was silently
// dropped. B was then mis-seeded into the same win-bracket as D (0 wins)
// instead of alongside the other genuine round-1 winners (C, E), producing a
// different (and wrong) round-2 pairing.
func TestGenerateSwissRound_WinnerTalliedEvenWhenOpponentRemoved(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-removed-opponent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Swiss Removed Opponent", Format: state.CompFormatSwiss,
		Status: state.CompStatusPools, Courts: []string{"A"}, SwissRounds: 3,
	}))

	idA := helper.NewUUID4()
	idB := helper.NewUUID4()
	idC := helper.NewUUID4()
	idD := helper.NewUUID4()
	idE := helper.NewUUID4()

	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: idA, Name: "A", Dojo: "DojoA"},
		{ID: idB, Name: "B", Dojo: "DojoB"},
		{ID: idC, Name: "C", Dojo: "DojoC"},
		{ID: idD, Name: "D", Dojo: "DojoD"},
		{ID: idE, Name: "E", Dojo: "DojoE"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Swiss-R1-0", SideA: "A", SideAID: idA, SideB: "B", SideBID: idB,
			Winner: "B", WinnerID: idB, Status: state.MatchStatusCompleted, Court: "A"},
		{ID: "Swiss-R1-1", SideA: "C", SideAID: idC, SideB: "D", SideBID: idD,
			Winner: "C", WinnerID: idC, Status: state.MatchStatusCompleted, Court: "A"},
		// E's round-1 bye: an auto-completed win, no SideB.
		{ID: "Swiss-R1-2", SideA: "E", SideAID: idE, SideB: "",
			Winner: "E", WinnerID: idE, Status: state.MatchStatusCompleted, Court: "A"},
	}))

	// Hand-edit: remove A from the roster before round 2.
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: idB, Name: "B", Dojo: "DojoB"},
		{ID: idC, Name: "C", Dojo: "DojoC"},
		{ID: idD, Name: "D", Dojo: "DojoD"},
		{ID: idE, Name: "E", Dojo: "DojoE"},
	}))

	matches, err := eng.GenerateSwissRound(compID, 2)
	require.NoError(t, err)
	require.NotEmpty(t, matches)

	// Find B's round-2 opponent (skip the bye row, which has no SideB/SideBID).
	var bOpponent string
	for _, m := range matches {
		if m.SideB == "" {
			continue
		}
		switch {
		case m.SideAID == idB:
			bOpponent = m.SideBID
		case m.SideBID == idB:
			bOpponent = m.SideAID
		}
	}
	require.NotEmpty(t, bOpponent, "B must be paired in round 2")
	assert.Equal(t, idC, bOpponent, "B (round-1 winner, 1 win) must be paired with a fellow 1-win player (C), not with D (0 wins)")
}

// --- Finding 8: markTiedStandingsLeague must resolve id-less namesakes via
// the SAME roster order computeStandingsFrom used ---
//
// The three tests that used to live here (TestComputeStandingsFrom_League_
// IDlessNamesakeDoesNotSuppressUnrelatedTie,
// TestComputeStandingsFrom_League_IDCarryingRosterIDlessMatchRowsResolveByRosterOrder,
// TestMarkTiedStandingsLeague_IDlessNamesakeBucketsStaySeparate) all pinned
// resolution of an ID-LESS match row via a name-based rosterIndex/
// CompetitorKey lookup -- roster order vs. points order, and bucket
// separation for two id-less namesakes. The operator ruling bc-pnum removed
// that resolution path entirely: lookupStandingsPlayer is id-only, so an
// id-less match row (or an id-less roster entry) now resolves to NOTHING,
// regardless of which index performs the lookup or how the bucket is keyed.
// There is no replacement scenario for these tests to pin (a fixture with
// no ids anywhere no longer exercises rosterIndex threading at all), so
// they are deleted rather than converted.

// --- Finding 9: LoadOverrides errors must propagate, not be silently swallowed ---

// TestComputeStandingsFrom_CorruptOverrides_PropagatesError pins that a
// corrupt overrides.json surfaces as an error from computeStandingsFrom
// (via CalculatePoolStandings) rather than silently dropping every chusen
// override (the old `overrides, _ := e.store.LoadOverrides(compId)`).
func TestComputeStandingsFrom_CorruptOverrides_PropagatesError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "corrupt-overrides"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Corrupt Overrides", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice", Dojo: "A"}, {Name: "Bob", Dojo: "B"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
	}))

	corruptOverridesFile(t, store, compID)

	_, err := eng.CalculatePoolStandings(compID)
	assert.Error(t, err, "a corrupt overrides.json must surface as an error, not silently drop every override")
}

// --- Finding 12: dhCycleExists comment + tiebreakerPairKey/pairKey dedup ---

// TestPairKeyRemoved_UsesTiebreakerPairKey documents swiss.go's pairKey being
// deduplicated into tiebreakerPairKey (byte-identical bodies), by pinning the
// order-independence property both versions shared directly on
// tiebreakerPairKey. It does NOT exercise the Swiss pairing pipeline itself
// (which uses tiebreakerPairKey internally for rematch avoidance) and so
// cannot detect a reintroduced separate pairKey, or the pipeline drifting to
// call something else -- only Swiss-pipeline-level tests elsewhere in this
// package cover that.
func TestPairKeyRemoved_UsesTiebreakerPairKey(t *testing.T) {
	assert.Equal(t, tiebreakerPairKey("a", "b"), tiebreakerPairKey("b", "a"), "order-independent")
	assert.Equal(t, "a|b", tiebreakerPairKey("a", "b"))
}

// --- id-only resolution pins ---

// TestApplyPoolWrite_RestorePolicyIgnoresWinnerIDMismatch is finding 2 from
// the round-2 review: backfillMatchIdentity's unattributable-WinnerID
// rejection (finding 10, round 1) ran regardless of matchWritePolicy. The K3
// rollback (AlreadyIneligibleError) replays a TRUSTED stored snapshot via
// matchWriteRestore to undo a partial forward write; rollbackMatchResultTx
// only LOGS a restore failure, it never retries or otherwise recovers, so a
// prior snapshot whose own WinnerID happens not to match its own
// SideAID/SideBID (legacy data written before this validation existed) would
// have made the RESTORE itself fail, leaving the rejected forward write
// sitting on disk uncorrected. The rejection must apply ONLY to a forward
// (client) write, exactly like the sidesDisagree gate a few lines above it.
func TestApplyPoolWrite_RestorePolicyIgnoresWinnerIDMismatch(t *testing.T) {
	// `stored` holds whatever the rejected forward write left behind.
	stored := &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "id-alice", SideBID: "id-bob",
		Winner: "Alice", WinnerID: "id-alice", Status: state.MatchStatusCompleted,
	}
	// The snapshot to restore: legacy data whose own WinnerID does not match
	// either of its own side ids.
	prior := &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "id-alice", SideBID: "id-bob",
		Winner: "Alice", WinnerID: "some-stale-id", Status: state.MatchStatusCompleted,
	}

	mismatch, superseded, err := applyPoolWrite(stored, prior, matchWriteRestore)
	require.NoError(t, err, "a restore must never be rejected by the forward-only WinnerID check")
	assert.False(t, mismatch)
	assert.False(t, superseded)
	assert.Equal(t, "some-stale-id", stored.WinnerID, "the restore must actually land, replaying the prior verbatim")
}

// TestApplyPoolWrite_ForwardPolicyStillRejectsWinnerIDMismatch is the
// counterpart: a genuine client FORWARD write with an unattributable
// WinnerID must still be rejected exactly as finding 10 (round 1) fixed.
func TestApplyPoolWrite_ForwardPolicyStillRejectsWinnerIDMismatch(t *testing.T) {
	stored := &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "id-alice", SideBID: "id-bob",
		Status: state.MatchStatusScheduled,
	}
	forward := &state.MatchResult{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "id-alice", SideBID: "id-bob",
		Winner: "Alice", WinnerID: "not-a-side-id", Status: state.MatchStatusCompleted,
	}
	_, _, err := applyPoolWrite(stored, forward, matchWriteForward)
	require.Error(t, err, "a client forward write naming an unattributable winnerId must still be rejected")
}

// TestCompletedPoolNames_CorruptOverrides_PropagatesError is finding 5 from
// the round-2 review: knockout.go's completedPoolNames LoadOverrides
// propagation had no dedicated regression test (reverting it stayed green).
// A team competition's completedPoolNames unconditionally consults overrides
// (to gate a pool whose daihyosen cycle isn't yet broken), and a corrupt
// overrides.json there must surface as an error, not silently read as "no
// overrides" and let a cyclic pool falsely read as complete.
func TestCompletedPoolNames_CorruptOverrides_PropagatesError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "completed-pool-corrupt-overrides"
	// Engi=true so CalculatePoolStandings (called by InjectPoolDaihyosenMatches
	// and directly below) dispatches to computeEngiStandings, which never
	// touches overrides.json -- isolating THIS function's own LoadOverrides
	// call from the one computeStandingsFrom already propagates (fixed and
	// pinned separately), so a revert of THIS site alone is what turns this
	// test red.
	comp := &state.Competition{
		ID: compID, Name: "Corrupt Overrides Team", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 2, Kind: "team",
		Engi: true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "TeamA", Dojo: "DojoA"}, {Name: "TeamB", Dojo: "DojoB"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideB: "TeamB", Winner: "TeamA", Status: state.MatchStatusCompleted},
	}))

	corruptOverridesFile(t, store, compID)

	_, err := eng.completedPoolNames(compID, comp)
	assert.Error(t, err, "a corrupt overrides.json must surface as an error from completedPoolNames, not be silently dropped")
}

// TestMaybeAutoCompletePools_CorruptOverrides_PropagatesError is finding 5's
// second site: competition.go's MaybeAutoCompletePools LoadOverrides
// propagation, reached once a team competition's regular matches are all
// complete and at least one pool daihyosen bout has been scored (the
// dhCycleExists guard consults overrides to honour any already-recorded
// chusen). Team-LEAGUE format is used because MIXED short-circuits to
// advanceMixedPools before this code, and because LeagueTiebreakCandidates
// (checked first for a team league) reports no candidates here -- the
// regular match is a clean win, not a tie -- so execution falls through to
// the target LoadOverrides call rather than returning early.
func TestMaybeAutoCompletePools_CorruptOverrides_PropagatesError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-complete-corrupt-overrides"
	// Engi=true for the same isolation reason as TestCompletedPoolNames_CorruptOverrides_PropagatesError:
	// CalculatePoolStandings (called by LeagueTiebreakCandidates and directly
	// below) dispatches to computeEngiStandings, which never touches
	// overrides.json, so only THIS function's own LoadOverrides call can turn
	// this test red on a revert.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Corrupt Overrides Auto Complete", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 2, Kind: "team",
		Engi: true,
	}))
	// SideAID/SideBID/WinnerID and roster ids are stamped (operator ruling
	// bc-pnum): leagueGroupHasDH resolves the DH row against the group's
	// OWN member ids (groupMemberIDs, id-only), so an id-less roster
	// and DH row would never resolve, and MaybeAutoCompletePools would
	// short-circuit at AwaitingLeagueTiebreak BEFORE ever reaching the
	// LoadOverrides call this test targets -- for a reason unrelated to
	// what it is testing.
	teamAID, teamBID := "team-a-id", "team-b-id"
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: teamAID, Name: "TeamA", Dojo: "DojoA"}, {ID: teamBID, Name: "TeamB", Dojo: "DojoB"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideAID: teamAID, SideB: "TeamB", SideBID: teamBID,
			Winner: "TeamA", WinnerID: teamAID, Status: state.MatchStatusCompleted},
		{ID: "Pool A-DH-0", SideA: "TeamA", SideAID: teamAID, SideB: "TeamB", SideBID: teamBID,
			Winner: "TeamA", WinnerID: teamAID, Status: state.MatchStatusCompleted},
	}))

	corruptOverridesFile(t, store, compID)

	_, err := eng.MaybeAutoCompletePools(compID)
	assert.Error(t, err, "a corrupt overrides.json must surface as an error from MaybeAutoCompletePools, not be silently dropped")
}

// --- Finding 8 / nit 16: ReplaceParticipantInDraw's bracket-ambiguity check ---

// TestReplaceParticipantInDraw_NamesakeAbsentFromBracketCascadesWithoutWarning
// pins the second-Opus-pass fix for item 2: the ambiguity check now collects
// every name actually appearing in bracket.json FIRST, and only loads
// participants (and scans for a namesake) when oldName is one of them. Alice
// (being renamed) sits only in pools.csv here; the crafted bracket names two
// entirely unrelated players (Bob, Charlie), so oldName ("Alice") is not one
// of the bracket's own names at all.
//
// The warnings-empty assertion below does NOT by itself distinguish the fix
// from the pre-fix code: bracketNameAmbiguous is gated on bracketFound too
// (a match must actually SAY "Alice" for the warning to fire), and neither
// crafted row does, so even the old always-scan-when-non-empty code produces
// no warning here (verified by mutation). What the fix actually changes is
// whether the SCAN runs at all: a corrupted (permission-stripped)
// participants.csv is used as the discriminator, mirroring
// corruptParticipantsFile's technique below -- the old code loaded
// participants unconditionally whenever the bracket was non-empty and would
// have surfaced that I/O error; the fix's pass-1 bracketNames check must
// skip the load entirely when oldName isn't a bracket name, so the rename
// still succeeds even with participants.csv unreadable.
func TestReplaceParticipantInDraw_NamesakeAbsentFromBracketCascadesWithoutWarning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-namesake-absent-from-bracket"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Absent From Bracket", Kind: "individual",
		Format: state.CompFormatMixed, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))

	aliceID := helper.NewUUID4()
	namesakeID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo0"},
		{ID: namesakeID, Name: "Alice", Dojo: "DojoOther"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: aliceID, Name: "Alice", Dojo: "Dojo0"},
			{ID: namesakeID, Name: "Alice", Dojo: "DojoOther"},
		}},
	}))
	// Neither Alice's name is anywhere in this bracket -- it names two
	// completely unrelated players who advanced from a different pool.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "R1-0", SideA: "Bob", SideB: "Charlie", Winner: "Bob", Status: state.MatchStatusCompleted}},
		},
	}))

	// participants.csv is unreadable: the fix must never attempt to load it
	// for this rename, since oldName is not a candidate bracket name.
	corruptParticipantsFile(t, store, compID)

	warnings, err := eng.ReplaceParticipantInDraw(compID, aliceID, "Alice", "Dojo0", "", "Alicia", "Dojo0", "")
	require.NoError(t, err, "the pass-1 bracketNames check must skip the participants.csv load entirely for a name the bracket never uses")
	assert.Empty(t, warnings)

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.Len(t, poolsAfter, 1)
	byID := map[string]helper.Player{}
	for _, p := range poolsAfter[0].Players {
		byID[p.ID] = p
	}
	assert.Equal(t, "Alicia", byID[aliceID].Name, "the renamed participant's own pools row must cascade")
	assert.Equal(t, "Alice", byID[namesakeID].Name, "the namesake's pools row must be untouched")

	bracketAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, findNameInBracket(bracketAfter, "Alicia"), "the bracket never named Alice at all, so nothing there should change")
	assert.True(t, findNameInBracket(bracketAfter, "Bob"), "the crafted bracket must be untouched")
}

// TestReplaceParticipantInDraw_CheckedOutNamesakeBlocksRewrite is the
// second-Opus-pass repro: a namesake who WAS placed in the bracket at draw
// time (her name is one of the two "Alice" rows below, indistinguishable
// from the renamed Alice's own row since bracket.json carries no per-side
// id) has since been checked OUT -- legal while draw-ready. The OLD
// filterCheckedIn-scoped ambiguity check would have excluded her from the
// candidate pool at rename time (she is no longer in today's checked-in
// snapshot) and silently rewritten BOTH "Alice" rows to "Alicia" -- the
// exact corruption this guard exists to prevent, striking the namesake's
// own match history along with the rename. The fix scans the FULL current
// roster (unfiltered by check-in) once bracket.json is known to contain
// oldName at all, so a checked-out namesake is found just as reliably as a
// checked-in one, and the rewrite is correctly blocked with a warning
// instead.
func TestReplaceParticipantInDraw_CheckedOutNamesakeBlocksRewrite(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-checked-out-namesake"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Checked Out Namesake", Kind: "individual",
		Format: state.CompFormatPlayoffs, Courts: []string{"A"},
		StartTime: "09:00", Status: state.CompStatusDrawReady,
	}))

	aliceID := helper.NewUUID4()
	namesakeID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo0", CheckedIn: true},
		// Was checked in and drawn at draw time; checked OUT since (legal in
		// draw-ready). She still exists in the roster and her bracket row
		// (from when she WAS drawn) is still sitting in bracket.json below.
		{ID: namesakeID, Name: "Alice", Dojo: "DojoOther", CheckedIn: false},
	}))
	// Both "Alice" rows were real at draw time; there is no id on either to
	// tell them apart now.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "R1-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
				{ID: "R1-1", SideA: "Alice", SideB: "Charlie", Winner: "Charlie", Status: state.MatchStatusCompleted},
			},
		},
	}))

	warnings, err := eng.ReplaceParticipantInDraw(compID, aliceID, "Alice", "Dojo0", "", "Alicia", "Dojo0", "")
	require.NoError(t, err)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "ambiguous across dojos", "a checked-out namesake must still be found and block the rewrite")

	bracketAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, findNameInBracket(bracketAfter, "Alicia"), "neither row may be rewritten while the namesake makes both ambiguous")
	assert.True(t, findNameInBracket(bracketAfter, "Alice"), "both original rows must survive untouched, including the checked-out namesake's own match history")
}

// corruptParticipantsFile forces LoadParticipants to error on compID's
// participants.csv. Unlike corruptOverridesFile (malformed JSON), malformed
// CSV content is not reliable here: helper.ReadCSVFile deliberately parses
// with LazyQuotes/FieldsPerRecord=-1 (participants.csv loading "stays
// tolerant on purpose so the roster can be repaired", CLAUDE.md), so
// syntactically broken CSV bytes are silently tolerated rather than
// rejected. Stripping read permission forces a genuine I/O error
// regardless of content. The permission strip must follow a fresh write
// (not just chmod on the existing file): loadParticipantsNoLock caches on
// participants.csv's mtime, so without a real content rewrite a warm cache
// from an earlier read in the same test would keep serving the pre-corruption
// data and never touch the now-unreadable file at all.
func corruptParticipantsFile(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	path := filepath.Join(store.GetFolder(), "competitions", compID, "participants.csv")
	require.NoError(t, os.WriteFile(path, []byte("unreadable\n"), 0o600))
	require.NoError(t, os.Chmod(path, 0o000))
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	// A root test runner ignores mode 0o000, and then the two tests built on
	// this helper pass under the old and the new code alike (a false pass, not
	// a false failure). Prove the corruption bites before any test acts on it.
	_, err := store.LoadParticipants(compID, false)
	require.Error(t, err, "participants.csv must be unreadable for this helper to discriminate anything; running as root defeats chmod")
}

// TestReplaceParticipantInDraw_EmptyBracketSkipsParticipantsLoad pins nit 16:
// the bracket-ambiguity check's LoadParticipants call must not run at all
// when the bracket is empty (League format never generates one), since there
// is nothing in bracket.json to rename or warn about. A corrupted
// participants.csv would surface as an error from the ambiguity check's own
// LoadParticipants call if it ran unconditionally; with the fix, the pools.csv
// rename still succeeds because the bracket branch is skipped entirely.
func TestReplaceParticipantInDraw_EmptyBracketSkipsParticipantsLoad(t *testing.T) {
	// setupDrawReadyMixed uses League format (createTestCompetition's
	// second call), which never generates an elimination bracket.
	eng, store, compID := setupDrawReadyMixed(t, []string{
		"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank",
	})

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Empty(t, bracket.Rounds, "League format must not generate an elimination bracket")
	require.Nil(t, bracket.ThirdPlaceMatch)

	// Resolve the id BEFORE corrupting the file: participantID does its own
	// LoadParticipants call, which must succeed to hand back a real pid.
	pid := participantID(t, store, compID, "Alice")
	corruptParticipantsFile(t, store, compID)

	warnings, err := eng.ReplaceParticipantInDraw(compID, pid, "Alice", "Dojo0", "", "Alicia", "Dojo0", "")
	require.NoError(t, err, "the empty-bracket branch must not touch the corrupted participants.csv at all")
	assert.Empty(t, warnings)

	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.False(t, findPlayerInPools(poolsAfter, "Alice"), "old name must be gone from pools")
	assert.True(t, findPlayerInPools(poolsAfter, "Alicia"), "new name must be present in pools")
}

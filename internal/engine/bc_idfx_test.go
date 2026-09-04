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

// --- Finding 1: newGroupKeyResolver must not fall through a foreign id to the name index ---

// TestNewGroupKeyResolver_NonMemberIDDoesNotFallThroughToName pins the
// resolver contract directly: a NON-EMPTY id that does not belong to any
// member of the group must resolve to ("", false), never fall through to the
// bare-name index. Only an EMPTY id takes the name path.
func TestNewGroupKeyResolver_NonMemberIDDoesNotFallThroughToName(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-x-a", Name: "X", Dojo: "Dojo A"}},
		{Player: domain.Player{ID: "id-y", Name: "Y", Dojo: "Dojo Y"}},
	}
	resolve := newGroupKeyResolver(group)

	// id-x-b is a REAL id, but belongs to X@DojoB, who is NOT a member of
	// this specific tied group (e.g. a stale TB row from before a score
	// correction moved the consequential tie). Resolving it must fail
	// outright rather than silently attributing it to X@DojoA merely
	// because they share the display name "X".
	key, ok := resolve("id-x-b", "X")
	assert.False(t, ok, "a non-member id must not resolve, even when the name matches a group member")
	assert.Empty(t, key)

	// An EMPTY id still takes the name path (legacy row with no id stamped).
	key, ok = resolve("", "X")
	assert.True(t, ok)
	assert.Equal(t, "id:id-x-a", key)
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

// TestNewGroupKeyResolver_FullyLegacyGroupFallsThroughForeignIDToName pins the
// bc-idfx review's nit 20 fix: the strict foreign-id rejection above only
// makes sense when the GROUP itself has at least one id-carrying member --
// otherwise there is nothing to check a bout's id against, ids simply are not
// authoritative for this group at all. A fully legacy group (neither member
// carries an id, e.g. a pool never regenerated since before ids were
// stamped) must fall through to the bare-name index even when the bout row
// happens to carry a non-empty id, rather than refusing to resolve a bout
// the name index could have handled just fine.
func TestNewGroupKeyResolver_FullyLegacyGroupFallsThroughForeignIDToName(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "X", Dojo: "Dojo A"}}, // no ID
		{Player: domain.Player{Name: "Y", Dojo: "Dojo Y"}}, // no ID
	}
	resolve := newGroupKeyResolver(group)

	// The bout row carries an id ("some-id") that belongs to NEITHER member
	// (neither has one at all). Because the GROUP has no ids to compare
	// against, this must fall through to the name index and resolve X by
	// name, not fail outright the way it would for an id-aware group.
	key, ok := resolve("some-id", "X")
	assert.True(t, ok, "a fully legacy group must fall through to the name index even when the bout row carries an id")
	assert.Equal(t, "name:X", key)
}

// TestApplyTiebreakSort_FullyLegacyGroupResolvesBoutWithStrayID is the
// end-to-end twin: a pool with no ids anywhere, but a supplementary bout row
// that (e.g. from a different data source, or hand-edited data) carries a
// non-empty SideAID. Before the nit-20 fix this bout would fail to resolve
// for either side and be silently skipped, leaving the tie unbroken even
// though the names alone are enough to identify both competitors.
//
// sorted starts as [X, Y] (input order) while Y is the BOUT WINNER: a
// resolved bout must move Y to first place, while a skipped bout (the
// pre-fix behaviour) leaves the input order untouched -- deliberately
// chosen so the two outcomes are distinguishable. An earlier draft of this
// test put the winner already in the position a no-op would also produce,
// which stayed green under the pre-fix mutation and pinned nothing; caught
// and fixed via mutation testing before landing.
func TestApplyTiebreakSort_FullyLegacyGroupResolvesBoutWithStrayID(t *testing.T) {
	sorted := []state.PlayerStanding{
		{Player: domain.Player{Name: "X", Dojo: "Dojo A"}, Points: 100},
		{Player: domain.Player{Name: "Y", Dojo: "Dojo Y"}, Points: 100},
	}
	matches := []state.MatchResult{
		{
			ID:    "Pool P-TB-0",
			SideA: "X", SideAID: "some-stray-id", // stray id, group has none
			SideB:  "Y",
			Winner: "Y",
			Status: state.MatchStatusCompleted,
		},
	}

	applyTiebreakSort(sorted, matches, IsTiebreakerMatchID)

	require.Len(t, sorted, 2)
	assert.Equal(t, "Y", sorted[0].Player.Name, "Y won the TB bout and must sort first, even though SideA's row carried a stray id the group has nothing to compare it against")
	assert.Equal(t, "X", sorted[1].Player.Name)
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

// TestComputeStandingsFrom_OverrideSort_IDlessNamesakesDoNotCollideOnNaturalRank
// is the BLOCKER from the Opus review round 2 of this bead: naturalRank was a
// map keyed by standingsPlayerKey(ID, Name), so two id-less namesakes (legal
// across dojos, CheckDuplicateEntriesByNameDojo) collapse onto ONE map entry
// -- whichever is processed LAST in points-sorted order overwrites the
// natural rank the FIRST one had just written. An unrelated override
// elsewhere in the pool is enough to expose it: the undefeated leader's own
// natural rank silently becomes her lower-placed namesake's, and she sorts
// below rows that never legitimately outrank her.
//
// Fixture: TanakaGhost (0-0-0, registered FIRST in roster order) and
// TanakaReal (2-0, undefeated leader, registered LAST) share the name
// "Tanaka" across different dojos and carry no id. Because match sides are
// id-less, both "Tanaka"-named match rows resolve via the roster's
// last-write-wins name index to TanakaReal, exactly as computeStandingsFrom's
// own win/loss accrual already does -- TanakaGhost genuinely never appears in
// a match and stays at 0-0-0. Carol carries an override unrelated to either
// Tanaka. Before the fix this drops the 2-0 leader to rank 3 (probe-verified);
// after it she is rank 1.
func TestComputeStandingsFrom_OverrideSort_IDlessNamesakesDoNotCollideOnNaturalRank(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-idless-namesake-collision"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "IDless Namesake Collision", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	// Roster order matters: TanakaGhost registered BEFORE TanakaReal, neither
	// carries an id (legacy data).
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Tanaka", Dojo: "DojoGhost"},
			{Name: "Bob", Dojo: "DojoBob"},
			{Name: "Carol", Dojo: "DojoCarol"},
			{Name: "Tanaka", Dojo: "DojoReal"},
		}},
	}))
	// "Tanaka" always resolves to the last-registered roster entry
	// (TanakaReal), exactly as the standings accrual itself resolves it.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideB: "Bob", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Tanaka", SideB: "Carol", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Carol", Winner: "Bob", Status: state.MatchStatusCompleted},
	}))
	// Carol's override is unrelated to either Tanaka; its mere presence is
	// enough to enter the override-sort code path.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "", "Carol", "DojoCarol", 2))

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
	assert.Equal(t, 1, byDojo["DojoReal"].Rank, "the undefeated leader must be rank 1, not collide with her id-less namesake's natural rank")
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

// TestGroupNeedsChusen_HanteiDHWithNoWinnerID_AttributesBySide is the bead's
// repro: three "Team X" (a namesake collision reachable through the
// documented checkNewTeamNameCollisions restore hole) are tied and play a
// full daihyosen round-robin, every bout decided by hantei with WinnerID left
// empty (the exact shape RecordDecisionTx produced before finding 5's fix).
// Before this fix, groupNeedsChusen resolved each bout's winner via
// resolve(m.WinnerID, m.Winner) directly: with WinnerID empty and every row
// sharing the ambiguous name "Team X", that falls straight to the group's
// bare-name index, which returns the SAME single member (whoever is
// registered LAST) for every bout, regardless of which two members actually
// played it or which SIDE the row's own ids name as the winner. That
// phantom-credits Dojo C three times (even for the two bouts she isn't even
// in) and leaves Dojo A and Dojo B tied at zero, so a genuinely decisive
// round (A beats B, A beats C, B beats C: strict order 2-1-0) reads as an
// unresolved duplicate and wrongly surfaces a chusen. After the fix,
// attribution mirrors applyTiebreakSort exactly (resolveWinnerSide over the
// SIDE ids + resolveGroupMatchKey), producing the correct strict order.
func TestGroupNeedsChusen_HanteiDHWithNoWinnerID_AttributesBySide(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-dojo-a", Name: "Team X", Dojo: "Dojo A"}},
		{Player: domain.Player{ID: "id-dojo-b", Name: "Team X", Dojo: "Dojo B"}},
		{Player: domain.Player{ID: "id-dojo-c", Name: "Team X", Dojo: "Dojo C"}},
	}
	// Every bout: WinnerID empty, Winner name ambiguously equal to BOTH
	// sides (same display name "Team X" throughout) -- the exact
	// unstamped-hantei shape. Dojo A is SideA in both her bouts and wins
	// both; Dojo B is SideA against Dojo C and wins.
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
		dh(0, "id-dojo-a", "id-dojo-b"), // A beats B
		dh(1, "id-dojo-a", "id-dojo-c"), // A beats C
		dh(2, "id-dojo-b", "id-dojo-c"), // B beats C
	}
	// Strict 2-1-0 order (A=2, B=1, C=0): decisive, no chusen needed.
	assert.False(t, groupNeedsChusen(group, matches, nil),
		"a decisive strict-order daihyosen round must not need a chusen, even when every bout's WinnerID is unstamped")
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

// --- Finding 8: markTiedStandingsLeague must resolve id-less namesakes via the SAME roster order computeStandingsFrom used ---

// TestComputeStandingsFrom_League_IDlessNamesakeDoesNotSuppressUnrelatedTie is
// the bead's repro: a legacy id-less league roster with two "Tanaka" entries
// (last-write-wins resolution depends on roster ORDER) must not leave a real,
// unrelated Suzuki/Yamada tie unmarked. Before the fix, markTiedStandingsLeague
// rebuilt its own identity index from the POINTS-sorted `sorted` slice rather
// than reusing computeStandingsFrom's roster-order index, so the two id-less
// Tanaka rows could resolve to a DIFFERENT namesake than the one whose
// Wins/Losses the original computation actually credited -- corrupting the
// per-competitor completion counters used by the emerging-tie trigger and, in
// this fixture, keeping the trigger from ever firing.
func TestComputeStandingsFrom_League_IDlessNamesakeDoesNotSuppressUnrelatedTie(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-idless-namesake"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Idless Namesake League", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	// Roster order matters: Tanaka1 (dojo D1) registered BEFORE Tanaka2
	// (dojo D2), neither carries an id (legacy data).
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Tanaka", Dojo: "D1"},
			{Name: "Tanaka", Dojo: "D2"},
			{Name: "Suzuki", Dojo: "D3"},
			{Name: "Yamada", Dojo: "D4"},
			{Name: "Filler", Dojo: "D5"},
		}},
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		// Tanaka's one completed win (id-less name resolves to the LAST
		// registered "Tanaka" in roster order -- Tanaka@D2 -- exactly as
		// computeStandingsFrom's own original resolution does).
		{ID: "Pool A-0", SideA: "Tanaka", SideB: "Filler", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		// Suzuki and Yamada's fixtures against Filler are still SCHEDULED
		// (not yet played), so neither individually satisfies the
		// emerging-tie trigger on her own.
		{ID: "Pool A-1", SideA: "Suzuki", SideB: "Filler", Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Yamada", SideB: "Filler", Status: state.MatchStatusScheduled},
		// Suzuki and Yamada draw each other (completed): they finish TIED,
		// a real, consequential tie within the default top-3 band.
		{ID: "Pool A-3", SideA: "Suzuki", SideB: "Yamada", Winner: "", Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 5)

	tiedNames := map[string]bool{}
	for _, s := range poolA {
		if s.Tied {
			tiedNames[s.Player.Dojo] = true
		}
	}
	assert.True(t, tiedNames["D3"] || tiedNames["D4"], "the real Suzuki/Yamada tie must be marked once the round-1 Tanaka winner's own fixtures (her only fixture) are all complete")
	assert.True(t, tiedNames["D3"] && tiedNames["D4"], "both Suzuki and Yamada must be marked, not just one")
}

// TestComputeStandingsFrom_League_IDCarryingRosterIDlessMatchRowsResolveByRosterOrder
// is the round-2 review's requested companion to the test above: an
// ID-CARRYING roster (every participant has a real UUID) whose MATCH ROWS
// still lack side ids (SideAID/SideBID empty on the wire -- legacy data
// written before that stamping existed, or any other id-less-row shape). A
// MatchResult side carries a bare NAME only, no dojo, so resolving an
// id-less row still depends on which identity index performs the lookup,
// exactly as it does for a fully id-less roster: roster order (which
// computeStandingsFrom's own accrual used) and points order (what a locally
// rebuilt index would produce) diverge the moment any match is played.
// Unlike the id-less-roster case, an id-carrying mismatch here has no
// coincidental save -- CompetitorKey is id-preferred, so crediting the WRONG
// same-named competitor lands the completion count in a bucket keyed by her
// own distinct real id, an unambiguous wrong answer. This is the test that
// actually needs rosterIndex (mutation-verified: rebuilding a local
// points-order index in its place turns this test red).
func TestComputeStandingsFrom_League_IDCarryingRosterIDlessMatchRowsResolveByRosterOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-idcarrying-idless-rows"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "ID-carrying Roster, ID-less Rows", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))

	// Every participant has a real id; roster order still matters, because
	// the MATCH ROWS below carry none.
	tanakaGhostID := helper.NewUUID4()
	tanakaRealID := helper.NewUUID4()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: tanakaGhostID, Name: "Tanaka", Dojo: "D1"},
			{ID: tanakaRealID, Name: "Tanaka", Dojo: "D2"},
			{ID: helper.NewUUID4(), Name: "Suzuki", Dojo: "D3"},
			{ID: helper.NewUUID4(), Name: "Yamada", Dojo: "D4"},
			{ID: helper.NewUUID4(), Name: "Filler", Dojo: "D5"},
		}},
	}))

	// No SideAID/SideBID on any row: id-less on the wire despite the roster
	// carrying real ids.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka", SideB: "Filler", Winner: "Tanaka", Status: state.MatchStatusCompleted},
		{ID: "Pool A-1", SideA: "Suzuki", SideB: "Filler", Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Yamada", SideB: "Filler", Status: state.MatchStatusScheduled},
		{ID: "Pool A-3", SideA: "Suzuki", SideB: "Yamada", Winner: "", Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 5)

	byID := map[string]state.PlayerStanding{}
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	require.Equal(t, 1, byID[tanakaRealID].Wins, "the id-less match row must still resolve to the roster-order Tanaka computeStandingsFrom itself credited")
	require.Equal(t, 0, byID[tanakaGhostID].Wins)

	tiedNames := map[string]bool{}
	for _, s := range poolA {
		if s.Tied {
			tiedNames[s.Player.Dojo] = true
		}
	}
	assert.True(t, tiedNames["D3"] && tiedNames["D4"], "both Suzuki and Yamada must be marked once the real Tanaka's own (id-less) fixture is correctly seen as complete")
}

// TestMarkTiedStandingsLeague_IDlessNamesakeBucketsStaySeparate is a
// WHITE-BOX isolation test for the "re-key" half of the round-2 review's
// finding 6 (the half the two tests above cannot reach): statusFor must
// bucket completion counts by helper.CompetitorKey(ID, Name, Dojo), not
// standingsPlayerKey(ID, Name), so two id-less namesakes from different
// dojos never share one completion bucket.
//
// This CANNOT be reproduced by driving real match data through
// computeStandingsFrom, and that is itself worth recording: id-less
// resolution (lookupStandingsPlayer's name-only branch) is a SINGLE map
// lookup keyed on the bare name, so every match naming that bare name
// resolves to the SAME one roster entry, regardless of which index performs
// the lookup or how many "Tanaka"-named matches exist. A second id-less
// "Tanaka" can therefore never accrue independently-attributable match
// activity through the realistic pipeline -- she is structurally always a
// zero-activity ghost, and merging a zero-activity ghost's bucket into
// another's changes nothing (the tests above prove rosterIndex is load-
// bearing, but cannot tell CompetitorKey and standingsPlayerKey apart:
// mutation-verified, reverting bucketing alone to standingsPlayerKey while
// keeping rosterIndex leaves both green).
//
// This test instead calls markTiedStandingsLeague directly with a
// HAND-BUILT rosterIndex carrying explicit id-keyed entries for both
// Tanakas (simulating a resolution layer able to tell them apart, which is
// what matters for isolating the BUCKET STORAGE question), so each can
// carry her OWN genuine, independent, and different completion state:
// Tanaka@DojoA's own fixture is complete; Tanaka@DojoB's is not. With
// buckets correctly separated, Tanaka@DojoA (checked first, topN=1) fires
// the trigger on her own genuinely complete fixture. With buckets merged
// (the reverted standingsPlayerKey scheme), her bucket also carries
// Tanaka@DojoB's incomplete fixture, so the merged total no longer equals
// the merged completed count, and the trigger wrongly fails to fire.
func TestMarkTiedStandingsLeague_IDlessNamesakeBucketsStaySeparate(t *testing.T) {
	comp := &state.Competition{Format: state.CompFormatLeague, LeagueTiebreakTopN: 1}

	// Both Tanakas are id-less in the DISPLAY data (sorted), mirroring real
	// legacy rosters; Player.ID is "" on both.
	tanakaA := domain.Player{Name: "Tanaka", Dojo: "DojoA"}
	tanakaB := domain.Player{Name: "Tanaka", Dojo: "DojoB"}
	other := domain.Player{Name: "Other", Dojo: "DojoC"}
	sorted := []state.PlayerStanding{
		{Player: tanakaA, Points: 100},
		{Player: tanakaB, Points: 100},
		{Player: other, Points: 50},
	}

	// Hand-built rosterIndex: id-keyed entries let each match below resolve
	// to a SPECIFIC Tanaka, bypassing the "one name, one slot" constraint
	// that a real id-less roster is subject to -- this isolates the bucket
	// storage question from the resolution question the other two tests
	// already cover.
	rosterIndex := map[string]*state.PlayerStanding{
		"id:lookup-tanaka-a": &sorted[0],
		"id:lookup-tanaka-b": &sorted[1],
		"name:Other":         &sorted[2],
	}

	regularMatches := []state.MatchResult{
		// Tanaka@DojoA's OWN fixture: complete.
		{ID: "Pool A-0", SideAID: "lookup-tanaka-a", SideA: "Tanaka", SideB: "Other", Status: state.MatchStatusCompleted},
		// Tanaka@DojoB's OWN fixture: still scheduled.
		{ID: "Pool A-1", SideAID: "lookup-tanaka-b", SideA: "Tanaka", SideB: "Other", Status: state.MatchStatusScheduled},
	}

	markTiedStandingsLeague(comp, sorted, regularMatches, rosterIndex)

	assert.True(t, sorted[0].Tied, "Tanaka@DojoA finished her own (and only her own) fixture; the trigger must fire on her genuinely complete bucket")
	assert.True(t, sorted[1].Tied, "the whole tied group (both Tanakas, tied on Points) is marked once the trigger fires")
}

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

// --- Opus review round 2 ---

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
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "TeamA", Dojo: "DojoA"}, {Name: "TeamB", Dojo: "DojoB"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideB: "TeamB", Winner: "TeamA", Status: state.MatchStatusCompleted},
		{ID: "Pool A-DH-0", SideA: "TeamA", SideB: "TeamB", Winner: "TeamA", Status: state.MatchStatusCompleted},
	}))

	corruptOverridesFile(t, store, compID)

	_, err := eng.MaybeAutoCompletePools(compID)
	assert.Error(t, err, "a corrupt overrides.json must surface as an error from MaybeAutoCompletePools, not be silently dropped")
}

// --- Finding 8 / nit 16: ReplaceParticipantInDraw's bracket-ambiguity check ---

// TestReplaceParticipantInDraw_NamesakeExcludedFromDrawCascadesWithoutWarning
// pins the bc-idfx review's item 8 fix: bracketNameAmbiguous used to scan the
// FULL roster for a namesake, including a participant who was never placed in
// the bracket at all (excluded by GenerateDraw's own filterCheckedIn opt-in
// check-in filter). A namesake who only ever existed in participants.csv can
// never occupy a bracket row, so counting her toward ambiguity produced a
// false "ambiguous across dojos" warning and silently skipped a rename that
// was actually perfectly safe. The fix scopes the candidate pool to
// filterCheckedIn(participants) -- the same roster GenerateDraw itself built
// the bracket from.
func TestReplaceParticipantInDraw_NamesakeExcludedFromDrawCascadesWithoutWarning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "replace-checkin-excluded-namesake"
	comp := &state.Competition{
		ID: compID, Name: "Checkin Namesake", Kind: "individual",
		Format: state.CompFormatPlayoffs, Courts: []string{"A"},
		StartTime: "09:00", Status: "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo0", CheckedIn: true},
		{Name: "Bob", Dojo: "Dojo1", CheckedIn: true},
		{Name: "Charlie", Dojo: "Dojo2", CheckedIn: true},
		{Name: "Dave", Dojo: "Dojo3", CheckedIn: true},
		// Same display name as the drawn Alice, different dojo, NOT checked
		// in. filterCheckedIn's opt-in semantics (at least one participant
		// above IS checked in) excludes her from GenerateDraw entirely, so
		// she can never appear as a bracket row.
		{Name: "Alice", Dojo: "DojoExcluded", CheckedIn: false},
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))

	bracketBefore, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, findNameInBracket(bracketBefore, "Alice"), "the checked-in Alice must be in the bracket before the rename")

	all, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	var drawnAliceID string
	for _, p := range all {
		if p.Name == "Alice" && p.Dojo == "Dojo0" {
			drawnAliceID = p.ID
		}
	}
	require.NotEmpty(t, drawnAliceID, "the checked-in Alice must have a minted id")

	warnings, err := eng.ReplaceParticipantInDraw(compID, drawnAliceID, "Alice", "Dojo0", "", "Alicia", "Dojo0", "")
	require.NoError(t, err)
	assert.Empty(t, warnings, "the checked-in-out namesake must not trigger a false bracket-ambiguity warning")

	bracketAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.False(t, findNameInBracket(bracketAfter, "Alice"), "old name must be cascaded away, not left ambiguous")
	assert.True(t, findNameInBracket(bracketAfter, "Alicia"), "new name must appear in the bracket after the rename")
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

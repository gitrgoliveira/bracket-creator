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

// TestPairKeyRemoved_UsesTiebreakerPairKey pins that swiss.go's pairKey was
// deduplicated into tiebreakerPairKey (byte-identical bodies): the Swiss
// pairing pipeline (which uses pairKey internally for rematch avoidance) must
// still behave identically once the duplicate is removed. This test exercises
// tiebreakerPairKey directly for order-independence, the property both
// versions shared.
func TestPairKeyRemoved_UsesTiebreakerPairKey(t *testing.T) {
	assert.Equal(t, tiebreakerPairKey("a", "b"), tiebreakerPairKey("b", "a"), "order-independent")
	assert.Equal(t, "a|b", tiebreakerPairKey("a", "b"))
}

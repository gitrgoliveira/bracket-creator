// Package engine, bc_pnum_review_test.go: regression tests for the bc-pnum
// adversarial review of the bc-brid bracket-id work. Every finding below is
// the SAME mistake: a writer that sets a bracket side or winner NAME without
// setting the matching id, so an id-first reader (which now beats a name
// whenever they disagree) answers with a definitively WRONG competitor
// instead of the one the name-only era would have guessed.
package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Finding F1: OverrideBracketWinner must set WinnerID alongside Winner ---

// TestOverrideBracketWinner_SetsMatchingWinnerID is F1's primary repro: a
// plain 4-player playoffs bracket with no same-name pair and no legacy data.
// Before the fix, OverrideBracketWinner's round branch did `m.Winner =
// winnerName` with no WinnerID assignment, so correcting the winner from
// SideA to SideB left the row holding SideB's NAME with SideA's stale ID
// (propagateBracketWinner only copies WinnerID forward, it never derives
// one), and the final's side inherited both. An id-first reader (e.g.
// GetBracketRanking) then answers with the ELIMINATED competitor.
func TestOverrideBracketWinner_SetsMatchingWinnerID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-sets-winnerid"

	createTestCompetition(t, store, compID, "playoffs", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m0 := bracket.Rounds[0][0] // Alice (SideA) vs Bob (SideB)
	require.NotEmpty(t, m0.SideAID)
	require.NotEmpty(t, m0.SideBID)

	// First override: Alice (SideA) wins. WinnerID must match her id, not be
	// left over from whatever the row held before (nothing, here — but this
	// establishes the baseline the correction below overturns).
	_, err = eng.OverrideBracketWinner(compID, m0.ID, "Alice", 0)
	require.NoError(t, err)
	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, m0.SideAID, reloaded.Rounds[0][0].WinnerID, "Alice's own id must be stamped as WinnerID")

	// Correction: the operator flips the winner to Bob (SideB). The bug: the
	// pre-fix code left WinnerID at Alice's id (SideAID) while Winner now
	// reads "Bob" — an id-first reader would resolve this row to Alice, the
	// loser.
	_, err = eng.OverrideBracketWinner(compID, m0.ID, "Bob", 0)
	require.NoError(t, err)
	reloaded, err = store.LoadBracket(compID)
	require.NoError(t, err)
	corrected := reloaded.Rounds[0][0]
	assert.Equal(t, "Bob", corrected.Winner)
	assert.Equal(t, m0.SideBID, corrected.WinnerID, "WinnerID must follow the corrected winner (Bob), never stay pinned to the previous winner's id")
	assert.NotEqual(t, m0.SideAID, corrected.WinnerID, "the eliminated competitor's id must not survive as WinnerID")

	// The final must inherit the CORRECTED winner's id, not the stale one.
	assert.Equal(t, m0.SideBID, reloaded.Rounds[1][0].SideAID, "the final's side id must reflect Bob (the corrected winner), not Alice")
}

// TestOverrideBracketWinner_SameNamePairing_WinnerIDLeftEmpty is F1's
// explicitly-required companion case: the override API takes a NAME only, so
// a same-name pairing (two competitors sharing a display name from different
// dojos, legal per CheckDuplicateEntriesByNameDojo) cannot be told apart by
// name alone. The fix must resolve this to an EMPTY id, never guess a side
// (domain.AttributeWinnerSide's own sideA-first tie-break would be exactly
// such a guess, which is why this call site does not reuse it directly).
func TestOverrideBracketWinner_SameNamePairing_WinnerIDLeftEmpty(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "override-samename-empty-id"

	tokyoID := helper.NewUUID4()
	osakaID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Override Same Name", Format: state.CompFormatPlayoffs,
		Courts: []string{"A"}, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			// A STALE prior WinnerID (Tokyo's, from some earlier resolution)
			// is already sitting on the row -- the only way a naive
			// `m.Winner = winnerName` (with WinnerID left untouched) could be
			// caught red-handed here: an override that never sets WinnerID at
			// all would leave this stale id in place rather than clearing it.
			{{ID: "m-r1-0", SideA: "Tanaka Kenji", SideAID: tokyoID, SideB: "Tanaka Kenji", SideBID: osakaID,
				Winner: "Tanaka Kenji", WinnerID: tokyoID, Status: state.MatchStatusRunning, Court: "A"}},
		},
	}))

	_, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Tanaka Kenji", 0)
	require.NoError(t, err)

	reloaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := reloaded.Rounds[0][0]
	assert.Equal(t, "Tanaka Kenji", m.Winner)
	assert.Empty(t, m.WinnerID, "a name matching BOTH sides identically must resolve to an empty id: a stale prior id must be CLEARED, never left in place as a guess")
}

// TestBronze_OverrideBracketWinner_SetsWinnerID is F1's bronze-branch half:
// the SAME `bm.Winner = winnerName` omission existed in OverrideBracketWinner's
// bronze (3rd-place) fallthrough.
func TestBronze_OverrideBracketWinner_SetsWinnerID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-override-winnerid"

	createBronzeTestCompetition(t, store, compID, true)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]

	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner: sf[0].SideA, Status: state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner: sf[1].SideB, Status: state.MatchStatusCompleted,
	}))

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	bronzeSideAID := bracket.ThirdPlaceMatch.SideAID
	bronzeWinnerName := bracket.ThirdPlaceMatch.SideA
	require.NotEmpty(t, bronzeSideAID, "the bronze match's own side must carry an id, fed from the semifinal loser")

	_, err = eng.OverrideBracketWinner(compID, "m-bronze", bronzeWinnerName, 0)
	require.NoError(t, err)

	bracket, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, bronzeWinnerName, bracket.ThirdPlaceMatch.Winner)
	assert.Equal(t, bronzeSideAID, bracket.ThirdPlaceMatch.WinnerID, "the bronze override must set WinnerID from the row's own matching side, not leave it unset")
}

// --- Finding F3: the bronze feed must pick the loser by ID, not by a bare name switch ---

// TestPropagateBracketWinner_BronzeFeed_SameNameSemifinal_LoserByID pins
// bc-brid: propagateBracketWinner's bronze-feed loser
// derivation used a bare `switch m.Winner { case m.SideA: ...}`. For a
// same-name semifinal (two competitors sharing a display name from different
// dojos, legal per CheckDuplicateEntriesByNameDojo) won by SideB, m.Winner ==
// m.SideA is ALSO true (both sides share the winner's name), so the switch's
// first-case-wins semantics fed the WINNER's own id into the bronze match
// instead of the actual loser's -- even though BracketMatch.SideAID's own
// doc comment already claimed this path was converted; only the final's
// advancement was, not the bronze feed.
func TestPropagateBracketWinner_BronzeFeed_SameNameSemifinal_LoserByID(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	tokyoID := "tokyo-id"
	osakaID := "osaka-id"
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				// Semifinal: Osaka (SideB) wins.
				{ID: "sf-0", SideA: "Tanaka Kenji", SideAID: tokyoID, SideB: "Tanaka Kenji", SideBID: osakaID,
					Winner: "Tanaka Kenji", WinnerID: osakaID, Status: state.MatchStatusCompleted},
				{ID: "sf-1", SideA: "Charlie", SideAID: "charlie-id", SideB: "Dave", SideBID: "dave-id",
					Winner: "Charlie", WinnerID: "charlie-id", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "final", Status: state.MatchStatusScheduled},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "bronze", Status: state.MatchStatusScheduled, DisplayRound: -1},
	}

	eng.propagateBracketWinner(bracket, 0, 0)

	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, "Tanaka Kenji", bracket.ThirdPlaceMatch.SideA, "the loser's bare name is ambiguous -- both sides share it")
	assert.Equal(t, tokyoID, bracket.ThirdPlaceMatch.SideAID, "the bronze match must receive the LOSER's id (Tokyo), never the winner's (Osaka)")
}

// --- Finding F4: RevertMatchToQueue's bracket branch must clear WinnerID with Winner ---

// TestRevertMatchToQueue_Bracket_ClearsWinnerID pins bc-pnum review finding
// F4: the bracket branch cleared Winner but left WinnerID behind (the pool
// branch, and reopenBracketMatch's own mirror obligation, both clear both).
// Reproduced through the public API: after requeueing a non-completed match
// that still carries stale score metadata, the row must read
// status=scheduled, winner="", winnerId="" -- not a stale id surviving
// alongside a cleared name, which a later withdrawal could resolve by id
// alone without ever needing a winner name.
func TestRevertMatchToQueue_Bracket_ClearsWinnerID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "revert-bracket-winnerid"

	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Revert WinnerID", Format: state.CompFormatPlayoffs,
		Courts: []string{"A"}, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			// RUNNING (not completed), but still carrying stale WinnerID
			// metadata from an earlier partial write -- exactly the shape
			// RevertMatchToQueue's own doc comment says it must normalise.
			{{ID: "m-r1-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
				Winner: "Alice", WinnerID: aliceID, Status: state.MatchStatusRunning, Court: "A"}},
		},
	}))

	require.NoError(t, eng.RevertMatchToQueue(compID, "m-r1-0"))

	reverted, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := reverted.Rounds[0][0]
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Empty(t, m.Winner, "Winner must be cleared by a requeue")
	assert.Empty(t, m.WinnerID, "WinnerID must be cleared alongside the name; a stale id surviving a requeue lets a later id-first read attribute a fresh decision to the wrong, no-longer-winning competitor")
}

// --- Ordering pin: the round-0 id stamp must run BEFORE bye-propagation ---

// TestBuildBracketFromDraw_ThreePlayers_ByeWinnerIDPropagatesToFinal pins the
// ordering buildBracketFromDraw depends on: Bracket.StampRoundZeroSideIDsFromDrawOrder
// must run BEFORE the bye-auto-resolution propagation pass, not after. With 3
// players, one round-0 match is a single bye that auto-resolves at
// generation time; if the id stamp ran AFTER the propagation pass instead,
// the bye's own WinnerID would still be empty at the moment
// propagateBracketWinner copies it into the final, and running the stamp
// afterward would not retroactively fix the final's already-propagated side
// (StampRoundZeroSideIDsFromDrawOrder only ever touches Rounds[0]). This
// reverses cleanly (moving the one call past the propagation loop) and the
// whole engine suite otherwise stays green, so it needs its own pin.
func TestBuildBracketFromDraw_ThreePlayers_ByeWinnerIDPropagatesToFinal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "playoffs-3-bye-id"

	createTestCompetition(t, store, compID, "playoffs", 3)
	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	charlieID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "DojoA"},
		{ID: bobID, Name: "Bob", Dojo: "DojoB"},
		{ID: charlieID, Name: "Charlie", Dojo: "DojoC"},
	}))

	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds, 2, "3 players -> padded to 4 slots -> round 0 + final")
	require.Len(t, bracket.Rounds[1], 1)

	// Exactly one round-0 match is a single bye (one side empty), auto
	// resolved with a Winner and, since bc-brid, a WinnerID stamped from
	// DrawOrder -- the identity that must survive into the final.
	var byeWinnerName, byeWinnerID string
	for _, m := range bracket.Rounds[0] {
		if (m.SideA == "" && m.SideB != "") || (m.SideA != "" && m.SideB == "") {
			require.Equal(t, state.MatchStatusCompleted, m.Status)
			byeWinnerName = m.Winner
			byeWinnerID = m.WinnerID
		}
	}
	require.NotEmpty(t, byeWinnerName, "3 players must produce exactly one bye")
	require.NotEmpty(t, byeWinnerID, "the bye's own winner id must be stamped at generation time, before propagation runs")

	final := bracket.Rounds[1][0]
	gotFinalID := final.SideAID
	if final.SideA != byeWinnerName {
		gotFinalID = final.SideBID
	}
	assert.Equal(t, byeWinnerID, gotFinalID, "the bye winner's id must propagate into the final; this requires StampRoundZeroSideIDsFromDrawOrder to run BEFORE the bye-propagation pass")
}

// --- Withdrawal-path pin: bracketMatchAsResult's id projection feeds a K3 rollback ---

// TestBracketRollback_SameNamePair_RestoresWinnerID pins the id projection in
// bracketMatchAsResult (internal/engine/bracket_result.go): deleting
// SideAID/SideBID/WinnerID from that projection leaves the whole engine
// suite green, because most rollback scenarios re-derive those ids from the
// live BracketMatch's own (untouched) pairing ids or from an unambiguous
// name comparison. A SAME-NAME pair is the one case neither escape hatch
// covers: resolveWinnerIDFromSides' name-comparison tier cannot tell the two
// sides apart (both share the winner's own name), so it falls back to an
// ippon-count heuristic that resolves to nothing when the match carries no
// ippons -- exactly this fixture.
//
// Repro: a same-name semifinal-shaped bracket match is ALREADY completed
// (Osaka, SideB, is the recorded winner). A second write attempts to flip
// the result and immediately fails with *AlreadyIneligibleError (the
// intended new loser, Osaka, is already ineligible from a different match),
// triggering the K3 rollback. The rollback must restore the ORIGINAL,
// correct WinnerID (Osaka's), which is only possible when the snapshot
// (bracketMatchAsResult's projection) carries it directly -- the row's
// SideA/SideB names alone cannot disambiguate a same-name pair, and there are
// no ippons on this fixture for the count-based fallback to infer from.
func TestBracketRollback_SameNamePair_RestoresWinnerID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bracket-rollback-samename"

	tokyoID := helper.NewUUID4()
	osakaID := helper.NewUUID4()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Rollback Same Name", Format: state.CompFormatPlayoffs,
		Courts: []string{"A"}, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: tokyoID, Name: "Tanaka Kenji", Dojo: "Tokyo"},
		{ID: osakaID, Name: "Tanaka Kenji", Dojo: "Osaka"},
	}))
	// Already completed: Osaka (SideB) is the recorded winner.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Tanaka Kenji", SideAID: tokyoID, SideB: "Tanaka Kenji", SideBID: osakaID,
				Winner: "Tanaka Kenji", WinnerID: osakaID, Status: state.MatchStatusCompleted, Court: "A"}},
		},
	}))

	// Osaka is already ineligible from a DIFFERENT match.
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: osakaID,
		Eligible: false,
		Reason:   "kiken in an earlier match",
		MatchID:  "some-other-match",
	}))

	// A rescore attempt: Tokyo (SideA) now wins, Osaka (SideB) withdraws.
	// This lands on disk first (the pool/bracket mutation always lands before
	// the ineligibility check runs), then recordIneligibilityFromDecision
	// finds Osaka already ineligible elsewhere and returns
	// *AlreadyIneligibleError, triggering the K3 rollback of THIS match back
	// to its prior (already-completed, Osaka-won) state.
	_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{
		Winner:     "Tanaka Kenji",
		WinnerSide: "A",
		Status:     state.MatchStatusCompleted,
		Decision:   "kiken",
		DecisionBy: "shiro",
	})
	require.Error(t, err, "must get AlreadyIneligibleError")
	var alreadyErr *AlreadyIneligibleError
	require.ErrorAs(t, err, &alreadyErr)

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	m := bracket.Rounds[0][0]
	assert.Equal(t, "Tanaka Kenji", m.Winner, "the rollback must restore the prior winner's name")
	assert.Equal(t, osakaID, m.WinnerID, "the rollback must restore the prior winner's id (Osaka), which the row's own SideA/SideB names cannot disambiguate on their own")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
}

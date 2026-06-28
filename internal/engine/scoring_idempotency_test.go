package engine

// TestRecordMatchResult_DuplicateCompletedSubmitIsIdempotent proves that
// re-submitting an identical COMPLETED match result does not double-advance
// the tournament (mp-gpra flaky-wifi hardening: terminal writes must be safe
// to retry from the client).
//
// Three scenarios are tested:
//  1. Knockout bracket — winner propagation to the next round must be a SET,
//     not an append: re-recording the same result must leave the next-round
//     slot unchanged.
//  2. Pool match — standings accounting must not double-count wins/points on a
//     second identical submission.
//  3. Kachinuki team match — MaybeAdvanceKachinuki must not double-append a
//     next bout when the last sub-result already has an outcome and the last
//     sub-result is an outcome-less (un-scored) bout that was appended on the
//     first call.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRecordMatchResult_DuplicateCompletedSubmitIsIdempotent(t *testing.T) {
	t.Run("knockout bracket: next-round slot is unchanged on second submit", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "idempotent-bracket"

		// 4 players → 2 rounds (semis + final). StartCompetition handles bracket
		// generation so match IDs match what the engine expects.
		createTestCompetition(t, store, compID, "playoffs", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
		require.NoError(t, eng.StartCompetition(compID))

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.Len(t, bracket.Rounds, 2, "expected semis + final")
		require.Len(t, bracket.Rounds[0], 2, "expected 2 semifinal matches")

		sf1ID := bracket.Rounds[0][0].ID
		// A FRESH result per submit: RecordMatchResult mutates the passed struct
		// in-place (ID, identity backfill, ippon normalization), so reusing one
		// pointer would make the "retry" send the already-mutated form rather than
		// the original client payload. A real wifi retry re-sends a fresh payload.
		// mIdx=0 → propagates to Rounds[1][0].SideA
		newResult := func() *state.MatchResult {
			return &state.MatchResult{
				Winner:  "Alice",
				IpponsA: []string{"M", "K"},
				Status:  state.MatchStatusCompleted,
			}
		}

		// First submit — should succeed and propagate Alice to the final.
		require.NoError(t, eng.RecordMatchResult(compID, sf1ID, newResult()))

		bracket, err = store.LoadBracket(compID)
		require.NoError(t, err)
		finalSlot := bracket.Rounds[1][0]
		assert.Equal(t, "Alice", finalSlot.SideA, "first submit: SideA of final must be Alice")
		assert.Empty(t, finalSlot.Winner, "final has not been scored yet")

		// Snapshot the entire final slot to detect any drift.
		snapshotSideA := finalSlot.SideA
		snapshotSideB := finalSlot.SideB
		snapshotWinner := finalSlot.Winner
		snapshotStatus := finalSlot.Status

		// Second submit — a FRESH identical payload, simulating a wifi retry.
		require.NoError(t, eng.RecordMatchResult(compID, sf1ID, newResult()))

		bracket, err = store.LoadBracket(compID)
		require.NoError(t, err)
		finalAfter := bracket.Rounds[1][0]

		// The next-round slot must be byte-for-byte equal to the snapshot.
		assert.Equal(t, snapshotSideA, finalAfter.SideA, "SideA must not change on second submit")
		assert.Equal(t, snapshotSideB, finalAfter.SideB, "SideB must not change on second submit")
		assert.Equal(t, snapshotWinner, finalAfter.Winner, "Winner must not change on second submit")
		assert.Equal(t, snapshotStatus, finalAfter.Status, "Status must not change on second submit")

		// The semifinal itself must still be completed with Alice as winner.
		sf1After := bracket.Rounds[0][0]
		assert.Equal(t, "Alice", sf1After.Winner)
		assert.Equal(t, state.MatchStatusCompleted, sf1After.Status)
	})

	t.Run("pool match: standings are unchanged on second identical submit", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "idempotent-pool"

		// 3-player league → 3 round-robin matches.
		createTestCompetition(t, store, compID, "league", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
		require.NoError(t, eng.StartCompetition(compID))

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.NotEmpty(t, matches)

		// Pick the first match. Alice wins with two ippons.
		m := matches[0]
		// Fresh result per submit — RecordMatchResult mutates in-place, so a real
		// retry must re-send a fresh identical payload, not the mutated struct.
		newResult := func() *state.MatchResult {
			return &state.MatchResult{
				SideA:   m.SideA,
				SideB:   m.SideB,
				Winner:  m.SideA,
				IpponsA: []string{"M", "K"},
				IpponsB: []string{},
				Status:  state.MatchStatusCompleted,
			}
		}

		// First submit.
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, newResult()))

		// Snapshot standings after first submit.
		standings1, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)

		// Extract counts for the two players in this match.
		winner1 := poolPlayerStanding(standings1, m.SideA)
		loser1 := poolPlayerStanding(standings1, m.SideB)
		require.NotNil(t, winner1, "winner must appear in standings")
		require.NotNil(t, loser1, "loser must appear in standings")

		// Second submit — a FRESH identical payload, wifi retry.
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, newResult()))

		standings2, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)

		winner2 := poolPlayerStanding(standings2, m.SideA)
		loser2 := poolPlayerStanding(standings2, m.SideB)
		require.NotNil(t, winner2)
		require.NotNil(t, loser2)

		// Wins, losses, ippons given/taken must all be identical.
		assert.Equal(t, winner1.Wins, winner2.Wins, "winner Wins must not double-count on second submit")
		assert.Equal(t, winner1.Losses, winner2.Losses, "winner Losses must not change on second submit")
		assert.Equal(t, winner1.IpponsGiven, winner2.IpponsGiven, "winner IpponsGiven must not double-count")
		assert.Equal(t, winner1.IpponsTaken, winner2.IpponsTaken, "winner IpponsTaken must not change")
		assert.Equal(t, loser1.Wins, loser2.Wins, "loser Wins must not change on second submit")
		assert.Equal(t, loser1.Losses, loser2.Losses, "loser Losses must not double-count on second submit")

		// Verify the stored match itself is not duplicated.
		reloaded, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		count := 0
		for _, rm := range reloaded {
			if rm.ID == m.ID {
				count++
				assert.Equal(t, state.MatchStatusCompleted, rm.Status)
				assert.Equal(t, m.SideA, rm.Winner)
			}
		}
		assert.Equal(t, 1, count, "the match record must appear exactly once after two identical submits")
	})

	t.Run("kachinuki: MaybeAdvanceKachinuki does not double-append a bout on second call", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "idempotent-kachinuki"

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            compID,
			TeamMatchType: state.TeamMatchTypeKachinuki,
			TeamSize:      5,
			Format:        state.CompFormatMixed,
		}))

		// Bout 1: B-Senpo beats A-Senpo (B-Senpo stays; A-Jiho is next for A).
		// Bout 2: A-Jiho beats B-Chuken (A-Jiho stays; B-Senpo is still live).
		// After bout 2 both sides still have players → AdvanceKachinuki produces
		// out.Next (position 3), and MaybeAdvanceKachinuki appends it.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:    "P1-0",
				SideA: "RedTeam",
				SideB: "WhiteTeam",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
					{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
				},
			},
		}))

		// First call — should append bout 3.
		changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
		require.NoError(t, err)
		assert.True(t, changed, "first advance must append bout 3")

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		lenAfterFirst := len(matches[0].SubResults)
		assert.Equal(t, 3, lenAfterFirst, "should have exactly 3 sub-results after first advance")

		// The appended bout 3 has no outcome yet (no Winner, no Decision).
		lastBout := matches[0].SubResults[lenAfterFirst-1]
		assert.Empty(t, lastBout.Winner, "freshly appended bout must have no winner yet")
		assert.Empty(t, lastBout.Decision, "freshly appended bout must have no decision yet")

		// Second call — the last sub-result has no outcome, so this must be a no-op.
		changed2, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
		require.NoError(t, err)

		matches2, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches2, 1)
		lenAfterSecond := len(matches2[0].SubResults)

		if changed2 {
			// If the engine reported changed=true on the second call it means
			// it DID perform some mutation (not necessarily a double-append —
			// e.g. match-ended detection). Record what we found for the report.
			t.Logf("NOTICE: second MaybeAdvanceKachinuki returned changed=true; SubResults len: %d -> %d",
				lenAfterFirst, lenAfterSecond)
		}

		// The critical invariant: a second call must NOT append another
		// outcome-less bout. The count may equal lenAfterFirst (pure no-op) or
		// it may decrease to reflect a match-ended collapse, but it must NEVER
		// exceed lenAfterFirst by a non-outcome bout.
		assert.LessOrEqual(t, lenAfterSecond, lenAfterFirst,
			"second MaybeAdvanceKachinuki must not append an extra outcome-less bout; SubResults grew from %d to %d",
			lenAfterFirst, lenAfterSecond)
	})
}

// poolPlayerStanding returns the PlayerStanding for the named player from
// standings (a map of pool name → []PlayerStanding), or nil if not found.
func poolPlayerStanding(standings map[string][]state.PlayerStanding, name string) *state.PlayerStanding {
	for _, pool := range standings {
		for i := range pool {
			if pool[i].Player.Name == name {
				cp := pool[i]
				return &cp
			}
		}
	}
	return nil
}

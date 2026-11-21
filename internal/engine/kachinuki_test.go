package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKachinukiWinnerAdvances covers the FR-044 happy path: when one
// side's player wins a bout, that player stays on and faces the head
// of the opposing side's remaining queue.
//
// T123.
func TestKachinukiWinnerAdvances(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "A-Senpo",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// SideA still has 4 more; SideB lost their Senpo so the next
		// un-retired opponent is Jiho.
		SideA: fighters("A-Jiho", "A-Chuken", "A-Fukusho", "A-Taisho"),
		SideB: fighters("B-Jiho", "B-Chuken", "B-Fukusho", "B-Taisho"),
	})

	require.NotNil(t, res.Next, "expected a follow-up bout, got match-ended")
	assert.False(t, res.MatchEnded)
	assert.Equal(t, 2, res.Next.Position, "bout position should increment by one")
	// SideA player stays, so the new bout still has SideA = A-Senpo.
	assert.Equal(t, "A-Senpo", res.Next.SideA)
	assert.Equal(t, "B-Jiho", res.Next.SideB)
}

// TestKachinukiSideBWinnerSwapsRole verifies the symmetric path: when
// the SideB player wins, they remain SideB on the next bout (the
// stayer's side role is preserved so admins reading subResults can
// still tell which team a name belongs to).
func TestKachinukiSideBWinnerSwapsRole(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "B-Senpo",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters("A-Jiho", "A-Chuken"),
		SideB:    fighters("B-Jiho", "B-Chuken"),
	})

	require.NotNil(t, res.Next)
	assert.Equal(t, 2, res.Next.Position)
	assert.Equal(t, "A-Jiho", res.Next.SideA)
	assert.Equal(t, "B-Senpo", res.Next.SideB)
}

// TestKachinukiHikiwakeRetiresBoth covers FR-044 hikiwake semantics:
// a draw retires BOTH players and the next pair from each remaining
// queue takes the court.
//
// T124.
func TestKachinukiHikiwakeRetiresBoth(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// Both Senpos retired; the caller already stripped them from
		// these remaining queues. Next bout should pair A-Jiho with
		// B-Jiho.
		SideA: fighters("A-Jiho", "A-Chuken"),
		SideB: fighters("B-Jiho", "B-Chuken"),
	})

	require.NotNil(t, res.Next, "expected next bout after hikiwake")
	assert.False(t, res.MatchEnded)
	assert.Equal(t, 2, res.Next.Position)
	assert.Equal(t, "A-Jiho", res.Next.SideA)
	assert.Equal(t, "B-Jiho", res.Next.SideB)
}

// TestKachinukiExhaustionEndsMatch covers FR-044 + T137: when one side
// has no remaining un-retired players, the other side wins by
// exhaustion (DecisionKachinukiExhaustion).
func TestKachinukiExhaustionEndsMatch(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 4,
		SideA:    "A-Fukusho",
		SideB:    "B-Taisho",
		Winner:   "A-Fukusho",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// SideA still has Taisho left; SideB is exhausted.
		SideA: fighters("A-Taisho"),
		SideB: fighters(),
	})

	assert.True(t, res.MatchEnded, "side B exhausted should end the match")
	assert.Equal(t, "A", res.WinningSide)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), res.Decision)
	assert.Nil(t, res.Next)
}

// TestKachinukiHikiwakeExhaustsLast covers the edge case where a
// hikiwake retires the last player on each side simultaneously. The
// engine returns BothExhausted=true (MatchEnded=false) so the caller
// can decide by phase: a pool encounter is finalized as a draw, a
// bracket encounter stays running until the operator adds a daihyosen.
// GAP 2b.
func TestKachinukiHikiwakeExhaustsLast(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 5,
		SideA:    "A-Taisho",
		SideB:    "B-Taisho",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters(),
		SideB:    fighters(),
	})

	assert.True(t, res.BothExhausted, "simultaneous exhaustion must set BothExhausted")
	assert.False(t, res.MatchEnded, "MatchEnded must remain false; caller decides by phase")
	assert.Equal(t, "", res.WinningSide, "no winner when both sides exhaust simultaneously")
	assert.Nil(t, res.Next, "no next bout")
}

// TestAdvanceKachinuki_SimultaneousExhaustionNoOp verifies that when both
// teams run out of players simultaneously after a hikiwake, the pure
// AdvanceKachinuki function returns BothExhausted=true with MatchEnded=false
// and Next=nil. The caller (MaybeAdvanceKachinuki) decides the outcome by
// phase: pool/league finalizes as a draw, bracket stays running for daihyosen.
// GAP 2b.
func TestAdvanceKachinuki_SimultaneousExhaustionNoOp(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 5,
		SideA:    "A-Taisho",
		SideB:    "B-Taisho",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters(),
		SideB:    fighters(),
	})

	assert.True(t, res.BothExhausted, "simultaneous exhaustion must set BothExhausted=true")
	assert.False(t, res.MatchEnded, "MatchEnded must be false; caller decides by phase")
	assert.Equal(t, "", res.WinningSide, "no default winner when both sides exhaust simultaneously")
	assert.Nil(t, res.Next, "no next bout scheduled")
}

// TestRetiredPlayersFromBoutLog verifies the helper that callers use
// to derive remaining rosters from the persisted SubResults log.
func TestRetiredPlayersFromBoutLog(t *testing.T) {
	boutLog := []state.SubMatchResult{
		{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "A-Senpo", Decision: "fought"},
		{Position: 2, SideA: "A-Senpo", SideB: "B-Jiho", Decision: string(domain.DecisionHikiwake)},
		{Position: 3, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "B-Chuken", Decision: "fought"},
	}
	retiredA, retiredB := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")

	// A-Senpo retired on the hikiwake in bout 2; A-Jiho retired in bout 3.
	// None of these rows carry a member id, so only Names is populated.
	assert.Contains(t, retiredA.Names, "A-Senpo")
	assert.Contains(t, retiredA.Names, "A-Jiho")
	assert.NotContains(t, retiredA.Names, "A-Chuken", "A-Chuken never played, not retired")
	assert.Empty(t, retiredA.IDs, "no bout row carried a member id")
	// B-Senpo retired in bout 1; B-Jiho retired in bout 2 (hikiwake).
	assert.Contains(t, retiredB.Names, "B-Senpo")
	assert.Contains(t, retiredB.Names, "B-Jiho")
	assert.NotContains(t, retiredB.Names, "B-Chuken", "B-Chuken won bout 3 and is still on the court")
	assert.Empty(t, retiredB.IDs, "no bout row carried a member id")
}

// TestRetiredPlayersFromBoutLog_WithMemberIDs verifies that a bout row
// carrying member ids populates BOTH RetiredMemberSet halves: the retiring
// fighter's id lands in IDs, their name (as recorded on that row) still
// lands in Names too, so a slot without its own id can still find the
// retirement by name (the fallback IsMemberRetired uses for exactly that
// case).
func TestRetiredPlayersFromBoutLog_WithMemberIDs(t *testing.T) {
	boutLog := []state.SubMatchResult{
		{Position: 1, SideA: "Sato", SideAMemberID: "id-sato", SideB: "Tanaka", SideBMemberID: "id-tanaka",
			Winner: "Tanaka", Decision: "fought"},
	}
	retiredA, _ := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")
	assert.Contains(t, retiredA.IDs, "id-sato", "Sato's member id must be recorded (Sato lost)")
	assert.Contains(t, retiredA.Names, "Sato", "the name is still recorded alongside the id")
}

// TestIsMemberRetired pins the resolution rule (bc-tmid pass 2 operator
// ruling: "a record that carries an id field is resolved by id only"): the
// member id wins WHENEVER the fighter carries one, and a fighter with none
// falls back to the name -- the pre-bc-tmid rule, unchanged for exactly
// that case.
func TestIsMemberRetired(t *testing.T) {
	retired := RetiredMemberSet{
		IDs:   map[string]struct{}{"id-sato": {}},
		Names: map[string]struct{}{"Suzuki": {}},
	}
	// The roster carries exactly one of each name unless a subtest says
	// otherwise: that is what lets the name tier speak at all.
	unique := map[string]struct{}{}
	t.Run("id present and retired", func(t *testing.T) {
		assert.True(t, IsMemberRetired(kachinukiFighter{Name: "Renamed Sato", MemberID: "id-sato"}, retired, unique),
			"the id matches regardless of what name currently rides with it")
	})
	t.Run("id present but not retired must not fall back to a SHARED name", func(t *testing.T) {
		// Two members of this team are called Suzuki, so a retirement
		// recorded under that name cannot say which of them it meant. The
		// fighter carries a different id, and the name cannot break the tie,
		// so nothing here may retire them.
		shared := map[string]struct{}{"Suzuki": {}}
		assert.False(t, IsMemberRetired(kachinukiFighter{Name: "Suzuki", MemberID: "id-different"}, retired, shared),
			"a name two teammates share can never identify which one retired")
	})
	t.Run("id present, id misses, and the name is the fighter's alone: retired", func(t *testing.T) {
		// The bout row that retired them carried no member id -- a row the
		// load-time repair could not resolve, or one written by a client too
		// old to send ids -- so it recorded only the name. The id lookup
		// therefore finds nothing, and refusing here would put a fighter who
		// has already lost back in the queue ahead of the reserve.
		assert.True(t, IsMemberRetired(kachinukiFighter{Name: "Suzuki", MemberID: "id-different"}, retired, unique),
			"an id miss over a name nobody else on this roster carries still retires them")
	})
	t.Run("no id falls back to name", func(t *testing.T) {
		assert.True(t, IsMemberRetired(kachinukiFighter{Name: "Suzuki"}, retired, unique))
		assert.False(t, IsMemberRetired(kachinukiFighter{Name: "Someone Else"}, retired, unique))
	})
	t.Run("a nil ambiguity set means no name is shared", func(t *testing.T) {
		assert.True(t, IsMemberRetired(kachinukiFighter{Name: "Suzuki", MemberID: "id-different"}, retired, nil))
	})
}

// TestAmbiguousFighterNames pins the gate itself: only a name MORE THAN ONE
// roster entry carries is ambiguous, and a blank name is never a key (an
// unfilled lineup slot is not a person two slots could be confused for).
func TestAmbiguousFighterNames(t *testing.T) {
	got := ambiguousFighterNames([]kachinukiFighter{
		{Name: "Suzuki", MemberID: "m1"},
		{Name: "Suzuki", MemberID: "m2"},
		{Name: "Sato", MemberID: "m3"},
		{Name: "", MemberID: "m4"},
		{Name: "", MemberID: "m5"},
	})
	assert.Equal(t, map[string]struct{}{"Suzuki": {}}, got)
}

// TestFilterRemainingFighters_UnrepairedRowRetiresByName is the defect in its
// own shape: a fighter whose LINEUP SLOT carries a member id, retired by a
// BOUT ROW that carries none. The two keys never meet, so before this gate
// the loser came back up the queue ahead of the reserve.
func TestFilterRemainingFighters_UnrepairedRowRetiresByName(t *testing.T) {
	// Bout row with no member id: only the name survives it.
	boutLog := []state.SubMatchResult{
		{Position: 1, SideA: "Sato", SideB: "Ito", Winner: "Ito"},
	}
	retiredA, _ := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")

	roster := []kachinukiFighter{
		{Name: "Sato", MemberID: "m-sato"}, // lost above; the row named no id
		{Name: "Kato", MemberID: "m-kato"}, // the reserve, never fought
	}
	remaining := filterRemainingFighters(roster, retiredA)
	require.Len(t, remaining, 1, "the beaten fighter must not be queued again")
	assert.Equal(t, "Kato", remaining[0].Name)

	t.Run("but not when a teammate shares the name", func(t *testing.T) {
		shared := []kachinukiFighter{
			{Name: "Sato", MemberID: "m-sato-1"},
			{Name: "Sato", MemberID: "m-sato-2"},
		}
		assert.Len(t, filterRemainingFighters(shared, retiredA), 2,
			"the row cannot say WHICH Sato lost, so it may not remove either")
	})
}

// TestFilterRemainingFighters_LegacyRowWithNoMemberID pins that retirement
// still works for a row with NO member id (an unrepaired legacy row, or a
// typed fighter name): the filter falls back to the name exactly
// as it always did before bc-tmid.
func TestFilterRemainingFighters_LegacyRowWithNoMemberID(t *testing.T) {
	boutLog := []state.SubMatchResult{
		// No member ids anywhere on this row -- the legacy shape.
		{Position: 1, SideA: "Sato", SideB: "Tanaka", Winner: "Tanaka", Decision: "fought"},
	}
	retiredA, _ := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")
	require.Empty(t, retiredA.IDs, "the fixture is legacy-shaped: no ids anywhere")

	roster := []kachinukiFighter{{Name: "Sato"}, {Name: "Backup"}}
	got := filterRemainingFighters(roster, retiredA)
	assert.Equal(t, []kachinukiFighter{{Name: "Backup"}}, got,
		"Sato (retired, by name) is filtered out; Backup remains")
}

// TestPreserveKachinukiMemberIDs_SurvivesRescoreWithoutIDs is the writer
// half of the fix: a bout the engine itself appended (SideAMemberID/
// SideBMemberID already stamped, drawn from a resolved lineup slot) must
// not lose those ids the moment the score editor -- which does not yet
// round-trip them on the wire -- writes a score for that SAME position.
// Without preserveKachinukiMemberIDs, mergeKachinukiSubResults's plain
// "incoming overwrites stored at this position" would silently wipe them,
// making every id this pass mints useless the instant the bout is scored.
func TestPreserveKachinukiMemberIDs_SurvivesRescoreWithoutIDs(t *testing.T) {
	stored := []state.SubMatchResult{
		{Position: 1, SideA: "Sato", SideAMemberID: "id-sato", SideB: "Tanaka", SideBMemberID: "id-tanaka"},
	}
	// The score editor's write: same names, same position, but no id
	// fields at all -- exactly what a pre-pass-3 client sends.
	incoming := []state.SubMatchResult{
		{Position: 1, SideA: "Sato", SideB: "Tanaka", Winner: "Sato", Decision: "fought"},
	}
	merged := mergeKachinukiSubResults(stored, incoming)
	require.Len(t, merged, 1)
	assert.Equal(t, "id-sato", merged[0].SideAMemberID, "the id must be inherited from the stored row, not wiped")
	assert.Equal(t, "id-tanaka", merged[0].SideBMemberID)
	assert.Equal(t, "id-sato", merged[0].WinnerMemberID, "WinnerMemberID is derived once the side ids are known")
}

// TestAdvanceKachinukiUnrecognizedOutcome guards the defensive branch:
// a Winner that names neither side returns the zero result so callers
// fall back to manual scheduling instead of producing a wrong pair.
func TestAdvanceKachinukiUnrecognizedOutcome(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "Someone Else",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters("A-Jiho"),
		SideB:    fighters("B-Jiho"),
	})
	assert.Nil(t, res.Next)
	assert.False(t, res.MatchEnded)
	assert.True(t, res.OutcomeUnread)
}

// TestDescribeKachinukiResult smoke-tests the stringer (used by handler logs).
func TestDescribeKachinukiResult(t *testing.T) {
	assert.Contains(t, describeKachinukiResult(AdvanceKachinukiResult{MatchEnded: true, WinningSide: "A", Decision: string(domain.DecisionKachinukiExhaustion)}), "MatchEnded")
	assert.Contains(t, describeKachinukiResult(AdvanceKachinukiResult{Next: &state.SubMatchResult{Position: 3, SideA: "x", SideB: "y"}}), "Next")
	assert.Equal(t, "no-op", describeKachinukiResult(AdvanceKachinukiResult{}))
}

// TestAdvanceKachinuki_HikiwakeSideAExhausted covers the case where SideA
// has no replacement after a hikiwake but SideB does. Operator ruling:
// the fighter who just tied STAYS ON THE SLOT (under the taisho rule a
// drawing Taisho continues, with nothing to re-type), paired against
// SideB's next fighter; under plain exhaustion the operator gives the
// survivor the per-bout fusensho and Ends on that point (the walkover,
// spec 006 decision 2). It must NOT flag MatchEnded: that is the win
// path's verdict, and here there is no decisive point to End on.
func TestAdvanceKachinuki_HikiwakeSideAExhausted(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 3,
		SideA:    "A-Chuken",
		SideB:    "B-Chuken",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters(),                        // SideA exhausted
		SideB:    fighters("B-Fukusho", "B-Taisho"), // SideB still has players
	})
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the appended bout expresses the outcome")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the next-bout slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "A-Chuken", res.Next.SideA, "the fighter who just tied stays on the slot")
	assert.Equal(t, "B-Fukusho", res.Next.SideB)
}

// TestAdvanceKachinuki_HikiwakeSideBExhausted mirrors the stays-on-slot
// contract for SideB having no replacement after a hikiwake.
func TestAdvanceKachinuki_HikiwakeSideBExhausted(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 3,
		SideA:    "A-Chuken",
		SideB:    "B-Chuken",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    fighters("A-Fukusho", "A-Taisho"), // SideA still has players
		SideB:    fighters(),                        // SideB exhausted
	})
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the appended bout expresses the outcome")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the next-bout slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "A-Fukusho", res.Next.SideA)
	assert.Equal(t, "B-Chuken", res.Next.SideB, "the fighter who just tied stays on the slot")
}

// TestRetiredPlayersFromBoutLog_SameNameBoutUsesMemberIDs pins the operator
// ruling where it matters most: who won a bout decides who STAYS ON. Two
// opposing fighters may legally share a display name, and the name switch
// resolves such a row to side A, so before this the winner of a same-name
// bout was retired and the loser kept fighting.
func TestRetiredPlayersFromBoutLog_SameNameBoutUsesMemberIDs(t *testing.T) {
	// Both fighters are called "Yamada". SHIRO (side B) won, and only the
	// member ids say so.
	boutLog := []state.SubMatchResult{{
		Position: 1,
		SideA:    "Yamada", SideB: "Yamada", Winner: "Yamada",
		SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-shiro",
	}}
	retiredA, retiredB := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")

	assert.Contains(t, retiredA.IDs, "m-aka", "the aka fighter lost and must retire")
	assert.NotContains(t, retiredB.IDs, "m-shiro", "the shiro fighter won and stays on")

	t.Run("and aka winning still retires shiro", func(t *testing.T) {
		log := []state.SubMatchResult{{
			Position: 1,
			SideA:    "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-aka",
		}}
		rA, rB := RetiredPlayersFromBoutLog(log, "Team A", "Team B")
		assert.Contains(t, rB.IDs, "m-shiro")
		assert.NotContains(t, rA.IDs, "m-aka")
	})

	t.Run("a row with no ids keeps the old name answer rather than stalling", func(t *testing.T) {
		// Nothing can say who won. Retiring side B is the pre-existing
		// answer and is kept deliberately: a queue whose head never clears
		// would re-offer the same pairing for ever.
		log := []state.SubMatchResult{{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
		}}
		rA, rB := RetiredPlayersFromBoutLog(log, "Team A", "Team B")
		assert.Contains(t, rB.Names, "Yamada")
		assert.Empty(t, rA.Names)
	})
}

// TestDeriveKachinukiWinner_SameNameDecidingBout is the most expensive form
// of the same-name defect: the deciding bout's winner is a FIGHTER name, and
// when both fighters carry the same one the name comparison's case order
// handed the ENCOUNTER to side A. That decides who advances, and it
// overturned the operator's own verdict on the way.
func TestDeriveKachinukiWinner_SameNameDecidingBout(t *testing.T) {
	newResult := func(sub state.SubMatchResult, clientWinner string) *state.MatchResult {
		return &state.MatchResult{
			SideA: "Tora", SideB: "Kaze", Winner: clientWinner,
			Decision:   string(domain.DecisionKachinukiExhaustion),
			SubResults: []state.SubMatchResult{sub},
		}
	}

	t.Run("the member ids decide the encounter", func(t *testing.T) {
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-shiro",
			IpponsB: []string{"M"},
		}, "Kaze")
		require.NoError(t, deriveKachinukiWinner(r))
		assert.Equal(t, "Kaze", r.Winner, "shiro's member id won the deciding bout")
	})

	t.Run("and for aka", func(t *testing.T) {
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-aka",
			IpponsA: []string{"M"},
		}, "Tora")
		require.NoError(t, deriveKachinukiWinner(r))
		assert.Equal(t, "Tora", r.Winner)
	})

	t.Run("the ids overrule a payload that names the other side", func(t *testing.T) {
		// This is what pins the ID TIER apart from the same-name relaxation
		// below: with the two agreeing, removing the ids changes nothing and
		// the tier is untested. Here the row's identity and the payload
		// disagree, and the row wins -- the ids are evidence about who
		// fought, the winner field is a name that cannot discriminate.
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-shiro",
			IpponsB: []string{"M"},
		}, "Tora")
		require.NoError(t, deriveKachinukiWinner(r))
		assert.Equal(t, "Kaze", r.Winner)
	})

	t.Run("no ids: the operator's verdict stands rather than name order", func(t *testing.T) {
		// Legacy data. Nothing stored can attribute the bout, so overturning
		// the operator is the one thing that must not happen.
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			IpponsB: []string{"M"},
		}, "Kaze")
		require.NoError(t, deriveKachinukiWinner(r))
		assert.Equal(t, "Kaze", r.Winner, "the editor said shiro; nothing here knows better")
	})

	t.Run("no ids and a winner naming neither team is still rejected", func(t *testing.T) {
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			IpponsB: []string{"M"},
		}, "Someone Else")
		assert.Error(t, deriveKachinukiWinner(r), "the relaxation must not accept an unattributable winner")
	})

	t.Run("distinct fighter names still derive from the names", func(t *testing.T) {
		r := newResult(state.SubMatchResult{
			Position: 1, SideA: "Sato", SideB: "Ito", Winner: "Ito",
			IpponsB: []string{"M"},
		}, "")
		require.NoError(t, deriveKachinukiWinner(r))
		assert.Equal(t, "Kaze", r.Winner)
	})
}

// TestAdvanceKachinuki_NamelessWinnerStaysOnByMemberID pins the bc-dnst
// rule that a fighter picked by squad number before being named stays on
// like any other: the client records such a winner as the TEAM name (there
// is no fighter name to record) alongside the member id, and the advance
// attributes the bout by id through the shared owner rather than by a name
// comparison that could never match.
func TestAdvanceKachinuki_NamelessWinnerStaysOnByMemberID(t *testing.T) {
	bout := state.SubMatchResult{
		Position:       2,
		SideA:          "Aoki",
		SideAMemberID:  "id-aoki",
		SideB:          "",
		SideBMemberID:  "id-b2",
		Winner:         "Minami Budokan",
		WinnerMemberID: "id-b2",
		Decision:       "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []kachinukiFighter{{Name: "", MemberID: "id-a2"}, {Name: "", MemberID: "id-a3"}},
		SideB:    []kachinukiFighter{{Name: "", MemberID: "id-b3"}},
	})
	require.NotNil(t, res.Next, "the nameless winner must stay on and be paired against side A's next fighter")
	assert.Equal(t, 3, res.Next.Position)
	assert.Equal(t, "id-a2", res.Next.SideAMemberID, "side A sends its next fighter, by id")
	assert.Equal(t, "id-b2", res.Next.SideBMemberID, "the winner keeps its side and its id")
	assert.False(t, res.MatchEnded)
}

// TestAdvanceKachinuki_WinnerStaysOnByIDWhenOpponentCarriesNoID pins the
// bc-dnst id-tier EXTENSION specifically: domain.AttributeWinnerSide names a
// side when the winner id equals THAT side's id even when the OTHER side
// carries no id at all. TestAdvanceKachinuki_NamelessWinnerStaysOnByMemberID
// above is not a distinguishing test for this: its fixture stamps a member id
// on BOTH SideA and SideB (id-aoki and id-b2), so the pre-bc-dnst id tier --
// which required a.WinnerID, a.SideAID AND a.SideBID all non-empty before
// attributing by id at all -- would have matched WinnerID against SideBID and
// produced the identical MatchSideB result. That older, stricter code is
// exactly as green on that fixture as the current one.
//
// Here SideB carries NO member id (a typed substitute fielded without a
// squad pick), so the old three-ids-required tier could never engage and
// fell through to the name comparison; Winner is deliberately the TEAM name
// ("Minami Budokan"), matching neither fighter's name, so that fallback
// answered MatchSideNone and the advance produced no next pairing at all.
// Only the current one-side-suffices rule attributes this bout.
func TestAdvanceKachinuki_WinnerStaysOnByIDWhenOpponentCarriesNoID(t *testing.T) {
	bout := state.SubMatchResult{
		Position:       2,
		SideA:          "Aoki",
		SideAMemberID:  "id-aoki",
		SideB:          "Substitute",
		SideBMemberID:  "", // the opponent carries NO id
		Winner:         "Minami Budokan",
		WinnerMemberID: "id-aoki",
		Decision:       "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []kachinukiFighter{{Name: "", MemberID: "id-a2"}, {Name: "", MemberID: "id-a3"}},
		SideB:    []kachinukiFighter{{Name: "", MemberID: "id-b3"}},
	})
	require.NotNil(t, res.Next, "Aoki's id alone must attribute the bout even though SideB carries no id")
	assert.Equal(t, 3, res.Next.Position)
	assert.Equal(t, "id-aoki", res.Next.SideAMemberID, "Aoki (side A) stays on, decided by id alone")
	assert.Equal(t, "id-b3", res.Next.SideBMemberID, "side B sends its next fighter, by id")
	assert.False(t, res.MatchEnded)
}

// TestAdvanceKachinuki_SameNameNoIDsStillAdvances pins the residue the advance
// keeps in step with RetiredPlayersFromBoutLog (bc-dnst): two opposing
// fighters sharing a display name on a row with no member ids cannot be
// settled by the shared attribution owner, and the retired-set derivation
// has already retired the loser by name (side A first), so the advance
// answers the same way rather than leaving a retired fighter with no next
// pairing.
func TestAdvanceKachinuki_SameNameNoIDsStillAdvances(t *testing.T) {
	bout := state.SubMatchResult{Position: 1, SideA: "Sato", SideB: "Sato", Winner: "Sato", Decision: "fought"}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []kachinukiFighter{{Name: "A-Jiho"}},
		SideB:    []kachinukiFighter{{Name: "B-Jiho"}},
	})
	require.NotNil(t, res.Next, "the same-name row must still produce a pairing")
	assert.Equal(t, "Sato", res.Next.SideA, "side A's Sato stays on, the aka-first residue")
	assert.Equal(t, "B-Jiho", res.Next.SideB)
}

// TestRetiredPlayersFromBoutLog_NamelessFighterRetiresByID pins that a
// fighter fielded by squad number before being named (bc-dnst), an id and an
// empty name, retires under that id, beside a named fighter with no id.
func TestRetiredPlayersFromBoutLog_NamelessFighterRetiresByID(t *testing.T) {
	log := []state.SubMatchResult{
		{Position: 1, SideA: "", SideAMemberID: "a1", SideB: "", SideBMemberID: "b1", Winner: "Team A", WinnerMemberID: "a1", Decision: "fought"},
		{Position: 2, SideA: "", SideAMemberID: "a1", SideB: "Tanaka", SideBMemberID: "b2", Winner: "Tanaka", WinnerMemberID: "b2", Decision: "fought"},
		{Position: 3, SideA: "Legacy", SideB: "Tanaka", SideBMemberID: "b2", Winner: "Tanaka", Decision: "fought"},
	}
	retiredA, retiredB := RetiredPlayersFromBoutLog(log, "Team A", "Team B")
	assert.Equal(t, map[string]struct{}{"a1": {}}, retiredA.IDs, "a1 retires by id")
	assert.Equal(t, map[string]struct{}{"Legacy": {}}, retiredA.Names, "Legacy retires by name; a1 has none")
	assert.Equal(t, map[string]struct{}{"b1": {}}, retiredB.IDs, "b1 retires by id")
	assert.Empty(t, retiredB.Names)
}

// TestRetiredPlayersFromBoutLog_PendingNamelessRowRetiresNobody pins that a
// pending pairing whose side is a fighter fielded by squad number and not yet
// named (empty name, member id, no outcome) retires nobody: the name fallback
// used to match the empty Winner against that empty side name and retire the
// opponent before the bout was fought.
func TestRetiredPlayersFromBoutLog_PendingNamelessRowRetiresNobody(t *testing.T) {
	log := []state.SubMatchResult{
		{Position: 4, SideA: "", SideAMemberID: "a3", SideB: "Tanaka", SideBMemberID: "b2"},
	}
	retiredA, retiredB := RetiredPlayersFromBoutLog(log, "Team A", "Team B")
	assert.Empty(t, retiredA.IDs)
	assert.Empty(t, retiredA.Names)
	assert.Empty(t, retiredB.IDs, "Tanaka must not be retired by a bout nobody has scored")
	assert.Empty(t, retiredB.Names, "Tanaka must not be retired by a bout nobody has scored")
}

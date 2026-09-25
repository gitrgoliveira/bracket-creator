package state

import (
	"encoding/json"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamResultFrom(t *testing.T) {
	t.Run("nil for no sub-bouts (individual match)", func(t *testing.T) {
		assert.Nil(t, TeamResultFrom(nil, "A", "B", domain.MatchSideNone))
		assert.Nil(t, TeamResultFrom([]SubMatchResult{}, "A", "B", domain.MatchSideNone))
	})

	t.Run("IV and PW per side, shiro=B aka=A", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "TeamB", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M", "K"}},
			{Position: 1, Winner: "TeamA", SideA: "P3", SideB: "P4", IpponsA: []string{"M", "K"}, IpponsB: []string{"M"}},
			{Position: 2, Winner: "TeamB", SideA: "P5", SideB: "P6", IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		// IV: B(shiro)=2, A(aka)=1. PW: shiro=2+1+0=3, aka=1+2+1=4.
		assert.Equal(t, &TeamResultLine{ShiroIV: 2, AkaIV: 1, ShiroPW: 3, AkaPW: 4}, got)
	})

	t.Run("daihyosen placeholder (position < 0) excluded", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "TeamB", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M", "K"}},
			{Position: -1, Winner: "TeamA", SideA: "P3", SideB: "P4", IpponsA: []string{"M", "K"}, IpponsB: []string{}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		// Only position 0 counts: shiroIV=1, akaIV=0, shiroPW=2, akaPW=1.
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 2, AkaPW: 1}, got)
	})

	t.Run("malformed negative position (< -1) defensively excluded", func(t *testing.T) {
		// Real bouts have a non-negative Position (fixed-format is 0-based,
		// kachinuki 1-based); the daihyosen is -1. Any Position < -1 is
		// malformed input and must not be counted into IV/PW (guards a
		// stale/malicious payload). Position 0 below is a legitimate,
		// countable bout.
		subs := []SubMatchResult{
			{Position: 0, Winner: "TeamB", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M"}},
			{Position: -2, Winner: "TeamA", SideA: "P3", SideB: "P4", IpponsA: []string{"M", "K"}, IpponsB: []string{"K"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		// Only position 0 counts; the -2 row is skipped like the daihyosen.
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 1, AkaPW: 1}, got)
	})

	t.Run("only daihyosen placeholder returns nil", func(t *testing.T) {
		// A slice containing ONLY the Position:-1 placeholder must return nil (no
		// countable sub-bouts), not a non-nil all-zero TeamResultLine.
		subs := []SubMatchResult{
			{Position: -1, Winner: "TeamA", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		assert.Nil(t, TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone))
	})

	t.Run("placeholder plus real bout counts the real bout", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: -1, Winner: "TeamA", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{}},
			{Position: 0, Winner: "TeamB", SideA: "P3", SideB: "P4", IpponsA: []string{}, IpponsB: []string{"K"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		// Placeholder skipped: shiroIV=1, akaIV=0, shiroPW=1, akaPW=0.
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 1, AkaPW: 0}, got)
	})

	t.Run("unfilled placeholder slots don't inflate PW", func(t *testing.T) {
		// "•" is the UI's unfilled-slot placeholder (see
		// engine.countScoringIppons); an in-progress bout scored on one side
		// only must not count the other side's still-empty slots as points.
		subs := []SubMatchResult{
			{Position: 0, Winner: "", SideA: "P1", SideB: "P2", IpponsA: []string{"M", "•", ""}, IpponsB: []string{"•", "•"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{ShiroIV: 0, AkaIV: 0, ShiroPW: 0, AkaPW: 1}, got)
	})

	t.Run("draw contributes PW but no IV; sub-level side name fallback", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M"}},
			// Winner carries the sub-level side name, not the match-level team name.
			{Position: 1, Winner: "P4", SideA: "P3", SideB: "P4", IpponsA: []string{}, IpponsB: []string{"K"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 2, AkaPW: 1}, got)
	})
}

func TestMatchResultMarshalJSON_TeamResult(t *testing.T) {
	t.Run("team match carries teamResult", func(t *testing.T) {
		m := MatchResult{
			ID: "Pool A-1", SideA: "TeamA", SideB: "TeamB", Status: MatchStatusCompleted,
			SubResults: []SubMatchResult{
				{Position: 0, Winner: "TeamB", IpponsA: []string{"M"}, IpponsB: []string{"M", "K"}},
			},
		}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &out))
		require.Contains(t, out, "teamResult")
		var tr TeamResultLine
		require.NoError(t, json.Unmarshal(out["teamResult"], &tr))
		assert.Equal(t, TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 2, AkaPW: 1}, tr)
	})

	t.Run("individual match omits teamResult", func(t *testing.T) {
		m := MatchResult{ID: "m1", SideA: "P1", SideB: "P2", Status: MatchStatusCompleted, IpponsA: []string{"M"}}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &out))
		assert.NotContains(t, out, "teamResult")
		// Existing fields still serialize.
		assert.Contains(t, out, "id")
		assert.Contains(t, out, "ipponsA")
	})
}

func TestBracketMatchMarshalJSON_TeamResult(t *testing.T) {
	t.Run("team bracket match carries teamResult", func(t *testing.T) {
		m := BracketMatch{
			ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Winner: "Ryu", Status: MatchStatusCompleted,
			SubResults: []SubMatchResult{
				{Position: 1, SideA: "Ryu Ichiro", SideB: "Tora Ichiro", Winner: "Ryu Ichiro", IpponsA: []string{"M"}},
				{Position: 2, SideA: "Ryu Ichiro", SideB: "Tora Jiro", Winner: "Ryu Ichiro", IpponsA: []string{"K"}},
			},
		}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &out))
		require.Contains(t, out, "teamResult")
		var tr TeamResultLine
		require.NoError(t, json.Unmarshal(out["teamResult"], &tr))
		assert.Equal(t, TeamResultLine{ShiroIV: 0, AkaIV: 2, ShiroPW: 0, AkaPW: 2}, tr)
	})

	t.Run("daihyosen-decided tie reports zero IV and PW", func(t *testing.T) {
		m := BracketMatch{
			ID: "m-r2-0", SideA: "Ryu", SideB: "Kaze", Winner: "Ryu", Status: MatchStatusCompleted,
			Decision: "daihyosen",
			SubResults: []SubMatchResult{
				{Position: 1, SideA: "Ryu Ichiro", SideB: "Kaze Ichiro", Decision: DecisionDraw},
				{Position: -1, SideA: "Ryu", SideB: "Kaze", Winner: "Ryu", IpponsA: []string{"M"}},
			},
		}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &out))
		require.Contains(t, out, "teamResult")
		var tr TeamResultLine
		require.NoError(t, json.Unmarshal(out["teamResult"], &tr))
		assert.Equal(t, TeamResultLine{}, tr)
	})

	t.Run("individual bracket match omits teamResult", func(t *testing.T) {
		m := BracketMatch{ID: "m-r1-1", SideA: "P1", SideB: "P2", Status: MatchStatusCompleted, ScoreA: "MM"}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var out map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(b, &out))
		assert.NotContains(t, out, "teamResult")
		assert.Contains(t, out, "id")
		assert.Contains(t, out, "scoreA")
	})

	t.Run("unmarshal ignores a serialized teamResult", func(t *testing.T) {
		m := BracketMatch{
			ID: "m-r1-0", SideA: "Ryu", SideB: "Tora", Status: MatchStatusCompleted,
			SubResults: []SubMatchResult{{Position: 1, Winner: "Ryu", IpponsA: []string{"M"}}},
		}
		b, err := json.Marshal(m)
		require.NoError(t, err)
		var back BracketMatch
		require.NoError(t, json.Unmarshal(b, &back))
		assert.Equal(t, m.ID, back.ID)
		assert.Equal(t, m.SideA, back.SideA)
		assert.Len(t, back.SubResults, 1)
	})
}

// TestTeamResultFrom_SameNameBout pins the operator ruling that a sub-bout
// between two fighters sharing a display name is attributed by MEMBER ID
// only. Before bc-pnum the name comparison's case order handed every such
// bout to Aka, which is a coin flip written into the standings: the two
// fighters are on opposing teams and sharing a name is legal.
func TestTeamResultFrom_SameNameBout(t *testing.T) {
	t.Run("ids decide it, including against the aka-first order", func(t *testing.T) {
		// Both fighters are called "Yamada"; the SHIRO one (side B) won.
		// Only the ids say so, and they must be believed.
		subs := []SubMatchResult{{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-shiro",
			IpponsB: []string{"M", "K"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.ShiroIV, "the member id names shiro as the winner")
		assert.Equal(t, 0, got.AkaIV, "aka must not take it on name order")
	})

	t.Run("ids decide it for aka too", func(t *testing.T) {
		subs := []SubMatchResult{{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-aka",
			IpponsA: []string{"M", "K"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.AkaIV)
		assert.Equal(t, 0, got.ShiroIV)
	})

	t.Run("no ids: neither side, never a guess", func(t *testing.T) {
		// The pre-bc-pnum behaviour gave this to Aka. Nothing stored can say
		// who won, so the honest answer is that nobody is credited. PW still
		// counts: the points were struck whoever struck them.
		subs := []SubMatchResult{{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			IpponsA: []string{"M"}, IpponsB: []string{"M", "K"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 0, got.AkaIV, "aka-first on a same-name bout is the coin flip this removes")
		assert.Equal(t, 0, got.ShiroIV)
		assert.Equal(t, 1, got.AkaPW)
		assert.Equal(t, 2, got.ShiroPW)
	})

	t.Run("a winner id matching neither side falls through to the names", func(t *testing.T) {
		// Drifted data: the id names nobody on this row. The names CAN tell
		// these two apart, so they answer, exactly as before.
		subs := []SubMatchResult{{
			Position: 1, SideA: "Sato", SideB: "Ito", Winner: "Ito",
			SideAMemberID: "m-a", SideBMemberID: "m-b", WinnerMemberID: "m-gone",
			IpponsB: []string{"M"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.ShiroIV)
		assert.Equal(t, 0, got.AkaIV)
	})

	t.Run("distinct names with no ids are unchanged", func(t *testing.T) {
		subs := []SubMatchResult{{
			Position: 1, SideA: "Sato", SideB: "Ito", Winner: "Sato",
			IpponsA: []string{"M"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.AkaIV)
		assert.Equal(t, 0, got.ShiroIV)
	})

	t.Run("the quick-score synth path still resolves by team name", func(t *testing.T) {
		// Rows naming the TEAMS, not two fighters: team names are unique by
		// rule, so this tier is untouched by the same-name guard.
		subs := []SubMatchResult{{
			Position: 0, SideA: "TeamA", SideB: "TeamB", Winner: "TeamB",
			IpponsB: []string{"M"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		require.NotNil(t, got)
		assert.Equal(t, 1, got.ShiroIV)
	})
}

// TestDefaultWinCreditSide pins the bc-tmfn follow-up rule: which side a
// default-win ruling (any kiken, fusenpai, or fusensho) closing a match
// credits every unfought numbered bout to.
func TestDefaultWinCreditSide(t *testing.T) {
	completedAtt := domain.WinnerAttribution{Winner: "TeamB", SideA: "TeamA", SideB: "TeamB"}

	t.Run("not completed credits nobody, whatever the decision", func(t *testing.T) {
		got := DefaultWinCreditSide(MatchStatusRunning, "kiken-voluntary", "aka", completedAtt)
		assert.Equal(t, domain.MatchSideNone, got)
	})

	t.Run("completed but not a default-win decision credits nobody", func(t *testing.T) {
		for _, d := range []string{"", "fought", "hikiwake", "daihyosen", "kachinuki-exhaustion", "ippon-shobu"} {
			got := DefaultWinCreditSide(MatchStatusCompleted, d, "aka", completedAtt)
			assert.Equal(t, domain.MatchSideNone, got, "decision %q", d)
		}
	})

	t.Run("decisionBy aka: SideA withdrew, SideB (shiro) is credited", func(t *testing.T) {
		for _, d := range []string{"kiken-voluntary", "kiken-injury", "kiken", "fusenpai", "fusensho"} {
			got := DefaultWinCreditSide(MatchStatusCompleted, d, "aka", domain.WinnerAttribution{})
			assert.Equal(t, domain.MatchSideB, got, "decision %q", d)
		}
	})

	t.Run("decisionBy shiro: SideB withdrew, SideA (aka) is credited", func(t *testing.T) {
		for _, d := range []string{"kiken-voluntary", "kiken-injury", "kiken", "fusenpai", "fusensho"} {
			got := DefaultWinCreditSide(MatchStatusCompleted, d, "shiro", domain.WinnerAttribution{})
			assert.Equal(t, domain.MatchSideA, got, "decision %q", d)
		}
	})

	t.Run("empty decisionBy (legacy) falls back to the match's own winner attribution, ids first", func(t *testing.T) {
		att := domain.WinnerAttribution{
			WinnerID: "id-b", SideAID: "id-a", SideBID: "id-b",
			Winner: "TeamA", SideA: "TeamA", SideB: "TeamB", // names disagree with the ids on purpose
		}
		got := DefaultWinCreditSide(MatchStatusCompleted, "fusensho", "", att)
		assert.Equal(t, domain.MatchSideB, got, "the id must win over the drifted name")
	})

	t.Run("empty decisionBy, no winner info at all credits nobody", func(t *testing.T) {
		got := DefaultWinCreditSide(MatchStatusCompleted, "fusenpai", "", domain.WinnerAttribution{SideA: "TeamA", SideB: "TeamB"})
		assert.Equal(t, domain.MatchSideNone, got)
	})
}

// TestSubBoutEffectiveResult pins SubBoutEffectiveResult's three-way split:
// a bout with its own result is untouched, an unfought numbered bout is
// synthesized from the credit, and the daihyosen placeholder is never
// touched.
func TestSubBoutEffectiveResult(t *testing.T) {
	t.Run("a fought bout keeps its own result, whatever the credit", func(t *testing.T) {
		sub := SubMatchResult{Position: 1, Winner: "P1", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}}
		got := SubBoutEffectiveResult(sub, domain.MatchSideB, "TeamA", "TeamB")
		assert.Equal(t, sub, got)
	})

	t.Run("a fusensho row entered before the kiken keeps its own result", func(t *testing.T) {
		// This bout was decided on its own, per-bout, before the match-level
		// kiken was ever recorded: it has its own Winner/Decision/ippons, so
		// HasResult() is true and the match-level credit must not override it.
		sub := SubMatchResult{Position: 2, Decision: "fusensho", Winner: "TeamB", IpponsB: []string{"○", "○"}}
		require.True(t, sub.HasResult())
		got := SubBoutEffectiveResult(sub, domain.MatchSideA, "TeamA", "TeamB")
		assert.Equal(t, sub, got, "the credit (TeamA) must not override this row's own fusensho (TeamB)")
	})

	t.Run("no credit leaves an unfought bout untouched", func(t *testing.T) {
		sub := SubMatchResult{Position: 3}
		got := SubBoutEffectiveResult(sub, domain.MatchSideNone, "TeamA", "TeamB")
		assert.Equal(t, sub, got)
	})

	t.Run("credit A synthesizes the maru for the unfought bout", func(t *testing.T) {
		sub := SubMatchResult{Position: 2}
		got := SubBoutEffectiveResult(sub, domain.MatchSideA, "TeamA", "TeamB")
		assert.Equal(t, "TeamA", got.Winner)
		assert.Equal(t, []string{"○", "○"}, got.IpponsA)
		assert.Empty(t, got.IpponsB)
		assert.Equal(t, "", got.Decision, "no per-row Kiken/Fus. mark; the mark rides the match-level summary")
		assert.Equal(t, 2, got.Position)
	})

	t.Run("credit B synthesizes the maru for the unfought bout", func(t *testing.T) {
		sub := SubMatchResult{Position: 1}
		got := SubBoutEffectiveResult(sub, domain.MatchSideB, "TeamA", "TeamB")
		assert.Equal(t, "TeamB", got.Winner)
		assert.Equal(t, []string{"○", "○"}, got.IpponsB)
		assert.Empty(t, got.IpponsA)
	})

	t.Run("the daihyosen placeholder is never synthesized, whatever the credit", func(t *testing.T) {
		sub := SubMatchResult{Position: DaihyosenSubPosition}
		got := SubBoutEffectiveResult(sub, domain.MatchSideA, "TeamA", "TeamB")
		assert.Equal(t, sub, got)
	})

	t.Run("position 0 is never synthesized either", func(t *testing.T) {
		sub := SubMatchResult{Position: 0}
		got := SubBoutEffectiveResult(sub, domain.MatchSideA, "TeamA", "TeamB")
		assert.Equal(t, sub, got)
	})
}

// TestPadDefaultWinBoutPositions pins the append-only, position-only padding
// rule.
func TestPadDefaultWinBoutPositions(t *testing.T) {
	t.Run("empty input pads every position 1..teamSize", func(t *testing.T) {
		got := PadDefaultWinBoutPositions(nil, 3)
		require.Len(t, got, 3)
		for i, want := range []int{1, 2, 3} {
			assert.Equal(t, want, got[i].Position)
			assert.False(t, got[i].HasResult())
		}
	})

	t.Run("only missing positions are appended; existing rows are untouched, output ordered by Position", func(t *testing.T) {
		existing := []SubMatchResult{
			{Position: 1, Winner: "P1", IpponsA: []string{"M"}},
			{Position: 3, Winner: "P3", IpponsA: []string{"K"}},
		}
		got := PadDefaultWinBoutPositions(existing, 3)
		require.Len(t, got, 3)
		// bc-cse finding 12: the output is ordered by Position ascending
		// (1, 2, 3), not append order (1, 3, then the padded 2 tacked on at
		// the end) -- TeamScoreboard reads a bout's lineup name by its ARRAY
		// INDEX, so an out-of-position array put the wrong fighter's name on
		// a scored row.
		assert.Equal(t, existing[0], got[0], "position 1 is untouched")
		assert.Equal(t, SubMatchResult{Position: 2}, got[1], "the missing position 2 is appended IN ORDER, not at the end")
		assert.Equal(t, existing[1], got[2], "position 3 is untouched")
	})

	t.Run("the daihyosen row (position < 1) is never padded, never counted as present for a real position, and sorts AFTER every numbered position", func(t *testing.T) {
		got := PadDefaultWinBoutPositions([]SubMatchResult{{Position: DaihyosenSubPosition, Winner: "P1"}}, 2)
		require.Len(t, got, 3, "the daihyosen row plus positions 1 and 2 padded")
		// bc-cse finding 12: never a plain Position sort of the whole slice,
		// which would put -1 first; the daihyosen row keeps its place AFTER
		// the numbered block instead.
		assert.Equal(t, 1, got[0].Position)
		assert.Equal(t, 2, got[1].Position)
		assert.Equal(t, DaihyosenSubPosition, got[2].Position)
		assert.Equal(t, "P1", got[2].Winner, "the daihyosen row's own data is untouched, only moved")
	})

	t.Run("bc-cse finding 12: a single stored row at position 2 of 5 is ordered 1..5, not [2,1,3,4,5]", func(t *testing.T) {
		existing := []SubMatchResult{
			{Position: 2, Winner: "P2", IpponsA: []string{"M", "K"}},
		}
		got := PadDefaultWinBoutPositions(existing, 5)
		require.Len(t, got, 5)
		for i, want := range []int{1, 2, 3, 4, 5} {
			assert.Equal(t, want, got[i].Position, "row %d", i)
		}
		assert.Equal(t, existing[0], got[1], "position 2's own recorded row lands at array index 1, not index 0")
		for i, idx := range []int{0, 2, 3, 4} {
			assert.False(t, got[idx].HasResult(), "position %d is a placeholder", i)
		}
	})

	t.Run("bc-cse finding 12: a stored numbered row plus a daihyosen row -- the DH row moves after the numbered block", func(t *testing.T) {
		existing := []SubMatchResult{
			{Position: DaihyosenSubPosition, Winner: "TeamA"},
			{Position: 2, Winner: "P2", IpponsA: []string{"M", "K"}},
		}
		got := PadDefaultWinBoutPositions(existing, 3)
		require.Len(t, got, 4, "positions 1..3 plus the daihyosen row")
		assert.Equal(t, []int{1, 2, 3, DaihyosenSubPosition}, []int{got[0].Position, got[1].Position, got[2].Position, got[3].Position})
		assert.Equal(t, existing[1], got[1], "position 2's own recorded row is untouched, only reordered")
		assert.Equal(t, existing[0], got[3], "the daihyosen row's own data is untouched, only moved to the end")
	})

	t.Run("teamSize 0 pads nothing", func(t *testing.T) {
		existing := []SubMatchResult{{Position: 1, Winner: "P1"}}
		got := PadDefaultWinBoutPositions(existing, 0)
		assert.Equal(t, existing, got)
	})

	t.Run("already fully present is a no-op: no padding needed means no reordering either", func(t *testing.T) {
		existing := []SubMatchResult{{Position: 1}, {Position: 2}}
		got := PadDefaultWinBoutPositions(existing, 2)
		assert.Equal(t, existing, got)
	})
}

// TestTeamResultFrom_DefaultWinCredit pins TeamResultFrom's crediting
// behaviour end to end: a match decided before any bout was fought (every
// position padded) and a match decided after one bout was fought (the rest
// padded) produce the SAME teamResult when the fought bout's own score
// matches what the maru would have given; a running match credits nothing;
// the daihyosen row is never credited.
func TestTeamResultFrom_DefaultWinCredit(t *testing.T) {
	t.Run("decided before any bout: every position padded and credited", func(t *testing.T) {
		subs := PadDefaultWinBoutPositions(nil, 3)
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideA)
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{AkaIV: 3, AkaPW: 6}, got)
	})

	t.Run("decided after bout 1 was fought (a clean 2-0 win for the same side): same teamResult", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 1, Winner: "TeamA", IpponsA: []string{"M", "K"}},
		}
		subs = PadDefaultWinBoutPositions(subs, 3)
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideA)
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{AkaIV: 3, AkaPW: 6}, got,
			"1 real 2-0 win + 2 credited default wins == 3 credited default wins on points and victories alike")
	})

	t.Run("kiken on the other side flips the credit", func(t *testing.T) {
		subs := PadDefaultWinBoutPositions(nil, 2)
		gotA := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideA)
		gotB := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideB)
		assert.Equal(t, &TeamResultLine{AkaIV: 2, AkaPW: 4}, gotA)
		assert.Equal(t, &TeamResultLine{ShiroIV: 2, ShiroPW: 4}, gotB)
	})

	t.Run("a running (reopened) match credits nothing: unfought bouts stay uncounted", func(t *testing.T) {
		subs := []SubMatchResult{{Position: 1}, {Position: 2}}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideNone)
		// Two real (non-daihyosen) positions exist, so hasBout is true and the
		// line is non-nil -- just all zero, since neither bout has a result
		// and there is no credit to fill the gap.
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{}, got, "no countable result on either bout")
	})

	t.Run("a fusensho row entered before the kiken keeps its own result", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 1, Decision: "fusensho", Winner: "TeamB", IpponsB: []string{"○", "○"}},
			{Position: 2},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideA)
		require.NotNil(t, got)
		// Bout 1: TeamB's own fusensho, unaffected by the TeamA credit.
		// Bout 2: unfought, credited to TeamA.
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, ShiroPW: 2, AkaIV: 1, AkaPW: 2}, got)
	})

	t.Run("the daihyosen row is ignored, credit or no", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: DaihyosenSubPosition, Winner: "TeamA", IpponsA: []string{"M"}},
			{Position: 1},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB", domain.MatchSideB)
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, ShiroPW: 2}, got, "only position 1 counts, credited to TeamB")
	})
}

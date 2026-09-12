package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTeamResultFrom(t *testing.T) {
	t.Run("nil for no sub-bouts (individual match)", func(t *testing.T) {
		assert.Nil(t, TeamResultFrom(nil, "A", "B"))
		assert.Nil(t, TeamResultFrom([]SubMatchResult{}, "A", "B"))
	})

	t.Run("IV and PW per side, shiro=B aka=A", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "TeamB", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M", "K"}},
			{Position: 1, Winner: "TeamA", SideA: "P3", SideB: "P4", IpponsA: []string{"M", "K"}, IpponsB: []string{"M"}},
			{Position: 2, Winner: "TeamB", SideA: "P5", SideB: "P6", IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB")
		require.NotNil(t, got)
		// IV: B(shiro)=2, A(aka)=1. PW: shiro=2+1+0=3, aka=1+2+1=4.
		assert.Equal(t, &TeamResultLine{ShiroIV: 2, AkaIV: 1, ShiroPW: 3, AkaPW: 4}, got)
	})

	t.Run("daihyosen placeholder (position < 0) excluded", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "TeamB", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M", "K"}},
			{Position: -1, Winner: "TeamA", SideA: "P3", SideB: "P4", IpponsA: []string{"M", "K"}, IpponsB: []string{}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
		// Only position 0 counts; the -2 row is skipped like the daihyosen.
		assert.Equal(t, &TeamResultLine{ShiroIV: 1, AkaIV: 0, ShiroPW: 1, AkaPW: 1}, got)
	})

	t.Run("only daihyosen placeholder returns nil", func(t *testing.T) {
		// A slice containing ONLY the Position:-1 placeholder must return nil (no
		// countable sub-bouts), not a non-nil all-zero TeamResultLine.
		subs := []SubMatchResult{
			{Position: -1, Winner: "TeamA", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		assert.Nil(t, TeamResultFrom(subs, "TeamA", "TeamB"))
	})

	t.Run("placeholder plus real bout counts the real bout", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: -1, Winner: "TeamA", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{}},
			{Position: 0, Winner: "TeamB", SideA: "P3", SideB: "P4", IpponsA: []string{}, IpponsB: []string{"K"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
		require.NotNil(t, got)
		assert.Equal(t, &TeamResultLine{ShiroIV: 0, AkaIV: 0, ShiroPW: 0, AkaPW: 1}, got)
	})

	t.Run("draw contributes PW but no IV; sub-level side name fallback", func(t *testing.T) {
		subs := []SubMatchResult{
			{Position: 0, Winner: "", SideA: "P1", SideB: "P2", IpponsA: []string{"M"}, IpponsB: []string{"M"}},
			// Winner carries the sub-level side name, not the match-level team name.
			{Position: 1, Winner: "P4", SideA: "P3", SideB: "P4", IpponsA: []string{}, IpponsB: []string{"K"}},
		}
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
		require.NotNil(t, got)
		assert.Equal(t, 1, got.ShiroIV)
		assert.Equal(t, 0, got.AkaIV)
	})

	t.Run("distinct names with no ids are unchanged", func(t *testing.T) {
		subs := []SubMatchResult{{
			Position: 1, SideA: "Sato", SideB: "Ito", Winner: "Sato",
			IpponsA: []string{"M"},
		}}
		got := TeamResultFrom(subs, "TeamA", "TeamB")
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
		got := TeamResultFrom(subs, "TeamA", "TeamB")
		require.NotNil(t, got)
		assert.Equal(t, 1, got.ShiroIV)
	})
}

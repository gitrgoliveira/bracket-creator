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

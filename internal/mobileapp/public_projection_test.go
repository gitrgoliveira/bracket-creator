package mobileapp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These guard the public-projection redaction: operator-only audit free-text
// (CorrectionReason / DecisionReason / ChangeReason) must be stripped before any
// payload leaves the server on an unauthenticated channel (viewer REST + SSE),
// while the caller's original (already persisted) value is left untouched.
// DecisionBy (an enum) is NOT audit free-text, it drives viewer winner/label
// rendering and must be preserved.

func TestMatchForBroadcast_StripsAuditAndCopies(t *testing.T) {
	orig := state.MatchResult{
		ID: "Pool A-0", SideA: "x",
		CorrectionReason: "wrong waza entered",
		DecisionReason:   "kiken: medial knee injury to Tanaka",
		DecisionBy:       "aka",
		Rev:              7,
		RevSession:       "client-session-abc",
	}
	got := matchForBroadcast(orig)

	assert.Empty(t, got.CorrectionReason, "broadcast copy must not carry the correction reason")
	assert.Empty(t, got.DecisionReason, "broadcast copy must not carry the decision reason (can name competitors / carry medical detail)")
	assert.Zero(t, got.Rev, "broadcast copy must not carry internal write-ordering Rev")
	assert.Empty(t, got.RevSession, "broadcast copy must not leak the client RevSession identifier")
	assert.Equal(t, "aka", got.DecisionBy, "DecisionBy is an enum that drives rendering, must be preserved")
	assert.Equal(t, "Pool A-0", got.ID, "non-audit fields preserved")
	assert.Equal(t, "wrong waza entered", orig.CorrectionReason, "caller's original must be untouched")
	assert.Equal(t, "kiken: medial knee injury to Tanaka", orig.DecisionReason, "caller's original must be untouched")
	assert.EqualValues(t, 7, orig.Rev, "caller's original Rev must be untouched")
	assert.Equal(t, "client-session-abc", orig.RevSession, "caller's original RevSession must be untouched")
}

func TestMatchPtrForBroadcast_NilStaysNil(t *testing.T) {
	assert.Nil(t, matchPtrForBroadcast(nil))

	orig := &state.MatchResult{ID: "m1", CorrectionReason: "fix"}
	got := matchPtrForBroadcast(orig)
	require.NotNil(t, got)
	assert.Empty(t, got.CorrectionReason)
	assert.Equal(t, "fix", orig.CorrectionReason, "pointee not mutated")
	assert.NotSame(t, orig, got, "must be a distinct copy")
}

func TestMatchesForBroadcast_StripsEachAndCopies(t *testing.T) {
	orig := []state.MatchResult{
		{ID: "a", CorrectionReason: "r1"},
		{ID: "b", CorrectionReason: "r2"},
	}
	got := matchesForBroadcast(orig)

	require.Len(t, got, 2)
	for _, m := range got {
		assert.Empty(t, m.CorrectionReason)
	}
	assert.Equal(t, "r1", orig[0].CorrectionReason, "source slice untouched")
	assert.Equal(t, "r2", orig[1].CorrectionReason)
}

func TestStripMatchesAudit_InPlace(t *testing.T) {
	ms := []state.MatchResult{
		{ID: "a", CorrectionReason: "r1", DecisionReason: "d1", DecisionBy: "shiro"},
		{ID: "b"},
	}
	stripMatchesAudit(ms)
	assert.Empty(t, ms[0].CorrectionReason)
	assert.Empty(t, ms[0].DecisionReason)
	assert.Equal(t, "shiro", ms[0].DecisionBy, "DecisionBy preserved")
	assert.Empty(t, ms[1].CorrectionReason)
	assert.Equal(t, "a", ms[0].ID, "non-audit fields preserved")
}

func TestStripBracketAudit(t *testing.T) {
	t.Run("nil is a no-op", func(t *testing.T) {
		require.NotPanics(t, func() { stripBracketAudit(nil) })
	})
	t.Run("clears every match across all rounds", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", CorrectionReason: "x", DecisionReason: "dx", DecisionBy: "aka"}, {ID: "r0-m1", CorrectionReason: "y"}},
			{{ID: "r1-m0", CorrectionReason: "z", DecisionReason: "dz"}},
		}}
		stripBracketAudit(b)
		assert.Empty(t, b.Rounds[0][0].CorrectionReason)
		assert.Empty(t, b.Rounds[0][0].DecisionReason)
		assert.Equal(t, "aka", b.Rounds[0][0].DecisionBy, "DecisionBy preserved")
		assert.Empty(t, b.Rounds[0][1].CorrectionReason)
		assert.Empty(t, b.Rounds[1][0].CorrectionReason)
		assert.Empty(t, b.Rounds[1][0].DecisionReason)
		assert.Equal(t, "r0-m0", b.Rounds[0][0].ID, "non-audit fields preserved")
	})
}

func TestLineupForPublic_StripsChangeReasonAndCopies(t *testing.T) {
	orig := domain.TeamLineup{TeamID: "teamA", Round: 1, ChangeReason: "injury to jiho"}
	got := lineupForPublic(orig)

	assert.Empty(t, got.ChangeReason)
	assert.Equal(t, "teamA", got.TeamID)
	assert.Equal(t, "injury to jiho", orig.ChangeReason, "caller's original untouched")
}

package mobileapp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These guard the public-projection redaction: operator-only audit free-text
// (CorrectionReason / ChangeReason) must be stripped before any payload leaves
// the server on an unauthenticated channel (viewer REST + SSE), while the
// caller's original (already persisted) value is left untouched.

func TestMatchForBroadcast_StripsAuditAndCopies(t *testing.T) {
	orig := state.MatchResult{ID: "Pool A-0", SideA: "x", CorrectionReason: "wrong waza entered"}
	got := matchForBroadcast(orig)

	assert.Empty(t, got.CorrectionReason, "broadcast copy must not carry the audit reason")
	assert.Equal(t, "Pool A-0", got.ID, "non-audit fields preserved")
	assert.Equal(t, "wrong waza entered", orig.CorrectionReason, "caller's original must be untouched")
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
	ms := []state.MatchResult{{ID: "a", CorrectionReason: "r1"}, {ID: "b"}}
	stripMatchesAudit(ms)
	assert.Empty(t, ms[0].CorrectionReason)
	assert.Empty(t, ms[1].CorrectionReason)
	assert.Equal(t, "a", ms[0].ID, "non-audit fields preserved")
}

func TestStripBracketAudit(t *testing.T) {
	t.Run("nil is a no-op", func(t *testing.T) {
		require.NotPanics(t, func() { stripBracketAudit(nil) })
	})
	t.Run("clears every match across all rounds", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", CorrectionReason: "x"}, {ID: "r0-m1", CorrectionReason: "y"}},
			{{ID: "r1-m0", CorrectionReason: "z"}},
		}}
		stripBracketAudit(b)
		assert.Empty(t, b.Rounds[0][0].CorrectionReason)
		assert.Empty(t, b.Rounds[0][1].CorrectionReason)
		assert.Empty(t, b.Rounds[1][0].CorrectionReason)
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

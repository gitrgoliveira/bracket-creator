package mobileapp

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Public-projection helpers strip operator-only audit justifications from
// match and lineup data before it leaves the server on a PUBLIC
// (unauthenticated) channel — the viewer REST endpoints and the SSE stream.
//
// CorrectionReason (MatchResult/BracketMatch) and ChangeReason (TeamLineup) are
// free-text fields an operator types to justify a score correction or a forced
// mid-match lineup change; the example copy ("Substitution: injury to jiho")
// shows they can name competitors and carry medical detail. They exist solely
// for the admin audit trail, persisted to disk (pool-matches.csv / bracket.json
// / lineup YAML), and no frontend ever reads them back over the wire. They must
// never reach spectators. This mirrors the existing redaction at
// handlers_viewer.go where the tournament password is zeroed before public
// serialization.
//
// All of these mutate in place. Callers pass either post-persistence locals or
// the deep copies returned by store.Load* (LoadPoolMatches/LoadBracket copy),
// so stripping here never corrupts stored or cached state.

// matchForBroadcast returns a copy of m with audit fields cleared, for use as a
// PUBLIC SSE payload. m is taken by value so the caller's original (and its
// already-persisted disk record) is untouched.
func matchForBroadcast(m state.MatchResult) state.MatchResult {
	m.CorrectionReason = ""
	return m
}

// matchPtrForBroadcast is the pointer form of matchForBroadcast: nil stays nil
// (so a handler that may broadcast a null result keeps that shape), otherwise it
// returns a stripped copy without mutating the caller's pointee.
func matchPtrForBroadcast(m *state.MatchResult) *state.MatchResult {
	if m == nil {
		return nil
	}
	c := matchForBroadcast(*m)
	return &c
}

// matchesForBroadcast clears audit fields from a copy of each entry, for a
// PUBLIC SSE batch payload.
func matchesForBroadcast(ms []state.MatchResult) []state.MatchResult {
	out := make([]state.MatchResult, len(ms))
	for i, m := range ms {
		out[i] = matchForBroadcast(m)
	}
	return out
}

// stripMatchesAudit clears CorrectionReason on each pool match in place, for a
// PUBLIC viewer projection. Pass the deep copy from LoadPoolMatches.
func stripMatchesAudit(ms []state.MatchResult) {
	for i := range ms {
		ms[i].CorrectionReason = ""
	}
}

// stripBracketAudit clears CorrectionReason on every bracket match in place, for
// a PUBLIC viewer projection. Pass the deep copy from LoadBracket; nil is a
// no-op.
func stripBracketAudit(b *state.Bracket) {
	if b == nil {
		return
	}
	for ri := range b.Rounds {
		for j := range b.Rounds[ri] {
			b.Rounds[ri][j].CorrectionReason = ""
		}
	}
}

// lineupForPublic returns a copy of the lineup with the audit ChangeReason
// cleared, for the public read endpoints. The map values returned by
// LoadTeamLineups are already copies, but taking by value here keeps the
// redaction explicit and independent of that.
func lineupForPublic(l domain.TeamLineup) domain.TeamLineup {
	l.ChangeReason = ""
	return l
}

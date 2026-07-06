package mobileapp

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Public-projection helpers strip operator-only audit justifications from
// match and lineup data before it leaves the server on a PUBLIC
// (unauthenticated) channel, the viewer REST endpoints and the SSE stream.
//
// CorrectionReason and DecisionReason (MatchResult/BracketMatch) are free-text
// fields an operator types to justify a score correction or a kiken/fusenpai
// decision; the example copy ("Substitution: injury to jiho") shows they can name
// competitors and carry medical detail (DecisionReason in particular records FIK
// Art. 30 kiken-injury notes). They exist solely for the admin audit trail,
// persisted to disk (pool-matches.csv / bracket.json), and no frontend ever reads
// them back over the wire (the SPA only WRITES decisionReason; decisionBy, an
// enum, is what drives viewer rendering and is deliberately preserved). They must
// never reach spectators. This mirrors the existing redaction at handlers_viewer.go
// where the tournament password is zeroed before public serialization.
//
// Two flavours, both safe against corrupting stored/cached state:
//   - COPY helpers take the value by value and return a redacted copy:
//     matchForBroadcast, matchPtrForBroadcast, matchesForBroadcast,
//     lineupForPublic. Use these for SSE payloads built from a caller's local.
//   - IN-PLACE helpers clear fields on the passed slice/pointer:
//     stripMatchesAudit, stripBracketAudit. Pass the deep copies returned by
//     store.Load* (LoadPoolMatches/LoadBracket already copy), so the on-disk /
//     cached state is never touched.

// matchForBroadcast returns a copy of m with audit fields cleared, for use as a
// PUBLIC SSE payload. m is taken by value so the caller's original (and its
// already-persisted disk record) is untouched.
func matchForBroadcast(m state.MatchResult) state.MatchResult {
	m.CorrectionReason = ""
	m.DecisionReason = ""
	// Rev / RevSession are internal write-ordering metadata (the latter a
	// client session identifier); they must never leak onto the PUBLIC SSE
	// stream. They're omitempty, so zeroing them drops them from the payload.
	m.Rev = 0
	m.RevSession = ""
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

// stripMatchesAudit clears the audit free-text (CorrectionReason, DecisionReason)
// on each pool match in place, for a PUBLIC viewer projection. Pass the deep copy
// from LoadPoolMatches.
// Rev/RevSession are also zeroed for consistency with matchForBroadcast; disk-loaded
// matches already have these zero, so this is defense-in-depth rather than
// functional correction.
func stripMatchesAudit(ms []state.MatchResult) {
	for i := range ms {
		ms[i].CorrectionReason = ""
		ms[i].DecisionReason = ""
		ms[i].Rev = 0
		ms[i].RevSession = ""
	}
}

// stripBracketAudit clears the audit free-text (CorrectionReason, DecisionReason)
// on every bracket match in place, for a PUBLIC viewer projection. Pass the deep
// copy from LoadBracket; nil is a no-op.
func stripBracketAudit(b *state.Bracket) {
	if b == nil {
		return
	}
	for ri := range b.Rounds {
		for j := range b.Rounds[ri] {
			b.Rounds[ri][j].CorrectionReason = ""
			b.Rounds[ri][j].DecisionReason = ""
		}
	}
	// ThirdPlaceMatch (Naginata bronze) is a sibling of Rounds; its audit
	// fields must also be stripped from the public viewer projection (Finding 2).
	if b.ThirdPlaceMatch != nil {
		b.ThirdPlaceMatch.CorrectionReason = ""
		b.ThirdPlaceMatch.DecisionReason = ""
	}
}

// lineupForPublic returns the lineup as-is for the public read endpoints.
// The map values returned by LoadTeamLineups are already copies, so this
// function exists as a clear hand-off point if future redaction is needed.
func lineupForPublic(l domain.TeamLineup) domain.TeamLineup {
	return l
}

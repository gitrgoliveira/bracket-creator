package state

import (
	"encoding/json"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// TeamResultLine is the authoritative team-match summary attached to the wire
// payload (HTTP responses and SSE match_updated events) so the frontend renders
// individual victories (IV) and points won (PW) directly rather than
// re-deriving them from sub-bouts. Shiro is SideB (rendered left), Aka is SideA
// (rendered right), matching the display convention used across the app.
type TeamResultLine struct {
	ShiroIV int `json:"shiroIV"`
	AkaIV   int `json:"akaIV"`
	ShiroPW int `json:"shiroPW"`
	AkaPW   int `json:"akaPW"`
}

// countScoringIppons is the package-local spelling of domain.CountScoringIppons
// (real ippon marks, ignoring empties and the "•" unfilled-slot placeholder).
// It used to be a hand-copy carrying a "keep in sync with engine" note; state
// cannot import engine, but both can import domain, so the rule now has one
// owner instead of two copies that agreed by discipline.
func countScoringIppons(ippons []string) int {
	return domain.CountScoringIppons(ippons)
}

// TeamResultFrom aggregates sub-bouts into IV and PW per side. It is the single
// source of truth for the team-match summary: the daihyosen placeholder
// (Position <= DaihyosenSubPosition, the -1 daihyosen or any negative) is skipped so a re-validated tie does
// not double-count, IV counts sub-bout winners through subBoutWinnerSide (member
// ids first, then names, and neither side when two fighters share a name and the
// ids cannot decide), and PW counts every scored ippon regardless of bout outcome (a drawn bout where
// both sides scored still contributes), skipping unfilled "•" placeholder slots
// via countScoringIppons. SideA is Aka, SideB is Shiro. Returns nil when there
// are no countable sub-bouts (an individual match, or a slice containing
// only the daihyosen placeholder with Position == DaihyosenSubPosition (-1)).
// engine.ComputeTeamSummary delegates here.
func TeamResultFrom(subResults []SubMatchResult, sideAName, sideBName string) *TeamResultLine {
	if len(subResults) == 0 {
		return nil
	}
	line := &TeamResultLine{}
	hasBout := false
	for _, sub := range subResults {
		if sub.Position <= DaihyosenSubPosition {
			// Skip the daihyosen placeholder (DaihyosenSubPosition, -1) and,
			// defensively, any other negative position: real bouts have a
			// non-negative Position (fixed-format 0-based, kachinuki 1-based),
			// so a Position < -1 is malformed input and must not count into
			// IV/PW.
			continue
		}
		hasBout = true
		switch subBoutWinnerSide(sub, sideAName, sideBName) {
		case domain.MatchSideA:
			line.AkaIV++
		case domain.MatchSideB:
			line.ShiroIV++
		}
		line.AkaPW += countScoringIppons(sub.IpponsA)
		line.ShiroPW += countScoringIppons(sub.IpponsB)
	}
	if !hasBout {
		return nil
	}
	return line
}

// subBoutWinnerSide answers "which side won this sub-bout", and is the one
// owner of that question for the team summary. Two tiers, in this order.
//
// MEMBER IDS FIRST (operator ruling bc-pnum, "this should only use the
// IDs"). A kachinuki bout row carries SideAMemberID/SideBMemberID, stamped
// when the pairing is appended, and WinnerMemberID, stamped by the score
// editor from the side the operator actually picked. Three ids present and
// the winner's matching one of them is a fact about identity, not about
// spelling, so it settles the row outright. Routed through
// domain.AttributeWinnerSide, the same owner the match-level triple uses,
// which returns MatchSideNone when the ids cannot decide (one absent, or a
// winner id matching neither side) and so falls through to the names.
//
// NAMES SECOND, and only where names can actually tell the two sides
// apart. This is the pre-existing rule and still carries the quick-score
// synth path, where a bout row names the TEAMS rather than two fighters.
//
// The guard between the tiers is the point of the change: when both sides
// hold the SAME non-empty name, a name comparison cannot say who won, and
// the old code's case order silently handed every such bout to Aka. Two
// opposing fighters may legally share a display name, so that was a coin
// flip written into the standings. Such a bout now counts for NEITHER side
// until its ids can speak, which is the same refusal every other id repair
// in this package makes rather than guess. A bout scored through the
// editor since bc-pnum carries the winner's member id and is unaffected;
// what loses an IV here is drifted or legacy data that never recorded who
// won in a form that survives two fighters sharing a name.
func subBoutWinnerSide(sub SubMatchResult, sideAName, sideBName string) domain.MatchSide {
	if side := domain.AttributeWinnerSide(domain.SubBoutAttribution(
		sub.Winner, sub.SideA, sub.SideB,
		sub.WinnerMemberID, sub.SideAMemberID, sub.SideBMemberID,
	)); side != domain.MatchSideNone {
		return side
	}
	if sub.Winner == "" || (sub.SideA != "" && sub.SideA == sub.SideB) {
		return domain.MatchSideNone
	}
	// The fighter-name comparison is repeated here rather than left to the
	// call above, because AttributeWinnerSide's id branch SHORT-CIRCUITS: a
	// row carrying all three ids whose winner id matches neither side
	// returns no side and never reaches its own name tier. That row is
	// drifted data whose NAMES can still tell the two fighters apart, so it
	// is attributed rather than dropped. The team-name arms carry the
	// quick-score synth path, where a bout row names the two TEAMS.
	switch {
	case sub.Winner == sideAName || (sub.SideA != "" && sub.Winner == sub.SideA):
		return domain.MatchSideA
	case sub.Winner == sideBName || (sub.SideB != "" && sub.Winner == sub.SideB):
		return domain.MatchSideB
	}
	return domain.MatchSideNone
}

// TeamResult returns the team-match summary for this match, or nil for an
// individual match. See TeamResultFrom.
func (m *MatchResult) TeamResult() *TeamResultLine {
	if m == nil {
		return nil
	}
	return TeamResultFrom(m.SubResults, m.SideA, m.SideB)
}

// MarshalJSON augments the wire form of a MatchResult with the computed
// teamResult (IV/PW) for team matches. The alias type sheds MarshalJSON to
// avoid infinite recursion while preserving every field tag, so the payload is
// byte-identical apart from the added, omitempty teamResult object. This is the
// single serialization choke point for every read path (pool-matches, bracket,
// schedule, SSE), so the frontend never re-derives PW. MatchResult is wire-only
// JSON (pool matches persist to CSV, bracket matches to bracket.json via
// BracketMatch), so this does not affect on-disk state.
func (m MatchResult) MarshalJSON() ([]byte, error) {
	type alias MatchResult
	return json.Marshal(struct {
		alias
		TeamResult *TeamResultLine `json:"teamResult,omitempty"`
	}{alias: alias(m), TeamResult: m.TeamResult()})
}

// TeamResult returns the team-match summary for this bracket match, or nil
// for an individual match. See TeamResultFrom.
func (m *BracketMatch) TeamResult() *TeamResultLine {
	if m == nil {
		return nil
	}
	return TeamResultFrom(m.SubResults, m.SideA, m.SideB)
}

// MarshalJSON mirrors MatchResult.MarshalJSON for bracket (elimination)
// matches: without it, knockout team matches reached the frontend with no
// teamResult and every score surface fell back to the legacy IV-only string
// while pool matches showed IV and PW (bead mp-8b1b). Unlike MatchResult,
// BracketMatch also persists to bracket.json through this same marshal, so
// the derived teamResult lands on disk too; that is deliberate (one choke
// point, no wire-vs-disk type to drift) and safe: the struct has no
// TeamResult field, so loading ignores it and every save recomputes it from
// SubResults.
func (m BracketMatch) MarshalJSON() ([]byte, error) {
	type alias BracketMatch
	return json.Marshal(struct {
		alias
		TeamResult *TeamResultLine `json:"teamResult,omitempty"`
	}{alias: alias(m), TeamResult: m.TeamResult()})
}

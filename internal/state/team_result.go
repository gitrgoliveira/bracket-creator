package state

import "encoding/json"

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

// countScoringIppons counts real ippon marks in an ippons slice, ignoring
// empty entries and the "•" placeholder the UI uses for an unfilled slot.
// Mirrors engine.countScoringIppons (internal/engine/scoring.go): state
// cannot import engine (engine already imports state), so this is a local
// copy. Keep the two in sync if the placeholder semantics ever change.
func countScoringIppons(ippons []string) int {
	n := 0
	for _, v := range ippons {
		if v != "" && v != "•" {
			n++
		}
	}
	return n
}

// TeamResultFrom aggregates sub-bouts into IV and PW per side. It is the single
// source of truth for the team-match summary: the daihyosen placeholder
// (Position <= DaihyosenSubPosition, the -1 daihyosen or any negative) is skipped so a re-validated tie does
// not double-count, IV counts sub-bout winners via the same side-matching
// fallback as scoring (winner may carry the match-level or sub-level side name),
// and PW counts every scored ippon regardless of bout outcome (a drawn bout where
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
		switch {
		case sub.Winner == sideAName || (sub.SideA != "" && sub.Winner == sub.SideA):
			line.AkaIV++
		case sub.Winner == sideBName || (sub.SideB != "" && sub.Winner == sub.SideB):
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

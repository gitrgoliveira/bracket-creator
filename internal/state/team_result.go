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

// TeamResultFrom aggregates sub-bouts into IV and PW per side. It is the single
// source of truth for the team-match summary: the daihyosen placeholder
// (Position < 0) is skipped so a re-validated tie does not double-count, IV
// counts sub-bout winners via the same side-matching fallback as scoring
// (winner may carry the match-level or sub-level side name), and PW counts every
// ippon scored regardless of bout outcome (a drawn bout where both sides scored
// still contributes). SideA is Aka, SideB is Shiro. Returns nil when there are
// no countable sub-bouts (an individual match, or a slice containing only the
// daihyosen placeholder with Position < 0). engine.ComputeTeamSummary delegates here.
func TeamResultFrom(subResults []SubMatchResult, sideAName, sideBName string) *TeamResultLine {
	if len(subResults) == 0 {
		return nil
	}
	line := &TeamResultLine{}
	hasBout := false
	for _, sub := range subResults {
		if sub.Position < 0 {
			continue // skip the daihyosen placeholder itself
		}
		hasBout = true
		switch {
		case sub.Winner == sideAName || (sub.SideA != "" && sub.Winner == sub.SideA):
			line.AkaIV++
		case sub.Winner == sideBName || (sub.SideB != "" && sub.Winner == sub.SideB):
			line.ShiroIV++
		}
		line.AkaPW += len(sub.IpponsA)
		line.ShiroPW += len(sub.IpponsB)
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

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
// not double-count, IV counts sub-bout winners through SubBoutWinnerSide (member
// ids first, then names, and neither side when two fighters share a name and the
// ids cannot decide), and PW counts every scored ippon regardless of bout outcome (a drawn bout where
// both sides scored still contributes), skipping unfilled "•" placeholder slots
// via countScoringIppons. SideA is Aka, SideB is Shiro. Returns nil when there
// are no countable sub-bouts (an individual match, or a slice containing
// only the daihyosen placeholder with Position == DaihyosenSubPosition (-1)).
// engine.ComputeTeamSummary delegates here.
//
// credit is REQUIRED (bc-cse: a variadic credit let a caller silently skip
// the default-win rule, which handlers_daihyosen.go's tie check did until
// this change; see ComputeTeamSummary's own doc comment for that call
// site). A caller that knows the match's default-win crediting
// (MatchResult.TeamResult, BracketMatch.TeamResult, the Excel export)
// passes exactly one domain.MatchSide from DefaultWinCreditSide. A
// numbered bout (Position >= 1) with no result of its own is then routed
// through SubBoutEffectiveResult, which credits it to that side the same
// way a played bout with its own per-bout fusensho decision already counts
// (operator ruling: every bout of a non-kachinuki team match a default-win
// decision closes has a result or is credited one). Passing
// domain.MatchSideNone reproduces the pre-credit behaviour exactly: an
// unfought bout contributes nothing; use it explicitly, with a short
// comment, only where no default-win ruling can possibly be in force.
func TeamResultFrom(subResults []SubMatchResult, sideAName, sideBName string, credit domain.MatchSide) *TeamResultLine {
	if len(subResults) == 0 {
		return nil
	}
	line := &TeamResultLine{}
	hasBout := false
	for _, sub := range subResults {
		if sub.Position <= DaihyosenSubPosition {
			// Skip the daihyosen placeholder (DaihyosenSubPosition, -1) and,
			// defensively, any other negative position: real bouts are
			// numbered from 1 in both formats (see DaihyosenSubPosition), so a
			// Position < -1 is malformed input and must not count into IV/PW.
			//
			// Position 0 is unproducible too, and this tally and the Excel
			// export answer for it differently ON PURPOSE: the export's
			// writeTeamSubMatchScores drops it as a BOUNDS check, because it
			// addresses a row as subStartExcelRow+(Position-1) and a 0 would
			// write above the grid. One is a count, the other is a cell
			// address; there is nothing to reconcile.
			continue
		}
		hasBout = true
		// SubBoutEffectiveResult substitutes the FIK default-win maru for an
		// unfought bout on a match a default-win ruling closed (credit names
		// the side; DefaultWinCreditSide), so a kiken/fusenpai/fusensho
		// declared before every numbered bout was scored still credits IV/PW
		// for the positions nobody fought, the same way a played bout with
		// its own per-bout fusensho decision already does. A real result on
		// the row, or no credit to apply, returns it unchanged.
		outcome := SubBoutEffectiveResult(sub, credit, sideAName, sideBName)
		switch SubBoutWinnerSide(outcome, sideAName, sideBName) {
		case domain.MatchSideA:
			line.AkaIV++
		case domain.MatchSideB:
			line.ShiroIV++
		}
		line.AkaPW += countScoringIppons(outcome.IpponsA)
		line.ShiroPW += countScoringIppons(outcome.IpponsB)
	}
	if !hasBout {
		return nil
	}
	return line
}

// SubBoutWinnerSide answers "which side won this sub-bout", and is the one
// owner of that question: the wire summary (TeamResultFrom, just above) and
// the standings accrual (engine.accrueTeamSubResults) both route through it,
// so the IV a spectator reads and the IV the tie-break ranks by cannot
// disagree. Two tiers, in this order.
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
//
// The Excel export's writeTeamSubMatchScores (internal/export/builder.go)
// reads the same match-level rule and reads it NARROWER: it substitutes the
// encounter's names only when the row names NO fighter, so a row that does
// name its fighters keeps deciding for itself. The two look unifiable and are
// not. Take a row naming Kenji and Taro whose Winner drifted to the team name
// Kyoto: this function attributes it to A through the sideAName arm below,
// while the export leaves it unmarked rather than print a default-win maru
// beside a fighter the winner does not name. Standings counting a bout the
// sheet declines to mark is the accepted asymmetry, so hoisting either
// reading onto the other is a behaviour change, not a tidy-up.
func SubBoutWinnerSide(sub SubMatchResult, sideAName, sideBName string) domain.MatchSide {
	if side := domain.AttributeWinnerSide(domain.SubBoutAttribution(sub.Attribution())); side != domain.MatchSideNone {
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

// DefaultWinCreditSide reports which side a default-win ruling (any kiken,
// fusenpai, or fusensho: domain.IsDefaultWinDecisionStr) closing THIS match
// credits every unfought numbered bout to -- domain.MatchSideNone for
// anything else: an individual match, a running/reopened match (the ruling
// only ever credits a COMPLETED match), or a completed match some other
// decision closed (fought, hikiwake, daihyosen, kachinuki-exhaustion).
//
// decisionBy names the side that WITHDREW or was BARRED ("aka" = SideA,
// "shiro" = SideB; see engine.recordDecisionTx / keptWithdrawalScoreline),
// so the credited side is the OTHER one. decisionBy is empty only for a
// ruling recorded before it was captured (legacy data); the credited side
// then falls back to the match's own winner attribution
// (domain.AttributeWinnerSide, ids first, names second).
//
// att is built via MatchResult.Attribution() / BracketMatch.Attribution()
// so a caller never hand-transposes the six identity fields.
func DefaultWinCreditSide(status MatchStatus, decision, decisionBy string, att domain.WinnerAttribution) domain.MatchSide {
	if status != MatchStatusCompleted || !domain.IsDefaultWinDecisionStr(decision) {
		return domain.MatchSideNone
	}
	switch decisionBy {
	case "aka": // SideA withdrew or was barred; the OTHER side (Shiro) is credited.
		return domain.MatchSideB
	case "shiro": // SideB withdrew or was barred; the OTHER side (Aka) is credited.
		return domain.MatchSideA
	}
	return domain.AttributeWinnerSide(att)
}

// SubBoutEffectiveResult returns sub's EFFECTIVE result for IV/PW and Excel
// export purposes: unchanged when it already carries one (HasResult), or,
// when it carries none and credit names a side (DefaultWinCreditSide), a
// synthetic row crediting that side the FIK default-win maru (Art. 32,
// domain.DefaultWinIppons) -- Winner set to the credited side's team name,
// SideA/SideB left exactly as the stored row held them (matching the
// existing quick-score / per-bout-fusensho shape: a fixed-order bout
// records no per-fighter identity, operator ruling bc-dnst).
//
// Decision is deliberately left untouched -- never set to the match's own
// decision: a reader shows the maru without a per-row Kiken/Fus. mark,
// because that mark names the ONE competitor who withdrew and already rides
// the match-level summary row's IV cell; repeating it on every credited
// bout would misname each fighter as having personally defaulted.
//
// credit is domain.MatchSideNone for anything but a completed default-win
// ruling (see DefaultWinCreditSide), and the daihyosen placeholder
// (Position < 1) is never synthesized, so a caller may call this
// unconditionally for every row in a match's SubResults.
func SubBoutEffectiveResult(sub SubMatchResult, credit domain.MatchSide, sideAName, sideBName string) SubMatchResult {
	if sub.Position < 1 || sub.HasResult() || credit == domain.MatchSideNone {
		return sub
	}
	out := sub
	maru := domain.DefaultWinIppons(false)
	switch credit {
	case domain.MatchSideA:
		out.Winner = sideAName
		out.IpponsA = maru
	case domain.MatchSideB:
		out.Winner = sideBName
		out.IpponsB = maru
	}
	return out
}

// PadDefaultWinBoutPositions returns subResults with an EMPTY SubMatchResult
// row (Position only; no winner, decision or ippons) appended for every
// numbered position 1..teamSize it does not already carry. A position
// already present -- fought, previously padded, or otherwise recorded -- is
// left untouched, and the daihyosen row (Position < 1) is never added here.
//
// Both the live write path (engine.RecordMatchResultWithIneligibilityTx, on
// a completed default-win team-match write) and the legacy-load repair
// (state.EnsureLegacyUpgraded) call this ONE function, so a completed
// default-win team match ends up with one row per position whoever wrote it
// or when. That matters because SubBoutEffectiveResult / DefaultWinCreditSide
// can only credit a Position actually PRESENT in SubResults -- a position
// missing from the slice entirely is invisible to every reader that ranges
// over it (TeamResultFrom, engine.accrueTeamSubResults, the Excel export),
// credit side or no.
func PadDefaultWinBoutPositions(subResults []SubMatchResult, teamSize int) []SubMatchResult {
	present := make(map[int]bool, len(subResults))
	for _, s := range subResults {
		present[s.Position] = true
	}
	out := subResults
	for pos := 1; pos <= teamSize; pos++ {
		if !present[pos] {
			out = append(out, SubMatchResult{Position: pos})
		}
	}
	return out
}

// NeedsDefaultWinBoutPadding reports whether a stored match needs
// PadDefaultWinBoutPositions applied: completed, both sides named, closed by
// a default-win decision (domain.IsDefaultWinDecisionStr; a caller correcting
// a match while KEEPING a stored withdrawal ruling passes that stored
// decision here, since its own incoming Decision is blank until the ruling
// is reinstated downstream), not a pool daihyosen/tiebreaker row
// (IsPoolDaihyosenMatchID / IsTiebreakerMatchID), and missing at least one
// numbered position 1..teamSize.
//
// subResultsUnreadable must be the SAME row's flag (MatchResult's own field
// for a pool match; a bracket match carries no such flag because
// bracket.json parses as one atomic document, so pass false there). A cell
// that failed to parse loads with SubResults empty and the corrupt bytes
// retained separately (MatchResult.SubResultsRaw) for repair; padding it
// anyway would fill SubResults with placeholder rows, making it non-empty
// and so defeating the raw-bytes preservation on the next write -- the very
// data loss this predicate exists to avoid. Shared by the legacy-load repair
// (state.EnsureLegacyUpgraded) and the write-time gate
// (engine.RecordMatchResultWithIneligibilityTx) so the two never drift.
func NeedsDefaultWinBoutPadding(status MatchStatus, decision, sideA, sideB, id string, subResults []SubMatchResult, subResultsUnreadable bool, teamSize int) bool {
	if status != MatchStatusCompleted || sideA == "" || sideB == "" {
		return false
	}
	if subResultsUnreadable {
		return false
	}
	if !domain.IsDefaultWinDecisionStr(decision) {
		return false
	}
	if IsPoolDaihyosenMatchID(id) || IsTiebreakerMatchID(id) {
		return false
	}
	present := make(map[int]bool, len(subResults))
	for _, sub := range subResults {
		present[sub.Position] = true
	}
	for pos := 1; pos <= teamSize; pos++ {
		if !present[pos] {
			return true
		}
	}
	return false
}

// TeamResult returns the team-match summary for this match, or nil for an
// individual match. See TeamResultFrom.
func (m *MatchResult) TeamResult() *TeamResultLine {
	if m == nil {
		return nil
	}
	credit := DefaultWinCreditSide(m.Status, m.Decision, m.DecisionBy, m.Attribution())
	return TeamResultFrom(m.SubResults, m.SideA, m.SideB, credit)
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
	credit := DefaultWinCreditSide(m.Status, m.Decision, m.DecisionBy, m.Attribution())
	return TeamResultFrom(m.SubResults, m.SideA, m.SideB, credit)
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

package state

import "github.com/gitrgoliveira/bracket-creator/internal/domain"

// This file folds TWO legacies, in order, at every bracket read boundary:
//
//  1. The rendered score STRING. A pre-array bracket.json rendered each
//     side's scoreline as one ScoreA/ScoreB string via the (now-removed)
//     domain.FormatScore codec; a current file carries ippon arrays plus
//     hansoku ints directly, the same shape as SubMatchResult. BracketMatch's
//     NormalizeLegacy decodes a non-empty legacy string into the arrays via
//     domain.ParseScore and clears both strings. Arrays WIN when a
//     hand-edited file carries both: the strings are cleared whenever the
//     arrays are already populated too, so the two representations can never
//     diverge after a normalize pass.
//  2. The hantei verdict, recorded as domain.HanteiMark in the WINNER's
//     ippon slice (operator ruling 2026-08-21: "Ht should be recorded as
//     just another ippon"). The decidedByHantei fields on MatchResult,
//     SubMatchResult and BracketMatch are LEGACY READ-ONLY channels:
//     everything below converts a flagged verdict into the mark at a read
//     boundary, in line with the same day's legacy policy (converted upon
//     reading, not supported as a standing dual representation). Writers
//     never set the fields; new files and payloads never carry them.
//
// Conversion sites: parsePoolMatchesRecords (SubResults JSON inside the CSV
// cell), LoadBracket (bracket.json matches and their sub-bouts), and the
// score-request decode in mobileapp (offline queues can replay pre-upgrade
// payloads for hours after a binary upgrade).
//
// The legacy tri-state maps onto the unified representation naturally:
// true → the mark in the winner's slice; explicit false → no mark (stripped);
// nil → the ippons say whatever they say. A flagged verdict with no
// attributable winner is DROPPED, the same degradation the old pool encoder
// applied: validation requires a winner, so that shape is malformed data, and
// guessing a side would be worse than losing the flag.

// foldLegacyHantei is the one fold this file exists for, extracted so the
// slice-based normalizers below (SubMatchResult, MatchResult) state the rule
// ONCE rather than as two switches that drifted into cosmetic variation:
//
//   - flagged == false: strip any stale mark from both sides (an explicit
//     false is a withdrawal of a previously-recorded verdict).
//   - flagged == true, winner resolves to side A: append the mark to A's slice.
//   - flagged == true, winner resolves to side B: append the mark to B's slice.
//   - flagged == true, winner unattributable (empty, or matching neither
//     named side): drop the flag and leave both slices untouched — this
//     is checked FIRST so an empty winner can never spuriously satisfy
//     "winner == sideA" against an equally-empty sideA.
//
// AppendHantei is idempotent (never doubles the mark), so calling this
// twice on the same slices is safe.
func foldLegacyHantei(flagged bool, ids sideIDs, winner, sideA, sideB string, ipponsA, ipponsB []string) ([]string, []string) {
	if !flagged {
		return domain.StripHantei(ipponsA), domain.StripHantei(ipponsB)
	}
	// Attribution goes through the one owner, domain.AttributeWinnerSide, so a
	// legacy flag lands on the same side every other surface would choose:
	// by participant id when the caller has all three (a same-name pair is
	// only separable that way), else by name with the sideA-first fallback.
	// Callers without ids (sub-bouts, bracket matches) pass the zero value and
	// take the name path, which is byte-identical to the pre-id behaviour.
	switch domain.AttributeWinnerSide(ids.winner, ids.sideA, ids.sideB, winner, sideA, sideB) {
	case domain.MatchSideA:
		return domain.AppendHantei(ipponsA), ipponsB
	case domain.MatchSideB:
		return ipponsA, domain.AppendHantei(ipponsB)
	default:
		return ipponsA, ipponsB // unattributable: drop, never guess
	}
}

// sideIDs carries the participant ids for an attribution, so the fold's
// signature does not grow three more bare strings that are easy to transpose
// at a call site. The zero value means "this record has no ids" (SubMatchResult
// and BracketMatch both persist names only).
type sideIDs struct{ winner, sideA, sideB string }

// normalizeLegacyHantei folds a legacy sub-bout flag into the mark.
func (s *SubMatchResult) normalizeLegacyHantei() {
	if s.DecidedByHantei == nil {
		return
	}
	flagged := *s.DecidedByHantei
	s.DecidedByHantei = nil
	s.IpponsA, s.IpponsB = foldLegacyHantei(flagged, sideIDs{}, s.Winner, s.SideA, s.SideB, s.IpponsA, s.IpponsB)
}

// NormalizeLegacyHantei folds legacy flags into the mark, match-level and
// per sub-bout. Idempotent (AppendHantei never doubles the mark), so it is
// safe at every read boundary a payload might cross twice.
func (m *MatchResult) NormalizeLegacyHantei() {
	if m.DecidedByHantei != nil {
		flagged := *m.DecidedByHantei
		m.DecidedByHantei = nil
		m.IpponsA, m.IpponsB = foldLegacyHantei(flagged, sideIDs{winner: m.WinnerID, sideA: m.SideAID, sideB: m.SideBID}, m.Winner, m.SideA, m.SideB, m.IpponsA, m.IpponsB)
	}
	for i := range m.SubResults {
		m.SubResults[i].normalizeLegacyHantei()
	}
}

// NormalizeLegacy folds both bracket-level legacies described in this file's
// header, in order: first the rendered score strings into the ippon arrays,
// then the legacy hantei flag into the mark inside those arrays. Idempotent,
// like every fold in this file, so it is safe at every read boundary a
// bracket match might cross twice.
func (b *BracketMatch) NormalizeLegacy() {
	// 1. Score strings -> arrays. Arrays win: whenever they are already
	// populated the strings are cleared without being decoded, so a
	// hand-edited file carrying both legacy strings and current arrays can
	// never have the two diverge after this pass.
	if b.IpponsA != nil || b.IpponsB != nil {
		b.ScoreA = ""
		b.ScoreB = ""
	} else if b.ScoreA != "" || b.ScoreB != "" {
		b.IpponsA, b.HansokuA = domain.ParseScore(b.ScoreA)
		b.IpponsB, b.HansokuB = domain.ParseScore(b.ScoreB)
		b.ScoreA = ""
		b.ScoreB = ""
	}
	// 2. Legacy hantei flag -> mark. The bool flag has no explicit-false to
	// honour: false was always simply "not a hantei", so this composes the
	// shared fold only for the flagged==true case rather than plumbing a
	// bool through foldLegacyHantei's signature. No winner pre-check here:
	// foldLegacyHantei's own `case "":` already drops an unattributable
	// winner untouched, so gating the call on b.Winner != "" would only
	// duplicate that rule at a second enforcement point.
	if b.DecidedByHantei {
		b.DecidedByHantei = false
		b.IpponsA, b.IpponsB = foldLegacyHantei(true, sideIDs{}, b.Winner, b.SideA, b.SideB, b.IpponsA, b.IpponsB)
	}
	for i := range b.SubResults {
		b.SubResults[i].normalizeLegacyHantei()
	}
}

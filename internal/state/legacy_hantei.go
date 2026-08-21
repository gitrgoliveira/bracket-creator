package state

import "github.com/gitrgoliveira/bracket-creator/internal/domain"

// The hantei verdict is recorded as domain.HanteiMark in the WINNER's ippon
// slice (operator ruling 2026-08-21: "Ht should be recorded as just another
// ippon"). The decidedByHantei fields on MatchResult, SubMatchResult and
// BracketMatch are LEGACY READ-ONLY channels: everything below converts a
// flagged verdict into the mark at a read boundary, in line with the same
// day's legacy policy (converted upon reading, not supported as a standing
// dual representation). Writers never set the fields; new files and payloads
// never carry them.
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
//   - flagged == true, winner == sideA: append the mark to A's slice.
//   - flagged == true, winner == sideB: append the mark to B's slice.
//   - flagged == true, winner unattributable (empty, or matching neither
//     named side): drop the flag and leave both slices untouched — this
//     is checked FIRST so an empty winner can never spuriously satisfy
//     "winner == sideA" against an equally-empty sideA.
//
// AppendHantei is idempotent (never doubles the mark), so calling this
// twice on the same slices is safe.
func foldLegacyHantei(flagged bool, winner, sideA, sideB string, ipponsA, ipponsB []string) ([]string, []string) {
	if !flagged {
		return domain.StripHantei(ipponsA), domain.StripHantei(ipponsB)
	}
	switch winner {
	case "":
		return ipponsA, ipponsB // unattributable: drop, never guess
	case sideA:
		return domain.AppendHantei(ipponsA), ipponsB
	case sideB:
		return ipponsA, domain.AppendHantei(ipponsB)
	default:
		return ipponsA, ipponsB
	}
}

// normalizeLegacyHantei folds a legacy sub-bout flag into the mark.
func (s *SubMatchResult) normalizeLegacyHantei() {
	if s.DecidedByHantei == nil {
		return
	}
	flagged := *s.DecidedByHantei
	s.DecidedByHantei = nil
	s.IpponsA, s.IpponsB = foldLegacyHantei(flagged, s.Winner, s.SideA, s.SideB, s.IpponsA, s.IpponsB)
}

// NormalizeLegacyHantei folds legacy flags into the mark, match-level and
// per sub-bout. Idempotent (AppendHantei never doubles the mark), so it is
// safe at every read boundary a payload might cross twice.
func (m *MatchResult) NormalizeLegacyHantei() {
	if m.DecidedByHantei != nil {
		flagged := *m.DecidedByHantei
		m.DecidedByHantei = nil
		m.IpponsA, m.IpponsB = foldLegacyHantei(flagged, m.Winner, m.SideA, m.SideB, m.IpponsA, m.IpponsB)
	}
	for i := range m.SubResults {
		m.SubResults[i].normalizeLegacyHantei()
	}
}

// NormalizeLegacyHantei folds a legacy bracket flag into the mark inside the
// winner's rendered score string (BracketMatch persists each side's ippons
// as one domain.FormatScore string). The bool flag has no explicit-false to
// honour: false was always simply "not a hantei", so this composes the
// shared fold with the score codec only for the flagged==true case rather
// than plumbing a bool through a rendered-string signature.
func (b *BracketMatch) NormalizeLegacyHantei() {
	if b.DecidedByHantei {
		b.DecidedByHantei = false
		if b.Winner != "" {
			ipponsA, hansokuA := domain.ParseScore(b.ScoreA)
			ipponsB, hansokuB := domain.ParseScore(b.ScoreB)
			ipponsA, ipponsB = foldLegacyHantei(true, b.Winner, b.SideA, b.SideB, ipponsA, ipponsB)
			b.ScoreA = domain.FormatScore(ipponsA, hansokuA)
			b.ScoreB = domain.FormatScore(ipponsB, hansokuB)
		}
	}
	for i := range b.SubResults {
		b.SubResults[i].normalizeLegacyHantei()
	}
}

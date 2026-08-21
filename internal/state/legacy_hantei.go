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

// normalizeLegacyHantei folds a legacy sub-bout flag into the mark.
func (s *SubMatchResult) normalizeLegacyHantei() {
	if s.DecidedByHantei == nil {
		return
	}
	flagged := *s.DecidedByHantei
	s.DecidedByHantei = nil
	if !flagged {
		s.IpponsA = domain.StripHantei(s.IpponsA)
		s.IpponsB = domain.StripHantei(s.IpponsB)
		return
	}
	switch s.Winner {
	case "":
		return // unattributable: drop, never guess
	case s.SideA:
		s.IpponsA = domain.AppendHantei(s.IpponsA)
	case s.SideB:
		s.IpponsB = domain.AppendHantei(s.IpponsB)
	}
}

// NormalizeLegacyHantei folds legacy flags into the mark, match-level and
// per sub-bout. Idempotent (AppendHantei never doubles the mark), so it is
// safe at every read boundary a payload might cross twice.
func (m *MatchResult) NormalizeLegacyHantei() {
	if m.DecidedByHantei != nil {
		flagged := *m.DecidedByHantei
		m.DecidedByHantei = nil
		switch {
		case !flagged:
			m.IpponsA = domain.StripHantei(m.IpponsA)
			m.IpponsB = domain.StripHantei(m.IpponsB)
		case m.Winner == m.SideA && m.SideA != "":
			m.IpponsA = domain.AppendHantei(m.IpponsA)
		case m.Winner == m.SideB && m.SideB != "":
			m.IpponsB = domain.AppendHantei(m.IpponsB)
		}
	}
	for i := range m.SubResults {
		m.SubResults[i].normalizeLegacyHantei()
	}
}

// NormalizeLegacyHantei folds a legacy bracket flag into the mark inside the
// winner's rendered score string (BracketMatch persists each side's ippons
// as one domain.FormatScore string). The bool flag has no explicit-false to
// honour: false was always simply "not a hantei".
func (b *BracketMatch) NormalizeLegacyHantei() {
	if b.DecidedByHantei {
		b.DecidedByHantei = false
		if b.Winner != "" {
			inject := func(score string) string {
				ippons, hansoku := domain.ParseScore(score)
				return domain.FormatScore(domain.AppendHantei(ippons), hansoku)
			}
			switch b.Winner {
			case b.SideA:
				b.ScoreA = inject(b.ScoreA)
			case b.SideB:
				b.ScoreB = inject(b.ScoreB)
			}
		}
	}
	for i := range b.SubResults {
		b.SubResults[i].normalizeLegacyHantei()
	}
}

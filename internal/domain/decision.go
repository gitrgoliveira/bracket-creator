package domain

import "gopkg.in/yaml.v3"

// Decision identifies how a match was concluded.
//
// FR-030, NFR-011, FR-044, see data-model §1.
type Decision string

const (
	DecisionNone                Decision = ""
	DecisionFought              Decision = "fought"
	DecisionHikiwake            Decision = "hikiwake"
	DecisionKiken               Decision = "kiken"
	DecisionKikenVoluntary      Decision = "kiken-voluntary"
	DecisionKikenInjury         Decision = "kiken-injury"
	DecisionFusenpai            Decision = "fusenpai"
	DecisionFusensho            Decision = "fusensho"
	DecisionDaihyosen           Decision = "daihyosen"
	DecisionKachinukiExhaustion Decision = "kachinuki-exhaustion"
	DecisionIpponShobu          Decision = "ippon-shobu"
)

// Valid reports whether d is one of the defined Decision constants
// (including the empty DecisionNone sentinel). Unknown wire values
// return false.
func (d Decision) Valid() bool {
	switch d {
	case DecisionNone, DecisionFought, DecisionHikiwake, DecisionKiken,
		DecisionKikenVoluntary, DecisionKikenInjury,
		DecisionFusenpai, DecisionFusensho, DecisionDaihyosen, DecisionKachinukiExhaustion,
		DecisionIpponShobu:
		return true
	}
	return false
}

// IsKikenDecision reports whether d is any kiken variant (legacy,
// voluntary, or injury). Use this instead of comparing against
// DecisionKiken alone, the legacy value is kept for backward
// compatibility but new code should use the specific sub-types.
func IsKikenDecision(d Decision) bool {
	return d == DecisionKiken || d == DecisionKikenVoluntary || d == DecisionKikenInjury
}

// IsHanteiCompatibleDecision reports whether a decision can coexist with a
// hantei verdict ON A SUB-BOUT. Hantei declares a winner from a TIED bout, so
// it is incompatible with any decision that already settles the bout another
// way (a withdrawal, a no-show, a draw). "" and "fought" are ordinary play;
// "daihyosen" is the rep-bout placeholder the verdict rides on.
//
// One owner because there are two enforcers at different layers: the HTTP
// validator (validateSubBout) rejects an incompatible pairing on the way in,
// and the engine (preserveSubHantei) must apply the SAME test on the way out,
// since it mutates a row AFTER validation and its output is never re-checked.
// Two copies could drift such that the engine stamps a row the validator
// would refuse.
func IsHanteiCompatibleDecision(d Decision) bool {
	switch d {
	case DecisionNone, DecisionFought, DecisionDaihyosen:
		return true
	}
	return false
}

// IsHanteiCompatibleDecisionStr is the wire-string form.
func IsHanteiCompatibleDecisionStr(s string) bool {
	return IsHanteiCompatibleDecision(Decision(s))
}

// IsMatchHanteiCompatibleDecision is the MATCH-level twin, and is deliberately
// NARROWER: it is the sub-bout set minus "daihyosen".
//
// A daihyosen IS a bout, so the verdict rides on the rep-bout sub-row
// (position -1), where the sub-bout predicate allows it. At match level the
// same value would claim the ENCOUNTER itself was decided by judges, which is
// exactly what the rep bout exists to avoid.
//
// Split out for the same reason its sibling is shared: the match level also has
// two enforcers that must not drift. ScoreRequest.Validate rejects the pairing
// on the way in, and engine.hanteiStillHolds re-applies it when carrying a
// stored verdict onto a verdict-silent write — which happens after validation
// and is never re-checked. Those two used to be a hand-written switch and a
// call to the SUB-bout predicate, so the engine could stamp a match-level
// hantei onto a "daihyosen" decision that the validator would have refused.
func IsMatchHanteiCompatibleDecision(d Decision) bool {
	switch d {
	case DecisionNone, DecisionFought:
		return true
	}
	return false
}

// IsMatchHanteiCompatibleDecisionStr is the wire-string form.
func IsMatchHanteiCompatibleDecisionStr(s string) bool {
	return IsMatchHanteiCompatibleDecision(Decision(s))
}

// IsKikenDecisionStr is the string-argument twin of IsKikenDecision,
// for call sites that hold the wire value as a string (e.g.
// MatchResult.Decision).
func IsKikenDecisionStr(s string) bool {
	return IsKikenDecision(Decision(s))
}

// IsDefaultWinDecisionStr reports whether the decision awards the match
// points without a technique — the "default win" class (any kiken,
// fusenpai, or fusensho) whose awarded points record as maru. These
// decisions apply only before any point has been scored. Mirrors
// isDefaultWinBC in web-mobile/js/bracket.jsx.
func IsDefaultWinDecisionStr(s string) bool {
	return IsKikenDecisionStr(s) || s == string(DecisionFusenpai) || s == string(DecisionFusensho)
}

// DefaultWinIppon is the FIK maru "○" (U+25CB) recorded for each point a
// default win awards without a technique. Exported so consumers can filter
// it out of a struck-ippon count (e.g. engine.struckIppons, which must tell
// an awarded maru apart from a real struck point) without hardcoding the
// glyph and risking a Unicode lookalike.
const DefaultWinIppon = "○"

// DefaultWinIppons returns the winner's ippon slots for a default win:
// one maru per awarded point, as prescribed by the FIK Regulations of
// Kendo Shiai and Shinpan — Article 32 ("The winner by virtue of
// Articles 30 or 31 shall be given two points ... However, the winner
// will be awarded one point in the case of encho") and the Score Board
// appendix (printed p.15: "Fusen-gachi, Kiken or Shiai-funo ... put one
// mark in case of Encho"). So the two-point pair "○○" in regulation, a
// single deciding "○" in encho (sudden death). THE single Go source of
// the maru-count rule, consumed by the engine's RecordDecision twins
// (the canonical record) and, joined, by the display fallbacks. Mirrors
// defaultWinMaru in web-mobile/js/bracket.jsx (same cells shape).
func DefaultWinIppons(inEncho bool) []string {
	if inEncho {
		return []string{DefaultWinIppon}
	}
	return []string{DefaultWinIppon, DefaultWinIppon}
}

// UnmarshalYAML migrates legacy `decision` values (NFR-025, R6):
//
//   - bool true  → DecisionHikiwake (the historical "draw" flag)
//   - bool false → DecisionFought   (legacy YAML only persisted the
//     field on completed matches; "not a draw" meant the match was
//     fought to a result)
//   - any defined string wire value → that Decision
//   - any other string → DecisionNone (schema-tolerant load)
func (d *Decision) UnmarshalYAML(node *yaml.Node) error {
	if node.Tag == "!!bool" {
		if node.Value == "true" {
			*d = DecisionHikiwake
		} else {
			*d = DecisionFought
		}
		return nil
	}
	candidate := Decision(node.Value)
	if candidate == DecisionKiken {
		*d = DecisionKikenVoluntary
		return nil
	}
	if candidate.Valid() {
		*d = candidate
	} else {
		*d = DecisionNone
	}
	return nil
}

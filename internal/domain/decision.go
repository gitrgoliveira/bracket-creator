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

package domain

import "gopkg.in/yaml.v3"

// Decision identifies how a match was concluded.
//
// FR-030, NFR-011, FR-044 — see data-model §1.
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
// DecisionKiken alone — the legacy value is kept for backward
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

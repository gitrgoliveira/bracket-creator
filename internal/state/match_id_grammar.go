package state

import "strings"

// IsPoolDaihyosenMatchID reports whether a match ID is a pool-stage
// daihyosen (representative) bout (IDs of the form "Pool X-DH-N"), generated
// when all team-pool matches complete with a tie on all 8 ranking criteria.
// These are structurally pool matches but scored as individual (one
// representative per side) rather than as full team bouts.
//
// The canonical home for this id grammar: engine.IsPoolDaihyosenMatchID
// delegates here (engine -> state, never the reverse), so there is exactly
// one definition rather than two that agree only by discipline.
func IsPoolDaihyosenMatchID(matchID string) bool {
	return hasNumericSuffixAfter(matchID, "-DH-")
}

// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N"), injected by
// engine/tiebreaker.go. See IsPoolDaihyosenMatchID's doc for why this
// grammar lives here rather than in engine.
func IsTiebreakerMatchID(matchID string) bool {
	return hasNumericSuffixAfter(matchID, "-TB-")
}

// hasNumericSuffixAfter reports whether id ends with marker followed by one
// or more digits and nothing else, e.g. hasNumericSuffixAfter("Pool A-DH-3",
// "-DH-") is true. A plain strings.Contains(id, marker) would also match a
// REGULAR pool match whose pool name happens to contain the marker, e.g. a
// pool literally named "Pool A-DH-East" produces regular match ids like
// "Pool A-DH-East-0"; that id contains "-DH-" but is not a daihyosen bout
// (its numeric suffix follows a later, unmarked "-"). Anchoring the digits to
// the LAST occurrence of marker rejects that case: the suffix after it is
// "East-0", not all-digits, so it correctly reports false. Mirrors the JS
// twin's anchored regex (pool_ids.jsx DAIHYOSEN_BOUT_RE / SUPPLEMENTARY_BOUT_RE,
// both /-DH-\d+$/ style), which was already correctly suffix-anchored.
func hasNumericSuffixAfter(id, marker string) bool {
	i := strings.LastIndex(id, marker)
	if i < 0 {
		return false
	}
	suffix := id[i+len(marker):]
	if suffix == "" {
		return false
	}
	for _, r := range suffix {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

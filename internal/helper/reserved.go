package helper

import (
	"regexp"
	"strings"
)

// poolFinalistPattern is the pool-finalist placeholder label the engine writes
// into bracket slots: "<PoolName>-<ordinal>", e.g. "Pool A-1st". Pool names are
// generated as "Pool <label>" where label is A–Z for the first 26 pools, then
// AA, BB, ... (same letter doubled) for pools beyond 26.
const poolFinalistPattern = `^Pool .+-\d+(st|nd|rd|th)$`

// winnerOfPattern is the next-round feeder label the engine emits into bracket
// slots: "Winner of r<d>-m<i>", e.g. "Winner of r1-m3".
const winnerOfPattern = `^Winner of r\d+-m\d+$`

// The bracket rule: the two placeholder shapes exactly as the engine writes
// them, case and all. Scoring and the draw gate on these to tell an
// unresolved slot from a competitor.
var (
	reservedPoolFinalistRE = regexp.MustCompile(poolFinalistPattern)
	reservedWinnerOfRE     = regexp.MustCompile(winnerOfPattern)
)

// The roster rule: the same two shapes, ignoring case. See IsReservedRosterName.
var (
	rosterPoolFinalistRE = regexp.MustCompile(`(?i)` + poolFinalistPattern)
	rosterWinnerOfRE     = regexp.MustCompile(`(?i)` + winnerOfPattern)
)

// IsReservedRosterName is the roster rule: whether a person may NOT be called
// name, because the name as the roster stores it (TitleCaseName, which turns
// "pool a-1st" into "Pool A-1st", the placeholder's exact shape) reads as a
// bracket placeholder in any case. "Pool A-1st", "pool a-1st", "Pool A-1St"
// and "POOL A-1ST" are one name here. Every roster save asks it, and so does
// the draw (engine runDrawPipeline), for a roster written before this rule or
// by hand. It differs from IsReservedParticipantName on purpose: that one is
// the bracket rule, asked of a slot that is already stored, and must stay
// case-sensitive so a slot an earlier release stored a competitor in as
// "Pool A-1St" (bout records are never rewritten) is never reread as a
// placeholder.
func IsReservedRosterName(name string) bool {
	n := TitleCaseName(name)
	return rosterPoolFinalistRE.MatchString(n) || rosterWinnerOfRE.MatchString(n)
}

// ReservedRosterNames returns, in roster order, the name of each player the
// roster rule (IsReservedRosterName) refuses, trimmed as it was given. Empty
// when none is refused.
func ReservedRosterNames(players []Player) []string {
	var refused []string
	for _, p := range players {
		if IsReservedRosterName(p.Name) {
			refused = append(refused, strings.TrimSpace(p.Name))
		}
	}
	return refused
}

// IsReservedParticipantName is the bracket rule: whether a stored side is a
// bracket placeholder exactly as the engine writes it (case-sensitive). A
// competitor named like one would be misclassified as an unresolved slot and
// make knockout matches permanently unscoreable, which is why the roster
// refuses such names through IsReservedRosterName, the case-insensitive roster
// rule. Do not use this one to judge a name a person is given.
func IsReservedParticipantName(name string) bool {
	return reservedPoolFinalistRE.MatchString(name) || reservedWinnerOfRE.MatchString(name)
}

// IsPoolFinalistPlaceholder reports whether s is a pool-origin finalist
// placeholder ("Pool A-1st", "Pool B-2nd", etc.) as emitted by the
// pool-to-knockout draw. Unlike IsReservedParticipantName, this does NOT
// match next-round feeder labels ("Winner of r1-m3"), callers that need
// to distinguish the two patterns (e.g. bracketHasPoolPlaceholders) should
// use this instead.
func IsPoolFinalistPlaceholder(s string) bool {
	return reservedPoolFinalistRE.MatchString(s)
}

// IsWinnerOfPlaceholder reports whether s is a next-round feeder label
// ("Winner of r1-m3") as the engine writes it into a bracket slot not decided
// yet. It matches that exact shape only, so a competitor whose name merely
// starts with "Winner of" is a competitor.
func IsWinnerOfPlaceholder(s string) bool {
	return reservedWinnerOfRE.MatchString(s)
}

package helper

import "regexp"

// reservedPoolFinalistRE matches the pool-finalist placeholder labels the
// engine writes into bracket slots: "<PoolName>-<ordinal>", e.g. "Pool A-1st".
// Pool names are generated as "Pool <char>" (A–Z), so a real participant named
// like a placeholder is extremely unlikely — but scoring now gates on this
// regex, so we block it at the write boundary to prevent silent mis-classification.
var reservedPoolFinalistRE = regexp.MustCompile(`^Pool .+-\d+(st|nd|rd|th)$`)

// reservedWinnerOfRE matches the next-round feeder labels the engine emits
// into bracket slots: "Winner of r<d>-m<i>", e.g. "Winner of r1-m3".
var reservedWinnerOfRE = regexp.MustCompile(`^Winner of r\d+-m\d+$`)

// IsReservedParticipantName reports whether name collides with a bracket
// placeholder pattern used by the engine.  Such names would be
// misclassified as unresolved bracket slots and make knockout matches
// permanently unscoreable.
func IsReservedParticipantName(name string) bool {
	return reservedPoolFinalistRE.MatchString(name) || reservedWinnerOfRE.MatchString(name)
}

// IsPoolFinalistPlaceholder reports whether s is a pool-origin finalist
// placeholder ("Pool A-1st", "Pool B-2nd", etc.) as emitted by
// helper.GenerateFinals.  Unlike IsReservedParticipantName, this does NOT
// match next-round feeder labels ("Winner of r1-m3") — callers that need
// to distinguish the two patterns (e.g. bracketHasPoolPlaceholders) should
// use this instead.
func IsPoolFinalistPlaceholder(s string) bool {
	return reservedPoolFinalistRE.MatchString(s)
}

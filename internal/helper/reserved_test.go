package helper

import "testing"

func TestIsReservedParticipantName(t *testing.T) {
	reserved := []string{
		"Pool A-1st",
		"Pool B-2nd",
		"Pool Z-3rd",
		"Pool ABC-4th",
		"Pool Long Name-1st",
		"Winner of r1-m0",
		"Winner of r2-m3",
		"Winner of r10-m99",
	}
	for _, name := range reserved {
		if !IsReservedParticipantName(name) {
			t.Errorf("expected %q to be reserved", name)
		}
	}

	allowed := []string{
		"Tanaka Yuki",
		"Winner of the 2025 Cup",
		"Pool Shark",
		"Pool A 1st",       // space before ordinal, no dash
		"pool a-1st",       // lowercase, regexes require "Pool"/"Winner of" capitalisation, so lowercase never matches
		"Winner of r1-m0x", // trailing char
		// "Pool A-0th" intentionally omitted, \d+(th) matches "0th"; that's acceptable
		// since no real competitor name takes that form.
		"",
	}
	for _, name := range allowed {
		if IsReservedParticipantName(name) {
			t.Errorf("expected %q to NOT be reserved", name)
		}
	}
}

// The two placeholder predicates each match their own shape exactly, so a
// competitor named like the start of one ("John Poole", "Winner of Kyushu")
// is a competitor.
func TestPlaceholderPredicatesMatchTheirShapeOnly(t *testing.T) {
	for _, tc := range []struct {
		s              string
		pool, winnerOf bool
	}{
		{s: "Pool A-1st", pool: true},
		{s: "Pool AA-2nd", pool: true},
		{s: "Winner of r1-m0", winnerOf: true},
		{s: "Winner of r10-m99", winnerOf: true},
		{s: "John Poole"},
		{s: "Pool Kendo Club"},
		{s: "Winner of Kyushu"},
		{s: "Winner Of Kyushu"},
		{s: "Winner of r1-m0 B"},
		{s: ""},
	} {
		if got := IsPoolFinalistPlaceholder(tc.s); got != tc.pool {
			t.Errorf("IsPoolFinalistPlaceholder(%q) = %v, want %v", tc.s, got, tc.pool)
		}
		if got := IsWinnerOfPlaceholder(tc.s); got != tc.winnerOf {
			t.Errorf("IsWinnerOfPlaceholder(%q) = %v, want %v", tc.s, got, tc.winnerOf)
		}
	}
}

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
		"pool a-1st",       // lowercase: the bracket rule is case-sensitive (the roster rule, IsReservedRosterName, is not)
		"Pool A-1St",       // title-cased: a stored competitor, never a slot
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

// The roster rule refuses a placeholder shape in any case, so the spelling a
// person types, the one TitleCaseName stores ("1St", "Of R1-M2") and an
// all-caps paste are one name; anything only starting like a placeholder is a
// person.
func TestIsReservedRosterName(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reserved bool
	}{
		{"Pool A-1st", true},
		{"pool a-1st", true},
		{"Pool A-1St", true},
		{"POOL A-1ST", true},
		{"  Pool B-3rd  ", true},
		{"Pool AA-2Nd", true},
		{"Winner of r1-m2", true},
		{"winner of r1-m2", true},
		{"Winner Of R1-M2", true},
		{"WINNER OF R10-M99", true},
		{"Tanaka Yuki", false},
		{"Pool Shark", false},
		{"pool a 1st", false},
		{"Winner of Kyushu", false},
		{"winner of r1-m0x", false},
		{"", false},
	} {
		if got := IsReservedRosterName(tc.name); got != tc.reserved {
			t.Errorf("IsReservedRosterName(%q) = %v, want %v", tc.name, got, tc.reserved)
		}
	}
}

// The bracket rule stays case-sensitive: a competitor stored as "Pool A-1St"
// (title-cased before the roster rule refused it) is a competitor to the
// bracket, never an unresolved slot.
func TestBracketRuleDoesNotReadATitleCasedNameAsAPlaceholder(t *testing.T) {
	for _, s := range []string{"Pool A-1St", "pool a-1st", "Winner Of R1-M2"} {
		if IsReservedParticipantName(s) || IsPoolFinalistPlaceholder(s) || IsWinnerOfPlaceholder(s) {
			t.Errorf("%q must not read as a bracket placeholder", s)
		}
	}
}

func TestReservedRosterNames(t *testing.T) {
	got := ReservedRosterNames([]Player{
		{Name: "Tanaka Yuki"},
		{Name: " pool a-1st "},
		{Name: "Winner Of R1-M2"},
	})
	want := []string{"pool a-1st", "Winner Of R1-M2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("ReservedRosterNames = %q, want %q", got, want)
	}
	if got := ReservedRosterNames([]Player{{Name: "Tanaka Yuki"}}); len(got) != 0 {
		t.Errorf("ReservedRosterNames = %q, want none", got)
	}
}

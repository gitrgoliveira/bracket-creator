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
		"pool a-1st",       // lowercase — TitleCase is applied before save; raw lowercase must not block pre-TitleCase paths
		"Winner of r1-m0x", // trailing char
		// "Pool A-0th" intentionally omitted — \d+(th) matches "0th"; that's acceptable
		// since no real competitor name takes that form.
		"",
	}
	for _, name := range allowed {
		if IsReservedParticipantName(name) {
			t.Errorf("expected %q to NOT be reserved", name)
		}
	}
}

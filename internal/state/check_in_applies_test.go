package state

import "testing"

// Operator ruling 2026-10-13: "Self run mode has no check-in."
func TestCompetitionCheckInApplies(t *testing.T) {
	cases := []struct {
		name string
		t    *Tournament
		on   bool
		want bool
	}{
		{"no tournament, on", nil, true, true},
		{"no tournament, off", nil, false, false},
		{"officiated, on", &Tournament{Mode: TournamentModeOfficiated}, true, true},
		{"officiated, off", &Tournament{Mode: TournamentModeOfficiated}, false, false},
		{"unset mode, on", &Tournament{}, true, true},
		{"self-run, on", &Tournament{Mode: TournamentModeSelfRun}, true, false},
		{"self-run, off", &Tournament{Mode: TournamentModeSelfRun}, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Competition{CheckInEnabled: tc.on}
			if got := c.CheckInApplies(tc.t); got != tc.want {
				t.Fatalf("CheckInApplies = %v, want %v", got, tc.want)
			}
			if c.CheckInEnabled != tc.on {
				t.Fatal("the stored setting was changed")
			}
		})
	}
}

func TestTournamentIsSelfRun(t *testing.T) {
	var nilT *Tournament
	if nilT.IsSelfRun() {
		t.Fatal("a nil tournament is not self-run")
	}
	if (&Tournament{Mode: TournamentModeOfficiated}).IsSelfRun() {
		t.Fatal("officiated is not self-run")
	}
	if !(&Tournament{Mode: TournamentModeSelfRun}).IsSelfRun() {
		t.Fatal("self-run is self-run")
	}
}

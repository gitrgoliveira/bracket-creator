package state

import (
	"testing"
)

func versionTestTournament() *Tournament {
	return &Tournament{
		Name:                     "Spring Taikai",
		Date:                     "01-03-2027",
		Venue:                    "Hall",
		Courts:                   []string{"A", "B"},
		Password:                 "secret",
		DurationDays:             1,
		ClockToElapsedMultiplier: 1.5,
		SlowestCourtBufferPct:    10,
		Mode:                     TournamentModeOfficiated,
		Theme:                    &Theme{PrimaryColor: "#112233"},
	}
}

func TestTournamentVersionIsStableAndShort(t *testing.T) {
	a := TournamentVersion(versionTestTournament())
	b := TournamentVersion(versionTestTournament())
	if a != b {
		t.Fatalf("same content hashed differently: %q vs %q", a, b)
	}
	if len(a) != 16 {
		t.Fatalf("version %q is not 16 hex characters", a)
	}
	if TournamentVersion(nil) != "" {
		t.Fatalf("nil tournament must have no version")
	}
}

func TestTournamentVersionNeverReadsThePassword(t *testing.T) {
	a := versionTestTournament()
	b := versionTestTournament()
	b.Password = "another"
	if TournamentVersion(a) != TournamentVersion(b) {
		t.Fatalf("a password change moved the version")
	}
}

func TestTournamentVersionIgnoresTheLogoFile(t *testing.T) {
	a := versionTestTournament()
	b := versionTestTournament()
	b.Theme.LogoPath = "logo.png"
	if TournamentVersion(a) != TournamentVersion(b) {
		t.Fatalf("a logo upload moved the version; the PUT never writes the logo")
	}
}

func TestTournamentVersionIgnoresTheSponsors(t *testing.T) {
	a := versionTestTournament()
	b := versionTestTournament()
	b.Sponsors = []Sponsor{{Name: "Shop", File: "s.png"}}
	if TournamentVersion(a) != TournamentVersion(b) {
		t.Fatalf("a sponsor upload moved the version; the PUT never writes the sponsors")
	}
}

func TestTournamentVersionTreatsAbsentAndEmptyListsAlike(t *testing.T) {
	a := versionTestTournament()
	b := versionTestTournament()
	b.Pauses = []Pause{}
	b.Contacts = []TournamentContact{}
	if TournamentVersion(a) != TournamentVersion(b) {
		t.Fatalf("empty lists hashed differently from absent ones")
	}
	c := versionTestTournament()
	c.Theme = nil
	d := versionTestTournament()
	d.Theme = &Theme{}
	if TournamentVersion(c) != TournamentVersion(d) {
		t.Fatalf("a nil theme hashed differently from an empty one")
	}
}

// The PUT's transform reads tournament.md as parsed, while the GET applies
// the defaults; a file missing those keys must give both the same version.
func TestTournamentVersionAgreesBeforeAndAfterDefaults(t *testing.T) {
	raw := &Tournament{Name: "Legacy", Date: "01-03-2027", Venue: "Hall", Password: "p", RulesURLLegacy: "https://example.org"}
	loaded := *raw
	ApplyTournamentDefaults(&loaded)
	if TournamentVersion(raw) != TournamentVersion(&loaded) {
		t.Fatalf("raw and defaulted copies hashed differently")
	}
	if raw.Mode != "" || raw.Courts != nil {
		t.Fatalf("TournamentVersion changed the tournament it was given")
	}
	unknownMode := *raw
	unknownMode.Mode = "bogus"
	if TournamentVersion(&unknownMode) != TournamentVersion(&loaded) {
		t.Fatalf("an unknown mode must hash as the officiated one LoadTournament serves")
	}
}

func TestTournamentVersionMovesWithEveryWrittenField(t *testing.T) {
	base := TournamentVersion(versionTestTournament())
	edits := map[string]func(*Tournament){
		"name":        func(x *Tournament) { x.Name = "Other" },
		"date":        func(x *Tournament) { x.Date = "02-03-2027" },
		"venue":       func(x *Tournament) { x.Venue = "Dojo" },
		"courts":      func(x *Tournament) { x.Courts = []string{"A"} },
		"days":        func(x *Tournament) { x.DurationDays = 2 },
		"pauses":      func(x *Tournament) { x.Pauses = []Pause{{Name: "Lunch", Start: "12:00", Length: "45m"}} },
		"multiplier":  func(x *Tournament) { x.ClockToElapsedMultiplier = 2 },
		"buffer":      func(x *Tournament) { x.SlowestCourtBufferPct = 20 },
		"mode":        func(x *Tournament) { x.Mode = TournamentModeSelfRun },
		"publicURL":   func(x *Tournament) { x.PublicURL = "https://example.org" },
		"address":     func(x *Tournament) { x.VenueAddress = "1 Road" },
		"map":         func(x *Tournament) { x.VenueMapURL = "https://maps.example.org" },
		"opening":     func(x *Tournament) { x.OpeningTime = "08:00" },
		"closing":     func(x *Tournament) { x.ClosingTime = "18:00" },
		"website":     func(x *Tournament) { x.WebsiteURL = "https://example.org" },
		"awardsNote":  func(x *Tournament) { x.AwardsNote = "Medals" },
		"infoNotes":   func(x *Tournament) { x.InfoNotes = "Bring bogu" },
		"contacts":    func(x *Tournament) { x.Contacts = []TournamentContact{{Label: "Email", Value: "a@b.c"}} },
		"colour":      func(x *Tournament) { x.Theme.AccentSoftColor = "#445566" },
		"windowTitle": func(x *Tournament) { x.Theme.WindowTitle = "Taikai" },
		"logoOnLobby": func(x *Tournament) { x.Theme.LogoOnLobby = true },
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			x := versionTestTournament()
			edit(x)
			if TournamentVersion(x) == base {
				t.Fatalf("changing %s left the version unchanged", name)
			}
		})
	}
}

func TestAwardsVersion(t *testing.T) {
	if AwardsVersion(nil) != "" {
		t.Fatalf("nil competition must have no version")
	}
	none := &Competition{ID: "c1"}
	empty := &Competition{ID: "c1", FightingSpiritAwards: []FightingSpiritAward{}}
	if AwardsVersion(none) != AwardsVersion(empty) {
		t.Fatalf("absent and empty award lists hashed differently")
	}
	one := &Competition{ID: "c1", FightingSpiritAwards: []FightingSpiritAward{{Title: "Fighting spirit", RecipientName: "Sato"}}}
	if AwardsVersion(one) == AwardsVersion(empty) {
		t.Fatalf("adding an award left the version unchanged")
	}
	renamed := &Competition{ID: "c1", FightingSpiritAwards: []FightingSpiritAward{{Title: "Fighting spirit", RecipientName: "Sato", RecipientDojo: "Hizen"}}}
	if AwardsVersion(renamed) == AwardsVersion(one) {
		t.Fatalf("changing a recipient's dojo left the version unchanged")
	}
	otherSettings := &Competition{ID: "c1", Name: "Open", PoolSize: 4, FightingSpiritAwards: one.FightingSpiritAwards}
	if AwardsVersion(otherSettings) != AwardsVersion(one) {
		t.Fatalf("a settings field outside the awards moved the awards version")
	}
	if len(AwardsVersion(one)) != 16 {
		t.Fatalf("awards version is not 16 hex characters")
	}
}

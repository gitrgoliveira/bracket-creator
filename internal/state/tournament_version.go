package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// tournamentVersionTheme is the part of Theme that PUT /api/tournament
// writes. LogoPath is left out: the PUT carries the stored logo over and only
// the branding logo routes change it, so an uploaded logo never makes a
// settings save stale. A nil Theme hashes as the zero theme, since the PUT
// drops an empty theme block.
type tournamentVersionTheme struct {
	PrimaryColor    string `json:"primaryColor"`
	AccentSoftColor string `json:"accentSoftColor"`
	WindowTitle     string `json:"windowTitle"`
	LogoOnLobby     bool   `json:"logoOnLobby"`
}

// tournamentVersionPayload is the allowlist of fields hashed into
// TournamentVersion: the fields PUT /api/tournament writes from its body.
// The password is NEVER included (the version is served on the public viewer
// GET), nor are the legacy ceremony and rules_url keys, which are never on
// the wire.
type tournamentVersionPayload struct {
	Name                     string                 `json:"name"`
	Date                     string                 `json:"date"`
	Venue                    string                 `json:"venue"`
	Courts                   []string               `json:"courts"`
	DurationDays             int                    `json:"durationDays"`
	Pauses                   []Pause                `json:"pauses"`
	ClockToElapsedMultiplier float64                `json:"clockToElapsedMultiplier"`
	SlowestCourtBufferPct    int                    `json:"slowestCourtBufferPct"`
	Mode                     string                 `json:"mode"`
	PublicURL                string                 `json:"publicURL"`
	VenueAddress             string                 `json:"venueAddress"`
	VenueMapURL              string                 `json:"venueMapURL"`
	OpeningTime              string                 `json:"openingTime"`
	ClosingTime              string                 `json:"closingTime"`
	WebsiteURL               string                 `json:"websiteURL"`
	AwardsNote               string                 `json:"awardsNote"`
	InfoNotes                string                 `json:"infoNotes"`
	Contacts                 []TournamentContact    `json:"contacts"`
	Sponsors                 []Sponsor              `json:"sponsors"`
	Theme                    tournamentVersionTheme `json:"theme"`
}

// TournamentVersion computes a content-hash version of the tournament
// settings (16 lowercase hex characters), the counterpart of ConfigVersion
// for tournament.md. A nil tournament returns "".
//
// The hash is taken over a normalised copy, so every copy the server compares
// agrees: the GET reads a tournament with ApplyTournamentDefaults applied,
// while the PUT's transform sees the file as parsed, and a list the body sent
// empty comes back from tournament.md as absent. Both differences hash alike.
func TournamentVersion(t *Tournament) string {
	if t == nil {
		return ""
	}
	cp := *t
	ApplyTournamentDefaults(&cp)
	if cp.Mode != TournamentModeOfficiated && cp.Mode != TournamentModeSelfRun {
		cp.Mode = TournamentModeOfficiated
	}
	var theme tournamentVersionTheme
	if cp.Theme != nil {
		theme = tournamentVersionTheme{
			PrimaryColor:    cp.Theme.PrimaryColor,
			AccentSoftColor: cp.Theme.AccentSoftColor,
			WindowTitle:     cp.Theme.WindowTitle,
			LogoOnLobby:     cp.Theme.LogoOnLobby,
		}
	}
	p := tournamentVersionPayload{
		Name:                     cp.Name,
		Date:                     cp.Date,
		Venue:                    cp.Venue,
		Courts:                   versionList(cp.Courts),
		DurationDays:             cp.DurationDays,
		Pauses:                   versionList(cp.Pauses),
		ClockToElapsedMultiplier: cp.ClockToElapsedMultiplier,
		SlowestCourtBufferPct:    cp.SlowestCourtBufferPct,
		Mode:                     cp.Mode,
		PublicURL:                cp.PublicURL,
		VenueAddress:             cp.VenueAddress,
		VenueMapURL:              cp.VenueMapURL,
		OpeningTime:              cp.OpeningTime,
		ClosingTime:              cp.ClosingTime,
		WebsiteURL:               cp.WebsiteURL,
		AwardsNote:               cp.AwardsNote,
		InfoNotes:                cp.InfoNotes,
		Contacts:                 versionList(cp.Contacts),
		Sponsors:                 versionList(cp.Sponsors),
		Theme:                    theme,
	}
	return versionHash(p)
}

// AwardsVersion computes a content-hash version of a competition's
// fighting-spirit awards (16 lowercase hex characters), the only field
// PUT /api/competitions/:id/awards writes. An absent list and an empty one
// hash alike. A nil competition returns "".
func AwardsVersion(c *Competition) string {
	if c == nil {
		return ""
	}
	return versionHash(versionList(c.FightingSpiritAwards))
}

// versionList hashes an absent list and an empty one alike.
func versionList[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func versionHash(v any) string {
	data, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])[:16]
}

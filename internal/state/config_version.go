package state

// configVersionPayload defines the allowlist of fields hashed to compute
// ConfigVersion. This list is the settings merge's field set copied from the
// request body onto current in PUT /competitions/:id (the comp.Players == nil
// branch in RegisterCompetitionHandlers).
//
// Runtime fields written outside Settings are NEVER included: Status,
// HasParticipantIDs, RosterShuffled, DrawHidden, SwissCurrentRound,
// FightingSpiritAwards, KachinukiEncounterEnchoCleared, RoundLineupsConverted,
// RoundLineupsGiven, RoundLineupsLegacy, RevealSecret, Players, RosterVersion,
// ConfigVersion.
// RevealSecret is server-managed (minted by the server, never on the wire),
// so it stays out of this set and out of the settings merge alike.
type configVersionPayload struct {
	Name                         string        `json:"name"`
	Date                         string        `json:"date"`
	StartTime                    string        `json:"startTime"`
	PoolSize                     int           `json:"poolSize"`
	PoolWinners                  int           `json:"poolWinners"`
	PoolSizeMode                 string        `json:"poolSizeMode"`
	ExtraQualifiers              string        `json:"extraQualifiers"`
	Courts                       []string      `json:"courts"`
	RoundRobin                   bool          `json:"roundRobin"`
	WithZekkenName               bool          `json:"withZekkenName"`
	TeamSize                     int           `json:"teamSize"`
	NumberPrefix                 string        `json:"numberPrefix"`
	Format                       string        `json:"format"`
	Kind                         string        `json:"kind"`
	PoolMatchDurationSeconds     int           `json:"poolMatchDurationSeconds"`
	KnockoutMatchDurationSeconds int           `json:"knockoutMatchDurationSeconds"`
	SwissRounds                  int           `json:"swissRounds"`
	Naginata                     bool          `json:"naginata"`
	Engi                         bool          `json:"engi"`
	TeamMatchType                TeamMatchType `json:"teamMatchType"`
	TwoThirdPlaces               *bool         `json:"twoThirdPlaces"`
	ExpectedParticipants         int           `json:"expectedParticipants"`
	CheckInEnabled               bool          `json:"checkInEnabled"`
	// omitempty on the fields added since v2.5 keeps an untouched record's
	// version as it was before the upgrade, so a save opened before it is
	// not refused for a change nobody made.
	PublicNames string `json:"publicNames,omitempty"`
	RevealUntil string `json:"revealUntil,omitempty"`
	EngiSolo    bool   `json:"engiSolo,omitempty"`
}

// ConfigVersion computes a content-hash config version for a competition's
// settings (16 lowercase hex characters). A nil competition returns "".
func ConfigVersion(c *Competition) string {
	if c == nil {
		return ""
	}
	return versionHash(configVersionPayloadOf(c))
}

// configVersionPayloadOf is the allowlisted projection ConfigVersion hashes.
func configVersionPayloadOf(c *Competition) configVersionPayload {
	courts := c.Courts
	if courts == nil {
		courts = []string{}
	}
	return configVersionPayload{
		Name:                         c.Name,
		Date:                         c.Date,
		StartTime:                    c.StartTime,
		PoolSize:                     c.PoolSize,
		PoolWinners:                  c.PoolWinners,
		PoolSizeMode:                 c.PoolSizeMode,
		ExtraQualifiers:              c.ExtraQualifiers,
		Courts:                       courts,
		RoundRobin:                   c.RoundRobin,
		WithZekkenName:               c.WithZekkenName,
		TeamSize:                     c.TeamSize,
		NumberPrefix:                 c.NumberPrefix,
		Format:                       c.Format,
		Kind:                         c.Kind,
		PoolMatchDurationSeconds:     c.PoolMatchDurationSeconds,
		KnockoutMatchDurationSeconds: c.KnockoutMatchDurationSeconds,
		SwissRounds:                  c.SwissRounds,
		Naginata:                     c.Naginata,
		Engi:                         c.Engi,
		EngiSolo:                     c.EngiSolo,
		TeamMatchType:                c.TeamMatchType,
		TwoThirdPlaces:               c.TwoThirdPlaces,
		ExpectedParticipants:         c.ExpectedParticipants,
		CheckInEnabled:               c.CheckInEnabled,
		PublicNames:                  c.PublicNames,
		RevealUntil:                  c.RevealUntil,
	}
}

package state

// configVersionPayload defines the allowlist of fields hashed to compute
// ConfigVersion. This list is the settings merge's field set copied from the
// request body onto current in PUT /competitions/:id (the comp.Players == nil
// branch in RegisterCompetitionHandlers).
//
// Runtime fields written outside Settings are NEVER included: Status,
// HasParticipantIDs, SwissCurrentRound, FightingSpiritAwards,
// LeagueTiebreakFinalized, KachinukiEncounterEnchoCleared, RoundLineupsConverted,
// RoundLineupsGiven, RoundLineupsLegacy, Players, RosterVersion, ConfigVersion.
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
	PoolFormat                   string        `json:"poolFormat"`
	Kind                         string        `json:"kind"`
	PoolMatchDurationSeconds     int           `json:"poolMatchDurationSeconds"`
	KnockoutMatchDurationSeconds int           `json:"knockoutMatchDurationSeconds"`
	SwissRounds                  int           `json:"swissRounds"`
	Naginata                     bool          `json:"naginata"`
	Engi                         bool          `json:"engi"`
	TeamMatchType                TeamMatchType `json:"teamMatchType"`
	LeagueTiebreakTopN           int           `json:"leagueTiebreakTopN"`
	TwoThirdPlaces               *bool         `json:"twoThirdPlaces"`
	ExpectedParticipants         int           `json:"expectedParticipants"`
	CheckInEnabled               bool          `json:"checkInEnabled"`
}

// ConfigVersion computes a content-hash config version for a competition's
// settings (16 lowercase hex characters). A nil competition returns "".
func ConfigVersion(c *Competition) string {
	if c == nil {
		return ""
	}
	courts := c.Courts
	if courts == nil {
		courts = []string{}
	}
	p := configVersionPayload{
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
		PoolFormat:                   c.PoolFormat,
		Kind:                         c.Kind,
		PoolMatchDurationSeconds:     c.PoolMatchDurationSeconds,
		KnockoutMatchDurationSeconds: c.KnockoutMatchDurationSeconds,
		SwissRounds:                  c.SwissRounds,
		Naginata:                     c.Naginata,
		Engi:                         c.Engi,
		TeamMatchType:                c.TeamMatchType,
		LeagueTiebreakTopN:           c.LeagueTiebreakTopN,
		TwoThirdPlaces:               c.TwoThirdPlaces,
		ExpectedParticipants:         c.ExpectedParticipants,
		CheckInEnabled:               c.CheckInEnabled,
	}
	return versionHash(p)
}

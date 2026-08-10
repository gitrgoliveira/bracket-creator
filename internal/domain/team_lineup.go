package domain

import (
	"errors"
	"fmt"
	"strconv"
)

// Position names a slot in a team lineup. For 5-person teams the named
// FIK constants apply; for other sizes positions are numeric strings
// "1".."N" produced by PositionNumbered.
//
// FR-040, data-model §4.
type Position string

const (
	PosSenpo   Position = "senpo"
	PosJiho    Position = "jiho"
	PosChuken  Position = "chuken"
	PosFukusho Position = "fukusho"
	PosTaisho  Position = "taisho"
)

// PositionNumbered returns the canonical Position value for a non-5
// team size, where positions are 1-indexed numeric strings.
func PositionNumbered(n int) Position { return Position(strconv.Itoa(n)) }

// TeamLineup pins which player occupies each Position for a team in a
// given round OR for a specific match. The lineup is always editable,
// including while a match is running or completed.
//
// Keying (mp-825): when MatchID is non-empty the lineup is
// match-scoped, a team may field a different order/roster for each
// encounter (e.g. successive pool matches). When MatchID is empty the
// lineup is round-scoped (the legacy behavior, still used by bracket
// rounds and pre-mp-825 data): one lineup per (team, round). The two
// scopes coexist; a match-scoped entry shadows the round-scoped
// fallback for that match.
//
// FR-040, data-model §4.
type TeamLineup struct {
	TeamID        string              `json:"teamId" yaml:"teamId"`
	CompetitionID string              `json:"competitionId" yaml:"competitionId"`
	Round         int                 `json:"round" yaml:"round"`
	MatchID       string              `json:"matchId,omitempty" yaml:"matchId,omitempty"`
	Positions     map[Position]string `json:"positions" yaml:"positions"`
}

var ErrLineupTeamSizeInvalid = errors.New("team_lineup: teamSize must be positive")

// ValidatePositions checks only that the position KEYS are valid for the team
// size; it does NOT enforce any completeness or vacancy rule. Position
// vacancies are irrelevant and never block a lineup (mp-gmcg): team sizes are
// unregulated, lineups are entered incrementally while bouts run, and a
// partial lineup must be persistable. The FIK back-fill/DQ rule that used to
// live here (Validate/validateFive) was removed, it was never called on a
// production path and contradicted operator-led kachinuki play.
func (t TeamLineup) ValidatePositions(teamSize int) error {
	if teamSize <= 0 {
		return ErrLineupTeamSizeInvalid
	}
	allowed := allowedPositionSet(teamSize)
	for pos := range t.Positions {
		if _, ok := allowed[pos]; !ok {
			return fmt.Errorf("team_lineup: position %q not allowed in %d-person team", pos, teamSize)
		}
	}
	return nil
}

// OrderedRoster returns the player names for this lineup in position order,
// skipping vacancies (empty strings). For 5-person teams the canonical
// Senpo/Jiho/Chuken/Fukusho/Taisho order is used; for other sizes
// positions are iterated 1..teamSize numerically.
//
// The returned slice is always non-nil. Its length equals the number of
// non-empty positions. Callers (e.g. kachinuki roster resolution) use
// this to get the full ordered queue before filtering out retired players.
func (t TeamLineup) OrderedRoster(teamSize int) []string {
	if teamSize == 5 {
		order := []Position{PosSenpo, PosJiho, PosChuken, PosFukusho, PosTaisho}
		out := make([]string, 0, 5)
		for _, pos := range order {
			if name := t.Positions[pos]; name != "" {
				out = append(out, name)
			}
		}
		return out
	}
	out := make([]string, 0, teamSize)
	for i := 1; i <= teamSize; i++ {
		if name := t.Positions[PositionNumbered(i)]; name != "" {
			out = append(out, name)
		}
	}
	return out
}

// allowedPositionSet returns the valid position keys for a team size: the five
// FIK names for 5-person teams, else numbered positions 1..teamSize.
func allowedPositionSet(teamSize int) map[Position]struct{} {
	if teamSize == 5 {
		return map[Position]struct{}{PosSenpo: {}, PosJiho: {}, PosChuken: {}, PosFukusho: {}, PosTaisho: {}}
	}
	allowed := make(map[Position]struct{}, teamSize)
	for i := 1; i <= teamSize; i++ {
		allowed[PositionNumbered(i)] = struct{}{}
	}
	return allowed
}

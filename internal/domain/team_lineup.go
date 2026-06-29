package domain

import (
	"errors"
	"fmt"
	"strconv"
	"time"
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
// given round OR for a specific match. The lineup is replaceable up
// until its match (match-scoped) or its round's first match
// (round-scoped) starts, at which point LockedAt is set and further
// PUT/PATCH operations are rejected.
//
// Keying (mp-825): when MatchID is non-empty the lineup is
// match-scoped, a team may field a different order/roster for each
// encounter (e.g. successive pool matches), and each entry locks
// independently when its own match starts. When MatchID is empty the
// lineup is round-scoped (the legacy behavior, still used by bracket
// rounds and pre-mp-825 data): one lineup per (team, round), frozen
// when the round's first match starts. The two scopes coexist; a
// match-scoped entry shadows the round-scoped fallback for that match.
//
// FR-040, data-model §4.
type TeamLineup struct {
	TeamID        string              `json:"teamId" yaml:"teamId"`
	CompetitionID string              `json:"competitionId" yaml:"competitionId"`
	Round         int                 `json:"round" yaml:"round"`
	MatchID       string              `json:"matchId,omitempty" yaml:"matchId,omitempty"`
	Positions     map[Position]string `json:"positions" yaml:"positions"`
	LockedAt      *time.Time          `json:"lockedAt,omitempty" yaml:"lockedAt,omitempty"`
	// ChangeReason is a mandatory audit justification when an operator
	// edits a lineup after the match has started (force=true). Format:
	// "<category>: <note>" (e.g. "Substitution: injury to jiho").
	// Omitted for pre-match lineup submissions.
	ChangeReason string `json:"changeReason,omitempty" yaml:"changeReason,omitempty"`
}

var (
	ErrLineupMissingSenpo    = errors.New("team_lineup: senpo must be present")
	ErrLineupMissingTaisho   = errors.New("team_lineup: taisho must be present")
	ErrLineupTooManyMissing  = errors.New("team_lineup: 3+ missing positions, team is disqualified")
	ErrLineupTeamSizeInvalid = errors.New("team_lineup: teamSize must be positive")
)

// Validate enforces FR-037 / FR-041 / R4 / CHK012: a 5-person lineup
// must include Senpo and Taisho, and any kiken vacancies must follow
// the FIK back-fill rule (the missing position is Jiho first, then
// Jiho+Fukusho). For non-5 sizes positions must be numeric "1".."N".
//
// Returning a non-nil error signals the team should not be allowed to
// take the court (either reject the lineup PUT or DQ the team via
// CompetitorStatus).
func (t TeamLineup) Validate(teamSize int) error {
	if teamSize <= 0 {
		return ErrLineupTeamSizeInvalid
	}
	if teamSize == 5 {
		return t.validateFive()
	}
	return t.validateNumbered(teamSize)
}

func (t TeamLineup) validateFive() error {
	allowed := allowedPositionSet(5)
	for pos := range t.Positions {
		if _, ok := allowed[pos]; !ok {
			return fmt.Errorf("team_lineup: position %q not allowed in 5-person team", pos)
		}
	}
	// Senpo and Taisho are mandatory (R4, they bookend the match).
	if t.Positions[PosSenpo] == "" {
		return ErrLineupMissingSenpo
	}
	if t.Positions[PosTaisho] == "" {
		return ErrLineupMissingTaisho
	}
	// Count missing among middle positions (Jiho, Chuken, Fukusho).
	missing := make([]Position, 0, 3)
	for _, p := range []Position{PosJiho, PosChuken, PosFukusho} {
		if t.Positions[p] == "" {
			missing = append(missing, p)
		}
	}
	switch len(missing) {
	case 0:
		return nil
	case 1:
		// The single vacancy must be Jiho (FIK back-fill rule).
		if missing[0] != PosJiho {
			return fmt.Errorf("team_lineup: with 1 vacancy, the missing position must be Jiho, got %q", missing[0])
		}
		return nil
	case 2:
		// The two vacancies must be Jiho and Fukusho.
		found := map[Position]bool{missing[0]: true, missing[1]: true}
		if !found[PosJiho] || !found[PosFukusho] {
			return fmt.Errorf("team_lineup: with 2 vacancies, the missing positions must be Jiho and Fukusho, got %v", missing)
		}
		return nil
	default:
		return ErrLineupTooManyMissing
	}
}

func (t TeamLineup) validateNumbered(teamSize int) error {
	// Identical to ValidatePositions on the reachable path (Validate already
	// rejected teamSize <= 0 and dispatched teamSize == 5 to validateFive), so
	// delegate rather than keep a second copy of the key-check loop.
	return t.ValidatePositions(teamSize)
}

// ValidatePositions checks only that the position KEYS are valid for the team
// size; it does NOT enforce the FIK completeness/vacancy rule. Lineups are
// entered incrementally while bouts run, so a partial lineup must be
// persistable, completeness is surfaced as a non-blocking UI warning, not
// enforced at write time.
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

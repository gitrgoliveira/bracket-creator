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
	// MemberIDs pairs each occupied Position with the squad member's stable
	// id (TeamMember.ID, bc-tmid pass 2), keyed by the SAME Position as
	// Positions. Positions keeps holding the display NAME exactly as
	// before, written and read unchanged; this map is the id half, added
	// BESIDE it rather than replacing it, so a legacy lineup with no ids at
	// all still round-trips byte-for-byte (omitempty). A position present
	// in Positions but absent here is an UNREPAIRED slot -- legacy data, or
	// one entered before its member existed in the squad -- and every
	// id-aware reader (kachinuki retirement) falls back to the Positions
	// name for exactly that slot and no other.
	MemberIDs map[Position]string `json:"memberIds,omitempty" yaml:"memberIds,omitempty"`
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
	// MemberIDs shares the SAME key space as Positions -- a slot's id lives
	// under the identical Position key -- so an illegal key here is
	// rejected exactly like an illegal Positions key. Checked as its own
	// loop, not folded into the one above, because MemberIDs may name a
	// position Positions itself does not (e.g. a single PUT that sets the
	// id slightly ahead of the name), so walking Positions alone would miss
	// it.
	for pos := range t.MemberIDs {
		if _, ok := allowed[pos]; !ok {
			return fmt.Errorf("team_lineup: position %q not allowed in %d-person team", pos, teamSize)
		}
	}
	return nil
}

// LineupSlot is one OCCUPIED position from a lineup: the Position itself,
// its display NAME (Positions), and its squad MEMBER ID (MemberIDs) when
// that position has been repaired/resolved -- empty for an unrepaired
// legacy slot. OrderedMembers is the ONE traversal that decides which
// positions are occupied and in what order (bc-tmid pass 2); do not add a
// second, independently-computed traversal (e.g. a parallel
// OrderedMemberIDs) alongside it. A skip-on-empty walk over Positions and a
// skip-on-empty walk over MemberIDs skip at DIFFERENT indices the moment
// one slot's id is missing, so two separate traversals would silently
// misalign a name to the wrong id the first time a lineup has even one
// unrepaired position -- exactly the hazard a single (Position, Name,
// MemberID) tuple per occupied slot avoids.
type LineupSlot struct {
	Position Position
	Name     string
	MemberID string
}

// canonicalPositionOrder returns the position traversal order OrderedMembers
// (and, via it, OrderedRoster) walks: the five FIK names for a 5-person
// team, else 1..teamSize numerically. Extracted so the two consumers of
// this exact order cannot drift the way an inline copy in each would risk.
func canonicalPositionOrder(teamSize int) []Position {
	if teamSize == 5 {
		return []Position{PosSenpo, PosJiho, PosChuken, PosFukusho, PosTaisho}
	}
	order := make([]Position, teamSize)
	for i := 1; i <= teamSize; i++ {
		order[i-1] = PositionNumbered(i)
	}
	return order
}

// OrderedMembers returns the occupied lineup slots in canonical position
// order, skipping vacancies (an empty Positions entry). Each slot's
// MemberID is read from the SAME position key as its Name in the SAME
// iteration step, so a slot with no id yet reports one that is simply
// empty rather than one borrowed from a neighbouring position.
func (t TeamLineup) OrderedMembers(teamSize int) []LineupSlot {
	order := canonicalPositionOrder(teamSize)
	out := make([]LineupSlot, 0, len(order))
	for _, pos := range order {
		name := t.Positions[pos]
		if name == "" {
			continue
		}
		out = append(out, LineupSlot{Position: pos, Name: name, MemberID: t.MemberIDs[pos]})
	}
	return out
}

// OrderedRoster returns the player names for this lineup in position order,
// skipping vacancies (empty strings) -- a thin projection of OrderedMembers
// (bc-tmid pass 2: see that method's doc for why this must stay a
// projection rather than becoming a second, independent traversal).
//
// The returned slice is always non-nil. Its length equals the number of
// non-empty positions. Callers (e.g. kachinuki roster resolution) use
// this to get the full ordered queue before filtering out retired players.
func (t TeamLineup) OrderedRoster(teamSize int) []string {
	members := t.OrderedMembers(teamSize)
	out := make([]string, len(members))
	for i, m := range members {
		out[i] = m.Name
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

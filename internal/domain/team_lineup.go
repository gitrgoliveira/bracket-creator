package domain

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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

// Label is the position as the operator reads it: a FIK name title-cased
// ("senpo" becomes "Senpo"), a numbered position unchanged ("3").
func (p Position) Label() string {
	s := string(p)
	if s == "" || (s[0] >= '0' && s[0] <= '9') {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

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

// ErrLineupDuplicateMember is the sentinel behind every duplicate-member
// error ValidatePositions returns (bc-dnst); check it with errors.Is rather
// than the message text. checkDuplicateMembers wraps it under a message
// that names the two conflicting positions.
var ErrLineupDuplicateMember = errors.New("member is placed at two positions")

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
	return t.checkDuplicateMembers(teamSize)
}

// checkDuplicateMembers rejects a lineup that places the same squad member
// id at two different positions: a member fights one bout at a time, so a
// duplicate id is never legal, whether typed via a name that resolved to an
// already-placed member or picked directly. It walks canonicalPositionOrder
// (every MemberIDs key is already known to be in it, see ValidatePositions
// above) so the reported pair is deterministic and in position order without
// copying or sorting the keys; a lexical sort would have put "10" before
// "2" on a team larger than nine.
func (t TeamLineup) checkDuplicateMembers(teamSize int) error {
	seen := make(map[string]Position, len(t.MemberIDs))
	for _, pos := range canonicalPositionOrder(teamSize) {
		id := t.MemberIDs[pos]
		if id == "" {
			continue
		}
		if prior, ok := seen[id]; ok {
			// Positions only: the message reaches the operator as the 400's
			// text, and a member id means nothing to them.
			return fmt.Errorf("team_lineup: the same member is at both %q and %q: %w", prior, pos, ErrLineupDuplicateMember)
		}
		seen[id] = pos
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
// walks: the five FIK names for a 5-person team, else 1..teamSize
// numerically. It has a single consumer today, but stays its own named step
// rather than being inlined into OrderedMembers, so the order itself stays
// separately readable and testable.
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
// order, skipping vacancies. A slot is occupied when it carries a name OR a
// member id: a squad slot picked by number before it is named is a real
// placement (bc-dnst, the number belongs to the member), so it fields a
// fighter with an empty Name and a MemberID, and only a position with
// neither is vacant. Each slot's MemberID is read from the SAME position
// key as its Name in the SAME iteration step, so a slot with no id yet
// reports one that is simply empty rather than one borrowed from a
// neighbouring position.
func (t TeamLineup) OrderedMembers(teamSize int) []LineupSlot {
	order := canonicalPositionOrder(teamSize)
	out := make([]LineupSlot, 0, len(order))
	for _, pos := range order {
		name := t.Positions[pos]
		memberID := t.MemberIDs[pos]
		if name == "" && memberID == "" {
			continue
		}
		out = append(out, LineupSlot{Position: pos, Name: name, MemberID: memberID})
	}
	return out
}

// BoutFighter is one side of a kachinuki bout as KachinukiTaishoPairing reads
// it: the fighter's display name and squad member id (either may be empty).
type BoutFighter struct {
	Name     string
	MemberID string
}

// holds reports whether this slot is fighter f: by member id when both carry
// one, else by display name (the order domain.SubBoutAttribution uses). An
// empty fighter is nobody.
func (s LineupSlot) holds(f BoutFighter) bool {
	if f.MemberID != "" && s.MemberID != "" {
		return f.MemberID == s.MemberID
	}
	return f.Name != "" && f.Name == s.Name
}

// taishoStanding is where one fighter stands in their team's lineup, for
// KachinukiTaishoPairing.
type taishoStanding int

const (
	// standingUnknown: no lineup, an empty one, or a fighter the lineup does
	// not place (a reserve, a free-typed name, or a lineup only partly
	// entered: the kachinuki score sheet writes row 1 alone into a match
	// lineup, so its one slot says nothing about who fights last).
	standingUnknown taishoStanding = iota
	// standingTaisho: nobody placed after the fighter is still to fight: they
	// hold the lineup's last occupied slot, or every team-mate placed after
	// them has already fought in this encounter.
	standingTaisho
	// standingBeforeTaisho: a team-mate placed after the fighter has not
	// fought yet, so this is provably not the last bout.
	standingBeforeTaisho
)

// standingIn places fighter f in their team's lineup. fought is who has
// already fought for that team in this encounter: the operator may pick a
// later bout's fighter out of lineup order, so a team-mate placed after f
// who has already fought is no longer to come.
func standingIn(lineup *TeamLineup, teamSize int, f BoutFighter, fought []BoutFighter) taishoStanding {
	if lineup == nil {
		return standingUnknown
	}
	members := lineup.OrderedMembers(teamSize)
	for i, slot := range members {
		if !slot.holds(f) {
			continue
		}
		for _, later := range members[i+1:] {
			if !slotHoldsAny(later, fought) {
				return standingBeforeTaisho
			}
		}
		return standingTaisho
	}
	return standingUnknown
}

func slotHoldsAny(s LineupSlot, fighters []BoutFighter) bool {
	for _, f := range fighters {
		if s.holds(f) {
			return true
		}
	}
	return false
}

// KachinukiTaishoPairing is the ONE rule for whether a kachinuki bout may go
// to encho (operator ruling 2026-09-25, bc-kten): only the last bout, taisho
// against taisho, may. Any other tie retires per the kachinuki mode in force.
//
// foughtA and foughtB are the fighters each team has already put up in this
// encounter, the bouts before this one. The last bout is the one where
// neither team has anyone left to come, so a fighter placed before a
// team-mate is provably not in it only while that team-mate has not fought:
// the operator may pick a later bout's fighter out of lineup order, and
// refusing encho on the real last bout would leave a tied knockout with no
// way to finish.
//
// known is true only when the lineups settle it: either fighter has a
// team-mate placed after them who has not fought yet (taisho=false), or both
// have nobody placed after them still to fight (taisho=true). Anything else
// is unknown (known=false): no lineup, an empty one, or a fighter the lineup
// does not place. Callers must NOT refuse on unknown. A tied knockout bout already has
// End match held back, so refusing encho on a guess would leave the court no
// way to finish; permitting it on a lineup that is merely incomplete is the
// safe error. A lineup's last entry reads as the taisho even when the rest
// is simply not entered yet, since the app cannot tell that from a vacancy.
//
// JS twin: kachinukiTaishoPairing (web-mobile/js/lineup_resolver.jsx). Both
// are pinned by internal/domain/testdata/kachinuki_taisho.json.
func KachinukiTaishoPairing(teamSize int, lineupA, lineupB *TeamLineup, a, b BoutFighter, foughtA, foughtB []BoutFighter) (taisho, known bool) {
	sa := standingIn(lineupA, teamSize, a, foughtA)
	sb := standingIn(lineupB, teamSize, b, foughtB)
	if sa == standingBeforeTaisho || sb == standingBeforeTaisho {
		return false, true
	}
	if sa == standingTaisho && sb == standingTaisho {
		return true, true
	}
	return false, false
}

// PositionForBout returns the lineup Position that fights numbered bout
// `bout` (1-based) of a team match: the bout-th entry of the canonical order
// OrderedMembers walks. False when bout is outside 1..teamSize.
func PositionForBout(teamSize, bout int) (Position, bool) {
	order := canonicalPositionOrder(teamSize)
	if bout < 1 || bout > len(order) {
		return "", false
	}
	return order[bout-1], true
}

// allowedPositionSet returns the valid position keys for a team size: the five
// FIK names for 5-person teams, else numbered positions 1..teamSize.
func allowedPositionSet(teamSize int) map[Position]struct{} {
	// Derived from canonicalPositionOrder rather than enumerating the
	// five-versus-numbered rule a second time: which positions a team size
	// has is one fact, and a second spelling of it is a second place to
	// forget when a new team format arrives.
	order := canonicalPositionOrder(teamSize)
	allowed := make(map[Position]struct{}, len(order))
	for _, pos := range order {
		allowed[pos] = struct{}{}
	}
	return allowed
}

package domain

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
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
// lineup is round-scoped (the Lineups page's lineup, and pre-mp-825 data):
// one lineup per (team, round). The two scopes coexist, and which one a team
// fields in a match is not decided here: a team carries the lineup of its
// previous match unless one is entered for the match (engine.Engine.LineupInForce).
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
	// Stamps holds when each position was last changed, as the device that
	// changed it stamped the change (server-relative ms, the save's
	// `changedAt`). A change sent with no stamp (an older build's) is stamped
	// with the server's time as it lands. Every lineup is ordered by it
	// (operator decision L1, engine/lineup_carry.go): a position holds its
	// latest change by stamp, and a carried lineup reads each position as it
	// stood at its own stamp. A lineup an older release stored, or one given
	// by the round-lineup migration, may have none: its values then count
	// from the beginning.
	Stamps map[Position]int64 `json:"-" yaml:"stamps,omitempty"`
	// StartedAt is set on the lineup the server kept as the one a kachinuki
	// match started with (its pin, engine/kachinuki_opener.go): the stamp of
	// the write that moved the match out of scheduled. Zero on a lineup an
	// operator saved. A pin is the match's own lineup for that match alone:
	// a later match never carries it (engine.LineupInForce).
	StartedAt int64 `json:"-" yaml:"startedAt,omitempty"`
	// StartedFrom names the lineup a pin was taken from: the earlier match
	// whose own lineup the match carried, or "" for the team's starting
	// lineup. A change to that lineup stamped before StartedAt reaches the pin.
	StartedFrom string `json:"-" yaml:"startedFrom,omitempty"`
	// CopiedAt is set on a match's own lineup a stamped partial save made
	// (operator decision L1): the stamp of its earliest change. Every
	// position its own saves did not change (Own) is the carry rule at that
	// stamp, BY STAMP (engine/lineup_carry.go): what the match would have
	// taken from the changes made before it, whatever order they arrive in,
	// and never a change made after it. Zero on any other lineup.
	CopiedAt int64 `json:"-" yaml:"copiedAt,omitempty"`
	// CopiedFrom names the lineup the carried positions come from: the
	// earlier match whose own lineup is in force at CopiedAt, or "" for the
	// starting lineup.
	CopiedFrom string `json:"-" yaml:"copiedFrom,omitempty"`
	// Own lists the positions a lineup with CopiedAt was changed at by its
	// own saves; the rest are carried.
	Own []Position `json:"-" yaml:"own,omitempty"`
	// History keeps, per position, the values it held before each stamped
	// change, each with the stamp it was set at, so the carry rule can read
	// what a position held at a stamp whatever order the changes arrived in.
	// Never trimmed: every stamped change of a position is kept (stage 6
	// review 8, m2; BenchmarkLineupSave measures the cost).
	History map[Position][]PositionChange `json:"-" yaml:"history,omitempty"`
	// RemovedAt is set on a lineup taken away (a match's own lineup by "Use
	// the previous match's lineup", or a starting lineup): the server's time
	// of the removal. The record is kept beside the lineups in force, never
	// read as one (state.Store.LoadTeamLineups leaves it out), so a lineup
	// carried by stamp (engine/lineup_carry.go) that was made while it was in
	// force still reads it: a removal is a change like any other, and one
	// made after a copy or a start never reaches it.
	RemovedAt int64 `json:"-" yaml:"removedAt,omitempty"`
}

// IsRemoved says the lineup is the kept record of one taken away (RemovedAt).
func (t TeamLineup) IsRemoved() bool { return t.RemovedAt > 0 }

// IsRemovalMarker says the record is a removal that took nothing away, kept
// for its stamp alone (stage 6 review 8, m1): CopiedAt equals RemovedAt, so no
// read is ever in force in it (a carry reads a removed record only at stamps
// after it was made and at or before it was removed). A change stamped before
// the removal that arrives after it lands in its epoch (SlotEpochAt), as it
// would have had it arrived in time.
func (t TeamLineup) IsRemovalMarker() bool { return t.RemovedAt > 0 && t.CopiedAt == t.RemovedAt }

// OwnPositionsOnly is a carried copy with its own positions alone, what a
// removed record is read for (stage 6 review 8, m3): a carry reads the
// positions a copy carried through the lineup it carried them from, never
// from the copy, so a removed copy keeps none of them. Any other lineup is
// returned as it is.
func (t TeamLineup) OwnPositionsOnly() TeamLineup {
	if !t.IsCarriedCopy() {
		return t
	}
	own := t.OwnSet()
	out := t.Clone()
	for p := range t.Positions {
		if !own[p] {
			delete(out.Positions, p)
		}
	}
	for p := range t.MemberIDs {
		if !own[p] {
			delete(out.MemberIDs, p)
		}
	}
	for p := range t.Stamps {
		if !own[p] {
			delete(out.Stamps, p)
		}
	}
	for p := range t.History {
		if !own[p] {
			delete(out.History, p)
		}
	}
	if len(out.MemberIDs) == 0 {
		out.MemberIDs = nil
	}
	if len(out.Stamps) == 0 {
		out.Stamps = nil
	}
	if len(out.History) == 0 {
		out.History = nil
	}
	return out
}

// RemovalMarker is the record of a removal at stamp at of a lineup slot (a
// match's own lineup, or a team's starting lineup) that held nothing then.
func RemovalMarker(teamID, matchID string, round int, at int64) TeamLineup {
	return TeamLineup{TeamID: teamID, MatchID: matchID, Round: round, Positions: map[Position]string{}, CopiedAt: at, RemovedAt: at}
}

// SlotEpochAt answers which record of one lineup slot a change stamped s
// belongs to (records: every record of the slot, the live one and the removed
// ones): the removed record whose removal is the first one stamped after s, as
// an index into records, or -1 for the live one (none of the removals is after
// s). A change stamped at a removal's own stamp comes after it.
func SlotEpochAt(records []TeamLineup, s int64) int {
	best := -1
	for i, r := range records {
		if r.IsRemoved() && r.RemovedAt > s && (best < 0 || r.RemovedAt < records[best].RemovedAt) {
			best = i
		}
	}
	return best
}

// ownChanges answers, per position the lineup changed itself (every position
// of a lineup that is not a carried copy, the Own ones of a copy), every value
// it held with the stamp it was set at, in stamp order, its current value last
// of equal stamps.
func (t TeamLineup) ownChanges() map[Position][]PositionChange {
	out := map[Position][]PositionChange{}
	add := func(p Position) {
		if _, done := out[p]; done {
			return
		}
		cs := slices.Clone(t.History[p])
		_, set := t.Positions[p]
		_, hasID := t.MemberIDs[p]
		_, stamped := t.Stamps[p]
		if set || hasID || stamped || len(cs) > 0 {
			cs = append(cs, t.Held(p))
		}
		if len(cs) > 0 {
			out[p] = sortHistory(cs)
		}
	}
	if t.IsCarriedCopy() {
		for _, p := range t.Own {
			add(p)
		}
		return out
	}
	for p := range t.Positions {
		add(p)
	}
	for p := range t.MemberIDs {
		add(p)
	}
	for p := range t.Stamps {
		add(p)
	}
	for p := range t.History {
		add(p)
	}
	return out
}

// withOwnChanges is t with its own positions set from changes: each
// position's latest change its value, the earlier ones its History. A
// position t held of its own that changes does not name is dropped (a copy
// then carries it). On a carried copy the positions it carried stay as they
// were, Own lists the positions changes names, and CopiedAt is the earliest
// change's stamp.
func (t TeamLineup) withOwnChanges(changes map[Position][]PositionChange) TeamLineup {
	out := t.Clone()
	for p := range t.ownChanges() {
		delete(out.Positions, p)
		delete(out.MemberIDs, p)
		delete(out.Stamps, p)
		delete(out.History, p)
	}
	if out.Positions == nil {
		out.Positions = map[Position]string{}
	}
	var own []Position
	first := int64(0)
	for p, cs := range changes {
		if len(cs) == 0 {
			continue
		}
		cur := cs[len(cs)-1]
		out.Positions[p] = cur.Name
		if cur.MemberID != "" {
			if out.MemberIDs == nil {
				out.MemberIDs = map[Position]string{}
			}
			out.MemberIDs[p] = cur.MemberID
		}
		if cur.Stamp > 0 {
			if out.Stamps == nil {
				out.Stamps = map[Position]int64{}
			}
			out.Stamps[p] = cur.Stamp
		}
		if len(cs) > 1 {
			if out.History == nil {
				out.History = map[Position][]PositionChange{}
			}
			out.History[p] = slices.Clone(cs[:len(cs)-1])
		}
		own = append(own, p)
		if cs[0].Stamp > 0 && (first == 0 || cs[0].Stamp < first) {
			first = cs[0].Stamp
		}
	}
	if len(out.MemberIDs) == 0 {
		out.MemberIDs = nil
	}
	if len(out.Stamps) == 0 {
		out.Stamps = nil
	}
	if len(out.History) == 0 {
		out.History = nil
	}
	if out.IsCarriedCopy() {
		slices.SortFunc(own, ComparePositions)
		out.Own = own
		if first > 0 {
			out.CopiedAt = first
		}
	}
	return out
}

// SplitAt splits the lineup at stamp at, as a removal stamped at sees it
// (stage 6 review 8, m1): before holds the changes it made before at, after
// the ones made at or after it, each change in exactly one of the two, so a
// History is never kept twice (m3). hasBefore says the lineup held something
// of its own before at (a value set before at; the empty value every first
// change keeps as "nothing from the beginning" does not count), hasAfter that
// it changed something at or after at. A match's own lineup that is not a
// carried copy (a migrated one) splits into its part before at and a carried
// copy made at its first change at or after at, as a save made after the
// removal would have made.
func (t TeamLineup) SplitAt(at int64) (before, after TeamLineup, hasBefore, hasAfter bool) {
	b, a := map[Position][]PositionChange{}, map[Position][]PositionChange{}
	for p, cs := range t.ownChanges() {
		for _, c := range cs {
			if c.Stamp < at {
				b[p] = append(b[p], c)
				if c.Stamp > 0 || c.Name != "" || c.MemberID != "" {
					hasBefore = true
				}
			} else {
				a[p] = append(a[p], c)
				hasAfter = true
			}
		}
	}
	before = t.withOwnChanges(b).OwnPositionsOnly()
	before.RemovedAt = at
	afterBase := t
	if !t.IsCarriedCopy() && t.MatchID != "" && !t.IsStartPin() {
		// A copy from the first change after the removal (withOwnChanges sets
		// CopiedAt and Own); nothing it held before carries over.
		afterBase = TeamLineup{TeamID: t.TeamID, CompetitionID: t.CompetitionID, MatchID: t.MatchID,
			Positions: map[Position]string{}, CopiedAt: at}
		for p, cs := range a {
			afterBase.Positions[p] = cs[len(cs)-1].Name
		}
		afterBase.Own = slices.SortedFunc(maps.Keys(a), ComparePositions)
	}
	after = afterBase.withOwnChanges(a)
	after.RemovedAt = t.RemovedAt
	return before, after, hasBefore, hasAfter
}

// RemoveFromSlot answers the records of one lineup slot (records: every record
// of the slot, the live one and the removed ones) after a removal of it
// stamped at, and whether the removal took a lineup away (stage 6 review 8,
// m1: a removal is ordered by its own stamp, like every change). The record
// whose epoch holds at (SlotEpochAt) splits there (SplitAt): what it held of
// its own before at is kept removed at at, what it changed at or after at
// stays in its epoch. A removal with nothing before it in its epoch is kept as
// a marker (RemovalMarker), so a change stamped before it that arrives later
// is still taken away by it. A removal at a stamp a removal of the slot
// already has is that removal (nothing changes). A kachinuki match's start
// pin taken away at or after its start leaves no record (a pin is never
// carried); one started after the removal stays.
func RemoveFromSlot(records []TeamLineup, teamID, matchID string, round int, at int64) (out []TeamLineup, removed, changed bool) {
	for _, r := range records {
		if r.IsRemoved() && r.RemovedAt == at {
			return records, false, false
		}
	}
	out, removed = removeFromSlot(records, teamID, matchID, round, at)
	return out, removed, true
}

func removeFromSlot(records []TeamLineup, teamID, matchID string, round int, at int64) ([]TeamLineup, bool) {
	target := SlotEpochAt(records, at)
	if target < 0 {
		for i, r := range records {
			if !r.IsRemoved() {
				target = i
			}
		}
	}
	out := make([]TeamLineup, 0, len(records)+2)
	for i, r := range records {
		if i != target {
			out = append(out, r)
		}
	}
	if target < 0 {
		return append(out, RemovalMarker(teamID, matchID, round, at)), false
	}
	t := records[target]
	if t.IsStartPin() {
		if at < t.StartedAt {
			return append(append(out, t), RemovalMarker(teamID, matchID, round, at)), false
		}
		return append(out, RemovalMarker(teamID, matchID, round, at)), true
	}
	before, after, hasBefore, hasAfter := t.SplitAt(at)
	if hasBefore {
		out = append(out, before)
	} else {
		out = append(out, RemovalMarker(teamID, matchID, round, at))
	}
	switch {
	case hasAfter:
		out = append(out, after)
	case t.IsRemoved():
		out = append(out, RemovalMarker(teamID, matchID, round, t.RemovedAt))
	}
	return out, hasBefore
}

// PositionChange is a value a lineup position held, from Stamp (0: from
// before any stamped change) on: a name, a member id, or neither (empty).
type PositionChange struct {
	Stamp    int64  `json:"-" yaml:"stamp"`
	Name     string `json:"-" yaml:"name,omitempty"`
	MemberID string `json:"-" yaml:"memberId,omitempty"`
}

// IsCarriedCopy says the lineup is a match's own lineup whose positions not
// in Own are carried by stamp (CopiedAt).
func (t TeamLineup) IsCarriedCopy() bool { return t.CopiedAt > 0 && !t.IsStartPin() }

// OwnSet is Own as a set.
func (t TeamLineup) OwnSet() map[Position]bool {
	out := make(map[Position]bool, len(t.Own))
	for _, p := range t.Own {
		out[p] = true
	}
	return out
}

// Clone is a deep copy: nothing of it is shared with t.
func (t TeamLineup) Clone() TeamLineup {
	t.Positions = maps.Clone(t.Positions)
	t.MemberIDs = maps.Clone(t.MemberIDs)
	t.Stamps = maps.Clone(t.Stamps)
	t.Own = slices.Clone(t.Own)
	if t.History != nil {
		h := make(map[Position][]PositionChange, len(t.History))
		for p, cs := range t.History {
			h[p] = slices.Clone(cs)
		}
		t.History = h
	}
	return t
}

// Held is the value p holds now, with the stamp it was set at.
func (t TeamLineup) Held(p Position) PositionChange {
	return PositionChange{Stamp: t.Stamps[p], Name: t.Positions[p], MemberID: t.MemberIDs[p]}
}

// HeldAt is the value p held at stamp at, as this lineup knows it: of its
// current value and its History, the one set latest before at (a value
// with no stamp counts from the beginning; the current value wins a tie).
// ok is false when none was set before at: on a carried copy the position
// was then the carried one, and elsewhere it held nothing then (every change
// keeps the value it replaced, and History is never trimmed). A value set
// after at is never answered for it.
func (t TeamLineup) HeldAt(p Position, at int64) (held PositionChange, ok bool) {
	cur := t.Held(p)
	_, set := t.Positions[p]
	_, hasID := t.MemberIDs[p]
	_, stamped := t.Stamps[p]
	entries := slices.Clone(t.History[p])
	if set || hasID || stamped || len(entries) > 0 {
		entries = append(entries, cur)
	}
	for _, e := range entries {
		if e.Stamp < at && (!ok || e.Stamp >= held.Stamp) {
			held, ok = e, true
		}
	}
	return held, ok
}

// ApplyStampedChanges is ApplyChanges for a save that may carry stamps
// (operator decision L1): a position holds its latest change BY STAMP, so a
// change stamped before the one it holds only joins its History; otherwise
// the value it held is kept in History (a carried position's is the
// source's, and is not) and the change lands. On a carried copy the changed
// positions join Own, an earlier change moving CopiedAt back to it.
func (t TeamLineup) ApplyStampedChanges(changed []Position, positions, memberIDs map[Position]string, stamps map[Position]int64, now int64) (TeamLineup, error) {
	if len(changed) == 0 {
		return TeamLineup{}, ErrLineupNoChangedPositions
	}
	before := t.Clone()
	own := before.OwnSet()
	carried := func(p Position) bool { return before.IsCarriedCopy() && !own[p] }
	history := before.History
	remember := func(p Position, c PositionChange) {
		if history == nil {
			history = map[Position][]PositionChange{}
		}
		history[p] = sortHistory(append(history[p], c))
	}
	// A change stamped before the one the position holds is kept in History,
	// where a carry read at a stamp between the two finds it, and the later
	// change keeps the position: the position is the latest change BY STAMP,
	// whatever order they arrive in.
	var apply []Position
	for _, p := range changed {
		s, cur := min(stamps[p], now), before.Stamps[p]
		if s > 0 && cur > 0 && s < cur && !carried(p) {
			name, ok := positions[p]
			if !ok {
				return TeamLineup{}, fmt.Errorf("%w: %q", ErrLineupChangedPositionMissing, p)
			}
			remember(p, PositionChange{Stamp: s, Name: name, MemberID: memberIDs[p]})
			continue
		}
		apply = append(apply, p)
	}
	out := before
	if len(apply) > 0 {
		var err error
		if out, err = before.ApplyChanges(apply, positions, memberIDs); err != nil {
			return TeamLineup{}, err
		}
	}
	for _, p := range apply {
		if stamps[p] > 0 && !carried(p) {
			remember(p, before.Held(p))
		}
	}
	out.History = history
	out.Own = before.Own
	out.StampChanges(apply, stamps, now)
	if out.IsCarriedCopy() {
		for _, p := range changed {
			if !own[p] {
				own[p] = true
				out.Own = append(out.Own, p)
			}
			if s := min(stamps[p], now); s > 0 && s < out.CopiedAt {
				out.CopiedAt = s
			}
		}
		slices.SortFunc(out.Own, ComparePositions)
	}
	return out, nil
}

// sortHistory puts one position's History in stamp order. Nothing is ever
// dropped: every stamped change of a position is kept (stage 6 review 8, m2).
func sortHistory(cs []PositionChange) []PositionChange {
	slices.SortStableFunc(cs, func(a, b PositionChange) int { return cmp.Compare(a.Stamp, b.Stamp) })
	return cs
}

// IsStartPin reports whether t is the lineup a kachinuki match started with,
// kept by the server (StartedAt), rather than one an operator saved.
func (t TeamLineup) IsStartPin() bool { return t.StartedAt > 0 }

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

// ErrLineupNoChangedPositions is the refusal of a save that says it changed
// positions and names none.
var ErrLineupNoChangedPositions = errors.New("team_lineup: a save that lists the positions it changed must list at least one")

// ErrLineupChangedPositionMissing is the refusal of a save that lists a changed
// position its positions map has no entry for: the new name is what the save
// says, so a changed position with nothing to say is a malformed save, not a
// request to clear it (a clear is an entry with no name).
var ErrLineupChangedPositionMissing = errors.New("team_lineup: a changed position has no entry in positions")

// ApplyChanges is t with each position in changed set from a save's own maps and
// every other position as t holds it: the way a save that names the positions it
// changed lands on the lineup stored when it arrives, so two saves changing
// different positions both keep their change. positions and memberIDs may carry
// the whole of the sender's form; only the changed keys are read, and the rest
// are ignored, whatever they hold.
//
// For a changed position p, positions must have an entry for it
// (ErrLineupChangedPositionMissing otherwise). The name is positions[p] and the
// member id memberIDs[p], absent meaning none. A position with neither is cleared,
// removed from both maps; any other is set: the name, and the id, which is removed
// when it is empty so a new name never keeps the old member's id beside it. A name
// with no id is a person typed in; an id with no name places a member who has none
// yet. An empty changed list is ErrLineupNoChangedPositions.
//
// The result shares nothing with t, and its Positions is never nil. It is not
// validated: ValidatePositions answers for the composed lineup, which is where a
// member placed at two positions is caught, naming both.
func (t TeamLineup) ApplyChanges(changed []Position, positions, memberIDs map[Position]string) (TeamLineup, error) {
	if len(changed) == 0 {
		return TeamLineup{}, ErrLineupNoChangedPositions
	}
	out := t
	out.Positions = maps.Clone(t.Positions)
	if out.Positions == nil {
		out.Positions = map[Position]string{}
	}
	out.MemberIDs = maps.Clone(t.MemberIDs)
	// A changed position's stamp is the caller's to set (StampChanges): the
	// one it had is no longer when it was last changed.
	out.Stamps = maps.Clone(t.Stamps)
	for _, p := range changed {
		delete(out.Stamps, p)
		name, ok := positions[p]
		if !ok {
			return TeamLineup{}, fmt.Errorf("%w: %q", ErrLineupChangedPositionMissing, p)
		}
		id := memberIDs[p]
		if name == "" && id == "" {
			delete(out.Positions, p)
			delete(out.MemberIDs, p)
			continue
		}
		out.Positions[p] = name
		if id == "" {
			delete(out.MemberIDs, p)
		} else {
			if out.MemberIDs == nil {
				out.MemberIDs = map[Position]string{}
			}
			out.MemberIDs[p] = id
		}
	}
	if len(out.MemberIDs) == 0 {
		out.MemberIDs = nil
	}
	if len(out.Stamps) == 0 {
		out.Stamps = nil
	}
	return out, nil
}

// StampChanges records when each changed position was changed, from stamps
// (position -> server-relative ms); a position with no stamp above zero keeps
// none. Every stamp is capped at now: a device clock ahead of the server's
// cannot place a change later than it reached the server.
func (t *TeamLineup) StampChanges(changed []Position, stamps map[Position]int64, now int64) {
	for _, p := range changed {
		s := stamps[p]
		if s <= 0 {
			continue
		}
		if t.Stamps == nil {
			t.Stamps = map[Position]int64{}
		}
		t.Stamps[p] = min(s, now)
	}
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

// fikPositionOrder is the five FIK names of a 5-person team in the order they
// are fielded, senpo first and taisho last. Both canonicalPositionOrder and
// ComparePositions read it, so the order is defined once.
var fikPositionOrder = [...]Position{PosSenpo, PosJiho, PosChuken, PosFukusho, PosTaisho}

// canonicalPositionOrder returns the position traversal order OrderedMembers
// walks: the five FIK names for a 5-person team, else 1..teamSize
// numerically. It stays its own named step rather than being inlined into
// OrderedMembers, so the order itself stays separately readable and testable.
func canonicalPositionOrder(teamSize int) []Position {
	if teamSize == 5 {
		return slices.Clone(fikPositionOrder[:])
	}
	order := make([]Position, teamSize)
	for i := 1; i <= teamSize; i++ {
		order[i-1] = PositionNumbered(i)
	}
	return order
}

// ComparePositions orders two positions the way a lineup is fielded, the order
// canonicalPositionOrder walks: the five FIK names from senpo to taisho, then
// numbered positions by their number (10 after 2), then any other position by
// name. A reader that must walk the positions a lineup holds without a team size
// (the Kachinuki Detail export, which labels a fighter by the first position it
// holds) orders them with this, so it agrees with the roster that walks
// OrderedMembers about which position comes first.
func ComparePositions(a, b Position) int {
	groupA, rankA := positionRank(a)
	groupB, rankB := positionRank(b)
	return cmp.Or(
		cmp.Compare(groupA, groupB),
		cmp.Compare(rankA, rankB),
		cmp.Compare(a, b),
	)
}

// positionRank places a position in the order ComparePositions gives: group 0
// for a FIK name (its rank is its place in fikPositionOrder), group 1 for a
// numbered position (its rank is its number), group 2 for any other.
func positionRank(p Position) (group, rank int) {
	if i := slices.Index(fikPositionOrder[:], p); i >= 0 {
		return 0, i
	}
	if n, err := strconv.Atoi(string(p)); err == nil && n >= 0 {
		return 1, n
	}
	return 2, 0
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

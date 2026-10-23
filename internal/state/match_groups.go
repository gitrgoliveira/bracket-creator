// Package state, match_groups.go owns the GROUPS a match write is merged by
// (bc-mrgc, operator ruling 2026-10-03: "Nothing should be dropped. All events
// must be ordered.").
//
// A match result is split into groups of fields that change together. A write
// names the groups it changes (MatchResult.Changed) and engine.mergeMatchWrite
// applies each one only if the write's stamp is not older than that group's
// stored stamp (MatchResult.GroupStamps); a group the write did not change is
// never overwritten, and a change that is not applied is kept in the match's
// history (match_history.go). This file is the ONE owner of what each group
// holds: CopyGroup moves a group's fields, GroupDiffers compares them,
// GroupValue projects them for the history, and stampGroups records a change.
// Everything else asks these, never a hand-copied field list.
//
// NOT groups, on purpose: the sides and their ids, court, schedule, round,
// ReopenPending, IsOverridden and the bracket structure. Each has its own
// writer (the draw, scheduling, the reopen, the handler's re-stamp), and none
// is something a score write changes.
package state

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// The scalar groups. A team match additionally has one group per bout row,
// BoutGroup(position).
const (
	// GroupPoints holds the match-level scoreline: IpponsA, IpponsB,
	// HansokuA, HansokuB.
	GroupPoints = "points"
	// GroupResult holds the verdict: Status, Winner, WinnerID, Decision,
	// DecisionBy, DecisionReason, and the audit fields that describe it,
	// CorrectionReason and ResultSource.
	GroupResult = "result"
	// GroupEncho holds the match-level overtime block.
	GroupEncho = "encho"
	// GroupFlags holds the engi referee flag counts.
	GroupFlags = "flags"
	// GroupRep holds the pool daihyosen/tiebreaker representative players.
	GroupRep = "rep"
	// GroupRepPickA and GroupRepPickB hold ONE side's representative for a team
	// match's representative bout each: SideAMemberID (side A, Aka) and
	// SideBMemberID (side B, Shiro) of the row at DaihyosenSubPosition. Each
	// side's pick is a change of its own, dated and ordered apart from the other
	// side's pick and from the bout row they sit on (bout:-1), which keeps the
	// bout's score and result: two captains picking at about the same moment on
	// their own phones both land, a point struck on the bout never alters who
	// was picked, and a pick never alters the score. The row's WinnerMemberID
	// moves with the bout (CopyGroup copies the row), but it is derived state:
	// no group's comparison includes it, and the merge works it out again from
	// the winner's name and the stored picks after every write
	// (SubMatchResult.ReconcileWinnerMemberID). Not to be confused with
	// GroupRep, the pool daihyosen/tiebreaker players. RepPickGroup names the
	// group of a side.
	GroupRepPickA = "repPickA"
	GroupRepPickB = "repPickB"
	// GroupRecorded holds which numbered kachinuki bouts Record bout closed
	// (SubMatchResult.Recorded on every row with a position > 0), as the sorted
	// list of their positions. It is the SERVER's (ServerGroups): the advance
	// sets it, RemoveTrailingKachinukiBout takes it off the bout whose pairing it
	// removes, and the merge takes it off a recorded bout whose overtime a write
	// raises. Each stamps it like any group, so a copy holding the mark is ordered
	// after one without it on every device. A bout row's group (bout:N) does not
	// hold the mark, so a point on bout N and Record bout never fence each other.
	GroupRecorded = "recorded"

	boutGroupPrefix = "bout:"
)

// ScalarGroups lists the scalar groups a client write names, in their fixed
// order.
var ScalarGroups = []string{GroupPoints, GroupResult, GroupEncho, GroupFlags, GroupRep, GroupRepPickA, GroupRepPickB}

// ServerGroups lists the groups only the server writes. No client write names
// one: the score handler refuses it in `changed`, and the merge copies the
// stored value over whatever a client's rows say.
var ServerGroups = []string{GroupRecorded}

// IsServerGroup reports whether group is one only the server writes.
func IsServerGroup(group string) bool {
	return slices.Contains(ServerGroups, group)
}

// RepPickGroup names the group of side's representative pick, "" for a value
// that is neither side.
func RepPickGroup(side domain.MatchSide) string {
	switch side {
	case domain.MatchSideA:
		return GroupRepPickA
	case domain.MatchSideB:
		return GroupRepPickB
	}
	return ""
}

// isRepPickGroup reports whether group is one side's representative pick.
func isRepPickGroup(group string) bool {
	return group == GroupRepPickA || group == GroupRepPickB
}

// BoutGroup names the group of the bout row at position (1..n, or
// DaihyosenSubPosition for the representative bout).
func BoutGroup(position int) string {
	return boutGroupPrefix + strconv.Itoa(position)
}

// ParseBoutGroup reports the bout position a group names, and whether it
// names one at all.
func ParseBoutGroup(group string) (int, bool) {
	rest, ok := strings.CutPrefix(group, boutGroupPrefix)
	if !ok {
		return 0, false
	}
	pos, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return pos, true
}

// ValidGroup reports whether group names a scalar group, a server group or a
// bout row. The score handler refuses a `changed` entry that is not one, or
// that is a server group (IsServerGroup).
func ValidGroup(group string) bool {
	if slices.Contains(ScalarGroups, group) || IsServerGroup(group) {
		return true
	}
	_, ok := ParseBoutGroup(group)
	return ok
}

// IsScoringGroup reports whether a change to group changes who won: the
// match-level points, a bout row, or the engi flags.
func IsScoringGroup(group string) bool {
	if group == GroupPoints || group == GroupFlags {
		return true
	}
	_, ok := ParseBoutGroup(group)
	return ok
}

// SubPositions lists the distinct bout positions of subs, in row order.
func SubPositions(subs []SubMatchResult) []int {
	seen := make(map[int]bool, len(subs))
	out := make([]int, 0, len(subs))
	for i := range subs {
		if !seen[subs[i].Position] {
			seen[subs[i].Position] = true
			out = append(out, subs[i].Position)
		}
	}
	return out
}

// subAt returns the first bout row at position, or nil.
func subAt(subs []SubMatchResult, position int) *SubMatchResult {
	for i := range subs {
		if subs[i].Position == position {
			return &subs[i]
		}
	}
	return nil
}

// SubResultAt is subAt as a value: a copy of the first bout row at position,
// the empty row when subs has none (a position a match lacks holds no member id,
// which is what a caller comparing a row with the stored one wants).
func SubResultAt(subs []SubMatchResult, position int) SubMatchResult {
	if row := subAt(subs, position); row != nil {
		return *row
	}
	return SubMatchResult{}
}

// repPicksOf returns the two representatives the row at DaihyosenSubPosition
// of subs names, empty when there is no such row.
func repPicksOf(subs []SubMatchResult) (sideA, sideB string) {
	if row := subAt(subs, DaihyosenSubPosition); row != nil {
		return row.SideAMemberID, row.SideBMemberID
	}
	return "", ""
}

// withRepPick returns a copy of subs whose row at DaihyosenSubPosition names id
// as side's representative, and subs itself when there is no such row or side
// is neither side. The list is copied, never edited in place, so a row another
// copy of the match shares is never written through.
func withRepPick(subs []SubMatchResult, side domain.MatchSide, id string) []SubMatchResult {
	if side != domain.MatchSideA && side != domain.MatchSideB {
		return subs
	}
	for i := range subs {
		if subs[i].Position != DaihyosenSubPosition {
			continue
		}
		out := append([]SubMatchResult(nil), subs...)
		if side == domain.MatchSideA {
			out[i].SideAMemberID = id
		} else {
			out[i].SideBMemberID = id
		}
		return out
	}
	return subs
}

// RepPicks returns the two representatives the row at DaihyosenSubPosition
// names, empty when the match has no such row.
func (m *MatchResult) RepPicks() (sideA, sideB string) {
	return repPicksOf(m.SubResults)
}

// SetRepPick puts id on the row at DaihyosenSubPosition as side's
// representative. It does nothing when the match has no such row: a pick has no
// bout to land on. The list is replaced, not edited in place (withRepPick).
func (m *MatchResult) SetRepPick(side domain.MatchSide, id string) {
	m.SubResults = withRepPick(m.SubResults, side, id)
}

// RepPicks is MatchResult.RepPicks for a bracket match.
func (bm *BracketMatch) RepPicks() (sideA, sideB string) {
	return repPicksOf(bm.SubResults)
}

// SetRepPick is MatchResult.SetRepPick for a bracket match.
func (bm *BracketMatch) SetRepPick(side domain.MatchSide, id string) {
	bm.SubResults = withRepPick(bm.SubResults, side, id)
}

// boutOrderKey sorts bout rows the way every writer appends them: numbered
// bouts ascending, the representative bout (and any other negative) after.
func boutOrderKey(position int) int {
	if position < 0 {
		return 1<<30 - position
	}
	return position
}

// setSubAt replaces the row at position in subs with row, removes it when row
// is nil, or inserts it in bout order when subs has none. Every row at that
// position goes: a hand-edited file holding two is repaired by the merge, not
// copied twice.
func setSubAt(subs []SubMatchResult, position int, row *SubMatchResult) []SubMatchResult {
	out := make([]SubMatchResult, 0, len(subs))
	placed := false
	for i := range subs {
		if subs[i].Position == position {
			if row != nil && !placed {
				out = append(out, CloneSubResults([]SubMatchResult{*row})[0])
				placed = true
			}
			continue
		}
		out = append(out, subs[i])
	}
	if row != nil && !placed {
		out = append(out, CloneSubResults([]SubMatchResult{*row})[0])
		sort.SliceStable(out, func(i, j int) bool {
			return boutOrderKey(out[i].Position) < boutOrderKey(out[j].Position)
		})
	}
	if len(out) == 0 && subs == nil {
		return nil
	}
	return out
}

// CopyGroup copies group's fields from src into dst, deep, so dst never
// aliases src. A bout group copies src's row at that position, or removes
// dst's when src has none.
func CopyGroup(dst, src *MatchResult, group string) {
	switch group {
	case GroupPoints:
		dst.IpponsA = cloneStrings(src.IpponsA)
		dst.IpponsB = cloneStrings(src.IpponsB)
		dst.HansokuA, dst.HansokuB = src.HansokuA, src.HansokuB
	case GroupResult:
		dst.Status = src.Status
		dst.Winner, dst.WinnerID = src.Winner, src.WinnerID
		// The transient side hint travels with the winner it describes (a
		// stored copy may carry one, see losingSide), never beside another.
		dst.WinnerSide = src.WinnerSide
		dst.Decision, dst.DecisionBy, dst.DecisionReason = src.Decision, src.DecisionBy, src.DecisionReason
		dst.CorrectionReason, dst.ResultSource = src.CorrectionReason, src.ResultSource
	case GroupEncho:
		dst.Encho = src.Encho.Clone()
	case GroupFlags:
		dst.FlagsA, dst.FlagsB = src.FlagsA, src.FlagsB
	case GroupRep:
		dst.RepPlayerA, dst.RepPlayerB = src.RepPlayerA, src.RepPlayerB
	case GroupRepPickA:
		a, _ := src.RepPicks()
		dst.SetRepPick(domain.MatchSideA, a)
	case GroupRepPickB:
		_, b := src.RepPicks()
		dst.SetRepPick(domain.MatchSideB, b)
	case GroupRecorded:
		setRecordedPositions(dst.SubResults, RecordedPositions(src.SubResults))
	default:
		if pos, ok := ParseBoutGroup(group); ok {
			row := subAt(src.SubResults, pos)
			// The recorded mark is GroupRecorded's, not the bout's: a copy of
			// the bout keeps the mark the destination row already holds, and a
			// row the destination lacks arrives unmarked. A bout copy never
			// moves a mark, so a row copied from a client's payload cannot
			// bring one; only GroupRecorded does.
			if row != nil {
				kept := *row
				kept.Recorded = false
				if dstRow := subAt(dst.SubResults, pos); dstRow != nil {
					kept.Recorded = dstRow.Recorded
				}
				row = &kept
			}
			// The representatives are not the bout's: a copy of the bout keeps the
			// picks the destination row already holds (GroupRepPickA and
			// GroupRepPickB move them), so the copies give the same result in
			// any order.
			if dstRow := subAt(dst.SubResults, pos); pos == DaihyosenSubPosition && row != nil && dstRow != nil {
				kept := *row
				kept.SideAMemberID, kept.SideBMemberID = dstRow.SideAMemberID, dstRow.SideBMemberID
				row = &kept
			}
			dst.SubResults = setSubAt(dst.SubResults, pos, row)
		}
	}
}

// GroupDiffers reports whether a and b hold different values in group. An
// empty and a nil ippon list are the same scoreline.
func GroupDiffers(a, b *MatchResult, group string) bool {
	return !reflect.DeepEqual(groupProjection(a, group), groupProjection(b, group))
}

// GroupValue is group's fields of m as JSON, the shape the match history keeps
// for a change it did not apply. A bout group that has no row is "null".
func GroupValue(m *MatchResult, group string) json.RawMessage {
	b, err := json.Marshal(groupProjection(m, group))
	if err != nil {
		return json.RawMessage("null")
	}
	return b
}

// SetGroupFromValue sets one group of m to a value GroupValue projected (a
// score log baseline's, bc-evts), through CopyGroup, so the one owner of
// which fields a group holds also decides what setting it moves. A bout
// group's null removes the bout (a tombstone).
func SetGroupFromValue(m *MatchResult, group string, raw json.RawMessage) error {
	var src MatchResult
	src.SubResults = CloneSubResults(m.SubResults)
	unmarshal := func(v any) error {
		if err := json.Unmarshal(raw, v); err != nil {
			return fmt.Errorf("group %s: %w", group, err)
		}
		return nil
	}
	switch group {
	case GroupPoints:
		var v pointsGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.IpponsA, src.IpponsB, src.HansokuA, src.HansokuB = v.IpponsA, v.IpponsB, v.HansokuA, v.HansokuB
	case GroupResult:
		var v resultGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.Status, src.Winner, src.WinnerID = v.Status, v.Winner, v.WinnerID
		src.Decision, src.DecisionBy, src.DecisionReason = v.Decision, v.DecisionBy, v.DecisionReason
		src.CorrectionReason, src.ResultSource = v.CorrectionReason, v.ResultSource
	case GroupEncho:
		if err := unmarshal(&src.Encho); err != nil {
			return err
		}
	case GroupFlags:
		var v flagsGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.FlagsA, src.FlagsB = v.FlagsA, v.FlagsB
	case GroupRep:
		var v repGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.RepPlayerA, src.RepPlayerB = v.RepPlayerA, v.RepPlayerB
	case GroupRepPickA:
		var v repPickAGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.SetRepPick(domain.MatchSideA, v.SideAMemberID)
	case GroupRepPickB:
		var v repPickBGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		src.SetRepPick(domain.MatchSideB, v.SideBMemberID)
	case GroupRecorded:
		var v recordedGroup
		if err := unmarshal(&v); err != nil {
			return err
		}
		setRecordedPositions(src.SubResults, v.Positions)
	default:
		pos, ok := ParseBoutGroup(group)
		if !ok {
			return fmt.Errorf("group %q: not a group", group)
		}
		var row *SubMatchResult
		if err := unmarshal(&row); err != nil {
			return err
		}
		if row != nil {
			row.Position = pos
		}
		src.SubResults = setSubAt(src.SubResults, pos, row)
	}
	CopyGroup(m, &src, group)
	return nil
}

type pointsGroup struct {
	IpponsA  []string `json:"ipponsA"`
	IpponsB  []string `json:"ipponsB"`
	HansokuA int      `json:"hansokuA"`
	HansokuB int      `json:"hansokuB"`
}

type resultGroup struct {
	Status           MatchStatus `json:"status"`
	Winner           string      `json:"winner"`
	WinnerID         string      `json:"winnerId,omitempty"`
	Decision         string      `json:"decision"`
	DecisionBy       string      `json:"decisionBy,omitempty"`
	DecisionReason   string      `json:"decisionReason,omitempty"`
	CorrectionReason string      `json:"correctionReason,omitempty"`
	ResultSource     string      `json:"resultSource,omitempty"`
}

type flagsGroup struct {
	FlagsA int `json:"flagsA"`
	FlagsB int `json:"flagsB"`
}

// repPickAGroup and repPickBGroup hold ONE key each, named as the row's field,
// so a pick kept in the history reads as the field it was.
type repPickAGroup struct {
	SideAMemberID string `json:"sideAMemberId"`
}

type repPickBGroup struct {
	SideBMemberID string `json:"sideBMemberId"`
}

// recordedGroup is GroupRecorded's value: the positions of the numbered bouts
// Record bout closed, sorted, never nil (so an empty set compares and
// serializes as one value).
type recordedGroup struct {
	Positions []int `json:"positions"`
}

// RecordedPositions lists, sorted, the positions of the numbered rows (position
// > 0) marked recorded: GroupRecorded's value.
func RecordedPositions(subs []SubMatchResult) []int {
	out := []int{}
	for i := range subs {
		if subs[i].Position > 0 && subs[i].Recorded {
			out = append(out, subs[i].Position)
		}
	}
	sort.Ints(out)
	return out
}

// setRecordedPositions marks each numbered row recorded exactly when its
// position is in positions. A position with no row marks nothing.
func setRecordedPositions(subs []SubMatchResult, positions []int) {
	for i := range subs {
		// Only a numbered bout can be recorded: a mark on any other row (the
		// representative bout, -1) can only have come from a client, which may
		// never write the mark, so it is cleared rather than kept.
		subs[i].Recorded = subs[i].Position > 0 && slices.Contains(positions, subs[i].Position)
	}
}

type repGroup struct {
	RepPlayerA string `json:"repPlayerA"`
	RepPlayerB string `json:"repPlayerB"`
}

// groupProjection is the single list of which fields each group holds; the
// comparison and the history read it, and CopyGroup moves the same fields.
func groupProjection(m *MatchResult, group string) any {
	switch group {
	case GroupPoints:
		return pointsGroup{IpponsA: nonNilStrings(m.IpponsA), IpponsB: nonNilStrings(m.IpponsB), HansokuA: m.HansokuA, HansokuB: m.HansokuB}
	case GroupResult:
		return resultGroup{
			Status: m.Status, Winner: m.Winner, WinnerID: m.WinnerID,
			Decision: m.Decision, DecisionBy: m.DecisionBy, DecisionReason: m.DecisionReason,
			CorrectionReason: m.CorrectionReason, ResultSource: m.ResultSource,
		}
	case GroupEncho:
		if !m.Encho.On() {
			return (*EnchoMetadata)(nil)
		}
		return m.Encho
	case GroupFlags:
		return flagsGroup{FlagsA: m.FlagsA, FlagsB: m.FlagsB}
	case GroupRep:
		return repGroup{RepPlayerA: m.RepPlayerA, RepPlayerB: m.RepPlayerB}
	case GroupRepPickA:
		a, _ := m.RepPicks()
		return repPickAGroup{SideAMemberID: a}
	case GroupRepPickB:
		_, b := m.RepPicks()
		return repPickBGroup{SideBMemberID: b}
	case GroupRecorded:
		return recordedGroup{Positions: RecordedPositions(m.SubResults)}
	}
	if pos, ok := ParseBoutGroup(group); ok {
		row := subAt(m.SubResults, pos)
		if row == nil {
			return (*SubMatchResult)(nil)
		}
		c := CloneSubResults([]SubMatchResult{*row})[0]
		c.IpponsA, c.IpponsB = nonNilStrings(c.IpponsA), nonNilStrings(c.IpponsB)
		// The recorded mark is GroupRecorded's, and the mark ids are derived
		// by the replay (bc-evts), never part of the bout's value.
		c.Recorded = false
		c.MarkIDs = nil
		if pos == DaihyosenSubPosition {
			// The representatives are GroupRepPickA's and GroupRepPickB's, not
			// the bout's. The row's WinnerMemberID is derived from the winner's
			// name and those picks after every merge
			// (SubMatchResult.ReconcileWinnerMemberID), and the client's bout
			// comparison leaves it out, so it is no change of the bout's: a
			// write that echoes the row with another id (or none) has changed
			// nothing.
			c.SideAMemberID, c.SideBMemberID, c.WinnerMemberID = "", "", ""
		}
		return &c
	}
	return nil
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func cloneStrings(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

// CloneGroupStamps deep-copies a stamp map; nil stays nil (a legacy match).
func CloneGroupStamps(stamps map[string]int64) map[string]int64 {
	if stamps == nil {
		return nil
	}
	out := make(map[string]int64, len(stamps))
	for k, v := range stamps {
		out[k] = v
	}
	return out
}

// groupStampOf is the stored stamp of group: its own entry, or ModifiedAt for
// a match written before groups existed (nil map), so a legacy match is
// compared exactly as the whole-match guard compared it. Once a match has a
// map, a group with no entry was never written: 0.
func groupStampOf(stamps map[string]int64, modifiedAt int64, group string) int64 {
	if stamps == nil {
		return modifiedAt
	}
	if v, ok := stamps[group]; ok || !isRepPickGroup(group) {
		return v
	}
	// A map written before the representatives had a group of their own dated
	// them with the bout row they sit on: until they are stamped themselves,
	// that is their date.
	return stamps[BoutGroup(DaihyosenSubPosition)]
}

// GroupStamp is the stored stamp of group on m (see groupStampOf).
func (m *MatchResult) GroupStamp(group string) int64 {
	return groupStampOf(m.GroupStamps, m.ModifiedAt, group)
}

// GroupStamp is the stored stamp of group on bm (see groupStampOf).
func (bm *BracketMatch) GroupStamp(group string) int64 {
	return groupStampOf(bm.GroupStamps, bm.ModifiedAt, group)
}

// MaterializedGroupStamps returns a copy of the stamp map with a legacy
// match's implicit stamps made explicit: every scalar group and every bout row
// it holds, at its ModifiedAt. Needed before ONE group of a legacy match is
// stamped, or every other group would start reading the new ModifiedAt.
func MaterializedGroupStamps(stamps map[string]int64, modifiedAt int64, positions []int) map[string]int64 {
	if stamps != nil {
		out := CloneGroupStamps(stamps)
		// Fix the fallback groupStampOf reads, or a side's pick date would
		// follow the bout row's every later stamp. A side without a key of its
		// own takes the bout row's stamp.
		for _, g := range []string{GroupRepPickA, GroupRepPickB} {
			if _, ok := out[g]; ok {
				continue
			}
			if v, ok := out[BoutGroup(DaihyosenSubPosition)]; ok {
				out[g] = v
			}
		}
		return out
	}
	out := map[string]int64{}
	if modifiedAt <= 0 {
		return out
	}
	hasRepBout := slices.Contains(positions, DaihyosenSubPosition)
	for _, g := range ScalarGroups {
		// The representatives' groups live on the representative bout's row:
		// a match without that row has nothing to date, so a legacy match is
		// not given a key it never held (a stamp a write NAMED is another
		// matter: stampGroups adds it itself).
		if isRepPickGroup(g) && !hasRepBout {
			continue
		}
		out[g] = modifiedAt
	}
	for _, p := range positions {
		out[BoutGroup(p)] = modifiedAt
	}
	return out
}

// stampGroups is the ONE writer of a change's stamp: it records that groups
// changed at stamp and keeps ModifiedAt the newest stamp. A stamp of 0 (an
// unstamped write) moves nothing, exactly as an unstamped write always kept
// the stored ModifiedAt. positions are the bout rows the match held BEFORE
// the change, for the legacy materialization.
// stampGroups is the ONE writer of a change's stamp: it records that groups
// changed at stamp and keeps ModifiedAt the newest stamp. A stamp of 0 (an
// unstamped write) moves nothing, exactly as an unstamped write always kept
// the stored ModifiedAt. It never LOWERS a group's stamp either: a caller
// that already holds a later count for a group (an engi finish let through
// as a HeldEcho under an older stamp, bc-mrgc Finding 2) must not drag that
// group's stamp backwards, or a later-arriving change made between the two
// would read as applying after a group it never actually followed. positions
// are the bout rows the match held BEFORE the change, for the legacy
// materialization.
func stampGroups(stamps *map[string]int64, modifiedAt *int64, positions []int, stamp int64, groups ...string) {
	if stamp <= 0 || len(groups) == 0 {
		return
	}
	m := MaterializedGroupStamps(*stamps, *modifiedAt, positions)
	for _, g := range groups {
		if stamp > m[g] {
			m[g] = stamp
		}
	}
	*stamps = m
	if stamp > *modifiedAt {
		*modifiedAt = stamp
	}
}

// StampGroups records that groups of m changed at stamp. Every writer that
// changes a group's fields outside the merge (a reopen, a requeue, an
// override, the engi recorder, the kachinuki advance) calls this or its
// BracketMatch twin, so the next write is ordered against the change.
func (m *MatchResult) StampGroups(stamp int64, groups ...string) {
	stampGroups(&m.GroupStamps, &m.ModifiedAt, SubPositions(m.SubResults), stamp, groups...)
}

// StampGroups is MatchResult.StampGroups for a bracket match.
func (bm *BracketMatch) StampGroups(stamp int64, groups ...string) {
	stampGroups(&bm.GroupStamps, &bm.ModifiedAt, SubPositions(bm.SubResults), stamp, groups...)
}

// applyMergedGroupStamps overwrites groups' recorded stamps with ones
// mergeMatchWrite has already decided for them, bypassing stampGroups' never-
// lower guard (bc-mrgc fix: the engi recorder). The merge is the one place
// that orders a group against every other stored group, including S2's
// displacement (mergeByReplay, MergeReport.Displaced): a finish that arrives after a newer but INVALID
// stored count applies on its own, older scoreline and moves that count to
// the history, which legitimately puts the group's stamp BACK to the
// finish's own time. A naive never-lower re-stamp cannot express that move
// and leaves the group stuck at the displaced count's later stamp, so a
// further, valid write made between the two is wrongly held. ModifiedAt is
// recomputed as the newest stamp left on the match (mergeMatchWrite's own
// rule), since lowering one group's stamp can leave an older one as the new
// newest.
func applyMergedGroupStamps(stamps *map[string]int64, modifiedAt *int64, positions []int, decided map[string]int64, groups ...string) {
	m := MaterializedGroupStamps(*stamps, *modifiedAt, positions)
	for _, g := range groups {
		if v, ok := decided[g]; ok {
			m[g] = v
		}
	}
	*stamps = m
	var newest int64
	for _, v := range m {
		if v > newest {
			newest = v
		}
	}
	*modifiedAt = newest
}

// ApplyMergedGroupStamps is applyMergedGroupStamps for a pool/league match.
func (m *MatchResult) ApplyMergedGroupStamps(decided map[string]int64, groups ...string) {
	applyMergedGroupStamps(&m.GroupStamps, &m.ModifiedAt, SubPositions(m.SubResults), decided, groups...)
}

// ApplyMergedGroupStamps is applyMergedGroupStamps for a bracket match.
func (bm *BracketMatch) ApplyMergedGroupStamps(decided map[string]int64, groups ...string) {
	applyMergedGroupStamps(&bm.GroupStamps, &bm.ModifiedAt, SubPositions(bm.SubResults), decided, groups...)
}

// MergeReport is what engine.mergeMatchWrite decided for one write (bc-mrgc).
// It rides on the incoming MatchResult (MatchResult.Merge) to the history
// writer and to the handlers' heldGroups; it is never persisted.
type MergeReport struct {
	// Stamp is the write's own stamp (its modifiedAt), before the merge folds
	// the stored ModifiedAt into the result.
	Stamp int64
	// Changed is the effective list of groups the write changes, after the
	// default (every group the payload carries) and the rule that a running
	// write over a finished match never carries the result.
	Changed []string
	// Applied, Held and HeldEcho partition Changed: a held group is one a
	// newer stored change outranks; its incoming value is in HeldValues.
	// HeldEcho is a held group whose incoming value equals the stored one
	// (an echo: a decision's "no overtime" over a match with none), which is
	// not a loss and is therefore neither listed as held nor kept in
	// HeldValues. Unchanged is the part of Applied whose value equals the
	// stored one (an echo: a client that names no groups sends every one of
	// them back).
	Applied    []string
	Held       []string
	HeldEcho   []string
	Unchanged  []string
	HeldValues map[string]json.RawMessage
	// HoldReason, when set, is why groups were held other than by their
	// stamps: every group the write changes, regardless of its stamp (a
	// running write older than one the same board already sent: "older
	// revision of this board"), or the scoring a finish or a decision stamped
	// before it had closed out (MatchClosed). Recorded in the history entry.
	HoldReason string
	// MatchClosed reports that the held groups were held because a finish or
	// a decision stamped before the write had already closed the match (the
	// closed-match rule, operator ruling 2026-10-15): the write is kept in the
	// history and the answer carries heldReason "match_closed", so the device
	// is told the match is finished and to tap Correct.
	MatchClosed bool
	// Displaced are STORED changes this write moved to the history: a finish
	// or a decision that arrives after scoring stamped later than it closes
	// that scoring out, exactly as the same scoring arriving after the finish
	// is refused, and the scoring is kept in the history as held, one entry
	// per stamp. The answer names their groups (displacedGroups).
	Displaced []DisplacedChange
	// ResultChanged reports whether the stored verdict moved: the result
	// group applied, a withdrawal was cleared, or the winner was worked out
	// again. A write that leaves it unmoved has no eligibility consequence.
	ResultChanged bool
	// EventsNotApplied are the score events (bc-evts) this write's baseline
	// replaced without having seen them (beforeBaseline, D5): each is kept in
	// the history with that outcome, and the answer names them.
	EventsNotApplied []string
	// Replayed reports that the score replay judged the write (a scoring
	// snapshot or a decision) rather than the merge's own holds: every group
	// it changed is then logged with its baseline at the write's stamp, held
	// ones with the values it sent, so the log orders it like a tap.
	Replayed bool
	// Baseline is the baseline the replay judged for such a write: the write
	// as it was made, which the door logs as it is (id, write id and
	// revision aside).
	Baseline *ScoreBaseline `json:"-"`
	// LogPrefix are the score log lines the replay that decided a snapshot
	// write needed ahead of the write's own baseline (the match's migration
	// baseline, or a drift baseline): the door logs them first.
	LogPrefix []ScoreLogLine
	// Prior is the match as it was stored before the write (a copy), set when
	// the write was decided by the replay over a log that had lines: the door
	// reads the outcome each earlier event had at its own arrival from it, so
	// a move the write causes (a tap that found no bout row, and finds one
	// the write pads) is recorded.
	Prior *MatchResult `json:"-"`
	// Replay is the engine's own record of the replay that decided the write
	// (the log it read, and the replay of it with the write's baseline),
	// which the door that logs the baseline reuses rather than reading and
	// replaying the log again. Opaque here.
	Replay any `json:"-"`
	// ResultLocked, when set, is the wire code (HeldReasonDownstreamPlayed or
	// HeldReasonDownstreamRunning) of a finish or decision held because the
	// next knockout match had already been played or started (operator decision
	// K1, 2026-10-16): the stored result is locked, and the write is kept in the
	// history instead of displacing it.
	ResultLocked string
	// StoredResultInForce and StoredResultSeen are what the replay's one owner
	// of "the result in force" (engine.ResultInForceAt) says of the log as the
	// write found it, set when the write displaced something and named no
	// correction: whether a finished result was in force at the log's end (the
	// stored result the write would displace; none after a reopen or a send
	// back to the queue), and whether the write's SENT basis holds the line
	// that set it. The result lock reads them (engine.rivalDisplacesResult).
	StoredResultInForce bool `json:"-"`
	StoredResultSeen    bool `json:"-"`
}

// Superseded reports whether nothing of the write applied: a group it
// changes is held, and every other one it names only echoes the stored value.
// Only then is it answered applied:false, and nothing is written but its
// history entry.
func (r *MergeReport) Superseded() bool {
	return r != nil && len(r.Held)+len(r.HeldEcho) > 0 && len(r.Applied) == len(r.Unchanged) && len(r.Displaced) == 0
}

// DisplacedChange is a stored change a later-arriving, earlier-stamped write
// moved out of the match and into its history (MergeReport.Displaced): its
// stamp, the values it had set per group, and why it could not stand.
type DisplacedChange struct {
	Stamp  int64
	Values map[string]json.RawMessage
	Reason string
}

// DisplacedGroups lists the groups whose stored values the write moved to
// the history, sorted; nil when none.
func (r *MergeReport) DisplacedGroups() []string {
	if r == nil {
		return nil
	}
	var out []string
	for _, d := range r.Displaced {
		for g := range d.Values {
			if !slices.Contains(out, g) {
				out = append(out, g)
			}
		}
	}
	sort.Strings(out)
	return out
}

// HeldReasonMatchClosed is the wire code (heldReason) of a scoring write held
// because a finish or a decision stamped before it had already closed the
// match (the closed-match rule): the write is kept in the history and the
// device is told to tap Correct.
const HeldReasonMatchClosed = "match_closed"

// HeldReason is the code a response carries beside heldGroups, "" when the
// held groups were held by their stamps alone.
func (r *MergeReport) HeldReason() string {
	if r != nil && r.ResultLocked != "" && len(r.Held) > 0 {
		return r.ResultLocked
	}
	if r == nil || len(r.Held)+len(r.Displaced) == 0 || !r.MatchClosed {
		return ""
	}
	return HeldReasonMatchClosed
}

// HeldReasonDownstreamPlayed and HeldReasonDownstreamRunning are the wire codes
// (heldReason) of a finish or decision held because the next knockout match was
// already played or started (operator decision K1, 2026-10-16): the recorded
// result is locked. They reuse the codes of the explicit-correction refusal.
const (
	HeldReasonDownstreamPlayed  = "downstream_knockout_played"
	HeldReasonDownstreamRunning = "downstream_knockout_running"
)

// HeldGroups is the list a response carries, nil when nothing was held.
func (r *MergeReport) HeldGroups() []string {
	if r == nil || len(r.Held) == 0 {
		return nil
	}
	return append([]string(nil), r.Held...)
}

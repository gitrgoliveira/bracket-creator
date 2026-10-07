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
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
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

	boutGroupPrefix = "bout:"
)

// ScalarGroups lists the scalar groups in their fixed order.
var ScalarGroups = []string{GroupPoints, GroupResult, GroupEncho, GroupFlags, GroupRep}

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

// ValidGroup reports whether group names a scalar group or a bout row. The
// score handler refuses a `changed` entry that is neither.
func ValidGroup(group string) bool {
	for _, g := range ScalarGroups {
		if g == group {
			return true
		}
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
	out := make([]SubMatchResult, 0, len(subs)+1)
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
	default:
		if pos, ok := ParseBoutGroup(group); ok {
			dst.SubResults = setSubAt(dst.SubResults, pos, subAt(src.SubResults, pos))
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
	}
	if pos, ok := ParseBoutGroup(group); ok {
		row := subAt(m.SubResults, pos)
		if row == nil {
			return (*SubMatchResult)(nil)
		}
		c := CloneSubResults([]SubMatchResult{*row})[0]
		c.IpponsA, c.IpponsB = nonNilStrings(c.IpponsA), nonNilStrings(c.IpponsB)
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
	return stamps[group]
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
		return CloneGroupStamps(stamps)
	}
	out := map[string]int64{}
	if modifiedAt <= 0 {
		return out
	}
	for _, g := range ScalarGroups {
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
// displaceNewerScoring: a finish that arrives after a newer but INVALID
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
	// default (every group the payload carries) and R3's rule that a running
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
	// revision of this board"), or the groups that would have left the match
	// without a winner it needs (NeedsWinner). Recorded in the history entry.
	HoldReason string
	// NeedsWinner reports that the held groups were held because applying
	// them would leave the match with no winner it must have (R4, operator
	// ruling 2026-10-04: a finished knockout match left tied, or an engi
	// match left with no valid flag count). The answer carries it as
	// heldReason "needs_winner", so the operator is told to correct the
	// result with a winner.
	NeedsWinner bool
	// DefaultWinStands reports that the held groups were held because a
	// default win (any of kiken, kiken-injury, fusenpai, or a match-level
	// fusensho awarded for a bar recorded on a DIFFERENT match) already
	// closed the match: a board still scoring it, points or overtime alike,
	// says nothing that overrides that ruling, and a scoreline (or an (E)
	// mark) cannot land beside the default win's circles without one
	// discarding the other, so the default win stands and the scoring is
	// kept in the history. Unlike NeedsWinner, the
	// match already has the winner it needs; the answer carries it as
	// heldReason "default_win_stands", so the operator is told to use Remove
	// default win rather than to correct the result with a winner.
	DefaultWinStands bool
	// StandingDecision is the decision code (e.g. "fusensho",
	// "kiken-voluntary") that closed the match when DefaultWinStands is set;
	// "" otherwise. The HTTP layer reads it through HeldDecision and surfaces
	// it as heldDecision beside heldReason "default_win_stands" so the
	// client can name the decision itself rather than the eliminated
	// "default win" umbrella term.
	StandingDecision string
	// Displaced are STORED changes this write moved to the history (S2 with
	// R4): a finish that arrives after newer scoring which would leave the
	// match without the winner it needs is applied on its own scoreline, as
	// in stamp order, and the newer scoring is kept in the history as held,
	// one entry per stamp. The answer names their groups (displacedGroups).
	Displaced []DisplacedChange
	// ClearedWithdrawal is the result group a later scoring change cleared
	// (R2: points scored means the withdrawal was a mistake), kept for the
	// history; nil when none was.
	ClearedWithdrawal json.RawMessage
	// ResultChanged reports whether the stored verdict moved: the result
	// group applied, a withdrawal was cleared, or the winner was worked out
	// again. A write that leaves it unmoved has no eligibility consequence.
	ResultChanged bool
}

// Superseded reports whether nothing of the write applied: a group it
// changes is held, and every other one it names only echoes the stored value.
// Only then is it answered applied:false, and nothing is written but its
// history entry.
func (r *MergeReport) Superseded() bool {
	return r != nil && len(r.Held)+len(r.HeldEcho) > 0 && len(r.Applied) == len(r.Unchanged)
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

// HeldReasonNeedsWinner is the wire code (heldReason) of groups held because
// applying them would leave the match without the winner it needs
// (MergeReport.NeedsWinner).
const HeldReasonNeedsWinner = "needs_winner"

// HeldReasonDefaultWinStands is the wire code (heldReason) of groups held
// because a default win -- a withdrawal of this match (kiken, kiken-injury,
// fusenpai) or a match-level fusensho awarded for a bar recorded on ANOTHER
// match -- already closed the match (MergeReport.DefaultWinStands). Unlike
// HeldReasonNeedsWinner, the match already has a winner, so the operator is
// not told to correct it with one; the remedy is the editor's own Remove
// default win.
const HeldReasonDefaultWinStands = "default_win_stands"

// HeldReason is the code a response carries beside heldGroups, "" when the
// held groups were held by their stamps alone.
func (r *MergeReport) HeldReason() string {
	if r == nil || len(r.Held)+len(r.Displaced) == 0 {
		return ""
	}
	if r.NeedsWinner {
		return HeldReasonNeedsWinner
	}
	if r.DefaultWinStands {
		return HeldReasonDefaultWinStands
	}
	return ""
}

// HeldDecision is the decision code (StandingDecision) a response carries
// as heldDecision beside heldReason "default_win_stands", "" when the held
// groups were not held for that reason.
func (r *MergeReport) HeldDecision() string {
	if r == nil || r.HeldReason() != HeldReasonDefaultWinStands {
		return ""
	}
	return r.StandingDecision
}

// HeldGroups is the list a response carries, nil when nothing was held.
func (r *MergeReport) HeldGroups() []string {
	if r == nil || len(r.Held) == 0 {
		return nil
	}
	return append([]string(nil), r.Held...)
}

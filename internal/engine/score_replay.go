// score_replay.go owns the replay of a match's score event log (bc-evts,
// operator ruling 2026-10-12: "timing should be per tap/event and not per
// snapshot"). ReplayScoring is pure: it folds the log's lines, in (Stamp, ID)
// order with a baseline after the events at its stamp, onto the match, and
// gives every event its outcome. Outcomes are recomputed at every replay from
// the whole log and never stored, so the order lines ARRIVED in changes
// nothing but which lines a writer's basis had seen.
//
// The rules after a result (R2, R3, R4 in .claude/rules/match-writes.md) have
// their event form here:
//   - R3: an event made after a finish applies to the finished match, which
//     stays finished; deriveWinnerAfterMerge works its winner out again and
//     the result takes the event's stamp when the verdict moved.
//   - R4: an event that would leave a finished knockout match tied, or an
//     engi match without a valid count, waits (pending) for a later event
//     that gives the match its winner; then both apply. None comes: the
//     waiting events are needsWinner and the match keeps its finish. That is
//     what makes {tie, untie, finish} reach one match in every arrival order.
//   - R2: an event after a kiken or fusenpai of this match clears it only
//     together with the result it needs (a pool tie is a draw, as the merge
//     has it); an overtime change alone never clears it; a fusensho stands
//     whatever is scored (defaultWinStands).
//   - S3: a hantei mark stays while the scoreline it rests on is tied, and
//     goes once a point unties it.
//
// Lazy migration: a match with no log is the match as stored. A door writing
// the first line of a log writes MigrationBaseline first, so the stored match
// becomes the log's first baseline at its own group stamps.
package engine

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ReplayShape is what the replay needs to know about the match beyond its
// log: the competition (engi, kachinuki, team; nil reads as individual
// kendo) and whether the match is a knockout one (R4: it cannot end tied).
type ReplayShape struct {
	Comp     *state.Competition
	Knockout bool
}

// EventOutcome is what the replay made of one event line: Line is its index
// in the log, Outcome one of the domain.ScoreOutcome* values.
type EventOutcome struct {
	ID      string `json:"id"`
	Line    int    `json:"line"`
	Outcome string `json:"outcome"`
}

// String is for test failure messages.
func (o EventOutcome) String() string {
	return fmt.Sprintf("%s@%d=%s", o.ID, o.Line, o.Outcome)
}

// Replay is the match a log folds into, with each event's outcome in log
// order. ClearedWithdrawal is set when events cleared a kiken or fusenpai of
// this match (R2): the door's derived write restores the competitor's
// eligibility then.
type Replay struct {
	Match             state.MatchResult
	Outcomes          []EventOutcome
	ClearedWithdrawal bool
}

// Outcome is the outcome of the event line with id, "" when there is none.
func (r Replay) Outcome(id string) string {
	for _, o := range r.Outcomes {
		if o.ID == id {
			return o.Outcome
		}
	}
	return ""
}

// MigrationDoor names the baseline a log starts with (MigrationBaseline).
const MigrationDoor = "migrate"

// BaselineFromMatch builds the baseline a write leaves: groups of m at their
// values (state.GroupValue, so a missing bout is its tombstone), each stamped
// with m's own stamp for it, else stamp.
func BaselineFromMatch(m *state.MatchResult, id, door string, stamp int64, groups []string, basis state.ScoreBasis) state.ScoreBaseline {
	b := state.ScoreBaseline{ID: id, Door: door, Stamp: stamp, Basis: basis,
		Values: map[string]json.RawMessage{}, Stamps: map[string]int64{}}
	for _, g := range groups {
		b.Values[g] = state.GroupValue(m, g)
		if s := m.GroupStamps[g]; s > 0 {
			b.Stamps[g] = s
		} else if stamp > 0 {
			b.Stamps[g] = stamp
		}
	}
	return b
}

// MigrationBaseline is the stored match as the first line of its log (lazy
// migration): every group at the stamp it already holds
// (state.MaterializedGroupStamps), a bout whose row is gone but whose stamp
// stays as its tombstone, ordered at the match's ModifiedAt. It has seen
// nothing of the log, which is empty when it is written.
func MigrationBaseline(current *state.MatchResult, id string, receivedAt int64) state.ScoreBaseline {
	positions := state.SubPositions(current.SubResults)
	stamps := state.MaterializedGroupStamps(current.GroupStamps, current.ModifiedAt, positions)
	groups := append(slices.Clone(state.ScalarGroups), state.ServerGroups...)
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g] = true
	}
	for _, p := range positions {
		if g := state.BoutGroup(p); !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	for g := range stamps {
		if _, ok := state.ParseBoutGroup(g); ok && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	b := state.ScoreBaseline{ID: id, Door: MigrationDoor, Stamp: current.ModifiedAt, ReceivedAt: receivedAt,
		Values: map[string]json.RawMessage{}, Stamps: map[string]int64{}}
	for _, g := range groups {
		b.Values[g] = state.GroupValue(current, g)
		if s := stamps[g]; s > 0 {
			b.Stamps[g] = s
		}
	}
	return b
}

// ScoreLogGaps reports, per session, the event sequence numbers missing from
// a match's log: Seq counts one session's events on one match from 1, so a
// session holding 1, 2, 3 and 5 is missing 4. Events with no Seq are not
// counted. The replay never reads Seq; the door logs a gap and tells the
// tapping device.
func ScoreLogGaps(lines []state.ScoreLogLine) map[string][]int64 {
	seqs := map[string]map[int64]bool{}
	for _, l := range lines {
		e := l.Event
		if e == nil || e.Seq <= 0 {
			continue
		}
		if seqs[e.Session] == nil {
			seqs[e.Session] = map[int64]bool{}
		}
		seqs[e.Session][e.Seq] = true
	}
	out := map[string][]int64{}
	for session, have := range seqs {
		var top int64
		for s := range have {
			top = max(top, s)
		}
		for s := int64(1); s < top; s++ {
			if !have[s] {
				out[session] = append(out[session], s)
			}
		}
	}
	return out
}

// replayItem is one line placed in the replay's order.
type replayItem struct {
	line     int
	stamp    int64
	baseline bool
	id       string
}

// compareReplayItems is THE replay order: stamp, then events before
// baselines at an equal stamp, then id, then line (only identical ids tie).
func compareReplayItems(a, b replayItem) int {
	if c := cmp.Compare(a.stamp, b.stamp); c != 0 {
		return c
	}
	if a.baseline != b.baseline {
		if a.baseline {
			return 1
		}
		return -1
	}
	if c := cmp.Compare(a.id, b.id); c != 0 {
		return c
	}
	return cmp.Compare(a.line, b.line)
}

// replayMatch is the match being folded plus each bout's fold state (the
// fusensho snapshot a toggle-off restores, which the stored row cannot hold).
type replayMatch struct {
	m     state.MatchResult
	bouts map[int]domain.BoutScore
}

func (r *replayMatch) clone() *replayMatch {
	c := &replayMatch{m: cloneMatchForReplay(r.m), bouts: make(map[int]domain.BoutScore, len(r.bouts))}
	for k, v := range r.bouts {
		c.bouts[k] = v.Clone()
	}
	return c
}

func cloneMatchForReplay(m state.MatchResult) state.MatchResult {
	c := m
	c.IpponsA, c.IpponsB = cloneIppons(m.IpponsA), cloneIppons(m.IpponsB)
	c.SubResults = state.CloneSubResults(m.SubResults)
	c.Encho = m.Encho.Clone()
	c.MarkIDs = m.MarkIDs.Clone()
	c.GroupStamps = state.CloneGroupStamps(m.GroupStamps)
	return c
}

// replayer carries one replay's state.
type replayer struct {
	lines   []state.ScoreLogLine
	shape   ReplayShape
	cur     *replayMatch
	stamps  map[string]int64
	govern  map[string]int   // group -> line of the baseline that last set it
	applied map[string][]int // group -> event lines folded into it since
	pending []int            // event lines waiting for the result they need (R4/R2)
	clears  bool             // the pending events would clear a withdrawal
	outcome map[int]string
	cleared bool
}

// ReplayScoring folds a match's event log onto it. current supplies what no
// group holds (sides, ids, court, schedule, round) and, while the log is
// empty, everything: a match with no log is the match as stored.
func ReplayScoring(current *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape) Replay {
	if len(lines) == 0 {
		return Replay{Match: cloneMatchForReplay(*current)}
	}
	rp := &replayer{
		lines: lines, shape: shape,
		cur:     &replayMatch{m: cloneMatchForReplay(*current), bouts: map[int]domain.BoutScore{}},
		stamps:  state.CloneGroupStamps(current.GroupStamps),
		govern:  map[string]int{},
		applied: map[string][]int{},
		outcome: map[int]string{},
	}
	if rp.stamps == nil {
		rp.stamps = map[string]int64{}
	}
	items := make([]replayItem, 0, len(lines))
	firstLine := map[string]int{}
	for i, l := range lines {
		items = append(items, replayItem{line: i, stamp: l.Stamp(), baseline: l.Baseline != nil, id: l.ID()})
		if l.Event != nil {
			if _, ok := firstLine[l.Event.ID]; !ok {
				firstLine[l.Event.ID] = i
			}
		}
	}
	slices.SortFunc(items, compareReplayItems)
	for _, it := range items {
		l := lines[it.line]
		switch {
		case l.Baseline != nil:
			rp.applyBaseline(it.line, l.Baseline)
		case firstLine[l.Event.ID] != it.line:
			rp.outcome[it.line] = domain.ScoreOutcomeDuplicate
		default:
			rp.applyEvent(it.line, l.Event)
		}
	}
	rp.settlePending()

	out := Replay{Match: rp.cur.m, ClearedWithdrawal: rp.cleared}
	out.Match.GroupStamps = rp.stamps
	if len(rp.stamps) == 0 {
		out.Match.GroupStamps = nil
	}
	for _, s := range rp.stamps {
		out.Match.ModifiedAt = max(out.Match.ModifiedAt, s)
	}
	out.Match.ScoreVersion = len(lines)
	for i, l := range lines {
		if l.Event != nil {
			out.Outcomes = append(out.Outcomes, EventOutcome{ID: l.Event.ID, Line: i, Outcome: rp.outcome[i]})
		}
	}
	return out
}

// baselineGroupOrder applies a baseline's groups so a bout row exists before
// its picks and recorded mark land on it.
func baselineGroupOrder(values map[string]json.RawMessage) []string {
	groups := make([]string, 0, len(values))
	for g := range values {
		groups = append(groups, g)
	}
	rank := func(g string) int {
		switch g {
		case state.GroupRecorded:
			return 3
		case state.GroupRepPickA, state.GroupRepPickB:
			return 2
		}
		if _, ok := state.ParseBoutGroup(g); ok {
			return 1
		}
		return 0
	}
	sort.Slice(groups, func(i, j int) bool {
		if ri, rj := rank(groups[i]), rank(groups[j]); ri != rj {
			return ri < rj
		}
		return groups[i] < groups[j]
	})
	return groups
}

// contains says whether baseline b was built with event line i in view.
func (rp *replayer) contains(b *state.ScoreBaseline, i int) bool {
	return BasisContains(b, i, rp.lines[i].Event)
}

// BasisContains is the one containment rule: a baseline was built with the
// event at log line i in view when the line is among the first Basis.Seen,
// its id is in Basis.Own, or it is one of the baseline's own session's events
// up to Basis.Seq.
func BasisContains(b *state.ScoreBaseline, i int, e *domain.ScoreEvent) bool {
	if i < b.Basis.Seen || slices.Contains(b.Basis.Own, e.ID) {
		return true
	}
	return b.Session != "" && e.Session == b.Session && b.Basis.Seq > 0 && e.Seq > 0 && e.Seq <= b.Basis.Seq
}

func (rp *replayer) applyBaseline(line int, b *state.ScoreBaseline) {
	m := &rp.cur.m
	for _, g := range baselineGroupOrder(b.Values) {
		pos, isBout := state.ParseBoutGroup(g)
		var prevA, prevB, prevMA, prevMB []string
		switch {
		case g == state.GroupPoints:
			prevA, prevB = m.IpponsA, m.IpponsB
			if m.MarkIDs != nil {
				prevMA, prevMB = m.MarkIDs.A, m.MarkIDs.B
			}
		case isBout:
			if row := state.SubResultAt(m.SubResults, pos); row.Position == pos {
				prevA, prevB = row.IpponsA, row.IpponsB
				if row.MarkIDs != nil {
					prevMA, prevMB = row.MarkIDs.A, row.MarkIDs.B
				}
			}
		}
		if err := state.SetGroupFromValue(m, g, b.Values[g]); err != nil {
			// A line is validated before it is logged; a value that still
			// does not read leaves the group as it was.
			continue
		}
		switch {
		case g == state.GroupPoints:
			m.MarkIDs = markIDsOf(m.IpponsA, m.IpponsB,
				assignMarks(prevA, prevMA, m.IpponsA, b.ID, 0, domain.ScoreSideA),
				assignMarks(prevB, prevMB, m.IpponsB, b.ID, 0, domain.ScoreSideB))
		case isBout:
			delete(rp.cur.bouts, pos)
			for i := range m.SubResults {
				if row := &m.SubResults[i]; row.Position == pos {
					row.MarkIDs = markIDsOf(row.IpponsA, row.IpponsB,
						assignMarks(prevA, prevMA, row.IpponsA, b.ID, pos, domain.ScoreSideA),
						assignMarks(prevB, prevMB, row.IpponsB, b.ID, pos, domain.ScoreSideB))
				}
			}
		}
		if s := cmp.Or(b.Stamps[g], b.Stamp); s > 0 {
			rp.stamps[g] = s
		} else {
			delete(rp.stamps, g)
		}
		// Every event folded into the group before this baseline is now
		// either in its values (recorded) or replaced by them (D5).
		for _, i := range rp.applied[g] {
			rp.absorb(b, i)
		}
		rp.applied[g] = nil
		rp.govern[g] = line
	}
	if len(rp.pending) == 0 {
		return
	}
	// A pending event whose group this baseline sets is settled by it; a new
	// result settles the rest, which never applied.
	_, setsResult := b.Values[state.GroupResult]
	var keep []int
	for _, i := range rp.pending {
		_, sets := b.Values[rp.groupOf(rp.lines[i].Event)]
		switch {
		case sets:
			rp.absorb(b, i)
		case setsResult:
			rp.outcome[i] = rp.pendingOutcome()
		default:
			keep = append(keep, i)
		}
	}
	rp.pending = keep
	if len(keep) == 0 {
		rp.clears = false
	}
}

func (rp *replayer) absorb(b *state.ScoreBaseline, i int) {
	if rp.contains(b, i) {
		rp.outcome[i] = domain.ScoreOutcomeRecorded
	} else {
		rp.outcome[i] = domain.ScoreOutcomeBeforeBaseline
	}
}

func (rp *replayer) pendingOutcome() string {
	if rp.clears {
		return domain.ScoreOutcomeDefaultWinStands
	}
	return domain.ScoreOutcomeNeedsWinner
}

func (rp *replayer) settlePending() {
	for _, i := range rp.pending {
		rp.outcome[i] = rp.pendingOutcome()
	}
	rp.pending, rp.clears = nil, false
}

// groupOf is the one group an event changes.
func (rp *replayer) groupOf(e *domain.ScoreEvent) string {
	return ScoreEventGroup(e)
}

// ScoreEventGroup is the one group (state/match_groups.go) an event changes.
func ScoreEventGroup(e *domain.ScoreEvent) string {
	switch e.Kind {
	case domain.ScoreRepPick:
		if e.Side == domain.ScoreSideB {
			return state.GroupRepPickB
		}
		return state.GroupRepPickA
	case domain.ScorePoolRep:
		return state.GroupRep
	case domain.ScoreFlagAdd, domain.ScoreFlagRemove:
		return state.GroupFlags
	}
	if e.Pos != 0 {
		return state.BoutGroup(e.Pos)
	}
	if e.Kind == domain.ScoreEnchoAdd || e.Kind == domain.ScoreEnchoRemove {
		return state.GroupEncho
	}
	return state.GroupPoints
}

func (rp *replayer) applyEvent(line int, e *domain.ScoreEvent) {
	g := rp.groupOf(e)
	if b, ok := rp.govern[g]; ok && rp.contains(rp.lines[b].Baseline, line) {
		rp.outcome[line] = domain.ScoreOutcomeRecorded
		return
	}
	m := &rp.cur.m
	if m.Status != state.MatchStatusCompleted {
		out := foldEvent(rp.cur, e, rp.shape, rp.stamps)
		rp.outcome[line] = out
		if out == domain.ScoreOutcomeApplied {
			rp.stamps[g] = max(rp.stamps[g], e.Stamp)
			rp.applied[g] = append(rp.applied[g], line)
		}
		return
	}
	// A finished match.
	if domain.IsDefaultWinDecisionStr(m.Decision) && !domain.IsWithdrawalDecisionStr(m.Decision) {
		rp.outcome[line] = domain.ScoreOutcomeDefaultWinStands
		return
	}
	rp.clears = domain.IsWithdrawalDecisionStr(m.Decision)
	probe := rp.probeWithPending()
	if out := foldEvent(probe, e, rp.shape, map[string]int64{}); out != domain.ScoreOutcomeApplied {
		rp.outcome[line] = out
		return
	}
	rp.pending = append(rp.pending, line)
	rp.tryCommit()
}

// probeWithPending is the committed match with the pending events folded
// in, the withdrawal's circles taken off when they would clear it.
func (rp *replayer) probeWithPending() *replayMatch {
	probe := rp.cur.clone()
	if rp.clears {
		stripMaru(&probe.m)
	}
	for _, i := range rp.pending {
		foldEvent(probe, rp.lines[i].Event, rp.shape, map[string]int64{})
	}
	return probe
}

// tryCommit applies the pending events together when they give the finished
// match the result it needs.
func (rp *replayer) tryCommit() {
	probe := rp.probeWithPending()
	scoring := false
	for _, i := range rp.pending {
		if k := rp.lines[i].Event.Kind; k != domain.ScoreEnchoAdd && k != domain.ScoreEnchoRemove {
			scoring = true
		}
	}
	if rp.clears {
		// An overtime change alone says nothing about the withdrawal.
		if !scoring {
			return
		}
		probe.m.Decision, probe.m.DecisionBy, probe.m.DecisionReason = "", "", ""
		probe.m.Winner, probe.m.WinnerID, probe.m.WinnerSide = "", "", ""
	}
	before := state.GroupValue(&rp.cur.m, state.GroupResult)
	if !deriveWinnerAfterMerge(&probe.m, mergeCtx{comp: rp.shape.Comp, knockout: rp.shape.Knockout}) {
		return
	}
	probe.m.WinnerSide = ""
	stripUntiedHantei(&probe.m)
	rp.cur = probe
	last := rp.lines[rp.pending[len(rp.pending)-1]].Event.Stamp
	for _, i := range rp.pending {
		e := rp.lines[i].Event
		g := rp.groupOf(e)
		rp.outcome[i] = domain.ScoreOutcomeApplied
		rp.stamps[g] = max(rp.stamps[g], e.Stamp)
		rp.applied[g] = append(rp.applied[g], i)
	}
	if string(before) != string(state.GroupValue(&rp.cur.m, state.GroupResult)) {
		// The verdict moved: the result is dated by the change that moved it.
		rp.stamps[state.GroupResult] = max(rp.stamps[state.GroupResult], last)
	}
	if rp.clears {
		rp.cleared = true
	}
	rp.pending, rp.clears = nil, false
}

// stripMaru takes a kiken's or fusenpai's circles off the match's scoreline (they go
// with the withdrawal R2 clears), keeping each kept mark's id.
func stripMaru(m *state.MatchResult) {
	var ma, mb []string
	if m.MarkIDs != nil {
		ma, mb = m.MarkIDs.A, m.MarkIDs.B
	}
	notMaru := func(v string) bool { return v != domain.DefaultWinIppon }
	a, am := keepEntries(m.IpponsA, ma, notMaru)
	b, bm := keepEntries(m.IpponsB, mb, notMaru)
	m.IpponsA, m.IpponsB = a, b
	m.MarkIDs = markIDsOf(a, b, am, bm)
}

// stripUntiedHantei is S3 on a recomputed finish: the hantei mark stays while
// the scoreline it rests on is tied (domain.HanteiTiedScoreline) and on the
// winner's side; otherwise it goes, with its mark id. stripInvalidHantei's
// rule, keeping the mark ids beside the marks.
func stripUntiedHantei(m *state.MatchResult) {
	if !m.HanteiDecided() {
		return
	}
	tied := domain.HanteiTiedScoreline(m.IpponsA, m.IpponsB)
	side := domain.AttributeWinnerSide(domain.WinnerAttribution{
		WinnerID: m.WinnerID, SideAID: m.SideAID, SideBID: m.SideBID,
		Winner: m.Winner, SideA: m.SideA, SideB: m.SideB,
	})
	var ma, mb []string
	if m.MarkIDs != nil {
		ma, mb = m.MarkIDs.A, m.MarkIDs.B
	}
	notHt := func(v string) bool { return v != domain.HanteiMark }
	all := func(string) bool { return true }
	keepA, keepB := all, all
	if !tied || side != domain.MatchSideA {
		keepA = notHt
	}
	if !tied || side != domain.MatchSideB {
		keepB = notHt
	}
	a, am := keepEntries(m.IpponsA, ma, keepA)
	b, bm := keepEntries(m.IpponsB, mb, keepB)
	m.IpponsA, m.IpponsB = a, b
	m.MarkIDs = markIDsOf(a, b, am, bm)
}

func keepEntries(ippons, marks []string, keep func(string) bool) ([]string, []string) {
	var outI, outM []string
	for i, v := range ippons {
		if keep(v) {
			outI = append(outI, v)
			mark := ""
			if i < len(marks) {
				mark = marks[i]
			}
			outM = append(outM, mark)
		}
	}
	return outI, outM
}

// assignMarks names the marks a baseline brings: a mark equal to the one at
// the same place before keeps its id (a finish echoing the board's points
// keeps the ids the board removes by), any other is b:<line>:<pos>:<side>:<i>.
func assignMarks(prev, prevMarks, next []string, lineID string, pos int, side string) []string {
	out := make([]string, len(next))
	for i, v := range next {
		switch {
		case v == "" || v == domain.IpponPlaceholder:
		case i < len(prev) && prev[i] == v && i < len(prevMarks) && prevMarks[i] != "":
			out[i] = prevMarks[i]
		default:
			out[i] = "b:" + lineID + ":" + strconv.Itoa(pos) + ":" + side + ":" + strconv.Itoa(i)
		}
	}
	return out
}

// markIDsOf is the stored form: nil when neither side holds a mark.
func markIDsOf(a, b, ma, mb []string) *state.MarkIDs {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	ids := &state.MarkIDs{}
	if len(a) > 0 {
		ids.A = alignTo(a, ma)
	}
	if len(b) > 0 {
		ids.B = alignTo(b, mb)
	}
	return ids
}

func alignTo(ippons, marks []string) []string {
	out := make([]string, len(ippons))
	copy(out, marks)
	return out
}

// --- the fold of one event ------------------------------------------------

func (shape ReplayShape) team() bool {
	return shape.Comp != nil && shape.Comp.IsTeam()
}

func (shape ReplayShape) kachinuki() bool {
	return shape.Comp != nil && shape.Comp.IsKachinuki()
}

func (shape ReplayShape) engi() bool {
	return shape.Comp != nil && shape.Comp.Engi
}

func scoreMatchSide(s string) domain.MatchSide {
	if s == domain.ScoreSideB {
		return domain.MatchSideB
	}
	return domain.MatchSideA
}

// foldEvent folds one event into r and answers its outcome. stamps receives
// the recorded group's stamp when the event takes a kachinuki bout's
// recorded mark off.
func foldEvent(r *replayMatch, e *domain.ScoreEvent, shape ReplayShape, stamps map[string]int64) string {
	m := &r.m
	switch e.Kind {
	case domain.ScoreFlagAdd, domain.ScoreFlagRemove:
		if !shape.engi() {
			return domain.ScoreOutcomeNotAllowed
		}
		delta := 1
		if e.Kind == domain.ScoreFlagRemove {
			delta = -1
		}
		f := &m.FlagsA
		if e.Side == domain.ScoreSideB {
			f = &m.FlagsB
		}
		next, out := domain.AdjustFlags(*f, delta)
		*f = next
		return out
	case domain.ScorePoolRep:
		if e.Side == domain.ScoreSideB {
			m.RepPlayerB = e.Name
		} else {
			m.RepPlayerA = e.Name
		}
		return domain.ScoreOutcomeApplied
	case domain.ScoreRepPick:
		row := state.SubResultAt(m.SubResults, state.DaihyosenSubPosition)
		if row.Position != state.DaihyosenSubPosition || e.Pos != state.DaihyosenSubPosition {
			return domain.ScoreOutcomeNoTarget
		}
		m.SetRepPick(scoreMatchSide(e.Side), e.MemberID)
		for i := range m.SubResults {
			if m.SubResults[i].Position == state.DaihyosenSubPosition {
				m.SubResults[i].ReconcileWinnerMemberID()
			}
		}
		return domain.ScoreOutcomeApplied
	}
	if shape.engi() {
		return domain.ScoreOutcomeNotAllowed
	}
	if e.Pos == 0 {
		return foldMatchLevel(m, e, shape)
	}
	if !shape.team() {
		return domain.ScoreOutcomeNotAllowed
	}
	return foldBout(r, e, shape, stamps)
}

// foldMatchLevel folds an event on the match itself: an individual match's
// points, fouls and overtime, or a team encounter's overtime.
func foldMatchLevel(m *state.MatchResult, e *domain.ScoreEvent, shape ReplayShape) string {
	singleBout := state.IsPoolDaihyosenMatchID(m.ID) || state.IsTiebreakerMatchID(m.ID)
	if e.Kind == domain.ScoreEnchoAdd || e.Kind == domain.ScoreEnchoRemove {
		if shape.kachinuki() && !singleBout {
			// A kachinuki encounter never carries a match-level overtime
			// (state.Competition.ClearKachinukiEncounterEncho).
			return domain.ScoreOutcomeNotAllowed
		}
		count := 0
		if m.Encho.On() {
			count = m.Encho.PeriodCount
		}
		b := domain.BoutScore{Encho: count}
		out := b.AdjustEncho(enchoDelta(e))
		m.Encho = enchoOf(b.Encho)
		return out
	}
	if e.Kind == domain.ScoreBoutDecision || e.Kind == domain.ScoreBoutFighter || e.Kind == domain.ScoreDaihyosenVerdict {
		return domain.ScoreOutcomeNotAllowed
	}
	if shape.team() && !singleBout {
		return domain.ScoreOutcomeNotAllowed
	}
	b := domain.BoutScore{IpponsA: m.IpponsA, IpponsB: m.IpponsB, FoulsA: m.HansokuA, FoulsB: m.HansokuB}
	if m.MarkIDs != nil {
		b.MarksA, b.MarksB = m.MarkIDs.A, m.MarkIDs.B
	}
	b = b.Clone()
	out := foldScoring(&b, e)
	if out != domain.ScoreOutcomeApplied {
		return out
	}
	m.IpponsA, m.IpponsB, m.HansokuA, m.HansokuB = b.IpponsA, b.IpponsB, b.FoulsA, b.FoulsB
	m.MarkIDs = markIDsOf(b.IpponsA, b.IpponsB, b.MarksA, b.MarksB)
	return out
}

func enchoDelta(e *domain.ScoreEvent) int {
	if e.Kind == domain.ScoreEnchoRemove {
		return -1
	}
	return 1
}

func enchoOf(count int) *state.EnchoMetadata {
	if count <= 0 {
		return nil
	}
	return &state.EnchoMetadata{PeriodCount: count}
}

// foldScoring folds an ippon, foul or overtime event into a bout score.
func foldScoring(b *domain.BoutScore, e *domain.ScoreEvent) string {
	switch e.Kind {
	case domain.ScoreIpponAdd:
		return b.AddIppon(e.Side, e.Letter, e.ID)
	case domain.ScoreIpponRemove:
		return b.RemoveMark(e.Target)
	case domain.ScoreFoulAdd:
		return b.AddFoul(e.Side, e.ID)
	case domain.ScoreFoulRemove:
		return b.RemoveFoul(e.Side)
	case domain.ScoreEnchoAdd, domain.ScoreEnchoRemove:
		return b.AdjustEncho(enchoDelta(e))
	}
	return domain.ScoreOutcomeNotAllowed
}

// boutScoreOf reads a team bout row as the fold sees it.
func boutScoreOf(row state.SubMatchResult) domain.BoutScore {
	b := domain.BoutScore{
		IpponsA: row.IpponsA, IpponsB: row.IpponsB, FoulsA: row.HansokuA, FoulsB: row.HansokuB,
		Draw: state.IsDraw(row.Decision),
	}
	if row.Encho.On() {
		b.Encho = row.Encho.PeriodCount
	}
	if row.MarkIDs != nil {
		b.MarksA, b.MarksB = row.MarkIDs.A, row.MarkIDs.B
	}
	if row.Decision == string(domain.DecisionFusensho) {
		switch {
		case slices.Contains(row.IpponsA, domain.DefaultWinIppon):
			b.Fusensho = domain.ScoreSideA
		case slices.Contains(row.IpponsB, domain.DefaultWinIppon):
			b.Fusensho = domain.ScoreSideB
		}
	}
	return b.Clone()
}

// foldBout folds an event on a team bout row (pos n, or -1 for the
// representative bout). Kachinuki events land only on rows that exist: a
// pairing Record bout has not appended, or one removed, is noTarget.
func foldBout(r *replayMatch, e *domain.ScoreEvent, shape ReplayShape, stamps map[string]int64) string {
	m := &r.m
	idx := -1
	for i := range m.SubResults {
		if m.SubResults[i].Position == e.Pos {
			idx = i
			break
		}
	}
	if idx < 0 {
		return domain.ScoreOutcomeNoTarget
	}
	row := &m.SubResults[idx]
	before := m.SubResults[idx]
	b, ok := r.bouts[e.Pos]
	if !ok {
		b = boutScoreOf(*row)
	}
	b = b.Clone()
	var out string
	notHt := func(v string) bool { return v != domain.HanteiMark }
	switch e.Kind {
	case domain.ScoreBoutDecision:
		switch e.Value {
		case domain.BoutDecisionHikiwake:
			out = b.SetDraw()
		case domain.BoutDecisionFusensho:
			if e.Pos == state.DaihyosenSubPosition {
				return domain.ScoreOutcomeNotAllowed
			}
			out = b.SetFusensho(e.Side, e.ID)
		default:
			out = b.ClearBoutDecision()
		}
	case domain.ScoreBoutFighter:
		if !shape.kachinuki() || e.Pos <= 0 || e.Pos == lowestNumberedPosition(m.SubResults) {
			// The first bout's fighters are the lineup's (a lineup save).
			return domain.ScoreOutcomeNotAllowed
		}
		if e.Side == domain.ScoreSideB {
			row.SideB, row.SideBMemberID = e.Name, e.MemberID
		} else {
			row.SideA, row.SideAMemberID = e.Name, e.MemberID
		}
		out = domain.ScoreOutcomeApplied
	case domain.ScoreDaihyosenVerdict:
		if e.Value != domain.BoutDecisionClear && !domain.HanteiTiedScoreline(b.IpponsA, b.IpponsB) {
			return domain.ScoreOutcomeNotAllowed
		}
		b.IpponsA, b.MarksA = keepEntries(b.IpponsA, b.MarksA, notHt)
		b.IpponsB, b.MarksB = keepEntries(b.IpponsB, b.MarksB, notHt)
		switch {
		case e.Value == domain.BoutDecisionClear:
		case e.Side == domain.ScoreSideB:
			b.IpponsB, b.MarksB = appendHanteiMark(b.IpponsB, b.MarksB, e.ID)
		default:
			b.IpponsA, b.MarksA = appendHanteiMark(b.IpponsA, b.MarksA, e.ID)
		}
		out = domain.ScoreOutcomeApplied
	default:
		out = foldScoring(&b, e)
	}
	if out != domain.ScoreOutcomeApplied {
		return out
	}
	r.bouts[e.Pos] = b
	writeBoutRow(row, b, m, shape)
	if row.Recorded && (enchoPeriodsRise(before.Encho, row.Encho) || boutResultChanged(before, *row, m.SideA, m.SideB)) {
		// The bout is being fought again: it is no longer recorded
		// (mergeHold.unrecordReopenedBouts' rule).
		row.Recorded = false
		stamps[state.GroupRecorded] = max(stamps[state.GroupRecorded], e.Stamp)
	}
	return out
}

// appendHanteiMark is domain.AppendHantei keeping the mark id beside it.
func appendHanteiMark(ippons, marks []string, id string) ([]string, []string) {
	marks = alignTo(ippons, marks)
	out := domain.AppendHantei(ippons)
	for i := range out {
		if i >= len(ippons) || ippons[i] != out[i] {
			if i < len(marks) {
				marks[i] = id
			} else {
				marks = append(marks, id)
			}
			break
		}
	}
	return out, marks
}

func lowestNumberedPosition(subs []state.SubMatchResult) int {
	low := 0
	for _, s := range subs {
		if s.Position > 0 && (low == 0 || s.Position < low) {
			low = s.Position
		}
	}
	return low
}

// writeBoutRow puts a folded bout score back on its row, with the row's
// winner and decision worked out as the team editor's buildPatch works them
// (domain.BoutWinnerSide; a kachinuki bout's winner is its fighter, else the
// team).
func writeBoutRow(row *state.SubMatchResult, b domain.BoutScore, m *state.MatchResult, shape ReplayShape) {
	row.IpponsA, row.IpponsB = b.IpponsA, b.IpponsB
	row.HansokuA, row.HansokuB = b.FoulsA, b.FoulsB
	row.Encho = enchoOf(b.Encho)
	row.MarkIDs = markIDsOf(b.IpponsA, b.IpponsB, b.MarksA, b.MarksB)
	hantei := ""
	switch {
	case domain.ContainsHantei(b.IpponsA):
		hantei = domain.ScoreSideA
	case domain.ContainsHantei(b.IpponsB):
		hantei = domain.ScoreSideB
	}
	a, bb := b.Points()
	w := domain.BoutWinnerSide(a, bb, b.Draw, b.Fusensho, hantei)
	isRep := row.Position == state.DaihyosenSubPosition
	switch {
	case isRep:
		row.Decision = string(domain.DecisionDaihyosen)
	case b.Fusensho != "":
		row.Decision = string(domain.DecisionFusensho)
	case w == "" && b.Played():
		row.Decision = state.DecisionDraw
	default:
		row.Decision = ""
	}
	team := ""
	switch w {
	case domain.ScoreSideA:
		team = m.SideA
	case domain.ScoreSideB:
		team = m.SideB
	}
	row.Winner = team
	if shape.kachinuki() && !isRep {
		switch w {
		case domain.ScoreSideA:
			row.Winner = cmp.Or(row.SideA, team)
		case domain.ScoreSideB:
			row.Winner = cmp.Or(row.SideB, team)
		}
	}
	row.WinnerMemberID = ""
	if shape.kachinuki() || isRep {
		switch w {
		case domain.ScoreSideA:
			row.WinnerMemberID = row.SideAMemberID
		case domain.ScoreSideB:
			row.WinnerMemberID = row.SideBMemberID
		}
	}
}

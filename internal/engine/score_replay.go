// score_replay.go owns the replay of a match's score event log (bc-evts,
// operator ruling 2026-10-12: "timing should be per tap/event and not per
// snapshot"). ReplayScoring is pure: it folds the log's lines, in (Stamp, ID)
// order with a baseline after the events at its stamp, onto the match, and
// gives every event its outcome. Outcomes are recomputed at every replay from
// the whole log and never stored, so the order lines ARRIVED in changes
// nothing but which lines a writer's basis had seen.
//
// THE CLOSED-MATCH RULE (operator ruling 2026-10-15, replacing R2/R3/R4/S2/S3):
// in stamp order, once a finish or a decision (kiken, fusenpai, fusensho,
// hantei, daihyosen, hikiwake) is recorded, any tap or scoring baseline
// stamped AFTER it is turned away: an event's outcome is matchClosed, a
// baseline's scoring groups are Replay.Held. Both are kept in the history and
// the device is told the match is finished and to tap Correct. What is
// stamped BEFORE the result keeps the D5 rule (recorded when the result's
// basis holds it, else beforeBaseline). The FIRST recorded result stands: a
// later finish or decision from a device that did not see it is turned away
// as scoring is (rivalResult). A correction or a reopen is the way to change a closed match,
// and the match is open again from the reopen's stamp. Because the rule is
// stamp order, the same lines give the same match in every arrival order.
//
// A baseline (a door's write: a snapshot, a finish, a decision) sets each of
// its groups at that group's own stamp (baselineItems), a fought finish's
// scoring together with its result. A decision's points are the circles of the
// side that did not withdraw and the points the withdrawer had struck BEFORE it
// (keepLoserPoints), worked out here from the lines before the decision and not
// copied from the value it carries. Since stage 2b every scoring snapshot and
// every decision is decided this way (mergeByReplay, match_merge.go).
//
// Lazy migration: a match with no log is the match as stored. A door writing
// the first line of a log writes MigrationBaseline first, so the stored match
// becomes the log's first baseline at its own group stamps.
package engine

import (
	"bytes"
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
// kendo) and whether the match is a knockout one (it cannot end tied).
type ReplayShape struct {
	Comp     *state.Competition
	Knockout bool
	// logLines, once logKnown, is how many of the replayed lines the log
	// really holds: those after it (a migration, drift or repair line, the
	// write's own baseline) were minted by the write being replayed, and no
	// basis can have seen them (rivalResult). Not known, every line counts.
	logLines int
	logKnown bool
}

// withLogLines is the shape with the log's own line count known.
func (s ReplayShape) withLogLines(n int) ReplayShape {
	s.logLines, s.logKnown = n, true
	return s
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
// order.
type Replay struct {
	Match    state.MatchResult
	Outcomes []EventOutcome
	// Held names, by line index, the baselines whose scoring the replay did
	// not apply because a result stamped before them had closed the match
	// (BaselineHold).
	Held map[int]BaselineHold
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

// derivedScore is what a stored match holds that only the replay derives
// (bc-evts): the mark ids of its scoreline and of each bout, and the event-log
// version and per-session seqs, with the ippons each set of ids names.
type derivedScore struct {
	ipponsA, ipponsB []string
	marks            *state.MarkIDs
	bouts            map[int]derivedBout
	version          int
	eventSeqs        map[string]int64
}

type derivedBout struct {
	ipponsA, ipponsB []string
	marks            *state.MarkIDs
}

func derivedScoreOf(ipponsA, ipponsB []string, marks *state.MarkIDs, subs []state.SubMatchResult, version int, eventSeqs map[string]int64) derivedScore {
	d := derivedScore{ipponsA: slices.Clone(ipponsA), ipponsB: slices.Clone(ipponsB), marks: marks.Clone(),
		bouts: make(map[int]derivedBout, len(subs)), version: version, eventSeqs: state.CloneGroupStamps(eventSeqs)}
	for _, row := range subs {
		d.bouts[row.Position] = derivedBout{ipponsA: slices.Clone(row.IpponsA), ipponsB: slices.Clone(row.IpponsB), marks: row.MarkIDs.Clone()}
	}
	return d
}

// carry settles the derived fields of a write that is not the replay's
// (DoorEvent): the mark ids, the version and the seqs are never taken from a write,
// which a client builds and may fill with anything. A scoreline (the match's,
// or a bout's) the write left as it was keeps the ids it had; one it changed
// has none until the replay names its marks again. It returns the match-level
// ids and rewrites each row's in subs; the version is d.version and the seqs
// d.eventSeqs.
func (d derivedScore) carry(ipponsA, ipponsB []string, subs []state.SubMatchResult) *state.MarkIDs {
	for i := range subs {
		row := &subs[i]
		row.MarkIDs = nil
		if b, ok := d.bouts[row.Position]; ok && slices.Equal(b.ipponsA, row.IpponsA) && slices.Equal(b.ipponsB, row.IpponsB) {
			row.MarkIDs = b.marks.Clone()
		}
	}
	if slices.Equal(d.ipponsA, ipponsA) && slices.Equal(d.ipponsB, ipponsB) {
		return d.marks.Clone()
	}
	return nil
}

// MigrationBaseline is the stored match as the first line of its log (lazy
// migration): every group at the stamp it already holds
// (state.MaterializedGroupStamps), a bout whose row is gone but whose stamp
// stays as its tombstone, ordered at the match's ModifiedAt. It has seen
// nothing of the log, which is empty when it is written.
func MigrationBaseline(current *state.MatchResult, id string, receivedAt int64) state.ScoreBaseline {
	return migrationBaselineCovering(current, id, receivedAt, nil)
}

// MigrationBaselineFor is MigrationBaseline for a match of shape: it also
// covers every bout an event may make the row of (createsMissingRow), a
// bout with no row as its "null" at stamp 0, so the replay folds such an
// event from no row rather than onto the stored match, which already holds
// what the event made (it would count twice).
func MigrationBaselineFor(current *state.MatchResult, id string, receivedAt int64, shape ReplayShape) state.ScoreBaseline {
	var extra []int
	if shape.team() {
		for p := 1; p <= max(shape.Comp.TeamSize, 1); p++ {
			if shape.createsMissingRow(p) {
				extra = append(extra, p)
			}
		}
	}
	return migrationBaselineCovering(current, id, receivedAt, extra)
}

func migrationBaselineCovering(current *state.MatchResult, id string, receivedAt int64, extra []int) state.ScoreBaseline {
	positions := state.SubPositions(current.SubResults)
	stamps := state.MaterializedGroupStamps(current.GroupStamps, current.ModifiedAt, positions)
	groups := append(slices.Clone(state.ScalarGroups), state.ServerGroups...)
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g] = true
	}
	for _, p := range append(slices.Clone(positions), extra...) {
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
		// A group the match never wrote is dated 0, not the line's stamp:
		// it replays before every change, which applies over it as the
		// merge applied a write over a group with no stamp.
		if s := stamps[g]; s > 0 || b.Stamp > 0 {
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

// ScoreLogEventSeqs is, per event session, the highest Seq of that session's
// events in a match's log (MatchResult.EventSeqs): every logged tap counts,
// whatever the replay made of it (applied, a duplicate mark, not allowed,
// turned away by a finished match), since each is held by the server and a
// board must not fold it again. Events with no session or no Seq are not
// counted; nil when none is. Past state.MaxEventSeqSessions sessions, the ones
// whose last event was logged latest are kept.
func ScoreLogEventSeqs(lines []state.ScoreLogLine) map[string]int64 {
	top := map[string]int64{}
	last := map[string]int{}
	for i, l := range lines {
		e := l.Event
		if e == nil || e.Seq <= 0 || e.Session == "" {
			continue
		}
		top[e.Session] = max(top[e.Session], e.Seq)
		last[e.Session] = i
	}
	if len(top) == 0 {
		return nil
	}
	if len(top) > state.MaxEventSeqSessions {
		sessions := make([]string, 0, len(top))
		for s := range top {
			sessions = append(sessions, s)
		}
		slices.SortFunc(sessions, func(a, b string) int { return cmp.Compare(last[b], last[a]) })
		for _, s := range sessions[state.MaxEventSeqSessions:] {
			delete(top, s)
		}
	}
	return top
}

// mergeEventSeqs settles the seqs of the replay's derived write (DoorEvent) on
// a stored match: the replay read the whole log, so its sessions are the ones
// kept, each never lower than the stored one (the seqs only grow, as the
// version does). A derived write with none keeps the stored ones.
func mergeEventSeqs(stored, derived map[string]int64) map[string]int64 {
	if len(derived) == 0 {
		return state.CloneGroupStamps(stored)
	}
	out := state.CloneGroupStamps(derived)
	for s, seq := range out {
		out[s] = max(seq, stored[s])
	}
	return out
}

// replayItem is one line placed in the replay's order.
type replayItem struct {
	line     int
	stamp    int64
	baseline bool
	id       string
	// groups are the baseline's groups this item sets, in apply order: the
	// ones whose own stamp is the item's (baselineItems).
	groups []string
}

// baselineItems splits a baseline into one item per distinct group stamp
// (baselineGroupStamp), each setting its groups in baselineGroupOrder. A
// baseline whose result is a FOUGHT finish declares the scoreline its verdict
// stood on, so a scoring group stamped before that result is set together
// with it: a change made between the two, arriving later, is older than the
// finish (the closed-match rule turns it away) rather than applied beneath it.
// A withdrawal's verdict (kiken, fusenpai, fusensho) does not stand on the
// scoreline (the bouts fought before it are ordered on their own stamps), so
// its scoring keeps its own stamps.
func baselineItems(line int, b *state.ScoreBaseline) []replayItem {
	resultAt := int64(0)
	if baselineFinishesFought(b) {
		resultAt = baselineGroupStamp(b, state.GroupResult)
	}
	byStamp := map[int64][]string{}
	var stamps []int64
	for _, g := range baselineGroupOrder(b.Values) {
		s := baselineGroupStamp(b, g)
		if resultAt > 0 && (state.IsScoringGroup(g) || g == state.GroupEncho) && s < resultAt {
			s = resultAt
		}
		if _, ok := byStamp[s]; !ok {
			stamps = append(stamps, s)
		}
		byStamp[s] = append(byStamp[s], g)
	}
	items := make([]replayItem, 0, len(stamps))
	for _, s := range stamps {
		// Within one stamp the groups keep baselineGroupOrder's order.
		items = append(items, replayItem{line: line, stamp: s, baseline: true, id: b.ID, groups: byStamp[s]})
	}
	return items
}

// baselineGroupStamp is the stamp a baseline dates a group at: its own when
// the baseline names one (0 for a group the match never wrote, a migration
// baseline's), else the line's.
func baselineGroupStamp(b *state.ScoreBaseline, g string) int64 {
	if s, ok := b.Stamps[g]; ok {
		return s
	}
	return b.Stamp
}

// baselineFinishesFought reports whether the baseline sets a completed result
// that no decision (kiken, fusenpai, fusensho) closed.
func baselineFinishesFought(b *state.ScoreBaseline) bool {
	raw, ok := b.Values[state.GroupResult]
	if !ok {
		return false
	}
	var m state.MatchResult
	if err := state.SetGroupFromValue(&m, state.GroupResult, raw); err != nil {
		return false
	}
	return m.Status == state.MatchStatusCompleted && !domain.IsDefaultWinDecisionStr(m.Decision)
}

// BaselineHold is what the replay did not apply of one baseline line: the
// scoring it carried onto a match a finish or a decision stamped before it had
// closed (the closed-match rule).
type BaselineHold struct {
	Groups []string
	Reason string
	// MatchClosed: a finish or a decision stamped before the baseline had
	// closed the match.
	MatchClosed bool
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

func cloneMatchForReplay(m state.MatchResult) state.MatchResult {
	c := m
	c.IpponsA, c.IpponsB = cloneIppons(m.IpponsA), cloneIppons(m.IpponsB)
	c.SubResults = state.CloneSubResults(m.SubResults)
	c.Encho = m.Encho.Clone()
	c.MarkIDs = m.MarkIDs.Clone()
	c.EventSeqs = state.CloneGroupStamps(m.EventSeqs)
	c.GroupStamps = state.CloneGroupStamps(m.GroupStamps)
	return c
}

// replayer carries one replay's state.
type replayer struct {
	lines  []state.ScoreLogLine
	shape  ReplayShape
	cur    *replayMatch
	stamps map[string]int64
	govern map[string]int // group -> line of the baseline that last set it
	// resultBy is the line that set the result group, as govern[result]
	// except that a repair line (ids only) never takes it over.
	resultBy int
	applied  map[string][]int // group -> event lines folded into it since
	// covered names every group some baseline of the log sets. An event on
	// such a group ordered before the first baseline that sets it is never
	// folded: the replay starts from the stored cache, the match AFTER every
	// line, so a fold there would judge it against a state that did not exist
	// yet. It waits in early for that baseline, which alone decides it
	// (recorded when its basis holds it, else beforeBaseline).
	covered map[string]bool
	early   map[string][]int
	outcome map[int]string
	held    map[int]BaselineHold
}

// padDefaultWin gives a team match a default win closes a row for every
// numbered bout nobody fought, as the write that records the decision does
// (shapeScoreWrite) and the load of a stored match repairs. The replay's
// match is the stored match, so it carries them too, dated by the result that
// made them.
func (rp *replayer) padDefaultWin() {
	comp, m := rp.shape.Comp, &rp.cur.m
	if comp == nil || comp.TeamSize < 2 || comp.IsKachinuki() {
		return
	}
	if !state.NeedsDefaultWinBoutPadding(m.Status, m.Decision, m.SideA, m.SideB, m.ID, m.SubResults, m.SubResultsUnreadable, comp.TeamSize) {
		return
	}
	had := map[int]bool{}
	for _, p := range state.SubPositions(m.SubResults) {
		had[p] = true
	}
	m.SubResults = state.PadDefaultWinBoutPositions(m.SubResults, comp.TeamSize)
	if at := rp.stamps[state.GroupResult]; at > 0 {
		for _, p := range state.SubPositions(m.SubResults) {
			if !had[p] {
				rp.stamps[state.BoutGroup(p)] = at
			}
		}
	}
}

// ReplayScoring folds a match's event log onto it. current supplies what no
// group holds (sides, ids, court, schedule, round) and, while the log is
// empty, everything: a match with no log is the match as stored.
func ReplayScoring(current *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape) Replay {
	out, _ := replayScoring(current, lines, shape, nil)
	return out
}

// replayScoring is ReplayScoring, stopping before the first item that does not
// sort before stopAt (nil: the whole log), and handing back the replayer so a
// caller can ask what it held at that point (resultInForce).
// startMadeRowsEmpty starts from no row every bout an event may make the row
// of (createsMissingRow) when no baseline of the log sets that bout: its row in
// the stored match (the replay's start) can only be one events made, and the
// events are folded again, so starting from the row would count each of them
// twice. A log begun by MigrationBaselineFor covers these bouts; one begun
// before it did not.
func (rp *replayer) startMadeRowsEmpty() {
	done := map[string]bool{}
	for _, l := range rp.lines {
		e := l.Event
		if e == nil || !rp.shape.createsMissingRow(e.Pos) {
			continue
		}
		g := rp.groupOf(e)
		if pos, ok := state.ParseBoutGroup(g); !ok || pos != e.Pos || rp.covered[g] || done[g] {
			continue
		}
		done[g] = true
		m := &rp.cur.m
		if i := boutRowIndex(m.SubResults, e.Pos); i >= 0 {
			m.SubResults = slices.Delete(slices.Clone(m.SubResults), i, i+1)
		}
		delete(rp.stamps, g)
	}
}

func replayScoring(current *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape, stopAt *replayItem) (Replay, *replayer) {
	if len(lines) == 0 {
		return Replay{Match: cloneMatchForReplay(*current)}, nil
	}
	rp := &replayer{
		lines: lines, shape: shape,
		cur:     &replayMatch{m: cloneMatchForReplay(*current), bouts: map[int]domain.BoutScore{}},
		stamps:  state.CloneGroupStamps(current.GroupStamps),
		govern:  map[string]int{},
		applied: map[string][]int{},
		covered: map[string]bool{},
		early:   map[string][]int{},
		outcome: map[int]string{},
	}
	if rp.stamps == nil {
		rp.stamps = map[string]int64{}
	}
	items := make([]replayItem, 0, len(lines))
	firstLine := map[string]int{}
	for i, l := range lines {
		if l.Baseline != nil {
			// A baseline's group holds the value it had at that group's own
			// stamp, so each group is replayed at its stamp, not the line's:
			// a migration baseline's points stamped T0 and result stamped T2
			// sort apart, and a change stamped between them lands between.
			items = append(items, baselineItems(i, l.Baseline)...)
			for g := range l.Baseline.Values {
				rp.covered[g] = true
			}
			continue
		}
		items = append(items, replayItem{line: i, stamp: l.Stamp(), id: l.ID()})
		if l.Event != nil {
			if _, ok := firstLine[l.Event.ID]; !ok {
				firstLine[l.Event.ID] = i
			}
		}
	}
	rp.startMadeRowsEmpty()
	slices.SortFunc(items, compareReplayItems)
	for _, it := range items {
		if stopAt != nil && compareReplayItems(it, *stopAt) >= 0 {
			break
		}
		l := lines[it.line]
		switch {
		case l.Baseline != nil:
			rp.applyBaseline(it.line, l.Baseline, it.groups)
		case firstLine[l.Event.ID] != it.line:
			rp.outcome[it.line] = domain.ScoreOutcomeDuplicate
		default:
			rp.applyEvent(it.line, l.Event)
		}
	}
	rp.settleEarly()
	rp.padDefaultWin()

	out := Replay{Match: rp.cur.m, Held: rp.held}
	out.Match.GroupStamps = rp.stamps
	if len(rp.stamps) == 0 {
		out.Match.GroupStamps = nil
	}
	// The newest stamp the replayed match holds: a newer one the cache held
	// for a change the replay did not apply is not the match's.
	if len(rp.stamps) > 0 {
		out.Match.ModifiedAt = 0
	}
	for _, s := range rp.stamps {
		out.Match.ModifiedAt = max(out.Match.ModifiedAt, s)
	}
	out.Match.ScoreVersion = len(lines)
	out.Match.EventSeqs = ScoreLogEventSeqs(lines)
	for i, l := range lines {
		if l.Event != nil {
			out.Outcomes = append(out.Outcomes, EventOutcome{ID: l.Event.ID, Line: i, Outcome: rp.outcome[i]})
		}
	}
	return out, rp
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
	e := rp.lines[i].Event
	// A baseline cannot hold a tap made after it: its basis counts the lines
	// the writer had seen, which for a server-built write is arrival order,
	// not stamp order.
	if e.Stamp > baselineGroupStamp(b, rp.groupOf(e)) {
		return false
	}
	return BasisContains(b, i, e)
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

func (rp *replayer) applyBaseline(line int, b *state.ScoreBaseline, groups []string) {
	m := &rp.cur.m
	// A baseline that carries scoring, and not the result, onto a match the
	// log has already closed is turned away as an event is (the closed-match
	// rule).
	if hold := rp.judgeOverFinished(line, b, groups); hold != nil {
		groups = slices.DeleteFunc(slices.Clone(groups), func(g string) bool { return slices.Contains(hold.Groups, g) })
		if rp.held == nil {
			rp.held = map[int]BaselineHold{}
		}
		prev := rp.held[line]
		hold.Groups = unionGroups(prev.Groups, hold.Groups)
		rp.held[line] = *hold
		// A bout no line has set yet is not in the match at this point: the
		// cache's row is the final one, a copy of what an earlier arrival of
		// this same line landed.
		for _, g := range hold.Groups {
			pos, isBout := state.ParseBoutGroup(g)
			if _, governed := rp.govern[g]; !isBout || governed {
				continue
			}
			m.SubResults = slices.DeleteFunc(m.SubResults, func(r state.SubMatchResult) bool { return r.Position == pos })
			delete(rp.cur.bouts, pos)
			delete(rp.stamps, g)
		}
	}
	rp.applyBaselineGroups(line, b, groups)
}

// decisionLoserSide is the side whose struck points a decision on group g
// keeps ("a" or "b"), or "" when the baseline is no decision of that kind.
func decisionLoserSide(b *state.ScoreBaseline, g string) string {
	if g != state.GroupPoints || b.Door != DoorDecision {
		return ""
	}
	var verdict state.MatchResult
	if err := state.SetGroupFromValue(&verdict, state.GroupResult, b.Values[state.GroupResult]); err != nil || verdict.Decision == "" {
		return ""
	}
	switch verdict.DecisionBy {
	case "shiro":
		return domain.ScoreSideB
	case "aka":
		return domain.ScoreSideA
	}
	return ""
}

// keepLoserPoints is a decision's points as the log gives them at its stamp:
// the side that did not withdraw takes the decision's circles, and the side
// that did keeps the points it had already struck (FIK Art. 32). The decision
// was built over the match as the server held it when it arrived, which can
// be before or after an earlier-stamped scoring write reached the server, so
// the loser's points are worked out here, from the lines stamped before the
// decision, and not copied from the value it carries: arrival order cannot
// change them.
func (rp *replayer) keepLoserPoints(b *state.ScoreBaseline, value json.RawMessage) json.RawMessage {
	var verdict state.MatchResult
	if err := state.SetGroupFromValue(&verdict, state.GroupResult, b.Values[state.GroupResult]); err != nil || verdict.Decision == "" {
		return value
	}
	var pv state.MatchResult
	if err := state.SetGroupFromValue(&pv, state.GroupPoints, value); err != nil {
		return value
	}
	switch verdict.DecisionBy {
	case "shiro":
		pv.IpponsB = struckIppons(rp.cur.m.IpponsB)
	case "aka":
		pv.IpponsA = struckIppons(rp.cur.m.IpponsA)
	default:
		return value
	}
	return state.GroupValue(&pv, state.GroupPoints)
}

// applyBaselineGroups sets each of the groups to the baseline's value at the
// group's own stamp (values overrides a value the replay adjusted).
func (rp *replayer) applyBaselineGroups(line int, b *state.ScoreBaseline, groups []string) {
	m := &rp.cur.m
	for _, g := range groups {
		pos, isBout := state.ParseBoutGroup(g)
		var prevA, prevB, prevMA, prevMB []string
		_, governed := rp.govern[g]
		switch {
		case !governed:
			// The first baseline that sets the group replays over the cache,
			// the match after EVERY line: its marks and ids are not this
			// point of the log's, so the baseline mints its own ids rather
			// than take ids of events the log has not reached yet.
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
		value := b.Values[g]
		if b.Door == DoorRepair && governed && !rp.repairFillsIDs(g, value) {
			// A repair line fills ids only: stamped at the stored match's
			// group stamps, it carries the whole group, so over a value the
			// log has since moved (an earlier-stamped result that arrived
			// later and displaced the one it repaired) it would put that
			// displaced value back. It is skipped there (bc-evts review
			// round 12). Nothing is lost: the load repair refills the ids, and the next
			// drift check mints a fresh repair line.
			continue
		}
		if g == state.GroupPoints && governed && b.Door == DoorDecision {
			value = rp.keepLoserPoints(b, value)
		}
		// A baseline that only echoes the group's value (a named echo, which
		// dates the group) leaves the events folded into it as they are:
		// their marks are in the value it echoes.
		echo := governed && bytes.Equal(state.GroupValue(m, g), value)
		if err := state.SetGroupFromValue(m, g, value); err != nil {
			// A line is validated before it is logged; a value that still
			// does not read leaves the group as it was, and the baseline
			// does not govern it. The replay's knowledge of a group begins
			// at its first READABLE baseline: until one is reached the
			// cache's value (the match after every line) stands and no
			// event is folded onto it, since a fold there would count again
			// a mark the cache already holds (and a replay from its own
			// output would count it once more each time). Governing the
			// group here would also let the next baseline take ids from
			// that cache. So an event on a group no readable baseline has
			// set stays early, and answers beforeBaseline at the end.
			continue
		}
		switch {
		case g == state.GroupPoints:
			m.MarkIDs = markIDsOf(m.IpponsA, m.IpponsB,
				assignMarks(prevA, prevMA, m.IpponsA, 0, domain.ScoreSideA),
				assignMarks(prevB, prevMB, m.IpponsB, 0, domain.ScoreSideB))
		case isBout:
			delete(rp.cur.bouts, pos)
			for i := range m.SubResults {
				if row := &m.SubResults[i]; row.Position == pos {
					row.MarkIDs = markIDsOf(row.IpponsA, row.IpponsB,
						assignMarks(prevA, prevMA, row.IpponsA, pos, domain.ScoreSideA),
						assignMarks(prevB, prevMB, row.IpponsB, pos, domain.ScoreSideB))
				}
			}
		}
		if s := baselineGroupStamp(b, g); s > 0 {
			rp.stamps[g] = s
		} else {
			delete(rp.stamps, g)
		}
		// Every event folded into the group before this baseline is now
		// either in its values (recorded) or replaced by them (D5).
		// An echo takes only the events it saw (recorded); one it did not
		// see stays applied, its mark being in the value echoed.
		var kept []int
		for _, i := range rp.applied[g] {
			if echo && !rp.contains(b, i) {
				kept = append(kept, i)
				continue
			}
			rp.absorb(b, i, g)
		}
		for _, i := range rp.early[g] {
			rp.absorb(b, i, g)
		}
		rp.applied[g], rp.early[g] = kept, nil
		rp.govern[g] = line
		// The line that SET the result (resultInForce). A repair line only
		// fills ids on the result the replay holds at its point (it is
		// skipped otherwise, above), so the line a device saw is still the
		// one that set it (bc-evts review round 11). A drift line is a write
		// the log missed and does set it, unless I1 holds it (rivalResult).
		if g == state.GroupResult && (b.Door != DoorRepair || !governed) {
			rp.resultBy = line
		}
	}
}

// repairFillsIDs reports whether a repair line's value of group g differs from
// the value the replay holds at that point in filled ids alone (the rule
// driftOf minted it by, repairFillsIDsOnly).
func (rp *replayer) repairFillsIDs(g string, value json.RawMessage) bool {
	repaired := cloneMatchForReplay(rp.cur.m)
	if err := state.SetGroupFromValue(&repaired, g, value); err != nil {
		return false
	}
	return repairFillsIDsOnly(&repaired, &rp.cur.m, g)
}

// judgeOverFinished decides a baseline item that sets scoring but not the
// result on a match the log has already closed, by the rule an event meets
// there: it is turned away. It answers the hold, or nil when the item is not
// scoring (or the match is not closed).
// closed reports whether the lines folded so far closed the match: a baseline
// has set the result and it is a finished one. A reopen sets it running again,
// so the match is open from the reopen's stamp.
func (rp *replayer) closed() bool {
	_, ok := rp.resultInForce()
	return ok
}

// resultInForce is THE answer to "is a result in force, and which line set
// it": the line of the baseline that last set the result group, when the match
// it left is finished. A reopen or a send back to the queue is such a baseline
// leaving the match open, so it clears the result.
func (rp *replayer) resultInForce() (line int, ok bool) {
	_, governed := rp.govern[state.GroupResult]
	return rp.resultBy, governed && rp.cur.m.Status == state.MatchStatusCompleted
}

func (rp *replayer) judgeOverFinished(line int, b *state.ScoreBaseline, groups []string) *BaselineHold {
	if !rp.closed() {
		return nil
	}
	// A result that carries the result group is a correction only when the
	// writer saw the one in force; a rival result is turned away (rivalResult).
	if slices.Contains(groups, state.GroupResult) {
		if !rp.rivalResult(line, b) {
			return nil
		}
		return &BaselineHold{Groups: rp.rivalHeldGroups(groups), Reason: HoldReasonMatchClosed, MatchClosed: true}
	}
	var scoring []string
	for _, g := range groups {
		if (state.IsScoringGroup(g) || g == state.GroupEncho) && !rp.boutsStayOpen(g) {
			scoring = append(scoring, g)
		}
	}
	if len(scoring) == 0 {
		return nil
	}
	return &BaselineHold{Groups: scoring, Reason: HoldReasonMatchClosed, MatchClosed: true}
}

// rivalResult reports whether baseline b, which carries a result onto a match
// the log has already closed, is a RIVAL of the result in force: a finish or a
// decision from a device that did not see it. THE FIRST RECORDED RESULT STANDS
// (operator decision 2026-10-16, I1): in stamp order, a later rival is turned
// away as scoring is (match_closed), whichever order the two arrived in.
//
// A result is no rival when its writer saw the one in force: it names a
// correction (a reason, or clearWithdrawal), sent a basis that holds the
// line of the result in force, or is the server's own write (it carries no
// stamp of a device's: an engine-internal caller, made with the whole match
// in view). A reopen or a send back to the queue (a result
// that leaves the match open), a direct writer's result (override-winner, the
// engine's own callers, bulk and quick score: an organiser acting on what the
// screen shows) is no rival either. Only a finish or a decision from the
// score, engi or decision doors is, and a drift line that carries a result is
// judged by the same rule (see the DoorDrift arm below).
func (rp *replayer) rivalResult(line int, b *state.ScoreBaseline) bool {
	switch b.Door {
	case DoorScore, doorEngi, DoorDecision:
	case DoorDrift:
		// A drift line (a fault: the stored match says what no line
		// explains) is judged like a result from a device: its basis is
		// the lines it was minted over, so a first-by-stamp result logged
		// after it is one it did not see (bc-evts review round 12).
	default:
		return false
	}
	inForce, ok := rp.resultInForce()
	if !ok || inForce == line || b.Informed || baselineResultStatus(b) != state.MatchStatusCompleted {
		return false
	}
	logLines := len(rp.lines)
	if rp.shape.logKnown {
		logLines = rp.shape.logLines
	}
	if (b.BasisSent || b.Door == DoorDrift) && basisSawLine(&b.Basis, logLines, inForce) {
		return false
	}
	// A result that says what the match already holds, in every group it
	// carries, is the same write sent again (a queued replay whose answer was
	// lost, a second board recording the same decision): it changes nothing, so
	// it is an echo, not a rival.
	for g, v := range b.Values {
		if !bytes.Equal(state.GroupValue(&rp.cur.m, g), v) {
			return true
		}
	}
	return false
}

// rivalHeldGroups is what a rival result loses: everything it carries, except
// the numbered bouts of a team match a default-win decision closed
// (boutsStayOpen), whose scores it brings along.
func (rp *replayer) rivalHeldGroups(groups []string) []string {
	return slices.DeleteFunc(slices.Clone(groups), rp.boutsStayOpen)
}

// boutsStayOpen reports whether group g is a numbered bout of a team match
// closed by a match-level default-win decision (kiken, fusenpai, fusensho): the
// decision decides the match and credits only the numbered bouts with no
// result of their own (padDefaultWin), so a bout scored on a device that never
// saw it keeps its score, whichever way the two arrive (operator decision
// 2026-10-16, T1: merge by bout). Kachinuki is left out, as the padding is: its
// bouts are a log with no bout to credit, so a closed kachinuki match is
// closed whole, like an individual one.
func (rp *replayer) boutsStayOpen(g string) bool {
	pos, isBout := state.ParseBoutGroup(g)
	if !isBout || pos < 1 {
		return false
	}
	comp, m := rp.shape.Comp, &rp.cur.m
	return comp != nil && comp.TeamSize >= 2 && !comp.IsKachinuki() &&
		m.Status == state.MatchStatusCompleted && domain.IsDefaultWinDecisionStr(m.Decision)
}

// baselineResultStatus is the status the baseline's result group carries.
func baselineResultStatus(b *state.ScoreBaseline) state.MatchStatus {
	var m state.MatchResult
	if raw, ok := b.Values[state.GroupResult]; !ok || state.SetGroupFromValue(&m, state.GroupResult, raw) != nil {
		return ""
	}
	return m.Status
}

// ResultInForce is what the score log says of the result a write meets: whether
// a finished result was in force at the point asked about, the log line that
// set it, and whether the write's basis (capped to the lines the log really
// holds) holds that line, so the writer saw it. It is the ONE answer every
// caller reads: the replay's rival test (rivalResult), the result lock
// (rivalDisplacesResult), the padding gate (writesAsRival) and the /score
// handler's reason check (ResultInForceAt).
type ResultInForce struct {
	InForce bool
	Line    int
	Saw     bool
}

// basisSawLine reports whether a write's basis holds the log line at index
// line. lineCount is how many lines the log really holds: a count past it
// names lines no device saw (the migration baseline minted for an empty log is
// no line a device saw either). A write sending no basis saw nothing.
func basisSawLine(basis *state.ScoreBasis, lineCount, line int) bool {
	return basis != nil && line < min(basis.Seen, lineCount)
}

// resultInForceIn replays lines (the log as a merge sees it: a migration or
// drift baseline included) up to stopAt (nil: all of them) and reads what the
// replay holds. basis is the writer's SENT basis, nil when it sent none.
func resultInForceIn(stored *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape, stopAt *replayItem, basis *state.ScoreBasis, lineCount int) ResultInForce {
	_, rp := replayScoring(stored, lines, shape, stopAt)
	if rp == nil {
		return ResultInForce{}
	}
	return rp.inForce(basis, lineCount)
}

// inForce is what the replay rp holds of the result in force, and whether
// basis (capped to lineCount) saw the line that set it.
func (rp *replayer) inForce(basis *state.ScoreBasis, lineCount int) ResultInForce {
	line, ok := rp.resultInForce()
	if !ok {
		return ResultInForce{}
	}
	return ResultInForce{InForce: true, Line: line, Saw: basisSawLine(basis, lineCount, line)}
}

// ResultInForceAt works out, from the stamp-ordered score log lines of a match
// (stored is the match as it stands), the result in force at stamp (0: after
// every line), exactly as the replay does: the line that last set the result
// while the match was finished, none once a reopen or a send back to the queue
// opened it again. basis is what the write sent (nil: none). An empty log is
// the stored match as its migration baseline.
func ResultInForceAt(stored *state.MatchResult, lines []state.ScoreLogLine, comp *state.Competition, knockout bool, stamp int64, basis *state.ScoreBasis) ResultInForce {
	count := len(lines)
	shape := ReplayShape{Comp: comp, Knockout: knockout}
	// The log as the merge reads it (replayLines): the migration baseline of
	// an empty log, a drift or repair line appended. It is not logged here;
	// the write that follows logs it.
	all, _, found := replayLines(stored, lines, shape, 0, false)
	if stamp <= 0 && found != nil {
		return found.inForce(basis, count)
	}
	var stopAt *replayItem
	if stamp > 0 {
		stopAt = &replayItem{stamp: stamp, baseline: true}
	}
	return resultInForceIn(stored, all, shape, stopAt, basis, count)
}

// writesAsRival reports whether a write is a rival of the stored result (the
// one in force at the end of lines): from the score, engi or decision door,
// stamped, naming no correction (no reason, no clearWithdrawal), and with no
// sent basis that holds the line of the result in force.
func writesAsRival(w, stored *state.MatchResult, lines []state.ScoreLogLine, comp *state.Competition, knockout bool) bool {
	switch doorOf(w) {
	case DoorScore, doorEngi, DoorDecision:
	default:
		return false
	}
	if w.ModifiedAt <= 0 || w.CorrectionReason != "" || w.ClearsWithdrawal {
		return false
	}
	if w.Basis == nil {
		return true
	}
	r := ResultInForceAt(stored, lines, comp, knockout, 0, w.Basis)
	return !r.InForce || !r.Saw
}

// absorb settles event i, which baseline b's group value now holds or replaces:
// recorded when b saw it (BasisContains), else older than the baseline.
func (rp *replayer) absorb(b *state.ScoreBaseline, i int, g string) {
	if loser := decisionLoserSide(b, g); loser != "" {
		// A decision keeps the points its loser had struck and replaces the
		// other side's (keepLoserPoints), so the side of a tap stamped before
		// it decides, not the basis: whether the decision saw the tap changes
		// with arrival order, the scoreline does not.
		if side := rp.lines[i].Event.Side; side == domain.ScoreSideA || side == domain.ScoreSideB {
			if side == loser {
				rp.outcome[i] = domain.ScoreOutcomeRecorded
			} else {
				rp.outcome[i] = domain.ScoreOutcomeBeforeBaseline
			}
			return
		}
	}
	if rp.contains(b, i) {
		rp.outcome[i] = domain.ScoreOutcomeRecorded
	} else {
		rp.outcome[i] = domain.ScoreOutcomeBeforeBaseline
	}
}

func (rp *replayer) settleEarly() {
	// An early event whose baseline never set its group (a value that did not
	// read) is still older than that baseline.
	for g, lines := range rp.early {
		for _, i := range lines {
			rp.outcome[i] = domain.ScoreOutcomeBeforeBaseline
		}
		delete(rp.early, g)
	}
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
	// THE CLOSED-MATCH RULE: a finish or a decision the log has already
	// recorded (in stamp order) closed the match, so a tap stamped after it
	// is kept and changes nothing. Correct (a baseline) or a reopen is
	// the only way to change it.
	if rp.closed() && !rp.boutsStayOpen(g) {
		rp.outcome[line] = domain.ScoreOutcomeMatchClosed
		return
	}
	b, governed := rp.govern[g]
	if !governed && rp.covered[g] {
		// Older than the first baseline that sets the group (see covered).
		rp.early[g] = append(rp.early[g], line)
		return
	}
	if governed && rp.contains(rp.lines[b].Baseline, line) {
		rp.outcome[line] = domain.ScoreOutcomeRecorded
		return
	}
	out := foldEvent(rp.cur, e, rp.shape, rp.stamps)
	rp.outcome[line] = out
	if out == domain.ScoreOutcomeApplied {
		rp.stamps[g] = max(rp.stamps[g], e.Stamp)
		rp.applied[g] = append(rp.applied[g], line)
	}
}

// keepEntries keeps the ippons keep accepts, with the mark ids beside them.
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

// assignMarks names the marks a baseline brings. The marks the board held
// before are matched to the new ones by value, in order (a longest common
// subsequence), so a mark the baseline keeps keeps its id wherever it now
// sits: a finish echoing the board's points, less a mark removed from the
// front, keeps the ids the board removes by. When equal letters compete for
// the ids that survive, the earliest copy keeps its id: the walk matches left
// to right, so [M M] before and [M] after keeps the first M's id and drops the
// second's. A new mark, or one whose earlier copy
// had no id, is b:<pos>:<side>:<letter>:<n> (markID), the same in every arrival
// order; a placeholder slot has none.
func assignMarks(prev, prevMarks, next []string, pos int, side string) []string {
	isMark := func(v string) bool { return v != "" && v != domain.IpponPlaceholder }
	same := func(i, j int) bool { return isMark(next[j]) && prev[i] == next[j] }
	// lcs[i][j] is the length of the longest common subsequence of prev[i:]
	// and next[j:] (both a handful of marks).
	lcs := make([][]int, len(prev)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(next)+1)
	}
	for i := len(prev) - 1; i >= 0; i-- {
		for j := len(next) - 1; j >= 0; j-- {
			if same(i, j) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	out := make([]string, len(next))
	for i, j := 0, 0; j < len(next); {
		switch {
		case i < len(prev) && same(i, j) && lcs[i][j] == lcs[i+1][j+1]+1:
			if i < len(prevMarks) {
				out[j] = prevMarks[i]
			}
			i++
			j++
		case i < len(prev) && lcs[i+1][j] >= lcs[i][j+1]:
			i++
		default:
			j++
		}
	}
	// A mark no earlier mark accounts for is minted an id from what it IS
	// (the bout, the side, the strike and how many of that strike the side
	// holds before it), never from the baseline that introduced it: which
	// baseline that is depends on the order writes arrived in (an older save
	// of the same mark arriving later introduces it first), and a device that
	// read the id from its answer would then find it gone. The ordinal skips
	// an id the side already holds, so two marks never share one.
	taken := make(map[string]bool, len(out))
	for _, id := range out {
		if id != "" {
			taken[id] = true
		}
	}
	ordinal := map[string]int{}
	for j, v := range next {
		if !isMark(v) {
			continue
		}
		n := ordinal[v]
		ordinal[v]++
		if out[j] != "" {
			continue
		}
		id := markID(pos, side, v, n)
		for taken[id] {
			n++
			id = markID(pos, side, v, n)
		}
		taken[id] = true
		out[j] = id
	}
	return out
}

// markID is the id a baseline-minted mark takes: the bout, the side, the strike
// and its ordinal among the side's strikes of that letter.
func markID(pos int, side, letter string, n int) string {
	return "b:" + strconv.Itoa(pos) + ":" + side + ":" + letter + ":" + strconv.Itoa(n)
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
		// A rep player names the fighter a TEAM fields for a pool
		// representative or tie-break bout (MatchResult.RepPlayerA: the
		// /score door strips the names from any other match, and the editor
		// offers the pickers only there, repIsTeam). The replay is stricter
		// than the /score door's rule: it also requires a team competition
		// (shape.team()), where the door keeps the names on any such id.
		supplementary := state.IsPoolSupplementaryMatchID(m.ID)
		if !shape.team() || !supplementary {
			return domain.ScoreOutcomeNotAllowed
		}
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
	singleBout := state.IsPoolSupplementaryMatchID(m.ID)
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
	if boutRowIndex(m.SubResults, e.Pos) >= 0 || !shape.createsMissingRow(e.Pos) {
		return foldBoutRow(r, e, shape, stamps)
	}
	// A numbered bout the match holds no row for yet (createsMissingRow: a
	// fixed-order bout of a match started from the queue, a kachinuki first
	// bout, a pairing added by hand): the event's row is made, empty, and kept
	// only if the event applies to it.
	prior := m.SubResults
	m.SubResults = withEmptyBoutRow(prior, e.Pos)
	out := foldBoutRow(r, e, shape, stamps)
	if out != domain.ScoreOutcomeApplied {
		m.SubResults = prior
	}
	return out
}

// createsMissingRow says an event on bout pos may make the row when the match
// holds none: a numbered bout of a fixed-order team match, within its team
// size, and any numbered bout of a kachinuki encounter (its first bout, which
// a match started from the queue holds no row for, and a pairing added by
// hand). Two devices tapping such a bout at once both count: nothing writes
// the row ahead of a tap, so no row write stamped after one device's tap can
// turn it away. Only the server writes a bout row ahead of the taps on it: a
// kachinuki pairing Record bout appends, and a representative bout.
func (shape ReplayShape) createsMissingRow(pos int) bool {
	if !shape.team() {
		return false
	}
	if shape.kachinuki() {
		return pos >= 1
	}
	return pos >= 1 && pos <= shape.Comp.TeamSize
}

// seatsFirstBoutFighter says a bout.fighter event on a kachinuki encounter's
// first bout may seat its fighter on a side that holds name and member: the
// side holds no fighter yet, or holds the same one (by member id when either
// carries one, else by name), which a second device seats again.
func seatsFirstBoutFighter(name, member string, e *domain.ScoreEvent) bool {
	if name == "" && member == "" {
		return true
	}
	if member != "" || e.MemberID != "" {
		return member == e.MemberID
	}
	return name == e.Name
}

// boutRowIndex is the index of the row at pos, -1 when there is none.
func boutRowIndex(subs []state.SubMatchResult, pos int) int {
	for i := range subs {
		if subs[i].Position == pos {
			return i
		}
	}
	return -1
}

// withEmptyBoutRow is subs with an empty row at pos, the numbered rows in
// position order ahead of the representative bout. subs is not changed.
func withEmptyBoutRow(subs []state.SubMatchResult, pos int) []state.SubMatchResult {
	out := make([]state.SubMatchResult, 0, len(subs)+1)
	placed := false
	for _, s := range subs {
		if !placed && (s.Position < 0 || s.Position > pos) {
			out = append(out, state.SubMatchResult{Position: pos})
			placed = true
		}
		out = append(out, s)
	}
	if !placed {
		out = append(out, state.SubMatchResult{Position: pos})
	}
	return out
}

func foldBoutRow(r *replayMatch, e *domain.ScoreEvent, shape ReplayShape, stamps map[string]int64) string {
	m := &r.m
	idx := boutRowIndex(m.SubResults, e.Pos)
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
		if !shape.kachinuki() || e.Pos <= 0 {
			return domain.ScoreOutcomeNotAllowed
		}
		name, member := &row.SideA, &row.SideAMemberID
		if e.Side == domain.ScoreSideB {
			name, member = &row.SideB, &row.SideBMemberID
		}
		if e.Pos == lowestNumberedPosition(m.SubResults) && !seatsFirstBoutFighter(*name, *member, e) {
			// The first bout's fighters are the lineup's (a name picked there
			// is a lineup save): the sheet seats them as its first taps on the
			// bout, so a side that holds none takes one and a side never has
			// its fighter replaced here.
			return domain.ScoreOutcomeNotAllowed
		}
		*name, *member = e.Name, e.MemberID
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

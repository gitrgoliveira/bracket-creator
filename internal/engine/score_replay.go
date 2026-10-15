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
// A baseline (a door's write: a snapshot, a finish, a decision) sets each of
// its groups at that group's own stamp (baselineItems), a fought finish's
// scoring together with its result. One that sets scoring but not the
// result on a match the log has finished meets the same rules as an event
// (judgeOverFinished); what it does not land is Replay.Held. Since stage 2b
// every scoring snapshot is decided this way (mergeByReplay, match_merge.go).
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
	// Held names, by line index, the baselines whose scoring the replay did
	// not apply to a finished match (BaselineHold).
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
// version, with the ippons each set of ids names.
type derivedScore struct {
	ipponsA, ipponsB []string
	marks            *state.MarkIDs
	bouts            map[int]derivedBout
	version          int
}

type derivedBout struct {
	ipponsA, ipponsB []string
	marks            *state.MarkIDs
}

func derivedScoreOf(ipponsA, ipponsB []string, marks *state.MarkIDs, subs []state.SubMatchResult, version int) derivedScore {
	d := derivedScore{ipponsA: slices.Clone(ipponsA), ipponsB: slices.Clone(ipponsB), marks: marks.Clone(),
		bouts: make(map[int]derivedBout, len(subs)), version: version}
	for _, row := range subs {
		d.bouts[row.Position] = derivedBout{ipponsA: slices.Clone(row.IpponsA), ipponsB: slices.Clone(row.IpponsB), marks: row.MarkIDs.Clone()}
	}
	return d
}

// carry settles the derived fields of a write that is not the replay's
// (DoorEvent): the mark ids and the version are never taken from a write,
// which a client builds and may fill with anything. A scoreline (the match's,
// or a bout's) the write left as it was keeps the ids it had; one it changed
// has none until the replay names its marks again. It returns the match-level
// ids and rewrites each row's in subs; the version is d.version.
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
// (the group's own, else the line's), each setting its groups in
// baselineGroupOrder.
// baselineItems splits a baseline into one item per distinct group stamp
// (the group's own, else the line's), each setting its groups in
// baselineGroupOrder. A baseline that FINISHES the match declares the
// scoreline its verdict stood on, so a scoring group stamped at or before
// its result is set together with the result: a change made between the
// two, arriving later, is older than the finish (R3) rather than applied
// beneath it.
// baselineItems splits a baseline into one item per distinct group stamp
// (baselineGroupStamp), each setting its groups in baselineGroupOrder.
// baselineItems splits a baseline into one item per distinct group stamp
// (baselineGroupStamp), each setting its groups in baselineGroupOrder. A
// baseline whose result is a FOUGHT finish declares the scoreline its verdict
// stood on, so a scoring group stamped before that result is set together
// with it: a change made between the two, arriving later, is older than the
// finish (R3) rather than applied beneath it. A decision's verdict does not
// stand on the scoreline (R2 orders the bouts fought before a withdrawal on
// their own stamps), so its scoring keeps its own stamps.
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
// scoring it carried onto a finished match that a recorded decision closed
// (DefaultWinStands, with that decision) or that would have left the match
// without the winner it needs (R4).
type BaselineHold struct {
	Groups           []string
	Reason           string
	DefaultWinStands bool
	Decision         string
	NeedsWinner      bool
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
	// covered names every group some baseline of the log sets. An event on
	// such a group ordered before the first baseline that sets it is never
	// folded: the replay starts from the stored cache, the match AFTER every
	// line, so a fold there would judge it against a state that did not exist
	// yet. It waits in early for that baseline, which alone decides it
	// (recorded when its basis holds it, else beforeBaseline).
	covered map[string]bool
	early   map[string][]int
	pending []int // event lines waiting for the result they need (R4/R2)
	clears  bool  // the pending events would clear a withdrawal
	outcome map[int]string
	cleared bool
	held    map[int]BaselineHold
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
	slices.SortFunc(items, compareReplayItems)
	for _, it := range items {
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
	rp.settlePending()

	out := Replay{Match: rp.cur.m, ClearedWithdrawal: rp.cleared, Held: rp.held}
	out.Match.GroupStamps = rp.stamps
	if len(rp.stamps) == 0 {
		out.Match.GroupStamps = nil
	}
	// The newest stamp the replayed match holds: a newer one the cache held
	// for a change the replay did not apply is not the match's (S2 with R4).
	if len(rp.stamps) > 0 {
		out.Match.ModifiedAt = 0
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

func (rp *replayer) applyBaseline(line int, b *state.ScoreBaseline, groups []string) {
	m := &rp.cur.m
	// A baseline that carries scoring, and not the result, onto a match the
	// log has already finished is judged as an event is (R2/R3/R4/S3).
	verdict, values, hold := rp.judgeOverFinished(b, groups)
	if hold != nil {
		groups = slices.DeleteFunc(slices.Clone(groups), func(g string) bool { return slices.Contains(hold.Groups, g) })
		if rp.held == nil {
			rp.held = map[int]BaselineHold{}
		}
		prev := rp.held[line]
		hold.Groups = unionGroups(prev.Groups, hold.Groups)
		rp.held[line] = *hold
	}
	said := *m
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
		if v, ok := values[g]; ok {
			value = v
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
			rp.absorb(b, i)
		}
		for _, i := range rp.early[g] {
			rp.absorb(b, i)
		}
		rp.applied[g], rp.early[g] = kept, nil
		rp.govern[g] = line
		if g == state.GroupResult {
			// The result is this baseline's: a withdrawal an earlier event
			// cleared is cleared (or recorded again) by it, not by the replay.
			rp.cleared = false
		}
	}
	if verdict != nil {
		// R3: the finished match takes the winner its new scoring gives (or
		// R2: the withdrawal that scoring cleared), and the result is dated
		// by the baseline when the verdict moved.
		state.CopyGroup(m, verdict, state.GroupResult)
		if verdictMoved(&said, m) {
			if s := cmp.Or(b.Stamp, rp.stamps[state.GroupResult]); s > 0 {
				rp.stamps[state.GroupResult] = max(rp.stamps[state.GroupResult], s)
			}
		}
		if verdict.ClearsWithdrawal {
			rp.cleared = true
		}
	}
	if len(rp.pending) == 0 {
		return
	}
	// A pending event whose group this baseline sets is settled by it; a new
	// result settles the rest, which never applied: still waiting for the
	// winner when the baseline saw it, else older than the baseline (D5).
	setsResult := slices.Contains(groups, state.GroupResult)
	var keep []int
	for _, i := range rp.pending {
		sets := slices.Contains(groups, rp.groupOf(rp.lines[i].Event))
		switch {
		case sets:
			rp.absorb(b, i)
		case setsResult && rp.contains(b, i):
			rp.outcome[i] = rp.pendingOutcome()
		case setsResult:
			rp.outcome[i] = domain.ScoreOutcomeBeforeBaseline
		default:
			keep = append(keep, i)
		}
	}
	rp.pending = keep
	if len(keep) == 0 {
		rp.clears = false
	}
}

// judgeOverFinished decides a baseline item that sets scoring but not the
// result on a match the log has already finished, by the rules an event
// meets there (R2, R3, R4, S3), through the same owner the merge used
// (deriveWinnerAfterMerge). It answers the verdict to take (nil: the
// result stands as it is), values to set in place of the baseline's for
// some groups (points carrying the stored judges' decision), or the hold.
// An item whose scoring only echoes the match is not judged.
func (rp *replayer) judgeOverFinished(b *state.ScoreBaseline, groups []string) (*state.MatchResult, map[string]json.RawMessage, *BaselineHold) {
	cur := &rp.cur.m
	// Until a line sets the result, the status is the cache's (the match
	// after every line), not this point's: nothing is judged against it.
	if _, governed := rp.govern[state.GroupResult]; !governed || cur.Status != state.MatchStatusCompleted || slices.Contains(groups, state.GroupResult) {
		return nil, nil, nil
	}
	probe := cloneMatchForReplay(*cur)
	var scoring []string
	changed, scored := false, false
	for _, g := range groups {
		if !state.IsScoringGroup(g) && g != state.GroupEncho {
			continue
		}
		if err := state.SetGroupFromValue(&probe, g, b.Values[g]); err != nil {
			continue
		}
		scoring = append(scoring, g)
		if state.GroupDiffers(cur, &probe, g) {
			changed = true
			scored = scored || state.IsScoringGroup(g)
		}
	}
	if !changed {
		return nil, nil, nil
	}
	mc := mergeCtx{comp: rp.shape.Comp, knockout: rp.shape.Knockout, storedAID: cur.SideAID, storedBID: cur.SideBID}
	standing := func(decision string) *BaselineHold {
		return &BaselineHold{Groups: scoring, Reason: defaultWinStandsReason(decision), DefaultWinStands: true, Decision: decision}
	}
	switch {
	case domain.IsDefaultWinDecisionStr(cur.Decision) && !domain.IsWithdrawalDecisionStr(cur.Decision):
		// A fusensho stands whatever is scored.
		return nil, nil, standing(cur.Decision)
	case domain.IsWithdrawalDecisionStr(cur.Decision):
		// Points scored after a withdrawal of this match mean it was a
		// mistake, but only together with the result they give; overtime
		// alone says nothing about it.
		if !scored {
			return nil, nil, standing(cur.Decision)
		}
		probe.Decision, probe.DecisionBy, probe.DecisionReason = "", "", ""
		probe.Winner, probe.WinnerID, probe.WinnerSide = "", "", ""
		if !deriveWinnerAfterMerge(&probe, mc) {
			return nil, nil, standing(cur.Decision)
		}
		probe.WinnerSide = ""
		probe.ClearsWithdrawal = true
		return &probe, nil, nil
	}
	// An overtime change alone moves no verdict a match fought to its end
	// already has.
	if !scored {
		return nil, nil, nil
	}
	// S3: a judges' decision is the result's; points that do not claim the
	// result keep it while the scoreline stays tied.
	if slices.Contains(scoring, state.GroupPoints) {
		carryHantei(cur, &probe)
	}
	if !deriveWinnerAfterMerge(&probe, mc) {
		// R4 holds the scoring; an overtime change beside it says nothing
		// about the winner and applies on its own.
		held := slices.DeleteFunc(slices.Clone(scoring), func(g string) bool { return !state.IsScoringGroup(g) })
		return nil, nil, &BaselineHold{Groups: held, Reason: needsWinnerReason(mc), NeedsWinner: true}
	}
	probe.WinnerSide = ""
	stripUntiedHantei(&probe)
	values := map[string]json.RawMessage{}
	if slices.Contains(scoring, state.GroupPoints) {
		values[state.GroupPoints] = state.GroupValue(&probe, state.GroupPoints)
	}
	return &probe, values, nil
}

// beforeTheFinish reports whether the replay has not reached the line that
// sets the result of a match the cache holds finished: the replay starts
// from the cache (the match after every line), so until a baseline sets the
// result its finished status is a later line's, not this point's.
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
	probe, _ := rp.probeWithPending()
	if out := foldEvent(probe, e, rp.shape, map[string]int64{}); out != domain.ScoreOutcomeApplied {
		rp.outcome[line] = out
		return
	}
	rp.pending = append(rp.pending, line)
	rp.tryCommit()
}

// probeWithPending is the committed match with the pending events folded
// in, the withdrawal's circles taken off when they would clear it.
func (rp *replayer) probeWithPending() (*replayMatch, map[string]int64) {
	probe := rp.cur.clone()
	if rp.clears {
		stripMaru(&probe.m)
	}
	// The stamps the folds set beside their own group (a bout fought again
	// is no longer recorded), merged into the replay's only on a commit.
	stamps := map[string]int64{}
	for _, i := range rp.pending {
		foldEvent(probe, rp.lines[i].Event, rp.shape, stamps)
	}
	return probe, stamps
}

// tryCommit applies the pending events together when they give the finished
// match the result it needs.
func (rp *replayer) tryCommit() {
	probe, probeStamps := rp.probeWithPending()
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
	for g, s := range probeStamps {
		rp.stamps[g] = max(rp.stamps[g], s)
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

// assignMarks names the marks a baseline brings. The marks the board held
// before are matched to the new ones by value, in order (a longest common
// subsequence), so a mark the baseline keeps keeps its id wherever it now
// sits: a finish echoing the board's points, less a mark removed from the
// front, keeps the ids the board removes by. When equal letters compete for
// the ids that survive, the earliest copy keeps its id: the walk matches left
// to right, so [M M] before and [M] after keeps the first M's id and drops the
// second's. A new mark, or one whose earlier copy
// had no id, is b:<line>:<pos>:<side>:<i>; a placeholder slot has none.
func assignMarks(prev, prevMarks, next []string, lineID string, pos int, side string) []string {
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
	for j, v := range next {
		if isMark(v) && out[j] == "" {
			out[j] = "b:" + lineID + ":" + strconv.Itoa(pos) + ":" + side + ":" + strconv.Itoa(j)
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

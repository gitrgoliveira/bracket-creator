// score_log_write.go owns the writes of a match's score event log (bc-evts,
// stage 2a), beside the pure replay in score_replay.go:
//   - RecordScoreEventsTx, the score-events door: it appends the taps it takes,
//     replays the whole log and writes the match the replay derives through the
//     ONE match write (RecordMatchResultWithIneligibilityTx, door "event"), so
//     eligibility, the knockout guards, kachinuki, requalification,
//     propagation and the history stay one path.
//   - noteWriteBaseline: "baselines are total". Once a match's log has begun,
//     every other write that lands on it appends a baseline holding what it
//     set, at the stamp the merge decided, or a later replay would put the
//     match back to what the log alone says.
//   - the drift net (driftBaseline): before replaying, the door compares the
//     replay with the stored match; a difference is a write that left no
//     baseline, logged as an ERROR and healed with a "drift" baseline holding
//     the stored values.
//   - answerLoggedWrite: a write whose writeId the log already holds (its
//     answer was lost) is answered as recorded, with nothing written (F1).
package engine

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// DoorDrift names the baseline the drift net appends.
const DoorDrift = "drift"

// The reasons a refused event carries (ScoreEventAnswer.Reason) that the
// engine itself decides.
const (
	// EventReasonNotStarted: the match has never run.
	EventReasonNotStarted = "match_not_started"
	// EventReasonSentBack: the match ran and was sent back to the queue.
	EventReasonSentBack = "match_sent_back"
	// EventReasonLogUnreadable: the match's score event log cannot be read.
	EventReasonLogUnreadable = "score_log_unreadable"
)

// The sentences the tapping device shows for the engine's own reasons (Q3:
// the device that tapped is told why its tap did nothing).
const (
	EventMessageNotStarted    = "This match has not started yet, so the tap was not scored. Start the match to score it."
	EventMessageSentBack      = "The match was sent back to the queue, so the tap was not scored. Start the match again to score it."
	EventMessageLogUnreadable = "This match's score log cannot be read, so the tap was not saved. The organiser needs to check the tournament data."
)

// EventRefusal is why the door's caller refuses one event (the self-run
// judges): a reason code and the sentence the device shows.
type EventRefusal struct {
	Reason  string
	Message string
}

// ScoreEventsRequest is one batch the score-events door takes.
type ScoreEventsRequest struct {
	// Session is the posting device's event session; every event carries it.
	Session string
	// Events are already shape-checked (domain.ScoreEvent.Validate) and
	// clear of clock skew; the door answers each in this order.
	Events []domain.ScoreEvent
	// Judge, when set, may refuse an event for a reason of the caller's own
	// (a self-run participant's limits), judged on the stored match under
	// the write's lock. A refused event is not logged.
	Judge func(stored *state.MatchResult, comp *state.Competition, ev domain.ScoreEvent) *EventRefusal
}

// ScoreEventAnswer is the door's answer for one event.
type ScoreEventAnswer struct {
	ID      string `json:"id"`
	Outcome string `json:"outcome"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// ScoreEventsResult is what the door did with a batch.
type ScoreEventsResult struct {
	// Match is the match as it now stands (as stored when nothing was
	// written), with a bracket match's court and schedule filled in.
	Match *state.MatchResult
	// Answers holds one answer per event, in the request's order.
	Answers []ScoreEventAnswer
	// Gaps are the posting session's sequence numbers the log is missing
	// (ScoreLogGaps): taps the device made that never arrived.
	Gaps []int64
	// Written reports whether the batch changed the log (and the match).
	Written bool
	// Status is the competitor status a write restored (a withdrawal the
	// events cleared, R2), for the caller to announce.
	Status *domain.CompetitorStatus
	// WriteErr is why the match the replay derived could not be written (a
	// knockout match after this one already played or running, ...): every
	// event the batch would have logged is refused with it, nothing logged.
	WriteErr error
}

// RecordScoreEventsTx is the score-events door, inside the caller's
// transaction. Events whose id the log holds are duplicates; on a match that
// is not running or finished they are notStarted; the caller's Judge refuses
// what it refuses; none of those is logged. The rest are appended, after a
// migration baseline when the log is empty and a drift baseline when the
// stored match differs from the log's replay, and the replay's match is
// written. A batch that logs nothing writes nothing (FileVersion unchanged).
func (e *Engine) RecordScoreEventsTx(tx state.StoreTx, compID, matchID string, req ScoreEventsRequest) (*ScoreEventsResult, error) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %q not found", compID)
	}
	stored, inPool, err := e.lookupExistingResultIn(tx, compID, matchID)
	if err != nil {
		return nil, err
	}
	res := &ScoreEventsResult{Answers: make([]ScoreEventAnswer, len(req.Events))}
	lines, err := tx.LoadScoreLog(compID, matchID)
	if errors.Is(err, state.ErrScoreLogUnreadable) {
		// Never a 5xx: the device's outbox retries those for ever, and this
		// one cannot be cured by retrying. The error names the file and line.
		log.Printf("engine: ERROR: match %s/%s: %v", compID, matchID, err)
		for i, ev := range req.Events {
			res.Answers[i] = ScoreEventAnswer{ID: ev.ID, Outcome: domain.ScoreOutcomeRefused,
				Reason: EventReasonLogUnreadable, Message: EventMessageLogUnreadable}
		}
		res.Match = e.storedForAnswer(tx, compID, matchID, stored, inPool)
		return res, nil
	}
	if err != nil {
		return nil, err
	}

	known := make(map[string]bool, len(lines)+len(req.Events))
	for _, l := range lines {
		if l.Event != nil {
			known[l.Event.ID] = true
		}
	}
	var take []int // indexes of the events to log
	var turnedAway []int
	for i, ev := range req.Events {
		ans := &res.Answers[i]
		ans.ID = ev.ID
		switch {
		case known[ev.ID]:
			ans.Outcome = domain.ScoreOutcomeDuplicate
			continue
		case stored.Status != state.MatchStatusRunning && stored.Status != state.MatchStatusCompleted:
			ans.Outcome = domain.ScoreOutcomeNotStarted
			ans.Reason, ans.Message = EventReasonNotStarted, EventMessageNotStarted
			if len(lines) > 0 || len(stored.GroupStamps) > 0 || stored.ModifiedAt > 0 {
				ans.Reason, ans.Message = EventReasonSentBack, EventMessageSentBack
			}
		default:
			if req.Judge != nil {
				if r := req.Judge(stored, comp, ev); r != nil {
					ans.Outcome, ans.Reason, ans.Message = domain.ScoreOutcomeRefused, r.Reason, r.Message
				}
			}
		}
		known[ev.ID] = true
		if ans.Outcome != "" {
			turnedAway = append(turnedAway, i)
			continue
		}
		take = append(take, i)
	}
	if len(take) == 0 {
		e.recordEventHistory(tx, compID, matchID, req.Events, turnedAway, res.Answers)
		res.Gaps = ScoreLogGaps(lines)[req.Session]
		res.Match = e.storedForAnswer(tx, compID, matchID, stored, inPool)
		return res, nil
	}

	shape := ReplayShape{Comp: comp, Knockout: !inPool}
	now := serverNowMs()
	var added []state.ScoreLogLine
	if len(lines) == 0 {
		// Never an event on an empty log: the stored match becomes its first
		// baseline, at its own group stamps (lazy migration).
		mb := MigrationBaseline(stored, MigrationDoor+"-0", now)
		added = append(added, state.ScoreLogLine{Baseline: &mb})
	} else if drift := driftBaseline(stored, lines, shape, now); drift != nil {
		added = append(added, *drift)
	}
	for _, i := range take {
		ev := req.Events[i]
		added = append(added, state.ScoreLogLine{Event: &ev})
	}
	all := append(slices.Clone(lines), added...)
	var prev Replay
	if len(lines) > 0 {
		prev = ReplayScoring(stored, lines, shape)
	}
	rp := ReplayScoring(stored, all, shape)

	derived := rp.Match
	derived.ClearRequestFields()
	derived.ID = matchID
	derived.WriteDoor = DoorEvent
	derived.ScoreVersion = len(all)
	derived.Changed = groupsReplayChanged(stored, &derived)
	// R2: the events cleared a withdrawal of this match (item 3): only when
	// the stored match still carries it, since the replay reads it on every
	// replay of such a log.
	derived.ClearsWithdrawal = rp.ClearedWithdrawal && domain.IsWithdrawalDecisionStr(stored.Decision)
	status, werr := e.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, &derived)
	if werr != nil {
		res.WriteErr = werr
		for _, i := range take {
			res.Answers[i].Outcome = domain.ScoreOutcomeRefused
		}
		e.recordEventHistory(tx, compID, matchID, req.Events, append(turnedAway, take...), res.Answers)
		res.Gaps = ScoreLogGaps(lines)[req.Session]
		res.Match = e.storedForAnswer(tx, compID, matchID, stored, inPool)
		return res, nil
	}
	if err := tx.AppendScoreLog(compID, matchID, added...); err != nil {
		return nil, err
	}
	res.Written, res.Status = true, status
	for _, i := range take {
		res.Answers[i].Outcome = rp.Outcome(req.Events[i].ID)
	}
	e.recordEventHistory(tx, compID, matchID, req.Events, append(turnedAway, take...), res.Answers)
	// An earlier event whose outcome this batch moved (a later tap gave the
	// match the winner a waiting one needed, R4) gets a line saying so.
	e.recordMovedOutcomes(tx, compID, matchID, lines, prev, rp)
	res.Gaps = ScoreLogGaps(all)[req.Session]
	if len(res.Gaps) > 0 {
		log.Printf("engine: match %s/%s: score events of session %s missing from the log: %v", compID, matchID, req.Session, res.Gaps)
	}
	res.Match = e.storedForAnswer(tx, compID, matchID, nil, inPool)
	return res, nil
}

// RecordScoreEvents is RecordScoreEventsTx in a transaction of its own.
func (e *Engine) RecordScoreEvents(compID, matchID string, req ScoreEventsRequest) (*ScoreEventsResult, error) {
	var res *ScoreEventsResult
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		res, err = e.RecordScoreEventsTx(tx, compID, matchID, req)
		return err
	})
	return res, err
}

// storedForAnswer is the match as the transaction now holds it (prior when
// given), with a bracket match's court and schedule filled in.
func (e *Engine) storedForAnswer(tx state.StoreTx, compID, matchID string, prior *state.MatchResult, inPool bool) *state.MatchResult {
	if prior == nil {
		m, pool, err := e.lookupExistingResultIn(tx, compID, matchID)
		if err != nil {
			log.Printf("engine: match %s/%s: written match not read back: %v", compID, matchID, err)
			return &state.MatchResult{ID: matchID}
		}
		prior, inPool = m, pool
	}
	out := &state.MatchResult{}
	answerAsStored(tx, compID, matchID, prior, inPool, out)
	return out
}

// recordEventHistory appends one door "event" history line for each of the
// events at idx, with the outcome it was answered.
func (e *Engine) recordEventHistory(h state.StoreTx, compID, matchID string, events []domain.ScoreEvent, idx []int, answers []ScoreEventAnswer) {
	slices.Sort(idx)
	for _, i := range idx {
		ans := answers[i]
		if ans.Outcome == domain.ScoreOutcomeDuplicate {
			continue
		}
		e.appendEventLine(h, compID, matchID, events[i], ans.Outcome, ans.Reason)
	}
}

// appendEventLine appends the history line of one event and its outcome.
func (e *Engine) appendEventLine(h state.StoreTx, compID, matchID string, ev domain.ScoreEvent, outcome, reason string) {
	g := ScoreEventGroup(&ev)
	groupOutcome := state.HistoryOutcomeHeld
	if outcome == domain.ScoreOutcomeApplied || outcome == domain.ScoreOutcomeRecorded {
		groupOutcome = state.HistoryOutcomeApplied
	}
	entry := state.MatchHistoryEntry{
		MatchID: matchID, Door: DoorEvent, Stamp: ev.Stamp, ReceivedAt: serverNowMs(),
		Session: ev.Session, Changed: []string{g}, Outcomes: map[string]string{g: groupOutcome},
		Reason: reason, Event: &ev, EventOutcome: outcome,
	}
	if err := h.AppendMatchHistory(compID, entry); err != nil {
		log.Printf("engine: match %s/%s: history entry not recorded: %v", compID, matchID, err)
	}
}

// recordMovedOutcomes appends a line for each event of lines whose outcome
// differs between the replay before (prev) and after (next) the log grew.
func (e *Engine) recordMovedOutcomes(h state.StoreTx, compID, matchID string, lines []state.ScoreLogLine, prev, next Replay) []string {
	was := make(map[string]string, len(prev.Outcomes))
	for _, o := range prev.Outcomes {
		was[o.ID] = o.Outcome
	}
	var moved []string
	seen := map[string]bool{}
	for _, o := range next.Outcomes {
		before, ok := was[o.ID]
		if !ok || before == o.Outcome || o.Line >= len(lines) || seen[o.ID] {
			continue
		}
		seen[o.ID] = true
		moved = append(moved, o.ID)
		e.appendEventLine(h, compID, matchID, *lines[o.Line].Event, o.Outcome, "")
	}
	return moved
}

// writeBaseline describes the baseline a landed non-event write leaves.
type writeBaseline struct {
	door    string
	stamp   int64
	named   []string
	writeID string
	basis   *state.ScoreBasis
}

// baselineNote is what noteWriteBaseline appended: the replayed match (the
// stored one, now with the replay's mark ids and version) and the events the
// baseline replaced without having seen them.
type baselineNote struct {
	match      state.MatchResult
	notApplied []string
}

// recordLandedWriteHistory is recordWriteHistory for a write that LANDED on the
// match, plus its baseline in the match's score event log (noteWriteBaseline).
// Its answer and broadcast take the version and mark ids the log now gives.
func (e *Engine) recordLandedWriteHistory(h state.StoreTx, compID, matchID string, result *state.MatchResult) {
	e.recordWriteHistory(h, compID, matchID, result)
	rep := result.Merge
	if rep == nil || result.WriteDoor == DoorEvent {
		return
	}
	note := e.noteWriteBaseline(h, compID, matchID, writeBaseline{
		door: doorOf(result), stamp: rep.Stamp, named: rep.Applied,
		writeID: result.WriteID, basis: result.Basis,
	})
	if note == nil {
		return
	}
	rep.EventsNotApplied = note.notApplied
	result.ScoreVersion = note.match.ScoreVersion
	takeDerivedMarks(&result.MarkIDs, result.SubResults, &note.match)
}

// noteWriteBaseline appends, to a match whose score event log has begun, the
// baseline a landed non-event write leaves: the groups it named, and every
// group whose stored value the log's replay does not give (the merge's own
// moves: a verdict worked out again, a displaced change, an un-recorded
// bout), at their stored values and stamps. A match with no log is the match
// as stored (lazy migration): nothing is appended. The baseline is placed at
// the write's stamp when the log, replayed with it, gives the stored match,
// else after every line (logged). The stored match then takes the replay's
// mark ids and version, so it is exactly what its log replays to.
//
// basis nil (a server-built write, or a client that sent none) reads as having
// seen every line logged: the write was judged against the match those lines
// made. A failure here never fails the write, which has landed: it is logged.
func (e *Engine) noteWriteBaseline(h state.StoreTx, compID, matchID string, wb writeBaseline) *baselineNote {
	lines, err := h.LoadScoreLog(compID, matchID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: score event log not read, the %s write leaves no baseline: %v", compID, matchID, wb.door, err)
		return nil
	}
	if len(lines) == 0 {
		return nil
	}
	post, inPool, err := e.lookupExistingResultIn(h, compID, matchID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: written match not read for its baseline: %v", compID, matchID, err)
		return nil
	}
	comp, err := h.LoadCompetition(compID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: competition not read for its baseline: %v", compID, matchID, err)
		return nil
	}
	shape := ReplayShape{Comp: comp, Knockout: !inPool}
	before := ReplayScoring(post, lines, shape)
	set := unionGroups(wb.named, replayGroupsDiffer(post, &before.Match))
	if len(set) == 0 {
		return nil
	}
	basis := state.ScoreBasis{Seen: len(lines)}
	if wb.basis != nil {
		basis = *wb.basis
	}
	newest := max(post.ModifiedAt, newestLineStamp(lines))
	stamps := []int64{wb.stamp, newest}
	if wb.stamp <= 0 || wb.stamp >= newest {
		stamps = []int64{max(wb.stamp, newest)}
	}
	var line state.ScoreLogLine
	var after Replay
	for i, s := range stamps {
		bl := BaselineFromMatch(post, fmt.Sprintf("%s-%d", wb.door, len(lines)), wb.door, s, set, basis)
		bl.WriteID, bl.ReceivedAt = wb.writeID, serverNowMs()
		line = state.ScoreLogLine{Baseline: &bl}
		after = ReplayScoring(post, append(slices.Clone(lines), line), shape)
		diff := replayGroupsDiffer(post, &after.Match)
		if len(diff) == 0 {
			break
		}
		if i == len(stamps)-1 {
			log.Printf("engine: ERROR: match %s/%s: the %s write's baseline does not replay to the stored match (groups %v); the drift net will heal it", compID, matchID, wb.door, diff)
		}
	}
	if err := h.AppendScoreLog(compID, matchID, line); err != nil {
		log.Printf("engine: ERROR: match %s/%s: the %s write's baseline not logged: %v", compID, matchID, wb.door, err)
		return nil
	}
	after.Match.ScoreVersion = len(lines) + 1
	e.takeReplayDerived(h, compID, matchID, inPool, &after.Match)
	note := &baselineNote{match: after.Match}
	for _, id := range e.recordMovedOutcomes(h, compID, matchID, lines, before, after) {
		if after.Outcome(id) == domain.ScoreOutcomeBeforeBaseline {
			note.notApplied = append(note.notApplied, id)
		}
	}
	return note
}

// driftBaseline is the drift net: when the stored match is not what its log
// replays to (a write that left no baseline), the stored values of the groups
// that differ become a "drift" baseline at their own stamps, and the ERROR is
// logged. nil when they agree.
func driftBaseline(stored *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape, now int64) *state.ScoreLogLine {
	rp := ReplayScoring(stored, lines, shape)
	diff := replayGroupsDiffer(stored, &rp.Match)
	if len(diff) == 0 {
		return nil
	}
	log.Printf("engine: ERROR: match %s: the stored match differs from its score event log in %v (a write left no baseline); logging a drift baseline", stored.ID, diff)
	stamp := max(stored.ModifiedAt, newestLineStamp(lines))
	b := BaselineFromMatch(stored, fmt.Sprintf("%s-%d", DoorDrift, len(lines)), DoorDrift, stamp, diff, state.ScoreBasis{Seen: len(lines)})
	b.ReceivedAt = now
	return &state.ScoreLogLine{Baseline: &b}
}

// takeReplayDerived stores the replay's mark ids and version on the match
// (no merge: nothing else of it moves).
func (e *Engine) takeReplayDerived(h state.StoreTx, compID, matchID string, inPool bool, derived *state.MatchResult) {
	var err error
	if inPool {
		_, err = h.UpdatePoolMatchByID(compID, matchID, func(r *state.MatchResult) error {
			r.ScoreVersion = derived.ScoreVersion
			takeDerivedMarks(&r.MarkIDs, r.SubResults, derived)
			return nil
		})
	} else {
		_, err = h.UpdateBracketMatchByID(compID, matchID, func(bm *state.BracketMatch) {
			bm.ScoreVersion = derived.ScoreVersion
			takeDerivedMarks(&bm.MarkIDs, bm.SubResults, derived)
		})
	}
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: score version not stored: %v", compID, matchID, err)
	}
}

// takeDerivedMarks copies derived's mark ids onto a match's own and its rows'.
func takeDerivedMarks(marks **state.MarkIDs, subs []state.SubMatchResult, derived *state.MatchResult) {
	*marks = derived.MarkIDs.Clone()
	for i := range subs {
		if row := state.SubResultAt(derived.SubResults, subs[i].Position); row.Position == subs[i].Position {
			subs[i].MarkIDs = row.MarkIDs.Clone()
		} else {
			subs[i].MarkIDs = nil
		}
	}
}

// answerLoggedWrite answers a write whose writeId the match's score event log
// already holds as recorded: the stored match, nothing written, no history
// line for anyone (bc-evts F1, Q6 b). A match with no log falls back to the
// exact-resend probe (answerExactScoreReplay).
func (e *Engine) answerLoggedWrite(tx state.StoreTx, compID, matchID string, result *state.MatchResult) (bool, error) {
	if result.WriteID == "" {
		return false, nil
	}
	lines, err := tx.LoadScoreLog(compID, matchID)
	if err != nil {
		// The write is judged as any other; its baseline will not be logged
		// either, which noteWriteBaseline reports.
		log.Printf("engine: ERROR: match %s/%s: score event log not read for a resend check: %v", compID, matchID, err)
		return false, nil
	}
	if !slices.ContainsFunc(lines, func(l state.ScoreLogLine) bool {
		return l.Baseline != nil && l.Baseline.WriteID == result.WriteID
	}) {
		return false, nil
	}
	prior, inPool, err := e.lookupExistingResultIn(tx, compID, matchID)
	if err != nil {
		return false, err
	}
	answerAsStored(tx, compID, matchID, prior, inPool, result)
	return true, nil
}

// replayedGroups is every group of a and b: the scalar and server groups and
// each bout either holds.
func replayedGroups(a, b *state.MatchResult) []string {
	groups := append(slices.Clone(state.ScalarGroups), state.ServerGroups...)
	for _, m := range []*state.MatchResult{a, b} {
		for _, p := range state.SubPositions(m.SubResults) {
			if g := state.BoutGroup(p); !slices.Contains(groups, g) {
				groups = append(groups, g)
			}
		}
	}
	return groups
}

// replayGroupsDiffer lists the groups whose values differ between a and b.
func replayGroupsDiffer(a, b *state.MatchResult) []string {
	var out []string
	for _, g := range replayedGroups(a, b) {
		if !bytes.Equal(state.GroupValue(a, g), state.GroupValue(b, g)) {
			out = append(out, g)
		}
	}
	return out
}

// groupsReplayChanged lists the groups the derived match changes against the
// stored one: by value or by stamp.
func groupsReplayChanged(stored, derived *state.MatchResult) []string {
	changed := []string{}
	for _, g := range replayedGroups(stored, derived) {
		if !bytes.Equal(state.GroupValue(stored, g), state.GroupValue(derived, g)) ||
			stored.GroupStamp(g) != derived.GroupStamp(g) {
			changed = append(changed, g)
		}
	}
	return changed
}

// unionGroups is a's groups then b's that a lacks, valid groups only.
func unionGroups(a, b []string) []string {
	var out []string
	for _, g := range append(slices.Clone(a), b...) {
		if state.ValidGroup(g) && !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// newestLineStamp is the latest stamp of any line.
func newestLineStamp(lines []state.ScoreLogLine) int64 {
	var newest int64
	for _, l := range lines {
		newest = max(newest, l.Stamp())
	}
	return newest
}

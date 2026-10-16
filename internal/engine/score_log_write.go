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
//     the stored values. A difference of ids alone, which a load-time repair
//     fills on a match the log already covers, is healed the same way with a
//     "repair" baseline, logged quietly.
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

// DoorDrift names the baseline the drift net appends; DoorRepair the one it
// appends when the only difference is ids a load-time repair filled.
const (
	DoorDrift  = "drift"
	DoorRepair = "repair"
)

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
	EventMessageNotRunning    = "This match is not running, so the tap was not scored. Start the match to score it."
	EventMessageLogUnreadable = "This match's score log cannot be read, so this tap was not applied; it is kept in the match history. The organiser needs to check the tournament data."
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
	// NoTurnedAwayHistory leaves out the history line of an event turned away
	// (refused, not started, or the write refused): a turned-away event never
	// enters the log, so an anonymous self-run caller could otherwise loop a
	// resend to grow the match's history file. The organiser's turned-away
	// events keep one line per event id.
	NoTurnedAwayHistory bool
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
	// Status is the competitor status a write restored, for the caller to
	// announce.
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
		turnedAway := make([]int, 0, len(req.Events))
		for i, ev := range req.Events {
			res.Answers[i] = ScoreEventAnswer{ID: ev.ID, Outcome: domain.ScoreOutcomeRefused,
				Reason: EventReasonLogUnreadable, Message: EventMessageLogUnreadable}
			turnedAway = append(turnedAway, i)
		}
		// The history is a separate file: the taps are kept there even though
		// the log that would hold them is not readable.
		e.recordEventHistory(tx, compID, matchID, req.Events, e.unrecordedTurnedAway(tx, compID, matchID, req, turnedAway, res.Answers), res.Answers)
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
	notRunning := stored.Status != state.MatchStatusRunning && stored.Status != state.MatchStatusCompleted
	// Whether the match was sent back is a property of the request's match, not
	// of an event: read its history once, and only when an event can need it.
	sentBack := notRunning && e.sentBackToQueue(tx, compID, matchID)
	var take []int // indexes of the events to log
	var turnedAway []int
	for i, ev := range req.Events {
		ans := &res.Answers[i]
		ans.ID = ev.ID
		switch {
		case known[ev.ID]:
			ans.Outcome = domain.ScoreOutcomeDuplicate
			continue
		case notRunning:
			ans.Outcome = domain.ScoreOutcomeNotStarted
			ans.Reason, ans.Message = EventReasonNotStarted, EventMessageNotStarted
			switch {
			case sentBack:
				ans.Reason, ans.Message = EventReasonSentBack, EventMessageSentBack
			case len(lines) > 0 || len(stored.GroupStamps) > 0 || stored.ModifiedAt > 0:
				// It ran, and is not running now, but nobody sent it back.
				ans.Message = EventMessageNotRunning
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
		e.recordEventHistory(tx, compID, matchID, req.Events, e.unrecordedTurnedAway(tx, compID, matchID, req, turnedAway, res.Answers), res.Answers)
		res.Gaps = ScoreLogGaps(lines)[req.Session]
		res.Match = e.storedForAnswer(tx, compID, matchID, stored, inPool)
		return res, nil
	}

	shape := ReplayShape{Comp: comp, Knockout: !inPool}
	now := serverNowMs()
	// Never an event on an empty log: the stored match becomes its first
	// baseline, at its own group stamps (lazy migration); a drift or repair
	// line is logged ahead of the events (replayLines, the one owner).
	_, prefix, _ := replayLines(stored, lines, shape, now, true)
	added := slices.Clone(prefix)
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
	status, werr := e.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, &derived)
	if werr != nil {
		res.WriteErr = werr
		// The refusal code is on the answer before the history line is
		// written from it, so the stored line says why the tap changed
		// nothing (a fault has no code to give, and its transaction aborts).
		reason, message, _ := EventWriteRefusal(werr)
		for _, i := range take {
			res.Answers[i].Outcome = domain.ScoreOutcomeRefused
			res.Answers[i].Reason, res.Answers[i].Message = reason, message
		}
		e.recordEventHistory(tx, compID, matchID, req.Events, e.unrecordedTurnedAway(tx, compID, matchID, req, append(turnedAway, take...), res.Answers), res.Answers)
		res.Gaps = ScoreLogGaps(lines)[req.Session]
		res.Match = e.storedForAnswer(tx, compID, matchID, stored, inPool)
		return res, nil
	}
	if err := tx.AppendScoreLog(compID, matchID, added...); err != nil {
		return nil, err
	}
	res.Written, res.Status = true, status
	for _, i := range take {
		ans := &res.Answers[i]
		ans.Outcome = rp.Outcome(req.Events[i].ID)
		if ans.Outcome == domain.ScoreOutcomeMatchClosed {
			// The closed-match rule: told the same words the score door uses.
			ans.Reason, ans.Message = state.HeldReasonMatchClosed, MatchClosedMessage
		}
	}
	e.recordEventHistory(tx, compID, matchID, req.Events, append(e.unrecordedTurnedAway(tx, compID, matchID, req, turnedAway, res.Answers), take...), res.Answers)
	// An earlier event whose outcome this batch moved gets a line saying so.
	e.recordMovedOutcomes(tx, compID, matchID, lines, prev, rp, "")
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
// EventWriteRefusal reads why the match the replay derived could not be
// written as a refusal the device is told about (the reason code and the
// sentence), or false for a fault (the request then answers 500). The score
// events door answers with it, and the history line kept for the tap carries
// the same code.
func EventWriteRefusal(err error) (reason, message string, ok bool) {
	var played *DownstreamKnockoutPlayedError
	var running *DownstreamKnockoutRunningError
	var verr *ValidationError
	var already *AlreadyIneligibleError
	var inelig *IneligibleCompetitorError
	switch {
	case errors.As(err, &played):
		return "downstream_knockout_played", played.Error(), true
	case errors.As(err, &running):
		return "downstream_knockout_running", running.Error(), true
	case errors.As(err, &already):
		return "already_ineligible", already.Error(), true
	case errors.As(err, &inelig):
		return "ineligible_competitor", inelig.Error(), true
	case errors.Is(err, ErrMatchSideMismatch):
		return "side_mismatch", "The competitors of this match changed. Close the score sheet and open it again.", true
	case errors.As(err, &verr):
		return "invalid", verr.Error(), true
	}
	return "", "", false
}

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
func (e *Engine) recordMovedOutcomes(h state.StoreTx, compID, matchID string, lines []state.ScoreLogLine, prev, next Replay, reason string) []string {
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
		why := reason
		if o.Outcome == domain.ScoreOutcomeMatchClosed {
			// A tap a later-arriving finish closes out reads as the same
			// refusal as one that arrived after it, whatever write moved it.
			why = state.HeldReasonMatchClosed
		}
		e.appendEventLine(h, compID, matchID, *lines[o.Line].Event, o.Outcome, why)
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
	// prefix are the lines the replay that decided the write needed ahead of
	// its baseline (MergeReport.LogPrefix): logged first, so a scoring
	// snapshot always leaves the log it was decided on.
	prefix []state.ScoreLogLine
	// revSession and rev are a running write's revision (MatchResult.Rev),
	// kept on its baseline for the older-revision hold.
	revSession string
	rev        int64
	// sent is the baseline the replay judged for the write
	// (MergeReport.Baseline): logged as it was made, whatever became of it.
	sent *state.ScoreBaseline
	// prior is the match as it was stored before the write
	// (MergeReport.Prior): the outcomes the earlier events had then are the
	// ones a move is recorded against. Nil reads them off the written match.
	prior *state.MatchResult
	// replay is the merge's own read and replay of the log
	// (MergeReport.Replay); nil reads the log and replays it here.
	replay *mergeReplay
}

// baselineNote is what noteWriteBaseline appended: the replayed match (the
// stored one, now with the replay's mark ids and version) and the events the
// baseline replaced without having seen them.
type baselineNote struct {
	match      state.MatchResult
	notApplied []string
}

// mergeReplay is what mergeByReplay read and replayed (MergeReport.Replay),
// for noteWriteBaseline to reuse instead of reading and replaying the log
// again (bc-evts review round 12).
type mergeReplay struct {
	// found is the log as the merge read it.
	found []state.ScoreLogLine
	// lines counts the lines the write was replayed on (found and the
	// prefix replayLines appended).
	lines int
	// after is the replay of those lines with the write's baseline.
	after Replay
}

func newMergeReplay(found []state.ScoreLogLine, lines int, after Replay) *mergeReplay {
	kept := after
	kept.Match = cloneMatchForReplay(after.Match)
	return &mergeReplay{found: found, lines: lines, after: kept}
}

// mergeReplayOf is the merge's replay a report carries, nil when it has none.
func mergeReplayOf(rep *state.MergeReport) *mergeReplay {
	if rep == nil {
		return nil
	}
	mr, _ := rep.Replay.(*mergeReplay)
	return mr
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
		prefix: rep.LogPrefix, revSession: result.RevSession, rev: result.Rev,
		sent: sentBaseline(rep), prior: rep.Prior, replay: mergeReplayOf(rep),
	})
	if note == nil {
		return
	}
	rep.EventsNotApplied = note.notApplied
	result.ScoreVersion = note.match.ScoreVersion
	takeDerivedMarks(&result.MarkIDs, result.SubResults, &note.match)
}

// recordSupersededWriteHistory is recordWriteHistory for a write the merge
// held whole. A result the replay turned away (one older than the stored
// result) still leaves its baseline in the score log, as it was sent: it
// closes the match for everything stamped after it.
func (e *Engine) recordSupersededWriteHistory(h state.StoreTx, compID, matchID string, result *state.MatchResult) {
	if result.Merge != nil && result.Merge.ResultLocked != "" {
		// Held by the result lock (K1): kept once, whatever the resends.
		e.recordLockedHoldHistory(h, compID, matchID, result)
		return
	}
	e.recordWriteHistory(h, compID, matchID, result)
	rep := result.Merge
	if rep == nil || result.WriteDoor == DoorEvent || sentBaseline(rep) == nil {
		return
	}
	// A result the replay turned away (one older than the stored result) is
	// still a line of the log: it closes the match for everything stamped
	// after it, and a newer result stands on it. A scoring write turned away
	// leaves its history entry alone, and no footprint in the match file.
	if _, sets := rep.Baseline.Values[state.GroupResult]; !sets {
		return
	}
	// A match whose log has not begun has nothing the turned-away result could
	// close: the stored match is its only baseline, and the write leaves no
	// footprint in the match file.
	mr := mergeReplayOf(rep)
	if mr == nil || len(mr.found) == 0 {
		if lines, err := h.LoadScoreLog(compID, matchID); err != nil || len(lines) == 0 {
			return
		}
	}
	e.noteWriteBaseline(h, compID, matchID, writeBaseline{
		door: doorOf(result), stamp: rep.Stamp, writeID: result.WriteID, basis: result.Basis,
		prefix: rep.LogPrefix, revSession: result.RevSession, rev: result.Rev, sent: sentBaseline(rep),
		replay: mr,
	})
}

// sentBaseline is the baseline the replay judged for a write, nil for a write
// the merge's own holds decided or one that never reached the replay.
func sentBaseline(rep *state.MergeReport) *state.ScoreBaseline {
	if rep == nil || !rep.Replayed {
		return nil
	}
	return rep.Baseline
}

// heldToLog are the groups of a write the replay judged and held, with the
// values it sent: they are logged at the write's stamp beside the groups it
// applied, so the log is the write as it was made and the replay orders it.
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
	var lines []state.ScoreLogLine
	if wb.replay != nil && len(wb.replay.found) > 0 {
		// The log the merge read under this write's lock: a log it could not
		// read reached it as none, so only a log with lines is taken from it.
		lines = slices.Clone(wb.replay.found)
	} else {
		var err error
		if lines, err = h.LoadScoreLog(compID, matchID); err != nil {
			log.Printf("engine: ERROR: match %s/%s: score event log not read, the %s write leaves no baseline: %v", compID, matchID, wb.door, err)
			return nil
		}
	}
	if len(wb.prefix) > 0 {
		if err := h.AppendScoreLog(compID, matchID, wb.prefix...); err != nil {
			log.Printf("engine: ERROR: match %s/%s: the lines the %s write was replayed on not logged: %v", compID, matchID, wb.door, err)
			return nil
		}
		lines = append(lines, wb.prefix...)
	}
	if len(lines) == 0 && wb.writeID == "" {
		return nil
	}
	// A judged write that says nothing (a board's echo of what the match
	// holds, naming no group) leaves no line, unless it must be found again
	// (its id) or orders its board's revisions.
	if wb.sent != nil && len(wb.sent.Values) == 0 && wb.writeID == "" &&
		(wb.rev <= 0 || wb.revSession == "" || olderRevisionThanLogged(lines, wb.revSession, wb.rev-1)) {
		return nil
	}
	post, inPool, err := e.lookupExistingResultIn(h, compID, matchID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: written match not read for its baseline: %v", compID, matchID, err)
		return nil
	}
	if len(lines) == 0 {
		// A write with an id a resend must find begins the log: the match
		// as written is its first baseline, and the write's own follows.
		mb := MigrationBaseline(post, MigrationDoor+"-0", serverNowMs())
		first := state.ScoreLogLine{Baseline: &mb}
		if err := h.AppendScoreLog(compID, matchID, first); err != nil {
			log.Printf("engine: ERROR: match %s/%s: the %s write's log not begun: %v", compID, matchID, wb.door, err)
			return nil
		}
		lines = []state.ScoreLogLine{first}
	}
	comp, err := h.LoadCompetition(compID)
	if err != nil {
		log.Printf("engine: ERROR: match %s/%s: competition not read for its baseline: %v", compID, matchID, err)
		return nil
	}
	shape := ReplayShape{Comp: comp, Knockout: !inPool}
	// The log before the write's line, read only where it is used: the groups
	// an unjudged write sets, and the outcomes a move is recorded against
	// when no prior was kept.
	var before Replay
	if wb.sent == nil || wb.prior == nil {
		before = ReplayScoring(post, lines, shape)
	}
	movedReason := ""
	basis := state.ScoreBasis{Seen: len(lines)}
	if wb.basis != nil {
		basis = *wb.basis
	} else if wb.door == DoorScore || wb.door == DoorBulkScore {
		// A write the client built says nothing of the events it saw, so none
		// is called recorded: Seen is unknown, not "all of them".
		basis = state.ScoreBasis{}
		movedReason = ReasonNoBasis
	}
	var line state.ScoreLogLine
	var after Replay
	if wb.sent != nil {
		// A write the replay judged is logged as it was made.
		bl := *wb.sent
		bl.ID = logBaselineID(len(lines), wb.door)
		bl.WriteID, bl.ReceivedAt = wb.writeID, serverNowMs()
		if wb.rev > 0 && wb.revSession != "" {
			bl.RevSession, bl.Rev = wb.revSession, wb.rev
		}
		line = state.ScoreLogLine{Baseline: &bl}
		if mr := wb.replay; mr != nil && mr.lines == len(lines) && len(replayGroupsDiffer(post, &mr.after.Match)) == 0 {
			// The merge replayed these same lines with this same baseline
			// (its id, write id and revision aside, which the replay does not
			// read), and the match as stored is what it gave. The merge's replay is told
			// the log's length (withLogLines) and a fresh uncapped one is not, yet they
			// agree: the merge clamps the incoming basis (match_merge.go) and a logged
			// line's Seen is bounded by the log's length when it is written.
			after = mr.after
			after.Match = cloneMatchForReplay(mr.after.Match)
		} else {
			after = ReplayScoring(post, append(slices.Clone(lines), line), shape)
		}
		if diff := replayGroupsDiffer(post, &after.Match); len(diff) > 0 {
			// The write pipeline still shapes what it stores after the merge
			// (a hantei mark a running match cannot hold is dropped): a group
			// the baseline sets takes the value that was stored, so the log
			// replays to the stored match.
			for _, g := range diff {
				if _, ok := bl.Values[g]; ok {
					bl.Values[g] = state.GroupValue(post, g)
				}
			}
			after = ReplayScoring(post, append(slices.Clone(lines), line), shape)
			if diff = replayGroupsDiffer(post, &after.Match); len(diff) > 0 {
				log.Printf("engine: ERROR: match %s/%s: the %s write's baseline does not replay to the stored match (groups %v); the drift net will heal it", compID, matchID, wb.door, diff)
			}
		}
	} else {
		named := wb.named
		set := unionGroups(named, replayGroupsDiffer(post, &before.Match))
		// A running write that changed nothing still leaves its board's
		// revision, so an older revision of that board arriving later is held
		// (olderRevisionThanLogged) rather than put back over the value it
		// echoed.
		revMarker := wb.rev > 0 && wb.revSession != "" && !olderRevisionThanLogged(lines, wb.revSession, wb.rev-1)
		if len(set) == 0 && !revMarker {
			return nil
		}
		// First at the write's own stamps (each group at the stamp the merge
		// left it); a baseline that does not replay there (a direct writer
		// that kept an older stamp on a group it changed) is placed after
		// every line.
		newest := max(post.ModifiedAt, newestLineStamp(lines), wb.stamp)
		variants := []string{"plain", "newest"}
		if directResultWrite(wb.door) {
			// A reopen or a requeue is placed at its own stamp and never bent
			// to a later one: a tap stamped after it (a device clock running
			// ahead of the server's) follows it in every arrival order, so
			// the match it gives is the replay's, taken below.
			variants = variants[:1]
		}
		for n, variant := range variants {
			vset := set
			if variant == "newest" && post.Status == state.MatchStatusCompleted && !slices.Contains(set, state.GroupResult) &&
				slices.ContainsFunc(set, func(g string) bool { return state.IsScoringGroup(g) || g == state.GroupEncho }) {
				// Scoring that lands on a finished match without moving its
				// verdict (a correction under a ruling it keeps) is logged with
				// that verdict, so no replay judges it again as a later change.
				vset = append(slices.Clone(set), state.GroupResult)
			}
			bl := BaselineFromMatch(post, logBaselineID(len(lines), wb.door), wb.door, wb.stamp, vset, basis)
			if variant == "newest" || wb.stamp <= 0 {
				bl.Stamp = newest
				for g := range bl.Stamps {
					bl.Stamps[g] = newest
				}
			}
			bl.WriteID, bl.ReceivedAt = wb.writeID, serverNowMs()
			if wb.rev > 0 && wb.revSession != "" {
				bl.RevSession, bl.Rev = wb.revSession, wb.rev
			}
			line = state.ScoreLogLine{Baseline: &bl}
			after = ReplayScoring(post, append(slices.Clone(lines), line), shape)
			diff := replayGroupsDiffer(post, &after.Match)
			if len(diff) == 0 {
				break
			}
			if n == len(variants)-1 && !directResultWrite(wb.door) {
				log.Printf("engine: ERROR: match %s/%s: the %s write's baseline does not replay to the stored match (groups %v); the drift net will heal it", compID, matchID, wb.door, diff)
			}
		}
	}
	if err := h.AppendScoreLog(compID, matchID, line); err != nil {
		log.Printf("engine: ERROR: match %s/%s: the %s write's baseline not logged: %v", compID, matchID, wb.door, err)
		return nil
	}
	after.Match.ScoreVersion = len(lines) + 1
	if directResultWrite(wb.door) && wb.sent == nil && len(replayGroupsDiffer(post, &after.Match)) > 0 {
		// The log orders a later-stamped tap after this write, which the
		// writer could not know: the match is the replay's, written as an
		// event's derived write is.
		derived := after.Match
		derived.ClearRequestFields()
		derived.ID = matchID
		derived.WriteDoor = DoorEvent
		derived.Changed = groupsReplayChanged(post, &derived)
		if _, err := e.RecordMatchResultWithIneligibilityTx(h, compID, matchID, &derived); err != nil {
			log.Printf("engine: ERROR: match %s/%s: the %s write's replay not stored: %v", compID, matchID, wb.door, err)
		}
	} else {
		e.takeReplayDerived(h, compID, matchID, inPool, &after.Match)
	}
	note := &baselineNote{match: after.Match}
	was := before
	if wb.prior != nil {
		// The outcomes at the earlier events' own arrival: a bout row the
		// write pads is not there yet for a tap that came first.
		was = ReplayScoring(wb.prior, lines, shape)
	}
	for _, id := range e.recordMovedOutcomes(h, compID, matchID, lines, was, after, movedReason) {
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
	line, diff, _ := driftOf(stored, lines, shape, now)
	logDrift(stored, line, diff)
	return line
}

// driftOf is driftBaseline without the log line: the drift or repair line
// (nil when the stored match is what lines replay to), the groups it carries,
// and the replayer over exactly lines, which a caller asking what result is
// in force at the end of the log reads instead of replaying again.
func driftOf(stored *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape, now int64) (*state.ScoreLogLine, []string, *replayer) {
	rp, found := replayScoring(stored, lines, shape, nil)
	diff := replayGroupsDiffer(stored, &rp.Match)
	if len(diff) == 0 {
		return nil, nil, found
	}
	// A load-time repair (EnsureLegacyUpgraded: a winner's id, a bout's member
	// ids) fills ids on a row the log already covers, in memory and again after
	// every restart. That is no write that skipped its baseline: the log says
	// the same thing without the ids, so the stored ids are logged as a repair
	// baseline, quietly, and drift stays a real fault.
	door := DoorRepair
	for _, g := range diff {
		if !repairFillsIDsOnly(stored, &rp.Match, g) {
			door = DoorDrift
			break
		}
	}
	stamp := max(stored.ModifiedAt, newestLineStamp(lines))
	b := BaselineFromMatch(stored, logBaselineID(len(lines), door), door, stamp, diff, state.ScoreBasis{Seen: len(lines)})
	b.ReceivedAt = now
	return &state.ScoreLogLine{Baseline: &b}, diff, nil
}

// logDrift says why a drift or repair line was minted; nothing for nil.
func logDrift(stored *state.MatchResult, line *state.ScoreLogLine, diff []string) {
	switch {
	case line == nil:
	case line.Baseline.Door == DoorDrift:
		log.Printf("engine: ERROR: match %s: the stored match differs from its score event log in %v (a write left no baseline); logging a drift baseline", stored.ID, diff)
	default:
		log.Printf("engine: match %s: a repair filled ids the score event log lacks in %v; logging a repair baseline", stored.ID, diff)
	}
}

// replayLines is the ONE owner of the lines a write's replay begins from: the
// log as found, with the stored match as its migration baseline when the log
// is empty, else with the drift or repair line appended when the stored match
// is not what the log replays to (bc-evts review round 11: the /score
// handler's reason check and the padding gate read the raw log while the merge
// read this one, so a repair line made them disagree). prefix is what the
// door logs ahead of the write (nil: nothing); found is the replayer over
// exactly lines when nothing was added (nil otherwise), so a caller asking
// what result is in force at the end of the log need not replay again.
// logIt says the drift or repair once, from the write that logs it.
func replayLines(stored *state.MatchResult, lines []state.ScoreLogLine, shape ReplayShape, now int64, logIt bool) (all, prefix []state.ScoreLogLine, found *replayer) {
	if len(lines) == 0 {
		mb := MigrationBaseline(stored, MigrationDoor+"-0", now)
		prefix = []state.ScoreLogLine{{Baseline: &mb}}
		return prefix, prefix, nil
	}
	line, diff, found := driftOf(stored, lines, shape, now)
	if line == nil {
		return lines, nil, found
	}
	if logIt {
		logDrift(stored, line, diff)
	}
	return append(slices.Clone(lines), *line), []state.ScoreLogLine{*line}, nil
}

// winnerSideIDs lists the side ids the match's winner can legitimately carry:
// the id of the side the winner names (its side hint, else its name), both
// when the two sides share a name.
func winnerSideIDs(m *state.MatchResult) []string {
	switch m.WinnerSide {
	case "A":
		return []string{m.SideAID}
	case "B":
		return []string{m.SideBID}
	}
	var ids []string
	if m.Winner == m.SideA {
		ids = append(ids, m.SideAID)
	}
	if m.Winner == m.SideB {
		ids = append(ids, m.SideBID)
	}
	return ids
}

// repairFillsIDsOnly reports whether the stored group differs from the
// replayed one only in ids the stored match holds and the replay lacks: a
// result's winner id, a bout's member ids. An id the replay holds and the
// stored match changed is no repair.
func repairFillsIDsOnly(stored, replayed *state.MatchResult, g string) bool {
	a, b := cloneMatchForReplay(*stored), cloneMatchForReplay(*replayed)
	if g == state.GroupResult {
		if b.WinnerID != "" && b.WinnerID != a.WinnerID {
			return false
		}
		// The id a repair filled must be the winning side's own id.
		if a.WinnerID != "" && !slices.Contains(winnerSideIDs(&a), a.WinnerID) {
			return false
		}
		a.WinnerID, b.WinnerID = "", ""
	}
	if pos, ok := state.ParseBoutGroup(g); ok {
		for i := range b.SubResults {
			if b.SubResults[i].Position != pos {
				continue
			}
			for j := range a.SubResults {
				if a.SubResults[j].Position != pos {
					continue
				}
				ra, rb := &a.SubResults[j], &b.SubResults[i]
				// A filled winner must be one of the row's two fighters, and
				// the fighters of the two sides must differ.
				if ra.WinnerMemberID != "" && ra.WinnerMemberID != ra.SideAMemberID && ra.WinnerMemberID != ra.SideBMemberID {
					return false
				}
				if ra.SideAMemberID != "" && ra.SideAMemberID == ra.SideBMemberID {
					return false
				}
				for _, ids := range [][2]*string{{&ra.SideAMemberID, &rb.SideAMemberID}, {&ra.SideBMemberID, &rb.SideBMemberID}, {&ra.WinnerMemberID, &rb.WinnerMemberID}} {
					if *ids[1] != "" && *ids[1] != *ids[0] {
						return false
					}
					*ids[0], *ids[1] = "", ""
				}
			}
		}
	}
	return bytes.Equal(state.GroupValue(&a, g), state.GroupValue(&b, g))
}

// logBaselineID names a baseline a door logs at line: two baselines dated at
// the same moment replay in the order they were logged (the id breaks the
// tie, compareReplayItems), after the match's migration baseline.
func logBaselineID(line int, door string) string {
	return fmt.Sprintf("w%06d-%s", line, door)
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
	if !e.loggedWriteID(tx, compID, matchID, result.WriteID) {
		return false, nil
	}
	prior, inPool, err := e.lookupExistingResultIn(tx, compID, matchID)
	if err != nil {
		return false, err
	}
	answerAsStored(tx, compID, matchID, prior, inPool, result)
	return true, nil
}

// loggedWriteID reports whether the match's score event log holds a baseline
// with this write id (bc-evts F1): the write was recorded, so a resend whose
// answer was lost is answered as recorded, with nothing written and no
// history line (Q6). An empty id is never logged.
func (e *Engine) loggedWriteID(h state.StoreTx, compID, matchID, writeID string) bool {
	if writeID == "" {
		return false
	}
	lines, err := h.LoadScoreLog(compID, matchID)
	if err != nil {
		// The write is judged as any other; its baseline will not be logged
		// either, which noteWriteBaseline reports.
		log.Printf("engine: ERROR: match %s/%s: score event log not read for a resend check: %v", compID, matchID, err)
		return false
	}
	return slices.ContainsFunc(lines, func(l state.ScoreLogLine) bool {
		return l.Baseline != nil && l.Baseline.WriteID == writeID
	})
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

// ReasonNoBasis is the history reason of an older event a client-built write
// put before its baseline because the write named no basis.
const ReasonNoBasis = "no basis"

// unrecordedTurnedAway returns the events (idx) that still need a history line
// for being turned away. An anonymous caller's get none; the organiser's get
// one per event id, so a resend of an id the history already holds adds
// nothing.
func (e *Engine) unrecordedTurnedAway(h state.StoreTx, compID, matchID string, req ScoreEventsRequest, idx []int, answers []ScoreEventAnswer) []int {
	if req.NoTurnedAwayHistory || len(idx) == 0 {
		return nil
	}
	entries, err := h.LoadMatchHistory(compID, matchID)
	if err != nil {
		log.Printf("engine: match %s/%s: history not read, turned-away events not recorded: %v", compID, matchID, err)
		return nil
	}
	// A turn-away is kept once per event id AND answer: the same event turned
	// away for a new reason (first not started, later sent back) is news to the
	// organiser, while an exact resend would only grow the file.
	type answered struct{ id, outcome, reason string }
	held := make(map[answered]bool, len(entries))
	for _, en := range entries {
		if en.Event != nil {
			held[answered{en.Event.ID, en.EventOutcome, en.Reason}] = true
		}
	}
	var out []int
	for _, i := range idx {
		key := answered{req.Events[i].ID, answers[i].Outcome, answers[i].Reason}
		if held[key] {
			continue
		}
		held[key] = true
		out = append(out, i)
	}
	return out
}

// sentBackToQueue says whether the match's latest write, apart from the taps
// turned away at this door, was the operator's send back to the queue. Lines
// whose result group was not applied are skipped.
func (e *Engine) sentBackToQueue(h state.StoreTx, compID, matchID string) bool {
	entries, err := h.LoadMatchHistory(compID, matchID)
	if err != nil {
		return false
	}
	for i := len(entries) - 1; i >= 0; i-- {
		// A line moves the status only when its door can AND its result group was
		// applied: a held or superseded line (a stale running autosave) changed
		// nothing, so it says nothing about how the match came to its status.
		if o, named := entries[i].Outcomes[state.GroupResult]; !doorChangesStatus(entries[i].Door) || (named && o != state.HistoryOutcomeApplied) {
			continue
		}
		return entries[i].Door == doorRequeue
	}
	return false
}

// directResultWrite reports whether a baseline is a reopen or a requeue: a
// direct write of the match's result that the server stamps itself.
func directResultWrite(door string) bool {
	return door == doorReopen || door == doorRequeue
}

// doorChangesStatus reports whether a history line from this door can have moved
// the match between scheduled, running and completed. The lines that cannot (a
// tap, a drift baseline, the migration, a displaced or reseated or downstream
// line, a kachinuki bout step) say nothing about HOW the match came to its
// status, so the latest line that can is the one that does.
func doorChangesStatus(door string) bool {
	switch door {
	case DoorEvent, DoorDrift, DoorRepair, MigrationDoor, DoorDisplaced, doorReseat, doorDownstreamReopen,
		doorKachinukiAdvance, doorKachinukiRecord, doorKachinukiRemove:
		return false
	}
	return true
}

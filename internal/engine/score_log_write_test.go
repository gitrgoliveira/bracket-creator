package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const (
	slwComp  = "slw"
	slwMatch = "Pool A-0"
	slwAID   = "11111111-1111-4111-8111-111111111111"
	slwBID   = "22222222-2222-4222-8222-222222222222"
)

// slwSetup is a league competition with one running match between Aka and
// Shiro and a second one still scheduled.
func slwSetup(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: slwComp, Name: slwComp, Kind: "individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants(slwComp, []domain.Player{
		{ID: slwAID, Name: "Aka", Dojo: "D1"}, {ID: slwBID, Name: "Shiro", Dojo: "D2"},
	}))
	require.NoError(t, store.SavePoolMatches(slwComp, []state.MatchResult{
		{ID: slwMatch, SideA: "Aka", SideAID: slwAID, SideB: "Shiro", SideBID: slwBID, Court: "A", Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: "Shiro", SideAID: slwBID, SideB: "Aka", SideBID: slwAID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	return eng, store, dir
}

func slwEvent(id string, seq, stamp int64, kind domain.ScoreEventKind, side, letter string) domain.ScoreEvent {
	return domain.ScoreEvent{ID: id, Session: "evs", Seq: seq, Stamp: stamp, Kind: kind, Side: side, Letter: letter}
}

func slwPost(t *testing.T, eng *Engine, matchID string, events ...domain.ScoreEvent) *ScoreEventsResult {
	t.Helper()
	res, err := eng.RecordScoreEvents(slwComp, matchID, ScoreEventsRequest{Session: "evs", Events: events})
	require.NoError(t, err)
	return res
}

func slwStored(t *testing.T, eng *Engine, store *state.Store, matchID string) (*state.MatchResult, bool) {
	t.Helper()
	m, inPool, err := eng.lookupExistingResultIn(store, slwComp, matchID)
	require.NoError(t, err)
	return m, inPool
}

// assertLogIsTheMatch pins "baselines are total": the stored match is exactly
// what its log replays to (values, mark ids, version), a replay of that output
// changes nothing, and no write needed the drift net.
func assertLogIsTheMatch(t *testing.T, eng *Engine, store *state.Store, matchID string) []state.ScoreLogLine {
	t.Helper()
	return assertLogIsTheMatchIn(t, eng, store, slwComp, matchID)
}

func assertLogIsTheMatchIn(t *testing.T, eng *Engine, store *state.Store, compID, matchID string) []state.ScoreLogLine {
	t.Helper()
	lines, err := store.LoadScoreLog(compID, matchID)
	require.NoError(t, err)
	m, inPool, err := eng.lookupExistingResultIn(store, compID, matchID)
	require.NoError(t, err)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	shape := ReplayShape{Comp: comp, Knockout: !inPool}
	rp := ReplayScoring(m, lines, shape)
	assert.Empty(t, replayGroupsDiffer(m, &rp.Match), "the stored match is what its log replays to")
	assert.Equal(t, rp.Match.MarkIDs, m.MarkIDs, "the stored mark ids are the replay's")
	assert.Equal(t, len(lines), m.ScoreVersion, "scoreVersion is the log's line count")
	again := ReplayScoring(&rp.Match, lines, shape)
	assert.Empty(t, replayGroupsDiffer(&rp.Match, &again.Match), "a replay of the replay's own output changes nothing")
	for _, l := range lines {
		if l.Baseline != nil {
			assert.NotEqual(t, DoorDrift, l.Baseline.Door, "no write needed the drift net")
		}
	}
	return lines
}

func TestScoreEvents_FirstEventLogsTheStoredMatchFirst(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	require.NoError(t, eng.RecordMatchResult(slwComp, slwMatch, &state.MatchResult{
		Status: state.MatchStatusRunning, IpponsA: []string{"M"}, IpponsB: []string{},
		ModifiedAt: now - 5000, Changed: []string{state.GroupPoints}, WriteDoor: DoorScore,
	}))
	_, err := store.LoadScoreLog(slwComp, slwMatch)
	require.NoError(t, err)

	res := slwPost(t, eng, slwMatch, slwEvent("e1", 1, now-1000, domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	require.Len(t, res.Answers, 1)
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assert.True(t, res.Written)
	assert.Equal(t, []string{"M", "K"}, res.Match.IpponsA)
	require.NotNil(t, res.Match.MarkIDs)
	assert.Equal(t, "e1", res.Match.MarkIDs.A[1], "the event's mark carries the event's id")

	lines := assertLogIsTheMatch(t, eng, store, slwMatch)
	// The /score write was replayed onto the stored match (bc-evts stage 2b),
	// so it already logged the match first, then its own baseline.
	require.Len(t, lines, 3)
	require.NotNil(t, lines[0].Baseline)
	assert.Equal(t, MigrationDoor, lines[0].Baseline.Door, "nothing is appended to an empty log before the stored match")
	require.NotNil(t, lines[1].Baseline)
	assert.Equal(t, DoorScore, lines[1].Baseline.Door)

	history, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	last := history[len(history)-1]
	assert.Equal(t, DoorEvent, last.Door)
	require.NotNil(t, last.Event)
	assert.Equal(t, "e1", last.Event.ID)
	assert.Equal(t, domain.ScoreOutcomeApplied, last.EventOutcome)
}

func TestScoreEvents_AllDuplicateBatchWritesNoBytes(t *testing.T) {
	eng, store, dir := slwSetup(t)
	now := time.Now().UnixMilli()
	e1 := slwEvent("e1", 1, now-1000, domain.ScoreIpponAdd, domain.ScoreSideA, "M")
	slwPost(t, eng, slwMatch, e1)
	logPath := filepath.Join(dir, "competitions", slwComp, "events", "Pool%20A-0.jsonl")
	before, err := os.ReadFile(logPath)
	require.NoError(t, err)
	version := store.FileVersion(slwComp, "pool-matches.csv")
	historyBefore, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)

	res := slwPost(t, eng, slwMatch, e1, e1)
	assert.False(t, res.Written)
	assert.Equal(t, domain.ScoreOutcomeDuplicate, res.Answers[0].Outcome)
	assert.Equal(t, domain.ScoreOutcomeDuplicate, res.Answers[1].Outcome)
	after, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, before, after, "the log is untouched")
	assert.Equal(t, version, store.FileVersion(slwComp, "pool-matches.csv"), "the match file is untouched")
	historyAfter, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	assert.Len(t, historyAfter, len(historyBefore), "a duplicate leaves no history line")
}

func TestScoreEvents_NotStartedAndSentBack(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	res := slwPost(t, eng, "Pool A-1", slwEvent("q1", 1, now, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	assert.Equal(t, domain.ScoreOutcomeNotStarted, res.Answers[0].Outcome)
	assert.Equal(t, EventReasonNotStarted, res.Answers[0].Reason)
	assert.Equal(t, EventMessageNotStarted, res.Answers[0].Message)
	lines, err := store.LoadScoreLog(slwComp, "Pool A-1")
	require.NoError(t, err)
	assert.Empty(t, lines, "a refused tap is not logged")

	slwPost(t, eng, slwMatch, slwEvent("e1", 1, now-2000, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch))
	res = slwPost(t, eng, slwMatch, slwEvent("e2", 2, time.Now().UnixMilli(), domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	assert.Equal(t, domain.ScoreOutcomeNotStarted, res.Answers[0].Outcome)
	assert.Equal(t, EventReasonSentBack, res.Answers[0].Reason)
	assert.Equal(t, EventMessageSentBack, res.Answers[0].Message)
	assert.Equal(t, []string{"M"}, res.Match.IpponsA, "send back keeps the score")
	assertLogIsTheMatch(t, eng, store, slwMatch)
}

func TestScoreEvents_GapsOfThePostingSession(t *testing.T) {
	eng, _, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	res := slwPost(t, eng, slwMatch,
		slwEvent("e1", 1, now-3000, domain.ScoreFoulAdd, domain.ScoreSideA, ""),
		slwEvent("e4", 4, now-1000, domain.ScoreFoulAdd, domain.ScoreSideB, ""))
	assert.Equal(t, []int64{2, 3}, res.Gaps)
}

func TestScoreEvents_UnreadableLogRefusesAndAppendsNothing(t *testing.T) {
	eng, _, dir := slwSetup(t)
	now := time.Now().UnixMilli()
	slwPost(t, eng, slwMatch, slwEvent("e1", 1, now-1000, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	logPath := filepath.Join(dir, "competitions", slwComp, "events", "Pool%20A-0.jsonl")
	require.NoError(t, os.WriteFile(logPath, []byte("{not json\n"), 0o600))

	res := slwPost(t, eng, slwMatch, slwEvent("e2", 2, now, domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	assert.Equal(t, domain.ScoreOutcomeRefused, res.Answers[0].Outcome)
	assert.Equal(t, EventReasonLogUnreadable, res.Answers[0].Reason)
	assert.NotEmpty(t, res.Answers[0].Message)
	raw, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, "{not json\n", string(raw), "nothing is appended to a log that cannot be read")
}

// TestScoreEvents_BaselinesAreTotal pins that no door triggers the drift net:
// after an event starts the log, each non-event write appends its baseline,
// the stored match stays what the log replays to, and an event on top folds
// onto what the write left.
func TestScoreEvents_BaselinesAreTotal(t *testing.T) {
	eng, store, _ := slwSetup(t)
	t0 := time.Now().UnixMilli() - 60_000
	slwPost(t, eng, slwMatch, slwEvent("e1", 1, t0, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	assertLogIsTheMatch(t, eng, store, slwMatch)

	write := func(m state.MatchResult) {
		t.Helper()
		_, err := eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, &m)
		require.NoError(t, err)
		assertLogIsTheMatch(t, eng, store, slwMatch)
	}

	// A previous build's running snapshot is a baseline (F3), events on top.
	write(state.MatchResult{Status: state.MatchStatusRunning, IpponsA: []string{"D"}, IpponsB: []string{},
		ModifiedAt: t0 + 1000, Changed: []string{state.GroupPoints}, WriteDoor: DoorScore})
	res := slwPost(t, eng, slwMatch, slwEvent("e2", 2, t0+2000, domain.ScoreIpponAdd, domain.ScoreSideB, "K"))
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assert.Equal(t, []string{"D"}, res.Match.IpponsA, "the snapshot replaced the board it never saw")
	assert.Equal(t, []string{"K"}, res.Match.IpponsB)
	assertLogIsTheMatch(t, eng, store, slwMatch)

	// Bulk-score (names no groups) and quick-score (every group).
	var err error
	write(state.MatchResult{Status: state.MatchStatusRunning, IpponsA: []string{"M"}, IpponsB: []string{"K"},
		ModifiedAt: t0 + 3000, WriteDoor: DoorBulkScore})
	quick := state.MatchResult{SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusRunning, IpponsA: []string{"M"}, IpponsB: []string{"D"},
		ModifiedAt: t0 + 4000, WriteDoor: DoorQuickScore}
	quick.Changed, err = eng.WholeMatchChangedGroups(store, slwComp, slwMatch, &quick)
	require.NoError(t, err)
	write(quick)
	res = slwPost(t, eng, slwMatch, slwEvent("e3", 3, t0+5000, domain.ScoreFoulAdd, domain.ScoreSideA, ""))
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assertLogIsTheMatch(t, eng, store, slwMatch)

	// The finish, a point made after it (turned away: the match is closed), the
	// correction, a decision, the reopen and the send back.
	write(state.MatchResult{SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusCompleted, Decision: "hikiwake",
		IpponsA: []string{"M"}, IpponsB: []string{"D"}, HansokuA: 1, ModifiedAt: t0 + 6000, WriteDoor: DoorScore})
	res = slwPost(t, eng, slwMatch, slwEvent("e4", 4, t0+7000, domain.ScoreIpponAdd, domain.ScoreSideA, "D"))
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, res.Answers[0].Outcome)
	assert.Equal(t, state.MatchStatusCompleted, res.Match.Status, "the finished match stays finished")
	assert.Empty(t, res.Match.Winner, "the draw stands: the tap after it changed nothing")
	assertLogIsTheMatch(t, eng, store, slwMatch)
	write(state.MatchResult{SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusCompleted, Winner: "Shiro",
		IpponsA: []string{}, IpponsB: []string{"K", "K"}, ModifiedAt: t0 + 8000, CorrectionReason: "fix", WriteDoor: DoorScore})
	// From the screen that showed the corrected result (its basis holds it):
	// a decision from a device that had not seen it would be turned away.
	_, _, err = eng.RecordDecisionWithOptions(slwComp, slwMatch, "kiken-voluntary", "aka", "", nil,
		ForceOptions{Basis: &state.ScoreBasis{Seen: 1 << 20}}, t0+9000)
	require.NoError(t, err)
	assertLogIsTheMatch(t, eng, store, slwMatch)
	_, err = eng.ReopenMatch(slwComp, slwMatch, "mistake")
	require.NoError(t, err)
	assertLogIsTheMatch(t, eng, store, slwMatch)
	res = slwPost(t, eng, slwMatch, slwEvent("e5", 5, time.Now().UnixMilli(), domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assertLogIsTheMatch(t, eng, store, slwMatch)
	require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch))
	lines := assertLogIsTheMatch(t, eng, store, slwMatch)

	doors := map[string]bool{}
	for _, l := range lines {
		if l.Baseline != nil {
			doors[l.Baseline.Door] = true
		}
	}
	for _, d := range []string{MigrationDoor, DoorScore, DoorBulkScore, DoorQuickScore, DoorDecision, doorReopen, doorRequeue} {
		assert.True(t, doors[d], "a %s baseline is in the log", d)
	}
}

// TestScoreEvents_WriteIDResendAnswersRecorded pins F1: a finish resent with
// the writeId its first send logged answers as recorded, keeps a point made
// after it (R3) and leaves no history line.
func TestScoreEvents_WriteIDResendAnswersRecorded(t *testing.T) {
	eng, store, _ := slwSetup(t)
	t0 := time.Now().UnixMilli() - 60_000
	slwPost(t, eng, slwMatch, slwEvent("e1", 1, t0, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	finish := func() *state.MatchResult {
		return &state.MatchResult{SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusCompleted, Winner: "Aka",
			IpponsA: []string{"M"}, IpponsB: []string{}, ModifiedAt: t0 + 1000, WriteDoor: DoorScore,
			WriteID: "w-finish", Basis: &state.ScoreBasis{Seen: 2}}
	}
	_, err := eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, finish())
	require.NoError(t, err)
	slwPost(t, eng, slwMatch, slwEvent("e2", 2, t0+2000, domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	history, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)

	again := finish()
	_, err = eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, again)
	require.NoError(t, err)
	assert.Equal(t, []string{"M"}, again.IpponsA, "answered with the stored match; the tap after the finish was turned away")
	after, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	assert.Len(t, after, len(history), "a recorded resend writes no history line (Q6 b)")
	assertLogIsTheMatch(t, eng, store, slwMatch)
}

// TestScoreEvents_FinishBasisDecidesOlderEvents pins D5 through the basis: an
// event the finish saw is recorded quietly, one it never saw is beforeBaseline,
// kept in the history and reported.
func TestScoreEvents_FinishBasisDecidesOlderEvents(t *testing.T) {
	eng, store, _ := slwSetup(t)
	t0 := time.Now().UnixMilli() - 60_000
	slwPost(t, eng, slwMatch, slwEvent("seen", 1, t0, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	other := domain.ScoreEvent{ID: "unseen", Session: "other", Seq: 1, Stamp: t0 + 500, Kind: domain.ScoreIpponAdd, Side: domain.ScoreSideB, Letter: "K"}
	_, err := eng.RecordScoreEvents(slwComp, slwMatch, ScoreEventsRequest{Session: "other", Events: []domain.ScoreEvent{other}})
	require.NoError(t, err)

	fin := &state.MatchResult{SideA: "Aka", SideB: "Shiro", Status: state.MatchStatusCompleted, Winner: "Aka",
		IpponsA: []string{"M"}, IpponsB: []string{}, ModifiedAt: t0 + 1000, WriteDoor: DoorScore,
		WriteID: "w1", Basis: &state.ScoreBasis{Own: []string{"seen"}}}
	_, err = eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, fin)
	require.NoError(t, err)
	require.NotNil(t, fin.Merge)
	assert.Equal(t, []string{"unseen"}, fin.Merge.EventsNotApplied)
	lines := assertLogIsTheMatch(t, eng, store, slwMatch)
	m, _ := slwStored(t, eng, store, slwMatch)
	comp, err := store.LoadCompetition(slwComp)
	require.NoError(t, err)
	rp := ReplayScoring(m, lines, ReplayShape{Comp: comp})
	assert.Equal(t, domain.ScoreOutcomeRecorded, rp.Outcome("seen"))
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, rp.Outcome("unseen"))
	history, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	found := false
	for _, h := range history {
		if h.Event != nil && h.Event.ID == "unseen" && h.EventOutcome == domain.ScoreOutcomeBeforeBaseline {
			found = true
		}
	}
	assert.True(t, found, "the unseen event's new outcome is in the history")
}

// A write the client builds with no basis cannot say which events it saw, so
// none of the older ones is called recorded: they are before the baseline, in
// the report and the history alike. A server-built write keeps what it read.
func TestScoreEvents_NoBasisLeavesOlderEventsBeforeTheBaseline(t *testing.T) {
	eng, store, _ := slwSetup(t)
	t0 := time.Now().UnixMilli() - 60_000
	slwPost(t, eng, slwMatch, slwEvent("ev1", 1, t0, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))
	other := domain.ScoreEvent{ID: "ev2", Session: "other", Seq: 1, Stamp: t0 + 500, Kind: domain.ScoreIpponAdd, Side: domain.ScoreSideB, Letter: "K"}
	_, err := eng.RecordScoreEvents(slwComp, slwMatch, ScoreEventsRequest{Session: "other", Events: []domain.ScoreEvent{other}})
	require.NoError(t, err)

	w := &state.MatchResult{Status: state.MatchStatusRunning, IpponsA: []string{"D"}, IpponsB: []string{},
		ModifiedAt: t0 + 1000, Changed: []string{state.GroupPoints}, WriteDoor: DoorScore}
	_, err = eng.RecordMatchResultWithIneligibility(slwComp, slwMatch, w)
	require.NoError(t, err)
	require.NotNil(t, w.Merge)
	assert.ElementsMatch(t, []string{"ev1", "ev2"}, w.Merge.EventsNotApplied)
	assertLogIsTheMatch(t, eng, store, slwMatch)
	history, err := store.LoadMatchHistory(slwComp, slwMatch)
	require.NoError(t, err)
	var reasons []string
	for _, h := range history {
		if h.Event != nil && h.EventOutcome == domain.ScoreOutcomeBeforeBaseline {
			reasons = append(reasons, h.Reason)
		}
	}
	assert.Equal(t, []string{"no basis", "no basis"}, reasons)
}

func TestScoreEvents_JudgeRefusesWithoutLogging(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	res, err := eng.RecordScoreEvents(slwComp, slwMatch, ScoreEventsRequest{Session: "evs",
		Events: []domain.ScoreEvent{
			slwEvent("ok", 1, now-1000, domain.ScoreIpponAdd, domain.ScoreSideA, "M"),
			slwEvent("no", 2, now-900, domain.ScoreEnchoAdd, "", ""),
		},
		Judge: func(_ *state.MatchResult, _ *state.Competition, ev domain.ScoreEvent) *EventRefusal {
			if ev.Kind == domain.ScoreEnchoAdd {
				return &EventRefusal{Reason: "nope", Message: "Not this one."}
			}
			return nil
		}})
	require.NoError(t, err)
	assert.Equal(t, domain.ScoreOutcomeApplied, res.Answers[0].Outcome)
	assert.Equal(t, ScoreEventAnswer{ID: "no", Outcome: domain.ScoreOutcomeRefused, Reason: "nope", Message: "Not this one."}, res.Answers[1])
	lines := assertLogIsTheMatch(t, eng, store, slwMatch)
	assert.Len(t, lines, 2, "the migration baseline and the one event taken")
}

// A tap turned away never enters the log, so a resend meets the same wall
// every time: an anonymous caller's turned-away taps leave no history line, and
// the organiser's leave one per event id, however often they are sent again.
func TestScoreEvents_TurnedAwayTapsDoNotGrowTheHistory(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	history := func(matchID string) []state.MatchHistoryEntry {
		t.Helper()
		h, err := store.LoadMatchHistory(slwComp, matchID)
		require.NoError(t, err)
		return h
	}
	send := func(anonymous bool, matchID string, judge func(*state.MatchResult, *state.Competition, domain.ScoreEvent) *EventRefusal) {
		t.Helper()
		_, err := eng.RecordScoreEvents(slwComp, matchID, ScoreEventsRequest{Session: "evs", NoTurnedAwayHistory: anonymous, Judge: judge,
			Events: []domain.ScoreEvent{
				slwEvent("t1", 1, now, domain.ScoreIpponAdd, domain.ScoreSideA, "M"),
				slwEvent("t2", 2, now+1, domain.ScoreIpponAdd, domain.ScoreSideB, "K"),
			}})
		require.NoError(t, err)
	}

	// Not started (the scheduled match): anonymous leaves nothing.
	for range 3 {
		send(true, "Pool A-1", nil)
	}
	assert.Empty(t, history("Pool A-1"), "an anonymous caller's turned-away taps write no history")
	// The organiser: one line per event id, looped.
	for range 3 {
		send(false, "Pool A-1", nil)
	}
	assert.Len(t, history("Pool A-1"), 2, "one line for each of the two ids, not one per send")

	// Refused by a judge on a running match: the same two rules.
	refuse := func(*state.MatchResult, *state.Competition, domain.ScoreEvent) *EventRefusal {
		return &EventRefusal{Reason: "nope", Message: "No."}
	}
	for range 3 {
		send(true, slwMatch, refuse)
	}
	assert.Empty(t, history(slwMatch))
	for range 3 {
		send(false, slwMatch, refuse)
	}
	assert.Len(t, history(slwMatch), 2)
}

// A tap the match was not running for is told the truth: "sent back to the
// queue" only when the match was sent back; otherwise that it is not running.
func TestScoreEvents_NotStartedWordingFollowsTheQueueSendBack(t *testing.T) {
	eng, store, _ := slwSetup(t)
	now := time.Now().UnixMilli()
	slwPost(t, eng, slwMatch, slwEvent("e1", 1, now-5000, domain.ScoreIpponAdd, domain.ScoreSideA, "M"))

	// Withdrawn, reopened, and scheduled again by something other than the
	// operator's send back (here the file edited directly).
	_, _, err := eng.RecordDecision(slwComp, slwMatch, "kiken-voluntary", "aka", "", nil, now-4000)
	require.NoError(t, err)
	_, err = eng.ReopenMatch(slwComp, slwMatch, "mistake")
	require.NoError(t, err)
	found, err := store.UpdatePoolMatchByID(slwComp, slwMatch, func(r *state.MatchResult) error {
		r.Status = state.MatchStatusScheduled
		return nil
	})
	require.NoError(t, err)
	require.True(t, found)
	res := slwPost(t, eng, slwMatch, slwEvent("e2", 2, time.Now().UnixMilli(), domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	assert.Equal(t, domain.ScoreOutcomeNotStarted, res.Answers[0].Outcome)
	assert.Equal(t, EventMessageNotRunning, res.Answers[0].Message, "the match was not sent back, so the tap is not told it was")

	_, err = store.UpdatePoolMatchByID(slwComp, slwMatch, func(r *state.MatchResult) error {
		r.Status = state.MatchStatusRunning
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, eng.RevertMatchToQueue(slwComp, slwMatch))
	res = slwPost(t, eng, slwMatch, slwEvent("e3", 3, time.Now().UnixMilli(), domain.ScoreIpponAdd, domain.ScoreSideA, "K"))
	assert.Equal(t, EventReasonSentBack, res.Answers[0].Reason)
	assert.Equal(t, EventMessageSentBack, res.Answers[0].Message)
}

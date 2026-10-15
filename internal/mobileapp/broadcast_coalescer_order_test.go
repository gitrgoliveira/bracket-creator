package mobileapp

import (
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// recorder collects what the sends deliver, in order.
type recorder struct {
	mu  sync.Mutex
	got []string
}

func (r *recorder) send(s string) func() {
	return func() { r.mu.Lock(); r.got = append(r.got, s); r.mu.Unlock() }
}

func (r *recorder) seen() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// A running push held in the window must not be sent after a newer state that
// went out at once once the window had passed (bc-evts review round 2: the late
// timer delivered [a c b]).
func TestMatchBroadcastCoalescer_LateTimerNeverPushesOlderStateAfterNewer(t *testing.T) {
	base := time.Now()
	var offMu sync.Mutex
	var off time.Duration
	setOff := func(d time.Duration) { offMu.Lock(); off = d; offMu.Unlock() }
	coalescerNow = func() time.Time { offMu.Lock(); defer offMu.Unlock(); return base.Add(off) }
	t.Cleanup(func() { coalescerNow = time.Now })

	rec := &recorder{}
	c := newMatchBroadcastCoalescer()
	c.Submit("m", true, stillRunning(rec.send("a")))
	setOff(100 * time.Millisecond)
	c.Submit("m", true, stillRunning(rec.send("b"))) // held
	setOff(300 * time.Millisecond)
	c.Submit("m", true, stillRunning(rec.send("c"))) // past the window: goes out at once
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []string{"a", "c"}, rec.seen(), "the held older state is dropped when a newer one goes out")
}

// A direct send (what every match_updated that is not a running score write
// uses) cancels the held running push: the held one is older than it.
func TestMatchBroadcastCoalescer_SendNowCancelsTheHeldPush(t *testing.T) {
	rec := &recorder{}
	c := newMatchBroadcastCoalescer()
	c.Submit("m", true, stillRunning(rec.send("a")))
	c.Submit("m", true, stillRunning(rec.send("b"))) // held
	c.SendNow("m", rec.send("direct"))
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []string{"a", "direct"}, rec.seen(), "the held tap must not follow the newer direct push")
}

// A send is made under the key's lock, so a direct push after its own commit
// waits for the send in flight and follows it, never the other way round.
func TestMatchBroadcastCoalescer_SendNowWaitsForASendInFlight(t *testing.T) {
	c := newMatchBroadcastCoalescer()
	started := make(chan struct{})
	release := make(chan struct{})
	rec := &recorder{}
	c.Submit("m", true, stillRunning(rec.send("a")))
	c.Submit("m", true, stillRunning(func() { close(started); <-release; rec.send("held")() }))
	<-started // the window's end fired the held send

	done := make(chan struct{})
	go func() { c.SendNow("m", rec.send("direct")); close(done) }()
	select {
	case <-done:
		t.Fatal("SendNow returned while the held send was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	assert.Equal(t, []string{"a", "held", "direct"}, rec.seen())
}

// The lock is per match key: a send stuck on a slow store read for one
// competition must not delay another competition's score answers.
func TestMatchBroadcastCoalescer_KeysAreIndependent(t *testing.T) {
	c := newMatchBroadcastCoalescer()
	started := make(chan struct{})
	release := make(chan struct{})
	slowDone := make(chan struct{})
	go func() {
		c.Submit("A:m", false, stillRunning(func() { close(started); <-release })) // a blocking fake reader
		close(slowDone)
	}()
	<-started

	rec := &recorder{}
	fast := make(chan struct{})
	go func() {
		c.Submit("B:m", true, stillRunning(rec.send("b-running")))
		c.SendNow("B:m", rec.send("b-direct"))
		close(fast)
	}()
	select {
	case <-fast:
	case <-time.After(2 * time.Second):
		t.Fatal("competition B was blocked behind competition A's slow send")
	}
	assert.Equal(t, []string{"b-running", "b-direct"}, rec.seen())
	close(release)
	<-slowDone
}

// A timer whose pending send was cancelled must not flush a LATER pending
// early: the 4/s cap holds across a cancel.
func TestMatchBroadcastCoalescer_CancelledTimerCannotFlushALaterPendingEarly(t *testing.T) {
	base := time.Now()
	var offMu sync.Mutex
	var off time.Duration
	setOff := func(d time.Duration) { offMu.Lock(); off = d; offMu.Unlock() }
	coalescerNow = func() time.Time { offMu.Lock(); defer offMu.Unlock(); return base.Add(off) }
	t.Cleanup(func() { coalescerNow = time.Now })

	rec := &recorder{}
	c := newMatchBroadcastCoalescer()
	c.Submit("m", true, stillRunning(rec.send("a")))
	setOff(200 * time.Millisecond)
	c.Submit("m", true, stillRunning(rec.send("b"))) // held, timer due in 50ms
	setOff(300 * time.Millisecond)
	c.Submit("m", true, stillRunning(rec.send("c"))) // window lapsed: goes out, cancels b and its timer
	c.Submit("m", true, stillRunning(rec.send("d"))) // inside the new window: held, due in 250ms
	time.Sleep(matchCoalesceWindow / 2)
	assert.Equal(t, []string{"a", "c"}, rec.seen(), "b's cancelled timer must not send d early")
	time.Sleep(matchCoalesceWindow)
	assert.Equal(t, []string{"a", "c", "d"}, rec.seen(), "d goes out once its own window ends")
}

// payloadRecorder is a Broadcaster that keeps the match_updated payloads and
// reaches a coalescer the way the Hub does.
type payloadRecorder struct {
	mu  sync.Mutex
	got []gin.H
	c   *matchBroadcastCoalescer
}

func (p *payloadRecorder) Broadcast(_ EventType, data any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.got = append(p.got, data.(gin.H))
}

func (p *payloadRecorder) pushMatchNow(key string, send func()) { p.c.SendNow(key, send) }

func (p *payloadRecorder) statuses() []state.MatchStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []state.MatchStatus
	for _, g := range p.got {
		out = append(out, g["result"].(*state.MatchResult).Status)
	}
	return out
}

func setPoolStatus(t *testing.T, store *state.Store, compID string, st state.MatchStatus) {
	t.Helper()
	pool, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	pool[0].Status = st
	require.NoError(t, store.SavePoolMatches(compID, pool))
}

// A direct send after a held running push leaves the NEWEST stored state last:
// the held push is cancelled and the direct one reads the store as it runs.
func TestBroadcastMatchNow_AfterAHeldRunningPushLeavesTheNewestStateLast(t *testing.T) {
	const compID = "now-after-held"
	_, store, _, _ := seServer(t, compID)
	c := newMatchBroadcastCoalescer()
	rec := &payloadRecorder{c: c}
	captured := gin.H{"competitionId": compID, "matchId": seMatch, "result": "captured"}
	running := runningPushSender(rec, store, compID, seMatch, captured)
	key := compID + ":" + seMatch
	c.Submit(key, true, running)
	c.Submit(key, true, running) // held
	setPoolStatus(t, store, compID, state.MatchStatusCompleted)
	broadcastMatchNow(rec, store, compID, seMatch, captured)
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []state.MatchStatus{state.MatchStatusRunning, state.MatchStatusCompleted}, rec.statuses())
}

// A match that is gone by the time the push is built (a draw discarded, a
// competition deleted) drops the push; a read error falls back to the captured
// payload, since the write did commit; an id-only push needs no read.
func TestFreshMatchPush_NotFoundDropsAndReadErrorFallsBack(t *testing.T) {
	const compID = "fresh-drop"
	_, store, _, _ := seServer(t, compID)
	captured := gin.H{"competitionId": compID, "matchId": "no-such-match", "result": "captured"}

	_, ok := freshMatchPush(store, compID, "no-such-match", captured)
	assert.False(t, ok, "not found: the push is dropped")

	rec := &payloadRecorder{c: newMatchBroadcastCoalescer()}
	broadcastMatchNow(rec, store, compID, "no-such-match", captured)
	assert.Empty(t, rec.got, "nothing is broadcast for a match that no longer exists")

	got, ok := freshMatchPush(erroringStore{store}, compID, seMatch, captured)
	assert.True(t, ok)
	assert.Equal(t, captured, got, "a read error sends the captured payload")

	idOnly := gin.H{"competitionId": compID, "matchId": "no-such-match"}
	got, ok = freshMatchPush(store, compID, "no-such-match", idOnly)
	assert.True(t, ok)
	assert.Equal(t, idOnly, got, "an id-only push is sent as it is")

	_, found, err := readStoredMatch(erroringStore{store}, compID, seMatch)
	assert.Error(t, err)
	assert.False(t, found)
}

type erroringStore struct{ *state.Store }

func (erroringStore) LoadPoolMatches(string) ([]state.MatchResult, error) {
	return nil, assert.AnError
}

// The payload keeps its keys: only "result" is replaced by the stored match.
func TestFreshMatchPush_ReadsTheStoredMatch(t *testing.T) {
	const compID = "fresh-push"
	_, store, _, _ := seServer(t, compID)
	captured := gin.H{"competitionId": compID, "matchId": seMatch, "court": "A", "result": "captured"}

	got, ok := freshMatchPush(store, compID, seMatch, captured)
	require.True(t, ok)
	res, isMatch := got["result"].(*state.MatchResult)
	require.True(t, isMatch, "the stored match, projected for the push")
	assert.Equal(t, state.MatchStatusRunning, res.Status)
	assert.Equal(t, "A", got["court"], "the payload's other keys are kept")
	assert.Equal(t, "captured", captured["result"], "the caller's map is not modified")

	setPoolStatus(t, store, compID, state.MatchStatusScheduled)
	got, _ = freshMatchPush(store, compID, seMatch, captured)
	assert.Equal(t, state.MatchStatusScheduled, got["result"].(*state.MatchResult).Status, "a later write shows, not the captured payload")
}

// Through the Hub, as a handler reaches it; a double with no coalescer just sends.
func TestHubPushMatchNow(t *testing.T) {
	hub := NewHub()
	var ran int
	hub.pushMatchNow("c:m", func() { ran++ })
	hub.heldPushes = newMatchBroadcastCoalescer()
	hub.pushMatchNow("c:m", func() { ran++ })
	assert.Equal(t, 2, ran)
}

// A held running push whose fresh read finds the match no longer running
// (another device finished it) ends the key's window: the key is dropped at
// once rather than kept until the match's next push.
func TestRunningPushSender_DropsTheKeyOnceTheMatchLeavesRunning(t *testing.T) {
	const compID = "held-then-finished"
	_, store, _, _ := seServer(t, compID)
	c := newMatchBroadcastCoalescer()
	rec := &payloadRecorder{c: c}
	captured := gin.H{"competitionId": compID, "matchId": seMatch, "result": "captured"}
	key := compID + ":" + seMatch
	send := runningPushSender(rec, store, compID, seMatch, captured)
	c.Submit(key, true, send)
	_, present := c.keys.Load(key)
	require.True(t, present, "a running push keeps its window")
	c.Submit(key, true, send) // held
	setPoolStatus(t, store, compID, state.MatchStatusCompleted)
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []state.MatchStatus{state.MatchStatusRunning, state.MatchStatusCompleted}, rec.statuses())
	_, present = c.keys.Load(key)
	assert.False(t, present, "the held push read a finished match: the key is dropped")
}

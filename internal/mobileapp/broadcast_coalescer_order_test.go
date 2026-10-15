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
	c.Submit("m", true, rec.send("a"))
	setOff(100 * time.Millisecond)
	c.Submit("m", true, rec.send("b")) // held
	setOff(300 * time.Millisecond)
	c.Submit("m", true, rec.send("c")) // past the window: goes out at once
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []string{"a", "c"}, rec.seen(), "the held older state is dropped when a newer one goes out")
}

// A completed broadcast during the window cancels the held running one, and
// Discard (what every direct match_updated broadcast calls) does the same.
func TestMatchBroadcastCoalescer_DiscardCancelsTheHeldPush(t *testing.T) {
	rec := &recorder{}
	c := newMatchBroadcastCoalescer()
	c.Submit("m", true, rec.send("a"))
	c.Submit("m", true, rec.send("b"))
	c.Discard("m")
	time.Sleep(2 * matchCoalesceWindow)
	assert.Equal(t, []string{"a"}, rec.seen(), "a requeue, decision or reopen announced meanwhile supersedes the held tap")

	// Through the hub, as a handler reaches it.
	hub := NewHub()
	hub.heldPushes = c
	c.Submit("comp:m2", true, rec.send("x"))
	c.Submit("comp:m2", true, rec.send("y"))
	discardHeldMatchPush(hub, "comp", "m2")
	c.mu.Lock()
	_, held := c.pending["comp:m2"]
	c.mu.Unlock()
	assert.False(t, held)
	discardHeldMatchPush(&fakeBroadcaster{}, "comp", "m2") // a double with no coalescer: no panic, nothing held
}

type fakeBroadcaster struct{}

func (*fakeBroadcaster) Broadcast(EventType, any) {}

// A held send is made under the lock, so a broadcast that discards the key
// after its own commit waits for the send in flight and follows it, never the
// other way round.
func TestMatchBroadcastCoalescer_DiscardWaitsForASendInFlight(t *testing.T) {
	c := newMatchBroadcastCoalescer()
	started := make(chan struct{})
	release := make(chan struct{})
	rec := &recorder{}
	c.Submit("m", true, rec.send("a"))
	c.Submit("m", true, func() { close(started); <-release; rec.send("held")() })
	<-started // the window's end fired the held send

	done := make(chan struct{})
	go func() { c.Discard("m"); rec.send("direct")(); close(done) }()
	select {
	case <-done:
		t.Fatal("Discard returned while the held send was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-done
	assert.Equal(t, []string{"a", "held", "direct"}, rec.seen())
}

// The push a send makes is built from what is stored when it runs.
func TestFreshMatchPush_ReadsTheStoredMatch(t *testing.T) {
	const compID = "fresh-push"
	_, store, _, _ := seServer(t, compID)
	captured := gin.H{"competitionId": compID, "matchId": seMatch, "result": "captured"}

	got := freshMatchPush(store, compID, seMatch, captured)
	res, ok := got["result"].(*state.MatchResult)
	require.True(t, ok, "the stored match, projected for the push")
	assert.Equal(t, state.MatchStatusRunning, res.Status)

	pool, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	pool[0].Status = state.MatchStatusScheduled
	require.NoError(t, store.SavePoolMatches(compID, pool))
	got = freshMatchPush(store, compID, seMatch, captured)
	assert.Equal(t, state.MatchStatusScheduled, got["result"].(*state.MatchResult).Status, "a later write shows, not the captured payload")

	assert.Equal(t, captured, freshMatchPush(store, compID, "no-such-match", captured), "an unreadable match falls back to the captured push")
}

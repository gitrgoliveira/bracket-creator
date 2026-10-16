package mobileapp

import (
	"log"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// matchBroadcastCoalescer owns ONE rule for a match's match_updated pushes:
// a push for a match key (compID:matchID) always sends the LATEST STORED
// match, read at send time, and the pushes for one key are serialized.
// Running-match pushes are also rate-limited to one per matchCoalesceWindow
// (250ms, so at most 4/s per match) with a trailing send.
//
// C3: C1's 300ms client-side debounce already limits autosave writes to
// <=3.3/s per match, so the rate limit is defense-in-depth: a reconnect flush
// (C2's offline queue) or any other burst cannot fan out more than 4
// broadcasts/s to the full SSE subscriber pool.
//
// Submit: a running push inside the window is held (the latest replaces any
// held one) and sent once when the window ends, so the last state of a burst
// is always broadcast. SendNow: every other push for the match (completed, a
// decision, a requeue, a reopen, a start, a court move, a downstream reopen)
// cancels what is held and sends at once; the sender reads the stored match
// when it runs, so the newest state is always the last one sent, whatever
// order the requests committed and arrived in.
//
// The lock is PER KEY. No lock spans keys, so one competition's slow store
// read (a WAL fsync) never delays another competition's score answers;
// ordering only matters within a key. A key's entry is dropped when it holds
// nothing, so the map is bounded by the set of currently-RUNNING matches.
type matchBroadcastCoalescer struct {
	keys sync.Map // matchKey -> *coalescerKey
	// now is the coalescer's clock, time.Now unless a test builds its own
	// instance with a frozen one. It is per instance, never a package
	// variable: a trailing-send timer from one coalescer reads it on its own
	// goroutine, so swapping a shared clock raced with another test's timer.
	now func() time.Time

	// mu guards closed and the Add of inflight, so no timer is armed once
	// Close has begun waiting.
	mu     sync.Mutex
	closed bool
	// inflight counts armed trailing-send timers: Done when one is stopped
	// before it fires, or when its send has finished.
	inflight sync.WaitGroup
}

// coalescerKey is one match's serialization point.
type coalescerKey struct {
	c       *matchBroadcastCoalescer
	mu      sync.Mutex
	dead    bool // dropped from the map; a holder that finds it retries
	last    time.Time
	hasLast bool
	pending func() bool
	timer   *time.Timer
	// gen counts cancellations: a timer flushes only the generation it was
	// armed for, so a cancelled timer that already fired cannot send a later
	// pending early and break the 4/s cap.
	gen uint64
}

const matchCoalesceWindow = 250 * time.Millisecond

func newMatchBroadcastCoalescer() *matchBroadcastCoalescer {
	return newMatchBroadcastCoalescerWithClock(time.Now)
}

// newMatchBroadcastCoalescerWithClock is the constructor with the clock given:
// TestScoreHandler_C3Coalescer and the window tests pin it so the 250ms window
// cannot lapse between two writes on a contended CI runner.
func newMatchBroadcastCoalescerWithClock(now func() time.Time) *matchBroadcastCoalescer {
	return &matchBroadcastCoalescer{now: now}
}

// Close stops every armed trailing-send timer and waits for any whose send is
// already under way. A push submitted after Close is sent at once (nothing is
// held, so nothing is left behind). Idempotent; called from Hub.Close at
// shutdown and from tests' cleanup.
func (c *matchBroadcastCoalescer) Close() {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()
	c.keys.Range(func(key, v any) bool {
		k := v.(*coalescerKey)
		k.mu.Lock()
		if k.dead {
			k.mu.Unlock()
			return true
		}
		k.cancelLocked()
		k.hasLast = false
		c.release(key.(string), k)
		return true
	})
	c.inflight.Wait()
}

// armLocked reserves a slot for a trailing-send timer; false once Close has
// begun, when the caller sends at once instead.
func (c *matchBroadcastCoalescer) armLocked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	c.inflight.Add(1)
	return true
}

// lock returns the key's state, locked. matchKey is the competition-scoped key
// (compID:matchID); callers must NOT pass a bare match id, or two competitions
// sharing a match id would share a window.
func (c *matchBroadcastCoalescer) lock(matchKey string) *coalescerKey {
	for {
		v, _ := c.keys.LoadOrStore(matchKey, &coalescerKey{c: c})
		k := v.(*coalescerKey)
		k.mu.Lock()
		if !k.dead {
			return k
		}
		k.mu.Unlock()
	}
}

// cancelLocked drops what is held and stops its timer.
func (k *coalescerKey) cancelLocked() {
	k.gen++
	k.pending = nil
	if k.timer != nil {
		if k.timer.Stop() {
			// Stopped before it fired: its callback never runs, so it
			// cannot release its own inflight slot.
			k.c.inflight.Done()
		}
		k.timer = nil
	}
}

// allowLocked answers whether a push may go out now. A true answer cancels
// what was held: the caller sends a newer state. Non-running ends the window.
func (k *coalescerKey) allowLocked(isRunning bool) bool {
	if !isRunning {
		k.cancelLocked()
		k.hasLast = false
		return true
	}
	now := k.c.now()
	if k.hasLast && now.Sub(k.last) < matchCoalesceWindow {
		return false
	}
	k.last, k.hasLast = now, true
	k.cancelLocked()
	return true
}

// release unlocks the key, dropping its map entry when it holds nothing.
func (c *matchBroadcastCoalescer) release(matchKey string, k *coalescerKey) {
	if !k.hasLast && k.pending == nil {
		k.dead = true
		c.keys.CompareAndDelete(matchKey, k)
	}
	k.mu.Unlock()
}

// SendNow cancels what is held for the key, ends its window and sends at once,
// under the key's lock: a flush already in flight finishes first and this
// push follows it. Every match_updated that is not a running score write
// (a decision, a requeue, a reopen, a start, a court move...) comes here
// through broadcastMatchNow.
func (c *matchBroadcastCoalescer) SendNow(matchKey string, send func()) {
	k := c.lock(matchKey)
	defer c.release(matchKey, k)
	k.allowLocked(false)
	send()
}

// Submit sends now when the push may proceed (allowLocked: a non-running push
// always, a running one outside the window). A running push
// inside the window is held instead, replacing any held earlier, and sent once
// when the window ends. send runs under the key's lock and must read the
// stored match when it runs (freshMatchPush). The held send runs on a one-shot
// timer of at most the window, recovered like any spawned goroutine (safeGo).
// A hub closed meanwhile still takes a seq and writes its ring when Broadcast
// runs; no subscriber is left to receive it.
func (c *matchBroadcastCoalescer) Submit(matchKey string, isRunning bool, send func() bool) {
	k := c.lock(matchKey)
	defer c.release(matchKey, k)
	if k.allowLocked(isRunning) {
		k.sentLocked(send())
		return
	}
	k.pending = send
	if k.timer != nil {
		return
	}
	if !c.armLocked() {
		// Closed: no timer outlives Close, so this push goes out at once.
		k.pending = nil
		k.sentLocked(send())
		return
	}
	delay := max(matchCoalesceWindow-c.now().Sub(k.last), time.Millisecond)
	gen := k.gen
	k.timer = time.AfterFunc(delay, func() {
		defer c.inflight.Done()
		var wg sync.WaitGroup
		safeGo(&wg, nil, func() { c.flushPending(matchKey, k, gen) })
		wg.Wait()
	})
}

// sentLocked takes a send's answer: a match no longer running ends its window,
// so the key holds nothing and release drops it.
func (k *coalescerKey) sentLocked(stillRunning bool) {
	if !stillRunning {
		k.cancelLocked()
		k.hasLast = false
	}
}

// flushPending sends what the key holds when its timer fires, unless that
// timer's generation was cancelled meanwhile.
func (c *matchBroadcastCoalescer) flushPending(matchKey string, k *coalescerKey, gen uint64) {
	k.mu.Lock()
	defer c.release(matchKey, k)
	if k.dead || k.gen != gen || k.pending == nil {
		return
	}
	send := k.pending
	k.pending, k.timer = nil, nil
	k.last, k.hasLast = c.now(), true
	k.sentLocked(send())
}

// matchPushStore is the read side a push needs: the stored match and the
// competitor-status records that annotate a withdrawal. CompetitionStore and
// *state.Store satisfy it.
type matchPushStore interface {
	LoadPoolMatches(id string) ([]state.MatchResult, error)
	LoadBracket(id string) (*state.Bracket, error)
	LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error)
}

// freshMatchPush builds the match_updated payload for a push from the STORED
// match, read now. captured is the payload the caller had: a push that carries
// no "result" has nothing to refresh and goes out as captured; one that does
// carries the stored match instead, under the same keys. The second return is
// false when the match no longer exists (a draw discarded or a competition
// deleted within the window): the push is dropped. A read error is logged and
// the captured payload goes out, since the write did commit.
func freshMatchPush(store matchPushStore, compID, matchID string, captured gin.H) (gin.H, bool) {
	p, ok, _ := freshMatchRead(store, compID, matchID, captured)
	return p, ok
}

// freshMatchRead is freshMatchPush with the match it read (nil when it did
// not read one: a payload with no result, or a read error).
func freshMatchRead(store matchPushStore, compID, matchID string, captured gin.H) (gin.H, bool, *state.MatchResult) {
	if _, carries := captured["result"]; !carries {
		return captured, true, nil
	}
	r, found, err := readStoredMatch(store, compID, matchID)
	if err != nil {
		log.Printf("mobileapp: push %s/%s: read stored match: %v", compID, matchID, err)
		return captured, true, nil
	}
	if !found {
		return nil, false, nil
	}
	stampWithdrawnStatus(store, compID, r)
	out := make(gin.H, len(captured))
	for k, v := range captured {
		out[k] = v
	}
	out["result"] = matchPtrForBroadcast(r)
	return out, true, r
}

// runningPushSender is the send a running match's push hands Submit: it
// broadcasts the match as stored when it runs (freshMatchRead) and answers
// whether that match still runs, so a key whose match has left running (or
// is gone) is dropped at once rather than kept until its next push.
func runningPushSender(b Broadcaster, store matchPushStore, compID, matchID string, captured gin.H) func() bool {
	return func() bool {
		p, ok, r := freshMatchRead(store, compID, matchID, captured)
		if !ok {
			return false
		}
		b.Broadcast(EventMatchUpdated, p)
		return r == nil || r.Status == state.MatchStatusRunning
	}
}

// readStoredMatch finds a match by id in the pool matches, then the bracket.
// found is false with a nil err when the competition holds no such match.
func readStoredMatch(store matchPushStore, compID, matchID string) (*state.MatchResult, bool, error) {
	pool, err := store.LoadPoolMatches(compID)
	if err != nil {
		return nil, false, err
	}
	for i := range pool {
		if pool[i].ID == matchID {
			m := pool[i]
			return &m, true, nil
		}
	}
	bracket, err := store.LoadBracket(compID)
	if err != nil {
		return nil, false, err
	}
	if bracket == nil {
		return nil, false, nil
	}
	fromBracket := func(bm *state.BracketMatch) (*state.MatchResult, bool, error) {
		r := engine.BracketMatchAsResult(bm)
		r.Court, r.ScheduledAt = bm.Court, bm.ScheduledAt
		return r, true, nil
	}
	for _, round := range bracket.Rounds {
		for i := range round {
			if round[i].ID == matchID {
				return fromBracket(&round[i])
			}
		}
	}
	if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
		return fromBracket(bracket.ThirdPlaceMatch)
	}
	return nil, false, nil
}

// matchPusher is implemented by the Hub: it runs send for a match key under
// the attached coalescer's key lock, cancelling a held running push first.
type matchPusher interface {
	pushMatchNow(matchKey string, send func())
}

// broadcastMatchNow is the ONE door for a match_updated push that is not a
// coalesced running score write: under the match key's lock it cancels any
// held push and sends the freshly read stored match now, so no handler
// broadcasts a result it captured earlier. Event type and payload keys are the
// caller's captured payload's (see freshMatchPush).
func broadcastMatchNow(b Broadcaster, store matchPushStore, compID, matchID string, captured gin.H) {
	send := func() {
		if p, ok := freshMatchPush(store, compID, matchID, captured); ok {
			b.Broadcast(EventMatchUpdated, p)
		}
	}
	if p, ok := b.(matchPusher); ok {
		p.pushMatchNow(compID+":"+matchID, send)
		return
	}
	send()
}

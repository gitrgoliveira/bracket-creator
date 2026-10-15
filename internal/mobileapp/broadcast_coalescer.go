package mobileapp

import (
	"sync"
	"time"
)

// matchBroadcastCoalescer rate-limits match_updated SSE broadcasts for
// "running"-status writes to ≤4/s per match (250ms minimum interval).
//
// C3: C1's 300ms client-side debounce already limits autosave writes to
// ≤3.3/s per match, so this coalescer is defense-in-depth, it prevents
// a reconnect flush (C2's offline queue) or any other burst from fanning
// out more than 4 broadcasts/s to the full SSE subscriber pool.
//
// Semantics: first-wins within the window, with a trailing send. If a running
// broadcast arrives within 250ms of the previous one for the same match,
// Submit holds it (the latest one replaces any held) and sends it once when
// the window ends, so the last state of a burst is always broadcast. Without
// that, a second tap inside the window (a point then its undo) was stored but
// never pushed, and the board stayed stale until the next write. The bare
// Allow keeps the old first-wins-and-drop shape for a caller with nothing to
// send later.
//
// Completed and other non-running broadcasts are never coalesced: Always
// returns true for those. Call sites check status and pass isRunning=false
// for completed writes.
//
// The coalescer is process-scoped. A match's entry is dropped when its first
// non-running (completed/terminal) broadcast passes through Allow, so the map
// is bounded by the set of currently-RUNNING matches (not every match the
// process ever saw), resident size stays negligible.
type matchBroadcastCoalescer struct {
	mu   sync.Mutex
	last map[string]time.Time
	// pending holds, per match key, the latest broadcast dropped inside the
	// window; a key in it has one timer armed to send it.
	pending map[string]func()
}

const matchCoalesceWindow = 250 * time.Millisecond

// coalescerNow is a test seam: TestScoreHandler_C3Coalescer pins it to a
// fixed instant so the 250ms window cannot lapse between two writes on a
// contended CI runner. Production code never reassigns it.
var coalescerNow = time.Now

func newMatchBroadcastCoalescer() *matchBroadcastCoalescer {
	return &matchBroadcastCoalescer{last: make(map[string]time.Time), pending: make(map[string]func())}
}

// Allow returns true when the broadcast should proceed and false when it
// should be coalesced (dropped). isRunning must be true for the rate-limit
// to apply; all other writes pass through unconditionally. matchKey is the
// competition-scoped key (compID:matchID), callers must NOT pass a bare match
// id, or two competitions sharing a match id would share a coalescing window.
func (c *matchBroadcastCoalescer) Allow(matchKey string, isRunning bool) bool {
	if !isRunning {
		// A non-running (completed/terminal) broadcast always proceeds AND ends
		// the match's coalescing window, drop its entry so the map stays bounded
		// by the currently-running matches rather than growing for the process
		// lifetime. A later correction that re-opens the match starts fresh.
		c.mu.Lock()
		delete(c.last, matchKey)
		delete(c.pending, matchKey)
		c.mu.Unlock()
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := coalescerNow()
	if t, ok := c.last[matchKey]; ok && now.Sub(t) < matchCoalesceWindow {
		return false
	}
	c.last[matchKey] = now
	return true
}

// Submit sends now when the broadcast may proceed (see Allow). A running
// broadcast inside the window is held instead, replacing any held earlier, and
// sent once when the window ends: the burst's last state always goes out. A
// non-running broadcast sends at once and discards what was held, since it
// supersedes it. The held send runs on a one-shot timer of at most the window,
// recovered like any spawned goroutine (safeGo); a hub closed meanwhile makes
// its Broadcast a no-op.
func (c *matchBroadcastCoalescer) Submit(matchKey string, isRunning bool, send func()) {
	if c.Allow(matchKey, isRunning) {
		send()
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, armed := c.pending[matchKey]
	c.pending[matchKey] = send
	if armed {
		return
	}
	delay := matchCoalesceWindow
	if t, ok := c.last[matchKey]; ok {
		delay = max(matchCoalesceWindow-coalescerNow().Sub(t), time.Millisecond)
	}
	time.AfterFunc(delay, func() {
		var wg sync.WaitGroup
		safeGo(&wg, nil, func() { c.flushPending(matchKey) })
		wg.Wait()
	})
}

// flushPending sends what Submit held for the key, if a later broadcast has
// not discarded it, and opens a new window from the send.
func (c *matchBroadcastCoalescer) flushPending(matchKey string) {
	c.mu.Lock()
	send := c.pending[matchKey]
	delete(c.pending, matchKey)
	if send != nil {
		c.last[matchKey] = coalescerNow()
	}
	c.mu.Unlock()
	if send != nil {
		send()
	}
}

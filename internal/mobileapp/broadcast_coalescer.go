package mobileapp

import (
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
// Completed and other non-running broadcasts are never coalesced: Allow
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
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.allowLocked(matchKey, isRunning)
}

// allowLocked is Allow under c.mu. A true answer also discards what was held
// for the key: the caller is about to send a newer state, so a timer that fires
// later must not push the older one after it.
func (c *matchBroadcastCoalescer) allowLocked(matchKey string, isRunning bool) bool {
	if !isRunning {
		// A non-running (completed/terminal) broadcast always proceeds AND ends
		// the match's coalescing window, drop its entry so the map stays bounded
		// by the currently-running matches rather than growing for the process
		// lifetime. A later correction that re-opens the match starts fresh.
		delete(c.last, matchKey)
		delete(c.pending, matchKey)
		return true
	}
	now := coalescerNow()
	if t, ok := c.last[matchKey]; ok && now.Sub(t) < matchCoalesceWindow {
		return false
	}
	c.last[matchKey] = now
	delete(c.pending, matchKey)
	return true
}

// Discard drops what Submit held for the key and ends its window. Every other
// match_updated broadcast for the match (a decision, a send back to the queue, a
// reopen, a start) calls it, through discardHeldMatchPush, after its write
// committed and before it broadcasts: the held running state is older than what
// that broadcast announces. It takes the same lock a send runs under, so a send
// already in flight finishes first and the newer broadcast follows it.
func (c *matchBroadcastCoalescer) Discard(matchKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.last, matchKey)
	delete(c.pending, matchKey)
}

// Submit sends now when the broadcast may proceed (see Allow). A running
// broadcast inside the window is held instead, replacing any held earlier, and
// sent once when the window ends: the burst's last state always goes out. A
// non-running broadcast sends at once and discards what was held, since it
// supersedes it, and so does a send that goes straight out. Every send, held or
// not, runs under the coalescer's lock and is expected to read the stored match
// when it runs (freshMatchPush), so the last broadcast of a match is its newest
// state whatever order the requests committed and arrived in. The held send runs
// on a one-shot timer of at most the window, recovered like any spawned
// goroutine (safeGo). A hub closed meanwhile still takes a seq and writes its
// ring when Broadcast runs; no subscriber is left to receive it.
func (c *matchBroadcastCoalescer) Submit(matchKey string, isRunning bool, send func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.allowLocked(matchKey, isRunning) {
		// Sent under the lock: two requests whose commits and Submits crossed
		// each still broadcast in the order they get here, and a send's own
		// re-read of the stored match makes the last one the newest state.
		send()
		return
	}
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
	defer c.mu.Unlock()
	send := c.pending[matchKey]
	delete(c.pending, matchKey)
	if send != nil {
		c.last[matchKey] = coalescerNow()
		// Under the lock, so a broadcast that discards the key waits for this
		// one and cannot be followed by it.
		send()
	}
}

// freshMatchPush builds the match_updated push for a match from what is stored
// NOW, projected as the door projects it (withdrawn status stamped, public
// fields only). A send the coalescer holds or orders runs it when it fires, so
// the push it makes is the match's newest state, never the one the request
// captured before a later write, a decision or a send back to the queue moved
// the match on (bc-evts review round 2). captured is the fallback when the match
// cannot be read, so the push is never lost for want of a read.
func freshMatchPush(store CompetitionStore, compID, matchID string, captured gin.H) gin.H {
	r, ok := readStoredMatch(store, compID, matchID)
	if !ok {
		return captured
	}
	stampWithdrawnStatus(store, compID, r)
	return gin.H{"competitionId": compID, "matchId": matchID, "result": matchPtrForBroadcast(r)}
}

// readStoredMatch reads one match from the pool file, else from the bracket
// (round matches and the 3rd-place match), as a MatchResult. A bracket match
// carries its court and schedule, which the engine projection leaves out.
func readStoredMatch(store CompetitionStore, compID, matchID string) (*state.MatchResult, bool) {
	pool, err := store.LoadPoolMatches(compID)
	if err != nil {
		return nil, false
	}
	for i := range pool {
		if pool[i].ID == matchID {
			m := pool[i]
			return &m, true
		}
	}
	bracket, err := store.LoadBracket(compID)
	if err != nil || bracket == nil {
		return nil, false
	}
	fromBracket := func(bm *state.BracketMatch) (*state.MatchResult, bool) {
		r := engine.BracketMatchAsResult(bm)
		r.Court, r.ScheduledAt = bm.Court, bm.ScheduledAt
		return r, true
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
	return nil, false
}

// heldPushDiscarder is what a hub offers a handler that is about to broadcast a
// match_updated of its own: the coalescer attached to it, if any.
type heldPushDiscarder interface {
	discardHeldMatchPush(matchKey string)
}

// discardHeldMatchPush is the ONE call every direct match_updated broadcast
// makes (a decision, a send back to the queue, a reopen, a start, the
// representative bout's add and remove, a reopened downstream match) AFTER its
// write committed and BEFORE it broadcasts: whatever running push the
// coalescer still holds for the match is older than what is about to be
// announced, and would otherwise be pushed after it. A hub with no coalescer
// attached (a test double) has nothing held.
func discardHeldMatchPush(b Broadcaster, compID, matchID string) {
	if d, ok := b.(heldPushDiscarder); ok {
		d.discardHeldMatchPush(compID + ":" + matchID)
	}
}

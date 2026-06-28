package mobileapp

import (
	"sync"
	"time"
)

// matchBroadcastCoalescer rate-limits match_updated SSE broadcasts for
// "running"-status writes to ≤4/s per match (250ms minimum interval).
//
// C3: C1's 300ms client-side debounce already limits autosave writes to
// ≤3.3/s per match, so this coalescer is defense-in-depth — it prevents
// a reconnect flush (C2's offline queue) or any other burst from fanning
// out more than 4 broadcasts/s to the full SSE subscriber pool.
//
// Semantics: first-wins within the window. If a running broadcast arrives
// within 250ms of the previous one for the same match, Allow returns false
// and the caller skips the fan-out. The next client-side debounce write
// (~300ms later) will carry the freshest state, so the worst-case viewer
// lag is one additional debounce period — acceptable for live scoring.
//
// Completed and other non-running broadcasts are never coalesced: Always
// returns true for those. Call sites check status and pass isRunning=false
// for completed writes.
//
// The coalescer is process-scoped. A match's entry is dropped when its first
// non-running (completed/terminal) broadcast passes through Allow, so the map
// is bounded by the set of currently-RUNNING matches (not every match the
// process ever saw) — resident size stays negligible.
type matchBroadcastCoalescer struct {
	mu   sync.Mutex
	last map[string]time.Time
}

const matchCoalesceWindow = 250 * time.Millisecond

func newMatchBroadcastCoalescer() *matchBroadcastCoalescer {
	return &matchBroadcastCoalescer{last: make(map[string]time.Time)}
}

// Allow returns true when the broadcast should proceed and false when it
// should be coalesced (dropped). isRunning must be true for the rate-limit
// to apply; all other writes pass through unconditionally. matchKey is the
// competition-scoped key (compID:matchID) — callers must NOT pass a bare match
// id, or two competitions sharing a match id would share a coalescing window.
func (c *matchBroadcastCoalescer) Allow(matchKey string, isRunning bool) bool {
	if !isRunning {
		// A non-running (completed/terminal) broadcast always proceeds AND ends
		// the match's coalescing window — drop its entry so the map stays bounded
		// by the currently-running matches rather than growing for the process
		// lifetime. A later correction that re-opens the match starts fresh.
		c.mu.Lock()
		delete(c.last, matchKey)
		c.mu.Unlock()
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if t, ok := c.last[matchKey]; ok && now.Sub(t) < matchCoalesceWindow {
		return false
	}
	c.last[matchKey] = now
	return true
}

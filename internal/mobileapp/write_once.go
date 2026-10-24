package mobileapp

import (
	"fmt"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// writeOnce is how the doors the server orders by ARRIVAL, not by a stamp (a
// court move, a time change, a lineup save, a member's rename or cleared name),
// answer a late duplicate (bc-evts stage 3 review 11, Y8). The outbox sends
// every write with a writeId and sends it again, with the same id, when its
// request was given up on at the deadline; the first request can still land
// after the resend and after the next write of the same match or team, and
// would then put an older value back. Each door records the writeIds it has
// applied and answers one it has seen as recorded, applying nothing.
//
// The record is kept in memory, for writeOnceKeep and at most writeOnceMax ids:
// a late duplicate is a request this same process is still working on or
// received a deadline ago, and a restart ends every request it held, so
// nothing older can arrive. A write the door refuses is not recorded, so its
// resend is judged again. Writes of one target (one match, or one team's
// lineup or member) are serialised while they are judged, so a duplicate
// arriving while its first copy is being applied waits for it and is then
// answered as recorded. The lock is the target's own (busy), so writes of
// different targets never wait on each other.
type writeOnce struct {
	mu   sync.Mutex // guards busy, seen and order; never held across apply
	busy map[string]*targetLock
	seen map[string]time.Time
	// order is every record in the order it was made, so in time order: the
	// oldest is at head, and expiry and the bound take from there. An entry
	// whose key was recorded again since is stale and is skipped.
	order []onceRecord
	head  int
	now   func() time.Time
}

// targetLock serialises the writes of one target; refs counts the callers
// holding or waiting for it, and the last one out removes it.
type targetLock struct {
	mu   sync.Mutex
	refs int
}

type onceRecord struct {
	key string
	at  time.Time
}

const (
	writeOnceKeep = 15 * time.Minute
	writeOnceMax  = 16384
)

func newWriteOnce() *writeOnce {
	return &writeOnce{busy: map[string]*targetLock{}, seen: map[string]time.Time{}, now: time.Now}
}

// validWriteID refuses a writeId no client mints (a 400 for the request).
func validWriteID(id string) error {
	if len(id) > domain.MaxScoreEventIDLen {
		return fmt.Errorf("writeId is longer than %d characters", domain.MaxScoreEventIDLen)
	}
	return nil
}

// do runs apply for the first arrival of writeID at target and records the
// id when apply reports it applied; it answers false, running nothing, for an
// id already recorded there. An empty writeID (an older client) always runs.
func (g *writeOnce) do(target, writeID string, apply func() bool) (ran bool) {
	if g == nil || writeID == "" {
		apply()
		return true
	}
	tl := g.lockTarget(target)
	defer g.unlockTarget(target, tl)
	key := target + "\x1f" + writeID
	if g.has(key) {
		return false
	}
	if apply() {
		g.record(key)
	}
	return true
}

// lockTarget takes target's own lock, making it on first use.
func (g *writeOnce) lockTarget(target string) *targetLock {
	g.mu.Lock()
	tl := g.busy[target]
	if tl == nil {
		tl = &targetLock{}
		g.busy[target] = tl
	}
	tl.refs++
	g.mu.Unlock()
	tl.mu.Lock()
	return tl
}

// unlockTarget releases target's lock, and forgets it once nobody holds or
// waits for it.
func (g *writeOnce) unlockTarget(target string, tl *targetLock) {
	tl.mu.Unlock()
	g.mu.Lock()
	if tl.refs--; tl.refs == 0 {
		delete(g.busy, target)
	}
	g.mu.Unlock()
}

func (g *writeOnce) has(key string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	at, ok := g.seen[key]
	return ok && g.now().Sub(at) < writeOnceKeep
}

func (g *writeOnce) record(key string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	// The expired go first, oldest at the head; then, while still full, the
	// oldest. Each record is taken off the head once, so a record costs O(1)
	// over time however full the set is.
	for g.head < len(g.order) {
		r := g.order[g.head]
		live := g.seen[r.key].Equal(r.at)
		if live && now.Sub(r.at) < writeOnceKeep && len(g.seen) < writeOnceMax {
			break
		}
		if live {
			delete(g.seen, r.key)
		}
		g.order[g.head] = onceRecord{}
		g.head++
	}
	if g.head > len(g.order)/2 {
		g.order = append(g.order[:0], g.order[g.head:]...)
		g.head = 0
	}
	g.seen[key] = now
	g.order = append(g.order, onceRecord{key: key, at: now})
}

// seenAt and remember are do's two halves, for a door that judges its write
// under a lock of its own (the lineup save, under the competition's
// transaction): seenAt answers whether writeID was applied at target, and
// remember records it once the write has committed.
func (g *writeOnce) seenAt(target, writeID string) bool {
	return g != nil && writeID != "" && g.has(target+"\x1f"+writeID)
}

func (g *writeOnce) remember(target, writeID string) {
	if g != nil && writeID != "" {
		g.record(target + "\x1f" + writeID)
	}
}

// poolRanksTarget names a competition's pool-rank overrides for writeOnce: a
// chusen's order and a reset of the overrides are ordered together.
func poolRanksTarget(compID string) string {
	return "pool-ranks\x1f" + compID
}

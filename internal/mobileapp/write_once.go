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
// answered as recorded.
type writeOnce struct {
	stripes [64]sync.Mutex
	mu      sync.Mutex
	seen    map[string]time.Time
	now     func() time.Time
}

const (
	writeOnceKeep = 15 * time.Minute
	writeOnceMax  = 16384
)

func newWriteOnce() *writeOnce {
	return &writeOnce{seen: map[string]time.Time{}, now: time.Now}
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
	// FNV-1a over the target picks its stripe.
	h := uint32(2166136261)
	for i := 0; i < len(target); i++ {
		h ^= uint32(target[i])
		h *= 16777619
	}
	stripe := &g.stripes[h%uint32(len(g.stripes))]
	stripe.Lock()
	defer stripe.Unlock()
	key := target + "\x1f" + writeID
	if g.has(key) {
		return false
	}
	if apply() {
		g.record(key)
	}
	return true
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
	if len(g.seen) >= writeOnceMax {
		for k, at := range g.seen {
			if now.Sub(at) >= writeOnceKeep {
				delete(g.seen, k)
			}
		}
		// Still full: the oldest go first.
		for len(g.seen) >= writeOnceMax {
			oldest, oldestAt := "", now
			for k, at := range g.seen {
				if !at.After(oldestAt) {
					oldest, oldestAt = k, at
				}
			}
			delete(g.seen, oldest)
		}
	}
	g.seen[key] = now
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

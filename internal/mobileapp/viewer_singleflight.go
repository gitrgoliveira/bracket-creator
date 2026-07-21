package mobileapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime/debug"
	"sync"

	"github.com/gin-gonic/gin"
)

// errNotFound is a sentinel returned by the sf.Do fn inside
// GET /api/viewer/competitions/:id when the competition does not exist.
// The handler maps it to HTTP 404; all other non-nil errors become 500.
var errNotFound = errors.New("not found")

// viewerSingleFlight collapses concurrent identical builds of the expensive
// polled viewer endpoints (GET /api/viewer/competitions, its /:id variant, and
// the court feed GET /api/viewer/court/:court/matches) to a single in-flight
// execution per key.
//
// Staleness bound vs SSE: a key stays in-flight only while the elected
// caller is still executing. The moment it finishes, the result is returned
// to all waiters and the key is removed, so any request arriving after
// completion re-executes. The maximum response age is therefore the latency
// of one build, not a fixed TTL. Note the bound is not zero: a refetch
// triggered by an SSE event can attach to a build elected just before the
// write that fired the event and receive pre-write data — stale by at most
// that one in-flight build, corrected on the next request.
//
// Scalability goal (P2, mp-9afd): 1000 concurrent GET /api/viewer/competitions
// arriving within the same 500ms SSE fan-out window collapse to O(1) builds
// instead of O(1000). Each extra caller blocks briefly on a WaitGroup
// rather than spinning up a full fan-out goroutine set.
type viewerSingleFlight struct {
	mu    sync.Mutex
	calls map[string]*sfCall
}

type sfCall struct {
	wg  sync.WaitGroup
	val []byte
	err error
}

// newViewerSingleFlight constructs a ready-to-use flight group.
func newViewerSingleFlight() *viewerSingleFlight {
	return &viewerSingleFlight{calls: make(map[string]*sfCall)}
}

// sfTestWaiterAttached, when non-nil, is invoked each time a Do caller finds
// an in-flight build and attaches as a waiter (after releasing the mutex,
// before blocking on the WaitGroup). Test-only hook, nil in production: it
// lets the collapse tests release the elected builder only once every
// concurrent caller has provably attached, making the exactly-one-election
// assertion deterministic instead of resting on a wall-clock hold.
var sfTestWaiterAttached func(key string)

// Do executes fn if no call for key is already in-flight, or waits for the
// in-flight call to complete and returns its result. fn must be safe to call
// concurrently under different keys. The returned bytes must not be modified
// by the caller.
//
// If fn panics, the panic is recovered and converted to an error so that
// waiting callers are unblocked and the key is cleaned up. The elected caller
// receives the error; it does not re-panic.
func (g *viewerSingleFlight) Do(key string, fn func() ([]byte, error)) (v []byte, err error) {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		// A call for this key is already in-flight, attach and wait.
		g.mu.Unlock()
		if sfTestWaiterAttached != nil {
			sfTestWaiterAttached(key)
		}
		c.wg.Wait()
		return c.val, c.err
	}
	c := &sfCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	// We are the elected caller, execute fn and broadcast to waiters.
	// Deferred cleanup guarantees key removal + wg.Done even on panic.
	// ORDERING: delete the key under the mutex BEFORE calling wg.Done.
	// If wg.Done ran first, a new caller could find the key in the map,
	// call c.wg.Wait() (which returns immediately since the WaitGroup is
	// already at zero), and receive the old result, violating the
	// guarantee that a request arriving after a build completes always
	// re-executes fn for fresh data.
	defer func() {
		if r := recover(); r != nil {
			c.err = fmt.Errorf("singleflight: fn panicked: %v", r)
			err = c.err // set named return so the elected caller also gets the error
			// Log with stack trace so production crashes are diagnosable,
			// mirrors the safeGo pattern in safego.go. The error returned
			// to handlers becomes a generic 500, which would otherwise mask
			// the root cause entirely.
			log.Printf("singleflight: recovered panic for key %q: %v\n%s", key, r, debug.Stack())
		}
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
		c.wg.Done() // unblock waiters only after key is gone
	}()

	c.val, c.err = fn()
	var rp *recoveredPanic
	if c.err != nil && !errors.Is(c.err, errNotFound) && !errors.As(c.err, &rp) {
		// Logged once per BUILD, not per waiter: with N callers collapsed
		// onto this build, logging in the response tail would emit N
		// identical lines per failure wave (unbounded under a sustained
		// store fault at high client counts). errNotFound is the
		// /competitions/:id 404 sentinel, not a fault; a *recoveredPanic
		// was already logged with its stack at the safeGo recovery site.
		log.Printf("singleflight: build for key %q failed: %v", key, c.err)
	}
	return c.val, c.err
}

// serveSingleFlightJSON writes a Do result to the response: marshalled JSON
// bytes on success, a generic 500 on error. The error is deliberately NOT
// logged here: this tail runs once per WAITER, so a collapsed wave of N
// callers would emit N identical lines; each failed build is already logged
// exactly once (store faults by Do, panics with a stack at their recovery
// site). Handlers with an extra error mapping (e.g. errNotFound → 404 on
// /competitions/:id) branch on that first and fall through here for the
// rest.
func serveSingleFlightJSON(c *gin.Context, data []byte, err error) {
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", data)
}

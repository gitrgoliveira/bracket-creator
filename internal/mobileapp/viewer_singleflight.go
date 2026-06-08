package mobileapp

import (
	"errors"
	"fmt"
	"sync"
)

// errNotFound is a sentinel returned by the sf.Do fn inside
// GET /viewer/competitions/:id when the competition does not exist.
// The handler maps it to HTTP 404; all other non-nil errors become 500.
var errNotFound = errors.New("not found")

// viewerSingleFlight collapses concurrent identical GET /viewer/competitions
// (and /viewer/competitions/:id) builds to a single in-flight execution.
//
// Correctness vs SSE staleness: a key stays in-flight only while the first
// caller is still executing. The moment it finishes, the result is returned
// to all waiters and the key is removed. A subsequent request (e.g. the next
// SSE fan-out wave) always re-executes. This means the maximum response age
// is the latency of one build, not a fixed TTL — there is never stale data
// served across SSE boundaries.
//
// Scalability goal (P2, mp-9afd): 1000 concurrent GET /viewer/competitions
// arriving within the same 500ms SSE fan-out window collapse to O(1) builds
// instead of O(1000). Each extra caller blocks briefly on a channel receive
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
		// A call for this key is already in-flight — attach and wait.
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &sfCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	// We are the elected caller — execute fn and broadcast to waiters.
	// Deferred cleanup guarantees wg.Done + key removal even on panic.
	defer func() {
		if r := recover(); r != nil {
			c.err = fmt.Errorf("singleflight: fn panicked: %v", r)
			err = c.err // set named return so the elected caller also gets the error
		}
		c.wg.Done()
		g.mu.Lock()
		delete(g.calls, key)
		g.mu.Unlock()
	}()

	c.val, c.err = fn()
	return c.val, c.err
}

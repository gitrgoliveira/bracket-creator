package mobileapp

import (
	"bytes"
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

// viewerFlightKey is a single-flight key for one audience's answer: the
// build's own key (what it reads) plus the audience's whole key
// (audience.key: the organiser, the public, or the reveal scopes held, sorted),
// so two callers share a build only when they are answered the same bytes. A
// masked payload can never reach the organiser, nor a revealed name a
// caller whose links do not reveal it. The audience is judged (and any
// invalid, expired or wrong-kind key dropped) before the key is formed.
//
// Trade-off, set against the plan's "build once per audience class, overlay the
// per-competitor reveals": a build here is one answer per audience, not one
// masked copy with overlays laid on it, because the mask rewrites the loaded
// payload in place and a shared copy cannot be handed to two audiences. The
// herd is still collapsed, since the public and the holders of the same links
// share a key, and only holders of per-person links in a hiding competition
// pay a build of their own; viewerFlightKeyFor narrows the key to one
// competition for the read that is about one.
func viewerFlightKey(base string, aud audience) string {
	return base + "#" + aud.key()
}

// viewerFlightKeyFor is viewerFlightKey for a read of one competition: the
// audience counts only as that competition's answer sees it (audience.keyFor),
// so a caller holding a link to another competition shares the public's build.
func viewerFlightKeyFor(base, compID string, aud audience) string {
	return base + "#" + aud.keyFor(compID)
}

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
// that one in-flight build, corrected on the next request. Such an answer
// names the seq its build read before the write's event (X-Event-Seq), so a
// page that skips covered events still refetches for that one.
//
// Scalability goal (P2, mp-9afd): 1000 concurrent GET /api/viewer/competitions
// arriving within the same 500ms SSE fan-out window collapse to O(1) builds
// instead of O(1000). Each extra caller blocks briefly on a WaitGroup
// rather than spinning up a full fan-out goroutine set.
type viewerSingleFlight struct {
	mu    sync.Mutex
	calls map[string]*sfCall
	// seq reads the event stream's head seq (headSeq) at the start of each
	// build, so every caller of one build is told the same position.
	seq func() int64
}

type sfCall struct {
	wg  sync.WaitGroup
	val []byte
	seq int64
	err error

	// gz is val gzipped, made at most once per build by the first caller
	// that accepts gzip (gzOnce), so a fan-out of N callers costs one
	// compression, not N. Nil when val is under gzipMinBytes or the
	// compression failed: those callers get val as it is.
	gzOnce sync.Once
	gz     []byte
}

// sfResult is what one caller of a build is answered: the JSON bytes, the
// same bytes gzipped when the caller asked for them and they are worth
// compressing, and the event seq the build's read began at (X-Event-Seq).
// The byte slices are shared by every caller and must not be modified.
type sfResult struct {
	body []byte
	gz   []byte
	seq  int64
}

// newViewerSingleFlight constructs a ready-to-use flight group whose builds
// read their event seq from seq (headSeq over the router's hub).
func newViewerSingleFlight(seq func() int64) *viewerSingleFlight {
	return &viewerSingleFlight{calls: make(map[string]*sfCall), seq: seq}
}

// sfTestWaiterAttached, when non-nil, is invoked each time a Do caller finds
// an in-flight build and attaches as a waiter (after releasing the mutex,
// before blocking on the WaitGroup). Test-only hook, nil in production: it
// lets the collapse tests release the elected builder only once every
// concurrent caller has provably attached, making the exactly-one-election
// assertion deterministic instead of resting on a wall-clock hold.
var sfTestWaiterAttached func(key string)

// sfTestCompressed, when non-nil, is invoked each time a build's bytes are
// gzipped. Test-only hook, nil in production: it lets the tests count one
// compression per build however many callers wait on it.
var sfTestCompressed func(key string)

// Do executes fn if no call for key is already in-flight, or waits for the
// in-flight call to complete and returns its result. fn must be safe to call
// concurrently under different keys. The returned bytes must not be modified
// by the caller.
//
// The elected caller reads the event seq before fn runs, so the answer holds
// every change announced at or below it. When wantGzip is set (the caller
// accepts gzip), the result also carries the build's bytes gzipped, made
// once per build whichever caller asks first.
//
// If fn panics, the panic is recovered and converted to an error so that
// waiting callers are unblocked and the key is cleaned up. The elected caller
// receives the error; it does not re-panic.
func (g *viewerSingleFlight) Do(key string, wantGzip bool, fn func() ([]byte, error)) (sfResult, error) {
	c := g.call(key, fn)
	if c.err != nil {
		return sfResult{}, c.err
	}
	res := sfResult{body: c.val, seq: c.seq}
	if wantGzip {
		res.gz = c.gzipped(key)
	}
	return res, nil
}

// call runs or joins the build for key and returns it once it is done.
func (g *viewerSingleFlight) call(key string, fn func() ([]byte, error)) *sfCall {
	g.mu.Lock()
	if c, ok := g.calls[key]; ok {
		// A call for this key is already in-flight, attach and wait.
		g.mu.Unlock()
		if sfTestWaiterAttached != nil {
			sfTestWaiterAttached(key)
		}
		c.wg.Wait()
		return c
	}
	c := &sfCall{}
	c.wg.Add(1)
	g.calls[key] = c
	g.mu.Unlock()

	g.build(key, c, fn)
	return c
}

// build runs fn for the elected caller and releases the waiters.
func (g *viewerSingleFlight) build(key string, c *sfCall, fn func() ([]byte, error)) {
	// Deferred cleanup guarantees key removal + wg.Done even on panic.
	// ORDERING: delete the key under the mutex BEFORE calling wg.Done.
	// If wg.Done ran first, a new caller could find the key in the map,
	// call c.wg.Wait() (which returns immediately since the WaitGroup is
	// already at zero), and receive the old result, violating the
	// guarantee that a request arriving after a build completes always
	// re-executes fn for fresh data.
	defer func() {
		if r := recover(); r != nil {
			c.val = nil
			c.err = fmt.Errorf("singleflight: fn panicked: %v", r)
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

	// The seq comes first: read after the build, it could name an event
	// whose change the build did not see.
	c.seq = g.seq()
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
}

// gzipped answers the build's bytes gzipped, compressing them on the first
// call only. It runs after the build is done (val is no longer written), and
// gzOnce makes every concurrent caller wait for the one compression. Nil
// means the plain bytes are sent: an answer under gzipMinBytes, which the
// middleware would not compress either, or a compression that failed.
func (c *sfCall) gzipped(key string) []byte {
	c.gzOnce.Do(func() {
		if len(c.val) < gzipMinBytes {
			return
		}
		if sfTestCompressed != nil {
			sfTestCompressed(key)
		}
		var buf bytes.Buffer
		zw := getGzipWriter(&buf)
		if _, err := zw.Write(c.val); err != nil {
			log.Printf("singleflight: gzip for key %q failed: %v", key, err)
			return
		}
		if err := zw.Close(); err != nil {
			log.Printf("singleflight: gzip for key %q failed: %v", key, err)
			return
		}
		putGzipWriter(zw)
		c.gz = buf.Bytes()
	})
	return c.gz
}

// serveSingleFlightJSON writes a Do result to the response: the JSON bytes
// on success, gzipped when the result carries them (the middleware leaves
// an answer already encoded alone), with the build's event seq; a generic
// 500 on error. Vary names Accept-Encoding on both paths, merged with what
// the audience judge set. The error is deliberately NOT logged here: this
// tail runs once per WAITER, so a collapsed wave of N callers would emit N
// identical lines; each failed build is already logged exactly once (store
// faults by Do, panics with a stack at their recovery site). Handlers with
// an extra error mapping (e.g. errNotFound → 404 on /competitions/:id)
// branch on that first and fall through here for the rest.
func serveSingleFlightJSON(c *gin.Context, res sfResult, err error) {
	h := c.Writer.Header()
	addVaryAcceptEncoding(h)
	if err != nil {
		// Through the shared helper rather than a hand-rolled 500: this path
		// used to swallow the cause entirely (no log line at all), and a
		// corrupt competition file reaching it deserves the same located
		// message every other handler now gives.
		internalError(c, err)
		return
	}
	setEventSeq(c, res.seq)
	if res.gz != nil {
		h.Set("Content-Encoding", "gzip")
		c.Data(http.StatusOK, "application/json; charset=utf-8", res.gz)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", res.body)
}

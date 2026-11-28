package mobileapp

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestViewerSingleFlight_CollatesConcurrentBuilds is the acceptance test for
// P2 (mp-9afd): N concurrent calls with the same key during a single
// in-flight build must collapse to exactly 1 real build invocation while all
// callers receive the correct result.
//
// Synchronisation strategy (deterministic, no wall-clock hold): the elected
// caller's fn blocks on a gate channel that the sfTestWaiterAttached hook
// closes only once every other caller has provably attached as a waiter. No
// caller can arrive "late" and elect a second build, because the first
// build cannot complete until all other callers are already waiting — so
// the exactly-1 assertion cannot flake on scheduling.
func TestViewerSingleFlight_CollatesConcurrentBuilds(t *testing.T) {
	const concurrency = 100
	g := newViewerSingleFlight(zeroSeq)

	var buildCount atomic.Int32

	gate := make(chan struct{})
	var attached atomic.Int32
	sfTestWaiterAttached = func(string) {
		if attached.Add(1) == concurrency-1 {
			close(gate)
		}
	}
	t.Cleanup(func() { sfTestWaiterAttached = nil })

	type result struct {
		data []byte
		err  error
	}
	results := make([]result, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := g.Do("test-key", false, func() ([]byte, error) {
				buildCount.Add(1)
				// Hold the in-flight slot until every other caller has
				// attached as a waiter (released by the hook above). The
				// timeout only exists for the FAILURE path: if collapse ever
				// regresses (a second election strands `attached` below the
				// release threshold), fail fast on the buildCount assertion
				// instead of deadlocking the package until the go-test
				// timeout.
				select {
				case <-gate:
				case <-time.After(10 * time.Second):
					t.Error("gate not released within 10s: not all callers attached as waiters (collapse regression?)")
				}
				return []byte(`["ok"]`), nil
			})
			results[i] = result{res.body, err}
		}()
	}

	wg.Wait()

	// Only one build should have executed.
	assert.Equalf(t, int32(1), buildCount.Load(),
		"expected exactly 1 build invocation; got %d (thundering-herd not suppressed)",
		buildCount.Load())

	// All callers must have received the correct bytes with no error.
	for i, r := range results {
		require.NoErrorf(t, r.err, "goroutine %d got an error", i)
		assert.Equalf(t, []byte(`["ok"]`), r.data, "goroutine %d got wrong data", i)
	}
}

// TestViewerSingleFlight_DifferentKeysAreIndependent ensures that concurrent
// calls for distinct keys each execute their own fn without interference.
func TestViewerSingleFlight_DifferentKeysAreIndependent(t *testing.T) {
	g := newViewerSingleFlight(zeroSeq)

	type result struct {
		data []byte
		err  error
	}
	const nKeys = 10
	results := make([]result, nKeys)
	var wg sync.WaitGroup

	for i := 0; i < nKeys; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("comp:%d", i)
			res, err := g.Do(key, false, func() ([]byte, error) {
				return []byte(fmt.Sprintf(`{"id":%d}`, i)), nil
			})
			results[i] = result{res.body, err}
		}()
	}
	wg.Wait()

	for i, r := range results {
		require.NoErrorf(t, r.err, "key %d got an error", i)
		assert.Equalf(t, []byte(fmt.Sprintf(`{"id":%d}`, i)), r.data, "key %d got wrong data", i)
	}
}

// TestViewerSingleFlight_SubsequentCallsReExecute verifies that after an
// in-flight build completes, a new independent request re-executes fn (the
// key is not permanently cached, there is no stale-data risk).
func TestViewerSingleFlight_SubsequentCallsReExecute(t *testing.T) {
	g := newViewerSingleFlight(zeroSeq)

	var count atomic.Int32
	do := func() ([]byte, error) {
		count.Add(1)
		return []byte("ok"), nil
	}

	_, err := g.Do("k", false, do)
	require.NoError(t, err)
	assert.Equal(t, int32(1), count.Load())

	_, err = g.Do("k", false, do)
	require.NoError(t, err)
	assert.Equal(t, int32(2), count.Load(), "expected fn to re-execute after first call completed")
}

// TestViewerSingleFlight_ErrorPropagatedToAllWaiters checks that when the
// elected fn returns an error, all concurrent waiters receive the same error.
// The fn stays in-flight long enough for all other goroutines to attach as
// waiters, and we assert exactly one build ran, confirming the waiter path
// is reliably exercised.
func TestViewerSingleFlight_ErrorPropagatedToAllWaiters(t *testing.T) {
	const concurrency = 50
	g := newViewerSingleFlight(zeroSeq)

	var buildCount atomic.Int32

	// readyBarrier ensures all goroutines are spawned before they race
	// into Do together, same pattern as CollatesConcurrentBuilds.
	var readyBarrier sync.WaitGroup
	readyBarrier.Add(concurrency)

	errs := make([]error, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			readyBarrier.Done()
			readyBarrier.Wait() // all goroutines race into Do together
			_, err := g.Do("err-key", false, func() ([]byte, error) {
				buildCount.Add(1)
				// Hold the in-flight slot so all other goroutines enter
				// Do and become waiters before the error is returned.
				time.Sleep(200 * time.Millisecond)
				return nil, fmt.Errorf("build failed")
			})
			errs[i] = err
		}()
	}
	wg.Wait()

	// Only one build should have executed, confirms waiters attached.
	assert.Equalf(t, int32(1), buildCount.Load(),
		"expected exactly 1 build invocation; got %d", buildCount.Load())

	for i, err := range errs {
		require.Errorf(t, err, "goroutine %d expected an error", i)
		assert.EqualErrorf(t, err, "build failed", "goroutine %d got unexpected error", i)
	}
}

// TestViewerSingleFlight_PanicRecoveredToError verifies that a panic inside
// the elected fn is recovered, the key is cleaned up, and all waiters receive
// an error instead of deadlocking.
func TestViewerSingleFlight_PanicRecoveredToError(t *testing.T) {
	const concurrency = 20
	g := newViewerSingleFlight(zeroSeq)

	// readyBarrier ensures all goroutines are spawned before they race
	// into Do together.
	var readyBarrier sync.WaitGroup
	readyBarrier.Add(concurrency)

	type result struct {
		data []byte
		err  error
	}
	results := make([]result, concurrency)
	var wg sync.WaitGroup

	for i := 0; i < concurrency; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			readyBarrier.Done()
			readyBarrier.Wait()
			res, err := g.Do("panic-key", false, func() ([]byte, error) {
				// Hold the slot briefly so all other goroutines attach,
				// then panic. The singleflight must recover the panic,
				// convert it to an error, and unblock all waiters.
				time.Sleep(100 * time.Millisecond)
				panic("kaboom")
			})
			results[i] = result{res.body, err}
		}()
	}

	wg.Wait()

	// All callers must get an error (not deadlock), and the error must
	// mention the panic value.
	for i, r := range results {
		require.Errorf(t, r.err, "goroutine %d expected an error from panic recovery", i)
		assert.Containsf(t, r.err.Error(), "kaboom",
			"goroutine %d error should contain the panic value", i)
		assert.Nilf(t, r.data, "goroutine %d should have nil data after panic", i)
	}

	// The key must be cleaned up, a subsequent call should execute normally.
	res, err := g.Do("panic-key", false, func() ([]byte, error) {
		return []byte("recovered"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("recovered"), res.body, "key should be usable after panic")
}

// TestViewerFlightKeySeparatesAudiences (bc-hnam): two callers share a viewer
// build only when they are answered the same bytes, so the organiser, the
// public and each reveal scope get keys of their own.
func TestViewerFlightKeySeparatesAudiences(t *testing.T) {
	public := viewerFlightKey("competitions", audience{})
	organiser := viewerFlightKey("competitions", organiserAudience())
	link := viewerFlightKey("competitions", audience{competitions: map[string]bool{"k": true}})
	person := viewerFlightKey("competitions", audience{competitors: map[string]map[string]bool{"k": {"p1": true}}})
	other := viewerFlightKey("competitions", audience{competitors: map[string]map[string]bool{"k": {"p2": true}}})
	keys := map[string]bool{public: true, organiser: true, link: true, person: true, other: true}
	if len(keys) != 5 {
		t.Fatalf("audiences share a key: %q %q %q %q %q", public, organiser, link, person, other)
	}
	if viewerFlightKey("competitions", audience{}) != public {
		t.Fatal("one audience must keep one key")
	}
}

// A read of one competition keys the audience as that competition's answer
// sees it: a link or a per-person link to ANOTHER competition shares the
// public's build, while this competition's own links and the organiser keep
// keys of their own.
func TestViewerFlightKeyFor_CountsOnlyTheCompetitionsOwnReveals(t *testing.T) {
	const base = "competition:k@1"
	public := viewerFlightKeyFor(base, "k", audience{})
	elsewhere := audience{
		competitions: map[string]bool{"other": true},
		competitors:  map[string]map[string]bool{"other": {"p1": true}},
	}
	if viewerFlightKeyFor(base, "k", elsewhere) != public {
		t.Fatal("a link to another competition must share the public's build")
	}
	link := viewerFlightKeyFor(base, "k", audience{competitions: map[string]bool{"k": true}})
	person := viewerFlightKeyFor(base, "k", audience{competitors: map[string]map[string]bool{"k": {"p1": true}}})
	other := viewerFlightKeyFor(base, "k", audience{competitors: map[string]map[string]bool{"k": {"p2": true}}})
	both := viewerFlightKeyFor(base, "k", audience{competitors: map[string]map[string]bool{"k": {"p2": true, "p1": true}}})
	organiser := viewerFlightKeyFor(base, "k", organiserAudience())
	keys := map[string]bool{public: true, link: true, person: true, other: true, both: true, organiser: true}
	if len(keys) != 6 {
		t.Fatalf("audiences that read different bytes share a key: %v", keys)
	}
}

// zeroSeq is the event seq of a flight group whose hub has broadcast nothing.
func zeroSeq() int64 { return 0 }

// bc-sseq FR3: the elected caller reads the event seq BEFORE its build runs,
// and every caller of the build is told that one seq. A broadcast landing
// during the build (the seq moving on while fn runs) is above the answer's
// seq, so the page still refetches for it.
func TestViewerSingleFlight_SeqIsReadBeforeTheBuildAndShared(t *testing.T) {
	const concurrency = 20
	var head atomic.Int64
	head.Store(7)
	g := newViewerSingleFlight(head.Load)

	gate := make(chan struct{})
	var attached atomic.Int32
	sfTestWaiterAttached = func(string) {
		if attached.Add(1) == concurrency-1 {
			close(gate)
		}
	}
	t.Cleanup(func() { sfTestWaiterAttached = nil })

	seqs := make([]int64, concurrency)
	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := g.Do("seq-key", false, func() ([]byte, error) {
				head.Add(1) // an event announced while the build runs
				select {
				case <-gate:
				case <-time.After(10 * time.Second):
					t.Error("gate not released within 10s")
				}
				return []byte(`{}`), nil
			})
			assert.NoError(t, err)
			seqs[i] = res.seq
		}()
	}
	wg.Wait()
	for i, s := range seqs {
		assert.Equalf(t, int64(7), s, "caller %d must be told the seq read before the build", i)
	}

	res, err := g.Do("seq-key", false, func() ([]byte, error) { return []byte(`{}`), nil })
	require.NoError(t, err)
	assert.Equal(t, int64(8), res.seq, "the next build reads the seq afresh")
}

// bc-sseq FR2: a shared build is compressed at most once, by the first
// caller that accepts gzip, however many callers wait on it; gzip and plain
// callers mixed, so the race detector sees the waiters read the gzipped
// bytes while the one compression is made.
func TestViewerSingleFlight_CompressesOncePerBuild(t *testing.T) {
	const concurrency = 64
	g := newViewerSingleFlight(zeroSeq)
	body := bigJSON(t, 200)
	require.GreaterOrEqual(t, len(body), gzipMinBytes)

	var compressed atomic.Int32
	sfTestCompressed = func(string) { compressed.Add(1) }
	gate := make(chan struct{})
	var attached atomic.Int32
	sfTestWaiterAttached = func(string) {
		if attached.Add(1) == concurrency-1 {
			close(gate)
		}
	}
	t.Cleanup(func() { sfTestCompressed, sfTestWaiterAttached = nil, nil })

	results := make([]sfResult, concurrency)
	var wg sync.WaitGroup
	for i := range concurrency {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := g.Do("gz-key", i%2 == 0, func() ([]byte, error) {
				select {
				case <-gate:
				case <-time.After(10 * time.Second):
					t.Error("gate not released within 10s")
				}
				return body, nil
			})
			assert.NoError(t, err)
			results[i] = res
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), compressed.Load(), "one build, one compression")
	for i, res := range results {
		assert.Equalf(t, body, res.body, "caller %d gets the build's bytes", i)
		if i%2 == 0 {
			require.NotNilf(t, res.gz, "gzip caller %d gets the compressed bytes", i)
			assert.Equalf(t, body, gunzip(t, res.gz), "caller %d's gzip holds the build's bytes", i)
		} else {
			assert.Nilf(t, res.gz, "plain caller %d gets no compressed bytes", i)
		}
	}

	// A new build compresses afresh: the bytes are the build's, not a cache.
	_, err := g.Do("gz-key", true, func() ([]byte, error) { return body, nil })
	require.NoError(t, err)
	assert.Equal(t, int32(2), compressed.Load())
}

// A build under gzipMinBytes is never compressed (the middleware would not
// compress it either), nor is a failed one.
func TestViewerSingleFlight_SmallOrFailedBuildIsNotCompressed(t *testing.T) {
	g := newViewerSingleFlight(zeroSeq)
	var compressed atomic.Int32
	sfTestCompressed = func(string) { compressed.Add(1) }
	t.Cleanup(func() { sfTestCompressed = nil })

	res, err := g.Do("small", true, func() ([]byte, error) { return []byte(`["ok"]`), nil })
	require.NoError(t, err)
	assert.Nil(t, res.gz)
	assert.Equal(t, []byte(`["ok"]`), res.body)

	_, err = g.Do("failed", true, func() ([]byte, error) { return nil, fmt.Errorf("build failed") })
	require.Error(t, err)
	assert.Equal(t, int32(0), compressed.Load())
}

// serveSingleFlightJSON sends the gzipped bytes encoded and the plain ones as
// they are, both with the build's X-Event-Seq and Accept-Encoding merged into
// the Vary the audience judge set.
func TestServeSingleFlightJSON_EncodingVaryAndSeq(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := bigJSON(t, 200)
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, err := zw.Write(body)
	require.NoError(t, err)
	require.NoError(t, zw.Close())

	serve := func(res sfResult) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Writer.Header().Set("Vary", "X-Tournament-Password")
		serveSingleFlightJSON(c, res, nil)
		return w
	}

	w := serve(sfResult{body: body, gz: buf.Bytes(), seq: 42})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "42", w.Header().Get(eventSeqHeader))
	assert.Equal(t, []string{"X-Tournament-Password", "Accept-Encoding"}, varyTokens(w.Header()))
	assert.Equal(t, body, gunzip(t, w.Body.Bytes()))

	w = serve(sfResult{body: body, seq: 0})
	assert.Empty(t, w.Header().Get("Content-Encoding"))
	assert.Equal(t, "0", w.Header().Get(eventSeqHeader))
	assert.Equal(t, []string{"X-Tournament-Password", "Accept-Encoding"}, varyTokens(w.Header()))
	assert.Equal(t, body, w.Body.Bytes())
}

// The error answer names Accept-Encoding in Vary too, with no middleware in
// front of it.
func TestServeSingleFlightJSON_ErrorAnswerCarriesVary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	c.Writer.Header().Set("Vary", "X-Tournament-Password")
	serveSingleFlightJSON(c, sfResult{}, errors.New("build failed"))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Equal(t, []string{"X-Tournament-Password", "Accept-Encoding"}, varyTokens(w.Header()))
}

// The list and the court feeds key their builds on the config write token
// (viewerFlightKeyAt): one audience keeps one key while no config.md is
// written, and a config write gives the next request a key of its own.
func TestViewerFlightKeyAt_MovesWithTheConfigWriteToken(t *testing.T) {
	at1 := viewerFlightKeyAt("competitions", 1, audience{})
	if viewerFlightKeyAt("competitions", 1, audience{}) != at1 {
		t.Fatal("one audience at one token must keep one key")
	}
	if viewerFlightKeyAt("competitions", 2, audience{}) == at1 {
		t.Fatal("a config write must give the next request a key of its own")
	}
	if viewerFlightKeyAt("competitions", 1, organiserAudience()) == at1 {
		t.Fatal("the organiser must not share the public's build")
	}
	if viewerFlightKeyAt("court-matches:A", 1, audience{}) == at1 {
		t.Fatal("the court feed and the list must not share a build")
	}
}

// A public read of the list or a court feed made straight after a draw is
// published or hidden again (bc-drpb) never joins a build begun before it:
// the first build is held after it has read the competition, the draw's
// visibility is flipped, and the second request is answered the new state
// while the first is still held.
func TestViewerFlights_ARequestAfterAPublishOrUnpublishNeverJoinsAnEarlierBuild(t *testing.T) {
	routes := []struct {
		name  string
		entry func(drawHiddenWorld, *testing.T) map[string]any
	}{
		{"list", func(w drawHiddenWorld, t *testing.T) map[string]any { return w.listEntry(t, dhComp, dhPublic) }},
		{"court feed", func(w drawHiddenWorld, t *testing.T) map[string]any { return w.courtFeedEntry(t, dhComp, dhPublic) }},
	}
	for _, route := range routes {
		for _, hideAfter := range []bool{false, true} {
			name := fmt.Sprintf("%s, hidden %v then %v", route.name, !hideAfter, hideAfter)
			t.Run(name, func(t *testing.T) {
				w := newDrawHiddenWorld(t, state.CompFormatMixed)
				require.NoError(t, w.eng.GenerateDraw(dhComp))
				if hideAfter {
					w.setHidden(t, false)
				}

				loaded, release := make(chan struct{}), make(chan struct{})
				// Only the first load is held; a sync.Once would hold every
				// later caller until the first returned.
				var held atomic.Bool
				orig := viewerLoadCompetition
				t.Cleanup(func() { viewerLoadCompetition = orig })
				viewerLoadCompetition = func(store *state.Store, compID string) (*state.Competition, error) {
					comp, err := orig(store, compID)
					if held.CompareAndSwap(false, true) {
						close(loaded)
						<-release
					}
					return comp, err
				}

				earlier := make(chan map[string]any, 1)
				go func() { earlier <- route.entry(w, t) }()
				<-loaded
				w.setHidden(t, hideAfter)

				later := make(chan map[string]any, 1)
				go func() { later <- route.entry(w, t) }()
				select {
				case got := <-later:
					assertAnswersVisibility(t, route.name, got, hideAfter)
				case <-time.After(10 * time.Second):
					close(release)
					t.Fatal("the request after the write joined the build begun before it")
				}
				close(release)
				assertAnswersVisibility(t, route.name, <-earlier, !hideAfter)
			})
		}
	}
}

// assertAnswersVisibility checks a public answer of the draft: hidden, the list
// holds it with no draw and the court feed leaves it out; published, both
// hold its draw.
func assertAnswersVisibility(t *testing.T, route string, entry map[string]any, hidden bool) {
	t.Helper()
	switch {
	case !hidden:
		require.NotNil(t, entry, "%s: the published draft is answered", route)
		assert.True(t, hasDraw(entry), "%s: the published draft shows its draw", route)
	case route == "court feed":
		assert.Nil(t, entry, "the court feed leaves the hidden draft out")
	default:
		require.NotNil(t, entry, "the list holds the hidden draft")
		assert.False(t, hasDraw(entry), "the list shows no draw of the hidden draft")
	}
}

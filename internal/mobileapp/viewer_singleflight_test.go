package mobileapp

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewerSingleFlight_CollatesConcurrentBuilds is the acceptance test for
// P2 (mp-9afd): N concurrent calls with the same key during a single
// in-flight build must collapse to exactly 1 real build invocation while all
// callers receive the correct result.
//
// Synchronisation strategy: all goroutines wait behind a readyBarrier so they
// race into Do() simultaneously. The elected caller holds the in-flight slot
// for 200ms — more than enough for the other goroutines to enter Do(), find
// the key in the map, and block on wg.Wait(). Any goroutine that enters Do()
// after the barrier but before fn completes will become a waiter, not an
// independent builder.
func TestViewerSingleFlight_CollatesConcurrentBuilds(t *testing.T) {
	const concurrency = 100
	g := newViewerSingleFlight()

	var buildCount atomic.Int32

	// readyBarrier ensures all goroutines are spawned and ready before
	// any of them call Do — eliminates the scheduling window that caused
	// goroutines to arrive after the elected caller had already finished.
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
			readyBarrier.Wait() // all goroutines race into Do together
			data, err := g.Do("test-key", func() ([]byte, error) {
				buildCount.Add(1)
				// Hold the in-flight slot long enough for all other
				// goroutines to enter Do and become waiters.
				time.Sleep(200 * time.Millisecond)
				return []byte(`["ok"]`), nil
			})
			results[i] = result{data, err}
		}()
	}

	wg.Wait()

	// Only one build should have executed.
	assert.Equal(t, int32(1), buildCount.Load(),
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
	g := newViewerSingleFlight()

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
			data, err := g.Do(key, func() ([]byte, error) {
				return []byte(fmt.Sprintf(`{"id":%d}`, i)), nil
			})
			results[i] = result{data, err}
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
// key is not permanently cached — there is no stale-data risk).
func TestViewerSingleFlight_SubsequentCallsReExecute(t *testing.T) {
	g := newViewerSingleFlight()

	var count atomic.Int32
	do := func() ([]byte, error) {
		count.Add(1)
		return []byte("ok"), nil
	}

	_, err := g.Do("k", do)
	require.NoError(t, err)
	assert.Equal(t, int32(1), count.Load())

	_, err = g.Do("k", do)
	require.NoError(t, err)
	assert.Equal(t, int32(2), count.Load(), "expected fn to re-execute after first call completed")
}

// TestViewerSingleFlight_ErrorPropagatedToAllWaiters checks that when the
// elected fn returns an error, all concurrent waiters receive the same error.
// The fn stays in-flight long enough for all other goroutines to attach as
// waiters, and we assert exactly one build ran — confirming the waiter path
// is reliably exercised.
func TestViewerSingleFlight_ErrorPropagatedToAllWaiters(t *testing.T) {
	const concurrency = 50
	g := newViewerSingleFlight()

	var buildCount atomic.Int32

	// readyBarrier ensures all goroutines are spawned before they race
	// into Do together — same pattern as CollatesConcurrentBuilds.
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
			_, err := g.Do("err-key", func() ([]byte, error) {
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

	// Only one build should have executed — confirms waiters attached.
	assert.Equal(t, int32(1), buildCount.Load(),
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
	g := newViewerSingleFlight()

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
			data, err := g.Do("panic-key", func() ([]byte, error) {
				// Hold the slot briefly so all other goroutines attach,
				// then panic. The singleflight must recover the panic,
				// convert it to an error, and unblock all waiters.
				time.Sleep(100 * time.Millisecond)
				panic("kaboom")
			})
			results[i] = result{data, err}
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

	// The key must be cleaned up — a subsequent call should execute normally.
	data, err := g.Do("panic-key", func() ([]byte, error) {
		return []byte("recovered"), nil
	})
	require.NoError(t, err)
	assert.Equal(t, []byte("recovered"), data, "key should be usable after panic")
}

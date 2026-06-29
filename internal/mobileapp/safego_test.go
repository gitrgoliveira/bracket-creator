package mobileapp

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSafeGo_RecoversPanicAndCallsDone(t *testing.T) {
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]

	safeGo(&wg, &panicRef, func() {
		panic("kaboom")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait did not return; safeGo did not call wg.Done on panic")
	}

	p := panicRef.Load()
	if p == nil {
		t.Fatal("expected panicRef to be populated, got nil")
	}
	if v, _ := p.value.(string); v != "kaboom" {
		t.Fatalf("expected panic value %q, got %v", "kaboom", p.value)
	}
	if len(p.stack) == 0 {
		t.Error("expected non-empty stack trace")
	}
}

func TestSafeGo_NoPanic_LeavesPanicRefNil(t *testing.T) {
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]

	executed := false
	safeGo(&wg, &panicRef, func() {
		executed = true
	})

	wg.Wait()

	if !executed {
		t.Fatal("fn did not execute")
	}
	if p := panicRef.Load(); p != nil {
		t.Fatalf("expected panicRef nil for clean run, got %v", p)
	}
}

func TestSafeGo_FirstPanicWins(t *testing.T) {
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]

	// Use a barrier so both goroutines panic concurrently. The first
	// CompareAndSwap winner becomes panicRef; the rest are still logged
	// but don't overwrite. This test just verifies SOMETHING is captured
	// and the second panic doesn't crash the process, it doesn't pin
	// which value wins, because that's a race we intentionally don't
	// constrain.
	start := make(chan struct{})
	safeGo(&wg, &panicRef, func() {
		<-start
		panic("first")
	})
	safeGo(&wg, &panicRef, func() {
		<-start
		panic(errors.New("second"))
	})
	close(start)

	wg.Wait()

	if p := panicRef.Load(); p == nil {
		t.Fatal("expected at least one panic to be captured")
	}
}

func TestSafeGo_NilPanicRef_DoesNotCrash(t *testing.T) {
	var wg sync.WaitGroup
	// A nil panicRef is allowed, safeGo must still recover the panic
	// and call wg.Done. Useful for fire-and-forget background work
	// where the caller doesn't need to surface the failure.
	safeGo(&wg, nil, func() {
		panic("nobody is listening")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("wg.Wait did not return; safeGo did not handle nil panicRef cleanly")
	}
}

func TestSafeGo_MultipleGoroutinesAllComplete(t *testing.T) {
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]
	var counter atomic.Int32

	const n = 50
	for i := 0; i < n; i++ {
		i := i
		safeGo(&wg, &panicRef, func() {
			if i%5 == 0 {
				panic(i)
			}
			counter.Add(1)
		})
	}

	wg.Wait()

	// 10 of the 50 panic (multiples of 5: 0,5,...,45). The other 40 must run.
	if got := counter.Load(); got != 40 {
		t.Errorf("expected 40 successful runs, got %d", got)
	}
	if p := panicRef.Load(); p == nil {
		t.Error("expected at least one panic to be captured")
	}
}

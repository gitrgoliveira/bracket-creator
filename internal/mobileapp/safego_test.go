package mobileapp

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestSafeGo_NormalExecution(t *testing.T) {
	var wg sync.WaitGroup
	errCh := make(chan error, 1)
	called := false

	wg.Add(1)
	safeGo(&wg, errCh, func() {
		called = true
	})
	wg.Wait()

	assert.True(t, called)
	assert.Empty(t, errCh, "no error expected on normal execution")
}

func TestSafeGo_PanicRecovery(t *testing.T) {
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	safeGo(&wg, errCh, func() {
		panic("test panic")
	})
	wg.Wait()

	select {
	case err := <-errCh:
		assert.ErrorContains(t, err, "internal error")
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected error in errCh after panic")
	}
}

func TestSafeGo_WaitGroupAlwaysDone(t *testing.T) {
	// Even when the goroutine panics, wg.Done() must be called exactly once
	// so wg.Wait() does not block forever.
	var wg sync.WaitGroup
	errCh := make(chan error, 1)

	wg.Add(1)
	safeGo(&wg, errCh, func() {
		panic("unexpected panic")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// WaitGroup unblocked — wg.Done() was called despite the panic.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("wg.Wait() blocked after panic in safeGo — wg.Done() not called")
	}
}

func TestSafeGo_ErrorChannelFullDoesNotBlock(t *testing.T) {
	// When errCh is full (capacity 0 / already filled), safeGo must not
	// block — the select default path must fire.
	var wg sync.WaitGroup
	errCh := make(chan error, 0) // zero-capacity: send will always fail default

	wg.Add(1)
	safeGo(&wg, errCh, func() {
		panic("overflow panic")
	})

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// safeGo did not block on the full channel.
	case <-time.After(200 * time.Millisecond):
		t.Fatal("safeGo blocked on full errCh instead of using select default")
	}
}

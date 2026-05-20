package mobileapp

import (
	"fmt"
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// recoveredPanic captures a panic value and its stack so callers can
// surface a single 500 response without leaking internals to the client.
type recoveredPanic struct {
	value any
	stack []byte
}

// Error renders the panic for log/telemetry use. The HTTP layer never
// returns this string to clients.
func (p *recoveredPanic) Error() string {
	return fmt.Sprintf("panic: %v", p.value)
}

// safeGo runs fn in a new goroutine with `wg.Add(1)` already accounted
// for inside it, and `wg.Done()` guaranteed to run even on panic.
//
// gin.Default()'s Recovery middleware only catches panics on the
// request goroutine — a panic in a goroutine spawned by a handler
// crashes the whole process. Handlers that fan out concurrent state
// loads MUST use safeGo (or another helper with equivalent guarantees)
// so a corrupt file or nil deref doesn't take down the mobile-app
// container.
//
// On recover, safeGo logs the panic + stack and stores the first panic
// it sees into `panicRef`. Subsequent panics in sibling goroutines are
// still logged (so post-mortem has them all) but the first one wins for
// reporting purposes — a handler typically returns a single 500
// regardless of how many goroutines failed.
func safeGo(wg *sync.WaitGroup, panicRef *atomic.Pointer[recoveredPanic], fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				stack := debug.Stack()
				log.Printf("mobileapp: spawned-goroutine panic recovered: %v\n%s", r, stack)
				if panicRef != nil {
					panicRef.CompareAndSwap(nil, &recoveredPanic{value: r, stack: stack})
				}
			}
		}()
		fn()
	}()
}

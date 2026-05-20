package mobileapp

import (
	"fmt"
	"log"
	"runtime/debug"
	"sync"
)

// safeGo runs fn in a goroutine. If fn panics, the panic is recovered:
// the value is logged with a full stack trace, and a generic error is
// written to errCh. wg.Done() is called in all paths so the WaitGroup
// is never leaked.
//
// The panic value itself is NOT forwarded to errCh or to HTTP responses —
// it may contain internal state (file paths, memory addresses) that must
// not be surfaced to unauthenticated callers on the viewer endpoints.
//
// Convention: every goroutine spawned inside a request handler MUST use
// safeGo. A panic in a bare goroutine bypasses gin.Default()'s Recovery
// middleware and crashes the process. safeGo closes that gap for
// handler-spawned goroutines.
func safeGo(wg *sync.WaitGroup, errCh chan<- error, fn func()) {
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				log.Printf("safeGo: recovered panic in viewer goroutine: %v\n%s", r, debug.Stack())
				select {
				case errCh <- fmt.Errorf("internal error"):
				default:
				}
			}
		}()
		fn()
	}()
}

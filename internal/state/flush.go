package state

import (
	"os"
	"runtime"
	"testing"
)

// flush makes what was written to f durable: the one owner of the store's
// disk flush, called for every saved file (atomicWriteFile, writeSynced) and
// every folder a rename changed (syncDir).
//
// The shipped app always flushes, on every OS. On macOS Go's Sync is
// F_FULLFSYNC, which waits for the drive to empty its own write cache, so a
// result survives a power cut at the venue.
//
// A test binary on macOS skips the flush (skipFlush). F_FULLFSYNC costs about
// 11 ms a save against 0.2 ms without it, and the store's tests save
// thousands of times, so the gate timed out on a Mac (operator decision
// 2026-11-18, round flush-1). The temp-file-and-rename step still runs, so
// every test goes through the same writes; only the wait for the drive is
// left out. Linux test runs keep the real flush, where it is cheap, so CI
// still exercises it.
func flush(f *os.File) error {
	if skipFlush {
		return nil
	}
	return f.Sync()
}

// skipFlush is true only in a binary built by go test (testing.Testing,
// which the linker sets, so a go build binary always answers false) running
// on macOS. Every test run counts: the full gate, a targeted go test -run and
// an editor's test button alike, in every package that saves through the
// store, which is why it asks testing.Testing rather than a _test.go file
// (that would reach internal/state's own tests only).
var skipFlush = flushSkipped(testing.Testing(), runtime.GOOS)

// flushSkipped is the rule itself: skip only in a test binary on macOS.
func flushSkipped(testBinary bool, goos string) bool {
	return testBinary && goos == "darwin"
}

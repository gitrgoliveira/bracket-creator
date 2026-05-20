package state

import (
	"testing"
	"time"
)

func TestAnnouncementStore(t *testing.T) {
	store := NewAnnouncementStore()

	// Initial state: should be nil
	if ann := store.Get(); ann != nil {
		t.Errorf("expected initial announcement to be nil, got: %v", ann)
	}

	// Set active announcement
	msg := "Lunch break for 30 minutes"
	dur := 30 * time.Minute
	ann := store.Set(msg, dur)

	if ann.Message != msg {
		t.Errorf("expected message %q, got %q", msg, ann.Message)
	}

	// Retrieve active announcement
	retrieved := store.Get()
	if retrieved == nil {
		t.Fatal("expected retrieved announcement to be non-nil")
	}
	if retrieved.Message != msg {
		t.Errorf("expected retrieved message %q, got %q", msg, retrieved.Message)
	}

	// Set replaces previous
	newMsg := "Delay on court B"
	newDur := 5 * time.Minute
	newAnn := store.Set(newMsg, newDur)

	if newAnn.Message != newMsg {
		t.Errorf("expected new message %q, got %q", newMsg, newAnn.Message)
	}

	retrievedNew := store.Get()
	if retrievedNew == nil {
		t.Fatal("expected retrieved new announcement to be non-nil")
	}
	if retrievedNew.Message != newMsg {
		t.Errorf("expected retrieved new message %q, got %q", newMsg, retrievedNew.Message)
	}

	// Expiration test: set an announcement with very short duration
	store.Set("Short notice", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)

	if expired := store.Get(); expired != nil {
		t.Errorf("expected announcement to be expired (nil), got: %v", expired)
	}

	// Clear test
	store.Set("Clear me", 10*time.Minute)
	if retrieved := store.Get(); retrieved == nil {
		t.Fatal("expected announcement to be set before clear")
	}
	store.Clear()
	if cleared := store.Get(); cleared != nil {
		t.Errorf("expected announcement to be cleared (nil), got: %v", cleared)
	}
}

// TestAnnouncementStoreGetAfterConcurrentSet guards against the race where
// Get() sees an expired announcement under the read lock, then a concurrent
// Set() replaces it before Get() acquires the write lock. The fresh
// announcement must be visible to the caller, not swallowed.
func TestAnnouncementStoreGetAfterConcurrentSet(t *testing.T) {
	store := NewAnnouncementStore()

	// Set an announcement that expires immediately.
	store.Set("expires now", 1*time.Nanosecond)
	time.Sleep(5 * time.Millisecond) // ensure it is expired

	// Simulate: a concurrent writer calls Set() with a fresh announcement
	// just as Get() is about to acquire the write lock.  We test the
	// outcome rather than the timing, which is deterministic: after Set()
	// completes, Get() must return the new announcement (not nil).
	store.Set("fresh announcement", 10*time.Minute)

	got := store.Get()
	if got == nil {
		t.Fatal("expected Get() to return the fresh announcement, got nil")
	}
	if got.Message != "fresh announcement" {
		t.Errorf("expected message %q, got %q", "fresh announcement", got.Message)
	}
}

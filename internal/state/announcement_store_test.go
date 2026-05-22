package state

import (
	"sync"
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

// TestAnnouncementStoreGetAfterSetReplacesExpired verifies the sequential
// contract: after Set() replaces an expired announcement, Get() returns the
// fresh value rather than the expired one or nil. (See the concurrent
// variant below for the read-lock/write-lock race guard.)
func TestAnnouncementStoreGetAfterSetReplacesExpired(t *testing.T) {
	store := NewAnnouncementStore()

	// Set an announcement that expires immediately.
	store.Set("expires now", 1*time.Nanosecond)
	time.Sleep(5 * time.Millisecond) // ensure it is expired

	store.Set("fresh announcement", 10*time.Minute)

	got := store.Get()
	if got == nil {
		t.Fatal("expected Get() to return the fresh announcement, got nil")
	}
	if got.Message != "fresh announcement" {
		t.Errorf("expected message %q, got %q", "fresh announcement", got.Message)
	}
}

// TestAnnouncementStoreGetSetRace exercises the actual race between Get()
// and Set(). Get() reads under the read lock, releases, then under the write
// lock decides whether to clear an expired announcement. A concurrent Set()
// can land in that window with a fresh announcement, and the double-check
// in Get() must NOT clear that fresh value.
//
// We run many short rounds: each round seeds an immediately-expired
// announcement, then runs Set("fresh") and Get() concurrently. After both
// goroutines return, Get() must always observe a non-nil "fresh"
// announcement on the next call. If the race guard regresses, the
// concurrent Get() inside the round can race-clear the fresh announcement
// and the post-round assertion catches it.
func TestAnnouncementStoreGetSetRace(t *testing.T) {
	const rounds = 200
	for i := 0; i < rounds; i++ {
		store := NewAnnouncementStore()
		store.Set("expires now", 1*time.Nanosecond)
		time.Sleep(time.Microsecond) // ensure expired

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			store.Set("fresh announcement", 10*time.Minute)
		}()
		go func() {
			defer wg.Done()
			// May see nil (expired cleared before Set lands) or "fresh"
			// (Set landed first). Must NOT race-clear the fresh value.
			_ = store.Get()
		}()
		wg.Wait()

		got := store.Get()
		if got == nil {
			t.Fatalf("round %d: expected Get() to return the fresh announcement after the concurrent pair, got nil — race guard regressed", i)
		}
		if got.Message != "fresh announcement" {
			t.Fatalf("round %d: expected message %q, got %q", i, "fresh announcement", got.Message)
		}
	}
}

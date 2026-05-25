package state

import (
	"sync"
	"testing"
	"time"
)

func TestAnnouncementStore(t *testing.T) {
	store := NewAnnouncementStore()

	if ann := store.Get(); ann != nil {
		t.Errorf("expected initial announcement to be nil, got: %v", ann)
	}
	if list := store.List(); len(list) != 0 {
		t.Errorf("expected initial list to be empty, got: %v", list)
	}

	// Set replaces all prior.
	msg := "Lunch break for 30 minutes"
	ann := store.Set(msg, 30*time.Minute)
	if ann.Message != msg {
		t.Errorf("expected message %q, got %q", msg, ann.Message)
	}
	if ann.ID == "" {
		t.Error("expected non-empty ID")
	}

	retrieved := store.Get()
	if retrieved == nil {
		t.Fatal("expected retrieved announcement to be non-nil")
	}
	if retrieved.Message != msg {
		t.Errorf("expected retrieved message %q, got %q", msg, retrieved.Message)
	}

	// Set replaces previous.
	newMsg := "Delay on court B"
	newAnn := store.Set(newMsg, 5*time.Minute)
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

	// Expiry.
	store.Set("Short notice", 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if expired := store.Get(); expired != nil {
		t.Errorf("expected announcement to be expired (nil), got: %v", expired)
	}

	// Clear.
	store.Set("Clear me", 10*time.Minute)
	store.Clear()
	if cleared := store.Get(); cleared != nil {
		t.Errorf("expected announcement to be cleared (nil), got: %v", cleared)
	}
}

func TestAnnouncementStoreAdd(t *testing.T) {
	store := NewAnnouncementStore()

	a1, _ := store.Add("first", 30*time.Minute)
	a2, _ := store.Add("second", 30*time.Minute)

	if a1.ID == a2.ID {
		t.Error("IDs should be unique")
	}

	list := store.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 announcements, got %d", len(list))
	}
	if list[0].Message != "first" {
		t.Errorf("expected oldest first, got %q", list[0].Message)
	}
	if list[1].Message != "second" {
		t.Errorf("expected newest last, got %q", list[1].Message)
	}

	// Get returns most recent.
	got := store.Get()
	if got == nil || got.Message != "second" {
		t.Errorf("Get() should return most recent, got %v", got)
	}
}

func TestAnnouncementStoreRemove(t *testing.T) {
	store := NewAnnouncementStore()

	a1, _ := store.Add("msg1", 30*time.Minute)
	_, _ = store.Add("msg2", 30*time.Minute)

	removed, list := store.Remove(a1.ID)
	if !removed {
		t.Error("expected Remove to return true for existing ID")
	}

	if len(list) != 1 {
		t.Fatalf("expected 1 remaining, got %d", len(list))
	}
	if list[0].Message != "msg2" {
		t.Errorf("expected msg2 to remain, got %q", list[0].Message)
	}

	notFound, _ := store.Remove("nonexistent")
	if notFound {
		t.Error("expected Remove to return false for missing ID")
	}
}

func TestAnnouncementStoreCapEvictsOldest(t *testing.T) {
	store := NewAnnouncementStore()

	for range maxActiveAnnouncements {
		_, _ = store.Add("msg", 30*time.Minute)
	}
	newest, _ := store.Add("newest", 30*time.Minute)
	list := store.List()
	if len(list) != maxActiveAnnouncements {
		t.Fatalf("expected cap %d, got %d", maxActiveAnnouncements, len(list))
	}
	if list[len(list)-1].ID != newest.ID {
		t.Error("newest should be last in list")
	}
}

func TestAnnouncementStoreListPrunesExpired(t *testing.T) {
	store := NewAnnouncementStore()

	store.Add("expires soon", 10*time.Millisecond)
	store.Add("long-lived", 30*time.Minute)
	time.Sleep(20 * time.Millisecond)

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 after expiry, got %d", len(list))
	}
	if list[0].Message != "long-lived" {
		t.Errorf("expected long-lived, got %q", list[0].Message)
	}
}

func TestAnnouncementStoreGetAfterSetReplacesExpired(t *testing.T) {
	store := NewAnnouncementStore()

	store.Set("expires now", 1*time.Nanosecond)
	time.Sleep(5 * time.Millisecond)

	store.Set("fresh announcement", 10*time.Minute)

	got := store.Get()
	if got == nil {
		t.Fatal("expected Get() to return the fresh announcement, got nil")
	}
	if got.Message != "fresh announcement" {
		t.Errorf("expected message %q, got %q", "fresh announcement", got.Message)
	}
}

func TestAnnouncementStoreConcurrentAddList(t *testing.T) {
	store := NewAnnouncementStore()
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = store.Add("concurrent", 30*time.Minute)
		}()
		go func() {
			defer wg.Done()
			_ = store.List()
		}()
	}
	wg.Wait()
}

package state

import (
	"fmt"
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

func TestAnnouncementStoreRemovePrunesExpired(t *testing.T) {
	store := NewAnnouncementStore()

	_, _ = store.Add("expires soon", 10*time.Millisecond)
	a2, _ := store.Add("long-lived", 30*time.Minute)
	time.Sleep(20 * time.Millisecond)

	// Remove the long-lived item; snapshot must not include the expired one.
	found, list := store.Remove(a2.ID)
	if !found {
		t.Fatal("expected Remove to find a2")
	}
	if len(list) != 0 {
		t.Errorf("expected empty snapshot after remove + prune, got %d items: %v", len(list), list)
	}
}

func TestAnnouncementStoreCapEvictsOldest(t *testing.T) {
	store := NewAnnouncementStore()

	// Distinct messages: an identical one would replace its predecessor
	// (bc-cdbl), and this test is about the cap, not about replacement.
	for i := range maxActiveAnnouncements {
		_, _ = store.Add(fmt.Sprintf("msg %d", i), 30*time.Minute)
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

func TestAnnouncementStore_AddReplacesIdenticalActive(t *testing.T) {
	tests := []struct {
		name     string
		scenario func(t *testing.T, store *AnnouncementStore)
	}{
		{
			name: "identical active announcement is replaced",
			scenario: func(t *testing.T, store *AnnouncementStore) {
				msg := "Call to court"
				a1, _ := store.Add(msg, 5*time.Minute)
				a2, list := store.Add(msg, 5*time.Minute)

				// Only one announcement should exist
				if len(list) != 1 {
					t.Fatalf("expected 1 announcement, got %d", len(list))
				}
				// IDs should differ (a2 is the new one)
				if a1.ID == a2.ID {
					t.Error("new announcement should have a different ID")
				}
				// The stored announcement should be a2, not a1
				if list[0].ID != a2.ID {
					t.Errorf("expected newest announcement (ID %s), got ID %s", a2.ID, list[0].ID)
				}
				if list[0].Message != msg {
					t.Errorf("expected message %q, got %q", msg, list[0].Message)
				}
			},
		},
		{
			name: "different messages both stack",
			scenario: func(t *testing.T, store *AnnouncementStore) {
				a1, _ := store.Add("First call", 5*time.Minute)
				a2, list := store.Add("Second call", 5*time.Minute)

				if len(list) != 2 {
					t.Fatalf("expected 2 announcements, got %d", len(list))
				}
				if list[0].ID != a1.ID {
					t.Errorf("expected first announcement first")
				}
				if list[1].ID != a2.ID {
					t.Errorf("expected second announcement second")
				}
			},
		},
		{
			name: "identical message after expiry just adds",
			scenario: func(t *testing.T, store *AnnouncementStore) {
				msg := "Call to court"
				a1, _ := store.Add(msg, 5*time.Millisecond)
				time.Sleep(10 * time.Millisecond)
				a2, list := store.Add(msg, 5*time.Minute)

				// The expired copy is pruned, so the fresh one is the only entry
				// and nothing is replaced: the identical message just adds.
				if len(list) != 1 {
					t.Fatalf("expected 1 announcement (expired first removed), got %d", len(list))
				}
				if list[0].ID != a2.ID {
					t.Errorf("expected the fresh announcement, got %q", list[0].ID)
				}
				// a1 should be gone (expired)
				if list[0].ID == a1.ID {
					t.Error("expired announcement should be pruned")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewAnnouncementStore()
			tt.scenario(t, store)
		})
	}
}

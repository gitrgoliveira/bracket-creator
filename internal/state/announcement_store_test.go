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

// aliasCount reads the size of the replaced-id map under the store's lock.
func aliasCount(s *AnnouncementStore) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.replaced)
}

// TestAnnouncementStore_RemoveThroughAReplacedID pins the follow-up to bc-cdbl:
// a court console holding the id of a call that a later identical call replaced
// (a second device re-called the match, or its own re-call's answer was lost)
// still takes the call down, because the id resolves to the announcement that
// replaced it.
func TestAnnouncementStore_RemoveThroughAReplacedID(t *testing.T) {
	store := NewAnnouncementStore()
	a1, _ := store.Add("x", 5*time.Minute)
	a2, _ := store.Add("x", 5*time.Minute)
	if a1.ID == a2.ID {
		t.Fatal("the replacement must carry a new ID")
	}

	found, list := store.Remove(a1.ID)
	if !found {
		t.Fatal("expected Remove through the replaced ID to find the announcement that replaced it")
	}
	if len(list) != 0 {
		t.Errorf("expected an empty list after the withdrawal, got %v", list)
	}
	if got := store.List(); len(got) != 0 {
		t.Errorf("expected the store to be empty, got %v", got)
	}
}

// TestAnnouncementStore_RemoveThroughAReplacedIDLeavesOthers: the withdrawal
// takes down only the announcement that replaced the id, never a different one.
func TestAnnouncementStore_RemoveThroughAReplacedIDLeavesOthers(t *testing.T) {
	store := NewAnnouncementStore()
	a1, _ := store.Add("x", 5*time.Minute)
	y, _ := store.Add("y", 5*time.Minute)
	a3, _ := store.Add("x", 5*time.Minute)

	found, list := store.Remove(a1.ID)
	if !found {
		t.Fatal("expected Remove through the replaced ID to find a3")
	}
	if len(list) != 1 || list[0].ID != y.ID {
		t.Fatalf("expected only the unrelated announcement to remain, got %v", list)
	}
	if found, _ := store.Remove(a3.ID); found {
		t.Error("a3 was withdrawn through a1, so removing it again must not be found")
	}
}

// TestAnnouncementStore_ReplacementChainResolvesToTheLatest: three identical
// Adds leave one announcement, and every older id reaches it in one hop.
func TestAnnouncementStore_ReplacementChainResolvesToTheLatest(t *testing.T) {
	for _, tc := range []struct {
		name string
		idx  int
	}{
		{"oldest id", 0},
		{"middle id", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := NewAnnouncementStore()
			a1, _ := store.Add("x", 5*time.Minute)
			a2, _ := store.Add("x", 5*time.Minute)
			a3, list := store.Add("x", 5*time.Minute)
			if len(list) != 1 || list[0].ID != a3.ID {
				t.Fatalf("expected only a3 to be active, got %v", list)
			}

			// One hop: re-pointed on the third Add, not a chain id1 -> id2 -> id3.
			store.mu.Lock()
			hop1, hop2 := store.replaced[a1.ID], store.replaced[a2.ID]
			store.mu.Unlock()
			if hop1 != a3.ID || hop2 != a3.ID {
				t.Fatalf("expected both aliases to point at a3 (%s), got %q and %q", a3.ID, hop1, hop2)
			}

			found, after := store.Remove([]string{a1.ID, a2.ID}[tc.idx])
			if !found {
				t.Fatal("expected the older ID to withdraw the latest announcement")
			}
			if len(after) != 0 {
				t.Errorf("expected an empty list, got %v", after)
			}
			if n := aliasCount(store); n != 0 {
				t.Errorf("expected the aliases to go with their target, %d remain", n)
			}
		})
	}
}

// TestAnnouncementStore_AliasGoesWithItsTarget: an alias is only an alternate
// name for an active announcement, so once that announcement is gone, by any
// route, the older id is not found and nothing is kept for it.
func TestAnnouncementStore_AliasGoesWithItsTarget(t *testing.T) {
	t.Run("removed through its own id", func(t *testing.T) {
		store := NewAnnouncementStore()
		a1, _ := store.Add("x", 5*time.Minute)
		a2, _ := store.Add("x", 5*time.Minute)
		if found, _ := store.Remove(a2.ID); !found {
			t.Fatal("expected a2 to be found")
		}
		if found, _ := store.Remove(a1.ID); found {
			t.Error("a1 resolved to a2, which is gone: it must not be found")
		}
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected no alias to remain, got %d", n)
		}
	})

	t.Run("expired, swept by List", func(t *testing.T) {
		store := NewAnnouncementStore()
		a1, _ := store.Add("x", 5*time.Minute)
		_, _ = store.Add("x", 10*time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		if got := store.List(); len(got) != 0 {
			t.Fatalf("expected the replacement to have expired, got %v", got)
		}
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected the alias to go with the expired target, got %d", n)
		}
		if found, _ := store.Remove(a1.ID); found {
			t.Error("a1 resolved to an expired announcement: it must not be found")
		}
	})

	t.Run("expired, found by Remove", func(t *testing.T) {
		store := NewAnnouncementStore()
		a1, _ := store.Add("x", 5*time.Minute)
		_, _ = store.Add("x", 10*time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		if found, list := store.Remove(a1.ID); found || len(list) != 0 {
			t.Errorf("expected not found and an empty list, got %v %v", found, list)
		}
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected the alias to go with the expired target, got %d", n)
		}
	})

	t.Run("cleared", func(t *testing.T) {
		store := NewAnnouncementStore()
		a1, _ := store.Add("x", 5*time.Minute)
		_, _ = store.Add("x", 5*time.Minute)
		store.Clear()
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected Clear to drop the alias, got %d", n)
		}
		if found, _ := store.Remove(a1.ID); found {
			t.Error("a1 must not be found after Clear")
		}
	})

	t.Run("replaced by Set", func(t *testing.T) {
		store := NewAnnouncementStore()
		a1, _ := store.Add("x", 5*time.Minute)
		_, _ = store.Add("x", 5*time.Minute)
		store.Set("y", 5*time.Minute)
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected Set to drop the alias, got %d", n)
		}
		if found, _ := store.Remove(a1.ID); found {
			t.Error("a1 must not be found after Set replaced the list")
		}
	})
}

// TestAnnouncementStore_AliasGoesWithAnEvictedTarget: the cap evicts the oldest
// announcement, and an alias that pointed at it must go too, or the map would
// keep ids nothing can reach.
func TestAnnouncementStore_AliasGoesWithAnEvictedTarget(t *testing.T) {
	store := NewAnnouncementStore()
	a1, _ := store.Add("x", 30*time.Minute)
	a2, _ := store.Add("x", 30*time.Minute)
	if n := aliasCount(store); n != 1 {
		t.Fatalf("expected one alias after the replacement, got %d", n)
	}

	// "x" (a2) is the oldest announcement; fill the store, then add one more.
	for i := range maxActiveAnnouncements {
		_, _ = store.Add(fmt.Sprintf("msg %d", i), 30*time.Minute)
	}
	for _, a := range store.List() {
		if a.ID == a2.ID {
			t.Fatal("expected the cap to have evicted a2")
		}
	}

	if n := aliasCount(store); n != 0 {
		t.Errorf("expected the alias to go with the evicted target, got %d", n)
	}
	if found, _ := store.Remove(a1.ID); found {
		t.Error("a1 resolved to the evicted a2: it must not be found")
	}
}

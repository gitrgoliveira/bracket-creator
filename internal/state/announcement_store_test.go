package state

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock is the store's clock in these tests, so expiry is reached by
// advancing it rather than by sleeping.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

func newClockedStore() (*AnnouncementStore, *fakeClock) {
	clk := &fakeClock{t: time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)}
	s := NewAnnouncementStore()
	s.now = clk.now
	return s, clk
}

// add makes a call now with a fresh id, then moves the clock on a millisecond
// so the next call is made strictly later (an identical call is ordered by
// made-at).
func add(s *AnnouncementStore, clk *fakeClock, msg string, dur time.Duration) (Announcement, []Announcement) {
	a, _, list := s.Add(newParticipantID(), msg, clk.now(), dur)
	clk.advance(time.Millisecond)
	return a, list
}

func TestAnnouncementStore(t *testing.T) {
	store, clk := newClockedStore()

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
	clk.advance(20 * time.Millisecond)
	if expired := store.Get(); expired != nil {
		t.Errorf("expected announcement to be expired (nil), got: %v", expired)
	}

	// Clear.
	store.Set("Clear me", 10*time.Minute)
	clk.advance(time.Millisecond)
	store.Clear(clk.now())
	if cleared := store.Get(); cleared != nil {
		t.Errorf("expected announcement to be cleared (nil), got: %v", cleared)
	}
}

func TestAnnouncementStoreAdd(t *testing.T) {
	store, clk := newClockedStore()

	a1, _ := add(store, clk, "first", 30*time.Minute)
	a2, _ := add(store, clk, "second", 30*time.Minute)

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
	store, clk := newClockedStore()

	a1, _ := add(store, clk, "msg1", 30*time.Minute)
	_, _ = add(store, clk, "msg2", 30*time.Minute)

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
	store, clk := newClockedStore()

	_, _ = add(store, clk, "expires soon", 10*time.Millisecond)
	a2, _ := add(store, clk, "long-lived", 30*time.Minute)
	// A fresh call shows its own time; 10 ms is under the late-call floor, so
	// it shows a minute.
	clk.advance(AnnouncementMinShow)

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
	store, clk := newClockedStore()

	// Distinct messages: an identical one would replace its predecessor
	// (bc-cdbl), and this test is about the cap, not about replacement.
	for i := range maxActiveAnnouncements {
		_, _ = add(store, clk, fmt.Sprintf("msg %d", i), 30*time.Minute)
	}
	newest, _ := add(store, clk, "newest", 30*time.Minute)
	list := store.List()
	if len(list) != maxActiveAnnouncements {
		t.Fatalf("expected cap %d, got %d", maxActiveAnnouncements, len(list))
	}
	if list[len(list)-1].ID != newest.ID {
		t.Error("newest should be last in list")
	}
}

func TestAnnouncementStoreListPrunesExpired(t *testing.T) {
	store, clk := newClockedStore()

	add(store, clk, "expires soon", 5*time.Minute)
	add(store, clk, "long-lived", 30*time.Minute)
	clk.advance(6 * time.Minute)

	list := store.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 after expiry, got %d", len(list))
	}
	if list[0].Message != "long-lived" {
		t.Errorf("expected long-lived, got %q", list[0].Message)
	}
}

func TestAnnouncementStoreGetAfterSetReplacesExpired(t *testing.T) {
	store, clk := newClockedStore()

	store.Set("expires now", 1*time.Nanosecond)
	clk.advance(5 * time.Millisecond)

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
			_, _, _ = store.Add(newParticipantID(), "concurrent", time.Now(), 30*time.Minute)
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
		scenario func(t *testing.T, store *AnnouncementStore, clk *fakeClock)
	}{
		{
			name: "identical active announcement is replaced",
			scenario: func(t *testing.T, store *AnnouncementStore, clk *fakeClock) {
				msg := "Call to court"
				a1, _ := add(store, clk, msg, 5*time.Minute)
				a2, list := add(store, clk, msg, 5*time.Minute)

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
			scenario: func(t *testing.T, store *AnnouncementStore, clk *fakeClock) {
				a1, _ := add(store, clk, "First call", 5*time.Minute)
				a2, list := add(store, clk, "Second call", 5*time.Minute)

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
			scenario: func(t *testing.T, store *AnnouncementStore, clk *fakeClock) {
				msg := "Call to court"
				a1, _ := add(store, clk, msg, 5*time.Minute)
				clk.advance(6 * time.Minute)
				a2, list := add(store, clk, msg, 5*time.Minute)

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
			store, clk := newClockedStore()
			tt.scenario(t, store, clk)
		})
	}
}

// aliasCount reads the size of the replaced-id map under the store's lock.
func aliasCount(s *AnnouncementStore) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.replaced)
}

// spentCount reads how many taken-down ids the store remembers.
func spentCount(s *AnnouncementStore) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.spent)
}

// TestAnnouncementStore_RemoveThroughAReplacedID pins the follow-up to bc-cdbl:
// a court console holding the id of a call that a later identical call replaced
// (a second device re-called the match, or its own re-call's answer was lost)
// still takes the call down, because the id resolves to the announcement that
// replaced it.
func TestAnnouncementStore_RemoveThroughAReplacedID(t *testing.T) {
	store, clk := newClockedStore()
	a1, _ := add(store, clk, "x", 5*time.Minute)
	a2, _ := add(store, clk, "x", 5*time.Minute)
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
	store, clk := newClockedStore()
	a1, _ := add(store, clk, "x", 5*time.Minute)
	y, _ := add(store, clk, "y", 5*time.Minute)
	a3, _ := add(store, clk, "x", 5*time.Minute)

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
			store, clk := newClockedStore()
			a1, _ := add(store, clk, "x", 5*time.Minute)
			a2, _ := add(store, clk, "x", 5*time.Minute)
			a3, list := add(store, clk, "x", 5*time.Minute)
			if len(list) != 1 || list[0].ID != a3.ID {
				t.Fatalf("expected only a3 to be active, got %v", list)
			}

			// One hop: re-pointed on the third Add, not a chain id1 -> id2 -> id3.
			store.mu.Lock()
			hop1, hop2 := store.replaced[a1.ID].target, store.replaced[a2.ID].target
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
// route, the older id is not found and no alias is kept for it.
func TestAnnouncementStore_AliasGoesWithItsTarget(t *testing.T) {
	t.Run("removed through its own id", func(t *testing.T) {
		store, clk := newClockedStore()
		a1, _ := add(store, clk, "x", 5*time.Minute)
		a2, _ := add(store, clk, "x", 5*time.Minute)
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
		store, clk := newClockedStore()
		a1, _ := add(store, clk, "x", 5*time.Minute)
		_, _ = add(store, clk, "x", 5*time.Minute)
		clk.advance(6 * time.Minute)
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
		store, clk := newClockedStore()
		a1, _ := add(store, clk, "x", 5*time.Minute)
		_, _ = add(store, clk, "x", 5*time.Minute)
		clk.advance(6 * time.Minute)
		if found, list := store.Remove(a1.ID); found || len(list) != 0 {
			t.Errorf("expected not found and an empty list, got %v %v", found, list)
		}
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected the alias to go with the expired target, got %d", n)
		}
	})

	t.Run("cleared", func(t *testing.T) {
		store, clk := newClockedStore()
		a1, _ := add(store, clk, "x", 5*time.Minute)
		_, _ = add(store, clk, "x", 5*time.Minute)
		store.Clear(clk.now())
		if n := aliasCount(store); n != 0 {
			t.Errorf("expected Clear to drop the alias, got %d", n)
		}
		if found, _ := store.Remove(a1.ID); found {
			t.Error("a1 must not be found after Clear")
		}
	})

	t.Run("replaced by Set", func(t *testing.T) {
		store, clk := newClockedStore()
		a1, _ := add(store, clk, "x", 5*time.Minute)
		_, _ = add(store, clk, "x", 5*time.Minute)
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
	store, clk := newClockedStore()
	a1, _ := add(store, clk, "x", 30*time.Minute)
	a2, _ := add(store, clk, "x", 30*time.Minute)
	if n := aliasCount(store); n != 1 {
		t.Fatalf("expected one alias after the replacement, got %d", n)
	}

	// "x" (a2) is the oldest announcement; fill the store, then add one more.
	for i := range maxActiveAnnouncements {
		_, _ = add(store, clk, fmt.Sprintf("msg %d", i), 30*time.Minute)
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
	// A late copy of the evicted call does not bring it back.
	if _, outcome, _ := store.Add(a2.ID, "x", a2.SentAt, 30*time.Minute); outcome != AnnounceRecorded {
		t.Errorf("a resend of the evicted call must be recorded, got outcome %d", outcome)
	}
}

// TestAnnouncementStore_KnownIDIsRecorded: a resend of a call the store holds
// (the outbox sending again after a lost answer) changes nothing and answers
// the call as stored.
func TestAnnouncementStore_KnownIDIsRecorded(t *testing.T) {
	store, clk := newClockedStore()
	id := newParticipantID()
	madeAt := clk.now()
	first, outcome, _ := store.Add(id, "Call A", madeAt, 5*time.Minute)
	if outcome != AnnounceAdded {
		t.Fatalf("expected the first send to be added, got %d", outcome)
	}
	clk.advance(30 * time.Second)

	again, outcome, list := store.Add(id, "Call A", madeAt, 5*time.Minute)
	if outcome != AnnounceRecorded {
		t.Fatalf("expected the resend to be recorded, got %d", outcome)
	}
	if again != first {
		t.Errorf("expected the call as stored %v, got %v", first, again)
	}
	if len(list) != 1 {
		t.Errorf("expected one call showing, got %v", list)
	}
}

// TestAnnouncementStore_KnownAliasIsRecorded: a resend of the older id of an
// identical call that a newer one replaced answers the one standing in its
// place and changes nothing.
func TestAnnouncementStore_KnownAliasIsRecorded(t *testing.T) {
	store, clk := newClockedStore()
	a1, _ := add(store, clk, "x", 5*time.Minute)
	a2, _ := add(store, clk, "x", 5*time.Minute)

	got, outcome, list := store.Add(a1.ID, "x", a1.SentAt, 5*time.Minute)
	if outcome != AnnounceRecorded {
		t.Fatalf("expected recorded, got %d", outcome)
	}
	if got.ID != a2.ID {
		t.Errorf("expected the answer to be the call standing in its place %s, got %s", a2.ID, got.ID)
	}
	if len(list) != 1 || list[0] != a2 {
		t.Errorf("expected only a2 showing, unchanged, got %v", list)
	}
}

// TestAnnouncementStore_OlderIdenticalArrivingLateBecomesAnAlias: of two
// identical calls, the one made later stands, whatever the arrival order. The
// older one arriving second never replaces it or shortens its time, and its id
// still takes the call down.
func TestAnnouncementStore_OlderIdenticalArrivingLateBecomesAnAlias(t *testing.T) {
	store, clk := newClockedStore()
	t0 := clk.now()
	newer, _, _ := store.Add(newParticipantID(), "x", t0.Add(time.Second), 30*time.Minute)
	clk.advance(2 * time.Second)

	olderID := newParticipantID()
	got, outcome, list := store.Add(olderID, "x", t0, 5*time.Minute)
	if outcome != AnnounceRecorded {
		t.Fatalf("expected the older call to be recorded, got %d", outcome)
	}
	if got != newer {
		t.Errorf("expected the answer to be the newer call %v, got %v", newer, got)
	}
	if len(list) != 1 || list[0] != newer {
		t.Fatalf("expected the newer call to stand untouched, got %v", list)
	}

	if found, after := store.Remove(olderID); !found || len(after) != 0 {
		t.Errorf("expected the older id to take the call down, got %v %v", found, after)
	}
}

// TestAnnouncementStore_LateCallShowsWhatIsLeftAtLeastAMinute pins decision Q1
// (floor): a call reaching the server late shows for what is left of its time,
// but at least AnnouncementMinShow; one whose time is over shows nothing.
func TestAnnouncementStore_LateCallShowsWhatIsLeftAtLeastAMinute(t *testing.T) {
	for _, tc := range []struct {
		name    string
		late    time.Duration
		outcome AnnounceOutcome
		showFor time.Duration
	}{
		{"on time shows its full time", 0, AnnounceAdded, 5 * time.Minute},
		{"three minutes left shows three minutes", 2 * time.Minute, AnnounceAdded, 3 * time.Minute},
		{"fifteen seconds left shows a minute", 4*time.Minute + 45*time.Second, AnnounceAdded, AnnouncementMinShow},
		{"no time left shows nothing", 5 * time.Minute, AnnounceExpired, 0},
		{"long over shows nothing", 20 * time.Minute, AnnounceExpired, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, clk := newClockedStore()
			now := clk.now()
			ann, outcome, list := store.Add(newParticipantID(), "late call", now.Add(-tc.late), 5*time.Minute)
			if outcome != tc.outcome {
				t.Fatalf("expected outcome %d, got %d", tc.outcome, outcome)
			}
			if tc.outcome == AnnounceExpired {
				if len(list) != 0 || len(store.List()) != 0 {
					t.Errorf("an expired call must not show, got %v", list)
				}
				return
			}
			if got := ann.ExpiresAt.Sub(now); got != tc.showFor {
				t.Errorf("expected it to show for %v, got %v", tc.showFor, got)
			}
			if !ann.SentAt.Equal(now.Add(-tc.late)) {
				t.Errorf("expected SentAt to be the made-at, got %v", ann.SentAt)
			}
		})
	}
}

// TestAnnouncementStore_WithdrawnCallIsRemembered: a late copy of a send that
// was already withdrawn never brings the call back, and the id is forgotten
// once its time would have run out.
func TestAnnouncementStore_WithdrawnCallIsRemembered(t *testing.T) {
	store, clk := newClockedStore()
	id := newParticipantID()
	madeAt := clk.now()
	if _, outcome, _ := store.Add(id, "Call A", madeAt, 5*time.Minute); outcome != AnnounceAdded {
		t.Fatalf("expected added, got %d", outcome)
	}
	if found, _ := store.Remove(id); !found {
		t.Fatal("expected the withdrawal to find the call")
	}
	clk.advance(time.Minute)

	if _, outcome, list := store.Add(id, "Call A", madeAt, 5*time.Minute); outcome != AnnounceRecorded || len(list) != 0 {
		t.Fatalf("a late copy of a withdrawn call must be recorded and show nothing, got %d %v", outcome, list)
	}
	if found, _ := store.Remove(id); found {
		t.Error("a second withdrawal of the same call finds nothing")
	}

	clk.advance(5 * time.Minute)
	_ = store.List()
	if n := spentCount(store); n != 0 {
		t.Errorf("expected the withdrawn id to be forgotten after its expiry, %d remembered", n)
	}
	if _, outcome, _ := store.Add(id, "Call A", madeAt, 5*time.Minute); outcome != AnnounceExpired {
		t.Errorf("after its expiry a copy is expired, got %d", outcome)
	}
}

// TestAnnouncementStore_WithdrawnAliasIsRemembered: the older id of an
// identical call is remembered with the call that stood in for it, so a late
// copy of the older send does not bring the call back either.
func TestAnnouncementStore_WithdrawnAliasIsRemembered(t *testing.T) {
	store, clk := newClockedStore()
	a1, _ := add(store, clk, "x", 30*time.Minute)
	a2, _ := add(store, clk, "x", 5*time.Minute)
	if found, _ := store.Remove(a2.ID); !found {
		t.Fatal("expected a2 to be found")
	}
	if _, outcome, list := store.Add(a1.ID, "x", a1.SentAt, 30*time.Minute); outcome != AnnounceRecorded || len(list) != 0 {
		t.Errorf("a late copy of the older id must show nothing, got %d %v", outcome, list)
	}

	// The older call outlives the newer one's expiry: once the newer call has
	// run out, a copy of the older one still stays down.
	store2, clk2 := newClockedStore()
	b1, _ := add(store2, clk2, "y", 30*time.Minute)
	_, _ = add(store2, clk2, "y", 5*time.Minute)
	clk2.advance(6 * time.Minute)
	if _, outcome, list := store2.Add(b1.ID, "y", b1.SentAt, 30*time.Minute); outcome != AnnounceRecorded || len(list) != 0 {
		t.Errorf("a copy of a call a newer one replaced must not show after the newer one ran out, got %d %v", outcome, list)
	}
}

// TestAnnouncementStore_ClearKeepsAFloor: a clear takes down what was made at
// or before it, keeps what was made after, and a call made before it that
// arrives later shows nothing.
func TestAnnouncementStore_ClearKeepsAFloor(t *testing.T) {
	store, clk := newClockedStore()
	t0 := clk.now()
	a, _, _ := store.Add(newParticipantID(), "A", t0, 10*time.Minute)
	b, _, _ := store.Add(newParticipantID(), "B", t0.Add(2*time.Second), 10*time.Minute)
	clk.advance(3 * time.Second)

	list, changed := store.Clear(t0.Add(time.Second))
	if !changed {
		t.Fatal("expected the clear to take A down")
	}
	if len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("expected only B, made after the clear, to stay, got %v", list)
	}

	if _, outcome, _ := store.Add(a.ID, "A", t0, 10*time.Minute); outcome != AnnounceRecorded {
		t.Errorf("a copy of a cleared call must be recorded, got %d", outcome)
	}
	if _, outcome, list := store.Add(newParticipantID(), "C", t0.Add(500*time.Millisecond), 10*time.Minute); outcome != AnnounceRecorded || len(list) != 1 {
		t.Errorf("a call made before the clear and arriving after it must show nothing, got %d %v", outcome, list)
	}
	if _, outcome, list := store.Add(newParticipantID(), "D", t0.Add(1500*time.Millisecond), 10*time.Minute); outcome != AnnounceAdded || len(list) != 2 {
		t.Errorf("a call made after the clear must show whatever the arrival order, got %d %v", outcome, list)
	}

	// An older clear arriving late does not lower the floor.
	if _, changed := store.Clear(t0); changed {
		t.Error("an older clear must take down nothing made after it")
	}
	if _, outcome, _ := store.Add(newParticipantID(), "E", t0.Add(800*time.Millisecond), 10*time.Minute); outcome != AnnounceRecorded {
		t.Errorf("the floor stays at the latest clear, got %d", outcome)
	}
}

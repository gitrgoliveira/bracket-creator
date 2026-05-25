package state

import (
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

const maxActiveAnnouncements = 10

type AnnouncementStore struct {
	mu     sync.Mutex
	active []Announcement
}

func NewAnnouncementStore() *AnnouncementStore {
	return &AnnouncementStore{}
}

// Add appends a new announcement and returns it. Expired entries are pruned
// first; if still at capacity the oldest is evicted.
func (s *AnnouncementStore) Add(msg string, dur time.Duration) Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ann := Announcement{
		ID:        newAnnouncementID(),
		Message:   msg,
		SentAt:    now,
		ExpiresAt: now.Add(dur),
	}

	s.pruneExpiredLocked(now)

	if len(s.active) >= maxActiveAnnouncements {
		s.active = s.active[1:]
	}

	s.active = append(s.active, ann)
	return ann
}

// List returns a copy of all currently active (non-expired) announcements,
// oldest first.
func (s *AnnouncementStore) List() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	if len(s.active) == 0 {
		return nil
	}
	out := make([]Announcement, len(s.active))
	copy(out, s.active)
	return out
}

// Remove dismisses the announcement with the given ID. Returns true if found.
func (s *AnnouncementStore) Remove(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, a := range s.active {
		if a.ID == id {
			s.active = append(s.active[:i], s.active[i+1:]...)
			return true
		}
	}
	return false
}

func (s *AnnouncementStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = nil
}

// Get returns the most recent active announcement, or nil.
// Kept for backward-compat with callers expecting the single-slot API.
func (s *AnnouncementStore) Get() *Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	if len(s.active) == 0 {
		return nil
	}
	c := s.active[len(s.active)-1]
	return &c
}

// Set adds a single announcement and clears all prior ones.
// Kept for backward-compat; new callers should prefer Add.
func (s *AnnouncementStore) Set(msg string, dur time.Duration) Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ann := Announcement{
		ID:        newAnnouncementID(),
		Message:   msg,
		SentAt:    now,
		ExpiresAt: now.Add(dur),
	}
	s.active = []Announcement{ann}
	return ann
}

// pruneExpiredLocked removes expired entries. Must be called with mu held.
func (s *AnnouncementStore) pruneExpiredLocked(now time.Time) {
	kept := s.active[:0]
	for _, a := range s.active {
		if now.Before(a.ExpiresAt) {
			kept = append(kept, a)
		}
	}
	s.active = kept
}

func newAnnouncementID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

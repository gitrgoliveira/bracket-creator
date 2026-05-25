package state

import (
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

// Add appends a new announcement. Returns the new item and the updated list
// snapshot under the same lock, eliminating any race between mutation and
// broadcast.
func (s *AnnouncementStore) Add(msg string, dur time.Duration) (Announcement, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ann := makeAnnouncement(msg, dur)
	s.pruneExpiredLocked(time.Now())

	if len(s.active) >= maxActiveAnnouncements {
		s.active = s.active[1:]
	}

	s.active = append(s.active, ann)
	return ann, snapshotLocked(s.active)
}

// Remove dismisses the announcement with the given ID. Returns whether it was
// found and the updated list snapshot under the same lock.
func (s *AnnouncementStore) Remove(id string) (bool, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	for i, a := range s.active {
		if a.ID == id {
			s.active = append(s.active[:i], s.active[i+1:]...)
			return true, snapshotLocked(s.active)
		}
	}
	return false, snapshotLocked(s.active)
}

// Clear removes all announcements and returns an empty snapshot.
func (s *AnnouncementStore) Clear() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = nil
	return []Announcement{}
}

// List returns a copy of all currently active (non-expired) announcements,
// oldest first.
func (s *AnnouncementStore) List() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	return snapshotLocked(s.active)
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

	ann := makeAnnouncement(msg, dur)
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

func makeAnnouncement(msg string, dur time.Duration) Announcement {
	now := time.Now()
	return Announcement{
		ID:        newParticipantID(),
		Message:   msg,
		SentAt:    now,
		ExpiresAt: now.Add(dur),
	}
}

func snapshotLocked(active []Announcement) []Announcement {
	if len(active) == 0 {
		return []Announcement{}
	}
	out := make([]Announcement, len(active))
	copy(out, active)
	return out
}

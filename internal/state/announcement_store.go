package state

import (
	"slices"
	"sync"
	"time"
)

const maxActiveAnnouncements = 10

type AnnouncementStore struct {
	mu     sync.Mutex
	active []Announcement
	// replaced maps the id of an announcement that an identical Add replaced to
	// the id of the announcement now standing in its place, always one hop. It
	// lets a court console that still holds the older id take the call down
	// (bc-cdbl). An alias never outlives its target: dropOrphanAliasesLocked
	// sweeps it after every removal from active.
	replaced map[string]string
}

func NewAnnouncementStore() *AnnouncementStore {
	return &AnnouncementStore{}
}

// Add appends a new announcement. If an active announcement with the identical
// message already exists, it is replaced (removed and re-added with a new ID and
// fresh TTL); the replaced ID stays valid for Remove while the new item is
// active. Returns the new item and the updated list snapshot under the same
// lock, eliminating any race between mutation and broadcast.
func (s *AnnouncementStore) Add(msg string, dur time.Duration) (Announcement, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	ann := makeAnnouncement(msg, dur)
	s.pruneExpiredLocked(time.Now())

	// Remove the active announcement with the identical message, if any: at
	// most one exists, since every Add replaces it (bc-cdbl). Its id keeps
	// resolving to the announcement that replaces it, so a court console that
	// still holds the older id can take the call down.
	if i := slices.IndexFunc(s.active, func(a Announcement) bool { return a.Message == msg }); i >= 0 {
		s.recordReplacementLocked(s.active[i].ID, ann.ID)
		s.active = slices.Delete(s.active, i, i+1)
	}

	if len(s.active) >= maxActiveAnnouncements {
		s.active = s.active[1:]
	}

	s.active = append(s.active, ann)
	s.dropOrphanAliasesLocked()
	return ann, snapshotLocked(s.active)
}

// Remove dismisses the announcement with the given ID, or, for an ID an
// identical Add replaced, the announcement that replaced it. Returns whether it
// was found and the updated list snapshot under the same lock.
func (s *AnnouncementStore) Remove(id string) (bool, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(time.Now())
	i := s.indexOfIDLocked(id)
	if i < 0 {
		// An id that an identical Add replaced withdraws the announcement now
		// standing in its place.
		if target, ok := s.replaced[id]; ok {
			i = s.indexOfIDLocked(target)
		}
	}
	if i < 0 {
		return false, snapshotLocked(s.active)
	}
	s.active = slices.Delete(s.active, i, i+1)
	s.dropOrphanAliasesLocked()
	return true, snapshotLocked(s.active)
}

// Clear removes all announcements and returns an empty snapshot.
func (s *AnnouncementStore) Clear() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active = nil
	s.dropOrphanAliasesLocked()
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
	s.dropOrphanAliasesLocked()
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
	s.dropOrphanAliasesLocked()
}

// indexOfIDLocked returns the index of the active announcement with the given
// id, or -1. Must be called with mu held.
func (s *AnnouncementStore) indexOfIDLocked(id string) int {
	return slices.IndexFunc(s.active, func(a Announcement) bool { return a.ID == id })
}

// recordReplacementLocked notes that the announcement newID replaced oldID, and
// re-points every alias that targeted oldID at newID, so a run of identical
// re-calls stays one hop from each older id to the latest. Must be called with
// mu held.
func (s *AnnouncementStore) recordReplacementLocked(oldID, newID string) {
	if s.replaced == nil {
		s.replaced = make(map[string]string)
	}
	for alias, target := range s.replaced {
		if target == oldID {
			s.replaced[alias] = newID
		}
	}
	s.replaced[oldID] = newID
}

// dropOrphanAliasesLocked drops every alias whose target is no longer active,
// so an alias never outlives the announcement it points at. Call it after ANY
// removal from s.active (expiry, the cap's eviction, Remove, Clear, Set). Must
// be called with mu held.
func (s *AnnouncementStore) dropOrphanAliasesLocked() {
	for alias, target := range s.replaced {
		if s.indexOfIDLocked(target) < 0 {
			delete(s.replaced, alias)
		}
	}
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

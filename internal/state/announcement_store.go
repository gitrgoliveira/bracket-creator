package state

import (
	"sync"
	"time"
)

type AnnouncementStore struct {
	mu      sync.RWMutex
	current *Announcement
}

func NewAnnouncementStore() *AnnouncementStore {
	return &AnnouncementStore{}
}

func (s *AnnouncementStore) Set(msg string, dur time.Duration) Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	ann := Announcement{
		Message:   msg,
		SentAt:    now,
		ExpiresAt: now.Add(dur),
	}
	s.current = &Announcement{
		Message:   ann.Message,
		SentAt:    ann.SentAt,
		ExpiresAt: ann.ExpiresAt,
	}
	return ann
}

func (s *AnnouncementStore) Get() *Announcement {
	s.mu.RLock()
	if s.current == nil {
		s.mu.RUnlock()
		return nil
	}
	if time.Now().Before(s.current.ExpiresAt) {
		annCopy := *s.current
		s.mu.RUnlock()
		return &annCopy
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// Double check under write lock
	if s.current != nil && time.Now().After(s.current.ExpiresAt) {
		s.current = nil
	}
	return nil
}

func (s *AnnouncementStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.current = nil
}

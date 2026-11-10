package state

import (
	"slices"
	"sync"
	"time"
)

const maxActiveAnnouncements = 10

// AnnouncementMinShow is the shortest time a court call shows when it reaches
// the server late with some of its time left (decision Q1, "floor"): a call
// made offline and sent when the link came back shows for what is left of its
// time, but never less than this, so players can read it. A call whose time is
// over when it arrives shows nothing.
const AnnouncementMinShow = time.Minute

// AnnouncementMaxShow is the longest a court call is made to show (the handler
// accepts 5 to 30 minutes). It is how long a withdrawal of a call the server has
// not seen is remembered: any send of that call still on its way was made
// before the withdrawal, so it is over by then.
const AnnouncementMaxShow = 30 * time.Minute

// AnnounceOutcome says what Add did with a call.
type AnnounceOutcome int

const (
	// AnnounceAdded: the call is now showing; the caller broadcasts the list.
	AnnounceAdded AnnounceOutcome = iota
	// AnnounceRecorded: nothing changed and nothing is broadcast. The id is
	// already known (showing, an alias of the call that replaced it, or a call
	// withdrawn or cleared and remembered until its expiry), the call is an
	// older copy of the identical message already showing, or it was made at
	// or before the last clear.
	AnnounceRecorded
	// AnnounceExpired: its time was over when it arrived; nothing is shown.
	AnnounceExpired
)

type AnnouncementStore struct {
	mu     sync.Mutex
	active []Announcement
	// replaced maps the id of an identical call that another call stands in
	// for to that call's id, always one hop, with the alias's own record. It
	// lets a court console that still holds the older id take the call down
	// (bc-cdbl). An alias never outlives its target: retireLocked moves it to
	// spent on every removal of the target.
	replaced map[string]announcementAlias
	// spent remembers the calls taken down (withdrawn, cleared, evicted by the
	// cap, or aliases whose target went) until the time they would have
	// expired, so a late copy of a send already withdrawn never brings the call
	// back. Each record's ExpiresAt is how long it is remembered.
	spent map[string]Announcement
	// clearFloor is the made-at of the latest clear: a call made at or before
	// it shows nothing, whenever it arrives.
	clearFloor time.Time
	// now is the clock; nil means time.Now. Tests set it.
	now func() time.Time
}

type announcementAlias struct {
	target string
	own    Announcement
}

func NewAnnouncementStore() *AnnouncementStore {
	return &AnnouncementStore{}
}

func (s *AnnouncementStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// Add records the court call id, made at madeAt, showing for dur from madeAt.
// It returns the announcement as stored (for a known id, the call it resolves
// to), what it did, and the list snapshot under the same lock, so mutation and
// broadcast cannot race.
//
// A call already known by its id is answered as recorded and changes nothing.
// One whose time is over is expired. A late call with time left shows for what
// is left, but at least AnnouncementMinShow. The identical message showing
// already is ordered by made-at (bc-cdbl): a newer call replaces it and the
// older id becomes its alias; an older call arriving after a newer one becomes
// the alias of the one showing, never replacing it or shortening its time.
func (s *AnnouncementStore) Add(id, msg string, madeAt time.Time, dur time.Duration) (Announcement, AnnounceOutcome, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock()
	s.pruneExpiredLocked(now)

	if known, ok := s.knownLocked(id); ok {
		return known, AnnounceRecorded, snapshotLocked(s.active)
	}

	end := madeAt.Add(dur)
	if !now.Before(end) {
		return Announcement{ID: id, Message: msg, SentAt: madeAt, ExpiresAt: end}, AnnounceExpired, snapshotLocked(s.active)
	}
	expires := end
	if floor := now.Add(AnnouncementMinShow); expires.Before(floor) {
		expires = floor
	}
	ann := Announcement{ID: id, Message: msg, SentAt: madeAt, ExpiresAt: expires}

	if !madeAt.After(s.clearFloor) {
		return ann, AnnounceRecorded, snapshotLocked(s.active)
	}

	// At most one identical call is showing, since every newer one replaces it.
	if i := slices.IndexFunc(s.active, func(a Announcement) bool { return a.Message == msg }); i >= 0 {
		cur := s.active[i]
		if !madeAt.After(cur.SentAt) {
			s.aliasLocked(ann, cur.ID)
			return cur, AnnounceRecorded, snapshotLocked(s.active)
		}
		s.active = slices.Delete(s.active, i, i+1)
		s.recordReplacementLocked(cur, ann.ID)
	}

	// Kept oldest made-at first, whatever order calls arrive in, so the cap
	// drops the oldest call and Get answers the newest.
	at, _ := slices.BinarySearchFunc(s.active, ann, func(a, b Announcement) int { return a.SentAt.Compare(b.SentAt) })
	s.active = slices.Insert(s.active, at, ann)
	if len(s.active) > maxActiveAnnouncements {
		evicted := s.active[0]
		s.active = s.active[1:]
		s.retireLocked(evicted, now)
		if evicted.ID == ann.ID {
			return ann, AnnounceRecorded, snapshotLocked(s.active)
		}
	}
	return ann, AnnounceAdded, snapshotLocked(s.active)
}

// Remove dismisses the announcement with the given ID, or, for an ID an
// identical call stands in for, that call. The call and its aliases are
// remembered until their expiry. Returns whether it was found and the updated
// list snapshot under the same lock.
//
// A withdrawal of a call the server has not seen (a client id, not showing and
// not remembered) is answered as not found but remembered for
// AnnouncementMaxShow: the send can still arrive from another tab of the device
// that had it out when the withdrawal was made, and it is then recorded, never
// shown.
func (s *AnnouncementStore) Remove(id string) (bool, []Announcement) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock()
	s.pruneExpiredLocked(now)
	i := s.indexOfIDLocked(id)
	if i < 0 {
		if al, ok := s.replaced[id]; ok {
			i = s.indexOfIDLocked(al.target)
		}
	}
	if i < 0 {
		if _, known := s.knownLocked(id); !known && IsClientUUID(id) {
			s.spendLocked(Announcement{ID: id, ExpiresAt: now.Add(AnnouncementMaxShow)}, now)
		}
		return false, snapshotLocked(s.active)
	}
	gone := s.active[i]
	s.active = slices.Delete(s.active, i, i+1)
	s.retireLocked(gone, now)
	return true, snapshotLocked(s.active)
}

// Clear takes down every call made at or before madeAt and keeps that as the
// floor below which a call arriving later shows nothing; a call made after it
// stays. The kept floor never passes the server's now, so a clear stamped a few
// seconds ahead cannot hide a call another device makes in that window, but the
// take-down of what is already showing goes by madeAt itself: a call the same
// device stamped ahead of the server and then cleared does not survive the
// clear. Returns the updated snapshot and whether anything was taken down.
func (s *AnnouncementStore) Clear(madeAt time.Time) ([]Announcement, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.clock()
	s.pruneExpiredLocked(now)
	// The floor never passes the server's now: a clear stamped a few seconds
	// ahead (within the skew the door accepts) must not hide a call another
	// device makes in that window.
	floor := madeAt
	if floor.After(now) {
		floor = now
	}
	if floor.After(s.clearFloor) {
		s.clearFloor = floor
	}
	var kept, gone []Announcement
	for _, a := range s.active {
		if a.SentAt.After(madeAt) {
			kept = append(kept, a)
		} else {
			gone = append(gone, a)
		}
	}
	s.active = kept
	for _, a := range gone {
		s.retireLocked(a, now)
	}
	return snapshotLocked(s.active), len(gone) > 0
}

// List returns a copy of all currently active (non-expired) announcements,
// oldest first.
func (s *AnnouncementStore) List() []Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(s.clock())
	return snapshotLocked(s.active)
}

// Get returns the most recent active announcement, or nil.
// Kept for backward-compat with callers expecting the single-slot API.
func (s *AnnouncementStore) Get() *Announcement {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pruneExpiredLocked(s.clock())
	if len(s.active) == 0 {
		return nil
	}
	c := s.active[len(s.active)-1]
	return &c
}

// pruneExpiredLocked removes expired entries and forgets spent ids whose time
// is over. Must be called with mu held.
func (s *AnnouncementStore) pruneExpiredLocked(now time.Time) {
	var gone []Announcement
	kept := s.active[:0]
	for _, a := range s.active {
		if now.Before(a.ExpiresAt) {
			kept = append(kept, a)
		} else {
			gone = append(gone, a)
		}
	}
	s.active = kept
	for _, a := range gone {
		s.retireLocked(a, now)
	}
	for id, a := range s.spent {
		if !now.Before(a.ExpiresAt) {
			delete(s.spent, id)
		}
	}
}

// knownLocked answers the call an id already names: the one showing under it
// or standing in for it, or the remembered record of one taken down. Must be
// called with mu held.
func (s *AnnouncementStore) knownLocked(id string) (Announcement, bool) {
	if i := s.indexOfIDLocked(id); i >= 0 {
		return s.active[i], true
	}
	if al, ok := s.replaced[id]; ok {
		if i := s.indexOfIDLocked(al.target); i >= 0 {
			return s.active[i], true
		}
	}
	if a, ok := s.spent[id]; ok {
		return a, true
	}
	return Announcement{}, false
}

// indexOfIDLocked returns the index of the active announcement with the given
// id, or -1. Must be called with mu held.
func (s *AnnouncementStore) indexOfIDLocked(id string) int {
	return slices.IndexFunc(s.active, func(a Announcement) bool { return a.ID == id })
}

// aliasLocked makes own an alias of the active call targetID. Must be called
// with mu held.
func (s *AnnouncementStore) aliasLocked(own Announcement, targetID string) {
	if s.replaced == nil {
		s.replaced = make(map[string]announcementAlias)
	}
	s.replaced[own.ID] = announcementAlias{target: targetID, own: own}
}

// recordReplacementLocked notes that the call newID replaced old, and
// re-points every alias that targeted old at newID, so a run of identical
// re-calls stays one hop from each older id to the latest. Must be called with
// mu held.
func (s *AnnouncementStore) recordReplacementLocked(old Announcement, newID string) {
	for alias, al := range s.replaced {
		if al.target == old.ID {
			al.target = newID
			s.replaced[alias] = al
		}
	}
	s.aliasLocked(old, newID)
}

// retireLocked remembers a call just taken off active, and every alias that
// pointed at it, until the time each would have expired, and drops those
// aliases. Call it after ANY removal from s.active (expiry, the cap's
// eviction, Remove, Clear). Must be called with mu held.
func (s *AnnouncementStore) retireLocked(a Announcement, now time.Time) {
	s.spendLocked(a, now)
	for alias, al := range s.replaced {
		if al.target == a.ID {
			s.spendLocked(al.own, now)
			delete(s.replaced, alias)
		}
	}
}

func (s *AnnouncementStore) spendLocked(a Announcement, now time.Time) {
	if !now.Before(a.ExpiresAt) {
		return
	}
	if s.spent == nil {
		s.spent = make(map[string]Announcement)
	}
	s.spent[a.ID] = a
}

func snapshotLocked(active []Announcement) []Announcement {
	if len(active) == 0 {
		return []Announcement{}
	}
	out := make([]Announcement, len(active))
	copy(out, active)
	return out
}

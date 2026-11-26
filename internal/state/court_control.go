package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
)

// CourtControlFilename is the file at the data folder's root that records
// which court console tab runs each shiaijo (FR9). It is not a tournament
// record: no backup lists it (snapshotTournamentFilesLocked names only
// tournament.md, branding/ and sponsors/), and it is cleared whenever the
// whole tournament is replaced.
const CourtControlFilename = "court-control.json"

// MaxCourtDisplaced caps each displaced list of a shiaijo: the tabs and the
// tablets that lost it by a take. The oldest key leaves first.
const MaxCourtDisplaced = 8

// courtControlKeyLen is the length of a one-way key: the first 16 hex
// characters of the SHA-256 of the raw id.
const courtControlKeyLen = 16

// CourtDeviceOther is the device word anything unknown reads as, so no free
// text from a request or a hand-edited file reaches another screen.
const CourtDeviceOther = "other"

// courtDeviceWords are the device words a holder may carry.
var courtDeviceWords = []string{"ipad", "android-tablet", "iphone", "android-phone", "mac", "windows", CourtDeviceOther}

// CourtDeviceWord answers word when it is one of the known device words,
// and CourtDeviceOther for anything else.
func CourtDeviceWord(word string) string {
	if slices.Contains(courtDeviceWords, word) {
		return word
	}
	return CourtDeviceOther
}

// CourtControlKey is the one-way key of a tab id or a tablet id: the first
// 16 hex characters of its SHA-256. The raw id is never stored, so the file
// grants nothing. An empty id has no key.
func CourtControlKey(id string) string {
	if id == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(id))
	return hex.EncodeToString(sum[:])[:courtControlKeyLen]
}

// isCourtControlKey reports whether k has the shape CourtControlKey makes.
func isCourtControlKey(k string) bool {
	if len(k) != courtControlKeyLen {
		return false
	}
	for _, c := range k {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// CourtHold is one shiaijo's control. HolderKey empty means nobody holds it;
// such an entry is kept only while a displaced list remembers a tab or a
// tablet that lost it by a take. Times are unix milliseconds.
type CourtHold struct {
	// HolderKey is the one-way key of the holding tab's id.
	HolderKey string `json:"holderKey,omitempty"`
	// DeviceKey is the one-way key of the holding tab's tablet id. The
	// court's standing is judged by it: it belongs to the tablet that holds.
	DeviceKey string `json:"deviceKey,omitempty"`
	// Device is the holder's device word (CourtDeviceWord).
	Device string `json:"device,omitempty"`
	// Since is when the holder took the shiaijo, on the server's clock.
	Since int64 `json:"since,omitempty"`
	// StandingFrom is the earliest write stamp of the holding tablet that
	// has the court's standing: 0 for a free shiaijo taken at once, the
	// take's takenAt for another tablet's take.
	StandingFrom int64 `json:"standingFrom,omitempty"`
	// PageID names the holding tab's page load, so a reload's late release
	// never frees the new page's claim.
	PageID string `json:"pageId,omitempty"`
	// Displaced holds the keys of the tabs that lost the shiaijo by a take,
	// oldest first, at most MaxCourtDisplaced.
	Displaced []string `json:"displaced,omitempty"`
	// DisplacedDevices holds the keys of the tablets that lost it by another
	// tablet's take, oldest first, at most MaxCourtDisplaced.
	DisplacedDevices []string `json:"displacedDevices,omitempty"`
}

// Held reports whether a tab holds the shiaijo.
func (h CourtHold) Held() bool { return h.HolderKey != "" }

// IsDisplaced reports whether the tab key is on the displaced list.
func (h CourtHold) IsDisplaced(tabKey string) bool {
	return tabKey != "" && slices.Contains(h.Displaced, tabKey)
}

// IsDisplacedDevice reports whether the tablet key is on the displaced
// tablets list.
func (h CourtHold) IsDisplacedDevice(deviceKey string) bool {
	return deviceKey != "" && slices.Contains(h.DisplacedDevices, deviceKey)
}

// AddDisplaced puts the tab key last on the displaced list, dropping the
// oldest past MaxCourtDisplaced.
func (h *CourtHold) AddDisplaced(tabKey string) { h.Displaced = pushCourtKey(h.Displaced, tabKey) }

// AddDisplacedDevice puts the tablet key last on the displaced tablets
// list, dropping the oldest past MaxCourtDisplaced.
func (h *CourtHold) AddDisplacedDevice(deviceKey string) {
	h.DisplacedDevices = pushCourtKey(h.DisplacedDevices, deviceKey)
}

// RemoveDisplaced takes the tab key off the displaced list.
func (h *CourtHold) RemoveDisplaced(tabKey string) {
	h.Displaced = slices.DeleteFunc(h.Displaced, func(k string) bool { return k == tabKey })
}

// RemoveDisplacedDevice takes the tablet key off the displaced tablets list.
func (h *CourtHold) RemoveDisplacedDevice(deviceKey string) {
	h.DisplacedDevices = slices.DeleteFunc(h.DisplacedDevices, func(k string) bool { return k == deviceKey })
}

// pushCourtKey moves key to the end of list (adding it when absent) and
// keeps the newest MaxCourtDisplaced.
func pushCourtKey(list []string, key string) []string {
	if key == "" {
		return list
	}
	list = slices.DeleteFunc(slices.Clone(list), func(k string) bool { return k == key })
	list = append(list, key)
	if len(list) > MaxCourtDisplaced {
		list = list[len(list)-MaxCourtDisplaced:]
	}
	return list
}

// normalized is h as the file keeps it: a holder with a malformed key reads
// as free, the device word is a known one, each displaced list holds only
// well-formed keys, once each, newest MaxCourtDisplaced. ok is false when
// nothing is left to keep.
func (h CourtHold) normalized() (CourtHold, bool) {
	out := CourtHold{
		Displaced:        cleanCourtKeys(h.Displaced),
		DisplacedDevices: cleanCourtKeys(h.DisplacedDevices),
	}
	if isCourtControlKey(h.HolderKey) && isCourtControlKey(h.DeviceKey) {
		out.HolderKey = h.HolderKey
		out.DeviceKey = h.DeviceKey
		out.Device = CourtDeviceWord(h.Device)
		out.Since = max(h.Since, 0)
		out.StandingFrom = max(h.StandingFrom, 0)
		out.PageID = h.PageID
	}
	return out, out.Held() || len(out.Displaced) > 0 || len(out.DisplacedDevices) > 0
}

// cleanCourtKeys keeps the well-formed keys of list, the last copy of each,
// newest MaxCourtDisplaced, or nil when none is left.
func cleanCourtKeys(list []string) []string {
	var out []string
	for _, k := range list {
		if isCourtControlKey(k) {
			out = pushCourtKey(out, k)
		}
	}
	return out
}

// CourtControl is the whole file: each shiaijo's control, keyed by its
// letter. A shiaijo absent from Courts is free and remembers nobody.
type CourtControl struct {
	Courts map[string]CourtHold `json:"courts"`
}

// Copy is a deep copy of c, so a caller may change it freely.
func (c CourtControl) Copy() CourtControl {
	out := CourtControl{Courts: make(map[string]CourtHold, len(c.Courts))}
	for court, h := range c.Courts {
		h.Displaced = slices.Clone(h.Displaced)
		h.DisplacedDevices = slices.Clone(h.DisplacedDevices)
		out.Courts[court] = h
	}
	return out
}

// courtControlMu serialises the file's load, save and clear. It is a leaf:
// taken under nothing the store holds (never inside a competition lock or
// s.mu) and holding nothing while it is held. One mutex for every store in
// the process, since the file is tiny and a test may open several stores.
var courtControlMu sync.Mutex

func (s *Store) courtControlPath() string {
	return filepath.Clean(filepath.Join(s.folder, CourtControlFilename))
}

// LoadCourtControl reads court-control.json. A missing file means nobody
// runs anything. A file that cannot be read or parsed is logged at WARN and
// read as empty: the holder's next claim puts it back, so it never refuses a
// start. Entries are normalized as the save keeps them. Never nil Courts.
func (s *Store) LoadCourtControl() CourtControl {
	courtControlMu.Lock()
	defer courtControlMu.Unlock()
	path := s.courtControlPath()
	out := CourtControl{Courts: map[string]CourtHold{}}
	raw, err := s.root.readFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("state: court-control.json cannot be read; every shiaijo is read as free", "path", path, "error", err)
		}
		return out
	}
	var stored CourtControl
	if err := json.Unmarshal(raw, &stored); err != nil {
		slog.Warn("state: court-control.json cannot be parsed; every shiaijo is read as free", "path", path, "error", err)
		return out
	}
	for court, h := range stored.Courts {
		if n, ok := h.normalized(); ok && court != "" {
			out.Courts[court] = n
		}
	}
	return out
}

// SaveCourtControl writes c to court-control.json (a temp file and a rename,
// through the data root). listed is the tournament's shiaijo: an entry for a
// shiaijo it does not list is dropped, as is an entry that holds nobody and
// remembers nobody. The caller saves only when a holder or a displaced list
// changes, never on a heartbeat.
func (s *Store) SaveCourtControl(c CourtControl, listed []string) error {
	keep := CourtControl{Courts: map[string]CourtHold{}}
	for court, h := range c.Courts {
		if !slices.Contains(listed, court) {
			continue
		}
		if n, ok := h.normalized(); ok {
			keep.Courts[court] = n
		}
	}
	data, err := json.MarshalIndent(keep, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", CourtControlFilename, err)
	}
	data = append(data, '\n')
	courtControlMu.Lock()
	defer courtControlMu.Unlock()
	return s.atomicWrite(s.courtControlPath(), data, 0o600)
}

// ClearCourtControl removes court-control.json, freeing every shiaijo and
// forgetting both displaced lists: the whole tournament was replaced (a new
// tournament, a full restore), and its shiaijo letters repeat. A file
// already gone is no error.
func (s *Store) ClearCourtControl() error {
	courtControlMu.Lock()
	defer courtControlMu.Unlock()
	if err := s.root.remove(s.courtControlPath()); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

package mobileapp

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The court control registry (bc-sseq FR9): which court console tab runs
// each shiaijo, kept in court-control.json at the data folder's root
// (state.CourtControl) and in memory here.
//
// The green bar belongs to a TAB (HolderKey), the court's standing to the
// TABLET (DeviceKey): a result from any court console tab of the tablet
// that runs the shiaijo counts as the court's (round bc-sseq-plan-4). The
// raw tab and tablet ids are never stored, answered or broadcast; only their
// one-way keys (state.CourtControlKey) are kept, so the file grants nothing.
//
// The file is saved only when a holder, a holder's page load or device word,
// or a displaced list changes, never on a heartbeat. lastActiveAt is kept in
// memory only: after a restart it starts at the holder's take.
//
// A shiaijo its tab gives back (the tab closed or reloaded) stays with its
// tablet for courtReleaseGrace, in memory only (the file has it free at
// once), so a reload gets it back before another console claims it. A
// restart during a grace ends it: the shiaijo is free, and the first console
// to claim it after the restart takes it.

// errUnknownCourt is a shiaijo the tournament does not list.
var errUnknownCourt = errors.New("unknown shiaijo")

// errCourtControlBadRequest is a claim or release whose ids are not the
// lowercase UUID v4 a court console mints, a take with no takenAt, or a claim
// whose keep list exceeds helper.MaxCourts entries.
var errCourtControlBadRequest = errors.New("bad court control request")

// CourtClaim is one claim of a shiaijo: a console opening it, its heartbeat,
// or (Take) an explicit Take control. TakenAt is the take's time on the
// taking tab's own clock, server-relative unix milliseconds, stamped as the
// outbox stamps a write. Keep names other shiaijo this tab must keep holding
// because its tablet still has court-stamped results queued for them.
type CourtClaim struct {
	Court    string
	TabID    string
	DeviceID string
	PageID   string
	Device   string
	Take     bool
	TakenAt  int64
	Keep     []string
}

// CourtControlAnswer is a shiaijo's control as one caller sees it. HolderKey
// is "" while nobody runs it. YouWereDisplaced is true when the caller's
// tablet lost the shiaijo to another tablet; TabWasDisplaced is true when
// only this tab lost it (to another tab of the same tablet, so the tablet
// itself is not displaced). Both are false while the caller's tablet holds
// it (a tab of the holding tablet is shown the "another tab on this tablet"
// bar, never a displacement warning). ReleasedAt is when the holder's tab
// gave the shiaijo back while the grace keeps it for its tablet (0 when it
// is held, or free); no console reads it as a state of its own. Times are
// unix milliseconds on the server's clock.
type CourtControlAnswer struct {
	Court            string `json:"court"`
	HolderKey        string `json:"holderKey"`
	Device           string `json:"device"`
	YouHold          bool   `json:"youHold"`
	YouWereDisplaced bool   `json:"youWereDisplaced"`
	TabWasDisplaced  bool   `json:"tabWasDisplaced"`
	SameDevice       bool   `json:"sameDevice"`
	Since            int64  `json:"since"`
	LastActiveAt     int64  `json:"lastActiveAt"`
	ReleasedAt       int64  `json:"releasedAt"`
}

// courtControlEvent is court_control_updated's payload: who runs the shiaijo
// now. It carries the holder's one-way key and device word only.
type courtControlEvent struct {
	Court     string `json:"court"`
	HolderKey string `json:"holderKey"`
	Device    string `json:"device"`
	Since     int64  `json:"since"`
}

// courtControlRegistry is the one owner of who runs each shiaijo. Its mutex
// is safe to take under a competition transaction (WriteStanding, when the
// write doors call it) and holds only the court-control file's own leaf
// mutex and the hub's, so the tournament is read before it is taken.
type courtControlRegistry struct {
	store CourtControlStore
	hub   Broadcaster
	now   func() time.Time

	// afterFunc starts a grace's timer (time.AfterFunc; a test's own).
	afterFunc func(time.Duration, func()) courtGraceTimer

	mu         sync.Mutex
	holds      map[string]state.CourtHold
	lastActive map[string]int64
	// grace is each shiaijo a tab gave back that is still kept for its
	// tablet (courtReleaseGrace). It is in memory only: the file already
	// has the shiaijo free, so a restart ends every grace.
	grace map[string]*courtGrace
}

// courtReleaseGrace is how long a shiaijo a court console tab gave back
// (closed or reloaded) stays with its tablet, so that a reload gets it back
// before any other console can claim it. A reload over a weak venue network
// can take several seconds (a slow page load, a lost request resent), so the
// grace is 10 seconds: long enough for that, short enough that a tablet that
// is really gone does not hold up the court (operator ruling 2026-11-27).
// Any console can still Take control at once, and every console claims the
// shiaijo as soon as the grace ends. The server keeps the
// grace, not the waiting consoles, because their clocks and the events they
// are sent are not to be trusted on such a network (operator ruling: "there
// needs to be a grace period. remember you are dealing with struggling
// network conditions").
const courtReleaseGrace = 10 * time.Second

// courtGraceTimer is a grace's timer: Stop it when the grace ends early.
type courtGraceTimer interface{ Stop() bool }

// courtGrace is a released shiaijo kept for its tablet: the hold as it was
// when its tab gave it back, and the timer that ends the grace.
type courtGrace struct {
	hold       state.CourtHold
	releasedAt int64
	timer      courtGraceTimer
}

// newCourtControlRegistry loads court-control.json (a missing or unreadable
// file reads as nobody running anything) and starts each holder's last
// activity at its take.
func newCourtControlRegistry(store CourtControlStore, hub Broadcaster) *courtControlRegistry {
	r := &courtControlRegistry{
		store:      store,
		hub:        hub,
		now:        time.Now,
		afterFunc:  func(d time.Duration, f func()) courtGraceTimer { return time.AfterFunc(d, f) },
		holds:      store.LoadCourtControl().Copy().Courts,
		lastActive: map[string]int64{},
		grace:      map[string]*courtGrace{},
	}
	for court, h := range r.holds {
		if h.Held() {
			r.lastActive[court] = h.Since
		}
	}
	return r
}

// listedCourts is the tournament's shiaijo, read before the registry's lock
// is taken. A tournament not created yet lists none.
func (r *courtControlRegistry) listedCourts() ([]string, error) {
	t, err := r.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	if t == nil {
		return nil, nil
	}
	return t.Courts, nil
}

// checkCourt reads the listed shiaijo and refuses one the tournament does
// not list.
func (r *courtControlRegistry) checkCourt(court string) ([]string, error) {
	listed, err := r.listedCourts()
	if err != nil {
		return nil, err
	}
	if !slices.Contains(listed, court) {
		return nil, errUnknownCourt
	}
	return listed, nil
}

// answerLocked is h as the caller with tabKey and devKey sees it. Caller
// holds r.mu.
func (r *courtControlRegistry) answerLocked(court string, h state.CourtHold, tabKey, devKey string) CourtControlAnswer {
	a := CourtControlAnswer{Court: court}
	sameDevice := false
	if h.Held() {
		a.HolderKey = h.HolderKey
		a.Device = h.Device
		a.Since = h.Since
		a.LastActiveAt = r.lastActive[court]
		sameDevice = devKey != "" && h.DeviceKey == devKey
		if g := r.grace[court]; g != nil && g.hold.HolderKey == h.HolderKey {
			a.ReleasedAt = g.releasedAt
		}
		// A hold given back is run by no tab, its own included.
		a.YouHold = tabKey != "" && h.HolderKey == tabKey && a.ReleasedAt == 0
	}
	a.SameDevice = sameDevice
	displacedDevice := h.IsDisplacedDevice(devKey)
	a.YouWereDisplaced = !sameDevice && displacedDevice
	a.TabWasDisplaced = !sameDevice && !displacedDevice && h.IsDisplaced(tabKey)
	return a
}

// Look is the shiaijo's control as any caller sees it: no tab, no tablet.
func (r *courtControlRegistry) Look(court string) (CourtControlAnswer, error) {
	if _, err := r.checkCourt(court); err != nil {
		return CourtControlAnswer{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.answerLocked(court, r.viewLocked(court), "", ""), nil
}

// viewLocked is the shiaijo's hold as a claim sees it: during a grace, the
// hold its tablet gave back (so another tablet is answered that the shiaijo
// is run from that device), else the hold as stored. Caller holds r.mu.
func (r *courtControlRegistry) viewLocked(court string) state.CourtHold {
	if g := r.grace[court]; g != nil {
		return g.hold
	}
	return r.holds[court]
}

// startGraceLocked keeps h, the hold its tab just gave back, for its tablet
// for courtReleaseGrace. Caller holds r.mu.
func (r *courtControlRegistry) startGraceLocked(court string, h state.CourtHold) {
	r.stopGraceLocked(court)
	g := &courtGrace{hold: h, releasedAt: r.now().UnixMilli()}
	g.timer = r.afterFunc(courtReleaseGrace, func() { r.graceOver(court, g) })
	r.grace[court] = g
}

// stopGraceLocked ends the shiaijo's grace early (a reclaim, a take, a
// switch, Clear). Caller holds r.mu.
func (r *courtControlRegistry) stopGraceLocked(court string) {
	if g := r.grace[court]; g != nil {
		g.timer.Stop()
		delete(r.grace, court)
	}
}

// graceOver ends g, the shiaijo's grace, when its time is up and nothing
// ended it before: the shiaijo is free, and that is announced so every open
// console claims it at once. A grace already ended (or replaced) is left.
func (r *courtControlRegistry) graceOver(court string, g *courtGrace) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.grace[court] != g {
		return
	}
	delete(r.grace, court)
	delete(r.lastActive, court)
	r.announceLocked([]string{court})
}

// clampTakenAt is a take's standing start: the taking tab's own stamp, so
// only stamps from the holding tablet are ever compared with it, held to at
// most the server's now plus the stamp margin the write doors allow, so a
// clock far ahead cannot strip the standing from the take's own results.
func clampTakenAt(takenAt int64, now time.Time) int64 {
	return min(takenAt, now.UnixMilli()+modifiedAtRefuseSkewMs)
}

// validCourtClaim reports whether req carries the ids a court console mints,
// at most helper.MaxCourts entries in Keep, and, for a take, its time.
func validCourtClaim(req CourtClaim) bool {
	if !state.IsClientUUID(req.TabID) || !state.IsClientUUID(req.DeviceID) || !state.IsClientUUID(req.PageID) {
		return false
	}
	if len(req.Keep) > helper.MaxCourts {
		return false
	}
	return !req.Take || req.TakenAt > 0
}

// Claim is a console's claim of a shiaijo:
//   - a free shiaijo is taken at once with standingFrom 0, unless the tab is
//     on its displaced list or the tablet on its displaced tablets list (it
//     lost the shiaijo by a take): then only an explicit take does it, with
//     standingFrom the take's takenAt when the tablet lost it to another
//     tablet (0 when only the tab lost it, to another tab of its tablet);
//   - the holder's own claim (a heartbeat) refreshes its last activity,
//     records its page load, device word and tablet id when they changed
//     (saved only when they did), keeps the standing where it was, and
//     announces nothing (other consoles read a new device word on their next
//     claim);
//   - taking from another tab needs Take; the former holder's tab joins the
//     displaced list. A take by ANOTHER tablet also puts the former holder's
//     tablet on the displaced tablets list (the taker's leaves it) and starts
//     the standing at the take's time; a take between two tabs of the SAME
//     tablet moves the bar only, so nothing the tablet queued earlier loses
//     the court's standing;
//   - claiming one shiaijo releases any other the same tab held (switching
//     the console's shiaijo), except those named in Keep, and ends at once
//     the grace of any other this tab gave back. Entries in Keep that this
//     tab does not already hold are ignored;
//   - during a shiaijo's grace (Release), the tab that gave it back or any
//     other tab of its tablet takes it back at once (a tab on the displaced
//     list only by a take), with the standing as it was; another tablet's
//     claim is answered as the not-holder answer, the hold that was given
//     back, and takes nothing, while its take takes it as from a running
//     holder.
func (r *courtControlRegistry) Claim(req CourtClaim) (CourtControlAnswer, error) {
	if !validCourtClaim(req) {
		return CourtControlAnswer{}, errCourtControlBadRequest
	}
	listed, err := r.checkCourt(req.Court)
	if err != nil {
		return CourtControlAnswer{}, err
	}
	tabKey, devKey := state.CourtControlKey(req.TabID), state.CourtControlKey(req.DeviceID)
	device := state.CourtDeviceWord(req.Device)

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	nowMs := now.UnixMilli()
	next := r.copyLocked()
	h := next[req.Court]
	_, inGrace := r.grace[req.Court]
	if inGrace {
		h = r.viewLocked(req.Court)
	}
	var changed []string

	switch {
	case inGrace && (h.HolderKey == tabKey || h.DeviceKey == devKey) &&
		(req.Take || h.HolderKey == tabKey || !h.IsDisplaced(tabKey)):
		// The tablet that gave it back takes it back (a reload, or another of
		// its tabs) with its standing whole; the tab that held it keeps its
		// since too.
		since := nowMs
		if h.HolderKey == tabKey {
			since = h.Since
		}
		h.RemoveDisplaced(tabKey)
		h = holdBy(h, tabKey, devKey, device, req.PageID, since, h.StandingFrom)

	case h.Held() && h.HolderKey == tabKey:
		if h.PageID != req.PageID || h.Device != device || h.DeviceKey != devKey {
			// A reload (a new page load), or a holder whose tablet id changed:
			// the tab still holds, and the standing stays where it was.
			h.PageID, h.Device, h.DeviceKey = req.PageID, device, devKey
			next[req.Court] = h
			if err := r.saveLocked(next, listed); err != nil {
				return CourtControlAnswer{}, err
			}
		}
		r.lastActive[req.Court] = nowMs
		return r.answerLocked(req.Court, r.holds[req.Court], tabKey, devKey), nil

	case !h.Held():
		displaced := h.IsDisplaced(tabKey) || h.IsDisplacedDevice(devKey)
		if displaced && !req.Take {
			return r.answerLocked(req.Court, h, tabKey, devKey), nil
		}
		// Only a tablet that lost the shiaijo to ANOTHER tablet starts its
		// standing afresh; a tab that lost it to another tab of its own
		// tablet keeps the tablet's standing whole.
		standingFrom := int64(0)
		if h.IsDisplacedDevice(devKey) {
			standingFrom = clampTakenAt(req.TakenAt, now)
		}
		h.RemoveDisplaced(tabKey)
		h.RemoveDisplacedDevice(devKey)
		h = holdBy(h, tabKey, devKey, device, req.PageID, nowMs, standingFrom)

	default:
		if !req.Take {
			return r.answerLocked(req.Court, h, tabKey, devKey), nil
		}
		h.AddDisplaced(h.HolderKey)
		h.RemoveDisplaced(tabKey)
		standingFrom := h.StandingFrom
		if h.DeviceKey != devKey {
			h.AddDisplacedDevice(h.DeviceKey)
			h.RemoveDisplacedDevice(devKey)
			standingFrom = clampTakenAt(req.TakenAt, now)
		}
		h = holdBy(h, tabKey, devKey, device, req.PageID, nowMs, standingFrom)
	}
	next[req.Court] = h
	changed = append(changed, req.Court)
	for court, other := range next {
		if court != req.Court && other.Held() && other.HolderKey == tabKey && !slices.Contains(req.Keep, court) {
			next[court] = freed(other)
			changed = append(changed, court)
		}
	}
	for court, g := range r.grace {
		if court != req.Court && g.hold.HolderKey == tabKey && !slices.Contains(req.Keep, court) {
			changed = append(changed, court)
		}
	}
	if err := r.saveLocked(next, listed); err != nil {
		return CourtControlAnswer{}, err
	}
	for _, court := range changed {
		r.stopGraceLocked(court)
		delete(r.lastActive, court)
	}
	r.lastActive[req.Court] = nowMs
	r.announceLocked(changed)
	return r.answerLocked(req.Court, r.holds[req.Court], tabKey, devKey), nil
}

// holdBy is h held by the tab tabKey of the tablet devKey from nowMs, its
// displaced lists as given.
func holdBy(h state.CourtHold, tabKey, devKey, device, pageID string, nowMs, standingFrom int64) state.CourtHold {
	h.HolderKey, h.DeviceKey, h.Device = tabKey, devKey, device
	h.Since, h.StandingFrom, h.PageID = nowMs, standingFrom, pageID
	return h
}

// freed is h with nobody holding it; its displaced lists stay.
func freed(h state.CourtHold) state.CourtHold {
	return state.CourtHold{Displaced: h.Displaced, DisplacedDevices: h.DisplacedDevices}
}

// Release gives the shiaijo back only when the tab holds it AND names the
// current page load, so a reload's late release never frees the new page's
// claim. Anything else changes nothing and answers the control as a claim
// sees it (viewLocked): during a grace, the hold given back, with releasedAt.
// A shiaijo given back is free in the file at once, but kept for its tablet
// for courtReleaseGrace (startGraceLocked): nothing is announced until the
// grace ends, since until then every other console still sees it run from
// that device. The answer is the shiaijo as the file has it: free.
func (r *courtControlRegistry) Release(court, tabID, pageID string) (CourtControlAnswer, error) {
	if !state.IsClientUUID(tabID) || !state.IsClientUUID(pageID) {
		return CourtControlAnswer{}, errCourtControlBadRequest
	}
	listed, err := r.checkCourt(court)
	if err != nil {
		return CourtControlAnswer{}, err
	}
	tabKey := state.CourtControlKey(tabID)
	r.mu.Lock()
	defer r.mu.Unlock()
	h := r.holds[court]
	if !h.Held() || h.HolderKey != tabKey || h.PageID != pageID {
		return r.answerLocked(court, r.viewLocked(court), tabKey, ""), nil
	}
	next := r.copyLocked()
	next[court] = freed(h)
	if err := r.saveLocked(next, listed); err != nil {
		return CourtControlAnswer{}, err
	}
	r.startGraceLocked(court, h)
	return r.answerLocked(court, r.holds[court], tabKey, ""), nil
}

// standingLocked is the court's standing of a write from the tablet devKey
// stamped at stamp: the tablet holds the shiaijo (through whichever of its
// tabs) and the write was made at or after the standing's start. So results
// a tablet queued before another tablet took the control never gain it
// later, and a tablet's own tabs share it. Caller holds r.mu.
func (r *courtControlRegistry) standingLocked(court, devKey string, stamp int64) bool {
	h := r.holds[court]
	return h.Held() && devKey != "" && h.DeviceKey == devKey && stamp >= h.StandingFrom
}

// WriteStanding reports whether a court console's write has the court's
// standing. The score and decision doors call it for a write that carries a
// court control (judgeCourtStanding, write_tag.go). Ids that are not a
// console's, or a shiaijo the tournament does not list, have no standing. A
// shiaijo nobody holds, from a tab and a tablet on neither displaced list (a
// console opened offline), is claimed by the write with standingFrom 0 when
// the tab holds no other shiaijo, as the claim route would claim it; its
// page load and device word arrive with the console's next claim. When that
// tab already holds another shiaijo, WriteStanding answers true without
// recording a hold, so no orphan hold is left behind. One that another tab
// of the SAME tablet holds has the standing and takes nothing. One that
// another tablet holds is ordinary. During a grace (Release) the shiaijo is
// the releasing tablet's: its write has the standing from the hold's own
// start and takes it back as a claim would (unless the tab holds another
// shiaijo or is on the displaced list), and another tablet's write is
// ordinary and takes nothing. A write never releases another shiaijo its tab
// holds: it is no switch of the console.
func (r *courtControlRegistry) WriteStanding(court, tabID, deviceID string, stamp int64) (bool, error) {
	if !state.IsClientUUID(tabID) || !state.IsClientUUID(deviceID) {
		return false, nil
	}
	listed, err := r.checkCourt(court)
	if errors.Is(err, errUnknownCourt) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	tabKey, devKey := state.CourtControlKey(tabID), state.CourtControlKey(deviceID)
	r.mu.Lock()
	defer r.mu.Unlock()
	if g := r.grace[court]; g != nil {
		return r.graceWriteStandingLocked(court, g.hold, tabKey, devKey, stamp, listed)
	}
	h := r.holds[court]
	if !h.Held() {
		if h.IsDisplaced(tabKey) || h.IsDisplacedDevice(devKey) {
			return false, nil
		}
		if r.tabHoldsOtherLocked(tabKey, court) {
			return true, nil
		}
		nowMs := r.now().UnixMilli()
		next := r.copyLocked()
		next[court] = holdBy(h, tabKey, devKey, state.CourtDeviceGeneric, "", nowMs, 0)
		if err := r.saveLocked(next, listed); err != nil {
			return false, err
		}
		r.lastActive[court] = nowMs
		r.announceLocked([]string{court})
	}
	return r.standingLocked(court, devKey, stamp), nil
}

// graceWriteStandingLocked is WriteStanding during the shiaijo's grace, gh
// the hold its tablet gave back. Caller holds r.mu.
func (r *courtControlRegistry) graceWriteStandingLocked(court string, gh state.CourtHold, tabKey, devKey string, stamp int64, listed []string) (bool, error) {
	if devKey == "" || gh.DeviceKey != devKey {
		return false, nil
	}
	standing := stamp >= gh.StandingFrom
	if gh.IsDisplaced(tabKey) || r.tabHoldsOtherLocked(tabKey, court) {
		return standing, nil
	}
	nowMs := r.now().UnixMilli()
	since := nowMs
	if gh.HolderKey == tabKey {
		since = gh.Since
	}
	next := r.copyLocked()
	next[court] = holdBy(gh, tabKey, devKey, gh.Device, "", since, gh.StandingFrom)
	if err := r.saveLocked(next, listed); err != nil {
		return false, err
	}
	r.stopGraceLocked(court)
	r.lastActive[court] = nowMs
	r.announceLocked([]string{court})
	return standing, nil
}

// tabHoldsOtherLocked reports whether tabKey already holds a shiaijo other than
// court. Caller holds r.mu.
func (r *courtControlRegistry) tabHoldsOtherLocked(tabKey, court string) bool {
	for c, h := range r.holds {
		if c != court && h.Held() && h.HolderKey == tabKey {
			return true
		}
	}
	return false
}

// Clear frees every shiaijo and forgets both displaced lists, on disk and in
// memory: the whole tournament was replaced, and its shiaijo letters repeat
// (A to P), so a new tournament's shiaijo A would otherwise open as run from
// another device on every console. A live tablet loses nothing: its next
// heartbeat is a claim, which takes a free shiaijo at once.
func (r *courtControlRegistry) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.store.ClearCourtControl(); err != nil {
		return err
	}
	var held []string
	for court, h := range r.holds {
		if h.Held() {
			held = append(held, court)
		}
	}
	// A grace ends with no wait: the tournament it was kept in is gone.
	for court, g := range r.grace {
		g.timer.Stop()
		held = append(held, court)
	}
	r.grace = map[string]*courtGrace{}
	r.holds = map[string]state.CourtHold{}
	r.lastActive = map[string]int64{}
	r.announceLocked(held)
	return nil
}

// copyLocked is a deep copy of the holds, changed and saved before it
// replaces them, so a save that fails changes nothing. Caller holds r.mu.
func (r *courtControlRegistry) copyLocked() map[string]state.CourtHold {
	return state.CourtControl{Courts: r.holds}.Copy().Courts
}

// saveLocked writes next and, once it is on disk, makes it the registry's:
// the store keeps only the listed shiaijo and the entries that still hold or
// remember someone, and so does the registry. Caller holds r.mu.
func (r *courtControlRegistry) saveLocked(next map[string]state.CourtHold, listed []string) error {
	if err := r.store.SaveCourtControl(state.CourtControl{Courts: next}, listed); err != nil {
		return fmt.Errorf("save %s: %w", state.CourtControlFilename, err)
	}
	for court, h := range next {
		if !slices.Contains(listed, court) || (!h.Held() && len(h.Displaced) == 0 && len(h.DisplacedDevices) == 0) {
			delete(next, court)
		}
	}
	r.holds = next
	return nil
}

// announceLocked broadcasts court_control_updated for each shiaijo whose
// holder changed, under r.mu so the events leave in the order of the
// changes. Caller holds r.mu.
func (r *courtControlRegistry) announceLocked(courts []string) {
	if r.hub == nil {
		return
	}
	slices.Sort(courts)
	for _, court := range courts {
		h := r.holds[court]
		r.hub.Broadcast(EventCourtControlUpdated, courtControlEvent{
			Court: court, HolderKey: h.HolderKey, Device: h.Device, Since: h.Since,
		})
	}
}

// clearCourtControlAfterReplace frees every shiaijo after the whole
// tournament was replaced (what names the replacement for the log). A
// failure is logged at WARN and never fails the replacement, which already
// landed: the old holders then stay until someone taps Take control.
func clearCourtControlAfterReplace(courts CourtControls, what string) {
	if courts == nil {
		return
	}
	if err := courts.Clear(); err != nil {
		slog.Warn("mobile-app: who runs each shiaijo was not cleared after the tournament was replaced", "replacement", what, "err", err)
	}
}

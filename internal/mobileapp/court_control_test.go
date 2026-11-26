package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Court control ids: lowercase UUID v4s, as a court console mints them.
const (
	ccTab1    = "11111111-1111-4111-8111-111111111111"
	ccTab2    = "22222222-2222-4222-8222-222222222222"
	ccTab3    = "33333333-3333-4333-8333-333333333333"
	ccTablet1 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	ccTablet2 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	ccPage1   = "c1c1c1c1-c1c1-4c1c-8c1c-c1c1c1c1c1c1"
	ccPage2   = "c2c2c2c2-c2c2-4c2c-8c2c-c2c2c2c2c2c2"
	ccPage3   = "c3c3c3c3-c3c3-4c3c-8c3c-c3c3c3c3c3c3"
)

// courtEventRecorder keeps every broadcast with its payload.
type courtEventRecorder struct {
	events []courtControlEvent
	others []EventType
}

func (b *courtEventRecorder) Broadcast(t EventType, data any) {
	if t == EventCourtControlUpdated {
		b.events = append(b.events, data.(courtControlEvent))
		return
	}
	b.others = append(b.others, t)
}

// courtRig is a registry over a real store whose tournament lists shiaijo A
// and B, on a clock the test moves.
type courtRig struct {
	dir   string
	store *state.Store
	reg   *courtControlRegistry
	rec   *courtEventRecorder
	clock time.Time
}

func newCourtRig(t *testing.T) *courtRig {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Court Taikai", Date: "01-06-2026", Venue: "Dojo", Courts: []string{"A", "B"},
		Password: "main-pw", Mode: state.TournamentModeOfficiated,
	}))
	rig := &courtRig{dir: dir, store: store, rec: &courtEventRecorder{}, clock: time.UnixMilli(1_800_000_000_000)}
	rig.reg = rig.open()
	return rig
}

// open is a new registry over the rig's store, as after a restart.
func (rig *courtRig) open() *courtControlRegistry {
	reg := newCourtControlRegistry(rig.store, rig.rec)
	reg.now = func() time.Time { return rig.clock }
	return reg
}

func (rig *courtRig) claim(t *testing.T, court, tab, tablet, page string, take bool, takenAt int64) CourtControlAnswer {
	t.Helper()
	a, err := rig.reg.Claim(CourtClaim{Court: court, TabID: tab, DeviceID: tablet, PageID: page, Device: "ipad", Take: take, TakenAt: takenAt})
	require.NoError(t, err)
	return a
}

func (rig *courtRig) stored(t *testing.T, court string) state.CourtHold {
	t.Helper()
	return rig.store.LoadCourtControl().Courts[court]
}

func (rig *courtRig) standing(t *testing.T, court, tab, tablet string, stamp int64) bool {
	t.Helper()
	ok, err := rig.reg.WriteStanding(court, tab, tablet, stamp)
	require.NoError(t, err)
	return ok
}

func TestCourtControl_AFreeShiaijoIsTakenAtOnceAndAHeartbeatSavesNothing(t *testing.T) {
	rig := newCourtRig(t)
	a := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, a.YouHold)
	assert.True(t, a.SameDevice)
	assert.False(t, a.YouWereDisplaced)
	assert.Equal(t, state.CourtControlKey(ccTab1), a.HolderKey)
	assert.Equal(t, "ipad", a.Device)
	assert.Equal(t, rig.clock.UnixMilli(), a.Since)
	require.Len(t, rig.rec.events, 1, "taking a free shiaijo is announced")
	assert.Equal(t, courtControlEvent{Court: "A", HolderKey: a.HolderKey, Device: "ipad", Since: a.Since}, rig.rec.events[0])

	h := rig.stored(t, "A")
	assert.Equal(t, state.CourtControlKey(ccTablet1), h.DeviceKey)
	assert.Zero(t, h.StandingFrom)
	assert.Equal(t, ccPage1, h.PageID)
	raw, err := json.Marshal(rig.store.LoadCourtControl())
	require.NoError(t, err)
	assert.NotContains(t, string(raw), ccTab1, "the raw tab id is never stored")
	assert.NotContains(t, string(raw), ccTablet1, "the raw tablet id is never stored")

	path := filepath.Join(rig.dir, state.CourtControlFilename)
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	rig.clock = rig.clock.Add(30 * time.Second)
	beat := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, beat.YouHold)
	assert.Equal(t, rig.clock.UnixMilli(), beat.LastActiveAt, "a heartbeat refreshes the last activity")
	assert.Equal(t, a.Since, beat.Since)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, before, after, "a heartbeat saves nothing")
	assert.Len(t, rig.rec.events, 1, "a heartbeat announces nothing")

	// A reload is a new page load: saved, so a release naming it frees the
	// shiaijo after a restart too.
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage2, false, 0)
	assert.Equal(t, ccPage2, rig.stored(t, "A").PageID)
	assert.Len(t, rig.rec.events, 1, "the holder did not change")
}

func TestCourtControl_AnotherTabsClaimNeverTakesItByItself(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)

	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.False(t, other.YouHold)
	assert.False(t, other.SameDevice)
	assert.False(t, other.YouWereDisplaced)
	assert.Equal(t, state.CourtControlKey(ccTab1), other.HolderKey)

	sameTablet := rig.claim(t, "A", ccTab2, ccTablet1, ccPage2, false, 0)
	assert.False(t, sameTablet.YouHold)
	assert.True(t, sameTablet.SameDevice, "another tab on this tablet")
	assert.Equal(t, state.CourtControlKey(ccTab1), rig.stored(t, "A").HolderKey)
	assert.Len(t, rig.rec.events, 1)
}

func TestCourtControl_AnotherTabletsTakeMovesTheStanding(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	early := rig.clock.UnixMilli() - 60_000
	assert.True(t, rig.standing(t, "A", ccTab1, ccTablet1, early), "a free shiaijo taken at once: every stamp of the tablet counts")

	rig.clock = rig.clock.Add(time.Minute)
	takenAt := rig.clock.UnixMilli() - 2_000 // the taking tab's own clock runs behind
	took := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, takenAt)
	assert.True(t, took.YouHold)
	require.Len(t, rig.rec.events, 2, "the take is announced")
	assert.Equal(t, state.CourtControlKey(ccTab2), rig.rec.events[1].HolderKey)

	h := rig.stored(t, "A")
	assert.Equal(t, takenAt, h.StandingFrom, "the standing starts at the take, on the taking tab's clock")
	assert.Equal(t, []string{state.CourtControlKey(ccTab1)}, h.Displaced)
	assert.Equal(t, []string{state.CourtControlKey(ccTablet1)}, h.DisplacedDevices)

	assert.True(t, rig.standing(t, "A", ccTab2, ccTablet2, takenAt))
	assert.False(t, rig.standing(t, "A", ccTab2, ccTablet2, takenAt-1), "made before the take")
	assert.False(t, rig.standing(t, "A", ccTab1, ccTablet1, rig.clock.UnixMilli()), "the tablet that lost it is ordinary")
	assert.False(t, rig.standing(t, "A", ccTab3, ccTablet1, rig.clock.UnixMilli()), "so is any other tab of it")

	lost := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.False(t, lost.YouHold)
	assert.True(t, lost.YouWereDisplaced, "another device took it")
	newTab := rig.claim(t, "A", ccTab3, ccTablet1, ccPage3, false, 0)
	assert.True(t, newTab.YouWereDisplaced, "a new tab of the tablet that lost it is told so too")
}

func TestCourtControl_ASameTabletTakeKeepsTheStanding(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	queued := rig.clock.UnixMilli() - 10_000

	rig.clock = rig.clock.Add(time.Minute)
	took := rig.claim(t, "A", ccTab2, ccTablet1, ccPage2, true, rig.clock.UnixMilli())
	assert.True(t, took.YouHold)
	assert.Equal(t, rig.clock.UnixMilli(), took.Since, "the bar moved")
	h := rig.stored(t, "A")
	assert.Equal(t, state.CourtControlKey(ccTab2), h.HolderKey)
	assert.Zero(t, h.StandingFrom, "the standing's start stays where it was")
	assert.Empty(t, h.DisplacedDevices, "no tablet lost the shiaijo")
	assert.Equal(t, []string{state.CourtControlKey(ccTab1)}, h.Displaced)

	assert.True(t, rig.standing(t, "A", ccTab1, ccTablet1, queued), "what the old tab queued is still the court's")
	assert.True(t, rig.standing(t, "A", ccTab2, ccTablet1, queued))

	old := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.False(t, old.YouHold)
	assert.True(t, old.SameDevice)
	assert.False(t, old.YouWereDisplaced, "while its tablet holds, the old tab is shown the grey bar, not the amber one")
}

func TestCourtControl_TakenAtIsClampedToTheServersNowPlusTheMargin(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, rig.clock.UnixMilli()+time.Hour.Milliseconds())
	assert.Equal(t, rig.clock.UnixMilli()+modifiedAtRefuseSkewMs, rig.stored(t, "A").StandingFrom)
}

func TestCourtControl_ReleaseNeedsTheHolderAndItsPageLoad(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	// The reload claims with a new page load; the old page's late release
	// must not free it.
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage2, false, 0)
	a, err := rig.reg.Release("A", ccTab1, ccPage1)
	require.NoError(t, err)
	assert.True(t, a.YouHold, "a reload's late release frees nothing")
	a, err = rig.reg.Release("A", ccTab2, ccPage2)
	require.NoError(t, err)
	assert.Equal(t, state.CourtControlKey(ccTab1), a.HolderKey, "another tab's release frees nothing")
	assert.Len(t, rig.rec.events, 1)

	a, err = rig.reg.Release("A", ccTab1, ccPage2)
	require.NoError(t, err)
	assert.False(t, a.YouHold)
	assert.Empty(t, a.HolderKey)
	require.Len(t, rig.rec.events, 2, "the release is announced")
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[1])
	_, kept := rig.store.LoadCourtControl().Courts["A"]
	assert.False(t, kept, "a free shiaijo that remembers nobody is dropped from the file")

	next := rig.claim(t, "A", ccTab2, ccTablet2, ccPage3, false, 0)
	assert.True(t, next.YouHold, "the next tablet takes it at once")
}

func TestCourtControl_ClaimingAnotherShiaijoReleasesTheFirst(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	b := rig.claim(t, "B", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, b.YouHold)
	assert.False(t, rig.stored(t, "A").Held(), "switching the console's shiaijo gives the first back")
	require.Len(t, rig.rec.events, 3)
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[1])
	assert.Equal(t, "B", rig.rec.events[2].Court)
}

func TestCourtControl_ATabletThatLostItTakesItBackOnlyByATake(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, rig.clock.UnixMilli())
	_, err := rig.reg.Release("A", ccTab2, ccPage2)
	require.NoError(t, err)
	require.False(t, rig.stored(t, "A").Held())

	again := rig.claim(t, "A", ccTab3, ccTablet1, ccPage3, false, 0)
	assert.False(t, again.YouHold, "no tab of the tablet that lost it takes it by itself")
	assert.True(t, again.YouWereDisplaced, "nobody runs it now, and this tablet lost it earlier")
	assert.False(t, rig.standing(t, "A", ccTab3, ccTablet1, rig.clock.UnixMilli()), "nor does its write claim it")
	assert.False(t, rig.stored(t, "A").Held())

	rig.clock = rig.clock.Add(time.Minute)
	takenAt := rig.clock.UnixMilli()
	took := rig.claim(t, "A", ccTab3, ccTablet1, ccPage3, true, takenAt)
	assert.True(t, took.YouHold)
	h := rig.stored(t, "A")
	assert.Equal(t, takenAt, h.StandingFrom, "its take counts only what it records from then on")
	assert.Empty(t, h.DisplacedDevices, "the taker's tablet leaves the list")
	assert.False(t, rig.standing(t, "A", ccTab1, ccTablet1, takenAt-1), "what it queued before it lost the shiaijo stays ordinary")
	assert.True(t, rig.standing(t, "A", ccTab1, ccTablet1, takenAt))
}

func TestCourtControl_ATabThatLostItToItsOwnTabletKeepsTheStandingWhole(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.claim(t, "A", ccTab2, ccTablet1, ccPage2, true, rig.clock.UnixMilli())
	_, err := rig.reg.Release("A", ccTab2, ccPage2)
	require.NoError(t, err)

	free := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.False(t, free.YouHold, "the displaced tab takes it back only by a take")
	assert.True(t, free.YouWereDisplaced)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, true, rig.clock.UnixMilli())
	assert.Zero(t, rig.stored(t, "A").StandingFrom, "the tablet never lost the shiaijo to another tablet")
}

func TestCourtControl_AWriteClaimsAFreeShiaijo(t *testing.T) {
	rig := newCourtRig(t)
	assert.True(t, rig.standing(t, "A", ccTab1, ccTablet1, 1), "a console opened offline: its write claims the free shiaijo")
	h := rig.stored(t, "A")
	assert.Equal(t, state.CourtControlKey(ccTab1), h.HolderKey)
	assert.Equal(t, state.CourtDeviceOther, h.Device)
	assert.Zero(t, h.StandingFrom)
	require.Len(t, rig.rec.events, 1)

	assert.True(t, rig.standing(t, "A", ccTab2, ccTablet1, 1), "another tab of the holding tablet has the standing")
	assert.Equal(t, state.CourtControlKey(ccTab1), rig.stored(t, "A").HolderKey, "and takes nothing")
	assert.False(t, rig.standing(t, "A", ccTab3, ccTablet2, 1), "another tablet's write is ordinary")
	assert.Len(t, rig.rec.events, 1)

	beat := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, beat.YouHold)
	assert.Equal(t, "ipad", rig.stored(t, "A").Device, "the console's next claim brings its device word")

	for name, ids := range map[string][2]string{
		"an unknown shiaijo":                  {"Z", ccTablet1},
		"a tablet id not minted by a console": {"A", "not-a-uuid"},
	} {
		ok, err := rig.reg.WriteStanding(ids[0], ccTab1, ids[1], 1)
		require.NoError(t, err, name)
		assert.False(t, ok, name)
	}
}

func TestCourtControl_ARestartKeepsWhoRunsEachShiaijo(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, rig.clock.UnixMilli())
	rig.reg = rig.open()
	a, err := rig.reg.Look("A")
	require.NoError(t, err)
	assert.Equal(t, state.CourtControlKey(ccTab2), a.HolderKey)
	assert.Equal(t, a.Since, a.LastActiveAt, "after a restart the last activity starts at the take")
	again := rig.claim(t, "A", ccTab3, ccTablet1, ccPage3, false, 0)
	assert.True(t, again.YouWereDisplaced, "the displaced tablet is remembered")
}

func TestCourtControl_ClearFreesEveryShiaijo(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, rig.clock.UnixMilli())
	require.NoError(t, rig.reg.Clear())
	_, err := os.Stat(filepath.Join(rig.dir, state.CourtControlFilename))
	assert.ErrorIs(t, err, os.ErrNotExist)
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[len(rig.rec.events)-1], "the freed shiaijo is announced")
	a := rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, a.YouHold, "the displaced lists are forgotten too")
}

func TestCourtControl_Refusals(t *testing.T) {
	rig := newCourtRig(t)
	_, err := rig.reg.Claim(CourtClaim{Court: "Z", TabID: ccTab1, DeviceID: ccTablet1, PageID: ccPage1})
	assert.ErrorIs(t, err, errUnknownCourt)
	_, err = rig.reg.Look("Z")
	assert.ErrorIs(t, err, errUnknownCourt)
	for name, req := range map[string]CourtClaim{
		"no tab id":           {Court: "A", DeviceID: ccTablet1, PageID: ccPage1},
		"no tablet id":        {Court: "A", TabID: ccTab1, PageID: ccPage1},
		"no page load":        {Court: "A", TabID: ccTab1, DeviceID: ccTablet1},
		"an upper-case id":    {Court: "A", TabID: "AAAAAAAA-AAAA-4AAA-8AAA-AAAAAAAAAAAA", DeviceID: ccTablet1, PageID: ccPage1},
		"a take with no time": {Court: "A", TabID: ccTab1, DeviceID: ccTablet1, PageID: ccPage1, Take: true},
	} {
		_, err := rig.reg.Claim(req)
		assert.ErrorIs(t, err, errCourtControlBadRequest, name)
	}
	_, err = rig.reg.Release("A", ccTab1, "")
	assert.ErrorIs(t, err, errCourtControlBadRequest)
	assert.Empty(t, rig.rec.events)

	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	got, err := rig.reg.Claim(CourtClaim{Court: "B", TabID: ccTab2, DeviceID: ccTablet2, PageID: ccPage2, Device: "<b>my tablet</b>"})
	require.NoError(t, err)
	assert.Equal(t, state.CourtDeviceOther, got.Device, "no free text reaches another screen")
}

// --- the routes ------------------------------------------------------------

func courtControlReq(t *testing.T, r http.Handler, method, court, password string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := jsonReq(method, "/api/courts/"+court+"/control", body)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeCourtAnswer(t *testing.T, w *httptest.ResponseRecorder) CourtControlAnswer {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var a CourtControlAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &a))
	return a
}

func TestCourtControlRoutes_ClaimTakeReleaseAndRefusals(t *testing.T) {
	rig := newBackupRig(t)
	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)
	claim := func(tab, tablet, page string, take bool) *httptest.ResponseRecorder {
		return courtControlReq(t, rig.r, http.MethodPut, "A", backupTestPassword, ccBody{
			"tabId": tab, "deviceId": tablet, "pageId": page, "take": take, "takenAt": time.Now().UnixMilli(), "device": "android-tablet",
		})
	}

	a := decodeCourtAnswer(t, claim(ccTab1, ccTablet1, ccPage1, false))
	assert.True(t, a.YouHold)
	assert.Equal(t, "android-tablet", a.Device)
	assert.True(t, waitForEvent(t, ch, EventCourtControlUpdated), "the claim is announced")

	look := decodeCourtAnswer(t, courtControlReq(t, rig.r, http.MethodGet, "A", backupTestPassword, nil))
	assert.Equal(t, a.HolderKey, look.HolderKey)
	assert.False(t, look.YouHold, "GET answers the control as anyone sees it")
	assert.NotContains(t, courtControlReq(t, rig.r, http.MethodGet, "A", backupTestPassword, nil).Body.String(), ccTab1)

	other := decodeCourtAnswer(t, claim(ccTab2, ccTablet2, ccPage2, false))
	assert.False(t, other.YouHold)
	took := decodeCourtAnswer(t, claim(ccTab2, ccTablet2, ccPage2, true))
	assert.True(t, took.YouHold)
	lost := decodeCourtAnswer(t, claim(ccTab1, ccTablet1, ccPage1, false))
	assert.True(t, lost.YouWereDisplaced)

	released := decodeCourtAnswer(t, courtControlReq(t, rig.r, http.MethodDelete, "A", backupTestPassword, ccBody{"tabId": ccTab2, "pageId": ccPage2}))
	assert.Empty(t, released.HolderKey)

	w := courtControlReq(t, rig.r, http.MethodPut, "Q", backupTestPassword, ccBody{"tabId": ccTab1, "deviceId": ccTablet1, "pageId": ccPage1})
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Equal(t, courtControlUnknownCode, responseCode(t, w))
	w = courtControlReq(t, rig.r, http.MethodPut, "A", backupTestPassword, ccBody{"tabId": "x"})
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Equal(t, courtControlBadRequestCode, responseCode(t, w))
	w = courtControlReq(t, rig.r, http.MethodDelete, "A", backupTestPassword, ccBody{"tabId": ccTab1})
	assert.Equal(t, http.StatusBadRequest, w.Code)

	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w := courtControlReq(t, rig.r, method, "A", "", ccBody{"tabId": ccTab1, "deviceId": ccTablet1, "pageId": ccPage1})
		assert.Equal(t, http.StatusUnauthorized, w.Code, "%s without the password", method)
	}
}

// ccBody is a request body.
type ccBody = map[string]any

// claimA has tab 1 of tablet 1 run shiaijo A through the route.
func claimA(t *testing.T, rig *backupTestRig) {
	t.Helper()
	a := decodeCourtAnswer(t, courtControlReq(t, rig.r, http.MethodPut, "A", backupTestPassword, ccBody{
		"tabId": ccTab1, "deviceId": ccTablet1, "pageId": ccPage1,
	}))
	require.True(t, a.YouHold)
}

func holderOfA(t *testing.T, rig *backupTestRig) string {
	t.Helper()
	return decodeCourtAnswer(t, courtControlReq(t, rig.r, http.MethodGet, "A", backupTestPassword, nil)).HolderKey
}

func TestCourtControl_ANewTournamentFreesEveryShiaijo(t *testing.T) {
	rig := newBackupRig(t)
	claimA(t, rig)
	w := httptest.NewRecorder()
	req := jsonReq(http.MethodPost, "/api/tournament", ccBody{
		"name": "Next Taikai", "date": "02-06-2026", "venue": "Dojo", "courts": []string{"A"}, "password": backupTestPassword,
	})
	req.Header.Set("X-Tournament-Password", backupTestPassword)
	rig.r.ServeHTTP(w, req)
	require.Less(t, w.Code, 300, w.Body.String())
	assert.Empty(t, holderOfA(t, rig), "the new tournament's shiaijo A is run by nobody")
}

func TestCourtControl_AnImportFreesEveryShiaijo(t *testing.T) {
	rig := newBackupRig(t)
	claimA(t, rig)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("files", "manifest.yaml")
	require.NoError(t, err)
	_, err = fw.Write([]byte("competitions:\n  - id: gamma\n    name: Gamma\n"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, "/api/tournament/import", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Tournament-Password", backupTestPassword)
	w := httptest.NewRecorder()
	rig.r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotContains(t, w.Body.String(), `"error"`, "the competition imported")
	assert.Empty(t, holderOfA(t, rig))
}

func TestCourtControl_AFullRestoreFreesEveryShiaijoAndACompetitionRestoreDoesNot(t *testing.T) {
	t.Run("a competition restore", func(t *testing.T) {
		rig := newBackupRig(t)
		archive := takeBackup(t, rig.r, "/api/competitions/alpha/backup")
		claimA(t, rig)
		w := restorePost(t, rig.r, "/api/tournament/restore", archive,
			map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{"alpha": {Action: state.RestoreReplace}})},
			backupTestPassword, backupTestPassword)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, state.CourtControlKey(ccTab1), holderOfA(t, rig), "one competition's restore leaves the control alone")
	})
	t.Run("a tournament restore", func(t *testing.T) {
		rig := newBackupRig(t)
		archive := takeBackup(t, rig.r, "/api/tournament/backup")
		claimA(t, rig)
		w := restorePost(t, rig.r, "/api/tournament/restore", archive,
			map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
				"alpha": {Action: state.RestoreReplace},
				"beta":  {Action: state.RestoreReplace},
			})},
			backupTestPassword, backupTestPassword)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Empty(t, holderOfA(t, rig), "the restored tournament's shiaijo A is run by nobody")
	})
}

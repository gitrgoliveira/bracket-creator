package mobileapp

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The grace after a release (courtReleaseGrace): a shiaijo its tab gives
// back stays with its tablet, so a reload over a slow venue network gets it
// back before any other console claims it.

func TestCourtControlGrace_AReloadTakesItBackAtOnceWithItsStandingWhole(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.clock = rig.clock.Add(time.Minute)
	takenAt := rig.clock.UnixMilli()
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, takenAt)
	before := rig.stored(t, "A")
	events := len(rig.rec.events)

	rig.release(t, "A", ccTab2, ccPage2)
	require.Len(t, rig.graces, 1)
	assert.Equal(t, courtReleaseGrace, rig.graces[0].after)
	assert.False(t, rig.stored(t, "A").Held(), "the file has it free at once")

	rig.clock = rig.clock.Add(20 * time.Second)
	back := rig.claim(t, "A", ccTab2, ccTablet2, ccPage3, false, 0)
	assert.True(t, back.YouHold, "the reloaded page (same tab, new page load) takes it back at once")
	h := rig.stored(t, "A")
	assert.Equal(t, before.StandingFrom, h.StandingFrom, "the reload keeps the tablet's standing whole")
	assert.Equal(t, before.Since, h.Since, "the same tab keeps its since")
	assert.Equal(t, ccPage3, h.PageID)
	assert.Equal(t, before.DisplacedDevices, h.DisplacedDevices, "the displaced tablet stays displaced")
	assert.True(t, rig.graces[0].stopped, "the reclaim ends the grace")

	rig.lapse()
	assert.True(t, rig.stored(t, "A").Held(), "a stopped grace frees nothing")
	for _, e := range rig.rec.events[events:] {
		assert.NotEmpty(t, e.HolderKey, "the shiaijo is never announced free")
	}
	_, err := rig.reg.Release("A", ccTab2, ccPage2)
	require.NoError(t, err)
	assert.True(t, rig.stored(t, "A").Held(), "the old page's late release frees nothing")
}

func TestCourtControlGrace_AnotherTabOfTheTabletTakesItBackAtOnce(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)

	rig.clock = rig.clock.Add(time.Second)
	other := rig.claim(t, "A", ccTab3, ccTablet1, ccPage3, false, 0)
	assert.True(t, other.YouHold, "another tab of the same tablet takes it at once")
	h := rig.stored(t, "A")
	assert.Equal(t, state.CourtControlKey(ccTab3), h.HolderKey)
	assert.Equal(t, rig.clock.UnixMilli(), h.Since, "a new tab holds it from now")
	assert.Zero(t, h.StandingFrom, "the tablet's standing stays whole")
	assert.Equal(t, state.CourtControlKey(ccTab3), rig.rec.events[len(rig.rec.events)-1].HolderKey, "the new holder is announced")
}

func TestCourtControlGrace_AnotherTabletsClaimIsAnsweredAsRunFromTheReleasingDevice(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.clock = rig.clock.Add(10 * time.Second)
	lastActive := rig.clock.UnixMilli()
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	since := rig.stored(t, "A").Since
	events := len(rig.rec.events)
	rig.release(t, "A", ccTab1, ccPage1)

	rig.clock = rig.clock.Add(5 * time.Second)
	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.False(t, other.YouHold, "another tablet's claim does not take it during the grace")
	assert.Equal(t, state.CourtControlKey(ccTab1), other.HolderKey)
	assert.Equal(t, "ipad", other.Device)
	assert.Equal(t, since, other.Since)
	assert.Equal(t, lastActive, other.LastActiveAt, "the releasing tablet's last activity")
	assert.Equal(t, lastActive, other.ReleasedAt, "when its tab gave it back")
	assert.False(t, other.SameDevice)
	assert.False(t, other.YouWereDisplaced)
	assert.False(t, rig.stored(t, "A").Held())
	look, err := rig.reg.Look("A")
	require.NoError(t, err)
	assert.Equal(t, state.CourtControlKey(ccTab1), look.HolderKey, "anyone is answered the same")
	assert.Len(t, rig.rec.events, events, "nothing is announced")

	back := rig.claim(t, "A", ccTab1, ccTablet1, ccPage2, false, 0)
	assert.True(t, back.YouHold, "the releasing tablet still gets it back")
	assert.Zero(t, back.ReleasedAt, "held again")
}

func TestCourtControlGrace_AnotherTabletsTakeTakesItAtOnce(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)

	takenAt := rig.clock.UnixMilli()
	took := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, takenAt)
	assert.True(t, took.YouHold, "Take control takes it at once")
	h := rig.stored(t, "A")
	assert.Equal(t, takenAt, h.StandingFrom, "as a take from a running holder: the standing starts at the take")
	assert.Contains(t, h.Displaced, state.CourtControlKey(ccTab1))
	assert.Contains(t, h.DisplacedDevices, state.CourtControlKey(ccTablet1))
	assert.True(t, rig.graces[0].stopped, "the take ends the grace")

	lost := rig.claim(t, "A", ccTab1, ccTablet1, ccPage2, false, 0)
	assert.False(t, lost.YouHold, "the reload comes back too late")
	assert.True(t, lost.YouWereDisplaced)
}

func TestCourtControlGrace_WhenTheGraceEndsTheShiaijoIsFreeAndAnnounced(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)
	events := len(rig.rec.events)

	rig.lapse()
	require.Len(t, rig.rec.events, events+1)
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[events], "the free shiaijo is announced")
	look, err := rig.reg.Look("A")
	require.NoError(t, err)
	assert.Empty(t, look.HolderKey)
	assert.Zero(t, look.LastActiveAt)

	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.True(t, other.YouHold, "another tablet claims it at once")
	assert.Zero(t, rig.stored(t, "A").StandingFrom)
}

func TestCourtControlGrace_ClearFreesItWithNoGrace(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)

	require.NoError(t, rig.reg.Clear())
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[len(rig.rec.events)-1], "the shiaijo is announced free at once")
	assert.True(t, rig.graces[0].stopped, "Clear ends the grace")
	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.True(t, other.YouHold, "another tablet claims it at once")
	events := len(rig.rec.events)
	rig.lapse()
	assert.Len(t, rig.rec.events, events, "the ended grace announces nothing")
	assert.True(t, rig.stored(t, "A").Held())
}

func TestCourtControlGrace_AWriteDuringTheGrace(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.clock = rig.clock.Add(time.Minute)
	takenAt := rig.clock.UnixMilli()
	rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, true, takenAt)
	rig.release(t, "A", ccTab2, ccPage2)
	events := len(rig.rec.events)

	assert.False(t, rig.standing(t, "A", ccTab3, ccTablet1, takenAt+5), "another tablet's write is ordinary")
	assert.False(t, rig.stored(t, "A").Held(), "and claims nothing")
	assert.Len(t, rig.rec.events, events)

	assert.False(t, rig.standing(t, "A", ccTab2, ccTablet2, takenAt-1), "the releasing tablet's write from before its take has no standing")
	assert.True(t, rig.stored(t, "A").Held(), "but its write takes the shiaijo back")
	h := rig.stored(t, "A")
	assert.Equal(t, state.CourtControlKey(ccTab2), h.HolderKey)
	assert.Equal(t, takenAt, h.StandingFrom, "with the standing as it was")
	assert.Empty(t, h.PageID, "the page load arrives with the console's next claim")
	assert.True(t, rig.standing(t, "A", ccTab2, ccTablet2, takenAt), "its write from the take on has the standing")
	assert.True(t, rig.graces[0].stopped, "the write ends the grace")
	_, err := rig.reg.Release("A", ccTab2, ccPage2)
	require.NoError(t, err)
	assert.True(t, rig.stored(t, "A").Held(), "the old page's release frees nothing")
}

func TestCourtControlGrace_AWriteFromATabHoldingAnotherShiaijoHasTheStandingAndTakesNothing(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)
	rig.claim(t, "B", ccTab3, ccTablet1, ccPage3, false, 0)

	assert.True(t, rig.standing(t, "A", ccTab3, ccTablet1, rig.clock.UnixMilli()), "a tab of the releasing tablet has the standing")
	assert.False(t, rig.stored(t, "A").Held(), "but leaves no hold behind")
	assert.False(t, rig.graces[0].stopped, "the grace runs on")
}

func TestCourtControlGrace_SwitchingShiaijoEndsTheTabsGrace(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)
	events := len(rig.rec.events)

	rig.claim(t, "B", ccTab1, ccTablet1, ccPage1, false, 0)
	assert.True(t, rig.graces[0].stopped, "the tab moved to B: A is not kept for it")
	require.Len(t, rig.rec.events, events+2)
	assert.Equal(t, courtControlEvent{Court: "A"}, rig.rec.events[events], "A is announced free")
	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.True(t, other.YouHold)
}

func TestCourtControlGrace_ARestartEndsTheGrace(t *testing.T) {
	rig := newCourtRig(t)
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)
	rig.reg = rig.open()
	other := rig.claim(t, "A", ccTab2, ccTablet2, ccPage2, false, 0)
	assert.True(t, other.YouHold, "the grace is in memory only: after a restart the shiaijo is free")
}

// The real timer: the grace ends on its own, racing claims (run with -race).
func TestCourtControlGrace_TheTimerEndsTheGraceUnderClaims(t *testing.T) {
	rig := newCourtRig(t)
	rig.reg.afterFunc = func(_ time.Duration, f func()) courtGraceTimer { return time.AfterFunc(time.Millisecond, f) }
	rig.reg.now = time.Now
	rig.claim(t, "A", ccTab1, ccTablet1, ccPage1, false, 0)
	rig.release(t, "A", ccTab1, ccPage1)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				_, err := rig.reg.Claim(CourtClaim{Court: "A", TabID: ccTab2, DeviceID: ccTablet2, PageID: ccPage2, Device: "ipad"})
				assert.NoError(t, err)
				_, err = rig.reg.Look("A")
				assert.NoError(t, err)
			}
		}()
	}
	wg.Wait()
	require.Eventually(t, func() bool {
		a, err := rig.reg.Claim(CourtClaim{Court: "A", TabID: ccTab2, DeviceID: ccTablet2, PageID: ccPage2, Device: "ipad"})
		return err == nil && a.YouHold
	}, 2*time.Second, 5*time.Millisecond, "once the grace ends another tablet claims it")
}

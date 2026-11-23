package mobileapp

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Readers that find the same stale token share one build.
func TestNameMaskCache_ConcurrentReadsShareOneBuild(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	cache := fx.pn.masks
	var builds atomic.Int32
	release := make(chan struct{})
	cache.build = func(compID string) (*nameMask, error) {
		builds.Add(1)
		<-release
		return failClosedMask(fx.comp), nil
	}

	const readers = 8
	var wg sync.WaitGroup
	masks := make([]*nameMask, readers)
	for i := 0; i < readers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, err := cache.get(fx.comp.ID)
			assert.NoError(t, err)
			masks[i] = m
		}(i)
	}
	require.Eventually(t, func() bool { return builds.Load() == 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond) // let the other readers reach the flight
	close(release)
	wg.Wait()

	assert.EqualValues(t, 1, builds.Load(), "one build for every concurrent reader")
	for _, m := range masks {
		assert.Same(t, masks[0], m)
	}
}

// A failed build is served for nameMaskRetryAfter and read again after it, and
// the answer in between fails closed.
func TestNameMaskCache_AFailedBuildIsKeptBrieflyThenRetried(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	cache := fx.pn.masks
	now := time.Now()
	cache.now = func() time.Time { return now }
	var builds int
	fail := errors.New("unreadable")
	cache.build = func(compID string) (*nameMask, error) {
		builds++
		return failClosedMask(fx.comp), fail
	}

	for i := 0; i < 3; i++ {
		m, err := cache.get(fx.comp.ID)
		require.ErrorIs(t, err, fail)
		require.NotNil(t, m, "a failed build still answers a mask that hides")
	}
	assert.Equal(t, 1, builds, "the failure is not read again at once")

	now = now.Add(nameMaskRetryAfter)
	_, err := cache.get(fx.comp.ID)
	require.ErrorIs(t, err, fail)
	assert.Equal(t, 2, builds, "after the wait the files are read again")
}

// A build that panics still releases the readers waiting on it.
func TestNameMaskCache_APanickingBuildReleasesItsWaiters(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	cache := fx.pn.masks
	entered := make(chan struct{})
	release := make(chan struct{})
	cache.build = func(compID string) (*nameMask, error) {
		close(entered)
		<-release
		panic("boom")
	}
	leader := make(chan any, 1)
	go func() {
		defer func() { leader <- recover() }()
		_, _ = cache.get(fx.comp.ID)
	}()
	<-entered

	type answer struct {
		m   *nameMask
		err error
	}
	waiter := make(chan answer, 1)
	go func() {
		m, err := cache.get(fx.comp.ID)
		waiter <- answer{m, err}
	}()
	time.Sleep(20 * time.Millisecond)
	close(release)

	assert.Equal(t, "boom", <-leader)
	select {
	case a := <-waiter:
		assert.Error(t, a.err)
		assert.NotNil(t, a.m, "the waiter gets a mask that hides")
	case <-time.After(time.Second):
		t.Fatal("a waiter was left hanging by a build that panicked")
	}
}

// The sweep for deleted competitions runs at most once a minute.
func TestNameMaskCache_SweepsDeletedCompetitionsAtMostOnceAMinute(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	cache := fx.pn.masks
	now := time.Now()
	cache.now = func() time.Time { return now }
	cache.entries["gone"] = nameMaskEntry{}

	_, err := cache.get(fx.comp.ID) // the first rebuild sweeps
	require.NoError(t, err)
	assert.NotContains(t, cache.entries, "gone")

	cache.entries["gone"] = nameMaskEntry{}
	cache.entries[fx.comp.ID] = nameMaskEntry{}
	_, err = cache.get(fx.comp.ID)
	require.NoError(t, err)
	assert.Contains(t, cache.entries, "gone", "no second sweep inside a minute")

	now = now.Add(nameMaskEvictEvery)
	cache.entries[fx.comp.ID] = nameMaskEntry{}
	_, err = cache.get(fx.comp.ID)
	require.NoError(t, err)
	assert.NotContains(t, cache.entries, "gone")
}

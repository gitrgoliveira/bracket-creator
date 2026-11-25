package mobileapp

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

// A write to any of the files a mask is built from makes the cached mask
// stale, so the next read builds again; a read with no write keeps it.
func TestNameMaskCache_AWriteToAnyMaskFileInvalidatesTheMask(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	cache := fx.pn.masks
	const cid = "teams-cache"
	tigers := helper.NewUUID4()
	teams := []domain.Player{{ID: tigers, Name: "Tigers", Dojo: "Dojo One"}}
	comp := &state.Competition{ID: cid, Name: "Teams", Kind: "team", TeamSize: 3, Format: state.CompFormatMixed, Status: state.CompStatusPools}
	require.NoError(t, fx.store.SaveCompetition(comp))
	require.NoError(t, fx.store.SaveParticipants(cid, teams))
	var builds int
	cache.build = func(string) (*nameMask, error) {
		builds++
		return failClosedMask(comp), nil
	}

	n := 0
	writes := map[string]func() error{
		"config.md": func() error { comp.Name = fmt.Sprintf("Teams %d", n); return fx.store.SaveCompetition(comp) },
		"participants.csv": func() error {
			return fx.store.SaveParticipants(cid, []domain.Player{{ID: tigers, Name: "Tigers", Dojo: fmt.Sprintf("Dojo %d", n)}})
		},
		"pools.csv": func() error {
			return fx.store.SavePools(cid, []helper.Pool{{PoolName: fmt.Sprintf("P%d", n), Players: teams}})
		},
		"bracket.json": func() error {
			return fx.store.SaveBracket(cid, &state.Bracket{Rounds: [][]state.BracketMatch{{{ID: fmt.Sprintf("M%d", n)}}}})
		},
		"team-members.yaml": func() error {
			_, err := fx.store.AddTeamMember(cid, tigers, fmt.Sprintf("Member %d", n))
			return err
		},
	}
	require.Len(t, writes, len(nameMaskFiles), "every file a mask is built from is exercised")

	_, err := cache.get(cid)
	require.NoError(t, err)
	_, err = cache.get(cid)
	require.NoError(t, err)
	require.Equal(t, 1, builds, "with no write the mask is built once")

	for _, file := range nameMaskFiles {
		write, ok := writes[file]
		require.True(t, ok, "%s has no write in this test", file)
		n++
		before := builds
		require.NoError(t, write(), file)
		_, err = cache.get(cid)
		require.NoError(t, err)
		assert.Equal(t, before+1, builds, "a write to %s builds the mask again", file)
		_, err = cache.get(cid)
		require.NoError(t, err)
		assert.Equal(t, before+1, builds, "the mask built after the %s write is kept", file)
	}
}

// A hidden draft's mask holds no draw numbers: a hidden name reads "?" as it
// does before the draw. Publishing writes config.md, so the next read builds
// the mask again with the draw's numbers; hiding it again takes them away.
func TestNameMaskCache_AHiddenDraftsMaskHasNoDrawNumbers(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	side := func() string {
		t.Helper()
		comp, err := f.store.LoadCompetition(f.comp.ID)
		require.NoError(t, err)
		v, err := f.pn.view(comp, audience{})
		require.NoError(t, err)
		return v.side("Alice Arai", pnAlice)
	}

	setDraft(t, &f, true)
	assert.Equal(t, maskedUnknown, side(), "a hidden draft shows no number from its draw")

	setDraft(t, &f, false)
	assert.Equal(t, "K1", side(), "the published draft shows the draw's number")

	setDraft(t, &f, true)
	assert.Equal(t, maskedUnknown, side(), "hidden again, the number goes")
}

// The mask judges a hidden draft from the competition alone, which holds only
// because no self-run read reaches the mask: a self-run tournament hides no
// names, so neither a public read nor a reveal key there builds one, even for
// a competition whose stored setting hides names and whose draw is stored
// hidden.
func TestNameMaskCache_NoSelfRunReadReachesTheMask(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesCompetition)
	setDraft(t, &f, true)
	makeSelfRun(t, &f)
	var builds atomic.Int32
	f.pn.masks.build = func(compID string) (*nameMask, error) {
		builds.Add(1)
		return failClosedMask(f.comp), nil
	}

	for _, hdr := range []map[string]string{
		nil,
		{revealKeyHeader: f.comp.ID + ":" + state.CompetitionRevealKey(f.comp)},
		{"X-Tournament-Password": "secret-pw"},
	} {
		aud, ok, w := judgeRequest(t, f.pn, hdr)
		require.True(t, ok, w.Body.String())
		v, err := f.pn.view(f.comp, aud)
		require.NoError(t, err)
		assert.False(t, v.namesHidden(), "self-run shows names")
		assert.False(t, v.drawHidden, "self-run shows the draw")
	}
	assert.Zero(t, builds.Load(), "no self-run read builds a mask")
}

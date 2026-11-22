package mobileapp

import (
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// setupVerifierTestStore creates an isolated state store backed by a
// temp dir. The cleanup tears down the temp dir at test end. Shared by
// every verifier test so we don't drag the middleware test harness into
// these unit tests.
func setupVerifierTestStore(t *testing.T) *state.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "verifier-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	return store
}

func TestFileVerifier_Mode(t *testing.T) {
	v := NewFileVerifier(setupVerifierTestStore(t))
	assert.Equal(t, "file", v.Mode())
	assert.True(t, v.ResetEnabled())
	assert.True(t, v.AllowsFileBootstrap())
	assert.True(t, v.EnforceEmptyStoredGuard())
}

func TestFileVerifier_Verify(t *testing.T) {
	store := setupVerifierTestStore(t)
	v := NewFileVerifier(store)

	t.Run("no tournament yet → false", func(t *testing.T) {
		ok, err := v.Verify("anything")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret123",
	}))

	t.Run("matching password → true", func(t *testing.T) {
		ok, err := v.Verify("secret123")
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("non-matching password → false", func(t *testing.T) {
		ok, err := v.Verify("wrong")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("empty presented against non-empty stored → false", func(t *testing.T) {
		ok, err := v.Verify("")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestBcryptVerifier_Mode(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("anything"), bcrypt.MinCost)
	require.NoError(t, err)
	v, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)
	assert.Equal(t, "locked", v.Mode())
	assert.False(t, v.ResetEnabled())
	// AllowsFileBootstrap is FALSE in locked mode: the env-var hash
	// IS the credential from request 1, and anonymous bootstrap on
	// an internet-exposed fresh deployment would let any network
	// client race-claim the initial tournament record with garbage
	// data. The SPA's CreateTournament form sends the env-var
	// password in X-Tournament-Password on locked-mode bootstraps;
	// the middleware accepts the POST when that header verifies.
	assert.False(t, v.AllowsFileBootstrap())
	assert.False(t, v.EnforceEmptyStoredGuard())
	assert.True(t, v.RedactStoredPassword())
}

func TestNewBcryptVerifier_EmptyHash(t *testing.T) {
	_, err := NewBcryptVerifier("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestNewBcryptVerifier_InvalidHash(t *testing.T) {
	_, err := NewBcryptVerifier("not-a-bcrypt-hash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid bcrypt hash")
}

func TestBcryptVerifier_Verify(t *testing.T) {
	plain := "mySecret"
	// Use MinCost to keep the test fast, cost choice doesn't affect the
	// verification contract.
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	require.NoError(t, err)

	v, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	t.Run("matching password → true", func(t *testing.T) {
		ok, err := v.Verify(plain)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("wrong password → false (no error)", func(t *testing.T) {
		ok, err := v.Verify("nope")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	t.Run("empty presented → false (short-circuit)", func(t *testing.T) {
		ok, err := v.Verify("")
		require.NoError(t, err)
		assert.False(t, ok)
	})

	// Oversized header: bcrypt.CompareHashAndPassword returns
	// ErrPasswordTooLong for inputs > 72 bytes. Pre-fix this propagated
	// as an error and the middleware emitted a 500; the verifier now
	// pre-checks length and short-circuits to (false, nil) so an
	// unauth'd client sending a giant header gets the normal 401 path.
	// This also closes a side-channel where the operator could tell
	// locked mode apart from file mode by submitting a 100-byte header
	// (locked → 500, file → 401).
	t.Run("oversize presented (>72 bytes) → false (no 500)", func(t *testing.T) {
		oversize := strings.Repeat("x", 100)
		ok, err := v.Verify(oversize)
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

// countingVerifier counts Verify calls and can hold them until released.
type countingVerifier struct {
	PasswordVerifier
	calls   atomic.Int32
	right   string
	err     error
	release chan struct{}
}

func (v *countingVerifier) Verify(p string) (bool, error) {
	v.calls.Add(1)
	if v.release != nil {
		<-v.release
	}
	if v.err != nil {
		return false, v.err
	}
	return p == v.right, nil
}

func TestCachedVerifier(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	inner := &countingVerifier{right: "right"}
	v := newCachedVerifier(inner, func() time.Time { return now })

	for i := 0; i < 3; i++ {
		ok, err := v.Verify("right")
		require.NoError(t, err)
		assert.True(t, ok)
	}
	assert.EqualValues(t, 1, inner.calls.Load(), "the correct password skips bcrypt once seen")

	for i := 0; i < 3; i++ {
		ok, err := v.Verify("wrong")
		require.NoError(t, err)
		assert.False(t, ok)
	}
	assert.EqualValues(t, 2, inner.calls.Load(), "a wrong password costs one check while cached")

	now = now.Add(wrongPasswordCacheTTL)
	ok, err := v.Verify("wrong")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.EqualValues(t, 3, inner.calls.Load(), "the wrong-password cache lasts 60 s")

	inner.err = errors.New("boom")
	_, err = v.Verify("other")
	require.Error(t, err)
	_, err = v.Verify("other")
	require.Error(t, err)
	assert.EqualValues(t, 5, inner.calls.Load(), "an error is never cached")

	// The mode and policy methods pass through.
	assert.Same(t, inner, v.PasswordVerifier)
}

func TestCachedVerifier_ConcurrentSharesOneCheck(t *testing.T) {
	inner := &countingVerifier{right: "right", release: make(chan struct{})}
	v := newCachedVerifier(inner, time.Now)
	var wg sync.WaitGroup
	results := make([]bool, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], _ = v.Verify("wrong")
		}(i)
	}
	require.Eventually(t, func() bool { return inner.calls.Load() == 1 }, time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	close(inner.release)
	wg.Wait()
	assert.EqualValues(t, 1, inner.calls.Load())
	for _, r := range results {
		assert.False(t, r)
	}
}

// panickingVerifier panics on its first Verify, after being released.
type panickingVerifier struct {
	PasswordVerifier
	calls   atomic.Int32
	release chan struct{}
}

func (v *panickingVerifier) Verify(p string) (bool, error) {
	if v.calls.Add(1) == 1 {
		<-v.release
		panic("verifier blew up")
	}
	return p == "right", nil
}

func TestCachedVerifier_APanicReleasesWaitersAndLaterChecks(t *testing.T) {
	inner := &panickingVerifier{release: make(chan struct{})}
	v := newCachedVerifier(inner, time.Now)

	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_, _ = v.Verify("right")
	}()
	require.Eventually(t, func() bool { return inner.calls.Load() == 1 }, time.Second, time.Millisecond)

	waiterErr := make(chan error, 1)
	go func() {
		_, err := v.Verify("right")
		waiterErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	close(inner.release)

	select {
	case p := <-panicked:
		assert.Equal(t, "verifier blew up", p, "the panic still reaches the caller that ran the check")
	case <-time.After(2 * time.Second):
		t.Fatal("the panicking check never returned")
	}
	select {
	case err := <-waiterErr:
		assert.ErrorIs(t, err, errVerifierPanicked, "a waiter is released with an error")
	case <-time.After(2 * time.Second):
		t.Fatal("a caller waiting on the panicked check is stuck")
	}

	later := make(chan bool, 1)
	go func() {
		ok, _ := v.Verify("right")
		later <- ok
	}()
	select {
	case ok := <-later:
		assert.True(t, ok, "a later check of the same password runs again")
	case <-time.After(2 * time.Second):
		t.Fatal("a later check of the same password is stuck behind the panicked one")
	}
}

func TestCachedVerifier_WrongCacheBounded(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	v := newCachedVerifier(&countingVerifier{right: "right"}, func() time.Time { return now })
	for i := 0; i < wrongPasswordCacheMax+10; i++ {
		_, err := v.Verify("wrong-" + strings.Repeat("x", i%7) + string(rune(i)))
		require.NoError(t, err)
	}
	assert.LessOrEqual(t, len(v.bad), wrongPasswordCacheMax)
	now = now.Add(wrongPasswordCacheTTL)
	_, err := v.Verify("fresh-wrong")
	require.NoError(t, err)
	assert.Len(t, v.bad, 1, "expired entries are dropped when the cache is full")
}

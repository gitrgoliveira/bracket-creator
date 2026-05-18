package mobileapp

import (
	"os"
	"testing"

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
	assert.False(t, v.AllowsFileBootstrap())
	assert.False(t, v.EnforceEmptyStoredGuard())
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
	// Use MinCost to keep the test fast — cost choice doesn't affect the
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
}

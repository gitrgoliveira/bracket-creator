package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

// setAdminPassword writes an admin password onto the stored tournament so
// the file-mode elevated verifier has something to compare against. Creates
// the tournament record if needed.
func setAdminPassword(t *testing.T, store *state.Store, admin string) {
	t.Helper()
	_, err := store.UpdateTournamentChanged(&state.Tournament{}, func(current, desired *state.Tournament) error {
		if current != nil {
			*desired = *current
		} else {
			desired.Name = "Test"
			desired.Password = "main"
		}
		desired.AdminPassword = admin
		return nil
	})
	require.NoError(t, err)
}

func TestFileElevatedVerifier_GateInactiveWhenUnset(t *testing.T) {
	store := setupVerifierTestStore(t)
	v := NewFileElevatedVerifier(store)
	assert.Equal(t, "file", v.Mode())
	// No tournament / no admin password → gate inactive, not configured.
	assert.False(t, v.GateActive())
	assert.False(t, v.Configured())
}

func TestFileElevatedVerifier_SetActivatesGate(t *testing.T) {
	store := setupVerifierTestStore(t)
	v := NewFileElevatedVerifier(store)
	setAdminPassword(t, store, "secret-admin")

	assert.True(t, v.GateActive())
	assert.True(t, v.Configured())

	t.Run("matching → true", func(t *testing.T) {
		ok, err := v.Verify("secret-admin")
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("wrong → false", func(t *testing.T) {
		ok, err := v.Verify("nope")
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("empty presented against non-empty stored → false", func(t *testing.T) {
		ok, err := v.Verify("")
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestBcryptElevatedVerifier(t *testing.T) {
	plain := "elevated-secret"
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	require.NoError(t, err)
	v, err := NewBcryptElevatedVerifier(string(hash))
	require.NoError(t, err)

	assert.Equal(t, "locked", v.Mode())
	assert.True(t, v.GateActive())
	assert.True(t, v.Configured())

	t.Run("matching → true", func(t *testing.T) {
		ok, err := v.Verify(plain)
		require.NoError(t, err)
		assert.True(t, ok)
	})
	t.Run("wrong → false", func(t *testing.T) {
		ok, err := v.Verify("nope")
		require.NoError(t, err)
		assert.False(t, ok)
	})
	t.Run("empty → false (short-circuit)", func(t *testing.T) {
		ok, err := v.Verify("")
		require.NoError(t, err)
		assert.False(t, ok)
	})
	// Hardening parity with bcryptPasswordVerifier: oversized input must not
	// 500 (which would be a locked-vs-file differential oracle).
	t.Run("oversize (>72 bytes) → false (no error)", func(t *testing.T) {
		ok, err := v.Verify(strings.Repeat("x", 100))
		require.NoError(t, err)
		assert.False(t, ok)
	})
}

func TestNewBcryptElevatedVerifier_EmptyAndInvalid(t *testing.T) {
	_, err := NewBcryptElevatedVerifier("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")

	_, err = NewBcryptElevatedVerifier("not-a-bcrypt-hash")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "valid bcrypt hash")
}

func TestLockedUnconfiguredElevatedVerifier(t *testing.T) {
	v := NewLockedUnconfiguredElevatedVerifier()
	assert.Equal(t, "locked", v.Mode())
	// Gate active (fail-closed) but no credential → middleware must 503.
	assert.True(t, v.GateActive())
	assert.False(t, v.Configured())
	ok, err := v.Verify("anything")
	require.NoError(t, err)
	assert.False(t, ok)
}

// --- middleware behavior ---

// elevatedTestRouter mounts a single gated GET behind RequireElevatedPassword
// so the three outcome paths can be exercised without the full admin stack.
func elevatedTestRouter(ev ElevatedVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/gated", RequireElevatedPassword(ev), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func doGated(r *gin.Engine, adminHeader string, setHeader bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/gated", nil)
	if setHeader {
		req.Header.Set("X-Admin-Password", adminHeader)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestRequireElevatedPassword_GateInactivePassesThrough(t *testing.T) {
	store := setupVerifierTestStore(t)
	// file mode, no admin password → gate inactive → request passes with no header.
	r := elevatedTestRouter(NewFileElevatedVerifier(store))
	w := doGated(r, "", false)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRequireElevatedPassword_FileMode(t *testing.T) {
	store := setupVerifierTestStore(t)
	setAdminPassword(t, store, "the-admin-pw")
	r := elevatedTestRouter(NewFileElevatedVerifier(store))

	t.Run("missing header → 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, doGated(r, "", false).Code)
	})
	t.Run("wrong header → 401", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, doGated(r, "wrong", true).Code)
	})
	t.Run("correct header → 200", func(t *testing.T) {
		assert.Equal(t, http.StatusOK, doGated(r, "the-admin-pw", true).Code)
	})
}

func TestRequireElevatedPassword_LockedUnconfigured503(t *testing.T) {
	r := elevatedTestRouter(NewLockedUnconfiguredElevatedVerifier())
	// Even a (wrong or right-looking) header can't help — no credential exists.
	w := doGated(r, "anything", true)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "not configured")
}

func TestRequireElevatedPassword_LockedConfigured(t *testing.T) {
	plain := "locked-elevated"
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.MinCost)
	require.NoError(t, err)
	ev, err := NewBcryptElevatedVerifier(string(hash))
	require.NoError(t, err)
	r := elevatedTestRouter(ev)

	assert.Equal(t, http.StatusUnauthorized, doGated(r, "wrong", true).Code)
	assert.Equal(t, http.StatusOK, doGated(r, plain, true).Code)
}

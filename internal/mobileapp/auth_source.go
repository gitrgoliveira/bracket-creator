package mobileapp

import (
	"errors"
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"golang.org/x/crypto/bcrypt"
)

// PasswordVerifier abstracts the admin-auth source so the same middleware
// can serve both:
//
//   - "file" mode: the password is stored plaintext in tournament.md and
//     verified by exact-string match. This is the default and matches the
//     pre-existing behavior.
//   - "locked" mode: the password is supplied out-of-band as a bcrypt hash
//     in the TOURNAMENT_PASSWORD_HASH environment variable and verified
//     via bcrypt.CompareHashAndPassword. The stored tournament.md
//     password is irrelevant in this mode.
//
// Locked mode is selected via the --lock-password CLI flag (or LOCK_PASSWORD=true
// env var) on the mobile-app command and is the recommended setting for
// any internet-exposed deployment.
type PasswordVerifier interface {
	// Verify reports whether the supplied X-Tournament-Password header
	// value authenticates as admin. Implementations decide whether to
	// consult the stored tournament record or an out-of-band secret.
	Verify(presented string) (bool, error)

	// Mode returns "file" or "locked" so the public GET /api/auth-config
	// endpoint can surface the active mode to the SPA. The SPA uses this
	// to hide the "Forgot password?" link and the reset form when locked.
	Mode() string

	// ResetEnabled reports whether the POST /api/tournament/reset endpoint
	// should accept resets. file mode = true; locked mode = false.
	ResetEnabled() bool

	// AllowsFileBootstrap reports whether the middleware should let an
	// unauthenticated POST/PUT to /api/tournament through when no
	// tournament has been bootstrapped yet. file mode = true (preserves
	// the historical first-run UX where the operator submits the
	// CreateTournament form before having anything to authenticate
	// against). locked mode = false (the env-var hash IS the auth
	// source from the first request — there is no chicken-and-egg).
	AllowsFileBootstrap() bool

	// EnforceEmptyStoredGuard reports whether the middleware should
	// 403 when the stored tournament has an empty Password field. This
	// is defense-in-depth against the F4 sentinel-into-auth scenario in
	// file mode (an empty stored password would let `password != t.Password`
	// pass vacuously for an empty header). In locked mode the stored
	// password is irrelevant, so the guard would 403 every request — it
	// must be disabled.
	EnforceEmptyStoredGuard() bool
}

// filePasswordVerifier implements PasswordVerifier for the historical
// plaintext-in-tournament.md mode. The store reference is the same one
// AuthMiddleware already uses, so verification reads the cached record
// without additional I/O when the file hasn't changed.
type filePasswordVerifier struct {
	store *state.Store
}

// NewFileVerifier constructs the default verifier. Returned as the
// concrete type so callers using assertions can recover the underlying
// store reference if needed; the interface is the canonical contract.
func NewFileVerifier(store *state.Store) *filePasswordVerifier {
	return &filePasswordVerifier{store: store}
}

func (v *filePasswordVerifier) Verify(presented string) (bool, error) {
	t, err := v.store.LoadTournament()
	if err != nil {
		return false, err
	}
	if t == nil {
		return false, nil
	}
	return presented == t.Password, nil
}

func (v *filePasswordVerifier) Mode() string                  { return "file" }
func (v *filePasswordVerifier) ResetEnabled() bool            { return true }
func (v *filePasswordVerifier) AllowsFileBootstrap() bool     { return true }
func (v *filePasswordVerifier) EnforceEmptyStoredGuard() bool { return true }

// bcryptPasswordVerifier implements PasswordVerifier for locked mode. The
// hash is captured once at construction (process start) so a rotation
// requires a restart — intentional, since the hash lives in an env var
// outside the application's control and re-reading on every request would
// give a confusing partial-rotation window.
type bcryptPasswordVerifier struct {
	hash []byte
}

// NewBcryptVerifier validates `hash` as a bcrypt-encoded password hash
// and returns a verifier that compares incoming X-Tournament-Password
// values against it. The validation runs `bcrypt.Cost([]byte(hash))`
// which parses the hash header (algorithm + cost prefix) without leaking
// any timing oracle on the secret itself — malformed hashes are caught
// at startup so the operator gets a clear error rather than a 401-on-every-
// request runtime puzzle.
//
// An empty hash is rejected with a distinct error so the calling CLI can
// distinguish "operator forgot to set TOURNAMENT_PASSWORD_HASH" from
// "operator set a malformed hash."
func NewBcryptVerifier(hash string) (*bcryptPasswordVerifier, error) {
	if hash == "" {
		return nil, errors.New("TOURNAMENT_PASSWORD_HASH is empty (locked mode requires a bcrypt hash)")
	}
	b := []byte(hash)
	if _, err := bcrypt.Cost(b); err != nil {
		return nil, fmt.Errorf("TOURNAMENT_PASSWORD_HASH is not a valid bcrypt hash: %w", err)
	}
	return &bcryptPasswordVerifier{hash: b}, nil
}

func (v *bcryptPasswordVerifier) Verify(presented string) (bool, error) {
	if presented == "" {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword(v.hash, []byte(presented))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return false, err
}

func (v *bcryptPasswordVerifier) Mode() string                  { return "locked" }
func (v *bcryptPasswordVerifier) ResetEnabled() bool            { return false }
func (v *bcryptPasswordVerifier) AllowsFileBootstrap() bool     { return false }
func (v *bcryptPasswordVerifier) EnforceEmptyStoredGuard() bool { return false }

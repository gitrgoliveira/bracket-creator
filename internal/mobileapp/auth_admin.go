package mobileapp

import (
	"errors"
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"golang.org/x/crypto/bcrypt"
)

// ElevatedVerifier gates the destructive-operation middleware
// (RequireElevatedPassword) for spec 004 / mp-e21. It is intentionally
// NARROWER than PasswordVerifier: there is no bootstrap branch, no reset
// semantics, and no stored-password redaction policy , those belong to the
// primary credential. The elevated password is a SECOND factor layered on
// top of the main-password AuthMiddleware, so by the time these methods run
// the request has already proven it holds the main password.
//
// Two orthogonal questions drive the middleware's three outcomes:
// GateActive() (should we enforce at all?) and Configured() (is there a
// credential to compare against?):
//
//	GateActive=false              -> pass through (file mode, no admin pw set;
//	                                 back-compat: main password alone suffices)
//	GateActive=true, Configured=false -> 503 "admin password not configured"
//	                                 (locked mode, env hash unset; fail-closed)
//	GateActive=true, Configured=true  -> compare X-Admin-Password via Verify()
type ElevatedVerifier interface {
	// GateActive reports whether the middleware should enforce at all.
	//   file mode:   true only when an admin password has been set.
	//   locked mode: always true (fail-closed , a locked deployment that
	//                forgot the env var must NOT silently allow destructive
	//                ops with the main password alone).
	GateActive() bool

	// Configured reports whether a credential exists to compare against.
	//   file mode:   admin password non-empty (== GateActive there).
	//   locked mode: TOURNAMENT_ADMIN_PASSWORD_HASH was a valid bcrypt hash.
	Configured() bool

	// Verify checks the presented X-Admin-Password value. Only consulted
	// when GateActive() && Configured(); implementations may still be called
	// defensively and must treat an empty presented value as a non-match.
	Verify(presented string) (bool, error)

	// Mode mirrors PasswordVerifier.Mode for /auth-config ("file"|"locked").
	Mode() string
}

// fileElevatedVerifier implements ElevatedVerifier for file mode. It reads
// Tournament.AdminPassword from the same store the rest of the app uses, so
// a rotation through PUT /api/auth/admin-password takes effect immediately
// on the next request (no process restart, unlike the bcrypt env-var path).
type fileElevatedVerifier struct {
	store *state.Store
}

// NewFileElevatedVerifier constructs the file-mode elevated verifier.
func NewFileElevatedVerifier(store *state.Store) ElevatedVerifier {
	return &fileElevatedVerifier{store: store}
}

// adminPassword loads the current on-disk elevated password, or "" if no
// tournament exists yet. Errors propagate so the middleware can 500 rather
// than fail open.
func (v *fileElevatedVerifier) adminPassword() (string, error) {
	t, err := v.store.LoadTournament()
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", nil
	}
	return t.AdminPassword, nil
}

// GateActive: in file mode the gate is opt-in , it activates only once the
// operator has set an admin password. A load error is treated as
// "active" so a transient store failure fails closed (the subsequent
// Verify will surface the error and the middleware 500s) rather than
// silently disabling the gate.
func (v *fileElevatedVerifier) GateActive() bool {
	pw, err := v.adminPassword()
	if err != nil {
		return true
	}
	return pw != ""
}

// Configured mirrors GateActive in file mode: the same on-disk field both
// activates the gate and is the credential. A load error reports false so
// the middleware's Configured() branch doesn't 503 on a transient error ,
// the error is surfaced by Verify instead.
func (v *fileElevatedVerifier) Configured() bool {
	pw, err := v.adminPassword()
	if err != nil {
		return false
	}
	return pw != ""
}

// Verify does an exact-string compare against the stored admin password.
// An empty presented value never matches a non-empty stored password; the
// middleware's GateActive() short-circuit means this is never reached when
// the stored password is empty, so there is no empty-vs-empty vacuous match.
func (v *fileElevatedVerifier) Verify(presented string) (bool, error) {
	stored, err := v.adminPassword()
	if err != nil {
		return false, err
	}
	if stored == "" {
		return false, nil
	}
	return presented == stored, nil
}

func (v *fileElevatedVerifier) Mode() string { return "file" }

// bcryptElevatedVerifier implements ElevatedVerifier for locked mode when a
// valid TOURNAMENT_ADMIN_PASSWORD_HASH is present. It reuses the exact
// hardening proven in bcryptPasswordVerifier (auth_source.go): an empty or
// oversized presented value is a non-match (never a 500), and only genuine
// bcrypt mismatch/too-long errors are folded into "wrong credential".
type bcryptElevatedVerifier struct {
	hash []byte
}

// NewBcryptElevatedVerifier validates `hash` as a bcrypt-encoded password
// hash and returns a configured locked-mode verifier. An empty hash is
// rejected with a distinct error so the caller can fall back to the
// unconfigured verifier (which 503s on use) rather than failing startup ,
// a locked deployment that never performs destructive ops shouldn't be
// forced to set this env var just to boot.
func NewBcryptElevatedVerifier(hash string) (ElevatedVerifier, error) {
	if hash == "" {
		return nil, errors.New("TOURNAMENT_ADMIN_PASSWORD_HASH is empty")
	}
	b := []byte(hash)
	if _, err := bcrypt.Cost(b); err != nil {
		return nil, fmt.Errorf("TOURNAMENT_ADMIN_PASSWORD_HASH is not a valid bcrypt hash: %w", err)
	}
	return &bcryptElevatedVerifier{hash: b}, nil
}

func (v *bcryptElevatedVerifier) GateActive() bool { return true }
func (v *bcryptElevatedVerifier) Configured() bool { return true }

func (v *bcryptElevatedVerifier) Verify(presented string) (bool, error) {
	if presented == "" {
		return false, nil
	}
	// Pre-check length so an oversized header can't trip a 500 (and thus a
	// differential-error oracle) , identical rationale to
	// bcryptPasswordVerifier.Verify, bcryptMaxInputBytes defined there.
	if len(presented) > bcryptMaxInputBytes {
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword(v.hash, []byte(presented))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) || errors.Is(err, bcrypt.ErrPasswordTooLong) {
		return false, nil
	}
	return false, err
}

func (v *bcryptElevatedVerifier) Mode() string { return "locked" }

// lockedUnconfiguredElevatedVerifier represents locked mode with no (or an
// invalid) TOURNAMENT_ADMIN_PASSWORD_HASH. The gate is active (locked mode
// fails closed) but there is no credential, so the middleware returns 503
// "admin password not configured" on any gated endpoint. Routine
// (non-gated) endpoints are unaffected.
type lockedUnconfiguredElevatedVerifier struct{}

// NewLockedUnconfiguredElevatedVerifier constructs the fail-closed locked
// verifier used when the elevated env-var hash is absent or malformed.
func NewLockedUnconfiguredElevatedVerifier() ElevatedVerifier {
	return &lockedUnconfiguredElevatedVerifier{}
}

func (v *lockedUnconfiguredElevatedVerifier) GateActive() bool { return true }
func (v *lockedUnconfiguredElevatedVerifier) Configured() bool { return false }

// Verify should never be reached (the middleware checks Configured() first)
// but returns a non-match defensively so a future caller can't fail open.
func (v *lockedUnconfiguredElevatedVerifier) Verify(string) (bool, error) {
	return false, nil
}

func (v *lockedUnconfiguredElevatedVerifier) Mode() string { return "locked" }

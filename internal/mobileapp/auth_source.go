package mobileapp

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

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
	// UNAUTHENTICATED POST/PUT to /api/tournament through when no
	// tournament has been bootstrapped yet.
	//
	//   - file mode: true. The operator picks the admin password during
	//     CreateTournament; before that, there's nothing to authenticate
	//     against. This is the historical UX.
	//   - locked mode: false. The env-var bcrypt hash IS the admin
	//     credential from the very first request; allowing anonymous
	//     bootstrap on an internet-exposed fresh deployment would let
	//     any network client race-claim the initial tournament record
	//     with garbage data before the operator finishes deploying.
	//     The SPA's CreateTournament form sends the env-var password
	//     in the X-Tournament-Password header when authConfig.mode ==
	//     "locked"; the middleware accepts the bootstrap if that header
	//     verifies.
	AllowsFileBootstrap() bool

	// EnforceEmptyStoredGuard reports whether the middleware should
	// 403 when the stored tournament has an empty Password field. This
	// is defense-in-depth against the F4 sentinel-into-auth scenario in
	// file mode (an empty stored password would let `password != t.Password`
	// pass vacuously for an empty header). In locked mode the stored
	// password is irrelevant, so the guard would 403 every request, it
	// must be disabled.
	EnforceEmptyStoredGuard() bool

	// RedactStoredPassword reports whether the on-disk Tournament.Password
	// field is non-authoritative for authentication and must therefore be
	// stripped from API responses AND ignored on writes. True for any
	// verifier whose authentication does not consult `tournament.md`,
	// today that's the bcrypt locked verifier; tomorrow it might be an
	// OIDC or LDAP variant. Centralizing the test here means handlers
	// don't grow scattered `verifier.Mode() == "locked"` string compares
	// that would silently break when a third mode is added.
	RedactStoredPassword() bool
}

// filePasswordVerifier implements PasswordVerifier for the historical
// plaintext-in-tournament.md mode. The store reference is the same one
// AuthMiddleware already uses, so verification reads the cached record
// without additional I/O when the file hasn't changed.
type filePasswordVerifier struct {
	store *state.Store
}

// NewFileVerifier constructs the default verifier. Returns the
// PasswordVerifier interface so callers cannot accidentally reach
// into the concrete type, the interface is the canonical contract,
// and any future verifier (LDAP, OIDC) can be wired in without
// breaking signatures.
func NewFileVerifier(store *state.Store) PasswordVerifier {
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
	return subtle.ConstantTimeCompare([]byte(presented), []byte(t.Password)) == 1, nil
}

func (v *filePasswordVerifier) Mode() string                  { return "file" }
func (v *filePasswordVerifier) ResetEnabled() bool            { return true }
func (v *filePasswordVerifier) AllowsFileBootstrap() bool     { return true }
func (v *filePasswordVerifier) EnforceEmptyStoredGuard() bool { return true }

// RedactStoredPassword: in file mode the on-disk Password IS the
// authoritative credential, never strip it.
func (v *filePasswordVerifier) RedactStoredPassword() bool { return false }

// bcryptPasswordVerifier implements PasswordVerifier for locked mode. The
// hash is captured once at construction (process start) so a rotation
// requires a restart, intentional, since the hash lives in an env var
// outside the application's control and re-reading on every request would
// give a confusing partial-rotation window.
type bcryptPasswordVerifier struct {
	hash []byte
}

// NewBcryptVerifier validates `hash` as a bcrypt-encoded password hash
// and returns a verifier that compares incoming X-Tournament-Password
// values against it. The validation runs `bcrypt.Cost([]byte(hash))`
// which parses the hash header (algorithm + cost prefix) without leaking
// any timing oracle on the secret itself, malformed hashes are caught
// at startup so the operator gets a clear error rather than a 401-on-every-
// request runtime puzzle.
//
// An empty hash is rejected with a distinct error so the calling CLI can
// distinguish "operator forgot to set TOURNAMENT_PASSWORD_HASH" from
// "operator set a malformed hash."
func NewBcryptVerifier(hash string) (PasswordVerifier, error) {
	if hash == "" {
		return nil, errors.New("TOURNAMENT_PASSWORD_HASH is empty (locked mode requires a bcrypt hash)")
	}
	b := []byte(hash)
	if _, err := bcrypt.Cost(b); err != nil {
		return nil, fmt.Errorf("TOURNAMENT_PASSWORD_HASH is not a valid bcrypt hash: %w", err)
	}
	return &bcryptPasswordVerifier{hash: b}, nil
}

// bcryptMaxInputBytes is bcrypt's hard limit on the plaintext input.
// Longer inputs cause CompareHashAndPassword to return ErrPasswordTooLong
// rather than a mismatch. We pre-check the length so an unauthenticated
// client cannot trip a 500 from the middleware by sending an oversized
// header, that would also let them probe whether locked mode is active
// by distinguishing 500 (locked, length-exceeded) from 401 (file mode
// or short-input mismatch). Treat oversize as a normal auth failure.
const bcryptMaxInputBytes = 72

func (v *bcryptPasswordVerifier) Verify(presented string) (bool, error) {
	if presented == "" {
		return false, nil
	}
	if len(presented) > bcryptMaxInputBytes {
		// Oversized header, same outcome as a wrong password. No 500,
		// no information leak via differential error codes.
		return false, nil
	}
	err := bcrypt.CompareHashAndPassword(v.hash, []byte(presented))
	if err == nil {
		return true, nil
	}
	// ErrMismatchedHashAndPassword and ErrPasswordTooLong both mean
	// "wrong credential" from the operator's perspective. ErrPasswordTooLong
	// shouldn't be reachable here (we pre-checked length) but the
	// defense-in-depth match keeps a future bcrypt-internals change from
	// turning long inputs into 500s.
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) || errors.Is(err, bcrypt.ErrPasswordTooLong) {
		return false, nil
	}
	return false, err
}

func (v *bcryptPasswordVerifier) Mode() string       { return "locked" }
func (v *bcryptPasswordVerifier) ResetEnabled() bool { return false }

// AllowsFileBootstrap returns false. Anonymous bootstrap on an
// internet-exposed locked deployment would let a network-reachable
// attacker race-claim the initial tournament record before the
// operator does, even though the submitted password is discarded
// (so the attacker can't inject a credential), they could fill the
// store with garbage Name/Date/Venue/Courts data the operator then
// has to clean up out-of-band. The SPA's CreateTournament form
// detects locked mode via /api/auth-config and sends the env-var
// password in the X-Tournament-Password header so the middleware
// can verify it; curl-style bootstrap requires the same header.
func (v *bcryptPasswordVerifier) AllowsFileBootstrap() bool { return false }

func (v *bcryptPasswordVerifier) EnforceEmptyStoredGuard() bool { return false }

// RedactStoredPassword: in locked mode the on-disk Password is
// irrelevant to authentication, so it must be stripped from API
// responses (to avoid leaking a stale or pre-migration credential)
// and ignored on writes (PUT preserves the stored value but the
// response redacts it). The on-disk value is preserved so an
// operator who later switches back to file mode can recover,
// documented in docs/user-guide/mobile-app.md.
func (v *bcryptPasswordVerifier) RedactStoredPassword() bool { return true }

// wrongPasswordCacheTTL is how long a wrong password stays known to be wrong
// in locked mode, so a flood of one wrong password on the public reads costs
// one bcrypt comparison a minute (the per-IP limiter bounds the rest).
const wrongPasswordCacheTTL = 60 * time.Second

// wrongPasswordCacheMax bounds the wrong-password cache. Past it, expired
// entries are dropped; while every entry is still live a new wrong password
// is simply not remembered.
const wrongPasswordCacheMax = 4096

// cacheableVerifier reports whether a verifier's answer for a password can
// never change while the process runs, so it may be cached: the bcrypt
// (locked-mode) verifier, whose hash is read once at start. The file
// verifier is not: its password changes with tournament.md, and its plain
// compare is cheap anyway.
func cacheableVerifier(v PasswordVerifier) bool {
	_, ok := v.(*bcryptPasswordVerifier)
	return ok
}

// cachedVerifier wraps a locked-mode verifier with a digest cache: the
// correct password, once seen, skips bcrypt; a wrong one is remembered for
// wrongPasswordCacheTTL. Concurrent checks of one password share one
// comparison. Digests are HMACs under a key minted per process, so the
// memory holds no plain hash of a password.
type cachedVerifier struct {
	PasswordVerifier
	now func() time.Time

	salt [32]byte

	mu       sync.Mutex
	good     [sha256.Size]byte
	haveGood bool
	bad      map[[sha256.Size]byte]time.Time
	inflight map[[sha256.Size]byte]*verifyCall
}

type verifyCall struct {
	done  chan struct{}
	valid bool
	err   error
}

func newCachedVerifier(inner PasswordVerifier, now func() time.Time) *cachedVerifier {
	v := &cachedVerifier{
		PasswordVerifier: inner,
		now:              now,
		bad:              map[[sha256.Size]byte]time.Time{},
		inflight:         map[[sha256.Size]byte]*verifyCall{},
	}
	if _, err := rand.Read(v.salt[:]); err != nil {
		// crypto/rand does not fail on supported platforms; a zero salt
		// still keys the digests, only less privately.
		v.salt = [32]byte{}
	}
	return v
}

func (v *cachedVerifier) digest(presented string) [sha256.Size]byte {
	mac := hmac.New(sha256.New, v.salt[:])
	mac.Write([]byte(presented)) // hash.Hash.Write never returns an error
	var d [sha256.Size]byte
	copy(d[:], mac.Sum(nil))
	return d
}

// Verify answers from the cache when it can, and otherwise asks the wrapped
// verifier once for every concurrent caller presenting the same password. An
// error is passed on and never cached.
func (v *cachedVerifier) Verify(presented string) (bool, error) {
	if presented == "" {
		return v.PasswordVerifier.Verify(presented)
	}
	d := v.digest(presented)
	now := v.now()

	v.mu.Lock()
	if v.haveGood && subtle.ConstantTimeCompare(d[:], v.good[:]) == 1 {
		v.mu.Unlock()
		return true, nil
	}
	if exp, ok := v.bad[d]; ok {
		if now.Before(exp) {
			v.mu.Unlock()
			return false, nil
		}
		delete(v.bad, d)
	}
	if call, ok := v.inflight[d]; ok {
		v.mu.Unlock()
		<-call.done
		return call.valid, call.err
	}
	call := &verifyCall{done: make(chan struct{})}
	v.inflight[d] = call
	v.mu.Unlock()

	call.valid, call.err = v.PasswordVerifier.Verify(presented)

	v.mu.Lock()
	delete(v.inflight, d)
	if call.err == nil {
		if call.valid {
			v.good, v.haveGood = d, true
		} else {
			v.rememberWrongLocked(d, now)
		}
	}
	v.mu.Unlock()
	close(call.done)
	return call.valid, call.err
}

// rememberWrongLocked records a wrong password's digest until
// wrongPasswordCacheTTL from now. Caller holds v.mu.
func (v *cachedVerifier) rememberWrongLocked(d [sha256.Size]byte, now time.Time) {
	if len(v.bad) >= wrongPasswordCacheMax {
		for k, exp := range v.bad {
			if !now.Before(exp) {
				delete(v.bad, k)
			}
		}
		if len(v.bad) >= wrongPasswordCacheMax {
			return
		}
	}
	v.bad[d] = now.Add(wrongPasswordCacheTTL)
}

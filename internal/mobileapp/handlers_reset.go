package mobileapp

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// resetPasswordRequest is the body shape for POST /api/tournament/reset.
// A single field — the new password — replaces whatever is currently
// stored in tournament.md. The endpoint is intentionally minimal: there
// is no "old password" check because the whole point is to recover from
// not knowing it.
//
// Operators who want to deny this endpoint should enable locked mode
// (--lock-password) which 404s the route. In file mode the endpoint is
// the documented recovery path for a forgotten admin password.
type resetPasswordRequest struct {
	Password string `json:"password"`
	// OriginatorID is an opaque per-tab identifier the SPA generates
	// on mount and sends with the reset POST so the SSE broadcast
	// (EventPasswordReset) can be ignored in the originating tab.
	// Without it, the tab that just submitted /reset would receive
	// its own broadcast and immediately clear the localStorage
	// credential ResetPasswordForm just wrote — kicking the operator
	// who reset straight back to the AuthModal. The server treats
	// the value as opaque: echo on the broadcast, never persist.
	OriginatorID string `json:"originatorId,omitempty"`
}

// passwordResetEventData is the payload of an EventPasswordReset SSE
// broadcast. Carries only the OriginatorID so consumer tabs can
// suppress their own resets; everything else (mode, etc.) is fetched
// fresh from /api/auth-config on demand.
type passwordResetEventData struct {
	OriginatorID string `json:"originatorId,omitempty"`
}

// MaxLenOriginatorID caps the originatorId at 128 bytes — a UUID is
// 36 bytes, a fallback random string is ~20; 128 leaves headroom for
// future variants without letting an attacker pump arbitrary bytes
// through the SSE channel.
const MaxLenOriginatorID = 128

// errResetPasswordRequired is the sentinel the reset transform returns
// when the new Password is empty after binding. Surfaced as a 400 by
// the handler. Mirrors errPasswordRequired in handlers_tournament.go.
var errResetPasswordRequired = errors.New("password is required")

// isSameOriginReset reports whether the request's Origin header is
// safe to treat as a same-origin / non-browser caller for the reset
// endpoint. The global CORS policy is `Access-Control-Allow-Origin: *`
// (the viewer routes intentionally support cross-origin reads), which
// means a malicious site that an operator visits could otherwise issue
// a cross-origin POST to /api/tournament/reset on the operator's
// LAN-reachable tournament server and rotate the admin password.
//
// Rules:
//   - No Origin header → non-browser caller (curl, scripted client,
//     mobile app over LAN that doesn't set Origin). Allowed.
//   - Origin scheme AND host:port both match → genuine same-origin
//     browser request (the operator opened /reset in their tab). Allowed.
//   - Origin set but scheme or host doesn't match (different scheme,
//     different host string, malformed URL, or Origin: null from a
//     sandboxed iframe/file://) → Rejected.
//
// Known limitations:
//   - The host comparison is exact-string on host:port. An operator at
//     `http://localhost:8089` and a colleague reaching the same machine
//     via `http://127.0.0.1:8089` are treated as different origins —
//     which is also the browser's behavior, so a cross-DNS pivot can't
//     happen through a normal browser session anyway.
//   - Behind a TLS-terminating reverse proxy, c.Request.TLS is nil even
//     when the browser sees HTTPS; the scheme check would reject the
//     legitimate https Origin. Such deployments should run with
//     --lock-password (which 404s /reset entirely) and rotate credentials
//     via env-var hash; the recovery endpoint is designed for direct
//     same-host operator access.
//
// We deliberately do NOT support an allowlist env var here: the
// recovery path is for operators sitting at the tournament server.
// Anyone reaching it cross-origin is either misconfigured or hostile.
func isSameOriginReset(c *gin.Context) bool {
	origin := strings.TrimSpace(c.GetHeader("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}

	// Derive the expected scheme from whether the connection uses TLS.
	// Same-origin includes both scheme AND host:port (RFC 6454 §3.2), so
	// an https Origin against an http server (or vice-versa) is
	// cross-origin even when the host matches.
	//
	// For direct connections, c.Request.TLS is authoritative. Behind a
	// TLS-terminating reverse proxy, c.Request.TLS is nil even when
	// the browser sees HTTPS; such deployments should run with
	// --lock-password (which 404s this endpoint entirely), so
	// conservatively rejecting the scheme mismatch is still the right
	// call here.
	expectedScheme := "http"
	if c.Request.TLS != nil {
		expectedScheme = "https"
	}

	// Compare both scheme and host:port. c.Request.Host already includes
	// the port if the client sent one (e.g. "localhost:8080"). Origin.Host
	// follows the same convention, so direct comparison works.
	return strings.EqualFold(u.Scheme, expectedScheme) &&
		strings.EqualFold(u.Host, c.Request.Host)
}

// RegisterResetHandlers wires POST /api/tournament/reset. The route is
// public (no admin auth header required) because it IS the unlock path
// for a forgotten password. In locked mode the verifier reports
// ResetEnabled()==false and the handler 404s — matching the
// path-doesn't-exist response so a scanner can't differentiate a locked
// deployment from one that's been compiled without this feature.
func RegisterResetHandlers(r *gin.RouterGroup, store *state.Store, verifier PasswordVerifier, hub *Hub) {
	r.POST("/tournament/reset", func(c *gin.Context) {
		if !verifier.ResetEnabled() {
			// 404 (not 403) so the response is indistinguishable from a
			// build that doesn't register this route. The SPA discovers
			// the locked state via GET /api/auth-config.
			c.JSON(http.StatusNotFound, gin.H{"error": "reset disabled"})
			return
		}

		// Cross-origin POST defense — see isSameOriginReset. Done before
		// body parsing so a malicious site can't even probe how the
		// endpoint reacts to a payload.
		if !isSameOriginReset(c) {
			c.JSON(http.StatusForbidden, gin.H{"error": "cross-origin reset not permitted"})
			return
		}

		var req resetPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Password is NOT trimmed (matches the PUT/POST tournament
		// handlers — passwords may legitimately contain whitespace and
		// the auth check is exact-string match).
		if req.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errResetPasswordRequired.Error()})
			return
		}
		if err := validateMaxLen("password", req.Password, MaxLenTournamentPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("originatorId", req.OriginatorID, MaxLenOriginatorID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Load the current tournament under the same atomic primitive
		// the PUT handler uses so a concurrent PUT/POST can't race the
		// reset. We refuse to bootstrap from reset — the operator must
		// use POST /api/tournament for that, since bootstrap also needs
		// name/date/venue/courts. A pure password-set against a
		// non-existent record would persist Name=="" which would fail
		// the tournament-has-a-name invariant on the next PUT.
		//
		// "Uninitialized" must match the middleware's sentinel exactly
		// (middleware.go:69): no file on disk, OR the default record
		// with name="New Tournament" AND empty password. Just checking
		// `t.Name == "New Tournament"` would 409 a legitimately-named
		// "New Tournament" record (rare but valid) and prevent its
		// admin from ever resetting via this endpoint.
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil || (t.Name == "New Tournament" && t.Password == "") {
			c.JSON(http.StatusConflict, gin.H{"error": "tournament not initialized; bootstrap via POST /api/tournament"})
			return
		}

		changed, err := store.UpdateTournamentChanged(t, func(current, desired *state.Tournament) error {
			if current == nil {
				return errors.New("tournament not initialized")
			}
			// Copy current forward, then overwrite Password. We mutate
			// `desired` (which is `t` from above) rather than returning a
			// new value because UpdateTournamentChanged persists `desired`.
			*desired = *current
			if current.Courts != nil {
				desired.Courts = make([]string, len(current.Courts))
				copy(desired.Courts, current.Courts)
			}
			desired.Password = req.Password
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			// Two events on a successful reset:
			//   - EventTournamentUpdated: every connected viewer re-fetches
			//     public tournament data so date/venue/etc remain in sync
			//     (mirrors the PUT handler's behavior).
			//   - EventPasswordReset: admin sessions clear localStorage
			//     and re-show AuthModal. Without this, other admins'
			//     cached password stays in localStorage until their next
			//     write fails with 401 — surprising UX.
			//     The OriginatorId echoed here lets the submitting tab
			//     identify and ignore its own broadcast so the operator
			//     who just reset isn't immediately logged out.
			hub.Broadcast(EventTournamentUpdated, nil)
			hub.Broadcast(EventPasswordReset, passwordResetEventData{
				OriginatorID: req.OriginatorID,
			})
		}
		c.Status(http.StatusNoContent)
	})
}

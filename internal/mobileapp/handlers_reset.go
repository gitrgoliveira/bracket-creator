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
}

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
//   - Origin matches the request host → genuine same-origin browser
//     request (the operator opened /reset in their browser tab).
//     Allowed.
//   - Origin set and doesn't match host → cross-origin from another
//     site. Rejected.
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
	// Compare host:port. c.Request.Host already includes the port if
	// the client sent one (e.g. "localhost:8080"). Origin.Host follows
	// the same convention, so direct comparison works.
	return strings.EqualFold(u.Host, c.Request.Host)
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

		// Load the current tournament under the same atomic primitive
		// the PUT handler uses so a concurrent PUT/POST can't race the
		// reset. We refuse to bootstrap from reset — the operator must
		// use POST /api/tournament for that, since bootstrap also needs
		// name/date/venue/courts. A pure password-set against a
		// non-existent record would persist Name=="" which would fail
		// the tournament-has-a-name invariant on the next PUT.
		//
		// "Uninitialized" must match the middleware's sentinel exactly
		// (middleware.go:52): no file on disk, OR the default record
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
			hub.Broadcast(EventTournamentUpdated, nil)
			hub.Broadcast(EventPasswordReset, nil)
		}
		c.Status(http.StatusNoContent)
	})
}

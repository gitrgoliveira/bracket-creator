package mobileapp

import (
	"errors"
	"net/http"

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
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil || t.Name == "New Tournament" {
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
			// All already-logged-in admin sessions store the previous
			// password in localStorage; broadcasting tournament_updated
			// causes their SSE-driven refresh to refetch and discover
			// they're no longer authorized — the SPA then re-shows
			// AuthModal. This is the correct behavior (password
			// changed ⇒ everyone re-authenticates).
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusNoContent)
	})
}

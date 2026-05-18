package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// authConfigResponse is the wire shape for GET /api/auth-config.
//
// The SPA fetches this once on App() mount to decide:
//   - whether to render the "Forgot password?" link in AuthModal
//   - whether the /reset route should show a form or a "disabled by
//     operator" message
//
// Mode is informational ("file" or "locked"); ResetEnabled is the
// canonical control bit. Both are exposed so we can grow other
// operator-visible auth metadata here without breaking clients that
// already key off the mode string.
type authConfigResponse struct {
	Mode         string `json:"mode"`
	ResetEnabled bool   `json:"resetEnabled"`
}

// RegisterAuthConfigHandlers wires GET /api/auth-config. The endpoint is
// public (no admin auth header required) — the response carries no
// sensitive material, only the toggle bits the SPA needs to render its
// auth UI consistently with the running configuration.
//
// A locked-mode deployment can still expose this safely because the
// information ("password is in env var, reset is off") is already
// inferable from any 404 on /api/tournament/reset. Exposing it
// explicitly lets the SPA branch up-front rather than relying on a
// 404-probe race during form rendering.
func RegisterAuthConfigHandlers(r *gin.RouterGroup, verifier PasswordVerifier) {
	r.GET("/auth-config", func(c *gin.Context) {
		c.JSON(http.StatusOK, authConfigResponse{
			Mode:         verifier.Mode(),
			ResetEnabled: verifier.ResetEnabled(),
		})
	})
}

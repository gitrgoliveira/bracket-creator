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

	// Elevated-password (destructive-ops) bits , spec 004 / mp-e21. The SPA
	// reads these on mount to decide whether to prompt for X-Admin-Password
	// before a destructive action and whether the Settings field is editable.
	//
	//   ElevatedRequired   , is the gate active? (file: admin pw set;
	//                         locked: always true). When false, destructive
	//                         ops need only the main password.
	//   ElevatedConfigured , does a credential exist? (file: admin pw set;
	//                         locked: env hash present). When required but
	//                         not configured, gated endpoints return 503.
	//   ElevatedEditable   , can the admin password be set via the API?
	//                         file mode true; locked mode false (env-var).
	ElevatedRequired   bool `json:"elevatedRequired"`
	ElevatedConfigured bool `json:"elevatedConfigured"`
	ElevatedEditable   bool `json:"elevatedEditable"`

	// ScheduleEnabled controls whether the SPA renders the admin
	// "Tournament schedule" card. Sourced from the ENABLE_TOURNAMENT_SCHEDULE
	// env var (truthy: "1", "true", "yes", "on"). Default OFF , mp-fwce.
	ScheduleEnabled bool `json:"scheduleEnabled"`
}

// RegisterAuthConfigHandlers wires GET /api/auth-config. The endpoint is
// public (no admin auth header required) , the response carries no
// sensitive material, only the toggle bits the SPA needs to render its
// auth UI consistently with the running configuration.
//
// A locked-mode deployment can still expose this safely because the
// information ("password is in env var, reset is off") is already
// inferable from any 404 on /api/tournament/reset. Exposing it
// explicitly lets the SPA branch up-front rather than relying on a
// 404-probe race during form rendering.
//
// scheduleEnabled is sourced from ENABLE_TOURNAMENT_SCHEDULE in
// cmd/mobile_app.go and threaded here so the env var is read once at
// startup rather than on every request.
func RegisterAuthConfigHandlers(r *gin.RouterGroup, verifier PasswordVerifier, elevated ElevatedVerifier, scheduleEnabled bool) {
	r.GET("/auth-config", func(c *gin.Context) {
		c.JSON(http.StatusOK, authConfigResponse{
			Mode:               verifier.Mode(),
			ResetEnabled:       verifier.ResetEnabled(),
			ElevatedRequired:   elevated.GateActive(),
			ElevatedConfigured: elevated.Configured(),
			// Editable only in file mode , the locked-mode credential is the
			// TOURNAMENT_ADMIN_PASSWORD_HASH env var, not settable via API.
			ElevatedEditable: elevated.Mode() == "file",
			ScheduleEnabled:  scheduleEnabled,
		})
	})
}

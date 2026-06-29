package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// adminPasswordRequest is the body shape for PUT /api/auth/admin-password.
//
//   - NewPassword is the elevated password to store (required, non-empty).
//   - CurrentPassword is required ONLY when an admin password is already set
//     (rotation): it must equal the stored value so a main-password holder
//     cannot silently re-set the elevated secret (spec 004 Finding 2). On
//     first-time set (TOFU) it is ignored.
//
// Neither value is ever echoed back, the elevated password is write-only at
// the API boundary (Tournament.AdminPassword has json:"-").
type adminPasswordRequest struct {
	NewPassword     string `json:"newPassword"`
	CurrentPassword string `json:"currentPassword,omitempty"`
}

// RegisterAdminPasswordHandler wires PUT /api/auth/admin-password, the only
// way to set or rotate the file-mode elevated password (spec 004 §6a). It is
// registered inside the admin group, so AuthMiddleware has already verified
// the MAIN password before this runs.
//
// Gating:
//   - locked mode → 404. The elevated credential is the env-var bcrypt hash
//     (TOURNAMENT_ADMIN_PASSWORD_HASH); it is not settable via the API. The
//     404 (not 405/403) matches the "route doesn't exist in this mode" shape
//     used by the password-reset endpoint.
//   - file mode, no admin password set yet → trust-on-first-use: the main
//     password (already verified) authorizes the initial set.
//   - file mode, admin password already set → rotation requires the correct
//     CurrentPassword (verified via the ElevatedVerifier) in addition to the
//     main password.
func RegisterAdminPasswordHandler(r *gin.RouterGroup, store *state.Store, ev ElevatedVerifier) {
	r.PUT("/auth/admin-password", func(c *gin.Context) {
		// Locked mode: not settable via API. ev.Mode() == "locked" for both
		// the configured (bcrypt) and unconfigured locked verifiers.
		if ev.Mode() == "locked" {
			c.JSON(http.StatusNotFound, gin.H{"error": "admin password is controlled by TOURNAMENT_ADMIN_PASSWORD_HASH and is not settable via the API"})
			return
		}

		var req adminPasswordRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// NewPassword is NOT trimmed (passwords may legitimately contain
		// whitespace; the auth comparison is exact-string match, same
		// policy as the main password).
		if req.NewPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "newPassword is required"})
			return
		}
		if err := validateMaxLen("newPassword", req.NewPassword, MaxLenTournamentPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("currentPassword", req.CurrentPassword, MaxLenTournamentPassword); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// A tournament record must exist before an admin password can be
		// attached to it, otherwise the set would land on a non-existent
		// record (and the bulk PUT/POST guards require a name anyway).
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil {
			c.JSON(http.StatusConflict, gin.H{"error": "tournament not configured yet"})
			return
		}

		// Rotation gate: if an admin password is already set, the caller
		// must prove possession of the current one. GateActive() is true in
		// file mode exactly when AdminPassword != "". Verify reads the
		// current stored value, so this is the canonical "old password"
		// check. First-time set (GateActive() == false) skips it (TOFU).
		if ev.GateActive() {
			ok, verr := ev.Verify(req.CurrentPassword)
			if verr != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "admin auth verification failed"})
				return
			}
			if !ok {
				c.JSON(http.StatusUnauthorized, gin.H{"error": "current admin password is incorrect"})
				return
			}
		}

		// Persist under the store's write lock. The desired struct's other
		// fields are irrelevant, the transform copies everything from the
		// current record forward and changes only AdminPassword, so a
		// concurrent bulk PUT can't be clobbered by a stale desired here.
		_, err = store.UpdateTournamentChanged(t, func(current, desired *state.Tournament) error {
			if current != nil {
				*desired = *current
			}
			desired.AdminPassword = req.NewPassword
			return nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Write-only: never echo the value. Report only the resulting state.
		c.JSON(http.StatusOK, gin.H{"status": "ok", "elevatedConfigured": true})
	})
}

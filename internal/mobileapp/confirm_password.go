package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Codes of the two refusals; both carry confirmPasswordSentence.
const (
	confirmPasswordRequiredCode = "confirm_password_required"
	confirmPasswordWrongCode    = "confirm_password_wrong"
)

const confirmPasswordSentence = "Wrong password. The competition was not deleted."

// isCompetitionDelete reports whether a request is DELETE /api/competitions/:id,
// by the route pattern, so DELETE .../draw and the other sub-routes are not it.
func isCompetitionDelete(method, fullPath string) bool {
	return method == http.MethodDelete && fullPath == "/api/competitions/:id"
}

// RequireConfirmPasswordForDelete refuses a competition delete unless the
// X-Confirm-Password header, the password typed in that moment, matches the
// admin password. Deleting a competition is the one action that asks for the
// password again (operator ruling 2026-10-12); every other organiser write is
// authorised by the sign-in alone. The password is judged by the same
// PasswordVerifier as the sign-in. It runs after AuthMiddleware, so the
// sign-in applies first in both modes, and before the handler deletes anything.
// The refusal is a 401 the SPA reads as a wrong typed password, not as a lost
// session (it names a code, and the sign-in's own 401 body does not).
func RequireConfirmPasswordForDelete(verifier PasswordVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !isCompetitionDelete(c.Request.Method, c.FullPath()) {
			c.Next()
			return
		}
		confirm := c.GetHeader("X-Confirm-Password")
		if confirm == "" {
			respondConfirmPassword(c, confirmPasswordRequiredCode)
			return
		}
		ok, err := verifier.Verify(confirm)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
			return
		}
		if !ok {
			respondConfirmPassword(c, confirmPasswordWrongCode)
			return
		}
		c.Next()
	}
}

func respondConfirmPassword(c *gin.Context, code string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   confirmPasswordSentence,
		"code":    code,
		"message": confirmPasswordSentence,
	})
}

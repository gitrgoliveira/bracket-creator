package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Codes of the two refusals; both carry the route's sentence (confirmedRoutes).
const (
	confirmPasswordRequiredCode = "confirm_password_required"
	confirmPasswordWrongCode    = "confirm_password_wrong"
)

// confirmedRoutes lists the organiser writes that ask for the password again,
// by method and route pattern (so DELETE .../draw is not the competition
// delete, and POST .../restore/plan is not the restore), each with the
// sentence its refusal carries. The restore's dry run is its own route, so no
// query or form field can skip the confirm.
var confirmedRoutes = map[string]string{
	http.MethodDelete + " /api/competitions/:id":         "Wrong password. The competition was not deleted.",
	http.MethodPost + " /api/tournament/restore":         "Wrong password. Nothing was restored.",
	http.MethodDelete + " /api/tournament/backups/:name": "Wrong password. The backup was not deleted.",
}

// confirmedRouteSentence reports whether a request is one of confirmedRoutes,
// with its refusal sentence.
func confirmedRouteSentence(method, fullPath string) (string, bool) {
	sentence, ok := confirmedRoutes[method+" "+fullPath]
	return sentence, ok
}

// RequireConfirmPassword refuses a route in confirmedRoutes unless the
// X-Confirm-Password header, the password typed in that moment, matches the
// admin password. Deleting a competition (operator ruling 2026-10-12), a
// restore and deleting a kept backup ask for the password again; every other
// organiser write is authorised by the sign-in alone. The password is judged
// by the same PasswordVerifier as the sign-in. It runs after AuthMiddleware,
// so the sign-in applies first in both modes, and before the handler changes
// anything. Gin's Use covers only the routes registered after it, so each
// group Uses it before registering its confirmed routes. The refusal is a 401
// the SPA reads as a wrong typed password, not as a lost session (it names a
// code, and the sign-in's own 401 body does not).
func RequireConfirmPassword(verifier PasswordVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		sentence, ok := confirmedRouteSentence(c.Request.Method, c.FullPath())
		if !ok {
			c.Next()
			return
		}
		confirm := c.GetHeader("X-Confirm-Password")
		if confirm == "" {
			respondConfirmPassword(c, confirmPasswordRequiredCode, sentence)
			return
		}
		match, err := verifier.Verify(confirm)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
			return
		}
		if !match {
			respondConfirmPassword(c, confirmPasswordWrongCode, sentence)
			return
		}
		c.Next()
	}
}

func respondConfirmPassword(c *gin.Context, code, sentence string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error":   sentence,
		"code":    code,
		"message": sentence,
	})
}

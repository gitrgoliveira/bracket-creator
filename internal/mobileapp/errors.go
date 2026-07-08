package mobileapp

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

// internalError logs err (with request method + path for context) and returns a
// generic HTTP 500 body, so wrapped internal details, filesystem paths, and
// underlying causes never reach the client. The full error is always preserved in
// the server log for operator diagnostics.
//
// Pass a single SAFE, caller-controlled publicMsg to keep an operator-friendly
// label in the response (e.g. "failed to save participants"); it must NOT embed
// err.Error() or any dynamic internal detail. When omitted or empty, a generic
// "internal error" is returned.
//
// Use this for the catch-all/unexpected 500 path. Specific, user-actionable
// failures should still return an explicit 4xx with their own message.
func internalError(c *gin.Context, err error, publicMsg ...string) {
	log.Printf("mobileapp: %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	msg := "internal error"
	if len(publicMsg) > 0 && publicMsg[0] != "" {
		msg = publicMsg[0]
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
}

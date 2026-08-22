package mobileapp

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
	// A file the operator can repair is the ONE internal failure worth naming.
	// Everything else here is deliberately opaque, but "internal error" on a
	// corrupt competition file tells an organiser mid tournament that scoring
	// has stopped and nothing else -- while the cause, the file and the exact
	// line sit in a server log they are not reading. The detail is a parser's
	// description of syntax, never competitor data, so it is safe to return.
	//
	// Hooked HERE rather than at each call site on purpose: this function is
	// the catch-all every handler already funnels its unexpected failures
	// through, so one branch upgrades all of them, and a future handler that
	// can reach a corrupt file inherits the message without having to know it
	// exists.
	if cf, ok := state.AsCorruptFile(err); ok {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":  "a competition data file could not be read",
			"code":   "corrupt_file",
			"file":   cf.File,
			"line":   cf.Line,
			"column": cf.Column,
			"detail": cf.Detail,
		})
		return
	}
	msg := "internal error"
	if len(publicMsg) > 0 && publicMsg[0] != "" {
		msg = publicMsg[0]
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
}

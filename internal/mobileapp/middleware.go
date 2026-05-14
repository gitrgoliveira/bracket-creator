package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// requireValidCompID extracts the `:id` URL parameter and validates it
// via state.ValidateCompetitionID. Rejects:
//   - empty
//   - > 64 chars
//   - any character outside [a-zA-Z0-9_-]
//   - a leading non-alphanumeric character (so "_foo", "-foo" are
//     rejected even though "_" and "-" are allowed elsewhere in the
//     string — the regex is ^[a-zA-Z0-9][a-zA-Z0-9_-]*$)
//
// On invalid input, writes a 400 response and returns ("", false); the
// caller should `return` immediately.
//
// Every handler that reads `c.Param("id")` and passes it to
// store.compPath(id, ...) must use this helper. compPath does
// filepath.Clean(filepath.Join(folder, "competitions", id, ...)) — an
// id like "../../../etc/passwd" would cleanly escape the data dir.
// Gated by AuthMiddleware (X-Tournament-Password), so requires admin
// compromise — but defense-in-depth.
func requireValidCompID(c *gin.Context) (string, bool) {
	id := c.Param("id")
	if err := state.ValidateCompetitionID(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return "", false
	}
	return id, true
}

func AuthMiddleware(store *state.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament config"})
			c.Abort()
			return
		}

		// If no tournament config exists yet (or it's the default blank one), only allow creating one
		if t == nil || (t.Name == "New Tournament" && t.Password == "") {
			if (c.Request.Method == http.MethodPut || c.Request.Method == http.MethodPost) && c.FullPath() == "/api/tournament" {
				c.Next()
				return
			}
			c.JSON(http.StatusForbidden, gin.H{"error": "tournament not configured yet"})
			c.Abort()
			return
		}

		password := c.GetHeader("X-Tournament-Password")
		if password != t.Password {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid tournament password"})
			c.Abort()
			return
		}

		c.Next()
	}
}

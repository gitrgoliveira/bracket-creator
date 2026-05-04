package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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
			if c.Request.Method == http.MethodPut && c.FullPath() == "/api/tournament" {
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

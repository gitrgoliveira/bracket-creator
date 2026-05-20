package mobileapp

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

type announcementRequest struct {
	Message         string `json:"message"`
	DurationMinutes int    `json:"durationMinutes"`
}

func RegisterAnnouncementHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
	// POST /api/tournament/announce is protected (requires admin credentials)
	r.POST("/tournament/announce", func(c *gin.Context) {
		var req announcementRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
			return
		}

		trimmedMsg := strings.TrimSpace(req.Message)
		if trimmedMsg == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Announcement message cannot be empty"})
			return
		}

		if utf8.RuneCountInString(trimmedMsg) > 200 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Announcement message cannot exceed 200 characters"})
			return
		}

		validDurations := map[int]bool{5: true, 10: true, 15: true, 30: true}
		if !validDurations[req.DurationMinutes] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Duration must be 5, 10, 15, or 30 minutes"})
			return
		}

		duration := time.Duration(req.DurationMinutes) * time.Minute
		ann := store.AnnouncementStore().Set(trimmedMsg, duration)

		// Broadcast new announcement to all connected SSE clients
		hub.Broadcast(EventAnnouncement, ann)

		c.JSON(http.StatusOK, ann)
	})
}

func RegisterPublicAnnouncementHandlers(r *gin.RouterGroup, store *state.Store) {
	// GET /api/tournament/announcement is public
	r.GET("/tournament/announcement", func(c *gin.Context) {
		ann := store.AnnouncementStore().Get()
		if ann == nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, ann)
	})
}

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

// Allowed announcement TTLs. Package-level so the validation map isn't
// reallocated on every POST /api/tournament/announce.
var validAnnouncementDurations = map[int]bool{5: true, 10: true, 15: true, 30: true}

func RegisterAnnouncementHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
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

		if !validAnnouncementDurations[req.DurationMinutes] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Duration must be 5, 10, 15, or 30 minutes"})
			return
		}

		ann, list := store.AnnouncementStore().Add(trimmedMsg, time.Duration(req.DurationMinutes)*time.Minute)
		hub.Broadcast(EventAnnouncement, list)
		c.JSON(http.StatusOK, ann)
	})

	// DELETE /api/announcements/:id — dismiss a single announcement.
	r.DELETE("/announcements/:id", func(c *gin.Context) {
		found, list := store.AnnouncementStore().Remove(c.Param("id"))
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "Announcement not found"})
			return
		}
		hub.Broadcast(EventAnnouncement, list)
		c.Status(http.StatusNoContent)
	})

	// DELETE /api/announcements — clear all announcements.
	r.DELETE("/announcements", func(c *gin.Context) {
		hub.Broadcast(EventAnnouncement, store.AnnouncementStore().Clear())
		c.Status(http.StatusNoContent)
	})
}

func RegisterPublicAnnouncementHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/tournament/announcements", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.AnnouncementStore().List())
	})

	// GET /api/tournament/announcement — legacy single-slot endpoint.
	r.GET("/tournament/announcement", func(c *gin.Context) {
		ann := store.AnnouncementStore().Get()
		if ann == nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, ann)
	})
}

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
	// POST /api/tournament/announce — add a new announcement to the queue.
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

		duration := time.Duration(req.DurationMinutes) * time.Minute
		ann := store.AnnouncementStore().Add(trimmedMsg, duration)

		// Broadcast full list snapshot so all clients stay in sync.
		hub.Broadcast(EventAnnouncement, store.AnnouncementStore().List())

		c.JSON(http.StatusOK, ann)
	})

	// DELETE /api/announcements/:id — dismiss a single announcement.
	r.DELETE("/announcements/:id", func(c *gin.Context) {
		id := c.Param("id")
		if !store.AnnouncementStore().Remove(id) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Announcement not found"})
			return
		}
		hub.Broadcast(EventAnnouncement, store.AnnouncementStore().List())
		c.Status(http.StatusNoContent)
	})

	// DELETE /api/announcements — clear all announcements.
	r.DELETE("/announcements", func(c *gin.Context) {
		store.AnnouncementStore().Clear()
		hub.Broadcast(EventAnnouncement, store.AnnouncementStore().List())
		c.Status(http.StatusNoContent)
	})
}

func RegisterPublicAnnouncementHandlers(r *gin.RouterGroup, store *state.Store) {
	// GET /api/tournament/announcements — returns the full active list (public).
	r.GET("/tournament/announcements", func(c *gin.Context) {
		list := store.AnnouncementStore().List()
		if list == nil {
			list = []state.Announcement{}
		}
		c.JSON(http.StatusOK, list)
	})

	// GET /api/tournament/announcement — legacy single-slot endpoint; kept for
	// any clients that haven't migrated to the list endpoint.
	r.GET("/tournament/announcement", func(c *gin.Context) {
		ann := store.AnnouncementStore().Get()
		if ann == nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, ann)
	})
}

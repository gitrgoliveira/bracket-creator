package mobileapp

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// announcementRequest is a court call as the device made it: the id it minted
// (a lowercase UUID v4) and when it was made (modifiedAt, server-relative ms).
// Both are required: the id is how a resend is known, and the made-at sets the
// call's expiry and orders it against an identical call and a clear.
type announcementRequest struct {
	ID              string `json:"id"`
	Message         string `json:"message"`
	DurationMinutes int    `json:"durationMinutes"`
	ModifiedAt      int64  `json:"modifiedAt"`
}

// announcementClearRequest is a clear as the device made it: everything made
// at or before modifiedAt is taken down.
type announcementClearRequest struct {
	ModifiedAt int64 `json:"modifiedAt"`
}

// announcementAnswer is the call as stored, with recorded set when nothing
// changed (a known id, an older identical call, a call made before the last
// clear) and expired set when its time was over on arrival.
type announcementAnswer struct {
	state.Announcement
	Recorded bool `json:"recorded,omitempty"`
	Expired  bool `json:"expired,omitempty"`
}

// Allowed announcement TTLs. Package-level so the validation map isn't
// reallocated on every POST /api/tournament/announce.
var validAnnouncementDurations = map[int]bool{5: true, 10: true, 15: true, 30: true}

// announcementMadeAt reads a request's made-at: 400 when it is missing, the
// clock_skew answer when it is too far ahead of the server. ok is false when
// an answer was written.
func announcementMadeAt(c *gin.Context, modifiedAt int64) (time.Time, bool) {
	if modifiedAt <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "modifiedAt is required"})
		return time.Time{}, false
	}
	if serverNowMs, aheadMs, refuse := clientClockSkew(modifiedAt); refuse {
		logClockSkewRefusal(c, aheadMs, "")
		c.JSON(http.StatusOK, gin.H{
			"applied":     false,
			"reason":      "clock_skew",
			"serverNowMs": serverNowMs,
			"message":     "Not sent: this device's clock appears to be ahead of the server. The app will resync its clock and send it again.",
		})
		return time.Time{}, false
	}
	return time.UnixMilli(clampClientModifiedAt(modifiedAt)), true
}

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

		if !state.IsClientMemberID(req.ID) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Announcement id must be a lowercase UUID v4"})
			return
		}

		madeAt, ok := announcementMadeAt(c, req.ModifiedAt)
		if !ok {
			return
		}

		ann, outcome, list := store.AnnouncementStore().Add(req.ID, trimmedMsg, madeAt, time.Duration(req.DurationMinutes)*time.Minute)
		if outcome == state.AnnounceAdded {
			hub.Broadcast(EventAnnouncement, list)
		}
		c.JSON(http.StatusOK, announcementAnswer{
			Announcement: ann,
			Recorded:     outcome == state.AnnounceRecorded,
			Expired:      outcome == state.AnnounceExpired,
		})
	})

	// DELETE /api/announcements/:id, dismiss a single announcement; 200 with
	// the list left showing, 404 when it is already gone. The id is the key: a
	// body (the outbox's modifiedAt and tags) is not read.
	r.DELETE("/announcements/:id", func(c *gin.Context) {
		found, list := store.AnnouncementStore().Remove(c.Param("id"))
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "Announcement not found"})
			return
		}
		hub.Broadcast(EventAnnouncement, list)
		c.JSON(http.StatusOK, list)
	})

	// DELETE /api/announcements, clear every call made at or before modifiedAt;
	// 200 with the list left showing.
	r.DELETE("/announcements", func(c *gin.Context) {
		var req announcementClearRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
			return
		}
		madeAt, ok := announcementMadeAt(c, req.ModifiedAt)
		if !ok {
			return
		}
		list, changed := store.AnnouncementStore().Clear(madeAt)
		if changed {
			hub.Broadcast(EventAnnouncement, list)
		}
		c.JSON(http.StatusOK, list)
	})
}

func RegisterPublicAnnouncementHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/tournament/announcements", func(c *gin.Context) {
		c.JSON(http.StatusOK, store.AnnouncementStore().List())
	})

	// GET /api/tournament/announcement, legacy single-slot endpoint.
	r.GET("/tournament/announcement", func(c *gin.Context) {
		ann := store.AnnouncementStore().Get()
		if ann == nil {
			c.Status(http.StatusNoContent)
			return
		}
		c.JSON(http.StatusOK, ann)
	})
}

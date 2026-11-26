package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// The court control routes (bc-sseq FR9), in the password group and listed
// in isSelfRunMainGatedConfigRoute, so they are the organiser's in a
// self-run tournament too:
//
//	GET    /api/courts/:court/control  who runs the shiaijo, as anyone sees it
//	PUT    /api/courts/:court/control  {tabId, pageId, deviceId, take, takenAt, device, keep}: a claim, a heartbeat or a take
//	DELETE /api/courts/:court/control  {tabId, pageId}: the console's release
//
// Each answers CourtControlAnswer. A shiaijo the tournament does not list is
// 404 unknown_court.

// courtClaimRequest is the PUT's body. The ids are lowercase UUID v4s the
// console mints; device is one of the device words (anything else reads
// "other"); takenAt is required with take; keep is an optional list of other
// shiaijo this tab must keep holding (at most helper.MaxCourts string entries).
type courtClaimRequest struct {
	TabID    string            `json:"tabId"`
	PageID   string            `json:"pageId"`
	DeviceID string            `json:"deviceId"`
	Take     bool              `json:"take"`
	TakenAt  int64             `json:"takenAt"`
	Device   string            `json:"device"`
	Keep     []json.RawMessage `json:"keep"`
}

// courtReleaseRequest is the DELETE's body.
type courtReleaseRequest struct {
	TabID  string `json:"tabId"`
	PageID string `json:"pageId"`
}

const (
	courtControlUnknownCode    = "unknown_court"
	courtControlBadRequestCode = "invalid_court_control"
)

// decodeCourtKeep checks that keep has at most helper.MaxCourts entries and
// every entry is a JSON string (encoding/json otherwise decodes null inside
// []string as "").
func decodeCourtKeep(raw []json.RawMessage) ([]string, bool) {
	if len(raw) == 0 {
		return nil, true
	}
	if len(raw) > helper.MaxCourts {
		return nil, false
	}
	out := make([]string, len(raw))
	for i, item := range raw {
		trimmed := bytes.TrimSpace(item)
		if len(trimmed) == 0 || trimmed[0] != '"' {
			return nil, false
		}
		if err := json.Unmarshal(trimmed, &out[i]); err != nil {
			return nil, false
		}
	}
	return out, true
}

// RegisterCourtControlHandlers wires the court control routes.
func RegisterCourtControlHandlers(r *gin.RouterGroup, courts CourtControls) {
	r.GET("/courts/:court/control", func(c *gin.Context) {
		a, err := courts.Look(c.Param("court"))
		answerCourtControl(c, a, err)
	})

	r.PUT("/courts/:court/control", func(c *gin.Context) {
		var req courtClaimRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondCourtControlBadRequest(c)
			return
		}
		keep, ok := decodeCourtKeep(req.Keep)
		if !ok {
			respondCourtControlBadRequest(c)
			return
		}
		a, err := courts.Claim(CourtClaim{
			Court: c.Param("court"), TabID: req.TabID, DeviceID: req.DeviceID, PageID: req.PageID,
			Device: req.Device, Take: req.Take, TakenAt: req.TakenAt, Keep: keep,
		})
		answerCourtControl(c, a, err)
	})

	r.DELETE("/courts/:court/control", func(c *gin.Context) {
		var req courtReleaseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondCourtControlBadRequest(c)
			return
		}
		a, err := courts.Release(c.Param("court"), req.TabID, req.PageID)
		answerCourtControl(c, a, err)
	})
}

// answerCourtControl answers a court control route: the control as the
// caller sees it, or the refusal err names.
func answerCourtControl(c *gin.Context, a CourtControlAnswer, err error) {
	switch {
	case err == nil:
		c.JSON(http.StatusOK, a)
	case errors.Is(err, errUnknownCourt):
		c.JSON(http.StatusNotFound, gin.H{"code": courtControlUnknownCode, "error": "This tournament has no such shiaijo."})
	case errors.Is(err, errCourtControlBadRequest):
		respondCourtControlBadRequest(c)
	default:
		internalError(c, err)
	}
}

// respondCourtControlBadRequest answers a body the court console does not
// send: ids that are not lowercase UUID v4s, a take with no takenAt, or a
// keep list with more than 16 entries or a non-string entry.
func respondCourtControlBadRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": courtControlBadRequestCode, "error": "The court control request is not one a court console sends. Reload the page and try again."})
}

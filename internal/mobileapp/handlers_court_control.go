package mobileapp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// The court control routes (bc-sseq FR9), in the password group and listed
// in isSelfRunMainGatedConfigRoute, so they are the organiser's in a
// self-run tournament too:
//
//	GET    /api/courts/:court/control  who runs the shiaijo, as anyone sees it
//	PUT    /api/courts/:court/control  {tabId, pageId, deviceId, take, takenAt, device}: a claim, a heartbeat or a take
//	DELETE /api/courts/:court/control  {tabId, pageId}: the console's release
//
// Each answers CourtControlAnswer. A shiaijo the tournament does not list is
// 404 unknown_court.

// courtClaimRequest is the PUT's body. The ids are lowercase UUID v4s the
// console mints; device is one of the device words (anything else reads
// "other"); takenAt is required with take.
type courtClaimRequest struct {
	TabID    string `json:"tabId"`
	PageID   string `json:"pageId"`
	DeviceID string `json:"deviceId"`
	Take     bool   `json:"take"`
	TakenAt  int64  `json:"takenAt"`
	Device   string `json:"device"`
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
		a, err := courts.Claim(CourtClaim{
			Court: c.Param("court"), TabID: req.TabID, DeviceID: req.DeviceID, PageID: req.PageID,
			Device: req.Device, Take: req.Take, TakenAt: req.TakenAt,
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
// send: ids that are not lowercase UUID v4s, or a take with no takenAt.
func respondCourtControlBadRequest(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{"code": courtControlBadRequestCode, "error": "The court control request is not one a court console sends. Reload the page and try again."})
}

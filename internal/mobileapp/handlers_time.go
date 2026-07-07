package mobileapp

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// RegisterTimeHandlers wires GET /api/time: a stateless, unauthenticated
// endpoint returning the server's current wall-clock time in unix milliseconds.
//
// Clients (notably shiaijo operator tablets) call it on connect to learn the
// server-clock offset, then stamp every score/override write in SERVER-RELATIVE
// time. That lets the field-level reconciliation (mp-y3nk) compare change
// timestamps across devices by ONE clock, so an offline court reconnecting later
// does not spuriously win purely because its tablet clock is fast. The endpoint
// reads no state and holds no auth requirement, matching the schedule-estimator
// contract, so it serves both `make run` and `make run-mobile` frontends.
func RegisterTimeHandlers(r *gin.RouterGroup) {
	r.GET("/time", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"nowMs": time.Now().UnixMilli()})
	})
}

// modifiedAtMaxSkewMs bounds how far into the future a client-supplied
// server-relative ModifiedAt may be before it is rejected. A legitimate offline
// write is stamped at action time (at or before "now" in the server frame), so
// any far-future value is a buggy or hostile clock. 5 minutes comfortably covers
// real client-clock drift.
const modifiedAtMaxSkewMs = 5 * 60 * 1000

// clampClientModifiedAt sanitises a client-supplied ModifiedAt for the timestamp
// last-write-wins guard (mp-y3nk). A negative value, or one more than
// modifiedAtMaxSkewMs into the future, is untrustworthy: honouring it would let a
// client FREEZE a match by making every subsequent legitimate write look
// "older" and be dropped. Such values fall back to 0, which the guard treats as
// unstamped (arrival-order) and is always safe.
func clampClientModifiedAt(v int64) int64 {
	if v < 0 || v > time.Now().UnixMilli()+modifiedAtMaxSkewMs {
		return 0
	}
	return v
}

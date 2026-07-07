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

package mobileapp

import (
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
)

// RegisterScheduleHandlers wires the stateless schedule estimator
// endpoint under r. T147a, T152a, the endpoint reads no state and
// holds no auth requirement so it can serve both the CLI web UI
// (`make run` mode) and the mobile-app frontend (`make run-mobile`
// mode) with one implementation. The web/js/time_estimator.js fetch
// caller is the canonical consumer; deck/admin renderers may also
// hit it for "how long will this take" hints.
//
// FR-059, SC-005, NFR-004.
func RegisterScheduleHandlers(r *gin.RouterGroup) {
	r.GET("/schedule/estimate", scheduleEstimateHandler)
}

// scheduleEstimateHandler parses GET /api/schedule/estimate query
// params, delegates to engine.EstimateSchedule, and returns JSON.
//
// Required query params:
//   - matchDuration: float, on-clock minutes per match (per bout); must be > 0
//   - multiplier:    float, clock→elapsed multiplier (e.g. 1.5)
//   - courts:        int >= 1, number of parallel courts
//
// Optional query params:
//   - numMatches:        int, total matches (default 1)
//   - teamSize:          int, 0 = individual, >0 = team
//   - boutsPerTeamMatch: int, used when teamSize > 0
//   - buffer:            int, slowest-court buffer % (default 0)
//   - ceremonyMinutes:   int, ceremony block minutes (default 0)
//
// Returns 400 when any required param is missing or unparsable, 200
// with ScheduleEstimate JSON otherwise.
func scheduleEstimateHandler(c *gin.Context) {
	matchDurationStr := c.Query("matchDuration")
	multiplierStr := c.Query("multiplier")
	courtsStr := c.Query("courts")

	if matchDurationStr == "" || multiplierStr == "" || courtsStr == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "matchDuration, multiplier, and courts are required query params",
		})
		return
	}

	matchDuration, err := strconv.ParseFloat(matchDurationStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "matchDuration must be a number"})
		return
	}
	if matchDuration <= 0 || math.IsNaN(matchDuration) || math.IsInf(matchDuration, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "matchDuration must be a positive finite number"})
		return
	}
	multiplier, err := strconv.ParseFloat(multiplierStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multiplier must be a number"})
		return
	}
	if multiplier <= 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "multiplier must be a positive finite number"})
		return
	}
	courts, err := strconv.Atoi(courtsStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "courts must be an integer"})
		return
	}
	// Reject hostile/garbage `courts` values up front rather than
	// silently clamping in the engine, the operator deserves a clear
	// 400 if their UI sends nonsense. engine.MaxCourts mirrors the
	// CLI's A–Z (26) cap; this guard also closes the
	// CodeQL go/uncontrolled-allocation-size finding on the engine's
	// slice allocation.
	if courts < 1 || courts > engine.MaxCourts {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "courts must be between 1 and 26",
		})
		return
	}

	// Optional fields default to 0/1; intDefault clamps parse failures
	// silently so a malformed optional param doesn't 400 the whole
	// request (the caller's UI is unlikely to send garbage on purpose
	// here and stricter validation belongs in the body-shape PRs).
	in := engine.EstimateInput{
		MatchDurationClockMinutes: matchDuration,
		Multiplier:                multiplier,
		NumMatches:                queryIntDefault(c, "numMatches", 1),
		NumCourts:                 courts,
		TeamSize:                  queryIntDefault(c, "teamSize", 0),
		BoutsPerTeamMatch:         queryIntDefault(c, "boutsPerTeamMatch", 0),
		SlowestCourtBufferPct:     queryIntDefault(c, "buffer", 0),
		CeremonyMinutes:           queryIntDefault(c, "ceremonyMinutes", 0),
	}

	c.JSON(http.StatusOK, engine.EstimateSchedule(in))
}

// queryIntDefault returns the parsed int value of c.Query(key), or
// def when the param is empty or unparsable. Used for optional
// schedule-estimator inputs where a malformed value should silently
// fall back rather than 400 the whole endpoint.
func queryIntDefault(c *gin.Context, key string, def int) int {
	raw := c.Query(key)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

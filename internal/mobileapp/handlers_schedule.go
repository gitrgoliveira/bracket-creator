package mobileapp

import (
	"fmt"
	"math"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
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
//   - multiplier:    float, clock→elapsed multiplier (e.g. 1.5); must be > 0
//   - courts:        int >= 1, number of parallel courts
//
// Optional query params:
//   - numMatches:        int, total matches (default 1)
//   - teamSize:          int, 0 = individual, >0 = team
//   - boutsPerTeamMatch: int, used when teamSize > 0
//   - teamMatchType:     string, "kachinuki" widens the estimate into a
//     best/average/worst range (variable bout count, mp-gmcg); any
//     other value (or absent) keeps a constant bout count
//   - buffer:            int, slowest-court buffer % (default 0; lenient — a
//     malformed value falls back to the default rather than 400ing)
//   - ceremonyMinutes:   int, ceremony block minutes (default 0; lenient — a
//     malformed value falls back to the default rather than 400ing)
//
// Returns 400 when a required param is missing, or when a provided param is
// rejected by its stated validation (parse, range, or per-param constraint) —
// the lenient buffer/ceremonyMinutes never reject (a malformed value falls back
// to its default). 200 with ScheduleEstimate JSON otherwise.
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
	// CLI's court cap; this guard also closes the
	// CodeQL go/uncontrolled-allocation-size finding on the engine's
	// slice allocation.
	if courts < 1 || courts > engine.MaxCourts {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("courts must be between 1 and %d", engine.MaxCourts),
		})
		return
	}

	// teamSize / boutsPerTeamMatch are load-bearing on this branch (they set
	// the bout count that scales the whole estimate), so — unlike the truly
	// optional buffer/ceremony scalars — they get STRICT parsing with an
	// explicit 0..MaxTeamSize 400. queryIntDefault would map a parse failure
	// (garbage, OR a value past int64 range) to the default 0 and answer 200
	// with an INDIVIDUAL-formula estimate for what the client asked as a team
	// question — the exact silent fallthrough the spec's "out of range → 400"
	// contract forbids (mp-gmcg review). 0 stays legal (individual-match default).
	teamSize, ok := parseOptionalBoundedInt(c, "teamSize", 0, engine.MaxTeamSize)
	if !ok {
		return
	}
	boutsPerTeamMatch, ok := parseOptionalBoundedInt(c, "boutsPerTeamMatch", 0, engine.MaxTeamSize)
	if !ok {
		return
	}
	// numMatches is load-bearing the same way (it scales the WHOLE estimate), so
	// it gets the same strict parse — garbage/overflow defaulting to 1 would
	// answer 200 with a one-match estimate for a client asking about a full day
	// (mp-gmcg review). Default 1 when absent. (The buffer/ceremonyMinutes
	// contrast lives at their parse site below.)
	numMatches, ok := parseOptionalBoundedInt(c, "numMatches", 1, engine.MaxScheduleCount)
	if !ok {
		return
	}

	// buffer/ceremonyMinutes stay on the silent queryIntDefault path (both
	// default 0): they only pad an already-computed number, so a malformed
	// value falling back to 0 is a harmless answer, not a wrong one — unlike
	// the bounded teamSize/boutsPerTeamMatch/numMatches above, which scale the
	// estimate and therefore 400 on garbage or out-of-range input.
	in := engine.EstimateInput{
		MatchDurationClockMinutes: matchDuration,
		Multiplier:                multiplier,
		NumMatches:                numMatches,
		NumCourts:                 courts,
		TeamSize:                  teamSize,
		BoutsPerTeamMatch:         boutsPerTeamMatch,
		SlowestCourtBufferPct:     queryIntDefault(c, "buffer", 0),
		CeremonyMinutes:           queryIntDefault(c, "ceremonyMinutes", 0),
		// teamMatchType widens the estimate into a best/average/worst range for
		// kachinuki. No first-party caller passes it yet (Overview/Settings use
		// the per-competition estimate); it is retained for the planned
		// hypothetical estimator page (mp-lw5p) and documented in the OpenAPI spec.
		Kachinuki: c.Query("teamMatchType") == string(state.TeamMatchTypeKachinuki),
	}

	c.JSON(http.StatusOK, engine.EstimateSchedule(in))
}

// parseOptionalBoundedInt reads an OPTIONAL query int constrained to [0, max].
// An absent/empty value returns (def, true). A present value that is unparsable
// (garbage OR past int64 range, both of which strconv.Atoi errors on) or out of
// range writes a 400 and returns ok=false, so the caller stops. This is the
// strict counterpart to queryIntDefault, for params whose silent fallback to
// the default would be a WRONG answer rather than a harmless one — the OpenAPI
// contract promises a 400 for out-of-range here (mp-gmcg review). The bound is a
// parameter so the message never hardcodes a number that could drift.
func parseOptionalBoundedInt(c *gin.Context, key string, def, max int) (int, bool) {
	raw := c.Query(key)
	if raw == "" {
		return def, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 || v > max {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%s must be between 0 and %d", key, max)})
		return 0, false
	}
	return v, true
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

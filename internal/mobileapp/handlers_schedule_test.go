package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScheduleEstimateEndpoint covers T147a: GET /api/schedule/estimate
// is a stateless, unauthenticated endpoint. Required params return 200
// with a ScheduleEstimate JSON body; missing required params return 400;
// no X-Tournament-Password header is required for success.
//
// FR-059, NFR-004, SC-005.
func TestScheduleEstimateEndpoint(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	t.Run("valid params returns 200 and ScheduleEstimate JSON", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&numMatches=20&courts=2&buffer=10",
			nil)
		// Deliberately no X-Tournament-Password, endpoint is public.
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Greater(t, resp.TotalDurationMinutes, 0, "expected non-zero total")
		assert.Len(t, resp.PerCourtMinutes, 2, "expected 2 court entries")
		// Excel-parity sanity: 20 * 3 * 1.5 / 2 * 1.10 = 49.5 → ~50.
		delta := resp.TotalDurationMinutes - 50
		if delta < 0 {
			delta = -delta
		}
		assert.LessOrEqual(t, delta, 3, "expected within ~5%% of 50, got %d", resp.TotalDurationMinutes)
	})

	t.Run("team-match params include bouts in estimate", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&numMatches=1&courts=1&teamSize=5&boutsPerTeamMatch=5",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		// 5*3*1.5 + 4 = 26.5 → 26 or 27
		assert.True(t, resp.TotalDurationMinutes >= 26 && resp.TotalDurationMinutes <= 28,
			"expected 26-28 for 5-bout team match, got %d", resp.TotalDurationMinutes)
	})

	t.Run("missing matchDuration returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// mp-gmcg review: teamSize / boutsPerTeamMatch are load-bearing (they set
	// the bout count that scales the estimate), so an unbounded value overflows
	// the duration math into a negative or inverted range. Reject out-of-range
	// values with a 400, mirroring the courts guard, rather than answering 200
	// with a garbage duration.
	t.Run("out-of-range teamSize returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&teamSize=9223372036854775807",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("out-of-range boutsPerTeamMatch returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&teamSize=5&boutsPerTeamMatch=100000",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// mp-gmcg review: a teamSize PAST int64 range must 400, not silently fall
	// back to 0 and answer 200 with an individual-formula estimate (what
	// queryIntDefault did — strconv.Atoi errors on a 20-digit value → default).
	// The OpenAPI contract promises out-of-range → 400.
	t.Run("teamSize beyond int64 range returns 400 (not a silent 200)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&teamSize=99999999999999999999",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("garbage boutsPerTeamMatch returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&teamSize=5&boutsPerTeamMatch=lots",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// mp-gmcg review: numMatches scales the whole estimate, so garbage/overflow
	// must 400, not silently default to 1 and answer 200 with a one-match
	// estimate for a client asking about a full day.
	t.Run("numMatches beyond int64 range returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&numMatches=99999999999999999999",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("garbage numMatches returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&numMatches=loads",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// mp-gmcg review: assert the VALUE, not just the 200 — numMatches=0 is also
	// legal (bounds 0..MaxScheduleCount) and also 200, so a status-only check
	// would still pass if the absent default silently regressed from 1 to 0. The
	// absent-numMatches estimate must equal an explicit numMatches=1 estimate and
	// be positive (i.e. not the zero-duration 0-default).
	t.Run("absent numMatches still defaults to 1", func(t *testing.T) {
		absent := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1", nil)
		r.ServeHTTP(absent, req)
		require.Equal(t, http.StatusOK, absent.Code)

		explicit := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1&numMatches=1", nil)
		r.ServeHTTP(explicit, req2)
		require.Equal(t, http.StatusOK, explicit.Code)

		var absentResp, explicitResp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(absent.Body.Bytes(), &absentResp))
		require.NoError(t, json.Unmarshal(explicit.Body.Bytes(), &explicitResp))
		assert.Greater(t, absentResp.TotalDurationMinutes, 0, "absent numMatches must default to 1, not the zero-duration 0-default")
		assert.Equal(t, explicitResp.TotalDurationMinutes, absentResp.TotalDurationMinutes,
			"absent numMatches must estimate identically to numMatches=1")
	})

	t.Run("teamSize at the MaxTeamSize boundary is accepted", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			fmt.Sprintf("/api/schedule/estimate?matchDuration=3&multiplier=1.5&numMatches=1&courts=1&teamSize=%d", engine.MaxTeamSize),
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Greater(t, resp.TotalDurationMinutes, 0, "a boundary team size still yields a positive estimate")
	})

	t.Run("missing multiplier returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing courts returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("unparsable matchDuration returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=abc&multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("fractional matchDuration is accepted", func(t *testing.T) {
		// 20 * 3.5 * 1.5 / 2 * 1.10 = 57.75 → 58.
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3.5&multiplier=1.5&numMatches=20&courts=2&buffer=10",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		delta := resp.TotalDurationMinutes - 58
		if delta < 0 {
			delta = -delta
		}
		assert.LessOrEqual(t, delta, 2, "expected ~58 for fractional duration, got %d", resp.TotalDurationMinutes)
	})

	t.Run("matchDuration=NaN returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=NaN&multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "matchDuration")
	})

	t.Run("matchDuration=Inf returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=Inf&multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "matchDuration")
	})

	t.Run("no auth header still returns 200 (endpoint is public)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=1",
			nil)
		// Intentionally no X-Tournament-Password header.
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("matchDuration=0 returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=0&multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "matchDuration")
	})

	t.Run("negative matchDuration returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=-5&multiplier=1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "matchDuration")
	})

	t.Run("multiplier=NaN returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=NaN&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "multiplier")
	})

	t.Run("multiplier=Inf returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=Inf&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "multiplier")
	})

	t.Run("multiplier=0 returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=0&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "multiplier")
	})

	t.Run("negative multiplier returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=-1.5&courts=2",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "multiplier")
	})

	t.Run("courts=0 returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=0",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), fmt.Sprintf("courts must be between 1 and %d", engine.MaxCourts))
	})

	t.Run("courts=27 returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=27",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), fmt.Sprintf("courts must be between 1 and %d", engine.MaxCourts))
	})
}

func TestParseRequiredBoundedInt(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		url        string
		min        int
		max        int
		wantVal    int
		wantOK     bool
		wantStatus int
		wantError  string
	}{
		{
			name:       "missing",
			url:        "/test",
			min:        1,
			max:        100,
			wantVal:    0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
			wantError:  "participants is required",
		},
		{
			name:       "empty",
			url:        "/test?participants=",
			min:        1,
			max:        100,
			wantVal:    0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
			wantError:  "participants is required",
		},
		{
			name:       "non-integer",
			url:        "/test?participants=abc",
			min:        1,
			max:        100,
			wantVal:    0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
			wantError:  "participants must be between 1 and 100",
		},
		{
			name:       "below min",
			url:        "/test?participants=0",
			min:        1,
			max:        100,
			wantVal:    0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
			wantError:  "participants must be between 1 and 100",
		},
		{
			name:       "above max",
			url:        "/test?participants=101",
			min:        1,
			max:        100,
			wantVal:    0,
			wantOK:     false,
			wantStatus: http.StatusBadRequest,
			wantError:  "participants must be between 1 and 100",
		},
		{
			name:       "at min",
			url:        "/test?participants=1",
			min:        1,
			max:        100,
			wantVal:    1,
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
		{
			name:       "at max",
			url:        "/test?participants=100",
			min:        1,
			max:        100,
			wantVal:    100,
			wantOK:     true,
			wantStatus: http.StatusOK,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			req, err := http.NewRequest(http.MethodGet, tc.url, nil)
			require.NoError(t, err)
			c.Request = req

			val, ok := parseRequiredBoundedInt(c, "participants", tc.min, tc.max)
			assert.Equal(t, tc.wantOK, ok)
			assert.Equal(t, tc.wantVal, val)

			if !tc.wantOK {
				assert.Equal(t, tc.wantStatus, w.Code)
				var body map[string]string
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
				assert.Equal(t, tc.wantError, body["error"])
			} else {
				assert.Equal(t, 200, w.Code)
			}
		})
	}
}

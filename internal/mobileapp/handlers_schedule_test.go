package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
		assert.Contains(t, w.Body.String(), "courts must be between 1 and 26")
	})

	t.Run("courts=27 returns 400", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET",
			"/api/schedule/estimate?matchDuration=3&multiplier=1.5&courts=27",
			nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "courts must be between 1 and 26")
	})
}

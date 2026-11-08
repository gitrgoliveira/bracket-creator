package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupDraftEstimateRouter builds the real router (so the admin group, its
// AuthMiddleware and the self-run gate all apply) over a two-day officiated
// tournament with two shiaijo.
func setupDraftEstimateRouter(t *testing.T) http.Handler {
	t.Helper()
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Draft Estimate Test",
		Date:         "01-06-2026",
		DurationDays: 2,
		Venue:        "Dojo",
		Courts:       []string{"A", "B"},
		Password:     "main-pw",
		Mode:         state.TournamentModeOfficiated,
	}))
	return setupSelfRunRouter(t, store, NewFileVerifier(store))
}

func getDraftEstimate(t *testing.T, r http.Handler, query string, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/schedule/draft-estimate?"+query, nil)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeDraftEstimate(t *testing.T, w *httptest.ResponseRecorder) draftEstimateResponse {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp draftEstimateResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	return resp
}

func draftErrorSentence(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	return body["error"]
}

func TestDraftEstimate_Formats(t *testing.T) {
	r := setupDraftEstimateRouter(t)

	t.Run("knockout", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=8&format=knockout", "main-pw"))
		assert.Equal(t, 0, resp.PoolMatches)
		assert.Equal(t, 7, resp.KnockoutMatches)
		assert.Positive(t, resp.TotalDurationMinutes)
		assert.Positive(t, resp.Timing.Multiplier)
		assert.NotNil(t, resp.Warnings)
		assert.Empty(t, resp.Warnings)
	})

	t.Run("mixed", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=12&format=mixed&poolSize=3&poolWinners=2", "main-pw"))
		assert.Equal(t, 12, resp.PoolMatches, "four pools of three, three bouts each")
		assert.Positive(t, resp.KnockoutMatches)
		assert.Empty(t, resp.Warnings)
	})

	t.Run("mixed with no pool size takes the create's default", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=12&format=mixed", "main-pw"))
		assert.Positive(t, resp.PoolMatches)
	})

	t.Run("league", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=5&format=league", "main-pw"))
		assert.Equal(t, 10, resp.PoolMatches, "five entrants, every pair once")
		assert.Equal(t, 0, resp.KnockoutMatches)
	})
}

func TestDraftEstimate_WarningsAlwaysAnArray(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	w := getDraftEstimate(t, r, "participants=8&format=knockout", "main-pw")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	assert.JSONEq(t, `[]`, string(raw["warnings"]))
	assert.Contains(t, raw, "timing")
	assert.Contains(t, raw, "poolMatches")
	assert.Contains(t, raw, "knockoutMatches")
}

func TestDraftEstimate_ParticipantsRefusals(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	cases := []struct{ name, query, want string }{
		{"missing", "format=knockout", "participants is required"},
		{"empty", "participants=&format=knockout", "participants is required"},
		{"zero", "participants=0&format=knockout", "participants must be between 1 and 1024"},
		{"above the cap", "participants=1025&format=knockout", "participants must be between 1 and 1024"},
		{"not a number", "participants=abc&format=knockout", "participants must be between 1 and 1024"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, draftErrorSentence(t, getDraftEstimate(t, r, tc.query, "main-pw")))
		})
	}
	t.Run("at the cap", func(t *testing.T) {
		decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=1024&format=knockout", "main-pw"))
	})
}

func TestDraftEstimate_Courts(t *testing.T) {
	r := setupDraftEstimateRouter(t)

	for _, q := range []string{"0", "17", "abc", "-1"} {
		t.Run("refuses courts="+q, func(t *testing.T) {
			assert.Equal(t, "courts must be between 1 and 16",
				draftErrorSentence(t, getDraftEstimate(t, r, "participants=16&format=knockout&courts="+q, "main-pw")))
		})
	}

	t.Run("absent takes the tournament's shiaijo", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=16&format=knockout", "main-pw"))
		assert.Len(t, resp.PerCourtMinutes, 2)
	})

	t.Run("a count prices that many shiaijo", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=16&format=knockout&courts=4", "main-pw"))
		assert.Len(t, resp.PerCourtMinutes, 4)
		assert.Empty(t, resp.Warnings)
	})

	t.Run("no power-of-two refusal", func(t *testing.T) {
		decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=16&format=knockout&courts=3", "main-pw"))
	})
}

func TestDraftEstimate_TooManyShiaijoWarns(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=4&format=knockout&courts=8", "main-pw"))
	require.Len(t, resp.Warnings, 1)
	assert.Equal(t, DraftWarning{
		Code:         "too_many_shiaijo",
		Participants: 4,
		Usable:       engine.MaxUsefulCourts(4),
		Chosen:       8,
	}, resp.Warnings[0])
	assert.Equal(t, 3, resp.KnockoutMatches, "the estimate is still answered")
	assert.Positive(t, resp.TotalDurationMinutes)

	t.Run("exactly the usable count does not warn", func(t *testing.T) {
		resp := decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=4&format=knockout&courts=2", "main-pw"))
		assert.Empty(t, resp.Warnings)
	})
}

func TestDraftEstimate_DateAndStartTime(t *testing.T) {
	r := setupDraftEstimateRouter(t)

	t.Run("a tournament day", func(t *testing.T) {
		decodeDraftEstimate(t, getDraftEstimate(t, r, "participants=8&format=knockout&date=02-06-2026&startTime=10:30", "main-pw"))
	})
	t.Run("outside the tournament", func(t *testing.T) {
		assert.Equal(t, "date must be one of the tournament days (01-06-2026 to 02-06-2026)",
			draftErrorSentence(t, getDraftEstimate(t, r, "participants=8&format=knockout&date=03-06-2026", "main-pw")))
	})
	t.Run("malformed date", func(t *testing.T) {
		w := getDraftEstimate(t, r, "participants=8&format=knockout&date=2026-06-01", "main-pw")
		var comp state.Competition
		comp.Date = "2026-06-01"
		assert.Equal(t, validateDateDMY(comp.Date).Error(), draftErrorSentence(t, w))
	})
	t.Run("malformed start time", func(t *testing.T) {
		assert.Equal(t, "startTime must be HH:MM",
			draftErrorSentence(t, getDraftEstimate(t, r, "participants=8&format=knockout&startTime=25:00", "main-pw")))
	})
}

func TestDraftEstimate_ShapeRefusals(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	cases := []struct{ name, query, want string }{
		{"unknown format", "participants=8&format=bogus", `unknown format "bogus"`},
		{"unknown pool format", "participants=8&format=mixed&poolFormat=bogus", `unknown poolFormat "bogus"`},
		{"swiss with no rounds", "participants=8&format=swiss", "swiss format requires swissRounds >= 1"},
		{"swiss rounds past the cap", fmt.Sprintf("participants=8&format=swiss&swissRounds=%d", engine.MaxSwissRounds+1),
			fmt.Sprintf("swissRounds must be at most %d", engine.MaxSwissRounds)},
		{"duration out of band", "participants=8&format=knockout&poolMatchDurationSeconds=1",
			fmt.Sprintf("match duration must be between %d and %d seconds", state.MinMatchDurationSeconds, state.MaxMatchDurationSeconds)},
		{"kachinuki under team size 2", "participants=8&format=knockout&kind=team&teamSize=1&teamMatchType=kachinuki",
			"kachinuki requires teamSize >= 2"},
		{"team size not a number", "participants=8&format=knockout&teamSize=x", "teamSize must be an integer"},
		{"bad boolean", "participants=8&format=knockout&naginata=maybe", "naginata must be a boolean"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, draftErrorSentence(t, getDraftEstimate(t, r, tc.query, "main-pw")))
		})
	}
}

// The largest Swiss draft the caps allow is still answered, so the cap is
// what bounds the estimate's work.
func TestDraftEstimate_LargestSwissIsAnswered(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	w := getDraftEstimate(t, r, fmt.Sprintf("participants=%d&format=swiss&swissRounds=%d", engine.MaxEstimateParticipants, engine.MaxSwissRounds), "main-pw")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A poolWinners past what a pool can hold is refused, not built: the
// qualifier skeleton grows with pools x poolWinners, and before the bound
// this query exhausted the server's memory. The create's own rule refuses it
// (state.Competition.ValidatePoolWinners), so the draft never reaches the draw.
func TestDraftEstimate_RefusesMorePoolWinnersThanAPoolHolds(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	w := getDraftEstimate(t, r, fmt.Sprintf("participants=%d&format=mixed&poolSize=3&poolSizeMode=max&poolWinners=300", engine.MaxEstimateParticipants), "main-pw")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, "poolWinners must be at most 3 for pools of 3 (got 300)", draftErrorSentence(t, w))
}

// A draft is judged by the create POST's own chain: a shape the create
// refuses is refused here with the same sentence.
func TestDraftEstimate_RefusesWhatTheCreateRefuses(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	assert.Equal(t, `kind: unknown kind "bogus" (expected "individual" or "team")`, draftErrorSentence(t, getDraftEstimate(t, r, "participants=8&format=knockout&kind=bogus", "main-pw")))
	assert.Equal(t, `poolSizeMode: unknown poolSizeMode "maxx" (expected "max" or "min")`, draftErrorSentence(t, getDraftEstimate(t, r, "participants=8&format=mixed&poolSize=3&poolSizeMode=maxx", "main-pw")))
	// Trimmed first, as the create trims, so a padded legal value passes.
	w := getDraftEstimate(t, r, "participants=8&format=mixed&poolSize=3&poolSizeMode=%20max%20", "main-pw")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

func TestDraftEstimate_RequiresThePassword(t *testing.T) {
	r := setupDraftEstimateRouter(t)
	assert.Equal(t, http.StatusUnauthorized, getDraftEstimate(t, r, "participants=8&format=knockout", "").Code)
	assert.Equal(t, http.StatusUnauthorized, getDraftEstimate(t, r, "participants=8&format=knockout", "wrong").Code)
}

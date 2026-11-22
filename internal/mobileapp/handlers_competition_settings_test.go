package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPUTCompetition_SettingsOnlyResponseIncludesPlayers pins the
// Copilot round-12 finding (#5): settings-only PUTs used to return
// `players: null` in the response because LoadCompetition doesn't
// populate Players from participants.csv. admin.jsx's
// `{ ...c, ...updated }` merge then pushed null into local state,
// crashing render paths that read `c.players.length`. The handler now
// loads the on-disk roster for the response when comp.Players == nil.
// TestCompetitionDurationSeconds_PersistAndNormalize covers mp-m5kf: the
// PUT settings path persists the canonical *Seconds fields to disk, and the
// GET read handlers normalize legacy whole-minute / single-field durations
// into *Seconds so the SPA always receives resolved values.
// A console left open across an upgrade still posts the pre-rename key. The
// decoder drops an unrecognised key, and the settings transform assigns
// KnockoutMatchDurationSeconds unconditionally, so before this guard the
// operator's stored duration was silently reset to unset. The retired FORMAT
// value in the same body already 400s, so the two retired inputs behaved in
// opposite ways: one loud, one destructive. Both are loud now.
func TestPUTCompetition_RetiredDurationKeyIsRefused(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	seed := state.Competition{
		ID: "stale-tab", Name: "Stale Tab", Date: "12-05-2026", Format: "mixed",
		PoolMatchDurationSeconds: 150, KnockoutMatchDurationSeconds: 240,
	}
	require.NoError(t, store.SaveCompetition(&seed))

	// The pre-rename bundle's body: it never names the current key. The literal
	// below is the POINT of the test -- do not "fix" it to the current spelling.
	body := []byte(`{"id":"stale-tab","name":"Stale Tab","date":"12-05-2026","format":"mixed","poolMatchDurationSeconds":150,"playoffMatchDurationSeconds":240}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/stale-tab", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "the retired key must be refused, not silently dropped")
	assert.Contains(t, w.Body.String(), "knockoutMatchDurationSeconds", "the error must name the current key")

	stored, err := store.LoadCompetition("stale-tab")
	require.NoError(t, err)
	assert.Equal(t, 240, stored.KnockoutMatchDurationSeconds,
		"the operator's stored duration must survive the refused write")
}

func TestCompetitionDurationSeconds_PersistAndNormalize(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. PUT with a sub-minute seconds value persists it verbatim to disk.
	seed := state.Competition{ID: "sec-comp", Name: "Sec Comp", Date: "12-05-2026", Format: "mixed"}
	require.NoError(t, store.SaveCompetition(&seed))
	body := []byte(`{"id":"sec-comp","name":"Sec Comp","date":"12-05-2026","format":"mixed","poolMatchDurationSeconds":150,"knockoutMatchDurationSeconds":210}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/sec-comp", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	stored, err := store.LoadCompetition("sec-comp")
	require.NoError(t, err)
	assert.Equal(t, 150, stored.PoolMatchDurationSeconds, "2m30s must persist to disk")
	assert.Equal(t, 210, stored.KnockoutMatchDurationSeconds)

	// 2. A legacy per-phase MINUTE competition is normalized to *Seconds on the
	//    single-GET read path (x60).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "legacy-min", Name: "Legacy Min", PoolMatchDurationSeconds: 180, KnockoutMatchDurationSeconds: 300,
	}))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/legacy-min", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var got state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, 180, got.PoolMatchDurationSeconds, "GET must back-fill 3 min -> 180s")
	assert.Equal(t, 300, got.KnockoutMatchDurationSeconds, "GET must back-fill 5 min -> 300s")

	// 3. A legacy SINGLE-field competition (only match_duration) is normalized
	//    to both per-phase *Seconds on the LIST read path, so the SPA's
	//    seconds/minutes resolver never sees a bare legacy value.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "legacy-single", Name: "Legacy Single", MatchDuration: 5,
	}))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var list []state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	var single *state.Competition
	for i := range list {
		if list[i].ID == "legacy-single" {
			single = &list[i]
		}
	}
	require.NotNil(t, single, "legacy-single must appear in the list")
	assert.Equal(t, 300, single.PoolMatchDurationSeconds, "list GET must normalize match_duration 5 -> 300s")
	assert.Equal(t, 300, single.KnockoutMatchDurationSeconds)
}

// TestValidateCompetitionDurations enforces the shiai band on the per-phase
// clock durations. A fat-fingered 3-second match drives the whole day's
// auto-schedule, so it is rejected outright rather than clamped. The check is a
// flat comparison with no reference to the stored value: state's legacy-duration
// migration clamps into the same band, so no stored duration can be out of range
// and there is nothing to grandfather.
func TestValidateCompetitionDurations(t *testing.T) {
	tests := []struct {
		name    string
		comp    state.Competition
		wantErr bool
	}{
		{"pool below floor", state.Competition{PoolMatchDurationSeconds: 3}, true},
		{"knockout below floor", state.Competition{KnockoutMatchDurationSeconds: state.MinMatchDurationSeconds - 1}, true},
		{"pool at floor", state.Competition{PoolMatchDurationSeconds: state.MinMatchDurationSeconds}, false},
		{"knockout at ceiling", state.Competition{KnockoutMatchDurationSeconds: state.MaxMatchDurationSeconds}, false},
		{"pool above ceiling", state.Competition{PoolMatchDurationSeconds: state.MaxMatchDurationSeconds + 1}, true},
		{"a valid sub-minute value passes", state.Competition{PoolMatchDurationSeconds: 150}, false},
		{"negative is rejected", state.Competition{PoolMatchDurationSeconds: -1}, true},
		// 0 is "unset, use the scheduler default" and must stay accepted:
		// otherwise clearing the field would fail to save.
		{"unset is allowed", state.Competition{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCompetitionDurations(&tt.comp)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestValidateCompetitionFormat_UnknownFormat verifies that unknown format
// strings are rejected.
func TestValidateCompetitionFormat_UnknownFormat(t *testing.T) {
	code, err := validateCompetitionFormat("garbage")
	assert.Error(t, err)
	assert.Equal(t, http.StatusBadRequest, code)
}

// TestPUTCompetition_CheckInEnabledPersists verifies that the settings-only
// PUT handler copies CheckInEnabled from the request body to the stored
// competition.  This was a regression: the settings merge path copied Naginata
// but omitted CheckInEnabled, so toggling check-in tracking appeared to save
// but reverted on refresh.
func TestPUTCompetition_CheckInEnabledPersists(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "checkin-persist"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Name: "CheckIn Test",
		Date: "12-05-2026",
	}))

	// Settings-only PUT, enable check-in tracking.
	body := []byte(`{"id":"checkin-persist","name":"CheckIn Test","date":"12-05-2026","checkInEnabled":true}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.True(t, saved.CheckInEnabled, "checkInEnabled must survive a settings-only PUT")

	// Disable it.
	body = []byte(`{"id":"checkin-persist","name":"CheckIn Test","date":"12-05-2026","checkInEnabled":false}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	saved, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.False(t, saved.CheckInEnabled, "checkInEnabled=false must also persist")
}

// TestPUTCompetition_NaginataStartedGuard is a Finding 8 regression test:
// the naginata flag may only be toggled before the competition starts. After
// start, flipping it would add or remove the bronze match while results are
// in flight, corrupting the bracket. The handler must return 400 and leave
// the flag unchanged.
func TestPUTCompetition_NaginataStartedGuard(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "naginata-guard"
	// Start the competition in an active state (not setup/draw-ready).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       cid,
		Name:     "Naginata Guard",
		Date:     "12-05-2026",
		Format:   state.CompFormatKnockout,
		Naginata: true,
		Status:   state.CompStatusKnockout, // past setup
	}))

	// Attempt to toggle Naginata=false on a started competition.
	body := []byte(`{"id":"naginata-guard","name":"Naginata Guard","date":"12-05-2026","format":"knockout","naginata":false}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equalf(t, http.StatusBadRequest, w.Code,
		"toggling naginata on a started competition must return 400; body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "naginata",
		"error message must mention naginata")

	// Confirm stored value is unchanged.
	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.True(t, saved.Naginata, "naginata flag must remain unchanged after rejected PUT")

	// Toggle is allowed on a setup (not yet started) competition.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       cid,
		Name:     "Naginata Guard",
		Date:     "12-05-2026",
		Format:   state.CompFormatKnockout,
		Naginata: true,
		Status:   state.CompStatusSetup,
	}))
	body = []byte(`{"id":"naginata-guard","name":"Naginata Guard","date":"12-05-2026","format":"knockout","naginata":false}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equalf(t, http.StatusOK, w.Code,
		"toggling naginata on a setup competition must succeed; body: %s", w.Body.String())

	saved, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.False(t, saved.Naginata, "naginata must be updated to false on setup competition")
}

func TestPUTCompetitionAwards(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "awards-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Awards Comp",
		Status: state.CompStatusComplete,
	}))

	t.Run("happy path: awards persisted + 200 + SSE emitted", func(t *testing.T) {
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Fighting Spirit", "recipientName": "Alice Yamada", "recipientDojo": "Shinjuku"},
				{"title": "Best Technique", "recipientName": "Bob Tanaka"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		// Parse response competition and check awards.
		var resp state.Competition
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.FightingSpiritAwards, 2)
		assert.Equal(t, "Fighting Spirit", resp.FightingSpiritAwards[0].Title)
		assert.Equal(t, "Alice Yamada", resp.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Shinjuku", resp.FightingSpiritAwards[0].RecipientDojo)
		assert.Equal(t, "Best Technique", resp.FightingSpiritAwards[1].Title)
		assert.Equal(t, "Bob Tanaka", resp.FightingSpiritAwards[1].RecipientName)
		assert.Equal(t, "", resp.FightingSpiritAwards[1].RecipientDojo)

		// Verify persistence.
		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.Len(t, saved.FightingSpiritAwards, 2)
		assert.Equal(t, "Alice Yamada", saved.FightingSpiritAwards[0].RecipientName)

		// Check SSE tournament_updated was broadcast.
		sawUpdate := false
		for range 4 {
			select {
			case msg := <-ch:
				if strings.Contains(msg.payload, `"type":"tournament_updated"`) {
					sawUpdate = true
				}
			default:
			}
		}
		assert.True(t, sawUpdate, "tournament_updated SSE event must be emitted after award save")
	})

	t.Run("clear awards: empty array persisted", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{"fightingSpiritAwards": []any{}})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Empty(t, saved.FightingSpiritAwards, "awards must be cleared")
	})

	t.Run("404 for unknown competition", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Ghost"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/no-such-comp/awards", bytes.NewBuffer(versionedAwardsBody(t, store, "no-such-comp", body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "competition not found")
	})

	t.Run("400: empty title rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "   ", "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "title is required")
	})

	t.Run("400: empty recipientName rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": ""},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "recipientName is required")
	})

	t.Run("400: over-cap count (21 awards) rejected", func(t *testing.T) {
		awards := make([]map[string]any, 21)
		for i := range awards {
			awards[i] = map[string]any{"title": "Spirit", "recipientName": fmt.Sprintf("Person %d", i)}
		}
		body, _ := json.Marshal(map[string]any{"fightingSpiritAwards": awards})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "fightingSpiritAwards must not exceed")
	})

	t.Run("400: title exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": strings.Repeat("x", MaxLenPlayerName+1), "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: recipientName exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": strings.Repeat("x", MaxLenPlayerName+1)},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: recipientDojo exceeds max length", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Alice", "recipientDojo": strings.Repeat("x", MaxLenPlayerDojo+1)},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("whitespace trimmed from awards before persisting", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "  Spirit  ", "recipientName": "  Carol  ", "recipientDojo": "  Osaka  "},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBuffer(versionedAwardsBody(t, store, cid, body)))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.Len(t, saved.FightingSpiritAwards, 1)
		assert.Equal(t, "Spirit", saved.FightingSpiritAwards[0].Title)
		assert.Equal(t, "Carol", saved.FightingSpiritAwards[0].RecipientName)
		assert.Equal(t, "Osaka", saved.FightingSpiritAwards[0].RecipientDojo)
	})

	t.Run("400: missing fightingSpiritAwards field (body {}) rejected", func(t *testing.T) {
		// The field is documented required; a body of `{}` must 400 rather
		// than silently clear the list. An explicit [] (covered above) clears.
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBufferString("{}"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "fightingSpiritAwards is required")
	})

	t.Run("400: malformed JSON body rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid+"/awards", bytes.NewBufferString("{not json"))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("400: invalid competition ID rejected", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"fightingSpiritAwards": []map[string]any{
				{"title": "Spirit", "recipientName": "Alice"},
			},
		})
		w := httptest.NewRecorder()
		// An over-length ID (>64 chars) is a single valid path segment that
		// reaches the handler and is rejected by requireValidCompID before
		// any store access.
		req, _ := http.NewRequest("PUT", "/api/competitions/"+strings.Repeat("x", 65)+"/awards", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

// TestCompetitionCourtsInvariant verifies that every competition resolves to at
// least one court: empty competition courts inherit the tournament's courts so
// generated matches carry a real court label (otherwise the per-court Shiaijo
// operator view at /admin/shiaijo/:court cannot surface them). See
// resolveCompetitionCourts in handlers_tournament.go.
func TestCompetitionCourtsInvariant(t *testing.T) {
	// readBackCourts POSTs/loads a competition and returns its persisted courts.
	postComp := func(t *testing.T, r *gin.Engine, body map[string]any) map[string]any {
		t.Helper()
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusCreated, w.Code, "resp: %s", w.Body.String())
		var got map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got
	}
	courtsOf := func(comp map[string]any) []string {
		raw, _ := comp["courts"].([]any)
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			out = append(out, v.(string))
		}
		return out
	}

	t.Run("empty courts inherit the tournament's courts", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		// A 4-shiaijo venue, because the inherited list is now validated like
		// any other: a competition may not reach an illegal allocation (3, 5,
		// 6, ...) by inheriting one. The inheritance invariant this test owns
		// is unchanged; only the venue size is, so it stays a legal allocation.
		// The refusal side is covered by
		// TestCreateCompetitionInheritedCourtsMatchExplicit.
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Courts: []string{"A", "B", "C", "D"},
		}))
		comp := postComp(t, r, map[string]any{"name": "No Courts Comp"})
		assert.Equal(t, []string{"A", "B", "C", "D"}, courtsOf(comp),
			"a competition created without courts must inherit the tournament's courts")

		// Confirm it is persisted, not just echoed in the response.
		reloaded, err := store.LoadCompetition(comp["id"].(string))
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B", "C", "D"}, reloaded.Courts)
	})

	t.Run("explicit competition courts are preserved", func(t *testing.T) {
		r, store, _, _, _ := setupTestRouter(t)
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "T", Courts: []string{"A", "B", "C"},
		}))
		comp := postComp(t, r, map[string]any{"name": "One Court Comp", "courts": []string{"B"}})
		assert.Equal(t, []string{"B"}, courtsOf(comp),
			"an explicit court selection must not be overridden by the tournament's courts")
	})

	t.Run("no tournament falls back to a single default court", func(t *testing.T) {
		r, _, _, _, _ := setupTestRouter(t) // setupTestRouter saves no tournament
		comp := postComp(t, r, map[string]any{"name": "Bootstrap Comp"})
		assert.Equal(t, []string{"A"}, courtsOf(comp),
			"with no tournament configured, an empty-courts competition defaults to court A")
	})
}

func TestPUTCompetition_DrawReadyOutputAffectingGate(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "draw-ready-gate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          cid,
		Name:        "Draw Ready Gate",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		Courts:      []string{"A"},
		PoolSize:    4,
		PoolWinners: 2,
		Status:      state.CompStatusDrawReady,
	}))

	t.Run("REJECT output-affecting PoolSize change while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate",
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    5, // changed from stored 4, output-affecting
			"poolWinners": 2,
			"roundRobin":  false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"changing PoolSize while draw-ready must return 409: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "discard",
			"409 body must mention discarding the draw")

		// Status must remain draw-ready, gate must not mutate state.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})

	t.Run("REJECT zero-value bypass: format set to empty while draw-ready", func(t *testing.T) {
		// Regression for the Copilot finding: the gate must compare effective
		// values directly (no zero/empty sentinels), else a caller can set an
		// output-affecting field TO its empty value to slip past the gate and
		// corrupt the draw. format:"" is accepted by validateCompetitionFormat.
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate",
			"format":      "", // changed from stored "mixed" TO empty, must still 409
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
			"roundRobin":  false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"setting format to empty while draw-ready must return 409 (no zero-value bypass): %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format,
			"format must be unchanged after a rejected zero-value PUT")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})

	// WithZekkenName reaches the Excel generator (POST /create), so it is
	// output-affecting and must be gated while draw-ready. numberPrefix is
	// deliberately NOT in this loop (bc-pnum G4): see the ALLOW subtest below.
	for _, tc := range []struct {
		name  string
		field string
		value any
	}{
		{"REJECT withZekkenName change while draw-ready", "withZekkenName", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(map[string]any{
				"id":          cid,
				"name":        "Draw Ready Gate",
				"format":      state.CompFormatMixed,
				"kind":        "individual",
				"courts":      []string{"A"},
				"poolSize":    4,
				"poolWinners": 2,
				"roundRobin":  false,
				tc.field:      tc.value, // the only output-affecting change
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equalf(t, http.StatusConflict, w.Code,
				"changing %s while draw-ready must return 409: %s", tc.field, w.Body.String())
			stored, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, state.CompStatusDrawReady, stored.Status)
		})
	}

	// bc-pnum G4: numberPrefix is explicitly OUTSIDE the output-affecting set.
	// Changing it while draw-ready returns 200, not 409, and RenumberCompetitors
	// rewrites pools.csv in place rather than requiring the draw to be discarded.
	t.Run("ALLOW numberPrefix change while draw-ready, renumbers pools.csv", func(t *testing.T) {
		// Seed a pool with numbers under the OLD prefix ("Y"), so a renumber to
		// "X" is observable in the saved file rather than a no-op over blanks.
		require.NoError(t, store.SavePools(cid, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{Name: "Player 1", Dojo: "Dojo Player 1", Number: "Y1"},
				{Name: "Player 2", Dojo: "Dojo Player 2", Number: "Y2"},
			}},
		}))

		body, _ := json.Marshal(map[string]any{
			"id":           cid,
			"name":         "Draw Ready Gate",
			"format":       state.CompFormatMixed,
			"kind":         "individual",
			"courts":       []string{"A"},
			"poolSize":     4,
			"poolWinners":  2,
			"roundRobin":   false,
			"numberPrefix": "X", // the only output-affecting-looking change
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"changing numberPrefix while draw-ready must return 200: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "X", stored.NumberPrefix)
		// The draw is untouched: still draw-ready, not discarded/regenerated.
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)

		pools, err := store.LoadPools(cid)
		require.NoError(t, err)
		require.Len(t, pools, 1)
		require.Len(t, pools[0].Players, 2)
		assert.Equal(t, "X1", pools[0].Players[0].Number,
			"RenumberCompetitors must rewrite pools.csv under the new prefix")
		assert.Equal(t, "X2", pools[0].Players[1].Number)
	})

	t.Run("ALLOW cosmetic Name rename while draw-ready", func(t *testing.T) {
		// All output-affecting fields match the stored comp; only Name differs.
		// The gate compares effective values directly (no sentinels): the real
		// client always PUTs the full config, and the omitted fields here
		// (poolSizeMode/teamSize) match the stored zero values, so no
		// output-affecting change is detected and the rename is allowed.
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Gate Renamed", // cosmetic change, allowed
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,     // same as stored
			"poolWinners": 2,     // same as stored
			"roundRobin":  false, // same as stored
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"renaming while draw-ready must be allowed: %s", w.Body.String())

		// Rename must have persisted.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "Draw Ready Gate Renamed", stored.Name,
			"cosmetic Name change must persist through the draw-ready gate")
		// Status must remain draw-ready, a rename does not discard the draw.
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})
}

// TestUpdateCompetition_LegacyDurationMigrates is the end-to-end guard for the
// retirement of the whole-minute duration fields. A competition stored as
// `match_duration: 15` is migrated to seconds when the store loads it and
// clamped into the shiai band, so by the time it reaches the API it is already
// in range. That is what lets the band be a flat check with nothing to
// grandfather, and it is why an unrelated edit (a rename) can never be blocked
// by a duration the operator did not touch.
func TestUpdateCompetition_LegacyDurationMigrates(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// A competition configured at 15 minutes, before per-phase seconds existed.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            "legacy-duration",
		Name:          "Legacy Duration",
		Format:        state.CompFormatMixed,
		PoolSize:      3,
		Courts:        []string{"A"},
		MatchDuration: 15,
		Status:        state.CompStatusSetup,
	}))

	t.Run("the stored legacy value is migrated on read and survives intact", func(t *testing.T) {
		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, 900, got.PoolMatchDurationSeconds,
			"15 minutes is inside the 1:00-60:00 band, so it must carry over unchanged")
		assert.Zero(t, got.MatchDuration, "the retired field must be cleared")
	})

	put := func(t *testing.T, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		b, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/legacy-duration", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	base := func() map[string]any {
		return map[string]any{
			"name": "Legacy Duration", "format": "mixed", "poolSize": 3,
			"courts":                   []string{"A"},
			"poolMatchDurationSeconds": 900,
		}
	}

	t.Run("renaming is never blocked by the migrated duration", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, "Renamed Legacy", got.Name)
		assert.Equal(t, 900, got.PoolMatchDurationSeconds)
	})

	t.Run("an out-of-band duration is rejected", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 5400 // 90 minutes, above the 60:00 ceiling
		w := put(t, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("a fat-fingered 3-second duration is rejected", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 3
		w := put(t, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("an in-band duration saves", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 150
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Equal(t, 150, got.PoolMatchDurationSeconds)
	})

	t.Run("clearing the duration resets it to the scheduler default", func(t *testing.T) {
		body := base()
		body["name"] = "Renamed Legacy"
		body["poolMatchDurationSeconds"] = 0
		w := put(t, body)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		got, err := store.LoadCompetition("legacy-duration")
		require.NoError(t, err)
		assert.Zero(t, got.PoolMatchDurationSeconds, "0 must persist as unset, not fall back to the old value")
	})
}

// TestUpdateCompetition_DurationChangeKeepsDraw pins the requirement that
// retiming a competition never invalidates a generated draw: match duration
// feeds the scheduler only, and reaches neither the pool allocation nor the
// bracket, so it is deliberately absent from the draw-ready outputAffecting set.
func TestUpdateCompetition_DurationChangeKeepsDraw(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                       "draw-ready-duration",
		Name:                     "Draw Ready Duration",
		Format:                   state.CompFormatMixed,
		PoolSize:                 3,
		Courts:                   []string{"A"},
		PoolMatchDurationSeconds: 150,
		Status:                   state.CompStatusDrawReady,
	}))

	body, _ := json.Marshal(map[string]any{
		"name": "Draw Ready Duration", "format": "mixed", "poolSize": 3,
		"courts":                   []string{"A"},
		"poolMatchDurationSeconds": 240,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/draw-ready-duration", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "changing duration must not require discarding the draw: %s", w.Body.String())

	got, err := store.LoadCompetition("draw-ready-duration")
	require.NoError(t, err)
	assert.Equal(t, 240, got.PoolMatchDurationSeconds)
	assert.Equal(t, state.CompStatusDrawReady, got.Status, "the draw must survive a duration change")
}

// TestCompetitionAPI_RejectsOutOfBandNeverClamps pins the separation between the
// two duration jobs: the API REFUSES an out-of-band value, while the store PINS
// one that is already on disk. Getting these backwards is a real hazard, because
// POST used to call state.ApplyCompetitionDefaults on the inbound body BEFORE
// validation; had the band clamp been added there, an out-of-band POST would
// have been silently rewritten to the ceiling and returned 201.
func TestCompetitionAPI_RejectsOutOfBandNeverClamps(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST above the ceiling is rejected, not clamped", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "Too Long", "poolMatchDurationSeconds": 99999})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code,
			"an out-of-band duration must 400; silently clamping it to the ceiling would be the exact behaviour this control exists to prevent")
		assert.Contains(t, w.Body.String(), "between 60 and 3600 seconds")
	})

	t.Run("POST below the floor is rejected, not clamped", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"name": "Too Short", "poolMatchDurationSeconds": 3})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("a hand-edited out-of-band config stays fully editable", func(t *testing.T) {
		// The store pins it on load, so the flat band check the API performs
		// never sees an out-of-band value and unrelated edits keep working.
		require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "competitions", "hand-edited"), 0o700))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", "hand-edited", "config.md"),
			[]byte("---\nid: hand-edited\nname: Hand Edited\nformat: mixed\npool_size: 3\ncourts:\n    - A\npool_match_duration_seconds: 99999\nstatus: setup\n---\n"),
			0o600))

		loaded, err := store.LoadCompetition("hand-edited")
		require.NoError(t, err)
		require.Equal(t, state.MaxMatchDurationSeconds, loaded.PoolMatchDurationSeconds)

		// Rename it, echoing back the pinned value the way the settings form does.
		b, _ := json.Marshal(map[string]any{
			"name": "Renamed", "format": "mixed", "poolSize": 3, "courts": []string{"A"},
			"poolMatchDurationSeconds": state.MaxMatchDurationSeconds,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/hand-edited", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	})
}

// TestUpdateCompetition_CourtsLockedWhenStarted pins operator decision bc-rrsh
// r7: once a competition has started its shiaijo stay as drawn, whether or not
// the bouts on a shiaijo are fought. A bout changes court by a court move.
func TestUpdateCompetition_CourtsLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Court Drop", Date: "20-08-2026", Courts: []string{"A", "B"}, Password: "pw",
	}))

	const cid = "court-drop"
	save := func(status state.CompetitionStatus) {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Court Drop", Format: state.CompFormatMixed,
			Courts: []string{"A", "B"}, PoolSize: 4, PoolWinners: 2, Status: status,
		}))
	}
	put := func(courts []string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{
			"id": cid, "name": "Court Drop", "format": state.CompFormatMixed,
			"courts": courts, "poolSize": 4, "poolWinners": 2,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	for _, tc := range []struct {
		name   string
		status state.CompetitionStatus
		courtB state.MatchStatus
	}{
		{"REJECT dropping a shiaijo a scheduled match is on", state.CompStatusPools, state.MatchStatusScheduled},
		{"REJECT dropping a shiaijo whose bouts are all fought", state.CompStatusPools, state.MatchStatusCompleted},
		{"REJECT dropping a shiaijo once the competition has finished", state.CompStatusComplete, state.MatchStatusCompleted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			save(tc.status)
			require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{
				{ID: "Pool A-1", Court: "A", Status: state.MatchStatusScheduled},
				{ID: "Pool B-1", Court: "B", Status: tc.courtB},
			}))

			w := put([]string{"A"})
			assert.Equal(t, http.StatusConflict, w.Code, "body: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), "shiaijo can only be changed before the competition starts")

			stored, err := store.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, []string{"A", "B"}, stored.Courts, "the allocation must not change after the refusal")
		})
	}

	t.Run("REJECT adding a shiaijo once started", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name: "Court Drop", Date: "20-08-2026", Courts: []string{"A", "B", "C", "D"}, Password: "pw",
		}))
		t.Cleanup(func() {
			require.NoError(t, store.SaveTournament(&state.Tournament{
				Name: "Court Drop", Date: "20-08-2026", Courts: []string{"A", "B"}, Password: "pw",
			}))
		})
		save(state.CompStatusPools)
		w := put([]string{"A", "B", "C", "D"})
		assert.Equal(t, http.StatusConflict, w.Code, "body: %s", w.Body.String())
	})

	t.Run("ALLOW the same edit before the competition starts", func(t *testing.T) {
		save(state.CompStatusSetup)
		require.NoError(t, store.SavePoolMatches(cid, []state.MatchResult{
			{ID: "Pool B-1", Court: "B", Status: state.MatchStatusScheduled},
		}))

		w := put([]string{"A"})
		assert.Equal(t, http.StatusOK, w.Code,
			"nothing is live before the draw, so the allocation is still the operator's to set: %s", w.Body.String())
	})
}

// TestUpdateCompetition_DrawShapeLockedWhenStarted pins operator decision
// bc-rrsh r5: once a competition has started, the settings PUT refuses a
// change to the shape its draw was made with (who fights whom, pool sizing,
// winners, team size), as the Settings page locks them. A save that changes
// none of them, the name or the zekken option included, still goes through.
func TestUpdateCompetition_DrawShapeLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	put := func(id string, payload map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+id, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	const mixed = "shape-started-mixed"
	saveMixed := func() {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: mixed, Name: "Shape Started", Format: state.CompFormatMixed, Kind: "team",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, RoundRobin: true, TeamSize: 5,
			Status: state.CompStatusPools,
		}))
	}
	mixedPayload := func(overrides map[string]any) map[string]any {
		p := map[string]any{
			"id": mixed, "name": "Shape Started", "format": state.CompFormatMixed, "kind": "team",
			"courts": []string{"A"}, "poolSize": 4, "poolWinners": 2, "roundRobin": true, "teamSize": 5,
		}
		for k, v := range overrides {
			p[k] = v
		}
		return p
	}

	for _, tc := range []struct {
		name     string
		override map[string]any
	}{
		{"who fights whom", map[string]any{"roundRobin": false}},
		{"players per pool", map[string]any{"poolSize": 5}},
		{"winners per pool", map[string]any{"poolWinners": 1}},
		{"pool size type", map[string]any{"poolSizeMode": "max"}},
		{"team size", map[string]any{"teamSize": 3}},
	} {
		t.Run("REJECT "+tc.name+" on a started competition", func(t *testing.T) {
			saveMixed()
			w := put(mixed, mixedPayload(tc.override))
			assert.Equal(t, http.StatusConflict, w.Code, "%s", w.Body.String())
			assert.Contains(t, w.Body.String(), "can only be changed before the competition starts")

			stored, err := store.LoadCompetition(mixed)
			require.NoError(t, err)
			assert.True(t, stored.RoundRobin)
			assert.Equal(t, 4, stored.PoolSize)
			assert.Equal(t, 2, stored.PoolWinners)
			assert.Equal(t, "", stored.PoolSizeMode)
			assert.Equal(t, 5, stored.TeamSize)
		})
	}

	// Changing format or kind also renormalises the pool pair and team size,
	// so the refusal must be the format/kind one, not a list of fields the
	// caller never touched.
	for _, tc := range []struct {
		name     string
		override map[string]any
	}{
		{"format to league", map[string]any{"format": state.CompFormatLeague}},
		{"kind to individual", map[string]any{"kind": "individual", "teamSize": 0}},
	} {
		t.Run("REJECT "+tc.name+" with the format and kind sentence", func(t *testing.T) {
			saveMixed()
			w := put(mixed, mixedPayload(tc.override))
			assert.Equal(t, http.StatusConflict, w.Code, "%s", w.Body.String())
			assert.Contains(t, w.Body.String(), "format and kind can only be changed before the competition starts")
		})
	}

	t.Run("ALLOW a save that leaves the shape alone", func(t *testing.T) {
		saveMixed()
		w := put(mixed, mixedPayload(map[string]any{"name": "Shape Started Renamed", "withZekkenName": true}))
		require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
		stored, err := store.LoadCompetition(mixed)
		require.NoError(t, err)
		assert.Equal(t, "Shape Started Renamed", stored.Name)
		assert.True(t, stored.WithZekkenName)
	})

	t.Run("ALLOW the same change before the draw", func(t *testing.T) {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: mixed, Name: "Shape Started", Format: state.CompFormatMixed, Kind: "team",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, RoundRobin: true, TeamSize: 5,
			Status: state.CompStatusSetup,
		}))
		w := put(mixed, mixedPayload(map[string]any{"roundRobin": false, "poolSize": 5}))
		require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
		stored, err := store.LoadCompetition(mixed)
		require.NoError(t, err)
		assert.False(t, stored.RoundRobin)
		assert.Equal(t, 5, stored.PoolSize)
	})

	// A league's request has its pool pair zeroed (normalizePoolConfig) while
	// the stored one is the pair it drew with: a plain save of a started
	// league is not a shape change, and keeps the stored pair.
	t.Run("ALLOW a plain save of a started league and keep its pool pair", func(t *testing.T) {
		const league = "shape-started-league"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: league, Name: "League Started", Format: state.CompFormatLeague,
			Courts: []string{"A"}, PoolSize: 6, PoolWinners: 3, RoundRobin: true,
			Status: state.CompStatusPools,
		}))
		w := put(league, map[string]any{
			"id": league, "name": "League Started", "format": state.CompFormatLeague,
			"courts": []string{"A"}, "poolSize": 6, "poolWinners": 3, "roundRobin": true,
		})
		require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())
		stored, err := store.LoadCompetition(league)
		require.NoError(t, err)
		assert.Equal(t, 6, stored.PoolSize)
		assert.Equal(t, 3, stored.PoolWinners)
	})
}

// TestCompetitionPutWritesCanonicalSeedIdentity pins the canonical form
// extractSeeds writes. seeds.csv is resolved against the roster by exact
// (name, dojo) key and is NOT canonicalized on load, while participants.csv IS
// (CreatePlayersFromRecords Title-cases the name and TrimSpaces every field on
// every parse). Writing the raw request name therefore produced a seed row
// that could not resolve against its own participant.
//
// This is the operator-visible half of the bulk-endpoint casing bug: the admin
// roster box's "Apply changes" goes through PUT /competitions/:id (deriving
// seeds.csv fresh from each player's Seed field), NOT the bulk participants
// POST, so retyping a seeded competitor's name in different casing returned
// 200 and silently dropped the seed - the panel just showed "0 seeded".
// Confirmed in the browser before and after the fix.
func TestCompetitionPutWritesCanonicalSeedIdentity(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-seed-canonical"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Seed Canonical Test", Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Van Der Berg", Dojo: "DojoA"},
		{Name: "Bob Jones", Dojo: "Tora"},
	}))
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{
		{Name: "Van Der Berg", Dojo: "DojoA", SeedRank: 1},
	}))

	// The roster box on "Apply changes": the whole competition, one seeded
	// name retyped in lower case with a padded dojo, seed rank carried along.
	body, _ := json.Marshal(map[string]any{
		"id": compID, "name": "Seed Canonical Test", "status": "setup",
		"players": []map[string]any{
			{"name": "van der berg", "dojo": "  DojoA  ", "seed": 1},
			{"name": "Bob Jones", "dojo": "Tora"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "testpass")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	raw, err := store.LoadSeedsRaw(compID)
	require.NoError(t, err)
	require.Len(t, raw, 1)
	assert.Equal(t, "Van Der Berg", raw[0].Name, "seeds.csv must hold the canonical name the roster reads back as")
	assert.Equal(t, "DojoA", raw[0].Dojo, "both halves of the seed key are canonicalized, not just the name")

	// The point of the canonical form: the seed still resolves onto its own
	// participant on the next load.
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	seeded := map[string]int{}
	for _, p := range players {
		if p.Seed > 0 {
			seeded[p.Name] = p.Seed
		}
	}
	assert.Equal(t, map[string]int{"Van Der Berg": 1}, seeded,
		"a casing-only retype must not silently orphan the seed")
}

package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-sseq FR1: the measurement behind the sync figures in the architecture
// pages. One point is recorded through the real router while one stream
// subscriber is held, and the bytes of the event it causes are set against the
// two reads a viewer's page fetches again after it (the competition's detail
// and the competitions list), as sent and as gzipped.

const sbmRosterSize = 12

func sbmPlayerID(i int) string { return fmt.Sprintf("%08x-0000-4000-8000-000000000000", i+1) }

// sbmSeed makes a tournament of two leagues, the first of sbmRosterSize
// competitors (a full round robin, the first bout running), and returns the
// router's rig. The names and dojos are as long as a real entry list's.
func sbmSeed(t *testing.T) (*backupTestRig, []state.MatchResult) {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Measurement Taikai", Date: "01-06-2026", Venue: "Sports Hall", Courts: []string{"A", "B"},
		Password: backupTestPassword, Mode: state.TournamentModeOfficiated,
	}))
	var alphaMatches []state.MatchResult
	for _, id := range []string{"alpha", "beta"} {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: id, Name: "Individual " + id, Kind: "individual", Format: state.CompFormatLeague,
			Status: state.CompStatusPools, Date: "01-06-2026", Courts: []string{"A"}, NumberPrefix: id[:1],
		}))
		players := make([]domain.Player, sbmRosterSize)
		for i := range players {
			players[i] = domain.Player{
				ID: sbmPlayerID(i), Name: fmt.Sprintf("Competitor %s Number %02d", id, i+1),
				Dojo: fmt.Sprintf("Dojo Number %d Kendo Club", i%5+1),
			}
		}
		require.NoError(t, store.SaveParticipants(id, players))
		var matches []state.MatchResult
		for a := 0; a < sbmRosterSize; a++ {
			for b := a + 1; b < sbmRosterSize; b++ {
				m := state.MatchResult{
					ID:    fmt.Sprintf("Pool A-%d", len(matches)),
					SideA: players[a].Name, SideAID: players[a].ID, SideB: players[b].Name, SideBID: players[b].ID,
					Court: "A", Status: state.MatchStatusScheduled,
				}
				if len(matches) == 0 {
					m.Status = state.MatchStatusRunning
				}
				matches = append(matches, m)
			}
		}
		require.NoError(t, store.SavePoolMatches(id, matches))
		if id == "alpha" {
			alphaMatches = matches
		}
	}
	r, hub := newBackupRouter(t, store)
	return &backupTestRig{r: r, store: store, hub: hub, dir: dir}, alphaMatches
}

// sbmGet reads path as a public viewer, asking for gzip when gzipped. It
// returns the bytes the page would receive, and says whether they were gzipped.
func sbmGet(t *testing.T, rig *backupTestRig, path string, gzipped bool) (int, bool) {
	t.Helper()
	enc := ""
	if gzipped {
		enc = "gzip"
	}
	w := eventSeqGet(t, rig, path, "", enc)
	require.Equalf(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
	return w.Body.Len(), w.Header().Get("Content-Encoding") == "gzip"
}

func TestSyncBytes_ARefetchIsLargerThanTheEventThatCausesIt(t *testing.T) {
	rig, matches := sbmSeed(t)
	// Beta has the same shape but is not scored: it makes the list a real
	// two-competition list and nothing else.
	first := matches[0]

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)

	// One point is recorded, as the court's operator does.
	w := serveJSONWithPassword(rig, http.MethodPut, "/api/competitions/alpha/matches/"+url.PathEscape(first.ID)+"/score", map[string]any{
		"status": "running", "sideA": first.SideA, "sideB": first.SideB,
		"ipponsA": []string{"M"}, "ipponsB": []string{}, "modifiedAt": time.Now().UnixMilli(),
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var event historyEntry
	deadline := time.After(3 * time.Second)
	for event.typ != EventMatchUpdated {
		select {
		case event = <-ch:
		case <-deadline:
			t.Fatal("the point caused no match_updated event")
		}
	}
	var frame bytes.Buffer
	writeSSEEnvelope(&frame, event.seq, event.payload)
	eventBytes := frame.Len()

	detailPlain, _ := sbmGet(t, rig, "/api/viewer/competitions/alpha", false)
	listPlain, _ := sbmGet(t, rig, "/api/viewer/competitions", false)
	detailGz, detailIsGz := sbmGet(t, rig, "/api/viewer/competitions/alpha", true)
	listGz, listIsGz := sbmGet(t, rig, "/api/viewer/competitions", true)

	t.Logf("match_updated event: %d bytes", eventBytes)
	t.Logf("detail refetch: %d bytes plain, %d bytes gzipped (%.0f%% of plain)", detailPlain, detailGz, 100*float64(detailGz)/float64(detailPlain))
	t.Logf("list refetch:   %d bytes plain, %d bytes gzipped (%.0f%% of plain)", listPlain, listGz, 100*float64(listGz)/float64(listPlain))
	for _, viewers := range []int{1, 100, 1000} {
		t.Logf("%4d viewers, one point: event fan-out %d bytes; refetches as sent %d bytes, gzipped %d bytes",
			viewers, viewers*eventBytes, viewers*(detailPlain+listPlain), viewers*(detailGz+listGz))
	}

	// The claim the docs rest on: a refetch is larger than the event that
	// causes it, so an event that fetches nothing is a saving.
	assert.Greater(t, detailPlain, eventBytes, "the detail refetch is larger than its event")
	assert.Greater(t, listPlain, eventBytes, "the list refetch is larger than its event")
	// Both competitions are on the list, so it outweighs the one detail it holds.
	assert.Greater(t, listPlain, detailPlain, "the list carries both competitions")

	// The answers a gzip-accepting page gets are compressed, and smaller.
	assert.True(t, detailIsGz, "the detail answer is gzipped for a caller that accepts it")
	assert.True(t, listIsGz, "the list answer is gzipped for a caller that accepts it")
	assert.Less(t, detailGz, detailPlain)
	assert.Less(t, listGz, listPlain)
}

// serveJSONWithPassword is serveJSON with the organiser's sign-in.
func serveJSONWithPassword(rig *backupTestRig, method, url string, payload any) *httptest.ResponseRecorder {
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(method, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", backupTestPassword)
	w := httptest.NewRecorder()
	rig.r.ServeHTTP(w, req)
	return w
}

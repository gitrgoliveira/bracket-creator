package mobileapp

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateDrawHandler(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed 8 players in a knockout competition so GenerateDraw can succeed.
	players := make([]domain.Player, 8)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
	}
	cid := "gen-draw-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Gen Draw",
		Format: state.CompFormatKnockout,
		Courts: []string{"A"},
		Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(cid, players))

	t.Run("Success: setup → draw-ready", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, saved.Status)
	})

	t.Run("Reject: already draw-ready", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Not found: unknown competition", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/no-such-comp/generate-draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestPOSTStartAndGenerateDraw_LegacyEmptyPrefix_AssignBeforeDrawing pins R7 /
// ensureNumberPrefix: a legacy competition (G7, saved before this rule
// existed) can still load with an empty NumberPrefix, and both the
// single-click POST .../start and the two-step POST .../generate-draw must
// assign one BEFORE the engine draws -- there is no numberless mode to fall
// back to (D1), so a legacy record cannot be allowed to draw without one.
// TestPOSTStart_PreflightAssignsThenEngineCallFails_StillBroadcasts pins
// the mutation guard for the shared tail: runWithNumberPrefixPreflight's shared tail must
// still broadcast EventTournamentUpdated when the pre-flight itself
// SUCCEEDED (assigned a prefix, saved it) but the subsequent engine call
// then fails for an UNRELATED reason (here, StartCompetition's own
// "no participants" guard). The prefix assignment already landed in
// config.md even though this request is now failing, so open clients must
// still be told to refetch rather than show stale state until reload --
// exactly the same rule respondNumberPrefixPreflightError applies to the
// pre-flight's OWN failure, but here for the SEPARATE engine-call failure
// path the extraction must not have dropped.
func TestPOSTStart_PreflightAssignsThenEngineCallFails_StillBroadcasts(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "preflight-assigns-then-start-fails"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Preflight Assigns Then Start Fails", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusSetup,
		// NumberPrefix intentionally blank: the pre-flight assigns it.
		// Deliberately no participants saved: StartCompetition's own draw
		// pipeline fails with "no participants", unrelated to the pre-flight.
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusBadRequest, w.Code, "response: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "no participants")

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.NumberPrefix, "the pre-flight's own assignment must have landed even though StartCompetition then failed")

	select {
	case msg := <-ch:
		env := decodeHubEvent(t, msg)
		assert.Equal(t, EventTournamentUpdated, env.Type,
			"the pre-flight's writes landed even though the engine call failed; open clients must be told to refetch")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the tournament_updated broadcast after a successful pre-flight assignment followed by an unrelated engine failure")
	}
}

func TestPOSTStartAndGenerateDraw_LegacyEmptyPrefix_AssignBeforeDrawing(t *testing.T) {
	t.Run("POST /start assigns a prefix before drawing", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "legacy-start"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Start", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusSetup,
			// NumberPrefix intentionally left empty: this is the legacy shape.
		}))
		players := make([]domain.Player, 6)
		for i := range players {
			players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
		}
		require.NoError(t, store.SaveParticipants(cid, players))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.NotEmpty(t, stored.NumberPrefix, "a legacy empty prefix must be assigned before the draw runs")

		pools, err := store.LoadPools(cid)
		require.NoError(t, err)
		require.NotEmpty(t, pools)
		for _, p := range pools {
			for _, pl := range p.Players {
				assert.NotEmpty(t, pl.Number)
				assert.Truef(t, strings.HasPrefix(pl.Number, stored.NumberPrefix),
					"number %q should start with the assigned prefix %q", pl.Number, stored.NumberPrefix)
			}
		}
	})

	t.Run("POST /generate-draw assigns a prefix before drawing", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "legacy-generate-draw"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Generate Draw", Format: state.CompFormatKnockout,
			Courts: []string{"A"}, Status: state.CompStatusSetup,
		}))
		players := make([]domain.Player, 4)
		for i := range players {
			players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
		}
		require.NoError(t, store.SaveParticipants(cid, players))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.NotEmpty(t, stored.NumberPrefix, "a legacy empty prefix must be assigned before the draw runs")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status)
	})

	// G4c on the start path: the renumber inside ensureNumberPrefix follows an
	// ASSIGNMENT only. A competition that already has a prefix is left alone,
	// so a pools.csv that will not parse (inherited damage) cannot refuse the
	// one action that starts the day. The revert this pins: renumbering
	// unconditionally in ensureNumberPrefix turned this request into a 500
	// with the competition stuck at draw-ready.
	t.Run("POST /start on a prefixed draw-ready competition is not refused for a broken pools.csv", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "prefixed-draw-ready-corrupt-pools"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Prefixed Draw Ready", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusDrawReady,
			NumberPrefix: "K",
		}))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", cid, "pools.csv"),
			[]byte("a,b\na,\"bad\nquote"), 0o600))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusPools, stored.Status, "start must proceed; the broken file is not this write's")
		assert.Equal(t, "K", stored.NumberPrefix)
	})

	// F3(iii): engine.StartCompetition's draw-ready branch
	// (transitionDrawToRunning) only flips status, it never re-runs the draw
	// pipeline, so a legacy pools.csv left over from a draw generated before
	// this rule existed (empty prefix, empty Number column) would otherwise
	// never get numbered. ensureNumberPrefix's own eng.RenumberCompetitors
	// call is what heals it as part of start.
	t.Run("POST /start on a draw-ready legacy competition numbers the existing pools.csv", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "legacy-draw-ready-start"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Draw Ready Start", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusDrawReady,
			// NumberPrefix intentionally left empty: this is the legacy shape.
		}))
		require.NoError(t, store.SavePools(cid, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "p1", Name: "Alice", Dojo: "Dojo Alice"},
				{ID: "p2", Name: "Bob", Dojo: "Dojo Bob"},
			}},
		}))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.NotEmpty(t, stored.NumberPrefix, "a legacy empty prefix must be assigned")

		pools, err := store.LoadPools(cid)
		require.NoError(t, err)
		require.Len(t, pools, 1)
		require.Len(t, pools[0].Players, 2)
		for _, pl := range pools[0].Players {
			assert.NotEmptyf(t, pl.Number, "competitor %q must be numbered by start, not left blank", pl.Name)
			assert.Truef(t, strings.HasPrefix(pl.Number, stored.NumberPrefix),
				"number %q should start with the assigned prefix %q", pl.Number, stored.NumberPrefix)
		}
	})

	// F3(i): a competition whose status the requested action cannot use
	// anyway (already running/in-progress) must be left untouched by
	// ensureNumberPrefix -- both its NumberPrefix and its config.md file --
	// and the rejection must come from the engine's own status switch
	// (StartCompetition's default case: "competition %s already started",
	// mapped to 400 by the handler), not from this pre-flight guard minting
	// and persisting a prefix for an action that was always going to fail.
	t.Run("POST /start on a running competition with an empty prefix is rejected and config.md is untouched", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "running-empty-prefix"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Running Empty Prefix", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusPools,
			// NumberPrefix intentionally left empty: this is the legacy shape,
			// and the competition is already in progress (status "pools"),
			// which engine.StartCompetition's default case refuses.
		}))
		configPath := tempDir + "/competitions/" + cid + "/config.md"
		before, err := os.ReadFile(configPath)
		require.NoError(t, err)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code,
			"an already-started competition must be rejected with the engine's usual status code: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "already started")

		after, err := os.ReadFile(configPath)
		require.NoError(t, err)
		assert.Equal(t, before, after, "config.md must be untouched: ensureNumberPrefix must not persist a prefix for a status the action can't use")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Empty(t, stored.NumberPrefix, "the empty prefix must not have been assigned")
	})

	// bc-pnum A5(a)/(b)/D5: a draw-ready competition with an EMPTY prefix and
	// a corrupt pools.csv is the one scenario where ensureNumberPrefix's own
	// RenumberCompetitors failure IS this write's fault (the assignment just
	// above is what made the call reachable at all): status must stay
	// draw-ready (StartCompetition itself must never run), the prefix IS
	// persisted (config.md already changed), the response is a 500 naming
	// what happened rather than a generic corrupt-file body, and a
	// tournament_updated broadcast fires so open clients refetch the changed
	// config even though the request failed.
	t.Run("POST /start on a draw-ready legacy competition with corrupt pools.csv reports assigned-but-not-numbered and does not start", func(t *testing.T) {
		r, store, _, hub, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "legacy-draw-ready-corrupt-pools"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Draw Ready Corrupt Pools", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusDrawReady,
			// NumberPrefix intentionally left empty: this is the legacy shape.
		}))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", cid, "pools.csv"),
			[]byte("a,b\na,\"bad\nquote"), 0o600))

		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusInternalServerError, w.Code, "response: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "assigned but competitors could not be renumbered",
			"the 500 must name what happened, not internalError's generic corrupt-file body")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, stored.Status, "StartCompetition must never run: the pre-flight itself failed")
		assert.NotEmpty(t, stored.NumberPrefix, "the prefix WAS assigned and saved before the renumber failed")

		select {
		case msg := <-ch:
			env := decodeHubEvent(t, msg)
			assert.Equal(t, EventTournamentUpdated, env.Type,
				"config.md already changed even though the request failed; open clients must be told to refetch")
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the tournament_updated broadcast")
		}
	})

	// bc-pnum A5(a): the RETRY this fix exists for. The previous
	// subtest pins the FIRST call (500, prefix saved, renumber failed). This
	// one continues past it: once the operator repairs pools.csv (here,
	// simulated by re-saving it with valid-but-unnumbered players, the exact
	// shape a hand-fixed legacy file would have), a SECOND POST /start must
	// actually retry the renumber and succeed -- because RenumberCompetitors
	// now runs UNCONDITIONALLY on every call, not only on the branch that
	// just assigned a prefix. Before this fix, the second call's
	// `comp.NumberPrefix != ""` (already saved by the first call) short-
	// circuited the WHOLE function, skipping the renumber forever and
	// starting the competition with every player permanently unnumbered.
	t.Run("POST /start retries the renumber on a second call after pools.csv is repaired", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		cid := "legacy-draw-ready-retry"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Draw Ready Retry", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusDrawReady,
			// NumberPrefix intentionally left empty: this is the legacy shape.
		}))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", cid, "pools.csv"),
			[]byte("a,b\na,\"bad\nquote"), 0o600))

		// First call: prefix is assigned and saved, but the renumber fails
		// against the corrupt file -- 500 (pinned by the previous subtest).
		w1 := httptest.NewRecorder()
		req1, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w1, req1)
		require.Equalf(t, http.StatusInternalServerError, w1.Code, "premise (first call): %s", w1.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotEmpty(t, stored.NumberPrefix, "premise: the prefix must already be saved before the retry")

		// The operator repairs pools.csv: a valid file, but not yet numbered
		// (G7's legacy shape) -- exactly what a hand-fix or an older backup
		// restore would leave behind.
		require.NoError(t, store.SavePools(cid, []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "p1", Name: "Alice", Dojo: "Dojo Alice"},
				{ID: "p2", Name: "Bob", Dojo: "Dojo Bob"},
			}},
		}))

		// Second call: the prefix is already saved (assigned=false this
		// time), so the ONLY way this numbers the players is if the renumber
		// actually runs again on this call, unconditionally.
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w2, req2)
		require.Equalf(t, http.StatusOK, w2.Code, "the retry must succeed once pools.csv is repaired: %s", w2.Body.String())

		pools, err := store.LoadPools(cid)
		require.NoError(t, err)
		require.Len(t, pools, 1)
		require.Len(t, pools[0].Players, 2)
		for _, p := range pools[0].Players {
			assert.NotEmptyf(t, p.Number, "competitor %q must be numbered on the retry, got %q", p.Name, p.Number)
		}
	})

	// bc-pnum A5(d): an UNRELATED sibling competition's unreadable config.md
	// must not turn a start/generate-draw pre-flight for THIS competition
	// into a 500. engine's takenNumberPrefixes (which assignDefaultNumberPrefix
	// uses via DefaultNumberPrefixFor) and EnsureNumberPrefix's uniqueness
	// check (which the pre-flight uses) must both log-and-skip the broken
	// sibling rather than propagate its load error.
	t.Run("POST /start is not refused by an unrelated sibling's unreadable config.md", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "broken-sibling", Name: "Broken Sibling", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusSetup,
		}))
		require.NoError(t, os.WriteFile(
			filepath.Join(tempDir, "competitions", "broken-sibling", "config.md"),
			[]byte("not front matter at all"), 0o600))

		cid := "legacy-start-with-broken-sibling"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Legacy Start With Broken Sibling", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusSetup,
		}))
		players := make([]domain.Player, 6)
		for i := range players {
			players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
		}
		require.NoError(t, store.SaveParticipants(cid, players))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code,
			"an unrelated sibling's broken config.md must not 500 this competition's start: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.NotEmpty(t, stored.NumberPrefix)
	})
}

func TestDiscardDrawHandler(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "discard-draw-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Discard Draw",
		Format: state.CompFormatKnockout,
		Courts: []string{"A"},
		Status: state.CompStatusDrawReady,
	}))

	t.Run("Success: draw-ready → setup", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/"+cid+"/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusNoContent, w.Code, "response: %s", w.Body.String())

		saved, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusSetup, saved.Status)
	})

	t.Run("Reject: not draw-ready (setup)", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/"+cid+"/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("Not found: unknown competition", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("DELETE", "/api/competitions/no-such-comp/draw", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

// TestPOSTStart_PrefixLessCompetitionWithDuplicateNameIsNotRefused pins
// bc-pnum A5(c)/D4(c): the pre-flight passes "" for the name (it validates
// only the field it introduces, the derived prefix), so a prefix-less
// competition sharing its NAME with a sibling (an out-of-band copy/restore,
// or simply two competitions the operator happened to name the same) is no
// longer refused start over a field the request never sent.
func TestPOSTStart_PrefixLessCompetitionWithDuplicateNameIsNotRefused(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "namesake-other", Name: "Shared Name", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, NumberPrefix: "S",
	}))
	cid := "namesake-starting"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Shared Name", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 3, PoolWinners: 2, Status: state.CompStatusSetup,
		// NumberPrefix intentionally empty: this is the legacy shape.
	}))
	players := make([]domain.Player, 6)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
	}
	require.NoError(t, store.SaveParticipants(cid, players))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/start", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code,
		"a duplicate NAME must not refuse start, since the pre-flight only introduces the prefix: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.NumberPrefix)
}

package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEnsureNumberPrefix_ConcurrentFlipSurvivesAtomicReadModifyWrite pins the atomic read-modify-write:
// ensureNumberPrefix used to LoadCompetition, mutate the LOCAL copy, then
// SaveCompetition it -- a read-modify-write with no lock spanning the gap
// (only compRenameMu, a DIFFERENT mutex from the per-competition lock every
// other writer takes). A concurrent writer going through an atomic
// primitive (flipHasParticipantIDs, itself store.UpdateCompetitionChanged)
// that landed in that gap was silently overwritten by this call's stale
// whole-struct save.
//
// The fix routes the assignment itself through store.UpdateCompetitionChanged,
// so load+mutate+save now run as ONE atomic sequence under the per-comp
// lock: a concurrent same-competition write through another atomic
// primitive can only ever land wholly BEFORE or wholly AFTER it, never
// inside it. Racing the two for real (no artificial delay needed post-fix)
// must therefore land BOTH changes regardless of scheduling order -- proven
// deterministically, not statistically, because whichever finishes second
// reads the first's already-committed change as `current` and preserves it
// (the transform mutates in place and returns it unchanged).
func TestEnsureNumberPrefix_ConcurrentFlipSurvivesAtomicReadModifyWrite(t *testing.T) {
	_, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "g7-concurrent-flip"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Concurrent Flip", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup,
		// NumberPrefix intentionally blank (ensureNumberPrefix assigns it) and
		// HasParticipantIDs intentionally false (the concurrent flip sets it).
	}))

	var wg sync.WaitGroup
	wg.Add(2)
	var assigned bool
	var ensureErr, flipErr error
	go func() {
		defer wg.Done()
		assigned, ensureErr = ensureNumberPrefix(eng, cid, state.CanStart)
	}()
	go func() {
		defer wg.Done()
		flipErr = flipHasParticipantIDs(store, cid)
	}()
	wg.Wait()

	require.NoError(t, ensureErr)
	require.NoError(t, flipErr)
	assert.True(t, assigned, "a blank stored prefix must be assigned")

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.NumberPrefix, "the assigned prefix must land")
	assert.True(t, stored.HasParticipantIDs,
		"a concurrent flip racing ensureNumberPrefix's own read-modify-write must not be clobbered by a stale whole-struct save")
}

// TestEnsureNumberPrefix_CorrelatesSkippedSiblingWithAssignedPrefix pins the correlated skip warning:
// the tolerant sibling walk (engine.siblingCompetitions) already logged its
// own "skipping unreadable" line, and the assignment itself was not logged anywhere -- so a
// derived prefix that persisted while a sibling was unreadable (and thus
// possibly colliding once that sibling's config.md is repaired) left no way
// to correlate the two facts from the server log alone. ensureNumberPrefix's
// assign branch now logs ONE line naming the assigned prefix, the
// competition id, and the skipped sibling id(s) together.
func TestEnsureNumberPrefix_CorrelatesSkippedSiblingWithAssignedPrefix(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// A sibling whose config.md will not parse: the tolerant uniqueness
	// check logs its own "skipping unreadable sibling" line and moves on.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "broken-sibling", Name: "Broken Sibling", NumberPrefix: "B",
	}))
	require.NoError(t, os.WriteFile(
		filepath.Join(tempDir, "competitions", "broken-sibling", "config.md"),
		[]byte("not front matter at all"), 0o600))

	const cid = "t2-generate-draw"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Generate Draw", Format: state.CompFormatKnockout,
		Courts: []string{"A"}, Status: state.CompStatusSetup,
		// NumberPrefix intentionally blank: this call is what assigns it.
	}))
	players := make([]domain.Player, 4)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "Dojo"}
	}
	require.NoError(t, store.SaveParticipants(cid, players))

	var logBuf bytes.Buffer
	prevOut := log.Writer()
	log.SetOutput(&logBuf)
	t.Cleanup(func() { log.SetOutput(prevOut) })

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	require.NotEmpty(t, stored.NumberPrefix, "a legacy blank prefix must still be assigned despite the unreadable sibling")

	logStr := logBuf.String()
	assert.Contains(t, logStr, "EnsureNumberPrefix", "must log from engine.EnsureNumberPrefix's own assign branch, not just the tolerant check's own skip line")
	assert.Contains(t, logStr, stored.NumberPrefix, "the log line must name the prefix THIS call assigned")
	assert.Contains(t, logStr, cid, "the log line must name the competition")
	assert.Contains(t, logStr, "broken-sibling", "the log line must name the skipped sibling id, correlating it with the assignment")
}

// TestGETNumberPrefixDefault exercises the R9 preview endpoint (item 8): it
// must return the exact value assignDefaultNumberPrefix would assign, since
// the create and settings forms pre-fill from it and must never show the
// operator a value a save would not actually land.
func TestGETNumberPrefixDefault(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "existing-k", Name: "Existing K Comp", NumberPrefix: "K",
	}))

	getPrefix := func(t *testing.T, query string) string {
		t.Helper()
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/number-prefix-default"+query, nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
		var got map[string]string
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		return got["numberPrefix"]
	}

	t.Run("derives from name when nothing collides", func(t *testing.T) {
		assert.Equal(t, "N", getPrefix(t, "?name=Novice%20Cup"))
	})

	t.Run("avoids a prefix already taken by another competition", func(t *testing.T) {
		assert.Equal(t, "KO", getPrefix(t, "?name=Kendo%20Open"), "K is already used by existing-k, so it escalates to KO")
	})

	// bc-pnum B5: the exclude query parameter is dead residue of a
	// settings-screen pre-fill caller this endpoint never actually shipped
	// (built and retired before the endpoint's first release) and has been
	// removed. This endpoint has exactly one caller, the CREATE form, which
	// never has an id to exclude, so a stray "exclude" on the query string
	// today is simply IGNORED rather than acted on: "Kendo Open" still
	// escalates to "KO" (K is taken by existing-k) even with
	// exclude=existing-k on the request.
	t.Run("a stray exclude query parameter is ignored (dead residue removed, not repurposed)", func(t *testing.T) {
		assert.Equal(t, "KO", getPrefix(t, "?name=Kendo%20Open&exclude=existing-k"))
	})

	t.Run("empty name resolves via the fallback, still avoiding what's taken", func(t *testing.T) {
		// No name -> nameInitials("") is empty -> DefaultNumberPrefixFallback
		// ("K"), which collides with existing-k. The fallback has no second
		// initial to escalate to, so it falls to a numeric suffix; every plain
		// digit suffix on "K" is ambiguous with "K" itself (bc-pnum A1), so it
		// lands on the zero-padded escape, "K02".
		assert.Equal(t, "K02", getPrefix(t, ""))
	})
}

// TestGETCompetitionByID_IDCollidingWithFormerPreviewPath pins the route move: gin v1.12
// resolves a static route segment before a wildcard, so while the number-
// prefix preview lived at GET /competitions/number-prefix-default, a
// competition whose id happened to slug to "number-prefix-default" (e.g.
// name "Number Prefix Default", or an explicit id on POST /competitions)
// could never be fetched via GET /competitions/:id -- the static route
// always won the match first, returning the preview's {numberPrefix: ...}
// shape (no "id" key) instead of the competition detail. Moving the preview
// out of the /competitions/ subtree (to GET /number-prefix-default) closes
// the collision.
func TestGETCompetitionByID_IDCollidingWithFormerPreviewPath(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "number-prefix-default"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Number Prefix Default", NumberPrefix: "N",
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/"+cid, nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, cid, got["id"],
		"must be the competition detail (carries id): the old static route shadowed this and returned the preview's {numberPrefix} body with no id key at all")
}

// TestGETNumberPrefixDefault_AnswersOnNewPath is the route move's companion assertion:
// the moved preview endpoint still answers, just one path segment up.
func TestGETNumberPrefixDefault_AnswersOnNewPath(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/number-prefix-default?name=Novice%20Cup", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())
	var got map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, "N", got["numberPrefix"])
}

// TestPOSTCompetition_NoNumberPrefixStoresDerivedPrefix pins F5(a) (bc-pnum
// review): POST /competitions with numberPrefix omitted must store a DERIVED
// prefix, not an empty one. Goes red if the assignDefaultNumberPrefix call in
// the POST handler is removed -- unlike "NumberPrefix Trimmed On Create"
// above (which sends an explicit, padded prefix and checks trimming), this
// sends no numberPrefix key at all.
func TestPOSTCompetition_NoNumberPrefixStoresDerivedPrefix(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	body, _ := json.Marshal(map[string]any{
		"id":   "no-prefix-post",
		"name": "Novice Cup",
		// numberPrefix deliberately omitted.
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "response: %s", w.Body.String())

	stored, err := store.LoadCompetition("no-prefix-post")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "N", stored.NumberPrefix, "an omitted numberPrefix on create must be derived from the name, never left empty")
}

// TestPUTCompetition_LegacyUnrelatedFieldChangeHealsPoolsCSV pins F5(b)
// (bc-pnum review): a settings PUT on a legacy competition (empty prefix on
// disk, empty Number column in pools.csv) that changes only an UNRELATED
// field must still assign the derived prefix and heal pools.csv. Goes red if
// the RenumberCompetitors call becomes conditional on the prefix having
// moved -- here the request doesn't send numberPrefix at all, so F1's
// inherit step sees an empty stored value to inherit, assignDefaultNumberPrefix
// then derives one because the competition truly has none, and G4's
// unconditional-renumber-after-every-save is what actually rewrites the
// blank Number column.
func TestPUTCompetition_LegacyUnrelatedFieldChangeHealsPoolsCSV(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "legacy-unrelated-field"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Legacy Unrelated Field", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
		// NumberPrefix intentionally left empty: this is the legacy shape.
	}))
	require.NoError(t, store.SavePools(cid, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			// Number intentionally left empty: legacy pools.csv drawn before
			// this rule existed (G7).
			{Name: "Player 1", Dojo: "Dojo Player 1"},
			{Name: "Player 2", Dojo: "Dojo Player 2"},
		}},
	}))

	body, _ := json.Marshal(map[string]any{
		"id":             cid,
		"name":           "Legacy Unrelated Field",
		"format":         state.CompFormatMixed,
		"kind":           "individual",
		"courts":         []string{"A"},
		"poolSize":       4,
		"poolWinners":    2,
		"roundRobin":     false,
		"checkInEnabled": true, // the only field this PUT actually changes
		// numberPrefix deliberately omitted.
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.NumberPrefix, "a legacy empty prefix must be assigned even though this PUT only touched checkInEnabled")
	assert.True(t, stored.CheckInEnabled, "the field the PUT actually asked to change must still land")

	pools, err := store.LoadPools(cid)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)
	for _, pl := range pools[0].Players {
		assert.NotEmptyf(t, pl.Number, "competitor %q must be numbered: pools.csv must be healed by this save, not left blank", pl.Name)
		assert.Truef(t, strings.HasPrefix(pl.Number, stored.NumberPrefix),
			"number %q should start with the assigned prefix %q", pl.Number, stored.NumberPrefix)
	}
}

// TestPUTCompetition_RosterPUT_UnreadableSiblingDoesNotBlockUnmovedPrefix
// pins the roster-save sibling scan: the roster-only branch used to call CheckUniqueCompFields(
// "", validatePrefix, id) UNCONDITIONALLY, even when validatePrefix was ""
// (nothing to validate, the common already-prefixed case). CheckUniqueCompFields
// is the STRICT policy, so it still listed every sibling and LoadCompetition'd
// each one, and one unreadable sibling's config.md 500'd this competition's
// roster save even though its own prefix never moved. The fix skips the
// call entirely when nothing moved.
func TestPUTCompetition_RosterPUT_UnreadableSiblingDoesNotBlockUnmovedPrefix(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cidA = "m1-roster-a"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cidA, Name: "Roster A", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup,
		NumberPrefix: "M", // already prefixed: this roster PUT never moves it.
	}))
	const cidB = "m1-roster-b"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: cidB, Name: "Roster B", NumberPrefix: "N"}))
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "competitions", cidB, "config.md"), []byte("not front matter at all"), 0o600))

	body, _ := json.Marshal(state.Competition{
		ID:   cidA,
		Name: "Roster A",
		Players: []domain.Player{
			{ID: "p1", Name: "Alice", Dojo: "Dojo Alice"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cidA, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code,
		"an unrelated sibling's unreadable config.md must not block a roster save whose own prefix never moved: %s", w.Body.String())

	players, err := store.LoadParticipants(cidA, false)
	require.NoError(t, err)
	require.Len(t, players, 1, "participants.csv must have changed on disk")
	assert.Equal(t, "Alice", players[0].Name)
}

// TestResolvePutNumberPrefix_MovedPrefixStillValidatedAgainstCollision is
// the companion case, exercised directly against the shared primitive
// (white-box, same package): whenever a write actually MOVES the prefix,
// the uniqueness/ambiguity check must still run -- the roster branch only skips it when
// NOTHING moved. This is tested directly against resolvePutNumberPrefix
// rather than through the roster-only HTTP PUT because the roster branch
// only ever reaches "moved" via auto-heal (blank -> DefaultNumberPrefix),
// and DefaultNumberPrefix's own candidate search already avoids every
// taken/ambiguous value in its input set by construction (see its doc
// comment on the "exhaustion" case) -- so a NATURAL collision on that path
// is reachable only by exhausting the entire ~107-candidate search space
// for a one-letter stem. The settings branch (comp.NumberPrefix explicitly
// client-supplied) already exercises this same invariant end-to-end via
// TestPUTCompetition_GrandfathersUnmovedAmbiguousOrDuplicateStoredValues's
// "settings PUT MOVING the prefix..." subtest; this test pins the identical
// invariant in the ONE function both branches now share, independent
// of which HTTP branch can reach it.
func TestResolvePutNumberPrefix_MovedPrefixStillValidatedAgainstCollision(t *testing.T) {
	_, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "m1-sibling", Name: "Sibling", NumberPrefix: "Z"}))

	// target already carries "Z" (non-empty), so inheritOrAssignNumberPrefix's
	// own inherit/derive steps are both no-ops (assignDefaultNumberPrefix
	// only derives when the field is still blank) -- target.NumberPrefix
	// stays "Z" verbatim, simulating "this write's own prefix is Z" without
	// going through DefaultNumberPrefix's collision-avoiding search.
	target := &state.Competition{ID: "m1-target", Name: "Target", NumberPrefix: "Z"}
	moved, infraErr, validationErr := resolvePutNumberPrefix(eng, target, "" /* storedPrefix: blank -> moved */, "", "m1-target")

	require.NoError(t, infraErr)
	assert.True(t, moved, "storedPrefix was blank and target now carries \"Z\": this call DID move it")
	require.Error(t, validationErr, "a write that moves its own prefix to a value already used by a sibling must still be refused")
	assert.Contains(t, validationErr.Error(), "Sibling")
}

// TestPUTCompetition_RosterOnlyPUTHealsBlankNumberPrefix pins bc-pnum A3: the
// roster-only branch used to return `current` straight to save WITHOUT
// running the inherit/assign step the settings branch runs (below), so a
// competition carrying a legacy blank NumberPrefix (G7, or the migration's
// own logged failure path) reached the unconditional RenumberCompetitors
// call at an empty prefix on ANY roster-only save -- not just a settings
// save -- and wrote bare "1","2" (no letters at all) into pools.csv. Status
// is deliberately NOT draw-ready (the roster-only branch's own draw-ready
// guard returns before this code executes, and a legacy blank-prefix
// competition with an existing draw is exactly the pools/playoffs-phase
// shape the bug reaches).
func TestPUTCompetition_RosterOnlyPUTHealsBlankNumberPrefix(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "roster-only-blank-prefix"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Roster Only Blank Prefix", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusPools,
		// NumberPrefix intentionally left empty: G7's legacy shape.
	}))
	require.NoError(t, store.SavePools(cid, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			// Number intentionally left empty too, matching the legacy shape.
			{ID: "p1", Name: "Player 1", Dojo: "Dojo Player 1"},
			{ID: "p2", Name: "Player 2", Dojo: "Dojo Player 2"},
		}},
	}))

	body, _ := json.Marshal(state.Competition{
		ID:   cid,
		Name: "Roster Only Blank Prefix",
		Players: []domain.Player{
			{ID: "p1", Name: "Player 1", Dojo: "Dojo Player 1"},
			{ID: "p2", Name: "Player 2", Dojo: "Dojo Player 2"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.NotEmpty(t, stored.NumberPrefix, "a roster-only PUT must heal a legacy blank prefix too, not only a settings PUT")

	pools, err := store.LoadPools(cid)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)
	for _, pl := range pools[0].Players {
		assert.Truef(t, strings.HasPrefix(pl.Number, stored.NumberPrefix),
			"competitor %q must carry a number under the assigned prefix %q, got %q (bare digits with no prefix means the old bug regressed)",
			pl.Name, stored.NumberPrefix, pl.Number)
	}
}

// TestPUTCompetition_GrandfathersUnmovedAmbiguousOrDuplicateStoredValues pins
// the review's BLOCKER on the ambiguity/duplicate-name fixes: CheckUniqueCompFields
// now also refuses a stored value ambiguous with (or identical to) a
// sibling's, but a PUT must validate only what IT actually moves, never
// re-litigate what it inherited unchanged. "K" and "K2" (or two same-named
// competitions) were LEGAL to save before this PR's ambiguity check existed;
// re-validating the untouched stored value on every future save would
// refuse an operator's roster edit, or an unrelated settings edit (e.g. the
// date), forever -- on a field the write never touched.
func TestPUTCompetition_GrandfathersUnmovedAmbiguousOrDuplicateStoredValues(t *testing.T) {
	seedAmbiguousPair := func(t *testing.T, store *state.Store) {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "grandfather-k", Name: "Grandfather K", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusPools, NumberPrefix: "K",
		}))
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "grandfather-k2", Name: "Grandfather K2", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusPools, NumberPrefix: "K2",
			Date: "01-01-2026",
		}))
		require.NoError(t, store.SavePools("grandfather-k2", []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "p1", Name: "Player 1", Dojo: "Dojo Player 1", Number: "K21"},
				{ID: "p2", Name: "Player 2", Dojo: "Dojo Player 2", Number: "K22"},
			}},
		}))
	}

	t.Run("roster PUT on the K2 competition beside K -> 200, numbers unchanged", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)
		seedAmbiguousPair(t, store)

		body, _ := json.Marshal(state.Competition{
			ID:   "grandfather-k2",
			Name: "Grandfather K2",
			Players: []domain.Player{
				{ID: "p1", Name: "Player 1", Dojo: "Dojo Player 1"},
				{ID: "p2", Name: "Player 2", Dojo: "Dojo Player 2"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/grandfather-k2", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "an unmoved stored prefix ambiguous with a sibling must not refuse a roster save: %s", w.Body.String())

		pools, err := store.LoadPools("grandfather-k2")
		require.NoError(t, err)
		require.Len(t, pools, 1)
		require.Len(t, pools[0].Players, 2)
		assert.Equal(t, "K21", pools[0].Players[0].Number, "the untouched prefix must not be renumbered under a different value")
		assert.Equal(t, "K22", pools[0].Players[1].Number)
	})

	t.Run("settings PUT changing only the date on it -> 200", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)
		seedAmbiguousPair(t, store)

		body, _ := json.Marshal(map[string]any{
			"id": "grandfather-k2", "name": "Grandfather K2", "format": state.CompFormatMixed,
			"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
			"roundRobin": false, "numberPrefix": "K2",
			"date": "02-01-2026", // the only field this PUT actually changes
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/grandfather-k2", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "an unmoved stored prefix ambiguous with a sibling must not refuse an unrelated settings save: %s", w.Body.String())

		stored, err := store.LoadCompetition("grandfather-k2")
		require.NoError(t, err)
		assert.Equal(t, "02-01-2026", stored.Date)
		assert.Equal(t, "K2", stored.NumberPrefix, "the prefix itself must be untouched")
	})

	t.Run("settings PUT MOVING the prefix to one ambiguous with a sibling -> 400 naming the sibling", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)
		seedAmbiguousPair(t, store)

		body, _ := json.Marshal(map[string]any{
			"id": "grandfather-k2", "name": "Grandfather K2", "format": state.CompFormatMixed,
			"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
			"roundRobin": false, "numberPrefix": "K9", // ambiguous with sibling "K"
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/grandfather-k2", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusBadRequest, w.Code, "actually MOVING the prefix to an ambiguous value must still be refused: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "Grandfather K", "the 400 must name the conflicting sibling")

		stored, err := store.LoadCompetition("grandfather-k2")
		require.NoError(t, err)
		assert.Equal(t, "K2", stored.NumberPrefix, "the refused move must not have landed")
	})

	t.Run("roster PUT on a same-named pair -> 200", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "dup-name-a", Name: "Duplicate Name", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusPools, NumberPrefix: "D",
		}))
		require.NoError(t, store.SaveCompetition(&state.Competition{
			// Out-of-band duplicate: the exact-name check pre-dates this PR
			// and would already have refused this via the API, but a restored
			// archive / hand-edited folder can still produce it on disk.
			ID: "dup-name-b", Name: "Duplicate Name", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusPools, NumberPrefix: "E",
		}))

		body, _ := json.Marshal(state.Competition{
			ID:   "dup-name-b",
			Name: "Duplicate Name",
			Players: []domain.Player{
				{ID: "p1", Name: "Player 1", Dojo: "Dojo Player 1"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/dup-name-b", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "a roster-only PUT never changes Name and must not be refused over a pre-existing duplicate: %s", w.Body.String())
	})
}

// TestPUTCompetition_HealOnlyRenumberBroadcasts pins F7: a settings PUT that
// changes NO config field and touches no roster, but whose unconditional
// renumber fills a legacy empty Number column, still broadcasts
// tournament_updated, so open viewers/TV repaint with the numbers. The revert
// this pins: gating the broadcast on `changed || participantsChanged` alone,
// which left this exact save silent.
func TestPUTCompetition_HealOnlyRenumberBroadcasts(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "heal-only-broadcast"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Heal Only Broadcast", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
		NumberPrefix: "K", // config already complete: this PUT changes nothing in it
	}))
	require.NoError(t, store.SavePools(cid, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Player 1", Dojo: "Dojo Player 1"}, // Number empty: legacy pools.csv
			{Name: "Player 2", Dojo: "Dojo Player 2"},
		}},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	body, _ := json.Marshal(map[string]any{
		"id": cid, "name": "Heal Only Broadcast", "format": state.CompFormatMixed,
		"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
		"roundRobin": false, "numberPrefix": "K",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	pools, err := store.LoadPools(cid)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	for _, pl := range pools[0].Players {
		assert.Truef(t, strings.HasPrefix(pl.Number, "K"), "competitor %q must be healed to K-numbers, got %q", pl.Name, pl.Number)
	}

	select {
	case msg := <-ch:
		env := decodeHubEvent(t, msg)
		assert.Equal(t, EventTournamentUpdated, env.Type,
			"a heal-only renumber must broadcast tournament_updated so open viewers refetch the numbers")
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for the tournament_updated broadcast after a heal-only renumber")
	}
}

// TestPUTCompetition_RosterResponseCarriesNoProvisionalNumbers pins the
// bc-pnum operator ruling ("pool by pool, after the draw, is the correct
// way. Remove the provisional numbers, they are confusing."): a roster
// save's response carries no provisionalNumbers key at all, and none of the
// fresh players carry a number before the draw runs.
func TestPUTCompetition_RosterResponseCarriesNoProvisionalNumbers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "roster-no-provisional"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Roster No Provisional", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup, NumberPrefix: "K",
	}))
	body, _ := json.Marshal(state.Competition{
		ID: cid, Name: "Roster No Provisional",
		Players: []domain.Player{
			{ID: "p1-uuid", Name: "Alice", Dojo: "Dojo Alice"},
			{ID: "p2-uuid", Name: "Bob", Dojo: "Dojo Bob"},
			{ID: "p3-uuid", Name: "Cara", Dojo: "Dojo Cara"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, present := raw["provisionalNumbers"]
	assert.False(t, present, "the response must not carry a provisionalNumbers key at all")

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 3)
	for _, p := range resp.Players {
		assert.Emptyf(t, p.Number, "competitor %q must carry no number before the draw runs", p.Name)
	}
}

// TestPUTCompetition_SettingsOnlyResponseCarriesNoProvisionalNumbers is the
// settings-only twin of TestPUTCompetition_RosterResponseCarriesNoProvisionalNumbers:
// a settings-only PUT (body omits `players` entirely) re-loads the on-disk
// roster for its response, and that response must show the same absence of
// any pre-draw number.
func TestPUTCompetition_SettingsOnlyResponseCarriesNoProvisionalNumbers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "settings-no-provisional"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Settings No Provisional", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup, NumberPrefix: "X",
	}))
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "p1-uuid", Name: "Alice", Dojo: "Dojo Alice"},
		{ID: "p2-uuid", Name: "Bob", Dojo: "Dojo Bob"},
	}))

	body, _ := json.Marshal(map[string]any{
		"id": cid, "name": "Settings No Provisional", "format": state.CompFormatMixed,
		"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
		"roundRobin": false, "numberPrefix": "X",
		// players deliberately OMITTED: this is a settings-only PUT.
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var raw map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, present := raw["provisionalNumbers"]
	assert.False(t, present, "the response must not carry a provisionalNumbers key at all")

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 2, "a settings-only PUT response must still carry the on-disk roster")
	for _, p := range resp.Players {
		assert.Emptyf(t, p.Number, "competitor %q must carry no number before the draw runs", p.Name)
	}
}

// TestCheckUniqueCompFields tests the CheckUniqueCompFields helper directly.
func TestCheckUniqueCompFields(t *testing.T) {
	_, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	seed := func(id, name, prefix string) {
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: id, Name: name, NumberPrefix: prefix}))
	}

	t.Run("empty prefix is always exempt", func(t *testing.T) {
		seed("pfx-empty-1", "EmptyPfx1", "")
		seed("pfx-empty-2", "EmptyPfx2", "")
		err := eng.CheckUniqueCompFields("NewComp", "", "")
		require.NoError(t, err)
	})

	t.Run("whitespace-only prefix is exempt", func(t *testing.T) {
		err := eng.CheckUniqueCompFields("AnotherNewComp", "  ", "")
		require.NoError(t, err)
	})

	t.Run("no collision for distinct prefixes", func(t *testing.T) {
		seed("pfx-k", "KendoComp", "K")
		err := eng.CheckUniqueCompFields("DistinctName", "M", "")
		require.NoError(t, err)
	})

	t.Run("collision detected (exact prefix match)", func(t *testing.T) {
		seed("pfx-collision", "CollisionComp", "X")
		err := eng.CheckUniqueCompFields("UniqueName", "X", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "number prefix")
		assert.Contains(t, err.Error(), "CollisionComp")
	})

	t.Run("collision detected (case-insensitive prefix)", func(t *testing.T) {
		seed("pfx-case", "CaseComp", "Y")
		err := eng.CheckUniqueCompFields("AnotherUnique", "y", "")
		assert.Error(t, err)
	})

	// bc-pnum A1: "K2" is not EQUAL to "K", but AssignPlayerNumbers's
	// prefix+counter concatenation makes them ambiguous ("K"'s 21st entrant
	// and "K2"'s 1st entrant both print "K21"), so this must be refused with
	// the same 400 shape as an exact match, naming the conflicting
	// competition.
	t.Run("collision detected (ambiguous prefix, K vs K2)", func(t *testing.T) {
		seed("pfx-ambiguous-k", "KendoAmbiguous", "K")
		err := eng.CheckUniqueCompFields("KendoAmbiguousChallenger", "K2", "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "number prefix")
		assert.Contains(t, err.Error(), "KendoAmbiguous")
	})

	t.Run("excludeID skips own record (PUT update)", func(t *testing.T) {
		seed("pfx-self", "SelfComp", "Z")
		err := eng.CheckUniqueCompFields("SelfComp", "Z", "pfx-self")
		require.NoError(t, err)
	})

	t.Run("collision detected (duplicate name)", func(t *testing.T) {
		seed("name-col", "DuplicateName", "Q")
		err := eng.CheckUniqueCompFields("DuplicateName", "W", "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "competition name")
	})

	// bc-pnum A5(c)/D4(a): an empty name is exempt from the uniqueness check
	// exactly as an empty prefix is (line ~3030 above): the start/generate-draw
	// pre-flight passes "" for the name deliberately, since it validates only
	// the field it introduces. Without this exemption, a blank-named record
	// already on disk (an out-of-band copy/restore) would refuse the
	// empty-name caller's OWN competition on a field it never touched.
	t.Run("empty name is always exempt, even against a stored blank-named competition", func(t *testing.T) {
		seed("blank-named", "", "BLK")
		err := eng.CheckUniqueCompFields("", "SomethingElse", "")
		require.NoError(t, err, "an empty name must never collide, even against a stored blank name")
	})
}

// TestNumberPrefixUniquenessHandlers tests POST and PUT validation via HTTP.
func TestNumberPrefixUniquenessHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed an existing competition with prefix "K".
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "pfx-existing", Name: "Kendo Open", NumberPrefix: "K",
	}))

	post := func(id, name, prefix string) *httptest.ResponseRecorder {
		comp := state.Competition{ID: id, Name: name, NumberPrefix: prefix}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("POST with duplicate prefix is rejected 400", func(t *testing.T) {
		w := post("pfx-dup-post", "Another Kendo", "K")
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "number prefix")
		stored, _ := store.LoadCompetition("pfx-dup-post")
		assert.Nil(t, stored, "duplicate-prefix competition must not be persisted")
	})

	t.Run("POST with case-insensitive duplicate prefix is rejected 400", func(t *testing.T) {
		w := post("pfx-dup-case", "Lower Kendo", "k")
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("POST with unique prefix succeeds", func(t *testing.T) {
		w := post("pfx-unique", "Men Open", "M")
		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("PUT can keep own prefix", func(t *testing.T) {
		comp := state.Competition{ID: "pfx-existing", Name: "Kendo Open Updated", NumberPrefix: "K"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/pfx-existing", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("PUT with another competition's prefix is rejected 400", func(t *testing.T) {
		// pfx-unique has prefix "M"; try to update pfx-existing to "M".
		comp := state.Competition{ID: "pfx-existing", Name: "Kendo Open", NumberPrefix: "M"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/pfx-existing", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "number prefix")
	})
}

// TestPUTCompetition_OmittedNumberPrefixInheritsStored pins the review's F1
// (HIGH): a PUT whose numberPrefix is blank/omitted must INHERIT the stored
// prefix, exactly like TeamMatchType and ExtraQualifiers already do, rather
// than assignDefaultNumberPrefix minting a brand new one for a competition
// that already has one. Without inheriting BEFORE assignDefaultNumberPrefix
// runs, any settings save that simply omits the field (any client that
// doesn't echo it back) would silently invent a new prefix, renumbering
// every competitor and invalidating printed tags for no reason the operator
// asked for.
func TestPUTCompetition_OmittedNumberPrefixInheritsStored(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "inherit-prefix"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Inherit Prefix", Format: state.CompFormatMixed, Kind: "individual",
		Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
		NumberPrefix: "Z",
	}))
	require.NoError(t, store.SavePools(cid, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Player 1", Dojo: "Dojo Player 1", Number: "Z1"},
			{Name: "Player 2", Dojo: "Dojo Player 2", Number: "Z2"},
		}},
	}))

	body, _ := json.Marshal(map[string]any{
		"id":          cid,
		"name":        "Inherit Prefix",
		"format":      state.CompFormatMixed,
		"kind":        "individual",
		"courts":      []string{"A"},
		"poolSize":    4,
		"poolWinners": 2,
		"roundRobin":  false,
		// numberPrefix deliberately OMITTED: the exact shape a client that
		// has never heard of the field, or simply doesn't echo it back, sends.
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "Z", stored.NumberPrefix, "an omitted numberPrefix must inherit the STORED prefix, not derive a new one")

	pools, err := store.LoadPools(cid)
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Len(t, pools[0].Players, 2)
	assert.Equal(t, "Z1", pools[0].Players[0].Number, "pools.csv must be unchanged: the prefix never moved")
	assert.Equal(t, "Z2", pools[0].Players[1].Number)
}

// TestPUTCompetition_RenumberFailurePolicy pins the review's F2 (MEDIUM): a
// RenumberCompetitors failure is reported differently depending on whether
// THIS PUT is the one that moved the prefix. A pools.csv broken by something
// else entirely (corrupted on disk, or a prefix change from an earlier save
// that never healed) must not block an operator editing an unrelated field --
// it is inherited damage, logged and retried by the next save. A PUT that
// itself moves the prefix and then fails to renumber is different: the
// operator's save only half-landed, and that is worth a 500 naming what
// happened. The "bare quote" CSV corruption mirrors
// state.TestLoadPools_MalformedCSV.
func TestPUTCompetition_RenumberFailurePolicy(t *testing.T) {
	writeCorruptPools := func(t *testing.T, dir, cid string) {
		t.Helper()
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "competitions", cid, "pools.csv"),
			[]byte("a,b\na,\"bad\nquote"), 0o600))
	}

	t.Run("prefix unchanged: rename over a broken pools.csv still returns 200 and lands", func(t *testing.T) {
		r, store, _, _, dir := setupTestRouter(t)

		const cid = "renumber-fail-rename"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Renumber Fail Rename", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
			NumberPrefix: "K",
		}))
		writeCorruptPools(t, dir, cid)

		body, _ := json.Marshal(map[string]any{
			"id": cid, "name": "Renumber Fail Rename Renamed", "format": state.CompFormatMixed,
			"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
			"roundRobin": false, "numberPrefix": "K", // unchanged
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equalf(t, http.StatusOK, w.Code,
			"a renumber failure over an UNCHANGED prefix is inherited damage, not this write's fault: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "Renumber Fail Rename Renamed", stored.Name, "the rename must still land")
	})

	t.Run("prefix moved: 500 naming that settings were saved but renumbering failed", func(t *testing.T) {
		r, store, _, hub, dir := setupTestRouter(t)
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		const cid = "renumber-fail-prefix"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Renumber Fail Prefix", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
			NumberPrefix: "K",
		}))
		writeCorruptPools(t, dir, cid)

		body, _ := json.Marshal(map[string]any{
			"id": cid, "name": "Renumber Fail Prefix", "format": state.CompFormatMixed,
			"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
			"roundRobin": false, "numberPrefix": "X", // moved
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equalf(t, http.StatusInternalServerError, w.Code,
			"a renumber failure over a MOVED prefix is this write's own fault: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "settings were saved",
			"the 500 body must say the settings landed even though renumbering failed")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "X", stored.NumberPrefix, "config.md must still carry the new prefix: the settings save itself succeeded")

		// The prefix landed, so open clients are told to refetch even though
		// the response is a 500 (the revert this pins: returning without the
		// broadcast, which left every board on the old prefix until a reload).
		select {
		case msg := <-ch:
			env := decodeHubEvent(t, msg)
			assert.Equal(t, EventTournamentUpdated, env.Type)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the tournament_updated broadcast after a half-landed prefix change")
		}
	})

	// G2a x G4c: a PUT that OMITS numberPrefix inherits the stored one, so it
	// can never count as "moved" and a renumber failure under it is inherited
	// damage. The revert this pins: computing numberPrefixMoved from the RAW
	// request value (before the inherit) would read a blank as a move and
	// answer 500 to exactly the client shape the inherit exists for.
	t.Run("omitted prefix over a broken pools.csv: inherited, not moved, 200", func(t *testing.T) {
		r, store, _, _, dir := setupTestRouter(t)

		const cid = "renumber-fail-omitted"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Renumber Fail Omitted", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusDrawReady,
			NumberPrefix: "K",
		}))
		writeCorruptPools(t, dir, cid)

		body, _ := json.Marshal(map[string]any{
			"id": cid, "name": "Renumber Fail Omitted Renamed", "format": state.CompFormatMixed,
			"kind": "individual", "courts": []string{"A"}, "poolSize": 4, "poolWinners": 2,
			"roundRobin": false,
			// numberPrefix deliberately omitted: inherited as "K".
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equalf(t, http.StatusOK, w.Code,
			"an omitted prefix is inherited, so the renumber failure is not this write's: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "K", stored.NumberPrefix, "the stored prefix must be inherited untouched")
		assert.Equal(t, "Renumber Fail Omitted Renamed", stored.Name, "the rename must still land")
	})

	t.Run("roster-only PUT over a broken pools.csv still returns 200 and saves participants", func(t *testing.T) {
		r, store, _, _, dir := setupTestRouter(t)

		const cid = "renumber-fail-roster"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Renumber Fail Roster", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup,
			NumberPrefix: "K",
		}))
		writeCorruptPools(t, dir, cid)

		body, _ := json.Marshal(state.Competition{
			ID:   cid,
			Name: "Renumber Fail Roster",
			Players: []domain.Player{
				{ID: "p1-uuid", Name: "Alice", Dojo: "Dojo Alice"},
				{ID: "p2-uuid", Name: "Bob", Dojo: "Dojo Bob"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equalf(t, http.StatusOK, w.Code,
			"the stored prefix here is already set (\"K\"), so THIS roster-only PUT does not move it, and a renumber failure over a broken pools.csv is inherited damage: %s", w.Body.String())

		players, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, players, 2, "participants.csv must have landed despite the broken pools.csv")
	})

	// bc-pnum: the sibling of "prefix moved: 500..." above, but
	// on the ROSTER-only branch. A roster-only PUT CAN move the prefix --
	// when it heals a blank stored one (bc-pnum A3) -- and when it does, a
	// renumber failure right after is this write's own damage exactly like
	// the settings branch, not inherited. Before bc-pnum's numberPrefixMoved
	// fix, this case silently took the "inherited, 200" path and swallowed
	// the half-landed prefix with no error and no broadcast. The 500's
	// WORDING must also name what THIS PUT actually persisted (a roster),
	// not the settings branch's wording -- what landed on disk here is
	// participants.csv (+ the healed prefix in config.md), never a settings
	// change, and telling the operator "settings were saved" when they PUT a
	// roster is a wrong-surface error message.
	t.Run("roster PUT that assigns a blank prefix, then fails to renumber: 500 naming the ROSTER, not settings", func(t *testing.T) {
		r, store, _, hub, dir := setupTestRouter(t)
		ch := hub.Subscribe()
		defer hub.Unsubscribe(ch)

		const cid = "renumber-fail-roster-assigns"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Renumber Fail Roster Assigns", Format: state.CompFormatMixed, Kind: "individual",
			Courts: []string{"A"}, PoolSize: 4, PoolWinners: 2, Status: state.CompStatusSetup,
			// NumberPrefix intentionally left empty: this roster PUT is what assigns one.
		}))
		writeCorruptPools(t, dir, cid)

		body, _ := json.Marshal(state.Competition{
			ID:   cid,
			Name: "Renumber Fail Roster Assigns",
			Players: []domain.Player{
				{ID: "p1-uuid", Name: "Alice", Dojo: "Dojo Alice"},
				{ID: "p2-uuid", Name: "Bob", Dojo: "Dojo Bob"},
			},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equalf(t, http.StatusInternalServerError, w.Code,
			"a roster PUT that itself assigned the prefix, then failed to renumber, is this write's own fault: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "the roster was saved",
			"a roster-only PUT's 500 must say ROSTER, not settings -- that is what this write actually persisted")
		assert.NotContains(t, w.Body.String(), "settings were saved",
			"the settings-branch wording must not leak onto a roster-only PUT's error")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.NotEmpty(t, stored.NumberPrefix, "config.md must still carry the newly-assigned prefix: the assignment itself succeeded")

		players, err := store.LoadParticipants(cid, false)
		require.NoError(t, err)
		assert.Len(t, players, 2, "participants.csv must have landed regardless of the renumber failure")

		select {
		case msg := <-ch:
			env := decodeHubEvent(t, msg)
			assert.Equal(t, EventTournamentUpdated, env.Type)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for the tournament_updated broadcast after a half-landed roster-assigned prefix")
		}
	})
}

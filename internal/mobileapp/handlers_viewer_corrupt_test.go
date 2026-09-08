package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// legacyPoolsCSVNoIDColumn is the pre-bc-pnum 7-column pools.csv shape
// (PoolName,Name,Position,DisplayName,Dojo,Seed,Number -- no 8th id column
// at all), shared by the two fixtures that need a readable, legitimately
// id-less pools.csv: one asserts the missing-ids advisory fires over it
// (drawn), the other that it does NOT (still draw-ready, so the bytes are
// a stray leftover).
const legacyPoolsCSVNoIDColumn = "Pool A,Alice,0,,Dojo A,,\nPool A,Bob,1,,Dojo B,,\n"

// The aggregate deliberately swallows a per-file load failure and serves what
// it got, so one unreadable file cannot blank a whole competition view. That
// left the operator with a silently half-empty competition and no way to learn
// why: "the bracket is missing" and "the bracket file will not parse" looked
// identical. These pin the located reason travelling with the payload.
func TestViewerAggregateReportsACorruptFile(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))

	broken := "{\n  \"rounds\": [\n    [{\"id\": \"R1-1\"x}]\n  ]\n}\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "bracket.json"), []byte(broken), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"one unreadable file must not blank the whole view")

	issues := dataIssuesFromResponse(t, w.Body.Bytes(), true, "kendo")
	require.Len(t, issues, 1, "the competition carries its data issues, got %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "corrupt-file", issue["kind"], "PR #416 finding 9: the kind must be explicit, not left for a consumer to infer from its absence")
	assert.Equal(t, "bracket.json", issue["file"])
	assert.EqualValues(t, 3, issue["line"], "the line the operator has to open")
	assert.NotEmpty(t, issue["detail"])
}

func TestViewerAggregateHasNoIssuesWhenTheFilesAreFine(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)
	_, present := payload[0]["dataIssues"]
	assert.False(t, present, "no banner when there is nothing to repair")
}

func TestDataIssuesFromKeepsOnlyRepairableFailures(t *testing.T) {
	// A missing file, a permissions problem or a nil is not something an
	// operator fixes in a text editor, so it gets no banner.
	issues := dataIssuesFrom(
		nil,
		os.ErrNotExist,
		&state.CorruptFileError{File: "pool-matches.csv", Line: 9, Column: 4, Detail: "bare \" in field"},
	)
	require.Len(t, issues, 1)
	assert.Equal(t, "corrupt-file", issues[0]["kind"], "PR #416 finding 9: partitioned by kind, not by object identity")
	assert.Equal(t, "pool-matches.csv", issues[0]["file"])
	assert.Equal(t, 9, issues[0]["line"])
}

// bc-pnum ruling 1b: a legacy participants.csv that predates the id-minting
// write path loads fine (no parse error) but leaves rows with no stable id.
// The viewer aggregate must still report it, distinct from a corrupt file
// (nothing failed to parse here), naming the affected competitors and the
// remedy.
func TestViewerAggregateReportsParticipantsMissingIDs(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))

	// Legacy, id-less rows: no leading UUID column.
	legacy := "Dave, Dojo D\nEve, Dojo E\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "participants.csv"), []byte(legacy), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	issues := dataIssuesFromResponse(t, w.Body.Bytes(), true, "kendo")
	require.Len(t, issues, 1, "the competition carries its data issues, got %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "missing-ids", issue["kind"])
	assert.Equal(t, "participants.csv", issue["file"])
	detail, _ := issue["detail"].(string)
	assert.Contains(t, detail, "Dave")
	assert.Contains(t, detail, "Eve")
	assert.Contains(t, detail, "Save the roster once and the ids are assigned.")
}

// A fully-stamped roster (every row already has a UUID from a prior save)
// must NOT trip the missing-ids issue.
func TestViewerAggregateNoMissingIDsIssueWhenRosterIsStamped(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{Name: "Dave", Dojo: "Dojo D"},
		{Name: "Eve", Dojo: "Dojo E"},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)
	_, present := payload[0]["dataIssues"]
	assert.False(t, present, "a fully-stamped roster raises no missing-ids issue")
}

func TestMissingParticipantIDsIssue_ManyRowsNamesFirstFewPlusCount(t *testing.T) {
	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
		{Name: "Carol", Dojo: "Dojo C"},
		{Name: "Dave", Dojo: "Dojo D"},
		{Name: "Eve", Dojo: "Dojo E"},
	}
	issue := missingIDsIssue("participants.csv", helper.MissingParticipantIDsMessage(players))
	require.NotNil(t, issue)
	detail, _ := (*issue)["detail"].(string)
	assert.Contains(t, detail, "5 competitors, including")
	assert.Contains(t, detail, "Alice")
	assert.Contains(t, detail, "Bob")
	assert.Contains(t, detail, "Carol")
	assert.NotContains(t, detail, "Dave", "only the first few are named for a large roster")
}

func TestMissingParticipantIDsIssue_NilWhenEveryRowHasAnID(t *testing.T) {
	players := []domain.Player{
		{ID: "00000000-0000-4000-8000-000000000000", Name: "Alice", Dojo: "Dojo A"},
	}
	assert.Nil(t, missingIDsIssue("participants.csv", helper.MissingParticipantIDsMessage(players)))
}

// bc-pnum ruling 1e follow-up: the single-competition detail endpoint
// (GET /api/viewer/competitions/:id) used to compute no dataIssues at all,
// so a competition whose OWN aggregate list entry named a missing-ids or
// corrupt-file issue lost that entry the moment the admin console's
// competition Overview loaded the detail (admin.jsx renders
// `detail?.config || c`, and `detail.config.dataIssues` did not exist).
// These two pin that the detail endpoint now reports the identical issues
// the aggregate does, via the shared viewerDataIssues.
func TestViewerDetail_ReportsParticipantsMissingIDs(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	legacy := "Dave, Dojo D\nEve, Dojo E\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "participants.csv"), []byte(legacy), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions/kendo", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	issues := dataIssuesFromResponse(t, w.Body.Bytes(), false, "kendo")
	require.Len(t, issues, 1, "the detail payload carries dataIssues, got %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "missing-ids", issue["kind"])
	detail, _ := issue["detail"].(string)
	assert.Contains(t, detail, "Dave")
	assert.Contains(t, detail, "Eve")
}

// TestViewerDetail_ReportsCorruptPoolsAndDoesNotFail is the corrupt-file
// half of the same pin: an unparseable pools.csv used to fail the WHOLE
// detail request (the pre-fix abort loop returned on ANY of the five
// concurrent loads' errors, including engine.CalculatePoolStandings' own
// internal LoadPools failing on the identical file), so the operator saw a
// 500 with no reason rather than a named, repairable fault. It now
// degrades, matching the aggregate's own resilience, and reports the
// located fault.
func TestViewerDetail_ReportsCorruptPoolsAndDoesNotFail(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "corrupt-pools-detail", Name: "Corrupt Pools", Status: state.CompStatusPools, Format: state.CompFormatMixed,
		Kind: "individual", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants("corrupt-pools-detail", []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice"},
		{Name: "Bob", Dojo: "Dojo Bob"},
	}))
	// The same corrupting bytes handlers_viewer_test.go's own corrupt-pools
	// fixture uses (a bare, unterminated quote): a genuine csv.Reader parse
	// failure, not a hand-built error.
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "corrupt-pools-detail", "pools.csv"),
		[]byte("a,b\na,\"bad\nquote"), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions/corrupt-pools-detail", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"an unparseable pools.csv must degrade, not fail the whole detail request; body: %s", w.Body.String())

	issues := dataIssuesFromResponse(t, w.Body.Bytes(), false, "corrupt-pools-detail")
	require.Len(t, issues, 1, "poolsErr and standingsErr report the identical fault and must not double up: %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "corrupt-file", issue["kind"])
	assert.Equal(t, "pools.csv", issue["file"])
	assert.NotEmpty(t, issue["detail"])
}

// TestViewerDetail_CorruptOverridesDegradesInsteadOfFailing is the
// overrides.json half of the same degrade pin as
// TestViewerDetail_ReportsCorruptPoolsAndDoesNotFail (bc-pnum FIX 1). Before
// the fix, loadOverridesLocked wrapped a JSON parse failure only in the
// plain state.ErrCorruptOverrides sentinel (errors.New, not a
// state.CorruptFileError), and computeStandingsFrom's own propagation of
// that error meant the detail endpoint's standingsErr carried it too --
// but state.AsCorruptFile could never match a bare sentinel, so the degrade
// loop's `if _, ok := state.AsCorruptFile(e); ok { continue }` fell through
// to its abort arm and the endpoint answered 500, exactly contradicting
// that loop's own comment naming overrides.json as a case that must
// degrade. This does not touch the write-path 422
// (respondIfCorruptOverrides); that mapping is asserted separately in
// handlers_match_test.go / handlers_league_tiebreak_test.go and is expected
// to still pass unchanged.
//
// Renamed from TestViewerDetail_ReportsCorruptOverridesAndDoesNotFail
// (bc-pnum review finding A): the old name promised the endpoint "reports"
// the corrupt overrides.json, but the body only ever asserted the 200
// status -- it pinned nothing about the degrade shape itself. standingsErr
// is the one carrier of this fault and is deliberately NEVER folded into
// dataIssues (see the degrade loop's own comment in handlers_viewer.go: the
// aggregate never computes standings, so doing so would make the two
// surfaces disagree about the same competition, and a corrupt
// overrides.json already has its own loud write-path 422 instead). So what
// this test now pins is the degrade SHAPE: standings comes back
// absent/null, no dataIssues entry is raised for it, and the REST of the
// payload -- config in particular -- is still genuinely served rather than
// the whole request failing.
func TestViewerDetail_CorruptOverridesDegradesInsteadOfFailing(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "corrupt-overrides-detail", Name: "Corrupt Overrides", Status: state.CompStatusPools, Format: state.CompFormatMixed,
		Kind: "individual", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants("corrupt-overrides-detail", []domain.Player{
		{Name: "Alice", Dojo: "Dojo Alice"},
		{Name: "Bob", Dojo: "Dojo Bob"},
	}))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "corrupt-overrides-detail", "overrides.json"),
		[]byte("{not valid json"), 0600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/viewer/competitions/corrupt-overrides-detail", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"a corrupt overrides.json must degrade the read path, not fail the whole detail request; body: %s", w.Body.String())

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))

	standings, hasStandings := payload["standings"]
	assert.Nil(t, standings, "standings must be absent or null when overrides.json is corrupt, got %#v (key present=%v)", standings, hasStandings)

	cfg, _ := payload["config"].(map[string]any)
	require.NotNil(t, cfg, "the rest of the payload must still be served despite the corrupt overrides.json")
	assert.Equal(t, "corrupt-overrides-detail", cfg["id"], "config must still be the real competition, not a stub")

	_, hasIssue := payload["dataIssues"]
	assert.False(t, hasIssue, "a corrupt overrides.json must not raise a dataIssues entry: it already has its own write-path 422 channel")
}

// dataIssueByFile finds the dataIssues entry naming file, or nil.
func dataIssueByFile(t *testing.T, issues []any, file string) map[string]any {
	t.Helper()
	for _, raw := range issues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if issue["file"] == file {
			return issue
		}
	}
	return nil
}

// dataIssuesFromResponse fetches dataIssues (possibly absent/empty) from a
// JSON response body shaped like either the aggregate's list (isAggregate
// true, filtered to the entry naming compID -- the ONE extractor every
// aggregate-or-detail test in this file goes through, whether the aggregate
// holds one competition or several) or the detail endpoint's single object
// (isAggregate false, compID unused).
func dataIssuesFromResponse(t *testing.T, body []byte, isAggregate bool, compID string) []any {
	t.Helper()
	if !isAggregate {
		var payload map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		issues, _ := payload["dataIssues"].([]any)
		return issues
	}
	var list []map[string]any
	require.NoError(t, json.Unmarshal(body, &list))
	for _, entry := range list {
		cfg, _ := entry["config"].(map[string]any)
		if cfg == nil || cfg["id"] != compID {
			continue
		}
		issues, _ := entry["dataIssues"].([]any)
		return issues
	}
	t.Fatalf("competition %q not in the aggregate payload", compID)
	return nil
}

// TestViewerAggregateAndDetail_PoolsMissingIDsAgree pins bc-pnum review
// finding 3/4: a legacy 7-column pools.csv (no id column at all, the
// pre-bc-pnum on-disk shape) that the load-time repair (bc-pnum,
// state.upgradePoolParticipantIDsLocked) still cannot resolve must raise
// the pools.csv "missing-ids" advisory, with the SAME wording, on BOTH the
// aggregate list and the single-competition detail endpoint.
//
// The roster's dojos deliberately do NOT match legacyPoolsCSVNoIDColumn's
// rows: the repair resolves a legacy row by an EXACT name+dojo match, so a
// mismatched dojo is what keeps this row genuinely unresolved (the residue
// case) rather than silently repaired out from under this test.
func TestViewerAggregateAndDetail_PoolsMissingIDsAgree(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo Z"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo Z"},
	}))
	legacyPools := legacyPoolsCSVNoIDColumn
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "pools.csv"), []byte(legacyPools), 0600))

	getIssues := func(url string, isAggregate bool) []any {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		return dataIssuesFromResponse(t, w.Body.Bytes(), isAggregate, "kendo")
	}

	aggIssues := getIssues("/api/viewer/competitions", true)
	detIssues := getIssues("/api/viewer/competitions/kendo", false)

	aggIssue := dataIssueByFile(t, aggIssues, "pools.csv")
	detIssue := dataIssueByFile(t, detIssues, "pools.csv")
	require.NotNil(t, aggIssue, "aggregate must report the pools.csv missing-ids issue")
	require.NotNil(t, detIssue, "detail must report the identical pools.csv missing-ids issue")
	assert.Equal(t, "missing-ids", aggIssue["kind"])
	assert.Equal(t, "missing-ids", detIssue["kind"])
	assert.Equal(t, aggIssue["detail"], detIssue["detail"], "both surfaces must use the exact same wording")
	detail, _ := aggIssue["detail"].(string)
	assert.Contains(t, detail, "Alice")
	assert.Contains(t, detail, "Bob")
	assert.Contains(t, detail, "could not be matched to a participant automatically")
}

// TestViewerAggregateAndDetail_PoolMatchesMissingIDsAgree pins the
// pool-matches.csv twin: a completed match missing SideAID that the
// load-time repair (bc-pnum, state.upgradePoolMatchSideIDsLocked) still
// cannot resolve must raise the "missing-ids" advisory identically on both
// surfaces.
//
// Two roster entries deliberately share the name "Alice" (different
// dojos): the repair only resolves a bare side name when it is UNIQUE
// across the roster, so this ambiguity is what keeps SideAID genuinely
// unresolved (the residue case) rather than silently repaired out from
// under this test.
func TestViewerAggregateAndDetail_PoolMatchesMissingIDsAgree(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: helper.NewUUID4(), Name: "Alice", Dojo: "Dojo A"},
		{ID: helper.NewUUID4(), Name: "Alice", Dojo: "Dojo C"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
	}))
	require.NoError(t, store.SavePoolMatches("kendo", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusCompleted, Winner: "Bob", WinnerID: bobID},
	}))

	getIssues := func(url string, isAggregate bool) []any {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		return dataIssuesFromResponse(t, w.Body.Bytes(), isAggregate, "kendo")
	}

	aggIssue := dataIssueByFile(t, getIssues("/api/viewer/competitions", true), "pool-matches.csv")
	detIssue := dataIssueByFile(t, getIssues("/api/viewer/competitions/kendo", false), "pool-matches.csv")
	require.NotNil(t, aggIssue, "aggregate must report the pool-matches.csv missing-ids issue")
	require.NotNil(t, detIssue, "detail must report the identical pool-matches.csv missing-ids issue")
	assert.Equal(t, "missing-ids", aggIssue["kind"])
	assert.Equal(t, aggIssue["detail"], detIssue["detail"])
	detail, _ := aggIssue["detail"].(string)
	assert.Contains(t, detail, "Alice vs Bob")
	assert.Contains(t, detail, "could not be resolved automatically")
	assert.Contains(t, detail, "regenerate the draw while it is still draw-ready to restore a missing side id")
}

// TestViewerAggregateAndDetail_NoIssuesWhenFullyStamped is the negative
// twin: a fully modern competition (every row on every one of the three
// files carries its id) must raise NEITHER new advisory on EITHER surface.
func TestViewerAggregateAndDetail_NoIssuesWhenFullyStamped(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo A"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
	}))
	require.NoError(t, store.SavePools("kendo", []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: aliceID, Name: "Alice", Dojo: "Dojo A"},
			{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches("kendo", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: aliceID},
	}))

	for _, tc := range []struct {
		name        string
		url         string
		isAggregate bool
	}{
		{"aggregate", "/api/viewer/competitions", true},
		{"detail", "/api/viewer/competitions/kendo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate, "kendo")
			assert.Nil(t, dataIssueByFile(t, issues, "pools.csv"), "a fully-stamped pools.csv raises no issue")
			assert.Nil(t, dataIssueByFile(t, issues, "pool-matches.csv"), "fully-stamped pool-matches raise no issue")
		})
	}
}

// TestViewerAggregateAndDetail_HikiwakeWithoutWinnerIDRaisesNoIssue pins the
// draw exclusion: a drawn (hikiwake) match records no Winner at all, so its
// empty WinnerID must NOT be mistaken for a missing-id row on either
// surface.
func TestViewerAggregateAndDetail_HikiwakeWithoutWinnerIDRaisesNoIssue(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo A"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
	}))
	require.NoError(t, store.SavePoolMatches("kendo", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake"},
	}))

	for _, tc := range []struct {
		name        string
		url         string
		isAggregate bool
	}{
		{"aggregate", "/api/viewer/competitions", true},
		{"detail", "/api/viewer/competitions/kendo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate, "kendo")
			assert.Nil(t, dataIssueByFile(t, issues, "pool-matches.csv"), "a hikiwake row with no Winner must not raise a missing-ids issue")
		})
	}
}

// TestViewerAggregateAndDetail_StraySetupPoolsCSVAgree pins bc-pnum review
// finding 4: the aggregate gates its pools.csv read on
// engine.CanGenerateDraw(comp.Status) (buildViewerCompetitionPayload) and
// the detail endpoint must apply the SAME gate before feeding pools into
// viewerDataIssues, or a setup-status competition with leftover pools.csv
// bytes (a discarded draw, a hand-placed file) shows the missing-ids notice
// on the detail endpoint only. Both must agree: NEITHER surface reports it
// while the competition is still draw-ready.
func TestViewerAggregateAndDetail_StraySetupPoolsCSVAgree(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: helper.NewUUID4(), Name: "Alice", Dojo: "Dojo A"},
		{ID: helper.NewUUID4(), Name: "Bob", Dojo: "Dojo B"},
	}))
	// Stray, readable, id-less pools.csv left over from a discarded draw.
	stray := legacyPoolsCSVNoIDColumn
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "pools.csv"), []byte(stray), 0600))

	for _, tc := range []struct {
		name        string
		url         string
		isAggregate bool
	}{
		{"aggregate", "/api/viewer/competitions", true},
		{"detail", "/api/viewer/competitions/kendo", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate, "kendo")
			assert.Nil(t, dataIssueByFile(t, issues, "pools.csv"),
				"a stray pools.csv on a still-draw-ready competition must raise NO issue on %s", tc.name)
		})
	}
}

// TestViewerAggregateAndDetail_CorruptPoolsCSVAgree pins that the
// corrupt-file half of viewerDataIssues reads the same on both builders,
// through the one drawInPoolsFile gate. A running pooled competition with NO
// number prefix used to report a corrupt pools.csv on the detail endpoint
// only: the aggregate's read lived inside the number merge, which skips
// without a prefix, so the dashboard list showed the competition as clean
// while its own page carried the loud banner. A knockout-only competition
// keeps its draw in bracket.json, so the same bytes at pools.csv are
// leftovers there and neither builder may report them.
func TestViewerAggregateAndDetail_CorruptPoolsCSVAgree(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	corrupt := []byte("a,b\na,\"bad\nquote")
	for _, comp := range []*state.Competition{
		{ID: "pooled", Name: "Pooled", Status: state.CompStatusPools, Format: state.CompFormatMixed, Kind: "individual", Courts: []string{"A"}},
		{ID: "knockout", Name: "Knockout", Status: state.CompStatusPlayoffs, Format: state.CompFormatPlayoffs, Kind: "individual", Courts: []string{"A"}},
	} {
		require.Empty(t, comp.NumberPrefix, "the fixture is the no-prefix shape on purpose")
		require.NoError(t, store.SaveCompetition(comp))
		require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
			{ID: helper.NewUUID4(), Name: "Alice", Dojo: "Dojo A"},
			{ID: helper.NewUUID4(), Name: "Bob", Dojo: "Dojo B"},
		}))
		require.NoError(t, os.WriteFile(
			filepath.Join(dir, "competitions", comp.ID, "pools.csv"), corrupt, 0600))
	}

	for _, tc := range []struct {
		name        string
		url         string
		compID      string
		isAggregate bool
		wantIssue   bool
	}{
		{"pooled aggregate", "/api/viewer/competitions", "pooled", true, true},
		{"pooled detail", "/api/viewer/competitions/pooled", "pooled", false, true},
		{"knockout aggregate", "/api/viewer/competitions", "knockout", true, false},
		{"knockout detail", "/api/viewer/competitions/knockout", "knockout", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(http.MethodGet, tc.url, nil)
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate, tc.compID)
			issue := dataIssueByFile(t, issues, "pools.csv")
			if !tc.wantIssue {
				assert.Nil(t, issue, "a knockout-only competition's draw is bracket.json; bytes at pools.csv are leftovers and must raise NO issue on %s", tc.name)
				return
			}
			require.NotNil(t, issue, "a corrupt pools.csv on a running pooled competition with no prefix must be reported on %s: %#v", tc.name, issues)
			assert.Equal(t, "corrupt-file", issue["kind"])
		})
	}
}

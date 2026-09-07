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

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)

	issues, ok := payload[0]["dataIssues"].([]any)
	require.True(t, ok, "the competition carries its data issues, got %#v", payload[0]["dataIssues"])
	require.Len(t, issues, 1)
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

	var payload []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	require.Len(t, payload, 1)

	issues, ok := payload[0]["dataIssues"].([]any)
	require.True(t, ok, "the competition carries its data issues, got %#v", payload[0]["dataIssues"])
	require.Len(t, issues, 1)
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

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	issues, ok := payload["dataIssues"].([]any)
	require.True(t, ok, "the detail payload carries dataIssues, got %#v", payload["dataIssues"])
	require.Len(t, issues, 1)
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

	var payload map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &payload))
	issues, ok := payload["dataIssues"].([]any)
	require.True(t, ok, "the detail payload carries dataIssues, got %#v", payload["dataIssues"])
	require.Len(t, issues, 1, "poolsErr and standingsErr report the identical fault and must not double up: %#v", issues)
	issue := issues[0].(map[string]any)
	assert.Equal(t, "corrupt-file", issue["kind"])
	assert.Equal(t, "pools.csv", issue["file"])
	assert.NotEmpty(t, issue["detail"])
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
// JSON response body shaped like either the aggregate's list entry or the
// detail endpoint's single object.
func dataIssuesFromResponse(t *testing.T, body []byte, isAggregate bool) []any {
	t.Helper()
	if isAggregate {
		var payload []map[string]any
		require.NoError(t, json.Unmarshal(body, &payload))
		require.Len(t, payload, 1)
		issues, _ := payload[0]["dataIssues"].([]any)
		return issues
	}
	var payload map[string]any
	require.NoError(t, json.Unmarshal(body, &payload))
	issues, _ := payload["dataIssues"].([]any)
	return issues
}

// TestViewerAggregateAndDetail_PoolsMissingIDsAgree pins bc-pnum review
// finding 3/4: a legacy 7-column pools.csv (no id column at all, the
// pre-bc-pnum on-disk shape) sitting alongside a fully modern (stamped)
// participants.csv must raise the pools.csv "missing-ids" advisory, with
// the SAME wording, on BOTH the aggregate list and the single-competition
// detail endpoint.
func TestViewerAggregateAndDetail_PoolsMissingIDsAgree(t *testing.T) {
	r, store, _, _, dir := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo A"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
	}))
	// Legacy 7-column pools.csv: PoolName,Name,Position,DisplayName,Dojo,Seed,Number
	// -- no 8th (id) column at all, the pre-bc-pnum on-disk shape.
	legacyPools := "Pool A,Alice,0,,Dojo A,,\nPool A,Bob,1,,Dojo B,,\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "kendo", "pools.csv"), []byte(legacyPools), 0600))

	getIssues := func(url string, isAggregate bool) []any {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(http.MethodGet, url, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		return dataIssuesFromResponse(t, w.Body.Bytes(), isAggregate)
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
	assert.Contains(t, detail, "regenerate the draw while it is still draw-ready")
}

// TestViewerAggregateAndDetail_PoolMatchesMissingIDsAgree pins the
// pool-matches.csv twin: a completed match missing SideAID must raise the
// "missing-ids" advisory identically on both surfaces.
func TestViewerAggregateAndDetail_PoolMatchesMissingIDsAgree(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "kendo", Name: "Kendo", Status: state.CompStatusPools, Format: state.CompFormatMixed,
	}))
	bobID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants("kendo", []domain.Player{
		{ID: helper.NewUUID4(), Name: "Alice", Dojo: "Dojo A"},
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
		return dataIssuesFromResponse(t, w.Body.Bytes(), isAggregate)
	}

	aggIssue := dataIssueByFile(t, getIssues("/api/viewer/competitions", true), "pool-matches.csv")
	detIssue := dataIssueByFile(t, getIssues("/api/viewer/competitions/kendo", false), "pool-matches.csv")
	require.NotNil(t, aggIssue, "aggregate must report the pool-matches.csv missing-ids issue")
	require.NotNil(t, detIssue, "detail must report the identical pool-matches.csv missing-ids issue")
	assert.Equal(t, "missing-ids", aggIssue["kind"])
	assert.Equal(t, aggIssue["detail"], detIssue["detail"])
	detail, _ := aggIssue["detail"].(string)
	assert.Contains(t, detail, "1 match(es)")
	assert.Contains(t, detail, "re-enter the results once the sides have ids")
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
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate)
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
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate)
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
	stray := "Pool A,Alice,0,,Dojo A,,\nPool A,Bob,1,,Dojo B,,\n"
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
			issues := dataIssuesFromResponse(t, w.Body.Bytes(), tc.isAggregate)
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
			var issues []any
			if tc.isAggregate {
				issues = aggregateDataIssuesFor(t, w.Body.Bytes(), tc.compID)
			} else {
				issues = dataIssuesFromResponse(t, w.Body.Bytes(), false)
			}
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

// aggregateDataIssuesFor picks one competition's dataIssues out of the
// aggregate list payload, so a test can hold several competitions at once.
func aggregateDataIssuesFor(t *testing.T, body []byte, compID string) []any {
	t.Helper()
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

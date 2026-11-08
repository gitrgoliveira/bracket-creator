package mobileapp

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

const backupTestPassword = "main-pw"

type backupTestRig struct {
	r     *gin.Engine
	store *state.Store
	hub   *Hub
	dir   string
}

// newBackupRouter builds the app's router over store, with its own hub.
func newBackupRouter(t *testing.T, store *state.Store) (*gin.Engine, *Hub) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}})
	hub := NewHub()
	r, _, limiter := NewRouterWithHub(store, engine.New(store), res, NewFileVerifier(store), hub, false)
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)
	return r, hub
}

// newBackupRig is a tournament with two competitions, alpha (prefix A) and
// beta (prefix B).
func newBackupRig(t *testing.T) *backupTestRig {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Backup Taikai", Date: "01-06-2026", Venue: "Dojo", Courts: []string{"A"},
		Password: backupTestPassword, Mode: state.TournamentModeOfficiated,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "alpha", Name: "Alpha", NumberPrefix: "A", Date: "01-06-2026", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "beta", Name: "Beta", NumberPrefix: "B", Date: "01-06-2026", Courts: []string{"A"}}))
	r, hub := newBackupRouter(t, store)
	return &backupTestRig{r: r, store: store, hub: hub, dir: dir}
}

func backupGet(t *testing.T, r http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Tournament-Password", backupTestPassword)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// takeBackup downloads a backup and returns its bytes.
func takeBackup(t *testing.T, r http.Handler, path string) []byte {
	t.Helper()
	w := backupGet(t, r, path)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	return w.Body.Bytes()
}

// restorePost sends a plan or a restore: archive as the uploaded file when
// non-nil, and fields as form fields. signIn and confirm go in the headers
// when non-empty.
func restorePost(t *testing.T, r http.Handler, path string, archive []byte, fields map[string]string, signIn, confirm string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	if archive != nil {
		fw, err := mw.CreateFormFile("archive", "backup.zip")
		require.NoError(t, err)
		_, err = fw.Write(archive)
		require.NoError(t, err)
	}
	for k, v := range fields {
		require.NoError(t, mw.WriteField(k, v))
	}
	require.NoError(t, mw.Close())
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if signIn != "" {
		req.Header.Set("X-Tournament-Password", signIn)
	}
	if confirm != "" {
		req.Header.Set("X-Confirm-Password", confirm)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decisionsJSON(t *testing.T, d map[string]state.RestoreDecision) string {
	t.Helper()
	raw, err := json.Marshal(d)
	require.NoError(t, err)
	return string(raw)
}

// folderHash hashes every path and file under dir, working folders included,
// so any change a refusal made shows.
func folderHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	var paths []string
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, p)
		return nil
	}))
	sort.Strings(paths)
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		require.NoError(t, err)
		h.Write([]byte(rel + "\x00"))
		info, err := os.Lstat(p)
		require.NoError(t, err)
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(p)
			require.NoError(t, err)
			h.Write(data)
		}
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func responseCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
	return body.Code
}

func readArchive(t *testing.T, data []byte) *backup.Archive {
	t.Helper()
	a, err := backup.Read(bytes.NewReader(data), int64(len(data)), backupReadChecks)
	require.NoError(t, err)
	return a
}

func TestBackup_DownloadsATournamentAndACompetition(t *testing.T) {
	rig := newBackupRig(t)

	w := backupGet(t, rig.r, "/api/tournament/backup")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "application/zip", w.Header().Get("Content-Type"))
	assert.Regexp(t, `^attachment; filename="backup-taikai-\d{8}T\d{6}Z\.zip"$`, w.Header().Get("Content-Disposition"))
	a := readArchive(t, w.Body.Bytes())
	assert.Equal(t, backup.ScopeTournament, a.Manifest.Scope)
	assert.Equal(t, backup.KindManual, a.Manifest.Kind)
	assert.Len(t, a.Manifest.Competitions, 2)
	for p, data := range a.Files {
		assert.NotContains(t, string(data), backupTestPassword, "%s carries the password", p)
	}

	w = backupGet(t, rig.r, "/api/competitions/alpha/backup")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Header().Get("Content-Disposition"), `filename="backup-taikai-alpha-`)
	a = readArchive(t, w.Body.Bytes())
	assert.Equal(t, backup.ScopeCompetition, a.Manifest.Scope)
	require.Len(t, a.Manifest.Competitions, 1)
	assert.Equal(t, "alpha", a.Manifest.Competitions[0].ID)

	assert.Equal(t, http.StatusNotFound, backupGet(t, rig.r, "/api/competitions/nobody/backup").Code)
	assert.Equal(t, http.StatusBadRequest, backupGet(t, rig.r, "/api/competitions/Not%20An%20Id/backup").Code)
}

// laterVersion is an app version later than any release this code will be
// built as, so its backups are refused whatever version the test binary has.
const laterVersion = "v999.0.0"

// Every refusal leaves the data folder exactly as it was.
func TestRestore_RefusalsLeaveTheFolderUnchanged(t *testing.T) {
	rig := newBackupRig(t)
	tournamentArchive := takeBackup(t, rig.r, "/api/tournament/backup")
	// A backup a later version made is never restored. v999.0.0 is later than
	// any release, and has no order against a build named for its branch.
	otherVersion, err := bctest.ArchiveWithAppVersion(tournamentArchive, laterVersion)
	require.NoError(t, err)
	// The same, kept on this computer (a daily copy the later version took).
	const otherVersionCopy = "20260102T000000Z-scheduled.zip"
	require.NoError(t, os.MkdirAll(filepath.Join(rig.dir, state.BackupsDirName), 0o750))
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, state.BackupsDirName, otherVersionCopy), otherVersion, 0o600))

	cases := []struct {
		name    string
		path    string
		archive []byte
		fields  map[string]string
		confirm string
		status  int
		code    string
	}{
		{"not a zip", "/api/tournament/restore", []byte("not a zip at all"), nil, backupTestPassword, http.StatusBadRequest, backupCodeArchiveInvalid},
		{"a later version's backup", "/api/tournament/restore", otherVersion, nil, backupTestPassword, http.StatusBadRequest, backupCodeVersionMismatch},
		{"plan of a later version's backup", "/api/tournament/restore/plan", otherVersion, nil, "", http.StatusBadRequest, backupCodeVersionMismatch},
		{"a later version's kept copy", "/api/tournament/restore", nil, map[string]string{"from": otherVersionCopy}, backupTestPassword, http.StatusBadRequest, backupCodeVersionMismatch},
		{"plan of a later version's kept copy", "/api/tournament/restore/plan", nil, map[string]string{"from": otherVersionCopy}, "", http.StatusBadRequest, backupCodeVersionMismatch},
		{"no archive", "/api/tournament/restore", nil, nil, backupTestPassword, http.StatusBadRequest, backupCodeBadRequest},
		{"both archive and from", "/api/tournament/restore", tournamentArchive, map[string]string{"from": "20260101T000000Z-scheduled.zip"}, backupTestPassword, http.StatusBadRequest, backupCodeBadRequest},
		{"from a bad name", "/api/tournament/restore", nil, map[string]string{"from": "../tournament.md"}, backupTestPassword, http.StatusBadRequest, backupCodeBadBackupName},
		{"from an unknown copy", "/api/tournament/restore", nil, map[string]string{"from": "20260101T000000Z-scheduled.zip"}, backupTestPassword, http.StatusNotFound, backupCodeBackupNotFound},
		{"plan from an unknown copy", "/api/tournament/restore/plan", nil, map[string]string{"from": "20260101T000000Z-scheduled.zip"}, "", http.StatusNotFound, backupCodeBackupNotFound},
		{"undo of a manual backup", "/api/tournament/restore", tournamentArchive, map[string]string{"undo": "true"}, backupTestPassword, http.StatusBadRequest, state.RestoreCodeNotASafetyCopy},
		{"choices not given", "/api/tournament/restore", tournamentArchive, nil, backupTestPassword, http.StatusConflict, state.RestoreCodeDecisionMissing},
		{"choices unreadable", "/api/tournament/restore", tournamentArchive, map[string]string{"decisions": "{not json"}, backupTestPassword, http.StatusBadRequest, backupCodeBadRequest},
		{"undo not a boolean", "/api/tournament/restore", tournamentArchive, map[string]string{"undo": "maybe"}, backupTestPassword, http.StatusBadRequest, backupCodeBadRequest},
		{"only a bad id", "/api/tournament/restore", tournamentArchive, map[string]string{"only": "Not An Id"}, backupTestPassword, http.StatusBadRequest, backupCodeBadRequest},
		{"no confirm password", "/api/tournament/restore", tournamentArchive, nil, "", http.StatusUnauthorized, confirmPasswordRequiredCode},
		{"wrong confirm password", "/api/tournament/restore", tournamentArchive, nil, "not-it", http.StatusUnauthorized, confirmPasswordWrongCode},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := folderHash(t, rig.dir)
			w := restorePost(t, rig.r, tc.path, tc.archive, tc.fields, backupTestPassword, tc.confirm)
			assert.Equal(t, tc.status, w.Code, w.Body.String())
			assert.Equal(t, tc.code, responseCode(t, w))
			assert.Equal(t, before, folderHash(t, rig.dir), "a refusal changed the data folder")
		})
	}

	// The refusal names both versions, in its sentence and as fields.
	w := restorePost(t, rig.r, "/api/tournament/restore/plan", otherVersion, nil, backupTestPassword, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	var body struct {
		Error         string `json:"error"`
		BackupVersion string `json:"backupVersion"`
		AppVersion    string `json:"appVersion"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	running := strings.TrimSpace(version.GetVersion())
	assert.Equal(t, laterVersion, body.BackupVersion)
	assert.Equal(t, running, body.AppVersion)
	// The sentence is CheckAppVersion's (its own test pins the wording).
	assert.Equal(t, backup.CheckAppVersion(backup.Manifest{AppVersion: laterVersion}, running).Detail, body.Error)
	assert.True(t, strings.HasPrefix(body.Error, "This backup was made with version "+laterVersion+" and this app is version "+running+"."), body.Error)
}

// A backup an earlier version made restores on a later one, over HTTP as in
// the store: the plan and the restore both go ahead. The running version is
// set, since the test binary's own (a branch name) has no release order.
func TestRestore_AnEarlierVersionsBackupRestores(t *testing.T) {
	saved := runningAppVersion
	runningAppVersion = func() string { return "v2.5.0" }
	t.Cleanup(func() { runningAppVersion = saved })

	rig := newBackupRig(t)
	archive, err := bctest.ArchiveWithAppVersion(takeBackup(t, rig.r, "/api/tournament/backup"), "v2.4.0")
	require.NoError(t, err)

	w := restorePost(t, rig.r, "/api/tournament/restore/plan", archive, nil, backupTestPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = restorePost(t, rig.r, "/api/tournament/restore", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
			"alpha": {Action: state.RestoreReplace},
			"beta":  {Action: state.RestoreReplace},
		})},
		backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// The same backup made by a later version than the one running is refused.
	runningAppVersion = func() string { return "v2.3.5" }
	w = restorePost(t, rig.r, "/api/tournament/restore/plan", archive, nil, backupTestPassword, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, backupCodeVersionMismatch, responseCode(t, w))
}

func TestRestore_DecisionMissingNamesTheCompetitions(t *testing.T) {
	rig := newBackupRig(t)
	archive := takeBackup(t, rig.r, "/api/tournament/backup")
	w := restorePost(t, rig.r, "/api/tournament/restore", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{"alpha": {Action: state.RestoreReplace}})},
		backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body struct {
		Code           string   `json:"code"`
		CompetitionIDs []string `json:"competitionIds"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, state.RestoreCodeDecisionMissing, body.Code)
	assert.Equal(t, []string{"beta"}, body.CompetitionIDs)
}

// The dry run never asks for the password again, changes nothing and leaves
// no staging folder behind.
func TestRestorePlan_ChangesNothingAndNeedsOnlyTheSignIn(t *testing.T) {
	rig := newBackupRig(t)
	archive := takeBackup(t, rig.r, "/api/tournament/backup")
	before := folderHash(t, rig.dir)

	w := restorePost(t, rig.r, "/api/tournament/restore/plan", archive, nil, backupTestPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var plan state.RestorePlan
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &plan))
	assert.ElementsMatch(t, []string{"alpha", "beta"}, plan.Missing)
	for _, c := range plan.Competitions {
		assert.True(t, c.Asked, "%s must be asked", c.ID)
	}
	assert.Equal(t, before, folderHash(t, rig.dir))
	entries, err := os.ReadDir(filepath.Join(rig.dir, backup.RestoreDirName))
	if err == nil {
		assert.Empty(t, entries, "the plan left a staging folder")
	} else {
		assert.ErrorIs(t, err, fs.ErrNotExist)
	}

	// Without the sign-in the plan is refused like every organiser route.
	w = restorePost(t, rig.r, "/api/tournament/restore/plan", archive, nil, "", "")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// The plan reads the same decisions the restore takes, so a copy name already
// used is reported before the restore asks for the password again; a bad
// decision is refused there too.
func TestRestorePlan_AppliesDecisionsAndReportsATakenCopyName(t *testing.T) {
	rig := newBackupRig(t)
	archive := takeBackup(t, rig.r, "/api/tournament/backup")
	before := folderHash(t, rig.dir)

	w := restorePost(t, rig.r, "/api/tournament/restore/plan", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
			"alpha": {Action: state.RestoreCopy, CopyName: "Beta"},
			"beta":  {Action: state.RestoreKeep},
		})},
		backupTestPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var plan state.RestorePlan
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &plan))
	assert.Empty(t, plan.Missing)
	byID := map[string]state.RestoreCompetitionPlan{}
	for _, c := range plan.Competitions {
		byID[c.ID] = c
	}
	assert.True(t, byID["alpha"].CopyNameTaken, "Beta stays in place, so its name is taken")
	assert.Empty(t, byID["alpha"].NewID)
	assert.Equal(t, state.RestoreKeep, byID["beta"].Action)
	assert.Equal(t, before, folderHash(t, rig.dir), "a plan changes nothing")

	w = restorePost(t, rig.r, "/api/tournament/restore/plan", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
			"alpha": {Action: state.RestoreCopy, CopyName: "Alpha Two"},
		})},
		backupTestPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var plan2 state.RestorePlan
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &plan2))
	assert.Equal(t, []string{"beta"}, plan2.Missing, "an unanswered question is listed, not refused")
	for _, c := range plan2.Competitions {
		if c.ID == "alpha" {
			assert.Equal(t, "alpha-two", c.NewID)
			assert.False(t, c.CopyNameTaken)
		}
	}

	w = restorePost(t, rig.r, "/api/tournament/restore/plan", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{"alpha": {Action: state.RestoreRemove}})},
		backupTestPassword, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, state.RestoreCodeBadDecision, responseCode(t, w))

	w = restorePost(t, rig.r, "/api/tournament/restore/plan", archive,
		map[string]string{"decisions": "{not json"}, backupTestPassword, "")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, backupCodeBadRequest, responseCode(t, w))
}

// waitForEvent reports whether the hub sends an event of type within a second.
func waitForEvent(t *testing.T, ch chan historyEntry, eventType EventType) bool {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		select {
		case e := <-ch:
			if strings.Contains(e.payload, string(eventType)) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestRestore_ReplaceWritesASafetyCopyAndAnnouncesIt(t *testing.T) {
	rig := newBackupRig(t)
	archive := takeBackup(t, rig.r, "/api/tournament/backup")

	alpha, err := rig.store.LoadCompetition("alpha")
	require.NoError(t, err)
	alpha.Name = "Alpha Renamed"
	require.NoError(t, rig.store.SaveCompetition(alpha))

	ch := rig.hub.Subscribe()
	defer rig.hub.Unsubscribe(ch)
	w := restorePost(t, rig.r, "/api/tournament/restore", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
			"alpha": {Action: state.RestoreReplace},
			"beta":  {Action: state.RestoreReplace},
		})},
		backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		SafetyCopy     string          `json:"safetyCopy"`
		PrefixWarnings []PrefixWarning `json:"prefixWarnings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Regexp(t, `^\d{8}T\d{6}Z-before-restore-tournament(-\d+)?\.zip$`, resp.SafetyCopy)
	assert.Empty(t, resp.PrefixWarnings)
	assert.True(t, waitForEvent(t, ch, EventTournamentUpdated), "the restore must announce tournament_updated")

	alpha, err = rig.store.LoadCompetition("alpha")
	require.NoError(t, err)
	assert.Equal(t, "Alpha", alpha.Name)

	// The safety copy holds what the restore replaced, and is kept.
	w = backupGet(t, rig.r, "/api/tournament/backups/"+resp.SafetyCopy)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	a := readArchive(t, w.Body.Bytes())
	assert.Equal(t, backup.KindBeforeRestore, a.Manifest.Kind)
	assert.Contains(t, string(a.Files["competitions/alpha/config.md"]), "Alpha Renamed")

	// The sign-in password is unchanged.
	tourn, err := rig.store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, backupTestPassword, tourn.Password)
}

// A copy of a competition takes its number prefix, which the original holds:
// the copy gets a new one and the operator is told to reprint its tags.
// Undoing the restore through its safety copy removes the copy again.
func TestRestore_CopyGetsANewPrefixAndUndoRemovesIt(t *testing.T) {
	rig := newBackupRig(t)
	archive := takeBackup(t, rig.r, "/api/competitions/alpha/backup")

	w := restorePost(t, rig.r, "/api/tournament/restore", archive,
		map[string]string{"decisions": decisionsJSON(t, map[string]state.RestoreDecision{
			"alpha": {Action: state.RestoreCopy, CopyName: "Alpha Copy"},
		})},
		backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp struct {
		SafetyCopy     string          `json:"safetyCopy"`
		PrefixWarnings []PrefixWarning `json:"prefixWarnings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.PrefixWarnings, 1, w.Body.String())
	pw := resp.PrefixWarnings[0]
	assert.Equal(t, "alpha-copy", pw.CompetitionID)
	assert.Equal(t, "A", pw.OldPrefix)
	assert.NotEmpty(t, pw.NewPrefix)
	assert.NotEqual(t, "A", pw.NewPrefix)
	assert.Contains(t, pw.Message, "reprint")

	cp, err := rig.store.LoadCompetition("alpha-copy")
	require.NoError(t, err)
	require.NotNil(t, cp)
	assert.Equal(t, pw.NewPrefix, cp.NumberPrefix)
	orig, err := rig.store.LoadCompetition("alpha")
	require.NoError(t, err)
	assert.Equal(t, "A", orig.NumberPrefix, "the original keeps its prefix")
	pending, err := rig.store.PrefixesPending()
	require.NoError(t, err)
	assert.Empty(t, pending)

	// Undo, from the kept safety copy.
	fields := map[string]string{"from": resp.SafetyCopy, "undo": "true"}
	w = restorePost(t, rig.r, "/api/tournament/restore/plan", nil, fields, backupTestPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = restorePost(t, rig.r, "/api/tournament/restore", nil, fields, backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	cp, err = rig.store.LoadCompetition("alpha-copy")
	require.NoError(t, err)
	assert.Nil(t, cp, "the undo removes the competition the restore created")
	orig, err = rig.store.LoadCompetition("alpha")
	require.NoError(t, err)
	assert.NotNil(t, orig)
}

func TestBackups_ListDownloadAndDelete(t *testing.T) {
	rig := newBackupRig(t)
	snap, err := rig.store.Snapshot(state.ScopeTournament, "")
	require.NoError(t, err)
	at := time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC)
	data, err := SnapshotArchive(snap, backup.KindScheduled, at, nil)
	require.NoError(t, err)
	name, err := rig.store.SaveBackupFile(state.BackupKindScheduled, state.ScopeTournament, at, data)
	require.NoError(t, err)

	w := backupGet(t, rig.r, "/api/tournament/backups")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Backups []struct {
			Name  string `json:"name"`
			Kind  string `json:"kind"`
			Scope string `json:"scope"`
			Time  string `json:"time"`
			Size  int64  `json:"size"`
		} `json:"backups"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Backups, 1)
	assert.Equal(t, name, list.Backups[0].Name)
	assert.Equal(t, "scheduled", list.Backups[0].Kind)
	assert.Equal(t, "tournament", list.Backups[0].Scope)
	assert.Equal(t, "2026-06-01T09:00:00Z", list.Backups[0].Time)
	assert.Equal(t, int64(len(data)), list.Backups[0].Size)

	w = backupGet(t, rig.r, "/api/tournament/backups/"+name)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, data, w.Body.Bytes())
	assert.Equal(t, http.StatusBadRequest, backupGet(t, rig.r, "/api/tournament/backups/notes.txt").Code)
	assert.Equal(t, http.StatusNotFound, backupGet(t, rig.r, "/api/tournament/backups/20200101T000000Z-scheduled.zip").Code)

	del := func(confirm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/api/tournament/backups/"+name, nil)
		req.Header.Set("X-Tournament-Password", backupTestPassword)
		if confirm != "" {
			req.Header.Set("X-Confirm-Password", confirm)
		}
		w := httptest.NewRecorder()
		rig.r.ServeHTTP(w, req)
		return w
	}
	w = del("")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, confirmPasswordRequiredCode, responseCode(t, w))
	w = del("not-it")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, confirmPasswordWrongCode, responseCode(t, w))
	files, err := rig.store.ListBackupFiles()
	require.NoError(t, err)
	assert.Len(t, files, 1, "a refused delete keeps the copy")

	w = del(backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	files, err = rig.store.ListBackupFiles()
	require.NoError(t, err)
	assert.Empty(t, files)
	assert.Equal(t, http.StatusNotFound, del(backupTestPassword).Code)
}

// A restore a crash stopped before its last step left prefixes-pending.json;
// the server gives those competitions their new prefixes before it serves.
func TestServerStart_ResolvesPrefixesARestoreLeftPending(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Backup Taikai", Date: "01-06-2026", Venue: "Dojo", Courts: []string{"A"},
		Password: backupTestPassword, Mode: state.TournamentModeOfficiated,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "alpha", Name: "Alpha", NumberPrefix: "A", Date: "01-06-2026", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "alpha-copy", Name: "Alpha Copy", NumberPrefix: "A", Date: "01-06-2026", Courts: []string{"A"}}))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, backup.RestoreDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, backup.RestoreDirName, "prefixes-pending.json"), []byte(`{"ids":["alpha-copy"]}`), 0o600))
	pending, err := store.PrefixesPending()
	require.NoError(t, err)
	require.Equal(t, []string{"alpha-copy"}, pending)

	newBackupRouter(t, store)

	cp, err := store.LoadCompetition("alpha-copy")
	require.NoError(t, err)
	assert.NotEqual(t, "A", cp.NumberPrefix)
	orig, err := store.LoadCompetition("alpha")
	require.NoError(t, err)
	assert.Equal(t, "A", orig.NumberPrefix)
	pending, err = store.PrefixesPending()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

// ReassignRestoredPrefixes leaves a competition whose prefix collides with
// nobody alone, and skips one the folder no longer holds.
func TestReassignRestoredPrefixes_LeavesAFreePrefixAlone(t *testing.T) {
	rig := newBackupRig(t)
	warnings, err := ReassignRestoredPrefixes(rig.store, engine.New(rig.store), []string{"alpha", "gone"})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	alpha, err := rig.store.LoadCompetition("alpha")
	require.NoError(t, err)
	assert.Equal(t, "A", alpha.NumberPrefix)
}

type spyPrefixEngine struct {
	PrefixEngine
	renumbered []string
}

func (s *spyPrefixEngine) RenumberCompetitors(compID string) (bool, error) {
	s.renumbered = append(s.renumbered, compID)
	return s.PrefixEngine.RenumberCompetitors(compID)
}

func TestReassignRestoredPrefixes_RenumbersEvenWhenPrefixNoLongerCollides(t *testing.T) {
	rig := newBackupRig(t)
	spy := &spyPrefixEngine{PrefixEngine: engine.New(rig.store)}
	// "alpha" exists and prefix "A" does not collide with anyone else ("beta" has "B").
	// "gone" does not exist in the folder.
	warnings, err := ReassignRestoredPrefixes(rig.store, spy, []string{"alpha", "gone"})
	require.NoError(t, err)
	assert.Empty(t, warnings)
	assert.Equal(t, []string{"alpha"}, spy.renumbered)
}

func TestResolveRestoredRequeues_RequeuesPendingMatches(t *testing.T) {
	rig := newBackupRig(t)
	m := state.MatchResult{
		ID: "Pool A-1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2",
		Status: state.MatchStatusRunning, Court: "A", IpponsA: []string{"M"},
	}
	require.NoError(t, rig.store.SavePoolMatches("alpha", []state.MatchResult{m}))
	bm := state.BracketMatch{
		ID: "r1-m1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2",
		Status: state.MatchStatusRunning, Court: "A",
	}
	b := &state.Bracket{Rounds: [][]state.BracketMatch{{bm}}}
	require.NoError(t, rig.store.SaveBracket("alpha", b))

	require.NoError(t, os.MkdirAll(filepath.Join(rig.dir, backup.RestoreDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, backup.RestoreDirName, "requeue-pending.json"),
		[]byte(`{"alpha":["Pool A-1","r1-m1"]}`), 0o600))

	err := ResolveRestoredRequeues(rig.store, engine.New(rig.store))
	require.NoError(t, err)

	pm, err := rig.store.LoadPoolMatches("alpha")
	require.NoError(t, err)
	require.Len(t, pm, 1)
	assert.Equal(t, state.MatchStatusScheduled, pm[0].Status)
	assert.Equal(t, []string{"M"}, pm[0].IpponsA)

	loadedBracket, err := rig.store.LoadBracket("alpha")
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusScheduled, loadedBracket.Rounds[0][0].Status)

	hist, err := rig.store.LoadMatchHistory("alpha", "Pool A-1")
	require.NoError(t, err)
	require.Len(t, hist, 1)
	assert.Equal(t, "requeue", hist[0].Door)

	pending, err := rig.store.RequeuePending()
	require.NoError(t, err)
	assert.Empty(t, pending)
	_, err = os.Stat(filepath.Join(rig.dir, backup.RestoreDirName, "requeue-pending.json"))
	assert.True(t, os.IsNotExist(err))
}

func TestResolveRestoredRequeues_DropsCompletedAndMissingMatchesWithoutError(t *testing.T) {
	rig := newBackupRig(t)
	m := state.MatchResult{
		ID: "Pool A-1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2",
		Status: state.MatchStatusCompleted, Court: "A", Winner: "Alice", WinnerID: "p1",
	}
	require.NoError(t, rig.store.SavePoolMatches("alpha", []state.MatchResult{m}))
	require.NoError(t, os.MkdirAll(filepath.Join(rig.dir, backup.RestoreDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, backup.RestoreDirName, "requeue-pending.json"),
		[]byte(`{"alpha":["Pool A-1","ghost-match"],"ghost-comp":["m1"]}`), 0o600))

	err := ResolveRestoredRequeues(rig.store, engine.New(rig.store))
	require.NoError(t, err)

	pending, err := rig.store.RequeuePending()
	require.NoError(t, err)
	assert.Empty(t, pending)
	_, err = os.Stat(filepath.Join(rig.dir, backup.RestoreDirName, "requeue-pending.json"))
	assert.True(t, os.IsNotExist(err))
}

type failingRequeueEngine struct {
	err error
}

func (f *failingRequeueEngine) RevertMatchToQueue(compID, matchID string, opts ...engine.ForceOptions) error {
	return f.err
}

func TestResolveRestoredRequeues_ErrorLeavesEntryPending(t *testing.T) {
	rig := newBackupRig(t)
	require.NoError(t, os.MkdirAll(filepath.Join(rig.dir, backup.RestoreDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(rig.dir, backup.RestoreDirName, "requeue-pending.json"),
		[]byte(`{"alpha":["Pool A-1"]}`), 0o600))

	boom := errors.New("boom")
	err := ResolveRestoredRequeues(rig.store, &failingRequeueEngine{err: boom})
	require.ErrorIs(t, err, boom)

	pending, err := rig.store.RequeuePending()
	require.NoError(t, err)
	assert.Equal(t, []string{"Pool A-1"}, pending["alpha"])
}

func TestRestoreHandler_EndToEndRequeuesRunningMatchWithScore(t *testing.T) {
	srcRig := newBackupRig(t)
	m := state.MatchResult{
		ID: "Pool A-1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2",
		Status: state.MatchStatusRunning, Court: "A", IpponsA: []string{"M"},
	}
	require.NoError(t, srcRig.store.SavePoolMatches("alpha", []state.MatchResult{m}))
	archiveData := takeBackup(t, srcRig.r, "/api/tournament/backup")

	dstRig := newBackupRig(t)
	w := restorePost(t, dstRig.r, "/api/tournament/restore", archiveData, map[string]string{
		"decisions":    `{"alpha":{"action":"replace"},"beta":{"action":"replace"}}`,
		"allowRunning": "true",
	}, backupTestPassword, backupTestPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := dstRig.store.LoadPoolMatches("alpha")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusScheduled, matches[0].Status, "match should be requeued to scheduled")
	assert.Equal(t, []string{"M"}, matches[0].IpponsA, "score points must be intact")

	pending, err := dstRig.store.RequeuePending()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestServerStart_ResolvesRequeuesARestoreLeftPending(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Backup Taikai", Date: "01-06-2026", Venue: "Dojo", Courts: []string{"A"},
		Password: backupTestPassword, Mode: state.TournamentModeOfficiated,
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "alpha", Name: "Alpha", NumberPrefix: "A", Date: "01-06-2026", Courts: []string{"A"}}))
	m := state.MatchResult{
		ID: "Pool A-1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2",
		Status: state.MatchStatusRunning, Court: "A", IpponsA: []string{"K"},
	}
	require.NoError(t, store.SavePoolMatches("alpha", []state.MatchResult{m}))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, backup.RestoreDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, backup.RestoreDirName, "requeue-pending.json"),
		[]byte(`{"alpha":["Pool A-1"]}`), 0o600))

	pending, err := store.RequeuePending()
	require.NoError(t, err)
	require.Equal(t, []string{"Pool A-1"}, pending["alpha"])

	newBackupRouter(t, store)

	ms, err := store.LoadPoolMatches("alpha")
	require.NoError(t, err)
	require.Len(t, ms, 1)
	assert.Equal(t, state.MatchStatusScheduled, ms[0].Status)
	assert.Equal(t, []string{"K"}, ms[0].IpponsA)

	pending, err = store.RequeuePending()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

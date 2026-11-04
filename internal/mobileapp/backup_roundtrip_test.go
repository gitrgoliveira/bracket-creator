package mobileapp

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

var roundTripPasswordLine = regexp.MustCompile(`(?m)^password:.*\n`)

// dataFiles reads every file of a data folder a backup carries: the store's
// working folders and temporary files are left out, and tournament.md's
// password line is dropped, since a restore never takes the password from
// the archive.
func dataFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if backup.IsExcludedPath(rel) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || backup.IsTempFileName(d.Name()) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if rel == "tournament.md" {
			data = roundTripPasswordLine.ReplaceAll(data, nil)
		}
		out[rel] = string(data)
		return nil
	}))
	return out
}

// The populated tournament, backed up and restored into a folder whose
// tournament was just created with its own password, gives the same files and
// the same answers, and the new folder's password still signs in.
func TestBackup_RoundTripIntoAFreshFolder(t *testing.T) {
	pt := buildPopulatedTournament(t)
	archive := takeBackupAs(t, pt.Router, pt.Password, "/api/tournament/backup")

	freshDir := t.TempDir()
	freshStore, err := state.NewStore(freshDir)
	require.NoError(t, err)
	freshRouter, _ := newBackupRouter(t, freshStore)
	const freshPassword = "fresh-pw"
	w := populatedServeJSON(t, freshRouter, "", http.MethodPost, "/api/tournament", map[string]any{
		"name": "Fresh Taikai", "date": "10-05-2026", "venue": "Hall", "courts": []string{"A"}, "password": freshPassword,
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	w = restorePost(t, freshRouter, "/api/tournament/restore/plan", archive, nil, freshPassword, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"password":"kept"`)
	w = restorePost(t, freshRouter, "/api/tournament/restore", archive, nil, freshPassword, freshPassword)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// A store opened fresh on the restored folder, compared with one opened
	// fresh on the source folder: opening a store runs the same load-time
	// repairs on both, and both answer from disk rather than from the source
	// store's in-memory caches.
	reopened, err := state.NewStore(freshDir)
	require.NoError(t, err)
	reopenedRouter, _ := newBackupRouter(t, reopened)
	source, err := state.NewStore(pt.Dir)
	require.NoError(t, err)
	sourceRouter, _ := newBackupRouter(t, source)

	want := dataFiles(t, pt.Dir)
	got := dataFiles(t, freshDir)
	for _, dir := range []string{"branding/", "sponsors/", "events/"} {
		found := false
		for p := range want {
			if strings.HasPrefix(p, dir) || strings.Contains(p, "/"+dir) {
				found = true
				break
			}
		}
		assert.True(t, found, "the fixture holds no %s file, so the round trip proves nothing about it", dir)
	}
	assert.Equal(t, keysOf(want), keysOf(got))
	for p, data := range want {
		assert.Equal(t, data, got[p], "%s differs after the round trip", p)
	}

	paths := []string{"/api/viewer/tournament", "/api/viewer/competitions", "/api/competitions"}
	for _, id := range []string{pt.MixedCompID, pt.KnockoutCompID, pt.TeamCompID, pt.KachinukiCompID, pt.SelfRunCompID} {
		paths = append(paths, "/api/viewer/competitions/"+id, "/api/competitions/"+id)
	}
	for _, p := range paths {
		a := populatedServeJSON(t, sourceRouter, pt.Password, http.MethodGet, p, nil)
		b := populatedServeJSON(t, reopenedRouter, freshPassword, http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, a.Code, "%s: %s", p, a.Body.String())
		assert.Equal(t, a.Code, b.Code, p)
		assert.JSONEq(t, a.Body.String(), b.Body.String(), "%s answers differently after the round trip", p)
	}

	// The password set at creation signs in; the source folder's does not.
	assert.Equal(t, http.StatusOK, populatedServeJSON(t, reopenedRouter, freshPassword, http.MethodGet, "/api/tournament", nil).Code)
	assert.Equal(t, http.StatusUnauthorized, populatedServeJSON(t, reopenedRouter, pt.Password, http.MethodGet, "/api/tournament", nil).Code)

	// No archive carries the password.
	for p, data := range readArchive(t, archive).Files {
		assert.NotContains(t, string(data), pt.Password, "%s carries the password", p)
	}
}

func takeBackupAs(t *testing.T, r http.Handler, password, path string) []byte {
	t.Helper()
	w := populatedServeJSON(t, r, password, http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	return w.Body.Bytes()
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

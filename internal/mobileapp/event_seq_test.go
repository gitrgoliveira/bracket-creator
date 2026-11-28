package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// eventSeqReads are the four reads a page fetches again after an event
// (bc-sseq FR3): the tournament, the competitions list, one competition and
// the court feed.
var eventSeqReads = []string{
	"/api/viewer/tournament",
	"/api/viewer/competitions",
	"/api/viewer/competitions/alpha",
	"/api/viewer/court/A/matches",
}

func eventSeqGet(t *testing.T, rig *backupTestRig, path, password, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	w := httptest.NewRecorder()
	rig.r.ServeHTTP(w, req)
	return w
}

// Every one of the four reads names the hub's head seq, for the organiser
// and the public alike, from 0 before the first event on.
func TestEventSeq_TheFourReadsNameTheHubsHeadSeq(t *testing.T) {
	rig := newBackupRig(t)
	for _, want := range []int64{0, 3} {
		for headSeq(rig.hub) < want {
			rig.hub.Broadcast(EventScheduleUpdated, map[string]string{"competitionId": "alpha"})
		}
		for _, path := range eventSeqReads {
			for _, pw := range []string{backupTestPassword, ""} {
				w := eventSeqGet(t, rig, path, pw, "")
				require.Equalf(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
				assert.Equalf(t, strconv.FormatInt(want, 10), w.Header().Get(eventSeqHeader),
					"%s (password %q) names the head seq", path, pw)
			}
		}
	}
}

// A browser page reads the header across origins only when CORS exposes it.
func TestEventSeq_IsExposedToTheBrowser(t *testing.T) {
	rig := newBackupRig(t)
	w := eventSeqGet(t, rig, "/api/viewer/competitions", "", "")
	require.Equal(t, http.StatusOK, w.Code)
	var exposed []string
	for _, tok := range strings.Split(w.Header().Get("Access-Control-Expose-Headers"), ",") {
		exposed = append(exposed, strings.TrimSpace(tok))
	}
	assert.Contains(t, exposed, eventSeqHeader)
	assert.Contains(t, exposed, BuildHeader)
}

// The shared builds send their gzipped bytes once, encoded once: through the
// app's router, the middleware leaves an answer the handler already encoded
// alone, and a caller that does not accept gzip gets the plain bytes with
// Accept-Encoding in Vary.
func TestEventSeq_SharedBuildsThroughTheRouterEncodeOnce(t *testing.T) {
	rig := newBackupRig(t)
	for i := range 30 {
		// Enough competitions that the list passes gzipMinBytes.
		require.NoError(t, rig.store.SaveCompetition(&state.Competition{
			ID: fmt.Sprintf("extra-%02d", i), Name: fmt.Sprintf("Extra competition %02d", i),
			Date: "01-06-2026", Courts: []string{"A"},
		}))
	}
	for range 3 {
		rig.hub.Broadcast(EventScheduleUpdated, map[string]string{"competitionId": "alpha"})
	}
	list := eventSeqGet(t, rig, "/api/viewer/competitions", backupTestPassword, "gzip")
	require.Equal(t, http.StatusOK, list.Code)
	require.Equal(t, "gzip", list.Header().Get("Content-Encoding"), "the list is large enough to be compressed")
	for _, path := range eventSeqReads[1:] {
		gz := eventSeqGet(t, rig, path, backupTestPassword, "gzip")
		plain := eventSeqGet(t, rig, path, backupTestPassword, "")
		require.Equalf(t, http.StatusOK, gz.Code, "%s: %s", path, gz.Body.String())
		require.Equalf(t, http.StatusOK, plain.Code, "%s: %s", path, plain.Body.String())
		assert.Equalf(t, "3", gz.Header().Get(eventSeqHeader), "%s gzip", path)
		assert.Equalf(t, "3", plain.Header().Get(eventSeqHeader), "%s plain", path)
		assert.Emptyf(t, plain.Header().Get("Content-Encoding"), "%s plain", path)
		assert.Containsf(t, varyTokens(plain.Header()), "Accept-Encoding", "%s plain", path)
		assert.Containsf(t, varyTokens(gz.Header()), "Accept-Encoding", "%s gzip", path)
		body := gz.Body.Bytes()
		if gz.Header().Get("Content-Encoding") == "gzip" {
			body = gunzip(t, body)
		}
		assert.Truef(t, json.Valid(body), "%s: one gunzip gives the JSON", path)
		assert.JSONEqf(t, plain.Body.String(), string(body), "%s: same answer either way", path)
	}
}

package mobileapp

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// In a self-run tournament the court control routes stay the organiser's
// (bc-sseq FR9): a participant's public page never claims, takes, reads or
// releases a shiaijo.
func TestSelfRun_CourtControlRoutesStayGated(t *testing.T) {
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		assert.True(t, isSelfRunMainGatedConfigRoute(method, "/api/courts/:court/control"), "%s /api/courts/:court/control must be organiser-only in self-run", method)
	}

	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	body := ccBody{"tabId": ccTab1, "deviceId": ccTablet1, "pageId": ccPage1}
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		w := courtControlReq(t, r, method, "A", "", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "an anonymous %s: %s", method, w.Body.String())
		w = courtControlReq(t, r, method, "A", "wrong-pw", body)
		assert.Equal(t, http.StatusUnauthorized, w.Code, "a wrong password's %s: %s", method, w.Body.String())
	}
	a := decodeCourtAnswer(t, courtControlReq(t, r, http.MethodPut, "A", "main-pw", body))
	assert.True(t, a.YouHold, "the organiser claims as in an officiated tournament")
}

// A self-run stream is sent every event as broadcast, but who runs each
// shiaijo is no name the self-run rule shows: a stream that is not the
// organiser's is sent court_control_updated with no data, so its seq still
// arrives and no gap is seen.
func TestSelfRun_CourtControlEventIsOrganiserOnly(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	h := hubWithNames(f)
	tour, err := f.store.LoadTournament()
	require.NoError(t, err)
	tour.Mode = state.TournamentModeSelfRun
	require.NoError(t, f.store.SaveTournament(tour))

	h.Broadcast(EventCourtControlUpdated, courtControlEvent{Court: "A", HolderKey: "c0ffee00c0ffee00", Device: "ipad", Since: 1})
	e := lastEntry(t, h)
	public := h.project(e, &sseSubscriber{})
	assert.NotContains(t, public, "c0ffee00c0ffee00")
	assert.Contains(t, public, string(EventCourtControlUpdated))
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{aud: audience{organiser: true}}), "the organiser's stream is sent it whole")
}

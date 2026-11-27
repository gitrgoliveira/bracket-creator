package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A draft draw kept off the public pages (bc-drpb) on the court reads: the
// court feed and the court's current match leave a hidden draft's matches out
// for the public, as a setup competition has none, and keep them for the
// organiser's court console.

const dhFeed = "/api/viewer/court/A/matches"

// courtFeedEntry is the court feed's payload of id, nil when it is left out.
func (w drawHiddenWorld) courtFeedEntry(t *testing.T, id string, headers map[string]string) map[string]any {
	t.Helper()
	code, body := w.get(t, dhFeed, headers)
	require.Equal(t, http.StatusOK, code, string(body))
	var feed struct {
		Competitions []map[string]any `json:"competitions"`
	}
	require.NoError(t, json.Unmarshal(body, &feed))
	return payloadOf(feed.Competitions, id)
}

func TestCourtFeed_LeavesAHiddenDraftOutForThePublic(t *testing.T) {
	for _, format := range drawHiddenFormats {
		t.Run(format, func(t *testing.T) {
			w := newDrawHiddenWorld(t, format)
			require.NoError(t, w.eng.GenerateDraw(dhComp))

			assert.Nil(t, w.courtFeedEntry(t, dhComp, dhPublic), "the public court feed leaves the hidden draft out")
			organiser := w.courtFeedEntry(t, dhComp, dhOrganiser)
			require.NotNil(t, organiser, "the court console reads the hidden draft")
			assert.True(t, hasDraw(organiser))
			assert.Equal(t, true, configOf(organiser)["drawHidden"])

			code, body := w.get(t, dhFeed, dhWrong)
			assert.Equal(t, http.StatusUnauthorized, code, string(body))
		})
	}
}

// A public court feed read straight after a publish or an unpublish answers
// the new state.
func TestCourtFeed_FollowsPublishAndUnpublishAtOnce(t *testing.T) {
	w := newDrawHiddenWorld(t, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(dhComp))
	require.Nil(t, w.courtFeedEntry(t, dhComp, dhPublic))

	w.setHidden(t, false)
	published := w.courtFeedEntry(t, dhComp, dhPublic)
	require.NotNil(t, published, "the published draft is on the court feed")
	assert.True(t, hasDraw(published))
	assert.NotContains(t, configOf(published), "drawHidden")

	w.setHidden(t, true)
	assert.Nil(t, w.courtFeedEntry(t, dhComp, dhPublic), "hidden again, the draft leaves the court feed")
}

// The queue positions of another competition on the same shiaijo count the
// hidden draft's matches for every audience: the court's real order
// (operator ruling bc-drpb-plan-1 Q1).
func TestCourtFeed_AHiddenDraftStillTakesItsQueuePlaces(t *testing.T) {
	w := newDrawHiddenWorld(t, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(dhComp))
	const other = "other-cup"
	addDrawHiddenCompetition(t, w.store, other, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(other))
	require.NoError(t, w.eng.StartCompetition(other))

	positions := func(headers map[string]string) []any {
		entry := w.courtFeedEntry(t, other, headers)
		require.NotNil(t, entry)
		var out []any
		for _, m := range entry["poolMatches"].([]any) {
			out = append(out, m.(map[string]any)["queuePosition"])
		}
		return out
	}
	organiser := positions(dhOrganiser)
	require.NotEmpty(t, organiser)
	highest := 0.0
	for _, p := range organiser {
		highest = max(highest, p.(float64))
	}
	require.Greater(t, highest, float64(len(organiser)), "premise: the hidden draft's matches share the court's queue")
	assert.Equal(t, organiser, positions(dhPublic), "the public reads the court's real order")

	list := w.listEntry(t, other, dhPublic)
	var listed []any
	for _, m := range list["poolMatches"].([]any) {
		listed = append(listed, m.(map[string]any)["queuePosition"])
	}
	assert.Equal(t, organiser, listed, "the list numbers the court as the feed does")
}

// The court's current match: a running match of a hidden draft is not the
// public's current match, as a setup competition runs none; the organiser,
// and the public once the draw is published, read it.
func TestCourtCurrent_LeavesAHiddenDraftOutForThePublic(t *testing.T) {
	w := newDrawHiddenWorld(t, state.CompFormatMixed)
	require.NoError(t, w.eng.GenerateDraw(dhComp))
	matches, err := w.store.LoadPoolMatches(dhComp)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	matches[0].Court = "A"
	matches[0].Status = state.MatchStatusRunning
	require.NoError(t, w.store.SavePoolMatches(dhComp, matches))

	current := func(headers map[string]string) map[string]any {
		return w.getJSON(t, "/api/viewer/court/A/current", headers)
	}
	assert.Equal(t, "idle", current(dhPublic)["status"], "the public reads no current match")
	assert.NotEqual(t, "idle", current(dhOrganiser)["status"], "the organiser reads the running match")
	code, body := w.get(t, "/api/viewer/court/A/current", dhWrong)
	assert.Equal(t, http.StatusUnauthorized, code, string(body))

	w.setHidden(t, false)
	assert.NotEqual(t, "idle", current(dhPublic)["status"], "published, the public reads it at once")
}

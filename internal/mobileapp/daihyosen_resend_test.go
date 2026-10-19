package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A representative bout's add or removal sent again by the outbox after its
// answer was lost (same writeId) is answered as recorded from the match's
// score log, as a reopen's or a send back's is, never refused as if the bout
// were already there (or already gone) because of some other write (stage 6
// review 12, m7).
func TestDaihyosen_AResendAfterALostAnswerIsAnsweredAsRecorded(t *testing.T) {
	f := newRepBoutFixture(t, false)

	add := map[string]any{"modifiedAt": f.now, "writeId": "w-add-1"}
	w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", add)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var first struct {
		Result map[string]any `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &first))

	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", add)
	require.Equal(t, http.StatusOK, w.Code, "the same add sent again is answered as recorded: %s", w.Body.String())
	var body struct {
		SubResult *state.SubMatchResult `json:"subResult"`
		Result    map[string]any        `json:"result"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.NotNil(t, body.SubResult, "the answer carries the bout as stored")
	assert.Equal(t, -1, body.SubResult.Position)
	require.NotNil(t, body.Result)
	for _, k := range []string{"status", "court", "sideA", "sideB", "winner"} {
		assert.Equal(t, first.Result[k], body.Result[k], "the resend's answer reads as the first one did: %s", k)
	}
	rows := 0
	for _, s := range storedB1(t, f.store, "c1").SubResults {
		if s.Position == -1 {
			rows++
		}
	}
	assert.Equal(t, 1, rows, "nothing is added twice")

	del := map[string]any{"modifiedAt": f.now + 10, "writeId": "w-del-1"}
	w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "main-pw", del)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "main-pw", del)
	require.Equal(t, http.StatusOK, w.Code, "the same removal sent again is answered as recorded: %s", w.Body.String())

	// A different add is still judged: the bout is gone, so it is added.
	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now + 20, "writeId": "w-add-2"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now + 30, "writeId": "w-add-3"})
	assert.Equal(t, http.StatusConflict, w.Code, "a second add that is no resend is refused as before")
}

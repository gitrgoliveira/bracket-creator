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

// A participant's add or removal that landed, whose answer was lost, and that
// is sent again once the match has finished or left running, is answered as
// recorded: the log already holds the write, so telling the participant 409
// result_finalized or match_not_running would name a refusal for a change that
// is in the match.
func TestDaihyosen_AnAnonymousResendOfALandedWriteIsAnsweredAsRecordedWhateverTheStatusIsNow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status state.MatchStatus
	}{
		{"the match finished meanwhile", state.MatchStatusCompleted},
		{"the match left running meanwhile", state.MatchStatusScheduled},
	} {
		t.Run("add, "+tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			add := map[string]any{"modifiedAt": f.now + 100, "writeId": "w-anon-add"}
			w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", add)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			f.setB1(t, func(bm *state.BracketMatch) { bm.Status = tc.status })

			w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", add)
			assert.Equal(t, http.StatusOK, w.Code, "the landed add sent again is answered as recorded: %s", w.Body.String())
		})
		t.Run("remove, "+tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, true)
			w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "", map[string]any{"modifiedAt": f.now + 100, "writeId": "w-anon-add"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			del := map[string]any{"modifiedAt": f.now + 200, "writeId": "w-anon-del"}
			w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", del)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			f.setB1(t, func(bm *state.BracketMatch) { bm.Status = tc.status })

			w = f.send(http.MethodDelete, repBoutMatchPath+"/daihyosen", "", del)
			assert.Equal(t, http.StatusOK, w.Code, "the landed removal sent again is answered as recorded: %s", w.Body.String())
		})
	}
}

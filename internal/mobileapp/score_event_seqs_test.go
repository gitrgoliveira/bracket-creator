package mobileapp

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// G1 (bc-evts stage 3): the match a device reads, the score-events answer's,
// the push's and the stored one, says per event session the highest seq the
// score log holds, so a board folding its pending taps on a push that arrived
// before their answer counts each tap once, a foul that lands no H and an
// overtime period included.
func TestScoreEventsDoor_MatchCarriesEachSessionsHighestLoggedSeq(t *testing.T) {
	const compID = "se-seqs"
	r, store, hub, _ := seServer(t, compID)
	now := time.Now().UnixMilli()

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)
	out := sePost(t, r, compID, seMatch,
		seTap("e1", 1, now-9000, "a", "M"),
		map[string]any{"id": "e2", "seq": 2, "stamp": now - 8900, "kind": "foul.add", "side": "b"},
		map[string]any{"id": "e3", "seq": 3, "stamp": now - 8800, "kind": "encho.add"},
	)
	assert.Equal(t, map[string]int64{"evs": 3}, out.Match.EventSeqs, "the answer's match")

	var payload string
	select {
	case msg := <-ch:
		payload = msg.payload
	case <-time.After(2 * time.Second):
		t.Fatal("no broadcast")
	}
	assert.Contains(t, payload, `"eventSeqs":{"evs":3}`, "the push's match")

	// Another device's session is kept beside this one.
	w := serveJSON(r, http.MethodPost, sePath(compID, seMatch), map[string]any{"session": "other",
		"events": []map[string]any{seTap("o1", 1, now-8700, "b", "K")}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// A write that is not a tap keeps them (the carry), and the stored match
	// holds them as the answer did.
	mergeScoreAt(t, r, compID, map[string]any{"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{"K"},
		"hansokuB": 1, "encho": map[string]any{"periodCount": 1}, "changed": []string{"rep"}, "modifiedAt": now - 8600})
	stored := seStoredMatch(t, store, compID)
	assert.Equal(t, map[string]int64{"evs": 3, "other": 1}, stored.EventSeqs)

	// A duplicate of a logged tap and a tap the log holds as not applied
	// (stamped after the finish) are held by the server too.
	mergeScoreAt(t, r, compID, map[string]any{"status": "completed", "winner": mgAlice, "ipponsA": []string{"M"},
		"ipponsB": []string{"K"}, "hansokuB": 1, "encho": map[string]any{"periodCount": 1}, "modifiedAt": now - 5000,
		"basis": map[string]any{"seen": 5}})
	closed := sePost(t, r, compID, seMatch, seTap("e1", 1, now-9000, "a", "M"), seTap("e4", 4, now-4000, "a", "K"))
	assert.Equal(t, domain.ScoreOutcomeDuplicate, closed.Outcomes[0].Outcome)
	assert.Equal(t, domain.ScoreOutcomeMatchClosed, closed.Outcomes[1].Outcome)
	assert.Equal(t, map[string]int64{"evs": 4, "other": 1}, closed.Match.EventSeqs)
	stored = seStoredMatch(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, stored.Status)
	assert.Equal(t, map[string]int64{"evs": 4, "other": 1}, stored.EventSeqs)
}

func seStoredMatch(t *testing.T, store *state.Store, compID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == seMatch {
			return m
		}
	}
	t.Fatalf("match %s not stored", seMatch)
	return state.MatchResult{}
}

// The same on a knockout match: the bracket stores the seqs, and a write that
// is not a tap (the representative-bout add) keeps them.
func TestScoreEventsDoor_KnockoutMatchCarriesTheSeqs(t *testing.T) {
	f := newRepBoutFixture(t, false)
	w := f.send(http.MethodPost, repBoutMatchPath+"/score-events", "main-pw", map[string]any{"session": "evs",
		"events": []map[string]any{{"id": "r1", "seq": 1, "stamp": f.now - 50_000, "kind": "foul.add", "pos": 3, "side": "a"}}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"eventSeqs":{"evs":1}`)

	w = f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now - 40_000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"eventSeqs":{"evs":1}`, "the add's answer")
	b, err := f.store.LoadBracket("c1")
	require.NoError(t, err)
	bm := b.MatchByID("B1")
	require.NotNil(t, bm)
	assert.Equal(t, map[string]int64{"evs": 1}, bm.EventSeqs)
}

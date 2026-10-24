package mobileapp

// bc-mrgc review: the write doors' side of the review round's findings, each
// failing without its fix.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func mergeReviewPut(t *testing.T, r http.Handler, compID string, body map[string]any) (int, map[string]any) {
	t.Helper()
	w := serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/Pool A-0/score", body)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

// F2: a stale payload naming a DIFFERENT competitor, with no side ids, is
// refused as a side mismatch. The hantei backfill used to copy the stored
// side id beside the payload's other name, and the engine took that id as
// proof of a rename and accepted the write.
func TestScoreHandler_ABackfilledIDIsNoProofOfARename(t *testing.T) {
	const compID = "merge-review-f2"
	r, store := mergeServer(t, compID)
	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": "Carol", "sideB": mgBob, "status": "completed", "winner": "Carol",
		"ipponsA": []string{domain.HanteiMark}, "ipponsB": []string{},
		"modifiedAt": time.Now().UnixMilli(),
	})
	assert.Equal(t, http.StatusConflict, code, out)
	assert.Equal(t, "side_mismatch", out["error"])
	m := mergeStored(t, store, compID)
	assert.Equal(t, mgAlice, m.SideA)
	assert.Equal(t, state.MatchStatusRunning, m.Status, "nothing was written")
}

// LOW: a Finish made before the match was finished on another device (a
// queued Finish replayed late) is no correction to justify: it reaches the
// merge, is held by its stamp and kept in the match's history. It used to be
// refused for a missing correction reason, and lost.
func TestScoreHandler_StaleFinishIsHeldNotRefusedForAReason(t *testing.T) {
	const compID = "merge-review-low"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgAlice,
		"ipponsA": []string{"M"}, "ipponsB": []string{}, "modifiedAt": now - 10_000,
	})
	require.Equal(t, http.StatusOK, code, out)

	code, out = mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgBob,
		"ipponsA": []string{}, "ipponsB": []string{"K"}, "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, code, out)
	// The FIRST recorded result stands, in stamp order (operator decision
	// 2026-10-16): the late finish was made before the stored one, so it is the
	// first, and the stored one, kept in the history, no longer stands.
	assert.NotEqual(t, false, out["applied"], "no 400, and no loss: it is applied")

	m := mergeStored(t, store, compID)
	assert.Equal(t, mgBob, m.Winner, "the first finish stands")
	entries, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	var displaced bool
	for _, e := range entries {
		if e.Door == "displaced" && e.Outcomes[state.GroupResult] == state.HistoryOutcomeHeld {
			displaced = true
			assert.Contains(t, string(e.Held[state.GroupResult]), `"winner":"`+mgAlice+`"`, "the later finish is kept")
		}
	}
	assert.True(t, displaced, "the later finish is kept in the history")

	t.Run("an equal stamp is a correction, which still needs its reason", func(t *testing.T) {
		code, out := mergeReviewPut(t, r, compID, map[string]any{
			"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgAlice,
			"ipponsA": []string{"M"}, "ipponsB": []string{}, "modifiedAt": now - 20_000,
		})
		assert.Equal(t, http.StatusBadRequest, code, out)
	})
}

// The closed-match rule on the wire: a recount stamped after an engi finish is
// not applied, and the answer says why (heldReason "match_closed", with its
// message), so the operator corrects the result with Correct.
func TestScoreHandler_HeldForAWinnerSaysSo(t *testing.T) {
	const compID = "merge-review-r4"
	r, store := mergeServer(t, compID)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Engi = true
	require.NoError(t, store.SaveCompetition(comp))
	now := time.Now().UnixMilli()

	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "flagsA": 3, "flagsB": 0,
		"changed": []string{"result", "flags"}, "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, code, out)

	code, out = mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "running", "flagsA": 1, "flagsB": 1,
		"changed": []string{"flags"}, "modifiedAt": now - 10_000,
	})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["applied"])
	assert.Equal(t, "superseded", out["reason"], "the not-retried, not-lost shape the queue keys on")
	assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
	assert.Equal(t, MatchClosedMessage, out["message"])
	assert.Equal(t, []any{"flags"}, out["heldGroups"])

	m := mergeStored(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status, "the finish stands")
	assert.Equal(t, mgAlice, m.Winner)
	assert.Equal(t, 3, m.FlagsA)
}

// The closed-match rule's fusensho arm (the twin of the test above): a running
// board's scoring over a match a match-level fusensho closed (the OTHER side is
// barred by a DIFFERENT match, recorded from the queue row) is not applied
// either, and the answer says why with the one code and message
// (heldReason "match_closed", MatchClosedMessage).
func TestScoreHandler_HeldForADefaultWinSaysSo(t *testing.T) {
	const compID = "merge-review-dws"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()

	// mgBob (SideB, shiro in a pool match) cannot fight (e.g. barred by a
	// withdrawal on another match): the default win is mgAlice's (SideA, aka).
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision", map[string]any{
		"decision": "fusensho", "decisionBy": "shiro", "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "running",
		"ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})
	require.Equal(t, http.StatusOK, code, out)
	assert.Equal(t, false, out["applied"])
	assert.Equal(t, "superseded", out["reason"], "the not-retried, not-lost shape the queue keys on")
	assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
	assert.NotContains(t, out, "heldDecision", "one code, one remedy: Correct")
	assert.Equal(t, MatchClosedMessage, out["message"])
	assert.Equal(t, []any{"points"}, out["heldGroups"])

	m := mergeStored(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status, "the default win stands")
	assert.Equal(t, "fusensho", m.Decision)
	assert.Equal(t, mgAlice, m.Winner)
}

// bc-evts stage 2b, operator decision 2026-10-16 (I1): the FIRST recorded
// result stands. A Finish stamped after a recorded kiken from a device that
// never saw it (no correction reason, no clearWithdrawal) is refused as
// match_closed in the 200 superseded shape the queue keys on, never the 400
// "missing correction reason" a queued replay would loop on; the withdrawal
// stands and its competitor stays barred. The decision arriving AFTER the
// finish stamped later gives the same match, in either order.
func TestScoreHandler_ALaterFinishThatNeverSawTheKikenIsMatchClosed(t *testing.T) {
	finish := func(now int64) map[string]any {
		return map[string]any{
			"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgBob,
			"ipponsA": []string{}, "ipponsB": []string{"M"},
			"changed": []string{"points", "result"}, "modifiedAt": now - 10_000,
		}
	}
	decision := func(now int64) map[string]any {
		return map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 20_000}
	}
	for _, order := range []string{"decision first", "finish first"} {
		t.Run(order, func(t *testing.T) {
			compID := "merge-r5-i1-" + map[string]string{"decision first": "a", "finish first": "b"}[order]
			r, store := mergeServer(t, compID)
			now := time.Now().UnixMilli()
			sendDecision := func() int {
				w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision", decision(now))
				return w.Code
			}
			var out map[string]any
			var code int
			if order == "decision first" {
				require.Equal(t, http.StatusOK, sendDecision())
				code, out = mergeReviewPut(t, r, compID, finish(now))
				require.Equal(t, http.StatusOK, code, "a queued replay must not be refused 400: %v", out)
				assert.Equal(t, false, out["applied"])
				assert.Equal(t, "superseded", out["reason"])
				assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
				assert.Equal(t, MatchClosedMessage, out["message"])
			} else {
				code, out = mergeReviewPut(t, r, compID, finish(now))
				require.Equal(t, http.StatusOK, code, out)
				require.Equal(t, http.StatusOK, sendDecision())
			}
			m := mergeStored(t, store, compID)
			assert.Equal(t, state.MatchStatusCompleted, m.Status)
			assert.Equal(t, "kiken-voluntary", m.Decision, "the first recorded result stands")
			assert.Equal(t, mgAlice, m.Winner)
			statuses, err := store.LoadCompetitorStatus(compID)
			require.NoError(t, err)
			barred := 0
			for _, s := range statuses {
				if !s.Eligible {
					barred++
				}
			}
			assert.Equal(t, 1, barred, "the withdrawer stays barred")
		})
	}
}

// The other arrival order of the same two writes: the recount arrives first,
// then the finish made before it. In stamp order the recount could not apply
// after the finish, so the finish is applied on its own count and the
// recount is moved to the match's history; the answer is applied and names
// what was moved (displacedGroups, with heldReason).
func TestScoreHandler_FinishMovesANewerTyingChangeToTheHistory(t *testing.T) {
	const compID = "merge-review-displaced"
	r, store := mergeServer(t, compID)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Engi = true
	require.NoError(t, store.SaveCompetition(comp))
	now := time.Now().UnixMilli()

	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "running", "flagsA": 1, "flagsB": 1,
		"changed": []string{"flags"}, "modifiedAt": now - 10_000,
	})
	require.Equal(t, http.StatusOK, code, out)

	code, out = mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "flagsA": 3, "flagsB": 0,
		"changed": []string{"result", "flags"}, "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, code, out)
	assert.NotEqual(t, false, out["applied"], "the finish is applied")
	assert.Equal(t, []any{"flags"}, out["displacedGroups"])
	assert.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
	assert.Nil(t, out["heldGroups"], "nothing of the finish was held")

	m := mergeStored(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, mgAlice, m.Winner)
	assert.Equal(t, 3, m.FlagsA)
	entries, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	found := false
	for _, e := range entries {
		if e.Stamp == now-10_000 && e.Outcomes[state.GroupFlags] == state.HistoryOutcomeHeld {
			found = true
			assert.Contains(t, string(e.Held[state.GroupFlags]), `"flagsA":1`)
		}
	}
	assert.True(t, found, "the recount is kept in the history")
}

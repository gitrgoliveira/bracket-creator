package mobileapp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// The write that started a kachinuki match is known by its logged line (its
// writeId), never by the stamp the match's result group holds now (stage 6
// review 12, m6): another device's write that moves the result group between
// the start and its resend must not take the warning away from the resend, and
// another write landing at the start's stamp must not be handed it.
func TestKachinukiResentStart_IsKnownByItsLoggedLine(t *testing.T) {
	start := func(e *carryEnv, writeID string) map[string]any {
		return map[string]any{
			"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
			"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
			"changed": []string{"result"}, "modifiedAt": e.base + 30, "writeId": writeID,
		}
	}
	finish := func(e *carryEnv, stamp int64, writeID string) map[string]any {
		return map[string]any{
			"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
			"status": "completed", "winner": "Taka", "winnerId": e.id["Taka"], "decision": "kachinuki-exhaustion",
			"subResults": []map[string]any{
				{"position": 1, "sideA": "Doi", "sideB": "K-1", "ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "winner": "Doi", "decision": "fought"},
			},
			"modifiedAt": stamp, "writeId": writeID,
		}
	}
	send := func(t *testing.T, e *carryEnv, body map[string]any) string {
		t.Helper()
		path := "/api/competitions/" + e.comp + "/matches/" + url.PathEscape("P1-0") + "/score"
		rec := e.do(http.MethodPut, path, body)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return warningOf(t, rec.Body)
	}

	t.Run("another device's write moves the result group before the resend", func(t *testing.T) {
		e := newCarryEnv(t, true)
		refusedCarryAfterP10(t, e)
		first := send(t, e, start(e, "start-P1-0"))
		require.Equal(t, "The match was started, "+refusedP11Warning, first)
		require.Empty(t, send(t, e, finish(e, e.base+60, "finish-elsewhere")), "the other device's finish is no start")
		require.Equal(t, first, send(t, e, start(e, "start-P1-0")),
			"the resent start is told what its first answer said, whatever moved the result group since")
	})
	t.Run("another write at the start's stamp is not handed the warning", func(t *testing.T) {
		e := newCarryEnv(t, true)
		refusedCarryAfterP10(t, e)
		require.Equal(t, "The match was started, "+refusedP11Warning, send(t, e, start(e, "start-P1-0")))
		require.Empty(t, send(t, e, finish(e, e.base+30, "finish-same-ms")),
			"a write that is not the start, stamped in the same millisecond, is told nothing")
	})
}

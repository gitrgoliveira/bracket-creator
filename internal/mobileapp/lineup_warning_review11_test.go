package mobileapp

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

// Stage 6 review 11, item 6: the write that first moved a kachinuki match out
// of scheduled, sent again because its answer was lost (the same writeId, or
// the same stamp), is answered with the warning its first answer carried, so
// the device that never saw that answer is still told. Nothing more is
// written for it.
func TestKachinukiResentFirstWrite_IsToldTheSameWarning(t *testing.T) {
	start := func(e *carryEnv, writeID string) map[string]any {
		return map[string]any{
			"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
			"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
			"changed": []string{"result"}, "modifiedAt": e.base + 30, "writeId": writeID,
		}
	}
	finish := func(e *carryEnv, writeID string) map[string]any {
		return map[string]any{
			"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
			"status": "completed", "winner": "Taka", "winnerId": e.id["Taka"], "decision": "kachinuki-exhaustion",
			"subResults": []map[string]any{
				{"position": 1, "sideA": "Doi", "sideB": "K-1", "ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "winner": "Doi", "decision": "fought"},
			},
			"modifiedAt": e.base + 30, "writeId": writeID,
		}
	}
	decide := func(e *carryEnv, writeID string) map[string]any {
		return map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": e.base + 30, "writeId": writeID}
	}
	for _, tc := range []struct {
		name          string
		door          string
		body          func(*carryEnv, string) map[string]any
		first, second string
		lead          string
	}{
		{"a start resent with its writeId", "score", start, "start-P1-0", "start-P1-0", "The match was started, "},
		{"a start resent with its stamp", "score", start, "start-P1-0", "start-P1-0-again", "The match was started, "},
		{"a finish from scheduled resent", "score", finish, "finish-P1-0", "finish-P1-0", "The match was finished, "},
		{"a decision from scheduled resent", "decision", decide, "decide-P1-0", "decide-P1-0", "The decision was recorded, "},
		{"a decision from scheduled resent with its stamp", "decision", decide, "decide-P1-0", "decide-P1-0-again", "The decision was recorded, "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newCarryEnv(t, true)
			if tc.door == "decision" {
				hub := NewHub()
				t.Cleanup(hub.Close)
				admin := e.r.Group("/api")
				admin.Use(AuthMiddleware(NewFileVerifier(e.store), e.store))
				RegisterDecisionHandlers(admin, e.eng, e.store, e.store, hub)
			}
			refusedCarryAfterP10(t, e)
			path := "/api/competitions/" + e.comp + "/matches/" + url.PathEscape("P1-0") + "/" + tc.door
			method := http.MethodPut
			if tc.door == "decision" {
				method = http.MethodPost
			}
			send := func(writeID string) string {
				rec := e.do(method, path, tc.body(e, writeID))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				return warningOf(t, rec.Body)
			}
			first := send(tc.first)
			require.Equal(t, tc.lead+refusedP11Warning, first)
			lineupsBefore, err := e.store.LoadTeamLineupsWithRemoved(e.comp)
			require.NoError(t, err)
			second := send(tc.second)
			require.Equal(t, first, second, "the resend is told what the first answer said")
			lineupsAfter, err := e.store.LoadTeamLineupsWithRemoved(e.comp)
			require.NoError(t, err)
			require.Equal(t, lineupsBefore, lineupsAfter, "the resend writes no lineup")
		})
	}
}

// Only that write is told again: a later write of the started match is not.
func TestKachinukiLaterWrite_IsNotToldTheStartsWarning(t *testing.T) {
	e := newCarryEnv(t, true)
	refusedCarryAfterP10(t, e)
	path := "/api/competitions/" + e.comp + "/matches/" + url.PathEscape("P1-0") + "/score"
	write := func(stamp int64, writeID string) string {
		rec := e.do(http.MethodPut, path, map[string]any{
			"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
			"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
			"changed": []string{"result"}, "modifiedAt": stamp, "writeId": writeID,
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		return warningOf(t, rec.Body)
	}
	require.Equal(t, "The match was started, "+refusedP11Warning, write(e.base+30, "start-P1-0"))
	require.Empty(t, write(e.base+60, "again-P1-0"), "a later write is no resend of the start")
}

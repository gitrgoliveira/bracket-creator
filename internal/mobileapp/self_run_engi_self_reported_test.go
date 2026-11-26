package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestSelfRunEngiFinishIsRecordedAsSelfReported pins the walk's finding S4:
// an engi result saved from the public flag editor is recorded as
// self-reported (FR1), on a pool bout and on a knockout bout alike, so the
// organiser can tell it came from a competitor.
func TestSelfRunEngiFinishIsRecordedAsSelfReported(t *testing.T) {
	alice := domain.Player{ID: "a1b2c3d4-0000-4000-8000-00000000000a", Name: "Alice", Dojo: "DojoA"}
	bob := domain.Player{ID: "a1b2c3d4-0000-4000-8000-00000000000b", Name: "Bob", Dojo: "DojoB"}

	setup := func(t *testing.T, knockout bool) (*engiRouter, *state.Store, string) {
		t.Helper()
		store := newTempStore(t)
		seedSelfRunTournament(t, store)
		comp := &state.Competition{ID: "engi", Name: "Kata", Engi: true, Courts: []string{"A"}}
		if knockout {
			comp.Format = state.CompFormatKnockout
			comp.Status = state.CompStatusKnockout
		} else {
			comp.Format = state.CompFormatMixed
			comp.Status = state.CompStatusPools
		}
		require.NoError(t, store.SaveCompetition(comp))
		require.NoError(t, store.SaveParticipants("engi", []domain.Player{alice, bob}))
		stamp := time.Now().UnixMilli() - 60_000
		matchID := "P1-0"
		if knockout {
			matchID = "B1"
			require.NoError(t, store.SaveBracket("engi", &state.Bracket{Rounds: [][]state.BracketMatch{{{
				ID: matchID, SideA: alice.Name, SideB: bob.Name, SideAID: alice.ID, SideBID: bob.ID,
				Status: state.MatchStatusRunning, Court: "A", ModifiedAt: stamp,
			}}}}))
		} else {
			require.NoError(t, store.SavePoolMatches("engi", []state.MatchResult{{
				ID: matchID, SideA: alice.Name, SideB: bob.Name, SideAID: alice.ID, SideBID: bob.ID,
				Status: state.MatchStatusRunning, Court: "A", ModifiedAt: stamp,
			}}))
		}
		return &engiRouter{setupSelfRunRouter(t, store, NewFileVerifier(store))}, store, matchID
	}

	stored := func(t *testing.T, store *state.Store, knockout bool) (state.MatchStatus, string) {
		t.Helper()
		if knockout {
			b, err := store.LoadBracket("engi")
			require.NoError(t, err)
			bm := b.Rounds[0][0]
			return bm.Status, bm.ResultSource
		}
		matches, err := store.LoadPoolMatches("engi")
		require.NoError(t, err)
		require.Len(t, matches, 1)
		return matches[0].Status, matches[0].ResultSource
	}

	for _, knockout := range []bool{false, true} {
		name := "pool"
		if knockout {
			name = "knockout"
		}
		t.Run(name, func(t *testing.T) {
			r, store, matchID := setup(t, knockout)
			path := "/api/competitions/engi/matches/" + matchID + "/score"
			at := time.Now().UnixMilli() - 1_000
			w := r.put(path, "", map[string]any{
				"sideA": alice.Name, "sideB": bob.Name, "sideAId": alice.ID, "sideBId": bob.ID,
				"status": "completed", "flagsA": 2, "flagsB": 1,
				"changed": []string{"result", "flags"}, "modifiedAt": at,
			})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			status, source := stored(t, store, knockout)
			assert.Equal(t, state.MatchStatusCompleted, status)
			assert.Equal(t, "self-reported", source, "FR1: an engi result from the public page is self-reported")
		})
	}
}

// engiRouter wraps the router for one-line requests in this file.
type engiRouter struct{ h http.Handler }

func (g *engiRouter) put(path, password string, body any) *httptest.ResponseRecorder {
	req := jsonReq(http.MethodPut, path, body)
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	g.h.ServeHTTP(w, req)
	return w
}

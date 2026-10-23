package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Review of PR #483, the kachinuki next-position rule (af7eab65): a tap makes a
// kachinuki row only at the position after the last bout the match holds. Record
// bout is a server write that appends the next pairing at the SERVER's time, so
// this pins that a tap on the newest pairing, made after two quick Record bouts
// and stamped the way a device stamps it (floored by the stamp the Record bout
// answer gave the match, bc-hlck), is never turned away for being past the
// next position, whichever order the two Record bouts and the tap reach the
// replay in stamp order.
func TestKachinukiTapOnTheNewestPairingAfterTwoRecordBoutsLands(t *testing.T) {
	for _, tc := range []struct {
		name string
		// lateTap: after the tap on bout 3 lands, a tap another device made
		// on bout 1 BEFORE either Record bout arrives, so the server orders the
		// whole log again with a line stamped before everything else.
		lateTap bool
		// early: the tap is stamped between the second Record bout's own stamp
		// and the advance's (no device stamps one so, since every tap is stamped
		// after the match it was made on, but the replay must not depend on it).
		early bool
	}{
		{"the tap on the newest pairing lands", false, false},
		{"and stays when an older tap arrives after it", true, false},
		{"stamped before the advance that appended its pairing", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compID := "kachinuki-tap-after-record"
			r, store := setupKachinukiScoreServer(t, compID)
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
			}}))
			now := time.Now().UnixMilli()
			record := func(stamp int64, subs ...map[string]any) state.MatchResult {
				w := putScore(t, r, compID, "P1-0", map[string]any{
					"sideA": "Ryu", "sideB": "Tora", "status": "running", "kachinukiBoutFinal": true,
					"modifiedAt": stamp, "subResults": subs,
				})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var echoed state.MatchResult
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &echoed))
				return echoed
			}
			// Two quick Record bouts: Ryu's first fighter wins bouts 1 and 2.
			first := record(now-2000, kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"))
			require.Len(t, first.SubResults, 2, "Record bout appends bout 2")
			// The device's next write is stamped after the answer it was made
			// on (bc-hlck: the Record bout answer carries the advance's stamp).
			second := record(first.ModifiedAt+1,
				kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
				kachinukiSub(2, "R-1", "W-2", []string{"D", "M"}, "R-1", "fought"))
			require.Len(t, second.SubResults, 3, "the second Record bout appends bout 3")

			tap := func(session string, stamp int64, pos int) string {
				ev := domain.ScoreEvent{ID: session + "-tap", Session: session, Seq: 1, Stamp: stamp,
					Kind: domain.ScoreIpponAdd, Pos: pos, Side: "a", Letter: "M"}
				body, err := json.Marshal(map[string]any{"session": session, "events": []domain.ScoreEvent{ev}})
				require.NoError(t, err)
				w := httptest.NewRecorder()
				req, err := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/P1-0/score-events", bytes.NewBuffer(body))
				require.NoError(t, err)
				req.Header.Set("Content-Type", "application/json")
				r.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var out struct {
					Outcomes []scoreEventOutcome `json:"outcomes"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
				require.Len(t, out.Outcomes, 1)
				return out.Outcomes[0].Outcome
			}
			// The tap on bout 3, stamped after the answer it was made on.
			if tc.early {
				at := first.ModifiedAt + 2
				require.Less(t, at, second.ModifiedAt, "the advance is stamped after the Record bout it answers")
				// The replay holds the pairing's row at every stamp, so the
				// next-position rule never decides such a tap: it is answered
				// as a change made before the row was written (the answer it
				// had before that rule), never noTarget.
				require.Equal(t, domain.ScoreOutcomeBeforeBaseline, tap("dev1", at, 3))
				return
			}
			require.Equal(t, domain.ScoreOutcomeApplied, tap("dev1", second.ModifiedAt+1, 3), "the tap on the newest pairing lands")
			if tc.lateTap {
				tap("dev2", now-3000, 1)
			}

			matches, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, matches[0].SubResults, 3, "no row is made past the newest pairing")
			require.Equal(t, []string{"M"}, matches[0].SubResults[2].IpponsA, "the point is on bout 3")
			require.Equal(t, "W-3", matches[0].SubResults[2].SideB, "on the pairing Record bout appended")
		})
	}
}

package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A fixed-order team match started from the queue holds no bout rows (a
// start-only write). A tap on one of its numbered bouts makes the row, within
// the team size, so the device needs no score write ahead of its first tap;
// a tap past the team size still has no target.
func TestScoreEventsDoor_TapMakesAFixedOrderBoutRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		// oldLog: the log was begun by a build whose migration baseline did
		// not cover the bouts with no row.
		oldLog bool
	}{{name: "new log"}, {name: "log begun before the fix", oldLog: true}} {
		t.Run(tc.name, func(t *testing.T) {
			f := newRepBoutFixture(t, false)
			b, err := f.store.LoadBracket("c1")
			require.NoError(t, err)
			bm := b.MatchByID("B1")
			require.NotNil(t, bm)
			bm.SubResults = nil
			require.NoError(t, f.store.SaveBracket("c1", b))
			if tc.oldLog {
				cur := state.MatchResult{ID: "B1", SideA: bm.SideA, SideB: bm.SideB, SideAID: bm.SideAID, SideBID: bm.SideBID,
					Status: bm.Status, ModifiedAt: bm.ModifiedAt}
				mb := engine.MigrationBaseline(&cur, engine.MigrationDoor+"-0", f.now-59_000)
				require.NoError(t, f.store.AppendScoreLog("c1", "B1", state.ScoreLogLine{Baseline: &mb}))
				bm.ScoreVersion = 1
				require.NoError(t, f.store.SaveBracket("c1", b))
			}
			post := func(events ...map[string]any) seAnswer {
				t.Helper()
				w := f.send(http.MethodPost, repBoutMatchPath+"/score-events", "main-pw", map[string]any{"session": "evs", "events": events})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var out seAnswer
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
				return out
			}
			out := post(map[string]any{"id": "t1", "seq": 1, "stamp": f.now - 50_000, "kind": "ippon.add", "pos": 2, "side": "a", "letter": "M"})
			assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome, "the tap makes bout 2's row")
			assertLogReplaysToStored(t, f.store, "c1", "B1", true)

			out = post(
				map[string]any{"id": "t2", "seq": 2, "stamp": f.now - 40_000, "kind": "ippon.add", "pos": 2, "side": "a", "letter": "K"},
				map[string]any{"id": "t3", "seq": 3, "stamp": f.now - 30_000, "kind": "foul.add", "pos": 3, "side": "b"},
				map[string]any{"id": "t4", "seq": 4, "stamp": f.now - 20_000, "kind": "ippon.add", "pos": 4, "side": "a", "letter": "D"},
			)
			assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[0].Outcome)
			assert.Equal(t, domain.ScoreOutcomeApplied, out.Outcomes[1].Outcome, "the tap makes bout 3's row")
			assert.Equal(t, domain.ScoreOutcomeNoTarget, out.Outcomes[2].Outcome, "bout 4 is past the team size")
			assertLogReplaysToStored(t, f.store, "c1", "B1", true)

			b, err = f.store.LoadBracket("c1")
			require.NoError(t, err)
			rows := b.MatchByID("B1").SubResults
			require.Len(t, rows, 2)
			assert.Equal(t, 2, rows[0].Position)
			assert.Equal(t, []string{"M", "K"}, rows[0].IpponsA, "each mark counted once")
			assert.Equal(t, 3, rows[1].Position)
			assert.Equal(t, 1, rows[1].HansokuB)
		})
	}
}

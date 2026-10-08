package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A decision closes a match, and until mp-jnvl it was the one completion that
// could carry no time at all: a queue row's Record default win closes a match
// that is still `scheduled`, which was never started and had no stored stamp
// to inherit. Nothing downstream could then order that result against the bouts
// around it. RecordDecision now takes the client's write stamp.
func TestRecordDecision_StoresTheClientWriteStamp(t *testing.T) {
	setup := func(t *testing.T) (*Engine, *state.Store, string) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		compID := "stamp-test"
		createTestCompetition(t, store, compID, "league", 2)
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{ID: helper.NewUUID4(), Name: "Alice", Dojo: "A"},
			{ID: helper.NewUUID4(), Name: "Bob", Dojo: "B"},
		}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		}))
		return eng, store, compID
	}

	storedStamp := func(t *testing.T, store *state.Store, compID string) int64 {
		t.Helper()
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range matches {
			if m.ID == "Pool A-0" {
				return m.ModifiedAt
			}
		}
		t.Fatal("Pool A-0 not found")
		return 0
	}

	t.Run("a stamped decision persists the stamp", func(t *testing.T) {
		eng, store, compID := setup(t)
		const stamp int64 = 1_700_000_000_000
		_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "knee", nil, stamp)
		require.NoError(t, err)
		require.Equal(t, stamp, storedStamp(t, store, compID))
	})

	// The engine-internal pass-through and every existing caller omit it.
	// Such a decision used to stay unstamped, which left nothing a later stale
	// write could be ordered against (bc-mrgc review S5): it now takes the
	// server's time, the time the decision was made.
	t.Run("an unstamped decision takes the server's time", func(t *testing.T) {
		eng, store, compID := setup(t)
		before := time.Now().UnixMilli()
		_, _, err := eng.RecordDecision(compID, "Pool A-0", "kiken", "aka", "knee", nil)
		require.NoError(t, err)
		got := storedStamp(t, store, compID)
		require.GreaterOrEqual(t, got, before)
		require.LessOrEqual(t, got, time.Now().UnixMilli())
	})
}

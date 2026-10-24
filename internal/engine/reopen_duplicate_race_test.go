package engine

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// raceDuplicateReopens sends two copies of one reopen (the same writeId, the
// first's answer lost) at once: both are parked on the court lock the test
// holds, past everything either does before it, and released together. The
// first reopens; the second must find the first's logged line and be answered
// as recorded (stage 6 review 12, m5), never refused because the match is no
// longer completed.
func raceDuplicateReopens(t *testing.T, store *state.Store, call func() (*domain.CompetitorStatus, error)) []*domain.CompetitorStatus {
	t.Helper()
	type answer struct {
		st  *domain.CompetitorStatus
		err error
	}
	answers := make(chan answer, 2)
	var wg sync.WaitGroup
	require.NoError(t, store.WithCourtExclusivityLock(func() error {
		for range 2 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				st, err := call()
				answers <- answer{st, err}
			}()
		}
		// Both copies reach the court lock (their reads before it are
		// in-memory and take a few milliseconds at most).
		time.Sleep(300 * time.Millisecond)
		return nil
	}))
	wg.Wait()
	close(answers)
	var restored []*domain.CompetitorStatus
	for a := range answers {
		require.NoError(t, a.err, "each copy of the reopen is answered as recorded")
		if a.st != nil {
			restored = append(restored, a.st)
		}
	}
	return restored
}

func countLoggedWriteID(t *testing.T, store *state.Store, compID, matchID, writeID string) int {
	t.Helper()
	lines, err := store.LoadScoreLog(compID, matchID)
	require.NoError(t, err)
	n := 0
	for _, l := range lines {
		if l.Baseline != nil && l.Baseline.WriteID == writeID {
			n++
		}
	}
	return n
}

func TestReopenDuplicates_RacingCopiesAreAnsweredAsRecorded(t *testing.T) {
	t.Run("reopen", func(t *testing.T) {
		eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
		lines := historyLen(t, store, compID, "Pool A-0")
		tag := ForceOptions{WriteID: "w-reopen-race"}
		restored := raceDuplicateReopens(t, store, func() (*domain.CompetitorStatus, error) {
			return eng.ReopenMatch(compID, "Pool A-0", "mistake", tag)
		})
		assert.Len(t, restored, 1, "the reopen restores the competitor once")
		assert.Equal(t, lines+1, historyLen(t, store, compID, "Pool A-0"), "one history line for the reopen")
		assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "Pool A-0", "w-reopen-race"), "one logged line for it")
		m := wrPoolMatch(t, store, compID)
		assert.Equal(t, state.MatchStatusRunning, m.Status)
	})
	t.Run("requeue and reopen", func(t *testing.T) {
		const compID = "kr-requeue-race"
		eng, store := seedKachinukiPoolWithdrawal(t, compID, "kiken-voluntary")
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		ms = append(ms, state.MatchResult{
			ID: "Pool A-1", SideA: wrTeamB, SideAID: wrTeamBID, SideB: wrTeamC, SideBID: wrTeamCID,
			Court: "A", Status: state.MatchStatusRunning,
		})
		require.NoError(t, store.SavePoolMatches(compID, ms))
		lines := historyLen(t, store, compID, "Pool A-0")
		tag := ForceOptions{WriteID: "w-requeue-race"}
		restored := raceDuplicateReopens(t, store, func() (*domain.CompetitorStatus, error) {
			return eng.RequeueBlockerAndReopen(compID, "Pool A-0", compID, "Pool A-1", "", tag)
		})
		assert.Len(t, restored, 1, "the reopen restores the competitor once")
		assert.Equal(t, lines+1, historyLen(t, store, compID, "Pool A-0"), "one history line for the reopen")
		assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "Pool A-0", "w-requeue-race"), "one logged line for it")
		assert.Equal(t, state.MatchStatusRunning, wrPoolMatch(t, store, compID).Status)
	})
}

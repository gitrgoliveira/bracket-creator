package state

import (
	"fmt"
	"sync"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConcurrent_SaveLoadCompetition fires N goroutines that each save then
// load the same competition. The race detector validates no data races;
// content assertions confirm no lost writes (last writer wins is fine —
// what we must NOT see is a corrupted or zero-value struct).
func TestConcurrent_SaveLoadCompetition(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const N = 20
	compID := "concurrent-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "base"}))

	var wg sync.WaitGroup
	errs := make(chan error, N*2)

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("writer-%d", i)
			if err := store.SaveCompetition(&Competition{ID: compID, Name: name}); err != nil {
				errs <- err
				return
			}
			c, err := store.LoadCompetition(compID)
			if err != nil {
				errs <- err
				return
			}
			if c == nil || c.ID == "" {
				errs <- fmt.Errorf("goroutine %d: LoadCompetition returned empty struct", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
}

// TestConcurrent_UpdateBracket fires N goroutines each updating a different
// match inside the same bracket. Under -race, any non-serialised access
// is detected. After all goroutines finish, every match must have a winner.
func TestConcurrent_UpdateBracket(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const N = 10
	compID := "concurrent-bracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Seed N matches.
	matches := make([]BracketMatch, N)
	for i := range N {
		matches[i] = BracketMatch{
			ID:    fmt.Sprintf("M%d", i),
			SideA: fmt.Sprintf("A%d", i),
			SideB: fmt.Sprintf("B%d", i),
		}
	}
	require.NoError(t, store.SaveBracket(compID, &Bracket{Rounds: [][]BracketMatch{matches}}))

	var wg sync.WaitGroup
	errs := make(chan error, N)

	for i := range N {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			id := fmt.Sprintf("M%d", idx)
			winner := fmt.Sprintf("A%d", idx)
			err := store.UpdateBracket(compID, func(b *Bracket) error {
				for j := range b.Rounds[0] {
					if b.Rounds[0][j].ID == id {
						b.Rounds[0][j].Winner = winner
						b.Rounds[0][j].Status = MatchStatusCompleted
					}
				}
				return nil
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}

	final, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, final.Rounds[0], N)
	for _, m := range final.Rounds[0] {
		assert.NotEmpty(t, m.Winner, "match %s must have a winner after concurrent updates", m.ID)
	}
}

// TestConcurrent_SaveLoadPoolMatches validates that concurrent SavePoolMatches
// and LoadPoolMatches calls on the same competition do not race or corrupt data.
func TestConcurrent_SaveLoadPoolMatches(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const writers = 5
	const readers = 10
	compID := "concurrent-pools"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
	}))

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers)

	for i := range writers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sideA := fmt.Sprintf("Player-%d", i)
			if err := store.SavePoolMatches(compID, []MatchResult{
				{ID: "P1-0", SideA: sideA, SideB: "Bob", Status: MatchStatusScheduled},
			}); err != nil {
				errs <- err
			}
		}(i)
	}
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.LoadPoolMatches(compID); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}
}

// TestConcurrent_SetCompetitorStatus fires N goroutines each writing a distinct
// player's status. After all complete, all N statuses must be present.
func TestConcurrent_SetCompetitorStatus(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const N = 15
	compID := "concurrent-status"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	var wg sync.WaitGroup
	errs := make(chan error, N)

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			err := store.SetCompetitorStatus(compID, domain.CompetitorStatus{
				PlayerID: fmt.Sprintf("player-%d", i),
				Eligible: false,
				MatchID:  fmt.Sprintf("M%d", i),
				Reason:   "injury",
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.Len(t, statuses, N, "all %d concurrent status writes must be persisted", N)
}

// TestConcurrent_UpdatePoolMatchByID validates that N goroutines each updating
// a distinct match by ID produce exactly N updated matches with no data loss.
func TestConcurrent_UpdatePoolMatchByID(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const N = 12
	compID := "concurrent-pm-byid"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	initial := make([]MatchResult, N)
	for i := range N {
		initial[i] = MatchResult{
			ID:     fmt.Sprintf("P1-%d", i),
			SideA:  fmt.Sprintf("A%d", i),
			SideB:  fmt.Sprintf("B%d", i),
			Status: MatchStatusScheduled,
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, initial))

	var wg sync.WaitGroup
	errs := make(chan error, N)

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := fmt.Sprintf("P1-%d", i)
			winner := fmt.Sprintf("A%d", i)
			_, err := store.UpdatePoolMatchByID(compID, id, func(m *MatchResult) {
				m.Winner = winner
				m.Status = MatchStatusCompleted
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}

	results, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, results, N)
	for _, m := range results {
		assert.Equal(t, MatchStatusCompleted, m.Status, "match %s must be completed", m.ID)
	}
}

// TestConcurrent_UpdateCompetitionChanged verifies that concurrent
// UpdateCompetitionChanged calls on the same competition serialize correctly:
// no lost updates visible after all goroutines finish.
func TestConcurrent_UpdateCompetitionChanged(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	const N = 10
	compID := "concurrent-ucc"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "initial"}))

	var wg sync.WaitGroup
	errs := make(chan error, N)

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			newName := fmt.Sprintf("writer-%d", i)
			_, err := store.UpdateCompetitionChanged(compID, func(c *Competition) (*Competition, error) {
				if c == nil {
					return &Competition{ID: compID, Name: newName}, nil
				}
				c.Name = newName
				return c, nil
			})
			if err != nil {
				errs <- err
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		require.NoError(t, e)
	}

	c, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.NotEmpty(t, c.Name)
}

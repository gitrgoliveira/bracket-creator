package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_ConcurrentScoringAndOverridesWritesProduceConsistentSnapshots(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{
		Name:     "Concurrency Taikai",
		Date:     "03-11-2026",
		Venue:    "Arena",
		Courts:   []string{"A"},
		Password: "initial-secret-password",
	}))

	const compID = "live-comp"
	require.NoError(t, store.SaveCompetition(&Competition{
		ID:     compID,
		Name:   "Live Scoring",
		Date:   "03-11-2026",
		Status: CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: "p1", Name: "Alice", Dojo: "DojoA"},
		{ID: "p2", Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{
		{ID: "m1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2", Status: MatchStatusScheduled, Court: "A"},
	}))
	require.NoError(t, store.SaveBracket(compID, &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "b1", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2", Status: MatchStatusScheduled, Court: "A"}},
		},
	}))

	const iterations = 40
	var stop atomic.Bool
	var wg sync.WaitGroup

	// Writer 1: multi-file transactional scoring + status + history + event log writes.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for step := 1; step <= iterations; step++ {
			tag := fmt.Sprintf("step-%d", step)
			stamp := int64(step)
			txErr := store.WithTransaction(compID, func(tx StoreTx) error {
				pm := []MatchResult{{
					ID:         "m1",
					SideA:      "Alice",
					SideB:      "Bob",
					SideAID:    "p1",
					SideBID:    "p2",
					Winner:     tag,
					Status:     MatchStatusCompleted,
					Court:      "A",
					ModifiedAt: stamp,
				}}
				if err := tx.SavePoolMatches(compID, pm); err != nil {
					return err
				}
				br := &Bracket{
					Rounds: [][]BracketMatch{{{
						ID:         "b1",
						SideA:      "Alice",
						SideB:      "Bob",
						SideAID:    "p1",
						SideBID:    "p2",
						Winner:     tag,
						Status:     MatchStatusCompleted,
						Court:      "A",
						ModifiedAt: stamp,
					}}},
				}
				if err := tx.SaveBracket(compID, br); err != nil {
					return err
				}
				if err := tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
					PlayerID: "p2",
					Eligible: false,
					Reason:   tag,
					MatchID:  "m1",
				}); err != nil {
					return err
				}
				if err := tx.AppendMatchHistory(compID, MatchHistoryEntry{
					MatchID:  "m1",
					Door:     "score",
					Stamp:    stamp,
					Changed:  []string{"score"},
					Outcomes: map[string]string{"score": HistoryOutcomeApplied},
				}); err != nil {
					return err
				}
				return tx.AppendScoreLog(compID, "m1", ScoreLogLine{
					V: ScoreLogFormatVersion,
					Event: &domain.ScoreEvent{
						ID:      tag,
						Session: "s1",
						Seq:     stamp,
						Stamp:   stamp,
						Side:    "a",
						Kind:    domain.ScoreIpponAdd,
						Letter:  "M",
					},
				})
			})
			assert.NoError(t, txErr)
		}
		stop.Store(true)
	}()

	// Writer 2: concurrent rank overrides writes (serialised on s.mu).
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; !stop.Load(); i++ {
			r1 := (i % 2) + 1
			r2 := ((i + 1) % 2) + 1
			_, oerr := store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{
				"p1": r1,
				"p2": r2,
			})
			assert.NoError(t, oerr)
		}
	}()

	// Writer 3: concurrent tournament saves with changing passwords.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; !stop.Load(); i++ {
			_, terr := store.SaveTournamentChanged(&Tournament{
				Name:     fmt.Sprintf("Concurrency Taikai %d", i),
				Date:     "03-11-2026",
				Venue:    "Arena",
				Courts:   []string{"A"},
				Password: fmt.Sprintf("rotating-secret-%d", i),
			})
			assert.NoError(t, terr)
		}
	}()

	verifySnap := func(snap Snapshot, isTournament bool) {
		if isTournament {
			tBytes, ok := snap.Files["tournament.md"]
			if !assert.True(t, ok) {
				return
			}
			if !assert.NoError(t, CheckFrontMatter(tBytes)) {
				return
			}
			assert.NotContains(t, string(tBytes), "rotating-secret-")
			assert.NotContains(t, string(tBytes), "initial-secret-password")
			assert.NotContains(t, string(tBytes), "password")
		}

		cfgBytes, ok := snap.Files["competitions/"+compID+"/config.md"]
		if !assert.True(t, ok) {
			return
		}
		if !assert.NoError(t, CheckFrontMatter(cfgBytes)) {
			return
		}

		pmBytes, ok := snap.Files["competitions/"+compID+"/pool-matches.csv"]
		if !assert.True(t, ok) {
			return
		}
		pm, err := parsePoolMatchesBytes(pmBytes)
		if !assert.NoError(t, err) || !assert.Len(t, pm, 1) {
			return
		}

		brBytes, ok := snap.Files["competitions/"+compID+"/bracket.json"]
		if !assert.True(t, ok) {
			return
		}
		br, err := parseBracketBytes(brBytes)
		if !assert.NoError(t, err) || !assert.Len(t, br.Rounds, 1) || !assert.Len(t, br.Rounds[0], 1) {
			return
		}

		// Paired files written in the same transaction must always agree on the committed step.
		assert.Equal(t, pm[0].ModifiedAt, br.Rounds[0][0].ModifiedAt, "pool-matches.csv and bracket.json must come from the same transaction commit")
		assert.Equal(t, pm[0].Winner, br.Rounds[0][0].Winner, "pool-matches.csv and bracket.json winners must agree")

		if pm[0].ModifiedAt > 0 {
			stBytes, ok := snap.Files["competitions/"+compID+"/competitor-status.yaml"]
			if !assert.True(t, ok, "competitor-status.yaml must be present once step >= 1 committed") {
				return
			}
			statuses, serr := parseCompetitorStatusBytes(stBytes)
			if !assert.NoError(t, serr) {
				return
			}
			assert.Equal(t, pm[0].Winner, statuses["p2"].Reason, "competitor-status.yaml must match the transaction step of pool-matches.csv")

			histBytes, ok := snap.Files["competitions/"+compID+"/history/m1.jsonl"]
			if !assert.True(t, ok) {
				return
			}
			hist := parseMatchHistory(compID, "m1", histBytes)
			if !assert.NotEmpty(t, hist) {
				return
			}
			assert.Equal(t, pm[0].ModifiedAt, hist[len(hist)-1].Stamp)

			evBytes, ok := snap.Files["competitions/"+compID+"/events/m1.jsonl"]
			if !assert.True(t, ok) {
				return
			}
			evLines, eerr := parseScoreLog("events/m1.jsonl", evBytes)
			if !assert.NoError(t, eerr) || !assert.NotEmpty(t, evLines) {
				return
			}
			assert.Equal(t, pm[0].ModifiedAt, evLines[len(evLines)-1].Stamp())
		}

		if ovBytes, ok := snap.Files["competitions/"+compID+"/overrides.json"]; ok {
			var ov Overrides
			if !assert.NoError(t, json.Unmarshal(ovBytes, &ov)) {
				return
			}
			if poolA := ov.PoolRanks["Pool A"]; len(poolA) > 0 {
				r1 := poolA["id:p1"]
				r2 := poolA["id:p2"]
				assert.Equal(t, 3, r1+r2, "paired rank overrides written together must agree")
			}
		}
	}

	// Readers: concurrent tournament and competition snapshots.
	for r := 0; r < 2; r++ {
		wg.Add(1)
		go func(readerIdx int) {
			defer wg.Done()
			for !stop.Load() {
				if readerIdx%2 == 0 {
					snap, serr := store.Snapshot(ScopeTournament, "")
					if !assert.NoError(t, serr) {
						return
					}
					verifySnap(snap, true)
				} else {
					snap, serr := store.Snapshot(ScopeCompetition, compID)
					if !assert.NoError(t, serr) {
						return
					}
					verifySnap(snap, false)
				}
			}
		}(r)
	}

	wg.Wait()
}

func TestSnapshot_CompetitionDeletedWhileWaitingForLockIsLeftOutNotReadThroughOrphanedMutex(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveTournament(&Tournament{
		Name:   "Delete Race",
		Date:   "03-11-2026",
		Venue:  "Hall",
		Courts: []string{"A"},
	}))

	const victimID = "comp-victim"
	const survivorID = "comp-survivor"
	require.NoError(t, store.SaveCompetition(&Competition{ID: victimID, Name: "Victim"}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: survivorID, Name: "Survivor"}))

	// Hold the victim competition's initial mutex (as DeleteCompetition does).
	oldMu := store.getCompLock(victimID)
	oldMu.Lock()

	type res struct {
		snap Snapshot
		err  error
	}
	tournCh := make(chan res, 1)
	compCh := make(chan res, 1)

	go func() {
		s, serr := store.Snapshot(ScopeTournament, "")
		tournCh <- res{snap: s, err: serr}
	}()
	go func() {
		s, serr := store.Snapshot(ScopeCompetition, victimID)
		compCh <- res{snap: s, err: serr}
	}()

	// Let both snapshots block on oldMu.RLock().
	time.Sleep(30 * time.Millisecond)

	// Simulate DeleteCompetition retiring oldMu from compMu while a new mutex
	// is installed and held during directory removal: if Snapshot did not
	// re-check getCompLock(victimID) after acquiring oldMu.RLock(), it would
	// read through oldMu while newMu is still held!
	store.compMu.Delete(victimID)
	newMu := store.getCompLock(victimID)
	newMu.Lock()

	// Write a canary file while holding newMu, then unlock oldMu.
	canaryPath := store.compPath(victimID, "canary.txt")
	require.NoError(t, store.root.writeFile(canaryPath, []byte("uncommitted-under-new-lock"), 0o600))
	oldMu.Unlock()

	// Both snapshots must now block on newMu.RLock() rather than reading the canary through oldMu.
	time.Sleep(20 * time.Millisecond)
	select {
	case <-tournCh:
		t.Fatal("tournament snapshot returned while newMu was still write-locked (read through orphaned mutex)")
	case <-compCh:
		t.Fatal("competition snapshot returned while newMu was still write-locked (read through orphaned mutex)")
	default:
	}

	// Finish removing the victim folder under newMu and s.mu, then unlock newMu.
	store.mu.Lock()
	require.NoError(t, store.root.removeAll(store.compPath(victimID)))
	store.discardCompCacheBodies(victimID)
	store.mu.Unlock()
	newMu.Unlock()

	tournRes := <-tournCh
	require.NoError(t, tournRes.err)
	assert.NotContains(t, tournRes.snap.Competitions, victimID, "deleted competition must be left out of tournament snapshot")
	assert.Contains(t, tournRes.snap.Competitions, survivorID)
	for path := range tournRes.snap.Files {
		assert.NotContains(t, path, filepath.ToSlash(filepath.Join("competitions", victimID)))
	}

	compRes := <-compCh
	require.Error(t, compRes.err)
	assert.True(t, errors.Is(compRes.err, ErrCompetitionNotFound), "deleted competition must return ErrCompetitionNotFound for competition scope")
}

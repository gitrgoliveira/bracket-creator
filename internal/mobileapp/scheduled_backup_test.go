package mobileapp

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFixtureBackup(t *testing.T, s *state.Store, kind state.BackupKind, at time.Time) string {
	t.Helper()
	snap, err := s.Snapshot(state.ScopeTournament, "")
	require.NoError(t, err)

	comps := make([]backup.CompetitionRef, 0, len(snap.Competitions))
	for id, name := range snap.Competitions {
		comps = append(comps, backup.CompetitionRef{ID: id, Name: name})
	}
	sort.Slice(comps, func(i, j int) bool {
		return comps[i].ID < comps[j].ID
	})

	bkind := backup.KindScheduled
	if kind == state.BackupKindBeforeRestore {
		bkind = backup.KindBeforeRestore
	}

	meta := backup.Meta{
		CreatedAt:    at.UTC(),
		Scope:        backup.ScopeTournament,
		Kind:         bkind,
		Tournament:   backup.TournamentRef{Name: snap.TournamentName},
		Competitions: comps,
		Fingerprint:  state.Fingerprint(snap.Files),
	}

	var buf bytes.Buffer
	require.NoError(t, backup.Write(&buf, meta, snap.Files))

	name, err := s.SaveBackupFile(kind, state.ScopeTournament, at, buf.Bytes())
	require.NoError(t, err)
	return name
}

func loadTestPoolMatch(t *testing.T, s *state.Store, compID, matchID string) state.MatchResult {
	t.Helper()
	pm, err := s.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range pm {
		if m.ID == matchID {
			return m
		}
	}
	t.Fatalf("pool match %s not found in %s", matchID, compID)
	return state.MatchResult{}
}

func TestScheduledBackups_EmptyBackupsWithCompetition(t *testing.T) {
	pt := buildPopulatedTournament(t)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return now },
	}

	ctx := context.Background()
	require.NoError(t, job.runOnce(ctx))

	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	require.Equal(t, state.BackupKindScheduled, files[0].Kind)

	f, err := pt.Store.OpenBackupFile(files[0].Name)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, f.Close())
	}()

	info, err := f.Stat()
	require.NoError(t, err)

	checks := backup.Checks{
		ValidID:     state.ValidateCompetitionID,
		FrontMatter: state.CheckFrontMatter,
	}
	arc, err := backup.Read(f, info.Size(), checks)
	require.NoError(t, err)
	require.Equal(t, backup.KindScheduled, arc.Manifest.Kind)

	snap, err := pt.Store.Snapshot(state.ScopeTournament, "")
	require.NoError(t, err)
	require.Equal(t, state.Fingerprint(snap.Files), arc.Manifest.Fingerprint)
}

func TestScheduledBackups_FreshFolderNoCompetition(t *testing.T) {
	freshDir := t.TempDir()
	freshStore, err := state.NewStore(freshDir)
	require.NoError(t, err)

	job := &ScheduledBackups{
		Store: freshStore,
		Clock: func() time.Time { return time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC) },
	}

	ctx := context.Background()
	require.NoError(t, job.runOnce(ctx))

	files, err := freshStore.ListBackupFiles()
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestScheduledBackups_CadenceAndUnchanged(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
	}

	ctx := context.Background()
	// Initial copy at start.
	require.NoError(t, job.runOnce(ctx))
	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// 23h after: not due, no new file.
	clockTime = clockTime.Add(23 * time.Hour)
	require.NoError(t, job.runOnce(ctx))
	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// 25h after initial copy: due, but no changes, so no new file.
	clockTime = clockTime.Add(2 * time.Hour) // now 25h after initial
	require.NoError(t, job.runOnce(ctx))
	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
}

func TestScheduledBackups_NoOpDoorsAndDateOnlyChange(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
	}

	ctx := context.Background()
	require.NoError(t, job.runOnce(ctx))
	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// 1. No-op door: re-save tournament settings unchanged.
	tour, err := pt.Store.LoadTournament()
	require.NoError(t, err)
	tourBody := map[string]any{
		"name": tour.Name, "date": tour.Date, "venue": tour.Venue, "courts": tour.Courts,
		"password": pt.Password, "durationDays": tour.DurationDays,
		"clockToElapsedMultiplier": tour.ClockToElapsedMultiplier, "slowestCourtBufferPct": tour.SlowestCourtBufferPct,
		"mode": tour.Mode, "sponsors": tour.Sponsors,
	}
	w := populatedServeJSON(t, pt.Router, pt.Password, http.MethodPut, "/api/tournament", tourBody)
	require.Less(t, w.Code, 300)

	// Advance 25h and check: no new file.
	clockTime = clockTime.Add(25 * time.Hour)
	require.NoError(t, job.runOnce(ctx))
	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// 2. Date-only change: rescore with ModifiedAt advanced but scores identical.
	m := loadTestPoolMatch(t, pt.Store, pt.MixedCompID, pt.MixedScoredMatchID)
	path := "/api/competitions/" + pt.MixedCompID + "/matches/" + url.PathEscape(m.ID) + "/score"
	scoreBody := scorePayload(m.ID, m.SideA, m.SideB, m.SideAID, m.SideBID, m.Winner, m.WinnerID, m.Status, m.IpponsA, m.IpponsB, m.ModifiedAt)
	// An identical rescore first, as the fingerprint doors table does, then the same
	// scores again under a later stamp (a date-only change).
	w = populatedServeJSON(t, pt.Router, pt.Password, http.MethodPut, path, scoreBody)
	require.Less(t, w.Code, 300)
	scoreBody["modifiedAt"] = time.Now().UnixMilli() + 5000
	w = populatedServeJSON(t, pt.Router, pt.Password, http.MethodPut, path, scoreBody)
	require.Less(t, w.Code, 300)

	// Advance another 25h and check: date-only stamp change yields no new file.
	clockTime = clockTime.Add(25 * time.Hour)
	require.NoError(t, job.runOnce(ctx))
	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
}

func TestScheduledBackups_RealChangeAfter25h(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
	}

	ctx := context.Background()
	require.NoError(t, job.runOnce(ctx))
	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// Advance 25h.
	clockTime = clockTime.Add(25 * time.Hour)

	// Make a real change: rename competition.
	comp, err := pt.Store.LoadCompetition(pt.MixedCompID)
	require.NoError(t, err)
	comp.Name = "Individual Mixed Championship"
	_, err = pt.Store.SaveCompetitionChanged(comp)
	require.NoError(t, err)

	require.NoError(t, job.runOnce(ctx))
	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 2)
}

func TestScheduledBackups_FutureDatedCopyAndRealChange(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	// Write a future-dated scheduled copy 3 days ahead of the clock.
	futureTime := clockTime.Add(3 * 24 * time.Hour)
	writeFixtureBackup(t, pt.Store, state.BackupKindScheduled, futureTime)

	// Make a real change so current content differs from future-dated copy.
	comp, err := pt.Store.LoadCompetition(pt.MixedCompID)
	require.NoError(t, err)
	comp.Name = "Updated Competition Title"
	_, err = pt.Store.SaveCompetitionChanged(comp)
	require.NoError(t, err)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
	}

	ctx := context.Background()
	// At clockTime, newest non-future is nil (only future-dated exists), so it is due.
	// Current fingerprint differs from the future-dated archive, so exactly one copy is written.
	require.NoError(t, job.runOnce(ctx))

	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 2) // Future-dated one + current one

	// For the next 23 hours, advance hourly ticks: no new files written.
	for h := 1; h <= 23; h++ {
		clockTime = clockTime.Add(time.Hour)
		require.NoError(t, job.runOnce(ctx))
		files, err = pt.Store.ListBackupFiles()
		require.NoError(t, err)
		require.Len(t, files, 2, "hour %d produced an unexpected copy", h)
	}
}

func TestScheduledBackups_BeforeRestoreDoesNotResetClock(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
	}

	ctx := context.Background()
	// Initial scheduled backup at clockTime.
	require.NoError(t, job.runOnce(ctx))
	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)

	// 24.5 hours later, a before-restore safety copy is written.
	safetyTime := clockTime.Add(24*time.Hour + 30*time.Minute)
	writeFixtureBackup(t, pt.Store, state.BackupKindBeforeRestore, safetyTime)

	// Make a real change.
	comp, err := pt.Store.LoadCompetition(pt.MixedCompID)
	require.NoError(t, err)
	comp.Name = "Altered After Safety Backup"
	_, err = pt.Store.SaveCompetitionChanged(comp)
	require.NoError(t, err)

	// At 25h, clockTime is 25h after initial scheduled backup.
	clockTime = clockTime.Add(25 * time.Hour)

	// The safety copy must NOT reset the scheduled clock: scheduled copy is still due!
	require.NoError(t, job.runOnce(ctx))

	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	scheduledCount := 0
	for _, f := range files {
		if f.Kind == state.BackupKindScheduled {
			scheduledCount++
		}
	}
	require.Equal(t, 2, scheduledCount)
}

func TestScheduledBackups_WriteFailureRetriedAtNextTick(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	var failOnce atomic.Bool
	failOnce.Store(true)

	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
		save: func(kind state.BackupKind, scope state.SnapshotScope, at time.Time, data []byte) (string, error) {
			if failOnce.CompareAndSwap(true, false) {
				return "", errors.New("simulated save failure")
			}
			return pt.Store.SaveBackupFile(kind, scope, at, data)
		},
	}

	ctx := context.Background()
	// First tick fails.
	err := job.runOnce(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "simulated save failure")

	files, err := pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Empty(t, files)

	// Next tick succeeds.
	require.NoError(t, job.runOnce(ctx))

	files, err = pt.Store.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
}

func TestScheduledBackups_CancelContextStopsRun(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	tickCh := make(chan time.Time)
	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time { return clockTime },
		tick: func(_ time.Duration) (<-chan time.Time, func()) {
			return tickCh, func() {}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		job.Run(ctx)
		close(done)
	}()

	// Verify cancel stops Run promptly.
	cancel()

	select {
	case <-done:
		// Success: stopped within 1 second.
	case <-time.After(time.Second):
		t.Fatal("Run did not exit within 1s after context cancellation")
	}
}

func TestScheduledBackups_PanicRecoveryInRun(t *testing.T) {
	pt := buildPopulatedTournament(t)
	clockTime := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	var panicOnce atomic.Bool
	panicOnce.Store(true)

	tickCh := make(chan time.Time)
	job := &ScheduledBackups{
		Store: pt.Store,
		Clock: func() time.Time {
			if panicOnce.CompareAndSwap(true, false) {
				panic("injected panic in scheduled backup")
			}
			return clockTime
		},
		tick: func(_ time.Duration) (<-chan time.Time, func()) {
			return tickCh, func() {}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runReturned := make(chan struct{})
	go func() {
		job.Run(ctx)
		close(runReturned)
	}()

	// The start tick panics and recovers. Then we send a tick to drive normal execution.
	tickCh <- clockTime

	require.Eventually(t, func() bool {
		files, err := pt.Store.ListBackupFiles()
		return err == nil && len(files) == 1
	}, 5*time.Second, 10*time.Millisecond)

	// Ensure Run is still running and has not crashed.
	select {
	case <-runReturned:
		t.Fatal("Run exited prematurely after panic")
	default:
	}

	cancel()
	select {
	case <-runReturned:
	case <-time.After(time.Second):
		t.Fatal("Run did not exit after cancel")
	}
}

// A scheduled backup the check cannot read (not a zip, cut short, empty, or
// not openable) is skipped: the tick neither panics nor fails, and it writes
// a fresh readable copy, which the next due check compares as usual. A
// readable copy the check also looks at (a future-dated one) is still
// compared past the unreadable one, so unchanged content writes nothing.
func TestScheduledBackups_UnreadableNewestCopyIsSkipped(t *testing.T) {
	cases := []struct {
		name    string
		corrupt func(t *testing.T, path string)
	}{
		{"not a zip", func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, []byte("this is not a zip archive"), 0o600))
		}},
		{"truncated", func(t *testing.T, path string) {
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, data[:len(data)/2], 0o600))
		}},
		{"zero bytes", func(t *testing.T, path string) {
			require.NoError(t, os.WriteFile(path, nil, 0o600))
		}},
		{"cannot be opened", func(t *testing.T, path string) {
			if runtime.GOOS == "windows" || os.Geteuid() == 0 {
				t.Skip("file permissions do not stop this user from reading")
			}
			require.NoError(t, os.Chmod(path, 0))
		}},
	}
	checks := backup.Checks{ValidID: state.ValidateCompetitionID, FrontMatter: state.CheckFrontMatter}
	start := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)

	newStore := func(t *testing.T) (*state.Store, string) {
		t.Helper()
		dir := t.TempDir()
		s, err := state.NewStore(dir)
		require.NoError(t, err)
		require.NoError(t, s.SaveTournament(&state.Tournament{Name: "Kendo Taikai", Password: "pw", Courts: []string{"A"}}))
		require.NoError(t, s.SaveCompetition(&state.Competition{ID: "open-comp", Name: "Open", Courts: []string{"A"}, NumberPrefix: "O"}))
		return s, dir
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()

			t.Run("a fresh copy is written", func(t *testing.T) {
				s, dir := newStore(t)
				bad := writeFixtureBackup(t, s, state.BackupKindScheduled, start)
				tc.corrupt(t, filepath.Join(dir, state.BackupsDirName, bad))

				clock := start.Add(25 * time.Hour)
				job := &ScheduledBackups{Store: s, Clock: func() time.Time { return clock }}
				require.NotPanics(t, func() { require.NoError(t, job.runOnce(ctx)) })

				files, err := s.ListBackupFiles()
				require.NoError(t, err)
				require.Len(t, files, 2)
				require.NotEqual(t, bad, files[0].Name)
				snap, err := s.Snapshot(state.ScopeTournament, "")
				require.NoError(t, err)
				fp, ok := job.readArchiveFingerprint(files[0].Name, checks)
				require.True(t, ok, "the fresh copy must be readable")
				assert.Equal(t, state.Fingerprint(snap.Files), fp)

				// Due again, content unchanged: the fresh copy is compared, so nothing is written.
				clock = clock.Add(25 * time.Hour)
				require.NoError(t, job.runOnce(ctx))
				files, err = s.ListBackupFiles()
				require.NoError(t, err)
				assert.Len(t, files, 2)
			})

			t.Run("a readable copy with the same content is still compared", func(t *testing.T) {
				s, dir := newStore(t)
				writeFixtureBackup(t, s, state.BackupKindScheduled, start.Add(72*time.Hour))
				bad := writeFixtureBackup(t, s, state.BackupKindScheduled, start)
				tc.corrupt(t, filepath.Join(dir, state.BackupsDirName, bad))

				job := &ScheduledBackups{Store: s, Clock: func() time.Time { return start.Add(25 * time.Hour) }}
				require.NoError(t, job.runOnce(ctx))
				files, err := s.ListBackupFiles()
				require.NoError(t, err)
				assert.Len(t, files, 2, "unchanged content matching a readable copy writes nothing")
			})
		})
	}
}

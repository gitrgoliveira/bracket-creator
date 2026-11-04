package mobileapp

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// cachedFingerprint is the fingerprint read from one backup file, kept with
// the file's size so a rewritten file is read again.
type cachedFingerprint struct {
	size int64
	fp   string
}

// ScheduledBackups runs a periodic backup job that creates a tournament
// backup archive if at least 24 hours have passed since the newest
// scheduled backup and the tournament content has changed.
type ScheduledBackups struct {
	Store    *state.Store
	Clock    func() time.Time
	Interval time.Duration

	// tick provides an injectable tick source for testing. If nil, time.NewTicker is used.
	tick func(time.Duration) (<-chan time.Time, func())
	// save provides an injectable SaveBackupFile seam for testing. If nil, Store.SaveBackupFile is used.
	save func(kind state.BackupKind, scope state.SnapshotScope, at time.Time, data []byte) (string, error)

	// cache remembers the fingerprint of each backup file already read, by file
	// name, so an hourly tick over unchanged data does not inflate and hash the
	// newest archive again.
	mu    sync.Mutex
	cache map[string]cachedFingerprint
}

func (s *ScheduledBackups) now() time.Time {
	if s.Clock != nil {
		return s.Clock()
	}
	return time.Now()
}

// Run starts the periodic scheduled backup loop. It runs one check immediately
// at start, then checks every Interval until ctx is cancelled.
func (s *ScheduledBackups) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s.tickOnceSafe(ctx)

	interval := s.Interval
	if interval <= 0 {
		interval = time.Hour
	}

	var ch <-chan time.Time
	var stop func()
	if s.tick != nil {
		ch, stop = s.tick(interval)
	} else {
		ticker := time.NewTicker(interval)
		ch = ticker.C
		stop = ticker.Stop
	}
	defer stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ch:
			s.tickOnceSafe(ctx)
		}
	}
}

func (s *ScheduledBackups) tickOnceSafe(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			stack := debug.Stack()
			slog.Error("scheduled backup: panic recovered", "panic", r, "stack", string(stack))
		}
	}()

	if err := s.runOnce(ctx); err != nil {
		slog.Warn("scheduled backup: check failed; will retry at next tick", "err", err)
	}
}

func (s *ScheduledBackups) readArchiveFingerprint(name string, checks backup.Checks) (string, bool) {
	f, err := s.Store.OpenBackupFile(name)
	if err != nil {
		slog.Warn("scheduled backup: open backup file for fingerprint check failed", "file", name, "err", err)
		return "", false
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			slog.Warn("scheduled backup: close backup file failed", "file", name, "err", cerr)
		}
	}()

	info, err := f.Stat()
	if err != nil {
		slog.Warn("scheduled backup: stat backup file failed", "file", name, "err", err)
		return "", false
	}

	arc, err := backup.Read(f, info.Size(), checks)
	if err != nil {
		slog.Warn("scheduled backup: read backup archive failed", "file", name, "err", err)
		return "", false
	}
	return arc.Manifest.Fingerprint, true
}

// getOrReadArchiveFingerprint is readArchiveFingerprint through the cache.
func (s *ScheduledBackups) getOrReadArchiveFingerprint(bf state.BackupFile, checks backup.Checks) (string, bool) {
	s.mu.Lock()
	if c, ok := s.cache[bf.Name]; ok && c.size == bf.Size {
		s.mu.Unlock()
		return c.fp, true
	}
	s.mu.Unlock()

	fp, ok := s.readArchiveFingerprint(bf.Name, checks)
	if !ok {
		return "", false
	}

	s.mu.Lock()
	if s.cache == nil {
		s.cache = make(map[string]cachedFingerprint)
	}
	s.cache[bf.Name] = cachedFingerprint{size: bf.Size, fp: fp}
	s.mu.Unlock()

	return fp, true
}

// runOnce performs one scheduled backup check.
func (s *ScheduledBackups) runOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	now := s.now()
	files, err := s.Store.ListBackupFiles()
	if err != nil {
		return fmt.Errorf("list backup files: %w", err)
	}

	s.mu.Lock()
	for name := range s.cache {
		if !slices.ContainsFunc(files, func(f state.BackupFile) bool { return f.Name == name }) {
			delete(s.cache, name)
		}
	}
	s.mu.Unlock()

	var newestNonFuture *state.BackupFile
	var futureScheduled []state.BackupFile

	for i := range files {
		bf := files[i]
		if bf.Kind != state.BackupKindScheduled {
			continue
		}
		if bf.Time.After(now) {
			futureScheduled = append(futureScheduled, bf)
		} else if newestNonFuture == nil {
			newestNonFuture = &bf
		}
	}

	isDue := false
	if newestNonFuture == nil {
		isDue = true
	} else if now.Sub(newestNonFuture.Time) >= 24*time.Hour {
		isDue = true
	}

	if !isDue {
		return nil
	}

	snap, err := s.Store.Snapshot(state.ScopeTournament, "")
	if err != nil {
		return fmt.Errorf("tournament snapshot: %w", err)
	}

	if len(snap.Competitions) == 0 {
		return nil
	}

	fp := state.Fingerprint(snap.Files)

	copiesToCheck := make([]state.BackupFile, 0, len(futureScheduled)+1)
	if newestNonFuture != nil {
		copiesToCheck = append(copiesToCheck, *newestNonFuture)
	}
	copiesToCheck = append(copiesToCheck, futureScheduled...)

	checks := BackupChecks()

	for _, copyFile := range copiesToCheck {
		existingFP, ok := s.getOrReadArchiveFingerprint(copyFile, checks)
		if ok && existingFP == fp {
			return nil
		}
	}

	data, err := SnapshotArchive(snap, backup.KindScheduled, now, nil)
	if err != nil {
		return fmt.Errorf("write backup archive: %w", err)
	}

	saveFn := s.save
	if saveFn == nil {
		saveFn = s.Store.SaveBackupFile
	}

	if _, err := saveFn(state.BackupKindScheduled, state.ScopeTournament, now, data); err != nil {
		return fmt.Errorf("save backup file: %w", err)
	}

	return nil
}

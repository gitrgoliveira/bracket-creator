package mobileapp

import (
	"bytes"
	"context"
	"log/slog"
	"runtime/debug"
	"sort"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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

// runOnce performs one scheduled backup check.
func (s *ScheduledBackups) runOnce(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	now := s.now()
	files, err := s.Store.ListBackupFiles()
	if err != nil {
		slog.Warn("scheduled backup: list backup files failed", "err", err)
		return err
	}

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
		slog.Warn("scheduled backup: tournament snapshot failed", "err", err)
		return err
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

	checks := backup.Checks{
		ValidID:     state.ValidateCompetitionID,
		FrontMatter: state.CheckFrontMatter,
	}

	for _, copyFile := range copiesToCheck {
		existingFP, ok := s.readArchiveFingerprint(copyFile.Name, checks)
		if ok && existingFP == fp {
			return nil
		}
	}

	comps := make([]backup.CompetitionRef, 0, len(snap.Competitions))
	for id, name := range snap.Competitions {
		comps = append(comps, backup.CompetitionRef{
			ID:   id,
			Name: name,
		})
	}
	sort.Slice(comps, func(i, j int) bool {
		return comps[i].ID < comps[j].ID
	})

	meta := backup.Meta{
		CreatedAt:    now.UTC(),
		Scope:        backup.ScopeTournament,
		Kind:         backup.KindScheduled,
		Tournament:   backup.TournamentRef{Name: snap.TournamentName},
		Competitions: comps,
		Fingerprint:  fp,
	}

	var buf bytes.Buffer
	if err := backup.Write(&buf, meta, snap.Files); err != nil {
		slog.Warn("scheduled backup: write backup archive failed", "err", err)
		return err
	}

	saveFn := s.save
	if saveFn == nil {
		saveFn = s.Store.SaveBackupFile
	}

	if _, err := saveFn(state.BackupKindScheduled, state.ScopeTournament, now, buf.Bytes()); err != nil {
		slog.Warn("scheduled backup: save backup file failed", "err", err)
		return err
	}

	return nil
}

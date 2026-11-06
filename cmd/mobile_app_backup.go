package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/mobileapp"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/spf13/cobra"
)

type operatorSentence string

func (s operatorSentence) Error() string {
	return string(s)
}

type mobileAppBackupOptions struct {
	folder      string
	out         string
	competition string
}

type mobileAppRestoreOptions struct {
	folder       string
	only         string
	replace      []string
	copy         []string
	keep         []string
	remove       []string
	undo         bool
	password     string
	allowRunning bool
	lockedMode   bool
}

// defaultTournamentFolder is the --folder default: TOURNAMENT_DATA_DIR, else the current folder.
func defaultTournamentFolder() string {
	if f := os.Getenv("TOURNAMENT_DATA_DIR"); f != "" {
		return f
	}
	return "."
}

func newMobileAppBackupCmd() *cobra.Command {
	o := &mobileAppBackupOptions{}

	defaultFolder := defaultTournamentFolder()

	cmd := &cobra.Command{
		Use:          "backup",
		Short:        "create a backup archive of the tournament or a single competition",
		Long:         "Create a backup archive of the tournament data or a single competition.\nStop the server (the running mobile-app) before running this command, because the CLI works on the tournament data folder directly and cannot see a running server.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd)
		},
	}

	cmd.Flags().StringVarP(&o.folder, "folder", "f", defaultFolder, "folder storing tournament data (env: TOURNAMENT_DATA_DIR)")
	cmd.Flags().StringVar(&o.out, "out", "", "output backup zip file path (required)")
	cmd.Flags().StringVar(&o.competition, "competition", "", "optional competition ID to backup (omitting backups whole tournament)")
	if err := cmd.MarkFlagRequired("out"); err != nil {
		panic(err)
	}

	return cmd
}

func newMobileAppRestoreCmd() *cobra.Command {
	o := &mobileAppRestoreOptions{}

	defaultFolder := defaultTournamentFolder()

	cmd := &cobra.Command{
		Use:          "restore FILE",
		Short:        "restore tournament data from a backup archive",
		Long:         "Restore tournament data from a backup archive.\nStop the server (the running mobile-app) before running this command, because the CLI works on the tournament data folder directly and cannot see a running server. The --password flag sets the password written into tournament.md, which only signs in under file mode. With --lock-password (env: LOCK_PASSWORD) the restore is handled as in locked mode: the password comes from TOURNAMENT_PASSWORD_HASH, so a restore that would otherwise need --password does not.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return o.run(cmd, args[0])
		},
	}

	cmd.Flags().StringVarP(&o.folder, "folder", "f", defaultFolder, "folder storing tournament data (env: TOURNAMENT_DATA_DIR)")
	cmd.Flags().StringVar(&o.only, "only", "", "restore only this competition ID")
	cmd.Flags().StringArrayVar(&o.replace, "replace", nil, "competition ID to replace")
	cmd.Flags().StringArrayVar(&o.copy, "copy", nil, "competition ID and new name to copy (ID=NAME)")
	cmd.Flags().StringArrayVar(&o.keep, "keep", nil, "competition ID to leave as the folder has it (whole-tournament restore): a competition you already have stays as it is, and one only the backup holds is not added")
	cmd.Flags().StringArrayVar(&o.remove, "remove", nil, "competition ID to remove")
	cmd.Flags().BoolVar(&o.undo, "undo", false, "undo a previous restore using its safety copy")
	cmd.Flags().StringVar(&o.password, "password", "", "admin password for the restored tournament (file mode only)")
	cmd.Flags().BoolVar(&o.allowRunning, "allow-running", false, "allow restore even if matches are currently running")
	cmd.Flags().BoolVar(&o.lockedMode, "lock-password", false, "handle the restored password as in locked mode: it comes from TOURNAMENT_PASSWORD_HASH, so --password is not needed (env: LOCK_PASSWORD)")

	return cmd
}

func (o *mobileAppBackupOptions) run(cmd *cobra.Command) (err error) {
	if o.out == "" {
		return errors.New("--out is required")
	}

	tournPath := filepath.Join(o.folder, "tournament.md")
	if _, err := os.Stat(tournPath); errors.Is(err, os.ErrNotExist) {
		return errors.New("no tournament in this folder")
	} else if err != nil {
		return fmt.Errorf("check tournament file: %w", err)
	}

	store, err := state.NewStore(o.folder)
	if err != nil {
		return fmt.Errorf("failed to initialize state store at %q: %w", o.folder, err)
	}

	var snap state.Snapshot
	if o.competition != "" {
		snap, err = store.Snapshot(state.ScopeCompetition, o.competition)
		if err != nil {
			if errors.Is(err, state.ErrCompetitionNotFound) {
				return fmt.Errorf("competition %q not found", o.competition)
			}
			if errors.Is(err, state.ErrPendingWALUnsettled) {
				return operatorSentence("A competition has a write that could not be finished. Start the app once so it replays its pending writes, stop it, then retry.")
			}
			return fmt.Errorf("snapshot competition %q: %w", o.competition, err)
		}
		if snap.TournamentName == "" {
			if t, lerr := store.LoadTournament(); lerr == nil && t != nil {
				snap.TournamentName = t.Name
			}
		}
	} else {
		snap, err = store.Snapshot(state.ScopeTournament, "")
		if err != nil {
			if errors.Is(err, state.ErrPendingWALUnsettled) {
				return operatorSentence("A competition has a write that could not be finished. Start the app once so it replays its pending writes, stop it, then retry.")
			}
			return fmt.Errorf("snapshot tournament: %w", err)
		}
	}

	data, err := mobileapp.SnapshotArchive(snap, backup.KindManual, time.Now().UTC(), nil)
	if err != nil {
		return fmt.Errorf("create backup archive: %w", err)
	}

	outDir := filepath.Dir(o.out)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0o750); err != nil {
			return fmt.Errorf("create output directory %q: %w", outDir, err)
		}
	}

	tmpFile, err := os.CreateTemp(outDir, ".backup-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary backup file: %w", err)
	}
	tmpName := tmpFile.Name()
	cleanup := true
	defer func() {
		if cleanup {
			if cerr := tmpFile.Close(); cerr != nil && !errors.Is(cerr, os.ErrClosed) {
				err = errors.Join(err, cerr)
			}
			if rerr := os.Remove(tmpName); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
				err = errors.Join(err, rerr)
			}
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		return fmt.Errorf("write backup file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		return fmt.Errorf("sync backup file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("close backup file: %w", err)
	}
	if err := os.Rename(tmpName, o.out); err != nil {
		return fmt.Errorf("finalize backup file %q: %w", o.out, err)
	}
	cleanup = false

	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Backup written to %s\n", o.out); err != nil {
		return err
	}
	return nil
}

func (o *mobileAppRestoreOptions) run(cmd *cobra.Command, archivePath string) (err error) {
	seen := make(map[string]string)
	for _, id := range o.replace {
		if prev, ok := seen[id]; ok {
			return fmt.Errorf("competition %q is specified multiple times (--%s and --replace)", id, prev)
		}
		seen[id] = "replace"
	}

	type copyPair struct {
		id   string
		name string
	}
	var copies []copyPair
	for _, item := range o.copy {
		id, name, found := strings.Cut(item, "=")
		if !found || id == "" || name == "" {
			return fmt.Errorf("invalid --copy flag value %q: expected ID=NAME with non-empty id and name", item)
		}
		if prev, ok := seen[id]; ok {
			return fmt.Errorf("competition %q is specified multiple times (--%s and --copy)", id, prev)
		}
		seen[id] = "copy"
		copies = append(copies, copyPair{id: id, name: name})
	}
	for _, id := range o.keep {
		if prev, ok := seen[id]; ok {
			return fmt.Errorf("competition %q is specified multiple times (--%s and --keep)", id, prev)
		}
		seen[id] = "keep"
	}
	for _, id := range o.remove {
		if prev, ok := seen[id]; ok {
			return fmt.Errorf("competition %q is specified multiple times (--%s and --remove)", id, prev)
		}
		seen[id] = "remove"
	}

	if o.only != "" {
		if err := state.ValidateCompetitionID(o.only); err != nil {
			return fmt.Errorf("invalid --only competition id %q: %w", o.only, err)
		}
	}

	decisions := make(map[string]state.RestoreDecision)
	for _, id := range o.replace {
		decisions[id] = state.RestoreDecision{Action: state.RestoreReplace}
	}
	for _, cp := range copies {
		decisions[cp.id] = state.RestoreDecision{Action: state.RestoreCopy, CopyName: cp.name}
	}
	for _, id := range o.keep {
		decisions[id] = state.RestoreDecision{Action: state.RestoreKeep}
	}
	for _, id := range o.remove {
		decisions[id] = state.RestoreDecision{Action: state.RestoreRemove}
	}

	tournPath := filepath.Join(o.folder, "tournament.md")
	_, statErr := os.Stat(tournPath)
	tournExists := statErr == nil

	lockedMode, err := resolveLockedMode(cmd, o.lockedMode)
	if err != nil {
		return err
	}

	f, err := os.Open(filepath.Clean(archivePath))
	if err != nil {
		return fmt.Errorf("open backup file %q: %w", archivePath, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("stat backup file %q: %w", archivePath, err)
	}

	archive, err := backup.Read(f, info.Size(), mobileapp.BackupChecks())
	if err != nil {
		var be *backup.Error
		if errors.As(err, &be) {
			return fmt.Errorf("this file is not a backup this app can restore (%s): %s", be.Code, be.Detail)
		}
		return fmt.Errorf("this file is not a backup this app can restore: %w", err)
	}

	if !tournExists {
		if archive.Manifest.Scope != backup.ScopeTournament || o.only != "" {
			return errors.New("cannot restore a competition backup into a folder with no tournament")
		}
		if o.password == "" && !lockedMode {
			return errors.New("admin password is required for a new tournament: specify --password")
		}
	}

	store, err := state.NewStore(o.folder)
	if err != nil {
		return fmt.Errorf("failed to initialize state store at %q: %w", o.folder, err)
	}

	req := state.RestoreRequest{
		Archive:      archive,
		Only:         o.only,
		Decisions:    decisions,
		Undo:         o.undo,
		AllowRunning: o.allowRunning,
		LockedMode:   lockedMode,
		NewPassword:  o.password,
	}

	dryRunPlan, err := store.PlanRestore(req)
	if err != nil {
		return formatRestoreError(err)
	}
	if dryRunPlan.NothingToRestore {
		msg := dryRunPlan.Message
		if msg == "" {
			msg, _ = mobileapp.RestoreRefusalSentence(state.RestoreCodeNothingToRestore)
		}
		return operatorSentence(msg)
	}

	if len(dryRunPlan.Missing) > 0 {
		var lines []string
		lines = append(lines, "restore requires a decision for the following competitions:")
		for _, missingID := range dryRunPlan.Missing {
			found := false
			for _, comp := range dryRunPlan.Competitions {
				if comp.ID == missingID {
					choiceStrs := make([]string, len(comp.Choices))
					for i, c := range comp.Choices {
						choiceStrs[i] = string(c)
					}
					lines = append(lines, fmt.Sprintf("  - %s (%s): choose %s", comp.ID, restoreCompName(comp), joinChoices(choiceStrs)))
					found = true
					break
				}
			}
			if !found {
				lines = append(lines, fmt.Sprintf("  - %s", missingID))
			}
		}
		lines = append(lines, "Rerun the command specifying --replace, --copy ID=NAME, --keep, or --remove for each competition.")
		return operatorSentence(strings.Join(lines, "\n"))
	}

	now := time.Now().UTC()
	var safetyCopyName string
	req.BeforeSwap = mobileapp.RestoreBeforeSwap(store, now, &safetyCopyName)

	restoredPlan, err := store.Restore(req)
	if err != nil {
		return formatRestoreError(err)
	}

	out := cmd.OutOrStdout()
	if safetyCopyName != "" {
		if _, err := fmt.Fprintf(out, "Safety copy saved: %s\n", safetyCopyName); err != nil {
			return err
		}
	}

	if err := printRestoreOutcome(out, restoredPlan); err != nil {
		return err
	}

	eng := engine.New(store)
	prefixWarnings, err := mobileapp.ResolveRestoredPrefixes(store, eng)
	if err != nil {
		return fmt.Errorf("reassign competitor number prefixes: %w", err)
	}
	for _, pw := range prefixWarnings {
		if _, err := fmt.Fprintf(out, "Warning: %s\n", pw.Message); err != nil {
			return err
		}
	}

	if err := mobileapp.ResolveRestoredRequeues(store, eng); err != nil {
		return fmt.Errorf("return restored running matches to queue: %w", err)
	}

	if _, err := fmt.Fprintln(out, "Restore completed successfully."); err != nil {
		return err
	}
	return nil
}

// joinChoices lists choices as "a or b", or "a, b or c".
func joinChoices(choices []string) string {
	if len(choices) <= 1 {
		return strings.Join(choices, "")
	}
	return strings.Join(choices[:len(choices)-1], ", ") + " or " + choices[len(choices)-1]
}

// restoreCompName names a competition by its name in the backup, adding the
// folder's name when it was renamed after the backup.
func restoreCompName(comp state.RestoreCompetitionPlan) string {
	if comp.CurrentName == "" {
		return fmt.Sprintf("%q", comp.Name)
	}
	return fmt.Sprintf("%q, called %q now", comp.Name, comp.CurrentName)
}

func printRestoreOutcome(out io.Writer, plan state.RestorePlan) error {
	var created, replaced, copied, kept, leftOut, removed []string
	for _, comp := range plan.Competitions {
		switch comp.Action {
		case state.RestoreCreate:
			created = append(created, fmt.Sprintf("%s (%q)", comp.ID, comp.Name))
		case state.RestoreReplace:
			replaced = append(replaced, fmt.Sprintf("%s (%q)", comp.ID, comp.Name))
		case state.RestoreCopy:
			copied = append(copied, fmt.Sprintf("%s (%q) -> %s", comp.ID, comp.Name, comp.NewID))
		case state.RestoreKeep:
			// A create answered keep was only in the backup: it was not added.
			if slices.Contains(comp.Choices, state.RestoreCreate) {
				leftOut = append(leftOut, fmt.Sprintf("%s (%q)", comp.ID, comp.Name))
			} else {
				kept = append(kept, fmt.Sprintf("%s (%s)", comp.ID, restoreCompName(comp)))
			}
		case state.RestoreRemove:
			removed = append(removed, fmt.Sprintf("%s (%q)", comp.ID, comp.Name))
		}
	}
	if len(created) > 0 {
		if _, err := fmt.Fprintf(out, "Created: %s\n", strings.Join(created, ", ")); err != nil {
			return err
		}
	}
	if len(replaced) > 0 {
		if _, err := fmt.Fprintf(out, "Replaced: %s\n", strings.Join(replaced, ", ")); err != nil {
			return err
		}
	}
	if len(copied) > 0 {
		if _, err := fmt.Fprintf(out, "Copied: %s\n", strings.Join(copied, ", ")); err != nil {
			return err
		}
	}
	if len(kept) > 0 {
		if _, err := fmt.Fprintf(out, "Kept: %s\n", strings.Join(kept, ", ")); err != nil {
			return err
		}
	}
	if len(leftOut) > 0 {
		if _, err := fmt.Fprintf(out, "Left out: %s\n", strings.Join(leftOut, ", ")); err != nil {
			return err
		}
	}
	if len(removed) > 0 {
		if _, err := fmt.Fprintf(out, "Removed: %s\n", strings.Join(removed, ", ")); err != nil {
			return err
		}
	}
	for _, w := range plan.Warnings {
		if _, err := fmt.Fprintf(out, "Warning: %s\n", w.Message); err != nil {
			return err
		}
	}
	return nil
}

func formatRestoreError(err error) error {
	if err == nil {
		return nil
	}
	var re *state.RestoreError
	if errors.As(err, &re) {
		switch re.Code {
		case state.RestoreCodePendingWriteUnsettled:
			return operatorSentence("A competition has a write that could not be finished. Start the app once so it replays its pending writes, stop it, then retry.")
		case state.RestoreCodeMatchesRunning:
			return operatorSentence("Matches are running in a competition this restore replaces or removes. Finish them, or pass --allow-running to restore anyway.")
		case state.RestoreCodePasswordRequired:
			return operatorSentence("Set an admin password for the restored tournament first (use --password).")
		default:
			if sentence, ok := mobileapp.RestoreRefusalSentence(re.Code); ok {
				return operatorSentence(sentence)
			}
			return fmt.Errorf("restore refused (%s): %s", re.Code, re.Error())
		}
	}
	if errors.Is(err, state.ErrPendingWALUnsettled) {
		return operatorSentence("A competition has a write that could not be finished. Start the app once so it replays its pending writes, stop it, then retry.")
	}
	return err
}

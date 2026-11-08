package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Backup and restore over HTTP (bc-bkrs). Every route here is the
// organiser's: all seven sit in isSelfRunMainGatedConfigRoute, and the restore
// and the backup delete also ask for the password again (confirmedRoutes).
//
//	GET    /api/tournament/backup          a backup of the whole tournament
//	GET    /api/competitions/:id/backup    a backup of one competition
//	POST   /api/tournament/restore/plan    what a restore would do; changes nothing
//	POST   /api/tournament/restore         the restore itself
//	GET    /api/tournament/backups         the copies kept in backups/
//	GET    /api/tournament/backups/:name   one of them
//	DELETE /api/tournament/backups/:name   remove one of them

var (
	_ BackupStore   = (*state.Store)(nil)
	_ PrefixEngine  = (*engine.Engine)(nil)
	_ RestoreEngine = (*engine.Engine)(nil)
)

// Codes of the refusals this file makes itself; the restore's own come from
// state.RestoreError.
const (
	backupCodeArchiveInvalid = "archive_invalid"
	backupCodeBadBackupName  = "bad_backup_name"
	backupCodeBackupNotFound = "backup_not_found"
	backupCodeBadRequest     = "bad_restore_request"
	// backupCodeVersionMismatch: the running version of the app cannot
	// restore the archive, a later or unordered version made it
	// (backup.CheckAppVersion).
	backupCodeVersionMismatch = "backup_version_mismatch"
)

// BackupChecks returns the store's validators for backup archives.
func BackupChecks() backup.Checks {
	return backup.Checks{
		ValidID:     state.ValidateCompetitionID,
		FrontMatter: state.CheckFrontMatter,
	}
}

// backupReadChecks are the store's own validators the archive reader runs.
var backupReadChecks = BackupChecks()

// restoreRefusalStatus is the HTTP status of each restore refusal. Every one
// of them leaves the data folder as it was.
var restoreRefusalStatus = map[string]int{
	state.RestoreCodeDecisionMissing:       http.StatusConflict,
	state.RestoreCodeCopyNameTaken:         http.StatusConflict,
	state.RestoreCodeNameTaken:             http.StatusConflict,
	state.RestoreCodePlanChanged:           http.StatusConflict,
	state.RestoreCodeMatchesRunning:        http.StatusConflict,
	state.RestoreCodePendingWriteUnsettled: http.StatusConflict,
	state.RestoreCodePasswordRequired:      http.StatusConflict,
	state.RestoreCodeNothingToRestore:      http.StatusConflict,
	state.RestoreCodeNotASafetyCopy:        http.StatusBadRequest,
	state.RestoreCodeBadDecision:           http.StatusBadRequest,
	state.RestoreCodeFileUnreadable:        http.StatusBadRequest,
	state.RestoreCodeFolderIncomplete:      http.StatusConflict,
	// `only` names a competition the archive does not hold: the request is
	// wrong, not the folder's state.
	state.RestoreCodeNotInArchive: http.StatusBadRequest,
}

// restoreRefusalSentence is what the operator reads for each refusal.
var restoreRefusalSentence = map[string]string{
	state.RestoreCodeDecisionMissing:       "Choose what to do with each competition listed, then restore again.",
	state.RestoreCodeCopyNameTaken:         "That name is already used. Pick another name for the copy.",
	state.RestoreCodeNameTaken:             "Another competition already has that name. Rename one of them, or choose a copy with a new name.",
	state.RestoreCodePlanChanged:           "The competitions changed since the restore was checked. Check it again, then restore.",
	state.RestoreCodeMatchesRunning:        "Matches are running in a competition this restore replaces or removes. Finish them, or choose to restore anyway.",
	state.RestoreCodePendingWriteUnsettled: "A competition has a write that could not be finished. Restart the app, then try again.",
	state.RestoreCodePasswordRequired:      "Set an admin password for the restored tournament first.",
	state.RestoreCodeNothingToRestore:      "This copy holds nothing to restore.",
	state.RestoreCodeNotASafetyCopy:        "Only a copy taken before a restore can undo it. Restore this file without Undo.",
	state.RestoreCodeBadDecision:           "One of the choices is not allowed for that competition. Check the choices again.",
	state.RestoreCodeFileUnreadable:        "A file in the backup cannot be read, so nothing was restored.",
	state.RestoreCodeFolderIncomplete:      "A competition folder in the app has no settings file, so it cannot be copied before the restore replaces or removes it. Keep it, or restore it as a copy.",
	state.RestoreCodeNotInArchive:          "That competition is not in this backup.",
}

// RestoreRefusalSentence returns the operator sentence for a restore refusal code.
func RestoreRefusalSentence(code string) (string, bool) {
	s, ok := restoreRefusalSentence[code]
	return s, ok
}

// respondBackupRefusal answers a refusal with the body the package's other
// coded refusals use: the sentence in error and message, plus the code.
func respondBackupRefusal(c *gin.Context, status int, code, sentence string, extra gin.H) {
	body := gin.H{"error": sentence, "code": code, "message": sentence}
	for k, v := range extra {
		body[k] = v
	}
	c.AbortWithStatusJSON(status, body)
}

// SnapshotArchive turns a snapshot into the bytes of a backup archive that
// names the running app's version.
func SnapshotArchive(snap state.Snapshot, kind backup.Kind, at time.Time, restoreCreated []string) ([]byte, error) {
	return snapshotArchiveAs(snap, kind, at, restoreCreated, "")
}

// snapshotArchiveAs is SnapshotArchive naming appVersion ("" for the running
// app's, backup.Meta.AppVersion).
func snapshotArchiveAs(snap state.Snapshot, kind backup.Kind, at time.Time, restoreCreated []string, appVersion string) ([]byte, error) {
	comps := make([]backup.CompetitionRef, 0, len(snap.Competitions))
	for id, name := range snap.Competitions {
		comps = append(comps, backup.CompetitionRef{ID: id, Name: name})
	}
	sort.Slice(comps, func(i, j int) bool { return comps[i].ID < comps[j].ID })
	meta := backup.Meta{
		AppVersion:     appVersion,
		CreatedAt:      at.UTC(),
		Scope:          backup.Scope(snap.Scope),
		Kind:           kind,
		Tournament:     backup.TournamentRef{Name: snap.TournamentName},
		Competitions:   comps,
		Fingerprint:    state.Fingerprint(snap.Files),
		RestoreCreated: restoreCreated,
	}
	var buf bytes.Buffer
	if err := backup.Write(&buf, meta, snap.Files); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// SafetyCopySaver is what RestoreBeforeSwap needs to save a backup file.
type SafetyCopySaver interface {
	SaveBackupFile(kind state.BackupKind, scope state.SnapshotScope, at time.Time, data []byte) (string, error)
}

// RestoreBeforeSwap creates a BeforeSwap hook that saves a before-restore safety copy
// and records its saved file name into *savedName (if savedName is non-nil).
func RestoreBeforeSwap(saver SafetyCopySaver, at time.Time, savedName *string) func(state.RestoreSafetyCopy) error {
	return func(sc state.RestoreSafetyCopy) error {
		data, err := SnapshotArchive(sc.Snapshot, sc.Kind, at, sc.RestoreCreated)
		if err != nil {
			return err
		}
		name, err := saver.SaveBackupFile(state.BackupKindBeforeRestore, sc.Snapshot.Scope, at, data)
		if err != nil {
			return err
		}
		if savedName != nil {
			*savedName = name
		}
		return nil
	}
}

// backupDownloadName is `<tournament slug>[-<competition id>]-<UTC stamp>.zip`.
func backupDownloadName(tournamentName, compID string, at time.Time) string {
	slug := slugifyID(tournamentName)
	if slug == "" {
		slug = "tournament"
	}
	if compID != "" {
		slug += "-" + compID
	}
	return slug + "-" + at.UTC().Format(state.BackupStampLayout) + ".zip"
}

func sendBackupZip(c *gin.Context, name string, data []byte) {
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
	c.Data(http.StatusOK, "application/zip", data)
}

// respondSnapshotError answers a snapshot the store could not take.
func respondSnapshotError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, state.ErrCompetitionNotFound):
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "competition not found"})
	case errors.Is(err, state.ErrPendingWALUnsettled):
		respondBackupRefusal(c, http.StatusConflict, state.RestoreCodePendingWriteUnsettled,
			"A competition has a write that could not be finished. Restart the app, then take the backup again.", nil)
	default:
		slog.Error("mobile-app: backup snapshot failed", "err", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "The backup could not be made. Check the server log, then try again."})
	}
}

// RegisterBackupHandlers wires the backup downloads and the backups/ list,
// download and delete on the 1 MB admin group, after the confirm middleware.
func RegisterBackupHandlers(r *gin.RouterGroup, store BackupStore) {
	r.GET("/tournament/backup", func(c *gin.Context) {
		snap, err := store.Snapshot(state.ScopeTournament, "")
		if err != nil {
			respondSnapshotError(c, err)
			return
		}
		now := time.Now().UTC()
		data, err := SnapshotArchive(snap, backup.KindManual, now, nil)
		if err != nil {
			respondSnapshotError(c, err)
			return
		}
		sendBackupZip(c, backupDownloadName(snap.TournamentName, "", now), data)
	})

	r.GET("/competitions/:id/backup", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		snap, err := store.Snapshot(state.ScopeCompetition, id)
		if err != nil {
			respondSnapshotError(c, err)
			return
		}
		now := time.Now().UTC()
		data, err := SnapshotArchive(snap, backup.KindManual, now, nil)
		if err != nil {
			respondSnapshotError(c, err)
			return
		}
		// A competition snapshot carries no tournament; the file is still
		// named after it, so the organiser can tell whose backup it is.
		tournamentName := snap.TournamentName
		if tournamentName == "" {
			t, err := store.LoadTournament()
			if err != nil {
				respondSnapshotError(c, err)
				return
			}
			if t != nil {
				tournamentName = t.Name
			}
		}
		sendBackupZip(c, backupDownloadName(tournamentName, id, now), data)
	})

	r.GET("/tournament/backups", func(c *gin.Context) {
		files, err := store.ListBackupFiles()
		if err != nil {
			slog.Error("mobile-app: list backups failed", "err", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "The backups could not be listed. Check the server log."})
			return
		}
		out := make([]gin.H, 0, len(files))
		for _, f := range files {
			out = append(out, gin.H{
				"name":  f.Name,
				"kind":  f.Kind,
				"scope": f.Scope,
				"time":  f.Time.UTC().Format(time.RFC3339),
				"size":  f.Size,
			})
		}
		c.JSON(http.StatusOK, gin.H{"backups": out})
	})

	r.GET("/tournament/backups/:name", func(c *gin.Context) {
		name, ok := requireBackupName(c, c.Param("name"))
		if !ok {
			return
		}
		f, err := store.OpenBackupFile(name)
		if err != nil {
			respondBackupFileError(c, err)
			return
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				slog.Warn("mobile-app: close backup file failed", "file", name, "err", cerr)
			}
		}()
		info, err := f.Stat()
		if err != nil {
			respondBackupFileError(c, err)
			return
		}
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		c.DataFromReader(http.StatusOK, info.Size(), "application/zip", f, nil)
	})

	r.DELETE("/tournament/backups/:name", func(c *gin.Context) {
		name, ok := requireBackupName(c, c.Param("name"))
		if !ok {
			return
		}
		if err := store.DeleteBackupFile(name); err != nil {
			respondBackupFileError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"deleted": name})
	})
}

// requireBackupName checks a backup name against the one shape the store
// writes before any path is built from it.
func requireBackupName(c *gin.Context, name string) (string, bool) {
	if !state.ValidBackupName(name) {
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadBackupName,
			"That is not the name of a backup this app keeps.", nil)
		return "", false
	}
	return name, true
}

func respondBackupFileError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, state.ErrBackupNotFound):
		respondBackupRefusal(c, http.StatusNotFound, backupCodeBackupNotFound,
			"That backup is not there any more. Reload the list.", nil)
	case errors.Is(err, state.ErrInvalidBackupName):
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadBackupName,
			"That is not the name of a backup this app keeps.", nil)
	default:
		slog.Error("mobile-app: backup file access failed", "err", err)
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "The backup could not be read. Check the server log."})
	}
}

// RegisterRestoreHandlers wires the restore's plan and the restore on the
// 64 MB admin group, after the confirm middleware (which asks only the
// restore for the password again; the plan needs the sign-in alone).
func RegisterRestoreHandlers(r *gin.RouterGroup, store BackupStore, eng RestoreEngine, hub Broadcaster, verifier PasswordVerifier) {
	r.POST("/tournament/restore/plan", func(c *gin.Context) {
		req, ok := readRestoreRequest(c, store, verifier)
		if !ok {
			return
		}
		plan, err := store.PlanRestore(req)
		if err != nil {
			respondRestoreError(c, err)
			return
		}
		c.JSON(http.StatusOK, plan)
	})

	r.POST("/tournament/restore", func(c *gin.Context) {
		req, ok := readRestoreRequest(c, store, verifier)
		if !ok {
			return
		}
		allowRunning, ok := readFormBool(c, "allowRunning")
		if !ok {
			return
		}
		req.AllowRunning = allowRunning

		now := time.Now().UTC()
		var safetyCopy string
		// Runs under the restore's locks: SaveBackupFile takes only its own
		// backups mutex, never a store lock.
		req.BeforeSwap = RestoreBeforeSwap(store, now, &safetyCopy)

		plan, err := store.Restore(req)
		if err != nil {
			respondRestoreError(c, err)
			return
		}

		// Step 9, after the store has returned and released its locks.
		resp := gin.H{"plan": plan, "safetyCopy": safetyCopy}
		warnings, perr := ResolveRestoredPrefixes(store, eng)
		if perr != nil {
			slog.Error("mobile-app: restored competitions' number prefixes not reassigned; retried at the next start", "err", perr)
			resp["prefixError"] = "The restore is done, but the number prefixes of the restored competitions could not be checked. Restart the app to finish it."
		}
		if warnings == nil {
			warnings = []PrefixWarning{}
		}
		resp["prefixWarnings"] = warnings

		if rerr := ResolveRestoredRequeues(store, eng); rerr != nil {
			slog.Error("mobile-app: restored running matches not returned to queue; retried at the next start", "err", rerr)
			resp["requeueError"] = "The restore is done, but running matches could not be returned to the queue. Restart the app to finish it."
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, resp)
	})
}

// readRestoreRequest reads the fields the plan and the restore share: the
// archive (an uploaded `archive` file, or `from`, a copy in backups/), `only`,
// `undo` and `decisions` (the plan applies them too, so a copy name already
// used is found before the restore asks for the password). On a refusal it has
// answered and returns false.
func readRestoreRequest(c *gin.Context, store BackupStore, verifier PasswordVerifier) (state.RestoreRequest, bool) {
	var req state.RestoreRequest
	if err := c.Request.ParseMultipartForm(MaxImportBodyBytes); err != nil {
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
			"The backup file could not be read from the upload. Choose the file again.", nil)
		return req, false
	}
	archive, ok := readRestoreArchive(c, store)
	if !ok {
		return req, false
	}
	req.Archive = archive

	if only := c.PostForm("only"); only != "" {
		if err := state.ValidateCompetitionID(only); err != nil {
			respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
				"That is not a competition id.", nil)
			return req, false
		}
		req.Only = only
	}
	undo, ok := readFormBool(c, "undo")
	if !ok {
		return req, false
	}
	req.Undo = undo
	decisions, ok := readRestoreDecisions(c)
	if !ok {
		return req, false
	}
	req.Decisions = decisions
	req.LockedMode = verifier.Mode() == "locked"
	return req, true
}

// runningAppVersion is the version a restore is judged against; a test sets
// it to restore an earlier version's backup whatever the test binary's own
// version is (a branch name, which has no release order).
var runningAppVersion = version.GetVersion

func readRestoreArchive(c *gin.Context, store BackupStore) (*backup.Archive, bool) {
	from := c.PostForm("from")
	fh, fileErr := c.FormFile("archive")
	hasFile := fileErr == nil
	switch {
	case from != "" && hasFile:
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
			"Send a backup file or pick a kept copy, not both.", nil)
		return nil, false
	case from == "" && !hasFile:
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
			"Choose a backup file to restore.", nil)
		return nil, false
	}

	var (
		src  io.ReaderAt
		size int64
	)
	if from != "" {
		name, ok := requireBackupName(c, from)
		if !ok {
			return nil, false
		}
		f, err := store.OpenBackupFile(name)
		if err != nil {
			respondBackupFileError(c, err)
			return nil, false
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				slog.Warn("mobile-app: close backup file failed", "file", name, "err", cerr)
			}
		}()
		info, err := f.Stat()
		if err != nil {
			respondBackupFileError(c, err)
			return nil, false
		}
		src, size = f, info.Size()
	} else {
		f, err := fh.Open()
		if err != nil {
			respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
				"The backup file could not be read from the upload. Choose the file again.", nil)
			return nil, false
		}
		defer func() {
			if cerr := f.Close(); cerr != nil {
				slog.Warn("mobile-app: close uploaded backup failed", "err", cerr)
			}
		}()
		src, size = f, fh.Size
	}

	archive, err := backup.Read(src, size, backupReadChecks)
	if err != nil {
		reason := ""
		var be *backup.Error
		if errors.As(err, &be) {
			reason = be.Code
		}
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeArchiveInvalid,
			"This file is not a backup this app can restore, so nothing was changed.",
			gin.H{"reason": reason, "detail": err.Error()})
		return nil, false
	}
	// A backup is restored only on the version that made it or a later one;
	// the plan is refused too, so the page says so before any choice is made.
	running := runningAppVersion()
	if verr := backup.CheckAppVersion(archive.Manifest, running); verr != nil {
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeVersionMismatch, verr.Detail,
			gin.H{"backupVersion": strings.TrimSpace(archive.Manifest.AppVersion), "appVersion": strings.TrimSpace(running)})
		return nil, false
	}
	return archive, true
}

// readRestoreDecisions reads `decisions`, a JSON object {id: {action, copyName}}.
func readRestoreDecisions(c *gin.Context) (map[string]state.RestoreDecision, bool) {
	raw := c.PostForm("decisions")
	if raw == "" {
		return nil, true
	}
	var decisions map[string]state.RestoreDecision
	if err := json.Unmarshal([]byte(raw), &decisions); err != nil {
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
			"The choices for the competitions could not be read. Check them again.", nil)
		return nil, false
	}
	return decisions, true
}

func readFormBool(c *gin.Context, field string) (bool, bool) {
	raw := c.PostForm(field)
	if raw == "" {
		return false, true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		respondBackupRefusal(c, http.StatusBadRequest, backupCodeBadRequest,
			fmt.Sprintf("%s must be true or false.", field), nil)
		return false, false
	}
	return v, true
}

// respondRestoreError answers a plan or a restore the store refused.
func respondRestoreError(c *gin.Context, err error) {
	var re *state.RestoreError
	if errors.As(err, &re) {
		status, ok := restoreRefusalStatus[re.Code]
		if !ok {
			status = http.StatusConflict
		}
		sentence, ok := restoreRefusalSentence[re.Code]
		if !ok {
			sentence = "The restore was refused, so nothing was changed."
		}
		// The wrapped error can name server paths, so it goes to the log
		// only; the answer carries the code, the sentence, the competitions
		// and the archive entry concerned.
		slog.Info("mobile-app: restore refused", "err", re)
		extra := gin.H{}
		if re.Path != "" {
			extra["path"] = re.Path
		}
		if len(re.CompetitionIDs) > 0 {
			extra["competitionIds"] = re.CompetitionIDs
		}
		respondBackupRefusal(c, status, re.Code, sentence, extra)
		return
	}
	if errors.Is(err, state.ErrPendingWALUnsettled) {
		respondBackupRefusal(c, http.StatusConflict, state.RestoreCodePendingWriteUnsettled,
			restoreRefusalSentence[state.RestoreCodePendingWriteUnsettled], nil)
		return
	}
	slog.Error("mobile-app: restore failed", "err", err)
	c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "The restore could not be completed. Check the server log; restarting the app finishes a restore that was under way.",
	})
}

// ResolveRestoredPrefixes is a restore's step 9: it gives each competition a
// restore left in prefixes-pending.json a free number prefix, then removes the
// file. The server runs it after every restore and once before it serves, so
// a restore a crash stopped before this step is finished at the next start.
// The file stays when a reassignment fails, so the next start tries again.
func ResolveRestoredPrefixes(store PendingPrefixStore, eng PrefixEngine) ([]PrefixWarning, error) {
	ids, err := store.PrefixesPending()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	warnings, err := ReassignRestoredPrefixes(store, eng, ids)
	if err != nil {
		return warnings, err
	}
	if err := store.RemovePrefixesPending(ids); err != nil {
		return warnings, err
	}
	return warnings, nil
}

// ResolveRestoredRequeues sends every match a restore brought back running
// back to the queue with its score kept, then removes the entry or file. The server
// runs it after every restore and once before it serves, so a restore a crash
// stopped before this step is finished at the next start.
func ResolveRestoredRequeues(store PendingRequeueStore, eng RequeueEngine) error {
	pending, err := store.RequeuePending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	compIDs := make([]string, 0, len(pending))
	for compID := range pending {
		compIDs = append(compIDs, compID)
	}
	slices.Sort(compIDs)

	for _, compID := range compIDs {
		matchIDs := pending[compID]
		var handled []string
		for _, matchID := range matchIDs {
			err := eng.RevertMatchToQueue(compID, matchID)
			if err != nil {
				var nfe *engine.NotFoundError
				if errors.Is(err, engine.ErrMatchAlreadyCompleted) ||
					errors.As(err, &nfe) ||
					errors.Is(err, state.ErrCompetitionNotFound) ||
					errors.Is(err, fs.ErrNotExist) {
					handled = append(handled, matchID)
					continue
				}
				if len(handled) > 0 {
					if rerr := store.RemoveRequeuePending(compID, handled); rerr != nil {
						slog.Warn("mobile-app: could not remove handled requeue entries", "competition", compID, "err", rerr)
					}
				}
				return err
			}
			handled = append(handled, matchID)
		}
		if len(handled) > 0 {
			if err := store.RemoveRequeuePending(compID, handled); err != nil {
				return err
			}
		}
	}
	return nil
}

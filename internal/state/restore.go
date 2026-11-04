package state

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"gopkg.in/yaml.v3"
)

// RestoreAction is what a restore does with one competition.
type RestoreAction string

const (
	// RestoreCreate puts an archive competition whose id the folder lacks in place.
	RestoreCreate RestoreAction = "create"
	// RestoreReplace puts an archive competition in place of the folder's one with its id.
	RestoreReplace RestoreAction = "replace"
	// RestoreCopy puts an archive competition in place under a new name and id,
	// leaving the folder's one with its id as it is.
	RestoreCopy RestoreAction = "copy"
	// RestoreKeep leaves a folder competition the tournament archive lacks as it is.
	RestoreKeep RestoreAction = "keep"
	// RestoreRemove removes a folder competition the tournament archive lacks.
	RestoreRemove RestoreAction = "remove"
)

// RestoreDecision is the operator's answer for one competition the plan asks
// about. CopyName is read only for RestoreCopy.
type RestoreDecision struct {
	Action   RestoreAction `json:"action"`
	CopyName string        `json:"copyName,omitempty"`
}

// RestoreSafetyCopy is what Restore hands its BeforeSwap hook: the folder's
// files the restore is about to replace or remove (the tournament files too,
// for a tournament restore), read under the restore's locks, with the manifest
// kind and the ids the restore creates (a create or a copy), which an Undo
// removes.
type RestoreSafetyCopy struct {
	Snapshot       Snapshot
	Kind           backup.Kind
	RestoreCreated []string
}

// RestoreRequest is one restore, or its plan (PlanRestore).
type RestoreRequest struct {
	// Archive is the validated archive (backup.Read).
	Archive *backup.Archive
	// Only restores one competition out of a tournament archive, with
	// competition scope (D8).
	Only string
	// Decisions answers each competition the plan asks about, by its id in the
	// archive (replace or copy) or in the folder (keep or remove).
	Decisions map[string]RestoreDecision
	// Undo restores a before-restore safety copy as the undo of the restore
	// that took it: it answers its own questions.
	Undo bool
	// AllowRunning lets a replace or a remove go ahead over a competition with
	// a match running now.
	AllowRunning bool
	// LockedMode is set when the password lives in the environment, so the
	// tournament.md one is never asked for.
	LockedMode bool
	// NewPassword, when set (the CLI's --password), is written into the
	// restored tournament.md; otherwise the current password is kept.
	NewPassword string
	// BeforeSwap, when set, runs under the restore's locks just before the
	// first live rename; the caller writes the safety copy through it. An
	// error stops the restore with nothing changed.
	BeforeSwap func(RestoreSafetyCopy) error
}

// Refusal codes a RestoreError carries.
const (
	RestoreCodeDecisionMissing       = "decision_missing"
	RestoreCodeBadDecision           = "bad_decision"
	RestoreCodeCopyNameTaken         = "copy_name_taken"
	RestoreCodePlanChanged           = "plan_changed"
	RestoreCodeMatchesRunning        = "matches_running"
	RestoreCodePasswordRequired      = "password_required"
	RestoreCodeFileUnreadable        = "file_unreadable"
	RestoreCodeNotASafetyCopy        = "not_a_safety_copy"
	RestoreCodePendingWriteUnsettled = "pending_write_unsettled"
	RestoreCodeNotInArchive          = "competition_not_in_archive"
	RestoreCodeNothingToRestore      = "nothing_to_restore"
)

// RestoreError is a restore (or plan) refused before any live path changed.
type RestoreError struct {
	Code           string
	CompetitionIDs []string
	Path           string
	Err            error
}

func (e *RestoreError) Error() string {
	msg := "restore refused: " + e.Code
	if len(e.CompetitionIDs) > 0 {
		msg += " (" + strings.Join(e.CompetitionIDs, ", ") + ")"
	}
	if e.Path != "" {
		msg += ": " + e.Path
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *RestoreError) Unwrap() error { return e.Err }

// What happens to the admin password (D1).
const (
	RestorePasswordKept     = "kept"
	RestorePasswordNeedsOne = "needs one"
)

// Warning codes a plan carries; none of them refuses the restore.
const (
	RestoreWarnMatchesRunning   = "matches_running"
	RestoreWarnCourtBusy        = "court_busy"
	RestoreWarnCourtMissing     = "court_missing"
	RestoreWarnModeChanges      = "mode_changes"
	RestoreWarnPrefixReassigned = "prefix_reassigned"
)

// RestoreWarning is one thing the operator is told before a restore runs.
type RestoreWarning struct {
	Code          string `json:"code"`
	CompetitionID string `json:"competitionId,omitempty"`
	Court         string `json:"court,omitempty"`
	Message       string `json:"message"`
}

// RestoreCompetitionPlan is what the restore does with one competition.
type RestoreCompetitionPlan struct {
	// ID is the competition's id in the archive, or in the folder for a keep
	// or a remove.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Action is the answer given (or the one the restore takes by itself);
	// empty while an asked competition is unanswered.
	Action RestoreAction `json:"action,omitempty"`
	// Asked is set when the operator must choose; Choices lists the answers.
	Asked   bool            `json:"asked"`
	Choices []RestoreAction `json:"choices,omitempty"`
	// NewID is a copy's id; CopyNameTaken is set when its name is empty, does
	// not make an id, or makes one already used.
	NewID         string `json:"newId,omitempty"`
	CopyNameTaken bool   `json:"copyNameTaken,omitempty"`
	// RunningNow lists the folder's matches running now (replace, remove);
	// RunningInArchive the archive's matches that come back running.
	RunningNow       []string `json:"runningNow,omitempty"`
	RunningInArchive []string `json:"runningInArchive,omitempty"`
	// ResultsChanged lists the folder's finished or running matches whose
	// result the restore loses or changes (replace, remove).
	ResultsChanged []string `json:"resultsChanged,omitempty"`
	// PrefixCollides is set when the restored competition's number prefix is
	// used by one the restore leaves in place: it gets a new prefix and its
	// tags must be reprinted.
	PrefixCollides bool `json:"prefixCollides,omitempty"`
}

// RestorePlan is what a restore will do (PlanRestore) or did (Restore).
type RestorePlan struct {
	Scope SnapshotScope `json:"scope"`
	Kind  backup.Kind   `json:"kind"`
	Undo  bool          `json:"undo"`
	// NothingToRestore is set, with Message, when the archive holds nothing to
	// put back (Q1: a before-restore copy of a restore that only added a
	// competition). Such a plan cannot run.
	NothingToRestore bool   `json:"nothingToRestore,omitempty"`
	Message          string `json:"message,omitempty"`
	// TournamentReplaced is set for a tournament restore, which replaces the
	// tournament settings, branding and sponsors.
	TournamentReplaced bool                     `json:"tournamentReplaced"`
	Password           string                   `json:"password"`
	Competitions       []RestoreCompetitionPlan `json:"competitions"`
	// Missing lists the asked competitions with no answer yet.
	Missing []string `json:"missing,omitempty"`
	// PrefixesPending lists the ids (as restored) whose prefix collides.
	PrefixesPending []string         `json:"prefixesPending,omitempty"`
	Warnings        []RestoreWarning `json:"warnings,omitempty"`
}

const nothingToRestoreMessage = "This copy was taken before a restore that only added a competition. " +
	"To take that competition out again, delete it from its settings."

const nothingToUndoMessage = "The competition the restore added is already gone, so there is nothing to undo."

// restoreMatch is the part of one match a plan reads.
type restoreMatch struct {
	court    string
	running  bool
	recorded bool // running or completed
	result   string
}

// restoreResultKey is what makes a match's recorded result: its status,
// winner, decision and score. A change to any of them is a result the
// restore loses or changes.
type restoreResultKey struct {
	Status     MatchStatus      `json:"status"`
	Winner     string           `json:"winner"`
	WinnerID   string           `json:"winnerId"`
	Decision   string           `json:"decision"`
	IpponsA    []string         `json:"ipponsA"`
	IpponsB    []string         `json:"ipponsB"`
	HansokuA   int              `json:"hansokuA"`
	HansokuB   int              `json:"hansokuB"`
	FlagsA     int              `json:"flagsA"`
	FlagsB     int              `json:"flagsB"`
	ScoreA     string           `json:"scoreA,omitempty"`
	ScoreB     string           `json:"scoreB,omitempty"`
	Encho      *EnchoMetadata   `json:"encho,omitempty"`
	SubResults []SubMatchResult `json:"subResults,omitempty"`
}

func matchesFromBytes(poolRaw, bracketRaw []byte) (map[string]restoreMatch, error) {
	out := map[string]restoreMatch{}
	pool, err := parsePoolMatchesBytes(poolRaw)
	if err != nil {
		return nil, err
	}
	for i := range pool {
		m := &pool[i]
		key, err := json.Marshal(restoreResultKey{
			Status: m.Status, Winner: m.Winner, WinnerID: m.WinnerID, Decision: m.Decision,
			IpponsA: m.IpponsA, IpponsB: m.IpponsB, HansokuA: m.HansokuA, HansokuB: m.HansokuB,
			FlagsA: m.FlagsA, FlagsB: m.FlagsB, Encho: m.Encho, SubResults: m.SubResults,
		})
		if err != nil {
			return nil, err
		}
		out[m.ID] = restoreMatch{
			court:    m.Court,
			running:  m.Status == MatchStatusRunning,
			recorded: m.Status == MatchStatusRunning || m.Status == MatchStatusCompleted,
			result:   string(key),
		}
	}
	br, err := parseBracketBytes(bracketRaw)
	if err != nil {
		return nil, err
	}
	for _, round := range br.Rounds {
		for i := range round {
			m := &round[i]
			key, err := json.Marshal(restoreResultKey{
				Status: m.Status, Winner: m.Winner, WinnerID: m.WinnerID, Decision: m.Decision,
				IpponsA: m.IpponsA, IpponsB: m.IpponsB, HansokuA: m.HansokuA, HansokuB: m.HansokuB,
				FlagsA: m.FlagsA, FlagsB: m.FlagsB, ScoreA: m.ScoreA, ScoreB: m.ScoreB,
				Encho: m.Encho, SubResults: m.SubResults,
			})
			if err != nil {
				return nil, err
			}
			out[m.ID] = restoreMatch{
				court:    m.Court,
				running:  m.Status == MatchStatusRunning,
				recorded: m.Status == MatchStatusRunning || m.Status == MatchStatusCompleted,
				result:   string(key),
			}
		}
	}
	return out, nil
}

func runningMatchIDs(ms map[string]restoreMatch) []string {
	var ids []string
	for id, m := range ms {
		if m.running {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// resultsChanged lists the matches finished or running in the folder whose
// result the archive changes or lacks.
func resultsChanged(live, archive map[string]restoreMatch) []string {
	var ids []string
	for id, m := range live {
		if !m.recorded {
			continue
		}
		if a, ok := archive[id]; !ok || a.result != m.result {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids
}

// restoreArchiveInfo is what a plan reads from the archive, parsed once.
type restoreArchiveInfo struct {
	ids         []string // every competition the manifest names, sorted
	names       map[string]string
	comps       map[string]*Competition
	matches     map[string]map[string]restoreMatch
	tourn       *Tournament
	hasBranding bool
	hasSponsors bool
}

// parseRestoreArchive parses every data file the store knows in the
// competitions in scope (all of them, or only), from the archive's bytes,
// with the store's own parsers; a file that does not parse refuses the
// restore as file_unreadable before anything is staged. A quarantined
// *.corrupt-* file and any name the store does not know are carried as bytes
// and never parsed. participants.csv, seeds.csv, pools.csv and overrides.json
// have no byte parser yet, so they are carried unparsed too.
func parseRestoreArchive(a *backup.Archive, only string) (*restoreArchiveInfo, error) {
	if a == nil {
		return nil, errors.New("restore: no archive")
	}
	ai := &restoreArchiveInfo{
		names:   map[string]string{},
		comps:   map[string]*Competition{},
		matches: map[string]map[string]restoreMatch{},
	}
	for _, c := range a.Manifest.Competitions {
		ai.ids = append(ai.ids, c.ID)
		ai.names[c.ID] = c.Name
	}
	slices.Sort(ai.ids)
	paths := make([]string, 0, len(a.Files))
	for p := range a.Files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	pool := map[string][]byte{}
	bracket := map[string][]byte{}
	unreadable := func(p, id string, err error) error {
		var ids []string
		if id != "" {
			ids = []string{id}
		}
		return &RestoreError{Code: RestoreCodeFileUnreadable, Path: p, CompetitionIDs: ids, Err: err}
	}
	for _, p := range paths {
		data := a.Files[p]
		switch {
		case p == "tournament.md":
			var t Tournament
			if err := parseFrontMatter(data, &t); err != nil {
				return nil, unreadable(p, "", err)
			}
			ai.tourn = &t
		case strings.HasPrefix(p, BrandingDirName+"/"):
			ai.hasBranding = true
		case strings.HasPrefix(p, SponsorsDirName+"/"):
			ai.hasSponsors = true
		case strings.HasPrefix(p, "competitions/"):
			id, rel, ok := strings.Cut(strings.TrimPrefix(p, "competitions/"), "/")
			if !ok || (only != "" && id != only) {
				continue
			}
			var err error
			switch rel {
			case "config.md":
				var c *Competition
				c, err = parseCompetitionBytes(data)
				ai.comps[id] = c
			case "pool-matches.csv":
				_, err = parsePoolMatchesBytes(data)
				pool[id] = data
			case "bracket.json":
				_, err = parseBracketBytes(data)
				bracket[id] = data
			case competitorStatusFilename:
				_, err = parseCompetitorStatusBytes(data)
			case teamLineupFilename:
				_, err = decodeTeamLineupsBytes(data)
			case teamMembersFilename:
				_, err = parseSquadsBytes(data)
			case legacySquadsFilename:
				_, err = parseLegacySquadsBytes(data)
			default:
				if strings.HasPrefix(rel, scoreLogDir+"/") && strings.HasSuffix(rel, ".jsonl") {
					_, err = parseScoreLog(p, data)
				}
			}
			if err != nil {
				return nil, unreadable(p, id, err)
			}
		}
	}
	for _, id := range ai.ids {
		if only != "" && id != only {
			continue
		}
		ms, err := matchesFromBytes(pool[id], bracket[id])
		if err != nil {
			return nil, unreadable("competitions/"+id, id, err)
		}
		ai.matches[id] = ms
	}
	return ai, nil
}

// restoreLiveState is what a plan reads from the folder.
type restoreLiveState struct {
	ids           []string // every valid competition id under competitions/, sorted
	comps         map[string]*Competition
	matches       map[string]map[string]restoreMatch
	tourn         *Tournament
	hasTournament bool
	hasBranding   bool
	hasSponsors   bool
}

// readRestoreLiveStateLocked reads the folder through s.root only, taking no
// lock of its own: PlanRestore calls it under s.mu's read lock, Restore under
// the write locks it holds. A live file that does not parse is read as empty:
// a broken competition must stay restorable over.
func (s *Store) readRestoreLiveStateLocked() (*restoreLiveState, error) {
	all, err := s.listCompetitionsLocked()
	if err != nil {
		return nil, err
	}
	live := &restoreLiveState{
		comps:   map[string]*Competition{},
		matches: map[string]map[string]restoreMatch{},
	}
	for _, id := range all {
		if ValidateCompetitionID(id) != nil {
			continue
		}
		live.ids = append(live.ids, id)
		if raw, rerr := s.root.readFile(s.compPath(id, "config.md")); rerr == nil {
			if c, perr := parseCompetitionBytes(raw); perr == nil {
				live.comps[id] = c
			}
		}
		poolRaw, perr := s.readOptional(s.compPath(id, "pool-matches.csv"))
		if perr != nil {
			return nil, perr
		}
		bracketRaw, berr := s.readOptional(s.compPath(id, "bracket.json"))
		if berr != nil {
			return nil, berr
		}
		ms, merr := matchesFromBytes(poolRaw, bracketRaw)
		if merr != nil {
			slog.Warn("state: restore plan reads a live competition's matches as empty", "competition", id, "err", merr)
			ms = map[string]restoreMatch{}
		}
		live.matches[id] = ms
	}
	slices.Sort(live.ids)
	raw, err := s.readOptional(filepath.Join(s.folder, "tournament.md"))
	if err != nil {
		return nil, err
	}
	if raw != nil {
		live.hasTournament = true
		var t Tournament
		if perr := parseFrontMatter(raw, &t); perr == nil {
			live.tourn = &t
		}
	}
	if live.hasBranding, err = s.root.exists(filepath.Join(s.folder, BrandingDirName)); err != nil {
		return nil, err
	}
	if live.hasSponsors, err = s.root.exists(filepath.Join(s.folder, SponsorsDirName)); err != nil {
		return nil, err
	}
	return live, nil
}

// readOptional reads path, answering nil for a file that is not there.
func (s *Store) readOptional(p string) ([]byte, error) {
	raw, err := s.root.readFile(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return raw, err
}

// restoreTarget is one archive competition the restore puts in place.
type restoreTarget struct {
	srcID  string
	dstID  string
	action RestoreAction
	name   string // a copy's new name
}

// restoreWork is a computed plan plus what the swap needs.
type restoreWork struct {
	plan        RestorePlan
	tournament  bool
	targets     []restoreTarget
	replaced    []string
	removed     []string
	created     []string // the ids a create or a copy brings in
	copyTaken   []string
	runningComp []string
}

func competitionName(live *restoreLiveState, id string) string {
	if c := live.comps[id]; c != nil && c.Name != "" {
		return c.Name
	}
	return id
}

// planRestore computes the plan from the archive and the folder. It writes
// nothing and takes no lock.
func planRestore(req RestoreRequest, ai *restoreArchiveInfo, live *restoreLiveState) (*restoreWork, error) {
	man := req.Archive.Manifest
	if req.Undo && man.Kind != backup.KindBeforeRestore {
		return nil, &RestoreError{Code: RestoreCodeNotASafetyCopy}
	}
	archiveIDs := ai.ids
	tournament := man.Scope == backup.ScopeTournament
	if req.Only != "" {
		if req.Undo {
			return nil, &RestoreError{Code: RestoreCodeBadDecision, Err: errors.New("an Undo puts back the whole safety copy")}
		}
		if !slices.Contains(ai.ids, req.Only) {
			return nil, &RestoreError{Code: RestoreCodeNotInArchive, CompetitionIDs: []string{req.Only}}
		}
		archiveIDs = []string{req.Only}
		tournament = false
	}
	if req.Undo && len(req.Decisions) > 0 {
		return nil, &RestoreError{Code: RestoreCodeBadDecision, Err: errors.New("an Undo answers its own questions")}
	}

	w := &restoreWork{tournament: tournament}
	w.plan = RestorePlan{Scope: ScopeCompetition, Kind: man.Kind, Undo: req.Undo, TournamentReplaced: tournament, Password: RestorePasswordKept}
	if tournament {
		w.plan.Scope = ScopeTournament
	}
	inFolder := map[string]bool{}
	for _, id := range live.ids {
		inFolder[id] = true
	}
	inArchive := map[string]bool{}
	for _, id := range ai.ids {
		inArchive[id] = true
	}
	created := map[string]bool{}
	if req.Undo && man.RestoreCreated != nil {
		for _, id := range *man.RestoreCreated {
			created[id] = true
		}
	}

	// Q1: restored without undo, a before-restore copy that holds no
	// competition has nothing to put back.
	if !req.Undo && man.Kind == backup.KindBeforeRestore && !tournament && len(archiveIDs) == 0 {
		w.plan.NothingToRestore = true
		w.plan.Message = nothingToRestoreMessage
		w.plan.Competitions = []RestoreCompetitionPlan{}
		return w, nil
	}

	var entries []RestoreCompetitionPlan
	for _, id := range archiveIDs {
		e := RestoreCompetitionPlan{ID: id, Name: ai.names[id]}
		switch {
		case !inFolder[id]:
			e.Action = RestoreCreate
		case req.Undo:
			e.Action = RestoreReplace
		default:
			e.Asked = true
			e.Choices = []RestoreAction{RestoreReplace, RestoreCopy}
		}
		entries = append(entries, e)
	}
	for _, id := range live.ids {
		if inArchive[id] {
			continue
		}
		e := RestoreCompetitionPlan{ID: id, Name: competitionName(live, id)}
		switch {
		case req.Undo && created[id]:
			e.Action = RestoreRemove
		case req.Undo && tournament:
			e.Action = RestoreKeep
		case tournament:
			e.Asked = true
			e.Choices = []RestoreAction{RestoreKeep, RestoreRemove}
		default:
			continue
		}
		entries = append(entries, e)
	}

	// Answers.
	copyNames := map[string]string{}
	asked := map[string]bool{}
	for i := range entries {
		e := &entries[i]
		if !e.Asked {
			continue
		}
		asked[e.ID] = true
		d, ok := req.Decisions[e.ID]
		if !ok {
			w.plan.Missing = append(w.plan.Missing, e.ID)
			continue
		}
		if !slices.Contains(e.Choices, d.Action) {
			return nil, &RestoreError{Code: RestoreCodeBadDecision, CompetitionIDs: []string{e.ID},
				Err: fmt.Errorf("%q is not one of %v", d.Action, e.Choices)}
		}
		e.Action = d.Action
		if d.Action == RestoreCopy {
			copyNames[e.ID] = strings.TrimSpace(d.CopyName)
		}
	}
	var unknown []string
	for id := range req.Decisions {
		if !asked[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, &RestoreError{Code: RestoreCodeBadDecision, CompetitionIDs: unknown,
			Err: errors.New("no question about these competitions")}
	}

	// A copy's id and name must differ from every competition in the folder,
	// every create and every other copy of this restore.
	taken := map[string]bool{}
	for _, id := range live.ids {
		taken[id] = true
	}
	for _, e := range entries {
		if e.Action == RestoreCreate {
			taken[e.ID] = true
		}
	}
	var takenNames []string
	for _, id := range live.ids {
		if name := competitionName(live, id); name != "" {
			takenNames = append(takenNames, name)
		}
	}
	for _, e := range entries {
		if e.Action == RestoreCreate {
			if name := ai.names[e.ID]; name != "" {
				takenNames = append(takenNames, name)
			}
		}
	}
	nameTaken := func(candidate string) bool {
		candidate = strings.TrimSpace(candidate)
		for _, ex := range takenNames {
			if strings.EqualFold(strings.TrimSpace(ex), candidate) {
				return true
			}
		}
		return false
	}
	for i := range entries {
		e := &entries[i]
		if e.Action != RestoreCopy {
			continue
		}
		name := copyNames[e.ID]
		newID := SlugifyID(name)
		if name == "" || ValidateCompetitionID(newID) != nil || taken[newID] || nameTaken(name) {
			e.CopyNameTaken = true
			w.copyTaken = append(w.copyTaken, e.ID)
			continue
		}
		e.NewID = newID
		taken[newID] = true
		takenNames = append(takenNames, name)
	}

	for _, e := range entries {
		switch e.Action {
		case RestoreCreate:
			w.targets = append(w.targets, restoreTarget{srcID: e.ID, dstID: e.ID, action: RestoreCreate})
			w.created = append(w.created, e.ID)
		case RestoreReplace:
			w.targets = append(w.targets, restoreTarget{srcID: e.ID, dstID: e.ID, action: RestoreReplace})
			w.replaced = append(w.replaced, e.ID)
		case RestoreCopy:
			if e.NewID != "" {
				w.targets = append(w.targets, restoreTarget{srcID: e.ID, dstID: e.NewID, action: RestoreCopy, name: copyNames[e.ID]})
				w.created = append(w.created, e.NewID)
			}
		case RestoreRemove:
			w.removed = append(w.removed, e.ID)
		}
	}
	slices.Sort(w.created)
	slices.Sort(w.replaced)
	slices.Sort(w.removed)

	// What stays in place: every folder competition not replaced or removed.
	leaving := map[string]bool{}
	for _, id := range w.replaced {
		leaving[id] = true
	}
	for _, id := range w.removed {
		leaving[id] = true
	}
	var inPlace []string
	for _, id := range live.ids {
		if !leaving[id] {
			inPlace = append(inPlace, id)
		}
	}
	busyCourts := map[string]bool{}
	for _, id := range inPlace {
		for _, m := range live.matches[id] {
			if m.running && m.court != "" {
				busyCourts[m.court] = true
			}
		}
	}

	for i := range entries {
		e := &entries[i]
		switch e.Action {
		case RestoreReplace, RestoreRemove:
			e.RunningNow = runningMatchIDs(live.matches[e.ID])
			if len(e.RunningNow) > 0 {
				w.runningComp = append(w.runningComp, e.ID)
				w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: RestoreWarnMatchesRunning, CompetitionID: e.ID,
					Message: fmt.Sprintf("%s has %d match(es) running now; they go back to the state the backup holds.", e.Name, len(e.RunningNow))})
			}
			var archived map[string]restoreMatch
			if e.Action == RestoreReplace {
				archived = ai.matches[e.ID]
			}
			e.ResultsChanged = resultsChanged(live.matches[e.ID], archived)
		}
		if e.Action == RestoreReplace || e.Action == RestoreCreate || (e.Action == RestoreCopy && e.NewID != "") {
			e.RunningInArchive = runningMatchIDs(ai.matches[e.ID])
		}
		if e.Action == RestoreCreate || (e.Action == RestoreCopy && e.NewID != "") {
			courts := map[string]bool{}
			for _, mid := range e.RunningInArchive {
				if c := ai.matches[e.ID][mid].court; busyCourts[c] {
					courts[c] = true
				}
			}
			for _, c := range sortedKeys(courts) {
				w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: RestoreWarnCourtBusy, CompetitionID: targetID(*e), Court: c,
					Message: fmt.Sprintf("%s comes back with a match running on shiaijo %s, where another competition runs a match now; send one of them back to the queue.", e.Name, c)})
			}
		}
	}

	if tournament && ai.tourn != nil {
		restored := *ai.tourn
		ApplyTournamentDefaults(&restored)
		if live.tourn != nil {
			current := *live.tourn
			ApplyTournamentDefaults(&current)
			if current.Mode != restored.Mode {
				w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: RestoreWarnModeChanges,
					Message: fmt.Sprintf("The tournament mode changes from %s to %s, which changes what the public page can do.", current.Mode, restored.Mode)})
			}
		}
		have := map[string]bool{}
		for _, c := range restored.Courts {
			have[c] = true
		}
		check := func(id, name string, c *Competition) {
			if c == nil {
				return
			}
			for _, court := range c.Courts {
				if !have[court] {
					w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: RestoreWarnCourtMissing, CompetitionID: id, Court: court,
						Message: fmt.Sprintf("%s uses shiaijo %s, which the restored tournament settings do not have; its matches there cannot be called until the shiaijo is added back or they are moved.", name, court)})
				}
			}
		}
		for _, t := range w.targets {
			check(t.dstID, ai.names[t.srcID], ai.comps[t.srcID])
		}
		for _, id := range inPlace {
			check(id, competitionName(live, id), live.comps[id])
		}
		if !req.LockedMode && (live.tourn == nil || live.tourn.Password == "") {
			w.plan.Password = RestorePasswordNeedsOne
		}
	}

	// Number prefixes: a restored competition whose prefix a competition left
	// in place (or an earlier restored one) already uses gets a new one.
	var usedPrefixes []string
	for _, id := range inPlace {
		if c := live.comps[id]; c != nil && c.EffectiveNumberPrefix() != "" {
			usedPrefixes = append(usedPrefixes, c.EffectiveNumberPrefix())
		}
	}
	collides := map[string]bool{}
	for _, t := range w.targets {
		c := ai.comps[t.srcID]
		if c == nil || c.EffectiveNumberPrefix() == "" {
			continue
		}
		prefix := c.EffectiveNumberPrefix()
		hit := false
		for _, u := range usedPrefixes {
			if strings.EqualFold(u, prefix) || helper.NumberPrefixesAmbiguous(u, prefix) {
				hit = true
				break
			}
		}
		if hit {
			collides[t.srcID] = true
			w.plan.PrefixesPending = append(w.plan.PrefixesPending, t.dstID)
		}
		usedPrefixes = append(usedPrefixes, prefix)
	}
	slices.Sort(w.plan.PrefixesPending)
	for i := range entries {
		e := &entries[i]
		if collides[e.ID] && (e.Action != RestoreCopy || e.NewID != "") {
			e.PrefixCollides = true
			w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: RestoreWarnPrefixReassigned, CompetitionID: targetID(*e),
				Message: fmt.Sprintf("%s's number prefix is used by another competition, so it gets a new one; reprint this competition's tags.", e.Name)})
		}
	}

	if req.Undo && len(w.targets) == 0 && len(w.removed) == 0 && !tournament {
		w.plan.NothingToRestore = true
		w.plan.Message = nothingToUndoMessage
	}
	w.plan.Competitions = entries
	if w.plan.Competitions == nil {
		w.plan.Competitions = []RestoreCompetitionPlan{}
	}
	return w, nil
}

func targetID(e RestoreCompetitionPlan) string {
	if e.NewID != "" {
		return e.NewID
	}
	return e.ID
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// refusal is the first precondition the plan fails for a restore that runs.
func (w *restoreWork) refusal(req RestoreRequest) error {
	switch {
	case w.plan.NothingToRestore:
		return &RestoreError{Code: RestoreCodeNothingToRestore}
	case len(w.plan.Missing) > 0:
		return &RestoreError{Code: RestoreCodeDecisionMissing, CompetitionIDs: slices.Clone(w.plan.Missing)}
	case len(w.copyTaken) > 0:
		return &RestoreError{Code: RestoreCodeCopyNameTaken, CompetitionIDs: slices.Clone(w.copyTaken)}
	case len(w.runningComp) > 0 && !req.AllowRunning:
		return &RestoreError{Code: RestoreCodeMatchesRunning, CompetitionIDs: slices.Clone(w.runningComp)}
	case w.plan.Password == RestorePasswordNeedsOne && req.NewPassword == "":
		return &RestoreError{Code: RestoreCodePasswordRequired}
	}
	return nil
}

// mergedPassword is the password the restored tournament.md carries: the
// one given (the CLI's --password), else the current one. Never the
// archive's (D1).
func mergedPassword(req RestoreRequest, live *restoreLiveState) string {
	if req.NewPassword != "" {
		return req.NewPassword
	}
	if live.tourn != nil {
		return live.tourn.Password
	}
	return ""
}

// PlanRestore is the restore's dry run: the same plan Restore computes, with
// nothing written and no staging folder made. An unanswered question is not
// refused here; it is listed in Missing.
func (s *Store) PlanRestore(req RestoreRequest) (RestorePlan, error) {
	ai, err := parseRestoreArchive(req.Archive, req.Only)
	if err != nil {
		return RestorePlan{}, err
	}
	s.mu.RLock()
	live, err := s.readRestoreLiveStateLocked()
	s.mu.RUnlock()
	if err != nil {
		return RestorePlan{}, err
	}
	w, err := planRestore(req, ai, live)
	if err != nil {
		return RestorePlan{}, err
	}
	return w.plan, nil
}

// Restore puts an archive in place (see the design in plan.md, Restore):
//
//  1. Check, with no lock held: the plan, then every refusal it carries.
//  2. Stage the files under .restore/<token>/new/ (a copy's config.md and
//     lineups.yaml carry its new id; the merged tournament.md carries the
//     current password or NewPassword), every file and folder fsynced. Any
//     failure from here to the journal removes the staging folder.
//  3. Lock, in the one order every arm takes: compRenameMu, courtCheckMu, the
//     affected competitions' write locks in sorted id order, s.mu, then
//     tournamentMu for a tournament restore. Then judge the plan again under
//     the locks (plan_changed, copy_name_taken, matches_running), and stage
//     tournament.md again when its password changed meanwhile.
//  4. Settle every affected competition's pending WAL, so a restart cannot
//     replay old intents over the restored files.
//  5. BeforeSwap, fed by snapshotLocked under the locks already held.
//  6. Write the journal (and prefixes-pending.json), then the renames.
//  7. The cache ritual, then the journal and the token folder are removed.
//
// Why the lock order cannot deadlock. No other path holds two competition
// locks at once, and this one takes them in sorted id order. Every holder of
// courtCheckMu (WithCourtExclusivityLock: the score and start paths) takes it
// before any competition lock and never takes compRenameMu inside it
// (grep -n 'courtCheckMu\|WithCourtExclusivityLock' internal/ finds no
// compRenameMu or WithCompetitionRenameLock under it), and every holder of
// compRenameMu takes it before any competition lock. s.mu is taken after a
// competition lock (DeleteCompetition) and tournamentMu after s.mu, as every
// tournament.md writer does. So compRenameMu -> courtCheckMu -> competition
// locks (sorted) -> s.mu -> tournamentMu follows every existing order. A
// future path that took compRenameMu inside courtCheckMu would close a cycle
// with this one; keep it out.
func (s *Store) Restore(req RestoreRequest) (plan RestorePlan, err error) {
	ai, err := parseRestoreArchive(req.Archive, req.Only)
	if err != nil {
		return RestorePlan{}, err
	}
	s.mu.RLock()
	live, err := s.readRestoreLiveStateLocked()
	s.mu.RUnlock()
	if err != nil {
		return RestorePlan{}, err
	}
	w, err := planRestore(req, ai, live)
	if err != nil {
		return RestorePlan{}, err
	}
	if err := w.refusal(req); err != nil {
		return w.plan, err
	}

	token, err := newRestoreToken()
	if err != nil {
		return w.plan, err
	}
	tokenDir := s.restoreTokenDir(token)
	journaled := false
	defer func() {
		if journaled {
			return
		}
		if rerr := s.root.removeAll(tokenDir); rerr != nil {
			err = errors.Join(err, rerr)
		}
	}()
	password := mergedPassword(req, live)
	if err := s.stageRestore(tokenDir, req.Archive, ai, w, password); err != nil {
		return w.plan, err
	}

	if restoreBeforeLock != nil {
		restoreBeforeLock()
	}

	// Step 3: the one lock order (see above).
	s.compRenameMu.Lock()
	defer s.compRenameMu.Unlock()
	s.courtCheckMu.Lock()
	defer s.courtCheckMu.Unlock()
	lockIDs := slices.Concat(w.replaced, w.removed, w.created)
	slices.Sort(lockIDs)
	lockIDs = slices.Compact(lockIDs)
	held := make([]*sync.RWMutex, 0, len(lockIDs))
	defer func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}()
	for _, id := range lockIDs {
		// lockComp re-checks that the mutex it holds is still getCompLock(id)'s
		// (a DeleteCompetition that ran while it waited drops the entry).
		held = append(held, s.lockComp(id))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.tournament {
		s.tournamentMu.Lock()
		defer s.tournamentMu.Unlock()
	}

	live2, err := s.readRestoreLiveStateLocked()
	if err != nil {
		return w.plan, err
	}
	if !slices.Equal(live.ids, live2.ids) {
		return w.plan, &RestoreError{Code: RestoreCodePlanChanged, CompetitionIDs: symmetricDifference(live.ids, live2.ids)}
	}
	w2, err := planRestore(req, ai, live2)
	if err != nil {
		return w.plan, err
	}
	if err := w2.refusal(req); err != nil {
		return w2.plan, err
	}
	if w2.tournament {
		if p2 := mergedPassword(req, live2); p2 != password {
			// A PUT /api/tournament landed between staging and the lock.
			if err := s.stageTournamentMD(tokenDir, ai, p2); err != nil {
				return w2.plan, err
			}
		}
	}

	// Step 4: settle pending WALs for every affected competition.
	for _, id := range lockIDs {
		if err := s.settleCompWALsLocked(id); err != nil {
			return w2.plan, &RestoreError{Code: RestoreCodePendingWriteUnsettled, CompetitionIDs: []string{id}, Err: err}
		}
	}

	// Step 5: the safety copy. snapshotLocked, never Snapshot, which would
	// take the locks this restore already holds. The hook writes the copy
	// through SaveBackupFile, which reaches atomicWriteFile and takes no
	// store lock (backupSaveMu is its own), so it cannot deadlock here.
	if req.BeforeSwap != nil {
		scope := ScopeCompetition
		if w2.tournament {
			scope = ScopeTournament
		}
		snap, err := s.snapshotLocked(scope, slices.Concat(w2.replaced, w2.removed), true)
		if err != nil {
			return w2.plan, err
		}
		if err := req.BeforeSwap(RestoreSafetyCopy{Snapshot: snap, Kind: backup.KindBeforeRestore, RestoreCreated: slices.Clone(w2.created)}); err != nil {
			return w2.plan, err
		}
	}

	// Step 6: prefixes-pending.json made durable before the journal, so a
	// crash anywhere in the swap still leaves the prefixes to resolve. A
	// restore that never completes its journal write leaves a harmless
	// pending entry whose prefix does not collide. Then the journal, durable
	// before the first live rename.
	if len(w2.plan.PrefixesPending) > 0 {
		if err := s.writePrefixesPending(w2.plan.PrefixesPending); err != nil {
			return w2.plan, err
		}
	}
	j := restoreJournalFor(w2, live2, ai)
	if err := s.writeRestoreJournal(tokenDir, j); err != nil {
		return w2.plan, err
	}
	journaled = true
	runErr := s.runRestoreJournal(tokenDir, j)

	// Step 7: the cache ritual, run even when a rename failed, so nothing
	// serves what was there before.
	s.restoreCacheRitualLocked(w2)
	if runErr != nil {
		return w2.plan, fmt.Errorf("restore stopped part way; it completes when the server next starts: %w", runErr)
	}
	if ferr := s.finishRestoreToken(tokenDir); ferr != nil {
		// The swap is complete; the next start removes what is left.
		slog.Warn("state: restore done, but its staging folder was not removed", "dir", tokenDir, "err", ferr)
	}
	return w2.plan, nil
}

// restoreBeforeLock, when set, runs after staging and before the restore takes
// its locks: the window in which another write can land. Only tests set it.
var restoreBeforeLock func()

func newRestoreToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func symmetricDifference(a, b []string) []string {
	in := map[string]int{}
	for _, x := range a {
		in[x]++
	}
	for _, x := range b {
		in[x]--
	}
	var out []string
	for x, n := range in {
		if n != 0 {
			out = append(out, x)
		}
	}
	slices.Sort(out)
	return out
}

// restoreJournalFor lists the swap: every move out first (a replaced or a
// removed competition, and for a tournament restore each tournament path the
// folder holds), then every move in (each restored competition, and for a
// tournament restore tournament.md and each of branding/ and sponsors/ the
// archive holds).
func restoreJournalFor(w *restoreWork, live *restoreLiveState, ai *restoreArchiveInfo) restoreJournal {
	j := restoreJournal{Format: restoreJournalV1}
	outs := slices.Concat(w.replaced, w.removed)
	slices.Sort(outs)
	for _, id := range outs {
		j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveOut, Path: "competitions/" + id})
	}
	if w.tournament {
		if live.hasTournament {
			j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveOut, Path: "tournament.md"})
		}
		if live.hasBranding {
			j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveOut, Path: BrandingDirName})
		}
		if live.hasSponsors {
			j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveOut, Path: SponsorsDirName})
		}
	}
	ins := make([]string, 0, len(w.targets))
	for _, t := range w.targets {
		ins = append(ins, t.dstID)
	}
	slices.Sort(ins)
	for _, id := range ins {
		j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveIn, Path: "competitions/" + id})
	}
	if w.tournament {
		j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveIn, Path: "tournament.md"})
		if ai.hasBranding {
			j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveIn, Path: BrandingDirName})
		}
		if ai.hasSponsors {
			j.Steps = append(j.Steps, restoreStep{Kind: restoreMoveIn, Path: SponsorsDirName})
		}
	}
	return j
}

// stageRestore writes every file the swap moves in under tokenDir/new/, makes
// tokenDir/old/, and fsyncs every staged file and folder.
func (s *Store) stageRestore(tokenDir string, a *backup.Archive, ai *restoreArchiveInfo, w *restoreWork, password string) error {
	newRoot := filepath.Join(tokenDir, restoreNewDir)
	dirs := map[string]struct{}{
		s.restoreRootDir():                     {},
		tokenDir:                               {},
		newRoot:                                {},
		filepath.Join(newRoot, "competitions"): {},
		filepath.Join(tokenDir, restoreOldDir): {},
		filepath.Join(tokenDir, restoreOldDir, "competitions"): {},
	}
	for d := range dirs {
		if err := s.root.mkdirAll(d, 0o700); err != nil {
			return err
		}
	}
	write := func(rel string, data []byte) error {
		p := filepath.Join(newRoot, filepath.FromSlash(rel))
		for d := filepath.Dir(p); d != newRoot && strings.HasPrefix(d, newRoot); d = filepath.Dir(d) {
			dirs[d] = struct{}{}
		}
		return s.root.writeSynced(p, data, 0o600)
	}
	paths := make([]string, 0, len(a.Files))
	for p := range a.Files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	for _, t := range w.targets {
		prefix := "competitions/" + t.srcID + "/"
		for _, p := range paths {
			rel, ok := strings.CutPrefix(p, prefix)
			if !ok {
				continue
			}
			data := a.Files[p]
			if t.action == RestoreCopy {
				var err error
				switch rel {
				case "config.md":
					data, err = rewriteCopyConfig(data, t.dstID, t.name)
				case teamLineupFilename:
					data, err = rewriteLineupsCompetitionID(data, t.srcID, t.dstID)
				}
				// history/*.jsonl and events/*.jsonl lines carry no
				// competition id (MatchHistoryEntry, ScoreLogLine), so a copy
				// keeps them as they are.
				if err != nil {
					return &RestoreError{Code: RestoreCodeFileUnreadable, Path: p, CompetitionIDs: []string{t.srcID}, Err: err}
				}
			}
			if err := write("competitions/"+t.dstID+"/"+rel, data); err != nil {
				return err
			}
		}
	}
	if w.tournament {
		for _, p := range paths {
			if strings.HasPrefix(p, BrandingDirName+"/") || strings.HasPrefix(p, SponsorsDirName+"/") {
				if err := write(p, a.Files[p]); err != nil {
					return err
				}
			}
		}
		if err := s.stageTournamentMD(tokenDir, ai, password); err != nil {
			return err
		}
	}
	all := make([]string, 0, len(dirs))
	for d := range dirs {
		all = append(all, d)
	}
	// Deepest first, so a folder is synced after the entries made in it.
	slices.SortFunc(all, func(x, y string) int {
		return strings.Count(y, string(filepath.Separator)) - strings.Count(x, string(filepath.Separator))
	})
	for _, d := range all {
		if err := syncDir(s.root, d); err != nil {
			return err
		}
	}
	return nil
}

// stageTournamentMD writes the merged tournament.md (the archive's settings
// with password) as tokenDir/new/tournament.md and fsyncs it and its folder.
func (s *Store) stageTournamentMD(tokenDir string, ai *restoreArchiveInfo, password string) error {
	var t Tournament
	if ai.tourn != nil {
		t = *ai.tourn
	}
	t.Password = password
	data, err := writeFrontMatter(&t)
	if err != nil {
		return err
	}
	newRoot := filepath.Join(tokenDir, restoreNewDir)
	if err := s.root.writeSynced(filepath.Join(newRoot, "tournament.md"), data, 0o600); err != nil {
		return err
	}
	return syncDir(s.root, newRoot)
}

// rewriteCopyConfig gives a copy's config.md its new id and name, leaving
// every other key as the archive holds it.
func rewriteCopyConfig(raw []byte, id, name string) ([]byte, error) {
	var doc yaml.Node
	if err := parseFrontMatter(raw, &doc); err != nil {
		return nil, err
	}
	top := &doc
	if top.Kind == yaml.DocumentNode && len(top.Content) > 0 {
		top = top.Content[0]
	}
	if top.Kind != yaml.MappingNode {
		return nil, errors.New("config.md front matter is not a mapping")
	}
	setMappingScalar(top, "id", id)
	setMappingScalar(top, "name", name)
	return writeFrontMatter(&doc)
}

func setMappingScalar(m *yaml.Node, key, value string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value})
}

// rewriteLineupsCompetitionID gives every lineup in a copy's lineups.yaml the
// copy's id in place of the original's.
func rewriteLineupsCompetitionID(raw []byte, from, to string) ([]byte, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	var walk func(n *yaml.Node)
	walk = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value == "competitionId" && n.Content[i+1].Value == from {
					n.Content[i+1].Value = to
				}
			}
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(&doc)
	return yaml.Marshal(&doc)
}

// restoreCacheRitualLocked drops what the store cached for every competition
// the restore replaced, created or removed. A replaced or created id keeps its
// compMu entry (the restore holds that mutex); a removed id gets
// DeleteCompetition's ritual, compMu.Delete included, while its lock is held.
// discardCompCacheBodies (never compCache.Delete) clears each cached body and
// bumps its version after the bytes landed, which also invalidates the
// engine's standings cache.
func (s *Store) restoreCacheRitualLocked(w *restoreWork) {
	for _, t := range w.targets {
		s.legacyUpgraded.Delete(t.dstID)
		s.bracketRoundsRefusalLogged.Delete(t.dstID)
		s.discardCompCacheBodies(t.dstID)
	}
	for _, id := range w.removed {
		s.legacyUpgraded.Delete(id)
		s.bracketRoundsRefusalLogged.Delete(id)
		s.discardCompCacheBodies(id)
		s.compMu.Delete(id)
	}
	if w.tournament {
		s.cachedTourn = nil
		s.tournMtime = 0
	}
}

package state

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// equalFoldTrimmed checks equality of two strings ignoring leading/trailing whitespace and case.
func equalFoldTrimmed(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

// containsName returns true if target equals any candidate in names (whitespace-trimmed, case-folded).
func containsName(names []string, target string) bool {
	for _, n := range names {
		if equalFoldTrimmed(n, target) {
			return true
		}
	}
	return false
}

func targetID(e RestoreCompetitionPlan) string {
	if e.NewID != "" {
		return e.NewID
	}
	return e.ID
}

// buildRestoreCompetitionEntries constructs initial competition plan entries for
// archive competitions and live competitions not in the archive.
func buildRestoreCompetitionEntries(
	archiveIDs []string,
	ai *restoreArchiveInfo,
	live *restoreLiveState,
	inFolder, inArchive, created map[string]bool,
	undo, tournament bool,
) []RestoreCompetitionPlan {
	var entries []RestoreCompetitionPlan
	for _, id := range archiveIDs {
		e := RestoreCompetitionPlan{ID: id, Name: ai.names[id]}
		switch {
		case !inFolder[id]:
			e.Action = RestoreCreate
		case undo:
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
		case undo && created[id]:
			e.Action = RestoreRemove
		case undo && tournament:
			e.Action = RestoreKeep
		case tournament:
			e.Asked = true
			e.Choices = []RestoreAction{RestoreKeep, RestoreRemove}
		default:
			continue
		}
		entries = append(entries, e)
	}
	return entries
}

// applyRestoreDecisions applies decisions to asked entries, checks for valid actions and unknown decisions,
// and collects missing decision IDs and copy names.
func applyRestoreDecisions(
	entries []RestoreCompetitionPlan,
	decisions map[string]RestoreDecision,
	plan *RestorePlan,
) (map[string]string, error) {
	copyNames := map[string]string{}
	asked := map[string]bool{}
	for i := range entries {
		e := &entries[i]
		if !e.Asked {
			continue
		}
		asked[e.ID] = true
		d, ok := decisions[e.ID]
		if !ok {
			plan.Missing = append(plan.Missing, e.ID)
			continue
		}
		if !slices.Contains(e.Choices, d.Action) {
			return nil, &RestoreError{
				Code:           RestoreCodeBadDecision,
				CompetitionIDs: []string{e.ID},
				Err:            fmt.Errorf("%q is not one of %v", d.Action, e.Choices),
			}
		}
		e.Action = d.Action
		if d.Action == RestoreCopy {
			copyNames[e.ID] = strings.TrimSpace(d.CopyName)
		}
	}
	var unknown []string
	for id := range decisions {
		if !asked[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) > 0 {
		slices.Sort(unknown)
		return nil, &RestoreError{
			Code:           RestoreCodeBadDecision,
			CompetitionIDs: unknown,
			Err:            errors.New("no question about these competitions"),
		}
	}
	return copyNames, nil
}

// checkRestoreNameClashes checks Create and Replace entries for name clashes against competitions
// that stay in place, or other competitions restored in the same plan. Returns the names of competitions
// staying in place.
func checkRestoreNameClashes(
	entries []RestoreCompetitionPlan,
	ai *restoreArchiveInfo,
	live *restoreLiveState,
	w *restoreWork,
) []string {
	actions := make(map[string]RestoreAction, len(entries))
	for _, e := range entries {
		actions[e.ID] = e.Action
	}

	// Names of live competitions that stay in place (not replaced and not removed).
	// A competition replaced in this restore no longer holds its old live name (it
	// takes the archive name), and a removed one frees its name.
	var inPlaceNames []string
	for _, id := range live.ids {
		if actions[id] == RestoreReplace || actions[id] == RestoreRemove {
			continue
		}
		if name := competitionName(live, id); name != "" {
			inPlaceNames = append(inPlaceNames, name)
		}
	}

	// Check Create and Replace (and undo) entries for name clashes against competitions
	// that stay in place, or other competitions restored in the same plan.
	var restoredNames []string
	for i := range entries {
		e := &entries[i]
		if e.Action != RestoreCreate && e.Action != RestoreReplace {
			continue
		}
		name := strings.TrimSpace(ai.names[e.ID])
		if name == "" {
			continue
		}
		if containsName(inPlaceNames, name) || containsName(restoredNames, name) {
			e.NameTaken = true
			w.nameClash = append(w.nameClash, e.ID)
		} else {
			restoredNames = append(restoredNames, name)
		}
	}
	return inPlaceNames
}

// resolveRestoreCopyNames checks and assigns new IDs for RestoreCopy entries against existing IDs,
// created IDs, and taken names.
func resolveRestoreCopyNames(
	entries []RestoreCompetitionPlan,
	copyNames map[string]string,
	inPlaceNames []string,
	liveIDs []string,
	ai *restoreArchiveInfo,
	w *restoreWork,
) {
	var takenNames []string
	takenNames = append(takenNames, inPlaceNames...)
	for _, e := range entries {
		if (e.Action == RestoreCreate || e.Action == RestoreReplace) && !e.NameTaken {
			if name := ai.names[e.ID]; name != "" {
				takenNames = append(takenNames, name)
			}
		}
	}
	taken := map[string]bool{}
	for _, id := range liveIDs {
		taken[id] = true
	}
	for _, e := range entries {
		if e.Action == RestoreCreate {
			taken[e.ID] = true
		}
	}
	for i := range entries {
		e := &entries[i]
		if e.Action != RestoreCopy {
			continue
		}
		name := copyNames[e.ID]
		newID := SlugifyID(name)
		if name == "" || ValidateCompetitionID(newID) != nil || taken[newID] || containsName(takenNames, name) {
			e.CopyNameTaken = true
			w.copyTaken = append(w.copyTaken, e.ID)
			continue
		}
		e.NewID = newID
		taken[newID] = true
		takenNames = append(takenNames, name)
	}
}

// populateRestoreTargets fills w.targets, w.created, w.replaced, and w.removed,
// and returns the IDs of competitions that remain in place.
func populateRestoreTargets(
	entries []RestoreCompetitionPlan,
	copyNames map[string]string,
	liveIDs []string,
	w *restoreWork,
) []string {
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

	leaving := map[string]bool{}
	for _, id := range w.replaced {
		leaving[id] = true
	}
	for _, id := range w.removed {
		leaving[id] = true
	}
	var inPlace []string
	for _, id := range liveIDs {
		if !leaving[id] {
			inPlace = append(inPlace, id)
		}
	}
	return inPlace
}

// checkRestoreRunningMatches warns about live matches running in a competition the restore
// replaces or removes, and collects the archive's running matches, which come back in the
// queue. An entry still waiting for the operator's choice gets the warning its choices could
// lead to, so the page shows it before the choice; it requeues and refuses nothing yet.
func checkRestoreRunningMatches(
	entries []RestoreCompetitionPlan,
	inPlace []string,
	ai *restoreArchiveInfo,
	live *restoreLiveState,
	w *restoreWork,
) {
	warn := func(code, compID string, n int, one, many string) {
		if n == 0 {
			return
		}
		msg := fmt.Sprintf(many, n)
		if n == 1 {
			msg = one
		}
		w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{Code: code, CompetitionID: compID, Message: msg})
	}
	warnRequeued := func(e *RestoreCompetitionPlan, compID string) {
		warn(RestoreWarnMatchesRequeued, compID, len(e.RunningInArchive),
			e.Name+" comes back with 1 match that was running in the backup; it returns to the queue with its score.",
			e.Name+" comes back with %d matches that were running in the backup; they return to the queue with their score.")
	}
	for i := range entries {
		e := &entries[i]
		undecided := e.Asked && e.Action == ""
		switch {
		case e.Action == RestoreReplace || e.Action == RestoreRemove:
			e.RunningNow = runningMatchIDs(live.matches[e.ID])
			if len(e.RunningNow) > 0 {
				w.runningComp = append(w.runningComp, e.ID)
				warn(RestoreWarnMatchesRunning, e.ID, len(e.RunningNow),
					e.Name+" has 1 match running now; the restore replaces it with the backup's version.",
					e.Name+" has %d matches running now; the restore replaces them with the backup's version.")
			}
			var archived map[string]restoreMatch
			if e.Action == RestoreReplace {
				archived = ai.matches[e.ID]
			}
			e.ResultsChanged = resultsChanged(live.matches[e.ID], archived)
		case undecided && slices.Contains(e.Choices, RestoreReplace):
			e.RunningNow = runningMatchIDs(live.matches[e.ID])
			warn(RestoreWarnMatchesRunning, e.ID, len(e.RunningNow),
				e.Name+" has 1 match running now; if you replace it, the restore replaces that match with the backup's version.",
				e.Name+" has %d matches running now; if you replace it, the restore replaces those matches with the backup's version.")
		case undecided && slices.Contains(e.Choices, RestoreRemove):
			e.RunningNow = runningMatchIDs(live.matches[e.ID])
			warn(RestoreWarnMatchesRunning, e.ID, len(e.RunningNow),
				e.Name+" has 1 match running now; if you remove it, that match is removed with it.",
				e.Name+" has %d matches running now; if you remove it, those matches are removed with it.")
		}
		switch {
		case e.Action == RestoreReplace || e.Action == RestoreCreate || (e.Action == RestoreCopy && e.NewID != ""):
			e.RunningInArchive = runningMatchIDs(ai.matches[e.ID])
			if len(e.RunningInArchive) > 0 {
				dstID := targetID(*e)
				if w.plan.RequeuePending == nil {
					w.plan.RequeuePending = make(map[string][]string)
				}
				w.plan.RequeuePending[dstID] = slices.Clone(e.RunningInArchive)
				warnRequeued(e, dstID)
			}
		case undecided && slices.Contains(e.Choices, RestoreReplace):
			e.RunningInArchive = runningMatchIDs(ai.matches[e.ID])
			warnRequeued(e, e.ID)
		}
	}
}

// checkRestoreTournamentSettings validates tournament settings like mode changes, missing courts,
// and password requirements when restoring a whole tournament.
func checkRestoreTournamentSettings(
	req RestoreRequest,
	inPlace []string,
	ai *restoreArchiveInfo,
	live *restoreLiveState,
	w *restoreWork,
) {
	if ai.tourn == nil {
		return
	}
	restored := *ai.tourn
	ApplyTournamentDefaults(&restored)
	if live.tourn != nil {
		current := *live.tourn
		ApplyTournamentDefaults(&current)
		if current.Mode != restored.Mode {
			w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{
				Code:    RestoreWarnModeChanges,
				Message: fmt.Sprintf("The tournament mode changes from %s to %s, which changes what the public page can do.", current.Mode, restored.Mode),
			})
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
				w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{
					Code:          RestoreWarnCourtMissing,
					CompetitionID: id,
					Court:         court,
					Message:       fmt.Sprintf("%s uses shiaijo %s, which the restored tournament settings do not have; its matches there cannot be called until the shiaijo is added back or they are moved.", name, court),
				})
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

// checkRestorePrefixCollisions verifies whether any restored competition's number prefix
// collides with an in-place or earlier restored competition, appending warnings and PrefixesPending.
func checkRestorePrefixCollisions(
	entries []RestoreCompetitionPlan,
	inPlace []string,
	ai *restoreArchiveInfo,
	live *restoreLiveState,
	w *restoreWork,
) {
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
			w.plan.Warnings = append(w.plan.Warnings, RestoreWarning{
				Code:          RestoreWarnPrefixReassigned,
				CompetitionID: targetID(*e),
				Message:       fmt.Sprintf("%s's number prefix is used by another competition, so it gets a new one; reprint this competition's tags.", e.Name),
			})
		}
	}
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
	w.plan = RestorePlan{
		Scope:              ScopeCompetition,
		Kind:               man.Kind,
		Undo:               req.Undo,
		TournamentReplaced: tournament,
		Password:           RestorePasswordKept,
	}
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

	entries := buildRestoreCompetitionEntries(archiveIDs, ai, live, inFolder, inArchive, created, req.Undo, tournament)

	copyNames, err := applyRestoreDecisions(entries, req.Decisions, &w.plan)
	if err != nil {
		return nil, err
	}

	inPlaceNames := checkRestoreNameClashes(entries, ai, live, w)
	resolveRestoreCopyNames(entries, copyNames, inPlaceNames, live.ids, ai, w)

	inPlace := populateRestoreTargets(entries, copyNames, live.ids, w)
	checkRestoreRunningMatches(entries, inPlace, ai, live, w)

	if tournament {
		checkRestoreTournamentSettings(req, inPlace, ai, live, w)
	}

	checkRestorePrefixCollisions(entries, inPlace, ai, live, w)

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

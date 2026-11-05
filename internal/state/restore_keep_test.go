package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Keep in a whole-tournament restore: a competition the archive and the folder
// both hold stays as the folder has it, and a competition only the archive
// holds is left out (decision page, 2026-11-05).

// keepRig is a tournament archive holding men and solo, taken before men
// changed (renamed, new prefix, a match now running) and solo was deleted.
func keepRig(t *testing.T) (*Store, string, *RestoreRequest) {
	t.Helper()
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	addRestoreComp(t, s, "solo", "Solo", "S", MatchStatusScheduled, "B")
	a := archiveOf(t, s, ScopeTournament, "")
	require.NoError(t, s.DeleteCompetition("solo"))
	men, err := s.LoadCompetition("men")
	require.NoError(t, err)
	men.Name = "Men Live"
	men.NumberPrefix = "S"
	require.NoError(t, s.SaveCompetition(men))
	setRestoreMatch(t, s, "men", MatchStatusRunning, "A", "")
	return s, dir, &RestoreRequest{Archive: a}
}

func TestRestore_TournamentScopeOffersKeepForArchiveCompetitions(t *testing.T) {
	s, _, req := keepRig(t)

	plan, err := s.PlanRestore(*req)
	require.NoError(t, err)
	men := planEntry(t, plan, "men")
	assert.True(t, men.Asked)
	assert.Equal(t, []RestoreAction{RestoreReplace, RestoreCopy, RestoreKeep}, men.Choices)
	solo := planEntry(t, plan, "solo")
	assert.False(t, solo.Asked, "a competition only the backup holds is added unless the operator says otherwise")
	assert.Equal(t, RestoreCreate, solo.Action)
	assert.Equal(t, []RestoreAction{RestoreCreate, RestoreKeep}, solo.Choices)
	assert.Equal(t, []string{"men"}, plan.Missing, "leaving solo unanswered is not a missing answer")

	// Under competition scope keeping would restore nothing, so it is not offered.
	only, err := s.PlanRestore(RestoreRequest{Archive: req.Archive, Only: "men"})
	require.NoError(t, err)
	assert.Equal(t, []RestoreAction{RestoreReplace, RestoreCopy}, planEntry(t, only, "men").Choices)
	only, err = s.PlanRestore(RestoreRequest{Archive: req.Archive, Only: "solo"})
	require.NoError(t, err)
	assert.Empty(t, planEntry(t, only, "solo").Choices)
	_, err = s.PlanRestore(RestoreRequest{Archive: req.Archive, Only: "solo", Decisions: map[string]RestoreDecision{"solo": {Action: RestoreKeep}}})
	assert.Equal(t, RestoreCodeBadDecision, restoreCode(t, err), "a create with no Choices takes no answer")
	compArchive := archiveOf(t, s, ScopeCompetition, "men")
	cp, err := s.PlanRestore(RestoreRequest{Archive: compArchive})
	require.NoError(t, err)
	assert.Equal(t, []RestoreAction{RestoreReplace, RestoreCopy}, planEntry(t, cp, "men").Choices)

	// A create answered with something it does not offer is refused.
	_, err = s.PlanRestore(RestoreRequest{Archive: req.Archive, Decisions: map[string]RestoreDecision{"solo": {Action: RestoreReplace}}})
	assert.Equal(t, RestoreCodeBadDecision, restoreCode(t, err))
}

func TestRestore_KeptCompetitionsStayAsTheFolderHasThemAndUndoLeavesThemAlone(t *testing.T) {
	s, dir, req := keepRig(t)
	menBefore := filesUnder(folderFiles(t, dir), "competitions/men/")

	req.Decisions = map[string]RestoreDecision{"men": {Action: RestoreKeep}, "solo": {Action: RestoreKeep}}
	plan, err := s.PlanRestore(*req)
	require.NoError(t, err)
	men := planEntry(t, plan, "men")
	assert.Equal(t, RestoreKeep, men.Action)
	assert.Empty(t, men.ResultsChanged, "a kept competition loses no result")
	assert.Empty(t, men.RunningNow)
	assert.Empty(t, warningsWithCode(plan.Warnings, RestoreWarnMatchesRunning), "a kept competition's running match is not touched")
	assert.Empty(t, plan.PrefixesPending)
	assert.True(t, plan.TournamentReplaced, "the tournament settings are still put back")

	sc := safetyCopy{store: s}
	req.BeforeSwap = sc.hook(t)
	// No AllowRunning: the match running in men is not replaced.
	_, err = s.Restore(*req)
	require.NoError(t, err)
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.Equal(t, []string{"men"}, ids, "a backup-only competition answered keep is not added")
	assert.Equal(t, menBefore, filesUnder(folderFiles(t, dir), "competitions/men/"),
		"a kept competition is the folder's, byte for byte, not the archive's")
	require.NotNil(t, sc.archive)
	assert.Empty(t, *sc.archive.Manifest.RestoreCreated, "nothing was created")
	assert.Empty(t, sc.archive.Manifest.Competitions, "the safety copy holds nothing it did not replace or remove")

	// The Undo has nothing to put back for either: men stays as it is.
	undo, err := s.PlanRestore(RestoreRequest{Archive: sc.archive, Undo: true})
	require.NoError(t, err)
	assert.Equal(t, RestoreKeep, planEntry(t, undo, "men").Action)
	for _, c := range undo.Competitions {
		assert.False(t, c.Asked)
		assert.Empty(t, c.Choices, "an undo offers no choice")
		assert.NotEqual(t, "solo", c.ID)
	}
	_, err = s.Restore(RestoreRequest{Archive: sc.archive, Undo: true, AllowRunning: true})
	require.NoError(t, err)
	assert.Equal(t, menBefore, filesUnder(folderFiles(t, dir), "competitions/men/"), "the undo leaves a kept competition alone")
	ids, err = s.ListCompetitions()
	require.NoError(t, err)
	assert.Equal(t, []string{"men"}, ids)
	assertNoStaging(t, dir)
}

// A kept competition keeps the folder's name and prefix: the name clash, copy
// name and prefix checks read those, never the archive's.
func TestRestore_AKeptCompetitionHoldsItsFolderNameAndPrefix(t *testing.T) {
	s, _, req := keepRig(t)
	plan := func(d map[string]RestoreDecision) RestorePlan {
		t.Helper()
		p, err := s.PlanRestore(RestoreRequest{Archive: req.Archive, Decisions: d})
		require.NoError(t, err)
		return p
	}

	kept := plan(map[string]RestoreDecision{"men": {Action: RestoreKeep}})
	solo := planEntry(t, kept, "solo")
	assert.True(t, solo.PrefixCollides, "the kept men holds prefix S, which solo brings back")
	assert.Equal(t, []string{"solo"}, kept.PrefixesPending)
	assert.False(t, solo.NameTaken)

	replaced := plan(map[string]RestoreDecision{"men": {Action: RestoreReplace}})
	assert.False(t, planEntry(t, replaced, "solo").PrefixCollides, "a replaced men takes the archive's prefix M")

	// men is kept, so it is not a copy here; the copy is of solo, which the
	// folder lacks, so ask the copy name through a second in-both competition.
	require.NoError(t, s.SaveCompetition(&Competition{ID: "women", Name: "Women", Date: "03-11-2026", Status: CompStatusPools, NumberPrefix: "W", Courts: []string{"B"}}))
	a2 := archiveOf(t, s, ScopeTournament, "")
	men, err := s.LoadCompetition("men")
	require.NoError(t, err)
	men.Name = "Men Later"
	require.NoError(t, s.SaveCompetition(men))
	copyPlan := func(menAction RestoreAction, copyName string) RestoreCompetitionPlan {
		t.Helper()
		p, err := s.PlanRestore(RestoreRequest{Archive: a2, Decisions: map[string]RestoreDecision{
			"men":   {Action: menAction},
			"women": {Action: RestoreCopy, CopyName: copyName},
		}})
		require.NoError(t, err)
		return planEntry(t, p, "women")
	}
	assert.True(t, copyPlan(RestoreKeep, "Men Later").CopyNameTaken, "a kept men keeps its folder name")
	assert.False(t, copyPlan(RestoreKeep, "Men Live").CopyNameTaken, "the archive's name for a kept men is not restored")
	assert.True(t, copyPlan(RestoreReplace, "Men Live").CopyNameTaken, "a replaced men takes the archive's name")
	assert.False(t, copyPlan(RestoreReplace, "Men Later").CopyNameTaken)
}

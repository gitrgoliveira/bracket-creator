package state

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers shared by the restore tests ---

func newRestoreTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	return s, dir
}

func restoreTestTournament(t *testing.T, s *Store, password string, courts ...string) {
	t.Helper()
	if len(courts) == 0 {
		courts = []string{"A", "B"}
	}
	require.NoError(t, s.SaveTournament(&Tournament{
		Name: "Restore Taikai", Date: "03-11-2026", Venue: "Budokan",
		Courts: courts, Password: password, DurationDays: 1, Mode: TournamentModeOfficiated,
	}))
}

// addRestoreComp saves a competition with one pool match in the given state.
func addRestoreComp(t *testing.T, s *Store, id, name, prefix string, status MatchStatus, court string) {
	t.Helper()
	require.NoError(t, s.SaveCompetition(&Competition{
		ID: id, Name: name, Date: "03-11-2026", Status: CompStatusPools,
		NumberPrefix: prefix, Courts: []string{court},
	}))
	setRestoreMatch(t, s, id, status, court, "")
}

func setRestoreMatch(t *testing.T, s *Store, id string, status MatchStatus, court, winner string) {
	t.Helper()
	m := MatchResult{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "p1", SideBID: "p2", Status: status, Court: court}
	if winner != "" {
		m.Winner = winner
		m.WinnerID = "p1"
		m.IpponsA = []string{"M"}
	}
	require.NoError(t, s.SavePoolMatches(id, []MatchResult{m}))
}

func archiveFiles(t *testing.T, scope backup.Scope, kind backup.Kind, snap Snapshot, restoreCreated []string) *backup.Archive {
	t.Helper()
	data := archiveBytes(t, scope, kind, snap, restoreCreated)
	a, err := backup.Read(bytes.NewReader(data), int64(len(data)), backup.Checks{ValidID: ValidateCompetitionID, FrontMatter: CheckFrontMatter})
	require.NoError(t, err)
	return a
}

func archiveBytes(t *testing.T, scope backup.Scope, kind backup.Kind, snap Snapshot, restoreCreated []string) []byte {
	t.Helper()
	var comps []backup.CompetitionRef
	for id, name := range snap.Competitions {
		comps = append(comps, backup.CompetitionRef{ID: id, Name: name})
	}
	slices.SortFunc(comps, func(a, b backup.CompetitionRef) int { return strings.Compare(a.ID, b.ID) })
	meta := backup.Meta{
		CreatedAt:    time.Date(2026, 11, 3, 10, 0, 0, 0, time.UTC),
		Scope:        scope,
		Kind:         kind,
		Tournament:   backup.TournamentRef{Name: snap.TournamentName},
		Competitions: comps,
	}
	if kind == backup.KindBeforeRestore {
		meta.RestoreCreated = restoreCreated
	}
	var buf bytes.Buffer
	require.NoError(t, backup.Write(&buf, meta, snap.Files))
	return buf.Bytes()
}

func archiveOf(t *testing.T, s *Store, scope SnapshotScope, compID string) *backup.Archive {
	t.Helper()
	snap, err := s.Snapshot(scope, compID)
	require.NoError(t, err)
	return archiveFiles(t, backup.Scope(scope), backup.KindManual, snap, nil)
}

// safetyCopy captures the safety copy a restore hands its hook, as an archive.
// With store set, the hook also saves it through SaveBackupFile, as the HTTP
// handler does, under the restore's locks.
type safetyCopy struct {
	archive *backup.Archive
	store   *Store
	saved   string
}

func (c *safetyCopy) hook(t *testing.T) func(RestoreSafetyCopy) error {
	return func(sc RestoreSafetyCopy) error {
		assert.Equal(t, backup.KindBeforeRestore, sc.Kind)
		data := archiveBytes(t, backup.Scope(sc.Snapshot.Scope), sc.Kind, sc.Snapshot, sc.RestoreCreated)
		a, err := backup.Read(bytes.NewReader(data), int64(len(data)), backup.Checks{ValidID: ValidateCompetitionID, FrontMatter: CheckFrontMatter})
		require.NoError(t, err)
		c.archive = a
		if c.store != nil {
			name, err := c.store.SaveBackupFile(BackupKindBeforeRestore, sc.Snapshot.Scope, time.Now().UTC(), data)
			if err != nil {
				return err
			}
			c.saved = name
		}
		return nil
	}
}

// folderFiles maps every file under dir (relative, slash-separated) to its
// bytes.
func folderFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	require.NoError(t, filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out[filepath.ToSlash(rel)] = string(b)
		return nil
	}))
	return out
}

func filesUnder(all map[string]string, prefix string) map[string]string {
	out := map[string]string{}
	for k, v := range all {
		if rest, ok := strings.CutPrefix(k, prefix); ok {
			out[rest] = v
		}
	}
	return out
}

func assertNoStaging(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(dir, backup.RestoreDirName))
	if errors.Is(err, fs.ErrNotExist) {
		return
	}
	require.NoError(t, err)
	for _, e := range entries {
		assert.False(t, e.IsDir(), "a staging folder was left behind: %s", e.Name())
	}
}

func restoreCode(t *testing.T, err error) string {
	t.Helper()
	var re *RestoreError
	require.ErrorAs(t, err, &re)
	return re.Code
}

func planEntry(t *testing.T, p RestorePlan, id string) RestoreCompetitionPlan {
	t.Helper()
	for _, e := range p.Competitions {
		if e.ID == id {
			return e
		}
	}
	t.Fatalf("plan lists no competition %q: %+v", id, p.Competitions)
	return RestoreCompetitionPlan{}
}

// --- tests ---

func TestSlugifyID_MatchesTheCompetitionIDRule(t *testing.T) {
	assert.Equal(t, "men-s-individual-2", SlugifyID("  Men's Individual (2) "))
	assert.Equal(t, "", SlugifyID("!!!"))
	long := SlugifyID(strings.Repeat("ab ", 40))
	assert.Len(t, long, 64)
	assert.NoError(t, ValidateCompetitionID(long))
	assert.Equal(t, "ab-ab", SlugifyID("ab - -ab-"), "a run of separators is one hyphen and a trailing one is dropped")
}

func TestRestore_CreateIntoAFreshFolderAsksNothingAndLeavesBootstrap(t *testing.T) {
	src, _ := newRestoreTestStore(t)
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusCompleted, "A")
	a := archiveOf(t, src, ScopeCompetition, "men")
	srcFiles := filesUnder(folderFiles(t, src.GetFolder()), "competitions/men/")

	dst, dstDir := newRestoreTestStore(t)
	plan, err := dst.PlanRestore(RestoreRequest{Archive: a})
	require.NoError(t, err)
	assert.Equal(t, ScopeCompetition, plan.Scope)
	assert.Equal(t, RestorePasswordKept, plan.Password)
	e := planEntry(t, plan, "men")
	assert.Equal(t, RestoreCreate, e.Action)
	assert.False(t, e.Asked)
	assertNoStaging(t, dstDir)

	_, err = dst.Restore(RestoreRequest{Archive: a})
	require.NoError(t, err)
	assert.Equal(t, srcFiles, filesUnder(folderFiles(t, dstDir), "competitions/men/"))
	_, statErr := os.Stat(filepath.Join(dstDir, "tournament.md"))
	assert.True(t, os.IsNotExist(statErr), "a competition restore never writes tournament.md, so a fresh folder stays in bootstrap")
	c, err := dst.LoadCompetition("men")
	require.NoError(t, err)
	assert.Equal(t, "Men", c.Name)
	assertNoStaging(t, dstDir)
}

func TestRestore_ReplaceIsAskedAndAnUnansweredOneIsRefused(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	addRestoreComp(t, s, "women", "Women", "W", MatchStatusScheduled, "B")
	a := archiveOf(t, s, ScopeCompetition, "men")
	setRestoreMatch(t, s, "men", MatchStatusCompleted, "A", "Alice")

	plan, err := s.PlanRestore(RestoreRequest{Archive: a})
	require.NoError(t, err)
	e := planEntry(t, plan, "men")
	assert.True(t, e.Asked)
	assert.Equal(t, []RestoreAction{RestoreReplace, RestoreCopy}, e.Choices)
	assert.Equal(t, []string{"men"}, plan.Missing)

	before := folderFiles(t, dir)
	_, err = s.Restore(RestoreRequest{Archive: a})
	assert.Equal(t, RestoreCodeDecisionMissing, restoreCode(t, err))
	var re *RestoreError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, []string{"men"}, re.CompetitionIDs)
	assert.Equal(t, before, folderFiles(t, dir), "a refusal changes nothing")
	assertNoStaging(t, dir)

	// The results the replace loses are counted.
	plan, err = s.PlanRestore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	require.NoError(t, err)
	assert.Equal(t, []string{"Pool A-0"}, planEntry(t, plan, "men").ResultsChanged)

	// Warm the caches, then replace: the store serves the restored data.
	ms, err := s.LoadPoolMatches("men")
	require.NoError(t, err)
	require.Equal(t, MatchStatusCompleted, ms[0].Status)
	otherBefore := filesUnder(folderFiles(t, dir), "competitions/women/")
	_, err = s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	require.NoError(t, err)
	ms, err = s.LoadPoolMatches("men")
	require.NoError(t, err)
	assert.Equal(t, MatchStatusScheduled, ms[0].Status)
	assert.Equal(t, otherBefore, filesUnder(folderFiles(t, dir), "competitions/women/"), "another competition is untouched")
	_, ok := s.compMu.Load("men")
	assert.True(t, ok, "a replaced id keeps its compMu entry")
	assertNoStaging(t, dir)
}

func TestRestore_CopyTakesANewIDAndWarnsAboutThePrefix(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "K", MatchStatusScheduled, "A")
	require.NoError(t, s.WithTransaction("men", func(tx StoreTx) error {
		return tx.SetTeamLineup("men", domain.TeamLineup{
			TeamID: "p1", Round: 0, CompetitionID: "men",
			Positions: map[domain.Position]string{domain.PosSenpo: "M1"},
		}, 5)
	}))
	a := archiveOf(t, s, ScopeCompetition, "men")
	original := filesUnder(folderFiles(t, dir), "competitions/men/")

	req := RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreCopy, CopyName: "Men Copy"}}}
	plan, err := s.PlanRestore(req)
	require.NoError(t, err)
	e := planEntry(t, plan, "men")
	assert.Equal(t, "men-copy", e.NewID)
	assert.True(t, e.PrefixCollides)
	assert.Equal(t, []string{"men-copy"}, plan.PrefixesPending)

	_, err = s.Restore(req)
	require.NoError(t, err)
	assert.Equal(t, original, filesUnder(folderFiles(t, dir), "competitions/men/"), "the original is left as it is")
	c, err := s.LoadCompetition("men-copy")
	require.NoError(t, err)
	assert.Equal(t, "men-copy", c.ID)
	assert.Equal(t, "Men Copy", c.Name)
	lineups, err := s.LoadTeamLineups("men-copy")
	require.NoError(t, err)
	for _, l := range lineups {
		assert.Equal(t, "men-copy", l.CompetitionID)
	}
	pending, err := s.PrefixesPending()
	require.NoError(t, err)
	assert.Equal(t, []string{"men-copy"}, pending)
	require.NoError(t, s.RemovePrefixesPending([]string{"men-copy"}))
	pending, err = s.PrefixesPending()
	require.NoError(t, err)
	assert.Empty(t, pending)
}

func TestRestore_PrefixAmbiguityCollision(t *testing.T) {
	src, _ := newRestoreTestStore(t)
	addRestoreComp(t, src, "k2", "K2 Comp", "K2", MatchStatusScheduled, "A")
	addRestoreComp(t, src, "k02", "K02 Comp", "K02", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeTournament, "")

	s, _ := newRestoreTestStore(t)
	addRestoreComp(t, s, "live", "Live", "K", MatchStatusScheduled, "A")

	plan, err := s.PlanRestore(RestoreRequest{Archive: a})
	require.NoError(t, err)

	eK2 := planEntry(t, plan, "k2")
	assert.True(t, eK2.PrefixCollides, "K2 is ambiguous with live K and must collide")
	assert.Contains(t, plan.PrefixesPending, "k2")

	eK02 := planEntry(t, plan, "k02")
	assert.False(t, eK02.PrefixCollides, "K02 has a leading zero, is not ambiguous with K, and must not collide")
	assert.NotContains(t, plan.PrefixesPending, "k02")
}

func TestRestore_CopyNameMatchesLiveCompetitionName(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men-old", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men-old")

	// Copy named "men": slug "men" is free, but name "men" matches live competition's Name "Men" case-insensitively -> CopyNameTaken true.
	planTaken, err := s.PlanRestore(RestoreRequest{
		Archive:   a,
		Decisions: map[string]RestoreDecision{"men-old": {Action: RestoreCopy, CopyName: "men"}},
	})
	require.NoError(t, err)
	eTaken := planEntry(t, planTaken, "men-old")
	assert.True(t, eTaken.CopyNameTaken)

	// Copy named "Men Two": unique name and slug -> accepted.
	planAccepted, err := s.PlanRestore(RestoreRequest{
		Archive:   a,
		Decisions: map[string]RestoreDecision{"men-old": {Action: RestoreCopy, CopyName: "Men Two"}},
	})
	require.NoError(t, err)
	eAccepted := planEntry(t, planAccepted, "men-old")
	assert.False(t, eAccepted.CopyNameTaken)
	assert.Equal(t, "men-two", eAccepted.NewID)
}

func TestRestore_CopyNameTakesReplacedArchiveName(t *testing.T) {
	// Replaced archive competition name is taken:
	// Live competition id "open-championship" with Name "Open Championship";
	// archive holding "open-championship" under Name "Spring Open" and another archive competition "spring-old".
	src, _ := newRestoreTestStore(t)
	restoreTestTournament(t, src, "pw")
	addRestoreComp(t, src, "open-championship", "Spring Open", "O", MatchStatusScheduled, "A")
	addRestoreComp(t, src, "spring-old", "Spring Old", "S", MatchStatusScheduled, "B")
	a := archiveOf(t, src, ScopeTournament, "")

	s, _ := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "open-championship", "Open Championship", "O", MatchStatusScheduled, "A")
	addRestoreComp(t, s, "spring-old", "Spring Old", "S", MatchStatusScheduled, "B")

	// Decisions: open-championship Replace, spring-old Copy named "Spring Open".
	// Slug "spring-open" is free, so only the archive name check catches it -> CopyNameTaken true.
	planTaken, err := s.PlanRestore(RestoreRequest{
		Archive: a,
		Decisions: map[string]RestoreDecision{
			"open-championship": {Action: RestoreReplace},
			"spring-old":        {Action: RestoreCopy, CopyName: "Spring Open"},
		},
	})
	require.NoError(t, err)
	eTaken := planEntry(t, planTaken, "spring-old")
	assert.True(t, eTaken.CopyNameTaken)

	// A copy named after the replaced competition's old live name is accepted:
	// Using comp id "open-champ" so the slug "open-championship" is free.
	src2, _ := newRestoreTestStore(t)
	restoreTestTournament(t, src2, "pw")
	addRestoreComp(t, src2, "open-champ", "Spring Open", "O", MatchStatusScheduled, "A")
	addRestoreComp(t, src2, "spring-old", "Spring Old", "S", MatchStatusScheduled, "B")
	a2 := archiveOf(t, src2, ScopeTournament, "")

	s2, _ := newRestoreTestStore(t)
	restoreTestTournament(t, s2, "pw")
	addRestoreComp(t, s2, "open-champ", "Open Championship", "O", MatchStatusScheduled, "A")
	addRestoreComp(t, s2, "spring-old", "Spring Old", "S", MatchStatusScheduled, "B")

	planAccepted, err := s2.PlanRestore(RestoreRequest{
		Archive: a2,
		Decisions: map[string]RestoreDecision{
			"open-champ": {Action: RestoreReplace},
			"spring-old": {Action: RestoreCopy, CopyName: "Open Championship"},
		},
	})
	require.NoError(t, err)
	eAccepted := planEntry(t, planAccepted, "spring-old")
	assert.False(t, eAccepted.CopyNameTaken)
	assert.Equal(t, "open-championship", eAccepted.NewID)
}

func TestRestore_CopyNameRules(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	addRestoreComp(t, s, "women", "Women", "W", MatchStatusScheduled, "B")
	a := archiveOf(t, s, ScopeTournament, "")

	for name, decisions := range map[string]map[string]RestoreDecision{
		"empty name":         {"men": {Action: RestoreCopy, CopyName: "  "}, "women": {Action: RestoreReplace}},
		"name makes no id":   {"men": {Action: RestoreCopy, CopyName: "!!!"}, "women": {Action: RestoreReplace}},
		"name of a folder":   {"men": {Action: RestoreCopy, CopyName: "Women"}, "women": {Action: RestoreReplace}},
		"two copies, a slug": {"men": {Action: RestoreCopy, CopyName: "Same"}, "women": {Action: RestoreCopy, CopyName: "same"}},
	} {
		t.Run(name, func(t *testing.T) {
			before := folderFiles(t, dir)
			plan, err := s.PlanRestore(RestoreRequest{Archive: a, Decisions: decisions})
			require.NoError(t, err)
			taken := 0
			for _, e := range plan.Competitions {
				if e.CopyNameTaken {
					taken++
				}
			}
			assert.Equal(t, 1, taken)
			_, err = s.Restore(RestoreRequest{Archive: a, Decisions: decisions})
			assert.Equal(t, RestoreCodeCopyNameTaken, restoreCode(t, err))
			assert.Equal(t, before, folderFiles(t, dir))
			assertNoStaging(t, dir)
		})
	}
}

func TestRestore_NameClashWithInPlaceCompetitionIsRefused(t *testing.T) {
	// Example: live competition id "a" is named "Open", and the backup holds id "open" named "Open".
	// Restoring "open" as a create leaves two competitions named "Open", which must be refused.
	src, _ := newRestoreTestStore(t)
	restoreTestTournament(t, src, "pw")
	addRestoreComp(t, src, "open", "Open", "O", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeCompetition, "open")

	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "a", "Open", "A", MatchStatusScheduled, "A")

	before := folderFiles(t, dir)
	req := RestoreRequest{Archive: a}
	plan, err := s.PlanRestore(req)
	require.NoError(t, err)
	e := planEntry(t, plan, "open")
	assert.Equal(t, RestoreCreate, e.Action)
	assert.True(t, e.NameTaken, "Create with name matching live competition must mark NameTaken")

	_, err = s.Restore(req)
	require.Error(t, err)
	assert.Equal(t, RestoreCodeNameTaken, restoreCode(t, err))
	var re *RestoreError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, []string{"open"}, re.CompetitionIDs)
	assert.Equal(t, before, folderFiles(t, dir), "a refusal changes nothing")
	assertNoStaging(t, dir)
}

func TestRestore_LegitimateReplaceSameIDAndNameNotRefused(t *testing.T) {
	// A legitimate replace of the same competition (same id "open", same name "Open") is NOT refused.
	src, _ := newRestoreTestStore(t)
	restoreTestTournament(t, src, "pw")
	addRestoreComp(t, src, "open", "Open", "O", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeCompetition, "open")

	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "open", "Open", "O", MatchStatusCompleted, "A")
	setRestoreMatch(t, s, "open", MatchStatusCompleted, "A", "Alice")

	req := RestoreRequest{
		Archive:   a,
		Decisions: map[string]RestoreDecision{"open": {Action: RestoreReplace}},
	}
	plan, err := s.PlanRestore(req)
	require.NoError(t, err)
	e := planEntry(t, plan, "open")
	assert.Equal(t, RestoreReplace, e.Action)
	assert.False(t, e.NameTaken, "Legitimate replace of same competition must not be marked NameTaken")

	_, err = s.Restore(req)
	require.NoError(t, err)
	c, err := s.LoadCompetition("open")
	require.NoError(t, err)
	assert.Equal(t, "Open", c.Name)
	assertNoStaging(t, dir)
}

func TestRestore_TournamentScopeKeepsOrRemovesWhatTheArchiveLacks(t *testing.T) {
	for _, action := range []RestoreAction{RestoreKeep, RestoreRemove} {
		t.Run(string(action), func(t *testing.T) {
			s, dir := newRestoreTestStore(t)
			restoreTestTournament(t, s, "pw")
			addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
			a := archiveOf(t, s, ScopeTournament, "")
			addRestoreComp(t, s, "late", "Late", "L", MatchStatusScheduled, "B")
			lateBefore := filesUnder(folderFiles(t, dir), "competitions/late/")

			plan, err := s.PlanRestore(RestoreRequest{Archive: a})
			require.NoError(t, err)
			assert.True(t, plan.TournamentReplaced)
			assert.Equal(t, RestorePasswordKept, plan.Password)
			late := planEntry(t, plan, "late")
			assert.True(t, late.Asked)
			assert.Equal(t, []RestoreAction{RestoreKeep, RestoreRemove}, late.Choices)
			assert.ElementsMatch(t, []string{"men", "late"}, plan.Missing)

			_, err = s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
			assert.Equal(t, RestoreCodeDecisionMissing, restoreCode(t, err))

			s.getCompLock("late") // the entry exists before the restore
			_, err = s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{
				"men": {Action: RestoreReplace}, "late": {Action: action},
			}})
			require.NoError(t, err)
			ids, err := s.ListCompetitions()
			require.NoError(t, err)
			_, held := s.compMu.Load("late")
			if action == RestoreKeep {
				assert.ElementsMatch(t, []string{"men", "late"}, ids)
				assert.Equal(t, lateBefore, filesUnder(folderFiles(t, dir), "competitions/late/"), "a kept competition is byte-identical")
				assert.True(t, held)
			} else {
				assert.Equal(t, []string{"men"}, ids)
				assert.False(t, held, "a removed id loses its compMu entry")
			}
			tourn, err := s.LoadTournament()
			require.NoError(t, err)
			assert.Equal(t, "pw", tourn.Password, "the current password is kept, never the archive's")
			assertNoStaging(t, dir)
		})
	}
}

func TestRestore_PasswordRule(t *testing.T) {
	src, _ := newRestoreTestStore(t)
	restoreTestTournament(t, src, "old-secret")
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeTournament, "")

	t.Run("a fresh folder needs one", func(t *testing.T) {
		dst, dir := newRestoreTestStore(t)
		plan, err := dst.PlanRestore(RestoreRequest{Archive: a})
		require.NoError(t, err)
		assert.Equal(t, RestorePasswordNeedsOne, plan.Password)
		_, err = dst.Restore(RestoreRequest{Archive: a})
		assert.Equal(t, RestoreCodePasswordRequired, restoreCode(t, err))
		assertNoStaging(t, dir)
		_, err = dst.Restore(RestoreRequest{Archive: a, NewPassword: "new-secret"})
		require.NoError(t, err)
		tourn, err := dst.LoadTournament()
		require.NoError(t, err)
		assert.Equal(t, "new-secret", tourn.Password)
		assert.Equal(t, "Restore Taikai", tourn.Name)
	})
	t.Run("locked mode keeps it", func(t *testing.T) {
		dst, _ := newRestoreTestStore(t)
		plan, err := dst.PlanRestore(RestoreRequest{Archive: a, LockedMode: true})
		require.NoError(t, err)
		assert.Equal(t, RestorePasswordKept, plan.Password)
	})
	t.Run("a password changed while staging is the one kept", func(t *testing.T) {
		dst, _ := newRestoreTestStore(t)
		restoreTestTournament(t, dst, "first")
		restoreBeforeLock = func() { restoreTestTournament(t, dst, "changed-meanwhile") }
		defer func() { restoreBeforeLock = nil }()
		_, err := dst.Restore(RestoreRequest{Archive: a})
		require.NoError(t, err)
		tourn, err := dst.LoadTournament()
		require.NoError(t, err)
		assert.Equal(t, "changed-meanwhile", tourn.Password)
	})
}

func TestRestore_OnlyOneCompetitionOutOfATournamentArchive(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	addRestoreComp(t, s, "women", "Women", "W", MatchStatusScheduled, "B")
	a := archiveOf(t, s, ScopeTournament, "")
	require.NoError(t, s.DeleteCompetition("women"))
	tournBefore := folderFiles(t, dir)["tournament.md"]

	plan, err := s.PlanRestore(RestoreRequest{Archive: a, Only: "women"})
	require.NoError(t, err)
	assert.Equal(t, ScopeCompetition, plan.Scope)
	require.Len(t, plan.Competitions, 1)
	assert.Equal(t, RestoreCreate, plan.Competitions[0].Action)

	_, err = s.Restore(RestoreRequest{Archive: a, Only: "women"})
	require.NoError(t, err)
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"men", "women"}, ids)
	assert.Equal(t, tournBefore, folderFiles(t, dir)["tournament.md"])

	_, err = s.PlanRestore(RestoreRequest{Archive: a, Only: "nobody"})
	assert.Equal(t, RestoreCodeNotInArchive, restoreCode(t, err))
}

func TestRestore_UndoOnAManualArchiveIsRefused(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	_, err := s.PlanRestore(RestoreRequest{Archive: a, Undo: true})
	assert.Equal(t, RestoreCodeNotASafetyCopy, restoreCode(t, err))
	before := folderFiles(t, dir)
	_, err = s.Restore(RestoreRequest{Archive: a, Undo: true})
	assert.Equal(t, RestoreCodeNotASafetyCopy, restoreCode(t, err))
	assert.Equal(t, before, folderFiles(t, dir))
}

func TestRestore_RunningMatchesRefuseUnlessAllowed(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	setRestoreMatch(t, s, "men", MatchStatusRunning, "A", "")
	req := RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}}

	plan, err := s.PlanRestore(req)
	require.NoError(t, err)
	assert.Equal(t, []string{"Pool A-0"}, planEntry(t, plan, "men").RunningNow)
	_, err = s.Restore(req)
	assert.Equal(t, RestoreCodeMatchesRunning, restoreCode(t, err))
	req.AllowRunning = true
	_, err = s.Restore(req)
	require.NoError(t, err)
}

func TestRestore_ACopyWarnsAboutABusyShiaijo(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusRunning, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	plan, err := s.PlanRestore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreCopy, CopyName: "Men again"}}})
	require.NoError(t, err)
	var busy []RestoreWarning
	for _, w := range plan.Warnings {
		if w.Code == RestoreWarnCourtBusy {
			busy = append(busy, w)
		}
	}
	require.Len(t, busy, 1)
	assert.Equal(t, "A", busy[0].Court)
	assert.Equal(t, "men-again", busy[0].CompetitionID)
}

func TestRestore_AnUnreadableArchiveFileIsRefusedBeforeStaging(t *testing.T) {
	src, srcDir := newRestoreTestStore(t)
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "competitions", "men", "pool-matches.csv"), []byte("\"unterminated\n"), 0o600))
	a := archiveOf(t, src, ScopeCompetition, "men")

	dst, dir := newRestoreTestStore(t)
	_, err := dst.PlanRestore(RestoreRequest{Archive: a})
	assert.Equal(t, RestoreCodeFileUnreadable, restoreCode(t, err))
	var re *RestoreError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, "competitions/men/pool-matches.csv", re.Path)
	_, err = dst.Restore(RestoreRequest{Archive: a})
	assert.Equal(t, RestoreCodeFileUnreadable, restoreCode(t, err))
	assertNoStaging(t, dir)
}

func TestRestore_AQuarantinedFileIsRestoredAsBytes(t *testing.T) {
	src, srcDir := newRestoreTestStore(t)
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
	junk := []byte("{ not json at all")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "competitions", "men", "bracket.json.corrupt-20261103-100000"), junk, 0o600))
	a := archiveOf(t, src, ScopeCompetition, "men")

	dst, dir := newRestoreTestStore(t)
	_, err := dst.Restore(RestoreRequest{Archive: a})
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, "competitions", "men", "bracket.json.corrupt-20261103-100000"))
	require.NoError(t, err)
	assert.Equal(t, junk, got)
}

func TestRestore_BrandingOnOneSideOnly(t *testing.T) {
	src, srcDir := newRestoreTestStore(t)
	restoreTestTournament(t, src, "pw")
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, BrandingDirName), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, BrandingDirName, "logo.png"), []byte("LOGO"), 0o600))
	withBranding := archiveOf(t, src, ScopeTournament, "")

	dst, dir := newRestoreTestStore(t)
	restoreTestTournament(t, dst, "pw")
	_, err := dst.Restore(RestoreRequest{Archive: withBranding})
	require.NoError(t, err)
	got, err := os.ReadFile(filepath.Join(dir, BrandingDirName, "logo.png"))
	require.NoError(t, err)
	assert.Equal(t, "LOGO", string(got))

	plain, _ := newRestoreTestStore(t)
	restoreTestTournament(t, plain, "pw")
	without := archiveOf(t, plain, ScopeTournament, "")
	_, err = dst.Restore(RestoreRequest{Archive: without})
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, BrandingDirName))
	assert.True(t, os.IsNotExist(err), "a branding folder the archive lacks is removed")
	assertNoStaging(t, dir)
}

func TestRestore_PlanChangedWhenACompetitionAppearsMeanwhile(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeTournament, "")
	restoreBeforeLock = func() { addRestoreComp(t, s, "sneaky", "Sneaky", "S", MatchStatusScheduled, "B") }
	defer func() { restoreBeforeLock = nil }()
	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	assert.Equal(t, RestoreCodePlanChanged, restoreCode(t, err))
	assertNoStaging(t, dir)
}

func TestRestore_ACreateWhoseIDIsTakenMeanwhileIsRefused(t *testing.T) {
	src, _ := newRestoreTestStore(t)
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeCompetition, "men")
	dst, dir := newRestoreTestStore(t)
	restoreBeforeLock = func() { addRestoreComp(t, dst, "men", "Men", "M", MatchStatusScheduled, "A") }
	defer func() { restoreBeforeLock = nil }()
	_, err := dst.Restore(RestoreRequest{Archive: a})
	assert.Equal(t, RestoreCodePlanChanged, restoreCode(t, err))
	assertNoStaging(t, dir)
}

func TestRestore_PlanChangedWhenACompetitionIsDeletedMeanwhile(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeTournament, "")
	addRestoreComp(t, s, "late", "Late", "L", MatchStatusScheduled, "B")
	restoreBeforeLock = func() { require.NoError(t, s.DeleteCompetition("late")) }
	defer func() { restoreBeforeLock = nil }()
	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{
		"men": {Action: RestoreReplace}, "late": {Action: RestoreKeep},
	}})
	assert.Equal(t, RestoreCodePlanChanged, restoreCode(t, err))
	var re *RestoreError
	require.ErrorAs(t, err, &re)
	assert.Equal(t, []string{"late"}, re.CompetitionIDs)
	assertNoStaging(t, dir)
}

func TestRestore_LegacyArchiveIsUpgradedOnLoad(t *testing.T) {
	src, srcDir := newRestoreTestStore(t)
	addRestoreComp(t, src, "team", "Team", "T", MatchStatusScheduled, "A")
	legacy := []byte("squads:\n  p1:\n    - id: tm1\n      index: 1\n      name: M1\n")
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "competitions", "team", legacySquadsFilename), legacy, 0o600))
	snap, err := src.Snapshot(ScopeCompetition, "team")
	require.NoError(t, err)
	a := archiveFiles(t, backup.ScopeCompetition, backup.KindManual, snap, nil)
	require.Contains(t, a.Files, "competitions/team/"+legacySquadsFilename)

	// The folder already holds "team", checked for legacy shapes this
	// process: the restore must drop that mark so the archive's legacy file
	// is upgraded on its first load.
	dst, dir := newRestoreTestStore(t)
	addRestoreComp(t, dst, "team", "Team", "T", MatchStatusScheduled, "A")
	dst.EnsureLegacyUpgraded("team")
	_, err = dst.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"team": {Action: RestoreReplace}}})
	require.NoError(t, err)
	dst.EnsureLegacyUpgraded("team")
	members, err := dst.LoadSquads("team")
	require.NoError(t, err)
	require.Len(t, members["p1"], 1)
	assert.Equal(t, "M1", members["p1"][0].Name)
	_, err = os.Stat(filepath.Join(dir, "competitions", "team", teamMembersFilename))
	assert.NoError(t, err, "the legacy file is converged on load")
}

func TestRestore_Q1ABeforeRestoreCopyRestoredWithoutUndoAsksEveryChoice(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	other, _ := newRestoreTestStore(t)
	restoreTestTournament(t, other, "pw")
	addRestoreComp(t, other, "men", "Men", "M", MatchStatusCompleted, "A")
	addRestoreComp(t, other, "extra", "Extra", "E", MatchStatusScheduled, "B")
	a := archiveOf(t, other, ScopeTournament, "")

	var sc safetyCopy
	_, err := s.Restore(RestoreRequest{Archive: a, BeforeSwap: sc.hook(t),
		Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	require.NoError(t, err)
	require.NotNil(t, sc.archive)
	require.NotNil(t, sc.archive.Manifest.RestoreCreated)
	assert.Equal(t, []string{"extra"}, *sc.archive.Manifest.RestoreCreated)

	plan, err := s.PlanRestore(RestoreRequest{Archive: sc.archive})
	require.NoError(t, err)
	assert.True(t, planEntry(t, plan, "men").Asked)
	extra := planEntry(t, plan, "extra")
	assert.True(t, extra.Asked, "restoreCreated is read by nothing without undo")
	assert.Equal(t, []RestoreAction{RestoreKeep, RestoreRemove}, extra.Choices)
	_, err = s.Restore(RestoreRequest{Archive: sc.archive})
	assert.Equal(t, RestoreCodeDecisionMissing, restoreCode(t, err))
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.Contains(t, ids, "extra")
	assertNoStaging(t, dir)
}

func TestRestore_Q1AnEmptyBeforeRestoreCopyPlansNothingToRestore(t *testing.T) {
	src, _ := newRestoreTestStore(t)
	addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, src, ScopeCompetition, "men")
	dst, _ := newRestoreTestStore(t)
	var sc safetyCopy
	_, err := dst.Restore(RestoreRequest{Archive: a, BeforeSwap: sc.hook(t)})
	require.NoError(t, err)
	require.NotNil(t, sc.archive)
	assert.Empty(t, sc.archive.Manifest.Competitions)

	plan, err := dst.PlanRestore(RestoreRequest{Archive: sc.archive})
	require.NoError(t, err)
	assert.True(t, plan.NothingToRestore)
	assert.Contains(t, plan.Message, "delete it from its settings")
	assert.NotNil(t, plan.Competitions, "the plan lists no competition as [], never null")
	_, err = dst.Restore(RestoreRequest{Archive: sc.archive})
	assert.Equal(t, RestoreCodeNothingToRestore, restoreCode(t, err))
}

func TestRestore_UndoOfACreateOrCopyRemovesIt(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	var sc safetyCopy
	_, err := s.Restore(RestoreRequest{Archive: a, BeforeSwap: sc.hook(t),
		Decisions: map[string]RestoreDecision{"men": {Action: RestoreCopy, CopyName: "Men two"}}})
	require.NoError(t, err)
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"men", "men-two"}, ids)

	plan, err := s.PlanRestore(RestoreRequest{Archive: sc.archive, Undo: true})
	require.NoError(t, err)
	assert.Equal(t, RestoreRemove, planEntry(t, plan, "men-two").Action)
	assert.Empty(t, plan.Missing)
	_, err = s.Restore(RestoreRequest{Archive: sc.archive, Undo: true})
	require.NoError(t, err)
	ids, err = s.ListCompetitions()
	require.NoError(t, err)
	assert.Equal(t, []string{"men"}, ids)
	assertNoStaging(t, dir)
}

func TestRestore_UndoBringsBackARemovalKeepsAKeptOneAndCanBeUndone(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeTournament, "")
	addRestoreComp(t, s, "gone", "Gone", "G", MatchStatusCompleted, "B")
	addRestoreComp(t, s, "kept", "Kept", "K", MatchStatusScheduled, "B")
	goneBefore := filesUnder(folderFiles(t, dir), "competitions/gone/")

	first := safetyCopy{store: s}
	_, err := s.Restore(RestoreRequest{Archive: a, BeforeSwap: first.hook(t), Decisions: map[string]RestoreDecision{
		"men": {Action: RestoreReplace}, "gone": {Action: RestoreRemove}, "kept": {Action: RestoreKeep},
	}})
	require.NoError(t, err)
	require.NotEmpty(t, first.saved, "the hook saved the copy under the restore's locks without deadlocking")
	files, err := s.ListBackupFiles()
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.Empty(t, *first.archive.Manifest.RestoreCreated, "a restore that created nothing still writes an empty list")
	keptBefore := filesUnder(folderFiles(t, dir), "competitions/kept/")

	// A result recorded since the restore is listed as lost by the Undo.
	setRestoreMatch(t, s, "men", MatchStatusCompleted, "A", "Alice")
	plan, err := s.PlanRestore(RestoreRequest{Archive: first.archive, Undo: true})
	require.NoError(t, err)
	assert.Equal(t, RestoreCreate, planEntry(t, plan, "gone").Action)
	assert.Equal(t, RestoreReplace, planEntry(t, plan, "men").Action)
	assert.Equal(t, RestoreKeep, planEntry(t, plan, "kept").Action)
	assert.Equal(t, []string{"Pool A-0"}, planEntry(t, plan, "men").ResultsChanged)

	var second safetyCopy
	_, err = s.Restore(RestoreRequest{Archive: first.archive, Undo: true, BeforeSwap: second.hook(t)})
	require.NoError(t, err)
	assert.Equal(t, goneBefore, filesUnder(folderFiles(t, dir), "competitions/gone/"), "Undo brings the removed competition back")
	assert.Equal(t, keptBefore, filesUnder(folderFiles(t, dir), "competitions/kept/"), "Undo leaves a kept one alone")
	assert.Equal(t, []string{"gone"}, *second.archive.Manifest.RestoreCreated)

	// The Undo wrote its own safety copy, so it can be undone.
	_, err = s.Restore(RestoreRequest{Archive: second.archive, Undo: true})
	require.NoError(t, err)
	ids, err := s.ListCompetitions()
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"men", "kept"}, ids)
	ms, err := s.LoadPoolMatches("men")
	require.NoError(t, err)
	assert.Equal(t, MatchStatusCompleted, ms[0].Status, "undoing the Undo puts back the result recorded before it")
	assertNoStaging(t, dir)
}

func TestRestore_ABeforeSwapErrorChangesNothing(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	setRestoreMatch(t, s, "men", MatchStatusCompleted, "A", "Alice")
	before := folderFiles(t, dir)
	boom := errors.New("disk full")
	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}},
		BeforeSwap: func(RestoreSafetyCopy) error { return boom }})
	require.ErrorIs(t, err, boom)
	assert.Equal(t, before, folderFiles(t, dir))
	assertNoStaging(t, dir)
}

func TestRestore_ADecisionForNoQuestionIsRefused(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	_, err := s.PlanRestore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{
		"men": {Action: RestoreReplace}, "ghost": {Action: RestoreKeep},
	}})
	assert.Equal(t, RestoreCodeBadDecision, restoreCode(t, err))
	_, err = s.PlanRestore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreRemove}}})
	assert.Equal(t, RestoreCodeBadDecision, restoreCode(t, err))
}

func TestRestore_ReplacesCompetitionWithCorruptConfigMD(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	restoreTestTournament(t, s, "pw")

	// Create a valid competition first so we can take an archive to restore later.
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	archive := archiveOf(t, s, ScopeCompetition, "men")

	// Overwrite config.md with garbage that fails front matter parsing.
	corruptBytes := []byte("garbage not valid front matter\n")
	require.NoError(t, s.root.writeFile(s.compPath("men", "config.md"), corruptBytes, 0o600))

	// Plain Snapshot of that competition must still fail.
	_, compErr := s.Snapshot(ScopeCompetition, "men")
	require.Error(t, compErr)
	assert.Contains(t, compErr.Error(), "men")

	_, tournErr := s.Snapshot(ScopeTournament, "")
	require.Error(t, tournErr)
	assert.Contains(t, tournErr.Error(), "men")

	// Restore replacing the corrupt competition succeeds and writes the safety copy.
	var safetyArchiveBytes []byte
	var savedName string
	beforeSwap := func(sc RestoreSafetyCopy) error {
		assert.Equal(t, backup.KindBeforeRestore, sc.Kind)
		cfgKey := "competitions/men/config.md"
		assert.Equal(t, corruptBytes, sc.Snapshot.Files[cfgKey])
		assert.Equal(t, "men", sc.Snapshot.Competitions["men"])

		data := archiveBytes(t, backup.Scope(sc.Snapshot.Scope), sc.Kind, sc.Snapshot, sc.RestoreCreated)
		safetyArchiveBytes = data

		name, err := s.SaveBackupFile(BackupKindBeforeRestore, sc.Snapshot.Scope, time.Now().UTC(), data)
		if err != nil {
			return err
		}
		savedName = name
		return nil
	}

	_, err := s.Restore(RestoreRequest{
		Archive:    archive,
		Decisions:  map[string]RestoreDecision{"men": {Action: RestoreReplace}},
		BeforeSwap: beforeSwap,
	})
	require.NoError(t, err)

	// Live competition is now restored and valid.
	comp, err := s.LoadCompetition("men")
	require.NoError(t, err)
	assert.Equal(t, "Men", comp.Name)

	// Safety copy archive exists on disk and contains the corrupt bytes.
	require.NotEmpty(t, savedName)
	backups, err := s.ListBackupFiles()
	require.NoError(t, err)
	require.NotEmpty(t, backups)
	assert.Equal(t, savedName, backups[0].Name)

	zr, err := zip.NewReader(bytes.NewReader(safetyArchiveBytes), int64(len(safetyArchiveBytes)))
	require.NoError(t, err)
	var foundCorrupt bool
	for _, f := range zr.File {
		if f.Name == "competitions/men/config.md" {
			rc, err := f.Open()
			require.NoError(t, err)
			data, err := io.ReadAll(rc)
			require.NoError(t, err)
			require.NoError(t, rc.Close())
			assert.Equal(t, corruptBytes, data)
			foundCorrupt = true
			break
		}
	}
	assert.True(t, foundCorrupt, "safety copy archive must contain the corrupt config.md bytes")
	assertNoStaging(t, dir)
}

// A folder with no config.md cannot go into the safety copy, so a restore that
// would replace or remove it is refused before anything changes, in either scope.
func TestRestore_ConfigLessCompetitionFolderIsRefused(t *testing.T) {
	addBroken := func(t *testing.T, s *Store) {
		require.NoError(t, s.root.mkdirAll(s.compPath("broken"), 0o700))
		require.NoError(t, s.root.writeFile(s.compPath("broken", "participants.csv"), []byte("ID,Name,Dojo\n"), 0o600))
	}
	for _, tc := range []struct {
		name  string
		scope SnapshotScope
		id    string
	}{
		{"replaced in a competition restore", ScopeCompetition, "broken"},
		{"removed in a tournament restore", ScopeTournament, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src, _ := newRestoreTestStore(t)
			restoreTestTournament(t, src, "pw")
			addRestoreComp(t, src, "men", "Men", "M", MatchStatusScheduled, "A")
			if tc.scope == ScopeCompetition {
				addRestoreComp(t, src, "broken", "Broken", "B", MatchStatusScheduled, "A")
			}
			a := archiveOf(t, src, tc.scope, tc.id)

			s, dir := newRestoreTestStore(t)
			restoreTestTournament(t, s, "pw")
			addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
			addBroken(t, s)

			decisions := map[string]RestoreDecision{"broken": {Action: RestoreReplace}}
			if tc.scope == ScopeTournament {
				decisions = map[string]RestoreDecision{"men": {Action: RestoreReplace}, "broken": {Action: RestoreRemove}}
			}
			before := folderFiles(t, dir)
			_, err := s.Restore(RestoreRequest{Archive: a, Decisions: decisions})
			require.Error(t, err)
			assert.Equal(t, RestoreCodeFolderIncomplete, restoreCode(t, err))
			var re *RestoreError
			require.ErrorAs(t, err, &re)
			assert.Equal(t, []string{"broken"}, re.CompetitionIDs)
			assert.Equal(t, before, folderFiles(t, dir), "a refusal changes nothing")
			assertNoStaging(t, dir)
		})
	}
}

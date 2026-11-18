package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/backup"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the held competition folder roots (dataRoot.comps) against
// every path that moves, removes or re-creates a competition folder: after
// each, a save lands in the folder named competitions/<id> and never in one
// moved away, and no held root points anywhere else (assertHeldRootsAreLive).
// The saves go through SaveOverrides, which writes into the folder without
// creating it, so the held root (not the mkdirAll drop) is what they use.

// holdComp makes the store hold id's folder root, by a read under it.
func holdComp(t *testing.T, s *Store, id string) {
	t.Helper()
	_, err := s.root.stat(s.compPath(id, "config.md"))
	require.NoError(t, err)
	require.Contains(t, heldComps(s.root), id)
}

func rankMark(n int) *Overrides {
	return &Overrides{PoolRanks: map[string]map[string]int{"A": {"p1": n}}}
}

// rankMarkAt reads the mark in the overrides.json under folder, by path.
func rankMarkAt(t *testing.T, folder string) (int, error) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(folder, "overrides.json"))
	if err != nil {
		return 0, err
	}
	var o Overrides
	require.NoError(t, json.Unmarshal(data, &o))
	return o.PoolRanks["A"]["p1"], nil
}

func TestCompRoot_DeleteCompetitionDropsTheHeldRoot(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	holdComp(t, s, "c1")

	require.NoError(t, s.DeleteCompetition("c1"))
	assert.Empty(t, heldComps(s.root), "the delete drops the root")
	assert.Zero(t, compEntries(s.root))

	err := s.SaveOverrides("c1", rankMark(1))
	require.ErrorIs(t, err, fs.ErrNotExist, "a write to a deleted competition fails with ENOENT")
	assert.NotContains(t, err.Error(), dir+"/overrides.json", "the error names the competition's path")
	for p := range folderFiles(t, dir) {
		assert.False(t, strings.HasPrefix(p, "competitions/"), "nothing is left under competitions/: %s", p)
	}

	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1 again", Date: "01-01-2026"}))
	require.NoError(t, s.SaveOverrides("c1", rankMark(2)))
	got, err := rankMarkAt(t, s.compPath("c1"))
	require.NoError(t, err)
	assert.Equal(t, 2, got, "the save lands in the re-created folder")
	assertHeldRootsAreLive(t, s.root)
}

// restoreCrashingAt runs the journal scenario's restore over a folder whose
// "men" and "gone" roots are held, stopping it (as a crash would) right after
// the step for path of kind.
func restoreCrashingAt(t *testing.T, kind restoreStepKind, path string) (*Store, string) {
	t.Helper()
	a := journalScenarioArchive(t)
	s, dir := journalScenarioFolder(t)
	holdComp(t, s, "men")
	holdComp(t, s, "gone")
	afterRestoreStep = func(_ int, st restoreStep) error {
		if st.Kind == kind && st.Path == path {
			return errInjectedCrash
		}
		return nil
	}
	t.Cleanup(func() { afterRestoreStep = nil })
	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	require.ErrorIs(t, err, errInjectedCrash)
	afterRestoreStep = nil
	return s, dir
}

// movedOut is the folder a restore moved competition id out to, under its
// token folder.
func movedOut(t *testing.T, dir, id string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, backup.RestoreDirName, "*", restoreOldDir, "competitions", id))
	require.NoError(t, err)
	require.Len(t, matches, 1, "one moved-out copy of %s", id)
	return matches[0]
}

// The restore's moves out and in drop the held root: a save after the swap
// lands in the restored folder, and the moved-out copy (still there while the
// token folder is) does not change.
func TestCompRoot_ARestoreSwapDropsTheHeldRoot(t *testing.T) {
	s, dir := restoreCrashingAt(t, restoreMoveIn, "competitions/men")
	old := movedOut(t, dir, "men")
	before := folderFiles(t, old)
	assertHeldRootsAreLive(t, s.root)
	assert.NotContains(t, heldComps(s.root), "gone", "the folder moved out and not back is held no more")

	require.NoError(t, s.SaveOverrides("men", rankMark(7)))
	got, err := rankMarkAt(t, s.compPath("men"))
	require.NoError(t, err)
	assert.Equal(t, 7, got, "the save lands in the restored folder")
	assert.Equal(t, before, folderFiles(t, old), "the moved-out copy does not change")
	_, err = rankMarkAt(t, old)
	assert.ErrorIs(t, err, fs.ErrNotExist)

	err = s.SaveOverrides("gone", rankMark(8))
	assert.ErrorIs(t, err, fs.ErrNotExist, "a removed competition is not written through a root that followed it out")
	assert.Equal(t, before, folderFiles(t, old))
	assertHeldRootsAreLive(t, s.root)
}

// A restore that finishes removes its token folder, with the moved-out
// copies in it: no held root points into it, and a save lands in the live
// folder.
func TestCompRoot_ARestoreThatFinishesLeavesNoRootInItsTokenFolder(t *testing.T) {
	a := journalScenarioArchive(t)
	s, dir := journalScenarioFolder(t)
	holdComp(t, s, "men")
	holdComp(t, s, "gone")

	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: journalScenarioDecisions})
	require.NoError(t, err)
	assertNoStaging(t, dir)
	assertHeldRootsAreLive(t, s.root)

	require.NoError(t, s.SaveOverrides("men", rankMark(3)))
	got, err := rankMarkAt(t, s.compPath("men"))
	require.NoError(t, err)
	assert.Equal(t, 3, got)
	require.NoError(t, s.SaveOverrides("fresh", rankMark(4)))
	got, err = rankMarkAt(t, s.compPath("fresh"))
	require.NoError(t, err)
	assert.Equal(t, 4, got)
	assertHeldRootsAreLive(t, s.root)
}

// A restore a crash stopped after its first move rolls forward at the next
// start, which already holds the roots of the folders it probed for write
// access (checkDataWritable runs before the recovery): the recovery's moves
// drop them, so the started store holds roots only on the folders there now.
// (A root left on the moved-out men would also be caught later: the token
// folder's removal unlinks it, and the next call's ENOENT retry drops it. So
// this pins the outcome at startup; the swap test above and
// TestCompRoot_ARecoveryDropsTheRootsHeldOnTheFoldersItMoves fail without the
// drop.)
func TestCompRoot_AnInterruptedRestoreReplayedAtStartupDropsTheHeldRoots(t *testing.T) {
	_, dir := restoreCrashingAt(t, restoreMoveOut, "competitions/gone")
	_, err := os.Stat(filepath.Join(dir, "competitions", "men"))
	require.NoError(t, err, "the crash left men in place for the start to probe")

	recovered, err := NewStore(dir)
	require.NoError(t, err)
	assertNoStaging(t, dir)
	assertHeldRootsAreLive(t, recovered.root)

	require.NoError(t, recovered.SaveOverrides("men", rankMark(5)))
	got, err := rankMarkAt(t, recovered.compPath("men"))
	require.NoError(t, err)
	assert.Equal(t, 5, got)
	c, err := recovered.LoadCompetition("men")
	require.NoError(t, err)
	assert.Equal(t, "M", c.NumberPrefix, "the restored copy is the one read")
	assertHeldRootsAreLive(t, recovered.root)
}

// A competition's display name changes in place: the id and the folder stay,
// and a save after it lands in that folder.
func TestCompRoot_ARenamedCompetitionKeepsItsFolder(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	holdComp(t, s, "c1")
	_, err := s.UpdateCompetitionChanged("c1", func(c *Competition) (*Competition, error) { c.Name = "Renamed"; return c, nil })
	require.NoError(t, err)
	require.NoError(t, s.SaveOverrides("c1", rankMark(6)))
	got, err := rankMarkAt(t, s.compPath("c1"))
	require.NoError(t, err)
	assert.Equal(t, 6, got)
	c, err := s.LoadCompetition("c1")
	require.NoError(t, err)
	assert.Equal(t, "Renamed", c.Name)
	assertHeldRootsAreLive(t, s.root)
}

// A folder removed outside the app and put back (a copy from a backup) is
// written again without a restart: the held root answers ENOENT, sees the
// folder named competitions/<id> is another, and the call runs once more on
// it. With the folder still gone, the save fails with ENOENT as it always did.
func TestCompRoot_AFolderRemovedByHandAndPutBackIsWrittenAgain(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	holdComp(t, s, "c1")
	folder := s.compPath("c1")
	copyDir := filepath.Join(t.TempDir(), "c1")
	require.NoError(t, os.CopyFS(copyDir, os.DirFS(folder)))
	require.NoError(t, os.RemoveAll(folder))
	require.NoError(t, os.CopyFS(folder, os.DirFS(copyDir)))

	require.NoError(t, s.SaveOverrides("c1", rankMark(9)))
	got, err := rankMarkAt(t, folder)
	require.NoError(t, err)
	assert.Equal(t, 9, got, "the save lands in the copy put back")
	assertHeldRootsAreLive(t, s.root)

	require.NoError(t, os.RemoveAll(folder))
	err = s.SaveOverrides("c1", rankMark(10))
	require.ErrorIs(t, err, fs.ErrNotExist, "with the folder gone the save fails as before")
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe), "%v", err)
	assert.True(t, strings.HasPrefix(pe.Path, folder+string(filepath.Separator)), "the error names the store's path: %v", err)
	assert.Empty(t, heldComps(s.root))
	_, err = os.Stat(folder)
	assert.ErrorIs(t, err, fs.ErrNotExist, "nothing re-creates the folder")
	for p := range folderFiles(t, dir) {
		assert.False(t, strings.HasPrefix(p, "competitions/"), "nothing is left under competitions/: %s", p)
	}
}

// A folder removed outside the app and made again by SaveCompetition (the
// one door that creates a competition folder): the mkdirAll drops the held
// root, so config.md lands in the new folder. The mkdirAll is called alone
// first, since the ENOENT retry would also land the save: only the drop leaves
// no root held right after it.
func TestCompRoot_AFolderMadeAgainBySaveCompetitionIsTheOneWritten(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	holdComp(t, s, "c1")
	require.NoError(t, os.RemoveAll(s.compPath("c1")))

	require.NoError(t, s.root.mkdirAll(s.compPath("c1"), 0o700))
	assert.Empty(t, heldComps(s.root), "making the folder drops the root held on the one removed")

	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1 made again", Date: "01-01-2026"}))
	data, err := os.ReadFile(s.compPath("c1", "config.md"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "C1 made again")
	assertHeldRootsAreLive(t, s.root)
}

// heldRoot is the os.Root the store holds on competition id, nil when none.
func heldRoot(d *dataRoot, id string) *os.Root {
	v, ok := d.comps.entries.Load(id)
	if !ok {
		return nil
	}
	e := v.(*compRoot)
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.root
}

// A config.md save asks mkdirAll for a folder that is already there, changed
// bytes or not: the held root stays, so the saves do not throw it away.
func TestCompRoot_ASaveOfAFolderThatIsThereKeepsItsRoot(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	c := &Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}
	require.NoError(t, s.SaveCompetition(c))
	holdComp(t, s, "c1")
	held := heldRoot(s.root, "c1")
	require.NotNil(t, held)

	require.NoError(t, s.SaveCompetition(c), "a save whose bytes come out equal")
	assert.Same(t, held, heldRoot(s.root, "c1"), "a no-op save keeps the root")
	c.Name = "C1 renamed"
	require.NoError(t, s.SaveCompetition(c), "a save that changes the file")
	assert.Same(t, held, heldRoot(s.root, "c1"), "a changing save keeps the root")
	assertHeldRootsAreLive(t, s.root)
}

// A restore the crash stopped midway, rolled forward by the SAME store that
// holds roots on the folders it moves: the recovery's moves drop them, so no
// root points at a moved-out folder once it is done. The startup test above
// cannot tell, since the next call's ENOENT retry would drop a stale root too;
// here nothing calls between the recovery and the check.
func TestCompRoot_ARecoveryDropsTheRootsHeldOnTheFoldersItMoves(t *testing.T) {
	s, _ := restoreCrashingAt(t, restoreMoveOut, "competitions/gone")
	s.mu.Lock()
	err := s.recoverRestoresLocked()
	s.mu.Unlock()
	require.NoError(t, err)

	assert.NotContains(t, heldComps(s.root), "gone", "the folder moved out and not back is held no more")
	assertHeldRootsAreLive(t, s.root)
}

// Saves and reads on one competition run while another goroutine deletes and
// re-creates it, under -race: nothing deadlocks, a save that reports success
// is in the live folder unless a delete came after it, nothing lands outside
// the competition's folder, and at the end every held root is live.
func TestCompRoot_SavesAndReadsDuringDeletesAndRecreates(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	comp := &Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}
	require.NoError(t, s.SaveCompetition(comp))

	const rounds = 40
	var wg sync.WaitGroup
	stop := make(chan struct{})
	errs := make(chan error, 16)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < rounds; i++ {
			if err := s.DeleteCompetition("c1"); err != nil {
				errs <- err
				return
			}
			if err := s.SaveCompetition(&Competition{ID: "c1", Name: fmt.Sprintf("C1 %d", i), Date: "01-01-2026"}); err != nil {
				errs <- err
				return
			}
		}
	}()
	for g := 0; g < 3; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for n := 1; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				mark := g*100000 + n
				if err := s.SaveOverrides("c1", rankMark(mark)); err != nil {
					if !errors.Is(err, fs.ErrNotExist) {
						errs <- err
						return
					}
					continue
				}
				if _, err := s.LoadCompetition("c1"); err != nil && !errors.Is(err, fs.ErrNotExist) {
					errs <- err
					return
				}
				if _, err := s.root.exists(s.compPath("c1", "pools.csv")); err != nil {
					errs <- err
					return
				}
			}
		}(g)
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("saves, reads and deletes on one competition deadlocked")
	}
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	require.NoError(t, s.SaveOverrides("c1", rankMark(1)))
	got, err := rankMarkAt(t, s.compPath("c1"))
	require.NoError(t, err)
	assert.Equal(t, 1, got)
	for p := range folderFiles(t, dir) {
		if rest, ok := strings.CutPrefix(p, "competitions/"); ok {
			assert.True(t, strings.HasPrefix(rest, "c1/"), "a file lands only in the competition's folder: %s", p)
		}
	}
	assertHeldRootsAreLive(t, s.root)
}

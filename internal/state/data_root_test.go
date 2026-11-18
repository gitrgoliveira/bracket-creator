package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openTestRoot opens a dataRoot on dir, for a test that writes without a Store.
func openTestRoot(t *testing.T, dir string) *dataRoot {
	t.Helper()
	root, err := openDataRoot(dir)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, root.close()) })
	return root
}

// A symlink inside the data folder that points out of it is refused by the
// operating system, whatever the path the store built looks like.
func TestDataRoot_RefusesASymlinkOutOfTheDataFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)

	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "config.md"), []byte("---\nid: evil\n---\n"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "competitions", "evil")))

	path := s.compPath("evil", "config.md")
	_, err = s.root.readFile(path)
	require.Error(t, err)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe), "a refused read is a PathError: %v", err)
	assert.Equal(t, path, pe.Path, "the error names the path the store built")
	assert.True(t, isPathEscape(err), "the root's refusal reads %q, which the write check's remedy matches: %v", pathEscapesText, err)

	_, err = s.root.openFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	require.Error(t, err, "a write through the link is refused too")
}

// The WAL reads what a target held through the store's root (directReadWAL),
// so a log naming a file under a competition folder that links out of the
// data folder cannot read it: the read is refused, never followed, and a
// path outside the data folder is refused before the root is asked.
func TestDataRoot_TheWALReadsATargetThroughTheRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "pools.csv"), []byte("x"), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(dir, "competitions", "evil")))

	_, err = s.directReadWAL(s.compPath("evil", "pools.csv"))
	require.Error(t, err)
	assert.True(t, isPathEscape(err), "the read through the link is refused: %v", err)

	_, err = s.directReadWAL(filepath.Join(outside, "pools.csv"))
	require.ErrorContains(t, err, "outside data directory")

	inside := filepath.Join(dir, "inside.txt")
	require.NoError(t, os.WriteFile(inside, []byte("in"), 0o600))
	data, err := s.directReadWAL(inside)
	require.NoError(t, err)
	assert.Equal(t, "in", string(data))
}

// A data folder that is itself a symlink is followed when the root is opened,
// so the store works on the folder it points at.
func TestDataRoot_FollowsASymlinkedDataFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "data")
	require.NoError(t, os.Symlink(target, link))

	s, err := NewStore(link)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))

	got, err := s.LoadCompetition("c1")
	require.NoError(t, err)
	assert.Equal(t, "C1", got.Name)
	_, err = os.Stat(filepath.Join(target, "competitions", "c1", "config.md"))
	assert.NoError(t, err, "the file lands in the folder the link points at")
}

// An error from the root names the file as the store built it, not the
// folder-relative name the root was given.
func TestDataRoot_ErrorsNameThePathTheStoreBuilt(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)

	missing := filepath.Join(dir, "competitions", "nope.json")
	_, err := root.readFile(missing)
	require.ErrorIs(t, err, fs.ErrNotExist)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, missing, pe.Path)

	err = root.rename(missing, filepath.Join(dir, "other.json"))
	require.ErrorIs(t, err, fs.ErrNotExist)
	var le *os.LinkError
	require.True(t, errors.As(err, &le))
	assert.Equal(t, missing, le.Old)
	assert.Equal(t, filepath.Join(dir, "other.json"), le.New)
}

// An error from the root reads word for word as the same call by path did
// before the store had a root: "open <path>: ...", never "openat <name>: ...".
func TestDataRoot_ErrorsNameTheOperationAsByPath(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	missing := filepath.Join(dir, "competitions", "nope")
	underMissing := filepath.Join(missing, "child")

	for _, c := range []struct {
		op   string
		call func() error
	}{
		{"open", func() error { _, err := root.readFile(missing); return err }},
		{"open", func() error { _, err := root.open(missing); return err }},
		{"open", func() error { _, err := root.openFile(underMissing, os.O_WRONLY|os.O_CREATE, 0o600); return err }},
		{"open", func() error { _, err := root.readDir(missing); return err }},
		{"stat", func() error { _, err := root.stat(missing); return err }},
		{"mkdir", func() error { return root.mkdir(underMissing, 0o700) }},
		{"remove", func() error { return root.remove(missing) }},
	} {
		err := c.call()
		require.ErrorIs(t, err, fs.ErrNotExist)
		var pe *fs.PathError
		require.True(t, errors.As(err, &pe), "%v", err)
		assert.Equal(t, c.op, pe.Op, "%v", err)
		assert.Contains(t, err.Error(), c.op+" "+filepath.Join(dir, "competitions")+string(filepath.Separator), "%v", err)
	}

	err := root.rename(missing, filepath.Join(dir, "other"))
	var le *os.LinkError
	require.True(t, errors.As(err, &le), "%v", err)
	assert.Equal(t, "rename", le.Op)
	assert.Equal(t, "rename "+missing+" "+filepath.Join(dir, "other")+": "+le.Err.Error(), err.Error())
}

// An error raised on a file the root has already opened (reading a folder,
// listing a file) names the path the store built once, never with the data
// folder in front of it a second time.
func TestDataRoot_AnErrorOnAnOpenedFileNamesItsPathOnce(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	aDir := filepath.Join(dir, "competitions")
	require.NoError(t, os.Mkdir(aDir, 0o700))
	aFile := filepath.Join(dir, "tournament.md")
	require.NoError(t, os.WriteFile(aFile, []byte("---\n---\n"), 0o600))

	_, err := root.readFile(aDir)
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(err.Error(), dir), err.Error())
	assert.Contains(t, err.Error(), aDir+":", err.Error())

	_, err = root.readDir(aFile)
	require.Error(t, err)
	assert.Equal(t, 1, strings.Count(err.Error(), dir), err.Error())
	assert.Contains(t, err.Error(), aFile+":", err.Error())
}

// A path that cannot be put relative to the data folder (a relative path
// against an absolute folder) is refused before it reaches the root.
func TestDataRoot_RefusesAPathOutsideTheFolder(t *testing.T) {
	dir := t.TempDir()
	root := openTestRoot(t, dir)
	const rel = "competitions/c1/config.md"

	_, err := root.readFile(rel)
	require.Error(t, err)
	var pe *fs.PathError
	require.True(t, errors.As(err, &pe))
	assert.Equal(t, rel, pe.Path)

	_, err = root.open(rel)
	assert.Error(t, err)
	_, err = root.openFile(rel, os.O_RDONLY, 0)
	assert.Error(t, err)
	_, err = root.stat(rel)
	assert.Error(t, err)
	_, err = root.readDir(rel)
	assert.Error(t, err)
	assert.Error(t, root.mkdir(rel, 0o700))
	assert.Error(t, root.mkdirAll(rel, 0o700))
	assert.Error(t, root.remove(rel))
	assert.Error(t, root.removeAll(rel))
	assert.Error(t, root.rename(rel, filepath.Join(dir, "x")))
	assert.Error(t, root.rename(filepath.Join(dir, "x"), rel))

	err = root.rename(filepath.Join(dir, "..", "escape"), filepath.Join(dir, "x"))
	assert.Error(t, err, "a name that climbs out of the folder is refused by the root")
}

// seeds.csv is read through the root like every other store file: a link in
// the competition folder that points at a seeds file outside the data folder
// is refused, not followed (it used to be read by path).
func TestDataRoot_RefusesASeedsFileThatLinksOutOfTheDataFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))

	outside := filepath.Join(t.TempDir(), "seeds.csv")
	require.NoError(t, os.WriteFile(outside, []byte("Rank,Name,Dojo\n1,Outside Name,Somewhere\n"), 0o600))
	require.NoError(t, os.Symlink(outside, s.compPath("c1", "seeds.csv")))

	seeds, err := s.LoadSeedsRaw("c1")
	require.Error(t, err)
	assert.True(t, isPathEscape(err), "the root refuses the link: %v", err)
	assert.Empty(t, seeds, "nothing from the file outside the data folder is read")
}

// heldComps is the ids whose competition folder root the store holds now.
func heldComps(d *dataRoot) []string {
	var ids []string
	d.comps.Range(func(k, v any) bool {
		e := v.(*compRoot)
		e.mu.RLock()
		if e.root != nil {
			ids = append(ids, k.(string))
		}
		e.mu.RUnlock()
		return true
	})
	slices.Sort(ids)
	return ids
}

// compEntries counts the entries in the competition-root map, held or not.
func compEntries(d *dataRoot) int {
	n := 0
	d.comps.Range(func(any, any) bool { n++; return true })
	return n
}

// assertHeldRootsAreLive checks that every competition root the store holds
// is on the folder named competitions/<id> now, never on one moved away or
// removed.
func assertHeldRootsAreLive(t *testing.T, d *dataRoot) {
	t.Helper()
	d.comps.Range(func(k, v any) bool {
		id := k.(string)
		e := v.(*compRoot)
		e.mu.RLock()
		defer e.mu.RUnlock()
		if !assert.NotNil(t, e.root, "an entry in the map holds a root (%s)", id) {
			return true
		}
		held, err := e.root.Stat(".")
		require.NoError(t, err, "the held root of %s answers", id)
		live, err := os.Stat(filepath.Join(d.folder, "competitions", id))
		if assert.NoError(t, err, "a root is held only on a folder that is there (%s)", id) {
			assert.True(t, os.SameFile(held, live), "the held root of %s is on the folder named competitions/%s", id, id)
		}
		return true
	})
}

// A call on a path under a competition folder runs on that folder's own root,
// opened once and held: the second call finds it held.
func TestDataRoot_HoldsOneRootPerCompetitionFolder(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c2", Name: "C2", Date: "01-01-2026"}))
	root, err := openDataRoot(dir)
	require.NoError(t, err)
	comp := func(id string, name ...string) string {
		return filepath.Join(append([]string{dir, "competitions", id}, name...)...)
	}

	_, err = root.readFile(comp("c1", "config.md"))
	require.NoError(t, err)
	assert.Equal(t, []string{"c1"}, heldComps(root), "only the folder a call ran under is held")
	_, err = root.stat(comp("c1", "config.md"))
	require.NoError(t, err)
	_, err = root.stat(comp("c2", "config.md"))
	require.NoError(t, err)
	assert.Equal(t, []string{"c1", "c2"}, heldComps(root))

	_, err = root.stat(comp("c1"))
	require.NoError(t, err, "the folder itself runs on the data root")
	_, err = root.readDir(filepath.Join(dir, "competitions"))
	require.NoError(t, err)
	assert.Equal(t, 2, compEntries(root), "a call on competitions/ or a folder itself adds no entry")
	assertHeldRootsAreLive(t, root)

	require.NoError(t, root.close())
	assert.Zero(t, compEntries(root), "close closes and forgets every held root")
}

// The map keeps an entry only while its root is open: a load of a well-shaped
// id with no folder (any public 404) adds nothing, however often it is asked,
// nor does a competition "folder" that is a file.
func TestDataRoot_AnIDWithNoFolderLeavesNoEntry(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(s.folder, "competitions", "afile"), []byte("x"), 0o600))

	for i := 0; i < 1000; i++ {
		_, err := s.root.readFile(s.compPath("nope", "config.md"))
		require.ErrorIs(t, err, fs.ErrNotExist)
		_, err = s.LoadOverrides("nope")
		require.NoError(t, err)
		_, err = s.root.stat(s.compPath("afile", "config.md"))
		require.Error(t, err)
	}
	assert.Zero(t, compEntries(s.root), "no entry is kept for a folder that does not open")
}

// An error from a call on a competition folder's root names the file as the
// store built it, with the by-path operation word, exactly as one on the data
// root does.
func TestDataRoot_ErrorsUnderAHeldCompetitionRootNameThePathTheStoreBuilt(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	_, err = s.root.stat(s.compPath("c1", "config.md"))
	require.NoError(t, err)
	require.Equal(t, []string{"c1"}, heldComps(s.root))

	missing := s.compPath("c1", "nope.json")
	for _, c := range []struct {
		op   string
		call func() error
	}{
		{"open", func() error { _, err := s.root.readFile(missing); return err }},
		{"open", func() error { _, err := s.root.open(missing); return err }},
		{"open", func() error {
			_, err := s.root.openFile(filepath.Join(missing, "child"), os.O_WRONLY|os.O_CREATE, 0o600)
			return err
		}},
		{"open", func() error { _, err := s.root.readDir(missing); return err }},
		{"stat", func() error { _, err := s.root.stat(missing); return err }},
		{"mkdir", func() error { return s.root.mkdir(filepath.Join(missing, "child"), 0o700) }},
		{"remove", func() error { return s.root.remove(missing) }},
	} {
		err := c.call()
		require.ErrorIs(t, err, fs.ErrNotExist)
		var pe *fs.PathError
		require.True(t, errors.As(err, &pe), "%v", err)
		assert.Equal(t, c.op, pe.Op, "%v", err)
		assert.True(t, strings.HasPrefix(pe.Path, missing), "the error names %s, not a name relative to the competition folder: %v", missing, err)
		assert.Equal(t, 1, strings.Count(err.Error(), s.folder), "the data folder is named once: %v", err)
	}

	other := s.compPath("c1", "other.json")
	err = s.root.rename(missing, other)
	var le *os.LinkError
	require.True(t, errors.As(err, &le), "%v", err)
	assert.Equal(t, "rename "+missing+" "+other+": "+le.Err.Error(), err.Error())
	assert.Equal(t, []string{"c1"}, heldComps(s.root), "a missing file is not a missing folder: the root stays held")
}

// A competition folder that links out of the data folder is refused on its
// first open, and again when it takes the place of a folder whose root was
// held (the held root sees the old folder gone, is dropped, and the fresh
// open is refused like the first).
func TestDataRoot_AFolderLinkingOutIsRefusedBeforeAndAfterADrop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "config.md"), []byte("---\nid: c1\n---\n"), 0o600))

	require.NoError(t, os.Symlink(outside, s.compPath("evil")))
	_, err = s.root.readFile(s.compPath("evil", "config.md"))
	assert.True(t, isPathEscape(err), "first open: %v", err)
	assert.Empty(t, heldComps(s.root))

	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	_, err = s.root.readFile(s.compPath("c1", "config.md"))
	require.NoError(t, err)
	require.Equal(t, []string{"c1"}, heldComps(s.root))
	require.NoError(t, os.RemoveAll(s.compPath("c1")))
	require.NoError(t, os.Symlink(outside, s.compPath("c1")))

	_, err = s.root.readFile(s.compPath("c1", "config.md"))
	assert.True(t, isPathEscape(err), "after the drop: %v", err)
	_, err = s.root.openFile(s.compPath("c1", "pools.csv"), os.O_WRONLY|os.O_CREATE, 0o600)
	assert.True(t, isPathEscape(err), "a write through the link is refused too: %v", err)
	assert.Empty(t, heldComps(s.root))
	_, err = os.Stat(filepath.Join(outside, "pools.csv"))
	assert.ErrorIs(t, err, fs.ErrNotExist, "nothing is written outside the data folder")
}

// startMove starts a moveComps on id whose move lasts until release closes,
// and answers once the move has had time to take the entry's write lock (or,
// when a reader holds the read lock, to queue behind it). done closes when
// the move is over.
func startMove(d *dataRoot, id string, release <-chan struct{}) (done <-chan struct{}) {
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		_ = d.moveComps(func() error { <-release; return nil }, filepath.Join("competitions", id))
	}()
	time.Sleep(50 * time.Millisecond)
	return ch
}

// walkDir holds no competition entry's lock: a backup that reads each file
// from inside the walk does not deadlock against a move of that folder that
// queues mid-walk (a read lock held across the walk would make the inner
// read a recursive read lock behind the queued writer).
func TestDataRoot_AWalkReadingFilesDoesNotDeadlockAgainstAMove(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	_, err = s.root.stat(s.compPath("c1", "config.md"))
	require.NoError(t, err)

	finished := make(chan error, 1)
	go func() {
		files := map[string][]byte{}
		var moved <-chan struct{}
		release := make(chan struct{})
		err := s.root.walkDir(s.compPath("c1"), func(p string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if moved == nil {
				// A move of the folder starts while the walk is under way and
				// holds the folder for a moment, as a rename does; the
				// inner read below waits for it, then runs.
				moved = startMove(s.root, "c1", release)
				time.AfterFunc(100*time.Millisecond, func() { close(release) })
			}
			if entry.IsDir() {
				return nil
			}
			data, err := s.root.readFile(p)
			files[p] = data
			return err
		})
		<-moved
		if err == nil && len(files) == 0 {
			err = errors.New("the walk read no file")
		}
		finished <- err
	}()
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("a walk reading files deadlocked against a queued move")
	}
}

// rename resolves its two paths once: a rename with both ends in one
// competition folder, or one end in .wal, while a move of that folder waits
// behind a reader, finishes once the reader and the move are done.
func TestDataRoot_ARenameDoesNotDeadlockAgainstAQueuedMove(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1", Date: "01-01-2026"}))
	a, b := s.compPath("c1", "a.txt"), s.compPath("c1", "b.txt")
	w := filepath.Join(s.folder, ".wal", "x.txt")
	require.NoError(t, os.WriteFile(a, []byte("a"), 0o600))

	e, r := s.root.acquireComp("c1")
	require.NotNil(t, r)
	release := make(chan struct{})
	moved := startMove(s.root, "c1", release)

	finished := make(chan error, 1)
	go func() {
		err := s.root.rename(a, b)
		if err == nil {
			err = s.root.rename(b, w)
		}
		if err == nil {
			err = s.root.rename(w, a)
		}
		finished <- err
	}()
	time.Sleep(20 * time.Millisecond)
	e.mu.RUnlock()
	close(release)
	select {
	case err := <-finished:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("a rename deadlocked against a queued move")
	}
	<-moved
	got, err := os.ReadFile(a)
	require.NoError(t, err)
	assert.Equal(t, "a", string(got))
	assertHeldRootsAreLive(t, s.root)
}

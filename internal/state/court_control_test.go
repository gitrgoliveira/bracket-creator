package state

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func courtTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	return s
}

// captureWarn sends the default logger to a buffer for the test's length.
func captureWarn(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func heldCourt(tab, tablet string) CourtHold {
	return CourtHold{
		HolderKey:    CourtControlKey(tab),
		DeviceKey:    CourtControlKey(tablet),
		Device:       "ipad",
		Since:        1_700_000_000_000,
		StandingFrom: 1_700_000_000_500,
		PageID:       "page-1",
	}
}

func TestCourtControlKey_IsAOneWayKeyOfSixteenHexCharacters(t *testing.T) {
	id := "2f1c7a4e-8b3d-4c2a-9e1f-0a1b2c3d4e5f"
	k := CourtControlKey(id)
	assert.Len(t, k, 16)
	assert.True(t, isCourtControlKey(k))
	assert.Equal(t, k, CourtControlKey(id), "the same id gives the same key")
	assert.NotEqual(t, k, CourtControlKey(id+"x"))
	assert.NotContains(t, id, k)
	assert.Empty(t, CourtControlKey(""), "an empty id has no key")
}

func TestCourtDeviceWord_AnythingUnknownReadsOther(t *testing.T) {
	for _, w := range []string{"ipad", "android-tablet", "iphone", "android-phone", "mac", "windows", "other"} {
		assert.Equal(t, w, CourtDeviceWord(w))
	}
	for _, w := range []string{"", "iPad", "Ricardo's tablet", "<script>"} {
		assert.Equal(t, CourtDeviceOther, CourtDeviceWord(w), w)
	}
}

func TestLoadCourtControl_AMissingFileMeansNobodyRunsAnything(t *testing.T) {
	buf := captureWarn(t)
	s := courtTestStore(t)
	c := s.LoadCourtControl()
	require.NotNil(t, c.Courts)
	assert.Empty(t, c.Courts)
	assert.Empty(t, buf.String(), "a missing file is not worth a warning")
}

func TestSaveCourtControl_RoundTripsHoldersAndBothDisplacedLists(t *testing.T) {
	s := courtTestStore(t)
	a := heldCourt("tab-a", "tablet-1")
	a.AddDisplaced(CourtControlKey("tab-old"))
	a.AddDisplacedDevice(CourtControlKey("tablet-old"))
	free := CourtHold{}
	free.AddDisplaced(CourtControlKey("tab-lost"))
	free.AddDisplacedDevice(CourtControlKey("tablet-lost"))
	in := CourtControl{Courts: map[string]CourtHold{"A": a, "B": free}}

	require.NoError(t, s.SaveCourtControl(in, []string{"A", "B"}))
	got := s.LoadCourtControl()
	assert.Equal(t, in, got)
	assert.True(t, got.Courts["A"].Held())
	assert.False(t, got.Courts["B"].Held())
	assert.True(t, got.Courts["B"].IsDisplaced(CourtControlKey("tab-lost")))
	assert.True(t, got.Courts["B"].IsDisplacedDevice(CourtControlKey("tablet-lost")))
	assert.False(t, got.Courts["B"].IsDisplaced(CourtControlKey("tab-a")))

	raw, err := os.ReadFile(filepath.Join(s.folder, CourtControlFilename))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "tab-a", "the raw tab id is never stored")
	assert.NotContains(t, string(raw), "tablet-1", "the raw tablet id is never stored")
}

func TestSaveCourtControl_DropsAShiaijoTheTournamentNoLongerListsAndEmptyEntries(t *testing.T) {
	s := courtTestStore(t)
	in := CourtControl{Courts: map[string]CourtHold{
		"A": heldCourt("tab-a", "tablet-1"),
		"C": heldCourt("tab-c", "tablet-2"),
		"B": {},
	}}
	require.NoError(t, s.SaveCourtControl(in, []string{"A", "B"}))
	got := s.LoadCourtControl()
	assert.Equal(t, []string{"A"}, courtKeys(got), "C is not listed and B holds and remembers nobody")
	assert.Contains(t, in.Courts, "C", "the caller's value is left as it was")
}

func courtKeys(c CourtControl) []string {
	var out []string
	for k := range c.Courts {
		out = append(out, k)
	}
	return out
}

func TestCourtHold_DisplacedListsKeepTheNewestEightOnceEach(t *testing.T) {
	var h CourtHold
	for i := range 10 {
		h.AddDisplaced(CourtControlKey(fmt.Sprintf("tab-%d", i)))
		h.AddDisplacedDevice(CourtControlKey(fmt.Sprintf("tablet-%d", i)))
	}
	require.Len(t, h.Displaced, MaxCourtDisplaced)
	require.Len(t, h.DisplacedDevices, MaxCourtDisplaced)
	assert.False(t, h.IsDisplaced(CourtControlKey("tab-0")), "the oldest leaves first")
	assert.False(t, h.IsDisplaced(CourtControlKey("tab-1")))
	assert.True(t, h.IsDisplaced(CourtControlKey("tab-2")))
	assert.False(t, h.IsDisplacedDevice(CourtControlKey("tablet-1")))

	// A key displaced again moves to the newest place, so it outlives the rest.
	h.AddDisplaced(CourtControlKey("tab-2"))
	assert.Len(t, h.Displaced, MaxCourtDisplaced)
	assert.Equal(t, CourtControlKey("tab-2"), h.Displaced[len(h.Displaced)-1])

	h.RemoveDisplaced(CourtControlKey("tab-2"))
	assert.False(t, h.IsDisplaced(CourtControlKey("tab-2")))
	h.RemoveDisplacedDevice(CourtControlKey("tablet-9"))
	assert.False(t, h.IsDisplacedDevice(CourtControlKey("tablet-9")))
	assert.False(t, h.IsDisplaced(""), "an empty key is never displaced")
	h.AddDisplaced("")
	assert.Len(t, h.Displaced, MaxCourtDisplaced-1, "an empty key is never added")
}

func TestLoadCourtControl_AHandEditedFileIsReadAsTheSaveKeepsIt(t *testing.T) {
	s := courtTestStore(t)
	good := CourtControlKey("tab-a")
	dev := CourtControlKey("tablet-1")
	long := make([]string, 0, 12)
	for i := range 12 {
		long = append(long, fmt.Sprintf("%q", CourtControlKey(fmt.Sprintf("tab-%d", i))))
	}
	file := fmt.Sprintf(`{"courts": {
		"A": {"holderKey": %q, "deviceKey": %q, "device": "Ricardo's tablet", "since": -5, "pageId": "p"},
		"B": {"holderKey": "tab-raw-id", "deviceKey": %q, "displaced": ["not-a-key", %q, %q]},
		"C": {"holderKey": "", "displaced": [%s]},
		"D": {"holderKey": "NOT-HEX-AT-ALL!!", "deviceKey": %q}
	}}`, good, dev, dev, good, good, strings.Join(long, ","), dev)
	require.NoError(t, os.WriteFile(filepath.Join(s.folder, CourtControlFilename), []byte(file), 0o600))

	got := s.LoadCourtControl()
	assert.Equal(t, CourtDeviceOther, got.Courts["A"].Device, "free text never reaches another screen")
	assert.Equal(t, int64(0), got.Courts["A"].Since)
	assert.True(t, got.Courts["A"].Held())
	assert.False(t, got.Courts["B"].Held(), "a holder with a malformed key reads as free")
	assert.Equal(t, []string{good}, got.Courts["B"].Displaced, "malformed keys dropped, each key once")
	assert.Len(t, got.Courts["C"].Displaced, MaxCourtDisplaced)
	assert.NotContains(t, got.Courts, "D", "an entry left with nothing is dropped")
}

func TestLoadCourtControl_AFileThatCannotBeParsedIsReadAsEmptyWithAWarning(t *testing.T) {
	buf := captureWarn(t)
	s := courtTestStore(t)
	require.NoError(t, os.WriteFile(filepath.Join(s.folder, CourtControlFilename), []byte("{not json"), 0o600))
	c := s.LoadCourtControl()
	require.NotNil(t, c.Courts)
	assert.Empty(t, c.Courts)
	assert.Contains(t, buf.String(), "level=WARN")
	assert.Contains(t, buf.String(), CourtControlFilename)

	// The holder's next claim puts it back.
	require.NoError(t, s.SaveCourtControl(CourtControl{Courts: map[string]CourtHold{"A": heldCourt("t", "d")}}, []string{"A"}))
	assert.True(t, s.LoadCourtControl().Courts["A"].Held())
}

func TestLoadCourtControl_AFileThatCannotBeReadIsReadAsEmptyWithAWarning(t *testing.T) {
	buf := captureWarn(t)
	s := courtTestStore(t)
	require.NoError(t, os.Mkdir(filepath.Join(s.folder, CourtControlFilename), 0o700))
	c := s.LoadCourtControl()
	require.NotNil(t, c.Courts)
	assert.Empty(t, c.Courts)
	assert.Contains(t, buf.String(), "level=WARN")
}

func TestCourtControl_ALinkOutOfTheDataFolderIsNeverFollowed(t *testing.T) {
	captureWarn(t)
	s := courtTestStore(t)
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	secret := `{"courts": {"A": {"holderKey": "0123456789abcdef", "deviceKey": "0123456789abcdef"}}}`
	require.NoError(t, os.WriteFile(outside, []byte(secret), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(s.folder, CourtControlFilename)))

	assert.Empty(t, s.LoadCourtControl().Courts, "the root refuses a link that leaves the data folder")

	require.NoError(t, s.SaveCourtControl(CourtControl{Courts: map[string]CourtHold{"B": heldCourt("t", "d")}}, []string{"B"}))
	kept, err := os.ReadFile(outside)
	require.NoError(t, err)
	assert.Equal(t, secret, string(kept), "the save replaces the link, never writes through it")
	assert.True(t, s.LoadCourtControl().Courts["B"].Held())
}

func TestClearCourtControl_FreesEveryShiaijoAndForgetsTheDisplacedLists(t *testing.T) {
	s := courtTestStore(t)
	require.NoError(t, s.ClearCourtControl(), "clearing a file that is not there is no error")

	free := CourtHold{}
	free.AddDisplacedDevice(CourtControlKey("tablet-lost"))
	require.NoError(t, s.SaveCourtControl(CourtControl{Courts: map[string]CourtHold{
		"A": heldCourt("tab-a", "tablet-1"),
		"B": free,
	}}, []string{"A", "B"}))
	require.NoError(t, s.ClearCourtControl())

	assert.Empty(t, s.LoadCourtControl().Courts)
	_, err := os.Stat(filepath.Join(s.folder, CourtControlFilename))
	assert.True(t, os.IsNotExist(err))
}

func TestCourtControl_IsNotInATournamentBackup(t *testing.T) {
	s := courtTestStore(t)
	require.NoError(t, s.SaveCourtControl(CourtControl{Courts: map[string]CourtHold{"A": heldCourt("t", "d")}}, []string{"A"}))
	snap, err := s.Snapshot(ScopeTournament, "")
	require.NoError(t, err)
	for path := range snap.Files {
		assert.NotContains(t, path, CourtControlFilename)
	}
	assert.Contains(t, snap.Files, "tournament.md", "the snapshot ran and took the tournament files")
}

func TestCourtControl_CopyIsDeep(t *testing.T) {
	h := heldCourt("t", "d")
	h.AddDisplaced(CourtControlKey("x"))
	h.AddDisplacedDevice(CourtControlKey("y"))
	c := CourtControl{Courts: map[string]CourtHold{"A": h}}
	cp := c.Copy()
	cp.Courts["A"].Displaced[0] = "changed"
	cp.Courts["A"].DisplacedDevices[0] = "changed"
	cp.Courts["B"] = CourtHold{}
	assert.Equal(t, CourtControlKey("x"), c.Courts["A"].Displaced[0])
	assert.Equal(t, CourtControlKey("y"), c.Courts["A"].DisplacedDevices[0])
	assert.NotContains(t, c.Courts, "B")
}

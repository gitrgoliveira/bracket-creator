package state

import (
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// The locked loaders read through the parsed-file cache (loadCachedLocked), so
// a transaction pays no parse for a config.md the store already holds parsed.
// These tests pin the parse counts, the out-of-app edit, the round trip of the
// body a save publishes, and that nothing a caller gets back reaches the cache.

func newLockedCacheStore(t *testing.T, comp *Competition) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(comp))
	return s
}

// loadLocked runs loadCompetitionLocked under the competition's write lock,
// the way a transaction or a load-mutate-save body calls it.
func loadLocked(t *testing.T, s *Store, id string) *Competition {
	t.Helper()
	mu, err := s.lockCompWrite(id)
	require.NoError(t, err)
	defer mu.Unlock()
	c, err := s.loadCompetitionLocked(id)
	require.NoError(t, err)
	return c
}

func TestLockedCache_ALoadAfterASaveParsesNothing(t *testing.T) {
	s := newLockedCacheStore(t, &Competition{ID: "c", Name: "Open"})
	before := competitionParses.Load()
	for range 2 {
		c := loadLocked(t, s, "c")
		require.Equal(t, "Open", c.Name)
	}
	require.Zero(t, competitionParses.Load()-before, "the save published the body; the locked loader reads it")
}

func TestLockedCache_ATransactionParsesAnUnstagedConfigOnlyWhenCold(t *testing.T) {
	s := newLockedCacheStore(t, &Competition{ID: "c", Name: "Open"})

	readTwice := func() int64 {
		var parses int64
		require.NoError(t, s.WithTransaction("c", func(tx StoreTx) error {
			before := competitionParses.Load()
			for range 2 {
				c, err := tx.LoadCompetition("c")
				require.NoError(t, err)
				require.Equal(t, "Open", c.Name)
			}
			parses = competitionParses.Load() - before
			return nil
		}))
		return parses
	}

	require.Zero(t, readTwice(), "warm cache: no parse")
	// Cold: drop the cached bodies (NewStore's legacy sweep has already warmed
	// the cache through loadCompetitionLocked, so a fresh store is not cold).
	s.discardCompCacheBodies("c")
	require.Equal(t, int64(1), readTwice(), "cold cache: one parse, then the cache answers")
}

func TestLockedCache_AConfigRewrittenOutsideTheStoreIsReadAgain(t *testing.T) {
	s := newLockedCacheStore(t, &Competition{ID: "c", Name: "Open"})
	require.Equal(t, "Open", loadLocked(t, s, "c").Name)

	path := s.compPath("c", "config.md")
	info, err := os.Stat(path)
	require.NoError(t, err)
	raw, err := writeFrontMatter(&Competition{ID: "c", Name: "Edited by hand"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	// A rewrite inside the same clock tick keeps the mtime; move it on a
	// second so the test does not pass or fail by timing.
	later := info.ModTime().Add(time.Second)
	require.NoError(t, os.Chtimes(path, later, later))

	before := competitionParses.Load()
	require.Equal(t, "Edited by hand", loadLocked(t, s, "c").Name)
	require.Equal(t, int64(1), competitionParses.Load()-before)
}

// UpdateCompetitionChanged decides from the file as it is now, even when an
// edit kept the file's mtime and so left a stale body in the cache; and its
// read leaves the cache agreeing with the file.
func TestLockedCache_UpdateCompetitionChangedReadsTheFileAndHealsTheCache(t *testing.T) {
	s := newLockedCacheStore(t, &Competition{ID: "c", Name: "Open"})
	require.Equal(t, "Open", loadLocked(t, s, "c").Name)

	path := s.compPath("c", "config.md")
	info, err := os.Stat(path)
	require.NoError(t, err)
	raw, err := writeFrontMatter(&Competition{ID: "c", Name: "Edited by hand"})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, raw, 0600))
	require.NoError(t, os.Chtimes(path, info.ModTime(), info.ModTime()))
	require.Equal(t, "Open", loadLocked(t, s, "c").Name, "the kept mtime hides the edit from the cache")

	var seen string
	changed, err := s.UpdateCompetitionChanged("c", func(current *Competition) (*Competition, error) {
		seen = current.Name
		return nil, nil // no save
	})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, "Edited by hand", seen, "the transform decides from the file")
	require.Equal(t, "Edited by hand", loadLocked(t, s, "c").Name, "the read healed the cache")
}

// The body a save publishes must equal a parse of the bytes it wrote, or a
// transaction would now see something a fresh read would not. The bodies
// carry the known divergences: retired wire values folded on read, the
// retired whole-minute durations, the fields the file never holds, a nil
// Courts and empty omitempty lists; and one with every field set, so a field
// added later is checked too.
func TestLockedCache_TheSavedBodyEqualsAParseOfTheWrittenBytes(t *testing.T) {
	off := false
	everyField := &Competition{}
	fillEveryField(reflect.ValueOf(everyField).Elem(), "", 0)
	everyField.ID = "c"
	cases := map[string]*Competition{
		"retired values": {
			ID:                    "c",
			Name:                  "Open",
			Format:                "playoffs",
			Status:                CompetitionStatus("playoffs"),
			PoolMatchDuration:     3,
			KnockoutMatchDuration: 4,
			MatchDuration:         5,
			TwoThirdPlaces:        &off,
			FightingSpiritAwards:  []FightingSpiritAward{{Title: "Spirit", RecipientName: "Sato", RecipientDojo: "D"}},
			Players:               []domain.Player{{Name: "Sato", Dojo: "D"}},
			RosterVersion:         "r",
			ConfigVersion:         "v",
		},
		"empty lists": {
			ID:                   "c",
			Courts:               []string{},
			FightingSpiritAwards: []FightingSpiritAward{},
			RoundLineupsGiven:    map[string][]string{},
			RoundLineupsLegacy:   []string{},
		},
		"every field": everyField,
	}
	for name, comp := range cases {
		t.Run(name, func(t *testing.T) {
			s := newLockedCacheStore(t, comp)

			raw, err := os.ReadFile(s.compPath("c", "config.md"))
			require.NoError(t, err)
			want, err := parseCompetitionBytes(raw)
			require.NoError(t, err)
			cache := s.getFileCache("c", "config.md")
			cache.mu.RLock()
			published := cache.data
			cache.mu.RUnlock()
			require.Equal(t, want, published, "the published body is what a fresh read of the file gives")
			require.Equal(t, want, loadLocked(t, s, "c"))
		})
	}
}

func TestLockedCache_AChangedCopyNeverReachesTheCache(t *testing.T) {
	on := true
	s := newLockedCacheStore(t, &Competition{
		ID:                   "c",
		Name:                 "Open",
		TwoThirdPlaces:       &on,
		FightingSpiritAwards: []FightingSpiritAward{{Title: "Spirit", RecipientName: "Sato"}},
	})
	check := func(why string) {
		t.Helper()
		for _, c := range []*Competition{loadLocked(t, s, "c"), mustLoad(t, s)} {
			require.Equal(t, "Spirit", c.FightingSpiritAwards[0].Title, why)
			require.True(t, *c.TwoThirdPlaces, why)
		}
	}

	c := loadLocked(t, s, "c")
	c.FightingSpiritAwards[0].Title = "changed"
	*c.TwoThirdPlaces = false
	check("a locked load answers a copy")

	c = mustLoad(t, s)
	c.FightingSpiritAwards[0].Title = "changed"
	*c.TwoThirdPlaces = false
	check("LoadCompetition answers a copy")

	_, err := s.UpdateCompetitionChanged("c", func(current *Competition) (*Competition, error) {
		current.FightingSpiritAwards[0].Title = "changed"
		*current.TwoThirdPlaces = false
		return nil, nil // no save
	})
	require.NoError(t, err)
	check("UpdateCompetitionChanged hands transform a copy")
}

func mustLoad(t *testing.T, s *Store) *Competition {
	t.Helper()
	c, err := s.LoadCompetition("c")
	require.NoError(t, err)
	return c
}

// copyCompetition is deep: every slice, map and pointer, at every depth, is
// the copy's own. Adding a field that the copy would share fails this test.
func TestLockedCache_CopyCompetitionIsDeep(t *testing.T) {
	var src, want Competition
	fillEveryField(reflect.ValueOf(&src).Elem(), "", 0)
	fillEveryField(reflect.ValueOf(&want).Elem(), "", 0)
	require.NotEmpty(t, src.Players[0].Metadata[0], "the fill reaches the deepest field")
	require.NotNil(t, src.TwoThirdPlaces)
	require.NotEmpty(t, src.RoundLineupsGiven)

	cp := (&Store{}).copyCompetition(&src)
	require.Equal(t, want, *cp)
	changeEveryField(reflect.ValueOf(cp).Elem())
	require.NotEqual(t, want, *cp, "the change reached the copy")
	require.Equal(t, want, src, "a change through the copy reached the source")
}

// changeEveryField changes every value reachable from v in place: through
// pointers, slice elements and map values (written back under their key, so a
// shared slice inside a map value is changed in its shared array), and adds a
// key to every map.
func changeEveryField(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			changeEveryField(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Field(i).CanSet() {
				changeEveryField(v.Field(i))
			}
		}
	case reflect.Slice:
		for i := range v.Len() {
			changeEveryField(v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := reflect.New(v.Type().Elem()).Elem()
			e.Set(v.MapIndex(k))
			changeEveryField(e)
			v.SetMapIndex(k, e)
		}
		if v.Type().Key().Kind() == reflect.String {
			added := reflect.ValueOf("added by the copy").Convert(v.Type().Key())
			v.SetMapIndex(added, reflect.New(v.Type().Elem()).Elem())
		}
	case reflect.String:
		v.SetString(v.String() + "!")
	case reflect.Bool:
		v.SetBool(!v.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(v.Int() + 1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(v.Uint() + 1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(v.Float() + 1)
	default:
		panic("changeEveryField: no change for " + v.Kind().String())
	}
}

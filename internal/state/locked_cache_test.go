package state

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
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

// A read changes no bytes, so a no-op UpdateCompetitionChanged (the pool score
// path's MaybeAutoCompletePools, on almost every score) must not advance the
// config.md version the standings and court caches key on.
func TestLockedCache_ANoOpUpdateLeavesTheConfigVersion(t *testing.T) {
	s := newLockedCacheStore(t, &Competition{ID: "c", Name: "Open"})
	before := s.FileVersion("c", "config.md")
	changed, err := s.UpdateCompetitionChanged("c", func(*Competition) (*Competition, error) {
		return nil, nil
	})
	require.NoError(t, err)
	require.False(t, changed)
	require.Equal(t, before, s.FileVersion("c", "config.md"))
}

// The body a save publishes must equal a parse of the bytes it wrote, or a
// transaction would now see something a fresh read would not. The bodies
// carry the known divergences: retired wire values folded on read, the
// retired whole-minute durations, the fields the file never holds, a nil
// Courts and every empty omitempty list; and one with every field set, so a field
// added later is checked too.
func TestLockedCache_TheSavedBodyEqualsAParseOfTheWrittenBytes(t *testing.T) {
	off := false
	everyField := &Competition{}
	fillEveryField(reflect.ValueOf(everyField).Elem(), "", 0)
	everyField.ID = "c"
	// Every slice or map field written with omitempty, set to an empty, non-nil
	// value, found by reflection so a field added later is covered without
	// listing it here: the file holds no such list, so a parse gives nil, and
	// the published body must too. Courts is no omitempty field and comes back
	// empty, so it is set by hand.
	emptyLists := &Competition{ID: "c", Courts: []string{}}
	omitemptyLists := 0
	listFields := reflect.ValueOf(emptyLists).Elem()
	for i := range listFields.NumField() {
		field := listFields.Type().Field(i)
		kind := field.Type.Kind()
		if (kind == reflect.Slice || kind == reflect.Map) && strings.Contains(field.Tag.Get("yaml"), ",omitempty") {
			if kind == reflect.Slice {
				listFields.Field(i).Set(reflect.MakeSlice(field.Type, 0, 0))
			} else {
				listFields.Field(i).Set(reflect.MakeMap(field.Type))
			}
			omitemptyLists++
		}
	}
	require.NotZero(t, omitemptyLists, "the reflection found no omitempty list on Competition")
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
		"empty lists": emptyLists,
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
		if !v.IsNil() && v.Type().Key().Kind() == reflect.String {
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

// The other cached files of a competition: pool matches, pools, the bracket,
// the lineups and the team members, each read by its locked loader.

// lockedCacheFile is one cached file and the doors the tests below go through.
type lockedCacheFile struct {
	name   string
	file   string
	parses *atomic.Int64
	comp   *Competition
	// save publishes a body through the store's own saver.
	save func(t *testing.T, s *Store)
	// load is the locked loader; lockedLoad holds the write lock around it.
	load func(s *Store) (any, error)
	// decode parses the written bytes without touching the parse counters.
	decode func(raw []byte) (any, error)
	// edit is the file as written outside the app; edited checks the locked
	// loader read it.
	edit   string
	edited func(t *testing.T, got any)
}

// lockedDo runs fn under the competition's write lock, as a transaction or a
// load-mutate-save body runs.
func lockedDo(t *testing.T, s *Store, id string, fn func()) {
	t.Helper()
	mu, err := s.lockCompWrite(id)
	require.NoError(t, err)
	defer mu.Unlock()
	fn()
}

func lockedLoad(t *testing.T, s *Store, f lockedCacheFile) any {
	t.Helper()
	var got any
	lockedDo(t, s, "c", func() {
		var err error
		got, err = f.load(s)
		require.NoError(t, err)
	})
	return got
}

// publishedBody is the body the cache holds for file, as a saver published it.
func publishedBody(s *Store, file string) any {
	cache := s.getFileCache("c", file)
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cache.data
}

// decodeWritten parses the bytes the saver wrote.
func decodeWritten(t *testing.T, s *Store, f lockedCacheFile) any {
	t.Helper()
	raw, err := os.ReadFile(s.compPath("c", f.file))
	require.NoError(t, err)
	want, err := f.decode(raw)
	require.NoError(t, err)
	return want
}

func lockedCacheFiles() []lockedCacheFile {
	team := &Competition{ID: "c", Kind: "team", TeamSize: 3}
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)

	poolMatches := lockedCacheFile{
		file:   "pool-matches.csv",
		parses: &poolMatchesParses,
		comp:   team,
		load:   func(s *Store) (any, error) { return s.LoadPoolMatchesLocked("c") },
		decode: func(raw []byte) (any, error) { return parsePoolMatchesBytes(raw) },
		edit:   poolMatchesGolden,
		edited: func(t *testing.T, got any) {
			require.Len(t, got.([]MatchResult), 2)
		},
	}
	pools := lockedCacheFile{
		file:   "pools.csv",
		parses: &poolsParses,
		comp:   &Competition{ID: "c"},
		load:   func(s *Store) (any, error) { return s.loadPoolsLocked("c") },
		decode: func(raw []byte) (any, error) { return parsePoolsReader("pools.csv", bytes.NewReader(raw)) },
		edit:   "Pool Z,Ito,0,,F,,,p9\n",
		edited: func(t *testing.T, got any) {
			require.Equal(t, "Pool Z", got.([]helper.Pool)[0].PoolName)
		},
	}
	bracket := lockedCacheFile{
		file:   "bracket.json",
		parses: &bracketParses,
		comp:   team,
		load:   func(s *Store) (any, error) { return s.loadBracketLocked("c") },
		decode: func(raw []byte) (any, error) { return parseBracketBytes(raw) },
		edit:   `{"rounds":[[{"id":"Edited"}]]}`,
		edited: func(t *testing.T, got any) {
			require.Equal(t, "Edited", got.(*Bracket).Rounds[0][0].ID)
		},
	}
	lineups := lockedCacheFile{
		file:   teamLineupFilename,
		parses: &lineupParses,
		comp:   team,
		load:   func(s *Store) (any, error) { return s.loadTeamLineupsLocked("c") },
		decode: func(raw []byte) (any, error) { return decodeTeamLineupsBytes(raw) },
		edit:   "lineups:\n- teamId: t9\n  competitionId: c\n  round: 0\n  positions: {}\n",
		edited: func(t *testing.T, got any) {
			require.Contains(t, got.(map[string]domain.TeamLineup), "t9-0")
		},
	}
	members := lockedCacheFile{
		file:   teamMembersFilename,
		parses: &teamMembersParses,
		comp:   team,
		load:   func(s *Store) (any, error) { return s.loadSquadsLocked("c") },
		decode: func(raw []byte) (any, error) { return parseSquadsBytes(raw) },
		edit:   "members:\n  t9:\n  - id: m9\n    index: 1\n    name: Ito\n",
		edited: func(t *testing.T, got any) {
			require.Equal(t, "Ito", got.(map[string][]domain.TeamMember)["t9"][0].Name)
		},
	}

	saveLineups := func(lineups ...domain.TeamLineup) func(t *testing.T, s *Store) {
		return func(t *testing.T, s *Store) {
			byKey := map[string]domain.TeamLineup{}
			for _, l := range lineups {
				byKey[lineupStorageKey(l)] = l
			}
			lockedDo(t, s, "c", func() {
				require.NoError(t, s.saveTeamLineupsLocked("c", byKey, s.directWrite))
			})
		}
	}
	saveMembers := func(m map[string][]domain.TeamMember) func(t *testing.T, s *Store) {
		return func(t *testing.T, s *Store) {
			lockedDo(t, s, "c", func() {
				require.NoError(t, s.saveSquadsLocked("c", m, s.directWrite))
			})
		}
	}

	var files []lockedCacheFile
	add := func(f lockedCacheFile, name string, save func(t *testing.T, s *Store)) {
		f.name, f.save = f.file+"/"+name, save
		files = append(files, f)
	}

	add(poolMatches, "team match with bouts and stamps", func(t *testing.T, s *Store) {
		// The golden body (every stored column set, a team match with a bout
		// and stamps, and a bare scheduled match), plus a match carrying the
		// fields the file never holds, as a write path may hand them over.
		body := append(poolMatchesGoldenInput(), MatchResult{
			ID: "Pool B-3", SideA: "Nara", SideB: "Kobe", Status: MatchStatusScheduled,
			IpponsA: []string{}, Rev: 4, RevSession: "session", QueuePosition: 2,
			WinnerSide:       "A",
			DecidedByHantei:  bctest.HanteiExplicit(true),
			IneligibleSides:  &IneligibleSidesAnnotation{},
			WithdrawnStatus:  &WithdrawnStatusAnnotation{},
			ClearsWithdrawal: true, Changed: []string{GroupPoints}, WriteDoor: "score",
		})
		require.NoError(t, s.SavePoolMatches("c", body))
	})
	add(poolMatches, "values a read folds", func(t *testing.T, s *Store) {
		require.NoError(t, s.SavePoolMatches("c", []MatchResult{
			{
				ID: "Pool A-1", SideA: "Kyoto", SideB: "Osaka", Status: MatchStatusCompleted,
				SubResults: []SubMatchResult{}, Encho: &EnchoMetadata{},
				GroupStamps: map[string]int64{}, EventSeqs: map[string]int64{}, TurnedAway: map[string]string{},
				FlagsA: -1, ModifiedAt: -5, ScoreVersion: -2,
			},
			{ID: "Pool A-2", SideA: "Nara", SideB: "Kobe", SubResultsRaw: `[{"position":`, SubResultsUnreadable: true},
		}))
	})
	add(poolMatches, "nothing", func(t *testing.T, s *Store) {
		require.NoError(t, s.SavePoolMatches("c", nil))
	})
	add(pools, "players and fields the file never holds", func(t *testing.T, s *Store) {
		require.NoError(t, s.SavePools("c", []helper.Pool{
			{PoolName: "Pool A", Players: []helper.Player{
				{ID: "p1", Name: "Sato", DisplayName: "SATO", Dojo: "D", Seed: 1, Number: "K1",
					PoolPosition: 7, Metadata: []string{"3 dan"}, Source: "manual", CheckedIn: true, CheckInAt: 5},
				{ID: "p2", Name: "Mori", Dojo: "E", Seed: -1},
			}, Matches: []helper.Match{{SideA: &helper.Player{Name: "Sato"}, Round: 1}}},
			{PoolName: "Pool B"},
		}))
	})
	add(pools, "nothing", func(t *testing.T, s *Store) {
		require.NoError(t, s.SavePools("c", nil))
	})
	add(bracket, "rounds and a third-place match", func(t *testing.T, s *Store) {
		require.NoError(t, s.SaveBracket("c", &Bracket{
			Rounds: [][]BracketMatch{
				{{
					ID: "R1-1", SideA: "Sato", SideB: "Mori", SideAID: "p1", SideBID: "p2",
					Winner: "Sato", WinnerID: "p1", Status: MatchStatusCompleted,
					IpponsA: []string{"M", "K"}, IpponsB: []string{}, Feeders: []string{},
					SubResults:  []SubMatchResult{{Position: 1, SideA: "S1", IpponsA: []string{"D"}}},
					Encho:       &EnchoMetadata{PeriodCount: 1},
					GroupStamps: map[string]int64{GroupPoints: 1},
					MarkIDs:     &MarkIDs{A: []string{"e1"}},
					ModifiedAt:  9,
				}},
				{{ID: "R2-1", Feeders: []string{"R1-1", "R1-2"}, PlaceholderA: "Winner of R1-1"}},
			},
			ThirdPlaceMatch: &BracketMatch{ID: "3rd", SideA: "Ito", IpponsA: []string{"M"}, Feeders: []string{"R1-1"}},
			DrawOrder:       []string{"p1", "p2"},
			TimesSettled:    true,
		}))
	})
	add(bracket, "values a read folds", func(t *testing.T, s *Store) {
		require.NoError(t, s.SaveBracket("c", &Bracket{
			Rounds: [][]BracketMatch{{{
				ID: "R1-1", SideA: "Sato", SideB: "Mori",
				SubResults: []SubMatchResult{}, GroupStamps: map[string]int64{},
				EventSeqs: map[string]int64{}, TurnedAway: map[string]string{},
				IpponsA: []string{}, FlagsA: -1,
			}}},
			DrawOrder: []string{},
		}))
	})
	add(bracket, "nothing", func(t *testing.T, s *Store) {
		require.NoError(t, s.SaveBracket("c", &Bracket{}))
	})
	add(lineups, "history and a removal marker", saveLineups(
		domain.TeamLineup{
			TeamID: "t1", CompetitionID: "c", MatchID: "M1",
			Positions: map[domain.Position]string{p1: "Sato"},
			MemberIDs: map[domain.Position]string{p1: "m1"},
			Stamps:    map[domain.Position]int64{p1: 100},
			Own:       []domain.Position{p1},
			CopiedAt:  50, CopiedFrom: "M0",
			History: map[domain.Position][]domain.PositionChange{
				p1: {{Stamp: 90, Name: "Ito", MemberID: "m2"}, {Stamp: 100, Name: "Sato", MemberID: "m1"}},
			},
		},
		domain.TeamLineup{TeamID: "t2", CompetitionID: "c", MatchID: "M2", RemovedAt: 70, CopiedAt: 70},
		domain.TeamLineup{
			TeamID: "t3", CompetitionID: "c", Round: 1,
			Positions: map[domain.Position]string{p2: "Doi"},
			StartedAt: 30, StartedFrom: "M5",
		},
	))
	add(lineups, "lists and maps a read folds", saveLineups(domain.TeamLineup{
		TeamID: "t1", CompetitionID: "c", MatchID: "M1",
		Positions: map[domain.Position]string{},
		MemberIDs: map[domain.Position]string{}, Stamps: map[domain.Position]int64{},
		Own: []domain.Position{}, History: map[domain.Position][]domain.PositionChange{p1: nil, p2: {}},
	}))
	add(lineups, "nothing", saveLineups())
	add(members, "a member list", saveMembers(map[string][]domain.TeamMember{
		"t1": {{ID: "m1", Index: 1, Name: "Sato", ModifiedAt: 5}, {ID: "m2", Index: 2}},
		"t2": {},
		"t3": nil,
	}))
	add(members, "nothing", saveMembers(nil))
	return files
}

// A body a save published is served to the locked loader with no parse; a
// cold cache parses once, then serves.
func TestLockedCache_EachFileParsesOnlyWhenCold(t *testing.T) {
	for _, f := range lockedCacheFiles() {
		t.Run(f.name, func(t *testing.T) {
			s := newLockedCacheStore(t, f.comp)
			f.save(t, s)
			before := f.parses.Load()
			for range 2 {
				lockedLoad(t, s, f)
			}
			require.Zero(t, f.parses.Load()-before, "warm cache: no parse")

			s.discardCompCacheBodies("c")
			before = f.parses.Load()
			for range 2 {
				lockedLoad(t, s, f)
			}
			require.Equal(t, int64(1), f.parses.Load()-before, "cold cache: one parse, then the cache answers")
		})
	}
}

// The body each save publishes equals a parse of the bytes it wrote, so a
// transaction served from the cache sees what a fresh read would.
func TestLockedCache_EachSavedBodyEqualsAParseOfTheWrittenBytes(t *testing.T) {
	for _, f := range lockedCacheFiles() {
		t.Run(f.name, func(t *testing.T) {
			s := newLockedCacheStore(t, f.comp)
			f.save(t, s)
			want := decodeWritten(t, s, f)
			require.Equal(t, want, publishedBody(s, f.file), "the published body is what a fresh read of the file gives")
			require.Equal(t, want, lockedLoad(t, s, f))
		})
	}
}

// A file rewritten outside the app is read again by the locked loader.
func TestLockedCache_EachFileRewrittenOutsideTheStoreIsReadAgain(t *testing.T) {
	for _, f := range lockedCacheFiles() {
		t.Run(f.name, func(t *testing.T) {
			s := newLockedCacheStore(t, f.comp)
			f.save(t, s)
			lockedLoad(t, s, f)

			path := s.compPath("c", f.file)
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte(f.edit), 0600))
			// A rewrite inside the same clock tick keeps the mtime; move it on a
			// second so the test does not pass or fail by timing.
			later := info.ModTime().Add(time.Second)
			require.NoError(t, os.Chtimes(path, later, later))

			before := f.parses.Load()
			f.edited(t, lockedLoad(t, s, f))
			require.Equal(t, int64(1), f.parses.Load()-before)
		})
	}
}

// Nothing a caller changes in what a locked loader answers reaches the cache.
func TestLockedCache_EachChangedCopyNeverReachesTheCache(t *testing.T) {
	for _, f := range lockedCacheFiles() {
		t.Run(f.name, func(t *testing.T) {
			s := newLockedCacheStore(t, f.comp)
			f.save(t, s)
			want := decodeWritten(t, s, f)
			changeEveryField(reflect.ValueOf(lockedLoad(t, s, f)))
			require.Equal(t, want, lockedLoad(t, s, f))
			require.Equal(t, want, publishedBody(s, f.file))
		})
	}
}

var errRefusedWrite = errors.New("write refused by the test")

func refuseWrite(string, []byte, fs.FileMode) error { return errRefusedWrite }

// The load-mutate-save bodies load through the locked loaders: what their
// mutate changes reaches the cache only through a save. A refused write
// publishes nothing, so the cache still holds the file's body.
func TestLockedCache_AnUpdateBodyThatDoesNotSaveLeavesTheCache(t *testing.T) {
	files := lockedCacheFiles()
	find := func(name string) lockedCacheFile {
		for _, f := range files {
			if f.name == name {
				return f
			}
		}
		t.Fatalf("no file %s", name)
		return lockedCacheFile{}
	}
	pm := find("pool-matches.csv/team match with bouts and stamps")
	br := find("bracket.json/rounds and a third-place match")

	t.Run("updatePoolMatchByIDLocked", func(t *testing.T) {
		s := newLockedCacheStore(t, pm.comp)
		pm.save(t, s)
		want := decodeWritten(t, s, pm)
		lockedDo(t, s, "c", func() {
			found, err := s.updatePoolMatchByIDLocked("c", "Pool A-1", func(m *MatchResult) error {
				changeEveryField(reflect.ValueOf(m).Elem())
				return nil
			}, refuseWrite)
			require.True(t, found)
			require.ErrorIs(t, err, errRefusedWrite)
		})
		require.Equal(t, want, lockedLoad(t, s, pm))
	})
	t.Run("updateBracketLocked", func(t *testing.T) {
		s := newLockedCacheStore(t, br.comp)
		br.save(t, s)
		want := decodeWritten(t, s, br)
		lockedDo(t, s, "c", func() {
			err := s.updateBracketLocked("c", func(b *Bracket) error {
				changeEveryField(reflect.ValueOf(b).Elem())
				return nil
			}, refuseWrite)
			require.ErrorIs(t, err, errRefusedWrite)
		})
		require.Equal(t, want, lockedLoad(t, s, br))
	})
	t.Run("updateBracketMatchByIDLocked", func(t *testing.T) {
		s := newLockedCacheStore(t, br.comp)
		br.save(t, s)
		want := decodeWritten(t, s, br)
		lockedDo(t, s, "c", func() {
			for _, id := range []string{"R1-1", "3rd"} {
				found, err := s.updateBracketMatchByIDLocked("c", id, func(m *BracketMatch) {
					changeEveryField(reflect.ValueOf(m).Elem())
				}, refuseWrite)
				require.True(t, found)
				require.ErrorIs(t, err, errRefusedWrite)
			}
		})
		require.Equal(t, want, lockedLoad(t, s, br))
	})
}

// A cache hit skips the parse, and with it noteLineupStamps; the saver notes
// the stamps it writes, so the arrival stamp is still past every stamp the
// file holds after a load the cache served.
func TestLockedCache_ACachedLineupLoadKeepsTheArrivalStampAhead(t *testing.T) {
	const comp = "arrival-ahead"
	s := newLockedCacheStore(t, &Competition{ID: comp, Kind: "team", TeamSize: 3})
	ahead := time.Now().Add(24 * time.Hour).UnixMilli()
	l := domain.TeamLineup{
		TeamID: "t1", CompetitionID: comp,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Sato"},
		StartedAt: ahead,
	}
	before := lineupParses.Load()
	lockedDo(t, s, comp, func() {
		require.NoError(t, s.saveTeamLineupsLocked(comp, map[string]domain.TeamLineup{lineupStorageKey(l): l}, s.directWrite))
		got, err := s.loadTeamLineupsLocked(comp)
		require.NoError(t, err)
		require.Equal(t, ahead, got[lineupStorageKey(l)].StartedAt)
	})
	require.Zero(t, lineupParses.Load()-before, "the load was a cache hit")
	require.Greater(t, LineupArrivalStamp(comp), ahead)
}

// fillAllFields is fillEveryField with no depth cut short of the types here: a
// bout's ippons inside a bracket round sit deeper than fillEveryField reaches.
func fillAllFields(v reflect.Value, depth int) {
	if depth > 12 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fillAllFields(p.Elem(), depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillAllFields(s.Index(0), depth+1)
		v.Set(s)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		m := reflect.MakeMap(v.Type())
		val := reflect.New(v.Type().Elem()).Elem()
		fillAllFields(val, depth+1)
		m.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), val)
		v.Set(m)
	case reflect.Struct:
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				fillAllFields(v.Field(i), depth+1)
			}
		}
	default:
		fillEveryField(v, "", 0)
	}
}

// requireDeepCopy fills a value at every depth, copies it, changes everything
// reachable from the copy and checks the source did not move. settle states
// what the copy deliberately leaves out.
func requireDeepCopy[T any](t *testing.T, copyOf func(T) T, settle func(*T)) {
	t.Helper()
	var src, want, copied T
	for _, v := range []*T{&src, &want, &copied} {
		fillAllFields(reflect.ValueOf(v).Elem(), 0)
	}
	if settle != nil {
		settle(&copied)
	}
	cp := copyOf(src)
	require.Equal(t, copied, cp, "the copy holds what the source holds")
	changeEveryField(reflect.ValueOf(&cp).Elem())
	require.NotEqual(t, copied, cp, "the change reached the copy")
	require.Equal(t, want, src, "a change through the copy reached the source")
}

// Each copy helper the locked loaders and the savers use is deep: adding a
// field the copy would share fails here.
func TestLockedCache_TheCopyHelpersAreDeep(t *testing.T) {
	s := &Store{}
	t.Run("copyMatchResults", func(t *testing.T) {
		requireDeepCopy(t, s.copyMatchResults, func(rs *[]MatchResult) {
			for i := range *rs {
				(*rs)[i].ClearRequestFields()
			}
		})
	})
	t.Run("copyPools", func(t *testing.T) { requireDeepCopy(t, s.copyPools, nil) })
	t.Run("copyBracket", func(t *testing.T) { requireDeepCopy(t, s.copyBracket, nil) })
	t.Run("copyTeamLineups", func(t *testing.T) { requireDeepCopy(t, copyTeamLineups, nil) })
	t.Run("copySquads", func(t *testing.T) { requireDeepCopy(t, copySquads, nil) })
}

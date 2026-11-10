package state

import (
	"io/fs"
	"reflect"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureWrite returns a writeFn that keeps the bytes a saver writes, so a
// test builds stamped files in the store's own format without touching disk.
func captureWrite(out *[]byte) writeFn {
	return func(_ string, data []byte, _ fs.FileMode) error {
		*out = append([]byte(nil), data...)
		return nil
	}
}

func fingerprintTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	return s
}

func TestFingerprint_OrderAndFraming(t *testing.T) {
	a := map[string][]byte{"tournament.md": []byte("x"), "competitions/c/config.md": []byte("y")}
	b := map[string][]byte{}
	b["competitions/c/config.md"] = []byte("y")
	b["tournament.md"] = []byte("x")
	assert.Equal(t, Fingerprint(a), Fingerprint(b), "insertion order must not matter")
	assert.Len(t, Fingerprint(a), 64)

	assert.NotEqual(t, Fingerprint(map[string][]byte{"a": []byte("bc")}), Fingerprint(map[string][]byte{"ab": []byte("c")}),
		"a path and its content must be framed apart")
	assert.NotEqual(t, Fingerprint(map[string][]byte{"a": []byte("x"), "b": nil}), Fingerprint(map[string][]byte{"a": []byte("x")}),
		"an empty file is still a file")
	assert.NotEqual(t, Fingerprint(a), Fingerprint(map[string][]byte{"tournament.md": []byte("x"), "competitions/c/config.md": []byte("z")}),
		"different content")
	assert.NotEqual(t, Fingerprint(a), Fingerprint(map[string][]byte{"tournament.md": []byte("x"), "competitions/d/config.md": []byte("y")}),
		"a renamed path")
}

func TestFingerprint_HistoryAndEventsLeftOut(t *testing.T) {
	base := map[string][]byte{"competitions/c/config.md": []byte("y")}
	with := map[string][]byte{
		"competitions/c/config.md":              []byte("y"),
		"competitions/c/history/Pool A-0.jsonl": []byte(`{"stamp":1}` + "\n"),
		"competitions/c/events/Pool A-0.jsonl":  []byte(`{"v":1}` + "\n"),
	}
	assert.Equal(t, Fingerprint(base), Fingerprint(with))
	assert.ElementsMatch(t, []string{"history", "events"}, FingerprintSkippedDirs)

	// Only a competition's own history/ and events/ folders are left out.
	assert.NotEqual(t, Fingerprint(base), Fingerprint(map[string][]byte{
		"competitions/c/config.md":  []byte("y"),
		"branding/history/logo.png": []byte("png"),
	}))
	assert.NotEqual(t, Fingerprint(base), Fingerprint(map[string][]byte{
		"competitions/c/config.md":  []byte("y"),
		"competitions/c/history.md": []byte("not a folder"),
	}))
}

func TestFingerprint_UnparseableStampedFileHashedRaw(t *testing.T) {
	for _, sf := range FingerprintStampedFiles {
		t.Run(sf.Name, func(t *testing.T) {
			p := "competitions/c/" + sf.Name
			x := Fingerprint(map[string][]byte{p: []byte("{{{ not \x00 valid")})
			y := Fingerprint(map[string][]byte{p: []byte("{{{ not \x00 valid either")})
			assert.NotEqual(t, x, y, "a corrupt file is still content")
		})
	}
}

// stampCase builds one stamped file three ways: base, base with only its
// stamp fields moved, and base with a real field changed.
type stampCase struct {
	base, restamped, real func(t *testing.T, s *Store) []byte
}

func TestFingerprint_StampFieldsZeroed(t *testing.T) {
	const comp = "c"
	t1, t2 := int64(1_793_700_000_000), int64(1_793_799_999_999)

	poolMatch := func(stamp int64, version int, ippons []string) []MatchResult {
		return []MatchResult{{
			ID: "Pool A-0", SideA: "Sato Ken", SideB: "Ito Haru", SideAID: "pa", SideBID: "pb",
			Winner: "Sato Ken", WinnerID: "pa", IpponsA: ippons, IpponsB: []string{},
			Status: MatchStatusCompleted, Court: "A",
			ModifiedAt: stamp, GroupStamps: map[string]int64{"points": stamp, "result": stamp}, ScoreVersion: version,
		}}
	}
	savePool := func(results []MatchResult) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			var out []byte
			require.NoError(t, s.savePoolMatchesLocked(comp, results, captureWrite(&out)))
			return out
		}
	}
	bracket := func(stamp int64, version int, winner string) *Bracket {
		return &Bracket{Rounds: [][]BracketMatch{{{
			ID: "m-r1-0", SideA: "Sato Ken", SideB: "Ito Haru", SideAID: "pa", SideBID: "pb",
			Winner: winner, Status: MatchStatusCompleted, Court: "A",
			ModifiedAt: stamp, GroupStamps: map[string]int64{"result": stamp}, ScoreVersion: version,
		}}}}
	}
	saveBracket := func(b *Bracket) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			var out []byte
			require.NoError(t, s.saveBracketLocked(comp, b, captureWrite(&out)))
			return out
		}
	}
	status := func(at time.Time, reason string) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			var out []byte
			require.NoError(t, s.saveCompetitorStatusLocked(comp, map[string]domain.CompetitorStatus{
				"pa": {PlayerID: "pa", Eligible: false, Reason: reason, MatchID: "Pool A-0", RecordedAt: at},
			}, captureWrite(&out)))
			return out
		}
	}
	members := func(stamp int64, name string) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			var out []byte
			require.NoError(t, s.saveSquadsLocked(comp, map[string][]domain.TeamMember{
				"team-1": {{ID: "m1", Index: 1, Name: name, ModifiedAt: stamp}},
			}, captureWrite(&out)))
			return out
		}
	}
	lineup := func(stamp int64, history bool, senpo string) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			lu := domain.TeamLineup{
				TeamID: "team-1", CompetitionID: comp, Round: 0,
				Positions: map[domain.Position]string{"1": senpo},
				Stamps:    map[domain.Position]int64{"1": stamp},
			}
			if history {
				lu.History = map[domain.Position][]domain.PositionChange{"1": {{Stamp: t1, Name: senpo}}}
			}
			var out []byte
			require.NoError(t, s.saveTeamLineupsLocked(comp, map[string]domain.TeamLineup{lineupStorageKey(lu): lu}, captureWrite(&out)))
			return out
		}
	}

	roster := func(checkedIn bool, at int64, name string) func(t *testing.T, s *Store) []byte {
		return func(t *testing.T, s *Store) []byte {
			out, err := marshalParticipantsCSV([]domain.Player{
				{ID: "3f2504e0-4f89-41d3-9a0c-0305e82c3301", Name: name, Dojo: "Kyoto", CheckedIn: checkedIn, CheckInAt: at},
			}, false)
			require.NoError(t, err)
			return out
		}
	}

	cases := map[string]stampCase{
		"pool-matches.csv": {
			base:      savePool(poolMatch(t1, 2, []string{"M"})),
			restamped: savePool(poolMatch(t2, 3, []string{"M"})),
			real:      savePool(poolMatch(t2, 3, []string{"M", "K"})),
		},
		"bracket.json": {
			base:      saveBracket(bracket(t1, 2, "Sato Ken")),
			restamped: saveBracket(bracket(t2, 3, "Sato Ken")),
			real:      saveBracket(bracket(t2, 3, "Ito Haru")),
		},
		"participants.csv": {
			base:      roster(true, t1, "Tanaka Ichiro"),
			restamped: roster(true, t2, "Tanaka Ichiro"),
			real:      roster(true, t2, "Tanaka Jiro"),
		},
		competitorStatusFilename: {
			base:      status(time.UnixMilli(t1).UTC(), "kiken-injury"),
			restamped: status(time.UnixMilli(t2).UTC(), "kiken-injury"),
			real:      status(time.UnixMilli(t2).UTC(), "kiken-illness"),
		},
		teamMembersFilename: {
			base:      members(t1, "Aoi Senpo"),
			restamped: members(t2, "Aoi Senpo"),
			real:      members(t2, "Aoi Jiho"),
		},
		teamLineupFilename: {
			base:      lineup(t1, false, "Aoi Senpo"),
			restamped: lineup(t2, true, "Aoi Senpo"),
			real:      lineup(t2, true, "Aoi Jiho"),
		},
	}

	// Every entry of the table has a case here, and no case names a file the
	// table does not hold.
	require.Len(t, cases, len(FingerprintStampedFiles))
	for _, sf := range FingerprintStampedFiles {
		c, ok := cases[sf.Name]
		require.True(t, ok, "no case for stamped file %s", sf.Name)
		t.Run(sf.Name, func(t *testing.T) {
			s := fingerprintTestStore(t)
			p := "competitions/" + comp + "/" + sf.Name
			base, restamped, real := c.base(t, s), c.restamped(t, s), c.real(t, s)
			require.NotEqual(t, base, restamped, "the case must move the stamp bytes")
			fp := func(b []byte) string { return Fingerprint(map[string][]byte{p: b}) }
			assert.Equal(t, fp(base), fp(restamped), "only stamp fields moved")
			assert.NotEqual(t, fp(base), fp(real), "a real field changed")

			// Outside a competition folder the same bytes are hashed as they are.
			assert.NotEqual(t, Fingerprint(map[string][]byte{sf.Name: base}), Fingerprint(map[string][]byte{sf.Name: restamped}))
		})
	}
}

// The reader reads a marker only from a row of more than two data columns, the
// id column not counted; the fingerprint rewrites by the same rule, so a
// dojo that reads like a marker on a three-column row with an id is left alone.
func TestUnstampCheckInMarkers_FollowsTheReadersGuard(t *testing.T) {
	const id = "3f2504e0-4f89-41d3-9a0c-0305e82c3301"
	records := [][]string{
		{id, "Tanaka", "checked_in@123"},          // dojo, not a marker
		{id, "Tanaka", "Kyoto", "checked_in@123"}, // marker
		{id, "Tanaka", "Kyoto", "not_checked_in@123"},
		{"Tanaka", "checked_in@123"},          // dojo, no id
		{"Tanaka", "Kyoto", "checked_in@123"}, // marker, no id
	}
	unstampCheckInMarkers(records)
	assert.Equal(t, [][]string{
		{id, "Tanaka", "checked_in@123"},
		{id, "Tanaka", "Kyoto", "checked_in"},
		{id, "Tanaka", "Kyoto"},
		{"Tanaka", "checked_in@123"},
		{"Tanaka", "Kyoto", "checked_in"},
	}, records)
}

func TestFingerprint_ZeroNamedFieldsReachesNestedValues(t *testing.T) {
	type inner struct {
		ModifiedAt int64
		Keep       string
	}
	type outer struct {
		ModifiedAt int64
		List       []inner
		ByKey      map[string]inner
		Ptr        *inner
		at         int64
	}
	v := &outer{
		ModifiedAt: 1, List: []inner{{ModifiedAt: 2, Keep: "a"}},
		ByKey: map[string]inner{"k": {ModifiedAt: 3, Keep: "b"}}, Ptr: &inner{ModifiedAt: 4, Keep: "c"}, at: 5,
	}
	zeroNamedFields(reflect.ValueOf(v), map[string]bool{"ModifiedAt": true})
	assert.Equal(t, &outer{
		List: []inner{{Keep: "a"}}, ByKey: map[string]inner{"k": {Keep: "b"}}, Ptr: &inner{Keep: "c"}, at: 5,
	}, v)
}

func TestFingerprint_LineupParseHasNoSideEffects(t *testing.T) {
	s := fingerprintTestStore(t)
	const comp = "fp-pure-lineup"
	var raw []byte
	highStamp := time.Now().Add(24 * time.Hour).UnixMilli()
	require.NoError(t, s.saveTeamLineupsLocked("fp-pure-builder", map[string]domain.TeamLineup{
		"team-1\x00": {
			TeamID: "team-1", CompetitionID: comp,
			Positions: map[domain.Position]string{"1": "Aoi Senpo"},
			Stamps:    map[domain.Position]int64{"1": highStamp},
		},
	}, captureWrite(&raw)))

	beforeParses := lineupParses.Load()
	beforeStamp := lineupArrival(comp).Load()

	fp := Fingerprint(map[string][]byte{"competitions/" + comp + "/" + teamLineupFilename: raw})
	require.NotEmpty(t, fp)

	assert.Equal(t, beforeParses, lineupParses.Load(), "fingerprinting must not increment lineupParses")
	assert.Equal(t, beforeStamp, lineupArrival(comp).Load(), "fingerprinting must not advance the lineup arrival stamp")
}

func TestFingerprint_NoOpLineupRemovalLeavesFingerprintUnchanged(t *testing.T) {
	s := fingerprintTestStore(t)
	const comp = "fp-noop-remove"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp, Name: "C", Kind: "team", TeamSize: 3}))
	require.NoError(t, s.SetTeamLineups(comp, []domain.TeamLineup{{
		TeamID: "team-1", CompetitionID: comp,
		Positions: map[domain.Position]string{"1": "Aoi Senpo", "2": "Aoi Chuken", "3": "Aoi Taisho"},
	}}, 3))

	snap0, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)
	fp0 := Fingerprint(snap0.Files)

	removed, err := s.RemoveTeamLineupAt(comp, "team-2", "", 0, 0)
	require.NoError(t, err)
	assert.False(t, removed)

	snap1, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)
	require.NotEqual(t, snap0.Files["competitions/"+comp+"/"+teamLineupFilename], snap1.Files["competitions/"+comp+"/"+teamLineupFilename])
	assert.Equal(t, fp0, Fingerprint(snap1.Files))

	removed, err = s.RemoveTeamLineupAt(comp, "team-1", "", 0, 0)
	require.NoError(t, err)
	assert.True(t, removed)

	snap2, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)
	fp2 := Fingerprint(snap2.Files)
	assert.NotEqual(t, fp0, fp2)

	removed, err = s.RemoveTeamLineupAt(comp, "team-1", "", 0, 0)
	require.NoError(t, err)
	assert.False(t, removed)

	snap3, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)
	assert.Equal(t, fp2, Fingerprint(snap3.Files))
}

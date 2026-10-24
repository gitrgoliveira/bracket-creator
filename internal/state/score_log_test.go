package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

func scoreLogStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))
	return s, dir
}

func tapLine(id string) ScoreLogLine {
	return ScoreLogLine{Event: &domain.ScoreEvent{ID: id, Session: "s", Seq: 1, Stamp: 10, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "M"}}
}

// Every line carries the format version; a match id with a slash is escaped
// into one file name; the store and a transaction read the same lines.
func TestScoreLog_AppendAndLoad(t *testing.T) {
	s, dir := scoreLogStore(t)
	const mid = "Pool A/1"
	base := ScoreLogLine{Baseline: &ScoreBaseline{ID: "m0", Door: "migrate", Values: map[string]json.RawMessage{GroupPoints: json.RawMessage(`{"ipponsA":["M"],"ipponsB":[],"hansokuA":0,"hansokuB":0}`)}}}
	require.NoError(t, s.AppendScoreLog("c", mid, base, tapLine("e1")))
	require.NoError(t, s.WithTransaction("c", func(tx StoreTx) error {
		if err := tx.AppendScoreLog("c", mid, tapLine("e2")); err != nil {
			return err
		}
		got, err := tx.LoadScoreLog("c", mid)
		require.NoError(t, err)
		require.Len(t, got, 3, "the transaction reads its own line")
		return nil
	}))
	got, err := s.LoadScoreLog("c", mid)
	require.NoError(t, err)
	require.Len(t, got, 3)
	for _, l := range got {
		assert.Equal(t, ScoreLogFormatVersion, l.V)
	}
	assert.Equal(t, "m0", got[0].ID())
	assert.Equal(t, int64(10), got[1].Stamp())
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", "c", scoreLogDir, "Pool%20A%2F1.jsonl"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), `"v":1`)
	assert.NoError(t, s.AppendScoreLog("c", mid), "no lines writes nothing")
	none, err := s.LoadScoreLog("c", "other")
	require.NoError(t, err)
	assert.Empty(t, none)
}

// A line of an unknown format version, or one that does not parse, is an
// error naming the file and the line, never skipped or guessed at.
func TestScoreLog_UnreadableLineIsAnError(t *testing.T) {
	for _, c := range []struct{ name, line, want string }{
		{"newer version", `{"v":2,"event":{"id":"x","stamp":1,"kind":"encho.add"}}`, "format version 2"},
		{"no version", `{"event":{"id":"x","stamp":1,"kind":"encho.add"}}`, "format version 0"},
		{"damaged", `{"v":1,"event":`, "line 2"},
		{"empty line object", `{"v":1}`, "exactly one"},
		{"unreadable group value", `{"v":1,"baseline":{"id":"b","door":"x","stamp":1,"values":{"points":"nope"},"basis":{"seen":0}}}`, "group points"},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, dir := scoreLogStore(t)
			require.NoError(t, s.AppendScoreLog("c", "m", tapLine("e1")))
			path := filepath.Join(dir, "competitions", "c", scoreLogDir, "m.jsonl")
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
			require.NoError(t, err)
			_, err = f.WriteString(c.line + "\n")
			require.NoError(t, err)
			require.NoError(t, f.Close())
			_, err = s.LoadScoreLog("c", "m")
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrScoreLogUnreadable))
			assert.Contains(t, err.Error(), path, "the error names the file")
			assert.Contains(t, err.Error(), "line 2")
			assert.Contains(t, err.Error(), c.want)
		})
	}
}

func TestScoreLog_AppendRefusesBadLines(t *testing.T) {
	s, _ := scoreLogStore(t)
	assert.Error(t, s.AppendScoreLog("c", "", tapLine("e")))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{}))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{V: 2, Event: tapLine("e").Event}))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{Baseline: &ScoreBaseline{}}))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{Baseline: &ScoreBaseline{ID: "b", Values: map[string]json.RawMessage{"nope": nil}}}))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{Event: &domain.ScoreEvent{ID: "e"}}))
	assert.Error(t, s.AppendScoreLog("c", "m", ScoreLogLine{Baseline: &ScoreBaseline{ID: "b", Values: map[string]json.RawMessage{GroupPoints: json.RawMessage(`"nope"`)}}}))
	assert.Error(t, s.AppendScoreLog("../x", "m", tapLine("e")))
	assert.Error(t, s.WithTransaction("c", func(tx StoreTx) error { return tx.AppendScoreLog("other", "m", tapLine("e")) }))
	assert.Equal(t, "", ScoreLogLine{}.ID())
	assert.Equal(t, int64(0), ScoreLogLine{}.Stamp())
	_, err := s.LoadScoreLog("../x", "m")
	assert.Error(t, err)
	assert.NoError(t, s.DeleteScoreLog("c"))
	assert.Error(t, s.DeleteScoreLog("../x"))
}

// A score log line staged in a transaction whose WAL committed, the process
// dying before the WAL was applied, lands when the store starts again: the
// events file rides the WAL like the history (bc-evts).
func TestScoreLog_CrashAfterCommitReplaysTheLine(t *testing.T) {
	const mid = "Pool A-0"
	// The bytes a real append writes, taken from a scratch store.
	scratch, _ := scoreLogStore(t)
	require.NoError(t, scratch.AppendScoreLog("c", mid, tapLine("e1")))
	want, err := os.ReadFile(scratch.scoreLogPath("c", mid))
	require.NoError(t, err)

	s, dir := scoreLogStore(t)
	// The transaction's append creates the events folder as it stages.
	require.NoError(t, s.ensureScoreLogDirLocked("c"))
	w, err := wal.BeginTx(s.walDir, wal.NewWALID(), s.directWriteWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: s.scoreLogPath("c", mid), Data: want, Mode: 0o600})
	require.NoError(t, w.Commit())
	// The crash: committed, never applied.
	_, err = os.Stat(s.scoreLogPath("c", mid))
	require.True(t, os.IsNotExist(err), "nothing applied before the restart")

	restarted, err := NewStore(dir)
	require.NoError(t, err)
	got, err := restarted.LoadScoreLog("c", mid)
	require.NoError(t, err)
	require.Len(t, got, 1, "the replay on startup lands the line")
	assert.Equal(t, "e1", got[0].ID())
	entries, err := os.ReadDir(filepath.Join(dir, ".wal"))
	require.NoError(t, err)
	for _, e := range entries {
		assert.NotEqual(t, ".json", filepath.Ext(e.Name()), "the WAL is removed once replayed: %s", e.Name())
	}
}

// A baseline's values set each group back exactly as GroupValue projected it.
func TestSetGroupFromValue_RoundTrip(t *testing.T) {
	src := &MatchResult{
		IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"}, HansokuA: 1,
		Status: MatchStatusCompleted, Winner: "A", WinnerID: "a", Decision: "fought", CorrectionReason: "r",
		Encho: &EnchoMetadata{PeriodCount: 2}, FlagsA: 3, FlagsB: 2, RepPlayerA: "ra", RepPlayerB: "rb",
		SubResults: []SubMatchResult{
			{Position: 1, IpponsA: []string{"D"}, Winner: "A", Recorded: true},
			{Position: -1, SideA: "A", SideB: "B", SideAMemberID: "pa", SideBMemberID: "pb", Decision: "daihyosen"},
		},
	}
	dst := &MatchResult{SubResults: []SubMatchResult{{Position: 2}, {Position: -1}}}
	groups := []string{GroupPoints, GroupResult, GroupEncho, GroupFlags, GroupRep, BoutGroup(1), BoutGroup(-1), BoutGroup(2), GroupRepPickA, GroupRepPickB, GroupRecorded}
	for _, g := range groups {
		require.NoError(t, SetGroupFromValue(dst, g, GroupValue(src, g)), g)
	}
	for _, g := range groups {
		assert.False(t, GroupDiffers(src, dst, g), g)
	}
	assert.Equal(t, []int{1, -1}, SubPositions(dst.SubResults), "bout 2's null removed it")
	assert.Error(t, SetGroupFromValue(dst, "nope", nil))
	assert.Error(t, SetGroupFromValue(dst, GroupPoints, json.RawMessage(`[`)))
}

// fillEveryField sets every exported field of v, recursively, to a value that
// is not its zero, so a test built on it sees a field added to a type.
func fillEveryField(v reflect.Value, name string, depth int) {
	if depth > 4 || !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("x" + name)
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(1)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(1)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		fillEveryField(p.Elem(), name, depth+1)
		v.Set(p)
	case reflect.Slice:
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fillEveryField(s.Index(0), name, depth+1)
		v.Set(s)
	case reflect.Map:
		if v.Type().Key().Kind() != reflect.String {
			return
		}
		m := reflect.MakeMap(v.Type())
		val := reflect.New(v.Type().Elem()).Elem()
		fillEveryField(val, name, depth+1)
		m.SetMapIndex(reflect.ValueOf("k").Convert(v.Type().Key()), val)
		v.Set(m)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				fillEveryField(v.Field(i), v.Type().Field(i).Name, depth+1)
			}
		}
	}
}

// Every field of every group's projection is set from a match whose every
// field is set, and comes back through SetGroupFromValue: a field added to a
// group's projection without its inverse (or to a group's type without its
// projection) fails here.
func TestSetGroupFromValue_EveryFieldRoundTrips(t *testing.T) {
	var src MatchResult
	fillEveryField(reflect.ValueOf(&src).Elem(), "", 0)
	var row1, rep SubMatchResult
	fillEveryField(reflect.ValueOf(&row1).Elem(), "", 0)
	fillEveryField(reflect.ValueOf(&rep).Elem(), "", 0)
	row1.Position, rep.Position = 1, DaihyosenSubPosition
	src.SubResults = []SubMatchResult{row1, rep}

	// What a bout's value deliberately leaves out (match_groups.go): the
	// recorded mark is GroupRecorded's, the mark ids the replay's, and on the
	// representative bout the members are the pick groups'.
	notInBout := map[string]bool{"Recorded": true, "MarkIDs": true}
	notInRepBout := map[string]bool{"SideAMemberID": true, "SideBMemberID": true, "WinnerMemberID": true}
	groups := append(slices.Clone(ScalarGroups), ServerGroups...)
	groups = append(groups, BoutGroup(1), BoutGroup(DaihyosenSubPosition))
	for _, g := range groups {
		pos, isBout := ParseBoutGroup(g)
		proj := reflect.ValueOf(groupProjection(&src, g))
		if proj.Kind() == reflect.Pointer {
			require.False(t, proj.IsNil(), "%s projects a value", g)
			proj = proj.Elem()
		}
		for i := 0; i < proj.NumField(); i++ {
			f := proj.Type().Field(i)
			if !f.IsExported() || (isBout && notInBout[f.Name]) || (pos == DaihyosenSubPosition && notInRepBout[f.Name]) {
				continue
			}
			assert.False(t, proj.Field(i).IsZero(), "%s: %s is not projected", g, f.Name)
		}
		dst := &MatchResult{SubResults: []SubMatchResult{{Position: 1}, {Position: DaihyosenSubPosition}}}
		require.NoError(t, SetGroupFromValue(dst, g, GroupValue(&src, g)), g)
		assert.False(t, GroupDiffers(&src, dst, g), "%s does not round-trip", g)
	}
}

// The mark ids and the event-log version survive bracket.json, and the store
// hands out copies that do not share them.
func TestMarkIDsPersistAndAreCopied(t *testing.T) {
	s, dir := scoreLogStore(t)
	bm := BracketMatch{ID: "r0-m0", SideA: "A", SideB: "B", IpponsA: []string{"M"},
		MarkIDs: &MarkIDs{A: []string{"e1"}}, ScoreVersion: 4,
		SubResults: []SubMatchResult{{Position: 1, IpponsA: []string{"K"}, MarkIDs: &MarkIDs{A: []string{"e2"}}}}}
	tp := bm
	tp.ID = "bronze"
	require.NoError(t, s.SaveBracket("c", &Bracket{Rounds: [][]BracketMatch{{bm}}, ThirdPlaceMatch: &tp}))
	fresh, err := NewStore(dir)
	require.NoError(t, err)
	b, err := fresh.LoadBracket("c")
	require.NoError(t, err)
	got := b.Rounds[0][0]
	assert.Equal(t, bm.MarkIDs, got.MarkIDs)
	assert.Equal(t, 4, got.ScoreVersion)
	assert.Equal(t, []string{"e2"}, got.SubResults[0].MarkIDs.A)
	got.MarkIDs.A[0] = "changed"
	b.ThirdPlaceMatch.MarkIDs.A[0] = "changed"
	again, err := fresh.LoadBracket("c")
	require.NoError(t, err)
	assert.Equal(t, "e1", again.Rounds[0][0].MarkIDs.A[0], "a copy does not share the ids")
	assert.Equal(t, "e1", again.ThirdPlaceMatch.MarkIDs.A[0])

	require.NoError(t, s.SavePoolMatches("c", []MatchResult{{ID: "Pool A-1", MarkIDs: &MarkIDs{B: []string{"x"}}, ScoreVersion: 2}}))
	pm, err := s.LoadPoolMatches("c")
	require.NoError(t, err)
	pm[0].MarkIDs.B[0] = "changed"
	pm2, err := s.LoadPoolMatches("c")
	require.NoError(t, err)
	assert.Equal(t, "x", pm2[0].MarkIDs.B[0])
	assert.Equal(t, 2, pm2[0].ScoreVersion)
	assert.Nil(t, (*MarkIDs)(nil).Clone())
}

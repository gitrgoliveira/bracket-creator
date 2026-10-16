package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// benchTeamLog is a running 5-bout fixed-order team match's log: its
// migration baseline and n-1 taps spread over the bouts (strikes, removals,
// fouls, overtime), the shape plan D9 asks the cost of.
func benchTeamLog(n int) (*state.MatchResult, []state.ScoreLogLine) {
	m := &state.MatchResult{ID: "r0-m0", SideA: "Kyoto", SideB: "Osaka", SideAID: "ta", SideBID: "tb",
		Status: state.MatchStatusRunning, ModifiedAt: 1}
	for p := 1; p <= 5; p++ {
		m.SubResults = append(m.SubResults, state.SubMatchResult{Position: p})
	}
	b := MigrationBaseline(m, "m0", 0)
	lines := []state.ScoreLogLine{{V: state.ScoreLogFormatVersion, Baseline: &b}}
	for i := 1; len(lines) < n; i++ {
		id := "e" + strconv.Itoa(i)
		pos := i%5 + 1
		side := "a"
		if i%2 == 0 {
			side = "b"
		}
		e := &domain.ScoreEvent{ID: id, Session: "s" + side, Seq: int64(i), Stamp: int64(10 + i), Pos: pos, Side: side}
		switch i % 4 {
		case 0:
			e.Kind, e.Letter = domain.ScoreIpponAdd, "M"
		case 1:
			e.Kind = domain.ScoreFoulAdd
		case 2:
			e.Kind, e.Side = domain.ScoreEnchoAdd, ""
		case 3:
			e.Kind, e.Target, e.Side = domain.ScoreIpponRemove, "e"+strconv.Itoa(i-3), ""
		}
		lines = append(lines, state.ScoreLogLine{V: state.ScoreLogFormatVersion, Event: e})
	}
	return m, lines
}

// BenchmarkScoreEventAppendReplay is one tap's server cost on a match whose
// log already holds 149 lines: the transaction loads the log, appends the
// tap (staging the whole file through the WAL), commits, and replays the 150
// lines.
func BenchmarkScoreEventAppendReplay(b *testing.B) {
	dir := b.TempDir()
	store, err := state.NewStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	if err := store.SaveCompetition(&state.Competition{ID: "c", Name: "C", Kind: "team", TeamSize: 5}); err != nil {
		b.Fatal(err)
	}
	m, lines := benchTeamLog(150)
	if err := store.AppendScoreLog("c", m.ID, lines[:149]...); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "competitions", "c", "events", "r0-m0.jsonl")
	seed, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	shape := ReplayShape{Comp: &state.Competition{Kind: "team", TeamSize: 5}, Knockout: true}
	tap := lines[149]
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		if err := os.WriteFile(path, seed, 0o600); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		var r Replay
		err := store.WithTransaction("c", func(tx state.StoreTx) error {
			if err := tx.AppendScoreLog("c", m.ID, tap); err != nil {
				return err
			}
			got, err := tx.LoadScoreLog("c", m.ID)
			if err != nil {
				return err
			}
			r = ReplayScoring(m, got, shape)
			return nil
		})
		if err != nil {
			b.Fatal(err)
		}
		if r.Match.ScoreVersion != 150 {
			b.Fatalf("replayed %d lines", r.Match.ScoreVersion)
		}
	}
}

// BenchmarkReplayScoring150 is the replay alone, over the same 150 lines.
func BenchmarkReplayScoring150(b *testing.B) {
	m, lines := benchTeamLog(150)
	shape := ReplayShape{Comp: &state.Competition{Kind: "team", TeamSize: 5}, Knockout: true}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ReplayScoring(m, lines, shape)
	}
}

// BenchmarkScoreWriteOverLog150 is one /score write (a stamped running point)
// through the engine onto a knockout match whose score log already holds 150
// lines: the write's log reads and replays (bc-evts review round 11: the
// /score path read the log twice). Each iteration starts from the same files.
func BenchmarkScoreWriteOverLog150(b *testing.B) {
	seedDir := b.TempDir()
	store, err := state.NewStore(seedDir)
	if err != nil {
		b.Fatal(err)
	}
	const compID, matchID = "c", "m-r1-0"
	if err := store.SaveCompetition(&state.Competition{ID: compID, Name: "C", Kind: "individual", Status: state.CompStatusKnockout}); err != nil {
		b.Fatal(err)
	}
	t0 := time.Now().UnixMilli() - 3_600_000
	if err := store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: matchID, SideA: "Ryu", SideAID: "pa", SideB: "Tora", SideBID: "pb",
		Status: state.MatchStatusRunning, ModifiedAt: t0, MatchNumber: 1,
	}}}}); err != nil {
		b.Fatal(err)
	}
	write := func(eng *Engine, at int64, a []string) error {
		w := &state.MatchResult{ID: matchID, SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			ModifiedAt: at, Changed: []string{state.GroupPoints}, WriteDoor: DoorScore, IpponsA: a, IpponsB: []string{}}
		_, err := eng.RecordMatchResultWithIneligibility(compID, matchID, w)
		return err
	}
	eng := New(store)
	for i := range 150 {
		a := []string{}
		if i%2 == 0 {
			a = []string{"M"}
		}
		if err := write(eng, t0+int64(i+1)*1000, a); err != nil {
			b.Fatal(err)
		}
	}
	lines, err := store.LoadScoreLog(compID, matchID)
	if err != nil || len(lines) < 150 {
		b.Fatalf("seeded %d lines: %v", len(lines), err)
	}
	seed := os.DirFS(seedDir)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		dir := filepath.Join(b.TempDir(), "d")
		if err := os.CopyFS(dir, seed); err != nil {
			b.Fatal(err)
		}
		s, err := state.NewStore(dir)
		if err != nil {
			b.Fatal(err)
		}
		e := New(s)
		b.StartTimer()
		if err := write(e, t0+200_000, []string{"M", "K"}); err != nil {
			b.Fatal(err)
		}
	}
}

package engine

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

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

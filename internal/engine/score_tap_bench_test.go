package engine

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// tapLogSizes are the score-log lengths a tap is measured at: an individual
// match's few dozen, a team match's few hundred, and a long team encounter.
var tapLogSizes = []int{50, 300, 1000}

// benchTapEvent is the i-th tap of a team match: a point, a foul, an
// overtime period and a point taken back, round the five bouts.
func benchTapEvent(i int) domain.ScoreEvent {
	pos := i%5 + 1
	side := "a"
	if i%2 == 0 {
		side = "b"
	}
	e := domain.ScoreEvent{ID: "e" + strconv.Itoa(i), Session: "evs", Seq: int64(i), Stamp: 1_000_000 + int64(i), Pos: pos, Side: side}
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
	return e
}

// seedTapLog makes a data folder holding one running team match whose score
// log has n lines, every one of them landed through the score-events door.
func seedTapLog(b *testing.B, n int) (dir, compID, matchID string) {
	b.Helper()
	dir = b.TempDir()
	store, err := state.NewStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	compID, matchID = "c", "Pool A-0"
	if err := store.SaveCompetition(&state.Competition{ID: compID, Name: "C", Kind: "team", TeamSize: 5,
		Format: state.CompFormatLeague, Status: state.CompStatusPools}); err != nil {
		b.Fatal(err)
	}
	if err := store.SaveParticipants(compID, []domain.Player{{ID: "ta", Name: "Kyoto", Dojo: "K"}, {ID: "tb", Name: "Osaka", Dojo: "O"}}); err != nil {
		b.Fatal(err)
	}
	m := state.MatchResult{ID: matchID, SideA: "Kyoto", SideAID: "ta", SideB: "Osaka", SideBID: "tb", Status: state.MatchStatusRunning}
	for p := 1; p <= 5; p++ {
		m.SubResults = append(m.SubResults, state.SubMatchResult{Position: p})
	}
	if err := store.SavePoolMatches(compID, []state.MatchResult{m}); err != nil {
		b.Fatal(err)
	}
	eng := New(store)
	// The first batch also logs the migration baseline (one line).
	for i := 1; i < n; {
		var batch []domain.ScoreEvent
		for ; i < n && len(batch) < 64; i++ {
			batch = append(batch, benchTapEvent(i))
		}
		if _, err := eng.RecordScoreEvents(compID, matchID, ScoreEventsRequest{Session: "evs", Events: batch}); err != nil {
			b.Fatal(err)
		}
	}
	lines, err := store.LoadScoreLog(compID, matchID)
	if err != nil || len(lines) != n {
		b.Fatalf("seeded %d lines, want %d: %v", len(lines), n, err)
	}
	return dir, compID, matchID
}

// BenchmarkTapOverLog is one tap (a one-event score-events batch) on a team
// match whose log already holds n lines: the whole door, transaction and
// fsyncs included.
func BenchmarkTapOverLog(b *testing.B) {
	for _, n := range tapLogSizes {
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			seedDir, compID, matchID := seedTapLog(b, n)
			seed := os.DirFS(seedDir)
			// The session's next tap, an overtime period: it always applies.
			tap := benchTapEvent(n)
			tap.Kind, tap.Side, tap.Letter, tap.Target = domain.ScoreEnchoAdd, "", "", ""
			log.SetOutput(io.Discard)
			b.Cleanup(func() { log.SetOutput(os.Stderr) })
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
				// A warm store, as on a live day: the competition and the
				// matches are already read once.
				if _, err := s.LoadCompetition(compID); err != nil {
					b.Fatal(err)
				}
				if _, err := s.LoadPoolMatches(compID); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				res, err := e.RecordScoreEvents(compID, matchID, ScoreEventsRequest{Session: "evs", Events: []domain.ScoreEvent{tap}})
				if err != nil || !res.Written {
					b.Fatalf("tap not written: %v", err)
				}
			}
		})
	}
}

// BenchmarkScoreLogAppendTx is the store's share of a tap: one line appended
// to an n-line log inside a transaction, committed (the WAL and the file).
func BenchmarkScoreLogAppendTx(b *testing.B) {
	for _, n := range tapLogSizes {
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			dir := b.TempDir()
			store, err := state.NewStore(dir)
			if err != nil {
				b.Fatal(err)
			}
			if err := store.SaveCompetition(&state.Competition{ID: "c", Name: "C", Kind: "team", TeamSize: 5}); err != nil {
				b.Fatal(err)
			}
			m, lines := benchTeamLog(n + 1)
			if err := store.AppendScoreLog("c", m.ID, lines[:n]...); err != nil {
				b.Fatal(err)
			}
			path := filepath.Join(dir, "competitions", "c", "events", "r0-m0.jsonl")
			seed, err := os.ReadFile(path)
			if err != nil {
				b.Fatal(err)
			}
			tap := lines[n]
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				if err := os.WriteFile(path, seed, 0o600); err != nil {
					b.Fatal(err)
				}
				b.StartTimer()
				if err := store.WithTransaction("c", func(tx state.StoreTx) error {
					return tx.AppendScoreLog("c", m.ID, tap)
				}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkReplayScoringN is one full replay of an n-line team log, the unit
// a tap pays several times.
func BenchmarkReplayScoringN(b *testing.B) {
	for _, n := range tapLogSizes {
		b.Run(fmt.Sprintf("lines=%d", n), func(b *testing.B) {
			m, lines := benchTeamLog(n)
			shape := ReplayShape{Comp: &state.Competition{Kind: "team", TeamSize: 5}, Knockout: true}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				ReplayScoring(m, lines, shape)
			}
		})
	}
}

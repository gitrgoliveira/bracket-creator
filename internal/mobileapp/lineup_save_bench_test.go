package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// benchLineupSave builds a team competition of `teams` teams of five, each
// with a starting lineup and a carried copy of its first match's lineup, the
// starting lineup's senpo changed `changes` times (on every team when
// everyTeam, else on the first). It answers the router and a function that
// puts lineups.yaml back as built, with its size.
func benchLineupSave(b *testing.B, teams, changes int, everyTeam bool) (*gin.Engine, string, func(), int) {
	b.Helper()
	gin.SetMode(gin.TestMode)
	dir := b.TempDir()
	store, err := state.NewStore(dir)
	if err != nil {
		b.Fatal(err)
	}
	if err := store.SaveTournament(&state.Tournament{Name: "Bench", Password: "secret"}); err != nil {
		b.Fatal(err)
	}
	const comp = "bench"
	if err := store.SaveCompetition(&state.Competition{ID: comp, Kind: "team", TeamSize: 5, Status: state.CompStatusPools}); err != nil {
		b.Fatal(err)
	}
	players := make([]domain.Player, teams)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Team %d", i), Dojo: fmt.Sprintf("D%d", i)}
	}
	if err := store.SaveParticipants(comp, players); err != nil {
		b.Fatal(err)
	}
	ps, err := store.LoadParticipants(comp, false)
	if err != nil {
		b.Fatal(err)
	}
	var matches []state.MatchResult
	for i := range ps {
		j := (i + 1) % len(ps)
		matches = append(matches, state.MatchResult{ID: fmt.Sprintf("P1-%d", i), SideA: ps[i].Name, SideB: ps[j].Name,
			SideAID: ps[i].ID, SideBID: ps[j].ID, Status: state.MatchStatusScheduled})
	}
	if err := store.SavePoolMatches(comp, matches); err != nil {
		b.Fatal(err)
	}
	base := time.Now().UnixMilli() - 3_600_000
	pos := []domain.Position{domain.PosSenpo, domain.PosJiho, domain.PosChuken, domain.PosFukusho, domain.PosTaisho}
	var lineups []domain.TeamLineup
	for i, p := range ps {
		start := domain.TeamLineup{TeamID: p.ID, Positions: map[domain.Position]string{}, Stamps: map[domain.Position]int64{},
			History: map[domain.Position][]domain.PositionChange{}}
		for k, q := range pos {
			start.Positions[q] = fmt.Sprintf("Member %d-%d", i, k)
			start.Stamps[q] = base
			start.History[q] = []domain.PositionChange{{Stamp: 0}}
		}
		if everyTeam || i == 0 {
			h := []domain.PositionChange{{Stamp: 0}}
			for k := 1; k <= changes; k++ {
				h = append(h, domain.PositionChange{Stamp: base + int64(k), Name: fmt.Sprintf("Senpo %d", k)})
			}
			start.History[domain.PosSenpo] = h
			start.Stamps[domain.PosSenpo] = base + int64(changes) + 1
		}
		lineups = append(lineups, start)
		copied := start.Clone()
		copied.MatchID, copied.History, copied.Stamps = fmt.Sprintf("P1-%d", i), nil, nil
		copied.CopiedAt, copied.Own = base+int64(changes)/2, []domain.Position{domain.PosJiho}
		copied.Positions[domain.PosJiho] = fmt.Sprintf("Own %d", i)
		lineups = append(lineups, copied)
	}
	if err := store.SetTeamLineups(comp, lineups, 5); err != nil {
		b.Fatal(err)
	}
	path := filepath.Join(dir, "competitions", comp, "lineups.yaml")
	built, err := os.ReadFile(path)
	if err != nil {
		b.Fatal(err)
	}
	eng := engine.New(store)
	hub := NewHub()
	b.Cleanup(hub.Close)
	verifier := NewFileVerifier(store)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(verifier, store))
	RegisterLineupHandlers(admin, store, store, store, hub, store, verifier, eng)
	reset := func() {
		if err := os.WriteFile(path, built, 0o600); err != nil {
			b.Fatal(err)
		}
	}
	return r, "/api/competitions/" + comp + "/teams/" + ps[0].ID, reset, len(built)
}

// One lineup save of a team's starting lineup, and one of a match's own
// lineup, on a competition of 50 teams whose senpo was changed `changes` times
// (stage 6 review 8, m2 and m4: History is no longer bounded by count).
func BenchmarkLineupSave(b *testing.B) {
	for _, sc := range []struct {
		changes   int
		everyTeam bool
	}{{16, false}, {200, false}, {200, true}} {
		for _, door := range []string{"starting", "match"} {
			b.Run(fmt.Sprintf("teams=50 changes=%d everyTeam=%v door=%s", sc.changes, sc.everyTeam, door), func(b *testing.B) {
				r, team, reset, size := benchLineupSave(b, 50, sc.changes, sc.everyTeam)
				defer b.ReportMetric(float64(size), "yaml-bytes")
				path := team + "/lineups/0"
				if door == "match" {
					path = team + "/match-lineups/P1-0"
				}
				p := domain.PosChuken
				b.ResetTimer()
				for i := range b.N {
					b.StopTimer()
					reset()
					body, _ := json.Marshal(map[string]any{
						"positions": map[domain.Position]string{p: fmt.Sprintf("Bench %d", i)},
						"changed":   []domain.Position{p}, "changedAt": map[domain.Position]int64{p: time.Now().UnixMilli() - 1000},
						"writeId": fmt.Sprintf("bench-%d", i),
					})
					req := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(body))
					req.Header.Set("Content-Type", "application/json")
					req.Header.Set("X-Tournament-Password", "secret")
					w := httptest.NewRecorder()
					b.StartTimer()
					r.ServeHTTP(w, req)
					if w.Code != http.StatusOK {
						b.Fatalf("save answered %d: %s", w.Code, w.Body.String())
					}
				}
			})
		}
	}
}

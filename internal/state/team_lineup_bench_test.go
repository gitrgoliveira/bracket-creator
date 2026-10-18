package state

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// benchLineups stores teams x per carried match lineups, each holding a
// History, as a large team competition holds them.
func benchLineups(b *testing.B, teams, per int) (*Store, string, map[string]domain.TeamLineup) {
	b.Helper()
	s, err := NewStore(b.TempDir())
	if err != nil {
		b.Fatal(err)
	}
	const comp = "big"
	if err := s.SaveCompetition(&Competition{ID: comp, Kind: "team", TeamSize: 5}); err != nil {
		b.Fatal(err)
	}
	ps := []domain.Position{domain.PosSenpo, domain.PosJiho, domain.PosChuken, domain.PosFukusho, domain.PosTaisho}
	all := map[string]domain.TeamLineup{}
	for a := range teams {
		for k := range per {
			l := domain.TeamLineup{TeamID: fmt.Sprintf("t%d", a), MatchID: fmt.Sprintf("P1-%d-%d", a, k), CompetitionID: comp,
				Positions: map[domain.Position]string{}, Stamps: map[domain.Position]int64{}, CopiedAt: 1000,
				Own: []domain.Position{ps[0]}, History: map[domain.Position][]domain.PositionChange{}}
			for i, p := range ps {
				l.Positions[p] = fmt.Sprintf("Member Name %d", i)
				l.Stamps[p] = int64(1000 + i)
				l.History[p] = []domain.PositionChange{{Stamp: 1, Name: "Old Name"}, {Stamp: 2, Name: "Older Name"}}
			}
			all[lineupStorageKey(l)] = l
		}
	}
	if err := s.saveTeamLineupsLocked(comp, all, s.directWrite); err != nil {
		b.Fatal(err)
	}
	return s, comp, all
}

// The follow after a lineup save stores every carried lineup it moved. Per
// lineup (one staged write each, as before stage 6 review 7) the cost grows
// with the moved lineups times the file; batched it is one write.
func BenchmarkFollowStoresMovedLineups(b *testing.B) {
	for _, size := range [][2]int{{32, 8}, {64, 16}} {
		for _, batched := range []bool{false, true} {
			b.Run(fmt.Sprintf("lineups=%d moved=8 batched=%v", size[0]*size[1], batched), func(b *testing.B) {
				s, comp, all := benchLineups(b, size[0], size[1])
				var moved []domain.TeamLineup
				for k := range 8 {
					l := all[lineupStorageKey(domain.TeamLineup{TeamID: "t0", MatchID: fmt.Sprintf("P1-0-%d", k)})]
					l.Positions[domain.PosJiho] = "Changed"
					moved = append(moved, l)
				}
				b.ResetTimer()
				for range b.N {
					err := s.WithTransaction(comp, func(tx StoreTx) error {
						if batched {
							return tx.SetTeamLineups(comp, moved, 5)
						}
						for _, l := range moved {
							if err := tx.SetTeamLineup(comp, l, 5); err != nil {
								return err
							}
						}
						return nil
					})
					if err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}

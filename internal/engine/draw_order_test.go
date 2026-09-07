package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// TestGeneratePlayoffs_DrawOrderMatchesRoundOneLeaves pins bc-pnum operator
// ruling 2: "Knockout-only numbering ... needs to follow bracket positions,
// so the numbering is clear and sequential." bracket.DrawOrder is stamped by
// generatePlayoffs from the order helper.StandardSeeding returns (the
// seeded-players slice, before CreateBalancedTree/NewPlayoffDraw's slot-codec
// round trip); this test verifies that order is EXACTLY the round-1 leaf
// order read top to bottom (Rounds[0][i].SideA then SideB, byes and "Winner
// of" placeholders skipped) -- the property that makes "a number belongs to
// a position in the draw" actually true for a live-generated bracket. If
// this ever fails, DrawOrder must be derived from the leaves instead (the
// SlotArray order mapped back to players BY INDEX, never by name), because
// the leaves -- not StandardSeeding's own return slice -- are the actual
// bracket geometry the operator sees on screen and on the printed sheet.
//
// makeCollidingDojoPlayers is makePlayers' colliding-dojo counterpart: only
// THREE dojo names across the whole roster, assigned so every adjacent
// roster pair (0,1), (2,3), (4,5), … shares one -- the shape a real roster
// pasted in dojo-by-dojo produces, and precisely what StandardSeeding's
// delayDojoMeetings pass (seed.go) exists to repair by swapping UNSEEDED
// competitors apart. makePlayers' unique-per-player dojo gives that pass
// nothing to do, so DrawOrder there is only ever tested against a
// permutation identical to plain roster order; this fixture forces at least
// one real swap, so a DrawOrder that captured names/ids from BEFORE
// delayDojoMeetings ran (or mismatched its result by index) would show up
// as a mismatch against the round-1 leaves, which reflect the swap.
func makeCollidingDojoPlayers(n int) []domain.Player {
	players := makePlayers(n)
	for i := range players {
		players[i].Dojo = fmt.Sprintf("Dojo%d", (i/2)%3)
	}
	return players
}

// Runs through the real store-backed engine path (StartCompetition ->
// generatePlayoffs), the same harness enginePlayoffsLeaves/enginePlayoffsBracket
// use in bracket_identity_test.go, across entrant counts that are and are not
// powers of two, several court counts (court count only changes each match's
// Court field, never the leaf order, but the ruling asks it be checked),
// with/without seeds, and with unique vs. colliding dojos (the latter is
// what actually exercises delayDojoMeetings' unseeded-permuting pass; see
// makeCollidingDojoPlayers).
func TestGeneratePlayoffs_DrawOrderMatchesRoundOneLeaves(t *testing.T) {
	entrantCounts := []int{5, 8, 13, 16, 33}
	courtCounts := []int{1, 2, 4}
	dojoModes := []struct {
		name string
		make func(int) []domain.Player
	}{
		{"unique-dojo", makePlayers},
		{"colliding-dojo", makeCollidingDojoPlayers},
	}

	for _, n := range entrantCounts {
		for _, courts := range courtCounts {
			for _, seeded := range []bool{false, true} {
				for _, dm := range dojoModes {
					tName := fmt.Sprintf("%d entrants %d courts seeded=%v %s", n, courts, seeded, dm.name)
					t.Run(tName, func(t *testing.T) {
						players := dm.make(n)
						rosterOrder := make([]string, len(players))
						for i, p := range players {
							rosterOrder[i] = p.Name
						}
						if seeded {
							// Seed ranks 1..4 on four distinct players, per the task's
							// spec ("seed ranks 1..4 on distinct players").
							for i := 0; i < 4 && i < n; i++ {
								players[i].Seed = i + 1
							}
						}

						eng, store, _ := setupTestEngine(t)
						compID := fmt.Sprintf("draw-order-%d-%d-%v-%s", n, courts, seeded, dm.name)
						courtNames := make([]string, courts)
						for i := range courtNames {
							courtNames[i] = string(rune('A' + i))
						}
						require.NoError(t, store.SaveCompetition(&state.Competition{
							ID:        compID,
							Format:    state.CompFormatPlayoffs,
							Kind:      "individual",
							Courts:    courtNames,
							StartTime: "09:00",
							Status:    state.CompStatusSetup,
						}))
						// Strip IDs before saving (as enginePlayoffsLeaves does): a
						// non-UUID id confuses the CSV hasIDs detector. Let the store
						// mint real UUIDs, which is what DrawOrder must carry.
						stripped := make([]domain.Player, len(players))
						for i, p := range players {
							stripped[i] = domain.Player{Name: p.Name, Dojo: p.Dojo}
						}
						require.NoError(t, store.SaveParticipants(compID, stripped))
						var seeds []domain.SeedAssignment
						for _, p := range players {
							if p.Seed > 0 {
								seeds = append(seeds, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
							}
						}
						if len(seeds) > 0 {
							require.NoError(t, store.SaveSeeds(compID, seeds))
						}
						require.NoError(t, eng.StartCompetition(compID))

						bracket, err := store.LoadBracket(compID)
						require.NoError(t, err)
						require.NotNil(t, bracket)
						require.NotEmpty(t, bracket.DrawOrder, "generatePlayoffs must stamp DrawOrder")
						require.Len(t, bracket.DrawOrder, n, "DrawOrder must name every entrant exactly once, no byes")

						roster, err := store.LoadParticipantsOpt(compID, false, state.LoadParticipantsOpts{})
						require.NoError(t, err)
						idToName := make(map[string]string, len(roster))
						for _, p := range roster {
							idToName[p.ID] = p.Name
						}

						fromDrawOrder := make([]string, 0, len(bracket.DrawOrder))
						for _, id := range bracket.DrawOrder {
							name, ok := idToName[id]
							require.Truef(t, ok, "DrawOrder id %q must resolve to a roster participant", id)
							fromDrawOrder = append(fromDrawOrder, name)
						}

						require.NotEmpty(t, bracket.Rounds)
						fromLeaves := make([]string, 0, n)
						for _, m := range bracket.Rounds[0] {
							for _, side := range []string{m.SideA, m.SideB} {
								if side == "" || strings.HasPrefix(side, "Winner of") {
									continue
								}
								fromLeaves = append(fromLeaves, side)
							}
						}

						require.Equal(t, fromLeaves, fromDrawOrder,
							"DrawOrder must equal the round-1 leaf order top to bottom, byes skipped")

						if dm.name == "colliding-dojo" && !seeded {
							// Premise check, unseeded only: with seeds in play the
							// seed placement alone would already move players,
							// masking whether delayDojoMeetings itself did anything.
							// Unseeded isolates the ONE pass this variant exists to
							// exercise -- prove it actually moved someone, or this
							// variant tests nothing beyond the unique-dojo one above
							// (see makeCollidingDojoPlayers).
							require.NotEqualf(t, rosterOrder, fromDrawOrder,
								"premise: the colliding dojos must make delayDojoMeetings actually permute someone (n=%d courts=%d)", n, courts)
						}
					})
				}
			}
		}
	}
}

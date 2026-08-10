package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

// bc-draw Phase 2a regression tests.
//
// The CLI (cmd/create-pools.go) and the app engine (internal/engine/pools.go)
// build the pool draw from the same helper primitives, but the engine had
// drifted in two ways:
//
//  1. It handed helper.PoolSeeding the pool SIZE where the pool COUNT is
//     expected, so seeds landed in the wrong pools whenever the two differ.
//  2. It never called helper.ReorderPoolsForCourts, which PoolSeeding's
//     placement maths assumes has run. helper.AssignPoolsToCourts allocates
//     contiguous court blocks and CreatePools' "max" mode gives the extra
//     player to the FIRST pools, so every oversized pool piled onto court A.
//
// This file lives in package cmd because that is the only package that can
// drive BOTH real code paths: poolOptions.createPools is unexported here, and
// cmd already imports internal/engine and internal/state. Neither side is
// re-implemented. The CLI's draw is read back out of the workbook it actually
// wrote, and the engine's out of the files it actually persisted.

// parityEntrant is one competitor, fed to the CLI as a CSV line and to the
// engine as a domain.Player.
type parityEntrant struct {
	name string
	dojo string
}

// parityRoster builds n competitors. Names are already title-cased because
// helper.CreatePlayers title-cases CLI input while the engine stores names
// verbatim; matching the caser up front keeps the two rosters byte-identical.
// Every dojo is unique so CreatePools' dojo-conflict avoidance never fires and
// pool placement is a pure deterministic fill.
func parityRoster(n int) []parityEntrant {
	roster := make([]parityEntrant, n)
	for i := range roster {
		roster[i] = parityEntrant{
			name: fmt.Sprintf("Kendoka %02d", i+1),
			dojo: fmt.Sprintf("Dojo %02d", i+1),
		}
	}
	return roster
}

func parityEntries(roster []parityEntrant) []string {
	entries := make([]string, len(roster))
	for i, e := range roster {
		entries[i] = e.name + "," + e.dojo
	}
	return entries
}

func parityPlayers(roster []parityEntrant) []domain.Player {
	players := make([]domain.Player, len(roster))
	for i, e := range roster {
		players[i] = domain.Player{Name: e.name, Dojo: e.dojo}
	}
	return players
}

// paritySeeds seeds the first n competitors of the roster, rank 1..n.
// The slice is rebuilt per call because state.SaveSeeds sorts in place.
func paritySeeds(roster []parityEntrant, n int) []domain.SeedAssignment {
	seeds := make([]domain.SeedAssignment, n)
	for i := 0; i < n; i++ {
		seeds[i] = domain.SeedAssignment{Name: roster[i].name, SeedRank: i + 1}
	}
	return seeds
}

// poolDraw is the comparable shape of a draw: who is in which pool, which
// shiaijo each pool runs on, and how big each pool is.
type poolDraw struct {
	playerPool map[string]string // competitor name -> "Pool X"
	poolCourt  map[string]string // "Pool X"        -> court label
	poolSize   map[string]int    // "Pool X"        -> competitor count
}

func newPoolDraw() poolDraw {
	return poolDraw{
		playerPool: map[string]string{},
		poolCourt:  map[string]string{},
		poolSize:   map[string]int{},
	}
}

// courtLoad totals the competitors running on each shiaijo.
func (d poolDraw) courtLoad() map[string]int {
	load := map[string]int{}
	for pool, court := range d.poolCourt {
		load[court] += d.poolSize[pool]
	}
	return load
}

// oversizedByCourt counts, per shiaijo, the pools larger than the competition's
// smallest pool. Those are the pools whose qualifier fights an extra round-robin
// match, so clustering them on one court skews that court's load and (under
// bc-draw R6) concentrates every "oversized pool" bye claim in one region.
func (d poolDraw) oversizedByCourt() map[string]int {
	smallest := -1
	for _, size := range d.poolSize {
		if smallest < 0 || size < smallest {
			smallest = size
		}
	}
	counts := map[string]int{}
	for pool, court := range d.poolCourt {
		counts[court] += 0 // make every court present even with zero oversized pools
		if d.poolSize[pool] > smallest {
			counts[court]++
		}
	}
	return counts
}

// cliPoolDraw runs the real CLI pool draw and reads the result back out of the
// workbook it produced. Nothing about the draw is recomputed here.
func cliPoolDraw(t *testing.T, roster []parityEntrant, seeds []domain.SeedAssignment, poolSize, courts int, isMax bool) poolDraw {
	t.Helper()

	var buf bytes.Buffer
	writer := bufio.NewWriter(&buf)
	o := &poolOptions{
		outputWriter:    writer,
		outputPath:      "parity.xlsx",
		poolWinners:     2,
		courts:          courts,
		determined:      true, // no shuffle: the roster order must match the engine's
		SeedAssignments: seeds,
	}
	if isMax {
		o.maxPlayers = poolSize
	} else {
		o.numPlayers = poolSize
	}

	require.NoError(t, o.createPools(parityEntries(roster)))
	require.NoError(t, writer.Flush())

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	draw := newPoolDraw()

	// The "data" sheet is the workbook's roster: column A is the pool name and
	// column B the competitor, from row 3 down (helper.AddPoolDataToSheet).
	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)
	for i, row := range rows {
		if i < 2 || len(row) < 2 || row[0] == "" || row[1] == "" {
			continue // rows 1-2 are the title/header block
		}
		draw.playerPool[row[1]] = row[0]
		draw.poolSize[row[0]]++
	}
	require.Len(t, draw.playerPool, len(roster), "every competitor should appear on the data sheet")

	// Each shiaijo gets its own "Names to Print <label>" sheet whose column A
	// holds "<pool letter><position>" tags (helper.CreateNamesWithPoolToPrint).
	// That is the workbook's own statement of which pools run where.
	for c := 0; c < courts; c++ {
		label := helper.CourtLabel(c)
		sheet := helper.SheetNamesToPrint + " " + label
		nameRows, err := f.GetRows(sheet)
		require.NoErrorf(t, err, "reading sheet %q", sheet)
		for _, row := range nameRows {
			if len(row) == 0 || row[0] == "" {
				continue
			}
			draw.poolCourt["Pool "+strings.TrimRight(row[0], "0123456789")] = label
		}
	}
	require.Len(t, draw.poolCourt, len(draw.poolSize), "every pool should be claimed by exactly one shiaijo")

	return draw
}

// enginePoolDraw runs the real engine draw (StartCompetition) and reads the
// result back out of the files it persisted.
func enginePoolDraw(t *testing.T, roster []parityEntrant, seeds []domain.SeedAssignment, poolSize, courts int, isMax bool) poolDraw {
	t.Helper()

	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)

	mode := "min"
	if isMax {
		mode = "max"
	}
	courtNames := make([]string, courts)
	for i := range courtNames {
		courtNames[i] = helper.CourtLabel(i)
	}

	const compID = "pool-parity"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Pool Parity", Kind: "individual",
		Format: "mixed", PoolSize: poolSize, PoolSizeMode: mode,
		PoolWinners: 2, RoundRobin: true, Courts: courtNames,
		StartTime: "09:00", Status: "setup",
	}))
	require.NoError(t, store.SaveParticipants(compID, parityPlayers(roster)))
	if len(seeds) > 0 {
		require.NoError(t, store.SaveSeeds(compID, seeds))
	}
	require.NoError(t, eng.StartCompetition(compID))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	draw := newPoolDraw()
	for _, p := range pools {
		draw.poolSize[p.PoolName] = len(p.Players)
		for _, player := range p.Players {
			draw.playerPool[player.Name] = p.PoolName
		}
	}
	require.Len(t, draw.playerPool, len(roster), "every competitor should be in a saved pool")

	// MatchResult.ID is "<pool name>-<index>" (internal/engine/pools.go), and
	// the court on those rows is the pool's shiaijo.
	for _, m := range matches {
		cut := strings.LastIndex(m.ID, "-")
		require.Positivef(t, cut, "match id %q should carry a pool-name prefix", m.ID)
		draw.poolCourt[m.ID[:cut]] = m.Court
	}
	require.Len(t, draw.poolCourt, len(draw.poolSize), "every pool should have scheduled matches on one shiaijo")

	return draw
}

// TestPoolDrawParity_CLIAndEngine is the bc-draw Phase 2a parity gate: the same
// roster and the same seeds must produce the same pool composition and the same
// pool-to-shiaijo mapping through the CLI and through the engine.
//
// Before the fix the engine passed PoolSize to helper.PoolSeeding instead of the
// pool count and skipped helper.ReorderPoolsForCourts, so both maps diverged in
// every seeded case and the pool letters themselves differed (the reorder
// realphabetises).
func TestPoolDrawParity_CLIAndEngine(t *testing.T) {
	cases := []struct {
		name    string
		players int
		// poolSize is --max-players in max mode and --players in min mode; the
		// engine's PoolSize under PoolSizeMode "max" / "min" respectively.
		poolSize int
		courts   int
		seeds    int
		isMax    bool
	}{
		// 26 / 4 in max mode is the worked example from the bead: 7 pools,
		// five of them oversized.
		{"max_mode_2_courts_4_seeds", 26, 4, 2, 4, true},
		// Same roster in min mode: 6 pools of 4 with the two leftovers pushed
		// to the ends by forcePoolSize.
		{"min_mode_2_courts_4_seeds", 26, 4, 2, 4, false},
		// No seeds at all. PoolSeeding still runs (it clusters by dojo), which
		// is the behaviour the engine used to skip entirely when seeds were
		// absent, i.e. in most real app competitions.
		{"max_mode_4_courts_no_seeds", 24, 4, 4, 0, true},
		{"min_mode_2_courts_no_seeds", 24, 4, 2, 0, false},
		// Single shiaijo: the deinterleave is a no-op, so this pins that the
		// fix did not change the 1-court draw.
		{"min_mode_1_court_3_seeds", 18, 3, 1, 3, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roster := parityRoster(tc.players)

			cli := cliPoolDraw(t, roster, paritySeeds(roster, tc.seeds), tc.poolSize, tc.courts, tc.isMax)
			eng := enginePoolDraw(t, roster, paritySeeds(roster, tc.seeds), tc.poolSize, tc.courts, tc.isMax)

			assert.Equal(t, cli.poolSize, eng.poolSize, "pool sizes must match between the CLI and the engine")
			assert.Equal(t, cli.playerPool, eng.playerPool, "every competitor must land in the same pool letter on both paths")
			assert.Equal(t, cli.poolCourt, eng.poolCourt, "every pool must land on the same shiaijo on both paths")

			// Restate the seed case on its own so a failure names the rank.
			for _, seed := range paritySeeds(roster, tc.seeds) {
				cliPool := cli.playerPool[seed.Name]
				engPool := eng.playerPool[seed.Name]
				require.NotEmptyf(t, cliPool, "seed %d (%s) missing from the CLI draw", seed.SeedRank, seed.Name)
				assert.Equalf(t, cliPool, engPool,
					"seed %d (%s): CLI put it in %q, engine in %q", seed.SeedRank, seed.Name, cliPool, engPool)
				assert.Equalf(t, cli.poolCourt[cliPool], eng.poolCourt[engPool],
					"seed %d (%s): CLI shiaijo %q, engine shiaijo %q",
					seed.SeedRank, seed.Name, cli.poolCourt[cliPool], eng.poolCourt[engPool])
			}
		})
	}
}

// TestPoolDrawOversizedPoolsSpreadAcrossCourts pins the second half of the
// Phase 2a defect on the engine path.
//
// CreatePools in "max" mode gives the extra player to the FIRST pools
// (internal/helper/tournament.go) and helper.AssignPoolsToCourts allocates
// contiguous blocks, so without the deinterleave every oversized pool sits on
// the first shiaijo.
//
// Worked example, 26 players at PoolSize 4 in "max" mode on 2 courts, 7 pools
// with sizes [4 4 4 4 4 3 3]:
//
//	BEFORE (no ReorderPoolsForCourts): court A = pools 0-3, 16 players, ALL
//	                                   FOUR oversized; court B = pools 4-6,
//	                                   10 players, one oversized.
//	AFTER  (deinterleaved):            court A = 15 players, 3 oversized;
//	                                   court B = 11 players, 2 oversized.
func TestPoolDrawOversizedPoolsSpreadAcrossCourts(t *testing.T) {
	cases := []struct {
		name      string
		players   int
		poolSize  int
		courts    int
		isMax     bool
		wantLoad  map[string]int
		wantSizes []int // per-pool sizes in draw order, for legibility
	}{
		{
			name: "max_mode_26_players_2_courts", players: 26, poolSize: 4, courts: 2, isMax: true,
			// Was A=16 / B=10 before the fix.
			wantLoad:  map[string]int{"A": 15, "B": 11},
			wantSizes: []int{4, 4, 4, 3, 4, 4, 3},
		},
		{
			// Min mode places the leftovers at BOTH ends of the pool list
			// (forcePoolSize fills inward), so contiguous blocks were already
			// balanced here. Included so the fix is pinned as a no-op for min
			// mode rather than assumed to be one.
			name: "min_mode_26_players_2_courts", players: 26, poolSize: 4, courts: 2, isMax: false,
			wantLoad:  map[string]int{"A": 13, "B": 13},
			wantSizes: []int{5, 4, 4, 4, 4, 5},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			roster := parityRoster(tc.players)
			eng := enginePoolDraw(t, roster, nil, tc.poolSize, tc.courts, tc.isMax)

			sizes := make([]int, 0, len(eng.poolSize))
			for i := 0; i < len(eng.poolSize); i++ {
				name := fmt.Sprintf("Pool %c", rune('A'+i))
				require.Containsf(t, eng.poolSize, name, "expected %d consecutively named pools", len(eng.poolSize))
				sizes = append(sizes, eng.poolSize[name])
			}
			assert.Equal(t, tc.wantSizes, sizes, "pool sizes in draw order")
			assert.Equal(t, tc.wantLoad, eng.courtLoad(), "competitors per shiaijo")

			// The load numbers above are the specific consequence; this is the
			// property they stand for. Oversized pools must not cluster.
			oversized := eng.oversizedByCourt()
			minCount, maxCount := -1, -1
			for _, n := range oversized {
				if minCount < 0 || n < minCount {
					minCount = n
				}
				if n > maxCount {
					maxCount = n
				}
			}
			assert.LessOrEqualf(t, maxCount-minCount, 1,
				"oversized pools must be spread across shiaijo, got %v", oversized)
		})
	}
}

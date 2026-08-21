package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The command line's seed-warning surface (R2/D7 of specs/007-ekc-draw).
//
// A seeding constraint the configuration cannot satisfy MUST NOT stop the
// draw: the deepest constraint gives way and the operator is told what was
// relaxed. On the command line the operator is watching stdout while the
// workbook is written, so that is where it goes; the workbook is the artifact,
// not a message channel.

// seedsFor builds seed assignments for the first n entrants of a poolRoster,
// matching the "Player %02d" / "Dojo %02d" names it writes.
func seedsFor(n int) []domain.SeedAssignment {
	out := make([]domain.SeedAssignment, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, domain.SeedAssignment{
			Name:     "Player " + twoDigits(i),
			Dojo:     "Dojo " + twoDigits(i),
			SeedRank: i + 1,
		})
	}
	return out
}

func twoDigits(i int) string {
	if i < 10 {
		return "0" + string(rune('0'+i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}

// TestPoolOptionsRun_WarnsOnSurplusSeedRanks: 4 seeds over 3 pools. Two seeds
// may never share a pool, so the 4th rank is ignored - with a warning on
// stdout, and the workbook still written.
func TestPoolOptionsRun_WarnsOnSurplusSeedRanks(t *testing.T) {
	dir := t.TempDir()
	output := filepath.Join(dir, "out.xlsx")
	o := &poolOptions{
		filePath:        poolRoster(t, dir, 12),
		outputPath:      output,
		numPlayers:      4,
		poolWinners:     2,
		courts:          2,
		determined:      true,
		SeedAssignments: seedsFor(4),
	}

	var err error
	out := captureStdout(t, func() { err = o.run(nil, nil) })
	require.NoError(t, err, "a seeding constraint that cannot be met is a warning, never an error")

	assert.Contains(t, out, "Warning: Seed 4 ignored")
	assert.Contains(t, out, "two seeds must never share a pool")

	// The draw still happened.
	info, statErr := os.Stat(output)
	require.NoError(t, statErr)
	assert.Positive(t, info.Size())
}

// TestPoolOptionsRun_WarnsOnRelaxedSeedConstraint is D7's worked example: 4
// seeds and 5 pools on ONE shiaijo. PoolSeeding spreads seeded pools over
// SHIAIJO, so with only one to spread over it puts seeds 1 and 4 in adjacent
// pools and the draw cannot give them separate quarters. (On TWO shiaijo the
// same competition now satisfies every constraint, because the pool set is
// subdivided into four blocks whatever the shiaijo count.)
func TestPoolOptionsRun_WarnsOnRelaxedSeedConstraint(t *testing.T) {
	dir := t.TempDir()
	o := &poolOptions{
		filePath:        poolRoster(t, dir, 20),
		outputPath:      filepath.Join(dir, "out.xlsx"),
		numPlayers:      4,
		poolWinners:     2,
		courts:          1,
		determined:      true,
		SeedAssignments: seedsFor(4),
	}

	var err error
	out := captureStdout(t, func() { err = o.run(nil, nil) })
	require.NoError(t, err)
	assert.Contains(t, out, "Warning: Not every seed could be given its own quarter of the draw")
}

// TestPoolOptionsRun_SilentWithoutSeeds: an unseeded competition is a normal
// configuration and MUST print no seeding warning at all.
func TestPoolOptionsRun_SilentWithoutSeeds(t *testing.T) {
	dir := t.TempDir()
	o := &poolOptions{
		filePath:    poolRoster(t, dir, 20),
		outputPath:  filepath.Join(dir, "out.xlsx"),
		numPlayers:  4,
		poolWinners: 2,
		courts:      2,
		determined:  true,
	}

	var err error
	out := captureStdout(t, func() { err = o.run(nil, nil) })
	require.NoError(t, err)
	// Asserted on the "Warning:" prefix rather than on the word "seed": the
	// run echoes its input path, and t.TempDir() names that path after the
	// test, so a bare word match would find the test's own name.
	assert.NotContains(t, out, "Warning:")
}

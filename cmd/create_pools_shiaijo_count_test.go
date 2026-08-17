package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// poolRoster writes a CSV with n entrants, each in their own dojo, and returns
// its path. Unique dojos keep CreatePools' dojo-conflict avoidance out of the
// picture so the pool count is a pure function of the roster size.
func poolRoster(t *testing.T, dir string, n int) string {
	t.Helper()
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "Player %02d,Dojo %02d\n", i, i)
	}
	path := filepath.Join(dir, "input.csv")
	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
	return path
}

// TestPoolOptionsRun_ShiaijoCount sweeps --courts on create-pools across
// 1..17. Same rule as create-playoffs: the tree gives each shiaijo its own
// block and the blocks merge in pairs, so only a power of two is accepted and
// everything else is refused before any file is written. The sweep spans the
// counts the retired "1 or an even number" rule wrongly accepted (6, 10, 12,
// 14) as well as the odd ones.
func TestPoolOptionsRun_ShiaijoCount(t *testing.T) {
	for n := 1; n <= helper.MaxCourts; n++ {
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			dir := t.TempDir()
			// 24 entrants at the default pool size of 4 gives 6 pools. Court
			// counts above that are clamped by EffectiveDrawCourts, which is
			// covered separately below; what this sweep pins is the flag
			// validator, which runs before any of that.
			input := poolRoster(t, dir, 24)
			output := filepath.Join(dir, "out.xlsx")

			o := &poolOptions{
				filePath:    input,
				outputPath:  output,
				numPlayers:  4,
				poolWinners: 2,
				courts:      n,
				determined:  true,
			}
			err := o.run(nil, nil)
			if bctest.LegalShiaijoCount(n) {
				assert.NoErrorf(t, err, "%d courts must be accepted", n)
				return
			}
			require.Errorf(t, err, "%d courts must be rejected", n)
			assert.Contains(t, err.Error(), "shiaijo count must be a power of two")
			assert.Contains(t, err.Error(), ", or 1",
				"the message must always offer a single shiaijo")

			// The check runs before the workbook is opened, so a rejected run
			// leaves no half-written output behind.
			_, statErr := os.Stat(output)
			assert.Truef(t, os.IsNotExist(statErr),
				"a rejected court count must not create %s", output)
		})
	}
}

// TestPoolOptionsRun_RejectsEvenNonPowerOfTwo is the regression for the rule
// change on the CLI: --courts 6 passed the retired rule and must now be
// refused, pointing at 4 or 8 rather than at the old rule's 5 or 7.
func TestPoolOptionsRun_RejectsEvenNonPowerOfTwo(t *testing.T) {
	dir := t.TempDir()
	o := &poolOptions{
		filePath:    poolRoster(t, dir, 24),
		outputPath:  filepath.Join(dir, "out.xlsx"),
		numPlayers:  4,
		poolWinners: 2,
		courts:      6,
		determined:  true,
	}
	err := o.run(nil, nil)
	require.Error(t, err, "6 is even but not a power of two")
	assert.Contains(t, err.Error(), "use 4 or 8, or 1")
}

// TestPoolOptionsRun_ClampKeepsCourtsLegal covers the second place create-pools
// can set a court count: the clamp that lowers --courts to the pool count when
// the operator asked for more courts than there are pools. That clamp produces
// a value the operator never chose, so it has to land on a power of two.
//
// The 8-courts-over-7-pools row is the case the old "step down to an even
// number" clamp got wrong: it produced 6, which R9 rejects. One pool is the
// explicitly allowed single-shiaijo case and stays at 1.
func TestPoolOptionsRun_ClampKeepsCourtsLegal(t *testing.T) {
	tests := []struct {
		name       string
		entrants   int
		poolSize   int
		courts     int
		wantCourts int
	}{
		// 12 entrants / pool size 4 = 3 pools, clamp 4 -> 3 -> step down to 2.
		{"three pools step down to two", 12, 4, 4, 2},
		// 28 entrants / pool size 4 = 7 pools, clamp 8 -> 7 -> step down to 4.
		// The old clamp stepped to 6 here, which the rule now forbids.
		{"seven pools step down to four", 28, 4, 8, 4},
		// 24 entrants / pool size 4 = 6 pools; 6 is even but illegal, so the
		// clamp must not stop there either.
		{"six pools step down to four", 24, 4, 8, 4},
		// 16 entrants / pool size 4 = 4 pools, clamp 8 -> 4, already legal.
		{"power-of-two pool count clamps unchanged", 16, 4, 8, 4},
		// 4 entrants / pool size 4 = 1 pool, clamp 2 -> 1, allowed.
		{"single pool stays on one shiaijo", 4, 4, 2, 1},
		// 24 entrants / pool size 4 = 6 pools, no clamp at all.
		{"no clamp when courts fit", 24, 4, 4, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			input := poolRoster(t, dir, tt.entrants)

			o := &poolOptions{
				filePath:    input,
				outputPath:  filepath.Join(dir, "out.xlsx"),
				numPlayers:  tt.poolSize,
				poolWinners: 2,
				courts:      tt.courts,
				determined:  true,
			}
			require.NoError(t, o.run(nil, nil))

			assert.Equal(t, tt.wantCourts, o.courts,
				"clamped court count must land on a power of two")
			assert.Truef(t, bctest.LegalShiaijoCount(o.courts),
				"clamp produced an illegal %d courts", o.courts)
		})
	}
}

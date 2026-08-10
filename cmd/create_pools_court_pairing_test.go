package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// TestPoolOptionsRun_CourtPairing sweeps --courts on create-pools. Same rule as
// create-playoffs: the tree is split into one region per shiaijo and those
// regions pair up, so 1 court or an even number is accepted and an odd count
// above 1 is refused before any file is written.
func TestPoolOptionsRun_CourtPairing(t *testing.T) {
	for n := 1; n <= 8; n++ {
		valid := n == 1 || n%2 == 0
		t.Run(fmt.Sprintf("courts=%d", n), func(t *testing.T) {
			dir := t.TempDir()
			// 24 entrants at the default pool size of 4 gives 6 pools, so
			// every court count in the sweep is reachable without the
			// pool-count clamp interfering.
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
			if valid {
				assert.NoErrorf(t, err, "%d courts must be accepted", n)
				return
			}
			require.Errorf(t, err, "%d courts must be rejected", n)
			assert.Contains(t, err.Error(), "courts must be 1 or an even number")
			assert.Contains(t, err.Error(), fmt.Sprintf("use %d or %d, or 1", n-1, n+1))

			// The check runs before the workbook is opened, so a rejected run
			// leaves no half-written output behind.
			_, statErr := os.Stat(output)
			assert.Truef(t, os.IsNotExist(statErr),
				"a rejected court count must not create %s", output)
		})
	}
}

// TestPoolOptionsRun_ClampKeepsCourtsPairable covers the second place
// create-pools can set a court count: the clamp that lowers --courts to the
// pool count when the operator asked for more courts than there are pools.
// A legal --courts 4 over 3 pools used to become an unpairable 3; it must step
// down to the nearest even count instead. One pool is the explicitly allowed
// single-shiaijo case and stays at 1.
func TestPoolOptionsRun_ClampKeepsCourtsPairable(t *testing.T) {
	tests := []struct {
		name       string
		entrants   int
		poolSize   int
		courts     int
		wantCourts int
	}{
		// 12 entrants / pool size 4 = 3 pools, clamp 4 -> 3 -> step down to 2.
		{"odd pool count steps down to even", 12, 4, 4, 2},
		// 16 entrants / pool size 4 = 4 pools, clamp 6 -> 4, already even.
		{"even pool count clamps unchanged", 16, 4, 6, 4},
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
				"clamped court count must stay pairable (1 or even)")
			if o.courts > 1 {
				assert.Zerof(t, o.courts%2,
					"clamp produced an unpairable %d courts", o.courts)
			}
		})
	}
}

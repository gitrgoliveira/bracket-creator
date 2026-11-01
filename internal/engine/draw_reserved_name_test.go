package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A roster written before the reserved-name rule, or by hand, never crosses
// the roster save: a raw "pool a-1st" reads "Pool A-1st", the exact shape the
// bracket rule takes for an unresolved slot (operator decision D22), so the
// draw refuses it, naming the competitor, in every format, and writes nothing.
func TestGenerateDraw_RefusesAReservedNameInALoadedRoster(t *testing.T) {
	for _, format := range []string{state.CompFormatMixed, state.CompFormatKnockout, state.CompFormatLeague} {
		t.Run(format, func(t *testing.T) {
			eng, store, dir := setupTestEngine(t)
			compID := "reserved-roster-" + format
			createTestCompetition(t, store, compID, format, 4, func(c *state.Competition) {
				c.Courts = []string{"A"}
			})
			rows := ""
			for _, r := range []string{"Alice,DojoA", "pool a-1st,DojoB", "Carol,DojoC", "Dave,DojoA", "Bob,DojoB", "Erin,DojoC"} {
				rows += helper.NewUUID4() + "," + r + "\n"
			}
			writeRawParticipantsCSV(t, dir, compID, rows)

			err := eng.GenerateDraw(compID)
			require.Error(t, err)
			var ve *ValidationError
			require.ErrorAs(t, err, &ve, "a 400 at generate-draw")
			assert.ErrorIs(t, err, state.ErrReservedName)
			assert.Contains(t, ve.Error(), `"Pool A-1st"`, "the refusal names the competitor as the roster reads it")

			comp, lerr := store.LoadCompetition(compID)
			require.NoError(t, lerr)
			assert.Equal(t, state.CompStatusSetup, comp.Status)
			bracket, berr := store.LoadBracket(compID)
			require.NoError(t, berr)
			if bracket != nil {
				assert.Empty(t, bracket.Rounds, "nothing is drawn")
			}
			pools, perr := store.LoadPools(compID)
			require.NoError(t, perr)
			assert.Empty(t, pools)
		})
	}
}

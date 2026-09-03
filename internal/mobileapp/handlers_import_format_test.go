package mobileapp

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-terminology commit 1 (playoffs -> knockout clean break): the import door
// is the ONE place, besides state.upgradeCompetitionFormatLocked, permitted to
// still recognise "playoffs" -- and unlike config.md, which converges to the
// canonical value after one load, this acceptance is PERMANENT: an exported
// manifest bundle is a static artifact that can be replayed at any point in
// the future. See normalizeImportFormat (handlers_import.go).
//
// importOneComp lives in handlers_import_extra_qualifiers_test.go.

// The OMITTED case is affected too, even though it never spells the retired
// word. Historically it persisted as the literal empty string and merely
// BEHAVED like a knockout-only competition (the draw pipeline's format switch
// falls through to its default branch for any unrecognised value, empty string
// included). normalizeImportFormat now states that outright: the persisted
// record says what it always did, rather than leaving a future reader to
// rediscover the fallback.
func TestImport_FormatNormalizesToKnockout(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		formatKey string
		why       string
	}{
		{
			name:      "retired playoffs literal",
			id:        "legacy-playoffs-format",
			formatKey: `    format: "playoffs"` + "\n",
			why:       "the pre-rename literal must normalize to the canonical value on import, not persist verbatim",
		},
		{
			name:      "omitted format key",
			id:        "omitted-format",
			formatKey: "",
			why:       "an omitted format must persist explicitly as knockout rather than as the empty string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, store, _, _, tempDir := setupTestRouter(t)
			defer os.RemoveAll(tempDir)

			res := importOneComp(t, r, "competitions:\n"+
				`  - id: "`+tt.id+`"`+"\n"+
				`    name: "`+tt.name+`"`+"\n"+
				tt.formatKey+
				`    courts: ["A"]`+"\n"+
				`    participants: "players.csv"`+"\n")
			require.Empty(t, res.Error)

			comp, err := store.LoadCompetition(tt.id)
			require.NoError(t, err)
			require.NotNil(t, comp, "the competition must still import")
			assert.Equal(t, state.CompFormatKnockout, comp.Format, tt.why)
		})
	}
}

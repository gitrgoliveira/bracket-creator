package mobileapp

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-terminology commit 1 (playoffs -> knockout clean break): the import door
// is the ONE place, besides config.md's in-memory conversion
// (parseCompetitionFile), permitted to still recognise "playoffs" -- and
// unlike config.md, this acceptance is PERMANENT: an exported manifest bundle
// is a static artifact that can be replayed at any point in the future. See
// normalizeImportFormat (handlers_import.go).
//
// importOneComp lives in handlers_import_extra_qualifiers_test.go.

// The omitted-format case is a deliberate NON-normalization: an omitted
// format must persist as the empty string, exactly as it did before the
// playoffs->knockout rename. Competition.IsKnockoutEnabled() treats "" and
// "knockout" differently (it gates the Excel export's Elimination Matches and
// Tree sheets), so coercing the omitted case to "knockout" would silently
// change what an imported competition exports.
func TestImport_FormatNormalizesToKnockout(t *testing.T) {
	tests := []struct {
		name      string
		id        string
		formatKey string
		expected  string
		why       string
	}{
		{
			name:      "retired playoffs literal",
			id:        "legacy-playoffs-format",
			formatKey: `    format: "playoffs"` + "\n",
			expected:  state.CompFormatKnockout,
			why:       "the pre-rename literal must normalize to the canonical value on import, not persist verbatim",
		},
		{
			name:      "omitted format key",
			id:        "omitted-format",
			formatKey: "",
			expected:  "",
			why:       "an omitted format must persist unchanged, not be coerced to knockout",
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
			assert.Equal(t, tt.expected, comp.Format, tt.why)
		})
	}
}

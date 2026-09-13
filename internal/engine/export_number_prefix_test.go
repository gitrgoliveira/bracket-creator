package engine

// Regression coverage for the three ExportCompetitionXlsx call sites (bc-pnum
// review) that used to read comp.NumberPrefix directly instead of routing
// through comp.EffectiveNumberPrefix() (state/models.go), the ONE fold every
// reader of the field is supposed to compare/compose under.
//
// A hand-edited config.md can hold `number_prefix: "K "` (a quoted YAML
// scalar; an unquoted trailing space is stripped by the parser, so only a
// quoted value keeps it), and nothing heals that on read: a settings PUT that
// omits the prefix inherits the stored value verbatim. The numbering
// pipeline already trims (helper.NumberPools/engine.numbering.go both route
// through EffectiveNumberPrefix; see TestRenumberCompetitors_TrimsPaddedPrefix
// in numbering_test.go), so a competitor's stored Number never carries the
// padding -- but before this fix, the untrimmed field still reached
// helper.CreateTagsSheet/CreateNamesToPrint/CreateNamesWithPoolToPrint, and
// what helper computes FROM the prefix STRING alone (not from any player's
// Number) still disagreed: stackedNumberPrefix's rune-count-over-one rule
// reads a padded one-letter prefix (" K ", three runes) as a multi-letter
// STACKED prefix.
//
// The Tags sheet is the cleanest place this is independently observable: its
// per-sheet style decision (tagNumberStyle, excel_tags.go) is driven solely
// by stackedNumberPrefix(numberPrefix), with no per-row guard of its own (it
// already reads each tag's actual split through splitNumberLines, so a
// prefix mismatch never fabricates a two-line VALUE -- only the STYLE reacts
// to the raw string's own rune count). The Names to Print sheet's SPLIT
// decision cannot show this fix in isolation: FIX 1 in the same bead made
// printNameEntries decide per row through splitNumberLines(player.Number,
// numberPrefix), and a competitor Number composed from the trimmed prefix
// ("K1") never has the padded raw prefix (" K ") as a literal Go string
// prefix either way, so that sheet already falls back to single-line
// correctly regardless of this fix.
import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportCompetitionXlsx_TrimsPaddedNumberPrefixForTagsSheetStyle(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "padded-number-prefix"

	createTestCompetition(t, store, compID, "mixed", 4, func(c *state.Competition) {
		c.NumberPrefix = "K"
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i+1)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	// Premise: the draw assigned exactly the one-letter prefix requested, and
	// stamped it onto the pool roster's numbers.
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pools)
	require.NotEmpty(t, pools[0].Players)
	require.Equal(t, "K1", pools[0].Players[0].Number, "premise: competitor numbers were composed from the one-letter prefix")

	// Simulate a hand-edited config.md written (or edited) after the draw:
	// the stored field gains real, non-whitespace-only padding, but nothing
	// re-derives the already-assigned competitor numbers from it.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.NumberPrefix = " K "
	require.NoError(t, store.SaveCompetition(comp))

	f := openExportedWorkbook(t, eng, compID)

	styleID, err := f.GetCellStyle(helper.SheetTags, "A1")
	require.NoError(t, err)
	style, err := f.GetStyle(styleID)
	require.NoError(t, err)
	require.NotNil(t, style.Alignment)
	require.NotNil(t, style.Font)

	assert.False(t, style.Alignment.WrapText,
		`CreateTagsSheet must be handed the TRIMMED prefix ("K", one rune, single-line style), not the padded raw field (" K ", which stackedNumberPrefix reads as two runes and therefore stacked)`)
	assert.True(t, style.Alignment.ShrinkToFit, "single-line tag style shrinks to fit rather than wrapping")
	assert.Equal(t, float64(250), style.Font.Size, "single-line tag style uses the 250pt font, not the stacked 160pt")

	val, err := f.GetCellValue(helper.SheetTags, "A1")
	require.NoError(t, err)
	assert.Equal(t, "K1", val, "the printed tag itself is unaffected either way -- only the STYLE reacts to the untrimmed prefix")
}

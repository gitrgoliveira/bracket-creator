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

func TestImport_AcceptsLegacyPlayoffsFormat(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "legacy-playoffs-format"
    name: "Legacy Playoffs Format"
    format: "playoffs"
    courts: ["A"]
    participants: "players.csv"
`)
	require.Empty(t, res.Error)

	comp, err := store.LoadCompetition("legacy-playoffs-format")
	require.NoError(t, err)
	require.NotNil(t, comp, "a legal (legacy) format must still import")
	assert.Equal(t, state.CompFormatKnockout, comp.Format,
		"the pre-rename literal must normalize to the canonical value on import, not persist verbatim")
}

// An OMITTED format key is affected too, even though it never spells the
// retired word. Historically it persisted as the literal empty string and
// merely BEHAVED like a knockout-only competition (the draw pipeline's format
// switch falls through to its default branch for any unrecognised value,
// empty string included). normalizeImportFormat now states that outright:
// the persisted record says what it always did, rather than leaving a future
// reader to rediscover the fallback.
func TestImport_OmittedFormatDefaultsToKnockout(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	res := importOneComp(t, r, `
competitions:
  - id: "omitted-format"
    name: "Omitted Format"
    courts: ["A"]
    participants: "players.csv"
`)
	require.Empty(t, res.Error)

	comp, err := store.LoadCompetition("omitted-format")
	require.NoError(t, err)
	require.NotNil(t, comp, "an omitted format must still import")
	assert.Equal(t, state.CompFormatKnockout, comp.Format,
		"an omitted format must persist explicitly as knockout rather than as the empty string")
}

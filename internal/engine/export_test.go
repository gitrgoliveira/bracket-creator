package engine

import (
	"bytes"
	"fmt"
	"testing"

	qrcode "github.com/skip2/go-qrcode"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// exportTagsQRs exports a drawn competition at the given public-names option
// and answers every QR image on its Tags sheet, with the reveal-link QR each
// competitor would carry under per-person links (built from the same secret,
// whatever the option exported).
func exportTagsQRs(t *testing.T, option string) (tagQRs [][]byte, revealQRs map[string][]byte) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "reveal-tags"
	secret, err := state.NewRevealSecret()
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Open", PublicURL: "https://example.com"}))
	createTestCompetition(t, store, compID, "mixed", 4, func(c *state.Competition) {
		c.NumberPrefix = "K"
		c.PublicNames = option
		c.RevealSecret = secret
	})
	names := make([]string, 8)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i+1)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	perPerson := *comp
	perPerson.PublicNames = state.PublicNamesPerson
	participants, err := store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	require.NoError(t, err)
	require.Len(t, participants, len(names))
	revealQRs = map[string][]byte{}
	for _, p := range participants {
		key := state.CompetitorRevealKey(&perPerson, p.ID)
		require.NotEmptyf(t, key, "premise: %s has a reveal key under per-person links", p.Name)
		png, err := qrcode.Encode("https://example.com/reveal/"+compID+"#"+key, qrcode.Medium, 400)
		require.NoError(t, err)
		revealQRs[p.ID] = png
	}

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	cells, err := f.GetPictureCells(helper.SheetTags)
	require.NoError(t, err)
	for _, cell := range cells {
		pics, err := f.GetPictures(helper.SheetTags, cell)
		require.NoError(t, err)
		for _, pic := range pics {
			tagQRs = append(tagQRs, pic.File)
		}
	}
	// Two copies of each competitor's tag, each with its QR.
	require.Len(t, tagQRs, 2*len(names), "every tag carries a QR")
	return tagQRs, revealQRs
}

// Per-person links put each competitor's own reveal link in their tag QR:
// both copies of every tag open /reveal/<competition>#<that competitor's key>.
func TestExportCompetitionXlsx_PerPersonTagQRIsTheCompetitorsRevealLink(t *testing.T) {
	tagQRs, revealQRs := exportTagsQRs(t, state.PublicNamesPerson)
	seen := map[string]int{}
	for _, qr := range tagQRs {
		matched := ""
		for id, want := range revealQRs {
			if bytes.Equal(qr, want) {
				matched = id
			}
		}
		require.NotEmpty(t, matched, "a tag QR is not any competitor's reveal link")
		seen[matched]++
	}
	for id := range revealQRs {
		assert.Equalf(t, 2, seen[id], "both tags of %s carry their reveal link", id)
	}
}

// Every other option prints no reveal link on a tag: the secret-derived key
// is never printed unless the competition gives per-person links.
func TestExportCompetitionXlsx_OtherOptionsPrintNoRevealLinkOnTags(t *testing.T) {
	for _, option := range []string{state.PublicNamesShown, state.PublicNamesCompetition, state.PublicNamesHidden} {
		t.Run("option="+option, func(t *testing.T) {
			tagQRs, revealQRs := exportTagsQRs(t, option)
			for _, qr := range tagQRs {
				for id, reveal := range revealQRs {
					assert.Falsef(t, bytes.Equal(qr, reveal), "option %q printed %s's reveal link", option, id)
				}
			}
		})
	}
}

func TestTagReveal_OnlyPerPerson(t *testing.T) {
	assert.Nil(t, tagReveal(nil))
	for _, option := range []string{state.PublicNamesShown, state.PublicNamesCompetition, state.PublicNamesHidden} {
		assert.Nilf(t, tagReveal(&state.Competition{ID: "c", PublicNames: option}), "option %q", option)
	}
	r := tagReveal(&state.Competition{ID: "c", PublicNames: state.PublicNamesPerson})
	require.NotNil(t, r)
	assert.Equal(t, "c", r.CompetitionID)
	assert.Empty(t, r.KeyFor("p1"), "no secret, no key")
}

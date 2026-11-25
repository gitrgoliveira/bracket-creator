package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type posterRulesTable struct {
	HeadingMaxLength      int      `json:"headingMaxLength"`
	CallToActionMaxLength int      `json:"callToActionMaxLength"`
	LogoValues            []string `json:"logoValues"`
	MaxCourtEntries       int      `json:"maxCourtEntries"`
	DefaultHeadings       struct {
		Viewer      string `json:"viewer"`
		CourtPrefix string `json:"courtPrefix"`
	} `json:"defaultHeadings"`
	LengthCases []struct {
		Text   string `json:"text"`
		Length int    `json:"length"`
	} `json:"lengthCases"`
}

func readPosterRules(t *testing.T) posterRulesTable {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", "poster_rules.json"))
	require.NoError(t, err)
	var table posterRulesTable
	require.NoError(t, json.Unmarshal(raw, &table))
	return table
}

// The browser's counters, limits, logo choices and default headings come from
// the shared table, so it must say what the server enforces.
func TestPosterRules_SharedTable(t *testing.T) {
	table := readPosterRules(t)
	assert.Equal(t, MaxPosterHeadingLen, table.HeadingMaxLength)
	assert.Equal(t, MaxPosterCallToActionLen, table.CallToActionMaxLength)
	assert.Equal(t, PosterLogoValues, table.LogoValues)
	assert.Equal(t, MaxPosterCourtEntries, table.MaxCourtEntries)
	assert.Equal(t, 2*helper.MaxCourts, table.MaxCourtEntries)
	assert.Equal(t, PosterViewerHeading, table.DefaultHeadings.Viewer)
	assert.Equal(t, PosterCourtHeadingPrefix, table.DefaultHeadings.CourtPrefix)
	require.NotEmpty(t, table.LengthCases)
	for _, c := range table.LengthCases {
		assert.Equal(t, c.Length, utf8.RuneCountInString(c.Text), "length of %q", c.Text)
	}
}

func posterMessage(t *testing.T, err error) string {
	t.Helper()
	require.Error(t, err)
	var pe *PosterError
	require.True(t, errors.As(err, &pe), "a refusal is a PosterError")
	assert.NotContains(t, pe.Message, "posters.", "the sentence names no field path")
	return pe.Message
}

func TestValidatePosters_AcceptsTheLimitsAndEveryLogoValue(t *testing.T) {
	assert.NoError(t, ValidatePosters(nil))
	assert.NoError(t, ValidatePosters(&Posters{}))
	for _, logo := range PosterLogoValues {
		p := &Posters{
			Viewer: PosterChoices{Heading: strings.Repeat("h", MaxPosterHeadingLen), CallToAction: strings.Repeat("c", MaxPosterCallToActionLen), Logo: logo},
			Courts: map[string]PosterChoices{"B": {Logo: logo, HideSponsors: true}},
		}
		assert.NoError(t, ValidatePosters(p), "logo %q", logo)
	}
	// Lengths are judged after trimming and in characters, not bytes.
	padded := &Posters{Viewer: PosterChoices{Heading: "  " + strings.Repeat("剣", MaxPosterHeadingLen) + "  "}}
	assert.NoError(t, ValidatePosters(padded))
	assert.NoError(t, ValidatePosters(&Posters{Viewer: PosterChoices{Logo: " above "}}), "the logo value is trimmed too")
}

func TestValidatePosters_RefusesEachFieldWithAPlainSentence(t *testing.T) {
	long := func(n int) string { return strings.Repeat("x", n+1) }
	cases := []struct {
		name string
		p    *Posters
		want string
	}{
		{"viewer heading", &Posters{Viewer: PosterChoices{Heading: long(MaxPosterHeadingLen)}},
			"The heading on the viewer home poster is longer than 80 characters."},
		{"viewer call to action", &Posters{Viewer: PosterChoices{CallToAction: long(MaxPosterCallToActionLen)}},
			"The call to action on the viewer home poster is longer than 120 characters."},
		{"viewer logo", &Posters{Viewer: PosterChoices{Logo: "middle"}},
			"The logo place on the viewer home poster must be in the centre of the code or above it."},
		{"court heading", &Posters{Courts: map[string]PosterChoices{"B": {Heading: long(MaxPosterHeadingLen)}}},
			"The heading on the Shiaijo B poster is longer than 80 characters."},
		{"court call to action", &Posters{Courts: map[string]PosterChoices{"C": {CallToAction: long(MaxPosterCallToActionLen)}}},
			"The call to action on the Shiaijo C poster is longer than 120 characters."},
		{"court logo", &Posters{Courts: map[string]PosterChoices{"A": {Logo: "Centre"}}},
			"The logo place on the Shiaijo A poster must be in the centre of the code or above it."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, posterMessage(t, ValidatePosters(tc.p)))
		})
	}
}

func TestValidatePosters_RefusesAKeyThatIsNoCourtLabel(t *testing.T) {
	for _, key := range []string{"", " ", "AB", "\t"} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			msg := posterMessage(t, ValidatePosters(&Posters{Courts: map[string]PosterChoices{key: {Heading: "x"}}}))
			assert.Contains(t, msg, "not a shiaijo label")
		})
	}
}

// A court removed from the tournament keeps its entry, whatever its label,
// so the next save of the block is never refused for it.
func TestValidatePosters_KeepsAStaleEntryOutsideAToP(t *testing.T) {
	p := &Posters{Courts: map[string]PosterChoices{"Z": {Heading: "Old court"}, "7": {Logo: PosterLogoAbove}, "B": {}}}
	assert.NoError(t, ValidatePosters(p))
}

func TestValidatePosters_BoundsTheCourtEntries(t *testing.T) {
	courts := map[string]PosterChoices{}
	for i := 0; len(courts) < MaxPosterCourtEntries; i++ {
		courts[string(rune('A'+i))] = PosterChoices{HideSponsors: true}
	}
	assert.NoError(t, ValidatePosters(&Posters{Courts: courts}), "32 entries are kept")
	courts["~"] = PosterChoices{HideSponsors: true}
	assert.Equal(t, "Poster choices can be kept for at most 32 shiaijo; this save holds 33.", posterMessage(t, ValidatePosters(&Posters{Courts: courts})))
}

func TestIsCourtLabel(t *testing.T) {
	for _, ok := range []string{"A", "P", "Z", "7", "剣"} {
		assert.True(t, IsCourtLabel(ok), ok)
	}
	for _, bad := range []string{"", " ", "　", "AB", "A "} {
		assert.False(t, IsCourtLabel(bad), "%q", bad)
	}
}

func TestNormalizePosters(t *testing.T) {
	assert.Nil(t, NormalizePosters(nil))
	assert.Nil(t, NormalizePosters(&Posters{}))
	assert.Nil(t, NormalizePosters(&Posters{Viewer: PosterChoices{Heading: "   "}, Courts: map[string]PosterChoices{"A": {CallToAction: " "}, "B": {}}}),
		"a block holding only blanks is no block")

	in := &Posters{
		Viewer: PosterChoices{Heading: "  Follow us  ", Logo: " centre "},
		Courts: map[string]PosterChoices{"A": {}, "B": {CallToAction: " Scan to follow B "}, "C": {HideSponsors: true}},
	}
	got := NormalizePosters(in)
	require.NotNil(t, got)
	assert.Equal(t, PosterChoices{Heading: "Follow us", Logo: PosterLogoCentre}, got.Viewer)
	assert.Equal(t, map[string]PosterChoices{"B": {CallToAction: "Scan to follow B"}, "C": {HideSponsors: true}}, got.Courts)
	assert.Equal(t, "  Follow us  ", in.Viewer.Heading, "the block given is not changed")
	assert.Len(t, in.Courts, 3)

	onlyCourt := NormalizePosters(&Posters{Courts: map[string]PosterChoices{"D": {Logo: PosterLogoAbove}}})
	require.NotNil(t, onlyCourt)
	assert.Equal(t, PosterChoices{}, onlyCourt.Viewer)
}

func TestPostersIsEmpty(t *testing.T) {
	var none *Posters
	assert.True(t, none.IsEmpty())
	assert.True(t, (&Posters{Courts: map[string]PosterChoices{"A": {}}}).IsEmpty())
	assert.False(t, (&Posters{Viewer: PosterChoices{HideSponsors: true}}).IsEmpty())
	assert.False(t, (&Posters{Courts: map[string]PosterChoices{"A": {Logo: PosterLogoAbove}}}).IsEmpty())
}

func posterTournament(posters *Posters) *Tournament {
	return &Tournament{
		Name: "Poster Taikai", Date: "03-11-2026", Venue: "Budokan",
		Courts: []string{"A", "B"}, Password: "pw", DurationDays: 1, Mode: TournamentModeOfficiated,
		Posters: posters,
	}
}

// YAML 1.1 reads a bare N or Y as a boolean; the court keys must come back
// from tournament.md as the strings they were, read by a fresh store.
func TestPosters_RoundTripThroughTournamentMD(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	want := &Posters{
		Viewer: PosterChoices{Heading: "Follow the taikai", CallToAction: "Scan with your phone", Logo: PosterLogoCentre},
		Courts: map[string]PosterChoices{
			"N": {Heading: "North", HideSponsors: true},
			"Y": {Logo: PosterLogoAbove},
			"1": {CallToAction: "Court one"},
		},
	}
	require.NoError(t, s.SaveTournament(posterTournament(want)))

	raw, err := os.ReadFile(filepath.Join(dir, "tournament.md"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), "posters:")
	assert.Contains(t, string(raw), "call_to_action:")
	assert.Contains(t, string(raw), "hide_sponsors: true")

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	got, err := fresh.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, got.Posters)
}

func TestPosters_AbsentBlockWritesNoKey(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveTournament(posterTournament(nil)))
	raw, err := os.ReadFile(filepath.Join(dir, "tournament.md"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "posters")

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	got, err := fresh.LoadTournament()
	require.NoError(t, err)
	assert.Nil(t, got.Posters, "an absent block reads as every default")
}

// A backup made after a poster save restores the same block.
func TestPosters_BackupRestoresTheBlock(t *testing.T) {
	want := &Posters{
		Viewer: PosterChoices{Heading: "Follow the taikai"},
		Courts: map[string]PosterChoices{"B": {Logo: PosterLogoAbove, HideSponsors: true}, "Z": {Heading: "Removed court"}},
	}
	src, _ := newRestoreTestStore(t)
	require.NoError(t, src.SaveTournament(posterTournament(want)))
	a := archiveOf(t, src, ScopeTournament, "")

	dst, dir := newRestoreTestStore(t)
	restoreTestTournament(t, dst, "pw")
	_, err := dst.Restore(RestoreRequest{Archive: a})
	require.NoError(t, err)

	got, err := dst.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want, got.Posters)
	fresh, err := NewStore(dir)
	require.NoError(t, err)
	again, err := fresh.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, want, again.Posters, "the restored block is on disk")
}

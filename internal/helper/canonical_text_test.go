package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// Names and dojos are title-cased and zekken are written in capitals
// (operator decisions D14 and D20, 2026-11-01). Both forms are idempotent, so
// text already in its stored form passes through unchanged.
func TestTitleCaseNameAndCapitalizeZekken(t *testing.T) {
	titleCases := []struct{ in, want string }{
		{"kenshinkan", "Kenshinkan"},
		{"  kenshinkan  ", "Kenshinkan"},
		{"BKA", "BKA"},
		{"kendo-kai", "Kendo-Kai"},
		{"nakano kendo club", "Nakano Kendo Club"},
		// A word that begins with a digit is left exactly as typed (D22); a
		// letter after an apostrophe does not start a word.
		{"2nd dojo", "2nd Dojo"},
		{"pool a-1st", "Pool A-1st"},
		{"3RD", "3RD"},
		{"team 2Nd", "Team 2Nd"},
		{"1st's dojo", "1st's Dojo"},
		{"a1b", "A1b"},
		{"o'neill", "O'neill"},
		{"strasse ßen", "Strasse Ssen"},
		{"", ""},
	}
	for _, tc := range titleCases {
		got := TitleCaseName(tc.in)
		assert.Equal(t, tc.want, got, "TitleCaseName(%q)", tc.in)
		assert.Equal(t, got, TitleCaseName(got), "TitleCaseName is idempotent on %q", got)
	}

	zekkenCases := []struct{ in, want string }{
		{"tanaka", "TANAKA"},
		{"  t. smith ", "T. SMITH"},
		{"YAMADA", "YAMADA"},
		{"", ""},
	}
	for _, tc := range zekkenCases {
		got := CapitalizeZekken(tc.in)
		assert.Equal(t, tc.want, got, "CapitalizeZekken(%q)", tc.in)
		assert.Equal(t, got, CapitalizeZekken(got), "CapitalizeZekken is idempotent on %q", got)
	}
}

func TestCanonicalizePlayerText(t *testing.T) {
	p := Player{Name: " john smith ", DisplayName: "j. smith", Dojo: "kenshinkan dojo"}
	CanonicalizePlayerText(&p)
	assert.Equal(t, "John Smith", p.Name)
	assert.Equal(t, "J. SMITH", p.DisplayName)
	assert.Equal(t, "Kenshinkan Dojo", p.Dojo)
}

// The shared CSV reader (the CLI, an import and the tournament app's load)
// gives every competitor the stored form (D17).
func TestCreatePlayers_CanonicalisesNameDojoAndZekken(t *testing.T) {
	players, err := CreatePlayers([]string{"john smith, smithy, kenshinkan"}, true)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "John Smith", players[0].Name)
	assert.Equal(t, "SMITHY", players[0].DisplayName)
	assert.Equal(t, "Kenshinkan", players[0].Dojo)

	players, err = CreatePlayers([]string{"jane doe, kendo-kai"}, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "Jane Doe", players[0].Name)
	assert.Equal(t, "Kendo-Kai", players[0].Dojo)
}

// Two rows that differ only in the case of the dojo are one competitor, so the
// reader refuses them as a duplicate.
func TestCreatePlayers_CaseOnlyDojoDifferenceIsADuplicate(t *testing.T) {
	_, err := CreatePlayers([]string{"John, kenshinkan", "John, Kenshinkan"}, false)
	require.Error(t, err)
}

// A seed row with no id is found on the roster whatever case its dojo was
// typed in, because the lookup title-cases it like the roster's own dojo.
func TestApplySeeds_RawDojoFindsTheCompetitor(t *testing.T) {
	players := []Player{
		{ID: "a", Name: "John Smith", Dojo: "Kenshinkan"},
		{ID: "b", Name: "Jane Doe", Dojo: "Kendo-Kai"},
	}
	err := ApplySeeds(players, []domain.SeedAssignment{{Name: "john smith", Dojo: "kenshinkan", SeedRank: 1}})
	require.NoError(t, err)
	assert.Equal(t, 1, players[0].Seed)
	assert.Equal(t, 0, players[1].Seed)
}

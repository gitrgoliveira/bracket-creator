package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNormalizeParticipantName covers the core normalization semantics required
// by the dedup design: Latin diacritics fold, Japanese dakuten preserved, CJK
// untouched, whitespace collapsed.
func TestNormalizeParticipantName(t *testing.T) {
	cases := []struct {
		desc  string
		input string
		want  string
	}{
		{"lowercase", "Alice Smith", "alice smith"},
		{"trim spaces", "  Bob  ", "bob"},
		{"collapse internal spaces", "Ana  Maria  Rossi", "ana maria rossi"},
		{"Latin diacritic fold, Müller", "Müller", "muller"},
		{"Latin diacritic fold, Ï", "Ï", "i"},
		{"Latin diacritic fold, accented name", "Résumé Café", "resume cafe"},
		{"Latin diacritic fold, e-acute", "Renée", "renee"},
		// Japanese dakuten combining mark U+3099 is OUTSIDE the stripped range
		// and MUST survive. が = U+304C (precomposed) or か+U+3099 (combining).
		// After NFD decompose it becomes か(U+304B) + ゛(U+3099); U+3099 is
		// outside [U+0300,U+036F] so it is NOT stripped; re-NFC gives が again.
		{"Japanese dakuten preserved, が", "が", "が"},
		{"Japanese dakuten preserved, full word", "剣道が好き", "剣道が好き"},
		// CJK characters must pass through untouched.
		{"CJK zekken, 渡邉", "渡邉", "渡邉"},
		{"CJK zekken, 早大 堀池", "早大 堀池", "早大 堀池"},
		{"mixed Latin+diacritic", "São Paulo", "sao paulo"},
		{"empty string", "", ""},
		{"already normalized", "alice smith", "alice smith"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			got := NormalizeParticipantName(tc.input)
			assert.Equalf(t, tc.want, got, "NormalizeParticipantName(%q)", tc.input)
		})
	}
}

// TestCheckDuplicateEntriesByNameDojo tests the Tier-1 perfect-match logic.
func TestCheckDuplicateEntriesByNameDojo(t *testing.T) {
	cases := []struct {
		desc    string
		entries [][2]string
		wantLen int // number of colliding entries reported
	}{
		{
			desc: "exact duplicate name+dojo",
			entries: [][2]string{
				{"John Smith", "Wakaba"},
				{"John Smith", "Wakaba"},
			},
			wantLen: 1,
		},
		{
			desc: "same name different dojo, ALLOWED",
			entries: [][2]string{
				{"John Smith", "Wakaba"},
				{"John Smith", "Tora"},
			},
			wantLen: 0,
		},
		{
			desc: "diacritic fold collision, Müller/muller same dojo",
			entries: [][2]string{
				{"Müller", "Wakaba"},
				{"muller", "Wakaba"},
			},
			wantLen: 1,
		},
		{
			desc: "diacritic fold with whitespace variant",
			entries: [][2]string{
				{"Müller", "wakaba "},
				{"muller / wakaba", ""},
				{"muller", "wakaba"},
			},
			// muller/wakaba vs muller/wakaba collide
			wantLen: 1,
		},
		{
			desc: "teams with empty dojo, two Shudokan collide",
			entries: [][2]string{
				{"Shudokan", ""},
				{"Shudokan", ""},
			},
			wantLen: 1,
		},
		{
			desc: "teams Shudokan A / Shudokan B, different names, ALLOWED",
			entries: [][2]string{
				{"Shudokan A", ""},
				{"Shudokan B", ""},
			},
			wantLen: 0,
		},
		{
			desc: "unique list",
			entries: [][2]string{
				{"Alice", "Dojo A"},
				{"Bob", "Dojo A"},
				{"Alice", "Dojo B"},
			},
			wantLen: 0,
		},
		{
			desc:    "empty list",
			entries: [][2]string{},
			wantLen: 0,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			got := CheckDuplicateEntriesByNameDojo(tc.entries)
			assert.Lenf(t, got, tc.wantLen, "entries=%v", tc.entries)
		})
	}
}

// TestFindNearDupWarnings_TokenSubset tests Signal 1 (token subset).
func TestFindNearDupWarnings_TokenSubset(t *testing.T) {
	cases := []struct {
		desc     string
		entries  [][2]string
		wantFire bool
	}{
		{
			desc: "middle-name-drop pattern fires (token-subset)",
			entries: [][2]string{
				{"Ana Maria Rossi", "Wakaba"},
				{"Ana Rossi", "Wakaba"},
			},
			wantFire: true,
		},
		{
			desc: "Shudokan A / Shudokan B does NOT fire (single-char trailing diff)",
			entries: [][2]string{
				{"Shudokan A", ""},
				{"Shudokan B", ""},
			},
			wantFire: false,
		},
		{
			desc: "Tora A / Tora B does NOT fire",
			entries: [][2]string{
				{"Tora A", "Tora Dojo"},
				{"Tora B", "Tora Dojo"},
			},
			wantFire: false,
		},
		{
			desc: "Manchester X / Manchester Z does NOT fire",
			entries: [][2]string{
				{"Manchester X", "Manchester KC"},
				{"Manchester Z", "Manchester KC"},
			},
			wantFire: false,
		},
		{
			desc: "GB men / GB women does NOT fire (different non-trivial token)",
			entries: [][2]string{
				{"GB men", ""},
				{"GB women", ""},
			},
			// "men" ⊄ {"gb","women"} and "women" ⊄ {"gb","men"}, so no token-subset
			// and Levenshtein "gb men" vs "gb women" = 2, ratio = 1 - 2/8 = 0.75 < 0.85, no gate
			wantFire: false,
		},
		{
			desc: "single-token names A / B (squad suffix) does NOT fire",
			entries: [][2]string{
				{"Shobukai A", ""},
				{"Shobukai B", ""},
			},
			wantFire: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			w := FindNearDupWarnings(tc.entries)
			if tc.wantFire {
				assert.NotEmptyf(t, w, "expected near-dup warning for %v", tc.entries)
			} else {
				assert.Emptyf(t, w, "expected NO near-dup warning for %v", tc.entries)
			}
		})
	}
}

// TestFindNearDupWarnings_Levenshtein tests Signal 2 (Levenshtein typo gate).
func TestFindNearDupWarnings_Levenshtein(t *testing.T) {
	cases := []struct {
		desc     string
		a        string
		b        string
		wantFire bool
	}{
		// lev=1 (one char deleted), ratio = 1 - 1/5 = 0.80 < 0.85 → NO
		{"Smith vs Smit (lev=1, ratio<0.85 for short name)", "Smith", "Smit", false},
		// lev=1, ratio = 1 - 1/9 ≈ 0.89 ≥ 0.85 → YES
		{"Takahashi vs Takahasi (lev=1, ratio>=0.85)", "Takahashi", "Takahasi", true},
		// lev=2, ratio = 1 - 2/10 = 0.80 < 0.85 → NO
		{"Smith vs Smath (lev=2, ratio<0.85)", "Smith", "Smath", false},
		// lev=1, ratio = 1 - 1/9 ≈ 0.89 ≥ 0.85 → YES (one extra char appended)
		{"Yamamoto vs Yamamotoo (lev=1, ratio≈0.89)", "Yamamoto", "Yamamotoo", true},
		// lev=3 → never fires
		{"Three edits never fires", "abcdef", "xyz123", false},
		// Squad suffix suppression: Shobukai A vs Shobukai B, lev=1, ratio high
		// but suppressed by isSingleTrailingTokenDiff.
		{"Shobukai A vs Shobukai B, suppressed", "Shobukai A", "Shobukai B", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			entries := [][2]string{{tc.a, "SameDojo"}, {tc.b, "SameDojo"}}
			w := FindNearDupWarnings(entries)
			if tc.wantFire {
				assert.NotEmptyf(t, w, "expected near-dup warning: %q vs %q", tc.a, tc.b)
				if len(w) > 0 {
					assert.Equal(t, "near-duplicate", w[0].Kind)
				}
			} else {
				assert.Emptyf(t, w, "expected NO near-dup warning: %q vs %q", tc.a, tc.b)
			}
		})
	}
}

// TestLevenshtein tests the internal levenshtein function directly.
func TestLevenshtein(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"abc", "abc", 0},
		{"kitten", "sitting", 3},
		{"Takahashi", "Takahasi", 1},
		{"Yamamoto", "Yamamotoo", 1},
	}
	for _, tc := range cases {
		got := levenshtein(tc.a, tc.b)
		assert.Equalf(t, tc.want, got, "levenshtein(%q, %q)", tc.a, tc.b)
	}
}

// TestIsSingleTrailingTokenDiff tests the squad-suffix suppression helper.
func TestIsSingleTrailingTokenDiff(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"shobukai a", "shobukai b", true},
		{"manchester x", "manchester z", true},
		{"tora a", "tora b", true},
		{"gb men", "gb women", false},           // last tokens differ, both > 1 char
		{"shobukai", "shudokan", false},         // only one token each
		{"a b c x", "a b c y", true},            // multi-token, last is single char
		{"ana maria rossi", "ana rossi", false}, // token counts differ
	}
	for _, tc := range cases {
		got := isSingleTrailingTokenDiff(tc.a, tc.b)
		assert.Equalf(t, tc.want, got, "isSingleTrailingTokenDiff(%q, %q)", tc.a, tc.b)
	}
}

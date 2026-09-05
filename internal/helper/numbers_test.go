package helper

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignPlayerNumbers(t *testing.T) {
	t.Run("basic numbering with prefix", func(t *testing.T) {
		players := makePlayers(3)

		next := AssignPlayerNumbers(players, "A", 1)

		require.Equal(t, 4, next)
		assert.Equal(t, "A1", players[0].Number)
		assert.Equal(t, "A2", players[1].Number)
		assert.Equal(t, "A3", players[2].Number)
	})

	t.Run("empty prefix produces bare numbers", func(t *testing.T) {
		players := makePlayers(2)

		next := AssignPlayerNumbers(players, "", 1)

		require.Equal(t, 3, next)
		assert.Equal(t, "1", players[0].Number)
		assert.Equal(t, "2", players[1].Number)
	})

	t.Run("empty slice returns start unchanged and mutates nothing", func(t *testing.T) {
		var players []Player

		next := AssignPlayerNumbers(players, "A", 5)

		assert.Equal(t, 5, next)
		assert.Empty(t, players)
	})

	t.Run("chaining continues sequence across slices without gaps or duplicates", func(t *testing.T) {
		// pool1's own numbering (A1..A3) is pinned by the first subtest;
		// here only the continuation matters: the returned counter feeds the
		// next slice with no gap or duplicate.
		pool1 := makePlayers(3)
		pool2 := makePlayers(2)

		next := AssignPlayerNumbers(pool1, "A", 1)
		require.Equal(t, 4, next)

		next = AssignPlayerNumbers(pool2, "A", next)
		require.Equal(t, 6, next)

		assert.Equal(t, "A4", pool2[0].Number)
		assert.Equal(t, "A5", pool2[1].Number)
	})

	t.Run("non-1 start value", func(t *testing.T) {
		players := makePlayers(2)

		next := AssignPlayerNumbers(players, "K", 10)

		require.Equal(t, 12, next)
		assert.Equal(t, "K10", players[0].Number)
		assert.Equal(t, "K11", players[1].Number)
	})
}

// TestNumberPools pins the shape G1 keeps unchanged: ONE counter running
// straight through the pools in the order they're given (their published
// court order at every real call site), restarting nowhere.
func TestNumberPools(t *testing.T) {
	t.Run("counter runs through pools with no restart", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: makePlayers(4)},
			{PoolName: "Pool B", Players: makePlayers(3)},
		}

		NumberPools(pools, "K")

		assert.Equal(t, "K1", pools[0].Players[0].Number)
		assert.Equal(t, "K4", pools[0].Players[3].Number)
		assert.Equal(t, "K5", pools[1].Players[0].Number, "second pool must continue the counter, not restart at 1")
		assert.Equal(t, "K7", pools[1].Players[2].Number)
	})

	t.Run("no pools is a no-op", func(t *testing.T) {
		var pools []Pool
		assert.NotPanics(t, func() { NumberPools(pools, "K") })
	})
}

// TestDefaultNumberPrefix covers the derivation itself: initials, escalating
// disambiguation against the taken set (bare initial, then progressively more
// of them, then a numeric suffix), the length cap, the no-ASCII-letters
// fallback, and case-insensitive comparison against taken.
func TestDefaultNumberPrefix(t *testing.T) {
	tests := []struct {
		name     string
		compName string
		taken    []string
		want     string
	}{
		{
			name:     "bare initial when nothing taken",
			compName: "Kendo Open",
			taken:    nil,
			want:     "K",
		},
		{
			name:     "escalates to two initials when the bare one collides",
			compName: "Kendo Open",
			taken:    []string{"K"},
			want:     "KO",
		},
		{
			// A plain digit suffix on "KO" ("KO2".."KO99") is ambiguous with
			// "KO" itself for every value (bc-pnum A1), so once both
			// initials-based candidates are taken, the derivation skips the
			// whole plain-suffix band on "KO" and falls to the zero-padded
			// escape on the shorter stem "K": "K02".
			name:     "escalates to a zero-padded numeric suffix once initials are exhausted",
			compName: "Kendo Open",
			taken:    []string{"K", "KO"},
			want:     "K02",
		},
		{
			// Climbing within the zero-padded band: "K02" is taken, so the
			// next untaken, unambiguous candidate is "K03".
			name:     "numeric suffix climbs past a taken K02",
			compName: "Kendo Open",
			taken:    []string{"K", "KO", "K02"},
			want:     "K03",
		},
		{
			name:     "initials cap at MaxNumberPrefixLen (3)",
			compName: "Kendo Open Regional",
			taken:    nil,
			want:     "K",
		},
		{
			// "KOR" (the full initials) is also taken, so the plain suffix on
			// its 2-char trim "KO" is ambiguous with "KO" itself, same as
			// above: the derivation falls to "K02".
			name:     "a 3-letter initials stem trims to fit a numeric suffix under the cap",
			compName: "Kendo Open Regional",
			taken:    []string{"K", "KO", "KOR"},
			want:     "K02",
		},
		{
			name:     "taken comparison is case-insensitive",
			compName: "Kendo Open",
			taken:    []string{"k"},
			want:     "KO",
		},
		{
			name:     "empty name falls back to the kendo default",
			compName: "",
			taken:    nil,
			want:     DefaultNumberPrefixFallback,
		},
		{
			name:     "a name with no ASCII letters falls back to the kendo default",
			compName: "東京 2026",
			taken:    nil,
			want:     DefaultNumberPrefixFallback,
		},
		{
			name:     "punctuation and digits are word separators, not letters",
			compName: "Kendo - Open (Senior)",
			taken:    []string{"K"},
			want:     "KO",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultNumberPrefix(tc.compName, tc.taken)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqualf(t, len(got), MaxNumberPrefixLen, "derived prefix %q must never exceed the length cap", got)
		})
	}
}

// TestDefaultNumberPrefix_ExhaustedSuffixesNeverPanics pins F8 (bc-pnum
// review) as extended by A1: the numeric-suffix fallback groups candidates by
// digit WIDTH (plain, then zero-padded), bounded at MaxNumberPrefixLen-1 so
// the stem is never trimmed to zero characters -- a candidate with no stem
// letter at all ("100") is never emitted, and the loop cannot run past the
// length cap into a negative trim. A taken set covering EVERY candidate the
// derivation could try (the two initials, the whole plain-suffix band that
// "KO" makes unavoidably ambiguous, and every zero-padded width-2 candidate)
// exhausts the derivation entirely; it must degrade to "return its best
// guess" rather than loop forever, panic, or grow past the length cap.
func TestDefaultNumberPrefix_ExhaustedSuffixesNeverPanics(t *testing.T) {
	taken := []string{"K", "KO"}
	// Width-2 zero-padded candidates ("K02".."K99"): these are NOT ambiguous
	// with "K" (leading zero) or "KO" (no prefix relation), so they must be
	// blocked explicitly, by exact match, to close off the escape this bead
	// adds.
	for n := 2; n <= 99; n++ {
		taken = append(taken, fmt.Sprintf("K%02d", n))
	}

	var got string
	assert.NotPanicsf(t, func() {
		got = DefaultNumberPrefix("Kendo Open", taken)
	}, "every candidate up to the length cap is taken; the derivation must degrade gracefully, not panic")

	// Every candidate is now taken or ambiguous, so the function returns the
	// LAST one it tried (the top of the width-2 zero-padded band) rather than
	// looping forever or growing past the length cap. checkUniqueCompFields
	// is what actually rejects this collision at save time, with its own
	// "already used by competition ..." error naming the real conflict.
	assert.Equal(t, "K99", got)
	assert.LessOrEqual(t, len(got), MaxNumberPrefixLen)
}

// TestNumberPrefixesAmbiguous pins the primitive bc-pnum A1 adds: a stem
// prefix is ambiguous with any non-zero-leading digit extension of itself
// (because AssignPlayerNumbers's counter reaches that string), but not with a
// zero-leading one (the counter never left-pads) and not with a
// letter-suffixed extension (only digit runs collide with a counter).
func TestNumberPrefixesAmbiguous(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"stem vs its own non-zero-leading extension", "K", "K2", true},
		{"stem vs a two-digit non-zero-leading extension", "K", "K21", true},
		{"order does not matter", "K2", "K", true},
		{"case-insensitive", "k", "K2", true},
		{"zero-leading extension is exempt", "K", "K02", false},
		{"letter-suffixed extension is not a digit run", "K", "KO", false},
		{"exactly equal prefixes are not reported here", "K", "K", false},
		{"unrelated prefixes", "K", "S2", false},
		{"empty inputs never ambiguous", "", "K2", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, NumberPrefixesAmbiguous(tc.a, tc.b))
		})
	}
}

// TestDefaultNumberPrefix_KendoWithKTakenDerivesSomethingUnambiguous is the
// bc-pnum A1 repro made concrete: a SINGLE-WORD name's initials-derived stem
// is exactly one character (nameInitials never contributes more than one
// initial per word), so when that bare stem is already taken, EVERY plain
// digit suffix on it ("K2".."K99") is unavoidably ambiguous with it -- there
// is no letter-based escalation available for a one-word name. The
// zero-padded width-2 band is the only way out, and the result must be
// verified unambiguous with the taken set, not just present in it.
func TestDefaultNumberPrefix_KendoWithKTakenDerivesSomethingUnambiguous(t *testing.T) {
	got := DefaultNumberPrefix("Kendo", []string{"K"})
	assert.Equal(t, "K02", got)
	assert.False(t, NumberPrefixesAmbiguous(got, "K"), "the derived prefix must not be ambiguous with the taken one")
}

// TestNameInitials_AccentedAndOtherScripts pins the word-initial rule: a Latin
// letter with diacritics folds to its base, a letter from another script
// starts a word but contributes no initial, and non-letters separate words.
func TestNameInitials_AccentedAndOtherScripts(t *testing.T) {
	for _, tc := range []struct {
		name string
		want string
	}{
		{"Épée Open", "EO"},
		{"Ångström Cup", "AC"},
		{"Öl Kendo", "OK"},
		{"élite", "E"},
		{"剣道 Open", "O"},
		{"剣道", ""},
		{"café-au-lait", "CAL"},
		{"Under 18", "U"},
		{"Kendo Open Senior Final", "KOS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, nameInitials(tc.name))
		})
	}
	assert.Equal(t, "E", DefaultNumberPrefix("Épée Open", nil), "the accented initial reaches the derived prefix")
	assert.Equal(t, DefaultNumberPrefixFallback, DefaultNumberPrefix("剣道", nil), "no Latin letters: the fallback")
}

// TestSplitNumberLines pins the bc-pnum operator ruling: a competitor number
// prints as two stacked lines only when its prefix is MORE THAN ONE
// CHARACTER (a rune count, not a byte count -- bc-pnum review) AND there are
// digits to put below it. A one-letter prefix, a bare digit string (the
// empty-prefix numbering AssignPlayerNumbers also supports), and a prefix
// with no digits at all each stay single-line. The Unicode cases pin the
// rune-vs-byte fix directly: "Ö" is ONE character but two UTF-8 bytes, so a
// byte-length check would wrongly stack "Ö20"; "劃" is one character at
// three UTF-8 bytes, same wrong-by-byte-count trap. The legacy
// digit-bearing cases document a pre-existing, accepted limitation: a
// digit inside the STORED prefix itself ("K2", "KO2") is indistinguishable
// from that digit being the counter's first digit, so "K220" reads as
// prefix "K" + counter "220", and "KO220" as prefix "KO" + counter "220" --
// both already correct under the leading-non-digit-run rule, kept here as
// regression coverage rather than new behaviour.
func TestSplitNumberLines(t *testing.T) {
	tests := []struct {
		name        string
		number      string
		wantLetters string
		wantDigits  string
		wantStacked bool
	}{
		{"one-letter prefix stays single-line", "K20", "K", "20", false},
		{"two-letter prefix stacks", "KO20", "KO", "20", true},
		{"three-letter prefix stacks with a three-digit number", "KOR120", "KOR", "120", true},
		{"bare digits, no prefix", "20", "", "20", false},
		{"one-letter prefix, no digits", "K", "K", "", false},
		{"multi-letter prefix, no digits, stays single-line", "KO", "KO", "", false},
		{"empty number", "", "", "", false},
		{"single accented letter is ONE character, stays single-line", "Ö20", "Ö", "20", false},
		{"accented letter plus a second letter is two characters, stacks", "ÖZ20", "ÖZ", "20", true},
		{"single multi-byte CJK-style letter is ONE character, stays single-line", "劃20", "劃", "20", false},
		{"legacy digit-bearing one-letter prefix stays single-line", "K220", "K", "220", false},
		{"legacy digit-bearing two-letter prefix stacks", "KO220", "KO", "220", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			letters, digits, stacked := splitNumberLines(tc.number)
			assert.Equal(t, tc.wantLetters, letters)
			assert.Equal(t, tc.wantDigits, digits)
			assert.Equal(t, tc.wantStacked, stacked)
		})
	}
}

// TestFirstNumberedSplit pins the "decide once per sheet" rule: the first
// player carrying a Number stands for every player on the sheet, and an
// unnumbered slice (or one with no players at all) reports stacked=false so
// callers keep the single-line layout rather than mis-reading a zero value
// as "single-letter prefix". Narrowed to (letters, stacked): no caller reads
// digits or a separate found/not-found flag (bc-pnum review).
func TestFirstNumberedSplit(t *testing.T) {
	t.Run("skips unnumbered players and uses the first numbered one", func(t *testing.T) {
		players := []Player{
			{Name: "Unnumbered"},
			{Name: "Alice", Number: "KO20"},
			{Name: "Bob", Number: "KO21"},
		}
		letters, stacked := firstNumberedSplit(players)
		assert.Equal(t, "KO", letters)
		assert.True(t, stacked)
	})

	t.Run("no numbered player reports stacked=false and empty letters", func(t *testing.T) {
		players := []Player{{Name: "Alice"}, {Name: "Bob"}}
		letters, stacked := firstNumberedSplit(players)
		assert.Equal(t, "", letters)
		assert.False(t, stacked)
	})

	t.Run("empty slice reports stacked=false and empty letters", func(t *testing.T) {
		letters, stacked := firstNumberedSplit(nil)
		assert.Equal(t, "", letters)
		assert.False(t, stacked)
	})
}

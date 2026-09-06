package helper

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// DefaultNumberPrefixFallback is the prefix DefaultNumberPrefix returns when a
// name yields no usable letters at all (an empty name, or one written entirely
// in a script this ASCII-only derivation cannot reduce). "K" for kendo.
const DefaultNumberPrefixFallback = "K"

// MaxNumberPrefixLen bounds a derived prefix. It matches the admin UI's
// maxLength="3"; mobileapp's MaxLenCompetitionNumberPrefix is an alias of
// this constant, not a second copy -- the dependency runs mobileapp ->
// helper (presentation depends on business logic, never the reverse), so
// this is the ONE place the cap is stated and every other layer imports it,
// including ValidateNumberPrefix below, which DefaultNumberPrefix must never
// propose a value that validator would then reject.
const MaxNumberPrefixLen = 3

// CompetitorNumber is the one composition of a competitor number: prefix
// then counter, no separator (e.g. CompetitorNumber("K", 3) == "K3").
func CompetitorNumber(prefix string, n int) string {
	return fmt.Sprintf("%s%d", prefix, n)
}

// AssignPlayerNumbers sets Number on each player to prefix+counter, where counter
// starts at start and increments by one. Returns the next counter value so callers
// can chain across multiple slices (e.g. pools).
//
// This is the ONE place the competitor-number string is composed. Three callers
// used to spell `prefix + strconv(i+1)` themselves (the pool loop in
// engine/pools.go, its verbatim twin in cmd/create-pools.go, and the playoffs
// re-derivation in mobileapp/handlers_viewer.go); they now all route here, so a
// change to the number's shape cannot reach one surface and miss another.
func AssignPlayerNumbers(players []Player, prefix string, start int) int {
	for i := range players {
		players[i].Number = CompetitorNumber(prefix, start+i)
	}
	return start + len(players)
}

// splitNumberLines splits a competitor number into the sheet's own known
// prefix and the remaining digits, and reports whether the pair should be
// PRINTED as two stacked lines rather than one (stackedNumberPrefix) AND
// number actually carries that prefix.
//
// The split is prefix-DRIVEN, not guessed from number's own shape (bc-pnum
// review): number was previously cut at its first ASCII digit, which
// silently mistook a digit-bearing prefix (DefaultNumberPrefix can legitimately
// derive "K2" or "KO2") for the boundary -- competitor 1 under prefix "KO2" is
// "KO21", which the old first-digit rule split as "KO"/"21" (reading as
// competitor 21 of "KO") instead of the correct "KO2"/"1". Knowing the actual
// prefix removes the guess.
//
// A number that does NOT carry the sheet's own prefix is hand-edited or
// legacy data: this is never guessed at with a fabricated cut (the same
// report-over-fabricate rule D1 applies to the printed number itself) -- it
// renders single-line, letters=="" digits=="". A bare digit string (no
// prefix, the empty-prefix numbering AssignPlayerNumbers also supports) and
// an empty prefix both fall into this case too.
//
// This is the ONE place that decides the split, for both print sites (the
// Tags sheet, internal/helper/excel_tags.go, and the Names to Print number
// cell, printNameEntries in excel.go): a change to what counts as a valid
// split only has to change here.
func splitNumberLines(number, prefix string) (letters, digits string, stacked bool) {
	if prefix == "" || !strings.HasPrefix(number, prefix) || number == prefix {
		return "", "", false
	}
	letters, digits = prefix, number[len(prefix):]
	stacked = stackedNumberPrefix(prefix)
	return letters, digits, stacked
}

// stackedNumberPrefix reports whether a number prefix is more than one
// CHARACTER (rune, not byte -- bc-pnum review: "Ö20" is a ONE-letter prefix
// and must stay single-line, but len("Ö") is 2 bytes in UTF-8, so a
// byte-length check wrongly stacked it). This is the ONE place that decides
// that layout question; every site laying out a prefix+digits number across
// one or two lines calls it rather than re-spelling the rune-count check.
func stackedNumberPrefix(prefix string) bool {
	return utf8.RuneCountInString(prefix) > 1
}

// NumberPools numbers every competitor across pools with a single counter that
// runs THROUGH the pools in their final order: with prefix "K" and pools of
// four, K1-K4 are the first pool, K5-K8 the second. The numbering is therefore
// only meaningful once the pools are in their published order, which they are
// at every call site: ReorderPoolsForCourts runs last inside pool formation and
// reassigns PoolName, so the slice handed here already IS the published sheet.
//
// Callers must pass a non-empty prefix. A competition's prefix is never empty
// by the time it reaches this call: the app assigns one before any draw (at
// create, at settings save, and at start/generate-draw for a legacy record
// that predates this rule), and the CLI resolves one when --number-prefix is
// omitted. There is no unnumbered mode to fall back to, and this function does
// not guard against an empty prefix reaching it: that guarantee belongs at the
// boundary that persists or draws, not here (see bc-pnum).
func NumberPools(pools []Pool, prefix string) {
	counter := 1
	for i := range pools {
		counter = AssignPlayerNumbers(pools[i].Players, prefix, counter)
	}
}

// NormalizeNumberPrefix is the ONE case/whitespace fold every number-prefix
// uniqueness decision compares under (bc-pnum review): before this,
// the fold was spelled three times independently -- strings.EqualFold at the
// mobileapp handler boundary, strings.ToUpper here in
// NumberPrefixesAmbiguous, and a second strings.ToUpper building
// DefaultNumberPrefix's own `used` map -- three call sites that could in
// principle drift out of agreement on what "the same prefix" means.
func NormalizeNumberPrefix(s string) string {
	return strings.ToUpper(strings.TrimSpace(s))
}

// NumberPrefixesAmbiguous reports whether two number prefixes can produce an
// identical competitor tag once AssignPlayerNumbers appends a counter to
// each: compared under NormalizeNumberPrefix, one is ambiguous with the
// other when it equals the other followed by a run of digits whose first
// digit is not '0'. "K" and "K2" are ambiguous because K's own counter
// reaches "K21" (its 21st entrant) at the same string K2's 1st entrant gets
// ("K2"+"1"). A leading zero breaks the ambiguity on purpose:
// AssignPlayerNumbers's fmt.Sprintf("%s%d", ...) never left-pads its
// counter, so a prefix like "K02" can never coincide with "K"'s own sequence
// (which only ever produces "K1".."K9","K10",... -- never "K02"). Two
// genuinely equal prefixes are NOT reported here: that collision is the
// pre-existing exact-match check (checkUniqueCompFields,
// DefaultNumberPrefix's own normalized-equality check); this function
// covers only the stem+digits shape those checks miss.
func NumberPrefixesAmbiguous(a, b string) bool {
	return numberPrefixesAmbiguousNormalized(NormalizeNumberPrefix(a), NormalizeNumberPrefix(b))
}

// numberPrefixesAmbiguousNormalized is NumberPrefixesAmbiguous' comparison,
// over a pair the CALLER has already run through NormalizeNumberPrefix
// (bc-pnum review): DefaultNumberPrefix's own acceptable closure below
// normalizes `taken` once, up front, and each candidate once per call, so
// routing its per-(candidate, taken) comparisons through this rather than
// NumberPrefixesAmbiguous itself skips a re-normalization NumberPrefixesAmbiguous
// would otherwise repeat on every one of those comparisons.
// NumberPrefixesAmbiguous is unchanged for external callers, which do not
// have a pre-normalized pair to hand it.
func numberPrefixesAmbiguousNormalized(a, b string) bool {
	if a == "" || b == "" || a == b {
		return false
	}
	return isDigitExtension(a, b) || isDigitExtension(b, a)
}

// isDigitExtension reports whether long equals short followed by a
// non-empty, non-zero-leading run of ASCII digits. Both arguments are assumed
// already upper-cased/trimmed by the caller.
func isDigitExtension(long, short string) bool {
	if !strings.HasPrefix(long, short) {
		return false
	}
	rest := long[len(short):]
	if rest == "" || rest[0] == '0' {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// DefaultNumberPrefix derives a competition's default number prefix from its
// name, avoiding every prefix in taken (compared case-insensitively, as
// checkUniqueCompFields compares them) and every prefix ambiguous with one
// (NumberPrefixesAmbiguous).
//
// The derivation walks the name's words and takes their initials, so "Kendo
// Open" gives "K" and, if "K" is taken, "KO". When initials are exhausted or
// still collide or are ambiguous, a numeric suffix is appended to the
// shortest stem that leaves room for it: see the width-grouped suffix loop
// below. Every candidate is capped at MaxNumberPrefixLen so the value can
// never be one the length validator would reject.
//
// The result is deterministic: the same name and the same taken set always
// produce the same prefix, which is what lets the create form show the operator
// the value the server would pick.
func DefaultNumberPrefix(name string, taken []string) string {
	// bc-pnum review: ONE loop over the trimmed taken slice, no
	// separate upper-cased `used` map -- exact equality and ambiguity are
	// both decided under the same NormalizeNumberPrefix fold, so a
	// candidate can never pass one check under a fold the other rejects.
	// Each entry is stored already normalized (bc-pnum review(d)) so
	// `acceptable` below never re-normalizes it.
	normalized := make([]string, 0, len(taken))
	for _, t := range taken {
		if t = strings.TrimSpace(t); t != "" {
			normalized = append(normalized, NormalizeNumberPrefix(t))
		}
	}
	acceptable := func(candidate string) bool {
		// candidateNorm is computed once per candidate rather than once per
		// (candidate, taken) comparison in the loop below (bc-pnum
		// review(d)); numberPrefixesAmbiguousNormalized then compares the
		// already-normalized pair directly instead of paying
		// NumberPrefixesAmbiguous' own re-normalization on every one of
		// those comparisons.
		candidateNorm := NormalizeNumberPrefix(candidate)
		for _, t := range normalized {
			if t == candidateNorm || numberPrefixesAmbiguousNormalized(candidateNorm, t) {
				return false
			}
		}
		return true
	}

	initials := nameInitials(name)
	if initials == "" {
		initials = DefaultNumberPrefixFallback
	}

	// Prefer a bare initial, then progressively more of them: "K", then "KO".
	for n := 1; n <= len(initials); n++ {
		if candidate := initials[:n]; acceptable(candidate) {
			return candidate
		}
	}

	// Every initial-based candidate collides or is ambiguous, so fall to a
	// numeric suffix. Candidates are grouped by digit WIDTH rather than by a
	// single counter, because a plain (non-zero-leading) digit suffix on a
	// taken stem is UNAVOIDABLY ambiguous with that stem for every value --
	// "KO2".."KO99" are all ambiguous with a taken "KO", with no plain digit
	// escaping it. Width bounds the ambiguity itself: a candidate at width W
	// is tried first without padding (n formatted plainly) so short, natural-
	// looking values like "K2" are still preferred when they are actually
	// available; once a width is exhausted, growing it to W+1 tries the SAME
	// values zero-padded ("02" instead of "2"), which is unambiguous with any
	// stem by construction (see NumberPrefixesAmbiguous) and therefore the
	// escape once a stem's plain suffixes are all taken or ambiguous. Width
	// is bounded at MaxNumberPrefixLen-1 so the stem is never trimmed to zero
	// characters -- a bare-digit candidate ("100") is never emitted, it is
	// indistinguishable from a desk-called number.
	stem := initials
	lastCandidate := stem
	for width := 1; width <= MaxNumberPrefixLen-1; width++ {
		trimmed := stem
		if len(trimmed)+width > MaxNumberPrefixLen {
			trimmed = trimmed[:MaxNumberPrefixLen-width]
		}
		max := 1
		for i := 0; i < width; i++ {
			max *= 10
		}
		max--
		for n := 2; n <= max; n++ {
			candidate := trimmed + fmt.Sprintf("%0*d", width, n)
			lastCandidate = candidate
			if acceptable(candidate) {
				return candidate
			}
		}
	}
	// Exhausted every candidate up to the length cap: every one of them is
	// already taken or ambiguous. Return the last one tried rather than
	// inventing a value beyond MaxNumberPrefixLen -- this function's
	// contract is a best-effort SUGGESTION, not a uniqueness guarantee, and
	// every request-driven caller validates that (via checkUniqueCompFields,
	// against the SAME taken set) before trusting it:
	//   - create, settings-save and the start/generate-draw pre-flight call
	//     this function ONLY to fill in a blank field, validate the
	//     resulting (derived-or-caller-supplied) prefix ONCE, and reject a
	//     collision outright with an error naming the actual conflict --
	//     there is no retry, because an explicit collision on one of these
	//     paths is the operator's own request to fix, not legacy data to
	//     heal.
	//   - import (mobileapp.importCompetition) additionally RE-DERIVES on a
	//     collision: a restored archive's collision is routinely legacy data
	//     that predates the ambiguity/uniqueness rule (this bead's governing
	//     rule is ASSIGN, never reject, on legacy data), so the row is
	//     healed by calling this function a second time and RE-VALIDATING
	//     that second candidate before trusting it -- catching the
	//     exhaustion case above, where the last-tried candidate this
	//     function returns can itself already be taken. Only when that
	//     second validation also fails (exhaustion reached twice) does
	//     import refuse the row, naming the conflict exactly like the
	//     non-retrying callers above.
	// The one caller that does not re-validate at all, the load-time
	// migration (engine.MigrateNumberPrefixes), derives over the taken set
	// of every competition it could READ, including its own in-pass
	// assignments; a competition it had to skip keeps a prefix that set does
	// not know, so a collision with it is possible and surfaces as that
	// competition's next settings save being refused, which names the
	// conflict.
	return lastCandidate
}

// nameInitials reduces a name to the uppercased ASCII initials of its words,
// capped at MaxNumberPrefixLen. Any letter starts or continues a word and any
// non-letter separates words, so "Kendo - Open (Senior)" reads as three words
// and "Under 18" as one. A word's initial is the letter's Latin base
// (initialOf): "Épée Open" gives "EO", not "PO". A word starting with a letter
// outside the Latin script contributes no initial, so "剣道 Open" gives "O",
// and a name with no Latin letters at all returns "", which is what makes the
// fallback constant necessary.
func nameInitials(name string) string {
	var b strings.Builder
	inWord := false
	for _, r := range name {
		if !unicode.IsLetter(r) {
			inWord = false
			continue
		}
		if !inWord {
			if c, ok := initialOf(r); ok {
				b.WriteByte(c)
				if b.Len() == MaxNumberPrefixLen {
					return b.String()
				}
			}
		}
		inWord = true
	}
	return b.String()
}

// initialOf returns the uppercased ASCII letter a word-initial letter
// contributes to a prefix. A Latin letter carrying diacritics folds to its
// base through NFD decomposition ("É" -> "E", "Å" -> "A", "Ö" -> "O"), so an
// accented first letter is captured rather than skipped. A letter that has no
// ASCII base (another script) contributes nothing: a competitor number is a
// tag the desk calls and prints, and the derived prefix stays within A-Z so
// it never depends on a font, a keyboard or a URL encoder.
func initialOf(r rune) (byte, bool) {
	base, _ := utf8.DecodeRuneInString(norm.NFD.String(string(r)))
	switch {
	case base >= 'A' && base <= 'Z':
		return byte(base), true
	case base >= 'a' && base <= 'z':
		return byte(base - 'a' + 'A'), true
	}
	return 0, false
}

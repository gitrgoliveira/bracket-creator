package helper

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Fuzzy duplicate detection constants — document the threshold choices so
// reviewers can reason about them without digging into the algorithm.
const (
	// NearDupLevenshteinMax is the maximum edit distance that the secondary
	// (Levenshtein) typo gate will consider as a near-duplicate.
	NearDupLevenshteinMax = 2

	// NearDupRatioMin is the minimum similarity ratio (1 - lev/maxLen) required
	// alongside NearDupLevenshteinMax.  0.85 means "at most ~15 % different".
	NearDupRatioMin = 0.85
)

// NearDupWarning describes a single near-duplicate pair.
type NearDupWarning struct {
	// Kind is always "near-duplicate".
	Kind string `json:"kind"`
	// A and B are the normalized display names of the near-duplicate pair.
	A string `json:"a"`
	// B is the second member of the pair.
	B string `json:"b"`
	// Score is a human-readable description of why the pair fired (e.g.
	// "token-subset" or "levenshtein:2/ratio:0.90").
	Score string `json:"score"`
}

// NormalizeParticipantName applies the shared normalization used by both the
// perfect-match dedup key and the near-duplicate signals:
//
//  1. NFC precompose (canonical composed form)
//  2. NFD decompose, then strip ONLY combining marks in U+0300–U+036F
//     (Latin combining diacriticals).  This folds Müller→muller and
//     Ï→i, but MUST leave Japanese dakuten (が) and all CJK/kana intact
//     because dakuten's combining mark U+3099 is outside the stripped range.
//  3. Re-NFC so the result is in canonical composed form.
//  4. Lowercase, trim, and collapse internal whitespace.
func NormalizeParticipantName(s string) string {
	// Step 1+2: NFC → NFD → strip combining marks U+0300..U+036F
	nfd := norm.NFD.String(s)
	var stripped strings.Builder
	stripped.Grow(len(nfd))
	for _, r := range nfd {
		if r >= 0x0300 && r <= 0x036F {
			// Latin combining diacritical mark — drop it.
			continue
		}
		stripped.WriteRune(r)
	}
	// Step 3: re-NFC
	nfc := norm.NFC.String(stripped.String())

	// Step 4: lowercase, trim, collapse whitespace.
	lower := strings.ToLower(nfc)
	trimmed := strings.TrimSpace(lower)
	// Collapse internal runs of whitespace to a single space.
	var out strings.Builder
	out.Grow(len(trimmed))
	prevSpace := false
	for _, r := range trimmed {
		isSpace := unicode.IsSpace(r)
		if isSpace {
			if !prevSpace {
				out.WriteRune(' ')
			}
			prevSpace = true
		} else {
			out.WriteRune(r)
			prevSpace = false
		}
	}
	return out.String()
}

// dupKey is the (normalizedName, normalizedDojo) pair used as the perfect-match
// dedup key.  Two entries with identical dupKey values are perfect duplicates
// regardless of original casing / diacritics.
type dupKey struct {
	name string
	dojo string
}

func newDupKey(name, dojo string) dupKey {
	return dupKey{
		name: NormalizeParticipantName(name),
		dojo: NormalizeParticipantName(dojo),
	}
}

// CheckDuplicateEntriesByNameDojo scans the raw entry list for perfect
// duplicate (name, dojo) pairs.  Each entry is a two-element []string
// {name, dojo}.  Returns a list of "name / dojo" strings that collide;
// empty means the list is unique.
//
// This replaces the old whole-row CheckDuplicateEntries for contexts that
// know the name and dojo fields.
func CheckDuplicateEntriesByNameDojo(entries [][2]string) []string {
	seen := make(map[dupKey]string, len(entries)) // key → original "name / dojo" label
	var out []string
	seenDupes := make(map[dupKey]bool)
	for _, e := range entries {
		k := newDupKey(e[0], e[1])
		if _, exists := seen[k]; exists {
			if !seenDupes[k] {
				seenDupes[k] = true
				out = append(out, seen[k])
			}
		} else {
			seen[k] = e[0] + " / " + e[1]
		}
	}
	return out
}

// tokenSet splits a normalized name into its whitespace-separated tokens and
// returns them as a set.
func tokenSet(normalized string) map[string]struct{} {
	parts := strings.Fields(normalized)
	set := make(map[string]struct{}, len(parts))
	for _, p := range parts {
		set[p] = struct{}{}
	}
	return set
}

// isSubset returns true when every key in a is also in b.
func isSubset(a, b map[string]struct{}) bool {
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// levenshtein computes the edit distance between a and b.
func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	m, n := len(ra), len(rb)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}
	prev := make([]int, n+1)
	curr := make([]int, n+1)
	for j := 0; j <= n; j++ {
		prev[j] = j
	}
	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			if ra[i-1] == rb[j-1] {
				curr[j] = prev[j-1]
			} else {
				c := prev[j]
				if curr[j-1] < c {
					c = curr[j-1]
				}
				if prev[j-1] < c {
					c = prev[j-1]
				}
				curr[j] = c + 1
			}
		}
		prev, curr = curr, prev
	}
	return prev[n]
}

// isSingleTrailingTokenDiff returns true when a and b differ only in their
// last single-character token.  This suppresses the Levenshtein gate for
// the squad-suffix convention: "Shudokan A" vs "Shudokan B", "Tora A" vs
// "Tora B", "Manchester X" vs "Manchester Z".
func isSingleTrailingTokenDiff(na, nb string) bool {
	ta := strings.Fields(na)
	tb := strings.Fields(nb)
	if len(ta) < 2 || len(ta) != len(tb) {
		return false
	}
	// All tokens except the last must match.
	for i := 0; i < len(ta)-1; i++ {
		if ta[i] != tb[i] {
			return false
		}
	}
	lastA := ta[len(ta)-1]
	lastB := tb[len(tb)-1]
	// The differing tokens must both be single characters.
	return len([]rune(lastA)) == 1 && len([]rune(lastB)) == 1
}

// FindNearDupWarnings scans normalised names for near-duplicate pairs.
// Each entry is {name, dojo} already as raw (non-normalized) strings; the
// function normalises internally.  Returns non-blocking warnings.
//
// Signal 1: token-subset — tokens of A ⊆ tokens of B (or vice-versa), and
// token sets are unequal.  Catches "Chau Earn Tan" / "Chau Tan".
//
// Signal 2: Levenshtein typo gate — lev ≤ NearDupLevenshteinMax AND
// ratio ≥ NearDupRatioMin, suppressed when the two strings differ only in
// a single trailing single-character token (squad suffix).
func FindNearDupWarnings(entries [][2]string) []NearDupWarning {
	type entry struct {
		norm   string
		tokens map[string]struct{}
		orig   string
	}
	all := make([]entry, len(entries))
	for i, e := range entries {
		n := NormalizeParticipantName(e[0])
		all[i] = entry{
			norm:   n,
			tokens: tokenSet(n),
			orig:   e[0],
		}
	}

	var warnings []NearDupWarning
	// Track pairs we've already warned about to avoid duplicates.
	type pair struct{ i, j int }
	warned := make(map[pair]bool)

	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if warned[pair{i, j}] {
				continue
			}
			na, nb := all[i].norm, all[j].norm
			if na == nb {
				// Perfect match — handled by Tier-1, skip here.
				continue
			}

			// Signal 1: token-subset
			ta, tb := all[i].tokens, all[j].tokens
			if len(ta) > 0 && len(tb) > 0 && (isSubset(ta, tb) || isSubset(tb, ta)) {
				warned[pair{i, j}] = true
				warnings = append(warnings, NearDupWarning{
					Kind:  "near-duplicate",
					A:     entries[i][0],
					B:     entries[j][0],
					Score: "token-subset",
				})
				continue
			}

			// Signal 2: Levenshtein typo gate
			lev := levenshtein(na, nb)
			if lev <= NearDupLevenshteinMax {
				maxLen := len([]rune(na))
				if len([]rune(nb)) > maxLen {
					maxLen = len([]rune(nb))
				}
				if maxLen == 0 {
					continue
				}
				ratio := 1.0 - float64(lev)/float64(maxLen)
				if ratio >= NearDupRatioMin {
					// Suppress squad-suffix pairs.
					if isSingleTrailingTokenDiff(na, nb) {
						continue
					}
					warned[pair{i, j}] = true
					score := "levenshtein:" + itoa(lev) + "/ratio:" + formatRatio(ratio)
					warnings = append(warnings, NearDupWarning{
						Kind:  "near-duplicate",
						A:     entries[i][0],
						B:     entries[j][0],
						Score: score,
					})
				}
			}
		}
	}
	return warnings
}

// itoa converts int to string without importing strconv here.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// formatRatio formats a ratio to 2 decimal places without fmt.
func formatRatio(r float64) string {
	// r is in [0,1]; format as "0.XX"
	r100 := int(r*100 + 0.5)
	if r100 >= 100 {
		return "1.00"
	}
	const digits = "0123456789"
	hi := r100 / 10
	lo := r100 % 10
	return "0." + digits[hi:hi+1] + digits[lo:lo+1]
}

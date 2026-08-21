package domain

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

// ParseScore is the LEGACY-FOLD half of what used to be a FormatScore/
// ParseScore codec pair. A pool match has always stored IpponsA/IpponsB +
// HansokuA/HansokuB as fields; a bracket match used to render each side into
// one rendered ScoreA/ScoreB string instead, before both shapes were unified
// onto the same ippon-array + hansoku-int fields (state/models.go,
// legacy_hantei.go). FormatScore, the encode half, is gone: nothing produces
// a rendered score string any more, so nothing needs to decode one — except a
// pre-unification bracket.json still on disk, which this function exists
// solely to read once, on load, before its legacy string field is cleared
// (state.BracketMatch.NormalizeLegacy).
//
// Format read by ParseScore: ippon letters joined with no separator, then the
// outstanding-hansoku count in parentheses. "MK (H1)", "MK", "(H1)", "".
//
// IsValidIpponEntry reports whether v is a well-formed single ippon entry: one
// rune, the two-rune HanteiMark, or empty (an unfilled slot, not a scoring
// ippon; CountScoringIppons drops it). validation.go's wire gate on
// freshly-written ippon slices calls this to reject anything else, in
// particular any other multi-rune string — a client-forged two-rune entry
// alongside a genuine HanteiMark would be ambiguous with it.
func IsValidIpponEntry(v string) bool {
	return utf8.RuneCountInString(v) <= 1 || v == HanteiMark
}

// ParseScore: "MK (H1)" → (["M","K"], 1), "MK" → (["M","K"], 0),
// "(H1)" → (nil, 1), "" → (nil, 0).
//
// An absent hansoku count comes back as 0 rather than a negative. A malformed
// count parses as 0 rather than failing: this reads persisted data on a
// live-tournament path, where dropping an unreadable hansoku is better than
// refusing to show the match at all.
//
// HanteiMark is the ONE multi-rune entry the format ever admitted: "t" is not
// a letter that can stand alone in a score, so ParseScore can consume the
// "Ht" pair unambiguously (an "H" ippon is only read as bare hansoku-H when
// NOT followed by "t").
func ParseScore(s string) ([]string, int) {
	s = strings.TrimSpace(s)
	hansoku := 0
	if i := strings.LastIndex(s, "(H"); i >= 0 {
		if j := strings.Index(s[i:], ")"); j >= 0 {
			if n, err := strconv.Atoi(s[i+2 : i+j]); err == nil {
				hansoku = n
			}
			s = strings.TrimSpace(s[:i])
		}
	}
	// Byte lookahead, not []rune(s): 'H' and 't' are both ASCII (one byte
	// each), so s[i+1] safely peeks the byte right after a rune-range 'H'
	// without decoding the whole string up front. skipNext consumes the 't'
	// that was already folded into the HanteiMark token, so the next
	// range step (which lands exactly on that byte) does not re-tokenize it.
	var ippons []string
	skipNext := false
	for i, r := range s {
		if skipNext {
			skipNext = false
			continue
		}
		if r == ' ' {
			continue
		}
		// The hantei mark is the codec's one two-rune token; see
		// IsValidIpponEntry for why the lookahead cannot misfire.
		if r == 'H' && i+1 < len(s) && s[i+1] == 't' {
			ippons = append(ippons, HanteiMark)
			skipNext = true
			continue
		}
		ippons = append(ippons, string(r))
	}
	return ippons, hansoku
}

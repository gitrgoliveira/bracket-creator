package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatScore and ParseScore are the score-cell CODEC: an inverse pair turning
// a side's ippons plus outstanding hansoku into the single string a bracket
// match persists, and back.
//
// A pool match stores IpponsA/IpponsB + HansokuA/HansokuB as fields; a bracket
// match stores one formatted ScoreA/ScoreB string per side. Anything moving a
// result between those two shapes has to cross this boundary.
//
// They live in domain because both layers need them and neither may import the
// other: the engine formats (writing a bracket match, and projecting one back
// for the rollback snapshot) and the HTTP layer parses (filling the ippon
// fields of a running bracket match for the display surfaces). The two used to
// sit in separate packages — formatScore in engine, a hand-written parseScore
// in mobileapp under a comment naming its inverse — so nothing made them
// round-trip, and TestScoreCodecRoundTrip is that guarantee made executable.
//
// Format: ippon letters joined with no separator, then the outstanding-hansoku
// count in parentheses. "MK (H1)", "MK", "(H1)", "".

// FormatScore renders a side's ippons and outstanding hansoku as the stored
// score string. Ippon marks are the waza letters (M/K/D/T/H, plus S for
// naginata) or the ○ default-win maru; each is a single rune, which is what
// makes ParseScore able to recover the slice.
func FormatScore(ippons []string, hansoku int) string {
	score := strings.Join(ippons, "")
	if hansoku > 0 {
		if score != "" {
			score += " "
		}
		score += fmt.Sprintf("(H%d)", hansoku)
	}
	return score
}

// ParseScore is the inverse of FormatScore: "MK (H1)" → (["M","K"], 1),
// "MK" → (["M","K"], 0), "(H1)" → (nil, 1), "" → (nil, 0).
//
// The round trip is exact for any slice FormatScore can render distinctly,
// i.e. one whose entries are single runes. Two deliberate normalisations:
// an empty-string entry is dropped (it renders as nothing, and is not a
// scoring ippon — see CountScoringIppons), and an absent hansoku count comes
// back as 0 rather than a negative. A malformed count parses as 0 rather than
// failing: this reads persisted data on a live-tournament path, where dropping
// an unreadable hansoku is better than refusing to show the match at all.
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
	var ippons []string
	for _, r := range s {
		if r == ' ' {
			continue
		}
		ippons = append(ippons, string(r))
	}
	return ippons, hansoku
}

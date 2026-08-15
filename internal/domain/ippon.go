package domain

// IpponPlaceholder is the marker the web editors put in an unfilled ippon slot.
// It is not a struck point: every "how many ippons were scored" count drops it,
// as it drops an empty cell.
const IpponPlaceholder = "•"

// HanteiMark is the judges'-decision mark that rides in the WINNER's score
// cell. It is not a waza letter and never a scored point (CountScoringIppons
// drops it, as it drops the placeholder): it records that the referees settled
// the match, not that anyone struck.
//
// It occupies a point SLOT, though, which is what lets a pool match persist a
// hantei without a new column — see encodeHanteiIntoIppons in state/pools.go.
// A hantei is only taken from a tied scoreline, and sanbon-shobu ends at 2, so
// the winner always has a free slot for it (the same reasoning resultSlot uses
// in web-mobile/js/result_slot.jsx).
const HanteiMark = "Ht"

// CountScoringIppons counts the real ippon marks in an ippons slice, ignoring
// empty entries and IpponPlaceholder. The default-win maru (written by the
// RecordDecision twins via DefaultWinIppons) counts like any struck ippon.
//
// It lives in domain because three layers need it and none of them may import
// another: the engine (standings, tie-breaks, preserveSubHantei), the store
// (TeamResultFrom's IV/PW) and the HTTP validator. The engine and store each
// held a copy under a "keep the two in sync" comment; this is that sync done by
// construction. Mirrors realIppons in web-mobile/js/result_slot.jsx.
func CountScoringIppons(ippons []string) int {
	n := 0
	for _, v := range ippons {
		if v != "" && v != IpponPlaceholder && v != HanteiMark {
			n++
		}
	}
	return n
}

// HanteiTiedScoreline reports whether two ippon arrays hold an equal number of
// scoring ippons, which is the precondition a hantei verdict rests on
// (FIK 7-5 / 29-6). Encho is NOT a precondition; a tied scoreline is.
//
// One owner for the same reason as IsHanteiCompatibleDecision, the sibling half
// of this same gate: the HTTP validator (validateSubBout) refuses an untied row
// on the way in, and the engine (preserveSubHantei) re-applies the test on the
// way out, because it mutates a row AFTER validation and its output is never
// re-checked. The two used to spell the count differently - raw len() at the
// validator, placeholder-dropping at the engine - so ["M","•"] against ["M","K"]
// read tied to one and untied to the other, and the engine could silently
// decline to preserve a verdict the validator had just accepted.
func HanteiTiedScoreline(a, b []string) bool {
	return CountScoringIppons(a) == CountScoringIppons(b)
}

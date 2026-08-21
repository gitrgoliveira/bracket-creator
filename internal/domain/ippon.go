package domain

// IpponPlaceholder is the marker the web editors put in an unfilled ippon slot.
// It is not a struck point: every "how many ippons were scored" count drops it,
// as it drops an empty cell.
const IpponPlaceholder = "•"

// HanteiMark is the judges'-decision mark, recorded as an ENTRY in the
// WINNER's ippon slice — the mark IS the record (operator ruling 2026-08-21:
// "Ht should be recorded as just another ippon"), exactly as the FIK score
// sheet writes it in the winner's column. It is not a waza letter and never a
// scored point (CountScoringIppons drops it, as it drops the placeholder): it
// records that the referees settled the match, not that anyone struck.
//
// It occupies a point SLOT: a hantei is only taken from a tied scoreline, and
// sanbon-shobu ends at 2, so the winner always has a free slot for it (the
// same reasoning resultSlot uses in web-mobile/js/result_slot.jsx). The
// legacy decidedByHantei fields (MatchResult, SubMatchResult, BracketMatch)
// are READ-ONLY compatibility channels: loaders and the request decoder
// normalise a flagged verdict into the winner's ippons; writers never set
// them.
const HanteiMark = "Ht"

// ContainsHantei reports whether an ippon slice carries the judges'-decision
// mark. Validation guarantees at most one mark per match, on the winner's
// side only, so "either side contains it" is the match-level verdict test.
func ContainsHantei(ippons []string) bool {
	for _, v := range ippons {
		if v == HanteiMark {
			return true
		}
	}
	return false
}

// StripHantei returns ippons without the judges'-decision mark, the original
// slice when no mark is present. Used where a verdict stops holding (a kiken
// recorded over a stored hantei, a legacy explicit-false normalisation): the
// points stay, the verdict goes.
func StripHantei(ippons []string) []string {
	if !ContainsHantei(ippons) {
		return ippons
	}
	out := make([]string, 0, len(ippons)-1)
	for _, v := range ippons {
		if v != HanteiMark {
			out = append(out, v)
		}
	}
	return out
}

// AppendHantei returns ippons with the judges'-decision mark appended once:
// filling the winner's next free slot is exactly "append", because slots
// render in array order. A slice already carrying the mark is returned
// unchanged, so normalising a legacy flag over an already-marked slice
// cannot double it.
func AppendHantei(ippons []string) []string {
	if ContainsHantei(ippons) {
		return ippons
	}
	// Fill an empty placeholder slot before growing the slice: the editors
	// persist "•" for an unfilled slot, and the mark takes a free slot.
	for i, v := range ippons {
		if v == "" || v == IpponPlaceholder {
			out := append([]string{}, ippons...)
			out[i] = HanteiMark
			return out
		}
	}
	return append(append([]string{}, ippons...), HanteiMark)
}

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
		if IsScoringIppon(v) {
			n++
		}
	}
	return n
}

// IsScoringIppon reports whether one entry of an ippon slice is a struck point.
// An empty cell, the unfilled-slot placeholder and the judges'-decision mark
// are all NOT points — the last because a hantei records who the referees chose,
// not that anyone scored.
//
// One predicate so a counter and a renderer cannot disagree about the same
// slice: CountScoringIppons counts through it, and export.IpponsScore draws
// through it. They previously differed on HanteiMark, so a mark that survived
// into an exported cell would have been printed as a struck point AND marked
// again by SideMarks. Mirrors realIppons in web-mobile/js/result_slot.jsx.
func IsScoringIppon(v string) bool {
	return v != "" && v != IpponPlaceholder && v != HanteiMark
}

// HanteiTiedScoreline reports whether two ippon arrays hold an equal number of
// scoring ippons, which is the precondition a hantei verdict rests on
// (FIK 7-5 / 29-6). Encho is NOT a precondition; a tied scoreline is.
//
// One owner for the same reason as IsSubBoutHanteiCompatibleDecision, the
// sibling half of this same gate: the HTTP validator (validateSubBout) refuses
// an untied row on the way in, and the engine (preserveSubHantei) re-applies
// the test on the way out, because it mutates a row AFTER validation and its
// output is never re-checked. The two used to spell the count differently -
// raw len() at the validator, placeholder-dropping at the engine - so
// ["M","•"] against ["M","K"] read tied to one and untied to the other, and
// the engine could silently decline to preserve a verdict the validator had
// just accepted.
func HanteiTiedScoreline(a, b []string) bool {
	return CountScoringIppons(a) == CountScoringIppons(b)
}

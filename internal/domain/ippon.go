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

// MaxIpponsPerSide is the kendo best-of-3 (sanbon-shobu) structural cap: each
// fighter can score at most 2 ippons, because the 2nd wins the match.
//
// One owner, in the leaf package both enforcers already import, because the
// cap is checked at two layers that must agree: mobileapp.validateIppons
// judges the payload as the client sent it, and the engine re-checks a row
// AFTER applyHansokuIppons has folded in an auto-awarded ippon (which the
// wire validator could not have seen). Two spellings of one rule let the
// engine accept a row the wire validator then 400s on every later save,
// wedging the editor on a scoreline nothing rejected at write time.
const MaxIpponsPerSide = 2

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
	return AppendIppon(ippons, HanteiMark)
}

// AppendIppon places one new entry in an ippon slice following the slot rule:
// the entry takes the first free slot (an empty cell or the "•" unfilled-slot
// placeholder) before growing the slice, exactly as the editors persist a
// struck point. AppendHantei rides it for the judges'-decision mark, and the
// engine's hansoku fold (applyHansokuIppons) rides it for the derived "H"
// ippon, so a legal two-slot row with a free slot stays a legal two-slot row
// after either award. Always returns a copy; the input is never mutated.
func AppendIppon(ippons []string, entry string) []string {
	for i, v := range ippons {
		if v == "" || v == IpponPlaceholder {
			out := append([]string{}, ippons...)
			out[i] = entry
			return out
		}
	}
	return append(append([]string{}, ippons...), entry)
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

// MatchSide is the three-value result of attributing a match's winner to a
// named side: MatchSideA, MatchSideB, or MatchSideNone when the winner
// cannot be attributed to either (empty winner, or a name/id that matches
// neither side). See AttributeWinnerSide, the one function that produces it.
type MatchSide string

const (
	MatchSideNone MatchSide = ""
	MatchSideA    MatchSide = "A"
	MatchSideB    MatchSide = "B"
)

// AttributeWinnerSide is the ONE owner of "which side won", used everywhere a
// hantei mark (or any other winner-attributed result) is placed, validated,
// or exported at MATCH level. It exists because the mark's side used to be
// decided by NAME alone, and a name is not unique within a competition: two
// participants from different dojos may share a name
// (CheckDuplicateEntriesByNameDojo only rejects same-name AND same-dojo), so
// a same-name pair let the mark land on the wrong side whenever the actual
// WinnerID named side B but names alone picked side A.
//
// Rule:
//
//   - If winnerID, sideAID and sideBID are ALL non-empty, attribute by id:
//     winnerID==sideAID -> MatchSideA; winnerID==sideBID -> MatchSideB;
//     matches neither -> MatchSideNone. Ids WIN over names when they
//     disagree; that is the point of this function.
//   - Otherwise (any of the three ids empty: legacy data, id-less payloads,
//     bracket rows, or a sub-bout, which carries no ids at all), fall back
//     to the name comparison: winner==sideA -> MatchSideA; else
//     winner==sideB -> MatchSideB; matches neither -> MatchSideNone. sideA
//     is checked first, so a winner name that matches BOTH sides (invalid
//     data - see the same convention documented for team aggregation)
//     resolves to MatchSideA, exactly as the equivalent name-only checks
//     elsewhere in this codebase (e.g. the switch order in
//     export.SideMarksLR) always have. This keeps id-less data byte-for-byte
//     identical to pre-id-threading behaviour.
//   - An empty winner is always MatchSideNone: it must never string-match an
//     empty sideA/sideB (e.g. an unset field), so this is checked before any
//     comparison.
//
// Mirrored in JS as attributeWinnerSide in web-mobile/js/result_slot.jsx (the
// declared owner of the Ht rules, which names this function as its twin);
// keep both in sync. Not bracket.jsx — that file holds only the name-based
// display helpers (winnerSideLR, subWinnerSides).
// WinnerAttribution carries everything AttributeWinnerSide needs to name a
// side: the participant ids when the record has them, and the names it always
// has. It is a struct rather than six positional strings because all six are
// the same type and mutually assignable, so a transposed pair compiles clean
// and silently marks the wrong competitor. That hazard was not theoretical -
// two functions implementing this one rule had already drifted into two
// different string orders (winner fourth in one, winner last in the other).
//
// The zero value means "this record has no ids", which SubMatchResult and
// BracketMatch both are (they persist names only); a partially-filled id set
// takes the name path too, per the rule below.
//
// Mirrored in JS as attributeWinnerSide's options object in
// web-mobile/js/result_slot.jsx, which took an object from the start.
type WinnerAttribution struct {
	WinnerID, SideAID, SideBID string
	Winner, SideA, SideB       string
}

func AttributeWinnerSide(a WinnerAttribution) MatchSide {
	if a.WinnerID != "" && a.SideAID != "" && a.SideBID != "" {
		switch a.WinnerID {
		case a.SideAID:
			return MatchSideA
		case a.SideBID:
			return MatchSideB
		default:
			return MatchSideNone
		}
	}
	if a.Winner == "" {
		return MatchSideNone
	}
	switch a.Winner {
	case a.SideA:
		return MatchSideA
	case a.SideB:
		return MatchSideB
	default:
		return MatchSideNone
	}
}

// BothSideIDsKnown reports whether a match record has BOTH participant ids
// stamped. This is the ONE gate for whether a stored/incoming WinnerID can be
// authoritatively checked against SideAID/SideBID at all: with only one
// side's id known, a WinnerID that matches neither known field is not
// necessarily a contradiction — it may simply be the OTHER side's (still
// absent) id, e.g. a client inventing an id from a name for an id-less side.
// Only when BOTH ids are known does "matches neither" prove the WinnerID
// names nobody on this row.
//
// Mirrors the gate domain.AttributeWinnerSide's id branch uses (all three of
// WinnerID/SideAID/SideBID non-empty): the two disagreed once (one used OR,
// the other AND), which is what let a partially-stamped pool row reject a
// legitimate score (PR #416 finding 6). Every WinnerID-vs-side-id consistency
// check in the codebase should call this rather than re-deriving its own
// non-empty test, so the two can never drift again.
func BothSideIDsKnown(sideAID, sideBID string) bool {
	return sideAID != "" && sideBID != ""
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

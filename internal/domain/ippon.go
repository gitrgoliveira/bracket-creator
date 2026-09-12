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
//     an unrepaired bracket row, or a sub-bout, which carries no ids at all
//     by design), fall back to the name comparison: winner==sideA -> MatchSideA; else
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
// The zero value means "no ids to attribute by here": unconditionally true
// for SubMatchResult (lineup names only, no id fields exist at all), and
// for BracketMatch only on a bye, an unresolved "Winner of ..." feeder, or
// an unrepaired legacy row (bc-brid: BracketMatch now carries
// SideAID/SideBID/WinnerID and this struct carries them whenever the side
// resolves); a partially-filled id set takes the name path too, per the
// rule below.
//
// Mirrored in JS as attributeWinnerSide's options object in
// web-mobile/js/result_slot.jsx, which took an object from the start.
type WinnerAttribution struct {
	WinnerID, SideAID, SideBID string
	Winner, SideA, SideB       string
}

// SubBoutAttribution builds the WinnerAttribution for one bout of a TEAM
// encounter, and owns the single rule that makes a sub-bout different from
// the match above it: its two sides are FIGHTERS, and two fighters on
// opposing teams may legally share a display name, where two teams may not.
//
// AttributeWinnerSide's name branch resolves a winner matching both sides to
// side A. At match level that is a deliberate convention for data that
// should not exist, keeping every surface on the same arbitrary answer. At
// sub-bout level the same order is a coin flip on ordinary valid data, and
// it decided individual victories, which decide the encounter. So when the
// two fighters share a name this drops the names from the attribution: the
// ids are then the only thing left that can answer, and where they cannot,
// AttributeWinnerSide reports no side at all rather than guessing.
//
// Every consumer of "which side won this bout" must build its attribution
// here: state.SubBoutWinnerSide for individual victories, export's bout rows
// for the mark beside a fighter's name. The JS mirror is subWinnerSides
// (match_scoreboard.jsx).
// Takes the struct rather than six strings for the reason WinnerAttribution
// itself gives: all six are the same type and mutually assignable, so a
// transposed pair would compile clean and silently mark the wrong competitor.
func SubBoutAttribution(att WinnerAttribution) WinnerAttribution {
	if att.SideA != "" && att.SideA == att.SideB {
		att.SideA, att.SideB = "", ""
	}
	return att
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

// WinnerIDNamesASide reports whether winnerID is either empty (nothing to
// check) or names one of the two given side ids (bc-pnum ruling 1d). This is
// the strict primitive every WinnerID-vs-side-id consistency check in the
// codebase is built from: it applies NO exemption for the sides being
// unknown, so a non-empty WinnerID naming neither side is rejected by this
// function specifically, unconditionally, not inferred around.
//
// Call this directly when that unconditional strictness is exactly what's
// wanted: validateWithdrawalNamesDisambiguated
// (internal/mobileapp/validation.go) does, on purpose, because a same-name
// withdrawal with both side ids empty must keep rejecting an unattributable
// winnerId -- folding an exemption in here would silently reopen that
// ambiguity for it too. A caller that instead wants the narrow
// both-sides-unknown exemption (a legacy pool row drawn before ids were
// minted, or a bracket match, where there is no known pairing for winnerId
// to have missed) calls the wrapper WinnerIDAcceptable, rather than
// re-deriving its own `sideAID == "" && sideBID == ""` comparison -- see that
// function's doc comment for the exemption's own rationale and scope.
//
// Ids are minted for every roster row at write time (participants.csv's one
// write chokepoint, internal/state/participants.go marshalParticipantsCSV)
// and the draw refuses to run over a roster that still has an id-less row
// (helper.ValidateNoMissingParticipantIDs, bc-pnum ruling 1c), so a drawn
// match's side ids are never partially known in current data. That closes
// the case this replaces (BothSideIDsKnown, PR #416 finding 6) tolerated: a
// WinnerID matching neither side used to be silently DROPPED rather than
// rejected whenever only one side id was known, on the theory that it might
// simply be the other side's still-absent id. With ids guaranteed complete
// by the time a match exists, that theory no longer holds.
func WinnerIDNamesASide(winnerID, sideAID, sideBID string) bool {
	return winnerID == "" || winnerID == sideAID || winnerID == sideBID
}

// WinnerIDAcceptable is WinnerIDNamesASide widened by ONE narrow exemption:
// when BOTH side ids are unknown (sideAID == "" && sideBID == ""), there is
// no known pairing for winnerId to have missed, so the write is accepted
// rather than rejected against data this check cannot evaluate. This is the
// ONE owner of that exemption -- internal/engine/scoring.go's
// backfillMatchIdentity and internal/mobileapp/validation.go's
// validateWinnerIDMatchesSide each used to hand-derive their own
// `bothSideIDsUnknown` local and OR it with WinnerIDNamesASide; call this
// instead of re-deriving that comparison a third time.
//
// Deliberately NOT folded into WinnerIDNamesASide itself: see that function's
// doc comment for why validateWithdrawalNamesDisambiguated needs the strict,
// unconditional form directly and would be silently weakened by this
// exemption.
//
// The exemption covers two bounded shapes of data (bc-brid narrowed this
// from "permanent" to "residual"): a legacy pool row drawn before ids were
// minted (ids were backfilled going forward only, never onto existing rows,
// state.upgradePoolMatchSideIDsLocked's own residue), and an UNREPAIRED
// bracket row -- a bye, an unresolved "Winner of ..." feeder, or a legacy
// bracket.json a load-time repair could not resolve (ambiguous name; see
// state.Bracket.StampRoundZeroSideIDsFromDrawOrder and
// state.upgradeBracketSideIDsLocked). A STAMPED bracket row is no different
// from a stamped pool row here: state.Store.MatchSidesByID returns its real
// SideAID/SideBID (that function's own doc comment), applyBracketMatchResult
// (internal/engine/scoring.go) resolves and persists bm.WinnerID from them
// via resolveWinnerIDFromSides, and an unattributable winnerId on a bracket
// write with both sides known is rejected exactly like the pool branch's,
// not silently discarded.
func WinnerIDAcceptable(winnerID, sideAID, sideBID string) bool {
	return (sideAID == "" && sideBID == "") || WinnerIDNamesASide(winnerID, sideAID, sideBID)
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

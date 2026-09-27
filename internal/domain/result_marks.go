package domain

// result_marks.go is the ONE domain-layer owner of the two closed-set display
// rules every results workbook sheet applies: the centre "vs" cell (see
// MiddleMark) and the per-side result mark beside a competitor's score (see
// SideMarks/SideMarksAB). It lives in domain, not internal/export, because
// TWO sheet families need it and neither may import the other: the main Pool
// Matches / Elimination Matches sheets (internal/export, which already
// imports domain) and the Kachinuki Detail sheet (built by
// internal/engine/kachinuki_export.go, which imports domain but not export --
// export imports engine, so the reverse would cycle). export.MiddleMark,
// export.enchoLabel, export.SideMarks and export.SideMarksLR are one-line
// delegates to the functions here, kept so their existing exported
// signatures (some taking *state.EnchoMetadata, which domain may not import)
// and their pinning tests (middle_closed_set_test.go, suffix_test.go,
// testdata/encho_labels.json) stay exactly where they are.

// MiddleMark returns the ONE mark the centre "vs" cell may carry for a
// completed match. The middle column of a score sheet can only ever read:
//
//	vs     not yet decided (the caller's own template text; "" means "leave it")
//	X      a tie (hikiwake)
//	(E)    the match went to overtime
//	(DH)   a team encounter sent to a representative bout
//
// The marks are mutually exclusive by rule, not by accident: X means a tie
// and a match that went to encho cannot end tied (encho runs until someone
// scores), so X beats (E) if stale data carries both; and a daihyosen bout is
// one-point sudden death, so DH bouts do not have encho and (DH) beats (E).
//
// Everything else — Kiken, Fus., Ht — is a RESULT, not a middle mark, and
// belongs beside the competitor it names: see SideMarks.
//
// enchoOn is the caller's own "did this bout go to overtime" predicate
// (state.EnchoMetadata.On() on every current caller); domain does not import
// state, so it cannot take the metadata struct itself. Mirrors
// middleMark()/formatIpponsScore in web-mobile/js/bracket.jsx.
func MiddleMark(decision string, enchoOn bool) string {
	switch {
	case decision == string(DecisionHikiwake):
		return "X"
	case decision == string(DecisionDaihyosen):
		return "(DH)"
	default:
		return EnchoLabel(enchoOn)
	}
}

// EnchoLabel renders the overtime marker: "" when no overtime ran, "(E)"
// otherwise — always bare, never a count.
//
// mp-m4bn: encho is just encho. The stepper records how many periods were
// fought (PeriodCount persists for the tournament log), but the result
// marking deliberately never carries the number: operator feedback is that
// counted markers ("(E×3)") confuse readers of brackets and result sheets.
// Do not reintroduce the count here. Mirrors enchoLabel() in
// web-mobile/js/bracket.jsx, pinned by the shared table in
// internal/export/testdata/encho_labels.json (which includes multi-digit
// counts precisely to pin that digits never leak into the marker).
func EnchoLabel(on bool) string {
	if !on {
		return ""
	}
	return "(E)"
}

// SideMarks returns the per-side result marks for a decision: winnerMark goes
// in the winning side's score cell, loserMark in the losing side's.
//
//	hantei    -> winner "Ht"   (FIK 7-5 / 29-6: judges picked the winner)
//	kiken     -> loser  "Kiken" (the mark names the competitor who withdrew)
//	fusenpai  -> loser  "Fus."  (the mark names the no-show)
//	fusensho  -> winner "Fus."  (the default WIN names the present side)
//
// The JS viewer surfaces fusensho via a separate bout badge, so its
// sideMarks() omits it; a flat spreadsheet cell has no badge, so every Go
// caller keeps the "Fus." mark (deliberate divergence, mirrored in the JS
// docstring).
func SideMarks(decision string, decidedByHantei bool) (winnerMark, loserMark string) {
	switch {
	case IsKikenDecisionStr(decision):
		loserMark = "Kiken"
	case decision == string(DecisionFusenpai):
		loserMark = "Fus."
	case decision == string(DecisionFusensho):
		winnerMark = "Fus."
	}
	if decidedByHantei {
		winnerMark = joinResultMarks(winnerMark, "Ht")
	}
	return winnerMark, loserMark
}

// SideMarksAB resolves SideMarks into (markA, markB) SIDE order -- SideA's
// mark first, SideB's second, NOT the sheet's White-left/Red-right column
// order. Placing the pair into sheet columns is an Excel-layout concern
// (helper.WhiteLeft), applied by each caller AFTER this function: see
// export.SideMarksLR (the main sheets) and the Kachinuki Detail bout writer
// (internal/helper/excel_kachinuki.go), which both call this and then
// White-left the result.
//
// att carries the ids and names of the record being marked: a pool or
// bracket row's SideAID/SideBID/WinnerID, or a sub-bout's member ids
// (state.SubMatchResult.Attribution, or domain.SubBoutAttribution's
// same-name-blanked variant for a bout whose two fighters may legally share
// a display name). Side attribution goes through AttributeWinnerSide, the
// one owner of "which side won": ids win over names when a same-name pair
// would otherwise pick the wrong side. A missing or unmatchable winner (a
// draw, an unfinished match, or drifted data) yields no marks: result marks
// hang off a winner by definition.
func SideMarksAB(decision string, decidedByHantei bool, att WinnerAttribution) (markA, markB string) {
	winnerMark, loserMark := SideMarks(decision, decidedByHantei)
	if att.Winner == "" {
		return "", "" // an empty winner must not string-match an empty side
	}
	switch AttributeWinnerSide(att) {
	case MatchSideA:
		return winnerMark, loserMark
	case MatchSideB:
		return loserMark, winnerMark
	default:
		return "", ""
	}
}

// joinResultMarks joins two result-mark fragments with a single space,
// skipping empties, so a composed mark never carries a leading, trailing, or
// doubled space. A private twin of export.joinSp (which composes whole CELL
// strings -- score + mark + foul triangle -- for that package's own callers,
// not just two marks), kept separate rather than shared so domain gains no
// dependency on export's wider cell-composition helper.
func joinResultMarks(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

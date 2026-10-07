package domain

// result_marks.go is the ONE domain-layer owner of the closed-set display
// rules every results workbook sheet applies: the centre "vs" cell (see
// MiddleMark), the per-side result mark beside a competitor's score (see
// SideMarks/SideMarksAB), and the default-win maru fallback for a winner
// whose recorded score cells are empty (see DefaultWinMaruAB). It lives in
// domain, not internal/export, because TWO sheet families need it and
// neither may import the other: the main Pool
// Matches / Elimination Matches sheets (internal/export, which already
// imports domain) and the Kachinuki Detail sheet (built by
// internal/engine/kachinuki_export.go, which imports domain but not export --
// export imports engine, so the reverse would cycle). internal/export keeps
// thin adapters over these.

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
	case IsDrawDecisionStr(decision):
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
// Mirrors sideMarks() in web-mobile/js/bracket.jsx exactly, fusensho
// included, so a fusensho match reads the same in the workbook and the app.
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
		winnerMark = JoinNonEmpty(winnerMark, HanteiMark)
	}
	return winnerMark, loserMark
}

// SideMarksAB resolves SideMarks into (markA, markB) SIDE order -- SideA's
// mark first, SideB's second, NOT the sheet's White-left/Red-right column
// order. Placing the pair into sheet columns is an Excel-layout concern
// (helper.WhiteLeft), applied by each caller AFTER this function returns:
// export.SideMarksLR (the main sheets) White-lefts immediately, while
// internal/engine/kachinuki_export.go's buildKachinukiDetail calls this and
// stores the SIDE-ordered pair on the bout -- the White-left step for the
// Kachinuki Detail sheet runs later, in the helper writer
// (internal/helper/excel_kachinuki.go's writeKachinukiBoutRow).
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

// DefaultWinMaruAB fills the WINNER's empty score cell with the FIK
// default-win maru (DefaultWinIppons), given SIDE-ordered scores: scored data
// carries the maru itself, so this covers results recorded before that fill
// or imported without it. Never the loser's cell, and never engi flag counts
// (callers gate). att is resolved through AttributeWinnerSide, the owner
// SideMarksAB uses too, so a cell's maru and its Kiken/Fus. mark can never
// name different sides; enchoOn is the caller's own overtime predicate, as
// for MiddleMark. The main sheets (export.DefaultWinMaruAB) and the
// Kachinuki Detail sheet both delegate here.
func DefaultWinMaruAB(scoreA, scoreB, decision string, enchoOn bool, att WinnerAttribution) (string, string) {
	if att.Winner == "" || !IsDefaultWinDecisionStr(decision) {
		return scoreA, scoreB
	}
	maru := IpponsScore(DefaultWinIppons(enchoOn))
	switch AttributeWinnerSide(att) {
	case MatchSideA:
		if scoreA == "" {
			scoreA = maru
		}
	case MatchSideB:
		if scoreB == "" {
			scoreB = maru
		}
	}
	return scoreA, scoreB
}

// JoinNonEmpty joins two display fragments with a single space, skipping
// empties, so a composed mark or cell never carries a leading, trailing, or
// doubled space. The ONE shared join helper for this job: internal/helper
// (excel_kachinuki.go) and internal/export (builder.go) both call it.
func JoinNonEmpty(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

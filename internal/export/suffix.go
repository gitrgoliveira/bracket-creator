// Package export builds results-populated XLSX workbooks from live mobile-app
// tournament state. It is a SEPARATE path from the blank-template export in
// internal/engine/export.go; the existing ExportCompetitionXlsx and
// GET /api/competitions/:id/export endpoint are not modified.
//
// The single public entry point is BuildResultsWorkbook. Follow-up agents
// (CLI command + HTTP handler) call it to get the xlsx bytes.
package export

import (
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MiddleMark returns the ONE mark the centre "vs" cell may carry for a
// completed match. The middle column of a score sheet can only ever read:
//
//	vs     not yet decided (the template's own text; we return "" and leave it)
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
// belongs beside the competitor it names: see SideMarks. Mirrors
// middleMark()/formatIpponsScore in web-mobile/js/bracket.jsx.
func MiddleMark(decision string, encho *state.EnchoMetadata) string {
	switch {
	case decision == state.DecisionDraw:
		return "X"
	case decision == string(domain.DecisionDaihyosen):
		return "(DH)"
	default:
		return enchoLabel(encho)
	}
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
// sideMarks() omits it; a flat spreadsheet cell has no badge, so this export
// keeps the "Fus." mark (deliberate divergence, mirrored in the JS docstring).
func SideMarks(decision string, decidedByHantei bool) (winnerMark, loserMark string) {
	switch {
	case domain.IsKikenDecisionStr(decision):
		loserMark = "Kiken"
	case decision == string(domain.DecisionFusenpai):
		loserMark = "Fus."
	case decision == string(domain.DecisionFusensho):
		winnerMark = "Fus."
	}
	if decidedByHantei {
		winnerMark = joinSp(winnerMark, "Ht")
	}
	return winnerMark, loserMark
}

// SideMarksLR resolves SideMarks into (left, right) on-sheet order for a
// match between sideA and sideB. Default layout is SideA (Aka) on the left;
// mirror swaps the sides physically, matching the leftIppons/rightIppons
// swaps at the call sites. A missing or unmatchable winner (a draw, an
// unfinished match, or drifted data) yields no marks: result marks hang off
// a winner by definition.
func SideMarksLR(decision string, decidedByHantei bool, winner, sideA, sideB string, mirror bool) (left, right string) {
	winnerMark, loserMark := SideMarks(decision, decidedByHantei)
	if winnerMark == "" && loserMark == "" || winner == "" {
		return "", ""
	}
	var aMark, bMark string
	switch winner {
	case sideA:
		aMark, bMark = winnerMark, loserMark
	case sideB:
		aMark, bMark = loserMark, winnerMark
	default:
		return "", ""
	}
	if mirror {
		return bMark, aMark
	}
	return aMark, bMark
}

// joinSp joins two display fragments with a single space, skipping empties, so
// a composed suffix never carries a leading, trailing, or doubled space. The JS
// mirror does the same job with [...].filter(Boolean).join(" ").
func joinSp(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

// enchoLabel renders the overtime marker for an encho block: "" when no
// overtime ran, "(E)" otherwise — always bare, never a count.
//
// mp-m4bn: encho is just encho. The stepper records how many periods were
// fought (PeriodCount persists for the tournament log), but the result
// marking deliberately never carries the number: operator feedback is that
// counted markers ("(E×3)") confuse readers of brackets and result sheets.
// Do not reintroduce the count here. Mirrors enchoLabel() in
// web-mobile/js/bracket.jsx, pinned by the shared table in
// testdata/encho_labels.json (which includes multi-digit counts precisely to
// pin that digits never leak into the marker). The editors' "· (E) Overtime
// ×N" eyebrow is different on purpose: a live readout of the stepper the
// operator is using, not a result marking.
func enchoLabel(encho *state.EnchoMetadata) string {
	if encho == nil || encho.PeriodCount <= 0 {
		return ""
	}
	return "(E)"
}

// FlagsScorePair returns the display strings for both sides of an engi bout.
//
// Pairwise rule: when EITHER side has a positive flag count, write BOTH counts
// numerically (clamping any negative to 0). When both counts are <=0, return
// ("", "") to leave both cells blank.
//
// Why pairwise? A flag-decided bout (e.g. 5-0) means the losing side genuinely
// scored zero flags - that "0" is a real score and must appear so the operator
// can tell "bout was fought and decided 5-0" from "bout was kiken/fusenpai with
// no flags recorded at all (0-0 but decided without scoring)". By contrast, a
// kiken/fusenpai decision with no flags on either side has nothing to display,
// so both cells stay blank.
func FlagsScorePair(a, b int) (string, string) {
	if a <= 0 && b <= 0 {
		return "", ""
	}
	return strconv.Itoa(max(0, a)), strconv.Itoa(max(0, b))
}

// mirroredFlagsScore is FlagsScorePair with the display-position swap applied:
// a/b are the stored SideA/SideB flag counts, and mirror flips them so the
// returned pair is (left, right) in on-sheet order. Shared by the pool and
// bracket overlays so the swap-then-format sequence lives in one place.
func mirroredFlagsScore(a, b int, mirror bool) (string, string) {
	if mirror {
		a, b = b, a
	}
	return FlagsScorePair(a, b)
}

// IpponsScore formats an ippon slice as a readable score string: ["M","K"] ->
// "MK", nil/empty -> "". Mirrors the character-join behaviour in
// formatIpponsScore (bracket.jsx) without the full display logic (bye/hikiwake
// special cases live in the caller).
func IpponsScore(ippons []string) string {
	result := ""
	for _, s := range ippons {
		if s != "" && s != "•" {
			result += s
		}
	}
	return result
}

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

// DecisionSuffix returns the display suffix for a match decision, encho, and
// hantei flag. It follows the canonical JS decisionSuffix() in
// web-mobile/js/bracket.jsx, including the "Ht" suffix mandated by the "Excel +
// viewer parity" comment there (FIK 7-5 / 29-6).
//
// Composition order:
//  1. Base decision label: kiken variants -> "Kiken"; fusenpai/fusensho -> "Fus."; daihyosen -> "DH".
//  2. If encho -> append " (E)" (always bare, regardless of period count).
//  3. If hanteiOn -> append " Ht".
//
// DELIBERATE DIVERGENCE from the JS: the JS omits fusensho (the per-bout default
// WIN) here because the viewer surfaces it via a separate bout badge. A flat
// spreadsheet cell has no such badge, so this export folds fusensho into the
// suffix ("Fus.") too, preserving the defaulted-bout signal in the archive
// rather than dropping it.
//
// A zero/nil Encho (or PeriodCount == 0) is treated as no encho.
// Returns "" when no suffix applies.
func DecisionSuffix(decision string, encho *state.EnchoMetadata, decidedByHantei bool) string {
	enchoSfx := enchoLabel(encho)

	var suffix string
	switch {
	case domain.IsKikenDecisionStr(decision):
		suffix = "Kiken"
	case decision == string(domain.DecisionFusenpai), decision == string(domain.DecisionFusensho):
		suffix = "Fus."
	case decision == string(domain.DecisionDaihyosen):
		suffix = "DH"
	}

	suffix = joinSp(suffix, enchoSfx)
	if decidedByHantei {
		suffix = joinSp(suffix, "Ht")
	}

	return suffix
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

// MiddleCellText composes the value for a match's centre "vs" cell from the
// hikiwake draw marker and the decision suffix. When a match is a draw AND also
// carries a suffix (a scoreless encho draw -> "X (E)", a hantei-decided draw ->
// "X Ht", a team encounter drawn into a daihyosen -> "X DH"), BOTH are kept so
// the exported workbook never loses the draw indicator. This mirrors
// formatIpponsScore in web-mobile/js/bracket.jsx, which renders "X" + suffix for
// a scoreless draw. Returns "" when neither applies, so the caller can leave the
// cell untouched rather than blanking a formula.
func MiddleCellText(decision, suffix string) string {
	marker := ""
	if decision == state.DecisionDraw {
		marker = "X"
	}
	return joinSp(marker, suffix)
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

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
//  2. If enchoOn -> append " (E)".
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
	enchoOn := encho != nil && encho.PeriodCount > 0

	var suffix string
	switch {
	case domain.IsKikenDecisionStr(decision):
		suffix = "Kiken"
	case decision == string(domain.DecisionFusenpai), decision == string(domain.DecisionFusensho):
		suffix = "Fus."
	case decision == string(domain.DecisionDaihyosen):
		suffix = "DH"
	}

	if enchoOn {
		if suffix != "" {
			suffix += " (E)"
		} else {
			suffix = "(E)"
		}
	}

	if decidedByHantei {
		if suffix != "" {
			suffix += " Ht"
		} else {
			suffix = "Ht"
		}
	}

	return suffix
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
	switch {
	case marker != "" && suffix != "":
		return marker + " " + suffix
	case marker != "":
		return marker
	default:
		return suffix
	}
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

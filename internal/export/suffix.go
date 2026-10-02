// Package export builds results-populated XLSX workbooks from live mobile-app
// tournament state. It is a SEPARATE path from the stored-draw export in
// internal/engine/export.go (Engine.ExportCompetitionXlsx, behind
// GET /api/competitions/:id/export and the PDF prints).
//
// The single public entry point is BuildResultsWorkbook. Follow-up agents
// (CLI command + HTTP handler) call it to get the xlsx bytes.
package export

import (
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MiddleMark returns the ONE mark the centre "vs" cell may carry for a
// completed match: "" (not yet decided, the template's own "vs" survives),
// "X" (hikiwake), "(E)" (overtime), or "(DH)" (a team encounter sent to a
// representative bout). A one-line delegate to domain.MiddleMark, the shared
// owner the Kachinuki Detail sheet also delegates to (internal/helper), kept
// here so this exported signature (state.EnchoMetadata, which domain may not
// import) and its pinning test (middle_closed_set_test.go) stay put. See
// domain.MiddleMark's doc comment for the full rule and domain.SideMarks for
// everything the middle may NOT carry.
func MiddleMark(decision string, encho *state.EnchoMetadata) string {
	return domain.MiddleMark(decision, encho.On())
}

// SideMarksLR resolves domain.SideMarks into (left, right) on-sheet order for a
// match between sideA and sideB: Shiro (SideB) on the left, Aka (SideA) on
// the right, through helper.WhiteLeft like the scores beside these marks.
// domain.SideMarksAB owns the decision+attribution part (SIDE order, shared
// with the Kachinuki Detail sheet); this function's own job is only the
// White-left placement domain may not perform (domain does not import
// helper).
//
// att carries the ids and names of the record being marked: a pool or
// bracket row's SideAID/SideBID/WinnerID, or a sub-bout's member ids
// (state.SubMatchResult.Attribution). Side attribution goes through
// domain.AttributeWinnerSide, the one owner of "which side won": ids win over
// names when a same-name pair (legal: two participants from different dojos
// may share a name) would otherwise pick the wrong side.
func SideMarksLR(decision string, decidedByHantei bool, att domain.WinnerAttribution) (left, right string) {
	aMark, bMark := domain.SideMarksAB(decision, decidedByHantei, att)
	return helper.WhiteLeft(aMark, bMark)
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

// DefaultWinMaruAB fills the WINNER's empty score cell with the default-win
// maru, given SIDE-ordered scores. A one-line delegate to
// domain.DefaultWinMaruAB, kept for this signature's state.EnchoMetadata,
// which domain may not import.
func DefaultWinMaruAB(scoreA, scoreB, decision string, encho *state.EnchoMetadata, att domain.WinnerAttribution) (string, string) {
	return domain.DefaultWinMaruAB(scoreA, scoreB, decision, encho.On(), att)
}

// HansokuMark renders a side's outstanding (undischarged) hansoku count as
// the FIK score-sheet triangle: one "▲" for an odd count (a single standing
// foul), nothing for an even one (the second foul deletes the triangle and
// becomes the opponent's "H" ippon, which already rides in the ippon slice).
// Mirrors boutHansokuMark in web-mobile/js/match_scoreboard.jsx; keep the two
// in sync. The stored ScoreA/ScoreB strings used to carry this as the codec's
// "(H1)" suffix — bc-bmsc removed the strings, so the export now draws the
// count as the rulebook writes it instead of echoing a wire format.
func HansokuMark(fouls int) string {
	if fouls%2 == 1 {
		return "▲"
	}
	return ""
}

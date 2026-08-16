package cmd

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	excelize "github.com/xuri/excelize/v2"
)

// blankWorkbookCourtPlan is the CLI's court plan: Draw and Courts only.
//
// The CLI generates a BLANK workbook with no stored bracket behind it, so the
// draw's own regions are the only assignment there is -- ByMatch and Bronze
// stay zero and every reader falls back to the region, which is exactly right
// here. The live app's counterpart is engine.LiveCourtPlan. One assembly per
// world, so neither the tree pages nor the elimination sheet can be handed a
// plan the other did not get.
func blankWorkbookCourtPlan(draw *helper.KnockoutDraw, courtNames []string) helper.CourtPlan {
	return helper.CourtPlan{Draw: draw, Courts: courtNames}
}

// printEliminationWithBronze renders the team elimination sheet and, for a
// naginata bracket with a real semifinal round, the bronze (3rd-place) block with
// its print area. Shared by create-pools and create-playoffs, which both run the
// bronze on the same court set with mirror=true.
func printEliminationWithBronze(f *excelize.File, matchWinners map[string]helper.MatchWinner, rounds [][]*helper.Node, teamMatches int, plan helper.CourtPlan, engi, naginata bool) {
	helper.PrintEliminationWithBronze(f, matchWinners, rounds, teamMatches, plan, true, engi, helper.NeedsBronzeBlock(naginata, len(rounds)))
}

// finishKnockoutPages runs the CLI epilogue shared by create-pools and
// create-playoffs after RenderKnockoutPages: log the page spread, delete the
// consumed tree template (deletion is caller-owned, see RenderTreePages), and
// log the per-round elimination match counts (round numbers count down toward
// the final, the last entry in rounds).
func finishKnockoutPages(f *excelize.File, numPages int, rounds [][]*helper.Node) {
	fmt.Printf("Spread across %d tree pages\n", numPages)
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		fmt.Println("Note: Tree sheet might not exist:", err)
	}
	for i, r := range rounds {
		fmt.Printf("Elimination matches for round %d: %d\n", len(rounds)-i, len(r))
	}
}

// openOutputFile opens (or creates) the file at outputPath for appending and
// returns the file and a buffered writer over it. The caller must defer
// both Close and Flush.
func openOutputFile(outputPath string) (*os.File, *bufio.Writer, error) {
	// O_TRUNC, not O_APPEND: each generator writes one complete workbook, and
	// appending to an existing .xlsx silently doubles the file on every re-run
	// with the same -o path (zip readers only see the trailing copy).
	f, err := os.OpenFile(outputPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0600) // #nosec G304, path is user-supplied CLI argument
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open output file: %w", err)
	}
	return f, bufio.NewWriter(f), nil
}

// processEntries validates the entry list (rejecting duplicates), optionally
// shuffles, and converts raw CSV entry strings into Player objects. Duplicate
// entries are returned as a hard error so the caller (CLI or web handler)
// can surface them to the user instead of silently dropping rows.
func processEntries(entries []string, determined bool, withZekkenName bool) ([]helper.Player, error) {
	if dups := helper.CheckDuplicateEntries(entries); len(dups) > 0 {
		return nil, fmt.Errorf("duplicate participant entries found: %v", dups)
	}
	// Drop empty strings (blank lines) without warning, duplicates have
	// already been rejected above.
	entries = helper.RemoveDuplicates(entries)
	if !determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}
	players, err := helper.CreatePlayers(entries, withZekkenName)
	if err != nil {
		return nil, err
	}
	return players, nil
}

//go:build inspect

// scratch/inspect_kachinuki.go is a manual verification script for T203
// (Phase 11, Kachinuki Excel Detail Sheet). It builds an *.xlsx file
// containing a synthesized kachinuki team match and the new Kachinuki
// Detail sheet, then prints the sheet's cell values for visual review.
//
// Run with:
//
//	go run -tags=inspect ./scratch/inspect_kachinuki.go
//
// The build tag keeps this file out of the default build so `go build`
// / `make go/build` are unaffected. The script is read-only beyond the
// output xlsx; it doesn't write to tournament-data/ or touch the store.

package main

import (
	"fmt"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	excelize "github.com/xuri/excelize/v2"
)

func main() {
	matches := []helper.KachinukiMatchDetail{
		{
			Label:        "Pool A, Round 1",
			SideATeam:    "Team Tiger",
			SideBTeam:    "Team Dragon",
			Winner:       "Team Tiger",
			Decision:     "kachinuki-exhaustion",
			EliminationA: 2,
			EliminationB: 5,
			Bouts: []helper.KachinukiBout{
				{Position: 1, SideAName: "Akira", SideAPos: "Senpo", ScoreA: "M", SideBName: "Bao", SideBPos: "Senpo", Winner: "Akira", Decision: "fought"},
				{Position: 2, SideAName: "Akira", SideAPos: "Senpo", ScoreA: "MK", SideBName: "Chen", SideBPos: "Jiho", Winner: "Akira", Decision: "fought"},
				{Position: 3, SideAName: "Akira", SideAPos: "Senpo", ScoreB: "M", SideBName: "Dao", SideBPos: "Chuken", Winner: "Dao", Decision: "fought"},
				{Position: 4, SideAName: "Emi", SideAPos: "Jiho", ScoreA: "M", SideBName: "Dao", SideBPos: "Chuken", Winner: "Emi", Decision: "fought"},
				{Position: 5, SideAName: "Emi", SideAPos: "Jiho", SideBName: "Feng", SideBPos: "Fukusho", Decision: "hikiwake"},
				{Position: 6, SideAName: "Goro", SideAPos: "Chuken", ScoreA: "MM", SideBName: "Hua", SideBPos: "Taisho", Winner: "Goro", Decision: "fought"},
				{Position: 7, SideAName: "Goro", SideAPos: "Chuken", ScoreA: "K", SideBName: "Ichi", SideBPos: "Fukusho-alt", Winner: "Goro", Decision: "fought"},
			},
		},
	}

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()

	if err := helper.WriteKachinukiDetailSheet(f, matches); err != nil {
		log.Fatalf("WriteKachinukiDetailSheet: %v", err)
	}

	out := "scratch-kachinuki-detail.xlsx"
	if err := f.SaveAs(out); err != nil {
		log.Fatalf("SaveAs: %v", err)
	}
	fmt.Printf("Wrote %s, sheets: %v\n", out, f.GetSheetList())

	// Dump the detail sheet's cells row-by-row for visual review.
	rows, err := f.GetRows(helper.SheetKachinukiDetail)
	if err != nil {
		log.Fatalf("GetRows: %v", err)
	}
	fmt.Printf("\n=== %s ===\n", helper.SheetKachinukiDetail)
	for i, row := range rows {
		fmt.Printf("row %2d: %q\n", i+1, row)
	}
}

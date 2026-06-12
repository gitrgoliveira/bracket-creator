//go:build inspect

// scratch/inspect_tags_qr.go generates a sample Tags sheet with QR codes for
// visual review of the mp-yin4 QR embedding feature.
//
// Run with:
//
//	go run -tags=inspect ./scratch/inspect_tags_qr.go
//
// Writes tags_qr_preview.xlsx in the current directory.

package main

import (
	"fmt"
	"log"
	"os"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

func main() {
	pools := []helper.Pool{
		{
			PoolName: "Pool A",
			Players: []helper.Player{
				{Name: "Alice Tanaka", Dojo: "Shinbu", PoolPosition: 1, Number: "K1"},
				{Name: "Bob Yamada", Dojo: "Mushin", PoolPosition: 2, Number: "K2"},
				{Name: "Carol Suzuki", Dojo: "Fudoshin", PoolPosition: 3, Number: "K3"},
			},
		},
		{
			PoolName: "Pool B",
			Players: []helper.Player{
				{Name: "Dave Ito", Dojo: "Shinbu", PoolPosition: 1, Number: "K4"},
				{Name: "Eve Nakamura", Dojo: "Mushin", PoolPosition: 2, Number: "K5"},
			},
		},
	}

	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	const publicURL = "https://kendo.example.com"
	if err := helper.CreateTagsSheet(f, pools, publicURL); err != nil {
		log.Fatalf("CreateTagsSheet: %v", err)
	}

	out := "tags_qr_preview.xlsx"
	if err := f.SaveAs(out); err != nil {
		log.Fatalf("SaveAs: %v", err)
	}

	abs, _ := os.Getwd()
	fmt.Printf("Wrote %s/%s\n", abs, out)
	fmt.Printf("Public URL: %s\n", publicURL)
	fmt.Printf("Players: %d (QR per player × 2 tags = %d QR codes embedded)\n",
		func() int {
			n := 0
			for _, p := range pools {
				n += len(p.Players)
			}
			return n
		}(),
		func() int {
			n := 0
			for _, p := range pools {
				n += len(p.Players) * 2
			}
			return n
		}(),
	)
}

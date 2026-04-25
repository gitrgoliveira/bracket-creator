//go:build ignore

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	fmt.Println("=== Page Break Checks ===")
	for _, f := range []string{
		"pools-example-large-teams.xlsx",
		"pools-example-large-teams-max-size.xlsx",
		"pools-example-large-max-size.xlsx",
	} {
		fmt.Printf("\n--- %s ---\n", f)
		checkBreaks(f)
	}

	fmt.Println("\n=== Seed Distribution Checks (pools) ===")
	// Pool files: look up full player names in the 'data' sheet (sheet1) which stores
	// the pool → player mapping as shared strings. Column A = pool label, Column B = full name.
	checkPoolSeeds("pools-example-medium-seeded.xlsx",
		[]string{"Cersei Lannister", "Daenerys Targaryen", "Eddard Stark", "Frodo Baggins"})
	checkPoolSeeds("pools-example-large-seeded.xlsx",
		[]string{"Kevin Clark", "Luke Rodriguez", "Michael Lewis", "Nathan Lee"})

	fmt.Println("\n=== Seed Distribution Checks (playoffs) ===")
	// Playoff files: player names are written as literals in Tree sheets; zekken display names used.
	checkPlayoffSeeds("playoffs-example-medium-seeded.xlsx",
		[]string{"LANNISTER", "TARGARYEN", "STARK", "BAGGINS"})
	checkPlayoffSeeds("playoffs-example-large-seeded.xlsx",
		[]string{"CLARK", "RODRIGUEZ", "LEWIS", "LEE"})
}

// checkPoolSeeds reads the 'data' sheet (col A = pool label, col B = full player name)
// and reports which pool each seed is assigned to, then verifies all seeds are in distinct pools.
func checkPoolSeeds(filename string, seedNames []string) {
	fmt.Printf("\n--- %s ---\n", filename)

	r, err := zip.OpenReader(filename)
	if err != nil {
		fmt.Printf("  error opening: %v\n", err)
		return
	}
	defer r.Close()

	ss := loadSharedStrings(r)
	sheetMap := buildSheetMap(r)

	dataPath, ok := sheetMap["data"]
	if !ok {
		fmt.Println("  'data' sheet not found")
		return
	}

	var dataContent string
	for _, f := range r.File {
		if f.Name == "xl/"+dataPath {
			dataContent = readFile(f)
			break
		}
	}

	// Build row -> pool label (col A) and row -> player name (col B) maps.
	cellRe := regexp.MustCompile(`<c r="([A-Z]+(\d+))"[^>]*t="s"[^>]*><v>(\d+)</v></c>`)
	rowPool := make(map[string]string) // row number string -> pool label
	rowName := make(map[string]string) // row number string -> player name

	for _, m := range cellRe.FindAllStringSubmatch(dataContent, -1) {
		cellRef, rowStr, idxStr := m[1], m[2], m[3]
		col := strings.TrimRight(cellRef, "0123456789")
		idx, _ := strconv.Atoi(idxStr)
		if idx >= len(ss) {
			continue
		}
		val := ss[idx]
		switch col {
		case "A":
			rowPool[rowStr] = val
		case "B":
			rowName[rowStr] = val
		}
	}

	// For each seed, find its pool.
	seedPool := make([]string, len(seedNames))
	for rowStr, name := range rowName {
		for i, seed := range seedNames {
			if name == seed {
				seedPool[i] = rowPool[rowStr]
			}
		}
	}

	for i, name := range seedNames {
		pool := seedPool[i]
		if pool == "" {
			pool = "NOT FOUND"
		}
		fmt.Printf("  Seed %d (%s): %s\n", i+1, name, pool)
	}

	// Verify all seeds are in different pools.
	fmt.Print("  Pool separation: ")
	pools := make(map[string]int) // pool label -> seed index (1-based)
	ok2 := true
	for i, pool := range seedPool {
		if pool == "" {
			fmt.Printf("FAIL (seed %d not found)\n", i+1)
			ok2 = false
			break
		}
		if prev, dup := pools[pool]; dup {
			fmt.Printf("FAIL (seed %d and seed %d both in %s)\n", prev, i+1, pool)
			ok2 = false
			break
		}
		pools[pool] = i + 1
	}
	if ok2 {
		fmt.Println("OK (all seeds in distinct pools)")
	}
}

// checkPlayoffSeeds finds where each seed's display name (zekken name) appears in
// Tree sheets and checks that seed 1 and seed 2 land in different tree sheets.
func checkPlayoffSeeds(filename string, seedNames []string) {
	fmt.Printf("\n--- %s ---\n", filename)

	r, err := zip.OpenReader(filename)
	if err != nil {
		fmt.Printf("  error opening: %v\n", err)
		return
	}
	defer r.Close()

	ss := loadSharedStrings(r)
	sheetMap := buildSheetMap(r)

	type location struct {
		sheet string
		row   int
	}
	// Record only the first (lowest-row) appearance per seed per sheet, which is the leaf position.
	firstLoc := make([]location, len(seedNames))
	found := make([]bool, len(seedNames))

	for treeNum := 1; treeNum <= 16; treeNum++ {
		sheetName := fmt.Sprintf("Tree %d", treeNum)
		path, ok := sheetMap[sheetName]
		if !ok {
			break
		}
		var content string
		for _, f := range r.File {
			if f.Name == "xl/"+path {
				content = readFile(f)
				break
			}
		}
		for i, name := range seedNames {
			rows := rowsContaining(content, ss, name)
			if len(rows) == 0 {
				continue
			}
			minRow := rows[0]
			for _, row := range rows[1:] {
				if row < minRow {
					minRow = row
				}
			}
			if !found[i] {
				firstLoc[i] = location{sheetName, minRow}
				found[i] = true
			}
		}
	}

	for i, name := range seedNames {
		if !found[i] {
			fmt.Printf("  Seed %d (%s): NOT FOUND\n", i+1, name)
		} else {
			fmt.Printf("  Seed %d (%s): first appears in %q at row %d\n",
				i+1, name, firstLoc[i].sheet, firstLoc[i].row)
		}
	}

	// Check seed 1 vs seed 2 are on different tree sheets.
	fmt.Print("  Seed 1 vs Seed 2 separation: ")
	if !found[0] || !found[1] {
		fmt.Println("SKIP (one or both not found)")
	} else if firstLoc[0].sheet != firstLoc[1].sheet {
		fmt.Printf("OK (seed 1 in %q, seed 2 in %q)\n", firstLoc[0].sheet, firstLoc[1].sheet)
	} else {
		fmt.Printf("same sheet %q — rows %d vs %d\n", firstLoc[0].sheet, firstLoc[0].row, firstLoc[1].row)
	}
}

// loadSharedStrings reads xl/sharedStrings.xml and returns the ordered string list.
func loadSharedStrings(r *zip.ReadCloser) []string {
	for _, f := range r.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		content := readFile(f)
		// Match both plain <t>...</t> and rich-text <r><t>...</t></r> inside <si>
		siRe := regexp.MustCompile(`<si>(.*?)</si>`)
		tRe := regexp.MustCompile(`<t[^>]*>([^<]*)</t>`)
		var result []string
		for _, si := range siRe.FindAllStringSubmatch(content, -1) {
			var parts []string
			for _, t := range tRe.FindAllStringSubmatch(si[1], -1) {
				parts = append(parts, t[1])
			}
			result = append(result, strings.Join(parts, ""))
		}
		return result
	}
	return nil
}

// buildSheetMap returns sheetName -> relative path (e.g. "worksheets/sheet1.xml").
func buildSheetMap(r *zip.ReadCloser) map[string]string {
	nameToRid := make(map[string]string)
	ridToTarget := make(map[string]string)

	for _, f := range r.File {
		switch f.Name {
		case "xl/workbook.xml":
			re := regexp.MustCompile(`<sheet name="([^"]+)"[^>]+r:id="([^"]+)"`)
			for _, m := range re.FindAllStringSubmatch(readFile(f), -1) {
				nameToRid[m[1]] = m[2]
			}
		case "xl/_rels/workbook.xml.rels":
			re := regexp.MustCompile(`<Relationship Id="([^"]+)"[^>]+Target="([^"]+)"`)
			for _, m := range re.FindAllStringSubmatch(readFile(f), -1) {
				ridToTarget[m[1]] = m[2]
			}
		}
	}

	result := make(map[string]string)
	for name, rid := range nameToRid {
		if target, ok := ridToTarget[rid]; ok {
			result[name] = target
		}
	}
	return result
}

// rowsContaining returns the distinct row numbers in the sheet XML where the target
// string appears (via shared string index or inline text).
func rowsContaining(sheetXML string, ss []string, target string) []int {
	targetUp := strings.ToUpper(target)
	seen := make(map[int]bool)
	var rows []int

	// Match individual cells: <c r="A12" ...>...</c>
	cellRe := regexp.MustCompile(`<c r="([A-Z]+(\d+))"[^>]*>(.*?)</c>`)
	sharedValRe := regexp.MustCompile(`<v>(\d+)</v>`)
	inlineRe := regexp.MustCompile(`<t[^>]*>([^<]*)</t>`)

	for _, m := range cellRe.FindAllStringSubmatch(sheetXML, -1) {
		rowStr := m[2]
		cellBody := m[3]

		row, err := strconv.Atoi(rowStr)
		if err != nil {
			continue
		}

		match := false
		// Shared string reference
		if vm := sharedValRe.FindStringSubmatch(cellBody); vm != nil {
			idx, err := strconv.Atoi(vm[1])
			if err == nil && idx < len(ss) {
				if strings.Contains(strings.ToUpper(ss[idx]), targetUp) {
					match = true
				}
			}
		}
		// Inline string
		if !match {
			if im := inlineRe.FindStringSubmatch(cellBody); im != nil {
				if strings.Contains(strings.ToUpper(im[1]), targetUp) {
					match = true
				}
			}
		}

		if match && !seen[row] {
			seen[row] = true
			rows = append(rows, row)
		}
	}
	return rows
}

func checkBreaks(filename string) {
	r, err := zip.OpenReader(filename)
	if err != nil {
		fmt.Println("Error opening zip:", err)
		return
	}
	defer r.Close()

	sheetIDMap := make(map[string]string)
	for _, f := range r.File {
		if f.Name == "xl/workbook.xml" {
			content := readFile(f)
			re := regexp.MustCompile(`<sheet name="([^"]+)" [^>]*r:id="([^"]+)"`)
			matches := re.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				sheetIDMap[m[1]] = m[2]
			}
		}
	}

	relMap := make(map[string]string)
	for _, f := range r.File {
		if f.Name == "xl/_rels/workbook.xml.rels" {
			content := readFile(f)
			re := regexp.MustCompile(`<Relationship Id="([^"]+)" [^>]*Target="([^"]+)"`)
			matches := re.FindAllStringSubmatch(content, -1)
			for _, m := range matches {
				relMap[m[1]] = m[2]
			}
		}
	}

	sheetName := "Pool Draw"
	rId, ok := sheetIDMap[sheetName]
	if !ok {
		fmt.Printf("Sheet %s not found\n", sheetName)
		return
	}
	target := relMap[rId]
	sheetPath := "xl/" + target

	fmt.Printf("Sheet: %s (%s)\n", sheetName, sheetPath)

	for _, f := range r.File {
		if f.Name == sheetPath {
			content := readFile(f)
			re := regexp.MustCompile(`<rowBreaks[^>]*>(.*?)</rowBreaks>`)
			matches := re.FindStringSubmatch(content)
			if len(matches) > 0 {
				brkRe := regexp.MustCompile(`<brk id="(\d+)"`)
				brks := brkRe.FindAllStringSubmatch(matches[1], -1)
				fmt.Printf("  Found %d manual page breaks at rows: ", len(brks))
				for _, b := range brks {
					fmt.Printf("%s ", b[1])
				}
				fmt.Println()
			} else {
				fmt.Println("  No manual page breaks found.")
			}
		}
	}
}

func readFile(f *zip.File) string {
	rc, err := f.Open()
	if err != nil {
		return ""
	}
	defer rc.Close()
	content, _ := io.ReadAll(rc)
	return string(content)
}

package main

import (
	"archive/zip"
	"fmt"
	"io"
	"regexp"
)

func main() {
	files := []string{
		"pools-example-large-teams.xlsx",
		"pools-example-large-teams-max-size.xlsx",
		"pools-example-large-max-size.xlsx",
	}

	for _, filename := range files {
		fmt.Printf("\n--- Checking %s ---\n", filename)
		checkBreaks(filename)
	}
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

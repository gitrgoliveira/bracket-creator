package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/xuri/excelize/v2"
)

type createOptions struct {
	numPlayers int
	filePath   string
}

func defaultCreateOptions() *createOptions {
	return &createOptions{}
}
func newCreateCmd() *cobra.Command {
	o := defaultCreateOptions()

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "subcommand to create brackets",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.Flags().IntVarP(&o.numPlayers, "players", "", 3, "minimum number of players per pool")

	cmd.Flags().StringVarP(&o.filePath, "path", "p", "", "file with the list of players")

	return cmd
}

func (o *createOptions) run(cmd *cobra.Command, args []string) error {
	fmt.Fprintf(cmd.OutOrStdout(), "%s\n", o.filePath)

	file, err := os.Open(o.filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	entries := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := scanner.Text()
		entries = append(entries, entry)
	}

	// shuffle all entries
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
		return nil
	}
	groups, _ := o.createGroups(entries)

	f := excelize.NewFile()
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()
	_ = o.addGroupsToSheet(f, groups)

	_ = o.addBracketsToSheet(f, groups)

	// Save the spreadsheet file
	outputPath := "groups.xlsx" // Replace with the desired output file path
	if err := f.SaveAs(outputPath); err != nil {
		fmt.Println("Error saving Excel file:", err)
		return nil
	}

	fmt.Println("Excel file created successfully:", outputPath)
	return nil
}

func (o *createOptions) createGroups(entries []string) ([]map[int]string, error) {

	groups := make([]map[int]string, 0)
	currentGroup := make(map[int]string)

	// Calculate the number of full groups and the remaining entries
	fullGroups := len(entries) / o.numPlayers
	remainingEntries := len(entries) % o.numPlayers

	// Assign entries to full groups
	for i := 0; i < fullGroups; i++ {
		for j := 1; j <= o.numPlayers; j++ {
			index := i*o.numPlayers + j - 1
			currentGroup[j] = entries[index]
		}
		groups = append(groups, currentGroup)
		currentGroup = make(map[int]string)
	}
	// Distribute remaining entries among previous groups
	if remainingEntries > 0 {
		fmt.Printf("Some groups will have more than %d\n", o.numPlayers)
	}
	for i := 0; i < remainingEntries; i++ {
		index := fullGroups*o.numPlayers + i
		groupIndex := i % fullGroups
		groups[groupIndex][o.numPlayers+i+1] = entries[index]
	}

	// for i, group := range groups {
	// 	fmt.Printf("Group %d:\n", i+1)
	// 	for _, entry := range group {
	// 		fmt.Printf("%s\n", entry)
	// 	}
	// 	fmt.Println()
	// }

	return groups, nil
}

func (o *createOptions) addGroupsToSheet(f *excelize.File, groups []map[int]string) error {
	sheetName := "Groups"
	f.NewSheet(sheetName)

	// Set the header row
	f.SetCellValue(sheetName, "A1", "Group Number")
	f.SetCellValue(sheetName, "B1", "Team Name")

	// Populate the groups in the spreadsheet
	row := 2
	for i, group := range groups {
		for _, entry := range group {
			groupNumber := fmt.Sprintf("Group %d", i+1)
			f.SetCellValue(sheetName, fmt.Sprintf("A%d", row), groupNumber)
			f.SetCellValue(sheetName, fmt.Sprintf("B%d", row), entry)
			row++
		}
		row++ // Add an extra row between groups
	}

	// Set the column widths
	f.SetColWidth(sheetName, "A", "A", 15)
	f.SetColWidth(sheetName, "B", "B", 30)

	return nil
}

func (o *createOptions) addBracketsToSheet(f *excelize.File, groups []map[int]string) error {
	// Calculate the number of rounds
	// numRounds := teamsPerGroup - 1

	// Set the starting row and column for the bracket
	startRow := 1
	startCol := 1

	// Write the bracket data to Excel
	for _, group := range groups {
		// Set the group header
		teamsPerGroup := len(group)
		numRounds := teamsPerGroup - 1

		cell_name, _ := excelize.ColumnNumberToName(startCol)
		cell := cell_name + fmt.Sprint(startRow)
		f.SetCellValue("Sheet1", cell, group)
		startRow++

		// Generate the round-robin matches
		for round := 1; round <= numRounds; round++ {
			for teamIndex := 0; teamIndex < teamsPerGroup; teamIndex++ {
				team1Index := teamIndex
				team2Index := (teamIndex + round) % teamsPerGroup
				team1 := group[team1Index]
				team2 := group[team2Index]

				cell_name, _ = excelize.ColumnNumberToName(startCol)
				cell = cell_name + fmt.Sprint(startRow)
				f.SetCellValue("Sheet1", cell, team1+" vs "+team2)
				startRow++
			}
		}

		startRow += 2
		startCol++
	}
	return nil
}

// func (o *exampleOptions) parseArgs(args []string) ([]int, error) {
// }

// createCmd represents the version command

func init() {
	rootCmd.AddCommand(newCreateCmd())
}

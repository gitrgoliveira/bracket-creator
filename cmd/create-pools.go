package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"
)

type poolOptions struct {
	numPlayers      int
	maxPlayers      int
	poolWinners     int
	teamMatches     int
	courts          int
	filePath        string
	outputPath      string
	seedsPath       string
	outputWriter    *bufio.Writer
	roundRobin      bool
	withZekkenName  bool
	singleTree      bool
	determined      bool
	mirror          bool
	titlePrefix     string
	numberPrefix    string
	SeedAssignments []domain.SeedAssignment
}

func newCreatePoolCmd() *cobra.Command {

	o := &poolOptions{}

	cmd := &cobra.Command{
		Use:          "create-pools",
		Short:        "creates Pool brackets",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.PersistentFlags().BoolVarP(&o.determined, "determined", "d", false, "Do not shuffle the names read from the input file (default false)")
	cmd.PersistentFlags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.PersistentFlags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")
	cmd.Flags().IntVarP(&o.numPlayers, "players", "p", 3, "minimum number of players/teams per pool")
	cmd.Flags().IntVarP(&o.maxPlayers, "max-players", "m", 0, "maximum number of players/teams per pool")
	cmd.Flags().IntVarP(&o.poolWinners, "pool-winners", "w", 2, "number of players/teams that can qualify from each pool")
	cmd.Flags().BoolVarP(&o.roundRobin, "round-robin", "r", false, "ensure all pools are round robin. Example, in a pool of 4, everyone would fight everyone (default false)")
	cmd.Flags().BoolVarP(&o.withZekkenName, "with-zekken-name", "z", false, "Use the second column of the input CSV as the participant's display name on the zekken. Falls back to sanitized name if empty.")
	cmd.Flags().BoolVarP(&o.singleTree, "single-tree", "", false, "Create a single tree instead of dividing into multiple sheets (default false)")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")
	cmd.Flags().IntVarP(&o.courts, "courts", "c", 2, "number of Shiaijo (courts) to distribute pools across (default 2)")
	cmd.Flags().BoolVarP(&o.mirror, "mirror", "", true, "Mirror match sides (White on left, Red on right) (default true)")
	cmd.Flags().StringVarP(&o.titlePrefix, "title-prefix", "", "", "title prefix for the tournament (default \"\")")
	cmd.Flags().StringVarP(&o.seedsPath, "seeds", "", "", "CSV file mapping exact participant names to their initial seed rank")
	cmd.Flags().StringVarP(&o.numberPrefix, "number-prefix", "n", "", "Assign consecutive numbers with this letter prefix (e.g. 'K' produces K1, K2, ...)")

	cmd.MarkFlagsMutuallyExclusive("players", "max-players")

	if err := cmd.MarkPersistentFlagRequired("file"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking file flag as required: %v\n", err)
	}
	if err := cmd.MarkPersistentFlagRequired("output"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking output flag as required: %v\n", err)
	}

	return cmd
}

func (o *poolOptions) run(cmd *cobra.Command, args []string) error {
	fmt.Printf("Reading file: %s\n", o.filePath)

	entries, err := helper.ReadEntriesFromFile(o.filePath)
	if err != nil {
		return fmt.Errorf("failed to read entries from file: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no entries found in file")
	}

	if err := helper.ValidateCourts(o.courts); err != nil {
		return err
	}

	outputFile, outputWriter, err := openOutputFile(o.outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := outputFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing output file: %v\n", err)
		}
	}()
	o.outputWriter = outputWriter
	defer func() {
		if err := o.outputWriter.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing output buffer: %v\n", err)
		}
	}()

	if o.seedsPath != "" {
		fmt.Printf("Parsing seeds file: %s\n", o.seedsPath)
		assignments, err := helper.ParseSeedsFile(o.seedsPath)
		if err != nil {
			return fmt.Errorf("failed to parse seeds file: %w", err)
		}
		o.SeedAssignments = append(o.SeedAssignments, assignments...)
	}

	err = o.createPools(entries)
	if err != nil {
		return fmt.Errorf("failed to create pools: %w", err)
	}

	fmt.Println("Excel file created successfully:", o.outputPath)
	return nil
}

func (o *poolOptions) createPools(entries []string) error {
	isMax := o.maxPlayers > 0
	activePoolSize := o.numPlayers
	if isMax {
		activePoolSize = o.maxPlayers
	}

	// Apply default for courts (0 means unset, e.g. when struct is built directly in tests)
	if o.courts < 1 {
		o.courts = 2
	}

	// validation
	if len(entries) < o.poolWinners {
		return fmt.Errorf("number of entries must be higher than number of winners per pool")
	}
	if !isMax && len(entries) < activePoolSize {
		return fmt.Errorf("number of entries must be greater than requested players in pool")
	}
	if isMax && len(entries) < 2 {
		return fmt.Errorf("number of entries must be at least 2")
	}
	// In max-mode the equality case (entries == poolWinners) would otherwise
	// produce a "tournament" where every player auto-qualifies. Reject it.
	if isMax && len(entries) <= o.poolWinners {
		return fmt.Errorf("number of entries must be higher than number of winners per pool")
	}

	if activePoolSize < 2 {
		return fmt.Errorf("number of players per pool must be greater than 1")
	}
	if o.poolWinners >= activePoolSize {
		return fmt.Errorf("number of pool winners must be less than number of players per pool")
	}

	players, err := processEntries(entries, o.determined, o.withZekkenName)
	if err != nil {
		return err
	}

	if len(o.SeedAssignments) > 0 {
		err := helper.ApplySeeds(players, o.SeedAssignments)
		if err != nil {
			return fmt.Errorf("failed to apply seeds: %w", err)
		}
	}

	// Calculate number of pools to ensure seeding distribution matches pool count
	var numPools int
	if isMax {
		numPools = (len(players) + activePoolSize - 1) / activePoolSize
	} else {
		numPools = len(players) / activePoolSize
	}
	if numPools == 0 {
		return fmt.Errorf("not enough valid participants (%d) to form a pool of size %d", len(players), activePoolSize)
	}

	// Reorder players to ensure seeded participants are distributed effectively across pools
	players = helper.PoolSeeding(players, numPools, o.courts)

	pools, err := helper.CreatePools(players, activePoolSize, isMax)
	if err != nil {
		return err
	}

	// Reorder pools so contiguous court blocks have balanced sizes and
	// seeds are spread across courts (deinterleave by numCourts).
	pools = helper.ReorderPoolsForCourts(pools, o.courts)

	if o.numberPrefix != "" {
		counter := 1
		for i := range pools {
			counter = assignPlayerNumbers(pools[i].Players, o.numberPrefix, counter)
		}
	}

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	if o.withZekkenName {
		fmt.Println("Using Zekken names")
	}

	helper.AddPoolDataToSheet(f, pools, o.withZekkenName, o.titlePrefix)

	if err := helper.AddPoolsToSheet(f, pools); err != nil {
		fmt.Fprintf(os.Stderr, "Error adding pools to sheet: %v\n", err)
	}
	finals := helper.GenerateFinals(pools, o.poolWinners)

	fmt.Printf("There will be %d finalists\n", len(finals))

	maxPlayersPerTree := helper.MaxPlayersPerTree
	numPages, err := helper.RoundToPowerOf2(float64(len(finals)), float64(maxPlayersPerTree))
	if err != nil {
		return err
	}
	if numPages < 1 || o.singleTree {
		numPages = 1
	}
	// Clamp courts to the number of pools (e.g. if defaulted to 2 but only 1 pool exists)
	if o.courts > numPools {
		o.courts = numPools
	}
	// Ensure enough tree pages for the number of courts
	if courtPages := helper.NextPow2(o.courts); courtPages > numPages {
		numPages = courtPages
	}
	fmt.Printf("Spread across %d tree pages\n", numPages)

	// Create balanced tree
	tree := helper.CreateBalancedTree(finals)

	// divide the tree depending on the number of pages
	subtrees := helper.SubdivideTree(tree, numPages)

	// Create pool matches and get winners BEFORE creating tree sheets
	if o.roundRobin {
		helper.CreatePoolRoundRobinMatches(pools)
	} else {
		helper.CreatePoolMatches(pools)
	}
	matchWinners := helper.PrintPoolMatches(f, pools, o.teamMatches, o.poolWinners, o.courts, o.mirror)

	treeSheet, err := f.GetSheetIndex(helper.SheetTree)
	if err != nil {
		return fmt.Errorf("could not find Tree sheet: %w", err)
	}
	// adding extra sheets
	for i := 0; i < len(subtrees); i++ {
		subtreeSheet := "Tree " + strconv.Itoa(i+1)
		fmt.Printf("Adding sheet %s\n", subtreeSheet)
		index, err := f.NewSheet(subtreeSheet)
		if err != nil {
			return fmt.Errorf("failed to create sheet %s: %w", subtreeSheet, err)
		}
		err = f.CopySheet(treeSheet, index)
		if err != nil {
			return fmt.Errorf("failed to copy sheet %d to %s: %w", treeSheet, subtreeSheet, err)
		}

		depth := helper.CalculateDepth(subtrees[i])
		fmt.Printf("With tree Depth: %d\n", depth)
		startRow := helper.TreeTitleRows + 1

		// pagesPerCourt should be >= 1 because numPages >= NextPow2(courts) >= courts,
		// but SubdivideTree may return fewer subtrees than requested for small
		// brackets. Guard against the divide-by-zero in that degenerate case
		// rather than relying on the invariant alone.
		pagesPerCourt := len(subtrees) / o.courts
		if pagesPerCourt < 1 {
			pagesPerCourt = 1
		}
		courtIndex := i / pagesPerCourt
		if courtIndex >= o.courts {
			courtIndex = o.courts - 1
		}
		courtLabel := string("ABCDEFGHIJKLMNOPQRSTUVWXYZ"[courtIndex])
		helper.SetTreeSheetTitle(f, subtreeSheet, "Shiaijo "+courtLabel)
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, true, matchWinners)

		poolStart, poolEnd := poolBoundsForSubtree(len(pools), o.courts, len(subtrees), i)
		helper.AddPoolsToTree(f, subtreeSheet, pools[poolStart:poolEnd])
	}
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		fmt.Println("Note: Tree sheet might not exist:", err)
	}

	depth := helper.CalculateDepth(tree)
	eliminationMatchRounds := make([][]*helper.Node, depth-1)
	// Get all the rounds
	for i := depth; i > 1; i-- {
		rounds := helper.TraverseRounds(tree, 1, i-1)
		eliminationMatchRounds[depth-i] = rounds
		fmt.Printf("Elimination matches for round %d: %d\n", i-1, len(eliminationMatchRounds[depth-i]))
	}

	helper.FillInMatches(f, eliminationMatchRounds)

	helper.CreateNamesWithPoolToPrint(f, pools, o.withZekkenName, o.courts)

	if err := helper.CreateTagsSheet(f, pools); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tags sheet: %v\n", err)
	}

	var totalPoolMatches int
	for _, p := range pools {
		totalPoolMatches += len(p.Matches)
	}

	helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, o.teamMatches, o.courts, o.mirror)
	helper.FillEstimations(f, int64(len(pools)), int64(totalPoolMatches), int64(o.teamMatches), int64(len(finals)-1), o.courts)

	// Apply sheet protection to all sheets except data and Time Estimator
	helper.ProtectAllSheets(f)

	// Save the spreadsheet file
	err = f.Write(o.outputWriter)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %w", err)
	}

	return nil
}

// poolBoundsForSubtree returns the [start, end) slice into the pool list for
// the given subtree. After ReorderPoolsForCourts the pool list is laid out in
// contiguous per-court blocks; this function respects those boundaries so that
// no subtree page ever references pools from more than one court.
// Uses the same AssignPoolsToCourts logic as PrintPoolMatches so both views
// are always consistent.
func poolBoundsForSubtree(numPools, numCourts, numSubtrees, subtreeIdx int) (start, end int) {
	if numCourts < 1 || numSubtrees < 1 {
		return 0, 0
	}
	pagesPerCourt := numSubtrees / numCourts
	if pagesPerCourt < 1 {
		pagesPerCourt = 1
	}
	courtIdx := subtreeIdx / pagesPerCourt
	if courtIdx >= numCourts {
		courtIdx = numCourts - 1
	}
	pageWithinCourt := subtreeIdx % pagesPerCourt

	// Derive court block boundaries from the same assignment used by Pool Matches.
	assignments, _ := helper.AssignPoolsToCourts(numPools, numCourts)
	courtStart, courtEnd := -1, 0
	for i, c := range assignments {
		if c == courtIdx {
			if courtStart < 0 {
				courtStart = i
			}
			courtEnd = i + 1
		}
	}
	if courtStart < 0 {
		return 0, 0
	}

	courtSize := courtEnd - courtStart
	poolsPerPage := (courtSize + pagesPerCourt - 1) / pagesPerCourt
	start = courtStart + pageWithinCourt*poolsPerPage
	end = min(start+poolsPerPage, courtEnd)
	return
}

func init() {
	rootCmd.AddCommand(newCreatePoolCmd())
}

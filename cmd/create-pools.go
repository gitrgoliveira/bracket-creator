package cmd

import (
	"bufio"
	"fmt"
	"os"

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
	poolFormat      string // "" / "full" → legacy roundRobin switch; "partial" → path-graph
	withZekkenName  bool
	singleTree      bool
	determined      bool
	engi            bool // engi (kata) competition: pair rosters + engi standings formulas. Set ONLY by the web /create handler (mobile-app blank-template download); deliberately NOT a CLI flag (owner decision: no new CLI options).
	naginata        bool // naginata: adds a 3rd-place bronze block after elimination matches. Web-handler-only, same as engi.
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
	cmd.Flags().IntVarP(&o.courts, "courts", "c", 2, "number of Shiaijo (courts) to distribute pools across: 1, 2, 4, 8 or 16 (default 2)")
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

	// The pool-count clamp below can lower o.courts to a value the operator
	// never asked for, so it steps down through helper.EffectiveDrawCourts,
	// which lands on a legal count by construction.
	if err := helper.ValidateDrawCourtCount(o.courts); err != nil {
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
	// The CLI has no NAMED shiaijo: --courts says how many the draw runs on, so
	// the sheets are titled A, B, C by position. The live app passes the
	// competition's own court list instead, which need not start at A.
	courtNames := helper.CourtLabels(o.courts)
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

	// Roster guard before the phase runs, so the operator gets a message about
	// their entry list rather than CreatePools' internal complaint. This is the
	// caller's job; helper.BuildPoolPhase re-derives the same count with the same
	// function, so the two cannot disagree about whether a pool can be formed.
	if helper.PoolCount(len(players), activePoolSize, isMax) == 0 {
		return fmt.Errorf("not enough valid participants (%d) to form a pool of size %d", len(players), activePoolSize)
	}

	// The whole pool phase, in the one order its steps are valid in, shared with
	// internal/engine/pools.go so the two paths cannot drift again.
	// helper.BuildPoolPhase's doc comment carries the constraints and the worked
	// examples.
	//
	// o.courts is REPLACED by the count that comes back: the clamp can produce a
	// value the operator never asked for (a shiaijo with no home pool would own an
	// empty bracket region), and every sheet below bands against it, so the Pool
	// Matches and Names sheets use the same shiaijo count as the bracket regions.
	// create-pools always produces a knockout bracket from its pools, so R9's
	// power-of-two step-down applies.
	pools, drawCourts, err := helper.BuildPoolPhase(players, activePoolSize, isMax, o.courts, true)
	if err != nil {
		return err
	}
	o.courts = drawCourts

	if o.numberPrefix != "" {
		counter := 1
		for i := range pools {
			counter = helper.AssignPlayerNumbers(pools[i].Players, o.numberPrefix, counter)
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

	poolCoords, playerCoords := helper.AddPoolDataToSheet(f, pools, o.withZekkenName, o.titlePrefix)

	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords); err != nil {
		fmt.Fprintf(os.Stderr, "Error adding pools to sheet: %v\n", err)
	}
	fmt.Printf("There will be %d finalists\n", len(pools)*o.poolWinners)

	// Create pool matches BEFORE the draw: R6's second bye criterion ranks by
	// how many pool matches a pool's qualifier plays (D1), which helper.poolLoad
	// reads off the drawn pool. Mirror the engine's authoritative PoolFormat ×
	// RoundRobin mapping (internal/engine/pools.go) so an exported partial-pool
	// competition gets the path-graph match set, not full round-robin.
	if o.poolFormat == "partial" {
		helper.CreatePartialPoolMatches(pools)
	} else if o.roundRobin {
		helper.CreatePoolRoundRobinMatches(pools)
	} else {
		helper.CreatePoolMatches(pools)
	}
	matchWinners, _ := helper.PrintPoolMatches(f, pools, o.teamMatches, o.poolWinners, courtNames, nil, true, poolCoords, playerCoords, o.engi)

	// Court-first pool-to-knockout draw (specs/007-ekc-draw): one bracket
	// region per shiaijo, 2nd places crossing to the partner court, byes
	// allocated inside each region by seed then pool load.
	draw := helper.BuildKnockoutDraw(pools, o.poolWinners, o.courts)
	if draw == nil {
		return fmt.Errorf("could not build a knockout draw from %d pools with %d winners per pool", len(pools), o.poolWinners)
	}

	// R2/D7: a seeding constraint the configuration cannot satisfy is a
	// WARNING, never an error -- the draw always happens and the operator can
	// move a seed by hand. On the command line the operator is watching this
	// output as the workbook is written, so stdout is where they read it; the
	// workbook itself is the artifact, not a message channel. Silent on a
	// competition with no seeds, which is a normal configuration.
	for _, w := range helper.SeedPlacementWarnings(draw, pools) {
		fmt.Printf("Warning: %s\n", w)
	}

	plan := blankWorkbookCourtPlan(draw, courtNames)
	eliminationMatchRounds, numPages, err := helper.RenderKnockoutPages(f, plan, o.singleTree, pools, poolCoords, playerCoords, matchWinners)
	if err != nil {
		return err
	}
	finishKnockoutPages(f, numPages, eliminationMatchRounds)

	helper.CreateNamesWithPoolToPrint(f, pools, o.withZekkenName, courtNames, nil, playerCoords)

	if err := helper.CreateTagsSheet(f, pools, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tags sheet: %v\n", err)
	}

	var totalPoolMatches int
	for _, p := range pools {
		totalPoolMatches += len(p.Matches)
	}

	printEliminationWithBronze(f, matchWinners, eliminationMatchRounds, o.teamMatches, plan, o.engi, o.naginata)
	helper.FillEstimations(f, int64(len(pools)), int64(totalPoolMatches), int64(o.teamMatches), int64(len(pools)*o.poolWinners-1), o.courts)

	// Apply sheet protection to all sheets except data and Time Estimator
	helper.ProtectAllSheets(f)

	// Save the spreadsheet file
	err = f.Write(o.outputWriter)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(newCreatePoolCmd())
}

package cmd

import (
	"bufio"
	"fmt"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/spf13/cobra"
)

type poolOptions struct {
	numPlayers     int
	maxPlayers     int
	poolWinners    int
	teamMatches    int
	courts         int
	filePath       string
	outputPath     string
	seedsPath      string
	outputWriter   *bufio.Writer
	roundRobin     bool
	poolFormat     string // "" / "full" → legacy roundRobin switch; "partial" → path-graph
	withZekkenName bool
	singleTree     bool
	determined     bool
	engi           bool // engi (kata) competition: pair rosters + engi standings formulas. Set ONLY by the web /create handler (mobile-app blank-template download); deliberately NOT a CLI flag (owner decision: no new CLI options).
	naginata       bool // naginata: adds a 3rd-place bronze block after elimination matches. Web-handler-only, same as engi.
	titlePrefix    string
	numberPrefix   string
	// extraQualifiers selects how many finishers a pool sends to the
	// knockout beyond poolWinners (bc-qual, --extra-qualifiers): "" (default,
	// state.ExtraQualifiersNone), "larger-pools"
	// (state.ExtraQualifiersLargerPools), or "fill-bracket"
	// (state.ExtraQualifiersFillBracket, bc-qual LP-4). Validated by
	// state.ValidateExtraQualifiers, the same function internal/engine and
	// internal/state use, so the rule is stated once.
	extraQualifiers string
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
	cmd.Flags().StringVarP(&o.extraQualifiers, "extra-qualifiers", "", "", "how many finishers each pool sends to the knockout: \"\" (standard, default), \"larger-pools\" (a pool larger than the minimum sends one extra qualifier, crossed to a neighbouring shiaijo), or \"fill-bracket\" (pools are cut so winners plus a handful of drafted 2nd places exactly fill the knockout with no byes); requires minimum-players-per-pool sizing (--players, not --max-players) and --pool-winners 1")

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
	//
	// extraQualifiers is validated up front, against the CLI's own view of
	// pool-size mode (isMax) and pool-winners count, via state's single
	// owner of the rule (state.ValidateExtraQualifiers) rather than
	// restating it here: min-mode-only, poolWinners==1 for both larger-pools
	// and fill-bracket (bc-qual LP-4).
	poolSizeModeForValidation := "min"
	if isMax {
		poolSizeModeForValidation = "max"
	}
	// EffectivePoolWinners(), not the raw flag. ValidateExtraQualifiers' doc
	// comment says so explicitly: its poolWinners == 1 gate reads a RESOLVED
	// count, and an unresolved 0 (`--pool-winners 0`, or a poolOptions built
	// directly in a test) is < 2 and slips straight through. The operator then
	// reaches BuildKnockoutDrawPerPool with defaultWinners=0, which refuses
	// with the generic "this pool/shiaijo shape is outside what
	// --extra-qualifiers larger-pools currently supports" instead of the
	// accurate "requires pool winners = 1". Resolved through state's own
	// accessor rather than a local `if <= 0 { 2 }`, so the CLI and the app
	// cannot disagree about what an unset pool-winners count means.
	effectivePoolWinners := (state.Competition{PoolWinners: o.poolWinners}).EffectivePoolWinners()
	if err := state.ValidateExtraQualifiers(o.extraQualifiers, poolSizeModeForValidation, effectivePoolWinners); err != nil {
		return err
	}
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
	//
	// Skipped under fill-bracket: its pool count is FillBracketPoolCount's
	// formation objective, not floor(n/activePoolSize) (bc-qual LP-4), so
	// this guard's own arithmetic does not apply -- BuildPoolPhaseFillBracket
	// below reports its own clean, actionable error (naming the entrant
	// count and minimum pool size) when no pool count fits.
	if o.extraQualifiers != state.ExtraQualifiersFillBracket && helper.PoolCount(len(players), activePoolSize, isMax) == 0 {
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
	//
	// --extra-qualifiers fill-bracket (bc-qual LP-4) forms pools via a
	// DIFFERENT objective (helper.BuildPoolPhaseFillBracket), beside
	// helper.BuildPoolPhase rather than a mutation of it (state.ValidateExtraQualifiers
	// above already rejected the combination with "max" sizing, so isMax is
	// never relevant on this branch).
	var pools []helper.Pool
	var drawCourts int
	if o.extraQualifiers == state.ExtraQualifiersFillBracket {
		pools, drawCourts, err = helper.BuildPoolPhaseFillBracket(players, activePoolSize, o.courts)
	} else {
		pools, drawCourts, err = helper.BuildPoolPhase(players, activePoolSize, isMax, o.courts)
	}
	if err != nil {
		return err
	}
	o.courts = drawCourts

	// The CLI has no NAMED shiaijo: --courts says how many the draw runs on, so
	// the sheets are titled A, B, C by position. The live app passes the
	// competition's own court list instead, which need not start at A.
	//
	// Derived AFTER the clamp above, never before. o.courts is not the number
	// the operator typed: it is raised to the default when unset and lowered by
	// BuildPoolPhase to what the pools can actually carry, so labelling the
	// pre-clamp value hands every consumer a band list that disagrees with the
	// draw (`-c 4 -p 3` on 7 competitors forms 2 pools and returned [A B C D]).
	// The workbook only survived that because PrintPoolMatches,
	// blankWorkbookCourtPlan and CreateNamesWithPoolToPrint each re-derive the
	// real count themselves; a consumer that reads len(plan.Courts) as the band
	// count -- the natural reading, and what TreePageTitle does with its own
	// argument -- would silently title a two-shiaijo draw "Shiaijo A-D".
	courtNames := helper.CourtLabels(o.courts)

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
	// MatchWinnerRanksNeeded, not o.poolWinners directly: under
	// --extra-qualifiers larger-pools, an oversized pool's crossed 2nd needs
	// a matchWinners["<pool>-2nd"] entry too, or the Tree/Elimination sheets
	// print it as inert literal text (or a broken CONCATENATE formula on the
	// Elimination Matches sheet) instead of a live link to the pool's actual
	// result. Reuses state's single owner of the rule (mirrors the engine's
	// two Excel export paths) rather than restating "+1" here.
	printPoolMatchesWinners := (state.Competition{PoolWinners: o.poolWinners, ExtraQualifiers: o.extraQualifiers}).MatchWinnerRanksNeeded()
	matchWinners, _ := helper.PrintPoolMatches(f, pools, o.teamMatches, printPoolMatchesWinners, courtNames, nil, true, poolCoords, playerCoords, o.engi)

	// Court-first pool-to-knockout draw (specs/007-ekc-draw): one bracket
	// region per shiaijo, 2nd places crossing to the partner court, byes
	// allocated inside each region by seed then pool load.
	//
	// state.ExtraQualifiersLargerPools (bc-qual --extra-qualifiers) sends a
	// SECOND qualifier from every oversized pool, crossed to a neighbouring
	// shiaijo (helper.BuildKnockoutDrawPerPool), instead of the uniform
	// one-qualifier-per-pool draw. The per-pool qualifier counts reuse
	// state.Competition.QualifiersForPool -- the single owner of the
	// oversized-pool arithmetic -- via cliExtraQualifierOverrides, rather
	// than restating that rule here.
	//
	// state.ExtraQualifiersFillBracket (bc-qual LP-4) instead drafts a 2nd
	// from the most senior oversized pools (helper.SelectFillBracketDrafts)
	// and crosses each to the OPPOSITE half of the draw from its own pool
	// (helper.BuildKnockoutDrawFillBracket) -- see that function's doc
	// comment for the evidence and scope.
	var draw *helper.KnockoutDraw
	// totalQualifiers feeds the Time Estimator sheet below (FillEstimations
	// derives elimination-round match count from finalist count - 1); it
	// starts at the uniform count and larger-pools/fill-bracket mode adds
	// each extra/drafted qualifier on top, so the estimate reflects the
	// actual draw size rather than silently under-counting the extras.
	totalQualifiers := len(pools) * o.poolWinners
	switch o.extraQualifiers {
	case state.ExtraQualifiersLargerPools:
		overrides := cliExtraQualifierOverrides(pools, activePoolSize, o.poolWinners)
		for _, w := range overrides {
			totalQualifiers += w - o.poolWinners
		}
		draw = helper.BuildKnockoutDrawPerPool(pools, o.poolWinners, overrides, o.courts)
		if draw == nil {
			// bc-qual LP-3a review item (b): NEVER fall back to the uniform
			// builder here -- that would silently seat the wrong number of
			// qualifiers per pool and drop the crossing the operator asked
			// for. This shape (e.g. a court count with no same-half
			// neighbour to cross to) is outside what larger-pools currently
			// supports; report it plainly instead of guessing.
			return fmt.Errorf("could not build a larger-pools knockout draw from %d pools with %d winner(s) per pool on %d shiaijo (this pool/shiaijo shape is outside what --extra-qualifiers larger-pools currently supports; adjust --courts/pool sizing, or drop --extra-qualifiers)", len(pools), o.poolWinners, o.courts)
		}
	case state.ExtraQualifiersFillBracket:
		drafts := helper.NextPow2(len(pools)) - len(pools)
		// Capacity-aware selection (bc-qual LP-4, second review rework):
		// FillBracketDraftCapacity computes each pool's home half and each
		// half's remaining draft capacity BEFORE any pool is chosen, so
		// SelectFillBracketDrafts can skip an oversized pool whose
		// opposite-half destination is already full instead of taking it
		// and stranding the build (helper.SelectFillBracketDrafts' own doc
		// comment has the mechanism and the 19WKC sheet verification).
		poolHalf, capacityByHalf, capOK := helper.FillBracketDraftCapacity(pools, drafts, o.courts)
		if !capOK {
			return fmt.Errorf("could not build a fill-bracket knockout draw: the per-court target arithmetic is not achievable for %d pools across %d shiaijo", len(pools), o.courts)
		}
		draftIdx, derr := helper.SelectFillBracketDrafts(pools, activePoolSize, poolHalf, capacityByHalf)
		if derr != nil {
			return fmt.Errorf("could not select fill-bracket drafts: %w", derr)
		}
		totalQualifiers += len(draftIdx)
		draw = helper.BuildKnockoutDrawFillBracket(pools, draftIdx, o.courts)
		if draw == nil {
			// Same discipline as larger-pools above: NEVER fall back to the
			// uniform builder. This shape (e.g. drafts that do not split
			// evenly across opposite halves for this court count) is
			// outside what fill-bracket currently supports.
			return fmt.Errorf("could not build a fill-bracket knockout draw from %d pools with %d draft(s) on %d shiaijo (this pool/shiaijo shape is outside what --extra-qualifiers fill-bracket currently supports; adjust --courts/pool sizing, or drop --extra-qualifiers)", len(pools), len(draftIdx), o.courts)
		}
	default:
		draw = helper.BuildKnockoutDraw(pools, o.poolWinners, o.courts)
		if draw == nil {
			return fmt.Errorf("could not build a knockout draw from %d pools with %d winners per pool", len(pools), o.poolWinners)
		}
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
	helper.FillEstimations(f, int64(len(pools)), int64(totalPoolMatches), int64(o.teamMatches), int64(totalQualifiers-1), o.courts)

	// Apply sheet protection to all sheets except data and Time Estimator
	helper.ProtectAllSheets(f)

	// Save the spreadsheet file
	err = f.Write(o.outputWriter)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %w", err)
	}

	return nil
}

// cliExtraQualifierOverrides builds the pool-index -> qualifier-count map
// helper.BuildKnockoutDrawPerPool expects for --extra-qualifiers
// larger-pools, reusing state.Competition.QualifiersForPool (the single
// owner of the oversized-pool arithmetic, bc-qual LP-3b) rather than
// restating its rule here -- the CLI has no state.Store-backed Competition of
// its own, so a throwaway value carrying just the fields QualifiersForPool
// reads (PoolSize, PoolWinners, ExtraQualifiers) is enough. poolSize is the
// minimum-mode pool size (createPools' activePoolSize) QualifiersForPool
// compares each pool's membership against.
func cliExtraQualifierOverrides(pools []helper.Pool, poolSize, poolWinners int) map[int]int {
	comp := state.Competition{
		PoolSize:        poolSize,
		PoolWinners:     poolWinners,
		ExtraQualifiers: state.ExtraQualifiersLargerPools,
	}
	var overrides map[int]int
	for i, p := range pools {
		if w := comp.QualifiersForPool(p); w != poolWinners {
			if overrides == nil {
				overrides = make(map[int]int, len(pools))
			}
			overrides[i] = w
		}
	}
	return overrides
}

func init() {
	rootCmd.AddCommand(newCreatePoolCmd())
}

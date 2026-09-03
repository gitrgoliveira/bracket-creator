package helper

import (
	"encoding/csv"
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// cellCoord holds an Excel workbook cell address; used only during workbook
// generation and never serialised or stored on domain types.
type cellCoord struct {
	sheetName string
	cell      string
}

// playerCellCoord extends cellCoord with an optional player-number cell.
type playerCellCoord struct {
	cellCoord
	numberCell string // non-empty only when the player has a Number field
}

type Pool struct {
	PoolName string   `json:"poolName"`
	Players  []Player `json:"players"`
	Matches  []Match  `json:"matches,omitempty"`
}

// Player is a type alias for domain.Player. The helper package used to
// own a parallel struct during the NFR-007 migration; it was collapsed
// to an alias once the two were proven field-identical (the converters
// were copying fields 1:1 with no translation). The helper name is kept
// for rendering-side ergonomics inside this package.
type Player = domain.Player

// MatchWinner records the Excel cell that contains a pool or elimination match
// winner's name; used to build cross-sheet formula references in bracket trees.
type MatchWinner struct {
	cellCoord
}

type Match struct {
	SideA *Player `json:"sideA"`
	SideB *Player `json:"sideB"`
	Round int     `json:"round"`
}

func CreatePlayers(entries []string, withZekkenName bool) ([]Player, error) {
	records := make([][]string, 0, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, `"`) {
			records = append(records, strings.Split(entry, ","))
			continue
		}
		r := csv.NewReader(strings.NewReader(entry))
		r.LazyQuotes = true
		r.TrimLeadingSpace = true
		fields, err := r.Read()
		if err != nil {
			records = append(records, strings.Split(entry, ","))
			continue
		}
		records = append(records, fields)
	}

	// Blank-name rejection lives here, not in CreatePlayersFromRecords: this
	// is the entry point for the CLI, the legacy web UI's parse endpoint and
	// archive import, all of which take raw string entries. The roster
	// loader in internal/state calls CreatePlayersFromRecords directly and
	// stays tolerant of a blank name on purpose, so a hand-edited
	// participants.csv can still be loaded and repaired.
	var missing []string
	for i, rec := range records {
		allBlank := true
		for _, f := range rec {
			if strings.TrimSpace(f) != "" {
				allBlank = false
				break
			}
		}
		if allBlank {
			continue
		}
		if len(rec) == 0 || strings.TrimSpace(rec[0]) == "" {
			// Quote the row: the CLI shuffles entries before this runs, so the
			// entry number alone does not identify a line in the file.
			missing = append(missing, fmt.Sprintf("entry %d: missing name in %q", i+1, strings.Join(rec, ",")))
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("participant validation failed:\n%s", strings.Join(missing, "\n"))
	}

	return CreatePlayersFromRecords(records, withZekkenName)
}

// CreatePlayersFromRecords builds players from pre-parsed CSV records
// (each record is a slice of fields). Use this when the CSV has already
// been parsed by encoding/csv so that quoted commas are handled correctly.
func CreatePlayersFromRecords(records [][]string, withZekkenName bool) ([]Player, error) {
	players := make([]Player, 0, len(records))
	var errors []string
	seenNames := make(map[string]int)
	c := cases.Title(language.Und, cases.NoLower)

	for i, line := range records {
		allEmpty := true
		for _, f := range line {
			if strings.TrimSpace(f) != "" {
				allEmpty = false
				break
			}
		}
		if allEmpty {
			continue
		}
		for j := range line {
			line[j] = strings.TrimSpace(line[j])
		}

		player := Player{
			PoolPosition: int64(len(players)),
		}

		if withZekkenName {
			if len(line) < 2 {
				errors = append(errors, fmt.Sprintf("entry %d: invalid format: expected 'Name, Dojo' or 'Name, DisplayName, Dojo'", i+1))
				continue
			}
			player.Name = c.String(line[0])
			if len(line) == 2 {
				player.DisplayName = SanitizeName(line[0])
				player.Dojo = line[1]
				if player.Dojo == "" {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
			} else {
				if line[2] == "" {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
				player.DisplayName = line[1]
				if player.DisplayName == "" {
					player.DisplayName = SanitizeName(line[0])
				}
				player.Dojo = line[2]
				if len(line) > 3 {
					meta := line[3:]
					// Canonicalize first so the legacy "reserved" alias is detected as a
					// source (→ "manual") instead of being left in Metadata.
					if len(meta) > 0 {
						if src := CanonicalRegistrationSource(meta[len(meta)-1]); IsRegistrationSource(src) {
							player.Source = src
							meta = meta[:len(meta)-1]
						}
					}
					if len(meta) > 0 {
						player.Metadata = meta
					}
				}
			}
		} else {
			player.Name = c.String(line[0])
			player.DisplayName = SanitizeName(line[0])
			player.Dojo = "NA"
			if len(line) >= 2 {
				player.Dojo = line[1]
			}
			if len(line) > 2 {
				meta := line[2:]
				// Canonicalize first so the legacy "reserved" alias is detected as a
				// source (→ "manual") instead of being left in Metadata.
				if len(meta) > 0 {
					if src := CanonicalRegistrationSource(meta[len(meta)-1]); IsRegistrationSource(src) {
						player.Source = src
						meta = meta[:len(meta)-1]
					}
				}
				if len(meta) > 0 {
					player.Metadata = meta
				}
			}
		}
		key := fmt.Sprintf("%s|%s|%s", player.Name, player.DisplayName, player.Dojo)
		if lineNo, seen := seenNames[key]; seen {
			errors = append(errors, fmt.Sprintf("entry %d: duplicate participant '%s' from '%s' (display name: '%s', originally at entry %d)", i+1, player.Name, player.Dojo, player.DisplayName, lineNo))
			continue
		}
		seenNames[key] = i + 1
		players = append(players, player)
	}

	if len(errors) > 0 {
		return nil, fmt.Errorf("participant validation failed:\n%s", strings.Join(errors, "\n"))
	}

	return players, nil
}

// IsRegistrationSource reports whether s is a recognised participant
// registration source (case-insensitive): manual / registered / transfer.
// Exported so the API boundary validator can reject unknown values before they
// are persisted, the CSV loader only recognises these tokens, so an unexpected
// value would otherwise shift into Metadata on reload.
func IsRegistrationSource(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "manual", "registered", "transfer":
		return true
	}
	return false
}

// CanonicalRegistrationSource returns the canonical stored form of a
// registration source: trimmed + lower-case. Keeps filter buckets from
// splitting on whitespace/casing ("Manual" vs "manual"). The legacy "reserved"
// token (an unused value in the old participant-tag enum) is aliased to
// "manual" so any hand-edited/older CSV row reloads as a recognised source
// instead of silently shifting into Metadata.
func CanonicalRegistrationSource(s string) string {
	c := strings.ToLower(strings.TrimSpace(s))
	if c == "reserved" {
		return "manual"
	}
	return c
}

// TitleCaseName applies the same Unicode Title-casing that CreatePlayers uses
// so names stored to participants.csv (and seeds.csv) match what is read back
// on the next load, avoiding seed-merge mismatches. TrimSpace is applied first
// to match CreatePlayers' per-column trim before title-casing.
func TitleCaseName(name string) string {
	return cases.Title(language.Und, cases.NoLower).String(strings.TrimSpace(name))
}

// SanitizeName returns the canonical display form derived from a participant
// name: a single token uppercased ("KAZUKI") or "F. LAST" for multi-token
// names. Exported so state.SaveParticipants can detect display names that
// match the auto-derived form and avoid round-trip data corruption (a 3-column
// row whose DisplayName equals SanitizeName(Name) carries no extra information
// and must not be written for non-zekken competitions, see
// internal/state/participants.go).
func SanitizeName(name string) string {
	//removing extra spaces
	name = strings.TrimSpace(name)

	// return only first and last name
	fullName := strings.Split(name, " ")

	if len(fullName) == 1 {
		return strings.ToUpper(fullName[0])
	}

	// First Name all caps
	firstName := strings.ToUpper(fullName[0])

	// Last Name all caps
	lastName := strings.ToUpper(fullName[len(fullName)-1])

	return fmt.Sprintf("%c. %s", firstName[0], lastName)
}

// PoolCount reports how many pools CreatePools will build for numPlayers
// participants at poolSize, without building them.
//
// This is the ONE definition of the pool count. CreatePools calls it, and so
// must every caller that needs the count before the pools exist, most notably
// PoolSeeding, whose second argument is the pool COUNT and not the pool SIZE.
// Keeping a second copy of this arithmetic is exactly how the engine came to
// hand PoolSeeding a pool size (bc-draw Phase 2a).
//
// isMax mirrors CreatePools' parameter: true means poolSize is the MAXIMUM
// players per pool (ceiling division, PoolSizeMode "max"), false means it is
// the minimum/target size (floor division). The result is 0 when no pool can
// be formed at all (non-positive poolSize, no players, or fewer players than
// poolSize in min mode); CreatePools turns that into an error.
func PoolCount(numPlayers, poolSize int, isMax bool) int {
	if poolSize <= 0 || numPlayers <= 0 {
		return 0
	}
	if isMax {
		return (numPlayers + poolSize - 1) / poolSize
	}
	return numPlayers / poolSize
}

// BuildPoolPhase is the whole pool-phase construction, in the one order the
// steps are valid in, returning the pools and the shiaijo count they were laid
// out against.
//
// It exists because the sequence is ORDERED and its steps share a derived
// modulus, and it used to be written out twice -- once in the CLI
// (cmd/create-pools.go) and once in the app engine (internal/engine/pools.go).
// Both copies have drifted before, each time silently and each time in a way
// that misplaced real competitors:
//
//   - the engine handed PoolSeeding the pool SIZE where the pool COUNT is
//     expected, so seeds landed in the wrong pools whenever the two differ;
//   - the engine never called ReorderPoolsForCourts at all, which PoolSeeding's
//     placement maths assumes has run, so every oversized pool piled onto the
//     first shiaijo;
//   - both fed the RAW shiaijo allocation to the seed spread and the
//     deinterleave while the draw ran on the clamped one.
//
// The four constraints the order encodes, which is what a caller assembling
// this by hand has to get right:
//
//  1. numPools comes from PoolCount, the same function CreatePools sizes its
//     own pool slice with, so the count fed to PoolSeeding cannot drift from
//     the pools that actually appear.
//  2. drawCourts is EffectiveDrawCourts, not the requested allocation: a
//     shiaijo with no home pool would own an empty bracket region, so the draw
//     steps the count down to what the pools can carry. It is the modulus for
//     the seed spread, the deinterleave AND the caller's pool-to-shiaijo
//     allocation, and all three must agree.
//  3. PoolSeeding runs BEFORE CreatePools: it reorders the roster so that
//     CreatePools' straight fill lands seeds and dojo-mates where they belong.
//     It runs whether or not anyone is seeded, because it also clusters by dojo.
//  4. ReorderPoolsForCourts runs AFTER CreatePools and before anything reads
//     pool order or pool names.
//
// Callers still own what happens either side: validating the roster before, and
// naming, numbering, persisting and allocating pools to shiaijo after. Use the
// returned court count for that allocation rather than re-deriving it.
//
// Until bc-dojo Phase 4 this function's own body ran
// PoolSeeding -> CreatePools -> ReorderPoolsForCourts: a fill that commits a
// placement before it can see what still has to be placed, then a
// post-fill dojo-swap repair pass (since deleted) that swapped unseeded
// competitors afterwards to break up what the fill could not avoid. It now
// DELEGATES to the region-aware distributor (BuildPoolPhaseTreeAware,
// pool_distribution_tree_aware.go), which enforces the same four
// constraints above -- the same numPools/drawCourts derivation, seeds
// placed before the unseeded, ReorderPoolsForCourts last -- inside a
// forward pass (assignUnseededByDojoTree) that can see the whole knockout
// tree before it places anyone, followed by a narrow pairwise-exchange
// pass (improveDojoMeetings) that closes the one residual the forward pass
// alone cannot see. That exchange pass is a different animal from the
// deleted post-fill repair: it is scored on the winner-path metric alone,
// touches only unseeded-for-unseeded swaps, and is a no-op on the
// unique-dojo and single-dojo cases the old repair also left untouched
// (see BuildPoolPhaseTreeAware's own doc comment for the full pipeline).
//
// poolWinners is fixed at defaultPoolWinners (2, the documented
// EffectivePoolWinners()/ResolveQualifiedPools default) and the mode at
// standard: this function's own signature has no poolWinners or
// extra-qualifiers parameter and never has, so every existing caller/test
// that has no real qualifier count to hand keeps exactly the shape it
// always got. A caller that DOES know its competition's real pool-winners
// count and extra-qualifiers mode -- internal/engine/pools.go,
// cmd/create-pools.go -- must call BuildPoolPhaseTreeAwareWithMode instead,
// or the distributor scores candidate placements against the WRONG knockout tree
// whenever the real values differ from the default.
func BuildPoolPhase(players []Player, poolSize int, isMax bool, numCourts int) ([]Pool, int, error) {
	return BuildPoolPhaseTreeAware(players, poolSize, isMax, numCourts, defaultPoolWinners)
}

// BuildPoolPhaseFillBracket is BuildPoolPhase's fill-bracket counterpart
// (bc-qual LP-4), beside the existing min path rather than a mutation of
// it: the pool COUNT comes from FillBracketPoolCount's formation objective
// instead of PoolCount's floor(n/minSize) -- the two can and do diverge (45
// entrants at minimum pool size 3 forms 14 pools here, not floor(45/3)=15;
// see FillBracketPoolCount's doc comment) -- and pool CUTTING goes through
// CreatePoolsForCount (an explicit pool count, min-size targets) instead of
// CreatePools. Every other step mirrors BuildPoolPhase's, in the same order
// and for the same reasons (see its doc comment): PoolSeeding runs first so
// seeds land in the pools they belong in, ReorderPoolsForCourts runs after
// so oversized pools spread across shiaijo instead of clustering on the
// first (bc-draw Phase 2a).
//
// numCourts is the RAW requested shiaijo allocation; the returned int is
// EffectiveDrawCourts(pools, numCourts), exactly as BuildPoolPhase's is --
// use it, not the input, for anything downstream that bands by court.
//
// minSize is always the MINIMUM pool size: fill-bracket has no "max
// players per pool" mode (state.ValidateExtraQualifiers gates it to
// minimum-players-per-pool sizing), so unlike BuildPoolPhase there is no
// isMax parameter here at all.
//
// Until bc-dojo Phase 4 this function's own body ran
// PoolSeeding -> CreatePoolsForCount -> ReorderPoolsForCourts, the same
// fill-then-repair shape BuildPoolPhase's pre-Phase-4 body had. It now
// delegates to BuildPoolPhaseFillBracketTreeAware
// (pool_distribution_tree_aware.go): the same region-aware one-pass
// distributor BuildPoolPhase itself now uses, cutting via this function's
// own FillBracketPoolCount formation objective and CreatePoolsForCount's
// min-size-plus-outer-to-inner-remainder target sizes (realTargetSizes,
// reused rather than re-derived), and scoring every unseeded placement
// against the tree fill-bracket mode actually builds
// (BuildKnockoutDrawFillBracket) rather than the standard uniform one.
func BuildPoolPhaseFillBracket(players []Player, minSize int, numCourts int) ([]Pool, int, error) {
	return BuildPoolPhaseFillBracketTreeAware(players, minSize, numCourts)
}

// poolTargetSizes is CreatePools' pool-COUNT-and-SIZE arithmetic, extracted
// so a second caller can derive the exact same shape CreatePools would
// without re-deriving -- or drifting from -- it (bc-dojo Phase 2:
// BuildPoolPhaseTreeAware needs the shape before it can place anyone, and
// "reuse, do not copy" is the constraint this exists to satisfy).
// CreatePools itself now calls this rather than carrying its own copy of the
// arithmetic; the error text and the max-mode remainder spread are both
// unchanged.
//
// For "max" sizing the returned targetSizes already sum to numPlayers
// exactly (the base row IS the final shape). For "min" (non-max) sizing they
// do NOT: every pool gets the uniform poolSize row here, and whatever is
// left over (numPlayers - totalPools*poolSize, always < poolSize) is spread
// later, dynamically, by assignPlayersToPools' own forcePoolSize fallback --
// interleaved with player placement in the real pipeline, so it is not
// reflected in this function's return at all. A caller that needs the FINAL
// min-mode sizes before any player is placed (bc-dojo Phase 4:
// BuildPoolPhaseTreeAware's one-pass distribution enforces target size as a
// hard per-pool cap, so it cannot discover the remainder the way the old
// fill-then-repair pipeline does) must run this through realTargetSizes.
func poolTargetSizes(numPlayers, poolSize int, isMax bool) (totalPools int, targetSizes []int, err error) {
	// Guard before the division below: poolSize is the divisor in both the
	// "max" and fixed-size branches, so a zero/negative value panics with an
	// integer divide-by-zero. Reject it here, the lowest shared point, so
	// every caller (engine draw, schedule estimator, CLI) is panic-proof
	// regardless of how PoolSize reached it. (mp-ebgz)
	if poolSize <= 0 {
		return 0, nil, fmt.Errorf("cannot create pools: pool size must be at least 1, got %d", poolSize)
	}
	// The upper guard, for the same reason at the other end (see MaxPoolSize).
	// Both modes keep every target size at or under poolSize, and min mode's
	// remainder can then add one seat to a pool (realTargetSizes), so refusing
	// poolSize at MaxPoolSize is what keeps a DRAWN pool inside it.
	if poolSize >= MaxPoolSize {
		return 0, nil, fmt.Errorf("cannot create pools: pool size must be less than %d, got %d", MaxPoolSize, poolSize)
	}
	totalPools = PoolCount(numPlayers, poolSize, isMax)

	if totalPools == 0 && numPlayers > 0 {
		return 0, nil, fmt.Errorf("cannot create pools: player count (%d) is less than pool size (%d)", numPlayers, poolSize)
	}

	targetSizes = make([]int, totalPools)
	if isMax && totalPools > 0 {
		base := numPlayers / totalPools
		rem := numPlayers % totalPools
		for i := 0; i < totalPools; i++ {
			if i < rem {
				targetSizes[i] = base + 1
			} else {
				targetSizes[i] = base
			}
		}
	} else {
		for i := 0; i < totalPools; i++ {
			targetSizes[i] = poolSize
		}
	}
	return totalPools, targetSizes, nil
}

// realTargetSizes closes poolTargetSizes' own documented gap for min-mode
// sizing (bc-dojo Phase 4, found while wiring BuildPoolPhaseTreeAware into
// production: every gate/invariant sweep before this happened to build a
// roster whose size was an exact multiple of poolSize, so the gap never
// fired -- BuildPoolPhaseTreeAware(17 players, poolSize=4, isMax=false, ...)
// returned "cannot place player: no pool has room" instead of the 5/4/4/4
// split the old pipeline produces, because its one-pass placement enforces
// poolTargetSizes' base row as a hard per-pool cap with no repair pass
// behind it to catch the shortfall).
//
// base is poolTargetSizes' own return (or an equivalent uniform-minSize row,
// for a caller like the fill-bracket phase that derives its pool count some
// other way); numPlayers is the real roster size. When sum(base) already
// equals numPlayers (max-mode, or a min-mode roster with no remainder) this
// is a no-op.
//
// Otherwise the shortfall (always < len(base) for every caller in this
// package: CreatePoolsForCount's own precondition bounds it directly, and
// poolTargetSizes' uniform row bounds it to < poolSize, which callers here
// only ever combine with a pool count derived so poolSize <= numPools) is
// spread by SIMULATING assignPlayersToPools' own forcePoolSize fallback over
// pool COUNTS -- forcePoolSizeFromCounts, the very walk CreatePools/
// CreatePoolsForCount reach through forcePoolSize, rather than re-deriving its
// outer-to-inner order a second time. This is safe to precompute, before any
// real player exists, because that walk never reads a player's identity,
// only pool LENGTHS against targetSizes, and every pool is providably at
// its base size before assignPlayersToPools' normal fill (discoverPool /
// leastConflictedPool) ever lets forcePoolSize fire at all: those two only
// return -1, handing off to forcePoolSize, once EVERY pool has already
// reached its own target -- so the remainder's landing pools depend only on
// the shortfall COUNT, never on which players filled the base rows first.
func realTargetSizes(base []int, numPlayers int) []int {
	sum := 0
	for _, s := range base {
		sum += s
	}
	remainder := numPlayers - sum
	if remainder <= 0 {
		return base
	}

	counts := make([]int, len(base))
	copy(counts, base)
	for r := 0; r < remainder; r++ {
		counts[forcePoolSizeFromCounts(counts, base)]++
	}
	return counts
}

func CreatePools(players []Player, poolSize int, isMax bool) ([]Pool, error) {
	_, targetSizes, err := poolTargetSizes(len(players), poolSize, isMax)
	if err != nil {
		return nil, err
	}
	return assignPlayersToPools(players, targetSizes), nil
}

// poolPositionName is the "Pool A".."Pool Z", then "Pool AA", "Pool BB", ...
// naming assignPlayersToPools gives the pool at position i (0-based),
// exposed as its own function so a caller that needs to name a pool by
// position WITHOUT going through assignPlayersToPools -- Phase 1's
// poolQualifierPaths seam builds placeholder pools before any player
// exists -- uses the identical scheme rather than a second copy that could
// drift from it. assignPlayersToPools is under a hard "do not modify"
// constraint for bc-dojo, so its own naming loop is left as it is rather
// than rewritten to call this; the two are pinned equal by test.
func poolPositionName(i int) string {
	char := string(rune('A' + i%26))
	if i > 25 {
		char = char + char
	}
	return fmt.Sprintf("Pool %s", char)
}

// CreatePoolsForCount is CreatePools with the pool COUNT supplied directly
// instead of derived from poolSize+isMax via PoolCount. It always sizes
// pools the MIN-MODE way: every pool's target is poolSize, and the
// len(players) - poolSize*totalPools remainder players force one extra into
// `totalPools`'s outer-to-inner pools exactly as CreatePools' own min-mode
// branch does (assignPlayersToPools' forcePoolSize fallback) -- the two
// share that one code path, so a caller of either gets the identical
// remainder-spread behaviour.
//
// It exists for the "fill-bracket" qualifier formation (bc-qual LP-4,
// FillBracketPoolCount), whose pool count is deliberately NOT
// floor(n/poolSize): FillBracketPoolCount can (and for 45 entrants at
// minimum pool size 3 does) choose FEWER, partly-oversized pools than the
// naive division, so the oversized remainder can supply drafted 2nd-place
// qualifiers that exactly fill a power-of-two knockout bracket (see
// BuildKnockoutDrawFillBracket). This function does the CUTTING half of
// that; it has no opinion on how totalPools was chosen.
//
// Preconditions, enforced below rather than trusted: poolSize >= 1,
// totalPools >= 1, and poolSize*totalPools <= len(players) <=
// (poolSize+1)*totalPools -- every pool must reach the minimum and no pool
// would need to grow by more than one over it. FillBracketPoolCount's own
// search only ever proposes a totalPools satisfying this, but a caller
// reaching this function some other way gets a clean error rather than a
// silently short-filled pool or a forcePoolSize fallback with nowhere
// correct to put the overflow.
func CreatePoolsForCount(players []Player, poolSize, totalPools int) ([]Pool, error) {
	if poolSize <= 0 {
		return nil, fmt.Errorf("cannot create pools: pool size must be at least 1, got %d", poolSize)
	}
	if totalPools <= 0 {
		return nil, fmt.Errorf("cannot create pools: pool count must be at least 1, got %d", totalPools)
	}
	if len(players) < poolSize*totalPools {
		return nil, fmt.Errorf("cannot create %d pool(s) of minimum size %d: only %d player(s) available (need at least %d)", totalPools, poolSize, len(players), poolSize*totalPools)
	}
	if len(players) > (poolSize+1)*totalPools {
		return nil, fmt.Errorf("cannot create %d pool(s) of minimum size %d: %d player(s) would need at least one pool larger than %d+1", totalPools, poolSize, len(players), poolSize)
	}

	targetSizes := make([]int, totalPools)
	for i := range targetSizes {
		targetSizes[i] = poolSize
	}
	return assignPlayersToPools(players, targetSizes), nil
}

// assignPlayersToPools is CreatePools' and CreatePoolsForCount's shared
// assignment body: given the target size for each of len(targetSizes)
// pools, distribute players into them avoiding dojo/name conflicts where
// possible, falling back to leastConflictedPool then forcePoolSize when a
// conflict-free placement does not exist, and name the pools alphabetically
// in the order they end up in. Extracted so the two callers cannot drift on
// how a remainder is spread; only what pool COUNT and target sizes they hand
// in differs.
//
// This used to end with a dojo-rebalancing repair pass (since deleted):
// swapping unseeded competitors afterwards to break up same-dojo pairings
// the greedy fill could not avoid at the time it placed them. The
// region-aware rebuild (bc-dojo Phase 4)
// subsumed it: BuildPoolPhase and BuildPoolPhaseFillBracket no longer route
// through this function at all (both now delegate to the tree-aware
// distributor, whose single forward pass gets dojo placement right without
// needing a second pass to fix it), and this function's only remaining
// production caller (the schedule estimator's synthetic roster,
// internal/helper/estimate.go) and its own direct test callers all use
// UNIQUE-dojo rosters, where the repair pass was already provably a no-op
// (nothing to rebalance). Removing it changes none of their output.
func assignPlayersToPools(players []Player, targetSizes []int) []Pool {
	totalPools := len(targetSizes)
	pools := make([]Pool, totalPools)

	// Per-pool sets for O(1) dojo-conflict detection.
	dojoSets := make([]map[string]bool, totalPools)
	for i := range dojoSets {
		dojoSets[i] = make(map[string]bool)
	}

	for i, player := range players {
		poolN := discoverPool(pools, dojoSets, player, targetSizes, i%totalPools)
		// no conflict-free pool available: pick the least-conflicted one
		if poolN < 0 {
			poolN = leastConflictedPool(pools, targetSizes, player.Dojo)
		}

		// try and force pool size
		if poolN < 0 {
			poolN = forcePoolSize(pools, targetSizes)
		}
		player.PoolPosition = int64(len(pools[poolN].Players) + 1)
		pools[poolN].Players = append(pools[poolN].Players, player)
		dojoSets[poolN][player.Dojo] = true
	}

	for i := 0; i < len(pools); i++ {
		char := string(rune('A' + i%26))
		if i > 25 {
			char = char + char
		}
		pools[i].PoolName = fmt.Sprintf("Pool %s", char)
	}

	return pools
}

// discoverPool returns the first pool, scanning from startIndex, that has room
// and holds nobody from the player's dojo, or -1 when no such pool exists (the
// caller then falls back to leastConflictedPool).
//
// Sharing a NAME is deliberately not a conflict. Two competitors can only share
// a name when their dojos differ, because a second entry with the same name AND
// dojo is refused as a duplicate of the same person, so namesakes are distinct
// people who may fight in one pool; they are told apart on the sheet by dojo and
// by competitor number. This function used to reject a same-name pool as well,
// which was measured to change nothing: across 414 pool shapes built from
// rosters of same-name pairs, dropping the name test moved no competitor,
// because PoolSeeding's dojo clustering and the rotating start already separate
// them. It is dropped rather than kept as a no-op so the draw has one stated
// separation rule instead of a second, silent one.
func discoverPool(pools []Pool, dojoSets []map[string]bool, player Player, targetSizes []int, startIndex int) int {
	totalPools := len(pools)
	if totalPools == 0 {
		return -1
	}

	for i := 0; i < totalPools; i++ {
		curr := (startIndex + i) % totalPools

		// making sure there's space first
		if len(pools[curr].Players) >= targetSizes[curr] {
			continue
		}

		// O(1): reject if this dojo is already present in the pool
		if dojoSets[curr][player.Dojo] {
			continue
		}

		return curr
	}

	// If no suitable pool is found, return -1
	return -1
}

// countDojoInPool returns how many of pool's competitors are from dojo. Shared
// by the fallback that chooses where an unplaceable competitor goes and by the
// repair pass that swaps competitors afterwards, so the two can never disagree
// about what "how many of this dojo are here" means -- if dojo comparison ever
// needs normalizing, this is the one place it changes.
func countDojoInPool(pool Pool, dojo string) int {
	n := 0
	for _, pl := range pool.Players {
		if pl.Dojo == dojo {
			n++
		}
	}
	return n
}

// leastConflictedPool is assignPlayersToPools' first fallback, reached when
// discoverPool finds no conflict-free pool for a player (a dojo conflict OR
// a name conflict against every pool with room). Among the pools that still
// have room, it picks the one holding the FEWEST players already sharing the
// incoming player's dojo, tie-broken by fewest players overall, then by
// lowest index. The strict "<" comparisons (rather than "<=") keep the
// lowest-index pool on a tie, which is what makes the output deterministic.
//
// It is deliberately name-conflict-blind: it only ranks by dojo count, so a
// name collision alone does not influence which pool is chosen. Returns -1
// if no pool has room.
func leastConflictedPool(pools []Pool, targetSizes []int, dojo string) int {
	best := -1
	bestDojo, bestSize := 0, 0
	for i, pool := range pools {
		if len(pool.Players) >= targetSizes[i] {
			continue
		}
		n := countDojoInPool(pool, dojo)
		if best < 0 || n < bestDojo || (n == bestDojo && len(pool.Players) < bestSize) {
			best, bestDojo, bestSize = i, n, len(pool.Players)
		}
	}
	return best
}

// forcePoolSize picks the pool an overflow player lands in, walking the pools
// outer to inner and taking the first with room for one over its target. It
// reads pool LENGTHS and nothing else, which is what lets realTargetSizes
// precompute the same landing order before any player exists.
func forcePoolSize(pools []Pool, targetSizes []int) int {
	counts := make([]int, len(pools))
	for i := range pools {
		counts[i] = len(pools[i].Players)
	}
	return forcePoolSizeFromCounts(counts, targetSizes)
}

// forcePoolSizeFromCounts is that rule expressed over the pool sizes alone, so
// a caller holding only counts does not have to fabricate players to ask it.
// realTargetSizes used to do exactly that: it built a placeholder Pool per pool
// and filled it with one zero-value Player per seat, purely so it could call
// forcePoolSize and then read the lengths straight back out. That allocation
// scaled with the entry count for no reason, and read to CodeQL as an
// allocation sized by request-derived input (go/uncontrolled-allocation-size).
// One walk, two entry points, so the two cannot drift.
func forcePoolSizeFromCounts(counts, targetSizes []int) int {
	for i, j := 0, len(counts)-1; i <= j; i, j = i+1, j-1 {
		if counts[i] < targetSizes[i]+1 {
			return i
		}
		if i != j && counts[j] < targetSizes[j]+1 {
			return j
		}
	}
	return 0
}

func CreatePoolMatches(pools []Pool) {
	for i := range pools {
		pool := &pools[i]
		players := pool.Players

		// Special case: pool of 2 only needs 1 match
		if len(players) == 2 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[0],
				SideB: &players[1],
			})
			continue
		}

		switch len(players) {
		case 0:
			continue
		case 1:
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[0],
				SideB: &players[0],
			})
			continue
		case 3:
			pool.Matches = append(pool.Matches,
				Match{SideA: &players[0], SideB: &players[1]},
				Match{SideA: &players[0], SideB: &players[2]},
				Match{SideA: &players[1], SideB: &players[2]},
			)
			continue
		case 4:
			pool.Matches = append(pool.Matches,
				Match{SideA: &players[0], SideB: &players[1]},
				Match{SideA: &players[2], SideB: &players[1]},
				Match{SideA: &players[2], SideB: &players[3]},
				Match{SideA: &players[0], SideB: &players[3]},
			)
			continue
		}

		for i := 0; i+1 < len(players); i += 2 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[i],
				SideB: &players[i+1],
			})
			next := (i + 2) % len(players)
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[next],
				SideB: &players[i+1],
			})
		}
		if len(players)%2 != 0 {
			pool.Matches = append(pool.Matches, Match{
				SideA: &players[len(players)-1],
				SideB: &players[0],
			})
		}

	}
}

// playerIndex returns the position of p in players by pointer identity, or -1.
func playerIndex(players []Player, p *Player) int {
	for i := range players {
		if &players[i] == p {
			return i
		}
	}
	return -1
}

// buildRoundLookup converts a CircleMethodRounds (or PathGraphRounds) result
// into a map from normalised IntPair (A < B) to round index.
func buildRoundLookup(rounds [][]IntPair) map[IntPair]int {
	lookup := make(map[IntPair]int)
	for r, pairs := range rounds {
		for _, p := range pairs {
			a, b := p.A, p.B
			if a > b {
				a, b = b, a
			}
			lookup[IntPair{A: a, B: b}] = r
		}
	}
	return lookup
}

func CreatePoolRoundRobinMatches(pools []Pool) {

	for poolN, pool := range pools {
		currentPool := &pools[poolN]
		size := len(pool.Players)

		switch size {
		case 0, 1:
			continue
		case 3:
			currentPool.Matches = append(currentPool.Matches,
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[2]},
				Match{SideA: &currentPool.Players[1], SideB: &currentPool.Players[2]},
			)
		case 4:
			currentPool.Matches = append(currentPool.Matches,
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[2], SideB: &currentPool.Players[1]},
				Match{SideA: &currentPool.Players[2], SideB: &currentPool.Players[3]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[3]},
				Match{SideA: &currentPool.Players[0], SideB: &currentPool.Players[2]},
				Match{SideA: &currentPool.Players[1], SideB: &currentPool.Players[3]},
			)
		default:
			for i := 1; i < size; i++ {
				for k, j := i, 0; j < size-i; j, k = j+1, k+1 {
					sideA := &currentPool.Players[j]
					sideB := &currentPool.Players[k]

					if len(currentPool.Matches) > 0 {
						prev := currentPool.Matches[len(currentPool.Matches)-1]
						prevSide := func(match Match, player *Player) int {
							if match.SideA == player {
								return 1
							}
							if match.SideB == player {
								return 2
							}
							return 0
						}

						sideAStatus := prevSide(prev, sideA)
						sideBStatus := prevSide(prev, sideB)
						if sideAStatus == 2 || sideBStatus == 1 {
							sideA, sideB = sideB, sideA
						}
					}

					currentPool.Matches = append(currentPool.Matches, Match{
						SideA: sideA,
						SideB: sideB,
					})
				}
			}
		}

		// Assign Round indices using the circle-method schedule.
		roundLookup := buildRoundLookup(CircleMethodRounds(size))
		for mi := range currentPool.Matches {
			m := &currentPool.Matches[mi]
			idxA := playerIndex(currentPool.Players, m.SideA)
			idxB := playerIndex(currentPool.Players, m.SideB)
			a, b := idxA, idxB
			if a > b {
				a, b = b, a
			}
			if r, ok := roundLookup[IntPair{A: a, B: b}]; ok {
				m.Round = r
			}
		}
	}

}

// playerCoordKey returns the lookup key for a player in a coord map.
// It mirrors the composite uniqueness key enforced by CreatePlayers, so two
// players with the same name but different dojos get distinct entries.
func playerCoordKey(p Player) string {
	return p.Name + "|" + p.DisplayName + "|" + p.Dojo
}

// ConvertPlayersToWinners maps a competitor's DISPLAYED name to the data-sheet
// cell the tree formula concatenates.
//
// Known and deliberate: the returned map is keyed by name, so two competitors
// who share a name (legal when their dojos differ) collapse to one entry. That
// is safe HERE, and only here, because the cell being referenced holds the
// player's NAME (column B, see dataColumnLayout.writePlayer) -- both namesakes
// resolve to a cell containing the same string, so the rendered bracket is
// identical either way. The lookup key would have to become an identity, and
// the tree would have to carry that identity instead of a bare name, for a
// difference nobody can see. If this map is ever pointed at a cell that
// differs BETWEEN two namesakes -- the competitor number is the obvious
// candidate, numberCell is already on playerCellCoord -- that reasoning
// expires and the key must become helper.CompetitorKey.
func ConvertPlayersToWinners(players []Player, sanitized bool, pCoords map[string]playerCellCoord) map[string]MatchWinner {
	matchWinners := make(map[string]MatchWinner, len(players))
	for _, player := range players {
		coord, ok := pCoords[playerCoordKey(player)]
		if !ok {
			continue
		}
		key := player.Name
		if sanitized && player.DisplayName != "" {
			key = player.DisplayName
		}
		matchWinners[key] = MatchWinner{cellCoord: coord.cellCoord}
	}
	return matchWinners
}

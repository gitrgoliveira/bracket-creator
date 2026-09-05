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

// CreatePlayers is the CLI/web-facing entry point: create-pools,
// create-playoffs, the /api/parse-participants preview and the mobile app's
// tournament-import path all build a NEW roster from raw pasted/uploaded
// text through this function, so it enforces the dojo requirement
// (CreatePlayersFromRecords' requireDojo=true) -- see that parameter's own
// doc comment for why this is NOT the same as state.LoadParticipants'
// tolerant read of an EXISTING roster from disk.
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
	return CreatePlayersFromRecords(records, withZekkenName, true)
}

// CreatePlayersFromRecords builds players from pre-parsed CSV records
// (each record is a slice of fields). Use this when the CSV has already
// been parsed by encoding/csv so that quoted commas are handled correctly.
//
// requireDojo (bc-drwx item 10) controls whether the NON-ZEKKEN branch
// below rejects a missing/blank dojo column (true, "entry N: missing
// dojo", matching the zekken branch's own long-standing behaviour a few
// lines down -- that branch is UNCONDITIONAL and always rejects,
// regardless of this parameter) or leaves it blank (false: a tolerant
// read, so the row still loads and can be repaired -- see below).
//
//   - CreatePlayers (this file) -- the CLI/web-preview/import entry point,
//     building a roster fresh from raw text -- always passes true: docs/
//     user-guide/organisers/input-format.md promises "a row with no dojo
//     is rejected" and "importing a saved tournament is refused the same
//     way", and until this fix the non-zekken branch broke that promise.
//   - state.LoadParticipants (internal/state/participants.go) passes
//     false: it reads an EXISTING roster back off disk, and
//     state.ErrBlankDojo's own doc comment states the deliberate,
//     documented architecture this preserves -- "READ is deliberately NOT
//     gated. A roster written before this rule (or hand edited) still
//     loads, so an operator can see it and repair the dojo through the
//     edit UI." Passing true here would make LoadParticipants itself
//     refuse to load a legacy blank-dojo roster at all, breaking that
//     repair path and the two-floor design (state.ErrBlankDojo at WRITE,
//     helper.ErrBlankDojoInDraw at DRAW) documented in CLAUDE.md's Common
//     Pitfalls section -- confirmed by fault injection: setting this true
//     unconditionally reddened TestGenerateDraw_RefusesBlankDojoRoster
//     (which specifically depends on a blank-dojo roster still LOADING so
//     the draw-time refusal, not a load-time one, is what fires) and
//     TestCheckIn_BlankDojoElsewhereInRoster_Returns400.
func CreatePlayersFromRecords(records [][]string, withZekkenName bool, requireDojo bool) ([]Player, error) {
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
			// bc-drwx item 10, gated on requireDojo (see this function's own
			// doc comment for the full rationale and the two callers'
			// opposite needs): a missing or blank dojo is refused with the
			// SAME "entry N: missing dojo" error the zekken branch above
			// uses, matching docs/user-guide/organisers/input-format.md's
			// promise ("a row with no dojo is rejected") -- this branch used
			// to default a missing column to the literal string "NA"
			// unconditionally, which docs never described and which
			// silently defeated the dojo-based separation rules downstream
			// (a roster of several "NA"-dojo players reads as one giant
			// dojo to the pool/knockout distributor).
			//
			// The tolerant (requireDojo=false) path preserves this
			// function's pre-bc-drwx behaviour EXCEPT for one asymmetry a
			// later review round (bc-drwx item 7) found and closed: no dojo
			// COLUMN at all (len(line) < 2) used to default to the literal
			// "NA" while an EMPTY dojo COLUMN (line[1] == "") was left
			// BLANK -- so a legacy, one-column roster (no comma anywhere)
			// sailed straight past ValidateNoBlankDojo, since "NA" reads as
			// a real, non-blank dojo, and drew as one giant "NA" dojo
			// instead of being refused the way "Name," (an EXPLICIT blank)
			// already was. Both spellings of "no dojo here" now yield the
			// SAME blank string, so state.LoadParticipants still loads a
			// legacy/hand-edited roster tolerantly (visible and repairable
			// in the edit UI, per state.ErrBlankDojo's own doc comment), and
			// the draw pre-flight (ErrBlankDojoInDraw) catches both shapes
			// identically instead of only one of them.
			if len(line) < 2 {
				if requireDojo {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
			} else {
				player.Dojo = line[1]
				if requireDojo && player.Dojo == "" {
					errors = append(errors, fmt.Sprintf("entry %d: missing dojo", i+1))
					continue
				}
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
//  3. Seeds are placed BEFORE the unseeded: placeSeedIndices lands each
//     seed on its D6 half/quarter first, then assignUnseededByDojoTree
//     descends the knockout skeleton for every unseeded player so dojo-mates
//     land apart where the tree allows. This is a CORRECTION (bc-drwx item
//  11. of an older version of this constraint, which named PoolSeeding
//     and CreatePools -- the pre-bc-dojo-Phase-4 pipeline this function's
//     own body describes below as already replaced; PoolSeeding and its
//     private helpers no longer have any production caller at all.
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
// deleted post-fill repair: it is scored on a four-tier lexicographic
// objective led by a total spread-cap excess delta, then the winner-path
// metric, then an all-qualifier best-effort tie-break (see
// improveDojoMeetings' own doc comment for the full tier list), touches
// only unseeded-for-unseeded swaps, and is a no-op on the
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
// see FillBracketPoolCount's doc comment).
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
// Until bc-dojo Phase 4 this function's own body ran its own
// fill-then-repair pipeline (pool CUTTING through a since-removed
// CreatePoolsForCount, the fill-bracket counterpart of CreatePools). It
// now delegates to BuildPoolPhaseFillBracketTreeAware
// (pool_distribution_tree_aware.go): the same region-aware one-pass
// distributor BuildPoolPhase itself now uses, cutting via this function's
// own FillBracketPoolCount formation objective and a uniform min-size
// target-size row spread by realTargetSizes (bc-drwx item 11: the
// remainder-spread arithmetic that used to live inside CreatePoolsForCount
// is realTargetSizes' own now, not a second copy to stay in sync with),
// and scoring every unseeded placement against the tree fill-bracket mode
// actually builds (BuildKnockoutDrawFillBracket) rather than the standard
// uniform one.
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
// Otherwise the shortfall (< poolSize for a poolTargetSizes-derived uniform
// row) is spread by SIMULATING assignPlayersToPools' own forcePoolSize
// fallback over pool COUNTS -- forcePoolSizeFromCounts, the very walk
// CreatePools reaches through forcePoolSize, rather than re-deriving its
// outer-to-inner order a second time.
//
// CORRECTION (bc-drwx item 5): this doc used to claim the shortfall is
// "always < len(base)", reasoning that poolSize is never bigger than the
// pool count in practice. That is not a real invariant -- 14 entrants at
// minimum pool size 5 forms only 2 pools (poolSize=5 > totalPools=2), and
// the shortfall (14 - 2*5 = 4) then EXCEEDS len(base). forcePoolSizeFromCounts
// itself now handles any shortfall size (see its own doc comment for the
// fix); this function's precompute never assumed a bound on the loop count
// in the first place, only on what a single forcePoolSizeFromCounts call
// does, so no change was needed here beyond correcting the claim.
//
// This is safe to precompute, before any
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

// poolPositionName is the "Pool A".."Pool Z", then "Pool AA", "Pool AB", ...
// naming assignPlayersToPools gives the pool at position i (0-based),
// exposed as its own function so a caller that needs to name a pool by
// position WITHOUT going through assignPlayersToPools -- Phase 1's
// poolQualifierPaths seam builds placeholder pools before any player
// exists -- uses the identical scheme rather than a second copy that could
// drift from it. assignPlayersToPools' own naming loop now calls this
// function directly (bc-drwx item 6) rather than carrying its own copy of
// the arithmetic, so the two can no longer drift; they used to be pinned
// equal by test instead, which only proves two copies AGREE, never that
// either is correct.
//
// This is Excel-style BIJECTIVE base-26 (A, B, ... Z, AA, AB, ... AZ, BA,
// ...), not the doubled-letter scheme it used to be ('A'+i%26, with the
// letter simply DOUBLED once i passed 25): that scheme collides every 26
// pools past the first double-letter one -- i=26 and i=52 both reduce to
// i%26==0 and both produced "Pool AA", so a 64-pool run silently gave two
// DIFFERENT pools the identical name, and the dojo-tree skeleton (which
// keys knockout leaves by pool name, qualifierSlotsFromLeaves) then dropped
// one of them, reading the second "Pool AA" as a duplicate winner label for
// the first.
//
// Mirrored (bc-drwx item 9) by poolLetterName in web-mobile/js/data.jsx,
// which builds the SAME sequence for buildPools' client-side preview: that
// JS copy used to wrap past 26 with the raw single-character
// String.fromCharCode(65+i) (no bijective carry at all, printing "Pool ["
// and worse past position 26), a different failure mode from this
// function's old doubled-letter collision but the same root cause -- naming
// a pool by a single ASCII-arithmetic letter instead of the bijective
// base-26 sequence. Keep the two in lockstep: a change here without the
// matching JS change reintroduces the exact class of bug bc-drwx item 6
// closed on this side.
func poolPositionName(i int) string {
	// i is 0-based; bijective base-26 is naturally 1-based (there is no
	// "digit zero" -- Z rolls over to AA the same way 9 rolls over to 10 in
	// ordinary base-10, but the NEXT letter after Z is AA, not A0).
	n := i + 1
	var letters []byte
	for n > 0 {
		n--
		letters = append([]byte{byte('A' + n%26)}, letters...)
		n /= 26
	}
	return fmt.Sprintf("Pool %s", string(letters))
}

// assignPlayersToPools is CreatePools' assignment body (bc-drwx item 11:
// CreatePoolsForCount, a second caller supplying the pool COUNT directly
// instead of deriving it from poolSize+isMax via PoolCount, was removed --
// it had no production caller of its own, only its own dedicated test):
// given the target size for each of len(targetSizes) pools, distribute
// players into them avoiding dojo/name conflicts where possible, falling
// back to leastConflictedPool then forcePoolSize when a conflict-free
// placement does not exist, and name the pools alphabetically in the order
// they end up in.
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

	// keys is built once for the whole assignment (bc-drwx review fix),
	// not re-normalized per player/pool comparison -- see dojoKeyCache's
	// own doc comment.
	keys := make(dojoKeyCache, totalPools)

	// Per-pool sets for O(1) dojo-conflict detection.
	dojoSets := make([]map[string]bool, totalPools)
	for i := range dojoSets {
		dojoSets[i] = make(map[string]bool)
	}

	for i, player := range players {
		poolN := discoverPool(pools, dojoSets, player, targetSizes, i%totalPools, keys)
		// no conflict-free pool available: pick the least-conflicted one
		if poolN < 0 {
			poolN = leastConflictedPool(pools, targetSizes, player.Dojo, keys)
		}

		// try and force pool size
		if poolN < 0 {
			poolN = forcePoolSize(pools, targetSizes)
		}
		player.PoolPosition = int64(len(pools[poolN].Players) + 1)
		pools[poolN].Players = append(pools[poolN].Players, player)
		dojoSets[poolN][keys.of(player.Dojo)] = true
	}

	for i := 0; i < len(pools); i++ {
		pools[i].PoolName = poolPositionName(i)
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
func discoverPool(pools []Pool, dojoSets []map[string]bool, player Player, targetSizes []int, startIndex int, keys dojoKeyCache) int {
	totalPools := len(pools)
	if totalPools == 0 {
		return -1
	}

	// Hoisted out of the loop (bc-drwx review fix): player.Dojo does not
	// change across candidates, so normalizing it once here instead of once
	// per candidate pool is a pure win.
	key := keys.of(player.Dojo)

	for i := 0; i < totalPools; i++ {
		curr := (startIndex + i) % totalPools

		// making sure there's space first
		if len(pools[curr].Players) >= targetSizes[curr] {
			continue
		}

		// O(1): reject if this dojo is already present in the pool
		if dojoSets[curr][key] {
			continue
		}

		return curr
	}

	// If no suitable pool is found, return -1
	return -1
}

// dojoKey is the ONE normalized projection every draw comparison and map key
// built from a dojo string must go through (bc-drwx item 3). Identity
// already normalizes the dojo the same way as the name
// (helper.NormalizeParticipantName, applied to both halves of a dedup key --
// dedup.go's newDupKey, and to CompetitorKey's own dojo half, identity.go)
// before comparing two participants, but the draw's own placement logic
// (countDojoInPool and every raw ".Dojo ==" / ".Dojo !=" / map-keyed-by-dojo
// site in pool_distribution_tree_aware.go and seed.go) used to compare the
// raw, as-typed string instead: two rows spelling one dojo "Mumeishi" and
// "mumeishi" landed in the SAME pool, because identical spelling is what
// every one of those raw comparisons required to treat them as one dojo.
// Routing every such comparison and map key through this function instead
// is what makes countDojoInPool's own "one place" doc comment true: dojo
// comparison needing to normalize is now a one-line change, here.
//
// This is a comparison-time projection ONLY -- it must never be written back
// onto a Player.Dojo field or into rendered output, which keeps the
// operator's original spelling on the sheet exactly as typed. A roster
// whose dojo spellings are already byte-identical is unaffected: dojoKey is
// the identity function on ASCII/already-normalized input, so every
// existing golden with consistent spelling holds unchanged.
func dojoKey(dojo string) string {
	return NormalizeParticipantName(dojo)
}

// dojoKeyCache memoizes dojoKey by its raw input string, built ONCE per draw
// call and threaded through every hot dojo-comparison path (bc-drwx review
// fix: the original item-3 fix called dojoKey/NormalizeParticipantName fresh
// inside countDojoInPool and its callers, which measured 25x-200x slower
// than origin/main -- NormalizeParticipantName does real work (NFD
// decompose, strip combining marks, re-NFC, lowercase, whitespace collapse),
// and a draw's hot paths compare the SAME small set of distinct dojo strings
// over and over. Keying by the raw string and normalizing only on a cache
// miss turns O(comparisons) normalizations into O(distinct dojos). dojoKey
// itself stays the one normalization primitive; this only avoids calling it
// twice for a string it has already seen.
//
// dojoIDCache (below) is this cache's int-keyed sibling (bc-pnum): a draw's
// PER-POOL and PER-NODE dojo tallies (the tree-aware distributor's `counts`
// and dojoNode.dojoCount, pool_distribution_tree_aware.go) still paid a
// map[string]* lookup on every candidate evaluated even once dojoKey itself
// was memoized here -- mapaccess2_faststr profiled at 51% cumulative in
// BuildPoolPhaseTreeAware_256_16x16_Interleaved. dojoIDCache wraps a
// dojoKeyCache to interning each normalized key ONE level further, into a
// dense int, so those tallies can be a plain []int instead.
//
// MEASURED (bc-pnum, this machine, -benchtime 1x, median of 3):
// BuildPoolPhaseTreeAware_256_16x16_Interleaved 3029ms before this change,
// 452-736ms after (pool composition dependent -- the shape's own run-to-run
// variance is real, not a regression; see this file's own comment on the
// tree-aware distributor's file-level doc comment for the residual this
// leaves and why); BuildPoolPhaseTreeAware_64_16x4_Interleaved 12.9ms
// before, 3.7ms after. A CPU profile of the same benchmark post-fix (with
// earliestDojoMeetingScan's buffer reuse, see that function's own doc
// comment) shows mapaccess2_faststr down to ~1.3% cumulative, from 51%.
type dojoKeyCache map[string]string

// of returns dojo's normalized key, computing and caching it on first use.
func (c dojoKeyCache) of(dojo string) string {
	if k, ok := c[dojo]; ok {
		return k
	}
	k := dojoKey(dojo)
	c[dojo] = k
	return k
}

// dojoIDCache interns each distinct NORMALIZED dojo key (via the
// dojoKeyCache it wraps) to a dense int id (0..n-1), built once per draw
// call, BEFORE any []int/[][]int sized by numDojos() is allocated: a caller
// that mints a new id (by calling `of` on a not-yet-seen dojo) AFTER sizing
// a slice to a prior numDojos() would index past the end of it. Every
// production caller avoids this by interning every player in the whole
// roster up front (buildPoolPhaseTreeAwareCore, delayDojoMeetings) before
// building anything sized by the result.
//
// `of` returns the SAME id for two spellings of one dojo, exactly as
// dojoKeyCache.of returns the same normalized string for them -- two raw
// spellings share one byKey entry, so a repeat raw string only ever costs
// one dojoKeyCache probe (of) plus one int-map probe (byKey), and a repeat
// NORMALIZED key costs only the second.
type dojoIDCache struct {
	keys  dojoKeyCache
	byKey map[string]int
}

// newDojoIDCache wraps an existing dojoKeyCache (raw -> normalized),
// layering the normalized -> dense-id map on top. capacity is a sizing hint
// for that map, matching dojoKeyCache's own make(dojoKeyCache, capacity)
// idiom.
func newDojoIDCache(keys dojoKeyCache, capacity int) dojoIDCache {
	return dojoIDCache{keys: keys, byKey: make(map[string]int, capacity)}
}

// of returns dojo's dense id, minting a new one (the current byKey size) the
// first time its normalized key is seen.
func (c dojoIDCache) of(dojo string) int {
	key := c.keys.of(dojo)
	if id, ok := c.byKey[key]; ok {
		return id
	}
	id := len(c.byKey)
	c.byKey[key] = id
	return id
}

// numDojos returns the number of distinct dojos interned so far: the dense
// id space's size, i.e. the length every []int/[][]int this cache indexes
// into must be allocated with.
func (c dojoIDCache) numDojos() int {
	return len(c.byKey)
}

// countDojoInPool returns how many of pool's competitors are from dojo. Shared
// by the fallback that chooses where an unplaceable competitor goes and by the
// repair pass that swaps competitors afterwards, so the two can never disagree
// about what "how many of this dojo are here" means -- dojo comparison is
// normalized (dojoKey) here, in the one place it changes.
func countDojoInPool(pool Pool, dojo string, keys dojoKeyCache) int {
	key := keys.of(dojo)
	n := 0
	for _, pl := range pool.Players {
		if keys.of(pl.Dojo) == key {
			n++
		}
	}
	return n
}

// leastConflictedPool is assignPlayersToPools' first fallback, reached when
// discoverPool finds no dojo-conflict-free pool with room for a player (bc-drwx
// item 11: discoverPool has never checked names -- see its own doc comment,
// "sharing a NAME is deliberately not a conflict" -- so this is a dojo
// conflict only, never a name one). Among the pools that still have room, it
// picks the one holding the FEWEST players already sharing the incoming
// player's dojo, tie-broken by fewest players overall, then by lowest index.
// The strict "<" comparisons (rather than "<=") keep the lowest-index pool on
// a tie, which is what makes the output deterministic. Returns -1 if no pool
// has room.
func leastConflictedPool(pools []Pool, targetSizes []int, dojo string, keys dojoKeyCache) int {
	best := -1
	bestDojo, bestSize := 0, 0
	for i, pool := range pools {
		if len(pool.Players) >= targetSizes[i] {
			continue
		}
		n := countDojoInPool(pool, dojo, keys)
		if best < 0 || n < bestDojo || (n == bestDojo && len(pool.Players) < bestSize) {
			best, bestDojo, bestSize = i, n, len(pool.Players)
		}
	}
	return best
}

// forcePoolSize picks the pool an overflow player lands in: the one with the
// LEAST excess over its own target, tie-broken outer to inner (see
// forcePoolSizeFromCounts' own doc comment for why "least excess" and not
// merely "the first with room" is the real rule). It reads pool LENGTHS and
// nothing else, which is what lets realTargetSizes precompute the same
// landing order before any player exists.
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
//
// Picks the pool with the LEAST excess over its own target
// (counts[i]-targetSizes[i]), tie-broken outer-to-inner -- not "the first
// pool still at or under target", which is only the SAME thing for the
// first pass around every pool (bc-drwx item 5). A remainder can exceed
// len(counts): poolTargetSizes' own uniform min-mode row bounds the
// dynamic shortfall to < poolSize, and poolSize is NOT guaranteed <=
// len(counts) for every caller (14 entrants at minimum pool size 5 forms
// only 2 pools, poolSize > totalPools) -- realTargetSizes' old doc comment
// claimed otherwise and was wrong. The excess-based comparison is what
// makes a SECOND (or further) full outer-to-inner round fall out for
// free instead of needing a repeated-rounds loop bolted on: once every
// pool has received its first extra seat (excess 1 everywhere), the same
// scan order finds the next one still on excess 1 rather than falling
// through to an unconditional "return 0" that piled every remaining seat
// onto pool 0 (repro: 14 entrants at min size 5 -> pools [8,6] instead of
// the correct [7,7]).
func forcePoolSizeFromCounts(counts, targetSizes []int) int {
	best := -1
	// bestExcess tracks the champion's own excess value directly, rather
	// than re-deriving it via counts[best]-targetSizes[best]: gosec (G602)
	// cannot see that the `best == -1 ||` short circuit makes that
	// re-derivation unreachable while best is still -1, and flags it as a
	// possible out-of-range index. Indexing counts/targetSizes only at the
	// loop-bound-safe i/j keeps every access provably in range to both the
	// reader and the linter.
	bestExcess := 0
	for i, j := 0, len(counts)-1; i <= j; i, j = i+1, j-1 {
		if e := counts[i] - targetSizes[i]; best == -1 || e < bestExcess {
			best, bestExcess = i, e
		}
		if i != j {
			if e := counts[j] - targetSizes[j]; e < bestExcess {
				best, bestExcess = j, e
			}
		}
	}
	if best == -1 {
		// Unreachable for a non-empty counts (the loop body always sets
		// best on its very first iteration); kept as a defensive fallback
		// rather than an index-out-of-range panic for an empty slice.
		return 0
	}
	return best
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

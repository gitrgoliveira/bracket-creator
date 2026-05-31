package helper

import "fmt"

// EstimateMatchCountsInput carries the scalar fields from a competition
// configuration that are needed to derive pre-draw pool and playoff match
// counts. Only scalar types are used (no *state.Competition) because the
// state package imports helper — adding the reverse import would create a
// cycle. Callers in the engine layer should populate this struct from the
// relevant Competition fields.
//
// Required field matrix by Format:
//
//	"playoffs"   — PlayerCount only (no pool fields needed).
//	"mixed"      — PlayerCount, PoolSize (>0), PoolSizeMode, PoolWinners,
//	               RoundRobin, PoolFormat.
//	"league"     — PlayerCount only (single pool, always full round-robin).
//	"swiss"      — PlayerCount, SwissRounds.
type EstimateMatchCountsInput struct {
	// Format is state.Competition.Format: "playoffs", "mixed", "league",
	// "swiss". Any other value returns an error.
	Format string

	// PlayerCount is the number of participants that will take part in
	// this competition. For competitions with an empty pre-draw roster
	// (source-linked playoffs) the caller must derive the count from the
	// source competition's pool count × PoolWinners.
	PlayerCount int

	// Pool-phase fields — relevant for "mixed" and "league" formats.
	PoolSize     int    // state.Competition.PoolSize
	PoolSizeMode string // state.Competition.PoolSizeMode: "max" | "" == "min"
	PoolWinners  int    // state.Competition.PoolWinners (default 2 when 0)
	RoundRobin   bool   // state.Competition.RoundRobin
	PoolFormat   string // state.Competition.PoolFormat: "" | "full" | "partial"

	// SwissRounds is the number of rounds for a swiss-format competition.
	SwissRounds int
}

// EstimateMatchCounts returns the expected number of pool matches and
// playoff bracket matches for a competition given its configuration and
// participant count. The estimates are purely derived from the same
// formulas used by the real draw pipeline:
//
//   - Pool count: helper.CreatePools (ceiling vs floor division by PoolSize,
//     driven by PoolSizeMode == "max").
//   - Pool matches: poolMatchesPerPool for each individual pool size.
//   - Bracket size: helper.NextPow2(numFinalists) - 1, where numFinalists
//     = numPools × PoolWinners.
//
// All three sub-helpers (poolMatchesPerPool, bracketMatchCount, and the
// pool-count math) mirror the real draw code without duplicating its
// formulas — making this the single source of truth for pre-draw estimates.
// See the bead mp-zoh plan's "Central design risk" section.
//
// Returned counts include all scheduled matches, including auto-resolved
// byes (which the slot assigners still allocate a time slot for). Negative
// PlayerCount or zero-round Swiss are clamped to zero matches rather than
// erroring, because the estimator may be called speculatively before
// validation is complete.
//
// Returns an error for:
//   - Unknown Format strings.
//   - PoolSize == 0 for the mixed format (would divide by zero).
func EstimateMatchCounts(in EstimateMatchCountsInput) (poolMatchCount, playoffMatchCount int, err error) {
	if in.PlayerCount <= 0 {
		return 0, 0, nil
	}

	switch in.Format {
	case "playoffs":
		// No pool phase. Bracket over all players.
		return 0, bracketMatchCount(in.PlayerCount), nil

	case "mixed":
		return estimateMixed(in)

	case "league":
		// Single pool of all players, always full round-robin, no playoffs.
		return in.PlayerCount * (in.PlayerCount - 1) / 2, 0, nil

	case "swiss":
		// SwissRounds * ceil(playerCount/2) matches per round.
		// The bye-recipient match (when playerCount is odd) is included
		// because buildSwissMatches persists it in pool-matches.csv alongside
		// the real pairings — the slot assigner allocates a slot for it.
		if in.SwissRounds <= 0 {
			return 0, 0, nil
		}
		perRound := (in.PlayerCount + 1) / 2 // ceil(N/2)
		return perRound * in.SwissRounds, 0, nil

	default:
		return 0, 0, fmt.Errorf("EstimateMatchCounts: unknown competition format %q", in.Format)
	}
}

// estimateMixed handles the "mixed" (Pools + Knockout) format. Extracted for
// readability.
func estimateMixed(in EstimateMatchCountsInput) (poolMatchCount, playoffMatchCount int, err error) {
	if in.PoolSize <= 0 {
		return 0, 0, fmt.Errorf("EstimateMatchCounts: PoolSize must be > 0 for mixed format, got %d", in.PoolSize)
	}

	// --- Pool count (mirrors CreatePools, tournament.go:213-278) ---
	// isMax == true  → ceil division  (PoolSizeMode == "max")
	// isMax == false → floor division (all other values)
	isMax := in.PoolSizeMode == "max"
	var numPools int
	if isMax {
		numPools = (in.PlayerCount + in.PoolSize - 1) / in.PoolSize
	} else {
		numPools = in.PlayerCount / in.PoolSize
	}
	if numPools == 0 {
		// Fewer players than pool size in min mode — no pools, no matches.
		return 0, 0, nil
	}

	// --- Per-pool sizes (mirrors the targetSizes distribution in CreatePools) ---
	// base = playerCount / numPools; first `rem` pools get base+1 players.
	base := in.PlayerCount / numPools
	rem := in.PlayerCount % numPools
	totalPoolMatches := 0
	for i := 0; i < numPools; i++ {
		size := base
		if i < rem {
			size = base + 1
		}
		totalPoolMatches += poolMatchesPerPool(size, in.RoundRobin, in.PoolFormat)
	}

	// --- Playoff bracket ---
	poolWinners := in.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2 // mirrors engine/ranking.go:169 default
	}
	numFinalists := numPools * poolWinners
	totalPlayoffMatches := bracketMatchCount(numFinalists)

	return totalPoolMatches, totalPlayoffMatches, nil
}

// poolMatchesPerPool returns the number of matches generated for a single
// pool of `size` players under the given format settings. It mirrors the
// three match-generator code paths in engine/pools.go:
//
//  1. state.PoolFormatPartial ("partial"): N-1 adjacent-pair matches.
//     (CreatePartialPoolMatches / pool.GenerateAdjacentPairings)
//  2. "full" or "" + RoundRobin == true: C(N,2) = N*(N-1)/2 full round-robin.
//     (CreatePoolRoundRobinMatches)
//  3. "full" or "" + RoundRobin == false: CreatePoolMatches pattern.
//     - N==0 → 0
//     - N==1 → 1 (degenerate self-match, kept for caller parity)
//     - N==2 → 1
//     - N==3 → 3
//     - N==4 → 4
//     - N>=5 → N
func poolMatchesPerPool(size int, roundRobin bool, poolFormat string) int {
	if size <= 0 {
		return 0
	}

	// Partial pool format overrides the RoundRobin flag (engine/pools.go:36-38).
	if poolFormat == "partial" {
		if size < 2 {
			return 0
		}
		return size - 1
	}

	// Full round-robin.
	if roundRobin {
		return size * (size - 1) / 2
	}

	// Non-RR (CreatePoolMatches). Mirrors the exact switch/loop in
	// tournament.go:330-388.
	switch size {
	case 1:
		return 1 // degenerate self-match
	case 2:
		return 1
	case 3:
		return 3
	case 4:
		return 4
	default:
		// N>=5: the loop adds 2 matches per even step plus 1 if odd.
		// Result = N regardless of parity (verified by inspection).
		return size
	}
}

// bracketMatchCount returns the number of bracket match slots generated by
// generatePlayoffs for `players` competitors. The tree has NextPow2(players)
// leaves; a full binary tree with K leaves has K-1 internal nodes, each
// representing one match. All auto-resolved bye matches are included because
// they are persisted in bracket.json and assigned schedule slots.
//
// Returns 0 for zero or one player (no real match can be played).
func bracketMatchCount(players int) int {
	if players <= 1 {
		return 0
	}
	return NextPow2(players) - 1
}

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
	// "swiss". Empty string is treated as "playoffs" for backward
	// compatibility with legacy configs. Any other value returns an error.
	Format string

	// PlayerCount is the number of participants that will take part in
	// this competition. For competitions with an empty pre-draw roster
	// (source-linked playoffs) the caller must derive the count from the
	// source competition's pool count × PoolWinners.
	PlayerCount int

	// Pool-phase fields — relevant for "mixed" and "league" formats.
	PoolSize     int    // state.Competition.PoolSize
	PoolSizeMode string // state.Competition.PoolSizeMode: "max" or "min" (any other value including "" treated as "min")
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
//   - Bracket matches: bracketMatchCount(numFinalists), which returns the
//     court-time-consuming match count (excludes auto-resolved byes) — equal to
//     numFinalists-1 now that byes are distributed to the top seeds (mp-sess).
//
// Pool count and per-pool sizes are derived by calling the real CreatePools
// (no duplication). Per-pool match counting (poolMatchesPerPool) and bracket
// match counting (bracketMatchCount) mirror the draw's match-generation
// logic; these are lightweight formulas cross-checked by integration tests
// against the real generators. See mp-zoh plan's "Central design risk".
//
// Returned counts reflect court-time-consuming matches only. Auto-resolved
// bracket byes (player-vs-bye leaf matches marked Completed at generation time)
// are excluded because assignBracketMatchSlots does not advance the court
// cursor for them (scheduler_slots.go:286-291). Pool-side byes are not
// applicable (pools have no bye mechanism). Negative PlayerCount or
// zero-round Swiss are clamped to zero matches rather than erroring,
// because the estimator may be called speculatively before validation is
// complete.
//
// Returns an error for:
//   - Unknown Format strings (only when PlayerCount > 0; the zero-player
//     early return takes precedence and returns nil — callers should not
//     rely on a format error for zero-player inputs).
//   - PoolSize == 0 for the mixed format (would divide by zero).
func EstimateMatchCounts(in EstimateMatchCountsInput) (poolMatchCount, playoffMatchCount int, err error) {
	if in.PlayerCount <= 0 {
		return 0, 0, nil
	}

	switch in.Format {
	case "playoffs", "":
		// No pool phase. Bracket over all players.
		// Empty format is treated as playoffs for backward compatibility with
		// legacy competition configs that predate the Format field.
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
//
// Finding 4 fix: when playerCount < poolSize in min mode, numPools is 0 and
// an error is returned — mirroring CreatePools' error at tournament.go:222.
// In max mode numPools is always ≥1 when playerCount ≥1, so the error only
// fires for min mode.
//
// Pool sizes are derived by calling CreatePools with synthetic players
// (all-unique names and dojos) rather than using an even base/rem split.
// This mirrors the EXACT distribution produced by the real draw (tournament.go
// CreatePools → forcePoolSize), including the "overflow > numPools" case where
// forcePoolSize piles extra players into pool[0] rather than distributing evenly.
//
// All-unique dojos ensure discoverPool never hits a dojo-conflict that would
// route players via forceSameDojo, keeping the pool-size distribution identical
// to the pure targetSize/forcePoolSize path.
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
		// Fewer players than pool size in min mode — mirrors CreatePools'
		// error at tournament.go:222.
		return 0, 0, fmt.Errorf("EstimateMatchCounts: player count (%d) is less than pool size (%d) in min mode", in.PlayerCount, in.PoolSize)
	}

	// --- Per-pool sizes via the REAL CreatePools (drift-proof) ---
	// Synthetic players: each has a unique name and a unique dojo so that
	// discoverPool never triggers the dojo-conflict skip — ensuring that pool
	// sizes are driven solely by targetSizes / forcePoolSize, exactly as the
	// real draw does for a freshly imported roster.
	synth := make([]Player, in.PlayerCount)
	for i := range synth {
		synth[i] = Player{
			Name: fmt.Sprintf("est%d", i),
			Dojo: fmt.Sprintf("d%d", i),
		}
	}
	realPools, perr := CreatePools(synth, in.PoolSize, isMax)
	if perr != nil {
		return 0, 0, fmt.Errorf("EstimateMatchCounts: CreatePools failed: %w", perr)
	}

	totalPoolMatches := 0
	for _, p := range realPools {
		totalPoolMatches += poolMatchesPerPool(len(p.Players), in.RoundRobin, in.PoolFormat)
	}

	// --- Playoff bracket ---
	poolWinners := in.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2 // mirrors buildFinalistResolver's default in engine/knockout.go
	}
	numFinalists := len(realPools) * poolWinners
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

// bracketMatchCount returns the number of bracket matches that CONSUME COURT
// TIME for `players` competitors — i.e., matches whose Status is NOT Completed
// at draw-generation time. The court cursor in assignBracketMatchSlots is NOT
// advanced for auto-resolved (Completed) matches, so they must NOT be counted
// for duration estimation.
//
// This is exactly players-1 (for players >= 2). The playoffs draw uses
// StandardSeeding + CreateBalancedTree + TreeToLeafArray (mp-5ng7), which
// embeds the tree's structural byes into a pow2 leaf array. Byes are clustered
// where the tree is asymmetric, so some first-round slots are "" vs "" (double
// byes) and later rounds can have a "" vs "Winner of…" latent bye. All such
// slots are auto-completed at generation time (Status=Completed) and do not
// advance the court cursor, so they do not consume court time. The identity
// still holds: every real match eliminates one of N competitors, so N-1
// eliminations crown a winner regardless of where byes cluster.
//
// Returns 0 for zero or one player (no real match can be played).
func bracketMatchCount(players int) int {
	if players <= 1 {
		return 0
	}
	return players - 1
}

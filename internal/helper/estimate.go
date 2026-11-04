package helper

import (
	"fmt"
	"math/bits"
)

// EstimateCountError is a shape the draw would refuse (a pool size, an entrant
// count the pool builders cannot cut), so the estimate cannot count it. Its
// text is the sentence the organiser reads; callers answer it as a 400.
type EstimateCountError struct {
	Err error
}

func (e *EstimateCountError) Error() string { return e.Err.Error() }

func (e *EstimateCountError) Unwrap() error { return e.Err }

func countErrorf(format string, args ...any) error {
	return &EstimateCountError{Err: fmt.Errorf(format, args...)}
}

// EstimateMatchCountsInput carries the scalar fields from a competition
// configuration that are needed to derive pre-draw pool and knockout match
// counts. Only scalar types are used (no *state.Competition) because the
// state package imports helper, adding the reverse import would create a
// cycle. Callers in the engine layer should populate this struct from the
// relevant Competition fields.
//
// Required field matrix by Format:
//
//	"knockout", PlayerCount, SingleThirdPlace.
//	"mixed", PlayerCount, PoolSize (>0), PoolSizeMode, PoolWinners,
//	               RoundRobin, PoolFormat, ExtraQualifiers, SeedRanks,
//	               SingleThirdPlace.
//	"league", PlayerCount only (single pool, always full round-robin).
//	"swiss", PlayerCount, SwissRounds.
type EstimateMatchCountsInput struct {
	// Format is state.Competition.Format: "knockout", "mixed", "league",
	// "swiss". Empty string is treated as "knockout" for backward
	// compatibility with legacy configs. Any other value returns an error.
	Format string

	// PlayerCount is the number of participants that will take part in
	// this competition.
	PlayerCount int

	// Pool-phase fields, relevant for "mixed" and "league" formats.
	PoolSize     int    // state.Competition.PoolSize
	PoolSizeMode string // state.Competition.PoolSizeMode: "max" or "min" (any other value including "" treated as "min")
	PoolWinners  int    // state.Competition.PoolWinners (default 2 when 0)
	RoundRobin   bool   // state.Competition.RoundRobin
	PoolFormat   string // state.Competition.PoolFormat: "" | "full" | "partial"

	// SwissRounds is the number of rounds for a swiss-format competition.
	SwissRounds int

	// ExtraQualifiers is state.Competition.ExtraQualifiers: "",
	// "larger-pools", or "fill-bracket". Relevant for "mixed" format.
	ExtraQualifiers string

	// SeedRanks carries the seed ranks of seeded competitors among the
	// counted participants, used in fill-bracket pool formation and draft
	// selection.
	SeedRanks []int

	// SingleThirdPlace indicates whether the competition format requires
	// a bronze (third-place) match. Derived from
	// state.Competition.RequiresSingleThirdPlace().
	SingleThirdPlace bool

	// Draft marks the estimate for a competition that does not exist yet
	// (the Add competition form). Such a competition has no seeds, so a
	// fill-bracket draft is estimated as if it were seeded enough for the
	// draw to run, rather than refused with "seed more pools".
	Draft bool
}

// EstimateMatchCounts returns the expected number of pool matches and
// knockout bracket matches for a competition given its configuration and
// participant count. The estimates are purely derived from the same
// formulas used by the real draw pipeline:
//
//   - Pool count and pool sizes: derived via BuildPoolPhaseTreeAwareWithMode
//     or BuildPoolPhaseFillBracket depending on ExtraQualifiers.
//   - Pool matches: poolMatchesPerPool for each individual pool size.
//   - Finalists: sum of per-pool qualifiers honoring ExtraQualifiers
//     (QualifiersForOversizedPool in larger-pools mode, SelectFillBracketDrafts
//     in fill-bracket mode).
//   - Bracket matches: bracketMatchCount(numFinalists), plus a third-place
//     match when SingleThirdPlace is requested and the bracket has a semifinal
//     (NeedsBronzeBlock).
//
// Returned counts reflect court-time-consuming matches only. Auto-resolved
// bracket byes (player-vs-bye leaf matches marked Completed at generation time)
// are excluded because assignBracketMatchSlots does not advance the court
// cursor for them (its Status == Completed branch in scheduler_slots.go). Pool-side byes are not
// applicable (pools have no bye mechanism). Negative PlayerCount or
// zero-round Swiss are clamped to zero matches rather than erroring,
// because the estimator may be called speculatively before validation is
// complete.
//
// Returns an *EstimateCountError for:
//   - Unknown Format strings (only when PlayerCount > 0; the zero-player
//     early return takes precedence and returns nil, callers should not
//     rely on a format error for zero-player inputs).
//   - PoolSize <= 0 for the mixed format.
//   - Player count or formation shortfalls refused by the real pool builders.
func EstimateMatchCounts(in EstimateMatchCountsInput) (poolMatchCount, knockoutMatchCount int, err error) {
	if in.PlayerCount <= 0 {
		return 0, 0, nil
	}

	switch in.Format {
	case "knockout", "":
		// No pool phase. Bracket over all players.
		// Empty format is treated as knockout for backward compatibility with
		// legacy competition configs that predate the Format field.
		knockoutMatches := bracketMatchCount(in.PlayerCount)
		if in.SingleThirdPlace {
			rounds := knockoutRounds(in.PlayerCount)
			if NeedsBronzeBlock(true, rounds) {
				knockoutMatches++
			}
		}
		return 0, knockoutMatches, nil

	case "mixed":
		return estimateMixed(in)

	case "league":
		// Single pool of all players, always full round-robin, no knockout.
		return in.PlayerCount * (in.PlayerCount - 1) / 2, 0, nil

	case "swiss":
		// SwissRounds * ceil(playerCount/2) matches per round.
		// The bye-recipient match (when playerCount is odd) is included
		// because buildSwissMatches persists it in pool-matches.csv alongside
		// the real pairings, the slot assigner allocates a slot for it.
		if in.SwissRounds <= 0 {
			return 0, 0, nil
		}
		perRound := (in.PlayerCount + 1) / 2 // ceil(N/2)
		return perRound * in.SwissRounds, 0, nil

	default:
		return 0, 0, countErrorf("unknown competition format %q: choose knockout, mixed, league or swiss", in.Format)
	}
}

// estimateMixed handles the "mixed" (Pools + Knockout) format.
//
// Pool sizes and qualifiers are derived by calling the real builders
// (BuildPoolPhaseFillBracket or BuildPoolPhaseTreeAwareWithMode) with synthetic
// players (unique names and dojos, with seeds applied by rank).
func estimateMixed(in EstimateMatchCountsInput) (poolMatchCount, knockoutMatchCount int, err error) {
	if in.PoolSize <= 0 {
		return 0, 0, countErrorf("cannot create pools: pool size must be at least 1, got %d", in.PoolSize)
	}
	if in.PoolSize >= MaxPoolSize {
		return 0, 0, countErrorf("cannot create pools: pool size must be less than %d, got %d", MaxPoolSize, in.PoolSize)
	}

	isMax := in.PoolSizeMode == "max"

	synth := make([]Player, in.PlayerCount)
	for i := range synth {
		synth[i] = Player{
			Name: fmt.Sprintf("est%d", i),
			Dojo: fmt.Sprintf("d%d", i),
		}
	}
	for i, r := range in.SeedRanks {
		if i < len(synth) {
			synth[i].Seed = r
		}
	}

	var realPools []Pool
	var numFinalists int

	if in.ExtraQualifiers == QualifierModeFillBracket {
		if in.Draft && len(in.SeedRanks) == 0 {
			// A draft has no roster to seed yet, so estimate the draw the
			// organiser gets once it is seeded: seeding the first
			// ceil(n/(size+1)) entrants supplies every drafted 2nd place
			// FillBracketPoolCount can ask for, and seeds move no one
			// between pool sizes, so the counts are the seeded draw's.
			minPools := (in.PlayerCount + in.PoolSize) / (in.PoolSize + 1)
			for i := 0; i < minPools && i < len(synth); i++ {
				synth[i].Seed = i + 1
			}
		}
		pools, _, perr := BuildPoolPhaseFillBracket(synth, in.PoolSize, 1)
		if perr != nil {
			return 0, 0, &EstimateCountError{Err: perr}
		}
		realPools = pools

		draftIdx, derr := SelectFillBracketDraftIndices(realPools, in.PoolSize, 1)
		if derr != nil {
			return 0, 0, &EstimateCountError{Err: derr}
		}
		numFinalists = len(realPools) + len(draftIdx)
	} else {
		poolWinners := in.PoolWinners
		if poolWinners <= 0 {
			poolWinners = defaultPoolWinners
		}

		pools, _, perr := BuildPoolPhaseTreeAwareWithMode(synth, in.PoolSize, isMax, 1, poolWinners, in.ExtraQualifiers)
		if perr != nil {
			return 0, 0, &EstimateCountError{Err: perr}
		}
		realPools = pools

		if in.ExtraQualifiers == QualifierModeLargerPools {
			// The same arguments state.Competition.QualifiersForPool passes the
			// draw: the competition's pool size is the minimum to be over.
			numFinalists = 0
			for _, p := range realPools {
				numFinalists += QualifiersForOversizedPool(len(p.Players), in.PoolSize, poolWinners)
			}
		} else {
			numFinalists = len(realPools) * poolWinners
		}
	}

	totalPoolMatches := 0
	for _, p := range realPools {
		totalPoolMatches += poolMatchesPerPool(len(p.Players), in.RoundRobin, in.PoolFormat)
	}

	totalKnockoutMatches := bracketMatchCount(numFinalists)
	if in.SingleThirdPlace {
		rounds := knockoutRounds(numFinalists)
		if NeedsBronzeBlock(true, rounds) {
			totalKnockoutMatches++
		}
	}

	return totalPoolMatches, totalKnockoutMatches, nil
}

// knockoutRounds returns the number of rounds for an elimination bracket
// of the given player/finalist count, which is ceil(log2(players)).
func knockoutRounds(players int) int {
	if players <= 1 {
		return 0
	}
	return bits.Len(uint(players - 1))
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
// TIME for `players` competitors, i.e., matches whose Status is NOT Completed
// at draw-generation time. The court cursor in assignBracketMatchSlots is NOT
// advanced for auto-resolved (Completed) matches, so they must NOT be counted
// for duration estimation.
//
// This is exactly players-1 (for players >= 2). The knockout draw uses
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

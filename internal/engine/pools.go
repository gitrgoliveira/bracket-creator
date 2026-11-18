package engine

import (
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// generatePools lays out the pool phase. courtFloor is the earliest start per
// court that the neighbouring same-day competitions leave (nil for none); the
// caller scans for it once and hands the same floor to the preview bracket.
func (e *Engine) generatePools(comp *state.Competition, players []domain.Player, seeds []domain.SeedAssignment, courtFloor map[string]time.Time) ([]state.MatchResult, error) {
	// The settings refusals come before the seeds: a stale seed row is a plain
	// error, and it must not hide the 400 that names the setting to fix.
	if err := refuseUnformablePoolConfig(comp); err != nil {
		return nil, wrapValidationErrorf(err, "competition %s cannot start: %s", comp.ID, err.Error())
	}

	// helper.Player is a type alias for domain.Player (NFR-007); the
	// Excel-coupled helpers accept domain values directly.
	if len(seeds) > 0 {
		if err := helper.ApplySeeds(players, seeds); err != nil {
			return nil, fmt.Errorf("applying seeds: %w", err)
		}
	}

	// A refusal is a *ValidationError (-> HTTP 400 at the generate-draw
	// handler): the ones formPools raises are operator-actionable roster or
	// config problems. The one that is NOT -- the "no pool has room" placement
	// guard (assignUnseededByDojoTree) -- is an internal invariant check that
	// cannot fire while sum(targetSizes) == len(players), which realTargetSizes
	// guarantees. It is wrapped all the same, deliberately: the case is
	// unreachable, so branching on it would be handling for something that
	// cannot happen, and a 500 carrying the same "cannot place player X" text is
	// no more actionable to an operator mid-tournament than a 400 is. What is NOT
	// acceptable is a comment claiming the guard belongs to the actionable class:
	// that is what would let a future writer add a genuine internal fault here and
	// assume 400 was the considered answer.
	pools, drawCourts, err := formPools(comp, players)
	if err != nil {
		return nil, wrapValidationErrorf(err, "competition %s cannot start: %s", comp.ID, err.Error())
	}

	helper.NumberPools(pools, comp.EffectiveNumberPrefix())
	hasRounds := createPoolMatches(comp, pools)

	// Save pools. The refusals are all behind us (BEFORE the save, not after):
	// every occurrence used to leave a pools.csv on disk for a draw that was
	// explicitly refused, and the competition stays in setup, where DiscardDraw
	// declines to clean it up (draw-ready only), so the artifact outlived the
	// refusal (bc-dnst).
	if err := e.store.SavePools(comp.ID, pools); err != nil {
		return nil, err
	}

	// Per-court slot assignment (T150) + ceremony-block skipping
	// (T151). Loads the tournament-level tuning (multiplier,
	// opening / lunch blocks) so a missing tournament.md falls back
	// to the function's documented defaults without aborting the
	// pipeline.
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}

	results, leagueSlots := layoutPoolMatches(comp, pools, drawCourts, hasRounds)
	if comp.Format == state.CompFormatLeague {
		results, _ = assignLeagueSlotTimesFrom(results, leagueSlots, comp, tournament, courtFloor)
	} else {
		results, _ = assignPoolMatchSlotsFrom(results, comp, tournament, courtFloor)
	}

	numberPoolMatchesInPlayingOrder(results)
	return results, e.store.SavePoolMatches(comp.ID, results)
}

// refuseUnformablePoolConfig refuses the settings no roster can fix, before
// anything reads the players: formPools runs it for every caller, and
// generatePools runs it earlier still, ahead of the seeds.
func refuseUnformablePoolConfig(comp *state.Competition) error {
	// PoolSize is the divisor in CreatePools; a zero or negative value would
	// otherwise reach the guard in helper.CreatePools as a plain error mapped to
	// HTTP 500. Refusing here makes an unset pool size a clean 400. (mp-ebgz)
	if comp.PoolSize <= 0 {
		return fmt.Errorf("pool size must be at least 1, got %d, set a pool size before starting", comp.PoolSize)
	}

	// bc-qual: ExtraQualifiers is only meaningful for "mixed" (pools feeding a
	// knockout). The league format also forms pools but has a single implicit
	// pool and no knockout, so a stored non-standard value on a league record (a
	// hand-edited config.md, or a format switched outside the HTTP layer, which
	// normalizePoolConfig would have zeroed) must not steer pool FORMATION.
	// poolFedKnockout gates both the validation and the formation dispatch in
	// formPools, and the two gates have to be the same expression. Validating before
	// anything reads the value also reports an invalid setting as itself rather
	// than as whatever formation error it happens to provoke.
	poolFedKnockout := comp.Format == state.CompFormatMixed
	if poolFedKnockout {
		if err := state.ValidateExtraQualifiers(comp.ExtraQualifiers, comp.PoolSizeMode, comp.EffectivePoolWinners()); err != nil {
			return err
		}
	}
	return nil
}

// formPools forms the pool phase over players and refuses what the draw
// refuses. It stores nothing and numbers nobody, and it is the one owner of
// those rules for both callers: generatePools (the draw) and blankDraw (the
// template's empty places), so the template cannot drift from the draw it
// prints. An error is the reason alone; each caller adds its own prefix.
//
// The pool-formation steps are shared with cmd/create-pools.go
// (helper.BuildPoolPhase's doc comment carries the four ordering constraints;
// BuildPoolPhaseTreeAwareWithMode's covers the mode-aware entry point called
// here). drawCourts is what comes back, not what went in: a shiaijo with no
// home pool would own an empty bracket region, so the count steps down to what
// the pools can carry, and everything downstream (AssignPoolsToCourts, the seed
// spread) reads THAT. The raw allocation stays the single-pool league spread's,
// which really does use every shiaijo the league was given.
func formPools(comp *state.Competition, players []domain.Player) (pools []helper.Pool, drawCourts int, err error) {
	if err := refuseUnformablePoolConfig(comp); err != nil {
		return nil, 0, err
	}
	poolFedKnockout := comp.Format == state.CompFormatMixed

	// numCourts is the competition's RAW shiaijo allocation, normalised: an unset
	// court list means one unnamed court.
	numCourts := max(len(comp.Courts), 1)

	if poolFedKnockout && comp.ExtraQualifiers == state.ExtraQualifiersFillBracket {
		// bc-qual LP-4: pool COUNT comes from a different formation objective
		// (helper.FillBracketPoolCount) than the standard min-mode
		// floor(n/PoolSize). isMax is irrelevant here: fill-bracket is gated to
		// minimum-players-per-pool sizing by state.ValidateExtraQualifiers above.
		pools, drawCourts, err = helper.BuildPoolPhaseFillBracket(players, comp.PoolSize, numCourts)
	} else {
		// BuildPoolPhaseTreeAwareWithMode, not plain helper.BuildPoolPhase
		// (bc-dojo Phase 4): BuildPoolPhase's poolWinners is FIXED at its
		// documented default (2). This always has a real one, and for a
		// poolFedKnockout competition a real extra-qualifiers mode too, and
		// scoring each candidate placement against the WRONG knockout tree is
		// what the fixed default would do whenever either differs.
		//
		// extraQualifiers is forced to standard for a non-poolFedKnockout format
		// regardless of what comp.ExtraQualifiers holds, so a stored non-standard
		// value on a league/swiss/knockout record never steers pool FORMATION.
		extraQualifiers := state.ExtraQualifiersNone
		if poolFedKnockout {
			extraQualifiers = comp.ExtraQualifiers
		}
		pools, drawCourts, err = helper.BuildPoolPhaseTreeAwareWithMode(players, comp.PoolSize, comp.PoolSizeMode == "max", numCourts, comp.EffectivePoolWinners(), extraQualifiers)
	}
	if err != nil {
		return nil, 0, err
	}

	// A "mixed" competition is "Pools + Knockout" by definition, a single pool
	// collapses to a round-robin with a tacked-on 2-player "final", which is the
	// same shape as `league` and is NOT what an operator picking "mixed"
	// intends. Refuse a mixed competition whose participant count + PoolSize
	// would produce fewer than 2 pools, so the operator can reduce PoolSize, add
	// participants, or switch to `league`. (league legitimately produces 1 pool.)
	if poolFedKnockout {
		if len(pools) < 2 {
			return nil, 0, fmt.Errorf("a Pools + Knockout competition needs at least 2 pools, but %d participants at %d players per pool make %d; lower Players per pool, add participants, or change the format to League", len(players), comp.PoolSize, len(pools))
		}
		// Every pool must be able to supply PoolWinners finishers to the knockout.
		// In "max" mode an odd participant count can leave an under-filled last
		// pool (PoolSize=2 with 3 players gives pools of 2 and 1); with the default
		// PoolWinners=2 that 1-player pool could never produce a 2nd-place
		// finisher, and ResolveQualifiedPools would later fail mid-tournament.
		// Catch it BEFORE any match is played.
		poolWinners := comp.EffectivePoolWinners()
		for _, p := range pools {
			if len(p.Players) < poolWinners {
				return nil, 0, fmt.Errorf("pool %q has only %d participant(s) but %d advance to the knockout, so every pool needs at least %d; lower Winners per pool, change Players per pool or Pool size is a, or add participants", p.PoolName, len(p.Players), poolWinners, poolWinners)
			}
		}
	}

	// A one-pool competition on several shiaijo needs enough players to keep
	// them all busy.
	if len(pools) == 1 && numCourts > 1 {
		if err := ValidateCourtCount(len(players), numCourts); err != nil {
			return nil, 0, err
		}
	}
	return pools, drawCourts, nil
}

// createPoolMatches fills each pool's matches by the competition's pool format
// (partial, round robin or full) and reports whether they carry a round.
func createPoolMatches(comp *state.Competition, pools []helper.Pool) (hasRounds bool) {
	switch comp.PoolFormat {
	case state.PoolFormatPartial:
		helper.CreatePartialPoolMatches(pools)
		return true
	default:
		// PoolFormatFull (default / unset) and any unrecognized value fall
		// through to the legacy code path. RoundRobin remains the inner
		// switch for backward compatibility (FR-052, R9).
		if comp.RoundRobin {
			helper.CreatePoolRoundRobinMatches(pools)
			return true
		}
		helper.CreatePoolMatches(pools)
		return false
	}
}

// layoutPoolMatches lays the pools' matches out as the draw stores them, before
// any start time: each with its pool's shiaijo (drawCourts is what formPools
// returned, reused rather than recomputed because the allocation, the seed
// spread and the deinterleave are one layout), a league's order and slots from
// the league scheduler, a single pool on several shiaijo spread by round. The
// slots are the league scheduler's, which assignLeagueSlotTimesFrom reads; nil
// for every other format. generatePools (which then times and numbers the
// matches) and the template's blankDraw both call it, so the Pool Matches sheet
// prints the order and the shiaijo the draw would.
func layoutPoolMatches(comp *state.Competition, pools []helper.Pool, drawCourts int, hasRounds bool) (results []state.MatchResult, leagueSlots []int) {
	courtAssign := helper.AssignPoolsToCourts(len(pools), drawCourts)
	for pi, p := range pools {
		poolCourts := []string{""}
		if len(comp.Courts) > 0 {
			poolCourts = []string{comp.Courts[courtAssign[pi]]}
			// When there is only one pool (league format) and multiple
			// courts, seed each match with a court so none is left blank.
			// For League this is provisional: the rest-aware scheduler
			// below reassigns courts per slot. It matters for the
			// non-league else branch's round-position spread.
			if len(pools) == 1 && len(comp.Courts) > 1 {
				poolCourts = comp.Courts
			}
		}
		for i, m := range p.Matches {
			round := m.Round
			if !hasRounds {
				round = -1
			}
			results = append(results, state.MatchResult{
				ID:      p.PoolName + "-" + strconv.Itoa(i),
				SideA:   m.SideA.Name,
				SideB:   m.SideB.Name,
				SideAID: m.SideA.ID,
				SideBID: m.SideB.ID,
				Status:  state.MatchStatusScheduled,
				Court:   poolCourts[i%len(poolCourts)],
				Round:   round,
				// ScheduledAt is populated by the caller
				// (assignPoolMatchSlotsFrom), uniform start times were
				// retired in T150.
			})
		}
	}

	if comp.Format == state.CompFormatLeague {
		// League scheduling (mp-sjaz): spread every player's matches so
		// nobody fights two slots in a row, and keep all courts in a slot
		// strictly time-aligned. This replaces the round-position court
		// assignment + per-court slot cursors used by the other single-pool
		// paths. The runtime simultaneity gate (checkSimultaneousMatchTx)
		// remains the defense-in-depth backstop at match start.
		// scheduleLeagueSlots treats an empty court list as one unnamed court.
		return scheduleLeagueSlots(results, comp.Courts)
	}
	// For single-pool multi-court, distribute each round's matches across
	// courts so that simultaneous matches never share a participant. When a
	// round has more matches than courts (e.g. 6 players / 2 courts gives 3
	// matches per round), the extra match is queued on the same court and runs
	// sequentially; the runtime simultaneity gate (checkSimultaneousMatchTx)
	// prevents double-booking at match start.
	if len(pools) == 1 && len(comp.Courts) > 1 {
		sort.SliceStable(results, func(i, j int) bool {
			return results[i].Round < results[j].Round
		})
		roundStart := 0
		currentRound := -1
		for i := range results {
			if results[i].Round != currentRound {
				currentRound = results[i].Round
				roundStart = 0
			}
			results[i].Court = comp.Courts[roundStart%len(comp.Courts)]
			roundStart++
		}
	}
	return results, nil
}

// numberPoolMatchesInPlayingOrder gives each pool's bouts their ids in the
// order they are saved, which is the order they are played (operator ruling
// 2026-10-04: one number on every surface, the playing order, with the id in
// sync). The league scheduler reorders the bouts after they are created so
// nobody fights twice in a row, which left "Pool A-1" as the first bout
// played: the court console called it "Match 1 of 6" while the server's
// label and the workbook, which read the id, called it Match 2. Numbering
// after every reorder, before anything stores an id, puts "Pool A-0" first.
func numberPoolMatchesInPlayingOrder(results []state.MatchResult) {
	next := make(map[string]int)
	for i := range results {
		// Every id here was built as "<pool>-<index>" above.
		pool, _ := poolNameFromMatchID(results[i].ID)
		results[i].ID = pool + "-" + strconv.Itoa(next[pool])
		next[pool]++
	}
}

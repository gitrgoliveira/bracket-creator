package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// EstimateScheduleForCompetition returns a pre-draw ScheduleEstimate for the
// competition identified by compID. It is a *Engine method (rather than a
// free function) because it needs the engine's store to load the competition,
// tournament, and participant roster — matching the pattern of other stateful
// engine entry points such as GenerateSchedule and StartCompetition.
// (EstimateForCounts is a free function because it takes pre-derived counts
// and has no store dependency.)
//
// Algorithm:
//  1. Load the competition; return *NotFoundError if it does not exist.
//  2. Load the tournament (nil is tolerated — EstimateForCounts and
//     ApplyTournamentDefaults both handle a nil *Tournament safely).
//  3. Derive the participant count:
//     - Normal competitions: len(filterCheckedIn(LoadParticipants(compID)))
//     when comp.CheckInEnabled (opt-in: full roster when nobody checked in).
//     - Source-linked playoffs (comp.SourceCompID != "" and comp has an
//     empty roster): derive from the SOURCE competition's pool count ×
//     SOURCE competition's PoolWinners (mirrors resolvePoolWinners/ranking.go).
//     If the source has no pools yet (draw not generated), returns a zero
//     ScheduleEstimate without an error — the estimate is unknown at that stage.
//  4. Map comp.Format → helper.EstimateMatchCountsInput and call
//     helper.EstimateMatchCounts to obtain pool and playoff counts.
//  5. Delegate to EstimateForCounts(poolCount, playoffCount, comp, tournament).
//
// Format mapping (state constant → helper format string):
//
//	state.CompFormatPlayoffs ("playoffs") → "playoffs"
//	state.CompFormatMixed    ("mixed")    → "mixed"
//	state.CompFormatLeague   ("league")   → "league"
//	state.CompFormatSwiss    ("swiss")    → "swiss"
//
// They are the same strings — the constants are defined in state/models.go and
// the helper uses the same literal values, so no translation is needed.
func (e *Engine) EstimateScheduleForCompetition(compID string) (ScheduleEstimate, error) {
	// Step 1: load competition.
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if comp == nil {
		return ScheduleEstimate{}, notFoundErrorf("competition %s not found", compID)
	}

	// Step 2: load tournament (nil is safe — EstimateForCounts handles it).
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return ScheduleEstimate{}, err
	}

	// Step 3: derive participant count.
	playerCount, err := e.estimateParticipantCount(comp)
	if err != nil {
		return ScheduleEstimate{}, err
	}
	if playerCount == 0 {
		// No participants yet (or source pools not generated) — return a zero
		// estimate rather than an error; the caller can display "—" or "unknown".
		return EstimateForCounts(0, 0, comp, tournament), nil
	}

	// Step 4: derive match counts via the Phase 1 helper.
	poolWinners := comp.PoolWinners
	if poolWinners <= 0 {
		poolWinners = 2 // mirrors engine/ranking.go:169 default
	}
	in := helper.EstimateMatchCountsInput{
		Format:       comp.Format,
		PlayerCount:  playerCount,
		PoolSize:     comp.PoolSize,
		PoolSizeMode: comp.PoolSizeMode,
		PoolWinners:  poolWinners,
		RoundRobin:   comp.RoundRobin,
		PoolFormat:   comp.PoolFormat,
		SwissRounds:  comp.SwissRounds,
	}
	poolCount, playoffCount, err := helper.EstimateMatchCounts(in)
	if err != nil {
		// EstimateMatchCounts errors are config-caused (unknown format, zero
		// pool size, player count < pool size) — surface as ValidationError
		// so the HTTP handler returns 400, not 500.
		return ScheduleEstimate{}, validationErrorf("%s", err)
	}

	// Step 5: delegate to EstimateForCounts.
	return EstimateForCounts(poolCount, playoffCount, comp, tournament), nil
}

// estimateParticipantCount returns the number of participants for a
// competition, handling the source-linked playoffs case and the check-in filter.
//
// Finding 1 fix: when comp.CheckInEnabled is true, the SAME filterCheckedIn
// opt-in semantics that runDrawPipeline applies (competition.go:565-567) are
// applied here. If at least one player is checked in, only checked-in players
// are counted; if nobody is checked in, the full roster is used (opt-in
// fallback). filterCheckedIn is called directly (same package) — the logic is
// NOT duplicated.
//
// For source-linked playoffs (comp.SourceCompID != "" and roster is empty),
// the participant count is derived from the SOURCE competition's pool count ×
// SOURCE comp.PoolWinners. This mirrors resolvePoolWinners in ranking.go, which
// uses len(pools) × winnersPerPool as the authoritative finalist count (not
// recomputed from participant count + pool size, because CreatePools may choose
// a different pool count in "min" vs "max" mode).
//
// If the source competition's pools have not been generated yet (len(pools)==0),
// returns (0, nil) — the caller treats this as "not enough data to estimate"
// and returns a zero ScheduleEstimate.
func (e *Engine) estimateParticipantCount(comp *state.Competition) (int, error) {
	// Source-linked playoffs: roster on disk is empty until StartCompetition.
	// Detect by checking SourceCompID and a zero-length roster (mirrors the
	// guard in runDrawPipeline: only auto-resolve when roster is empty).
	if comp.Format == state.CompFormatPlayoffs && comp.SourceCompID != "" {
		players, err := e.store.LoadParticipants(comp.ID, comp.WithZekkenName)
		if err != nil {
			return 0, err
		}
		if len(players) == 0 {
			// Roster not yet populated — derive from source pools.
			// Finding 2: estimateFinalistCount loads the source comp to get
			// the SOURCE's PoolWinners, not comp.PoolWinners.
			return e.estimateFinalistCount(comp.SourceCompID)
		}
		// Roster already populated (competition already started) — use it.
		// Apply check-in filter (Finding 1) consistently with runDrawPipeline.
		if comp.CheckInEnabled {
			players = filterCheckedIn(players)
		}
		return len(players), nil
	}

	// Normal path: load roster from disk.
	players, err := e.store.LoadParticipants(comp.ID, comp.WithZekkenName)
	if err != nil {
		return 0, err
	}
	// Finding 1: mirror runDrawPipeline's filterCheckedIn (competition.go:565-567).
	// Opt-in semantics: if nobody is checked in, full roster is returned unchanged.
	if comp.CheckInEnabled {
		players = filterCheckedIn(players)
	}
	return len(players), nil
}

// estimateFinalistCount returns the number of pool winners that will advance
// from a source competition (identified by srcCompID) to a linked playoffs
// competition.
//
// Finding 2 fix: the winners-per-pool comes from the SOURCE competition's
// PoolWinners field, not from the playoffs competition's PoolWinners. This
// mirrors resolvePoolWinners in ranking.go:168 which uses srcComp.PoolWinners.
//
// It reads:
//  1. The source competition config to get its PoolWinners (default 2 when ≤0).
//  2. The SOURCE competition's pools.csv for the authoritative pool count.
//
// Returns (0, nil) when the source has no pools yet.
func (e *Engine) estimateFinalistCount(srcCompID string) (int, error) {
	// Load the SOURCE competition to get its PoolWinners — not the playoffs
	// comp's PoolWinners (which is what the old code used via the parameter).
	srcComp, err := e.store.LoadCompetition(srcCompID)
	if err != nil {
		return 0, err
	}
	if srcComp == nil {
		// Source competition referenced by SourceCompID doesn't exist — this
		// is a misconfiguration (deleted or never created), not a normal
		// pre-draw state. Surface it as an error so the operator sees it.
		return 0, notFoundErrorf("playoffs source competition %q not found", srcCompID)
	}

	pools, err := e.store.LoadPools(srcCompID)
	if err != nil {
		return 0, err
	}
	if len(pools) == 0 {
		// Source draw not generated yet — can't estimate.
		return 0, nil
	}
	// Mirror resolvePoolWinners (ranking.go:168-171): use srcComp.PoolWinners,
	// default 2 when ≤0.
	winnersPerPool := srcComp.PoolWinners
	if winnersPerPool <= 0 {
		winnersPerPool = 2
	}
	return len(pools) * winnersPerPool, nil
}

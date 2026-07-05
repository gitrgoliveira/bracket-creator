package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// courtsEqual returns true when two court-label slices are
// element-wise equal (used by StartCompetition's mid-pipeline
// settings-drift check). nil and empty slices are treated as
// equivalent, both mean "no courts" from the config's POV.
func courtsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AutoCompleteOutcome is the result of a MaybeAutoCompletePools call.
type AutoCompleteOutcome int

const (
	// AutoCompleteNoChange means no transition occurred (matches still pending).
	AutoCompleteNoChange AutoCompleteOutcome = 0
	// AutoCompleteTransitioned means all matches finished and the competition
	// moved to CompStatusComplete. Callers should broadcast EventCompetitionCompleted.
	AutoCompleteTransitioned AutoCompleteOutcome = 1
	// AutoCompleteTiebreakInjected means all regular pool matches finished but
	// tied competitors were found. Supplementary ippon-shobu matches were injected
	// and the competition remains in CompStatusPools. Callers should broadcast
	// EventMatchUpdated and EventScheduleUpdated.
	AutoCompleteTiebreakInjected AutoCompleteOutcome = 2
	// AutoCompleteKnockoutStarted means the LAST pool of a mixed competition was
	// just seeded into the knockout bracket: every pool is now resolved, so the
	// competition moved CompStatusPools → CompStatusPlayoffs (only knockout
	// matches remain). Callers should broadcast EventCompetitionStarted and
	// EventScheduleUpdated.
	AutoCompleteKnockoutStarted AutoCompleteOutcome = 3
	// AutoCompletePoolsResolved means one or more (but not all) pools of a mixed
	// competition were just seeded into the knockout bracket, OR tiebreaker/DH
	// matches were injected. The competition stays in CompStatusPools while the
	// remaining pools run, but the bracket changed and knockout matches whose
	// both sides are now resolved have become SCOREABLE (scheduling is never
	// gated, court/time can be set on a placeholder match at any time). Callers
	// should broadcast EventMatchUpdated and EventScheduleUpdated.
	AutoCompletePoolsResolved AutoCompleteOutcome = 4
	// AutoCompleteAwaitingLeagueTiebreak means all regular team-league matches are
	// done and there is at least one consequential tie (a group of tied teams whose
	// position range intersects [1..LeagueTiebreakTopN], adjusted for the two-joint-
	// 3rd-places convention). The competition stays in CompStatusPools; the engine
	// did NOT auto-inject any DH matches. The operator must use the league-tiebreak
	// endpoints to either generate tie-breaker matches or accept shared ranks. Until
	// the operator acts, the competition cannot transition to CompStatusComplete.
	// Callers should broadcast both EventMatchUpdated (reload standings) and
	// EventScheduleUpdated (so the UI shows the "awaiting tie-breaker" banner).
	AutoCompleteAwaitingLeagueTiebreak AutoCompleteOutcome = 5
)

// MaybeAutoCompletePools advances a competition past its pool phase after a pool
// score:
//
//   - League format (individual) → transitions to CompStatusComplete once every
//     pool match is recorded as completed, with supplementary ippon-shobu
//     tiebreaker matches auto-injected for tied competitors.
//   - League format (team) → transitions to CompStatusComplete once every pool
//     match is done AND there are no consequential ties requiring a tie-breaker. If
//     there are consequential ties, AutoCompleteAwaitingLeagueTiebreak is returned
//     and the competition stays in CompStatusPools; NO DH matches are auto-injected.
//     The operator decides whether to run a tie-breaker (Phase 3b). If all ties are
//     non-consequential (below the tie-break band or covered by the two-thirds rule),
//     the league completes with shared ranks.
//   - Mixed format → delegates to advanceMixedPools, which seeds each COMPLETED
//     pool's finishers into the in-place knockout bracket incrementally (no
//     separate playoffs competition, no manual "start knockout" step), and flips
//     the competition to CompStatusPlayoffs once the LAST pool has been seeded.
//     Knockout matches become scoreable per-match as their feeder pools finish,
//     there is no wait for the whole pool phase.
//
// The function is a no-op for any other format or status.
//
// Atomic: the league status flip runs inside state.Store.UpdateCompetitionChanged.
// The mixed path delegates to advanceMixedPools, which takes its own per-comp
// locks; that is safe because MaybeAutoCompletePools is NOT inside an open
// transform at that point.
func (e *Engine) MaybeAutoCompletePools(compID string) (AutoCompleteOutcome, error) {
	// Determine whether this is a team competition for tie-injection routing.
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return AutoCompleteNoChange, err
	}

	// MIXED (Pools + Knockout): resolve incrementally, pool finishers drop into
	// their knockout slots the moment each pool completes, with NO wait for the
	// rest of the pool phase. Short-circuit BEFORE the comp-wide "all pools done"
	// gate below (which is only meaningful for league completion).
	if comp != nil && comp.Format == state.CompFormatMixed && comp.Status == state.CompStatusPools {
		return e.advanceMixedPools(compID, comp)
	}

	// Optional fast-path outside the lock, avoids taking the
	// per-comp write lock for the common "still in progress" case.
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return AutoCompleteNoChange, err
	}

	isTeamComp := comp != nil && comp.TeamSize > 0
	isTeamLeague := isTeamComp && comp.Format == state.CompFormatLeague

	// Partition matches into regular vs tiebreaker vs pool-daihyosen.
	allComplete := true
	hasIncompleteTB := false
	hasCompleteTB := false
	hasIncompleteDH := false
	hasCompleteDH := false
	for _, m := range matches {
		switch {
		case IsTiebreakerMatchID(m.ID):
			// A TB match without a winner (malformed payload) leaves standings
			// still tied, so treat it as unresolved just like DH.
			if m.Status != state.MatchStatusCompleted || m.Winner == "" {
				hasIncompleteTB = true
			} else {
				hasCompleteTB = true
			}
		case IsPoolDaihyosenMatchID(m.ID):
			// A DH match without a winner (e.g. hikiwake) leaves standings
			// still tied, so it must not count as resolved.
			if m.Status != state.MatchStatusCompleted || m.Winner == "" {
				hasIncompleteDH = true
			} else {
				hasCompleteDH = true
			}
		default:
			if m.Status != state.MatchStatusCompleted {
				allComplete = false
			}
		}
	}

	if !allComplete {
		return AutoCompleteNoChange, nil
	}
	if hasIncompleteTB || hasIncompleteDH {
		return AutoCompleteNoChange, nil
	}

	// All regular matches (and any existing TB/DH matches) are complete.
	//
	// Team-league path: the operator decides whether to run supplementary
	// tie-breaker matches, we do NOT auto-inject DH matches. Instead, block
	// completion while any CONSEQUENTIAL tied group still lacks an operator
	// tie-breaker, returning AwaitingLeagueTiebreak so the UI can surface the
	// decision.
	//
	// We check per-group, NOT via the coarse hasCompleteDH flag: a league
	// table can hold several separate consequential ties (e.g. 1st–2nd and
	// 3rd–4th). Resolving one group's DH would flip hasCompleteDH true and,
	// under a blanket `!hasCompleteDH` gate, let the competition complete with
	// the other group still unresolved. DH results are excluded from the Points
	// totals (scoring.go), so LeagueTiebreakCandidates keeps reporting a group
	// even after its DH is scored; we therefore treat a group as "actioned"
	// when a DH match exists for it (any DH is guaranteed complete here,
	// hasIncompleteDH bailed above) and hand the resolved/cyclic verdict to the
	// dhCycleExists guard below. LeagueTiebreakCandidates returns [] when the
	// operator has finalized shared ranks, so that path completes normally.
	//
	// Non-consequential ties (below the tie-break band, or covered by the
	// LeagueTwoThirdPlaces exemption) are accepted as shared ranks.
	if isTeamLeague {
		candidates, candErr := e.LeagueTiebreakCandidates(compID)
		if candErr != nil {
			return AutoCompleteNoChange, candErr
		}
		for _, g := range candidates {
			if !leagueGroupHasDH(g.Teams, matches) {
				// A consequential tie with no tie-breaker yet, operator must act.
				return AutoCompleteAwaitingLeagueTiebreak, nil
			}
		}
		// Every consequential group either has a DH (verified below) or none
		// remain; fall through to the dhCycleExists guard / completion.
	}

	// Non-team-league team competitions (mixed): auto-inject DH matches for
	// any ties. Individual competitions: auto-inject TB matches.
	// Skip for team-league (handled above) and when supplementary matches
	// already exist (hasCompleteDH / hasCompleteTB flags prevent double-inject).
	if !isTeamLeague {
		if (isTeamComp && !hasCompleteDH) || (!isTeamComp && !hasCompleteTB) {
			if isTeamComp {
				injected, injErr := e.InjectPoolDaihyosenMatches(compID)
				if injErr != nil {
					return AutoCompleteNoChange, injErr
				}
				if len(injected) > 0 {
					return AutoCompleteTiebreakInjected, nil
				}
			} else {
				injected, injErr := e.InjectTiebreakerMatches(compID)
				if injErr != nil {
					return AutoCompleteNoChange, injErr
				}
				if len(injected) > 0 {
					return AutoCompleteTiebreakInjected, nil
				}
			}
		}
	}

	// For team competitions where DH matches have been played (either via
	// operator-triggered league tie-breakers or auto-injected mixed/pools DH):
	// verify that the DH results actually broke all ties before transitioning.
	// In the rare event that DH bouts produce a cycle (A>B, B>C, C>A, only
	// possible in a 3+ team pool whose tie was consequential), every team in
	// that group still has equal DH win counts and standings remain unresolved.
	// Per the rules a still-level 3-4 way group goes to a further round of
	// supplementary ippon-shobu and ultimately chusen / drawing lots
	// (running_a_kendo_tournament.md:181); rather than seed the playoff from an
	// arbitrary order we block auto-completion until a decisive result exists.
	// Any pre-existing pool-rank overrides are still honoured by dhCycleExists.
	if isTeamComp && hasCompleteDH {
		standings, standErr := e.CalculatePoolStandings(compID)
		if standErr != nil {
			return AutoCompleteNoChange, standErr
		}
		overridesObj, _ := e.store.LoadOverrides(compID)
		var poolRanks map[string]map[string]int
		if overridesObj != nil {
			poolRanks = overridesObj.PoolRanks
		}
		if dhCycleExists(standings, matches, poolRanks) {
			return AutoCompleteNoChange, nil
		}
	}

	// No ties (or ties already resolved). Advance past pools.
	//
	// Only league reaches here (mixed short-circuited at the top to
	// advanceMixedPools; other formats are no-ops). League flips to
	// CompStatusComplete once every pool match is done.
	changed, err := e.store.UpdateCompetitionChanged(compID, func(comp *state.Competition) (*state.Competition, error) {
		if comp == nil || comp.Format != state.CompFormatLeague || comp.Status != state.CompStatusPools {
			return nil, nil
		}
		// Re-check under the lock.
		freshMatches, ferr := e.store.LoadPoolMatchesLocked(compID)
		if ferr != nil {
			return nil, ferr
		}
		for _, m := range freshMatches {
			if m.Status != state.MatchStatusCompleted {
				return nil, nil
			}
			if (IsPoolDaihyosenMatchID(m.ID) || IsTiebreakerMatchID(m.ID)) && m.Winner == "" {
				return nil, nil
			}
		}
		comp.Status = state.CompStatusComplete
		return comp, nil
	})
	if err != nil {
		return AutoCompleteNoChange, err
	}
	if changed {
		return AutoCompleteTransitioned, nil
	}
	return AutoCompleteNoChange, nil
}

// advanceMixedPools drives the incremental Pools → Knockout flow for a mixed
// competition after a pool score. It is gate-free: each pool's finishers are
// seeded into their knockout slots as soon as that pool completes, regardless of
// whether the other pools are still running. Concretely:
//
//  1. Inject tiebreaker/DH matches for any newly-tied pools (idempotent).
//  2. Seed every COMPLETED pool's finishers into the bracket placeholders
//     (ResolveQualifiedPools), newly-playable knockout matches were already
//     scheduled at draw time, so they become live immediately.
//  3. When the final pool has been seeded (no pool placeholders remain), flip
//     CompStatusPools → CompStatusPlayoffs (informational, knockout matches are
//     already playable per-match during "pools").
//
// Outcomes: AutoCompleteKnockoutStarted (last pool seeded, status flipped),
// AutoCompletePoolsResolved (some pools seeded and/or tiebreakers injected,
// bracket/schedule changed), or AutoCompleteNoChange (nothing new this score).
func (e *Engine) advanceMixedPools(compID string, comp *state.Competition) (AutoCompleteOutcome, error) {
	// 1. Inject tie-break matches for tied pools (idempotent; per-tied-pool).
	injected := 0
	if comp.TeamSize > 0 {
		m, err := e.InjectPoolDaihyosenMatches(compID)
		if err != nil {
			return AutoCompleteNoChange, err
		}
		injected = len(m)
	} else {
		m, err := e.InjectTiebreakerMatches(compID)
		if err != nil {
			return AutoCompleteNoChange, err
		}
		injected = len(m)
	}

	// 2. Seed every completed pool into the bracket (no all-pools gate).
	resolvedNow, allResolved, err := e.ResolveQualifiedPools(compID)
	if err != nil {
		return AutoCompleteNoChange, err
	}

	// 3. Flip pools → playoffs once every pool is seeded.
	if allResolved {
		changed, cerr := e.store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			if c == nil || c.Format != state.CompFormatMixed || c.Status != state.CompStatusPools {
				return nil, nil
			}
			c.Status = state.CompStatusPlayoffs
			return c, nil
		})
		if cerr != nil {
			return AutoCompleteNoChange, cerr
		}
		if changed {
			return AutoCompleteKnockoutStarted, nil
		}
	}

	if resolvedNow > 0 || injected > 0 {
		return AutoCompletePoolsResolved, nil
	}
	return AutoCompleteNoChange, nil
}

// leagueGroupHasDH reports whether a daihyosen tie-breaker match already exists
// between two members of the given tied group, i.e. the operator has run a
// tie-breaker for it. Used by MaybeAutoCompletePools to decide, per consequential
// group, whether the operator still needs to act. At that call site every DH
// match is guaranteed complete (an incomplete DH bails earlier), so "a DH match
// exists" means "the operator has actioned this group"; whether that tie-breaker
// actually resolved the order is then verified by dhCycleExists.
func leagueGroupHasDH(group []state.PlayerStanding, allMatches []state.MatchResult) bool {
	names := make(map[string]bool, len(group))
	for _, s := range group {
		names[s.Player.Name] = true
	}
	for _, m := range allMatches {
		if IsPoolDaihyosenMatchID(m.ID) && names[m.SideA] && names[m.SideB] {
			return true
		}
	}
	return false
}

// dhCycleExists reports whether any tied group is still unresolved after its
// daihyosen bouts (a cycle / all-drawn), i.e. it needs a chusen. Delegates the
// per-group check to groupNeedsChusen (the same predicate ChusenCandidates uses
// to surface those groups to the operator). Below-cut ties never block: DH
// matches are injected only for advancement-affecting groups, so a below-cut
// group has no DH bouts and groupNeedsChusen returns false. When it does return
// true the operator resolves the group via the chusen (drawing lots) panel,
// which writes poolRanks (pool name -> team name -> rank); a group whose every
// member has an override is resolved and no longer blocks completion.
func dhCycleExists(standings map[string][]state.PlayerStanding, allMatches []state.MatchResult, poolRanks map[string]map[string]int) bool {
	for poolName, poolStandings := range standings {
		for _, positions := range detectPoolTies(poolStandings) {
			group := standingsAt(poolStandings, positions)
			if groupNeedsChusen(group, allMatches, poolRanks[poolName]) {
				return true
			}
		}
	}
	return false
}

// StartCompetition starts a competition. When called on a draw-ready
// competition it transitions directly to running (no regeneration).
// When called on a setup competition it generates the draw first then
// transitions, preserving the single-click "Start" UX.
//
// See runDrawPipeline for pipeline limitations (pre-existing).
func (e *Engine) StartCompetition(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}
	switch comp.Status {
	case state.CompStatusDrawReady:
		// Draw already exists; only flip status and generate schedule.
		if err := e.transitionDrawToRunning(id); err != nil {
			return err
		}
		return e.GenerateSchedule(id)
	case state.CompStatusSetup, "":
		// One-click path: generate draw then transition.
		if err := e.runDrawPipeline(id); err != nil {
			return err
		}
		if err := e.transitionDrawToRunning(id); err != nil {
			return err
		}
		return e.GenerateSchedule(id)
	default:
		return validationErrorf("competition %s already started", id)
	}
}

// GenerateDraw generates pools/bracket/Swiss-r1 for a Setup competition
// and transitions it to CompStatusDrawReady without starting the competition.
// The operator can preview, discard, and regenerate the draw before calling
// StartCompetition to enter the running state.
func (e *Engine) GenerateDraw(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}
	switch comp.Status {
	case state.CompStatusSetup, "":
		return e.runDrawPipeline(id)
	case state.CompStatusDrawReady:
		return validationErrorf("competition %s draw already generated; discard it first to regenerate", id)
	default:
		return validationErrorf("competition %s cannot generate draw (status: %s)", id, comp.Status)
	}
}

// DiscardDraw discards the generated draw for a draw-ready competition,
// deleting the draw artifacts and resetting the competition to Setup.
// Returns an error when the competition is not in draw-ready state.
//
// Ordering rationale: files are deleted BEFORE the status flip. While the
// competition is still draw-ready, GenerateDraw rejects new requests, so no
// concurrent caller can generate fresh artifacts during the deletion window.
// If we flipped to Setup first, a concurrent GenerateDraw could start,
// write new artifacts, commit draw-ready, and then our deferred deletes
// would erase the freshly generated files, leaving draw-ready with no
// artifacts.
func (e *Engine) DiscardDraw(id string) error {
	// Pre-check: verify draw-ready status before touching the filesystem.
	// This prevents deleting artifacts from a running competition when the
	// caller is wrong. The authoritative check is the status CAS below; this
	// early return is the common-case guard.
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}
	if comp.Status != state.CompStatusDrawReady {
		return validationErrorf("competition %s is not in draw-ready state (status: %s)", id, comp.Status)
	}

	// Delete draw artifacts while status is still draw-ready (verified above).
	// GenerateDraw rejects draw-ready state, so no concurrent GenerateDraw can
	// write new artifacts here. The final status CAS below is the authoritative
	// guard.
	for _, f := range []string{"pools.csv", "pool-matches.csv", "bracket.json"} {
		if err := e.store.DeleteCompetitionFile(id, f); err != nil {
			return fmt.Errorf("DiscardDraw: failed to delete %s: %w", f, err)
		}
	}
	_, err = e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found", id)
		}
		if current.Status != state.CompStatusDrawReady {
			return nil, validationErrorf("competition %s is not in draw-ready state (status: %s)", id, current.Status)
		}
		current.Status = state.CompStatusSetup
		current.SwissCurrentRound = 0 // reset so a fresh GenerateDraw can re-initialise it
		return current, nil
	})
	return err
}

// transitionDrawToRunning atomically moves a draw-ready competition to
// the appropriate running status (Pools or Playoffs) based on its format.
func (e *Engine) transitionDrawToRunning(id string) error {
	_, err := e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found (deleted during start)", id)
		}
		if current.Status != state.CompStatusDrawReady {
			return nil, validationErrorf("competition %s not in draw-ready state (status: %s); concurrent modification?", id, current.Status)
		}
		switch current.Format {
		case state.CompFormatMixed, state.CompFormatLeague, state.CompFormatSwiss:
			current.Status = state.CompStatusPools
		default:
			current.Status = state.CompStatusPlayoffs
		}
		return current, nil
	})
	return err
}

// filterCheckedIn applies the mp-w7x check-in filter with opt-in semantics:
// if at least one player is checked in, return only the checked-in players;
// if nobody is checked in, the operator never used check-in for this
// competition, so return the roster unchanged. This guarantees the filter can
// never silently empty the field. The input slice is not mutated.
func filterCheckedIn(players []domain.Player) []domain.Player {
	anyCheckedIn := false
	for _, p := range players {
		if p.CheckedIn {
			anyCheckedIn = true
			break
		}
	}
	if !anyCheckedIn {
		return players
	}
	eligible := make([]domain.Player, 0, len(players))
	for _, p := range players {
		if p.CheckedIn {
			eligible = append(eligible, p)
		}
	}
	return eligible
}

// checkInExcludedNames returns the names of players that filterCheckedIn would
// remove under opt-in semantics: the non-checked-in players when at least one
// is checked in, else nil. Used to prune their seed assignments so ApplySeeds
// doesn't fail on an absent player.
//
// The key is the raw, case-sensitive player Name, matching the roster identity
// the draw uses (helper.CheckDuplicateEntries and ApplySeeds' playerMap both key
// on the exact Name, so "Alice" and "alice" are distinct participants).
// Normalizing case here would let an excluded "alice" drop the seed of a
// checked-in "Alice" (PR #199 review round 3).
func checkInExcludedNames(players []domain.Player) map[string]bool {
	anyCheckedIn := false
	for _, p := range players {
		if p.CheckedIn {
			anyCheckedIn = true
			break
		}
	}
	if !anyCheckedIn {
		return nil
	}
	excluded := make(map[string]bool)
	for _, p := range players {
		if !p.CheckedIn {
			excluded[p.Name] = true
		}
	}
	return excluded
}

// dropSeedAssignments removes seed assignments whose participant name is in the
// excluded set, matched case-sensitively on the exact Name (same key as
// checkInExcludedNames / the roster identity). A nil/empty excluded set returns
// the input unchanged, so a seed for a name that was never a participant still
// flows through to ApplySeeds and surfaces the same error as before.
func dropSeedAssignments(seeds []domain.SeedAssignment, excluded map[string]bool) []domain.SeedAssignment {
	if len(excluded) == 0 {
		return seeds
	}
	out := make([]domain.SeedAssignment, 0, len(seeds))
	for _, a := range seeds {
		if excluded[a.Name] {
			continue
		}
		out = append(out, a)
	}
	return out
}

// runDrawPipeline runs the full draw-generation pipeline for a Setup
// competition and commits CompStatusDrawReady. It does NOT generate the
// schedule; callers must call GenerateSchedule after transitioning to a
// running status.
//
// Pipeline limitations (pre-existing):
//   - Pool/bracket generation (writes pools.csv / bracket.json) runs
//     OUTSIDE the comp-config lock. Two concurrent GenerateDraw calls
//     could overwrite each other's pools.csv before the atomic Status
//     commit serializes them. Left as a follow-up.
//   - SaveParticipants also has its own lock acquisition. A failure
//     mid-pipeline leaves partial state on disk.
func (e *Engine) runDrawPipeline(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}

	// Snapshot the loaded config BEFORE the pipeline mutates anything.
	// The atomic-commit transform below compares `current` (freshly
	// reloaded under the lock) to THESE snapshots, not to the
	// post-pipeline `comp`. Why: the pipeline applies an auto-default
	// to comp.TeamSize (0 → 5 for team competitions) below. Comparing
	// current.TeamSize to comp.TeamSize would falsely report "admin
	// changed TeamSize during start" whenever the default was applied.
	// Comparing current.TeamSize to the SNAPSHOT (loaded value before
	// the default) correctly distinguishes "admin's concurrent change"
	// from "our pipeline's default". Same shape for any future field
	// the pipeline mutates pre-commit.
	loadedFormat := comp.Format
	loadedPoolSize := comp.PoolSize
	loadedPoolSizeMode := comp.PoolSizeMode
	loadedNumberPrefix := comp.NumberPrefix
	loadedStartTime := comp.StartTime
	loadedRoundRobin := comp.RoundRobin
	loadedKind := comp.Kind
	loadedWithZekken := comp.WithZekkenName
	loadedCourts := append([]string(nil), comp.Courts...)
	loadedTeamSize := comp.TeamSize
	loadedCheckInEnabled := comp.CheckInEnabled
	loadedPoolWinners := comp.PoolWinners
	// Note: PoolWinners drives generatePoolPreviewBracket for mixed-format
	// competitions (mp-9dz: the preview bracket leaf-count equals PoolWinners
	// per pool). It is therefore snapshotted and validated in the atomic commit
	// below, but ONLY for mixed format, for other formats it still doesn't
	// drive generation, so admin's concurrent change is preserved by leaving
	// current.PoolWinners alone. Mirror (export-only), Name, Date, Venue are
	// still NOT snapshotted (UI-only, never read during generation).
	//
	// Roster/seed mtimes. Settings drift is detected via the field-by-
	// field snapshot above; participants and seeds live in separate
	// files and have no per-field snapshot, so use the file mtime as a
	// fingerprint. A concurrent AdminParticipants PUT between our outer
	// Load and the atomic commit below changes participants.csv mtime,
	// if we don't detect it, our generated pools.csv / bracket.json
	// reflect a stale roster while participants.csv on disk has the new
	// one. The transform below aborts the start with a validation error
	// when either file's mtime changed; the operator retries against
	// the fresh roster. FileMtime returns 0 if the file does not exist,
	// which is a valid "no participants yet" state, we snapshot the
	// same 0 and the comparison still works.
	loadedParticipantsMtime := e.store.FileMtime(id, "participants.csv")
	loadedSeedsMtime := e.store.FileMtime(id, "seeds.csv")

	if comp.Kind == "team" && comp.TeamSize == 0 {
		comp.TeamSize = 5 // Default for Kendo
	}

	// Only pass HasIDs hint when explicitly true; false means unset (let
	// auto-detect run) to avoid misclassifying UUID-prefixed files.
	var hasIDsHint *bool
	if comp.HasParticipantIDs {
		t := true
		hasIDsHint = &t
	}
	players, err := e.store.LoadParticipantsOpt(id, comp.WithZekkenName, state.LoadParticipantsOpts{
		WithSeeds: true,
		HasIDs:    hasIDsHint,
	})
	if err != nil {
		return err
	}
	// Exclude participants who have not checked in (mp-w7x), but ONLY when the
	// competition has check-in tracking enabled (comp.CheckInEnabled). The rest
	// of the stack masks checkedIn behind this flag (the viewer derives
	// checkedIn as `checkInEnabled && p.checkedIn`), so a stale/imported
	// checked_in marker on a competition that doesn't use check-in must not
	// silently shrink the field (PR #199 review). When enabled, opt-in
	// semantics still apply (see filterCheckedIn): if nobody is checked in,
	// everyone is included.
	//
	// excludedByCheckIn captures the names check-in removes so we can drop
	// their seed assignments too: helper.ApplySeeds errors with "seeded
	// participant not found in main list" for a seed whose player is absent,
	// which would make a competition with a non-checked-in seeded player
	// undrawable.
	var excludedByCheckIn map[string]bool
	if comp.CheckInEnabled {
		excludedByCheckIn = checkInExcludedNames(players)
		players = filterCheckedIn(players)
	}

	if len(players) == 0 {
		return validationErrorf("no participants found for competition %s", id)
	}

	seeds, err := e.store.LoadSeeds(id)
	if err != nil {
		return err
	}
	// Drop seed assignments for participants removed by check-in (PR #199
	// review) so ApplySeeds doesn't fail on an absent seeded player. Remaining
	// seeds keep their ranks; sparse ranks are handled by the seeding pass.
	seeds = dropSeedAssignments(seeds, excludedByCheckIn)

	// League format: enforce the single-pool invariant so that
	// generatePools always produces exactly one pool containing all
	// participants, and round-robin is guaranteed. The viewer surface
	// relies on pools[0] being the only pool. PoolSize and RoundRobin
	// may hold any admin-configured value at this point; override them
	// here so the pipeline and the atomic commit below agree.
	if comp.Format == state.CompFormatLeague {
		comp.PoolSize = len(players)
		comp.RoundRobin = true
	}

	// Generate Pools, Bracket, or Swiss round-1. These calls write to other
	// files (pools.csv / bracket.json / pool-matches.csv) via their own
	// per-comp lock acquisitions, so they run OUTSIDE the
	// UpdateCompetitionChanged transform below (re-entering the lock would
	// deadlock).
	switch comp.Format {
	case state.CompFormatMixed, state.CompFormatLeague:
		if err := e.generatePools(comp, players, seeds); err != nil {
			return err
		}
		// mp-9dz: a mixed (Pools + Knockout) competition feeds a knockout
		// bracket. Generate a PREVIEW bracket (pool-origin placeholder
		// leaves) so the operator sees the elimination structure on the
		// source competition at draw time, mirroring the Excel Tree sheet.
		// League has no knockout stage, so skip it there.
		if comp.Format == state.CompFormatMixed {
			if err := e.generatePoolPreviewBracket(comp); err != nil {
				return err
			}
		}
		comp.Status = state.CompStatusDrawReady
	case state.CompFormatSwiss:
		// Guard 1: SwissCurrentRound already bumped, AdvanceSwissRound ran to
		// completion before StartCompetition was called.
		if comp.SwissCurrentRound != 0 {
			return validationErrorf("competition %s Swiss round %d already generated; cannot start again", id, comp.SwissCurrentRound)
		}
		// Guard 2: pool-matches.csv has matches that have already been acted on
		// (at least one is non-scheduled). AdvanceSwissRound may have written
		// round-1 matches before its UpdateCompetitionChanged round-bump failed,
		// leaving SwissCurrentRound=0 while the CSV has scored/running content.
		// We only block when scoring has started, purely-scheduled entries are
		// safe to overwrite on a retry (StartCompetition itself could have written
		// them in a previous call that then failed inside UpdateCompetitionChanged,
		// and the operator retrying should not be permanently stuck).
		if existing, loadErr := e.store.LoadPoolMatches(id); loadErr != nil {
			return loadErr
		} else {
			for _, m := range existing {
				if m.Status != "" && m.Status != state.MatchStatusScheduled {
					return validationErrorf("competition %s already has scored Swiss matches on disk (match %s status=%s); cannot start again", id, m.ID, m.Status)
				}
			}
		}
		r1, err := e.GenerateSwissRound(id, 1)
		if err != nil {
			return err
		}
		if err := e.store.SavePoolMatches(id, r1); err != nil {
			return err
		}
		comp.SwissCurrentRound = 1
		comp.Status = state.CompStatusDrawReady
	default:
		// A standalone playoffs competition (the only remaining playoffs case
		// after the derived-playoffs path was removed in mp-turx) uses standalone
		// seeding, there is no pool-preview topology to mirror.
		if err := e.generatePlayoffs(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = state.CompStatusDrawReady
	}

	// Atomic commit of the modified competition. The transform
	// re-validates Status under the per-comp lock, if a concurrent
	// StartCompetition won the race and already moved Status to
	// Pools/Playoffs, we abort here with a validation error rather
	// than clobbering their result with ours.
	//
	// The transform ALSO re-validates the generation-relevant fields
	// (Format, PoolSize, PoolSizeMode, NumberPrefix, StartTime,
	// RoundRobin, Kind, WithZekkenName, Courts, the exact set
	// listed in the validation block below). If a concurrent settings
	// save changed any of those between our outer Load (the basis
	// for the pools/playoffs files we just generated) and this atomic
	// commit, the generated artifacts no longer match the new config,
	// e.g. a Format change from "mixed" to "playoffs" would leave
	// pools.csv on disk while Status committed to "playoffs". Better
	// to abort with a 409-style conflict than to commit inconsistent
	// state. Note: TeamSize and PoolWinners are deliberately NOT in
	// this set, see the inline comment on the validation block for
	// the rationale.
	//
	// Note: our generated pools.csv / bracket.json have already been
	// written by this point (see pipeline limitations in the function
	// comment), aborting here leaves them as orphaned artifacts that
	// the next successful start overwrites. Pre-existing partial-
	// atomicity issue; the fix here only guarantees comp-config
	// consistency.
	_, err = e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found (deleted during start)", id)
		}
		if current.Status != state.CompStatusSetup && current.Status != "" {
			return nil, validationErrorf("competition %s started concurrently by another writer", id)
		}
		// Generation-relevant fields must match the SNAPSHOT we
		// generated from (loaded* values captured before the pipeline
		// mutated anything). The list is EXACTLY what
		// generatePools / generatePlayoffs read:
		//   - Format (decides which generator)
		//   - PoolSize, PoolSizeMode, RoundRobin (pools structure)
		//   - NumberPrefix (player numbering in both generators)
		//   - StartTime (initial ScheduledAt for generated matches)
		//   - Courts (court labels assigned to generated matches)
		//   - Kind / WithZekkenName (participants loading)
		//   - CheckInEnabled (decides which participants are included)
		// Other config fields (TeamSize, Name, Date, Venue, Mirror) are NOT
		// validated, they don't drive generation, so admin's concurrent
		// change to them doesn't invalidate the pools.csv / bracket.json we
		// just wrote. Their values are preserved by leaving `current.X` alone
		// in the transform (except TeamSize, see below).
		//
		// PoolWinners is validated below for mixed format only: it drives the
		// preview bracket leaf-count (generatePoolPreviewBracket, mp-9dz), so
		// a concurrent change would produce a bracket.json whose shape
		// disagrees with the committed comp.PoolWinners. For other formats it
		// remains non-generation-relevant and is left unvalidated.
		if current.Format != loadedFormat ||
			current.PoolSize != loadedPoolSize ||
			current.PoolSizeMode != loadedPoolSizeMode ||
			current.NumberPrefix != loadedNumberPrefix ||
			current.StartTime != loadedStartTime ||
			current.RoundRobin != loadedRoundRobin ||
			current.Kind != loadedKind ||
			current.WithZekkenName != loadedWithZekken ||
			current.CheckInEnabled != loadedCheckInEnabled ||
			!courtsEqual(current.Courts, loadedCourts) {
			return nil, validationErrorf("competition %s configuration changed during start (Format/PoolSize/PoolSizeMode/NumberPrefix/StartTime/RoundRobin/Kind/WithZekkenName/CheckInEnabled/Courts); regenerate by retrying", id)
		}
		if loadedFormat == state.CompFormatMixed && current.PoolWinners != loadedPoolWinners {
			return nil, validationErrorf("competition %s PoolWinners changed during start; regenerate by retrying", id)
		}
		// Participants / seeds drift: detected via file mtime captured
		// at outer Load. A concurrent AdminParticipants PUT between our
		// outer Load and this point would change the file mtime; without
		// this check our generated pools/bracket reflect the stale roster
		// while participants.csv on disk has the new one. Stat is
		// lock-free, so calling FileMtime inside the transform is safe
		// (no deadlock against the per-comp lock UpdateCompetitionChanged
		// holds).
		//
		// Serialization vs concurrent writers: both SaveParticipants and
		// SaveSeeds now acquire the per-comp write lock that
		// UpdateCompetitionChanged holds, so a concurrent write of
		// either file BLOCKS until the transform commits. That closes
		// the race between mtime-check and status-commit (previously
		// SaveSeeds took the store-wide s.mu, a different mutex from
		// the per-comp lock, leaving a microsecond window where a seed
		// save could land between our check and our commit, persisting
		// status=Pools alongside seeds.csv content the engine never
		// read). See seeds.go for the locking-strategy rationale.
		//
		// Remaining caveat: a write that lands AFTER this check but
		// BEFORE the transform commit still races with our pipeline.
		// The mtime check shrinks the window from "outer Load → commit"
		// which is acceptable in practice (microseconds of CPU +
		// filesystem latency).
		if e.store.FileMtime(id, "participants.csv") != loadedParticipantsMtime ||
			e.store.FileMtime(id, "seeds.csv") != loadedSeedsMtime {
			return nil, validationErrorf("competition %s participants or seeds changed during start; retry", id)
		}
		// TeamSize handling: not in the drift validation above
		// because it doesn't drive generation. If admin DIDN'T
		// concurrently change it (current == loaded), apply our
		// pipeline's value, which may be the loaded value unchanged
		// OR the auto-default 5 for team comps that started with 0.
		// If admin DID concurrently change it (current != loaded),
		// preserve their change, leaving current.TeamSize alone.
		// Pre-fix this line was `current.TeamSize = comp.TeamSize`
		// unconditionally, which clobbered admin's concurrent change
		// AND the validation list (including TeamSize) rejected the
		// race instead of merging. Both were wrong: the right answer
		// is to merge admin's concurrent change with our pipeline's
		// default in the no-drift direction only.
		if current.TeamSize == loadedTeamSize {
			current.TeamSize = comp.TeamSize
		}
		// League format: mirror the single-pool invariant applied above
		// (comp.PoolSize = len(players), comp.RoundRobin = true) into the
		// persisted config. Same merge logic as TeamSize: if admin didn't
		// concurrently change the field (current == loaded), apply our
		// pipeline's overridden value; if they did, the conflict check
		// above already returned an error before we reach this block, so
		// the guard is always true here, it's kept for symmetry.
		if comp.Format == state.CompFormatLeague {
			if current.PoolSize == loadedPoolSize {
				current.PoolSize = comp.PoolSize
			}
			if current.RoundRobin == loadedRoundRobin {
				current.RoundRobin = true
			}
		}
		current.Status = comp.Status
		if comp.Format == state.CompFormatSwiss {
			current.SwissCurrentRound = comp.SwissCurrentRound
		}
		// HasParticipantIDs is auto-managed (saveCompetitionWithPlayers
		// sets it to true when Players is non-empty) and not exposed in
		// the admin UI as an editable field. Pre-fix this was an
		// UNCONDITIONAL restore from the outer-Load snapshot
		// (current.HasParticipantIDs = comp.HasParticipantIDs), which
		// reverted any concurrent PUT that flipped the flag to true
		// (e.g. AdminParticipants persisting a UUID roster in parallel
		// with this start). Combined with the no-roster-rewrite path
		// NOT rewriting participants.csv, the result was a UUID file
		// on disk paired with a HasParticipantIDs=false metadata flag,
		// and the list-view's HasIDs hint would then misparse the
		// UUID as part of each player's Name.
		//
		// The participants/seeds drift check above already aborts the
		// start when participants.csv mtime changed, so a concurrent
		// PUT is rejected before we reach this point. Defense in depth:
		// preserve the fresh `current.HasParticipantIDs` (loaded inside
		// the transform) by NOT overwriting it from the snapshot.
		return current, nil
	})
	if err != nil {
		return err
	}

	return nil
}

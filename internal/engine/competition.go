package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// courtsEqual returns true when two court-label slices are
// element-wise equal (used by StartCompetition's mid-pipeline
// settings-drift check). nil and empty slices are treated as
// equivalent — both mean "no courts" from the config's POV.
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
)

// MaybeAutoCompletePools transitions a pools-format competition from
// CompStatusPools to CompStatusComplete when every pool match has been
// recorded as completed. It is a no-op for any other format or status,
// or when at least one pool match is still scheduled/running.
//
// When all regular pool matches are done but tied competitors remain,
// supplementary ippon-shobu tiebreaker matches are injected and
// AutoCompleteTiebreakInjected is returned instead of transitioning.
//
// Atomic: the status check and the save run inside
// state.Store.UpdateCompetitionChanged so a concurrent
// invalidate-vs-auto-complete pair can't lose either mutation.
func (e *Engine) MaybeAutoCompletePools(compID string) (AutoCompleteOutcome, error) {
	// Optional fast-path outside the lock — avoids taking the
	// per-comp write lock for the common "still in progress" case.
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return AutoCompleteNoChange, err
	}

	// Determine whether this is a team competition for tie-injection routing.
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return AutoCompleteNoChange, err
	}
	isTeamComp := comp != nil && comp.TeamSize > 0

	// Partition matches into regular vs tiebreaker vs pool-daihyosen.
	allComplete := true
	hasIncompleteTB := false
	hasCompleteTB := false
	hasIncompleteDH := false
	hasCompleteDH := false
	for _, m := range matches {
		switch {
		case IsTiebreakerMatchID(m.ID):
			if m.Status != state.MatchStatusCompleted {
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
	// If no supplementary matches exist yet, check for ties and inject.
	//
	// Concurrent callers are safe: the injection functions load fresh state
	// and use existingPairs guards, so parallel goroutines produce identical
	// content. SavePoolMatches is a full overwrite — last write wins but the
	// data is the same, making concurrent injection idempotent.
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

	// For team competitions where DH matches have been played: verify that
	// the DH results actually broke all ties before transitioning.  In the
	// rare event that DH bouts produce a cycle (A>B, B>C, C>A — only
	// possible in a 3+ team pool with a full round-robin DH), every team in
	// that group still has equal DH win counts and standings remain
	// unresolved.  Per tournament practice the pool would normally be
	// replayed; here we block auto-completion so the operator can apply
	// manual rank overrides via the admin UI rather than seeding playoffs
	// from an arbitrary order.
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

	// No ties (or ties already resolved). Transition to complete.
	changed, err := e.store.UpdateCompetitionChanged(compID, func(comp *state.Competition) (*state.Competition, error) {
		if comp == nil || (comp.Format != state.CompFormatPools && comp.Format != state.CompFormatLeague) || comp.Status != state.CompStatusPools {
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
			if IsPoolDaihyosenMatchID(m.ID) && m.Winner == "" {
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

// dhCycleExists reports whether any pool still has a tied group that DH
// results did not fully resolve. This catches the cyclic case (A>B, B>C,
// C>A) where every team ends up with the same DH win count inside the
// group. When true, auto-completion is blocked; the operator must use
// manual rank overrides (or physically replay the pool).
//
// poolRanks is the operator's manual rank override map (keyed by pool
// name → team name → rank). A tied group whose every member has a
// manual rank override is considered resolved — the operator has
// explicitly ranked them, so the cycle no longer blocks completion.
func dhCycleExists(standings map[string][]state.PlayerStanding, allMatches []state.MatchResult, poolRanks map[string]map[string]int) bool {
	for poolName, poolStandings := range standings {
		for _, group := range detectPoolTies(poolStandings) {
			// If the operator has manually ranked every member of this
			// tied group, treat the cycle as resolved.
			if overrides := poolRanks[poolName]; len(overrides) > 0 {
				allOverridden := true
				for _, s := range group {
					if _, ok := overrides[s.Player.Name]; !ok {
						allOverridden = false
						break
					}
				}
				if allOverridden {
					continue
				}
			}
			groupNames := make(map[string]bool, len(group))
			for _, s := range group {
				groupNames[s.Player.Name] = true
			}
			dhWins := make(map[string]int, len(group))
			dhPlayed := false
			for _, m := range allMatches {
				if !IsPoolDaihyosenMatchID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
					continue
				}
				if groupNames[m.SideA] && groupNames[m.SideB] {
					dhWins[m.Winner]++
					dhPlayed = true
				}
			}
			if !dhPlayed {
				continue
			}
			seen := make(map[int]bool, len(group))
			for _, s := range group {
				count := dhWins[s.Player.Name]
				if seen[count] {
					return true
				}
				seen[count] = true
			}
		}
	}
	return false
}

// StartCompetition runs the competition-start pipeline: validate
// status, load participants/seeds, generate pools or bracket, commit
// the new Status atomically, save participants, generate schedule.
//
// The final comp-status commit is wrapped in
// state.Store.UpdateCompetitionChanged so two concurrent
// StartCompetition calls can't both write the new Status (or, more
// importantly, one's "start" can't clobber the other's already-applied
// status change). The status re-check inside the transform aborts if
// another writer moved Status off Setup between our outer Load and
// the atomic commit.
//
// Pipeline limitations (pre-existing, NOT addressed by this fix):
//   - Pool/bracket generation (writes pools.csv / bracket.json) runs
//     OUTSIDE the comp-config lock. Two concurrent starts could each
//     generate and overwrite each other's pools.csv before the
//     atomic Status commit serializes them. The later start's
//     pools.csv wins; users see the second start's player ordering.
//     A full fix would require holding the comp lock across the
//     entire pipeline, which would conflict with the generator's
//     internal use of the same lock for pools.csv / bracket.json
//     writes. Left as a follow-up — needs a deeper refactor that
//     either threads "lock already held" through the generators or
//     restructures the lock granularity.
//   - SaveParticipants + GenerateSchedule (steps after the comp
//     commit) also have their own lock acquisitions. A failure
//     mid-pipeline leaves partial state on disk. Pre-existing.
func (e *Engine) StartCompetition(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return notFoundErrorf("competition %s not found", id)
	}

	// Best-effort early validation outside the lock — fast-fails the
	// obviously-not-startable case (admin clicks start twice). The
	// authoritative re-check is inside the atomic commit below.
	if comp.Status != state.CompStatusSetup && comp.Status != "" {
		return validationErrorf("competition %s already started", id)
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
	// Note: PoolWinners is intentionally NOT snapshotted. The
	// validation block below excludes it because it doesn't drive
	// pool/bracket generation — admin's concurrent change is
	// preserved by leaving current.PoolWinners alone. Same applies
	// to Mirror (export-only), Name, Date, Venue (all UI-only).
	//
	// Roster/seed mtimes. Settings drift is detected via the field-by-
	// field snapshot above; participants and seeds live in separate
	// files and have no per-field snapshot, so use the file mtime as a
	// fingerprint. A concurrent AdminParticipants PUT between our outer
	// Load and the atomic commit below changes participants.csv mtime
	// — if we don't detect it, our generated pools.csv / bracket.json
	// reflect a stale roster while participants.csv on disk has the new
	// one. The transform below aborts the start with a validation error
	// when either file's mtime changed; the operator retries against
	// the fresh roster. FileMtime returns 0 if the file does not exist,
	// which is a valid "no participants yet" state — we snapshot the
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
	if len(players) == 0 {
		return validationErrorf("no participants found for competition %s", id)
	}

	seeds, err := e.store.LoadSeeds(id)
	if err != nil {
		return err
	}

	// Resolve any cross-competition reserved slots before generation.
	// The returned `mutated` flag tells us whether the function actually
	// changed the players slice (in-place field update OR placeholder
	// removal). We gate the trailing SaveParticipants on this flag: if
	// nothing was mutated, the players slice still matches disk
	// byte-for-byte, so re-saving it is wasted I/O AND a participant-
	// race risk (a concurrent admin participants upload between our
	// outer Load and the trailing save would be clobbered by our stale
	// snapshot). Deriving the flag from the function's actual mutation
	// (rather than an outer LoadReservedSlots call) avoids a TOCTOU
	// window where the outer check sees no slots but resolveReservedSlots
	// then sees them under a race.
	var resolvedSlots bool
	players, resolvedSlots, err = e.resolveReservedSlots(id, players)
	if err != nil {
		return err
	}

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

	// Generate Pools or Bracket. These calls write to other files
	// (pools.csv / bracket.json) via their own per-comp lock
	// acquisitions, so they run OUTSIDE the UpdateCompetitionChanged
	// transform below (re-entering the lock would deadlock).
	if comp.Format == state.CompFormatPools || comp.Format == state.CompFormatLeague {
		if err := e.generatePools(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = state.CompStatusPools
	} else {
		if err := e.generatePlayoffs(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = state.CompStatusPlayoffs
	}

	// Atomic commit of the modified competition. The transform
	// re-validates Status under the per-comp lock — if a concurrent
	// StartCompetition won the race and already moved Status to
	// Pools/Playoffs, we abort here with a validation error rather
	// than clobbering their result with ours.
	//
	// The transform ALSO re-validates the generation-relevant fields
	// (Format, PoolSize, PoolSizeMode, NumberPrefix, StartTime,
	// RoundRobin, Kind, WithZekkenName, Courts — the exact set
	// listed in the validation block below). If a concurrent settings
	// save changed any of those between our outer Load (the basis
	// for the pools/playoffs files we just generated) and this atomic
	// commit, the generated artifacts no longer match the new config
	// — e.g. a Format change from "pools" to "playoffs" would leave
	// pools.csv on disk while Status committed to "playoffs". Better
	// to abort with a 409-style conflict than to commit inconsistent
	// state. Note: TeamSize and PoolWinners are deliberately NOT in
	// this set — see the inline comment on the validation block for
	// the rationale.
	//
	// Note: our generated pools.csv / bracket.json have already been
	// written by this point (see pipeline limitations in the function
	// comment) — aborting here leaves them as orphaned artifacts that
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
		// Other config fields (TeamSize, PoolWinners, Name, Date, Venue,
		// Mirror) are NOT validated — they don't drive generation, so
		// admin's concurrent change to them doesn't invalidate the
		// pools.csv / bracket.json we just wrote. Their values are
		// preserved by leaving `current.X` alone in the transform
		// (except TeamSize, see below).
		if current.Format != loadedFormat ||
			current.PoolSize != loadedPoolSize ||
			current.PoolSizeMode != loadedPoolSizeMode ||
			current.NumberPrefix != loadedNumberPrefix ||
			current.StartTime != loadedStartTime ||
			current.RoundRobin != loadedRoundRobin ||
			current.Kind != loadedKind ||
			current.WithZekkenName != loadedWithZekken ||
			!courtsEqual(current.Courts, loadedCourts) {
			return nil, validationErrorf("competition %s configuration changed during start (Format/PoolSize/PoolSizeMode/NumberPrefix/StartTime/RoundRobin/Kind/WithZekkenName/Courts); regenerate by retrying", id)
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
		// SaveSeeds took the store-wide s.mu — a different mutex from
		// the per-comp lock — leaving a microsecond window where a seed
		// save could land between our check and our commit, persisting
		// status=Pools alongside seeds.csv content the engine never
		// read). See seeds.go for the locking-strategy rationale.
		//
		// Remaining caveat: a write that lands AFTER this check but
		// BEFORE the trailing SaveParticipants (resolvedSlots path)
		// still races with our pipeline. That window remains because
		// SaveParticipants takes the same per-comp lock that the
		// transform holds, so it can't be folded inside. The mtime
		// check shrinks the window from "outer Load → trailing save"
		// to "transform commit → trailing save," which is acceptable
		// in practice (microseconds of CPU + filesystem latency).
		if e.store.FileMtime(id, "participants.csv") != loadedParticipantsMtime ||
			e.store.FileMtime(id, "seeds.csv") != loadedSeedsMtime {
			return nil, validationErrorf("competition %s participants or seeds changed during start; retry", id)
		}
		// TeamSize handling: not in the drift validation above
		// because it doesn't drive generation. If admin DIDN'T
		// concurrently change it (current == loaded), apply our
		// pipeline's value — which may be the loaded value unchanged
		// OR the auto-default 5 for team comps that started with 0.
		// If admin DID concurrently change it (current != loaded),
		// preserve their change — leaving current.TeamSize alone.
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
		// the guard is always true here — it's kept for symmetry.
		if comp.Format == state.CompFormatLeague {
			if current.PoolSize == loadedPoolSize {
				current.PoolSize = comp.PoolSize
			}
			if current.RoundRobin == loadedRoundRobin {
				current.RoundRobin = true
			}
		}
		current.Status = comp.Status
		// HasParticipantIDs is auto-managed (saveCompetitionWithPlayers
		// sets it to true when Players is non-empty) and not exposed in
		// the admin UI as an editable field. Pre-fix this was an
		// UNCONDITIONAL restore from the outer-Load snapshot
		// (current.HasParticipantIDs = comp.HasParticipantIDs), which
		// reverted any concurrent PUT that flipped the flag to true
		// (e.g. AdminParticipants persisting a UUID roster in parallel
		// with this start). Combined with the no-reserved-slot branch
		// NOT rewriting participants.csv, the result was a UUID file
		// on disk paired with a HasParticipantIDs=false metadata flag
		// — and the list-view's HasIDs hint would then misparse the
		// UUID as part of each player's Name.
		//
		// The participants/seeds drift check above already aborts the
		// start when participants.csv mtime changed, so a concurrent
		// PUT is rejected before we reach this point. Defense in depth:
		// preserve the fresh `current.HasParticipantIDs` (loaded inside
		// the transform) by NOT overwriting it from the snapshot. The
		// resolvedSlots branch below still upgrades to true when our
		// pipeline rewrites the roster with UUIDs — that path is the
		// only legitimate reason to flip the flag here.
		// HasParticipantIDs flip for the resolvedSlots path is DEFERRED
		// to AFTER SaveParticipants below — pre-fix, this transform
		// flipped the flag to true, but if the trailing SaveParticipants
		// then failed (disk full, EISDIR, etc.), the config carried
		// HasParticipantIDs=true while participants.csv retained the
		// OLD non-UUID format. On next load, the HasIDs-hinted parser
		// would misparse the file (UUID extraction on non-UUID rows).
		// Deferral ensures the (flag, file) pair stays consistent.
		return current, nil
	})
	if err != nil {
		return err
	}

	// See `resolvedSlots` flag from resolveReservedSlots above. Skip the
	// save when the pipeline didn't mutate the roster (no reserved-slot
	// resolution happened); otherwise persist the resolved IDs/names.
	if resolvedSlots {
		if err := e.store.SaveParticipants(id, players); err != nil {
			return err
		}
		// Deferred HasParticipantIDs flip — runs ONLY after the
		// participants file lands successfully with UUID-prefixed rows.
		// See the transform above for the bug-shape comment.
		if _, fierr := e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
			if current == nil {
				return nil, nil
			}
			current.HasParticipantIDs = true
			return current, nil
		}); fierr != nil {
			// Log only — the file save succeeded (which is the
			// load-bearing write). A stale `false` flag at this point
			// is safe because EVERY reader site uses the conditional
			// hint pattern (only pass &true when comp.HasParticipantIDs;
			// otherwise nil → LoadParticipantsOpt auto-detects from the
			// first line's UUID prefix). Sites: handlers_viewer.go list
			// (line ~45) and detail (line ~101), and StartCompetition
			// itself (line ~183). Aborting the start here after a
			// successful save would commit Status (transform above
			// already ran) but surface a 500 to the operator — they'd
			// retry and hit "already started," leaving the competition
			// in a confusing half-started state.
			fmt.Printf("Warning: failed to flip HasParticipantIDs after SaveParticipants: %v\n", fierr)
		}
	}

	return e.GenerateSchedule(id)
}


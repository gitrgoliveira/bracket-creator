package engine

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// isUnresolvedBracketSide reports whether a bracket side is still a forward
// reference rather than a resolved competitor: an empty structural bye slot, a
// "Winner of rX-mY" feeder, or a pool-origin finalist placeholder that has not
// been seeded yet (its feeder pool is still in progress).
//
// The two placeholder patterns are defined authoritatively in
// helper.IsReservedParticipantName (mp-igdg); knockout.go no longer
// duplicates them.
func isUnresolvedBracketSide(side string) bool {
	if side == "" {
		return true
	}
	return helper.IsReservedParticipantName(side)
}

// bracketMatchPlayable reports whether a bracket match can be scored: both sides
// must be resolved competitors. This is the per-match replacement for the old
// bracket-wide Preview gate, a knockout match becomes playable as soon as both
// its feeder pools (or feeder matches) have produced real competitors, with NO
// wait for the rest of the pool phase. Standalone (knockout-only) competitions
// satisfy this from draw time because their round-1 leaves are real players.
func bracketMatchPlayable(m *state.BracketMatch) bool {
	return !isUnresolvedBracketSide(m.SideA) && !isUnresolvedBracketSide(m.SideB)
}

// bracketHasPoolPlaceholders reports whether any side anywhere in the bracket is
// still an unseeded pool-origin finalist placeholder. Used to decide when every
// pool has been folded into the knockout (status pools → knockout).
func bracketHasPoolPlaceholders(b *state.Bracket) bool {
	if b == nil {
		return false
	}
	for _, round := range b.Rounds {
		for _, m := range round {
			if helper.IsPoolFinalistPlaceholder(m.SideA) || helper.IsPoolFinalistPlaceholder(m.SideB) {
				return true
			}
		}
	}
	return false
}

// resolvedFinisher pairs a pool finisher's display name with their
// participant id, the value ResolveQualifiedPools' label→finisher resolver
// maps a bracket placeholder ("Pool A-1st") onto (bc-brid). The zero value
// (both fields empty) represents a degenerate pool's unfillable rank, mapped
// to a bye exactly as a bare "" name used to be.
type resolvedFinisher struct {
	Name, ID string
}

// completedPoolNames returns poolName → isComplete for every pool in compID,
// after first injecting any supplementary tie-break bouts a newly tied pool
// needs (comp-wide, idempotent), so a pool that just became tied flips to "not
// complete" on this very call. The judgement itself is poolCompletion's; this
// is the bare-store door, which may write (the injectors do).
func (e *Engine) completedPoolNames(compID string, comp *state.Competition) (map[string]bool, error) {
	if comp != nil && comp.TeamSize > 0 {
		if _, err := e.InjectPoolDaihyosenMatches(compID); err != nil {
			return nil, err
		}
	} else {
		if _, err := e.InjectTiebreakerMatches(compID); err != nil {
			return nil, err
		}
	}
	return e.poolCompletion(e.store, compID, comp, func() (map[string][]state.PlayerStanding, error) {
		return e.CalculatePoolStandings(compID)
	})
}

// poolCompletion returns poolName → isComplete for every pool in compID. A
// pool is complete when all of its matches (regular + any tiebreaker/daihyosen)
// are completed with a winner and, for team competitions, its daihyosen
// results actually broke the ties (no cycle).
//
// It only READS, through loader, so a transaction can ask it about the state
// its own uncommitted write produced (pool_requalify.go); completedPoolNames is
// the bare-store door that injects first. It does NOT ask whether a tie-break
// is still owed but not yet injected: inside a transaction nothing has been
// injected, so a caller there must also ask pendingTieBreaks. standings is
// consulted only for a team competition, and lazily, so the bare-store door
// can keep reading the standings cache.
func (e *Engine) poolCompletion(loader poolStandingsLoader, compID string, comp *state.Competition, standings func() (map[string][]state.PlayerStanding, error)) (map[string]bool, error) {
	isTeam := comp != nil && comp.TeamSize > 0

	pools, err := loader.LoadPools(compID)
	if err != nil {
		return nil, err
	}
	matches, err := loader.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	playerCount := make(map[string]int, len(pools))
	done := make(map[string]bool, len(pools))
	seen := make(map[string]bool, len(pools))
	for _, p := range pools {
		done[p.PoolName] = true // optimistic; cleared below on any incomplete match
		playerCount[p.PoolName] = len(p.Players)
	}
	for _, m := range matches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		seen[pn] = true
		complete := m.Status == state.MatchStatusCompleted
		if (IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID)) && m.Winner == "" {
			complete = false
		}
		if !complete {
			if _, known := done[pn]; known {
				done[pn] = false
			}
		}
	}
	// A pool with NO matches on disk is "complete" ONLY when it has exactly one
	// participant: round-robin (and partial) match generation skips pools of size
	// 0/1, so a lone qualifier legitimately produces zero matches and is already
	// decided (they are the pool's 1st place). A 0-participant pool, or a ≥2-player
	// pool with no matches yet (draw not generated / mid-generation), is NOT
	// complete, otherwise the mixed comp could get stuck in `pools` forever (a
	// single-competitor pool's placeholder would never resolve).
	for pn := range done {
		if !seen[pn] {
			done[pn] = playerCount[pn] == 1
		}
	}

	// Team competitions: a pool whose daihyosen results produced a cycle (ties
	// not broken) must not be treated as resolvable.
	if isTeam {
		st, serr := standings()
		if serr != nil {
			return nil, serr
		}
		overrides, oerr := e.store.LoadOverrides(compID)
		if oerr != nil {
			return nil, oerr
		}
		var poolRanks map[string]map[string]int
		if overrides != nil {
			poolRanks = overrides.PoolRanks
		}
		for pn, ok := range done {
			if !ok {
				continue
			}
			scoped := map[string][]state.PlayerStanding{pn: st[pn]}
			if dhCycleExists(scoped, matches, poolRanks) {
				done[pn] = false
			}
		}
	}
	return done, nil
}

// qualifierResolver maps each placeholder label a completed pool can fill
// ("Pool A-1st", up to MatchWinnerRanksNeeded) onto the finisher now holding
// that rank. Incomplete pools contribute nothing, so their placeholders survive
// untouched. The ONE statement of "who a label means", shared by
// ResolveQualifiedPools and the in-transaction requalification planner.
//
// MatchWinnerRanksNeeded, not poolWinners: under the extra-qualifier modes
// (bc-qual) the bracket seats "Pool X-2nd" placeholders while PoolWinners is
// pinned to 1, so a resolver bounded by poolWinners never builds a -2nd key and
// those slots stay placeholders forever -- every pool done, the drafted/crossed
// matches never playable, the competition stuck in pools status. Keys for pools
// that send no 2nd are inert: the resolver is only ever consulted with the
// labels the draw actually seated.
//
// resolvedFinisher carries both the display name and the participant id
// (bc-brid), so a bracket slot's identity survives the resolution, not just its
// label.
func qualifierResolver(pools []helper.Pool, completed map[string]bool, standings map[string][]state.PlayerStanding, ranksNeeded int) map[string]resolvedFinisher {
	resolver := make(map[string]resolvedFinisher)
	for _, pool := range pools {
		if !completed[pool.PoolName] {
			continue
		}
		ps := standings[pool.PoolName]
		for rank := 1; rank <= ranksNeeded; rank++ {
			key := qualifierLabel(pool.PoolName, rank)
			if rank-1 >= len(ps) {
				// Degenerate pool (hand-edited data / legacy import): fewer
				// finishers than PoolWinners. Map the unfillable placeholder
				// to "" (bye) so the bracket slot auto-resolves. Draw-time
				// validation prevents this in supported flows.
				log.Printf("engine: pool %q has only %d ranked finisher(s) but placeholders may reference %d rank(s); treating rank %d as bye", pool.PoolName, len(ps), ranksNeeded, rank)
				resolver[key] = resolvedFinisher{}
				continue
			}
			p := ps[rank-1].Player
			resolver[key] = resolvedFinisher{Name: p.Name, ID: p.ID}
		}
	}
	return resolver
}

// qualifierLabel is the placeholder a pool's rank holds in the draw ("Pool
// A-1st"), the key both resolvers look slots up by.
func qualifierLabel(poolName string, rank int) string {
	return fmt.Sprintf("%s-%s", poolName, helper.GetOrdinal(rank))
}

// qualifierLabelPool is qualifierLabel's inverse for the pool half ("Pool
// A-East-2nd" -> "Pool A-East"): the ordinal never holds a hyphen, so the last
// one separates the two, whatever hyphens the pool name has.
func qualifierLabelPool(label string) string {
	if i := strings.LastIndexByte(label, '-'); i > 0 {
		return label[:i]
	}
	return label
}

// resolveSlots writes resolver's finishers into every bracket slot whose
// draw-time label (PlaceholderA/B/Winner) is a resolver key, then completes and
// propagates any bye that leaves. It does no I/O, so it serves both the
// bare-store resolver and a transaction. Returns how many sides changed.
//
// A slot in a match that is RUNNING, or closed with a result of its own
// (bracketMatchCarriesOwnResult), is never repainted here: someone is fighting
// it, or it was fought by the competitor already in it, and silently swapping
// the name above that verdict is the defect the requalification planner exists
// to prevent. That planner reopens such a match (with the operator's
// confirmation) BEFORE calling this, so on its path nothing is left to skip,
// and so does the pool-rank override door (OverridePoolRank). The skip is the
// safety net for the bare-store door, where standings can still move without
// either: a pool file edited by hand, or DELETE .../overrides (the corrupt-file
// repair door) clearing a chusen. It is all-or-nothing per pool: one locked
// slot that would change freezes every slot of its pool, so no door can ever
// half-apply a pool's new order. A skipped slot is logged, never silently
// dropped.
func (e *Engine) resolveSlots(bracket *state.Bracket, resolver map[string]resolvedFinisher) int {
	n := 0
	isLocked := func(m *state.BracketMatch) bool {
		return m.Status == state.MatchStatusRunning || bracketMatchCarriesOwnResult(m)
	}
	// The safety net is all-or-nothing PER POOL. Skipping only the locked slot
	// while repainting the pool's others seats one competitor twice: a
	// 1st/2nd swap kept A1 in the played "Pool A-1st" match and painted A1
	// into the scheduled "Pool A-2nd" one as well. So a pool any of whose
	// slots is locked AND would get a different competitor (occupantChanges:
	// an id backfill moves nobody and freezes nothing) is FROZEN, and none of
	// its slots is written. frozen maps the pool to the first match that froze
	// it, for the one log line per pool below.
	frozen := map[string]string{}
	scanLocked := func(m *state.BracketMatch) {
		if !isLocked(m) {
			return
		}
		for _, s := range [...]struct{ label, name, id string }{
			{m.PlaceholderA, m.SideA, m.SideAID},
			{m.PlaceholderB, m.SideB, m.SideBID},
			{m.PlaceholderWinner, m.Winner, m.WinnerID},
		} {
			rf, ok := resolver[s.label]
			if !ok || (s.name == rf.Name && s.id == rf.ID) || !occupantChanges(s.name, s.id, rf) {
				continue
			}
			if pool := qualifierLabelPool(s.label); frozen[pool] == "" {
				frozen[pool] = m.ID
			}
		}
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			scanLocked(&bracket.Rounds[ri][mi])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		scanLocked(bracket.ThirdPlaceMatch)
	}
	frozenPools := make([]string, 0, len(frozen))
	for pool := range frozen {
		frozenPools = append(frozenPools, pool)
	}
	sort.Strings(frozenPools)
	for _, pool := range frozenPools {
		log.Printf("engine: no knockout slot of %s is repainted: match %s is running or has its own result and would get a different competitor, and repainting the pool's other slots alone would seat one competitor twice; the pool's correction must go through the requalification planner, which reopens that match first", pool, frozen[pool])
	}
	resolveMatch := func(m *state.BracketMatch) {
		locked := isLocked(m)
		// PlaceholderA/B/Winner hold the ORIGINAL draw labels (or
		// "Winner of …"/""), stable across re-scores. Only completed-pool
		// placeholders are resolver keys; "Winner of" and "" never are, so
		// already-scored knockout sides and unresolved feeders are untouched.
		// Compare against the current (name, id) pair so an unchanged
		// re-run is a no-op, and so a bracket resolved once BEFORE bc-brid
		// (name only, id still "") gets its id backfilled the next time its
		// pool's placeholder is looked at.
		paint := func(label string, name, id *string) {
			rf, ok := resolver[label]
			if !ok || (*name == rf.Name && *id == rf.ID) {
				return
			}
			if frozen[qualifierLabelPool(label)] != "" {
				return // logged once for the pool above
			}
			if locked {
				log.Printf("engine: bracket match %s (%s) keeps %q in its %s slot: it is running or has its own result, so the new occupant %q is not written over it", m.ID, m.Status, *name, label, rf.Name)
				return
			}
			*name, *id = rf.Name, rf.ID
			n++
		}
		paint(m.PlaceholderA, &m.SideA, &m.SideAID)
		paint(m.PlaceholderB, &m.SideB, &m.SideBID)
		// Winner-only changes count too, so a bye-propagated Winner fix is persisted.
		paint(m.PlaceholderWinner, &m.Winner, &m.WinnerID)
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			resolveMatch(&bracket.Rounds[ri][mi])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		// Inert in every current draw: the bronze is fed by semifinal
		// losers, so its placeholders are empty and no lookup can hit.
		// Present so a bronze that ever DOES carry one is not the single
		// slot the resolver skips.
		resolveMatch(bracket.ThirdPlaceMatch)
	}
	// Auto-complete newly created bye matches: a resolver mapping to ""
	// (degenerate pool) leaves a match with one empty side still
	// Scheduled. Mirror buildBracketFromDraw's bye logic and
	// propagate winners so downstream matches become playable.
	// Guard: only auto-complete when the non-empty side is a resolved
	// competitor, NOT a still-unresolved placeholder (its feeder pool
	// may still be in progress).
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			m := &bracket.Rounds[ri][mi]
			if m.Status != state.MatchStatusScheduled {
				continue
			}
			aEmpty := m.SideA == ""
			bEmpty := m.SideB == ""
			aResolved := !aEmpty && !isUnresolvedBracketSide(m.SideA)
			bResolved := !bEmpty && !isUnresolvedBracketSide(m.SideB)
			if aEmpty && bResolved {
				m.Winner = m.SideB
				m.WinnerID = m.SideBID
				m.Status = state.MatchStatusCompleted
				e.propagateBracketWinner(bracket, ri, mi)
				n++
			} else if bEmpty && aResolved {
				m.Winner = m.SideA
				m.WinnerID = m.SideAID
				m.Status = state.MatchStatusCompleted
				e.propagateBracketWinner(bracket, ri, mi)
				n++
			} else if aEmpty && bEmpty {
				m.Status = state.MatchStatusCompleted
				e.propagateBracketWinner(bracket, ri, mi)
				n++
			}
		}
	}
	return n
}

// ResolveQualifiedPools incrementally seeds the in-place knockout bracket of a
// mixed (Pools + Knockout) competition. For EVERY pool whose results are final
// it writes that pool's real finishers into the bracket slots their finalist
// placeholders ("Pool A-1st", …) occupy, and resolves any bye those finishers
// inherit. Pools still in progress keep their placeholders. There is NO all-pools
// gate: a knockout match becomes playable the moment both its feeder pools have
// finished, while other pools are still running.
//
// Resolution is RE-SEEDABLE, not a one-shot string replace. Each match carries
// the label its slot held at draw time (PlaceholderA/B/Winner, written once by
// buildBracketFromDraw), and THAT is what the resolver keys on. So if an
// operator re-scores a completed pool match after that pool was already seeded,
// changing the 1st/2nd finisher, the new finisher overwrites the stale name in
// the same slot instead of being silently dropped: the live side no longer holds
// the placeholder string, but the match's own record of it is immutable. The
// bracket's court/time slots are assigned at draw time and never change here,
// only competitor labels.
//
// It used to RECOMPUTE that template instead, rerunning the whole draw and
// buildBracketFromDraw on every call and matching it against the live
// bracket BY POSITION. That was correct only while the placement algorithm never
// changed: an operator who upgraded between a competition's draw and the end of
// its pool phase (ordinary for a two-day event) would have had qualifiers written
// into the WRONG slots of a live knockout, with nothing to detect it — the
// structural guards it carried only caught differing round/match COUNTS, which a
// placement change does not alter. Reading the persisted placeholder removes the
// dependency on the draw algorithm entirely, for this transition and every future
// one, and takes a full bracket rebuild off a path that runs on every pool-match
// completion. Do not reintroduce the recompute.
//
// A bracket drawn BEFORE those fields existed carries none, which is exactly how
// such a file is detected; it is reconstructed once from the frozen v1 builder
// (legacy_template_v1.go) and the fields are backfilled and saved on first use.
//
// It never repaints a slot whose match is running or carries its own result
// (resolveSlots' safety net): a pool CORRECTION that moves a qualifier out from
// under such a match is caught inside the correcting write's own transaction
// (planRequalification), which warns and, confirmed, reopens it. What still
// reaches this door after the fact (a pool-rank override, a league tie-break
// finalize) leaves that one slot as it is and logs it.
//
// Returns (resolvedNow, allResolved): how many bracket sides changed THIS call,
// and whether the bracket now has zero pool-origin placeholders left (every pool
// seeded). No-op (0, false, nil) for non-mixed competitions, standalone knockout
// brackets carry no pool placeholders.
//
// Identity (bc-brid): the resolver (qualifierResolver) writes both a
// competitor's display NAME and their participant id into the bracket slot,
// closing the seam bc-cse's identity work left open
// -- BracketMatch now persists per-side ids (state/models.go's
// BracketMatch.SideAID doc), so two same-name-different-dojo pool finishers
// qualifying into the same knockout stay distinguishable from here on.
func (e *Engine) ResolveQualifiedPools(compID string) (int, bool, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return 0, false, err
	}
	if comp == nil || comp.Format != state.CompFormatMixed {
		return 0, false, nil
	}

	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return 0, false, err
	}
	// Mixed requires ≥2 pools by invariant (enforced at draw in generatePools);
	// defend against legacy/hand-edited data so we never seed a degenerate
	// single-pool "knockout".
	if len(pools) < 2 {
		return 0, false, validationErrorf("mixed competition %s has only %d pool(s), at least 2 are required for a knockout phase; this competition should be 'league' format", compID, len(pools))
	}

	completed, err := e.completedPoolNames(compID, comp)
	if err != nil {
		return 0, false, err
	}
	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return 0, false, err
	}
	poolWinners := comp.EffectivePoolWinners()
	resolver := qualifierResolver(pools, completed, standings, comp.MatchWinnerRanksNeeded())

	resolvedNow := 0
	allResolved := false
	backfilled := false
	uerr := e.store.UpdateBracket(compID, func(bracket *state.Bracket) error {
		if bracket == nil || len(bracket.Rounds) == 0 {
			return errMatchNotFound // nothing to resolve; signal no-save
		}
		// Pre-Phase-4 bracket: no slot remembers its draw-time label. Rebuild
		// them ONCE from the frozen v1 draw and write them in, so from here on
		// this competition resolves off its own record like any new bracket.
		// The write is part of this same UpdateBracket mutation, so a backfill
		// is never persisted without the resolution it enabled, nor lost by a
		// call that resolved nothing (see the save condition below).
		if !bracketHasDrawPlaceholders(bracket) {
			// Built here rather than alongside the resolver above: this is the
			// only reader, and the branch fires at most once per competition
			// (never at all for a bracket drawn on this version), while
			// ResolveQualifiedPools itself runs on every pool-match score.
			poolNames := make([]string, len(pools))
			for i, p := range pools {
				poolNames[i] = p.PoolName
			}
			// poolWinners, not ranksNeeded: a v1 bracket predates the
			// extra-qualifier modes by construction (they shipped after the
			// placeholder fields), so its geometry is always the uniform
			// pools-times-winners layout this backfill reconstructs.
			backfilled = backfillDrawPlaceholdersV1(bracket, poolNames, poolWinners)
		}
		n := e.resolveSlots(bracket, resolver)

		allResolved = !bracketHasPoolPlaceholders(bracket)
		if n == 0 && !backfilled {
			return errMatchNotFound // no effective change → skip the rewrite
		}
		resolvedNow = n
		if n > 0 {
			// The bracket is now (partially) live; the legacy global Preview flag
			// is obsolete, playability is per-match from here on. A backfill-only
			// call resolved nothing, so it must not flip this.
			bracket.Preview = false
		}
		return nil
	})
	if uerr != nil && !errors.Is(uerr, errMatchNotFound) {
		return 0, false, uerr
	}
	return resolvedNow, allResolved, nil
}

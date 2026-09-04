package engine

import (
	"fmt"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N"). Suffix-anchored
// via hasNumericSuffixAfter (daihyosen.go) for the same reason as its
// IsPoolDaihyosenMatchID sibling: a plain substring match would misclassify a
// regular match in a pool whose name happens to contain "-TB-".
func IsTiebreakerMatchID(matchID string) bool {
	return hasNumericSuffixAfter(matchID, "-TB-")
}

// teamStandingPoints and individualStandingPoints compute the single packed
// ranking score for a standing. The packing encodes the full ORDERED tiebreak
// chain into one integer, so comparing the score is equivalent to comparing
// each criterion in priority order: two competitors with equal scores are tied
// on every official criterion (and a difference in any criterion, however far
// down the chain, produces different scores). This is the single source of
// truth for both the standings sort (scoring.go) and tie detection
// (detectPoolTies), so the two can never disagree on what "tied" means.
//
// Team chain (CLAUDE.md): W, L, T, IV (individual victories), IL, IT,
// PW (points won), PL (points lost).
func teamStandingPoints(s state.PlayerStanding) int {
	return s.Wins*100_000_000_000 - s.Losses*1_000_000_000 + s.Draws*10_000_000 +
		s.IndividualWins*100_000 - s.IndividualLosses*10_000 + s.IndividualDraws*1_000 +
		s.PointsWon*100 - s.PointsLost
}

// individualStandingPoints packs the individual chain: W, L, D, ippons given,
// ippons taken.
func individualStandingPoints(s state.PlayerStanding) int {
	return s.Wins*100_000_000 - s.Losses*1_000_000 + s.Draws*10_000 + s.IpponsGiven*100 - s.IpponsTaken
}

// detectPoolTies walks a sorted (descending Points) standings slice and returns
// the POSITIONS of every group of 2+ competitors that share the same Points
// value. Each inner slice holds the 0-based positions (indices into the passed
// sorted standings) of one tied group, top-to-bottom; e.g. [][]int{{1,2}} means
// the 2nd- and 3rd-placed competitors are tied, and [][]int{{0,1},{3,4}} means
// two separate tied groups. The result is empty (len 0) when there are no ties.
//
// Points encodes the full ordered tiebreak chain (see teamStandingPoints /
// individualStandingPoints), so equal Points means genuinely tied on every
// official criterion, for both team and individual competitions. The caller
// MUST pass standings already sorted by Points descending; the returned indices
// point straight back into that slice.
func detectPoolTies(standings []state.PlayerStanding) [][]int {
	var groups [][]int
	i := 0
	for i < len(standings) {
		j := i + 1
		for j < len(standings) && standings[j].Points == standings[i].Points {
			j++
		}
		if j-i >= 2 {
			g := make([]int, 0, j-i)
			for k := i; k < j; k++ {
				g = append(g, k)
			}
			groups = append(groups, g)
		}
		i = j
	}
	return groups
}

// standingsAt resolves a position group from detectPoolTies back into the
// standings it indexes, preserving order. Out-of-range positions are skipped
// defensively (the caller always passes indices straight from detectPoolTies
// over the same slice, so this is a guard, not an expected path).
func standingsAt(standings []state.PlayerStanding, positions []int) []state.PlayerStanding {
	group := make([]state.PlayerStanding, 0, len(positions))
	for _, idx := range positions {
		if idx >= 0 && idx < len(standings) {
			group = append(group, standings[idx])
		}
	}
	return group
}

// applyTiebreakSort re-orders each tied group in `sorted` (in place) by per-group
// win count from the supplementary bouts whose ID satisfies isSupplementaryID
// (TB ippon-shobu or DH representative). Tied groups are located via
// detectPoolTies, the single source of the Points-equality walk, so the two
// callers (TB, DH) share one implementation. Win counts are scoped to bouts
// between members of the same tied group, so an unrelated group's results never
// bleed across; a group with no decided supplementary bouts is left untouched.
//
// Group membership and win attribution are keyed by standingsPlayerKey
// (id-preferring, name fallback): two tied competitors can share a display
// name across dojos (CheckDuplicateEntriesByNameDojo), and a bare-name key
// would both misclassify group membership (a same-named, unrelated
// competitor elsewhere in the pool falsely counted as "in this group") and
// cross-attribute a supplementary win between the two namesakes.
// generateTiebreakerMatches / generatePoolDaihyosenMatches stamp
// SideAID/SideBID at generation so the id half is actually populated for
// TB/DH rows going forward.
//
// The sort comparator below recomputes each element's key FRESH from
// sorted[i+a]/sorted[i+b] on every call rather than caching a
// position->key/count mapping once: sort.SliceStable physically swaps
// elements as it sorts, so a fixed "this position had N wins" cache goes
// stale the moment two elements it describes are swapped, and the comparator
// silently starts comparing the wrong pair of counts (verified: an earlier
// version of this function cached original-position keys and produced a
// wrong final order on exactly the fixture in
// TestApplyTiebreakSort_SameNameDifferentDojo). Reading sorted[i+a] directly
// always reflects whoever CURRENTLY occupies that slot, so it can't go
// stale.
//
// A match side is resolved to a group member's canonical key via
// resolveGroupMatchKey: when the match carries an id for that side, the key
// is symmetric (both a member's own key and a match's believed key for the
// same id compute to the identical "id:<uuid>" string, so no lookup table is
// needed for that path -- only membership in this specific tied group is
// verified). When the match carries no id for that side (a pre-fix TB/DH
// row, or any other legacy data), resolution falls back to a name lookup
// built from the group's own members; a genuine same-name collision with NO
// id on the match to disambiguate degrades to the same "last one registered
// wins" behavior standings always had before ids existed -- there is no data
// left to do better with -- but a correctly id-stamped match is never
// misattributed merely because some OTHER row in the same pool lacks one.
func applyTiebreakSort(sorted []state.PlayerStanding, matches []state.MatchResult, isSupplementaryID func(string) bool) {
	for _, positions := range detectPoolTies(sorted) {
		i := positions[0]
		j := positions[len(positions)-1] + 1

		resolveGroupMatchKey := newGroupKeyResolver(sorted[i:j])

		groupWins := map[string]int{}
		for _, m := range matches {
			if !isSupplementaryID(m.ID) || m.Status != state.MatchStatusCompleted || m.Winner == "" {
				continue
			}
			keyA, aOK := resolveGroupMatchKey(m.SideAID, m.SideA)
			keyB, bOK := resolveGroupMatchKey(m.SideBID, m.SideB)
			if !aOK || !bOK || keyA == keyB {
				continue
			}
			// Winner by id where recorded, else by name; see resolveWinnerSide.
			winnerIsA, winnerIsB := resolveWinnerSide(m)
			switch {
			case winnerIsA:
				groupWins[keyA]++
			case winnerIsB:
				groupWins[keyB]++
			}
		}
		if len(groupWins) > 0 {
			sort.SliceStable(sorted[i:j], func(a, b int) bool {
				keyA := standingsPlayerKey(sorted[i+a].Player.ID, sorted[i+a].Player.Name)
				keyB := standingsPlayerKey(sorted[i+b].Player.ID, sorted[i+b].Player.Name)
				return groupWins[keyA] > groupWins[keyB]
			})
		}
	}
}

// tieAffectsAdvancement reports whether a tied group (identified by its 0-based
// positions from detectPoolTies over the sorted standings) can change the pool
// outcome and therefore warrants a supplementary bout.
//
// A pool seeds its knockout from the top EffectivePoolWinners of each pool, and
// finishing position decides where in the bracket a qualifier lands: the 1st
// stays in its own block while lower finishers cross to a partner block half the
// bracket away (R4), and a block's structural bye goes to its highest-precedence
// occupant (R6, helper.BuildKnockoutDraw). That occupant is NOT necessarily a
// pool winner -- a block may hold only crossed-in qualifiers, and R5's separation
// can take a bye off a winner -- but the ordering always favours the better
// finish, so every position in [1..poolWinners] is a distinct, consequential
// seed. That is all this predicate needs; it does not depend on who byes. A tied
// group whose BEST position is already past the cutoff (positions[0]+1 > poolWinners)
// sits entirely among eliminated ranks: those teams share that rank and no
// supplementary ippon-shobu / daihyosen is played. This mirrors the rule that a
// supplementary bout is held only "to determine their relative ranking" where
// that ranking matters (running_a_kendo_tournament.md:405/441), and the
// band-aware LeagueTiebreakCandidates gate used for team leagues.
func tieAffectsAdvancement(positions []int, poolWinners int) bool {
	if len(positions) == 0 {
		return false
	}
	// positions[0] is 0-based into the descending-sorted standings; +1 makes it
	// the 1-based finishing rank of the best-placed member of the tied group.
	return positions[0]+1 <= poolWinners
}

// tieNeedsIndividualBreak decides whether InjectTiebreakerMatches should hold an
// ippon-shobu bout for a tied group in an INDIVIDUAL competition.
//
//   - League: a final placement must be EARNED, exactly like a bracket/knockout,
//     so every tie inside the tie-break band [1..effectiveTopN] is broken. The
//     one sanctioned exception is the kendo joint-3rd convention: when
//     LeagueTwoThirdPlaces is enabled and the group sits entirely at 3rd or
//     below, the tie is allowed to stand as a shared bronze (isConsequentialTie
//     returns false). This mirrors the team-league LeagueTiebreakCandidates gate
//     so an individual league never completes with an unearned tied position.
//   - Non-league (mixed pools feeding a knockout): only ties that change who
//     advances or their seed warrant a bout; a tie entirely below the qualifying
//     cut shares its rank with no bout.
func tieNeedsIndividualBreak(comp *state.Competition, positions []int, group []state.PlayerStanding, poolWinners int) bool {
	if len(positions) == 0 {
		return false
	}
	if comp != nil && comp.Format == state.CompFormatLeague {
		g := TiedGroup{
			Teams:       group,
			MinPosition: positions[0] + 1,
			MaxPosition: positions[len(positions)-1] + 1,
		}
		return isConsequentialTie(g, comp)
	}
	return tieAffectsAdvancement(positions, poolWinners)
}

// tiebreakerPairKey returns a canonical (order-independent) key for a
// pair of player names, used to detect already-existing TB matches.
func tiebreakerPairKey(a, b string) string {
	if a < b {
		return a + "|" + b
	}
	return b + "|" + a
}

// generateTiebreakerMatches creates the round-robin MatchResult entries
// for tiedGroup. existingTBCount is the current number of TB matches in
// the pool (used to produce unique TB-N indices). court is the court
// label assigned to the pool. existingRows are the TB rows already on disk
// for this pool (from a prior injection); a pair already represented among
// them is skipped, which is what makes repeated injection idempotent.
//
// Pairs are enumerated by INDEX over tiedGroup (i < j), never by comparing
// display names. The prior `a.Player.Name >= b.Player.Name` skip assumed
// names were a strict order over DISTINCT competitors, which is false: two
// tied competitors are explicitly allowed to share a display name across
// dojos (CheckDuplicateEntriesByNameDojo). For a tied NAMESAKE pair, that
// comparison is false in BOTH loop orientations (neither name is ever
// strictly greater), so `continue` fired every time and the pair's own bout
// was never generated at all -- the pool then reported "complete" with no TB
// bout ever played for the pair that most needed one. In a 3+-way group
// containing one namesake pair, the same name-based test also drops a
// SEPARATE bout: dedup keyed on bare names sees "X vs Y" for BOTH X@dojoA-vs-Y
// and X@dojoB-vs-Y and treats the second as a duplicate of the first. Index
// enumeration sidesteps both failure modes: every unordered pair of distinct
// group members is visited exactly once, regardless of what any of them are
// named.
//
// Dedup against existingRows resolves each row's sides to a group member's
// canonical identity key via newGroupKeyResolver (id-preferring, name
// fallback -- the same resolver applyTiebreakSort uses), so the two
// namesake-involving pairs above are tracked as the distinct pairs they are,
// never merged under one bare-name bucket.
//
// Stamps SideAID/SideBID from the tied competitors' participant ids (mirrors
// pools.go's regular-match generation), so applyTiebreakSort can resolve the
// winning side by id rather than by name when two tied competitors share a
// display name (allowed across dojos, CheckDuplicateEntriesByNameDojo).
func generateTiebreakerMatches(poolName string, tiedGroup []state.PlayerStanding, existingTBCount int, court string, existingRows []state.MatchResult) []state.MatchResult {
	resolve := newGroupKeyResolver(tiedGroup)
	existingPairs := make(map[string]bool, len(existingRows))
	for _, m := range existingRows {
		keyA, okA := resolve(m.SideAID, m.SideA)
		keyB, okB := resolve(m.SideBID, m.SideB)
		if okA && okB {
			existingPairs[tiebreakerPairKey(keyA, keyB)] = true
		}
	}

	var results []state.MatchResult
	idx := existingTBCount
	for i := 0; i < len(tiedGroup); i++ {
		for j := i + 1; j < len(tiedGroup); j++ {
			a, b := tiedGroup[i], tiedGroup[j]
			keyA := standingsPlayerKey(a.Player.ID, a.Player.Name)
			keyB := standingsPlayerKey(b.Player.ID, b.Player.Name)
			if existingPairs[tiebreakerPairKey(keyA, keyB)] {
				continue
			}
			results = append(results, state.MatchResult{
				ID:      fmt.Sprintf("%s-TB-%d", poolName, idx),
				SideA:   a.Player.Name,
				SideB:   b.Player.Name,
				SideAID: a.Player.ID,
				SideBID: b.Player.ID,
				Status:  state.MatchStatusScheduled,
				Court:   court,
			})
			idx++
		}
	}
	return results
}

// isBlockingEngiTBRow reports whether m is a tiebreaker row that would block
// pool completion of an engi competition. A TB row blocks unless it is
// completed with a recorded winner (the completion guards treat
// Status != completed OR Winner == "" as unresolved). Engi never holds TB
// bouts, so every blocking TB row is a pre-fix leftover with no ranking
// information (engi standings and applyTiebreakSort both ignore winnerless
// TB rows) and is safe to remove. Completed TB rows with a Winner are
// preserved: recorded results are never deleted.
func isBlockingEngiTBRow(m state.MatchResult) bool {
	return IsTiebreakerMatchID(m.ID) && (m.Status != state.MatchStatusCompleted || m.Winner == "")
}

// InjectTiebreakerMatches inspects all pool standings for compID after
// regular pool matches are complete. For every tied group (same Points
// after the full cascade), it generates a round-robin of ippon-shobu
// tiebreaker matches, appends them to the pool-matches CSV, and
// regenerates the schedule. Returns the newly injected matches (nil
// when there are no ties or all TB pairs already exist).
func (e *Engine) InjectTiebreakerMatches(compID string) ([]state.MatchResult, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	// Engi (kata competition) ranks by wins then accumulated flags (naginata.md);
	// Points is left at zero for all engi standings (no points metric), so
	// detectPoolTies would see every pool as fully tied and inject spurious
	// ippon-shobu bouts. Supplementary bouts are never held for engi.
	// Self-heal: remove any TB row a pre-fix engine left behind that would
	// block pool completion (Status != completed OR Winner == "", including a
	// bogus bout finalized as hikiwake via the decision endpoint); left in
	// place such rows block completion forever. Only completed TB rows with a
	// recorded winner are preserved: recorded results are never deleted.
	if comp.Engi {
		allMatches, loadErr := e.store.LoadPoolMatches(compID)
		if loadErr != nil {
			return nil, loadErr
		}
		var kept []state.MatchResult
		for _, m := range allMatches {
			if isBlockingEngiTBRow(m) {
				continue
			}
			kept = append(kept, m)
		}
		if len(kept) < len(allMatches) {
			if saveErr := e.store.SavePoolMatches(compID, kept); saveErr != nil {
				return nil, saveErr
			}
			e.standingsCache.Delete(compID)
			e.standingsFlight.Delete(compID)
			return nil, nil
		}
		return nil, nil
	}

	// Supplementary ippon-shobu bouts are held only where the tie affects
	// advancement/seeding (see tieAffectsAdvancement): top poolWinners advance.
	poolWinners := comp.EffectivePoolWinners()

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	allMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	// Scan existing TB matches per pool for idempotency and ID sequencing.
	// existingRows are handed to generateTiebreakerMatches raw (not reduced to
	// a bare-name dedup map here) so it can resolve each row's sides against
	// the SPECIFIC tied group being processed via newGroupKeyResolver -- see
	// that function's doc comment for why a bare-name reduction at this scan
	// stage would collapse distinct namesake-involving pairs.
	type poolTBInfo struct {
		existingRows []state.MatchResult
		count        int
	}
	poolTB := map[string]*poolTBInfo{}
	poolCourt := map[string]string{}
	// regularIncomplete[pool] becomes true if ANY regular (non-TB) match in the
	// pool is not yet completed. Tiebreakers must only be injected once a pool's
	// regular round-robin is finished, otherwise an intermediate, partial-result
	// tie (e.g. everyone 0–0 after one match) would spuriously inject TB matches
	// that a later result then breaks, leaving orphaned scheduled TB matches that
	// never clear. (The pre-incremental caller enforced this via a comp-wide
	// "all regular matches complete" gate; per-pool seeding needs it here.)
	regularIncomplete := map[string]bool{}
	for _, m := range allMatches {
		pn, ok := poolNameFromMatchID(m.ID)
		if !ok {
			continue
		}
		if _, inStandings := standings[pn]; !inStandings {
			continue
		}
		if _, ok := poolCourt[pn]; !ok {
			poolCourt[pn] = m.Court
		}
		if IsTiebreakerMatchID(m.ID) {
			if poolTB[pn] == nil {
				poolTB[pn] = &poolTBInfo{}
			}
			poolTB[pn].count++
			poolTB[pn].existingRows = append(poolTB[pn].existingRows, m)
		} else if m.Status != state.MatchStatusCompleted {
			regularIncomplete[pn] = true
		}
	}

	var injected []state.MatchResult
	for poolName, poolStandings := range standings {
		// Don't inject tiebreakers until the pool's regular matches are all done.
		if regularIncomplete[poolName] {
			continue
		}
		info := poolTB[poolName]
		existingCount := 0
		var existingRows []state.MatchResult
		if info != nil {
			existingCount = info.count
			existingRows = info.existingRows
		}

		for _, positions := range detectPoolTies(poolStandings) {
			group := standingsAt(poolStandings, positions)
			if !tieNeedsIndividualBreak(comp, positions, group, poolWinners) {
				continue
			}
			newMatches := generateTiebreakerMatches(poolName, group, existingCount, poolCourt[poolName], existingRows)
			existingCount += len(newMatches)
			injected = append(injected, newMatches...)
		}
	}

	if len(injected) == 0 {
		return nil, nil
	}

	allMatches = append(allMatches, injected...)

	// Reassign slots so the new TB matches get ScheduledAt values.
	// Snapshot operator-adjusted times first so they survive the reassignment;
	// only newly injected matches (ScheduledAt == "") should receive new slots.
	existingTimes := make(map[string]string, len(allMatches))
	for _, m := range allMatches {
		if m.ScheduledAt != "" {
			existingTimes[m.ID] = m.ScheduledAt
		}
	}
	tournament, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}
	allMatches, _ = assignPoolMatchSlots(allMatches, comp, tournament)
	for i := range allMatches {
		if t, ok := existingTimes[allMatches[i].ID]; ok {
			allMatches[i].ScheduledAt = t
		}
	}

	if err := e.store.SavePoolMatches(compID, allMatches); err != nil {
		return nil, err
	}

	// Invalidate the standings cache so the next read reflects the injected matches.
	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)

	return injected, nil
}

// newGroupKeyResolver indexes a tied group and returns the (id, name) ->
// canonical member key resolution
// shared by applyTiebreakSort and groupNeedsChusen (chusen.go). Both ask the
// same question of the same kind of data: a supplementary bout names two sides,
// and the caller needs to know which member of THIS tied group each side is.
//
// An id is preferred because two members can share a display name across dojos
// (competitor identity is name+dojo, never name alone). A non-empty id that is
// not one of this group's members -- foreign or stale data -- falls through to
// the name lookup rather than resolving to nothing. When the bout carries no id
// for that side (a row written before TB/DH generation stamped them), the name
// lookup is all there is, and a genuine same-name collision there degrades to
// the single entry byName holds, which is exactly the behaviour that predates
// ids. A correctly stamped bout is never misattributed just because some other
// row in the same pool lacks an id.
func newGroupKeyResolver(members []state.PlayerStanding) func(id, name string) (string, bool) {
	groupKeys := make(map[string]bool, len(members))
	byName := make(map[string]string, len(members))
	for _, s := range members {
		ck := standingsPlayerKey(s.Player.ID, s.Player.Name)
		groupKeys[ck] = true
		byName[standingsPlayerKey("", s.Player.Name)] = ck
	}
	return func(id, name string) (string, bool) {
		if id != "" {
			if ck := standingsPlayerKey(id, ""); groupKeys[ck] {
				return ck, true
			}
		}
		ck, ok := byName[standingsPlayerKey("", name)]
		return ck, ok
	}
}

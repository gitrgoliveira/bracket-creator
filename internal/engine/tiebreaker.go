package engine

import (
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N"). Suffix-anchored
// via hasNumericSuffixAfter (daihyosen.go) for the same reason as its
// IsPoolDaihyosenMatchID sibling: a plain substring match would misclassify a
// regular match in a pool whose name happens to contain "-TB-".
// IsTiebreakerMatchID reports whether matchID identifies a supplementary
// ippon-shobu tiebreaker match (IDs of the form "Pool X-TB-N").
//
// Delegates to state.IsTiebreakerMatchID, the id grammar's one owner (see
// its IsPoolDaihyosenMatchID sibling's doc for why); kept here under the
// engine-facing name every other file in this package already uses.
func IsTiebreakerMatchID(matchID string) bool {
	return state.IsTiebreakerMatchID(matchID)
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
// Group membership and win attribution are keyed by participant id ONLY
// (operator ruling bc-pnum, via groupMemberIDs): two tied competitors
// can share a display name across dojos (CheckDuplicateEntriesByNameDojo),
// so a bare-name key would both misclassify group membership (a
// same-named, unrelated competitor elsewhere in the pool falsely counted as
// "in this group") and cross-attribute a supplementary win between the two
// namesakes. generateTiebreakerMatches / generatePoolDaihyosenMatches stamp
// SideAID/SideBID at generation, so every TB/DH row this engine writes
// carries the ids this resolution needs; a row with no side id (hand-edited,
// or predating id-stamping) simply cannot be attributed to any member.
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
// A match side counts toward the group only when both sides are members of
// THIS tied group (groupMemberIDs, id-only membership) and the two sides
// are not the same member. A match side with no id, or a foreign id (not a
// group member's), is simply not a member -- there is no name-based
// fallback attribution.
func applyTiebreakSort(sorted []state.PlayerStanding, matches []state.MatchResult, isSupplementaryID func(string) bool) {
	for _, positions := range detectPoolTies(sorted) {
		i := positions[0]
		j := positions[len(positions)-1] + 1
		copy(sorted[i:j], judgeTieBreakRounds(sorted[i:j], matches, isSupplementaryID).order)
	}
}

// tieBreakRounds splits a tied group's supplementary rows of one kind
// (isSupplementaryID: TB or DH) into ROUNDS: a pair's first row (by bout
// number, supplementaryBoutNumber) is its bout in round 1, its second row its
// bout in round 2, and so on. A round is held among the members a finished
// round left level (RunTiebreakerRound), so every pair of such a run has had
// exactly one bout in each earlier round, and its new bout lands in the next
// one: the pair key is round-aware without the bout id carrying the round. A
// tie a correction grew keeps one round, since the injector adds only the
// pairs no row holds yet (generateTiebreakerMatches).
//
// Rows count only when both sides are members of the group (groupMemberIDs,
// id-only) and are not the same member. Each round maps tiebreakerPairKey to
// the pair's row.
func tieBreakRounds(group []state.PlayerStanding, matches []state.MatchResult, isSupplementaryID func(string) bool) []map[string]state.MatchResult {
	ids := groupMemberIDs(group)
	var rows []state.MatchResult
	for _, m := range matches {
		if isSupplementaryID(m.ID) && ids[m.SideAID] && ids[m.SideBID] && m.SideAID != m.SideBID {
			rows = append(rows, m)
		}
	}
	sort.SliceStable(rows, func(a, b int) bool {
		return supplementaryBoutNumber(rows[a].ID) < supplementaryBoutNumber(rows[b].ID)
	})
	var rounds []map[string]state.MatchResult
	seen := map[string]int{}
	for _, m := range rows {
		key := tiebreakerPairKey(m.SideAID, m.SideBID)
		k := seen[key]
		seen[key]++
		if k == len(rounds) {
			rounds = append(rounds, map[string]state.MatchResult{})
		}
		rounds[k][key] = m
	}
	return rounds
}

// supplementaryBoutNumber is a supplementary bout's number, the N of
// "Pool A-TB-N" or "Pool A-DH-N": the order the bouts were added in. -1 when
// the id carries none (the id grammar is state.IsPoolSupplementaryMatchID's).
func supplementaryBoutNumber(id string) int {
	i := strings.LastIndex(id, "-")
	if i < 0 {
		return -1
	}
	n, err := strconv.Atoi(id[i+1:])
	if err != nil {
		return -1
	}
	return n
}

// tieBreakVerdict is what a tied group's supplementary rounds decided
// (judgeTieBreakRounds).
type tieBreakVerdict struct {
	// order is the group in the order the rounds put it: by wins in round 1,
	// then each run round 1 left level by its wins in round 2, and so on. A
	// run no round has separated keeps the order it came in.
	order []state.PlayerStanding
	// settled: the rounds ordered the whole group, every bout fought with a
	// winner.
	settled bool
	// level lists the runs a FINISHED round (every bout fought, each with a
	// winner) left level that no further round has started for yet: a circle
	// (A beat B, B beat C, C beat A), or any equal win count. Each is owed
	// either another tie-breaker round among its members or the lots, both the
	// operator's choice (decision F5, round-or-lots).
	level [][]state.PlayerStanding
	// runs partitions the group, in order, into the stretches the bouts fought
	// so far left level: a member the bouts put clear of the rest is a run of
	// one, and a group no bout has separated is one run. A round still short of
	// a bout (an unfought pair the Accept removed) separates by the wins it has.
	runs [][]state.PlayerStanding
}

// judgeTieBreakRounds is the ONE reader of a tied group's supplementary rounds
// (tieBreakRounds): the order the standings sort by (applyTiebreakSort),
// whether the bouts settled the group (tieSettledByBouts, so the tie highlight,
// poolRanksInForce and completion agree), and which runs a round left level
// (tieBreakLeftLevel: the lots or another round). Membership and attribution
// are id-only, exactly as applyTiebreakSort's doc comment states.
func judgeTieBreakRounds(group []state.PlayerStanding, matches []state.MatchResult, isSupplementaryID func(string) bool) tieBreakVerdict {
	var v tieBreakVerdict
	v.order, v.settled = rankTieBreakRound(group, tieBreakRounds(group, matches, isSupplementaryID), 0, &v)
	return v
}

// rankTieBreakRound orders members by their wins in round k (0-based) and,
// once that round is finished, each run it left level by the next round. It
// returns the order and whether it is complete. A round still being fought
// orders by the bouts decided so far, as the table always has. A run with no
// row in round k after a finished round k-1 is a run left level (level); with
// no row in the first round it is a tie whose round is still to be added
// (pendingTieBreaks), not a run left level. v collects the runs left level
// (v.level) and the whole partition into runs (v.runs).
func rankTieBreakRound(members []state.PlayerStanding, rounds []map[string]state.MatchResult, k int, v *tieBreakVerdict) ([]state.PlayerStanding, bool) {
	out := append([]state.PlayerStanding(nil), members...)
	if len(out) < 2 {
		v.runs = append(v.runs, out)
		return out, true
	}
	var round map[string]state.MatchResult
	if k < len(rounds) {
		round = rounds[k]
	}
	wins := map[string]int{}
	pairs, present, decided := 0, 0, 0
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			pairs++
			a, b := out[i].Player.ID, out[j].Player.ID
			if a == "" || b == "" || a == b {
				continue
			}
			m, ok := round[tiebreakerPairKey(a, b)]
			if !ok {
				continue
			}
			present++
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Winner by id only (operator ruling bc-pnum); see resolveWinnerSide.
			switch winnerIsA, winnerIsB := resolveWinnerSide(m); {
			case winnerIsA:
				wins[m.SideAID]++
				decided++
			case winnerIsB:
				wins[m.SideBID]++
				decided++
			}
		}
	}
	if present == 0 {
		if k > 0 {
			v.level = append(v.level, out)
		}
		v.runs = append(v.runs, out)
		return out, false
	}
	sort.SliceStable(out, func(a, b int) bool {
		return wins[out[a].Player.ID] > wins[out[b].Player.ID]
	})
	finished := decided == pairs
	settled := finished
	for i := 0; i < len(out); {
		j := i + 1
		for j < len(out) && wins[out[j].Player.ID] == wins[out[i].Player.ID] {
			j++
		}
		if j-i >= 2 && finished {
			run, ok := rankTieBreakRound(out[i:j], rounds, k+1, v)
			copy(out[i:j], run)
			settled = settled && ok
		} else {
			v.runs = append(v.runs, out[i:j])
		}
		i = j
	}
	return out, settled
}

// tieBreakRuns partitions a Points-tied group, in the order applyTiebreakSort
// left it, into the runs its fought tie-breaker bouts left level: the ippon-shobu
// (TB) bouts in any competition, then in a team one the daihyosen (DH) bouts
// among each run those left. A team the bouts put clear of the others is a run
// of one, so a place shared among the level ones is never shared with it.
func tieBreakRuns(group []state.PlayerStanding, matches []state.MatchResult, isTeam bool) [][]state.PlayerStanding {
	runs := judgeTieBreakRounds(group, matches, IsTiebreakerMatchID).runs
	if !isTeam {
		return runs
	}
	var refined [][]state.PlayerStanding
	for _, run := range runs {
		refined = append(refined, judgeTieBreakRounds(run, matches, IsPoolDaihyosenMatchID).runs...)
	}
	return refined
}

// tieBreakLeftLevel lists the runs of a tied group that a finished ippon-shobu
// ("-TB-") round left level with no further round started (tieBreakVerdict's
// level). A representative bout's circle is daihyosenLeftTied's.
func tieBreakLeftLevel(group []state.PlayerStanding, matches []state.MatchResult) [][]state.PlayerStanding {
	return judgeTieBreakRounds(group, matches, IsTiebreakerMatchID).level
}

// tieSettled reports whether a Points-tied group's finishing order has
// already been decided, so it is no longer a tie to break: the operator
// recorded a chusen for every member that is in force (chusenRecorded, reading
// the rows' IsOverridden), or the group's supplementary bouts ordered it
// completely (tieSettledByBouts). Points never change after either
// (applyTiebreakSort only reorders within the group), so detectPoolTies alone
// still reports such a group, and the tie highlight read it as a tie forever
// (operator ruling 2026-09-24: a tie settled by chusen or by daihyosen is no
// longer a tie).
func tieSettled(group []state.PlayerStanding, matches []state.MatchResult, isTeam bool) bool {
	return chusenRecorded(group) || tieSettledByBouts(group, matches, isTeam)
}

// tieSettledByBouts reports whether a Points-tied group's supplementary bouts
// ordered it completely, over every round they were fought in
// (judgeTieBreakRounds): the ippon-shobu (TB) bouts in any competition and the
// daihyosen (DH) bouts in a team one, the same two passes computeStandingsFrom
// sorts by. A group they settled is decided by what was fought, so no recorded
// lots govern it (poolRanksInForce).
func tieSettledByBouts(group []state.PlayerStanding, matches []state.MatchResult, isTeam bool) bool {
	if judgeTieBreakRounds(group, matches, IsTiebreakerMatchID).settled {
		return true
	}
	return isTeam && judgeTieBreakRounds(group, matches, IsPoolDaihyosenMatchID).settled
}

// tieAffectsAdvancement reports whether a tied group (identified by its 0-based
// positions from detectPoolTies over the sorted standings) can change the pool
// outcome and therefore warrants a supplementary bout.
//
// A pool seeds its knockout from its top qualifying places (poolTieBreakCut), and
// finishing position decides where in the bracket a qualifier lands: the 1st
// stays in its own block while lower finishers cross to a partner block half the
// bracket away (R4), and a block's structural bye goes to its highest-precedence
// occupant (R6, helper.BuildKnockoutDraw). That occupant is NOT necessarily a
// pool winner -- a block may hold only crossed-in qualifiers, and R5's separation
// can take a bye off a winner -- but the ordering always favours the better
// finish, so every position in [1..cut] is a distinct, consequential
// seed. That is all this predicate needs; it does not depend on who byes. A tied
// group whose BEST position is already past the cutoff (positions[0]+1 > cut)
// sits entirely among eliminated ranks: those teams share that rank and no
// supplementary ippon-shobu / daihyosen is played. This mirrors the rule that a
// supplementary bout is held only "to determine their relative ranking" where
// that ranking matters (running_a_kendo_tournament.md:405/441), and the
// band-aware LeagueTiebreakCandidates gate used for team leagues.
func tieAffectsAdvancement(positions []int, cut int) bool {
	if len(positions) == 0 {
		return false
	}
	// positions[0] is 0-based into the descending-sorted standings; +1 makes it
	// the 1-based finishing rank of the best-placed member of the tied group.
	return positions[0]+1 <= cut
}

// poolTieBreakCut is the ONE statement of the cut a pool's ties are judged
// against: how many of its places send a qualifier. Under larger-pools an
// oversized pool sends one more than Winners per pool, and under fill-bracket a
// DRAFTED pool sends its 2nd too, so a tie over such a place is owed a bout like
// any other qualifying place.
//
// The count is read off the stored draw (drawnQualifierPlaces), the truth the
// qualifier seating uses and the only one that knows which pools fill-bracket
// drafted. Where the bracket names none of the pool's places (a v1 bracket
// predating the draw placeholders, which carries only the uniform Winners per
// pool layout, or no bracket yet) the pool's own Competition.QualifiersForPool
// answers. A pool absent from pools (not yet saved, or hand-edited data) falls
// back to EffectivePoolWinners, the count every pool sends without extra
// qualifiers.
func poolTieBreakCut(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, poolName string) int {
	for _, p := range pools {
		if p.PoolName != poolName {
			continue
		}
		if n := drawnQualifierPlaces(bracket, p); n > 0 {
			return n
		}
		return comp.QualifiersForPool(p)
	}
	return comp.EffectivePoolWinners()
}

// drawnQualifierPlaces is the highest place of pool that the STORED draw seats a
// qualifier from: the largest rank r for which the bracket's draw-time
// placeholders (recordDrawPlaceholders) name qualifierLabel(pool, r). It is 0
// when the bracket names none of the pool's places (no bracket, a knockout
// seeded without pool placeholders, or a v1 bracket whose placeholders
// ResolveQualifiedPools has not backfilled yet).
//
// The stored draw, not the competition's config, is the truth the qualifier
// seating reads: qualifierResolver fills exactly the labels the bracket holds.
// Which pools a fill-bracket draw DRAFTED to send their 2nd is a whole-field
// selection (helper.SelectFillBracketDraftIndices, which depends on the court
// count and on every pool's seeds) that Competition.QualifiersForPool
// deliberately cannot express, and re-running it here would drift from the
// bracket on disk the moment courts, seeds or pool size changed after the draw.
// Reading the placeholders cannot disagree with the seating, and covers
// larger-pools and standard mode by the same rule.
func drawnQualifierPlaces(bracket *state.Bracket, pool helper.Pool) int {
	if bracket == nil {
		return 0
	}
	labels := map[string]bool{}
	note := func(m *state.BracketMatch) {
		labels[m.PlaceholderA], labels[m.PlaceholderB], labels[m.PlaceholderWinner] = true, true, true
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			note(&bracket.Rounds[ri][mi])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		note(bracket.ThirdPlaceMatch)
	}
	for rank := len(pool.Players); rank >= 1; rank-- {
		if labels[qualifierLabel(pool.PoolName, rank)] {
			return rank
		}
	}
	return 0
}

// PoolQualifierCuts maps each pool's name to how many of its places send a
// qualifier: poolTieBreakCut for every pool. The viewer payload carries it so a
// standings table marks the advancing places and the tie badge reads a tie over
// the last of them against the SAME count the server holds a tie-break bout to,
// instead of re-deriving it (the client cannot: which fill-bracket pools were
// drafted is a property of the stored draw).
func PoolQualifierCuts(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket) map[string]int {
	out := make(map[string]int, len(pools))
	for _, p := range pools {
		out[p.PoolName] = poolTieBreakCut(comp, pools, bracket, p.PoolName)
	}
	return out
}

// tieNeedsBreak is the ONE statement of the band a tied group is broken in, for
// every kind of break: whether an ippon-shobu or a daihyosen round is held for
// it, whether a tie-breaker that left it level is offered a chusen
// (ChusenStatus), and whether such a group blocks completion (dhCycleExists).
// One band for all three, or a tie the round was held for could be left with no
// chusen offered and the competition stranded.
//
//   - League: a final placement must be EARNED, exactly like a bracket/knockout,
//     so every tie inside the tie-break band [1..effectiveTopN] is broken. The
//     one sanctioned exception is the kendo joint-3rd convention: when
//     LeagueTwoThirdPlaces is enabled and the group sits entirely at 3rd or
//     below, the tie is allowed to stand as a shared bronze (isConsequentialTie
//     returns false). It is the team-league LeagueTiebreakCandidates gate too,
//     so a league never completes with an unearned tied position.
//   - Non-league (mixed pools feeding a knockout): only ties that change who
//     advances or their seed warrant a bout; a tie entirely below the qualifying
//     cut shares its rank with no bout.
func tieNeedsBreak(comp *state.Competition, positions []int, group []state.PlayerStanding, cut int) bool {
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
	return tieAffectsAdvancement(positions, cut)
}

// breakBand is tieNeedsBreak for a competition's pools, each judged against its
// own cut (poolTieBreakCut over the stored draw): the form the readers that walk
// every pool take it in (ChusenStatus, dhCycleExists).
type breakBand func(poolName string, positions []int, group []state.PlayerStanding) bool

func newBreakBand(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket) breakBand {
	return func(poolName string, positions []int, group []state.PlayerStanding) bool {
		return tieNeedsBreak(comp, positions, group, poolTieBreakCut(comp, pools, bracket, poolName))
	}
}

// markTiesOutOfBand sets TieOutOfBand on every row of a tied group of sorted (a
// pool's Points-sorted standings) that band leaves outside the places a tie is
// broken in. It is how the standings carry the band to poolMatchSettled and the
// Pools tab's notice, which have no competition or draw to ask it of.
func markTiesOutOfBand(band breakBand, poolName string, sorted []state.PlayerStanding) {
	for _, positions := range detectPoolTies(sorted) {
		if band(poolName, positions, standingsAt(sorted, positions)) {
			continue
		}
		for _, idx := range positions {
			sorted[idx].TieOutOfBand = true
		}
	}
}

// pendingTieBreak is one tied group of a pool that still owes supplementary
// bouts: its positions (0-based, into the Points-sorted standings) and the
// bouts that would settle it.
type pendingTieBreak struct {
	positions []int
	bouts     []state.MatchResult
}

// pendingTieBreaks is the ONE statement of which supplementary bouts a pool
// still owes, given the rows already on disk. It is the per-pool body of both
// injectors (InjectTiebreakerMatches with daihyosen=false,
// InjectPoolDaihyosenMatches with daihyosen=true), extracted so the
// requalification planner (pool_requalify.go) can ask the same question
// inside a transaction, where the injectors themselves cannot run (they write
// through the bare store).
//
// A group whose bouts have all been generated returns nothing here even though
// detectPoolTies still reports it: Points never change after a supplementary
// bout (applyTiebreakSort only reorders within the group), so "tied" is not
// "unsettled". A caller wanting "unsettled" must use this, never
// detectPoolTies alone, or a settled pool reads as tied forever.
//
// The band both kinds break is tieNeedsBreak, against the pool's OWN qualifier
// count, cut (poolTieBreakCut, which is
// Competition.QualifiersForPool), so a larger-pools extra qualifier's place
// is tie-broken like any other qualifying place. Not MatchWinnerRanksNeeded:
// that is the one global bound for the whole competition, so a pool that
// sends no extra qualifier would owe a bout for a place that qualifies
// nobody and leave it unresolved forever.
//
// existingRows are the pool's rows of the matching kind (TB rows for
// individual, DH rows for team); their count seeds the new bouts' ids.
func pendingTieBreaks(comp *state.Competition, poolName string, cut int, poolStandings []state.PlayerStanding, existingRows []state.MatchResult, court string, daihyosen bool) []pendingTieBreak {
	existingCount := nextSupplementaryBoutNumber(existingRows, len(existingRows))
	var out []pendingTieBreak
	for _, positions := range detectPoolTies(poolStandings) {
		group := standingsAt(poolStandings, positions)
		if !tieNeedsBreak(comp, positions, group, cut) {
			continue
		}
		var bouts []state.MatchResult
		if daihyosen {
			bouts = generatePoolDaihyosenMatches(poolName, group, existingCount, court, existingRows)
		} else {
			bouts = generateTiebreakerMatches(poolName, group, existingCount, court, existingRows)
		}
		if len(bouts) == 0 {
			continue
		}
		existingCount += len(bouts)
		out = append(out, pendingTieBreak{positions: positions, bouts: bouts})
	}
	return out
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
// for tiedGroup. existingTBCount is the lowest TB-N number the new bouts may
// take (nextSupplementaryBoutNumber also keeps them past every existing row's).
// court is the court label assigned to the pool. existingRows are the TB rows
// already on disk for this pool (from a prior injection); a pair already
// represented among them is skipped, which is what makes repeated injection
// idempotent, and why it never starts a further round by itself: a round a
// circle left level is the operator's to run (RunTiebreakerRound) or settle
// by the lots (decision F5).
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
// Dedup against existingRows resolves each row's sides against
// groupMemberIDs (id-only, operator ruling bc-pnum -- the same set
// applyTiebreakSort uses), so the two namesake-involving pairs above are
// tracked as the distinct pairs they are, never merged under one bare-name
// bucket.
//
// Stamps SideAID/SideBID from the tied competitors' participant ids (mirrors
// pools.go's regular-match generation), so applyTiebreakSort can resolve the
// winning side by id when two tied competitors share a display name
// (allowed across dojos, CheckDuplicateEntriesByNameDojo).
func generateTiebreakerMatches(poolName string, tiedGroup []state.PlayerStanding, existingTBCount int, court string, existingRows []state.MatchResult) []state.MatchResult {
	ids := groupMemberIDs(tiedGroup)
	existingPairs := make(map[string]bool, len(existingRows))
	for _, m := range existingRows {
		// No m.SideAID != m.SideBID guard here (unlike the two real
		// self-pair PREVENTION sites, groupNeedsChusen and
		// applyTiebreakSort): this scan only remembers what is already on
		// disk, it does not decide what to generate. A self-referential
		// stored row (SideAID == SideBID, reachable when two distinct
		// tiedGroup entries share a corrupted/hand-edited duplicate
		// participant id, so generation itself emitted the row) must still
		// be recognized as existing, or re-injection over the same group
		// regenerates it every call.
		if ids[m.SideAID] && ids[m.SideBID] {
			existingPairs[tiebreakerPairKey(m.SideAID, m.SideBID)] = true
		}
	}

	return tieBreakBouts(poolName, tiedGroup, nextSupplementaryBoutNumber(existingRows, existingTBCount), court, func(a, b string) bool {
		return existingPairs[tiebreakerPairKey(a, b)]
	})
}

// tieBreakBouts is the one maker of ippon-shobu ("-TB-") bouts: one per pair
// of members (by index, see generateTiebreakerMatches) that held does not
// already hold, numbered from first on. The injector passes the pairs a row
// already holds; a fresh round (RunTiebreakerRound) passes none, since its
// pairs each hold a row of every earlier round already.
func tieBreakBouts(poolName string, members []state.PlayerStanding, first int, court string, held func(a, b string) bool) []state.MatchResult {
	var results []state.MatchResult
	idx := first
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			a, b := members[i], members[j]
			if held != nil && held(a.Player.ID, b.Player.ID) {
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

// nextSupplementaryBoutNumber is the first bout number a new supplementary
// bout of a pool may take: past every row's own number, and never below
// floor (the count a caller has handed out already). Counting the rows alone
// could hand out a number a row still holds once an earlier row was removed
// (the engi heal), and the round order (tieBreakRounds) reads the numbers.
func nextSupplementaryBoutNumber(rows []state.MatchResult, floor int) int {
	next := floor
	for _, m := range rows {
		if n := supplementaryBoutNumber(m.ID); n >= next {
			next = n + 1
		}
	}
	return next
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
	return IsTiebreakerMatchID(m.ID) && !poolMatchSettled(&m, poolLots{})
}

// InjectTiebreakerMatches inspects all pool standings for compID after
// regular pool matches are complete. For every tied group (same Points
// after the full cascade), it generates a round-robin of ippon-shobu
// tiebreaker matches, appends them to the pool-matches CSV, and
// regenerates the schedule. Returns the newly injected matches (nil
// when there are no ties or all TB pairs already exist).
func (e *Engine) InjectTiebreakerMatches(compID string) ([]state.MatchResult, error) {
	// Read before the transaction (the tournament has its own lock), and
	// only reported when there is something to schedule, as it always was.
	tournament, tournErr := e.store.LoadTournament()

	// ONE read-modify-write of pool-matches.csv under the per-competition
	// lock, both the engi self-heal and the injection (bc-mrgc phase 3): each
	// used to load and save under two separate lock acquisitions, so a score
	// landing in between was overwritten by the stale copy saved back. The
	// standings that decide the injection are read inside the same
	// transaction, from the same rows.
	// The reads below run under the competition's lock (tx.LoadPoolMatches),
	// which skips the first-read legacy conversion; run it first, as the
	// store's own LoadPoolMatches always did (a no-op after the first call).
	e.store.EnsureLegacyUpgraded(compID)
	var injected []state.MatchResult
	healed := false
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(compID)
		if err != nil {
			return err
		}
		if comp == nil {
			return notFoundErrorf("competition %s not found", compID)
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
			allMatches, loadErr := tx.LoadPoolMatches(compID)
			if loadErr != nil {
				return loadErr
			}
			e.noteMatchRead(compID)
			var kept []state.MatchResult
			for _, m := range allMatches {
				if isBlockingEngiTBRow(m) {
					continue
				}
				kept = append(kept, m)
			}
			if len(kept) < len(allMatches) {
				healed = true
				return tx.SavePoolMatches(compID, kept)
			}
			return nil
		}

		// Supplementary ippon-shobu bouts are held only where the tie affects
		// advancement/seeding; pendingTieBreaks owns that band.
		standings, err := e.computeStandingsFrom(tx, compID)
		if err != nil {
			return err
		}

		allMatches, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		e.noteMatchRead(compID)
		pools, err := tx.LoadPools(compID)
		if err != nil {
			return err
		}
		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return err
		}

		// Scan existing TB matches per pool for idempotency and ID sequencing.
		// existingRows are handed to generateTiebreakerMatches raw (not reduced to
		// a bare-name dedup map here) so it can resolve each row's sides against
		// the SPECIFIC tied group being processed via groupMemberIDs -- see
		// that function's doc comment for why a bare-name reduction at this scan
		// stage would collapse distinct namesake-involving pairs.
		poolTB := map[string][]state.MatchResult{}
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
				poolTB[pn] = append(poolTB[pn], m)
			} else if m.Status != state.MatchStatusCompleted {
				regularIncomplete[pn] = true
			}
		}

		for poolName, poolStandings := range standings {
			// Don't inject tiebreakers until the pool's regular matches are all done.
			if regularIncomplete[poolName] {
				continue
			}
			for _, p := range pendingTieBreaks(comp, poolName, poolTieBreakCut(comp, pools, bracket, poolName), poolStandings, poolTB[poolName], poolCourt[poolName], false) {
				injected = append(injected, p.bouts...)
			}
		}

		if len(injected) == 0 {
			return nil
		}
		if tournErr != nil {
			return tournErr
		}
		// Reassign slots so the new TB matches get ScheduledAt values,
		// keeping operator-adjusted times, and move the knockout past them.
		return savePoolMatchesAndKnockoutTimes(tx, compID, appendWithSlots(allMatches, injected, comp, tournament, nil), comp, tournament)
	})
	if err != nil {
		return nil, err
	}
	if healed || len(injected) > 0 {
		// Invalidate the standings cache so the next read reflects the change.
		e.standingsCache.Delete(compID)
		e.standingsFlight.Delete(compID)
	}
	if len(injected) == 0 {
		return nil, nil
	}
	return injected, nil
}

// RunTiebreakerRound adds another round of ippon-shobu bouts among
// memberIDs, the members of one of a pool's tied groups that a finished
// tie-breaker round left level (tieBreakLeftLevel: a circle, A beat B, B beat
// C, C beat A, or any equal win count). It is the operator's "Run another
// tie-breaker round" (decision F5, round-or-lots): the injector never starts a
// further round by itself, because drawing lots is the other way on and the
// operator chooses between them. Every pair of the run gets a fresh bout,
// numbered after the pool's last, which tieBreakRounds reads as the next
// round. Returns the bouts added.
//
// Refused with a ValidationError for a team competition (a circle of
// representative bouts is decided by the lots), an engi one (no supplementary
// bouts), a competition whose pools are no longer being played (status pools,
// or knockout for a pools + knockout competition), a selection that is not
// exactly such a run, or a group the lots already ordered. NotFoundError for a
// competition or a pool that does not exist.
func (e *Engine) RunTiebreakerRound(compID, poolName string, memberIDs []string) ([]state.MatchResult, error) {
	want := append([]string(nil), memberIDs...)
	slices.Sort(want)
	if len(want) < 2 || slices.Contains(want, "") || len(slices.Compact(slices.Clone(want))) != len(want) {
		return nil, validationErrorf("name at least two different competitors, each by id")
	}
	// Read before the transaction (the tournament has its own lock), as the
	// injector does.
	tournament, tournErr := e.store.LoadTournament()
	e.store.EnsureLegacyUpgraded(compID)
	var injected []state.MatchResult
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(compID)
		if err != nil {
			return err
		}
		if comp == nil {
			return notFoundErrorf("competition %s not found", compID)
		}
		if comp.IsTeam() || comp.Engi {
			return validationErrorf("another tie-breaker round is held only in an individual competition")
		}
		if comp.Status != state.CompStatusPools && (comp.Format != state.CompFormatMixed || comp.Status != state.CompStatusKnockout) {
			return validationErrorf("another tie-breaker round can only be added while the matches are being played")
		}
		standings, err := e.computeStandingsFrom(tx, compID)
		if err != nil {
			return err
		}
		poolStandings, ok := standings[poolName]
		if !ok {
			return notFoundErrorf("pool %q not found in competition %s", poolName, compID)
		}
		allMatches, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		e.noteMatchRead(compID)
		pools, err := tx.LoadPools(compID)
		if err != nil {
			return err
		}
		bracket, err := tx.LoadBracket(compID)
		if err != nil {
			return err
		}
		band := newBreakBand(comp, pools, bracket)
		var run []state.PlayerStanding
		lotsDrawn := false
		for _, positions := range detectPoolTies(poolStandings) {
			group := standingsAt(poolStandings, positions)
			if !band(poolName, positions, group) {
				continue
			}
			for _, r := range tieBreakLeftLevel(group, allMatches) {
				ids := make([]string, len(r))
				for i, s := range r {
					ids[i] = s.Player.ID
				}
				slices.Sort(ids)
				if !slices.Equal(ids, want) {
					continue
				}
				if chusenRecorded(group) {
					lotsDrawn = true
					continue
				}
				run = r
			}
		}
		if lotsDrawn && run == nil {
			return validationErrorf("the lots already ordered these competitors; change the recorded lots instead")
		}
		if run == nil {
			return validationErrorf("no tie-breaker round left exactly these competitors level")
		}

		// The new round goes where the pool's last tie-break bout was played,
		// or else where the pool plays.
		var poolTB []state.MatchResult
		court, lastTB := "", -1
		for _, m := range allMatches {
			if pn, ok := poolNameFromMatchID(m.ID); !ok || pn != poolName {
				continue
			}
			if court == "" && lastTB < 0 {
				court = m.Court
			}
			if IsTiebreakerMatchID(m.ID) {
				poolTB = append(poolTB, m)
				if n := supplementaryBoutNumber(m.ID); n > lastTB {
					lastTB, court = n, m.Court
				}
			}
		}
		injected = tieBreakBouts(poolName, run, nextSupplementaryBoutNumber(poolTB, len(poolTB)), court, nil)
		if tournErr != nil {
			return tournErr
		}
		return savePoolMatchesAndKnockoutTimes(tx, compID, appendWithSlots(allMatches, injected, comp, tournament, nil), comp, tournament)
	})
	if err != nil {
		return nil, err
	}
	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)
	return injected, nil
}

// groupMemberIDs returns the set of participant ids among a tied group's
// members, shared by applyTiebreakSort, groupNeedsChusen (chusen.go),
// generateTiebreakerMatches and generatePoolDaihyosenMatches (daihyosen.go).
// All four ask the same
// question of the same kind of data: a supplementary bout names two sides,
// and the caller needs to know whether each is a member of THIS tied group.
//
// ID-only (operator ruling bc-pnum): a row is keyed by its participant id; a
// row without one resolves to nothing. There is no name fallback: falling
// through to a bare-name lookup would risk attributing the row to whichever
// OTHER member of THIS group happens to share a display name (two members
// can share a name across dojos), which is exactly the misattribution the
// operator ruling forbids. A member with no id (Player.ID == "") is simply
// never inserted, so no bout side can ever resolve to it by id; that member
// still appears in the group for sorting/output purposes, it just cannot be
// credited a supplementary win.
func groupMemberIDs(members []state.PlayerStanding) map[string]bool {
	ids := make(map[string]bool, len(members))
	for _, s := range members {
		if s.Player.ID == "" {
			continue
		}
		ids[s.Player.ID] = true
	}
	return ids
}

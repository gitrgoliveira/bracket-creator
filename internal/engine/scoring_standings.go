package engine

import (
	"fmt"
	"slices"
	"sort"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// accrueTeamSubResults folds one completed team match's sub-bout results into
// both sides' standings (IV/IL/IT and PW/PL). Shared by the pool/league core
// (computeStandingsFrom) and SwissStandings so the team tie-break accrual
// rules live in exactly one place: a future change to how draws, empty-winner
// sub-bouts, or countScoringIppons feed the tie-break cannot silently diverge
// between formats.
func accrueTeamSubResults(sA, sB *state.PlayerStanding, m state.MatchResult) {
	// The SAME crediting rule the wire summary uses (state.TeamResultFrom ->
	// DefaultWinCreditSide), computed once for the whole match: a completed
	// default-win ruling (any kiken, fusenpai, or fusensho) credits every
	// numbered bout with no result of its own to the side the ruling
	// credits, so a kiken declared before a team's bouts were scored still
	// contributes IV/PW to standings instead of silently contributing
	// nothing. domain.MatchSideNone for anything else, which makes
	// SubBoutEffectiveResult below a no-op and reproduces the prior
	// behaviour exactly.
	credit := state.DefaultWinCreditSide(m.Status, m.Decision, m.DecisionBy, m.Attribution())
	for _, sub := range m.SubResults {
		// bc-pnum: the SAME owner the wire summary uses (state.TeamResultFrom
		// -> SubBoutWinnerSide), so the IV a spectator reads and the IV this
		// tie-break ranks by cannot disagree. That is the pact the PW comment
		// below already describes, extended to victories, and it was broken
		// for exactly one shape: a bout between two fighters sharing a display
		// name, which the name comparison here credited to side A by case
		// order while the summary credited the member id -- or neither.
		outcome := state.SubBoutEffectiveResult(sub, credit, m.SideA, m.SideB)
		switch state.SubBoutWinnerSide(outcome, m.SideA, m.SideB) {
		case domain.MatchSideA:
			sA.IndividualWins++
			sB.IndividualLosses++
		case domain.MatchSideB:
			sB.IndividualWins++
			sA.IndividualLosses++
		default:
			// A draw is a bout that HAS a result and names no winner: a Tie
			// (hikiwake) or a scored tie. A row with no result was never
			// fought (an unfought bout kept on a match a withdrawal decided,
			// and not credited above either -- an individual competition, or
			// a match some other decision closed), and counting it as IT for
			// both teams credited a draw nobody fought.
			// state.SubMatchResult.HasResult is the one played-bout
			// predicate, the same one the team finish gate asks; asked of
			// outcome so a CREDITED bout (Winner now set, HasResult() true)
			// never also falls into this branch.
			if outcome.Winner == "" && outcome.HasResult() {
				sA.IndividualDraws++
				sB.IndividualDraws++
			}
		}
		// countScoringIppons (not len): a completed bout can retain
		// "•" unfilled-slot placeholders or empty entries, which are
		// not scored points. This keeps the ranking PointsWon in sync
		// with the wire teamResult PW (state.TeamResultFrom uses the
		// same rule) so the displayed points and the tie-break points
		// cannot drift.
		sA.PointsWon += countScoringIppons(outcome.IpponsA)
		sA.PointsLost += countScoringIppons(outcome.IpponsB)
		sB.PointsWon += countScoringIppons(outcome.IpponsB)
		sB.PointsLost += countScoringIppons(outcome.IpponsA)
	}
}

// teamScoreSummary / individualScoreSummary render the human-readable score
// cell for a standing. One format definition each, shared by the pool/league
// and Swiss standings assembly loops so the two tables can never drift.
func teamScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d L:%d D:%d | IV:%d IL:%d IT:%d | PW:%d PL:%d",
		s.Wins, s.Losses, s.Draws,
		s.IndividualWins, s.IndividualLosses, s.IndividualDraws,
		s.PointsWon, s.PointsLost)
}

func individualScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d L:%d D:%d | P:%d-%d",
		s.Wins, s.Losses, s.Draws, s.IpponsGiven, s.IpponsTaken)
}

// standingsFlightRetries bounds how many times CalculatePoolStandings will
// re-enter the single-flight path before giving up on collapsing and computing
// directly. Each retry costs two stats and a map load, so a small number is
// enough to absorb the ordinary "winner's snapshot predates my write" case.
const standingsFlightRetries = 3

func (e *Engine) CalculatePoolStandings(compId string) (map[string][]state.PlayerStanding, error) {
	// Retry as a bounded loop rather than by self-recursion: every pass
	// re-samples the tokens, so the stack stays flat, and the bound means
	// termination does not rest on the (correct but subtle) argument that the
	// flight winner always deletes its entry before releasing the losers. A spin
	// in this function would wedge a live scoring handler, which is a worse
	// failure than the stale read the retry exists to avoid.
	for attempt := 0; attempt < standingsFlightRetries; attempt++ {
		// Fast path: return cached result when neither pool-matches nor overrides
		// changed. Tokens are sampled BEFORE computeStandings below, see
		// standingsTokens for why mtime alone is not sound.
		tokens := e.sampleStandingsTokens(compId)
		if result, ok := e.cachedStandingsIfValid(compId, tokens); ok {
			return result, nil
		}

		// Single-flight: collapse concurrent cold-cache callers into one compute.
		flightV, _ := e.standingsFlight.LoadOrStore(compId, &sync.Once{})
		once := flightV.(*sync.Once)
		var (
			flightResult map[string][]state.PlayerStanding
			flightErr    error
		)
		once.Do(func() {
			defer e.standingsFlight.Delete(compId)
			flightResult, flightErr = e.computeStandings(compId)
			if flightErr == nil {
				// Stamped with the PRE-compute tokens on purpose: a write landing
				// during computeStandings then leaves a strictly newer version on
				// the next call, forcing a recompute rather than blessing a result
				// that may predate that write.
				e.standingsCache.Store(compId, &standingsCacheEntry{
					standingsTokens: tokens,
					result:          flightResult,
				})
			}
		})
		if flightErr != nil {
			return nil, flightErr
		}
		if flightResult != nil {
			return flightResult, nil
		}

		// Lost the flight race. The winner stamped its entry with the tokens IT
		// sampled, which may predate a write that had already landed before we
		// sampled ours, so its result is not automatically ours to return: the
		// caller here is typically advanceMixedPools right after saving a pool
		// score, and handing it standings that predate its own write is exactly
		// the fresh-matches-vs-stale-standings split that mp-n6ke fixed. Validate
		// against our own tokens, same check as the fast path.
		if result, ok := e.cachedStandingsIfValid(compId, tokens); ok {
			return result, nil
		}
		// Either the winner's snapshot predates our tokens, or the cache was
		// invalidated between Do completion and this Load. Either way, loop: the
		// winner has already deleted the flight, so the next pass computes fresh
		// (and any sibling losers rejoin that one compute rather than each
		// starting their own).
	}

	// Out of retries under sustained contention. Compute directly: single-flight
	// is an optimisation, freshness is not negotiable. Deliberately not cached,
	// the tokens moved under us often enough that any stamp we chose would be a
	// guess, and a guess in this cache is what mp-n6ke was.
	return e.computeStandings(compId)
}

// cachedStandingsIfValid returns the cached standings for a competition only if
// the entry was stamped with exactly the tokens passed in.
//
// This is THE read path for the standings cache: every caller goes through it,
// so there is one place where "is this entry still valid" is decided. That is
// deliberate. The bug this cache was rebuilt for was a read path that skipped
// the check (the single-flight loser returned the winner's entry unvalidated),
// and a check open-coded at each site is one a future read path can silently
// half-implement. Callers pass a snapshot from sampleStandingsTokens rather than
// having this function sample its own, so the same tokens govern the lookup and
// whatever the caller does next with the result.
func (e *Engine) cachedStandingsIfValid(compId string, tokens standingsTokens) (map[string][]state.PlayerStanding, bool) {
	v, ok := e.standingsCache.Load(compId)
	if !ok {
		return nil, false
	}
	cached := v.(*standingsCacheEntry)
	if cached.standingsTokens != tokens {
		return nil, false
	}
	return cached.result, true
}

// sampleStandingsTokens reads the current cache-validity key for a competition.
// Callers must sample ONCE and reuse the snapshot: re-reading per comparison
// would let a write slip between two reads of the same logical check.
func (e *Engine) sampleStandingsTokens(compId string) standingsTokens {
	var t standingsTokens
	t.poolMatchesMtime, t.poolMatchesVersion = e.store.FileToken(compId, "pool-matches.csv")
	t.overridesMtime, t.overridesVersion = e.store.FileToken(compId, "overrides.json")
	t.poolsMtime, t.poolsVersion = e.store.FileToken(compId, "pools.csv")
	t.configMtime, t.configVersion = e.store.FileToken(compId, "config.md")
	t.bracketMtime, t.bracketVersion = e.store.FileToken(compId, "bracket.json")
	return t
}

// poolStandingsLoader is the read surface computeStandingsFrom needs. Both
// *state.Store and state.StoreTx satisfy it (identical signatures), so the
// single scoring core below can run either against the cached/single-flight
// store path (CalculatePoolStandings) or inside a write transaction (the pool
// requalification check in pool_requalify.go), with NO duplicated formula.
type poolStandingsLoader interface {
	LoadCompetition(compID string) (*state.Competition, error)
	LoadPools(compID string) ([]helper.Pool, error)
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
	LoadPoolRanks(compID string) (map[string]map[string]int, error)
	LoadBracket(compID string) (*state.Bracket, error)
	// LoadCompetitorStatus is read by the completion judgement alone
	// (poolCompletion), never by the standings: a standings entry must not
	// depend on a file the standings cache does not key on.
	LoadCompetitorStatus(compID string) (map[string]domain.CompetitorStatus, error)
}

// computeStandings is the non-tx standings core. It delegates to the shared
// computeStandingsFrom so the kendo scoring weights, tiebreaker/daihyosen
// grouping, and override sort live in exactly ONE place.
func (e *Engine) computeStandings(compId string) (map[string][]state.PlayerStanding, error) {
	// The engi accrual lives inside computeStandingsFrom, on the one
	// competition load the rest of the rule shares.
	return e.computeStandingsFrom(e.store, compId)
}

// computeStandingsFrom is the single source of truth for pool standings. It
// reads pools/matches/competition through loader (so a transaction can pass a
// StoreTx and see its just-applied write), and reads the recorded pool ranks
// through the same loader, so one caller's standings and the settled judgement
// beside them read overrides.json by one route. Overrides are read-only in the
// scoring path and are not part of any transaction's mutation set.
func (e *Engine) computeStandingsFrom(loader poolStandingsLoader, compId string) (map[string][]state.PlayerStanding, error) {
	comp, err := loader.LoadCompetition(compId)
	if err != nil {
		// Propagate a genuine read/parse fault rather than silently proceeding
		// with comp==nil, which would pick the wrong scoring mode (individual vs
		// team) and undermine the tx guard's fail-closed intent. A genuinely
		// absent competition maps to (nil, nil) and is left as individual mode.
		return nil, fmt.Errorf("computeStandingsFrom: load competition %s: %w", compId, err)
	}
	// A flag-scored (engi) competition accrues its own figures (accrueEngiMatch:
	// wins, then flags) and packs them with engiStandingPoints; everything after
	// the accrual, the tie-break bouts, the lots, the tie marks and the shared
	// places, is the same rule as every other format's (operator ruling: a tie
	// in engi is settled like any other match).
	isEngi := comp != nil && comp.Engi

	pools, err := loader.LoadPools(compId)
	if err != nil {
		return nil, err
	}
	results, err := loader.LoadPoolMatches(compId)
	if err != nil {
		return nil, err
	}

	isTeam := comp.IsTeam()

	// The band a tie is broken in (tieNeedsBreak) reads the stored draw's
	// qualifier places only outside a league, whose band is its own tie-break
	// band.
	var band breakBand
	if comp != nil {
		var bracket *state.Bracket
		if comp.Format != state.CompFormatLeague {
			if bracket, err = loader.LoadBracket(compId); err != nil {
				return nil, err
			}
		}
		band = newBreakBand(comp, pools, bracket)
	}

	// Map match results by pool using poolNameFromMatchID so hyphenated pool
	// names (e.g. "Pool A-East") are handled correctly for all ID forms
	// ("Pool A-East-0", "Pool A-East-TB-0", "Pool A-East-DH-0").
	poolResults := make(map[string][]state.MatchResult)
	for _, r := range results {
		if pn, ok := poolNameFromMatchID(r.ID); ok {
			poolResults[pn] = append(poolResults[pn], r)
		}
	}

	// Loaded ONCE for the whole competition, before the per-pool loop: a
	// corrupt overrides.json must abort the whole standings computation (this
	// function's callers already return errors), not silently drop every
	// chusen for every pool with no signal to the operator, and there is no
	// reason to re-read the same file once per pool -- overrides read-only in
	// the scoring path (see this function's own doc comment above) makes a
	// single load correct for every pool in one call.
	poolRanks, err := loader.LoadPoolRanks(compId)
	if err != nil {
		return nil, fmt.Errorf("computeStandingsFrom: load overrides for %s: %w", compId, err)
	}

	allStandings := make(map[string][]state.PlayerStanding)
	for _, p := range pools {
		matches := poolResults[p.PoolName]
		// Indexed by IDENTITY, not bare name: two competitors sharing a name
		// from different dojos are explicitly allowed, and a league is a
		// single pool holding every competitor, so a bare-name key here
		// silently collapses namesakes into one standings row.
		// (helper.Player is a type alias for domain.Player, NFR-007, so the
		// pool player flows straight into PlayerStanding.)
		playerStandings, order := newStandingsIndex(p.Players)

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Tiebreaker and pool-daihyosen matches don't count toward regular pool stats.
			if state.IsPoolSupplementaryMatchID(m.ID) {
				continue
			}
			sA := lookupStandingsPlayer(playerStandings, m.SideAID)
			sB := lookupStandingsPlayer(playerStandings, m.SideBID)
			if sA == nil || sB == nil {
				continue
			}
			if isEngi {
				accrueEngiMatch(sA, sB, m)
				continue
			}

			// Winner by id only (operator ruling bc-pnum); see resolveWinnerSide.
			winnerIsA, winnerIsB := resolveWinnerSide(m)
			switch {
			case winnerIsA:
				sA.Wins++
				sB.Losses++
			case winnerIsB:
				sB.Wins++
				sA.Losses++
			case state.IsDraw(m.Decision) || m.Winner == "":
				sA.Draws++
				sB.Draws++
			}

			if isTeam {
				// A team match's tie-break columns (IV/IL/IT/PW/PL) come from
				// the sub-bouts, via accrueTeamSubResults -- unconditionally,
				// not gated on len(m.SubResults) > 0. Routing is by isTeam (a
				// competition-level property) alone, so a padding-less default
				// win match was never misrouted into the individual branch
				// below; the gap padding closes is CREDIT, not routing: a
				// completed default-win match (kiken before any bout was
				// scored) with no SubResults rows still reaches this branch,
				// but accrueTeamSubResults' loop over an empty slice is then a
				// no-op, silently contributing zero IV/PW for a decided match.
				// state.PadDefaultWinBoutPositions gives it rows to credit.
				accrueTeamSubResults(sA, sB, m)
			} else {
				// Individual scoring: ippons at match level. countScoringIppons
				// (not len) so leftover "•" placeholders / empty slots in a
				// completed match don't inflate the ippon tallies.
				sA.IpponsGiven += countScoringIppons(m.IpponsA)
				sA.IpponsTaken += countScoringIppons(m.IpponsB)
				sB.IpponsGiven += countScoringIppons(m.IpponsB)
				sB.IpponsTaken += countScoringIppons(m.IpponsA)
			}
		}

		var sorted []state.PlayerStanding
		for _, s := range order {
			switch {
			case isEngi:
				// Wins, then own-side flags. See engiStandingPoints.
				s.Points = engiStandingPoints(*s)
				s.ScoreSummary = engiScoreSummary(s)
			case isTeam:
				// Single packed ranking score over the full team tiebreak chain
				// (W, L, T, IV, IL, IT, PW, PL). See teamStandingPoints.
				s.Points = teamStandingPoints(*s)
				s.ScoreSummary = teamScoreSummary(s)
			default:
				// Single packed ranking score over the individual chain
				// (W, L, D, ippons given, ippons taken). See individualStandingPoints.
				s.Points = individualStandingPoints(*s)
				s.ScoreSummary = individualScoreSummary(s)
			}
			sorted = append(sorted, *s)
		}

		sort.Slice(sorted, func(i, j int) bool {
			if sorted[i].Points != sorted[j].Points {
				return sorted[i].Points > sorted[j].Points
			}
			// Total-order tiebreaker. A genuine Points tie (equal on every ranking
			// criterion, no supplementary bout) must resolve deterministically
			// rather than by the iteration order of playerStandings (ranged just
			// above), or two tied qualifiers would seed into knockout slots in a
			// run-dependent order (mp-xemw). ID first (stable UUID); Name is the
			// human-unique fallback for players persisted without an ID. This only
			// reorders players already equal on Points, so applyTiebreakSort (which
			// reorders a tied group only when a decided TB/DH bout exists) and
			// markTiedStandings (which flags on Points equality) are unaffected.
			if sorted[i].Player.ID != sorted[j].Player.ID {
				return sorted[i].Player.ID < sorted[j].Player.ID
			}
			return sorted[i].Player.Name < sorted[j].Player.Name
		})

		// Apply supplementary-bout results as a secondary sort within each tied
		// group (groups located by the single detectPoolTies Points-equality
		// walk). Win counts are scoped per group, only bouts between members of
		// the same tied group count, so results from an unrelated tied group
		// never bleed across. TB (ippon-shobu) applies to all formats; DH
		// (representative) only to team competitions.
		applyTiebreakSort(sorted, matches, IsTiebreakerMatchID)
		if isTeam {
			applyTiebreakSort(sorted, matches, IsPoolDaihyosenMatchID)
		}

		// `poolRanks` itself is loaded ONCE above this loop, not per pool. Only
		// the ranks still in force govern the order (poolRanksInForce, the one
		// owner): those of a group still level on Points that no fought bout
		// settled. Each such row is marked IsOverridden here, and every other
		// reader of a chusen reads that mark.
		poolOverrides := poolRanksInForce(sorted, matches, isTeam, poolRanks[p.PoolName])
		for i := range sorted {
			if _, ok := lookupPoolRankOverride(poolOverrides, sorted[i].Player.ID); ok {
				sorted[i].IsOverridden = true
			}
		}
		markTieRuns(sorted, matches, isTeam)

		// Detect ties before applying manual rank overrides. detectPoolTies walks
		// adjacent elements, so it must run while the slice is still Points-sorted.
		// A tie already settled is not flagged: a chusen in force for every
		// member of the group (IsOverridden), or supplementary bouts that
		// ordered it completely, decided the order, so the group is no longer a
		// tie to break (operator ruling 2026-09-24; see openTieRuns).
		markTiedStandings(comp, sorted, poolResults[p.PoolName], playerStandings)
		if band != nil {
			markTiesOutOfBand(band, p.PoolName, sorted, matches)
		}

		// Apply the ranks in force. They are keyed by competitor participant id
		// ONLY (bc-cse, bc-pnum), not bare name: lookupPoolRankOverride
		// resolves nothing for an id-less row, so a pre-bc-cse overrides.json
		// entry keyed by bare name alone is unresolvable until the operator
		// re-records it through the current chusen/override-rank flow.
		if len(poolOverrides) > 0 {
			applyPoolRanksInForce(sorted, poolOverrides)
		}

		for i := range sorted {
			sorted[i].Rank = i + 1
		}

		applyJointThirdRanks(comp, sorted, matches)
		allStandings[p.PoolName] = sorted
	}

	return allStandings, nil
}

// applyPoolRanksInForce orders each tied group of sorted (Points order) that
// holds a rank in force (poolRanksInForce) by that rank, in place and within
// the group's own positions, so a chusen never moves a team above or below a
// team it was not level with. A member with no rank in force (a chusen only
// partly entered) keeps its natural 1-based position as its key, on the same
// scale as a recorded rank; a key both hold is resolved rank-first, then by the
// order the rows were in. The key travels with its row through the sort, so two
// id-less namesakes cannot read each other's position (bc-idfx).
//
// The order is kept within each run the fought bouts left level (TieRun, stamped
// by markTieRuns): the lots rank a run alone, so a competitor the bouts put
// clear of it keeps the place they gave.
func applyPoolRanksInForce(sorted []state.PlayerStanding, inForce map[string]int) {
	type keyed struct {
		standing state.PlayerStanding
		key      int
		ranked   bool
	}
	for _, positions := range detectPoolTies(sorted) {
		for start := positions[0]; start <= positions[len(positions)-1]; {
			end := start + 1
			for end <= positions[len(positions)-1] && sorted[end].TieRun == sorted[start].TieRun {
				end++
			}
			run := make([]keyed, 0, end-start)
			for k := start; k < end; k++ {
				r, ok := lookupPoolRankOverride(inForce, sorted[k].Player.ID)
				if !ok {
					r = k + 1
				}
				run = append(run, keyed{standing: sorted[k], key: r, ranked: ok})
			}
			sort.SliceStable(run, func(a, b int) bool {
				if run[a].key != run[b].key {
					return run[a].key < run[b].key
				}
				return run[a].ranked && !run[b].ranked
			})
			for k, g := range run {
				sorted[start+k] = g.standing
			}
			start = end
		}
	}
}

// applyJointThirdRanks is the ONE place a league's level group is given a
// SHARED rank (the group's best 1-based position) on its table. Only 3rd can
// be shared (operator rule, rounds rule-1 and rule-3); a group shares its place
// in two cases:
//
//   - the kendo joint-3rd convention: with the joint-3rd rule enabled
//     (state.Competition.EffectiveTwoThirdPlaces, bc-3rdp), a genuine
//     Points-tie, or a run of one the bouts left level (tieBreakRuns), whose
//     best finishing position is 3rd or lower, so both the
//     standings table and the closing-ceremony podium show two (or more)
//     equal 3rd places instead of relabeling the 4th finisher;
//   - a run the bouts left level outside the band a tie is broken in (its best
//     place is 4th or lower): no bout or lots are owed for it, so its members
//     share their better place (4, 4): ties below the
//     band can always share places.
//
// A tie at 1st or 2nd (or at 3rd with joint 3rd places off) never shares: a
// tie-breaker with a winner orders it, or the lots do (a circle the bouts
// cannot separate, or a tie-breaker drawn because both its sides are barred).
//
// It is a no-op for non-leagues. A run whose members hold a rank in force
// (IsOverridden, poolRanksInForce) keeps its sequential ranks, since the
// operator's explicit order decides that run; a different run of the same
// group, and a different tie in the same pool, is still shared. Scoping to leagues keeps mixed and knockout seeding
// strictly sequential -- those formats decide a single/joint 3rd with a bronze
// match instead, via RequiresSingleThirdPlace; naginata leagues leave the
// joint-3rd setting off and so keep a single 3rd.
//
// A shared place is shared only among the members the group's fought
// tie-breaker bouts left level (tieBreakRuns): a team that beat both others
// keeps its place, and the two it beat share the next one. A group no bout
// separated is one run, so it shares as a whole.
//
// Mutates sorted in place. Callers pass a Points-sorted slice that already has
// sequential ranks assigned and the Tied/IsOverridden stamps, so
// detectPoolTies groups by adjacent Points equality exactly as it does for the
// amber-tie highlight, and the pool's matches, which hold those bouts.
func applyJointThirdRanks(comp *state.Competition, sorted []state.PlayerStanding, matches []state.MatchResult) {
	if comp == nil || comp.Format != state.CompFormatLeague {
		return
	}
	for _, positions := range detectPoolTies(sorted) {
		group := standingsAt(sorted, positions)
		at := 0
		for _, run := range tieBreakRuns(group, matches, comp.IsTeam()) {
			place := positions[at] + 1
			// Both rules are judged on the run's own best place, never the
			// group's: a pair the bouts left level at 4th and 5th shares 4th
			// inside a group that started at 1st, and one level at 3rd and 4th
			// shares 3rd where joint 3rd places are awarded.
			// A run holding a rank in force keeps its sequential ranks: the
			// operator's order decides it. A different run of the group is
			// judged on its own.
			held := slices.ContainsFunc(run, func(s state.PlayerStanding) bool { return s.IsOverridden })
			shares := !held && (place > leagueTiebreakBand || (comp.EffectiveTwoThirdPlaces() && place >= 3))
			for range run {
				if shares {
					sorted[positions[at]].Rank = place
				}
				at++
			}
		}
	}
}

// markTiedStandings sets Tied=true on standings rows that are genuinely tied
// (same Points on every criterion) and where the tie is visible/consequential
// given the competition format and match-completion state.
//
// Gating rules by format:
//
//   - Pools (not league): mark tied groups only after ALL regular matches in
//     that pool are complete. Regular = excludes TB ("-TB-") and DH ("-DH-")
//     supplementary bouts. Pre-completion the rank is provisional, so no amber.
//
//   - League (comp.Format == state.CompFormatLeague), both team and individual:
//     EMERGING-TIE trigger. Once ANY of the top-N competitors (N =
//     leagueTiebreakBand, clamped to pool size) has completed ALL their own
//     regular fights, mark Tied=true on every tied group whose MinPosition <=
//     leagueTiebreakBand AND that is not covered by the two-joint-3rd-places
//     exemption (LeagueTwoThirdPlaces + MinPosition >= 3).
//
// matches contains only the regular+supplementary matches for this specific
// pool (already filtered upstream by poolNameFromMatchID).
//
// rosterIndex is computeStandingsFrom's OWN identity index (playerStandings,
// built from p.Players in the pool's on-disk roster order), threaded through
// to markTiedStandingsLeague so its match-side resolution can never disagree
// with the resolution that actually computed `sorted`'s Wins/Losses -- see
// that function's doc comment. Unused by the pools branch (nil is fine, e.g.
// direct callers such as the unit tests in tied_standings_test.go, which
// build a synthetic byKey from `sorted` itself via rosterIndexFrom).
//
// A chusen is read off the rows' IsOverridden (the ranks in force,
// poolRanksInForce). Both branches mark only the runs openTieRuns leaves open:
// Tied means a tie still to be broken, which is what the amber row tells the
// operator, so a run a chusen or its supplementary bouts already ordered is not
// one even though its Points stay equal.
func markTiedStandings(comp *state.Competition, sorted []state.PlayerStanding, matches []state.MatchResult, rosterIndex map[string]*state.PlayerStanding) {
	if len(sorted) == 0 {
		return
	}

	// Separate regular matches (exclude supplementary TB/DH bouts).
	var regularMatches []state.MatchResult
	for _, m := range matches {
		if !state.IsPoolSupplementaryMatchID(m.ID) {
			regularMatches = append(regularMatches, m)
		}
	}

	isLeague := comp != nil && comp.Format == state.CompFormatLeague
	isTeam := comp.IsTeam()

	// The supplementary rows are read here, from the full pool list, before
	// the branches see only the regular matches.
	var open [][]int
	for _, positions := range detectPoolTies(sorted) {
		open = append(open, openTieRuns(positions, standingsAt(sorted, positions), matches, isTeam)...)
	}

	if isLeague {
		markTiedStandingsLeague(comp, sorted, regularMatches, rosterIndex, open)
	} else {
		markTiedStandingsPools(sorted, regularMatches, open)
	}
}

// markTiedStandingsPools marks tied rows in a pools (non-league) competition.
// Rows are only marked once ALL regular matches in the pool are complete.
// open is the tied groups (positions into sorted) still to be broken.
func markTiedStandingsPools(sorted []state.PlayerStanding, regularMatches []state.MatchResult, open [][]int) {
	// Gate: there must be at least one regular match, and all must be completed.
	// With no matches at all the pool hasn't started, everyone is tied at 0
	// points, which must NOT surface as amber.
	if len(regularMatches) == 0 {
		return
	}
	for _, m := range regularMatches {
		if m.Status != state.MatchStatusCompleted {
			return // pool not yet finished, no amber
		}
	}

	// Pool is complete; mark every tied group still open.
	for _, positions := range open {
		for _, idx := range positions {
			sorted[idx].Tied = true
		}
	}
}

// markTiedStandingsLeague marks tied rows in a league competition using the
// emerging-tie trigger: once ANY top-N competitor has finished all their own
// regular fights, mark consequential tied groups amber. Works for both team
// and individual leagues.
//
// TWO SEPARATE fixes are needed together here, and each addresses a
// DIFFERENT failure mode (round-2 review correction: an earlier version of
// this comment credited rosterIndex ALONE, but rosterIndex was inert on its
// own regression test -- the completion buckets were still merging id-less
// namesakes into one shared entry, exactly what this function's OWN
// pre-existing comment used to warn against, and that merge happened to
// still read "done" often enough to pass the one fixture that existed):
//
//  1. Completion buckets (statusFor) are keyed by helper.CompetitorKey(ID,
//     Name, Dojo), not by the participant ID alone: rosterIndex's own
//     lookup below is id-only (operator ruling bc-pnum), so once a match
//     side resolves at all it names exactly one roster entry; the roster
//     may nonetheless carry an entry with no id yet, and CompetitorKey's
//     (name, dojo) composite is what keeps that entry from colliding with an
//     unrelated namesake in a DIFFERENT dojo (CompetitorKey's identity rule
//     -- (name, dojo), never bare name -- is a general helper, not the
//     match-side resolution this doc comment is otherwise about).
//
//  2. rosterIndex resolves a match side to the roster entry
//     computeStandingsFrom itself used to accrue that side's Wins/Losses in
//     the first place (its playerStandings map, built from p.Players in the
//     pool's ON-DISK ROSTER order) -- REQUIRED so the match is credited to
//     the SAME roster entry computeStandingsFrom itself credited, not a
//     second, independently-ordered index. A match side with no id
//     (SideAID/SideBID blank) resolves to nothing here (lookupStandingsPlayer
//     is id-only), exactly as it contributes nothing to computeStandingsFrom's
//     own tally, so the two never disagree about who a match side means.
func markTiedStandingsLeague(comp *state.Competition, sorted []state.PlayerStanding, regularMatches []state.MatchResult, rosterIndex map[string]*state.PlayerStanding, open [][]int) {
	topN := min(leagueTiebreakBand, len(sorted))

	// statusFor is keyed by helper.CompetitorKey (id-preferred, name+dojo
	// fallback -- see the fix-1 note above), not by a locally rebuilt
	// *state.PlayerStanding pointer: `sorted` holds VALUE copies
	// (computeStandingsFrom appends *s, not s itself), so its rows are
	// different objects from rosterIndex's pointers even for the same
	// competitor -- the string key is what ties the two together.
	type compStatus struct {
		total     int
		completed int
	}
	statusFor := make(map[string]*compStatus, len(sorted))
	for _, s := range sorted {
		statusFor[helper.PlayerKey(s.Player)] = &compStatus{}
	}
	tally := func(id string) *compStatus {
		st := lookupStandingsPlayer(rosterIndex, id)
		if st == nil {
			return nil
		}
		return statusFor[helper.PlayerKey(st.Player)]
	}
	for _, m := range regularMatches {
		if cs := tally(m.SideAID); cs != nil {
			cs.total++
			if m.Status == state.MatchStatusCompleted {
				cs.completed++
			}
		}
		if cs := tally(m.SideBID); cs != nil {
			cs.total++
			if m.Status == state.MatchStatusCompleted {
				cs.completed++
			}
		}
	}

	// Check if ANY top-N competitor has completed all their own fights.
	triggerFired := false
	for i := 0; i < topN; i++ {
		cs := statusFor[helper.PlayerKey(sorted[i].Player)]
		if cs != nil && cs.total > 0 && cs.completed == cs.total {
			triggerFired = true
			break
		}
	}
	if !triggerFired {
		return
	}

	// Trigger has fired: mark the open tied groups (see markTiedStandings)
	// that intersect the top-N band and are not covered by the
	// two-joint-3rd-places exemption.
	for _, positions := range open {
		minPos := positions[0] + 1 // 1-based
		g := TiedGroup{
			Teams:       standingsAt(sorted, positions),
			MinPosition: minPos,
			MaxPosition: positions[len(positions)-1] + 1,
		}
		if !isConsequentialTie(g, comp) {
			continue
		}
		for _, idx := range positions {
			sorted[idx].Tied = true
		}
	}
}

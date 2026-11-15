package engine

import (
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TiedGroup describes a group of teams that are tied on all official ranking
// criteria at the end of league play. MinPosition and MaxPosition are the
// 1-based finishing positions that the tied teams collectively occupy (e.g.
// a two-way tie at 2nd place gives MinPosition=2, MaxPosition=3).
type TiedGroup struct {
	// Teams holds the standings entries of all tied members, in the order
	// produced by detectPoolTies (which preserves the sorted standings order).
	Teams []state.PlayerStanding

	// MinPosition is the 1-based rank of the best-placed team in the group.
	// For a group starting at index i (0-based) in the sorted standings,
	// MinPosition == i+1.
	MinPosition int

	// MaxPosition is the 1-based rank of the worst-placed team in the group.
	// For a group of n teams starting at index i, MaxPosition == i+n.
	MaxPosition int
}

// leagueTiebreakBand is the last place a league breaks a tie for (operator
// rule, rounds rule-1 and rule-3): only 3rd can be shared, and only when two
// joint 3rd places are awarded, so a tie at 1st or 2nd (and at 3rd with joint
// 3rd places off) is settled by a tie-breaker with a winner, or the lots the
// organiser enters for a tie the bouts cannot separate, while a tie at 4th or
// lower always shares its place. Every league, team or individual, whatever
// a retired "Break ties for top" setting once said.
const leagueTiebreakBand = 3

// isConsequentialTie reports whether a league's TiedGroup must be broken given
// the tie-break band (leagueTiebreakBand) and the two-joint-3rd-places
// convention.
//
// A group is consequential when ALL of the following hold:
//  1. The group's MinPosition is within the tie-break band [1..3], meaning at
//     least one of the tied positions determines a podium place.
//  2. The group is NOT fully covered by the two-joint-3rd-places exemption:
//     when EffectiveTwoThirdPlaces() is true and ALL positions in the group are
//     at position >= 3, there is no need to distinguish 3rd from 4th, both
//     teams share 3rd. The group is therefore non-consequential.
//
// Rule precision for the two-thirds exemption:
//   - The exemption fires only when EVERY position in the group is >= 3
//     (i.e. MinPosition >= 3). If MinPosition < 3 the group straddles 2nd/3rd
//     or higher, and a tie-breaker IS needed to decide who finishes 2nd.
//   - A group sitting at positions [3,4] is just "two joint 3rds" and no
//     tie-breaker is required when EffectiveTwoThirdPlaces() is true. All
//     callers of this function are league-only (see their own gating), so the
//     league branch of EffectiveTwoThirdPlaces (bc-3rdp) is what actually
//     resolves here; it still reads the legacy LeagueTwoThirdPlaces field for
//     a record that predates the unified TwoThirdPlaces field.
func isConsequentialTie(g TiedGroup, comp *state.Competition) bool {
	// Group must intersect [1..3]: the best position in the group must be
	// within the tie-break band.
	if g.MinPosition > leagueTiebreakBand {
		return false
	}

	// Two-joint-3rd-places exemption: when enabled, a group that sits entirely
	// at positions >= 3 (i.e. only 3rd-place-or-below slots) does not need a
	// decider, all members share 3rd place. The minimum position of the group
	// must be at least 3 for this exemption to apply.
	if comp.EffectiveTwoThirdPlaces() && g.MinPosition >= 3 {
		return false
	}

	return true
}

// LeagueTiebreakCandidates returns the consequential tied groups in a team-league
// competition after all regular pool matches are complete. A group is
// "consequential" when it intersects the tie-break band [1..3] and
// is not covered by the LeagueTwoThirdPlaces exemption (see isConsequentialTie).
//
// Returns an empty slice (not an error) when:
//   - The competition is not a team league (Format != "league" or not IsTeam()).
//   - There are no ties in the standings.
//   - All ties fall outside the consequential band.
//
// This function is the single source of truth for "are there ties that need an
// operator decision?" and is used by both MaybeAutoCompletePools (to block
// premature completion) and Phase 3b's operator endpoints (to list which teams
// need a tie-breaker).
//
// The league standings come from a single implicit pool ("Pool A" by convention
// for single-pool leagues). All tied groups across all pools are evaluated;
// in practice a league has exactly one pool.
func (e *Engine) LeagueTiebreakCandidates(compID string) ([]TiedGroup, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}

	// Only applicable to team leagues.
	if comp.Format != state.CompFormatLeague || !comp.IsTeam() {
		return nil, nil
	}

	// Consequential ties are only meaningful once EVERY regular league match is
	// complete. Mid-league, standings are provisional (often everyone tied at 0
	// points), which would surface a spurious "everyone tied" group and pop the
	// "Tie-breaker required" banner prematurely. Supplementary DH/TB matches do
	// not count as regular. This mirrors the all-regular-complete gate in
	// MaybeAutoCompletePools, so the public GET candidates endpoint can't get
	// ahead of it.
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	for _, m := range matches {
		if state.IsPoolSupplementaryMatchID(m.ID) {
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			return nil, nil
		}
	}

	standings, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}

	var candidates []TiedGroup
	for _, poolStandings := range standings {
		for _, positions := range detectPoolTies(poolStandings) {
			// detectPoolTies only returns groups of 2+ positions, so positions
			// is guaranteed non-empty here.
			// positions is 0-based into poolStandings (sorted descending by Points).
			// Convert to 1-based ranks.
			minPos := positions[0] + 1
			maxPos := positions[len(positions)-1] + 1

			g := TiedGroup{
				Teams:       standingsAt(poolStandings, positions),
				MinPosition: minPos,
				MaxPosition: maxPos,
			}
			if isConsequentialTie(g, comp) {
				candidates = append(candidates, g)
			}
		}
	}
	return candidates, nil
}

// GenerateLeagueTiebreakMatches generates the round-robin daihyosen (tie-breaker)
// matches for a specific set of tied teams in a team-league competition. It is
// the operator-triggered path, called by the POST /league-tiebreak handler after
// it has validated the selection against LeagueTiebreakCandidates (this function
// does NOT re-validate consequentiality, the handler is the gate).
//
// tiedTeamIDs is REQUIRED (operator ruling bc-pnum): group membership is
// resolved by participant id only, which is unambiguous even when two tied
// teams share a display name -- a namesake collision is reachable through
// the documented checkNewTeamNameCollisions restore hole (an unreadable
// config.md disables the uniqueness check for that write, logged and
// allowed through), so name-based selection could not always disambiguate,
// and there is no name-based fallback path. Idempotency dedup against
// existing DH rows is done downstream by generatePoolDaihyosenMatches,
// which resolves existing rows against tiedGroup (the resolved standings
// entries), not against the raw id request parameters.
//
// The matches use the "Pool X-DH-N" ID format so they are recognized by the
// existing IsPoolDaihyosenMatchID predicate and routed to the DH score editor.
// Idempotent: pairs that already exist in the store are skipped. For league
// competitions it operates on the single league pool.
func (e *Engine) GenerateLeagueTiebreakMatches(compID string, tiedTeamIDs []string) ([]state.MatchResult, error) {
	// Read before the transaction (the tournament has its own lock), and
	// only reported when there is something to schedule, as it always was.
	tournament, tournErr := e.store.LoadTournament()

	// ONE read-modify-write of pool-matches.csv under the per-competition
	// lock (bc-mrgc phase 3): it used to load and save under two separate
	// lock acquisitions, so a score landing in between was overwritten by
	// the stale copy saved back. The standings that select the tied group
	// are read inside the same transaction, from the same rows.
	// The reads below run under the competition's lock (tx.LoadPoolMatches),
	// which skips the first-read legacy conversion; run it first, as the
	// store's own LoadPoolMatches always did (a no-op after the first call).
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
		if comp.Format != state.CompFormatLeague || !comp.IsTeam() {
			return validationErrorf("GenerateLeagueTiebreakMatches is only valid for team-league competitions")
		}
		if len(tiedTeamIDs) < 2 {
			return validationErrorf("GenerateLeagueTiebreakMatches requires teamIds (at least two) for competition %s", compID)
		}

		standings, err := e.computeStandingsFrom(tx, compID)
		if err != nil {
			return err
		}

		var poolName string
		var tiedGroup []state.PlayerStanding

		// ID-only selection (operator ruling bc-pnum): unambiguous by
		// construction (a participant id names exactly one competitor), so no
		// ambiguity diagnostic is needed -- a missing id is simply "not found".
		idSet := make(map[string]bool, len(tiedTeamIDs))
		for _, id := range tiedTeamIDs {
			if id == "" {
				return validationErrorf("teamIds entries must be non-empty for competition %s", compID)
			}
			if idSet[id] {
				return validationErrorf("duplicate team id %q in tie-break request for competition %s", id, compID)
			}
			idSet[id] = true
		}
		for pn, ps := range standings {
			for _, s := range ps {
				if idSet[s.Player.ID] {
					poolName = pn
					tiedGroup = append(tiedGroup, s)
				}
			}
		}
		if len(tiedGroup) != len(idSet) {
			return validationErrorf("one or more requested team ids not found in standings for competition %s", compID)
		}
		// No separate len(tiedGroup) < 2 check: idSet has at least the two
		// non-empty, deduplicated entries the loop above already enforced, and
		// the match just above pins len(tiedGroup) == len(idSet).

		// Determine the court from existing matches. existingRows are handed to
		// generatePoolDaihyosenMatches raw (not reduced to a bare-name dedup map
		// here): it resolves each row's sides against tiedGroup itself via
		// groupMemberIDs, the same identity-keyed contract
		// InjectPoolDaihyosenMatches uses, so a namesake-involving existing bout
		// cannot suppress a distinct pair on this operator-triggered path either.
		allMatches, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		e.noteMatchRead(compID)
		court := ""
		existingCount := 0
		var existingRows []state.MatchResult
		for _, m := range allMatches {
			pn, ok := poolNameFromMatchID(m.ID)
			if !ok {
				continue
			}
			if pn == poolName && court == "" {
				court = m.Court
			}
			if IsPoolDaihyosenMatchID(m.ID) && pn == poolName {
				existingCount++
				existingRows = append(existingRows, m)
			}
		}

		injected = generatePoolDaihyosenMatches(poolName, tiedGroup, existingCount, court, existingRows)
		if len(injected) == 0 {
			return nil
		}
		if tournErr != nil {
			return tournErr
		}
		// Reassign schedule slots for the new DH matches, keeping every time
		// already set.
		return tx.SavePoolMatches(compID, appendWithSlots(allMatches, injected, comp, tournament, nil))
	})
	if err != nil {
		return nil, err
	}
	if len(injected) == 0 {
		return nil, nil
	}

	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)

	return injected, nil
}

// LeagueReopen is what putting a finished league back into match play changed
// (reopenLeagueTx).
type LeagueReopen struct {
	// Reopened is true when the league was completed and is in pools again.
	Reopened bool
	// UnassignedMatches are the ids of the league's matches whose shiaijo the
	// tournament no longer has (a court can be removed while a league is
	// completed); the reopen cleared their court, so they show as unassigned.
	UnassignedMatches []string
	// RemovedCourts names those shiaijo, in name order.
	RemovedCourts []string
}

// reopenLeagueTx is the ONE body that puts a finished league back into match
// play: a completed league is set to pools, and a match on a shiaijo the
// tournament no longer has loses its court (a court can be removed while the
// league is completed, competitionsBlockingCourtRemoval; left set, the match
// would sit under a court no console lists). It mutates comp, which the caller
// saves in the same transaction, and stages the pool matches itself. A
// correction that unsettles a finished table (reconcileLeagueAfterWrite) and a
// match reopened, a reinstatement or a lots change on a finished league
// (reopenFinishedLeagueIfOpen) reach it.
func (e *Engine) reopenLeagueTx(tx state.StoreTx, compID string, comp *state.Competition) (LeagueReopen, error) {
	var out LeagueReopen
	if comp.Status != state.CompStatusComplete {
		return out, nil
	}
	comp.Status = state.CompStatusPools
	out.Reopened = true

	tournament, err := e.store.LoadTournament()
	if err != nil {
		return out, err
	}
	matches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return out, err
	}
	stranded, removed := MatchesOnRemovedCourts(matches, tournament)
	if len(stranded) == 0 {
		return out, nil
	}
	for _, i := range stranded {
		matches[i].Court = ""
		out.UnassignedMatches = append(out.UnassignedMatches, matches[i].ID)
	}
	out.RemovedCourts = removed
	return out, tx.SavePoolMatches(compID, matches)
}

// MatchesOnRemovedCourts is the ONE owner of "which shiaijo has the tournament
// lost since these matches were given one": it returns the indexes of the
// matches whose court the tournament no longer lists, and those courts in name
// order. reopenLeagueTx clears them; the finished-league confirm names them
// beforehand, so the warning and the change cannot drift.
func MatchesOnRemovedCourts(matches []state.MatchResult, tournament *state.Tournament) (stranded []int, removed []string) {
	have := map[string]bool{}
	if tournament != nil {
		for _, c := range tournament.Courts {
			have[c] = true
		}
	}
	gone := map[string]bool{}
	for i := range matches {
		if c := matches[i].Court; c != "" && !have[c] {
			stranded = append(stranded, i)
			gone[c] = true
		}
	}
	for c := range gone {
		removed = append(removed, c)
	}
	sort.Strings(removed)
	return stranded, removed
}

// leagueBeforeWrite is what reconcileLeagueAfterWrite needs to know of a
// league before a write in the write's transaction: whether it had finished.
type leagueBeforeWrite struct {
	finished bool
}

// leagueStateBeforeWrite reads what a league pool match's write is judged
// against afterwards (reconcileLeagueAfterWrite), in the write's transaction: a
// regular match or a tie-breaker bout of a finished league, whose correction
// can leave the table unsettled. nil for anything else (another format, a
// league still in match play), so no other write pays for a standings read. It
// is judged on the competition the write already loaded.
func leagueStateBeforeWrite(comp *state.Competition, matchID string) *leagueBeforeWrite {
	if !IsPoolMatchID(matchID) || comp == nil || comp.Format != state.CompFormatLeague || comp.Status != state.CompStatusComplete {
		return nil
	}
	return &leagueBeforeWrite{finished: true}
}

// reconcileLeagueAfterWrite keeps a finished league's status true to its table
// after a match's write landed, in the write's own transaction (so the status
// moves with the result or not at all): a match left unfinished, or a tie at a
// place that cannot be shared that no tie-breaker with a winner and no lots
// have settled (PoolsSettledNow), puts the league back into match play
// (reopenLeagueTx), so the tie is broken again. A correction that keeps the
// table settled leaves the league finished, and its table and podium follow
// the corrected result. A finished league from an earlier release whose shared
// ranks were accepted at 1st or 2nd reopens the same way on its next result.
//
// A reopen is noted for the door's auto-complete pass, which reports it as
// AutoCompleteStarted (competition_started).
func (e *Engine) reconcileLeagueAfterWrite(tx state.StoreTx, compID string, before *leagueBeforeWrite) error {
	if before == nil || !before.finished {
		return nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil || comp.Status != state.CompStatusComplete {
		return err
	}
	verdict, err := e.PoolsSettledNow(tx, compID)
	if err != nil || verdict == PoolsSettled {
		return err
	}
	if _, err := e.reopenLeagueTx(tx, compID, comp); err != nil {
		return err
	}
	if err := tx.SaveCompetition(comp); err != nil {
		return err
	}
	e.leagueReopenNotes.Store(compID, struct{}{})
	return nil
}

// takeLeagueReopenNote reports and clears a reopen noted for compID. The notes
// (Engine.leagueReopenNotes) hold a league a write's transaction put back into
// match play (reconcileLeagueAfterWrite) until the door's auto-complete pass
// (maybeAutoCompletePools) takes the note and reports AutoCompleteStarted, whose
// broadcast every surface reloads on. The pass takes a note only once the
// committed league is in match play (leagueBackInPlay), so a pass racing the
// transaction that wrote it leaves it for the pass that follows the commit.
func (e *Engine) takeLeagueReopenNote(compID string) bool {
	_, ok := e.leagueReopenNotes.LoadAndDelete(compID)
	return ok
}

// reopenFinishedLeagueIfOpen puts a finished league back into match play when
// it no longer holds the verdict it finished on (PoolsSettledNow): a regular
// match or a tie-break bout reopened, a send back to the queue having reached
// it outside a result write (reconcileLeagueAfterWrite covers those), a drawn
// tie-break bout that settled only while both its sides were barred and one has
// been reinstated since, or the lots that alone ordered a decisive circle
// cleared. Decided under the competition's lock.
func (e *Engine) reopenFinishedLeagueIfOpen(compID string) (bool, error) {
	return e.store.UpdateCompetitionChangedTx(compID, func(tx state.StoreTx, comp *state.Competition) (*state.Competition, error) {
		if comp == nil || comp.Format != state.CompFormatLeague || comp.Status != state.CompStatusComplete {
			return nil, nil
		}
		verdict, err := e.PoolsSettledNow(tx, compID)
		if err != nil {
			return nil, err
		}
		if verdict == PoolsSettled {
			return nil, nil
		}
		if _, err := e.reopenLeagueTx(tx, compID, comp); err != nil {
			return nil, err
		}
		return comp, nil
	})
}

// withoutLots is a transaction that reads no recorded pool ranks, so the
// standings and the verdicts built on them are judged as if the lots had been
// cleared (LotsResetReopensLeague). Everything else reads through.
type withoutLots struct{ state.StoreTx }

func (withoutLots) LoadPoolRanks(string) (map[string]map[string]int, error) { return nil, nil }

// LotsResetReopensLeague reports whether clearing every recorded lot would put
// the finished league compID back into match play (reopenFinishedLeagueIfOpen
// asks the same PoolsSettledNow, once the lots are gone): the league is settled
// with the lots stored now and not without them, because the lots alone ordered
// a decisive circle, or a tie the bouts left level. A league that is not settled
// with them (one finished before the tie rule, say) has nothing for the reset
// to reopen, and a league holding no lots is settled the same either way. The
// organiser's reset is warned about first when it does reopen. It writes
// nothing; false for any competition that is not a finished league.
func (e *Engine) LotsResetReopensLeague(tx state.StoreTx, compID string) (bool, error) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil || comp.Format != state.CompFormatLeague || comp.Status != state.CompStatusComplete {
		return false, err
	}
	with, err := e.PoolsSettledNow(tx, compID)
	if err != nil || with != PoolsSettled {
		return false, err
	}
	without, err := e.PoolsSettledNow(withoutLots{tx}, compID)
	return without != PoolsSettled, err
}

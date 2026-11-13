package engine

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// effectiveTopN returns the resolved LeagueTiebreakTopN for the competition.
// When the field is zero (unset), it defaults to 3 (the standard kendo top-N).
// The band is a team league's setting alone (the settings page offers it for
// a team league only): an individual league breaks the top 3, whatever a
// value saved while the competition was a team one still says.
func effectiveTopN(comp *state.Competition) int {
	if comp.IsTeam() && comp.LeagueTiebreakTopN > 0 {
		return comp.LeagueTiebreakTopN
	}
	return 3
}

// isConsequentialTie reports whether a TiedGroup requires an operator-initiated
// tie-breaker given the competition's tie-break band and two-third-places convention.
//
// A group is consequential when ALL of the following hold:
//  1. The group's MinPosition is within the tie-break band [1..topN], meaning at
//     least one of the tied positions determines a top-N place.
//  2. The group is NOT fully covered by the two-joint-3rd-places exemption:
//     when EffectiveTwoThirdPlaces() is true and ALL positions in the group are
//     at position >= 3, there is no need to distinguish 3rd from 4th, both
//     teams share 3rd. The group is therefore non-consequential.
//
// Rule precision for the two-thirds exemption:
//   - The exemption fires only when EVERY position in the group is >= 3
//     (i.e. MinPosition >= 3). If MinPosition < 3 the group straddles 2nd/3rd
//     or higher, and a tie-breaker IS needed to decide who finishes 2nd.
//   - The exemption is applied regardless of topN, even if topN==4, a
//     group sitting entirely at positions [3,4] is just "two joint 3rds" and
//     no tie-breaker is required when EffectiveTwoThirdPlaces() is true. All
//     callers of this function are league-only (see their own gating), so the
//     league branch of EffectiveTwoThirdPlaces (bc-3rdp) is what actually
//     resolves here; it still reads the legacy LeagueTwoThirdPlaces field for
//     a record that predates the unified TwoThirdPlaces field.
func isConsequentialTie(g TiedGroup, comp *state.Competition) bool {
	topN := effectiveTopN(comp)

	// Group must intersect [1..topN]: the best position in the group must be
	// within the tie-break band.
	if g.MinPosition > topN {
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
// "consequential" when it intersects the tie-break band [1..LeagueTiebreakTopN] and
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

	// When the operator has accepted shared ranks without a tie-breaker
	// (Phase 3b finalize endpoint), treat the competition as having no
	// consequential ties so MaybeAutoCompletePools can transition normally.
	if comp.LeagueTiebreakFinalized {
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

// ErrLeagueNotFinalized is UndoLeagueSharedRanks' refusal for a league whose
// shared ranks were never accepted (or were already undone): there is nothing
// to take back. Handlers answer it 409 not_finalized.
var ErrLeagueNotFinalized = errors.New("league shared ranks were not accepted")

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

// reopenLeagueTx is the ONE body that takes a league's accepted shared ranks
// back and, when the league had finished, puts it back into match play: the
// flag goes, a completed league is set to pools, and a match on a shiaijo the
// tournament no longer has loses its court (a court can be removed while the
// league is completed, competitionsBlockingCourtRemoval; left set, the match
// would sit under a court no console lists). It mutates comp, which the caller
// saves in the same transaction, and stages the pool matches itself. Undo
// (UndoLeagueSharedRanks), a correction that unsettles a finished table
// (reconcileLeagueAfterWrite) and a match reopened on a finished league
// (reopenFinishedLeagueIfOpen) all reach it.
func (e *Engine) reopenLeagueTx(tx state.StoreTx, compID string, comp *state.Competition) (LeagueReopen, error) {
	var out LeagueReopen
	comp.LeagueTiebreakFinalized = false
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

// UndoLeagueSharedRanks takes back a team league's accepted shared ranks
// (DELETE .../league-tiebreak/finalize), in one transaction: the flag is
// cleared and, when the league had finished, it goes back to match play with
// its dead courts cleared (reopenLeagueTx). The tie is then asked again: the
// tie-break banner offers Run tie-breaker and Accept shared ranks. Tie-breaker
// bouts that the Accept removed unfought are not put back. Refuses a missing
// competition (NotFoundError), one that is not a team league (ValidationError)
// and one whose shared ranks were not accepted (ErrLeagueNotFinalized).
func (e *Engine) UndoLeagueSharedRanks(compID string) (LeagueReopen, error) {
	var out LeagueReopen
	var refusal error
	_, err := e.store.UpdateCompetitionChangedTx(compID, func(tx state.StoreTx, comp *state.Competition) (*state.Competition, error) {
		switch {
		case comp == nil:
			refusal = notFoundErrorf("competition %s not found", compID)
			return nil, nil
		case comp.Format != state.CompFormatLeague || !comp.IsTeam():
			refusal = validationErrorf("league tie-breaker endpoints apply only to team-league competitions")
			return nil, nil
		case !comp.LeagueTiebreakFinalized:
			refusal = ErrLeagueNotFinalized
			return nil, nil
		}
		r, rerr := e.reopenLeagueTx(tx, compID, comp)
		if rerr != nil {
			return nil, rerr
		}
		out = r
		return comp, nil
	})
	if err != nil {
		return LeagueReopen{}, err
	}
	if refusal != nil {
		return LeagueReopen{}, refusal
	}
	e.standingsCache.Delete(compID)
	e.standingsFlight.Delete(compID)
	return out, nil
}

// openLeagueTie is one tie openLeagueTies finds, with the pool it is in.
type openLeagueTie struct {
	pool  string
	group TiedGroup
}

// openLeagueTies is the ONE walk over a league's table for the ties that owe a
// decision, whatever the operator has accepted: a tie in the band
// (tieNeedsBreak) that no tie-breaker round and no lots have ordered
// (tieSettled). The reconciler compares them before and after a write
// (unsettledLeagueGroups), and the candidates GET names them as the shared
// ranks accepted (LeagueSharedRanks). Read through tx, so a write's
// transaction sees its own write. Pools are walked in name order.
func (e *Engine) openLeagueTies(tx state.StoreTx, compID string, comp *state.Competition) ([]openLeagueTie, error) {
	standings, err := e.computeStandingsFrom(tx, compID)
	if err != nil {
		return nil, err
	}
	pools, err := tx.LoadPools(compID)
	if err != nil {
		return nil, err
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		return nil, err
	}
	matches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	band := newBreakBand(comp, pools, bracket)
	isTeam := comp.IsTeam()
	poolNames := make([]string, 0, len(standings))
	for poolName := range standings {
		poolNames = append(poolNames, poolName)
	}
	sort.Strings(poolNames)
	var out []openLeagueTie
	for _, poolName := range poolNames {
		poolStandings := standings[poolName]
		for _, positions := range detectPoolTies(poolStandings) {
			group := standingsAt(poolStandings, positions)
			if !band(poolName, positions, group) || tieSettled(group, matches, isTeam) {
				continue
			}
			out = append(out, openLeagueTie{pool: poolName, group: TiedGroup{
				Teams:       group,
				MinPosition: positions[0] + 1,
				MaxPosition: positions[len(positions)-1] + 1,
			}})
		}
	}
	return out, nil
}

// unsettledLeagueGroups keys each of openLeagueTies' ties by its pool, its
// best place and its members' participant ids (leagueGroupKey), so a
// correction that moves a tie to another place, or changes who is in it, makes
// a different key.
func (e *Engine) unsettledLeagueGroups(tx state.StoreTx, compID string, comp *state.Competition) (map[string]bool, error) {
	ties, err := e.openLeagueTies(tx, compID, comp)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(ties))
	for _, t := range ties {
		out[leagueGroupKey(t.pool, t.group.MinPosition, t.group.Teams)] = true
	}
	return out, nil
}

// LeagueSharedRanks lists the ties a team league's accepted shared ranks cover
// (the candidates GET's `accepted`, which the Undo control names): the ties in
// the band its table holds now that no tie-breaker round or lots have ordered
// (openLeagueTies). The flag is cleared by any correction that leaves a tie it
// did not cover (reconcileLeagueAfterWrite), so these are the ties the
// operator accepted. Empty when the competition is not a team league or its
// shared ranks were not accepted; NotFoundError for a missing competition.
func (e *Engine) LeagueSharedRanks(compID string) ([]TiedGroup, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Format != state.CompFormatLeague || !comp.IsTeam() || !comp.LeagueTiebreakFinalized {
		return nil, nil
	}
	ties, err := e.openLeagueTies(e.store, compID, comp)
	if err != nil {
		return nil, err
	}
	out := make([]TiedGroup, len(ties))
	for i, t := range ties {
		out[i] = t.group
	}
	return out, nil
}

// OpenDrawnSupplementaryBouts lists the bouts of matches (the competition's
// pool rows, read through tx) that were finished as a draw and still settle
// nothing (drawnSupplementaryBout and not poolMatchSettled): a drawn bout whose
// tie the lots have ranked, or that a correction has split or moved out of the
// band a tie is broken in, decides no place any more and is left out. Accepting
// the shared ranks asks it, so a drawn bout that settles is no reason to refuse.
func (e *Engine) OpenDrawnSupplementaryBouts(tx state.StoreTx, compID string, matches []state.MatchResult) ([]state.MatchResult, error) {
	lots, err := loadPoolLots(matches, sync.OnceValues(func() (map[string][]state.PlayerStanding, error) {
		return e.computeStandingsFrom(tx, compID)
	}), func() (map[string]domain.CompetitorStatus, error) {
		return tx.LoadCompetitorStatus(compID)
	})
	if err != nil {
		return nil, err
	}
	var open []state.MatchResult
	for i := range matches {
		if drawnSupplementaryBout(&matches[i]) && !poolMatchSettled(&matches[i], lots) {
			open = append(open, matches[i])
		}
	}
	return open, nil
}

// leagueGroupKey names a tied group by its pool, best place and members'
// participant ids (sorted), the identity a correction is compared on.
func leagueGroupKey(poolName string, minPosition int, group []state.PlayerStanding) string {
	ids := make([]string, len(group))
	for i, s := range group {
		ids[i] = s.Player.ID
	}
	sort.Strings(ids)
	return fmt.Sprintf("%s|%d|%s", poolName, minPosition, strings.Join(ids, ","))
}

// leagueBeforeWrite is what reconcileLeagueAfterWrite compares a league's
// table against: read in the write's transaction, before the write.
type leagueBeforeWrite struct {
	// finished: the league was completed, and groups are the ties that were
	// on its table then (accepted as shared ranks when the flag is set).
	finished bool
	groups   map[string]bool
	// acceptedInPlay: the league was in match play with its shared ranks
	// accepted (a flag set before finalize asked for a finished table).
	acceptedInPlay bool
}

// leagueStateBeforeWrite reads what a league pool match's write is judged
// against afterwards (reconcileLeagueAfterWrite), in the write's transaction: a
// regular match or a tie-breaker bout, whose correction can leave the round in
// a circle. nil for anything else (another format, an engi league, a league in
// play with nothing accepted), so no other write pays for a standings read.
func (e *Engine) leagueStateBeforeWrite(tx state.StoreTx, compID, matchID string) (*leagueBeforeWrite, error) {
	if !IsPoolMatchID(matchID) {
		return nil, nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return nil, fmt.Errorf("RecordMatchResultWithIneligibilityTx: load competition %s: %w", compID, err)
	}
	if comp == nil || comp.Format != state.CompFormatLeague || comp.Engi {
		return nil, nil
	}
	switch {
	case comp.Status == state.CompStatusComplete:
		groups, err := e.unsettledLeagueGroups(tx, compID, comp)
		if err != nil {
			return nil, err
		}
		return &leagueBeforeWrite{finished: true, groups: groups}, nil
	case comp.LeagueTiebreakFinalized:
		return &leagueBeforeWrite{acceptedInPlay: true}, nil
	}
	return nil, nil
}

// reconcileLeagueAfterWrite keeps a league's status and accepted shared ranks
// true to its table after a regular match's write landed, in the write's own
// transaction (so the status moves with the result or not at all):
//
//   - in match play, a result change takes back shared ranks accepted early:
//     the table they were accepted on is gone;
//   - on a finished league, a match left unfinished, or a tie owing a decision
//     that was not on the table before the write (a new tie, a tie at another
//     place, or one with other members), puts the league back into match play
//     (reopenLeagueTx), so the tie is asked again. A correction that keeps the
//     accepted ties, or settles one, leaves the league finished, and its table
//     and podium follow the corrected result.
//
// A reopen is noted for the door's auto-complete pass, which reports it as
// AutoCompleteStarted (competition_started).
func (e *Engine) reconcileLeagueAfterWrite(tx state.StoreTx, compID string, before *leagueBeforeWrite, result *state.MatchResult) error {
	if before == nil {
		return nil
	}
	comp, err := tx.LoadCompetition(compID)
	if err != nil || comp == nil {
		return err
	}
	if before.acceptedInPlay {
		if comp.Status != state.CompStatusPools || !comp.LeagueTiebreakFinalized || (result.Merge != nil && !result.Merge.ResultChanged) {
			return nil
		}
		comp.LeagueTiebreakFinalized = false
		return tx.SaveCompetition(comp)
	}
	if !before.finished || comp.Status != state.CompStatusComplete {
		return nil
	}
	reopen, err := e.leagueTableUnsettled(tx, compID, comp, before.groups)
	if err != nil || !reopen {
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

// leagueTableUnsettled reports whether a finished league's table no longer
// stands: a regular match is not finished, or a tie owes a decision that
// accepted (the ties on the table before the write) does not hold.
func (e *Engine) leagueTableUnsettled(tx state.StoreTx, compID string, comp *state.Competition, accepted map[string]bool) (bool, error) {
	matches, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return false, err
	}
	for i := range matches {
		if !state.IsPoolSupplementaryMatchID(matches[i].ID) && matches[i].Status != state.MatchStatusCompleted {
			return true, nil
		}
	}
	after, err := e.unsettledLeagueGroups(tx, compID, comp)
	if err != nil {
		return false, err
	}
	for k := range after {
		if !accepted[k] {
			return true, nil
		}
	}
	return false, nil
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

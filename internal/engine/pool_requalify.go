package engine

// pool_requalify.go keeps a mixed competition's knockout seated with the
// competitors its pools actually produced when a pool result is CORRECTED
// after the knockout has been seeded, or when a pool's order is set by hand
// (a pool-rank override, OverridePoolRank). Both doors answer through
// answerRequalification; the bare-store resolver (ResolveQualifiedPools) is
// only the safety net behind them, all-or-nothing per pool (resolveSlots).
//
// Operator rulings: "Save correction should just save what the operator
// enters"; "Everything should be able to be fixed, in case of a wrong entry.
// The operator just needs to be aware of the consequences, if it affects
// downstream matches"; and, for a pool fix that changes who qualified after
// the knockout started: warn, naming the affected knockout matches, and on
// "Proceed anyway" reopen them and seat the new qualifier.
//
// The change is measured against the BRACKET, not against the standings
// before the write: the question is "does the competitor sitting in each slot
// this pool feeds still hold the place that seated them?", and only the
// bracket knows who is sitting there. Comparing standings before and after
// missed a 1st/2nd swap (the same two people still qualify), the "-2nd" slots
// of the extra-qualifier modes, a correction made while the knockout was still
// untouched (nothing re-seated it once the competition reached knockout
// status), and a correction finished after a reopen (the reopened match had
// already dropped out of the "before" standings).

import (
	"fmt"
	"log"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// poolWriteCanMoveQualifiers reports whether a write of matchID that leaves it
// in status can change who holds one of its pool's qualifying places: a pool
// match (a tie-break or pool daihyosen row included, since those decide places
// too) left completed. A running or scheduled write (the live autosave, an
// undo, a reopen) leaves its pool incomplete, and an incomplete pool seats
// nobody, so it can move no qualifier. The ONE statement of that rule, shared
// by the in-transaction planner's door (requalifyAfterPoolWrite) and the
// after-write auto-complete (MaybeAutoCompletePoolsAfterWrite), so the two
// cannot disagree about which writes are worth the pool reads.
func poolWriteCanMoveQualifiers(matchID string, status state.MatchStatus) bool {
	return IsPoolMatchID(matchID) && status != state.MatchStatusRunning && status != state.MatchStatusScheduled
}

// requalifyPlan is what a pool change means for the knockout it feeds.
type requalifyPlan struct {
	// resolver maps each of the written pool's qualifying labels onto who
	// should now sit in its slots; a place the corrected standings leave tied
	// maps onto the label itself, the value the slot held at draw time.
	resolver map[string]resolvedFinisher
	// repaint reports that some slot's (name, id) differs from the resolver,
	// including an id backfill that changes nobody.
	repaint bool
	// changes are the places whose OCCUPANT changes, by rank.
	changes []QualifierChange
	// affected are the matches (in bracket order) that the old occupant has
	// already fought: closed with a result of their own.
	affected []string
	blocking []ReopenedMatch
	// displaced is the competitor sitting in blocking[0]'s moved slot: the
	// name the operator sees in that match, which is not necessarily the
	// first place's old occupant (only the 2nd-place match may be played).
	displaced string
	// running are the matches the move reaches that are being fought now.
	running []ReopenedMatch
}

// planRequalification reads, inside the write's own transaction and AFTER the
// pool write, what that write does to the knockout. nil means nothing: a
// non-mixed competition, a pool not complete (a reopened match, a tie-break
// still unplayed), a bracket with no draw labels yet (the bare-store resolver
// backfills those first), or no slot whose occupant moves. An engi (kata)
// competition is planned like any other; only the tie check is skipped for it
// (see below).
//
// Only OCCUPIED slots are considered. A slot still holding its label is being
// seated for the first time, which stays advanceMixedPools' job after the
// transaction: seating it here would leave that door nothing to report, and
// the "knockout match now playable" broadcast it sends would never go out.
func (e *Engine) planRequalification(tx state.StoreTx, compID string, comp *state.Competition, poolName string) (*requalifyPlan, error) {
	if comp == nil || comp.Format != state.CompFormatMixed {
		return nil, nil
	}
	bracket, err := tx.LoadBracket(compID)
	if err != nil {
		// Fail closed: a bracket that cannot be read cannot be shown safe.
		return nil, fmt.Errorf("requalification: load bracket for %s: %w", compID, err)
	}
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil, nil
	}
	if !bracketHasDrawPlaceholders(bracket) {
		log.Printf("engine: requalification skipped for %s: its bracket predates draw labels; the pool resolver backfills them on its next run", compID)
		return nil, nil
	}
	// Standings are computed at most once, and only when needed: an
	// individual pool's completion does not read them, so an incomplete one
	// returns before paying for them.
	var standings map[string][]state.PlayerStanding
	standingsOnce := func() (map[string][]state.PlayerStanding, error) {
		if standings != nil {
			return standings, nil
		}
		st, serr := e.computeStandingsFrom(tx, compID)
		if serr != nil {
			return nil, fmt.Errorf("requalification: standings for %s: %w", compID, serr)
		}
		standings = st
		return st, nil
	}
	completed, err := e.poolCompletion(tx, compID, comp, standingsOnce)
	if err != nil {
		return nil, fmt.Errorf("requalification: pool completion for %s: %w", compID, err)
	}
	if !completed[poolName] {
		return nil, nil
	}
	if _, err := standingsOnce(); err != nil {
		return nil, err
	}
	pools, err := tx.LoadPools(compID)
	if err != nil {
		return nil, fmt.Errorf("requalification: load pools for %s: %w", compID, err)
	}
	var pool *helper.Pool
	for i := range pools {
		if pools[i].PoolName == poolName {
			pool = &pools[i]
			break
		}
	}
	if pool == nil {
		return nil, nil
	}
	ranksNeeded := comp.MatchWinnerRanksNeeded()
	resolver := qualifierResolver([]helper.Pool{*pool}, map[string]bool{poolName: true}, standings, ranksNeeded)

	// A tie the corrected standings leave on a place the injectors would break
	// is not a finisher: that place returns to its label until the tie-break
	// is fought, and advanceMixedPools seats the winner then.
	//
	// Not for engi: it has no supplementary bouts (the injectors heal any
	// that appear), and every engi standing carries Points 0, so the tie
	// check would read every place as tied and send each slot back to its
	// label. Engi places are decided by the standings' own order.
	tied := map[string]bool{}
	if !comp.Engi {
		tied, err = e.tiedQualifyingLabels(tx, compID, comp, poolName, standings[poolName], ranksNeeded)
		if err != nil {
			return nil, err
		}
	}
	for label := range tied {
		resolver[label] = resolvedFinisher{Name: label}
	}

	plan := &requalifyPlan{resolver: resolver}
	byLabel := map[string]*QualifierChange{}
	seenAffected := map[string]bool{}
	seenRunning := map[string]bool{}
	visit := func(m *state.BracketMatch) {
		check := func(label, name, id string) {
			rf, ok := resolver[label]
			if !ok || (name == rf.Name && id == rf.ID) || isUnresolvedBracketSide(name) {
				return
			}
			plan.repaint = true
			if !occupantChanges(name, id, rf) {
				return // an id backfill: the same competitor stays
			}
			if byLabel[label] == nil {
				rank := rankOfLabel(poolName, label, ranksNeeded)
				c := QualifierChange{Pool: poolName, Rank: rank, Place: helper.GetOrdinal(rank), From: QualifierIdentity{Name: name, ID: id}, Tied: tied[label]}
				if !c.Tied {
					c.To = QualifierIdentity(rf)
				}
				byLabel[label] = &c
			}
			ref := bracketMatchRef(m)
			switch {
			case m.Status == state.MatchStatusRunning:
				if !seenRunning[m.ID] {
					seenRunning[m.ID] = true
					plan.running = append(plan.running, ref)
				}
			case bracketMatchCarriesOwnResult(m):
				if !seenAffected[m.ID] {
					seenAffected[m.ID] = true
					if len(plan.blocking) == 0 {
						plan.displaced = name
					}
					plan.affected = append(plan.affected, m.ID)
					plan.blocking = append(plan.blocking, ref)
				}
			}
		}
		check(m.PlaceholderA, m.SideA, m.SideAID)
		check(m.PlaceholderB, m.SideB, m.SideBID)
		check(m.PlaceholderWinner, m.Winner, m.WinnerID)
	}
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			visit(&bracket.Rounds[ri][mi])
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		visit(bracket.ThirdPlaceMatch)
	}
	if !plan.repaint {
		return nil, nil
	}
	for _, c := range byLabel {
		plan.changes = append(plan.changes, *c)
	}
	sort.Slice(plan.changes, func(i, j int) bool { return plan.changes[i].Rank < plan.changes[j].Rank })
	return plan, nil
}

// occupantChanges reports whether seating rf in a slot now holding (name, id)
// replaces its competitor. By id when both carry one; by name otherwise, the
// BracketMatch carve-out for the id-less shapes (a slot resolved before
// bracket ids existed keeps its name and gains its id, which moves nobody).
//
// Known limit, accepted: on a slot that still carries no id (a legacy bracket
// resolved before per-side ids existed, not yet backfilled) the comparison is
// by NAME, so two namesakes from different dojos, which the roster allows, are
// indistinguishable there. A correction that swaps one namesake for the other
// in such a slot reads as no move: nothing is warned about or reopened, and an
// unplayed slot simply takes the new one's id. The first resolver pass over a pool
// stamps its slots' ids, so the window is a bracket drawn before bc-brid whose
// pool has not been looked at since.
func occupantChanges(name, id string, rf resolvedFinisher) bool {
	if id != "" && rf.ID != "" {
		return id != rf.ID
	}
	return name != rf.Name
}

// rankOfLabel returns the rank a pool's label stands for ("Pool A-2nd" -> 2).
func rankOfLabel(poolName, label string, ranksNeeded int) int {
	for rank := 1; rank <= ranksNeeded; rank++ {
		if qualifierLabel(poolName, rank) == label {
			return rank
		}
	}
	return 0
}

// tiedQualifyingLabels returns the pool's qualifying labels whose rank sits in
// a tied group that still owes a supplementary bout (pendingTieBreaks, the
// injectors' own rule), read through the transaction.
func (e *Engine) tiedQualifyingLabels(tx state.StoreTx, compID string, comp *state.Competition, poolName string, poolStandings []state.PlayerStanding, ranksNeeded int) (map[string]bool, error) {
	rows, err := tx.LoadPoolMatches(compID)
	if err != nil {
		return nil, fmt.Errorf("requalification: load pool matches for %s: %w", compID, err)
	}
	daihyosen := comp.TeamSize > 0
	var existing []state.MatchResult
	court, courtSet := "", false
	for _, m := range rows {
		if pn, ok := poolNameFromMatchID(m.ID); !ok || pn != poolName {
			continue
		}
		if !courtSet {
			court, courtSet = m.Court, true
		}
		if (daihyosen && IsPoolDaihyosenMatchID(m.ID)) || (!daihyosen && IsTiebreakerMatchID(m.ID)) {
			existing = append(existing, m)
		}
	}
	tied := map[string]bool{}
	for _, p := range pendingTieBreaks(comp, poolName, poolStandings, existing, court, daihyosen) {
		for _, pos := range p.positions {
			if pos+1 <= ranksNeeded {
				tied[qualifierLabel(poolName, pos+1)] = true
			}
		}
	}
	return tied, nil
}

// applyRequalification writes a plan the operator has cleared (nothing
// affected, or confirmed): every affected match is reopened for re-entry, the
// winner each had propagated is taken back out of a next round nobody has
// touched, and every slot of the pool is repainted. The eligibility half of
// each reopen runs AFTER the bracket write, never inside its callback, where
// the bare store's per-competition lock is already held.
func (e *Engine) applyRequalification(tx state.StoreTx, compID, reason string, plan *requalifyPlan) ([]ReopenedMatch, error) {
	var reopened []ReopenedMatch
	err := tx.UpdateBracket(compID, func(b *state.Bracket) error {
		if b == nil {
			return errMatchNotFound
		}
		reopened = nil
		for _, id := range plan.affected {
			for ri := range b.Rounds {
				for mi := range b.Rounds[ri] {
					if b.Rounds[ri][mi].ID != id {
						continue
					}
					reopened = append(reopened, reopenDisplacedBracketMatch(&b.Rounds[ri][mi], reason))
					retractIntoUntouched(b, ri, mi)
				}
			}
			if b.ThirdPlaceMatch != nil && b.ThirdPlaceMatch.ID == id {
				reopened = append(reopened, reopenDisplacedBracketMatch(b.ThirdPlaceMatch, reason))
			}
		}
		e.resolveSlots(b, plan.resolver)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("requalification: update bracket for %s: %w", compID, err)
	}
	e.restoreForceReopened(tx, compID, reopened)
	return reopened, nil
}

// requalifyMixedPoolWrite is the pool-write door's call into
// requalifyAfterPoolWrite, shared by the kendo path and the engi seam of
// RecordMatchResultWithIneligibilityTx so both answer a pool correction the
// same way: nothing for a competition that is not mixed, and what the forced
// correction reopened appended to fo.Reopened.
func (e *Engine) requalifyMixedPoolWrite(tx state.StoreTx, compID string, comp *state.Competition, matchID string, written, prior *state.MatchResult, fo ForceOptions) error {
	if comp == nil || comp.Format != state.CompFormatMixed {
		return nil
	}
	requalified, err := e.requalifyAfterPoolWrite(tx, compID, comp, matchID, written, prior, fo.Force)
	if err != nil {
		return err
	}
	if fo.Reopened != nil && len(requalified) > 0 {
		*fo.Reopened = append(*fo.Reopened, requalified...)
	}
	return nil
}

// requalifyAfterPoolWrite is the pool-write door's whole knockout rule, run
// after the write landed and before anything else answers for it: a write
// that cannot move a qualifier (poolWriteCanMoveQualifiers) is let through,
// and any other is answered by answerRequalification, with rolling the pool
// row back to prior as its undo. It returns the matches it reopened.
func (e *Engine) requalifyAfterPoolWrite(tx state.StoreTx, compID string, comp *state.Competition, matchID string, result, prior *state.MatchResult, force bool) ([]ReopenedMatch, error) {
	if !poolWriteCanMoveQualifiers(matchID, result.Status) {
		return nil, nil
	}
	poolName, ok := poolNameFromMatchID(matchID)
	if !ok {
		return nil, nil
	}
	rollback := func() {
		if prior != nil {
			e.rollbackMatchResultTx(tx, compID, matchID, prior)
		}
	}
	return e.answerRequalification(tx, compID, comp, poolName, matchID, downstreamReopenReason(matchID), force, rollback)
}

// answerRequalification is the knockout rule shared by every door that moves
// a pool's standings inside a transaction: a pool match written
// (requalifyAfterPoolWrite) or a pool rank set by hand (OverridePoolRank). It
// runs after the change landed and before anything else answers for it.
// matchID is the pool match a correction wrote, "" for a door with no match;
// reason is the note each reopened match carries; undo takes the change back
// and runs before every refusal or error:
//
//   - a match the move reaches is being fought: DownstreamKnockoutRunningError,
//     never confirmable (checked first, so the operator is never asked to
//     confirm something that would then be refused);
//   - a match the old qualifier already fought, without force:
//     DownstreamKnockoutPlayedError naming them and the places that move;
//   - otherwise the plan is applied (with force, the fought matches reopen).
func (e *Engine) answerRequalification(tx state.StoreTx, compID string, comp *state.Competition, poolName, matchID, reason string, force bool, undo func()) ([]ReopenedMatch, error) {
	plan, err := e.planRequalification(tx, compID, comp, poolName)
	if err != nil {
		undo()
		return nil, err
	}
	if plan == nil {
		return nil, nil
	}
	if len(plan.running) > 0 {
		undo()
		return nil, &DownstreamKnockoutRunningError{MatchID: matchID, Running: plan.running}
	}
	if len(plan.affected) > 0 && !force {
		undo()
		return nil, &DownstreamKnockoutPlayedError{
			MatchID:         matchID,
			BlockingMatchID: plan.blocking[0].ID,
			Blocking:        plan.blocking,
			Displaced:       plan.displaced,
			QualifierChange: plan.changes,
		}
	}
	reopened, err := e.applyRequalification(tx, compID, reason, plan)
	if err != nil {
		undo()
		return nil, err
	}
	return reopened, nil
}

package engine

import (
	"errors"
	"fmt"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// lookupPoolRankOverride resolves a manual pool-rank override
// (state.Overrides.PoolRanks[poolName]) against a single competitor,
// identified by participant id ONLY, exactly as SaveRankOverrideChanged
// writes it (helper.CompetitorKey(id, "", ""), which resolves to "id:"+id).
// A row without an id resolves to nothing (operator ruling bc-pnum): a
// legacy bare-name-keyed entry, from an overrides.json written before
// identity keys existed at all, is not read back by anyone. An operator
// carrying such a file forward re-records the override through the current
// chusen/override-rank flow, which always writes the identity-keyed form.
func lookupPoolRankOverride(overrides map[string]int, id string) (int, bool) {
	if len(overrides) == 0 || id == "" {
		return 0, false
	}
	rank, ok := overrides[helper.CompetitorKey(id, "", "")]
	return rank, ok
}

// OverridePoolRank records a manual pool rank for one competitor, identified
// by participant id only (the chusen door: PUT .../pools/:poolId/override-rank),
// and answers for what the pool's new order does to the knockout it feeds
// exactly as a pool result correction does (answerRequalification, operator
// ruling: "Everything should be able to be fixed, in case of a wrong entry. The
// operator just needs to be aware of the consequences"). A place whose
// occupant moves is repainted; a knockout match the old qualifier already
// fought is named and refused until fo.Force confirms it, which reopens it
// with the new qualifier seated; a knockout match being fought refuses the
// override outright. Before this door went through the planner it changed
// the standings with no answer at all, and the next resolver pass kept the
// played slot's competitor while repainting the pool's other slot to the same
// person (one competitor seated twice).
//
// overrides.json serializes on the store-wide lock, never the per-competition
// one (computeStandingsFrom reads it inside transactions, so moving it would
// self-deadlock), so it is written directly rather than staged: taken inside
// the transaction (compLock then s.mu, the documented order) and put back by
// hand on any refusal, and when the transaction fails without committing. A
// transaction that committed and then failed to apply (state.ErrTxCommitted)
// keeps it: its WAL is replayed on restart, reopening the knockout for this
// order, so taking the override back would repaint the old qualifier over
// that reopen (undoAfterFailedTx). Returns whether the override changed
// anything.
func (e *Engine) OverridePoolRank(compID, poolName, playerID string, rank int, opts ...ForceOptions) (bool, error) {
	fo := firstForceOptions(opts)
	var changed bool
	var undo func()
	var engErr error
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		changed, undo, engErr = e.overridePoolRankTx(tx, compID, poolName, playerID, rank, fo)
		// A refusal returns its error so nothing the planner staged commits.
		return engErr
	})
	if engErr != nil {
		return false, engErr
	}
	if txErr != nil {
		undoAfterFailedTx(txErr, undo)
		return false, txErr
	}
	return changed, nil
}

// undoAfterFailedTx runs undo, the hand-made rollback of work a transaction
// did outside its WAL, when txErr says the transaction was dropped, and
// reports whether it ran. A transaction whose WAL committed before its Apply
// failed (state.ErrTxCommitted) was not dropped: the next startup replays it,
// so its outside work must stay to match, and the kept change is logged.
func undoAfterFailedTx(txErr error, undo func()) bool {
	if undo == nil {
		return false
	}
	if errors.Is(txErr, state.ErrTxCommitted) {
		log.Printf("engine: transaction committed but not applied (%v); its work outside the WAL is kept for the replay on restart", txErr)
		return false
	}
	undo()
	return true
}

// overridePoolRankTx is OverridePoolRank's body under the per-competition
// lock. On success it hands back the undo for a transaction that then fails
// without committing (undoAfterFailedTx); every refusal and error has already
// run it.
func (e *Engine) overridePoolRankTx(tx state.StoreTx, compID, poolName, playerID string, rank int, fo ForceOptions) (bool, func(), error) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return false, nil, err
	}
	prior, err := e.store.LoadOverrides(compID)
	if err != nil {
		return false, nil, err
	}
	var priorRank int
	var hadPrior bool
	if prior != nil {
		priorRank, hadPrior = prior.PoolRanks[poolName][helper.CompetitorKey(playerID, "", "")]
	}
	changed, err := e.store.SaveRankOverrideChanged(compID, poolName, playerID, rank)
	if err != nil || !changed {
		return changed, nil, err
	}
	undo := func() {
		if rerr := e.store.RestoreRankOverride(compID, poolName, playerID, priorRank, hadPrior); rerr != nil {
			log.Printf("engine: OverridePoolRank %s/%s: putting back the refused override for %s failed: %v", compID, poolName, playerID, rerr)
		}
	}
	reason := fmt.Sprintf("reopened: the ranking of %s was changed", poolName)
	reopened, err := e.answerRequalification(tx, compID, comp, poolName, "", reason, fo.Force, undo)
	if err != nil {
		return false, nil, err
	}
	if fo.Reopened != nil {
		*fo.Reopened = append(*fo.Reopened, reopened...)
	}
	return true, undo, nil
}

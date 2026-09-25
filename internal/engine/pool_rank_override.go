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
// identified by participant id ONLY, exactly as SaveRankOverridesChanged
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

// RankOverride is one competitor's manual pool rank: the participant id and
// the rank to record for it.
type RankOverride struct {
	PlayerID string
	Rank     int
}

// OverridePoolRanks records manual pool ranks for one or more competitors of
// one pool, each identified by participant id only (the chusen door: PUT
// .../pools/:poolId/override-rank), as ONE change: every rank lands together
// and the pool's new order is answered for once, on the final order. A
// chusen's whole order goes through here in one call, because setting its
// ranks one at a time answers for orders nobody chose: changing A=1,B=2,C=3
// to C=1,B=2,A=3 passes through A=3,B=2,C=3, where B holds 1st and A and C
// tie for 2nd, so the fought knockout match 2nd place feeds was named, and
// reopened on confirmation, although the final order keeps B 2nd.
//
// The answer is the one a pool result correction gets
// (answerRequalification, operator ruling: "Everything should be able to be
// fixed, in case of a wrong entry. The operator just needs to be aware of the
// consequences"). A place whose occupant moves is repainted; a knockout match
// the old qualifier already fought is named and refused until fo.Force
// confirms it, which reopens it with the new qualifier seated; a knockout
// match being fought refuses the override outright. A refusal takes back
// EVERY rank of the group. Before this door went through the planner it
// changed the standings with no answer at all, and the next resolver pass kept
// the played slot's competitor while repainting the pool's other slot to the
// same person (one competitor seated twice).
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
// anything. The caller validates the group (ids in the pool, no competitor
// or rank twice); an empty group changes nothing.
func (e *Engine) OverridePoolRanks(compID, poolName string, ranks []RankOverride, opts ...ForceOptions) (bool, error) {
	if len(ranks) == 0 {
		return false, nil
	}
	fo := firstForceOptions(opts)
	var changed bool
	var undo func()
	var engErr error
	txErr := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		changed, undo, engErr = e.overridePoolRanksTx(tx, compID, poolName, ranks, fo)
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

// overridePoolRanksTx is OverridePoolRanks' body under the per-competition
// lock. On success it hands back the undo for a transaction that then fails
// without committing (undoAfterFailedTx); every refusal and error has already
// run it.
func (e *Engine) overridePoolRanksTx(tx state.StoreTx, compID, poolName string, ranks []RankOverride, fo ForceOptions) (bool, func(), error) {
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return false, nil, err
	}
	current, err := e.store.LoadOverrides(compID)
	if err != nil {
		return false, nil, err
	}
	// Every member's prior value is read before anything is written, so the
	// undo puts back the order as it stood, not a half-changed one.
	next := make(map[string]int, len(ranks))
	prior := make(map[string]state.PriorRank, len(ranks))
	for _, r := range ranks {
		next[r.PlayerID] = r.Rank
		var p state.PriorRank
		if current != nil {
			p.Rank, p.Present = current.PoolRanks[poolName][helper.CompetitorKey(r.PlayerID, "", "")]
		}
		prior[r.PlayerID] = p
	}
	changed, err := e.store.SaveRankOverridesChanged(compID, poolName, next)
	if err != nil || !changed {
		return changed, nil, err
	}
	undo := func() {
		if rerr := e.store.RestoreRankOverrides(compID, poolName, prior); rerr != nil {
			log.Printf("engine: OverridePoolRanks %s/%s: putting back the refused ranks failed: %v", compID, poolName, rerr)
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

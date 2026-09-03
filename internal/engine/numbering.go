package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RenumberCompetitors rewrites every competitor's Number from the competition's
// CURRENT NumberPrefix, so changing that prefix in Settings does not require
// discarding and regenerating the draw. Pool membership and draw order are
// untouched: this only restates each competitor's label.
//
// It is idempotent and unconditional: callers run it after every successful
// settings save, whether or not that save touched NumberPrefix. It snapshots
// the stored Number of every competitor, runs helper.NumberPools over the
// pools in place (the same primitive that numbers a fresh draw), and compares
// the two -- writing pools.csv back only when at least one Number actually
// differs. A save that left the prefix untouched is therefore a cheap
// read-compare-discard, a save that moved it rewrites the file, and a retry
// after a prior renumber failed (or a legacy pools.csv whose Number column
// was never populated, see G7) is healed by the very next save with no extra
// code.
//
// pools.csv column 7 is the only persisted home of Player.Number (BracketMatch
// has no such field and participants.csv does not persist it), so rewriting the
// pools file renumbers every surface. "No pools file" is not the same thing as
// "playoffs-only": a playoffs competition never has one (its numbers are
// composed from participant order on read, see
// mobileapp.mergePoolNumbersIntoPlayers); a mixed or league competition that
// has not been drawn YET has none either, because SavePools has not run; and a
// Swiss competition NEVER has one (its draw writes rounds to pool-matches.csv
// and never calls SavePools), so Swiss competitors carry no number on any
// surface and this call is a permanent no-op for them, a pre-existing gap this
// bead names rather than closes. In every case there is nothing on disk to
// rewrite, so this returns (false, nil).
//
// The load and the save share one WithTransaction, so a concurrent score write
// cannot interleave between reading the pools and writing them back.
//
// Returns whether pools.csv was actually rewritten, so a caller that only
// broadcasts a change notification when something changed (the settings PUT
// handler) does not have to re-derive that from its own before/after
// comparison -- this function already computed it to decide whether to save.
func (e *Engine) RenumberCompetitors(compID string) (bool, error) {
	var changed bool
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		comp, err := tx.LoadCompetition(compID)
		if err != nil {
			return fmt.Errorf("renumber %s: load competition: %w", compID, err)
		}
		if comp == nil {
			return notFoundErrorf("competition %s not found", compID)
		}

		pools, err := tx.LoadPools(compID)
		if err != nil {
			return fmt.Errorf("renumber %s: load pools: %w", compID, err)
		}
		if len(pools) == 0 {
			return nil
		}

		before := make([]string, 0, len(pools))
		for _, pool := range pools {
			for _, p := range pool.Players {
				before = append(before, p.Number)
			}
		}

		helper.NumberPools(pools, comp.NumberPrefix)

		i := 0
		for _, pool := range pools {
			for _, p := range pool.Players {
				if p.Number != before[i] {
					changed = true
				}
				i++
			}
		}
		if !changed {
			return nil
		}

		if err := tx.SavePools(compID, pools); err != nil {
			return fmt.Errorf("renumber %s: save pools: %w", compID, err)
		}
		return nil
	})
	return changed, err
}

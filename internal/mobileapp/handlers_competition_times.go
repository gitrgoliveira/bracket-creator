package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The two steps the settings PUT takes for a drawn competition's match times:
// a move of its date or start re-times the matches (DR1), and an Undo puts
// back exactly the times that move replaced (DR3). Both are saved in the same
// transaction as the settings, or neither is.

// retimeKey records only this competition's own date and start: a change to
// either that lands between the pre-lock read and the save is a retry and
// never a guess. A pause edit or another competition's move in that window is
// NOT guarded; the times it would have shifted are saved from the floors read
// before the lock, which is the same drift the dialog's preview already shows
// (the engine's NotFreshCount).
type retimeKey struct{ oldDate, oldStart, newDate, newStart string }

// pendingRetime is what a re-time reads from the store before the lock is
// taken, because the transaction may not call a public Store method.
type pendingRetime struct {
	inputs engine.RetimeInputs
	key    retimeKey
}

// readPendingRetime reads that for a save that moves a drawn competition's date
// or start, and returns nil for any other save. A refusal in its error is
// answered through retimeRefusal.
func readPendingRetime(store *state.Store, eng *engine.Engine, id string, comp *state.Competition) (*pendingRetime, error) {
	pre, err := store.LoadCompetition(id)
	if err != nil || pre == nil || pre.Status != state.CompStatusDrawReady ||
		(pre.Date == comp.Date && pre.StartTime == comp.StartTime) {
		return nil, nil
	}
	moved := *pre
	moved.Date, moved.StartTime = comp.Date, comp.StartTime
	in, err := eng.ReadRetimeInputs(pre, &moved)
	if err != nil {
		return nil, err
	}
	return &pendingRetime{inputs: in, key: retimeKey{pre.Date, pre.StartTime, comp.Date, comp.StartTime}}, nil
}

// drawnTimes is what applyDrawnTimes did.
type drawnTimes struct {
	// retime is the outcome of the re-time it ran, nil when none ran.
	retime *engine.RetimeOutcome
	// restored is true when an Undo put back at least one time.
	restored bool
	// refusal is set when the step refused: nothing was written, and the
	// handler answers it once the lock is released.
	refusal *txResponse
}

// applyDrawnTimes runs inside the settings transaction, after current has
// taken the saved settings. before is the competition as it was stored. A
// competition that is not drawn has no match times to move, so only an Undo is
// refused for it.
func applyDrawnTimes(tx state.StoreTx, before, current *state.Competition, restore *[]engine.MatchTimeChange, pending *pendingRetime) (drawnTimes, error) {
	if current.Status != state.CompStatusDrawReady {
		if restore != nil {
			return drawnTimes{refusal: retimeRefusal(engine.ErrUndoStale)}, nil
		}
		return drawnTimes{}, nil
	}
	if restore != nil {
		// The exact Undo: write the times the move replaced, instead of the
		// re-time the date change would run. Nothing is staged before every
		// listed match has been checked.
		if err := engine.RestoreMatchTimesTx(tx, current, *restore); err != nil {
			return refuseOrFail(err)
		}
		return drawnTimes{restored: len(*restore) > 0}, nil
	}
	if before.Date == current.Date && before.StartTime == current.StartTime {
		return drawnTimes{}, nil
	}
	if pending == nil || pending.key != (retimeKey{before.Date, before.StartTime, current.Date, current.StartTime}) {
		return drawnTimes{refusal: &txResponse{status: http.StatusConflict, body: gin.H{"code": "retime_retry", "error": "This competition changed while you were moving it. Nothing was moved; try again."}}}, nil
	}
	out, err := engine.RetimeDrawTx(tx, before, current, pending.inputs)
	if err != nil {
		return refuseOrFail(err)
	}
	return drawnTimes{retime: &out}, nil
}

// refuseOrFail turns an error a re-time or restore refuses with into the
// refusal to answer, and any other error into a failure.
func refuseOrFail(err error) (drawnTimes, error) {
	if refusal := retimeRefusal(err); refusal != nil {
		return drawnTimes{refusal: refusal}, nil
	}
	return drawnTimes{}, err
}

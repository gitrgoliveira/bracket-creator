package engine

import (
	"errors"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrDrawNotReady is the sentinel errors.Is finds under the refusal
// SetDrawHidden answers for a competition that is not draw-ready: only a
// draw waiting to start can be published or hidden. Handlers answer it
// 409 draw_not_ready.
var ErrDrawNotReady = errors.New("the draw can be published or hidden only while it waits to start")

// SetDrawHidden stores whether the draw of a draw-ready competition is kept
// off the public pages (Unpublish: hidden true; Publish: hidden false), in one
// UpdateCompetitionChanged, so the draw-ready check and the write are one step
// under the competition lock. It reports whether the stored config changed: a
// competition already in the asked state is answered (false, nil) with
// nothing written, so a repeated tap is harmless. A competition that is not
// draw-ready is refused with a ValidationError wrapping ErrDrawNotReady, and
// a missing one with a NotFoundError. The tournament mode is not judged here:
// state.Competition.DrawHiddenIn reads it at read time, and the routes refuse
// a self-run tournament before they call this.
func (e *Engine) SetDrawHidden(id string, hidden bool) (bool, error) {
	return e.store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, notFoundErrorf("competition %s not found", id)
		}
		if current.Status != state.CompStatusDrawReady {
			return nil, wrapValidationErrorf(ErrDrawNotReady, "%s (competition %s status: %s)", ErrDrawNotReady.Error(), id, current.Status)
		}
		if current.DrawHidden == hidden {
			return nil, nil
		}
		current.DrawHidden = hidden
		return current, nil
	})
}

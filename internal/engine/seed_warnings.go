package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// SeedWarnings reports the seed-placement constraints a competition's drawn
// pools could not honour (R2, D6 and D7 of specs/007-ekc-draw/spec.md): surplus
// seed ranks that had to be ignored because two seeds may never share a pool,
// and any half/quarter/shiaijo spread the configuration made impossible.
//
// It is a WARNING channel, never an error one. D7 is explicit that every
// configuration must produce a draw ("a seeding rule that can refuse to draw is
// worse than a seeding rule that degrades predictably"), so this is read AFTER
// the draw exists and reports what happened. Nothing is persisted: the answer
// is a pure function of the drawn pools and the shiaijo allocation, so it is
// recomputed on read and can never go stale against a redrawn competition.
//
// Returns nil - no warnings, and no error - for every competition that has no
// pools yet, no seeds, or nothing to report. A competition without seeds is a
// normal configuration and MUST be warning-free.
func (e *Engine) SeedWarnings(id string) []string {
	comp, err := e.store.LoadCompetition(id)
	if err != nil || comp == nil {
		return nil
	}
	pools, err := e.store.LoadPools(id)
	if err != nil || len(pools) == 0 {
		return nil
	}
	// Every warning below is about a SEED, so an unseeded competition is
	// answered without building anything. That is the common case and this runs
	// on every admin competition read, which otherwise paid a full draw
	// construction to be told there was nothing to say.
	if !helper.AnySeeded(pools) {
		return nil
	}
	numCourts := len(comp.Courts)
	draw := poolDraw(comp, pools, numCourts)
	if draw == nil {
		return nil
	}
	return helper.SeedPlacementWarnings(draw, pools, numCourts)
}

// seedingProblem turns a seed-validation failure into a sentence an operator can
// act on, naming the ranks that are actually missing rather than restating the
// rule.
//
// The admin console's seeding panel already says "seed gap detected: rank 1, 2,
// 3 are missing", but the draw can be started from screens where that panel is
// not on show, and "seed ranks must be sequential without gaps" alone does not
// say WHICH rank to go and type. So this re-reads the stored set (raw, since the
// validating read is what just refused it) and works out the gap.
//
// Falls back to the underlying error whenever the set cannot be re-read or the
// fault is something other than a gap, e.g. a duplicate rank, which the error
// already describes precisely.
func (e *Engine) seedingProblem(id string, cause error) string {
	raw, rerr := e.store.LoadSeedsRaw(id)
	if rerr != nil {
		return fmt.Sprintf("competition %s: %v", id, cause)
	}
	diagnosis := helper.SeedGapDiagnosis(raw)
	if diagnosis == "" {
		return fmt.Sprintf("competition %s: %v", id, cause)
	}
	return diagnosis + " " + seedGapRemedyDraw
}

// The remedy clause that rides behind helper.SeedGapDiagnosis here. Each
// boundary names the action available AT THAT boundary, so the write endpoints
// carry their own (internal/mobileapp/handlers_participants.go): an operator
// refused at the draw goes back to the seeding panel, while an API client that
// PUT a seeding is holding the list itself and has nothing to "clear".
const seedGapRemedyDraw = "Set the missing ranks or clear the seeds, then generate the draw again."

package engine

import (
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
	numCourts := len(comp.Courts)
	draw := poolDraw(comp, pools, numCourts)
	if draw == nil {
		return nil
	}
	return helper.SeedPlacementWarnings(draw, pools, numCourts)
}

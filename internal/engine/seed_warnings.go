package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// SeedWarningsFor reports the seed-placement constraints a competition's drawn
// pools could not honour (R2, D6 and D7 of specs/007-ekc-draw/spec.md): surplus
// seed ranks that had to be ignored because two seeds may never share a pool,
// and any half/quarter/shiaijo spread the configuration made impossible.
//
// It is a WARNING channel, never an error one. D7 is explicit that every
// configuration must produce a draw ("a seeding rule that can refuse to draw is
// worse than a seeding rule that degrades predictably"), so this is read AFTER
// the draw exists and reports what happened.
//
// Nothing is persisted: the answer is a pure function of the drawn pools and the
// competition's shiaijo count, so it is recomputed on read and cannot go stale
// against a REDRAWN competition. See the note at the count itself for why that
// count is comp.Courts and not something derived from the live schedule.
//
// Returns nil - no warnings, and no error - for a nil record and for every
// competition that has no pools yet, no seeds, no bracket to place seeds in, or
// nothing to report. A competition without seeds is a normal configuration and
// MUST be warning-free.
//
// It takes the RECORD, not the id, because the one caller
// (GET /competitions/:id/draw-warnings) has just loaded it to decide between 200
// and 404; an id-taking wrapper would re-load identical bytes behind a
// per-competition lock on every admin read. There was one, exported and reached
// only from tests, which is why this note now names the caller that exists
// rather than a second one that never got built.
func (e *Engine) SeedWarningsFor(comp *state.Competition) []string {
	if comp == nil {
		return nil
	}
	// Every warning here is about where a seed's qualifier LANDS IN A BRACKET:
	// which half, which quarter, which shiaijo, and which surplus rank had to be
	// dropped to keep two seeds out of one pool. A league or a Swiss has no
	// bracket, so none of that describes anything the operator will ever see --
	// and the console shows these unconditionally, under a heading about a draw.
	// Left ungated, a league with two seeds reported "Seed 2 ignored ... The
	// draw used seed 1" about a draw that is never built.
	if !CompetitionDrawsBracket(comp.Format) {
		return nil
	}
	pools, err := e.store.LoadPools(comp.ID)
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
	// The shiaijo this competition runs on, resolved through InheritedDrawCourts
	// like every other reader: an empty list MEANS "inherit the tournament's",
	// and reading the raw field instead built a 1-shiaijo draw for exactly the
	// legacy/imported records the rest of this bead protects -- reporting
	// half/quarter relaxations from a draw the workbook never renders.
	//
	// It is not a perfect record of what the draw was built on, because nothing
	// persists a FROZEN allocation; comp.Courts is the draw-time value for
	// anything drawn by this code (runDrawPipeline materialises it), but a later
	// settings PUT can still edit it. That is the remaining gap, and it is the
	// one worth closing if these warnings ever need to be exact.
	//
	// Counting the distinct shiaijo the pool matches run on was tried and
	// reverted: a match's court is data the operator reassigns constantly, so
	// moving ONE bout rewrote warnings that describe placement in the draw.
	numCourts := len(CompetitionCourts(e.store, comp))
	draw := poolDraw(comp, pools, numCourts)
	if draw == nil {
		return nil
	}
	return helper.SeedPlacementWarnings(draw, pools)
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

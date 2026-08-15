package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// SeedWarnings reports the seed-placement constraints a competition's drawn
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
// Returns nil - no warnings, and no error - for every competition that has no
// pools yet, no seeds, no bracket to place seeds in, or nothing to report. A
// competition without seeds is a normal configuration and MUST be warning-free.
func (e *Engine) SeedWarnings(id string) []string {
	comp, err := e.store.LoadCompetition(id)
	if err != nil || comp == nil {
		return nil
	}
	return e.SeedWarningsFor(comp)
}

// SeedWarningsFor is SeedWarnings for a caller that already holds the record.
// Both HTTP callers do: one has just loaded the competition to answer a detail
// GET, the other to decide between 200 and 404. Taking the record rather than
// the id saves them a second load of the identical bytes (a per-competition
// lock, a stat and a full copy of the courts and player slices) on every admin
// competition read.
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
	// The competition's own shiaijo count. It is not a perfect record of what
	// the draw was built on -- nothing persists that -- but it is the closest
	// thing available, and the alternatives are worse:
	//
	// Counting the distinct shiaijo the pool matches run on was tried and
	// reverted. A match's court is data the operator reassigns constantly, so
	// moving ONE bout to a neighbouring shiaijo changed the count and visibly
	// rewrote these warnings, which are supposed to describe placement in the
	// draw. comp.Courts moves far more rarely: an allocation cannot be shrunk
	// while live matches are still on the dropped shiaijo (CourtsStillInUse), so
	// in practice it only changes once a shiaijo's bouts are all fought.
	//
	// The real fix is to snapshot the allocation at draw time and read it back
	// here; until something persists it, this is the stabler of two proxies.
	numCourts := len(comp.Courts)
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

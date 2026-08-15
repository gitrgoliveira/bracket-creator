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
// shiaijo count, so it is recomputed on read and cannot go stale against a
// REDRAWN competition. That holds only because both inputs are taken from what
// was actually drawn -- the pools from disk, and the count from the shiaijo
// those pools' matches run on (drawnCourtCount), NOT from comp.Courts, which the
// operator can still edit while the competition runs.
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
	// The shiaijo count the LIVE draw was built on, not whatever the competition
	// is allocated at the moment of this read. These warnings describe where a
	// seed's qualifier landed, so they have to describe the draw that actually
	// exists: comp.Courts is editable while the competition runs (a shiaijo may
	// be dropped once its bouts are all fought), and recomputing on the new
	// count would report relaxations from a draw nobody ever played.
	//
	// Once pools are on disk, the number of distinct shiaijo their matches run
	// on IS that allocation -- AssignPoolsToCourts spreads the pools over
	// exactly the courts the draw used. Before the draw there are no pool
	// matches, and the current allocation is the right answer, because the
	// warning is then about the draw the operator is ABOUT to generate.
	numCourts := len(comp.Courts)
	if drawn := e.drawnCourtCount(comp.ID); drawn > 0 {
		numCourts = drawn
	}
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

// drawnCourtCount is how many distinct shiaijo this competition's pool matches
// are spread across, or 0 when there are none to tell. That count is the
// allocation the draw was built on, recovered from the schedule rather than
// re-read from a field the operator can still edit.
func (e *Engine) drawnCourtCount(compID string) int {
	matches, err := e.store.LoadPoolMatches(compID)
	if err != nil || len(matches) == 0 {
		return 0
	}
	seen := make(map[string]bool)
	for _, m := range matches {
		if m.Court != "" {
			seen[m.Court] = true
		}
	}
	return len(seen)
}

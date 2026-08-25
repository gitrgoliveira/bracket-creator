package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bracketMatchAsResult projects a stored BracketMatch into the MatchResult
// shape the eligibility / rollback paths consume. The SubResults slice is
// carried through so a rollback replay restores the full team-bout state;
// LoadBracket deep-copies, so the slice is safe to hand back without aliasing
// the store cache.
//
// This is the engine-internal projection only. The mobileapp handlers
// (handlers_daihyosen.go) build their own projection that additionally carries
// Court / ScheduledAt for scheduling, so they deliberately do NOT use this
// helper.
//
// It must be FAITHFUL, because matchWriteRestore reads it as the truth: an
// empty field in the snapshot means "this was empty", so anything omitted here
// is not preserved on rollback — it is actively cleared. A partial projection
// is therefore silent data loss, not a missing optimisation.
func bracketMatchAsResult(bm *state.BracketMatch) *state.MatchResult {
	// ModifiedAt IS projected, and used not to be. The old omission was
	// justified by a mechanism that does not exist: it claimed a projected
	// stamp would lose the timestamp LWW comparison against the write being
	// rolled back and be dropped. applyMatchWrite returns true for
	// matchWriteRestore BEFORE reading any stamp, so a restore can never lose
	// that comparison whatever it carries — the exemption is stated at the
	// POLICY, not earned by leaving this field 0.
	//
	// What the field actually decides is the stamp LEFT BEHIND, because the
	// write is a whole-struct overwrite. Omitting it zeroed the stored stamp,
	// so after a bracket rollback the NEXT write to that match took
	// ApplyByTimestamp's unstamped bypass and applied unconditionally — the
	// match silently lost its fencing precisely when a contested write had just
	// been rejected on it, which is when fencing matters most.
	//
	// The pool branch never had this hole: its snapshot comes from
	// lookupExistingResult, a straight copy of the stored MatchResult, so it
	// always restored the true prior stamp. This was a branch asymmetry, not a
	// property of "restore", and projecting the field removes it — the same
	// "a match is a match" argument applyMatchWrite already makes for refusing
	// to let the pool and the bracket arbitrate differently.
	return &state.MatchResult{
		ID:             bm.ID,
		SideA:          bm.SideA,
		SideB:          bm.SideB,
		Winner:         bm.Winner,
		Status:         bm.Status,
		Decision:       bm.Decision,
		DecisionBy:     bm.DecisionBy,
		DecisionReason: bm.DecisionReason,
		Encho:          bm.Encho,
		// BracketMatch persists ippon arrays natively (the same shape as
		// MatchResult), so this is a direct field copy; defensive so the
		// snapshot never aliases bm's backing arrays.
		IpponsA:  append([]string(nil), bm.IpponsA...),
		IpponsB:  append([]string(nil), bm.IpponsB...),
		HansokuA: bm.HansokuA,
		HansokuB: bm.HansokuB,
		// The operator-audit pair. Carried so a rollback restores the note the
		// match actually held, and — because applyBracketMatchResult assigns
		// them unconditionally under restore — clears one the rejected write
		// added. That matches applyPoolWrite, whose whole-struct overwrite has
		// always done both.
		ResultSource:     bm.ResultSource,
		CorrectionReason: bm.CorrectionReason,
		// FlagsA/FlagsB carry the engi referee-flag counts so a rollback
		// replay of an engi flag-scored bracket match restores them too.
		FlagsA:     bm.FlagsA,
		FlagsB:     bm.FlagsB,
		SubResults: bm.SubResults,
		// See the note above: this restores the match's real prior stamp so a
		// rolled-back bracket match stays fenced, instead of being left at 0
		// and letting the next write through unconditionally.
		ModifiedAt: bm.ModifiedAt,
	}
}

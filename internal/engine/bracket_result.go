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
	// ModifiedAt is deliberately NOT projected. Note carefully what that does
	// and does not do, because an earlier version of this comment had the
	// mechanism backwards and the wrong version is the more reassuring one.
	//
	// It does NOT decide whether the rollback applies. applyMatchWrite returns
	// true for matchWriteRestore before it looks at any stamp, so a restore can
	// never lose the LWW comparison whatever it carries — the bypass is stated
	// at the POLICY, not earned by leaving this field 0. The old claim that a
	// projected stamp would "lose to the write it is undoing and be silently
	// dropped" is not reachable.
	//
	// What it actually decides is the stamp LEFT on the match afterwards, since
	// the write is a whole-struct overwrite: omitting it zeroes the stored
	// stamp, so the next write to this match takes ApplyByTimestamp's unstamped
	// bypass instead of being fenced against the restored result's real time.
	// The pool snapshot behaves differently here — lookupExistingResult returns
	// a copy of the stored MatchResult, so that branch restores the true prior
	// stamp — which makes this an asymmetry between the two branches rather
	// than a property of "restore".
	//
	// Left as-is because changing it changes fencing behaviour, not because it
	// is provably right. If you are completing this projection, that is a
	// behavioural change to reason about deliberately, not a tidy-up.
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
	}
}

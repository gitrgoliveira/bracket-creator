package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	// A bracket match persists each side's score as one formatted string, while
	// MatchResult carries the ippon arrays. Without decoding it the snapshot has
	// no ippons at all, and the restore writes formatScore(nil, 0) — blanking
	// the score of the match it is supposed to be putting back.
	ipponsA, hansokuA := domain.ParseScore(bm.ScoreA)
	ipponsB, hansokuB := domain.ParseScore(bm.ScoreB)
	// ModifiedAt is deliberately NOT projected, and that is the one omission
	// which is correct. The restore runs through applyMatchWrite, the
	// timestamp LWW guard, against the stamp the REJECTED write just left on
	// the match. Carrying the snapshot's older stamp would make the rollback
	// lose to the write it is undoing and be silently dropped; leaving it 0
	// takes ApplyByTimestamp's unstamped bypass, so the rollback always
	// applies. Do not "complete" the projection with this field.
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
		IpponsA:        ipponsA,
		IpponsB:        ipponsB,
		HansokuA:       hansokuA,
		HansokuB:       hansokuB,
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

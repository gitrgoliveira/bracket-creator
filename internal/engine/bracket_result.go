package engine

import "github.com/gitrgoliveira/bracket-creator/internal/state"

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
func bracketMatchAsResult(bm *state.BracketMatch) *state.MatchResult {
	return &state.MatchResult{
		ID:              bm.ID,
		SideA:           bm.SideA,
		SideB:           bm.SideB,
		Winner:          bm.Winner,
		Status:          bm.Status,
		Decision:        bm.Decision,
		DecisionBy:      bm.DecisionBy,
		DecisionReason:  bm.DecisionReason,
		Encho:           bm.Encho,
		DecidedByHantei: state.HanteiPtr(bm.DecidedByHantei),
		// FlagsA/FlagsB carry the engi referee-flag counts so a rollback
		// replay of an engi flag-scored bracket match restores them too.
		FlagsA:     bm.FlagsA,
		FlagsB:     bm.FlagsB,
		SubResults: bm.SubResults,
	}
}

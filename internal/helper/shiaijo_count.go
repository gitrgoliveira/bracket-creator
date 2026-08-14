package helper

import "fmt"

// validShiaijoCounts is the complete set of legal shiaijo allocations for one
// competition, ascending. It is exactly the powers of two that fit inside the
// A-Z label cap: 32 would need 32 labels and MaxCourts is 26, so 16 is the
// practical ceiling rather than an arbitrary one.
var validShiaijoCounts = []int{1, 2, 4, 8, 16}

// ValidateShiaijoCount enforces the shiaijo-count rule (R9,
// specs/007-ekc-draw/spec.md) for a single competition: the allocation MUST be
// a POWER OF TWO -- 1, 2, 4, 8 or 16. Everything else is rejected, including
// EVEN counts such as 6 and 10.
//
// Why: the knockout draw gives each shiaijo its own block of the bracket and
// those blocks merge in PAIRS, so the count has to halve cleanly all the way
// down. An even-but-not-power-of-two count survives the first merge and then
// breaks: at 6 shiaijo a half holds 3 blocks, which cannot merge two into one,
// so one court's block reaches the semi-final a round earlier purely because
// of how many blocks there are. That also stops "one seeded pool per quarter"
// (R2) being well defined. A power of two keeps every block at the same depth
// and makes every half and quarter exact.
//
// A 1-shiaijo competition is explicitly ALLOWED: its single block splits into
// two half-blocks that merge with each other (R4(e)), so the draw has the same
// shape as a multi-court one. The error text therefore always names 1 as a
// valid answer, and must never read as "at least 2 shiaijo".
//
// The rule is per COMPETITION, not per venue. A venue may have ANY number of
// shiaijo -- 3, 5 and 7 are all perfectly legal tournaments -- so the
// tournament-level court list is deliberately NOT constrained by this
// function. A 5-shiaijo venue runs one competition on 4 and another on 1; a
// 3-shiaijo venue runs its competitions on 1 or 2 and simply never gives all
// three to one competition. Because of that, wherever a competition's
// allocation is DERIVED rather than chosen -- most importantly the create and
// import paths, which materialise an omitted court list from the tournament's
// -- it is the DERIVED value that has to be passed here, or a venue with an
// invalid count would smuggle it in by inheritance.
//
// n <= 1 returns nil. 1 is valid, and the "0 or negative courts" case belongs
// to ValidateCourts (and to the competition-level validators that read an
// empty court list as "inherit the tournament's courts").
//
// This is the single source of truth for every enforcement point: the CLI
// --courts flag (cmd/create-pools.go, cmd/create-playoffs.go and the web form
// in cmd/create_handler.go) and, through engine.ValidateCompetitionShiaijoCount,
// both the mobile API and the engine's draw pipeline. That one wraps this rule
// in the competition-level exemptions (empty list, non-bracket format), so the
// API and the pipeline cannot come to different answers. The clamps that LOWER
// a count rather than validate it share the same set through EffectiveDrawCourts
// (draw.go).
//
// TWO browser surfaces mirror this message in JS, and BOTH must be updated
// with it: shiaijoCountError in web-mobile/js/admin_helpers.jsx (the operator
// console) and shiaijoCountError in web/js/validation.js (the classic CLI web
// form, which has to reject a bad count client-side because the server answers
// a native form POST with JSON that would replace the page and lose the pasted
// roster). Both are pinned against this message from the Go side by
// TestShiaijoRuleJSMirrorsMatchTheGoMessage in shiaijo_count_test.go, which is
// what catches a rewording here that reaches only one of them.
func ValidateShiaijoCount(n int) error {
	if n <= 1 {
		return nil
	}
	for _, v := range validShiaijoCounts {
		if n == v {
			return nil
		}
	}
	return fmt.Errorf(
		"shiaijo count must be a power of two (1, 2, 4, 8 or 16), got %d: use %s, or 1; the knockout draw gives each shiaijo its own block of the bracket and the blocks merge in pairs, so the count has to halve cleanly",
		n, nearestShiaijoCounts(n),
	)
}

// nearestShiaijoCounts renders the legal counts an operator can reach from an
// illegal n: the power of two immediately below it and the one immediately
// above. Above 16 there is no "above" to offer, because 32 exceeds the A-Z
// label cap, so only the count below is named.
//
// n is always a rejected count here, so n >= 3 and the count below is always
// >= 2. That is why the caller can append ", or 1" unconditionally without
// ever producing "use 1, or 1".
func nearestShiaijoCounts(n int) string {
	below, above := 0, 0
	for _, v := range validShiaijoCounts {
		if v < n {
			below = v
		}
		if v > n && above == 0 {
			above = v
		}
	}
	if above == 0 {
		return fmt.Sprintf("%d", below)
	}
	return fmt.Sprintf("%d or %d", below, above)
}

// LargestShiaijoCountAtMost returns the biggest legal shiaijo allocation that
// does not exceed n, and is how every clamp that LOWERS a court count lands on
// a legal value instead of merely an even one. n < 1 returns 1: a competition
// always draws on at least one shiaijo.
//
// Splitting this out of the clamp keeps the legal set (validShiaijoCounts) the
// single thing a future change to the rule has to touch, so a clamp can never
// drift from the validator.
func LargestShiaijoCountAtMost(n int) int {
	best := 1
	for _, v := range validShiaijoCounts {
		if v <= n {
			best = v
		}
	}
	return best
}

package helper

import "fmt"

// ValidateCourtPairing enforces the shiaijo-count rule for a single
// competition: it must be allocated exactly 1 shiaijo, or an EVEN number of
// shiaijo. An odd allocation greater than 1 (3, 5, 7, ...) is rejected.
//
// Why: the knockout draw builds one bracket region per shiaijo and pairs
// those regions up, so each pool's runner-up crosses to its court's PARTNER
// court. Partner courts sit in opposite halves of the draw, which is also
// what guarantees a pool's two qualifiers can only meet in the final. With
// an odd count one court has no partner and its runners-up have nowhere to
// cross to.
//
// A 1-shiaijo competition is explicitly ALLOWED: its single region splits
// into two half-blocks that act as partner courts, so the draw has the same
// shape as a multi-court one. The error text therefore always names 1 as a
// valid answer, and must never read as "at least 2 courts".
//
// The rule is per COMPETITION, not per venue: a 5-shiaijo tournament may
// legitimately run one competition on 4 courts and another on 1, so the
// tournament-level court list is deliberately NOT constrained by this
// function.
//
// n <= 1 returns nil. 1 is valid, and the "0 or negative courts" case
// belongs to ValidateCourts (and to the competition-level validators that
// read an empty court list as "inherit the tournament's courts").
//
// This is the single source of truth for every enforcement point: the CLI
// --courts flag (cmd/create-pools.go, cmd/create-playoffs.go and the web
// form in cmd/create_handler.go), the mobile API
// (validateCompetitionCourts, internal/mobileapp/handlers_tournament.go),
// the engine draw pipeline (engine.ValidateCourtPairing) and, mirrored in
// JS, the operator UI (shiaijoCountError in
// web-mobile/js/admin_helpers.jsx, pinned against this message by that
// file's vitest suite).
func ValidateCourtPairing(n int) error {
	if n <= 1 || n%2 == 0 {
		return nil
	}
	return fmt.Errorf(
		"courts must be 1 or an even number, got %d: use %d or %d, or 1; the knockout draw pairs shiaijo so each pool's runner-up crosses to a partner shiaijo, and an odd number leaves one shiaijo without a partner",
		n, n-1, n+1,
	)
}

// Package mobileapp — see validation.go for the Validate() error
// pattern that request bodies use after JSON binding (Slice 0 / NFR-004).
//
// Pattern (used by `c.ShouldBindJSON(&req); req.Validate()`):
//
//  1. Define the body as a struct with explicit JSON tags.
//  2. Implement `Validate() error` on the struct (pointer receiver
//     when the struct is large) and return a typed `ValidationError`
//     describing the first failed field. Stop on the first failure —
//     handlers map ValidationError to HTTP 400 with the embedded message.
//  3. Handlers call `req.Validate()` immediately after `ShouldBindJSON`.
//     Anything more semantic (e.g. cross-resource lookups, store reads)
//     stays in the handler — Validate() handles only request-shape
//     invariants that don't need I/O.
//
// ScoreRequest is the example implementation. Other handler families
// will adopt the same pattern as later slices touch them.
package mobileapp

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Validator is the contract every request body should satisfy after
// JSON binding. Validate() returns nil when the body is well-formed
// against its own shape rules; ValidationError when it isn't.
type Validator interface {
	Validate() error
}

// ValidationError is a typed error returned by Validate() so handlers
// can distinguish shape errors (400) from store / engine errors (500).
// Handlers map ValidationError directly to a 400 with the Message body.
type ValidationError struct {
	// Field is the JSON field name that failed validation, or "" when
	// the failure spans multiple fields.
	Field string
	// Message is the operator-facing reason, designed to be returned
	// verbatim in the HTTP 400 response body.
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ScoreRequest is the body shape for `PUT /api/competitions/:id/matches/:mid/score`.
// It is the minimal example implementation of the Validator pattern (T015).
//
// Defined as a named type whose underlying type is state.MatchResult so
// the JSON shape is identical to the pre-Slice-0 endpoint (clients send
// MatchResult fields at the top level) — no client-side change. The
// named type lets us hang Validate() off it without polluting state
// (which is a pure-data package).
//
// As later slices add decision-type / encho fields (see Slice 3 FR-031,
// T077), the matching Validate() rules land here.
type ScoreRequest state.MatchResult

// Validate enforces request-shape invariants on a score payload before
// the engine touches it. Rules deliberately match the existing engine
// guards so behaviour is unchanged:
//
//   - Status, when set, must be one of the documented MatchStatus values.
//   - Winner, when set alongside both sides, must name one of the sides
//     (a Winner that names neither side would silently miscount in
//     standings).
//   - Decision (T077, FR-031, contracts/match-decisions.md):
//     value must be one of fought/hikiwake/kiken/fusenpai/fusensho/
//     daihyosen/kachinuki-exhaustion (or empty).
//     kiken/fusenpai require decisionBy and a winning-side scoreline
//     (2-0 in regulation, 1-0 in encho for kiken). fusensho is only
//     valid on a per-bout SubResult, not on a top-level score request.
func (r *ScoreRequest) Validate() error {
	if r.Status != "" {
		switch r.Status {
		case state.MatchStatusScheduled, state.MatchStatusRunning, state.MatchStatusCompleted:
			// ok
		default:
			return &ValidationError{
				Field:   "status",
				Message: fmt.Sprintf("must be one of scheduled/running/completed, got %q", r.Status),
			}
		}
	}
	// Winner, when supplied, must name one of the two sides. Empty
	// winner is permitted (draw or pre-completion update). We only
	// check when both sides AND winner are present in the request —
	// the engine's preserve-on-empty fallback handles the side-omitted
	// case.
	if r.Winner != "" && r.SideA != "" && r.SideB != "" {
		if r.Winner != r.SideA && r.Winner != r.SideB {
			return &ValidationError{
				Field:   "winner",
				Message: fmt.Sprintf("must equal sideA or sideB, got %q", r.Winner),
			}
		}
	}
	return r.validateDecision()
}

// validateDecision enforces the FR-031 / contracts/match-decisions.md
// rules. Splitting it out keeps Validate() at a glance.
func (r *ScoreRequest) validateDecision() error {
	switch r.Decision {
	case "", "fought", "hikiwake", "kiken", "fusenpai", "fusensho", "daihyosen", "kachinuki-exhaustion":
		// ok
	default:
		return &ValidationError{
			Field:   "decision",
			Message: fmt.Sprintf("unknown decision %q", r.Decision),
		}
	}
	if r.DecisionBy != "" && r.DecisionBy != "shiro" && r.DecisionBy != "aka" {
		return &ValidationError{
			Field:   "decisionBy",
			Message: fmt.Sprintf("must be 'shiro' or 'aka', got %q", r.DecisionBy),
		}
	}
	switch r.Decision {
	case "kiken":
		if r.DecisionBy == "" {
			return &ValidationError{Field: "decisionBy", Message: "required when decision is kiken"}
		}
		need := 2
		if r.Encho != nil {
			need = 1
		}
		if !winningScoreline(r.IpponsA, r.IpponsB, need) {
			return &ValidationError{
				Field:   "scoreline",
				Message: fmt.Sprintf("kiken requires %d-0 scoreline", need),
			}
		}
		if err := r.requireWinnerForDecision("kiken"); err != nil {
			return err
		}
	case "fusenpai":
		if r.DecisionBy == "" {
			return &ValidationError{Field: "decisionBy", Message: "required when decision is fusenpai"}
		}
		if !winningScoreline(r.IpponsA, r.IpponsB, 2) {
			return &ValidationError{Field: "scoreline", Message: "fusenpai requires 2-0 scoreline"}
		}
		if err := r.requireWinnerForDecision("fusenpai"); err != nil {
			return err
		}
	case "fusensho":
		return &ValidationError{
			Field:   "decision",
			Message: "fusensho is only valid on a per-bout sub-result, not a top-level match",
		}
	}
	return nil
}

// winningScoreline reports whether exactly one of the two ippon slices
// has `n` entries while the other is empty (i.e. an n-0 result).
func winningScoreline(ipponsA, ipponsB []string, n int) bool {
	a := len(ipponsA)
	b := len(ipponsB)
	return (a == n && b == 0) || (a == 0 && b == n)
}

// requireWinnerForDecision enforces that Winner is set when a kiken/
// fusenpai is recorded — the engine's eligibility side effect uses
// Winner as the canonical surviving side. Without this, a bulk-score
// or hand-crafted request could record an ineligibility against the
// wrong player.
func (r *ScoreRequest) requireWinnerForDecision(label string) error {
	if r.Winner == "" {
		return &ValidationError{
			Field:   "winner",
			Message: fmt.Sprintf("required when decision is %s (names the surviving side)", label),
		}
	}
	return nil
}

// AsMatchResult returns the underlying state.MatchResult value so the
// score handler can forward it to the engine. The conversion is a
// zero-cost type conversion (identical underlying layout).
func (r *ScoreRequest) AsMatchResult() *state.MatchResult {
	mr := state.MatchResult(*r)
	return &mr
}

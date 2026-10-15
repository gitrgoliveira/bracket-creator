package mobileapp

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// maxBasisOwnIDs bounds the event ids a write's basis may name: the device's
// own taps not yet acknowledged, a few hundred at the most.
const maxBasisOwnIDs = 1024

// writeTag is the optional pair a rule-bound write carries (bc-evts F1, Q9):
// the client's id for the write, so a resend of one the match's score event
// log already holds is answered as recorded, and the basis, what the board
// showed when the write was built (the score events it had seen, its own
// still unacknowledged ones). Both ride on the baseline the write leaves.
type writeTag struct {
	WriteID string            `json:"writeId,omitempty"`
	Basis   *state.ScoreBasis `json:"basis,omitempty"`
}

// validateTag refuses a tag no write could carry (a 400 for the request).
func (w writeTag) validateTag() error {
	if len(w.WriteID) > domain.MaxScoreEventIDLen {
		return fmt.Errorf("writeId is longer than %d characters", domain.MaxScoreEventIDLen)
	}
	if w.Basis == nil {
		return nil
	}
	if w.Basis.Seen < 0 || w.Basis.Seq < 0 {
		return fmt.Errorf("basis: seen and seq must not be negative")
	}
	if len(w.Basis.Own) > maxBasisOwnIDs {
		return fmt.Errorf("basis: own names more than %d events", maxBasisOwnIDs)
	}
	for _, id := range w.Basis.Own {
		if id == "" || len(id) > domain.MaxScoreEventIDLen {
			return fmt.Errorf("basis: own ids must be 1 to %d characters", domain.MaxScoreEventIDLen)
		}
	}
	return nil
}

// tagOnto puts the tag on a write the client built.
func (w writeTag) tagOnto(m *state.MatchResult) {
	m.WriteID, m.Basis = w.WriteID, w.Basis
}

// tagInto puts the tag on the options of a write a door builds on the server.
func (w writeTag) tagInto(fo engine.ForceOptions) engine.ForceOptions {
	fo.WriteID, fo.Basis = w.WriteID, w.Basis
	return fo
}

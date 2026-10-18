package mobileapp

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

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
// still unacknowledged ones), with the device's event session. All ride on
// the baseline the write leaves.
type writeTag struct {
	WriteID string            `json:"writeId,omitempty"`
	Basis   *state.ScoreBasis `json:"basis,omitempty"`
	// Session is the device's event session (the score-events batch's), so
	// the baseline carries it and basis.seq counts that device's own events
	// up to seq as seen (bc-evts S8 b). Meaningless without a basis.
	Session string `json:"session,omitempty"`
}

// validateTag refuses a tag no write could carry (a 400 for the request).
func (w writeTag) validateTag() error {
	if len(w.WriteID) > domain.MaxScoreEventIDLen {
		return fmt.Errorf("writeId is longer than %d characters", domain.MaxScoreEventIDLen)
	}
	if len(w.Session) > domain.MaxScoreEventFieldLen {
		return fmt.Errorf("session is longer than %d characters", domain.MaxScoreEventFieldLen)
	}
	// The server's own event session (engine/kachinuki_opener.go) is no
	// device's: a write claiming it could make its basis count the server's
	// fighter events as the writer's own.
	if w.Session == engine.ServerEventSession {
		return fmt.Errorf("session %q is the server's own", engine.ServerEventSession)
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
	m.WriteID, m.Basis, m.WriteSession = w.WriteID, w.Basis, w.Session
}

// tagInto puts the tag on the options of a write a door builds on the server.
func (w writeTag) tagInto(fo engine.ForceOptions) engine.ForceOptions {
	fo.WriteID, fo.Basis, fo.Session = w.WriteID, w.Basis, w.Session
	return fo
}

// bindOptionalWriteTag reads the optional {writeId, basis} body of a door
// that takes no other input (a send back to the queue, a kachinuki bout's
// removal): an empty body is the one-tap shape and carries no tag. It answers
// 400 itself for a malformed body or tag.
func bindOptionalWriteTag(c *gin.Context) (writeTag, bool) {
	var tag writeTag
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return tag, true
	}
	if err := c.ShouldBindJSON(&tag); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return writeTag{}, false
	}
	if err := tag.validateTag(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return writeTag{}, false
	}
	return tag, true
}

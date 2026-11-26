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
	// CourtStanding is the court's standing (bc-sseq FR8), never read from a
	// body: the door sets it once it has judged that the write came from a
	// court console of the tablet that runs the match's shiaijo, and it rides
	// to the baseline beside the write's id (MatchResult.CourtStanding,
	// ForceOptions.CourtStanding).
	CourtStanding bool `json:"-"`
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
	m.CourtStanding = w.CourtStanding
}

// tagInto puts the tag on the options of a write a door builds on the server.
func (w writeTag) tagInto(fo engine.ForceOptions) engine.ForceOptions {
	fo.WriteID, fo.Basis, fo.Session = w.WriteID, w.Basis, w.Session
	fo.CourtStanding = w.CourtStanding
	return fo
}

// courtControlStamp is a court console's stamp on a /score or /decision write
// (bc-sseq FR8): the tab that made the write and the tablet that tab belongs
// to (court_control.jsx's tab id and deviceId), both lowercase UUID v4.
// Request-only: the door judges it on arrival (courtWrite.judgeCourtStanding)
// and never stores it; only the registry keeps their one-way keys.
type courtControlStamp struct {
	TabID    string `json:"tabId"`
	DeviceID string `json:"deviceId"`
}

// courtWrite is the optional court control the two result doors (/score and
// /decision) take beside their tag. The other doors that take a tag take no
// court control: only a result has the court's standing.
type courtWrite struct {
	CourtControl *courtControlStamp `json:"courtControl,omitempty"`
}

// validateCourtControl refuses a court control no court console could send
// (a 400 for the request): both ids are the lowercase UUID v4 a console mints.
func (w courtWrite) validateCourtControl() error {
	if w.CourtControl == nil {
		return nil
	}
	if !state.IsClientUUID(w.CourtControl.TabID) || !state.IsClientUUID(w.CourtControl.DeviceID) {
		return errors.New("courtControl: tabId and deviceId must be lowercase UUID v4")
	}
	return nil
}

// judgeCourtStanding is the court's standing of a write stamped at stamp to
// the stored match snap, judged on arrival in the write's transaction stx: the
// shiaijo is the match's as stored there, so a court move made before the
// write arrived is judged by the shiaijo it moved to. Through
// CourtControls.WriteStanding: the write's tablet runs the shiaijo, through
// whichever of its tabs, and the write was made at or after the standing's
// start; a shiaijo nobody holds, from a tab and a tablet on neither displaced
// list, is claimed by the write first, while one another tab of the same
// tablet holds is left with that tab. None without a court control, a
// registry, a stored match or a shiaijo, and never in a self-run tournament,
// whose results carry no court's standing.
func (w courtWrite) judgeCourtStanding(stx state.StoreTx, compID, matchID string, snap matchSnapshot, courts CourtControls, tl TournamentLoader, stamp int64) (bool, error) {
	if w.CourtControl == nil || courts == nil || tl == nil || snap.Stored == nil {
		return false, nil
	}
	court := snap.Stored.Court
	if snap.InBracket {
		// engine.BracketMatchAsResult leaves the slot out (storedMatchForAnswer).
		bracket, err := stx.LoadBracket(compID)
		if err != nil {
			return false, err
		}
		court, _ = bracketSlot(bracket, matchID)
	}
	if court == "" {
		return false, nil
	}
	t, err := tl.LoadTournament()
	if err != nil {
		return false, err
	}
	if t != nil && t.Mode == state.TournamentModeSelfRun {
		return false, nil
	}
	return courts.WriteStanding(court, w.CourtControl.TabID, w.CourtControl.DeviceID, stamp)
}

// courtsOf is the court control registry the router attached to its hub,
// read per request: nil on a bare hub (a test's), where no write has the
// court's standing.
func courtsOf(hub Broadcaster) CourtControls {
	h, ok := hub.(*Hub)
	if !ok {
		return nil
	}
	return h.courtControl()
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

// overridesReset is the optional body of DELETE /competitions/:id/overrides: the
// write's id and the request-only confirmFinished, the organiser's confirmation
// that clearing the lots may put a finished league back into match play (never
// stored; the same flag every result door of a finished league takes).
type overridesReset struct {
	writeTag
	ConfirmFinished bool `json:"confirmFinished"`
}

// bindOptionalOverridesReset reads that body: an empty one is the one-tap shape.
// It answers 400 itself for a malformed body or tag.
func bindOptionalOverridesReset(c *gin.Context) (overridesReset, bool) {
	var body overridesReset
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return body, true
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return overridesReset{}, false
	}
	if err := body.validateTag(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return overridesReset{}, false
	}
	return body, true
}

// lineupRemoval is the optional body of a lineup removal (DELETE
// match-lineups/:matchId and lineups/:round): the write's id (a late
// duplicate is answered as recorded, Y8) and when the removal was made
// (removedAt, server-relative ms), by which it is ordered against the saves
// of the same lineup like every change (stage 6 review 8, m1). An older
// build sends none: its removal is stamped as it lands.
type lineupRemoval struct {
	writeTag
	RemovedAt int64 `json:"removedAt,omitempty"`
}

// bindLineupRemoval reads a lineup removal's optional body, answering 400
// (and false) for one that is not a removal's.
func bindLineupRemoval(c *gin.Context) (lineupRemoval, bool) {
	var body lineupRemoval
	if c.Request.Body == nil || c.Request.Body == http.NoBody {
		return body, true
	}
	if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return lineupRemoval{}, false
	}
	if err := body.validateTag(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return lineupRemoval{}, false
	}
	if body.RemovedAt < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "removedAt must not be negative"})
		return lineupRemoval{}, false
	}
	return body, true
}

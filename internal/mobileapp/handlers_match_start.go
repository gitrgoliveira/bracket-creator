package mobileapp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// courtBusyRemedyStart / courtBusyRemedyReopen (bc-cse item 11e) are
// respondCourtBusy's two `remedy` strings, each used at two call sites
// (score start's two doors, the reopen family's two doors); named once so
// the pair cannot drift apart the way a hand-copied literal eventually does.
const (
	courtBusyRemedyStart  = "Finish that match before starting a new one."
	courtBusyRemedyReopen = "Clear its score, queue it, and reopen this one instead."
)

// respondCourtBusy writes the shared 409 court_busy body. The court-
// exclusivity gate fires on three paths (score start, score reopen, and
// kachinuki reopen); `remedy` names the one-step fix for THIS caller
// (courtBusyRemedyStart or courtBusyRemedyReopen above) so the message reads
// naturally for whichever action was attempted. Keeping the
// {error,court,matchId,compId,label,message} shape in ONE place means every
// client court_busy branch stays in lockstep when the wire contract moves.
func respondCourtBusy(c *gin.Context, store CompetitionStore, err *engine.CourtBusyError, remedy string) {
	// bc-cse: err.CompID is the BLOCKING match's own competition, which for
	// a cross-competition conflict (courts are tournament-global) is not
	// the competition this request targets -- the label must be resolved
	// against it, never the target's.
	label := matchLabelOrID(store, err.CompID, err.MatchID)
	c.JSON(http.StatusConflict, gin.H{
		"error":   "court_busy",
		"court":   err.Court,
		"matchId": err.MatchID,
		"compId":  err.CompID,
		"label":   label,
		// bc-rawm/bc-cse: the message names no internal id (the label
		// stands in for it) and states the actual remedy for THIS caller --
		// "reopening this one" used to say "finish that match", which is
		// wrong for a reopen: the panel's own remedy is "Clear its score,
		// queue it, and reopen", not finishing the blocker.
		"message": fmt.Sprintf("Shiaijo %s already has %s running now. %s", err.Court, label, remedy),
	})
}

// respondStartRefusal writes the 409 ineligible_competitor body a start
// refused for one of its sides gets. It is ONE place (bc-stpf) because two
// doors answer it and the console reads both the same way: the /score start
// and GET .../start-check, which must give the byte-identical body for the
// same state.
//
// bc-rawm/bc-cse: reasonHuman is ONE operator sentence, never the raw
// kendo-term reason. Simultaneous is how this error type tells its two
// producers apart: the simultaneity gate
// (checkSimultaneousMatchTx) already builds a complete operator
// sentence into Reason itself, while a barred-status refusal
// (engine.BarredSides) needs the shared builder to name the match and the
// remedy -- even when that refusal's own MatchID is empty (a status set
// directly via POST /competitor-status, with no match tied to it), which is
// why MatchID emptiness alone can no longer be the test.
func respondStartRefusal(c *gin.Context, store CompetitionStore, compID, matchID string, err *engine.IneligibleCompetitorError) {
	reasonHuman := err.Reason
	switch {
	case err.Simultaneous:
		// reasonHuman stays err.Reason, already complete.
	case err.BothSidesBarred:
		// bc-cse item 10: neither side has an opponent to hand the
		// decision to, so the single-sided builder's remedy clauses do
		// not apply.
		reasonHuman = bothSidesBarredReasonHuman(store, compID, matchID)
	default:
		reasonHuman = reasonHumanForBarredCompetitor(store, compID, matchID, err.PlayerID, err.Reason, err.MatchID, err.Decision)
	}
	c.JSON(http.StatusConflict, gin.H{
		"error":       "ineligible_competitor",
		"playerId":    err.PlayerID,
		"reason":      err.Reason,
		"reasonHuman": reasonHuman,
	})
}

// matchNotScheduledReasonHuman is the start check's sentence for a pick that
// is no longer waiting: the console shows it as it is.
const matchNotScheduledReasonHuman = "This match has already started or finished on another device. Check the court's list."

// matchOnOtherCourtReasonHuman is the start check's sentence when the pick is
// assigned to another court: the console operator learns it was moved and
// should refresh the court list.
const matchOnOtherCourtReasonHuman = "This match was moved to another court. Check the court's list."

// registerStartCheckHandler registers GET
// /competitions/:id/matches/:mid/start-check?court=<court> (bc-stpf): would
// starting mid be refused for its status, court or competitors once the bout
// now running on the console's court is sent back to the queue? The court
// console asks it before it sends that bout back, so a start the server would
// refuse leaves the running bout running. The console names its court
// (required `court` query parameter; empty for the unassigned bucket), and
// when it is non-empty the check leaves out every match running on that court;
// an empty court leaves out nothing. A pick that was moved to another court
// since the console last refreshed would leave that bout sent back for a start
// the server then refuses, so a court that is not the pick's stored court is
// refused here with match_on_other_court.
//
// A GET: it writes nothing and is idempotent, so it carries no writeId or
// basis and never goes through the outbox. Answers 200 {ok: true}; 409
// ineligible_competitor with the /score start's own body (respondStartRefusal);
// 409 match_not_scheduled when the pick is running or finished; 409
// match_on_other_court when the pick is on another court; 404 for an unknown
// competition or match.
//
// Intentionally NOT in isSelfRunMainGatedConfigRoute: the start check is
// operational play (same as start-match and revert-to-queue), not organiser
// configuration, so it stays accessible to court operators in self-run mode.
func registerStartCheckHandler(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore) {
	r.GET("/competitions/:id/matches/:mid/start-check", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		court, ok := c.GetQuery("court")
		if !ok {
			c.JSON(http.StatusBadRequest, gin.H{"error": "court is required"})
			return
		}
		if err := validateMaxLen("court", court, MaxLenMatchCourt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		err := eng.CheckStartAfterSendBack(id, mid, court)
		if err == nil {
			c.JSON(http.StatusOK, gin.H{"ok": true})
			return
		}
		var ineligErr *engine.IneligibleCompetitorError
		if errors.As(err, &ineligErr) {
			respondStartRefusal(c, store, id, mid, ineligErr)
			return
		}
		var notScheduledErr *engine.MatchNotScheduledError
		if errors.As(err, &notScheduledErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "match_not_scheduled",
				"matchId":     notScheduledErr.MatchID,
				"status":      notScheduledErr.Status,
				"reasonHuman": matchNotScheduledReasonHuman,
			})
			return
		}
		var otherCourtErr *engine.MatchOnOtherCourtError
		if errors.As(err, &otherCourtErr) {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "match_on_other_court",
				"matchId":     otherCourtErr.MatchID,
				"court":       otherCourtErr.Court,
				"reasonHuman": matchOnOtherCourtReasonHuman,
			})
			return
		}
		respondEngineError(c, err)
	})
}

// matchStatusFromStore is the non-transactional status read used to detect a
// correction (a completed -> completed overwrite) BEFORE the transaction begins,
// letting the pre-tx court gate skip it. A benign TOCTOU with a concurrent
// status change only decides whether the (spurious-for-corrections) court check
// runs; the audit-critical correction-REASON requirement still uses the
// race-free in-tx read.
func matchStatusFromStore(store CompetitionStore, compID, matchID string) state.MatchStatus {
	// MatchStatusByID reads the cached parse and copies nothing, unlike
	// lookupMatchSnapshot (which deep-clones every pool match's SubResults and
	// the whole bracket just to read one enum). A load error or an unknown match
	// yields "" — the isCorrection caller treats a non-completed status as
	// "not a correction", the safe default (mp-gmcg review E5).
	status, _, err := store.MatchStatusByID(compID, matchID)
	if err != nil {
		return ""
	}
	return status
}

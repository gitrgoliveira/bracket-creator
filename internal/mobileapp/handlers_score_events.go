package mobileapp

// handlers_score_events.go: POST /api/competitions/:id/matches/:mid/score-events
// (bc-evts), the door a score editor's taps go through, one event per tap.
// The engine (engine.RecordScoreEventsTx) logs them and writes the match their
// replay derives; this file owns the request's shape, the clock and the
// self-run participant's limits, and the answer. The answer is ALWAYS 200 with
// one outcome per event (a refusal included): the device's outbox retries a
// 5xx for ever, and no event here can be cured by a retry.

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxScoreEventsPerBatch is the most events one request may carry.
const MaxScoreEventsPerBatch = 64

// scoreEventsBody is the request: the device's event session and its taps.
type scoreEventsBody struct {
	Session string              `json:"session"`
	Events  []domain.ScoreEvent `json:"events"`
}

// scoreEventOutcome is the answer for one event: the engine's, or the
// handler's clock_skew with the server's clock and how far ahead the stamp is.
type scoreEventOutcome struct {
	ID          string `json:"id"`
	Outcome     string `json:"outcome"`
	Reason      string `json:"reason,omitempty"`
	Message     string `json:"message,omitempty"`
	ServerNowMs int64  `json:"serverNowMs,omitempty"`
	AheadMs     int64  `json:"aheadMs,omitempty"`
}

// clockSkewEventMessage is what the device shows for a tap stamped too far
// ahead; it re-stamps and sends it again with the same id.
const clockSkewEventMessage = "This device's clock is ahead of the server's, so the tap was not saved yet. It is sent again once the clock is corrected."

// validScoreEventsBody checks the batch's shape; any fault refuses the whole
// batch (400), since the device sent something no retry can cure.
func validScoreEventsBody(body *scoreEventsBody) error {
	if body.Session == "" || len(body.Session) > domain.MaxScoreEventFieldLen {
		return fmt.Errorf("session must be 1 to %d characters", domain.MaxScoreEventFieldLen)
	}
	if len(body.Events) == 0 || len(body.Events) > MaxScoreEventsPerBatch {
		return fmt.Errorf("events must hold 1 to %d events", MaxScoreEventsPerBatch)
	}
	for i := range body.Events {
		ev := &body.Events[i]
		if ev.Session == "" {
			ev.Session = body.Session
		}
		if ev.Session != body.Session {
			return fmt.Errorf("events[%d]: session %q is not the batch's", i, ev.Session)
		}
		if err := ev.Validate(); err != nil {
			return fmt.Errorf("events[%d]: %w", i, err)
		}
	}
	return nil
}

// selfRunEventJudge is the participant's limits on a tap, judged on the stored
// match under the write's lock: the same judges the self-run score path holds
// them to. The organiser (anonymous false) is judged on the representative
// picks alone, as the score path judges its picks (landedMembersRefusal).
func selfRunEventJudge(stx state.StoreTx, compID string, anonymous bool) func(*state.MatchResult, *state.Competition, domain.ScoreEvent) *engine.EventRefusal {
	members := &memberJudge{stx: stx, compID: compID}
	refuse := func(r *selfRunRefusal) *engine.EventRefusal {
		return &engine.EventRefusal{Reason: r.code, Message: r.message}
	}
	return func(stored *state.MatchResult, _ *state.Competition, ev domain.ScoreEvent) *engine.EventRefusal {
		if anonymous {
			switch {
			case isMatchFinalized(stored.Status):
				return refuse(errResultFinalized)
			case ev.Kind == domain.ScoreDaihyosenVerdict:
				return refuse(repBoutHanteiRefusal(false))
			case ev.Pos == state.DaihyosenSubPosition && repBoutHanteiRecorded(stored):
				return refuse(repBoutHanteiRefusal(true))
			case ev.Kind == domain.ScoreBoutDecision && ev.Value != domain.BoutDecisionClear &&
				!IsSelfRunReportableSubDecision(ev.Value, ev.Pos):
				return &engine.EventRefusal{Reason: "decision_not_allowed",
					Message: "This decision is the tournament organizer's to record."}
			}
		}
		judged := ev.Kind == domain.ScoreRepPick || (anonymous && ev.Kind == domain.ScoreBoutFighter)
		if !judged || ev.MemberID == "" {
			return nil
		}
		teamID := stored.SideAID
		if ev.Side == domain.ScoreSideB {
			teamID = stored.SideBID
		}
		if teamID == "" {
			return nil
		}
		held, err := members.holds(teamID, ev.MemberID)
		switch {
		case err != nil:
			return refuse(errTeamMembersUnreadable)
		case held:
			return nil
		case ev.Kind == domain.ScoreRepPick:
			return refuse(errRepMemberNotInTeam)
		default:
			return refuse(errBoutFighterNotInTeam)
		}
	}
}

// repBoutHanteiRecorded reports whether the stored representative bout
// carries the judges' decision (the hantei mark).
func repBoutHanteiRecorded(stored *state.MatchResult) bool {
	row := state.SubResultAt(stored.SubResults, state.DaihyosenSubPosition)
	if row.Position != state.DaihyosenSubPosition {
		return false
	}
	return slices.Contains(row.IpponsA, domain.HanteiMark) || slices.Contains(row.IpponsB, domain.HanteiMark)
}

// eventWriteRefusal reads why the match the replay derived could not be
// written as a refusal the device is told about, or false for a fault (the
// request then answers 500).
func eventWriteRefusal(err error) (reason, message string, ok bool) {
	var played *engine.DownstreamKnockoutPlayedError
	var running *engine.DownstreamKnockoutRunningError
	var verr *engine.ValidationError
	var already *engine.AlreadyIneligibleError
	var inelig *engine.IneligibleCompetitorError
	switch {
	case errors.As(err, &played):
		return "downstream_knockout_played", played.Error(), true
	case errors.As(err, &running):
		return "downstream_knockout_running", running.Error(), true
	case errors.As(err, &already):
		return "already_ineligible", already.Error(), true
	case errors.As(err, &inelig):
		return "ineligible_competitor", inelig.Error(), true
	case errors.Is(err, engine.ErrMatchSideMismatch):
		return "side_mismatch", "The competitors of this match changed. Close the score sheet and open it again.", true
	case errors.As(err, &verr):
		return "invalid", verr.Error(), true
	}
	return "", "", false
}

// registerScoreEventsHandler wires the score-events door. It shares the score
// door's broadcast coalescer, so a match's running pushes stay at most four a
// second whichever door wrote them.
func registerScoreEventsHandler(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, tx CompetitionTransactor, hub Broadcaster, verifier PasswordVerifier, tl TournamentLoader, coalescer *matchBroadcastCoalescer) {
	r.POST("/competitions/:id/matches/:mid/score-events", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// The caller first, so a wrong password is 401 whatever the body.
		anonymous, ok := selfRunAnonymous(c, tl, verifier)
		if !ok {
			return
		}
		var body scoreEventsBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validScoreEventsBody(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// The clock, per event: a stamp too far ahead is answered clock_skew
		// and never logged; the device re-stamps it under the same id.
		outcomes := make([]scoreEventOutcome, len(body.Events))
		var take []domain.ScoreEvent
		var takeAt []int
		for i, ev := range body.Events {
			if serverNowMs, aheadMs, refuse := clientClockSkew(ev.Stamp); refuse {
				logClockSkewRefusal(c, aheadMs, mid)
				outcomes[i] = scoreEventOutcome{ID: ev.ID, Outcome: domain.ScoreOutcomeClockSkew, Reason: "clock_skew",
					Message: clockSkewEventMessage, ServerNowMs: serverNowMs, AheadMs: aheadMs}
				continue
			}
			take = append(take, ev)
			takeAt = append(takeAt, i)
		}

		// Every batch reaches the engine, an all-skewed one too (it logs
		// nothing): its answer carries the match as stored, or a 404.
		var res *engine.ScoreEventsResult
		txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
			var err error
			res, err = eng.RecordScoreEventsTx(stx, id, mid, engine.ScoreEventsRequest{
				Session: body.Session, Events: take, Judge: selfRunEventJudge(stx, id, anonymous),
				NoTurnedAwayHistory: anonymous,
			})
			if err == nil && res.WriteErr != nil {
				if _, _, refusal := eventWriteRefusal(res.WriteErr); !refusal {
					return res.WriteErr
				}
			}
			return err
		})
		if txErr != nil {
			var notFound *engine.NotFoundError
			if errors.As(txErr, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": txErr.Error()})
				return
			}
			internalError(c, txErr)
			return
		}
		reason, message, _ := eventWriteRefusal(res.WriteErr)
		for j, ans := range res.Answers {
			out := scoreEventOutcome{ID: ans.ID, Outcome: ans.Outcome, Reason: ans.Reason, Message: ans.Message}
			if res.WriteErr != nil && ans.Outcome == domain.ScoreOutcomeRefused && ans.Reason == "" {
				out.Reason, out.Message = reason, message
			}
			outcomes[takeAt[j]] = out
		}

		answer := gin.H{"outcomes": outcomes}
		if len(res.Gaps) > 0 {
			answer["gaps"] = res.Gaps
		}
		match := res.Match
		stampWithdrawnStatus(store, id, match)
		answer["match"] = matchPtrForBroadcast(match)

		if res.Written {
			// A second tap inside the window is held and sent when it ends,
			// so the board and the other devices end on the last state.
			matchUpdated := gin.H{"competitionId": id, "matchId": mid, "result": matchPtrForBroadcast(match)}
			// The send reads the match again when it runs, so a push held to the
			// window's end, or one that crossed another request, shows the
			// newest stored state and not this request's.
			coalescer.Submit(id+":"+mid, match.Status == state.MatchStatusRunning, func() {
				if p, ok := freshMatchPush(store, id, mid, matchUpdated); ok {
					hub.Broadcast(EventMatchUpdated, p)
				}
			})
			if res.Status != nil {
				hub.Broadcast(EventCompetitorStatusUpdated, gin.H{"competitionId": id, "status": res.Status})
			}
			tryAutoCompletePoolsAfterWrite(c, eng, hub, id, *match)
		}
		if res.WriteErr != nil {
			log.Printf("mobileapp: POST %s: the replayed match was not written: %v", c.Request.URL.Path, res.WriteErr)
		}
		c.JSON(http.StatusOK, answer)
	})
}

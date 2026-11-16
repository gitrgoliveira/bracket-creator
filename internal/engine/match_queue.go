package engine

import (
	"errors"
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// requeuePoolMatch sends a pool match back to the queue: scheduled, no verdict,
// no provenance, no reopen flag, and its score kept (points, penalties,
// overtime, bouts, flags, rep-bout fighters; operator ruling 2026-09-26,
// bc-sbq). Shared by RevertMatchToQueue and a confirmed running court move.
func requeuePoolMatch(r *state.MatchResult, stamp int64) {
	// Any non-completed match (running, or an already-scheduled match that
	// still carries a stale verdict or audit note from an earlier partial
	// write) goes back to the queue WITH ITS SCORE: points, penalties,
	// overtime, bouts, flags and rep-bout fighters stay (keepQueuedScore
	// names them), and the operator removes a wrong mark themselves
	// (operator ruling 2026-09-26, bc-sbq). Only the verdict and the
	// audit notes go. Idempotent: a pristine scheduled match is left
	// effectively unchanged.
	r.Status = state.MatchStatusScheduled
	r.Winner = ""
	r.WinnerID = ""
	r.Decision = ""
	r.DecisionBy = ""
	r.DecisionReason = ""
	r.ResultSource = ""
	r.CorrectionReason = ""
	// ReopenPending is a match-level field a reopened result carries
	// (reopenPoolMatch sets it), so requeue clears it too: the result the
	// reopen discarded no longer matters to a match sent back to the
	// queue. reopenBracketMatch's doc names this mirror obligation on
	// RevertMatchToQueue.
	r.ReopenPending = false
	// Revert fence, the same one the bracket branch sets
	// (requeueBracketMatch, mp-y3nk): a write stamped before the requeue,
	// such as an offline-queued score replayed afterwards, loses the
	// timestamp comparison instead of resurrecting the result the operator
	// just sent back to the queue. Left unstamped, the requeued row kept
	// whatever stamp it had, which the stale write could beat. Only the
	// verdict is stamped (bc-mrgc): the score stays, and so does the
	// stamp each part of it was last changed at.
	r.StampGroups(stamp, state.GroupResult)
}

// requeueBracketMatch sends a bracket match back to the queue: scheduled, no
// verdict, no provenance, no reopen flag, and its score KEPT (points,
// penalties, overtime, bouts, flags; operator ruling 2026-09-26, bc-sbq). It is
// RevertMatchToQueue's and UpdateMatchCourt's bracket branch, extracted so the
// body has a name and a doc rather than sitting inline in a closure.
//
// bc-kcdg's forced correction briefly used it too, until
// the operator ruled that a downstream match invalidated by a correction was
// already played and should be reopened where it is, leaving the queue alone
// (see reopenDisplacedBracketMatch, which forceReopenDownstreamChain calls
// instead). Keep the two distinct: requeue is "this match is not finished,
// carry on from where it stopped", reopen is "this match finished and its
// verdict no longer stands". Both keep what was scored.
func requeueBracketMatch(m *state.BracketMatch, stamp int64) {
	m.Status = state.MatchStatusScheduled
	m.Winner = ""
	m.WinnerID = "" // bc-brid: the verdict's id half, cleared with the name.
	m.Decision = ""
	m.DecisionBy = ""
	m.DecisionReason = ""
	m.IsOverridden = false
	m.ResultSource = ""
	m.CorrectionReason = ""
	// Mirror of the pool branch (mp-gmcg review): clear the reopen-pending
	// audit debt so a reopened-then-requeued bracket match can be replayed
	// and finalized without the handler's applyCorrectionReason demanding a
	// reason for a result the requeue already discarded.
	m.ReopenPending = false
	// Revert fence (mp-y3nk): stamp now() so any pre-revert offline write
	// (T_stale < T_revert) cannot put the verdict back on replay. Using 0
	// would make ApplyByTimestamp always return true (weaker). Only the
	// verdict group is stamped (bc-mrgc): the kept score keeps its stamps.
	// The caller passes the SAME stamp its history entry records, so the
	// match and that entry never disagree by the sub-millisecond gap a
	// second serverNowMs() call here would introduce.
	m.StampGroups(stamp, state.GroupResult)
}

// CourtMove is how a court move was asked for.
type CourtMove struct {
	// StopsRunning: the operator saw the match running and confirmed stopping it
	// there and putting it first on the new court. It only has effect when the
	// stored match is running.
	StopsRunning bool
	// WriteID is the client's id for the move, so a resend after a restart is
	// answered as recorded (loggedWriteID).
	WriteID string
}

// UpdateMatchCourt moves a match to another court under the tournament-wide
// court lock and the competition's transaction lock.
//
// Lock discipline: the whole body runs under store.WithCourtExclusivityLock
// (court lock first, then the competition lock). That mutex is
// tournament-global and non-reentrant, so nothing called inside this body
// (LoadTournament, EnsureLegacyUpgraded, CourtQueueBounds, WithTransaction,
// moveKnockoutPastPools, the requeue helpers, or recordRequeueHistory) may
// acquire it again. CourtQueueBounds acquires per-competition read locks across
// the tournament, which are also non-reentrant, so it runs before
// WithTransaction acquires compId's write lock, never inside the transaction.
func (e *Engine) UpdateMatchCourt(compId string, matchId string, newCourt string, mv CourtMove) error {
	return e.store.WithCourtExclusivityLock(func() error {
		// Read before the transaction: the tournament has its own lock, and the
		// competition reads inside WithTransaction skip the first-read legacy
		// conversion the store's own reads run.
		tournament, tournErr := e.store.LoadTournament()
		e.store.EnsureLegacyUpgraded(compId)

		var earliest, latest string
		if newCourt != "" {
			var berr error
			earliest, latest, berr = e.store.CourtQueueBounds(newCourt, compId, matchId)
			if berr != nil {
				log.Printf("engine: court move of %s/%s kept the match's own time: court queue bounds unreadable: %v", compId, matchId, berr)
				earliest, latest = "", ""
			}
		}

		return e.store.WithTransaction(compId, func(tx state.StoreTx) error {
			// A resend of a confirmed running court move already logged
			// in the match's score event log is answered as recorded, with
			// nothing written and no second history line.
			if mv.WriteID != "" && e.loggedWriteID(tx, compId, matchId, mv.WriteID) {
				return nil
			}

			poolMatches, err := tx.LoadPoolMatches(compId)
			if err != nil {
				return err
			}
			var (
				storedStatus state.MatchStatus
				storedCourt  string
				inPool       bool
				found        bool
				bracket      *state.Bracket
			)
			for i := range poolMatches {
				if poolMatches[i].ID == matchId {
					storedStatus = poolMatches[i].Status
					storedCourt = poolMatches[i].Court
					inPool = true
					found = true
					break
				}
			}
			if !found {
				var berr error
				bracket, berr = tx.LoadBracket(compId)
				if berr != nil {
					return berr
				}
				if bm := bracket.MatchByID(matchId); bm != nil {
					storedStatus = bm.Status
					storedCourt = bm.Court
					found = true
				}
			}
			if !found {
				return notFoundErrorf("match %s not found in competition %s", matchId, compId)
			}

			// Moving to the court the match already sits on changes
			// nothing, whatever its status. Return before any with*Match call
			// so nothing is saved and no file version moves.
			if newCourt == storedCourt {
				return nil
			}

			refuseMove := func() error {
				comp, cerr := tx.LoadCompetition(compId)
				if cerr != nil {
					log.Printf("engine: UpdateMatchCourt: LoadCompetition compId=%s: %v (label degrades to pool-phase-only)", compId, cerr)
				}
				return &CourtMoveRefusedError{
					Label:       OperatorMatchLabel(comp, bracket, matchId),
					StoredCourt: storedCourt,
					AskedCourt:  newCourt,
				}
			}

			// Stored running and not confirmed: refused. If the match is not
			// running, StopsRunning has no effect (the move proceeds as for a
			// waiting match, or, for a finished one, changes only its court).
			running := storedStatus == state.MatchStatusRunning
			if running && !mv.StopsRunning {
				return refuseMove()
			}

			stamp := serverNowMs()
			requeued := running

			if inPool {
				if err := e.withPoolMatch(tx, compId, matchId, func(r *state.MatchResult) error {
					if requeued {
						requeuePoolMatch(r, stamp)
					}
					r.Court = newCourt
					r.ScheduledAt = movedQueueTime(requeued, r.Status, r.ScheduledAt, earliest, latest)
					return nil
				}); err != nil {
					return err
				}
				if requeued {
					e.recordRequeueHistory(tx, compId, matchId, stamp, ForceOptions{WriteID: mv.WriteID}, newCourt)
				}
				comp, err := tx.LoadCompetition(compId)
				if err != nil {
					return err
				}
				if !comp.IsKnockoutEnabled() {
					return nil
				}
				// The move is what the operator asked for; the knockout push only
				// follows it. A tournament that cannot be read leaves the knockout
				// times as they are rather than refusing the move.
				if tournErr != nil {
					log.Printf("engine: court move of %s/%s kept the knockout times: tournament unreadable: %v", compId, matchId, tournErr)
					return nil
				}
				matches, err := tx.LoadPoolMatches(compId)
				if err != nil {
					return err
				}
				return moveKnockoutPastPools(tx, compId, matches, comp, tournament)
			}

			if err := e.withBracketMatch(tx, compId, matchId, func(m *state.BracketMatch) {
				if requeued {
					requeueBracketMatch(m, stamp)
				}
				m.Court = newCourt
				m.ScheduledAt = movedQueueTime(requeued, m.Status, m.ScheduledAt, earliest, latest)
			}); err != nil {
				return matchNotFoundAsNotFound(compId, matchId, err)
			}
			if requeued {
				e.recordRequeueHistory(tx, compId, matchId, stamp, ForceOptions{WriteID: mv.WriteID}, newCourt)
			}
			return nil
		})
	})
}

// movedQueueTime is the ScheduledAt a match carries after a court move: first
// on the new court when a running match was requeued (ahead of timed waiting
// matches), last when it was waiting there (after timed waiting matches), tied
// with the earliest at "00:00" or the latest at "23:59" when no minute is left
// beyond it, and its own time when it had already finished or when the target
// court has no timed waiting matches; a waiting match moved onto a court whose
// waiting matches are all untimed is left untimed, last with them.
func movedQueueTime(requeued bool, status state.MatchStatus, own, earliest, latest string) string {
	switch {
	case requeued:
		return firstInQueueTime(own, earliest)
	case status == state.MatchStatusScheduled:
		return lastInQueueTime(own, latest)
	default:
		return own
	}
}

// firstInQueueTime returns one minute before earliest (the target court's
// earliest timed scheduled match) so a confirmed running court move places the
// requeued match first in the target court's queue. When earliest is "00:00"
// there is no earlier minute, so it returns "00:00" itself and the match ties
// for first. It keeps own when earliest is empty, when own already sorts before
// earliest in queue order (where an empty own reads as "99:99"), or when
// earliest is unparseable or not in canonical two-digit HH:MM form.
func firstInQueueTime(own, earliest string) string {
	mins, ok := state.CanonicalQueueTime(earliest)
	if !ok || state.QueueSortTime(own) < earliest {
		return own
	}
	if mins == 0 {
		return earliest
	}
	return fmtHHMM(mins - 1)
}

// lastInQueueTime returns one minute after latest (the target court's latest
// timed scheduled match) so a waiting match moved to another court lands last
// in that court's queue. When latest is "23:59" there is no later minute, so it
// returns "23:59" itself and the match ties for last. When latest is "99:99"
// (state.QueueSortTime of an untimed match: the court's waiting matches are all
// untimed) it returns "", so the match sinks with them. It keeps own when latest
// is empty, when own already sorts after latest in queue order (an empty own
// reads as "99:99" and stays ""), or when latest is not in canonical two-digit
// HH:MM form.
func lastInQueueTime(own, latest string) string {
	if latest == state.QueueSortTime("") {
		return ""
	}
	mins, ok := state.CanonicalQueueTime(latest)
	if !ok || state.QueueSortTime(own) > latest {
		return own
	}
	if mins == 23*60+59 {
		return latest
	}
	return fmtHHMM(mins + 1)
}

// matchNotFoundAsNotFound turns the pool-then-bracket fall-through's final
// errMatchNotFound into a *NotFoundError, so a handler answers 404 for a match
// the competition does not hold rather than a 500 the outbox would retry
// forever (bc-evts stage 3 review 2, R3). Any other error is returned as is.
func matchNotFoundAsNotFound(compId, matchId string, err error) error {
	if errors.Is(err, errMatchNotFound) {
		return notFoundErrorf("match %s not found in competition %s", matchId, compId)
	}
	return err
}

// UpdateMatchTime sets a match's time and moves nothing else: a time set by
// hand is the operator ordering the court (the queue's up/down is two of
// these), so the knockout is not pushed past a pool bout moved later.
func (e *Engine) UpdateMatchTime(compId string, matchId string, scheduledAt string) error {
	err := e.withPoolMatch(e.store, compId, matchId, func(r *state.MatchResult) error {
		r.ScheduledAt = scheduledAt
		return nil
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, errMatchNotFound) {
		return err
	}
	return matchNotFoundAsNotFound(compId, matchId, e.withBracketMatch(e.store, compId, matchId, func(m *state.BracketMatch) {
		m.ScheduledAt = scheduledAt
	}))
}

// RevertMatchToQueue reverts a running match back to the scheduled (queued)
// state. It keeps the match's score: a match sent back to the queue never
// loses what was entered, and starting it again carries on from it (the
// StartOnly write, keepQueuedScore). It is idempotent for already-scheduled matches (no-op success). Completed
// matches return ErrMatchAlreadyCompleted (HTTP 409); the operator must use
// the score editor to correct a recorded result instead.
//
// Modelled on UpdateMatchCourt: pool-match first, bracket-match fallback,
// using the same atomic withPoolMatch/withBracketMatch primitives so the
// entire load+mutate+save runs under the per-competition lock.
//
// COMPOSED UNDER THE COURT LOCK: RequeueBlockerAndReopen calls this
// from INSIDE store.WithCourtExclusivityLock so the blocker requeue and the
// reopen share one lock section (mp-gmcg review A4/R3). This method must
// therefore take ONLY the per-competition lock (via withPoolMatch/
// withBracketMatch) and MUST NOT acquire the court-exclusivity lock — that
// mutex is tournament-global and non-reentrant, so a court check added here
// would deadlock the whole tournament under that composition. It is a
// court-FREEING operation and needs no court gate; if that ever changes, add a
// lock-free `revertMatchToQueueUnderCourtLock` core and call THAT from the
// composition, mirroring reopenUnderCourtLock.
func (e *Engine) RevertMatchToQueue(compId, matchId string, opts ...ForceOptions) error {
	fo := firstForceOptions(opts)
	var alreadyCompleted, notFound bool

	// The write and its history entry are one transaction (bc-mrgc): a
	// requeue frees a court and clears a verdict, so a process dying right
	// after it lands must not leave the match with nothing explaining how
	// it got there.
	err := e.store.WithTransaction(compId, func(tx state.StoreTx) error {
		// A resend of a send back already logged (its write id) is answered
		// as recorded: nothing written, no history line (bc-evts F1, Q6).
		if e.loggedWriteID(tx, compId, matchId, fo.WriteID) {
			return nil
		}
		stamp := serverNowMs()
		perr := e.withPoolMatch(tx, compId, matchId, func(r *state.MatchResult) error {
			if r.Status == state.MatchStatusCompleted {
				// Reported through the captured flag, not an abort: the caller
				// turns it into its own error AFTER the store call, and aborting
				// here would change that error's identity.
				alreadyCompleted = true
				return nil
			}
			requeuePoolMatch(r, stamp)
			return nil
		})
		if perr == nil {
			if !alreadyCompleted {
				e.recordRequeueHistory(tx, compId, matchId, stamp, fo, "")
			}
			return nil
		}
		if !errors.Is(perr, errMatchNotFound) {
			return perr
		}

		// Pool match not found; try the elimination bracket. alreadyCompleted is
		// still false here (the pool closure never ran on the errMatchNotFound path).
		berr := e.withBracketMatch(tx, compId, matchId, func(m *state.BracketMatch) {
			if m.Status == state.MatchStatusCompleted {
				alreadyCompleted = true
				return
			}
			// Same contract as the pool path: any non-completed match goes back
			// to the queue with its score, losing only the verdict and the audit
			// notes, even if it was already scheduled.
			requeueBracketMatch(m, stamp)
		})
		if berr != nil {
			// Neither pool nor bracket holds this match: reported through the
			// captured flag rather than an error here, so the outer call can
			// surface a typed NotFoundError (a fabricated match id is a client
			// error, not a server fault) without that error being misread as a
			// transaction failure.
			if errors.Is(berr, errMatchNotFound) {
				notFound = true
				return nil
			}
			return berr
		}
		if !alreadyCompleted {
			e.recordRequeueHistory(tx, compId, matchId, stamp, fo, "")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if notFound {
		return notFoundErrorf("match %s not found in competition %s", matchId, compId)
	}
	if alreadyCompleted {
		return ErrMatchAlreadyCompleted
	}
	return nil
}

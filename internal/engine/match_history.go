package engine

import (
	"encoding/json"
	"errors"
	"log"
	"slices"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The doors a match write comes through, as the match history names them
// (bc-mrgc). The handlers set MatchResult.WriteDoor to one of the write doors;
// the direct writers (a reopen, a requeue, an override, ...) name their own.
const (
	DoorScore        = "score"
	DoorBulkScore    = "bulk-score"
	DoorQuickScore   = "quick-score"
	DoorDecision     = "decision"
	DoorDaihyosenAdd = "daihyosen-add"
	DoorDaihyosenDel = "daihyosen-remove"
	DoorEngine       = "engine"
	// DoorDisplaced names a history entry for a stored change an earlier-
	// stamped write arriving after it moved to the history (S2 with R4): the
	// change came through some door before; this entry records it held.
	DoorDisplaced = "displaced"

	doorReopen           = "reopen"
	doorRequeue          = "requeue"
	doorOverride         = "override-winner"
	doorEngi             = "engi"
	doorKachinukiAdvance = "kachinuki-advance"
	doorKachinukiRemove  = "kachinuki-remove-bout"
	doorDownstreamReopen = "downstream-reopen"
)

// clientDoors are the doors whose writer may legitimately state no groups:
// a client payload without `changed` (an older client) means every group it
// carries. Every other door builds its write on the server and must state
// its groups (see mergeMatchWrite's check).
var clientDoors = []string{"", DoorScore, DoorBulkScore, DoorEngine}

// serverBuiltWithoutGroups reports a server-built write that reached the
// merge without naming its groups, which is a bug in that door: it would be
// merged as if it changed everything it carries.
func serverBuiltWithoutGroups(result *state.MatchResult) bool {
	return result.Changed == nil && !slices.Contains(clientDoors, result.WriteDoor)
}

func doorOf(result *state.MatchResult) string {
	if result.WriteDoor == "" {
		return DoorEngine
	}
	return result.WriteDoor
}

// recordWriteHistory appends the history entry of a write that reached the
// merge, from what the merge decided (result.Merge): every group it changes
// with its outcome, and the incoming values of the held ones. Called by the
// write paths at the point the write can no longer be rolled back, inside the
// write's transaction, so the entry lands exactly when the write does. A
// failure to stage it is logged, not returned: the write itself is already
// staged, and failing it here would report an error for a result that lands.
func (e *Engine) recordWriteHistory(h state.StoreTx, compID, matchID string, result *state.MatchResult) {
	rep := result.Merge
	if rep == nil {
		return
	}
	outcomes := make(map[string]string, len(rep.Changed))
	for _, g := range rep.Applied {
		outcomes[g] = state.HistoryOutcomeApplied
	}
	for _, g := range rep.Unchanged {
		outcomes[g] = state.HistoryOutcomeUnchanged
	}
	// A held group that said exactly what is stored lost nothing: it is
	// recorded as unchanged, never as held.
	for _, g := range rep.HeldEcho {
		outcomes[g] = state.HistoryOutcomeUnchanged
	}
	for _, g := range rep.Held {
		outcomes[g] = state.HistoryOutcomeHeld
	}
	entry := state.MatchHistoryEntry{
		MatchID:           matchID,
		Door:              doorOf(result),
		Stamp:             rep.Stamp,
		ReceivedAt:        time.Now().UnixMilli(),
		Session:           result.RevSession,
		Changed:           rep.Changed,
		Outcomes:          outcomes,
		Held:              rep.HeldValues,
		Reason:            rep.HoldReason,
		ClearedWithdrawal: rep.ClearedWithdrawal,
	}
	if err := h.AppendMatchHistory(compID, entry); err != nil {
		log.Printf("engine: match %s/%s: history entry not recorded: %v", compID, matchID, err)
	}
	e.recordDisplacedHistory(h, compID, matchID, rep)
}

// recordDisplacedHistory appends one entry per stored change the write moved
// to the history (MergeReport.Displaced): at that change's own stamp, every
// group it had set held with its value and the reason. It reads exactly as
// the entry that change gets when it arrives AFTER the write instead, so the
// history holds it whichever order the two arrived in.
func (e *Engine) recordDisplacedHistory(h state.StoreTx, compID, matchID string, rep *state.MergeReport) {
	if rep == nil {
		return
	}
	for _, d := range rep.Displaced {
		changed := make([]string, 0, len(d.Values))
		outcomes := make(map[string]string, len(d.Values))
		for g := range d.Values {
			changed = append(changed, g)
			outcomes[g] = state.HistoryOutcomeHeld
		}
		slices.Sort(changed)
		entry := state.MatchHistoryEntry{
			MatchID:    matchID,
			Door:       DoorDisplaced,
			Stamp:      d.Stamp,
			ReceivedAt: time.Now().UnixMilli(),
			Changed:    changed,
			Outcomes:   outcomes,
			Held:       d.Values,
			Reason:     d.Reason,
		}
		if err := h.AppendMatchHistory(compID, entry); err != nil {
			log.Printf("engine: match %s/%s: displaced history entry not recorded: %v", compID, matchID, err)
		}
	}
}

// recordDirectHistory appends the history entry of a write that changes a
// match's groups outside the merge (a reopen, a requeue, an override, the
// engi recorder, the kachinuki advance): every group it changed, applied, at
// stamp. Same failure rule as recordWriteHistory.
func (e *Engine) recordDirectHistory(h state.StoreTx, compID, matchID, door string, stamp int64, groups ...string) {
	outcomes := make(map[string]string, len(groups))
	for _, g := range groups {
		outcomes[g] = state.HistoryOutcomeApplied
	}
	entry := state.MatchHistoryEntry{
		MatchID:    matchID,
		Door:       door,
		Stamp:      stamp,
		ReceivedAt: time.Now().UnixMilli(),
		Changed:    append([]string(nil), groups...),
		Outcomes:   outcomes,
	}
	if err := h.AppendMatchHistory(compID, entry); err != nil {
		log.Printf("engine: match %s/%s: history entry not recorded: %v", compID, matchID, err)
	}
}

// noteServerBoutChanges adds to an explicit Changed every bout row the
// server itself changed on the write's behalf after the writer sent it (the
// kachinuki merge's server-appended rows and trailing strip, the default-win
// padding): before is the bout list as the writer sent it, stored the stored
// match's, and kachinuki whether the write merged its bout log by position.
// Without this the merge would read those rows as unchanged and put the
// stored ones back over them. This and daihyosenChangedGroups (the
// representative-bout add and remove, mobileapp) are the two places the
// server diffs a write against the match it was built from. Diffing here is safe where diffing a client payload is not: both
// sides are the server's own, made under the write's lock. A write that named
// no groups needs nothing: its default already covers every row it carries.
func noteServerBoutChanges(result *state.MatchResult, before, stored []state.SubMatchResult, kachinuki bool) {
	if result.Changed == nil {
		return
	}
	// What the merge would keep for each row the server did not touch: the
	// writer's own row, or, in a kachinuki write, which merges its bout log
	// BY POSITION (an omitted row is kept, mergeKachinukiSubResults), the
	// stored row the writer left out. Without that half a stored row the
	// kachinuki merge brought in and then stripped (a trailing unscored
	// pairing at the finish) read as unchanged, and the merge copied it back
	// (bc-mrgc review S4). In any other write an omitted row is just not
	// part of it, never a server change, so stored rows are not consulted.
	wasSubs := state.CloneSubResults(before)
	if kachinuki {
		sent := state.SubPositions(before)
		for i := range stored {
			if !slices.Contains(sent, stored[i].Position) {
				wasSubs = append(wasSubs, stored[i])
			}
		}
	}
	was := &state.MatchResult{SubResults: wasSubs}
	positions := state.SubPositions(wasSubs)
	for _, p := range state.SubPositions(result.SubResults) {
		if !slices.Contains(positions, p) {
			positions = append(positions, p)
		}
	}
	for _, p := range positions {
		g := state.BoutGroup(p)
		if !slices.Contains(result.Changed, g) && state.GroupDiffers(was, result, g) {
			result.Changed = append(result.Changed, g)
		}
	}
}

// serverNowMs is the stamp a direct writer gives the groups it changes: the
// server's own clock, the frame every client stamp is relative to.
func serverNowMs() int64 {
	return time.Now().UnixMilli()
}

// recordOverrideHistory records an override-winner write in the match's
// history: applied, or held with the winner it named. The override writes
// outside a transaction (OverrideBracketWinner), so this is its own append,
// after the bracket save.
func (e *Engine) recordOverrideHistory(compID, matchID, winnerName string, stamp int64, applied bool) {
	if applied {
		e.recordDirectHistory(e.store, compID, matchID, doorOverride, stamp, state.GroupResult)
		return
	}
	held, err := json.Marshal(map[string]string{"winner": winnerName})
	if err != nil {
		held = json.RawMessage("null")
	}
	entry := state.MatchHistoryEntry{
		MatchID:    matchID,
		Door:       doorOverride,
		Stamp:      stamp,
		ReceivedAt: time.Now().UnixMilli(),
		Changed:    []string{state.GroupResult},
		Outcomes:   map[string]string{state.GroupResult: state.HistoryOutcomeHeld},
		Held:       map[string]json.RawMessage{state.GroupResult: held},
	}
	if err := e.store.AppendMatchHistory(compID, entry); err != nil {
		log.Printf("engine: match %s/%s: history entry not recorded: %v", compID, matchID, err)
	}
}

// SupersededError is ErrMatchSuperseded carrying the groups the write
// changes, every one of them held (bc-mrgc): the handlers answer it with
// applied:false plus heldGroups. errors.Is(err, ErrMatchSuperseded) holds.
type SupersededError struct {
	Held []string
	// HeldReason is the merge report's code for why they were held
	// (state.MergeReport.HeldReason): "" for a newer stored change,
	// state.HeldReasonNeedsWinner when applying them would have left the
	// match without the winner it needs.
	HeldReason string
}

func (e *SupersededError) Error() string { return ErrMatchSuperseded.Error() }

// Is makes a SupersededError match ErrMatchSuperseded.
func (e *SupersededError) Is(target error) bool { return target == ErrMatchSuperseded }

// supersededBy is the error a write whose every change was held returns.
func supersededBy(result *state.MatchResult) error {
	return &SupersededError{Held: result.Merge.HeldGroups(), HeldReason: result.Merge.HeldReason()}
}

// HeldReasonOf returns why a superseded write's groups were held (see
// SupersededError.HeldReason), or "".
func HeldReasonOf(err error) string {
	var se *SupersededError
	if errors.As(err, &se) {
		return se.HeldReason
	}
	return ""
}

// HeldGroupsOf returns the groups a superseded write had held, or nil.
func HeldGroupsOf(err error) []string {
	var se *SupersededError
	if errors.As(err, &se) {
		return se.Held
	}
	return nil
}

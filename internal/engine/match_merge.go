package engine

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"log"
	"maps"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// match_merge.go is the ONE owner of how an incoming match write is merged
// onto the stored match (bc-mrgc, operator ruling 2026-10-03: "Nothing should
// be dropped. All events must be ordered."). Both write branches call
// mergeMatchWrite: applyPoolWrite for a pool/league match, and
// applyBracketResultIn (rounds and bronze) for a knockout match, before
// anything else in either pipeline judges the write.
//
// bc-evts stage 2b split the work between this merge and the score replay
// (score_replay.go):
//
// OWNED BY THE REPLAY (mergeByReplay): a scoring snapshot -- /score (running
// or finishing), bulk-score, quick-score, the engine's own callers, the engi
// finish, and every decision (kiken, fusenpai, fusensho, daihyosen, hikiwake;
// replaysSnapshot) -- becomes a baseline of the groups it changes at its
// stamp (points, encho, flags, rep, repPickA/B, bout:N, and the result it
// carries) and is replayed onto the match's log (its migration baseline when
// it has none). Each group is ordered at its own stamp, so per-group
// last-write-wins needs no merge here, and the baseline is logged exactly as
// it was made (MergeReport.Baseline), a held one included, so the log is the
// truth and any arrival order replays to the same match.
//
// THE CLOSED-MATCH RULE (operator ruling 2026-10-15) is the replay's whole
// account of a finished match: in stamp order, once a finish or a decision is
// recorded, scoring stamped after it is turned away (MergeReport.MatchClosed,
// heldReason match_closed, or displaced into the history when the result
// arrives second). It replaces R2 (points after a withdrawal clear it), R3 (a
// point after a finish applies), R4 (a tie waits for a winner), S2 and S3.
// A newer result over an older one is the newer one: the older result is a
// line of the log all the same, so a tap stamped between the two is closed out
// by it. What the replay does not keep of the write is reported held, from the
// values alone.
//
// STILL THE MERGE'S (writes the replay does not take, and the holds a snapshot
// meets before any replay):
//
//   - Group by group by stamp (domain.ApplyByTimestamp) for every write
//     replaysSnapshot does not take: the representative bout's add and remove,
//     a start's server twins.
//   - The engi finish's atomicity (HoldReasonEngiAtomic): a stale engi finish
//     is held with its count.
//   - A running write unstamped over a finished match, which cannot be
//     ordered, is held whole.
//   - The older-revision hold (HoldReasonOlderRevision): a previous build's
//     running write with a lower revision than one its board's session
//     already logged is held whole (olderRevisionThanLogged; it replaced the
//     score handler's in-memory rev guard).
//   - The representative picks on a removed row (settleOrphanPicks, the
//     daihyosen DELETE), the server's recorded marks (unrecordReopenedBouts),
//     the echo rule for a write naming no groups, and the superseded answer
//     (MergeReport.Superseded) for every door.
//
// A write the server orders itself (a completing write or a server door's)
// that arrives unstamped takes the server's time (writeStamp), so it leaves
// a fence a later stale replay is ordered against.
//
// matchWriteRestore (the K3 rollback) does not merge: it replays a trusted
// snapshot of every group and its stamps.

// mergeCtx is what the merge needs to know about where the match lives.
type mergeCtx struct {
	// comp decides which owner works a winner out again (engi, kachinuki,
	// team, individual). nil reads as an individual kendo competition.
	comp *state.Competition
	// knockout is true for a bracket match (it cannot end tied, and an empty
	// status completes it: completesMatch).
	knockout bool
	// nilSubsClear says what an incoming nil SubResults means on this branch
	// when the writer stated no groups: the pool's whole-struct write has
	// always replaced the stored bouts with it, the bracket has always kept
	// them. The default keeps both exactly as they were.
	nilSubsClear bool
	// holdAll, when set, holds every group the write changes whatever its
	// stamp, and is recorded as the reason in the history entry (the engi
	// finish's atomic hold, HoldReasonEngiAtomic): kept, never applied,
	// never dropped.
	holdAll string
	// storedAID and storedBID are the stored match's side ids, set by
	// mergeMatchWrite: a payload that omits its ids (they are backfilled
	// only after the merge) still gets the winner's id worked out.
	storedAID, storedBID string
	// lines is the match's score event log (bc-evts) a snapshot write is
	// replayed onto (mergeByReplay); nil reads as an empty log, the stored
	// match being its only baseline.
	lines []state.ScoreLogLine
}

// runningOverFinished reports the shape the closed-match rule turns away: a
// running or scheduled write over a finished match. A client's such write never carries the result group
// (effectiveChangedGroups); a server-built one that names it on purpose (a
// representative bout added to a finished knockout match) does.
func runningOverFinished(stored, incoming *state.MatchResult) bool {
	return stored.Status == state.MatchStatusCompleted &&
		(incoming.Status == state.MatchStatusRunning || incoming.Status == state.MatchStatusScheduled)
}

// defaultChangedGroups is what a write that names no groups changes: every
// group the payload carries. Every client score write sends the full match,
// so for /score that is all of them. The bout rows are the payload's own,
// plus the stored ones whenever the payload replaces the bout list (a
// non-nil list, or a nil one on the pool branch, see mergeCtx.nilSubsClear).
func defaultChangedGroups(stored, incoming *state.MatchResult, nilSubsClear bool) []string {
	out := append([]string(nil), state.ScalarGroups...)
	positions := state.SubPositions(incoming.SubResults)
	if incoming.SubResults != nil || nilSubsClear {
		for _, p := range state.SubPositions(stored.SubResults) {
			if !slices.Contains(positions, p) {
				positions = append(positions, p)
			}
		}
	}
	for _, p := range positions {
		out = append(out, state.BoutGroup(p))
	}
	// The representatives sit on the representative bout's row: a write that
	// says nothing about that row says nothing about either side's pick.
	if !slices.Contains(out, state.BoutGroup(state.DaihyosenSubPosition)) {
		out = slices.DeleteFunc(out, func(g string) bool {
			return g == state.GroupRepPickA || g == state.GroupRepPickB
		})
	}
	return out
}

// effectiveChangedGroups is the list the merge works from: the writer's own
// (unknown names dropped, duplicates folded), or the default; minus the
// result group for a running or scheduled write over a finished match.
func effectiveChangedGroups(stored, incoming *state.MatchResult, nilSubsClear bool) []string {
	var changed []string
	if incoming.Changed == nil {
		changed = defaultChangedGroups(stored, incoming, nilSubsClear)
	} else {
		for _, g := range incoming.Changed {
			// A server group (the recorded marks) is never a write's to name:
			// the merge alone changes it, below (mergeHold.unrecordReopenedBouts).
			if state.ValidGroup(g) && !state.IsServerGroup(g) && !slices.Contains(changed, g) {
				changed = append(changed, g)
			}
		}
	}
	// A board still scoring a match it has not seen finish sends its running
	// status along with everything else; that is no verdict, so a client's
	// write never carries the result over a finished match. A server-built
	// write states its groups on purpose: a representative bout added to a
	// finished knockout match does reopen its result, and says so.
	if runningOverFinished(stored, incoming) && (incoming.Changed == nil || slices.Contains(clientDoors, incoming.WriteDoor)) {
		changed = slices.DeleteFunc(changed, func(g string) bool { return g == state.GroupResult })
	}
	if changed == nil {
		changed = []string{}
	}
	return changed
}

// storedBoutGroups lists every bout group the stored match has a stamp or a
// row for, tombstones included.
func storedBoutGroups(stored *state.MatchResult) []string {
	var out []string
	for _, p := range state.SubPositions(stored.SubResults) {
		out = append(out, state.BoutGroup(p))
	}
	for g := range stored.GroupStamps {
		if _, ok := state.ParseBoutGroup(g); ok && !slices.Contains(out, g) {
			out = append(out, g)
		}
	}
	return out
}

// withdrawalOutranked is R2's late half: a withdrawal or default win stands
// on the scoreline it was declared over, so it is held, with everything it
// changes, when any of its groups is held or when ANY stored points or bout
// change was made after it. Points scored after the withdrawal was declared
// mean the withdrawal no longer describes the match.
// mergeMatchWrite merges incoming onto stored in place (see the file header)
// and returns what it decided, also left on incoming.Merge. Under
// matchWriteRestore it merges nothing: the snapshot's groups and stamps are
// restored exactly, and the report is nil.
func mergeMatchWrite(stored, incoming *state.MatchResult, policy matchWritePolicy, mc mergeCtx) *state.MergeReport {
	if policy == matchWriteRestore {
		incoming.GroupStamps = state.CloneGroupStamps(incoming.GroupStamps)
		incoming.Merge = nil
		return nil
	}
	// The score replay's derived write (bc-evts) was ordered line by line by
	// the replay, which owns the closed-match rule: nothing below may judge it
	// again by its stamps.
	if incoming.WriteDoor == DoorEvent {
		return mergeReplayedWrite(stored, incoming)
	}
	// An unstamped write the server itself orders (a completing write, or one
	// a server door built) takes the server's time here, so it leaves a fence
	// a later stale replay is ordered against (writeStamp).
	incoming.ModifiedAt = writeStamp(stored, incoming)
	stamp := incoming.ModifiedAt
	mc.storedAID, mc.storedBID = stored.SideAID, stored.SideBID
	logUnstampedOverwrite(incoming, stored.ModifiedAt)
	if serverBuiltWithoutGroups(incoming) {
		// A door that builds its write on the server knows exactly what it
		// changes and must say so; merged as "everything it carries", it
		// can overwrite a newer change it never meant to touch.
		log.Printf("engine: BUG: match %s: a %q write reached the merge without naming the groups it changes; merging it as every group it carries",
			incoming.ID, incoming.WriteDoor)
	}
	changed := effectiveChangedGroups(stored, incoming, mc.nilSubsClear)
	inChanged := make(map[string]bool, len(changed))
	for _, g := range changed {
		inChanged[g] = true
	}

	hold := map[string]bool{}
	for _, g := range changed {
		if mc.holdAll != "" || !domain.ApplyByTimestamp(stamp, stored.GroupStamp(g)) {
			hold[g] = true
		}
	}
	// What a write that completes the match, or carries a decision, still
	// needs of the merge: it is atomic with what it changes, so these rules
	// hold it WHOLE, before the score replay ever sees it.
	holdWhole := mc.holdAll != ""
	// A running or scheduled write over a finished match does not carry the
	// verdict. One that cannot be ordered against the result (unstamped) is
	// held whole; a stamped one is the replay's to order (mergeByReplay): a
	// change made before the result is ordered by its stamp, one made after it
	// is turned away (the closed-match rule).
	finished := runningOverFinished(stored, incoming) && !inChanged[state.GroupResult]
	if finished && stamp <= 0 {
		holdWhole = true
	}

	// An engi finish is atomic with its count (HoldReasonEngiAtomic): a
	// verdict older than the stored one (a reopen made after it) holds the
	// count with it, rather than landing the count under a running match.
	engiAtomic := mc.comp != nil && mc.comp.Engi && inChanged[state.GroupResult] && hold[state.GroupResult] &&
		completesMatch(incoming, mc) && state.GroupDiffers(stored, incoming, state.GroupResult)
	holdWhole = holdWhole || engiAtomic
	// A previous build's running write older than one its own board already
	// sent (a lower revision of the same session than one the log holds,
	// out of order after a reconnect flush) is held whole: its stamp may tie
	// the newer one's, which the replay could not order.
	olderRevision := !holdWhole && replaysSnapshot(incoming) && incoming.Status == state.MatchStatusRunning &&
		olderRevisionThanLogged(mc.lines, incoming.RevSession, incoming.Rev)
	holdWhole = holdWhole || olderRevision
	if holdWhole {
		for _, g := range changed {
			hold[g] = true
		}
	}

	// What the write said about the representatives, before anything copies
	// stored rows over its own.
	payloadPickA, payloadPickB := incoming.RepPicks()
	// A write that names the representative bout and carries no row removes
	// it: the daihyosen DELETE, or a stale whole-match write that omits it.
	removalWrite := inChanged[state.BoutGroup(state.DaihyosenSubPosition)] &&
		state.DaihyosenSubIndex(incoming.SubResults) < 0 && state.DaihyosenSubIndex(stored.SubResults) >= 0

	// A scoring snapshot (a score door, the engine's own callers, the engi
	// finish) is the score replay's: its groups are ordered at their stamps
	// and a change to a finished match is judged by the replay (the
	// closed-match rule), not by this merge.
	if !holdWhole && replaysSnapshot(incoming) {
		return mergeByReplay(stored, incoming, changed, stamp, mc, payloadPickA, payloadPickB, removalWrite)
	}

	// Every group either side holds, plus a changed bout neither holds (a
	// removal over a tombstone).
	groups := append([]string(nil), state.ScalarGroups...)
	// The server's groups are copied from the stored match here, and again
	// once every row is in place (below), since a bout copy never moves a
	// mark (CopyGroup).
	groups = append(groups, state.ServerGroups...)
	groups = append(groups, storedBoutGroups(stored)...)
	for _, p := range state.SubPositions(incoming.SubResults) {
		if g := state.BoutGroup(p); !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}
	for _, g := range changed {
		if !slices.Contains(groups, g) {
			groups = append(groups, g)
		}
	}

	holdReason := mc.holdAll
	switch {
	case holdReason != "":
	case engiAtomic:
		holdReason = HoldReasonEngiAtomic
	case olderRevision:
		holdReason = HoldReasonOlderRevision
	}
	rep := &state.MergeReport{Stamp: stamp, Changed: changed, HoldReason: holdReason}
	storedStamps := state.MaterializedGroupStamps(stored.GroupStamps, stored.ModifiedAt, state.SubPositions(stored.SubResults))
	stamps := state.CloneGroupStamps(storedStamps)
	for _, g := range groups {
		if inChanged[g] && !hold[g] {
			rep.Applied = append(rep.Applied, g)
			if stamp > 0 {
				stamps[g] = stamp
			}
			continue
		}
		if inChanged[g] {
			reportHeld(rep, g, stored, incoming, nil)
		}
		state.CopyGroup(incoming, stored, g)
	}
	resultApplied := inChanged[state.GroupResult] && !hold[state.GroupResult]
	rep.ResultChanged = resultApplied
	mh := mergeHold{rep: rep, stored: stored, incoming: incoming, stamps: stamps, storedStamps: storedStamps}

	// S3: a judges' decision is the result's. A points change from a write
	// whose result did not apply (a write that does not name the verdict)
	// keeps the stored hantei mark rather than erasing it.
	if !resultApplied && slices.Contains(rep.Applied, state.GroupPoints) {
		carryHantei(stored, incoming)
	}

	// The representatives sit on the representative bout's row, so what
	// became of the row is judged once every hold above has had its say.
	recomputeModifiedAt := mh.settleOrphanPicks(payloadPickA, payloadPickB, removalWrite, stamp)
	// The recorded marks are the server's (ServerGroups): no write names
	// them, so once every hold and displacement above has put its rows in
	// place, every row takes the stored match's marks, whichever copy (the
	// stored match's or the write's own) the row came from. A mark the
	// payload carried can never land.
	state.CopyGroup(incoming, stored, state.GroupRecorded)
	// Last: a recorded bout whose overtime this write raises, or whose
	// result it changes, is being fought again.
	mh.unrecordReopenedBouts(stamp)

	for _, g := range rep.Applied {
		if !state.GroupDiffers(stored, incoming, g) {
			rep.Unchanged = append(rep.Unchanged, g)
		}
	}
	// A group that moved nothing keeps its stored stamp, but ONLY when the
	// write named no groups at all (Changed is nil: an older client). Such a
	// write cannot tell an echo from a change -- it re-sends every group it
	// carries whether or not the operator touched it -- so letting its
	// echoes take its own stamp would fence out a real, older change made on
	// another device in the gap. A write that DOES name a group named it
	// because it changed against the operator's own screen, so a named echo
	// (the same value sent back, e.g. a replayed decision) is still a real
	// action taken at this write's stamp and must keep that stamp. Delete
	// rather than zero when the stored match never stamped the group at all,
	// so an echoed legacy group keeps reading as never written.
	if incoming.Changed == nil {
		for _, g := range rep.Unchanged {
			if s, ok := storedStamps[g]; ok {
				stamps[g] = s
			} else {
				delete(stamps, g)
			}
		}
	}
	// A verdict that applied but equals the stored one (an echo: the same
	// decision sent again under a later stamp) did not move. Recording it
	// again would rewrite the competitor's status (and undo a reinstatement
	// made since), so it has no eligibility consequence.
	if slices.Contains(rep.Unchanged, state.GroupResult) {
		rep.ResultChanged = false
	}
	// The winner's member id is the bout's, the representatives their own
	// change: after both are settled, the id is derived again from the winner's
	// name and the stored picks, so it is exactly the winning side's pick (or
	// empty), whatever id the write carried.
	if i := state.DaihyosenSubIndex(incoming.SubResults); i >= 0 {
		incoming.SubResults[i].ReconcileWinnerMemberID()
	}
	if len(stamps) == 0 {
		stamps = nil
	}
	incoming.GroupStamps = stamps
	incoming.ModifiedAt = stored.ModifiedAt
	if recomputeModifiedAt {
		// A newer stamp left with the pick it was moved out with:
		// ModifiedAt is the newest stamp the match still holds.
		incoming.ModifiedAt = 0
		for _, s := range stamps {
			incoming.ModifiedAt = max(incoming.ModifiedAt, s)
		}
	}
	// A write that named its groups moves ModifiedAt whenever it is actually
	// persisted (an echo included: see above), since naming a group is
	// itself the operator's action at this stamp. A write that named NONE
	// (Changed nil) moves it only when something it carried actually
	// differed from stored, or a whole-match echo would drag ModifiedAt
	// forward and fence out a real, older change the same way a per-group
	// one would.
	if (incoming.Changed != nil || len(rep.Applied) > len(rep.Unchanged)) && stamp > incoming.ModifiedAt {
		incoming.ModifiedAt = stamp
	}
	incoming.Merge = rep
	return rep
}

// replaysSnapshot reports whether a write is one the score replay decides
// (bc-evts stage 2b): a scoring snapshot (/score, bulk-score, quick-score, the
// engine's own callers, the engi finish) or any decision (a kiken, fusenpai,
// fusensho, daihyosen or hikiwake closes a match, so it is ordered by its
// stamp against the scoring around it, as a finish is). Every other door (the
// representative bout's add and remove; a start's server twins) names what it
// changes on purpose and is merged group by group below.
func replaysSnapshot(incoming *state.MatchResult) bool {
	switch doorOf(incoming) {
	case DoorScore, DoorBulkScore, DoorQuickScore, DoorEngine, doorEngi, DoorDecision:
		return true
	}
	return false
}

// olderRevisionThanLogged reports whether the log holds a baseline written by
// the same board session (RevSession) at a higher revision than rev, since the
// match last started running: a reopen, a send back to the queue or a result
// that left running ends the board's run, and a board may count its
// revisions afresh after one.
func olderRevisionThanLogged(lines []state.ScoreLogLine, session string, rev int64) bool {
	if session == "" || rev <= 0 {
		return false
	}
	for i := len(lines) - 1; i >= 0; i-- {
		b := lines[i].Baseline
		if b == nil {
			continue
		}
		if b.Door == doorReopen || b.Door == doorRequeue {
			return false
		}
		if raw, ok := b.Values[state.GroupResult]; ok {
			var m state.MatchResult
			if state.SetGroupFromValue(&m, state.GroupResult, raw) == nil && m.Status != state.MatchStatusRunning {
				return false
			}
		}
		if b.RevSession == session && b.Rev > rev {
			return true
		}
	}
	return false
}

// mergeByReplay decides a scoring snapshot by the score replay: the stored
// match's log (its migration baseline when it has none, a drift baseline
// when the stored match is not what the log replays to), then the write as
// a baseline of the groups it changes at its stamp, replayed together. The
// match takes what the replay gives for every group; what the write sent
// and the replay did not keep is held (in the history), and a newer stored
// change the write's result closed out of the match is displaced (the
// closed-match rule, in the other arrival order). The lines the replay needed ahead of the write's own ride on the
// report (LogPrefix), for the door's baseline to log first.
func mergeByReplay(stored, incoming *state.MatchResult, changed []string, stamp int64, mc mergeCtx, payloadPickA, payloadPickB string, removalWrite bool) *state.MergeReport {
	rep := &state.MergeReport{Stamp: stamp, Changed: changed, Replayed: true}
	shape := ReplayShape{Comp: mc.comp, Knockout: mc.knockout}
	now := serverNowMs()
	lines := mc.lines
	if len(lines) == 0 {
		mb := MigrationBaseline(stored, MigrationDoor+"-0", now)
		lines = []state.ScoreLogLine{{Baseline: &mb}}
		rep.LogPrefix = lines
	} else if drift := driftBaseline(stored, lines, shape, now); drift != nil {
		lines = append(slices.Clone(lines), *drift)
		rep.LogPrefix = []state.ScoreLogLine{*drift}
	}
	// The write's own stamps are never a client's: every group it changes is
	// dated by the write. An unstamped one is ordered after everything the
	// match holds (the old unstamped bypass).
	at := stamp
	if at == 0 {
		at = max(now, stored.ModifiedAt, newestLineStamp(lines))
	}
	sent := *incoming
	sent.GroupStamps = nil
	sent.SubResults = state.CloneSubResults(incoming.SubResults)
	// A representative bout the write is silent on (nil ippons) keeps the
	// judges' decision recorded on it: the baseline carries the verdict, not
	// a blank the replay would read as the operator clearing it.
	preserveDaihyosenOutcome(stored.SubResults, &sent)
	// The winner's id is worked out after the merge, from the sides, for a
	// payload that omits it; the baseline is logged as sent, so it carries
	// the id the stored match will hold.
	if sent.SideAID == "" && sent.SideBID == "" {
		sent.SideAID, sent.SideBID = stored.SideAID, stored.SideBID
	}
	if sent.SideA == "" && sent.SideB == "" {
		sent.SideA, sent.SideB = stored.SideA, stored.SideB
	}
	if sent.Winner != "" {
		if err := resolveWinnerIDFromSides(&sent, matchWriteRestore); err != nil {
			log.Printf("engine: match %s: winner id not worked out for the baseline: %v", incoming.ID, err)
		}
	}
	// A write naming no groups cannot tell an echo from a change, so a group
	// it only echoes keeps its stored stamp: it is not part of its baseline.
	groups := changed
	if incoming.Changed == nil {
		groups = slices.DeleteFunc(slices.Clone(changed), func(g string) bool { return !state.GroupDiffers(stored, &sent, g) })
		// A finish that changes anything declares its verdict with it, even
		// one equal to the stored verdict: the scoreline it lands is the one
		// the verdict stands on, so both are dated by the finish.
		if len(groups) > 0 && slices.Contains(changed, state.GroupResult) && !slices.Contains(groups, state.GroupResult) &&
			completesMatch(&sent, mc) {
			groups = append(groups, state.GroupResult)
		}
	}
	// The baseline is the write as it was made: this one baseline is judged
	// here and logged by the door (rep.Baseline), so the log holds what was
	// sent and not what the replay made of it at the time.
	basis := state.ScoreBasis{Seen: len(lines)}
	switch {
	case incoming.Basis != nil:
		basis = *incoming.Basis
	case doorOf(incoming) == DoorScore || doorOf(incoming) == DoorBulkScore:
		// A write the client built says nothing of the events it saw, so
		// none is called recorded: Seen is unknown, not "all of them".
		basis = state.ScoreBasis{}
	}
	bl := BaselineFromMatch(&sent, logBaselineID(len(lines), doorOf(incoming)), doorOf(incoming), at, groups, basis)
	rep.Baseline = &bl
	all := append(slices.Clone(lines), state.ScoreLogLine{Baseline: &bl})
	after := ReplayScoring(stored, all, shape)
	final := &after.Match

	// Every group takes what the replay gives; one the replay left as the
	// write sent it keeps the write's own copy (an omitted scoreline stays
	// omitted for what judges silence after the merge).
	groupsOf := replayedGroups(stored, final)
	for _, p := range state.SubPositions(sent.SubResults) {
		if g := state.BoutGroup(p); !slices.Contains(groupsOf, g) {
			groupsOf = append(groupsOf, g)
		}
	}
	for _, g := range groupsOf {
		if !bytes.Equal(state.GroupValue(final, g), state.GroupValue(&sent, g)) {
			state.CopyGroup(incoming, final, g)
		}
	}
	// The write's side hint travels with the winner it named, for the
	// identity backfill after the merge to resolve the winner's id by.
	if incoming.Winner == sent.Winner && incoming.WinnerID == sent.WinnerID {
		incoming.WinnerSide = sent.WinnerSide
	}
	stamps := state.CloneGroupStamps(final.GroupStamps)
	if stamps == nil {
		stamps = map[string]int64{}
	}
	// The groups the replay itself turned away: a scoring write over a match
	// a result stamped before it had closed lands nothing, echo or not.
	var turnedAway []string
	if h, ok := after.Held[len(all)-1]; ok && h.MatchClosed {
		turnedAway = h.Groups
	}
	for _, g := range changed {
		was, got, said := state.GroupValue(stored, g), state.GroupValue(final, g), state.GroupValue(&sent, g)
		switch {
		case slices.Contains(turnedAway, g):
			reportHeld(rep, g, stored, &sent, said)
		case bytes.Equal(got, said), !bytes.Equal(got, was), g == state.GroupPoints && sameModuloHantei(final, &sent):
			// Landed, as sent or as the replay adjusted it (a winner worked
			// out again; a judges' decision kept: a board that never showed
			// the mark sends the scoreline without it, which echoes the
			// stored one and only the mark is carried).
			rep.Applied = append(rep.Applied, g)
		default:
			reportHeld(rep, g, stored, &sent, nil)
		}
	}
	if h, ok := after.Held[len(all)-1]; ok && len(rep.Held) > 0 {
		rep.HoldReason, rep.MatchClosed = h.Reason, h.MatchClosed
	}
	// The closed-match rule, in the other arrival order: scoring the log
	// holds from an EARLIER line (a stored baseline no write of this one
	// names) is stamped after this write's result, so it cannot apply. It
	// leaves the match for the history exactly as the same scoring arriving
	// after this write is refused (one entry per stamp), and the match is
	// what that other arrival order gives.
	displacedEarlier := false
	if stamp > 0 {
		byStamp := map[int64]map[string]json.RawMessage{}
		for line, h := range after.Held {
			if !h.MatchClosed || line == len(all)-1 {
				continue
			}
			for _, g := range h.Groups {
				if !state.IsScoringGroup(g) && g != state.GroupEncho {
					continue
				}
				// Only a held line that IS the stored value is displaced: a
				// later stored scoring of the group superseded the held one
				// and stays in the match.
				if hb := all[line].Baseline; hb == nil || baselineGroupStamp(hb, g) != stored.GroupStamp(g) {
					continue
				}
				// And only one the replay took out of the match.
				was := state.GroupValue(stored, g)
				if bytes.Equal(was, state.GroupValue(final, g)) {
					continue
				}
				at := stored.GroupStamp(g)
				if byStamp[at] == nil {
					byStamp[at] = map[string]json.RawMessage{}
				}
				byStamp[at][g] = was
				state.CopyGroup(incoming, final, g)
				displacedEarlier = true
			}
		}
		for _, at := range slices.Sorted(maps.Keys(byStamp)) {
			rep.Displaced = append(rep.Displaced, state.DisplacedChange{Stamp: at, Values: byStamp[at], Reason: HoldReasonMatchClosed})
			rep.MatchClosed = true
		}
	}
	rep.ResultChanged = state.GroupDiffers(stored, final, state.GroupResult)
	storedStamps := state.MaterializedGroupStamps(stored.GroupStamps, stored.ModifiedAt, state.SubPositions(stored.SubResults))
	mh := mergeHold{rep: rep, stored: stored, incoming: incoming, stamps: stamps, storedStamps: storedStamps}
	// A group the write did not land keeps the stamp it had (a pick on a row
	// the replay took out lands nothing, though its baseline dated it).
	for _, g := range append(slices.Clone(rep.Held), rep.HeldEcho...) {
		mh.restoreStamp(g)
	}
	recompute := mh.settleOrphanPicks(payloadPickA, payloadPickB, removalWrite, stamp) || displacedEarlier
	state.CopyGroup(incoming, final, state.GroupRecorded)
	mh.unrecordReopenedBouts(stamp)
	for _, g := range rep.Applied {
		if !state.GroupDiffers(stored, incoming, g) {
			rep.Unchanged = append(rep.Unchanged, g)
		}
	}
	if i := state.DaihyosenSubIndex(incoming.SubResults); i >= 0 {
		incoming.SubResults[i].ReconcileWinnerMemberID()
	}
	if len(stamps) == 0 {
		stamps = nil
	}
	incoming.GroupStamps = stamps
	incoming.ModifiedAt = cmp.Or(final.ModifiedAt, stored.ModifiedAt)
	if recompute {
		incoming.ModifiedAt = 0
		for _, s := range stamps {
			incoming.ModifiedAt = max(incoming.ModifiedAt, s)
		}
	}
	incoming.Merge = rep
	return rep
}

// mergeReplayedWrite is the merge of the score replay's derived write
// (DoorEvent, bc-evts). The replay built incoming from the stored match and the
// whole event log, in (Stamp, ID) order, with the rules after a result in
// event form, so every group it changed applies with the stamp the replay gave
// it (never the write's newest, or a later baseline would absorb events it
// never saw). The recorded marks are the replay's too: an event un-records a
// kachinuki bout it reopens. A group the replay left alone already holds the
// stored value. The representative bout's winner id is derived again, as
// every write's is.
func mergeReplayedWrite(stored, incoming *state.MatchResult) *state.MergeReport {
	rep := &state.MergeReport{Changed: slices.Clone(incoming.Changed)}
	for _, s := range incoming.GroupStamps {
		rep.Stamp = max(rep.Stamp, s)
	}
	for _, g := range rep.Changed {
		rep.Applied = append(rep.Applied, g)
		if !state.GroupDiffers(stored, incoming, g) {
			rep.Unchanged = append(rep.Unchanged, g)
		}
	}
	rep.ResultChanged = state.GroupDiffers(stored, incoming, state.GroupResult)
	if i := state.DaihyosenSubIndex(incoming.SubResults); i >= 0 {
		incoming.SubResults[i].ReconcileWinnerMemberID()
	}
	incoming.GroupStamps = state.CloneGroupStamps(incoming.GroupStamps)
	if len(incoming.GroupStamps) == 0 {
		incoming.GroupStamps = nil
	}
	incoming.ModifiedAt = max(incoming.ModifiedAt, stored.ModifiedAt)
	incoming.Merge = rep
	return rep
}

// verdictMoved reports whether working the winner out again changed the
// verdict a write stated: its status, decision or winner (by name, and by id
// where the write named one; a payload's omitted id is not a change).
// snapshotScoring copies the groups of m that decide who won (points, flags,
// bouts), deep, so the merge's later copies over m cannot reach it.
func cloneIppons(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

// carryHantei puts a judges'-decision mark from one scoreline onto another
// that carries none, on the same side, through domain.AppendHantei (the one
// placement rule): the judges' decision is the result's, so a points change
// that does not claim the result does not erase it (S3). The replay applies
// the same rule to a baseline over a finished match (judgeOverFinished).
func carryHantei(from, to *state.MatchResult) {
	if domain.ContainsHantei(to.IpponsA) || domain.ContainsHantei(to.IpponsB) {
		return
	}
	switch {
	case domain.ContainsHantei(from.IpponsA):
		to.IpponsA = domain.AppendHantei(append([]string(nil), to.IpponsA...))
	case domain.ContainsHantei(from.IpponsB):
		to.IpponsB = domain.AppendHantei(append([]string(nil), to.IpponsB...))
	}
}

// sameModuloHantei reports whether two matches hold the same points group
// apart from the judges' hantei mark (which a write that never showed it does
// not carry, and carryHantei puts back): the write then echoed the stored
// scoreline and is no loss.
func sameModuloHantei(a, b *state.MatchResult) bool {
	return slices.Equal(domain.StripHantei(a.IpponsA), domain.StripHantei(b.IpponsA)) &&
		slices.Equal(domain.StripHantei(a.IpponsB), domain.StripHantei(b.IpponsB)) &&
		a.HansokuA == b.HansokuA && a.HansokuB == b.HansokuB
}

// mergeHold is what the group-by-group merge needs of a write once its groups
// are placed: the report, the stored and incoming matches and both stamp maps.
// Its methods settle the parts of a write that depend on the placed groups
// (the representative picks, the stamps a held group keeps, the recorded
// kachinuki marks); a group the merge or the replay holds is reported held
// with reportHeld.
type mergeHold struct {
	rep          *state.MergeReport
	stored       *state.MatchResult
	incoming     *state.MatchResult
	stamps       map[string]int64
	storedStamps map[string]int64
}

// unrecordReopenedBouts takes the recorded mark off every numbered bout this
// write reopens, as a change of GroupRecorded at the write's stamp, named in
// its history line. A recorded bout is reopened when the write either
//
//   - raises its overtime (bc-kfnl: the bout is being fought again), or
//   - changes its RESULT (boutResultChanged: the side its winner names, or its
//     decision, a draw read as a draw), e.g. a point scored on a tie recorded
//     with both sides out: the bout Record bout closed is no longer the one
//     on the row, so the operator records it again (review round 8). A
//     correction that keeps the result (another waza letter, the same winner)
//     keeps the mark.
//
// Only a bout whose own group applied counts: a held bout changed nothing. And
// only when the write is not older than the stored marks (a Record bout
// stamped after this change keeps its mark, as it would arriving second): the
// stamp decides, as for any group. An UNSTAMPED write (stamp 0, which
// ApplyByTimestamp applies over anything) never clears a mark: it cannot be
// ordered after the Record bout that set it.
func (h *mergeHold) unrecordReopenedBouts(stamp int64) {
	if stamp <= 0 {
		return
	}
	var raised []int
	for _, g := range h.rep.Applied {
		pos, ok := state.ParseBoutGroup(g)
		if !ok || pos <= 0 {
			continue
		}
		storedRow := state.SubResultAt(h.stored.SubResults, pos)
		row := state.SubResultAt(h.incoming.SubResults, pos)
		if !storedRow.Recorded || row.Position != pos {
			continue
		}
		if enchoPeriodsRise(storedRow.Encho, row.Encho) || boutResultChanged(storedRow, row, h.stored.SideA, h.stored.SideB) {
			raised = append(raised, pos)
		}
	}
	if len(raised) == 0 || !domain.ApplyByTimestamp(stamp, h.stored.GroupStamp(state.GroupRecorded)) {
		return
	}
	for i := range h.incoming.SubResults {
		if slices.Contains(raised, h.incoming.SubResults[i].Position) {
			h.incoming.SubResults[i].Recorded = false
		}
	}
	h.rep.Changed = append(slices.Clone(h.rep.Changed), state.GroupRecorded)
	h.rep.Applied = append(h.rep.Applied, state.GroupRecorded)
	h.stamps[state.GroupRecorded] = stamp
}

// enchoPeriodsRise reports whether a bout row's overtime period count rises
// from stored to incoming: overtime started (from none) or extended. A write
// that raises it on a recorded bout takes the bout's recorded mark off
// (mergeHold.unrecordReopenedBouts): the bout is being fought again, so both its
// fighters are on, e.g. a knockout tie recorded when both teams had nobody
// left, then fought on in encho.
func enchoPeriodsRise(stored, incoming *state.EnchoMetadata) bool {
	periods := func(e *state.EnchoMetadata) int {
		if e == nil {
			return 0
		}
		return e.PeriodCount
	}
	return periods(incoming) > periods(stored)
}

// boutResultChanged reports whether row says a different result for its bout
// than stored: the side its winner names (state.SubBoutWinnerSide, ids first,
// read against the match's sides sideA/sideB) or its decision, every draw
// decision read as one draw and "fought" as no decision. A different waza
// letter for the same winner is not a different result.
func boutResultChanged(stored, row state.SubMatchResult, sideA, sideB string) bool {
	decision := func(sub state.SubMatchResult) string {
		switch {
		case state.IsDraw(sub.Decision):
			return string(domain.DecisionHikiwake)
		case sub.Decision == string(domain.DecisionFought):
			return ""
		}
		return sub.Decision
	}
	return state.SubBoutWinnerSide(stored, sideA, sideB) != state.SubBoutWinnerSide(row, sideA, sideB) ||
		decision(stored) != decision(row)
}

// restoreStamp puts group's stamp back to the one the stored match had, or
// takes it out again when the stored match never stamped it.
func (h *mergeHold) restoreStamp(group string) {
	if s, ok := h.storedStamps[group]; ok {
		h.stamps[group] = s
	} else {
		delete(h.stamps, group)
	}
}

// settleOrphanPicks is the representatives' rule, asked ONCE after every hold
// has had its say (a hold can take the representative bout's row out of the
// write, or put the stored one back): a pick lives on that row, so what became
// of the row decides what became of each side's pick. payloadA and payloadB are
// the ids the write carried, captured before the merge copied stored rows over
// its own, and removalWrite says it named the bout and carried no row.
//
//   - The row is gone and the write introduced a pick for a side (a row its
//     stamp could not bring back, or one a hold took out of the write): the pick is held with its id, never applied onto
//     nothing, and the side keeps the stamp it had.
//   - The write removed the row and applied, and a pick stamped AFTER the
//     removal is stored (J): in stamp order the removal came first and the pick
//     landed on no bout, which is what the line above does when they arrive
//     that way round. The stored pick is moved to the history under its own
//     stamp with its id (a DisplacedChange, HoldReasonRepBoutRemoved), and the
//     side is dated by the removal, the last thing that happened to the row, so
//     both orders end in one state. It returns true then: the match no longer
//     holds the newer stamp.
//   - The write removed the row and a hold kept it (a point made after the
//     removal is stored): a removal is one action, so its picks stay with the
//     row that stays, held with the bout instead of clearing a pick the row
//     still carries.
func (h *mergeHold) settleOrphanPicks(payloadA, payloadB string, removalWrite bool, stamp int64) bool {
	hasRow := state.DaihyosenSubIndex(h.incoming.SubResults) >= 0
	storedRow := state.DaihyosenSubIndex(h.stored.SubResults) >= 0
	storedA, storedB := h.stored.RepPicks()
	displaced := map[int64]map[string]json.RawMessage{}
	for _, side := range []struct {
		side            domain.MatchSide
		payload, stored string
	}{{domain.MatchSideA, payloadA, storedA}, {domain.MatchSideB, payloadB, storedB}} {
		g := state.RepPickGroup(side.side)
		applied := slices.Index(h.rep.Applied, g)
		switch {
		case hasRow:
			if !removalWrite || applied < 0 {
				continue
			}
			h.rep.Applied = slices.Delete(h.rep.Applied, applied, applied+1)
			// What the write said was an empty pick: held if the row still
			// carries one, an echo if it does not.
			reportHeld(h.rep, g, h.stored, &state.MatchResult{}, nil)
			state.CopyGroup(h.incoming, h.stored, g)
			h.restoreStamp(g)
		case side.payload != "" && applied >= 0:
			h.rep.Applied = slices.Delete(h.rep.Applied, applied, applied+1)
			ghost := &state.MatchResult{SubResults: []state.SubMatchResult{{Position: state.DaihyosenSubPosition}}}
			ghost.SetRepPick(side.side, side.payload)
			reportHeld(h.rep, g, h.stored, ghost, nil)
			h.restoreStamp(g)
		case storedRow && side.stored != "" && slices.Contains(h.rep.Held, g):
			at := h.stored.GroupStamp(g)
			if displaced[at] == nil {
				displaced[at] = map[string]json.RawMessage{}
			}
			displaced[at][g] = state.GroupValue(h.stored, g)
			// The removal's own (empty) pick now applies, as it does when it
			// arrives first, and the newer one is the history's.
			i := slices.Index(h.rep.Held, g)
			h.rep.Held = slices.Delete(h.rep.Held, i, i+1)
			delete(h.rep.HeldValues, g)
			if len(h.rep.HeldValues) == 0 {
				h.rep.HeldValues = nil
			}
			if applied < 0 {
				h.rep.Applied = append(h.rep.Applied, g)
			}
			if stamp > 0 {
				h.stamps[g] = stamp
			}
		}
	}
	stamps := make([]int64, 0, len(displaced))
	for at := range displaced {
		stamps = append(stamps, at)
	}
	slices.Sort(stamps)
	for _, at := range stamps {
		h.rep.Displaced = append(h.rep.Displaced, state.DisplacedChange{
			Stamp: at, Values: displaced[at], Reason: HoldReasonRepBoutRemoved,
		})
	}
	return len(stamps) > 0
}

// HoldReasonMatchClosed is the history reason of a scoring write held because a
// finish or a decision stamped before it had already closed the match (the
// closed-match rule): Correct result is how a closed match changes.
const HoldReasonMatchClosed = "the match was already finished"

// MatchClosedMessage is the sentence a device is answered with when a tap or a
// scoring write was turned away because the match was already closed, on the
// score door and the score-events door alike. It is kept in the match's
// history. The sentence names no button beyond the one every score editor
// offers for a finished match, Correct result.
const MatchClosedMessage = "Not applied: this match is already finished, so this change was kept in the match's history. To change the result, use Correct result."

// HoldReasonRepBoutRemoved is the history reason of a representative picked
// after the representative bout was removed (stamp order: the removal first,
// the pick onto a bout that no longer existed), which a removal arriving
// second moves out of the match. It is no match_closed reason: the answer
// carries displacedGroups for it and no heldReason.
const HoldReasonRepBoutRemoved = "the representative bout was removed"

// completesMatch reports whether the merged write leaves the match finished
// (an empty status completes a bracket match, effectiveBracketWriteStatus).
func completesMatch(m *state.MatchResult, mc mergeCtx) bool {
	return m.Status == state.MatchStatusCompleted || (mc.knockout && m.Status == "")
}

// reportHeld records that group of the write was held: as held, with its
// incoming value (raw when the merge had adjusted it) kept for the history,
// or, when that value is exactly the stored one, as an echo, which loses
// nothing and so is neither listed as held nor kept.
func reportHeld(rep *state.MergeReport, group string, stored, incoming *state.MatchResult, raw json.RawMessage) {
	if raw == nil && !state.GroupDiffers(stored, incoming, group) {
		rep.HeldEcho = append(rep.HeldEcho, group)
		return
	}
	if raw == nil {
		raw = state.GroupValue(incoming, group)
	}
	rep.Held = append(rep.Held, group)
	if rep.HeldValues == nil {
		rep.HeldValues = map[string]json.RawMessage{}
	}
	rep.HeldValues[group] = raw
}

// logUnstampedOverwrite keeps the unstamped bypass visible (bc-cse). An
// unstamped forward write over a STAMPED stored result is the one path that
// overwrites a known-newer result with no comparison possible:
// domain.ApplyByTimestamp reads 0 as "no opinion" and applies. The bypass
// STAYS (legacy clients and files written before the ModifiedAt column depend
// on it), but it must not be invisible. Since the bc-mrgc review a completing
// or server-built unstamped write is stamped by the server (writeStamp), so
// what still bypasses is a legacy client's unstamped running or scheduled
// write. RUNNING writes are excluded by volume: a legacy SPA autosaving on
// the debounce would log once per keystroke and bury every other line; a
// SCHEDULED one still logs.
func logUnstampedOverwrite(result *state.MatchResult, storedModifiedAt int64) {
	if result.ModifiedAt == 0 && storedModifiedAt > 0 && result.Status != state.MatchStatusRunning {
		log.Printf("engine: match %s: unstamped write overwrites a result stamped %d (unstamped bypass, no last-write-wins comparison possible)",
			result.ID, storedModifiedAt)
	}
}

// writeStamp is the stamp a write is ordered by: its own modifiedAt, or, for
// an UNSTAMPED write the server orders itself, the server's time. That is a
// completing write (a legacy client's Finish, a bulk-score entry, an engine
// caller) and every write a server door builds (quick-score, a decision made
// without a stamp): left at 0 they applied over everything and left no group
// stamp behind, so a stale running replay arriving after them was ordered
// against the OLDER stamp before them and overwrote the correction (bc-mrgc
// review S5). The server's time is the time of the write: anything already
// stored arrived before it, so a stored stamp ahead of the server's clock is
// a device's skew (at most the 5s the handlers accept), and the write is
// stamped no older than it, keeping such a write's always-applies behaviour.
//
// A legacy client's unstamped RUNNING or scheduled write keeps the unstamped
// bypass (0: applies, moves no stamp), which TestMerge_LegacyMatchesBehaveAsBefore
// pins: an autosave from a client that predates stamps cannot be ordered, and
// stamping it now would let it outrank a finish made before it arrived.
func writeStamp(stored, incoming *state.MatchResult) int64 {
	if incoming.ModifiedAt > 0 {
		return incoming.ModifiedAt
	}
	clientRunning := (incoming.Status == state.MatchStatusRunning || incoming.Status == state.MatchStatusScheduled) &&
		slices.Contains(clientDoors, incoming.WriteDoor)
	if clientRunning {
		return incoming.ModifiedAt
	}
	return max(serverNowMs(), stored.ModifiedAt)
}

// WholeMatchChangedGroups is what a server-built write that replaces the
// whole encounter changes (quick-score): every group, and every bout row the
// stored match holds as well as the ones the write builds, so a row it does
// not rebuild is removed exactly as the whole-match write always removed it.
// An unknown match answers no groups and leaves the error to the write.
func (e *Engine) WholeMatchChangedGroups(h state.StoreTx, compID, matchID string, result *state.MatchResult) ([]string, error) {
	prior, err := e.lookupExistingResult(h, compID, matchID)
	if err != nil {
		var nf *NotFoundError
		if errors.As(err, &nf) {
			return []string{}, nil
		}
		return nil, err
	}
	return defaultChangedGroups(prior, result, true), nil
}

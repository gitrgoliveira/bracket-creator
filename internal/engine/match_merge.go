package engine

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
// The rule (design C, "merge changes"):
//
//   - A write names the GROUPS it changes (state.MatchResult.Changed; the
//     groups are owned by state/match_groups.go). Each changed group is
//     applied only if the write's stamp (modifiedAt: when the change was
//     made, server-relative, never arrival order) is not older than that
//     group's stored stamp, with domain.ApplyByTimestamp's semantics (0 on
//     either side applies). An applied group takes the write's stamp.
//   - A group the write does not change is never overwritten: the stored
//     value is copied into the incoming result, so the rest of the pipeline
//     judges the merged match.
//   - A changed group that is not applied is HELD: its incoming value goes to
//     the match's history (match_history.go), never discarded.
//   - The write is superseded only when every group it changes is held.
//
// Four rules ride on top, each with one home here:
//
//   - R2 (withdrawalOutranked, and the clearing block in mergeMatchWrite): a
//     withdrawal or default win is atomic with its scoreline, and is held when
//     any stored points or bout change was made after it. A scoring change
//     made after a stored withdrawal, on a board that was scoring the match
//     (a running write), clears it: points scored mean the withdrawal was a
//     mistake. The eligibility restore follows from the decision changing
//     (restoreIfWithdrawalRemoved).
//   - R3 (runningOverFinished): a running or scheduled write over a finished
//     match never carries the result group. Its scoring changes apply to the
//     finished match only when made after the result; the match stays
//     finished and the winner is worked out again (deriveWinnerAfterMerge).
//   - R4 (deriveWinnerAfterMerge reporting false, and mergeHold), operator
//     ruling 2026-10-04: "It needs a winner if the match is finished and is
//     being corrected." A scoring change that would leave a finished knockout
//     match tied, or an engi match with no valid flag count (pool or
//     knockout), is NOT applied: the match keeps its recorded finish (never
//     back to running), the change is held in the history with that reason,
//     and the answer carries heldReason "needs_winner". A pool or league tie
//     becomes a completed draw.
//   - S2 (the same owners, the other arrival order): a finish whose result
//     applies over scoring stored NEWER than it gets the verdict that scoring
//     gives, exactly as R3 would have made of the two arriving the other way
//     round, keeping the finish's own hantei mark (carryHantei). When that
//     scoring would leave the match without the winner it needs, it could
//     not have applied after the finish (R4): the finish is applied on its
//     own scoreline and the newer scoring is moved to the history as held
//     (displaceNewerScoring), so both arrival orders reach one state.
//   - S3 (carryHantei): a judges' decision belongs to the result. A
//     points change from a write whose result did not apply keeps the stored
//     hantei mark.
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
	// knockout is true for a bracket match (R4: it cannot end tied, so a
	// change that would tie a finished one is held).
	knockout bool
	// nilSubsClear says what an incoming nil SubResults means on this branch
	// when the writer stated no groups: the pool's whole-struct write has
	// always replaced the stored bouts with it, the bracket has always kept
	// them. The default keeps both exactly as they were.
	nilSubsClear bool
	// holdAll, when set, holds every group the write changes whatever its
	// stamp, and is recorded as the reason in the history entry. The running
	// rev guard uses it for a write older than one the same board already
	// sent (ForceOptions.HoldReason): kept, never applied, never dropped.
	holdAll string
	// storedAID and storedBID are the stored match's side ids, set by
	// mergeMatchWrite: a payload that omits its ids (they are backfilled
	// only after the merge) still gets the winner's id worked out.
	storedAID, storedBID string
}

// runningOverFinished reports R3's shape: a running or scheduled write over a
// finished match. A client's such write never carries the result group
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
// result group for R3's shape.
func effectiveChangedGroups(stored, incoming *state.MatchResult, nilSubsClear bool) []string {
	var changed []string
	if incoming.Changed == nil {
		changed = defaultChangedGroups(stored, incoming, nilSubsClear)
	} else {
		for _, g := range incoming.Changed {
			if state.ValidGroup(g) && !slices.Contains(changed, g) {
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
func withdrawalOutranked(stored *state.MatchResult, changed []string, hold map[string]bool, stamp int64) bool {
	for _, g := range changed {
		if hold[g] {
			return true
		}
	}
	if stamp <= 0 {
		return false
	}
	for _, g := range append([]string{state.GroupPoints}, storedBoutGroups(stored)...) {
		if stored.GroupStamp(g) > stamp {
			return true
		}
	}
	return false
}

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
	// An unstamped write the server itself orders (a completing write, or one
	// a server door built) takes the server's time here, so it leaves a fence
	// a later stale replay is ordered against (writeStamp).
	incoming.ModifiedAt = writeStamp(stored, incoming)
	stamp := incoming.ModifiedAt
	mc.storedAID, mc.storedBID = stored.SideAID, stored.SideBID
	// The write as it arrived, before the merge copies stored groups over
	// it: S2 reads the scoreline a finish was made on from here.
	orig := snapshotScoring(incoming)
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
	// R3's shape: the write would leave a finished match finished, since it
	// does not carry the verdict.
	finished := runningOverFinished(stored, incoming) && !inChanged[state.GroupResult]
	// R3: a change from a board still scoring a finished match applies only
	// when it was made after the result was recorded. An earlier one (an
	// autosave queued before Finish) is held whole; an unstamped one cannot
	// be ordered and is held exactly as the whole-match guard dropped it.
	if finished && (stamp <= 0 || stamp <= stored.GroupStamp(state.GroupResult)) {
		for _, g := range changed {
			hold[g] = true
		}
	}
	// R2, late half: a withdrawal is atomic with what it changes.
	if inChanged[state.GroupResult] && domain.IsDefaultWinDecisionStr(incoming.Decision) &&
		withdrawalOutranked(stored, changed, hold, stamp) {
		for _, g := range changed {
			hold[g] = true
		}
	}
	// Finish atomicity (a write that COMPLETES the match is atomic with
	// what it changes, mirroring R2's withdrawal atomicity and
	// HoldReasonEngiAtomic): when the STORED match already has a verdict,
	// and the write's own verdict is held by its stamp -- a GENUINE hold,
	// not an echo of what is already stored -- the whole write holds with
	// it, so a stale Finish can never move a scoreline in beside a verdict
	// it never declared (a correction made on another device, after it,
	// judging the match differently). Scoped to stored.Status ==
	// completed on purpose: a write stamped before a REOPEN (the stored
	// match is running again, no verdict to protect) is an ordinary stale
	// write, and its bouts apply on their own stamps exactly as they did
	// before this rule, pinned by the reopen handler's own superseded-write
	// tests. Excludes a write OR a stored result that is itself a
	// withdrawal/default win too: that side is already covered above, by
	// withdrawalOutranked, which has its own rule for a stale "fought"
	// write arriving against a stored withdrawal (its bouts are ordered on
	// their own stamps, not held with the verdict). An echoed result (the
	// SAME verdict replayed under an older stamp) is not a hold at all, by
	// rule: the write still merges group by group, as it does today.
	finishAtomic := inChanged[state.GroupResult] && hold[state.GroupResult] &&
		stored.Status == state.MatchStatusCompleted && completesMatch(incoming, mc) &&
		!domain.IsDefaultWinDecisionStr(incoming.Decision) && !domain.IsDefaultWinDecisionStr(stored.Decision) &&
		state.GroupDiffers(stored, incoming, state.GroupResult)
	if finishAtomic {
		for _, g := range changed {
			hold[g] = true
		}
	}

	// Every group either side holds, plus a changed bout neither holds (a
	// removal over a tombstone).
	groups := append([]string(nil), state.ScalarGroups...)
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
	if holdReason == "" && finishAtomic {
		holdReason = HoldReasonFinishAtomic
	}
	// What the write said about the representatives, before the loop below
	// copies stored rows over its own.
	payloadPickA, payloadPickB := incoming.RepPicks()
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
	// A pick has no bout to land on once the representative bout is gone (a
	// removal's tombstone, or a row the write carried that its stamp could not
	// bring back): each side's pick is held, in the history, not applied onto
	// nothing, and that side's stamp goes back to what it was.
	if state.DaihyosenSubIndex(incoming.SubResults) < 0 {
		for _, pick := range []struct {
			side domain.MatchSide
			id   string
		}{{domain.MatchSideA, payloadPickA}, {domain.MatchSideB, payloadPickB}} {
			g := state.RepPickGroup(pick.side)
			i := slices.Index(rep.Applied, g)
			if i < 0 || pick.id == "" {
				continue
			}
			rep.Applied = slices.Delete(rep.Applied, i, i+1)
			ghost := &state.MatchResult{SubResults: []state.SubMatchResult{{Position: state.DaihyosenSubPosition}}}
			ghost.SetRepPick(pick.side, pick.id)
			reportHeld(rep, g, stored, ghost, nil)
			if s, ok := storedStamps[g]; ok {
				stamps[g] = s
			} else {
				delete(stamps, g)
			}
		}
	}
	resultApplied := inChanged[state.GroupResult] && !hold[state.GroupResult]
	rep.ResultChanged = resultApplied
	mh := mergeHold{rep: rep, stored: stored, incoming: incoming, stamps: stamps, storedStamps: storedStamps}

	// S3: a judges' decision is the result's. A points change from a write
	// whose result did not apply (a board still scoring, a write that does
	// not name the verdict) keeps the stored hantei mark rather than erasing
	// it. Before any winner is worked out, so the mark still settles a tie.
	if !resultApplied && slices.Contains(rep.Applied, state.GroupPoints) {
		mh.raw(state.GroupPoints)
		carryHantei(stored, incoming)
	}

	// R2 and R3: a scoring change applied to a finished match. A GENUINE
	// overtime change (its value actually differs from the stored one)
	// rides along even with no scoring change of its own (a board still
	// toggling encho on a match a default win already closed), since
	// R2/R4's default-win branch below must hold it with any scoring too:
	// otherwise an (E) mark could land on a default win on its own, which
	// FIK Art. 32 never produces (one maru in encho, never overtime). An
	// ECHO of the stored overtime (the same "no overtime" sent back) must
	// NOT ride along: it is in rep.Applied like any echo, but entering this
	// block for it alone used to hold the whole write as if something had
	// changed, answering "Not applied" for a write that changed nothing.
	if finished && (scoringChanged(stored, incoming, rep.Applied) ||
		(slices.Contains(rep.Applied, state.GroupEncho) && state.GroupDiffers(stored, incoming, state.GroupEncho))) {
		probe := *incoming
		// R2 clears a withdrawal OF THIS MATCH: points scored after it mean
		// it was a mistake. A fusensho is a default win awarded for a bar
		// recorded on ANOTHER match (the other side cannot fight), which
		// points scored here say nothing about, and a scoreline cannot land
		// beside the circles a default win records without one discarding
		// the other: the default win stands and the scoring is held, in the
		// history with the reason (R4's shape).
		// A clear needs an actual SCORING change: an overtime toggle alone
		// (no point, bout or flag moved) says nothing about whether the
		// withdrawal was a mistake, so it must not read as one -- the
		// stored decision stays, and the encho-only change is held below
		// with the same default-win-stands reason a real default win gets,
		// never silently reopened on an (E) tap alone.
		cleared := domain.IsWithdrawalDecisionStr(probe.Decision) && scoringChanged(stored, incoming, rep.Applied)
		if cleared {
			probe.Decision, probe.DecisionBy, probe.DecisionReason = "", "", ""
			probe.Winner, probe.WinnerID, probe.WinnerSide = "", "", ""
		}
		switch {
		case !cleared && domain.IsDefaultWinDecisionStr(probe.Decision):
			mh.holdDefaultWinScoring(probe.Decision)
		case deriveWinnerAfterMerge(&probe, mc):
			if cleared {
				rep.ClearedWithdrawal = state.GroupValue(incoming, state.GroupResult)
				// The scoring change replaces the ruling: KeepsWithdrawalRuling
				// must not reinstate it from the stored match.
				probe.ClearsWithdrawal = true
			}
			*incoming = probe
			if state.GroupDiffers(stored, incoming, state.GroupResult) {
				rep.ResultChanged = true
				if stamp > 0 {
					stamps[state.GroupResult] = stamp
				}
			}
		default:
			if cleared {
				// The clear cannot give the match the winner it needs (the
				// new scoreline leaves a knockout tied, or an engi count
				// with no valid total): the decision it would have cleared
				// is the only thing that gives the match a winner, so it
				// STANDS instead, exactly as a write that never tried to
				// clear it (holdDefaultWinScoring holds the scoring AND the
				// overtime together, so an (E) mark can never land on a
				// default win on its own, FIK Art. 32). stored.Decision,
				// never probe's: cleared already emptied that.
				mh.holdDefaultWinScoring(stored.Decision)
				break
			}
			// R4 (operator ruling 2026-10-04): the change would leave a
			// finished knockout match tied, or an engi match with no valid
			// count. It is not applied: the match keeps its recorded finish,
			// and the change is kept in the history for the operator to
			// correct the result with a winner.
			mh.holdScoring(needsWinnerReason(mc))
		}
	}

	// S2: a finish applied over scoring stored NEWER than it. In stamp order
	// the finish came first and the newer scoring after it, so the verdict
	// is the one that scoring gives (exactly what R3 makes of the reverse
	// arrival order), worked out by the same owner. A withdrawal is R2's
	// (withdrawalOutranked holds it), never worked out here.
	recomputeModifiedAt := false
	if resultApplied && completesMatch(incoming, mc) && !domain.IsDefaultWinDecisionStr(incoming.Decision) {
		if newest := newestStoredScoringStamp(stored); stamp > 0 && newest > stamp {
			probe := *incoming
			// The finish's judges' decision stays with its result, as S3
			// keeps a stored one: newer points that do not claim the
			// result do not erase it, in this arrival order either.
			carryHantei(orig, &probe)
			switch {
			case deriveWinnerAfterMerge(&probe, mc):
				if verdictMoved(incoming, &probe) {
					// The verdict moved because of the newer scoring, so it
					// is as new as that scoring (R3 stamps it the same way).
					stamps[state.GroupResult] = newest
				}
				*incoming = probe
			case mh.displaceNewerScoring(orig, mc, stamp):
				// In stamp order the finish came first, and the newer
				// scoring, which leaves the knockout tied, could not be
				// applied after it (R4). The finish stands on the scoreline
				// it was made on, and the newer scoring is moved to the
				// history as held, exactly as it is when it arrives second.
				recomputeModifiedAt = true
			default:
				// The finish's own scoreline is not in the write, so the
				// state it stood on cannot be rebuilt: it is held, and the
				// match stays as stored, its later scoring included.
				mh.holdGroups(needsWinnerReason(mc), state.GroupResult)
				rep.ResultChanged = false
			}
		}
	}

	for _, g := range rep.Applied {
		if !state.GroupDiffers(stored, incoming, g) {
			rep.Unchanged = append(rep.Unchanged, g)
		}
	}
	// A group that moved nothing keeps its stored stamp, but ONLY when the
	// write named no groups at all (Changed is nil: bulk-score, an older
	// client). Such a write cannot tell an echo from a change -- it re-sends
	// every group it carries whether or not the operator touched it -- so
	// letting its echoes take its own stamp would fence out a real, older
	// change made on another device in the gap. A write that DOES name a
	// group named it because it changed against the operator's own screen,
	// so a named echo (the same value sent back, e.g. a replayed decision)
	// is still a real action taken at this write's stamp and must keep that
	// stamp: reverting it is what let a later-arriving change compare
	// itself against a stale, reverted stamp instead of the time the
	// operator actually acted (the regression this guards against; a named
	// write's groups already took `stamp` in the Applied loop above, so
	// here there is nothing to do for it). Delete rather than zero when the
	// stored match never stamped the group at all, so an echoed legacy
	// group keeps reading as never written.
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
	// decision sent again under a later stamp, or a winner worked out again
	// to the same answer) did not move. Recording it again would rewrite the
	// competitor's status (and undo a reinstatement made since), so it has
	// no eligibility consequence.
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
		// A newer stamp left with the scoring it was moved out with:
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
	// differed from stored, or a whole-match echo (bulk-score, an older
	// client) would drag ModifiedAt forward and fence out a real, older
	// change the same way a per-group one would.
	if (incoming.Changed != nil || len(rep.Applied) > len(rep.Unchanged)) && stamp > incoming.ModifiedAt {
		incoming.ModifiedAt = stamp
	}
	incoming.Merge = rep
	return rep
}

// verdictMoved reports whether working the winner out again changed the
// verdict a write stated: its status, decision or winner (by name, and by id
// where the write named one; a payload's omitted id is not a change).
func verdictMoved(said, derived *state.MatchResult) bool {
	return said.Status != derived.Status || said.Decision != derived.Decision || said.Winner != derived.Winner ||
		(said.WinnerID != "" && said.WinnerID != derived.WinnerID)
}

// snapshotScoring copies the groups of m that decide who won (points, flags,
// bouts), deep, so the merge's later copies over m cannot reach it.
func snapshotScoring(m *state.MatchResult) *state.MatchResult {
	return &state.MatchResult{
		IpponsA: cloneIppons(m.IpponsA), IpponsB: cloneIppons(m.IpponsB),
		HansokuA: m.HansokuA, HansokuB: m.HansokuB,
		FlagsA: m.FlagsA, FlagsB: m.FlagsB,
		SubResults: state.CloneSubResults(m.SubResults),
	}
}

func cloneIppons(s []string) []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s...)
}

// carriesGroup reports whether a write's own payload holds a value for
// scoring group g: points it sent, its engi flags, or a row at that bout
// position. A payload that omits a group says nothing about it.
func carriesGroup(w *state.MatchResult, g string) bool {
	switch g {
	case state.GroupPoints:
		return w.IpponsA != nil || w.IpponsB != nil || w.HansokuA != 0 || w.HansokuB != 0
	case state.GroupFlags:
		return true
	}
	if pos, ok := state.ParseBoutGroup(g); ok {
		for i := range w.SubResults {
			if w.SubResults[i].Position == pos {
				return true
			}
		}
	}
	return false
}

// displaceNewerScoring is S2's answer when the scoring stored after a finish
// would leave the match without the winner it needs (R4): in stamp order the
// finish came first and that scoring could not apply after it. Each scoring
// group stored newer than the finish takes the finish's own value (orig) and
// the finish's stamp, and the stored newer value is moved to the history as
// held (MergeReport.Displaced), so the match reaches the state the other
// arrival order gives and nothing is lost. It reports false, changing
// nothing, when the finish does not carry every such group, or when its own
// scoreline does not decide the match either.
func (h *mergeHold) displaceNewerScoring(orig *state.MatchResult, mc mergeCtx, stamp int64) bool {
	var newer []string
	for _, g := range append([]string{state.GroupPoints, state.GroupFlags}, storedBoutGroups(h.stored)...) {
		if h.stored.GroupStamp(g) > stamp && !slices.Contains(newer, g) {
			newer = append(newer, g)
		}
	}
	probe := *h.incoming
	probe.SubResults = state.CloneSubResults(h.incoming.SubResults)
	var moved []string
	for _, g := range newer {
		if !state.GroupDiffers(h.stored, orig, g) {
			continue
		}
		if !carriesGroup(orig, g) {
			return false
		}
		state.CopyGroup(&probe, orig, g)
		moved = append(moved, g)
	}
	if len(moved) == 0 || !deriveWinnerAfterMerge(&probe, mc) {
		return false
	}
	*h.incoming = probe
	byStamp := map[int64]map[string]json.RawMessage{}
	for _, g := range moved {
		at := h.stored.GroupStamp(g)
		if byStamp[at] == nil {
			byStamp[at] = map[string]json.RawMessage{}
		}
		byStamp[at][g] = state.GroupValue(h.stored, g)
		h.stamps[g] = stamp
		// The finish's own value now applies.
		if i := slices.Index(h.rep.Held, g); i >= 0 {
			h.rep.Held = slices.Delete(h.rep.Held, i, i+1)
			delete(h.rep.HeldValues, g)
		}
		if i := slices.Index(h.rep.HeldEcho, g); i >= 0 {
			h.rep.HeldEcho = slices.Delete(h.rep.HeldEcho, i, i+1)
		}
		if !slices.Contains(h.rep.Applied, g) {
			h.rep.Applied = append(h.rep.Applied, g)
		}
	}
	if len(h.rep.HeldValues) == 0 {
		h.rep.HeldValues = nil
	}
	stamps := make([]int64, 0, len(byStamp))
	for at := range byStamp {
		stamps = append(stamps, at)
	}
	slices.Sort(stamps)
	for _, at := range stamps {
		h.rep.Displaced = append(h.rep.Displaced, state.DisplacedChange{
			Stamp: at, Values: byStamp[at], Reason: needsWinnerReason(mc),
		})
	}
	h.rep.NeedsWinner = true
	return true
}

// carryHantei puts a judges'-decision mark from one scoreline onto another
// that carries none, on the same side, through domain.AppendHantei (the one
// placement rule): the judges' decision is the result's, so a points change
// that does not claim the result does not erase it (S3: from the stored
// match; S2: from the finish that arrived after newer points).
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

// mergeHold takes groups the merge had applied back out of a write: each is
// reported held (its incoming value kept for the history), the stored value
// and stamp are put back. The ONE place a group is un-applied, used by R4 and
// S2 (bc-mrgc review).
type mergeHold struct {
	rep          *state.MergeReport
	stored       *state.MatchResult
	incoming     *state.MatchResult
	stamps       map[string]int64
	storedStamps map[string]int64
	// rawValues are a group's incoming value captured before the merge
	// adjusted it (S3's kept hantei), so the history keeps what the writer
	// sent.
	rawValues map[string]json.RawMessage
}

// raw captures group's incoming value before the merge adjusts it.
func (h *mergeHold) raw(group string) {
	if h.rawValues == nil {
		h.rawValues = map[string]json.RawMessage{}
	}
	h.rawValues[group] = state.GroupValue(h.incoming, group)
}

// holdGroups takes each applied group back out, with reason as the history
// entry's reason and NeedsWinner reported to the writer.
func (h *mergeHold) holdGroups(reason string, groups ...string) bool {
	var heldSomething bool
	for _, g := range groups {
		i := slices.Index(h.rep.Applied, g)
		if i < 0 {
			continue
		}
		h.rep.Applied = slices.Delete(h.rep.Applied, i, i+1)
		// An echo (the incoming value equals the stored one, and reportHeld
		// has no raw pre-adjustment value to fall back on) is no loss: it
		// goes to HeldEcho below, and holding nothing real must not report a
		// reason or ask the operator for a correction nothing needed.
		if h.rawValues[g] != nil || state.GroupDiffers(h.stored, h.incoming, g) {
			heldSomething = true
		}
		reportHeld(h.rep, g, h.stored, h.incoming, h.rawValues[g])
		state.CopyGroup(h.incoming, h.stored, g)
		if s, ok := h.storedStamps[g]; ok {
			h.stamps[g] = s
		} else {
			delete(h.stamps, g)
		}
	}
	if !heldSomething {
		return false
	}
	h.rep.HoldReason = reason
	// "Correct the result with a winner" is the answer for the two R4
	// reasons only; a default win that stands asks for no correction.
	// holdDefaultWinScoring sets DefaultWinStands and StandingDecision
	// itself, since the reason text it builds here now names the decision
	// and so can no longer be compared against one fixed constant.
	h.rep.NeedsWinner = reason == HoldReasonKnockoutNeedsWinner || reason == HoldReasonEngiNeedsValidCount
	return true
}

// holdScoring holds every applied group that decides who won.
func (h *mergeHold) holdScoring(reason string) bool {
	var scoring []string
	for _, g := range h.rep.Applied {
		if state.IsScoringGroup(g) {
			scoring = append(scoring, g)
		}
	}
	return h.holdGroups(reason, scoring...)
}

// holdDefaultWinScoring holds every applied group that decides who won
// together with a changed overtime: a default win already decided this
// match, and nothing a board still scoring it sends says otherwise, whether
// that is a point, a bout, or an (E) mark. holdScoring alone would miss the
// overtime group, since IsScoringGroup does not count it as scoring, and a
// held scoreline landing beside an applied (E) would put overtime on a match
// the default win rule says had none (FIK Art. 32: one maru in encho).
func (h *mergeHold) holdDefaultWinScoring(decision string) {
	var groups []string
	for _, g := range h.rep.Applied {
		if state.IsScoringGroup(g) || g == state.GroupEncho {
			groups = append(groups, g)
		}
	}
	if !h.holdGroups(defaultWinStandsReason(decision), groups...) {
		// Nothing of it was genuinely held (every named group was an echo,
		// or there was none to hold at all): the write fully applied, so it
		// gets no reason and names no decision either.
		return
	}
	// The decision that closed the match already has the winner it needs,
	// so the answer sends the operator to the editor's own Remove <decision>
	// instead of asking for a correction with a winner (holdGroups'
	// NeedsWinner stays false for this reason). StandingDecision lets the
	// HTTP layer name the decision on the wire (heldDecision).
	h.rep.DefaultWinStands = true
	h.rep.StandingDecision = decision
}

// needsWinnerReason is the history reason of a change held because the
// match would be left with no winner it must have.
func needsWinnerReason(mc mergeCtx) string {
	if mc.comp != nil && mc.comp.Engi {
		return HoldReasonEngiNeedsValidCount
	}
	return HoldReasonKnockoutNeedsWinner
}

// HoldReasonKnockoutNeedsWinner is the history reason of a change held because
// it would leave a finished knockout match tied (R4, operator ruling
// 2026-10-04: "It needs a winner if the match is finished and is being
// corrected").
const HoldReasonKnockoutNeedsWinner = "a knockout match needs a winner"

// HoldReasonEngiNeedsValidCount is R4's engi twin: an engi result stands only
// on a valid flag count, which a change that leaves an even or incomplete
// count does not give.
const HoldReasonEngiNeedsValidCount = "an engi result needs a valid flag count"

// defaultWinStandsReason builds the history reason for a running board's
// write over a match a decision already closed -- a withdrawal of this
// match (kiken, kiken-injury, fusenpai) or a fusensho awarded for a bar
// recorded on ANOTHER match -- naming that decision itself (e.g. "a kiken
// closed this match") rather than the eliminated "default win" umbrella
// term, since kendo has no shared word for the class and every sentence a
// person reads must name one of the three (operator ruling 2026-10-04).
// Neither a point, a bout, nor an overtime toggle the board sends says the
// decision was wrong, so it stands and the write is kept in the history.
// R2's clear is for a withdrawal of the match itself, and needs an actual
// scoring change (cleared, above) -- an (E) tap alone is not one.
func defaultWinStandsReason(decision string) string {
	return fmt.Sprintf("a %s closed this match", domain.DecisionWord(decision))
}

// HoldReasonFinishAtomic is why a stale Finish is held whole: a write that
// completes the match carries a scoreline its verdict stood on, so when the
// verdict itself is outranked by a newer one (a correction made on another
// device, after it), everything else the stale write changes is kept with
// it, in the history, rather than landing beside a verdict it never
// declared. Mirrors HoldReasonEngiAtomic; a withdrawal has its own atomicity
// rule (R2's withdrawalOutranked) and never reaches this one.
const HoldReasonFinishAtomic = "a finish and the scoreline it stood on are kept together"

// completesMatch reports whether the merged write leaves the match finished
// (an empty status completes a bracket match, effectiveBracketWriteStatus).
func completesMatch(m *state.MatchResult, mc mergeCtx) bool {
	return m.Status == state.MatchStatusCompleted || (mc.knockout && m.Status == "")
}

// newestStoredScoringStamp is the newest stored stamp of a group that decides
// who won: the points, the engi flags, or any bout row (tombstones included).
func newestStoredScoringStamp(stored *state.MatchResult) int64 {
	newest := max(stored.GroupStamp(state.GroupPoints), stored.GroupStamp(state.GroupFlags))
	for _, g := range storedBoutGroups(stored) {
		newest = max(newest, stored.GroupStamp(g))
	}
	return newest
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

// scoringChanged reports whether an applied group that decides who won (the
// points, a bout, the engi flags) now holds a different value from the
// stored one. An echo of the stored scoreline is not a correction.
func scoringChanged(stored, merged *state.MatchResult, applied []string) bool {
	for _, g := range applied {
		if state.IsScoringGroup(g) && state.GroupDiffers(stored, merged, g) {
			return true
		}
	}
	return false
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

// deriveWinnerAfterMerge works the winner out again from a finished match's
// merged scoreline (R3, S2), through the owners every other write uses:
//
//   - engi: the side with more flags (engiWinnerSide's rule), only on a
//     valid flag total; an engi match is never a draw, so any other total
//     decides nothing, whatever the phase;
//   - kachinuki: the deciding bout, deriveKachinukiWinner;
//   - a team match: IV, then PW (state.TeamResultFrom through
//     MatchResult.TeamResult), then the representative bout's winner
//     (deriveDaihyosenWinner);
//   - an individual match: the scoring ippons per side
//     (domain.CountScoringIppons), the hantei mark deciding a tie.
//
// With no winner, a pool or league match becomes a completed draw
// (hikiwake). It reports false, and changes nothing, when the merged
// scoreline cannot give the verdict the match needs: a knockout tie (it
// cannot end tied) or an engi count that is not a valid total. The caller
// then holds the change (R4, operator ruling 2026-10-04); the match never
// goes back to running for it.
func deriveWinnerAfterMerge(m *state.MatchResult, mc mergeCtx) bool {
	side := domain.MatchSideNone
	comp := mc.comp
	kachinukiDecided := false
	switch {
	case comp != nil && comp.Engi:
		// An engi match cannot be drawn (a 3- or 5-referee panel), and its
		// winner is decided only by a valid flag total. A merged count that is
		// not one (a board part-way through its count, an even total) decides
		// nothing, in a pool and a knockout alike.
		if !engiValidTotal(m.FlagsA, m.FlagsB) {
			return false
		}
		switch {
		case m.FlagsA > m.FlagsB:
			side = domain.MatchSideA
		case m.FlagsB > m.FlagsA:
			side = domain.MatchSideB
		}
	case comp != nil && comp.IsKachinuki() && len(m.SubResults) > 0:
		probe := *m
		probe.Winner, probe.Decision = "", string(domain.DecisionKachinukiExhaustion)
		if err := deriveKachinukiWinner(&probe); err == nil {
			side = sideNamed(m, probe.Winner)
		}
		kachinukiDecided = side != domain.MatchSideNone
	case hasTeamBout(m.SubResults):
		if line := m.TeamResult(); line != nil {
			// SideA is Aka, SideB is Shiro (TeamResultLine).
			switch {
			case line.AkaIV > line.ShiroIV:
				side = domain.MatchSideA
			case line.ShiroIV > line.AkaIV:
				side = domain.MatchSideB
			case line.AkaPW > line.ShiroPW:
				side = domain.MatchSideA
			case line.ShiroPW > line.AkaPW:
				side = domain.MatchSideB
			}
		}
		if side == domain.MatchSideNone {
			probe := *m
			probe.Winner = ""
			deriveDaihyosenWinner(&probe)
			side = sideNamed(m, probe.Winner)
		}
	default:
		a, b := domain.CountScoringIppons(m.IpponsA), domain.CountScoringIppons(m.IpponsB)
		switch {
		case a > b:
			side = domain.MatchSideA
		case b > a:
			side = domain.MatchSideB
		case domain.ContainsHantei(m.IpponsA):
			side = domain.MatchSideA
		case domain.ContainsHantei(m.IpponsB):
			side = domain.MatchSideB
		}
	}

	switch side {
	case domain.MatchSideA:
		m.Winner, m.WinnerID, m.WinnerSide = m.SideA, cmp.Or(m.SideAID, mc.storedAID), "A"
	case domain.MatchSideB:
		m.Winner, m.WinnerID, m.WinnerSide = m.SideB, cmp.Or(m.SideBID, mc.storedBID), "B"
	default:
		// R4: a knockout match cannot end tied. Nothing is changed; the
		// caller holds the change that would have tied it.
		if mc.knockout {
			return false
		}
		m.Winner, m.WinnerID, m.WinnerSide = "", "", ""
		m.Status = state.MatchStatusCompleted
		m.Decision = state.DecisionDraw
		return true
	}
	if kachinukiDecided {
		m.Decision = string(domain.DecisionKachinukiExhaustion)
	}
	if m.Decision == state.DecisionDraw {
		m.Decision = ""
	}
	return true
}

// sideNamed maps a winner NAME an owner derived back to the side it names.
func sideNamed(m *state.MatchResult, winner string) domain.MatchSide {
	switch winner {
	case "":
		return domain.MatchSideNone
	case m.SideA:
		return domain.MatchSideA
	case m.SideB:
		return domain.MatchSideB
	}
	return domain.MatchSideNone
}

// hasTeamBout: the bout rows make this a team match whose winner the bouts
// decide: a numbered bout, or the representative bout alone (a team match's
// match-level ippons are empty, so reading them would call it tied).
func hasTeamBout(subs []state.SubMatchResult) bool {
	for i := range subs {
		if subs[i].Position >= 1 {
			return true
		}
	}
	return state.DaihyosenSubIndex(subs) >= 0
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

package engine

import (
	"encoding/json"
	"errors"
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
// Three rules ride on top, each with one home here:
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
//   - R4 (deriveWinnerAfterMerge): when that leaves no winner, a knockout
//     match goes back to running (it cannot end tied) and a pool or league
//     match becomes a completed draw.
//
// matchWriteRestore (the K3 rollback) does not merge: it replays a trusted
// snapshot of every group and its stamps.

// mergeCtx is what the merge needs to know about where the match lives.
type mergeCtx struct {
	// comp decides which owner works a winner out again (engi, kachinuki,
	// team, individual). nil reads as an individual kendo competition.
	comp *state.Competition
	// knockout is true for a bracket match (R4: it cannot end tied).
	knockout bool
	// nilSubsClear says what an incoming nil SubResults means on this branch
	// when the writer stated no groups: the pool's whole-struct write has
	// always replaced the stored bouts with it, the bracket has always kept
	// them. The default keeps both exactly as they were.
	nilSubsClear bool
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
	stamp := incoming.ModifiedAt
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
		if !domain.ApplyByTimestamp(stamp, stored.GroupStamp(g)) {
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

	rep := &state.MergeReport{Stamp: stamp, Changed: changed}
	stamps := state.MaterializedGroupStamps(stored.GroupStamps, stored.ModifiedAt, state.SubPositions(stored.SubResults))
	for _, g := range groups {
		if inChanged[g] && !hold[g] {
			rep.Applied = append(rep.Applied, g)
			if stamp > 0 {
				stamps[g] = stamp
			}
			continue
		}
		if inChanged[g] {
			rep.Held = append(rep.Held, g)
			if rep.HeldValues == nil {
				rep.HeldValues = map[string]json.RawMessage{}
			}
			rep.HeldValues[g] = state.GroupValue(incoming, g)
		}
		state.CopyGroup(incoming, stored, g)
	}
	for _, g := range rep.Applied {
		if !state.GroupDiffers(stored, incoming, g) {
			rep.Unchanged = append(rep.Unchanged, g)
		}
	}
	rep.ResultChanged = inChanged[state.GroupResult] && !hold[state.GroupResult]

	// R2 and R3: a scoring change applied to a finished match.
	if finished && scoringChanged(stored, incoming, rep.Applied) {
		if domain.IsDefaultWinDecisionStr(incoming.Decision) {
			rep.ClearedWithdrawal = state.GroupValue(incoming, state.GroupResult)
			incoming.Decision, incoming.DecisionBy, incoming.DecisionReason = "", "", ""
			incoming.Winner, incoming.WinnerID, incoming.WinnerSide = "", "", ""
			// The scoring change replaces the ruling: KeepsWithdrawalRuling
			// must not reinstate it from the stored match.
			incoming.ClearsWithdrawal = true
		}
		deriveWinnerAfterMerge(incoming, mc)
		if state.GroupDiffers(stored, incoming, state.GroupResult) {
			rep.ResultChanged = true
			if stamp > 0 {
				stamps[state.GroupResult] = stamp
			}
		}
	}

	if len(stamps) == 0 {
		stamps = nil
	}
	incoming.GroupStamps = stamps
	incoming.ModifiedAt = stored.ModifiedAt
	if len(rep.Applied) > 0 && stamp > incoming.ModifiedAt {
		incoming.ModifiedAt = stamp
	}
	incoming.Merge = rep
	return rep
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
// on it), but it must not be invisible. Quick-score builds its write with no
// stamp by design, so every correction made through it logs here. RUNNING
// writes are excluded by volume: a legacy SPA autosaving on the debounce
// would log once per keystroke and bury the terminal line that answers
// "where did the result go", and the stored stamp survives an unstamped
// write, so the completed write that follows still logs.
func logUnstampedOverwrite(result *state.MatchResult, storedModifiedAt int64) {
	if result.ModifiedAt == 0 && storedModifiedAt > 0 && result.Status != state.MatchStatusRunning {
		log.Printf("engine: match %s: unstamped write overwrites a result stamped %d (unstamped bypass, no last-write-wins comparison possible)",
			result.ID, storedModifiedAt)
	}
}

// deriveWinnerAfterMerge works the winner out again from a finished match's
// merged scoreline (R3), through the owners every other write uses:
//
//   - engi: the side with more flags (engiWinnerSide's rule);
//   - kachinuki: the deciding bout, deriveKachinukiWinner;
//   - a team match: IV, then PW (state.TeamResultFrom through
//     MatchResult.TeamResult), then the representative bout's winner
//     (deriveDaihyosenWinner);
//   - an individual match: the scoring ippons per side
//     (domain.CountScoringIppons), the hantei mark deciding a tie.
//
// R4: with no winner, a knockout match goes back to running (it cannot end
// tied) and a pool or league match becomes a completed draw (hikiwake).
func deriveWinnerAfterMerge(m *state.MatchResult, mc mergeCtx) {
	side := domain.MatchSideNone
	comp := mc.comp
	switch {
	case comp != nil && comp.Engi:
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
		if side != domain.MatchSideNone {
			m.Decision = string(domain.DecisionKachinukiExhaustion)
		}
	case hasNumberedBout(m.SubResults):
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
		m.Winner, m.WinnerID, m.WinnerSide = m.SideA, m.SideAID, "A"
	case domain.MatchSideB:
		m.Winner, m.WinnerID, m.WinnerSide = m.SideB, m.SideBID, "B"
	default:
		m.Winner, m.WinnerID, m.WinnerSide = "", "", ""
		if mc.knockout {
			m.Status = state.MatchStatusRunning
			m.Decision = ""
		} else {
			m.Status = state.MatchStatusCompleted
			m.Decision = state.DecisionDraw
		}
		return
	}
	if m.Decision == state.DecisionDraw {
		m.Decision = ""
	}
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

func hasNumberedBout(subs []state.SubMatchResult) bool {
	for i := range subs {
		if subs[i].Position >= 1 {
			return true
		}
	}
	return false
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

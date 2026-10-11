package engine

import (
	"errors"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrNotKachinuki is returned by KachinukiRoster for a competition whose team
// matches are not fought winner-stays-on: only a kachinuki encounter has a
// queue of fighters to read.
var ErrNotKachinuki = errors.New("not a kachinuki competition")

// KachinukiRosterFighter is one fighter of a side's queue: the member id when
// the lineup position or bout row carries one, and the name it holds (empty
// for a member picked by number and not named yet). The client resolves the
// fighter's current name and team-member number from its own member list.
type KachinukiRosterFighter struct {
	Name     string `json:"name"`
	MemberID string `json:"memberId,omitempty"`
}

// KachinukiRosterSide is one side's queue, split by the server so the client
// never has to decide which queue entry is the fighter on the court.
//
// On is the side's fighter in the live bout (the last numbered bout row), as
// that row names them; nil when the row names nobody for the side, when there
// is no bout row yet, or when Record bout has recorded that row as a WIN that
// retires this fighter (see KachinukiRoster). A recorded draw that is still
// the last bout keeps both its fighters on, in every phase.
//
// Remaining is the queue the advance takes this side's next fighter from if
// the fighter on loses: every fighter of the lineup in force who has not
// retired, the fighter on taken out by the same identity rule
// (IsMemberRetired), in queue order, so index 0 is next.
//
// LineupFound is false when no lineup is in force for the side's team at this
// match, or when the one in force fields nobody (a match's own empty lineup,
// positions: {}, shows none). Remaining is then EMPTY, because the only other
// source, the bout log, knows only the fighters already seen and any count
// taken from it would be false; On is still given.
//
// Exhausted is the advance's own verdict that the side has nobody left: the
// last numbered bout is RECORDED, it was a win (the advance's MatchEnded:
// the loser's queue is empty, so Record bout appended nothing after it),
// this side lost it, and this side's queue came from a lineup (LineupFound).
// Without a lineup the advance's queue is the bout log's, which knows only
// the fighters already seen and so empties after any loss: Exhausted is then
// false, and the bout stays live as it always did. A recorded draw exhausts
// nobody (both fighters stay on; the advance reports BothExhausted, never
// MatchEnded), and an unrecorded last bout is no result, so it exhausts
// nobody either. At most one side is exhausted.
type KachinukiRosterSide struct {
	LineupFound bool                     `json:"lineupFound"`
	On          *KachinukiRosterFighter  `json:"on"`
	Remaining   []KachinukiRosterFighter `json:"remaining"`
	Exhausted   bool                     `json:"exhausted"`
}

// KachinukiRoster is the advisory read of a kachinuki encounter's two queues.
// SideA and SideB are the match's sides as stored (SideA is Aka on the sheet).
//
// NextPairing is whether Record bout, tapped now on the match as stored,
// would append a pairing: the very decision MaybeAdvanceKachinuki makes
// (decideKachinukiAdvance), asked without writing. On a RECORDED last bout it
// says whether a second tap could append anything (false: Record bout writes
// nothing, a lineup saved since may make it true). On an unrecorded last bout
// it reads the row's stored outcome as Record bout would (the running
// autosave's winner at a one-point lead counts), so it is current only as of
// the read. False for a completed match, a log with no numbered bout, a last
// bout with no outcome or no fighter, and an outcome the advance cannot read.
type KachinukiRoster struct {
	SideA       KachinukiRosterSide `json:"sideA"`
	SideB       KachinukiRosterSide `json:"sideB"`
	NextPairing bool                `json:"nextPairing"`
}

// KachinukiRoster answers, for a kachinuki team match, who each side has on
// the court and who it has left. It is ADVISORY ONLY (operator ruling
// 2026-09-24): kachinuki is operator-led, team sizes are unregulated, and a
// side that reads as exhausted may still field fighters the app has never
// seen, so nothing ends the encounter or arms End match on this answer. The
// sheet reads two verdicts from it, both the advance's own: NextPairing (Record
// bout on a recorded bout would write nothing, so the button is disabled) and
// Exhausted (a recorded win left that side's lineup with nobody, so the bout
// is shown closed).
//
// Every numbered bout before the last retires by its result, as the advance
// reads it (RetiredPlayersFromBoutLog: the attribution owners, id first, and
// state.IsDraw). The last numbered bout is the one being fought: both its
// fighters are on whatever its score, because the running autosave writes a
// one-point lead's winner and a 1-1's hikiwake on that row and neither is a
// result (bc-kfnl, operator decision 2026-10-11). Only once Record bout has
// marked it recorded (state.SubMatchResult.Recorded, which the server alone
// writes) is its result read the same way: the loser is no longer on, the
// winner stays on. The mark is stored, so a fresh
// sheet, a second device and the public sheet read the same line. When a draw
// leaves one side with nobody, the advance appends a pairing that keeps that
// side's fighter (advanceAfterHikiwake), which is then the last bout and on.
// A recorded draw that is still the last bout keeps both its fighters on, in
// every phase: nothing followed it because both sides had nobody left, and
// whether that pair fights on in encho is the operator's call (a write that
// starts the encho takes the mark off); End match decides how it ends.
//
// The queues come from kachinukiRosterQueues and the retirements from
// RetiredPlayersFromBoutLog, as the advance (MaybeAdvanceKachinuki) takes
// them, and Remaining is the advance's remaining list with the fighter on
// retired too: the fighter it names as next is the one the advance appends
// when the fighter on loses. It writes nothing. A lineups.yaml that cannot be
// read is an error here (the advance logs it and reads no lineup).
func (e *Engine) KachinukiRoster(compID, matchID string) (KachinukiRoster, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if comp == nil {
		return KachinukiRoster{}, notFoundErrorf("competition %s not found", compID)
	}
	if !comp.IsKachinuki() {
		return KachinukiRoster{}, ErrNotKachinuki
	}
	located, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if located == nil || located.Result == nil {
		return KachinukiRoster{}, notFoundErrorf("match %s not found", matchID)
	}
	parent := located.Result
	// The queues are built inside the read, and hold their own copies of the
	// names and ids they take from the lineups.
	var queueA, queueB kachinukiSideQueue
	// The advance's own decision, asked quietly (no breadcrumb: nothing is
	// tapped), on the same rule: the read's verdict is what Record bout does.
	var verdict kachinukiAdvanceVerdict
	var decided bool
	if err := e.readLineupRule(compID, comp.IsKnockoutEnabled(), located.PoolMatches, located.Bracket, func(rule *lineupRule) {
		queueA, queueB = kachinukiRosterQueues(comp, parent, rule)
		verdict, decided = decideKachinukiAdvance(comp, parent, rule, true)
	}); err != nil {
		return KachinukiRoster{}, err
	}
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)

	// The last numbered bout. Its fighters are on while it is being fought,
	// whatever its score: the running autosave writes a winner at a one-point
	// lead and a hikiwake at 1-1, and neither is a result (bc-kfnl, operator
	// decision 2026-10-11). Once Record bout has marked it recorded
	// (SubMatchResult.Recorded) its result says who is no longer on
	// (liveRetiredA/B, from that row alone), as the advance reads it. Every
	// bout before it retires by its result in retiredA/B; the last row's own
	// retirement there changes nothing below, because the fighter on is taken
	// out of the queue either way (retired.with(on)).
	var onA, onB kachinukiFighter
	liveRetiredA, liveRetiredB := newRetiredMemberSet(), newRetiredMemberSet()
	if i := lastNumberedBout(parent.SubResults); i >= 0 {
		live := parent.SubResults[i]
		onA = kachinukiFighter{Name: live.SideA, MemberID: live.SideAMemberID}
		onB = kachinukiFighter{Name: live.SideB, MemberID: live.SideBMemberID}
		// A recorded DRAW that is still the last bout keeps both its fighters
		// on, in every phase: the advance appended nothing after it, which
		// after a draw means both sides had nobody left (a draw leaving one
		// side empty appends the pairing that keeps that side's fighter,
		// advanceAfterHikiwake). Whether that pair fights on in encho is the
		// operator's call on any bout in any phase, and an encho tap clears
		// the mark (mergeHold.unrecordReopenedBouts); End match decides the rest (a drawn
		// encounter in a pool or league, blocked in a knockout). So the line
		// never reads "no fighters left" while that bout can still be fought
		// on. Only a recorded WIN takes a fighter off here.
		if live.Recorded && !state.IsDraw(live.Decision) {
			liveRetiredA, liveRetiredB = RetiredPlayersFromBoutLog(parent.SubResults[i:i+1], parent.SideA, parent.SideB)
		}
	}
	out := KachinukiRoster{
		SideA:       kachinukiRosterSide(queueA, retiredA, onA, liveRetiredA),
		SideB:       kachinukiRosterSide(queueB, retiredB, onB, liveRetiredB),
		NextPairing: decided && verdict.out.Next != nil,
	}
	// Exhausted: a recorded win whose loser the advance found with nobody
	// left (MatchEnded), on a lineup's queue only (see KachinukiRosterSide).
	if decided && verdict.last.Recorded && verdict.out.MatchEnded {
		switch verdict.out.WinningSide {
		case "A":
			out.SideB.Exhausted = verdict.lineupB
		case "B":
			out.SideA.Exhausted = verdict.lineupA
		}
	}
	return out, nil
}

// kachinukiRosterSide shapes one side's answer (see KachinukiRosterSide). on
// is the live bout row's fighter (zero when there is none); liveRetired holds
// whom that bout's RECORDED outcome retired (empty while it is live). The list
// is never nil, so the wire carries [] rather than null.
func kachinukiRosterSide(queue kachinukiSideQueue, retired RetiredMemberSet, on kachinukiFighter, liveRetired RetiredMemberSet) KachinukiRosterSide {
	out := KachinukiRosterSide{
		LineupFound: queue.fromLineup(),
		Remaining:   []KachinukiRosterFighter{},
	}
	onKnown := kachinukiFighterKnown(on)
	if onKnown && !IsMemberRetired(on, liveRetired, nil) {
		f := KachinukiRosterFighter(on)
		out.On = &f
	}
	if !out.LineupFound {
		return out
	}
	// The advance's remaining list once the fighter on has retired: the same
	// queue, the same filter, the retired set with the fighter on added.
	if onKnown {
		retired = retired.with(on)
	}
	for _, f := range queue.remaining(retired) {
		out.Remaining = append(out.Remaining, KachinukiRosterFighter(f))
	}
	return out
}

// with returns a copy of r with f retired as well, keyed both ways exactly as
// a bout row retires its fighter (retire), so IsMemberRetired settles f
// against the queue by its own id-then-unambiguous-name tiers.
func (r RetiredMemberSet) with(f kachinukiFighter) RetiredMemberSet {
	out := newRetiredMemberSet()
	for id := range r.IDs {
		out.IDs[id] = struct{}{}
	}
	for name := range r.Names {
		out.Names[name] = struct{}{}
	}
	out.retire(f.Name, f.MemberID)
	return out
}

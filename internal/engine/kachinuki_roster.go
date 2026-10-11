package engine

import (
	"errors"
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
// is no bout row yet, or when the live bout was RECORDED (KachinukiRoster's
// recordedThrough) and its outcome retired this fighter.
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
type KachinukiRosterSide struct {
	LineupFound bool                     `json:"lineupFound"`
	On          *KachinukiRosterFighter  `json:"on"`
	Remaining   []KachinukiRosterFighter `json:"remaining"`
}

// KachinukiRoster is the advisory read of a kachinuki encounter's two queues.
// SideA and SideB are the match's sides as stored (SideA is Aka on the sheet).
type KachinukiRoster struct {
	SideA KachinukiRosterSide `json:"sideA"`
	SideB KachinukiRosterSide `json:"sideB"`
}

// KachinukiRoster answers, for a kachinuki team match, who each side has on
// the court and who it has left. It is ADVISORY ONLY (operator ruling
// 2026-09-24): kachinuki is operator-led, team sizes are unregulated, and a
// side that reads as exhausted may still field fighters the app has never
// seen, so nothing ends the encounter, arms End match or holds a button back
// on this answer.
//
// recordedThrough is the highest bout position the caller knows was RECORDED
// (Record bout), 0 for none. The stored data cannot tell a recorded bout from
// one still being fought (a 1-0 lead already sets the bout's winner), and
// Record bout appends nothing when a side has nobody left, so only the caller
// can say the live bout is over; then a fighter its outcome retired is no
// longer on.
//
// The queues come from kachinukiRosterQueues and the retirements from
// RetiredPlayersFromBoutLog, as the advance (MaybeAdvanceKachinuki) takes
// them, and Remaining is the advance's remaining list with the fighter on
// retired too: the fighter it names as next is the one the advance appends
// when the fighter on loses. It writes nothing. A lineups.yaml that cannot be
// read is an error here (the advance logs it and reads no lineup).
func (e *Engine) KachinukiRoster(compID, matchID string, recordedThrough int) (KachinukiRoster, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if !comp.IsKachinuki() {
		return KachinukiRoster{}, ErrNotKachinuki
	}
	located, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return KachinukiRoster{}, err
	}
	if located == nil || located.Result == nil {
		return KachinukiRoster{}, ErrMatchNotFound
	}
	rule, err := e.lineupRuleOver(compID, comp.IsKnockoutEnabled(), located.PoolMatches, located.Bracket)
	if err != nil {
		return KachinukiRoster{}, err
	}
	parent := located.Result
	queueA, queueB := kachinukiRosterQueues(comp, parent, rule)
	retiredA, retiredB := RetiredPlayersFromBoutLog(parent.SubResults, parent.SideA, parent.SideB)

	// The live bout, and whether the caller saw it recorded: then its own
	// outcome says who is no longer on (liveRetiredA/B, from that row alone).
	var onA, onB kachinukiFighter
	liveRetiredA, liveRetiredB := newRetiredMemberSet(), newRetiredMemberSet()
	if i := lastNumberedBout(parent.SubResults); i >= 0 {
		live := parent.SubResults[i]
		onA = kachinukiFighter{Name: live.SideA, MemberID: live.SideAMemberID}
		onB = kachinukiFighter{Name: live.SideB, MemberID: live.SideBMemberID}
		if recordedThrough > 0 && recordedThrough >= live.Position {
			liveRetiredA, liveRetiredB = RetiredPlayersFromBoutLog(parent.SubResults[i:i+1], parent.SideA, parent.SideB)
		}
	}
	return KachinukiRoster{
		SideA: kachinukiRosterSide(queueA, retiredA, onA, liveRetiredA),
		SideB: kachinukiRosterSide(queueB, retiredB, onB, liveRetiredB),
	}, nil
}

// kachinukiRosterSide shapes one side's answer (see KachinukiRosterSide). on
// is the live bout row's fighter (zero when there is none); liveRetired holds
// whom that bout's RECORDED outcome retired (empty while it is live). The list
// is never nil, so the wire carries [] rather than null.
func kachinukiRosterSide(queue kachinukiSideQueue, retired RetiredMemberSet, on kachinukiFighter, liveRetired RetiredMemberSet) KachinukiRosterSide {
	out := KachinukiRosterSide{
		LineupFound: queue.found && len(queue.fighters) > 0,
		Remaining:   []KachinukiRosterFighter{},
	}
	onKnown := on.Name != "" || on.MemberID != ""
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

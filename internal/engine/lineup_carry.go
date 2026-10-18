package engine

// The carry rule by stamp (operator decision L1). A match's own lineup that a
// stamped partial save made (TeamLineup.CopiedAt), and the lineup a kachinuki
// match started with (its pin, TeamLineup.StartedAt), both hold positions
// CARRIED from an earlier lineup: every position of a pin, and every position
// of a match's own lineup its own saves did not change (TeamLineup.Own). A
// carried position is the carry rule AT that lineup's stamp, read by stamp:
// the lineup the match would have carried then (an earlier match's own lineup
// counts only once it was made, CopiedAt before the stamp), and the value each
// of its positions held then (TeamLineup.HeldAt, over its History). So a
// match's own lineup is the same whatever order the saves arrive in, a change
// made before it that arrives later is worked into it, and a change made after
// it never is.
//
// lineupCarry is the ONE owner: the base a match's first partial save lands on
// (LandMatchLineupSave), the pin taken at a kachinuki start (pinLineup), and
// the follow after every lineup save (FollowLineupSave), which works every
// carried lineup of the team out again in match order, so a copy that changes
// is read by the ones after it.
//
// A change sent with no stamp (an older build's) takes its arrival as its
// stamp: the lineup handler stamps it with the server's time as it lands, so
// it is ordered like any other, and a match's first such save makes a copy
// at that time. An older build's whole save (no `changed`) is a change of
// every position at its arrival (lineupSave.lineupToStore), so it reaches no
// copy or pin made before it.

import (
	"cmp"
	"fmt"
	"log"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// lineupCarry holds a competition's lineups, by store key, and its draw.
type lineupCarry struct {
	lineups  map[string]domain.TeamLineup
	draw     lineupDraw
	teamSize int
}

func newLineupCarry(lineups map[string]domain.TeamLineup, pool []state.MatchResult, bracket *state.Bracket, knockout bool, teamSize int) *lineupCarry {
	return &lineupCarry{
		lineups:  lineups,
		draw:     newLineupDraw(state.TeamMatches(state.DrawMatchesFrom(pool, bracket), knockout)),
		teamSize: teamSize,
	}
}

// carryDepthMax stops a carry read on a malformed file whose lineups would
// carry from one another in a circle (match order forbids it).
const carryDepthMax = 64

// sourceAt is the lineup the carry rule gives team at matchID at stamp at:
// the match's own lineup left out, and an earlier match's own lineup that was
// not yet made then (a carried copy with CopiedAt at or after at) as well.
func (lc *lineupCarry) sourceAt(teamID, matchID string, at int64) InForceLineup {
	// slot names the lineup a record is of: a match's own, or a round's.
	slot := func(l domain.TeamLineup) string {
		if l.MatchID != "" {
			return "m\x00" + l.MatchID
		}
		return "r\x00" + strconv.Itoa(l.Round)
	}
	// made: a match's carried copy counts only once it was made.
	made := func(l domain.TeamLineup) bool { return !l.IsCarriedCopy() || l.CopiedAt < at }
	// A lineup taken away (RemovedAt) stands for its match, or for the
	// starting lineup, at the stamps it was in force: made before at, removed
	// at or after it. A removal made after at never reaches what at reads. Of
	// two such records, the one removed first after at was in force then.
	removed := map[string]domain.TeamLineup{}
	for _, l := range lc.lineups {
		if l.TeamID != teamID || !l.IsRemoved() || l.RemovedAt < at || (l.MatchID != "" && l.MatchID == matchID) || !made(l) {
			continue
		}
		s := slot(l)
		if prev, ok := removed[s]; ok && prev.RemovedAt <= l.RemovedAt {
			continue
		}
		removed[s] = l
	}
	filtered := make(map[string]domain.TeamLineup, len(lc.lineups))
	for k, l := range lc.lineups {
		if l.IsRemoved() {
			continue
		}
		if l.TeamID == teamID {
			if l.MatchID != "" && (l.MatchID == matchID || !made(l)) {
				continue
			}
			if _, ok := removed[slot(l)]; ok {
				continue
			}
		}
		filtered[k] = l
	}
	for s, l := range removed {
		l.RemovedAt = 0
		filtered["removed\x00"+s] = l
	}
	return (&lineupRule{lineups: filtered, draw: lc.draw}).inForce(teamID, matchID)
}

// valueAt is what position p of l held at stamp at.
func (lc *lineupCarry) valueAt(l domain.TeamLineup, p domain.Position, at int64, depth int) (domain.PositionChange, bool) {
	held, ok := l.HeldAt(p, at)
	if l.IsCarriedCopy() && (!ok || !slices.Contains(l.Own, p)) {
		// Carried, or own only from a change made after at: what l carried.
		if depth >= carryDepthMax {
			return domain.PositionChange{}, false
		}
		src := lc.sourceAt(l.TeamID, l.MatchID, l.CopiedAt)
		if !src.Found {
			return domain.PositionChange{}, true
		}
		return lc.valueAt(src.Lineup, p, l.CopiedAt, depth+1)
	}
	// Not ok: nothing this lineup holds was set before at, so it held nothing
	// there (carryInto reads it empty), and a value set later is never taken
	// for it.
	return held, ok
}

// teamPositions is every position any lineup of the team, or l, names.
func (lc *lineupCarry) teamPositions(teamID string, l domain.TeamLineup) []domain.Position {
	keys := map[domain.Position]bool{}
	add := func(x domain.TeamLineup) {
		for p := range x.Positions {
			keys[p] = true
		}
		for p := range x.MemberIDs {
			keys[p] = true
		}
		for p := range x.History {
			keys[p] = true
		}
	}
	for _, x := range lc.lineups {
		if x.TeamID == teamID {
			add(x)
		}
	}
	add(l)
	return slices.SortedFunc(maps.Keys(keys), domain.ComparePositions)
}

// carryInto sets every position of l but its own (none on a pin) to the
// carry rule at stamp at, and answers the lineup it was carried from ("" for
// the starting lineup). A member id an own position already holds is not
// carried beside it (the name is), so the lineup never places one member twice.
func (lc *lineupCarry) carryInto(l *domain.TeamLineup, at int64) string {
	src := lc.sourceAt(l.TeamID, l.MatchID, at)
	own := map[domain.Position]bool{}
	seen := map[string]bool{}
	if !l.IsStartPin() {
		own = l.OwnSet()
		for p := range own {
			if id := l.MemberIDs[p]; id != "" {
				seen[id] = true
			}
		}
	}
	if l.Positions == nil {
		l.Positions = map[domain.Position]string{}
	}
	for _, p := range lc.teamPositions(l.TeamID, *l) {
		if own[p] {
			continue
		}
		delete(l.Positions, p)
		delete(l.MemberIDs, p)
		delete(l.Stamps, p)
		if (domain.TeamLineup{Positions: map[domain.Position]string{p: ""}}).ValidatePositions(lc.teamSize) != nil {
			continue
		}
		if !src.Found {
			continue
		}
		// Not known: the source never held the position before at (every
		// change keeps the value it replaced in History), so it was empty
		// then. The value the lineup stores is never kept: it may be one
		// carried at a later stamp (a copy whose stamp moved back, a pin made
		// a copy), which at never saw.
		v, known := lc.valueAt(src.Lineup, p, at, 0)
		if !known || (v.Name == "" && v.MemberID == "") {
			continue
		}
		l.Positions[p] = v.Name
		if v.MemberID != "" && !seen[v.MemberID] {
			seen[v.MemberID] = true
			if l.MemberIDs == nil {
				l.MemberIDs = map[domain.Position]string{}
			}
			l.MemberIDs[p] = v.MemberID
		}
		if v.Stamp > 0 {
			if l.Stamps == nil {
				l.Stamps = map[domain.Position]int64{}
			}
			l.Stamps[p] = v.Stamp
		}
	}
	if len(l.MemberIDs) == 0 {
		l.MemberIDs = nil
	}
	if len(l.Stamps) == 0 {
		l.Stamps = nil
	}
	if !src.Found {
		return ""
	}
	return src.Source.MatchID
}

// rederive works out again every carried lineup of the team (its copies, and
// its pins), in match order, so each reads the ones before it as they now
// are, and answers the store keys of the ones that changed.
func (lc *lineupCarry) rederive(teamID string, frozen map[string]bool) []string {
	var keys []string
	for k, l := range lc.lineups {
		// A removed record is what it was when taken away: only its stamps'
		// reads use it, through sourceAt, which works its carry out again.
		// A frozen one stays as stored (FollowLineupSave).
		if l.TeamID != teamID || l.IsRemoved() || (!l.IsCarriedCopy() && !l.IsStartPin()) || frozen[k] {
			continue
		}
		if _, ok := lc.draw[l.MatchID]; !ok {
			continue
		}
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(a, b string) int {
		if c := lc.draw[lc.lineups[a].MatchID].Place.Compare(lc.draw[lc.lineups[b].MatchID].Place); c != 0 {
			return c
		}
		return cmp.Compare(a, b)
	})
	var moved []string
	for _, k := range keys {
		l := lc.lineups[k]
		next := l.Clone()
		if next.IsStartPin() {
			next.StartedFrom = lc.carryInto(&next, next.StartedAt)
		} else {
			next.CopiedFrom = lc.carryInto(&next, next.CopiedAt)
		}
		if sameCarried(l, next) {
			continue
		}
		lc.lineups[k] = next
		moved = append(moved, k)
	}
	return moved
}

// sameCarried says a re-derivation changed nothing of l.
func sameCarried(a, b domain.TeamLineup) bool {
	return maps.Equal(a.Positions, b.Positions) && maps.Equal(a.MemberIDs, b.MemberIDs) &&
		maps.Equal(a.Stamps, b.Stamps) && a.StartedFrom == b.StartedFrom && a.CopiedFrom == b.CopiedFrom
}

// LandedLineup is what a lineup save left in its slot (a team's starting
// lineup, or a match's own lineup): every record of the slot, to store whole
// (state.StoreTx.ReplaceTeamLineupSlot), and the lineup in force there now,
// the live one (Live false when the slot holds none: every change of the save
// was made before a removal of the slot stamped after it).
type LandedLineup struct {
	Records []domain.TeamLineup
	Lineup  domain.TeamLineup
	Live    bool
}

// slotRecords is every record of one lineup slot in lineups (the live one and
// the removed ones), cloned, in a fixed order.
func slotRecords(lineups map[string]domain.TeamLineup, teamID, matchID string) []domain.TeamLineup {
	var out []domain.TeamLineup
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == matchID && (matchID != "" || l.Round == 0) {
			out = append(out, l.Clone())
		}
	}
	slices.SortFunc(out, func(a, b domain.TeamLineup) int { return cmp.Compare(a.RemovedAt, b.RemovedAt) })
	return out
}

// landEpochs lands a save's changed positions on the records of one slot,
// each on the epoch its stamp falls in (domain.SlotEpochAt, stage 6 review 8,
// m1): a change made before a removal of the slot stamped after it belongs to
// the lineup that removal took away, whenever either arrives, so it is landed
// on that removed record, and only a change made after every removal lands on
// the live lineup. land builds the epoch's record from its base (found false:
// the epoch holds no lineup, a removal marker or no live one) and the
// positions landing there, with the earliest stamp among them.
func landEpochs(records []domain.TeamLineup, changed []domain.Position, stamps map[domain.Position]int64, now int64,
	land func(base domain.TeamLineup, found bool, ps []domain.Position, first int64) (domain.TeamLineup, error)) (LandedLineup, error) {
	stampOf := func(p domain.Position) int64 {
		if s := min(stamps[p], now); s > 0 {
			return s
		}
		// Every change carries a stamp: the handler stamps an unstamped one
		// (an older build's) with the server's time as it lands.
		return now
	}
	live := -1
	for i, r := range records {
		if !r.IsRemoved() {
			live = i
		}
	}
	groups := map[int][]domain.Position{}
	var order []int
	for _, p := range changed {
		e := domain.SlotEpochAt(records, stampOf(p))
		if e < 0 {
			e = live
		}
		if _, ok := groups[e]; !ok {
			order = append(order, e)
		}
		groups[e] = append(groups[e], p)
	}
	for _, e := range order {
		ps := groups[e]
		first := now
		for _, p := range ps {
			first = min(first, stampOf(p))
		}
		var base domain.TeamLineup
		found := false
		if e >= 0 {
			base, found = records[e], !records[e].IsRemovalMarker()
		}
		out, err := land(base, found, ps, first)
		if err != nil {
			return LandedLineup{}, err
		}
		if e >= 0 {
			// A removed record keeps what a carry reads of it, its own
			// positions alone (m3); the live one is stored whole, carried
			// positions included, so it never depends on a follow putting
			// them back (stage 6 review 9, G6).
			out.RemovedAt = records[e].RemovedAt
			if out.IsRemoved() {
				out = out.OwnPositionsOnly()
			}
			records[e] = out
		} else {
			live = len(records)
			records = append(records, out)
		}
	}
	res := LandedLineup{Records: records}
	if live >= 0 {
		res.Lineup, res.Live = records[live], true
	}
	return res, nil
}

// LandStartingLineupSave lands a stamped partial save on a team's starting
// lineup (its round-0 entry; empty when it has none): the position holds its
// latest change by stamp (domain.TeamLineup.ApplyStampedChanges), and its
// History keeps every value it held, with the stamp it was set at, with no
// bound by count (stage 6 review 8, m2: a bound made a copy whose first save
// arrived after many changes read the oldest value kept). A change made
// before a removal of the starting lineup stamped after it lands on the
// lineup that removal took away (landEpochs, m1).
func LandStartingLineupSave(lineups map[string]domain.TeamLineup, teamID string,
	changed []domain.Position, positions, memberIDs map[domain.Position]string, stamps map[domain.Position]int64, now int64) (LandedLineup, error) {
	return landEpochs(slotRecords(lineups, teamID, ""), changed, stamps, now,
		func(base domain.TeamLineup, found bool, ps []domain.Position, _ int64) (domain.TeamLineup, error) {
			if !found {
				base = domain.TeamLineup{TeamID: teamID, Positions: map[domain.Position]string{}}
			}
			out, err := base.ApplyStampedChanges(ps, positions, memberIDs, stamps, now)
			out.TeamID, out.MatchID, out.Round = teamID, "", 0
			return out, err
		})
}

// LandMatchLineupSave is what a partial save of matchID's own lineup for
// teamID leaves (the lineup handler's, under the competition's lock, over what
// its transaction loaded). The first stamped save of a match's own lineup
// makes a carried copy: its other positions are the carry rule at its
// earliest change's stamp (lineupCarry). A save over a carried copy joins its
// own positions, and one made before its earliest change moves that point
// back, the carried positions being worked out again there. A save over the
// lineup a kachinuki match started with turns it into the operator's own,
// carried from the same point. A change sent with no stamp (an older build's)
// comes stamped with the server's time as it landed (lineupSave.landPartial),
// so a first such save makes a copy at its arrival like any other. A change
// made before a removal of the match's own lineup stamped after it lands on
// the lineup that removal took away (landEpochs, m1).
func LandMatchLineupSave(lineups map[string]domain.TeamLineup, pool []state.MatchResult, bracket *state.Bracket, knockout bool, teamSize int,
	teamID, matchID string, changed []domain.Position, positions, memberIDs map[domain.Position]string, stamps map[domain.Position]int64, now int64) (LandedLineup, error) {
	lc := newLineupCarry(lineups, pool, bracket, knockout, teamSize)
	return landEpochs(slotRecords(lineups, teamID, matchID), changed, stamps, now,
		func(base domain.TeamLineup, found bool, ps []domain.Position, first int64) (domain.TeamLineup, error) {
			switch {
			case found && base.IsStartPin():
				base.CopiedAt, base.CopiedFrom, base.Own = base.StartedAt, base.StartedFrom, nil
				base.StartedAt, base.StartedFrom = 0, ""
			case !found:
				base = domain.TeamLineup{TeamID: teamID, MatchID: matchID, Positions: map[domain.Position]string{}, CopiedAt: first}
				base.CopiedFrom = lc.carryInto(&base, first)
			}
			out, err := base.ApplyStampedChanges(ps, positions, memberIDs, stamps, now)
			if err != nil {
				return domain.TeamLineup{}, err
			}
			if out.IsCarriedCopy() {
				out.CopiedFrom = lc.carryInto(&out, out.CopiedAt)
			}
			out.TeamID, out.MatchID, out.Round = teamID, matchID, 0
			return out, nil
		})
}

// FollowLineupSave works every carried lineup of the saved lineup's team out
// again (lineupCarry.rederive), in the save's transaction, and stores the ones
// that changed: a copy or a pin reads a change made before its stamp whenever
// it arrives. It answers the matches whose lineup changed. A copy or a pin the
// store cannot stage is logged and left as it was.
func (e *Engine) FollowLineupSave(h state.StoreTx, comp *state.Competition, saved domain.TeamLineup, changed []domain.Position) ([]string, error) {
	if comp == nil || (saved.MatchID == "" && saved.Round != 0) {
		return nil, nil
	}
	lineups, err := h.LoadTeamLineupsWithRemoved(comp.ID)
	if err != nil {
		return nil, err
	}
	pool, err := h.LoadPoolMatches(comp.ID)
	if err != nil {
		return nil, err
	}
	bracket, err := h.LoadBracket(comp.ID)
	if err != nil {
		return nil, err
	}
	// A carried lineup the domain refuses as worked out (a position the
	// competition's team size has no place for, kept from older data) is a
	// refusal sending the change again would meet again: it stays as stored,
	// the ones after it are worked out from it as stored, and the change
	// itself is kept (stage 6 review 8, m7: failing the save held its outbox
	// entry, and every later write of the match behind it, forever).
	frozen := map[string]bool{}
	var refused *DerivedLineupError
	var lc *lineupCarry
	var keys []string
	for {
		lc = newLineupCarry(maps.Clone(lineups), pool, bracket, comp.IsKnockoutEnabled(), comp.TeamSize)
		keys = lc.rederive(saved.TeamID, frozen)
		bad := ""
		for _, k := range keys {
			if verr := lc.lineups[k].ValidatePositions(comp.TeamSize); verr != nil {
				bad = k
				if refused == nil {
					refused = &DerivedLineupError{Err: verr}
				}
				refused.MatchIDs = append(refused.MatchIDs, lc.lineups[k].MatchID)
				break
			}
		}
		if bad == "" {
			break
		}
		frozen[bad] = true
	}
	moved := make([]string, 0, len(keys))
	if len(keys) > 0 {
		// Every lineup that moved in ONE staged write, and a store that
		// refuses it fails the save's transaction, as a pin that cannot be
		// kept fails the start: what the carry worked out is never half
		// stored.
		out := make([]domain.TeamLineup, 0, len(keys))
		for _, k := range keys {
			out = append(out, lc.lineups[k])
			moved = append(moved, lc.lineups[k].MatchID)
		}
		if err := h.SetTeamLineups(comp.ID, out, comp.TeamSize); err != nil {
			return nil, fmt.Errorf("competition %s team %s: the lineups carried after a lineup save were not kept: %w", comp.ID, saved.TeamID, err)
		}
	}
	if refused != nil {
		log.Printf("ERROR engine: competition %s team %s: %v", comp.ID, saved.TeamID, refused)
		return moved, refused
	}
	return moved, nil
}

// DerivedLineupError reports the carried lineups a lineup change could not
// keep as worked out, because the domain refuses them (Err): a refusal that
// sending the change again would meet again. The change itself and every
// other carried lineup were kept; the refused ones stay as they were stored
// (FollowLineupSave, stage 6 review 8, m7).
type DerivedLineupError struct {
	MatchIDs []string
	Err      error
}

func (d *DerivedLineupError) Error() string {
	return fmt.Sprintf("the lineups carried at %s could not follow the change and were left as they were: %v", strings.Join(d.MatchIDs, ", "), d.Err)
}

func (d *DerivedLineupError) Unwrap() error { return d.Err }

// Warning is the sentence the operator is told.
func (d *DerivedLineupError) Warning() string {
	return fmt.Sprintf("Lineup saved, but the lineup of %s could not follow it and was left as it was (%v). Check that match's lineup.",
		strings.Join(d.MatchIDs, ", "), d.Err)
}

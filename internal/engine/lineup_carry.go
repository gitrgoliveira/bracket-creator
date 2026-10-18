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
// Unstamped saves (an older build's) make no copy: their base is the lineup in
// force when they arrive, as before.

import (
	"cmp"
	"log"
	"maps"
	"slices"

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
	filtered := make(map[string]domain.TeamLineup, len(lc.lineups))
	for k, l := range lc.lineups {
		if l.TeamID == teamID && l.MatchID != "" {
			if l.MatchID == matchID || (l.IsCarriedCopy() && l.CopiedAt >= at) {
				continue
			}
		}
		filtered[k] = l
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
	// Not ok: nothing this lineup knows was set before at. What it held
	// then is not known, and a value set later is never taken for it.
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
		stored := l.Positions[p]
		storedID, storedStamp := l.MemberIDs[p], l.Stamps[p]
		delete(l.Positions, p)
		delete(l.MemberIDs, p)
		delete(l.Stamps, p)
		if (domain.TeamLineup{Positions: map[domain.Position]string{p: ""}}).ValidatePositions(lc.teamSize) != nil {
			continue
		}
		if !src.Found {
			continue
		}
		v, known := lc.valueAt(src.Lineup, p, at, 0)
		if !known {
			// Nothing is known of the position at that stamp: the value the
			// lineup stores stays, and a value set later is never taken.
			v = domain.PositionChange{Stamp: storedStamp, Name: stored, MemberID: storedID}
		}
		if v.Name == "" && v.MemberID == "" {
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
func (lc *lineupCarry) rederive(teamID string) []string {
	var keys []string
	for k, l := range lc.lineups {
		if l.TeamID != teamID || (!l.IsCarriedCopy() && !l.IsStartPin()) {
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

// trimHistory bounds l's History by what is still read
// (domain.MaxPositionHistory): the stamps the team's carried lineups are
// read at (each copy's CopiedAt, each pin's StartedAt, l's own included)
// keep the value they read.
func (lc *lineupCarry) trimHistory(l *domain.TeamLineup) {
	var refs []int64
	add := func(x domain.TeamLineup) {
		if x.CopiedAt > 0 {
			refs = append(refs, x.CopiedAt)
		}
		if x.StartedAt > 0 {
			refs = append(refs, x.StartedAt)
		}
	}
	for _, x := range lc.lineups {
		if x.TeamID == l.TeamID {
			add(x)
		}
	}
	add(*l)
	l.TrimHistory(refs)
}

// LandStartingLineupSave lands a stamped partial save on a team's starting
// lineup (its round-0 entry, start; empty when it has none): the position
// holds its latest change by stamp (domain.TeamLineup.ApplyStampedChanges),
// and its History keeps every value a copy or a pin of the team reads.
func LandStartingLineupSave(lineups map[string]domain.TeamLineup, start domain.TeamLineup,
	changed []domain.Position, positions, memberIDs map[domain.Position]string, stamps map[domain.Position]int64, now int64) (domain.TeamLineup, error) {
	out, err := start.ApplyStampedChanges(changed, positions, memberIDs, stamps, now)
	if err != nil {
		return domain.TeamLineup{}, err
	}
	(&lineupCarry{lineups: lineups}).trimHistory(&out)
	return out, nil
}

// LandMatchLineupSave is the lineup a partial save of matchID's own lineup
// for teamID stores (the lineup handler's, under the competition's lock, over
// what its transaction loaded). The first stamped save of a match's own
// lineup makes a carried copy: its other positions are the carry rule at its
// earliest change's stamp (lineupCarry). A save over a carried copy joins its
// own positions, and one made before its earliest change moves that point
// back, the carried positions being worked out again there. A save over the
// lineup a kachinuki match started with turns it into the operator's own,
// carried from the same point. An unstamped first save lands on the lineup in
// force when it arrives, as before.
func LandMatchLineupSave(lineups map[string]domain.TeamLineup, pool []state.MatchResult, bracket *state.Bracket, knockout bool, teamSize int,
	teamID, matchID string, changed []domain.Position, positions, memberIDs map[domain.Position]string, stamps map[domain.Position]int64, now int64) (domain.TeamLineup, error) {
	lc := newLineupCarry(lineups, pool, bracket, knockout, teamSize)
	var base domain.TeamLineup
	found := false
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == matchID {
			base, found = l.Clone(), true
		}
	}
	first := int64(0)
	for _, p := range changed {
		if s := min(stamps[p], now); s > 0 && (first == 0 || s < first) {
			first = s
		}
	}
	switch {
	case found && base.IsStartPin():
		base.CopiedAt, base.CopiedFrom, base.Own = base.StartedAt, base.StartedFrom, nil
		base.StartedAt, base.StartedFrom = 0, ""
	case !found && first > 0:
		base = domain.TeamLineup{TeamID: teamID, MatchID: matchID, Positions: map[domain.Position]string{}, CopiedAt: first}
		base.CopiedFrom = lc.carryInto(&base, first)
	case !found:
		in := lc.sourceAt(teamID, matchID, now+1)
		base = domain.TeamLineup{TeamID: teamID, MatchID: matchID, Positions: maps.Clone(in.Lineup.Positions),
			MemberIDs: maps.Clone(in.Lineup.MemberIDs), Stamps: maps.Clone(in.Lineup.Stamps)}
		if base.Positions == nil {
			base.Positions = map[domain.Position]string{}
		}
	}
	out, err := base.ApplyStampedChanges(changed, positions, memberIDs, stamps, now)
	if err != nil {
		return domain.TeamLineup{}, err
	}
	if out.IsCarriedCopy() {
		out.CopiedFrom = lc.carryInto(&out, out.CopiedAt)
	}
	out.TeamID, out.MatchID, out.Round = teamID, matchID, 0
	lc.trimHistory(&out)
	return out, nil
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
	lineups, err := h.LoadTeamLineups(comp.ID)
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
	lc := newLineupCarry(lineups, pool, bracket, comp.IsKnockoutEnabled(), comp.TeamSize)
	var moved []string
	for _, k := range lc.rederive(saved.TeamID) {
		l := lc.lineups[k]
		if err := h.SetTeamLineup(comp.ID, l, comp.TeamSize); err != nil {
			log.Printf("engine: competition %s match %s: the lineup team %s carries there was not worked out again after a lineup save: %v", comp.ID, l.MatchID, l.TeamID, err)
			continue
		}
		moved = append(moved, l.MatchID)
	}
	return moved, nil
}

package engine

// kachinuki_export.go converts a kachinuki competition's persisted state
// (pool matches + bracket + team lineups) into the helper-layer
// KachinukiMatchDetail shape used by the new "Kachinuki Detail" Excel
// sheet. The Excel renderer lives in internal/helper/excel_kachinuki.go;
// this file bridges state.MatchResult / SubMatchResult into that pure
// rendering input.
//
// CHK037, T195–T203.

import (
	"cmp"
	"fmt"
	"log"
	"maps"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// KachinukiDetailMatches returns the bout-by-bout kachinuki detail for a
// competition, or an empty slice for fixed-format/individual comps.
// Exported so sibling workbook builders (internal/export.BuildResultsWorkbook,
// the "Download results" path) can emit the Kachinuki Detail sheet without
// duplicating the collection logic that Engine.ExportCompetitionXlsx uses.
func (e *Engine) KachinukiDetailMatches(id string) ([]helper.KachinukiMatchDetail, error) {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return nil, err
	}
	return e.collectKachinukiMatches(id, comp)
}

// collectKachinukiMatches returns one Kachinuki Detail section per match of
// a kachinuki competition's draw, for both workbook exports, and nil for any
// other competition. A match with recorded bouts lists exactly those; one
// still to be decided with none gets comp.TeamBoutRows() empty numbered rows
// for hand entry (operator decision 2026-09-27, bc-kdsc), and one decided
// without a bout gets none, since nothing is left to enter.
//
// The function is read-only: load pools, pool matches, bracket, and team
// lineups, flatten into helper.KachinukiMatchDetail. The order is pool
// matches in the Pool Matches grid's order, then bracket matches by match
// number (the order the Elimination Matches sheet prints them, not storage
// order), then the 3rd-place match.
func (e *Engine) collectKachinukiMatches(compID string, comp *state.Competition) ([]helper.KachinukiMatchDetail, error) {
	if !comp.IsKachinuki() {
		return nil, nil
	}

	draw, err := e.loadDetailDraw("engine.collectKachinukiMatches", compID)
	if err != nil {
		return nil, err
	}

	// Each match's section reads its fighters' positions from the lineup in
	// force for its teams at that match, over the draw loaded above. The
	// lineups are read once per export; they may be missing entirely, in which
	// case positions render as empty strings.
	rule := e.lineupRuleOrNone("engine.collectKachinukiMatches", compID, comp.IsKnockoutEnabled(), draw.poolMatches, draw.bracket)

	// Squad member labels (bc-pnum: "make a team member's label available
	// to the public surfaces" -- the printed record is one of the
	// surfaces the operator ruling names). Both lookups are tolerant of
	// every read failure the positions are: a missing/corrupt
	// team-members.yaml, roster, pools.csv or bracket.json must degrade to
	// blank labels, not fail the whole Kachinuki Detail export.
	teamNumbers := e.buildKachinukiTeamNumbers(compID, comp)
	squads := e.buildKachinukiSquads(compID)

	// A match not yet decided and with no bout recorded gets the empty rows
	// for hand entry.
	return draw.sections(comp, func(m *state.MatchResult, label string) (helper.KachinukiMatchDetail, bool) {
		detail := buildKachinukiDetail(m, label, rule.positionsForMatch(m), teamNumbers, squads)
		if len(detail.Bouts) == 0 && m.Status != state.MatchStatusCompleted {
			detail.BlankBoutRows = comp.TeamBoutRows()
		}
		return detail, true
	}), nil
}

// detailDraw is a competition's draw as the bout-detail sheets read it. The
// Kachinuki Detail and Representative Bouts sheets list their sections from
// the one walk of it (sections), so they cannot order a draw differently.
type detailDraw struct {
	pools       []helper.Pool
	poolMatches []state.MatchResult
	bracket     *state.Bracket // nil when bracket.json could not be read
}

// detailSection builds the section of one match titled label, or reports that
// the match has none.
type detailSection func(m *state.MatchResult, label string) (helper.KachinukiMatchDetail, bool)

// loadDetailDraw reads the pools, pool matches and bracket of compID. A
// bracket that cannot be read leaves the pool sections alone; caller names the
// export in that log line.
func (e *Engine) loadDetailDraw(caller, compID string) (detailDraw, error) {
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return detailDraw{}, err
	}
	pools, err := e.store.LoadPools(compID)
	if err != nil {
		return detailDraw{}, err
	}
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		log.Printf("%s compId=%s: bracket.json load error: %v; the export lists the pool sections only", caller, compID, err)
		bracket = nil
	}
	return detailDraw{pools: pools, poolMatches: poolMatches, bracket: bracket}, nil
}

// sections lists the draw's matches that section gives a section for, in the
// order every bout-detail sheet prints them: pool matches in the Pool Matches
// grid's order, then bracket matches by match number (the order the
// Elimination Matches sheet prints them, not storage order), then the
// 3rd-place match.
func (d detailDraw) sections(comp *state.Competition, section detailSection) []helper.KachinukiMatchDetail {
	var out []helper.KachinukiMatchDetail
	add := func(m *state.MatchResult, label string) {
		if detail, ok := section(m, label); ok {
			out = append(out, detail)
		}
	}

	// Pool matches first, numbered as the Pool Matches grid lists them
	// (helper.EachPoolMatch, shared with the blank template). A row the grid
	// has no block for (a tie-break or daihyosen) has a section only once it
	// has bouts, titled by its operator label rather than a draw number.
	ordinals := AttachPoolMatches(d.pools, d.poolMatches)
	byID := make(map[string]*state.MatchResult, len(d.poolMatches))
	for i := range d.poolMatches {
		byID[d.poolMatches[i].ID] = &d.poolMatches[i]
	}
	listed := make(map[string]bool, len(d.poolMatches))
	helper.EachPoolMatch(d.pools, func(label string, pool helper.Pool, i int) {
		id := fmt.Sprintf("%s-%d", pool.PoolName, ordinals[pool.PoolName][i])
		if m := byID[id]; m != nil {
			listed[id] = true
			add(m, label)
		}
	})
	for i := range d.poolMatches {
		m := &d.poolMatches[i]
		if listed[m.ID] || len(m.SubResults) == 0 {
			continue
		}
		add(m, OperatorMatchLabel(comp, nil, m.ID))
	}

	// Bracket matches in match-number order, the order the Elimination Matches
	// sheet prints its blocks, then the 3rd-place match (a sibling of
	// bracket.Rounds). Storage order is not that order: once byes lift a pair
	// into a later printed round it sits in an earlier storage row
	// (state.Bracket.NumberMatches). A bye, hidden with one side empty, has no
	// section. Each is titled and its sides named as the sheet prints them, so
	// a side reading "M 3" leads to the section titled with match 3.
	if d.bracket != nil {
		printed := PrintedBracket(d.bracket)
		type stored struct {
			bm            state.BracketMatch
			fallbackTitle string // for a bracket stored before match numbers
		}
		var matches []stored
		for rIdx, round := range d.bracket.Rounds {
			for mIdx, bm := range round {
				if len(bm.SubResults) == 0 && (bm.Hidden || bm.SideA == "" || bm.SideB == "") {
					continue
				}
				matches = append(matches, stored{bm, fmt.Sprintf("Bracket R%d-M%d", rIdx+1, mIdx+1)})
			}
		}
		// Stable, so matches without a number come first, in storage order.
		slices.SortStableFunc(matches, func(a, b stored) int { return cmp.Compare(a.bm.MatchNumber, b.bm.MatchNumber) })
		bracketSection := func(bm state.BracketMatch, fallbackTitle string) {
			p := printed[bm.ID]
			title := p.Title
			if title == "" {
				title = fallbackTitle
			}
			detail, ok := section(bracketMatchToTeamResult(bm), title)
			if !ok {
				return
			}
			detail.SideATeam, detail.SideBTeam = p.SideA, p.SideB
			out = append(out, detail)
		}
		for _, m := range matches {
			bracketSection(m.bm, m.fallbackTitle)
		}
		if bm := d.bracket.ThirdPlaceMatch; bm != nil {
			bracketSection(*bm, helper.ThirdPlaceLabel)
		}
	}

	return out
}

// buildKachinukiTeamNumbers resolves every team-shaped participant's
// CURRENTLY assigned competitor number, keyed by participant id, so a
// bout row's recorded team id (state.MatchResult.SideAID/SideBID, or the
// bracket-origin twin bracketMatchToTeamResult now carries) can be
// labelled without exposing the pools.csv/bracket.json file formats to
// the Excel renderer. Numbers are never persisted for a knockout-only
// draw (bc-pnum ruling 2: composed at read time from the bracket's
// DrawOrder), so this mirrors the two-format switch every other numbering
// reader in this codebase applies (engine.DrawSourceFor), rather than
// assuming pools.csv always has the answer.
//
// Tolerant of every read failure: a nil comp, no number prefix assigned
// yet, or an unreadable roster/pools/bracket file all return an empty
// map, so the caller's labels fall back to blank rather than the whole
// export failing over an auxiliary read -- the same tolerance
// lineupRuleOrNone applies to a missing/corrupt lineups file.
func (e *Engine) buildKachinukiTeamNumbers(compID string, comp *state.Competition) map[string]string {
	out := map[string]string{}
	if comp == nil || comp.EffectiveNumberPrefix() == "" {
		return out
	}
	switch DrawSourceFor(comp) {
	case DrawInPools:
		// pools.csv already carries each competitor's persisted Number
		// (RenumberCompetitors is the writer), so this is a direct
		// read-and-index rather than a merge onto a separately loaded
		// roster.
		pools, err := e.store.LoadPools(compID)
		if err != nil {
			return out
		}
		for _, pool := range pools {
			for _, pp := range pool.Players {
				if pp.Number != "" && pp.ID != "" {
					out[pp.ID] = pp.Number
				}
			}
		}
	case DrawInBracket:
		bracket, err := e.store.LoadBracket(compID)
		if err != nil || bracket == nil {
			return out
		}
		players, perr := e.store.LoadParticipantsOpt(compID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: false, HasIDs: comp.ParticipantIDsHint()})
		if perr != nil {
			return out
		}
		NumberKnockoutParticipants(comp, bracket.DrawOrder, players)
		for _, p := range players {
			if p.Number != "" && p.ID != "" {
				out[p.ID] = p.Number
			}
		}
	}
	return out
}

// buildKachinukiSquads loads compID's team-members.yaml (internal/state/squad.go),
// tolerant of any read failure. A missing file is already "no squads"
// from LoadSquads itself (state.parseSquadsFile's contract); a genuinely
// corrupt file degrades to no labels for this export rather than failing
// it, the same DEGRADE class the rest of this file's auxiliary lookups
// apply.
func (e *Engine) buildKachinukiSquads(compID string) map[string][]domain.TeamMember {
	squads, err := e.store.LoadSquads(compID)
	if err != nil {
		return map[string][]domain.TeamMember{}
	}
	return squads
}

// resolveKachinukiMemberLabel composes one bout side's squad member label
// (domain.SquadMemberLabel, "T10.1") from the team's current competitor
// number (teamNumbers) and the member's stable display index (squads).
// Returns "" when either lookup misses: no team id or member id recorded
// on the row (a legacy bout predating member ids, or an individual
// competition that never reaches this code path at all), the team has no
// assigned number yet, no squad is recorded for the team, or the member
// id names nobody currently in it. Blank is the same "nothing to show
// yet" fallback formatKachinukiPlayer already applies to a blank lineup
// position.
func resolveKachinukiMemberLabel(teamNumbers map[string]string, squads map[string][]domain.TeamMember, teamID, memberID string) string {
	if teamID == "" || memberID == "" {
		return ""
	}
	number := teamNumbers[teamID]
	if number == "" {
		return ""
	}
	for _, member := range squads[teamID] {
		if member.ID == memberID {
			return domain.SquadMemberLabel(number, member.Index)
		}
	}
	return ""
}

// resolveKachinukiDisplayName is the Go twin of resolveBoutSideDisplayName
// (web-mobile/js/lineup_resolver.jsx, bc-dnst): a bout side shows its
// member's CURRENT name, resolved by member id against the team's squad,
// on every surface including this Excel export -- the stored sub.SideA/
// sub.SideB text (storedName here) stays frozen forever and is never
// rewritten. Display-only, exactly like its JS twin: nothing that WRITES a
// bout may route a name through this. Returns storedName verbatim when
// memberID is empty, the team id is empty, no member in squads[teamID]
// carries that id, or that member's own Name is still blank (an unnamed
// seeded slot has nothing newer to show).
func resolveKachinukiDisplayName(squads map[string][]domain.TeamMember, teamID, memberID, storedName string) string {
	if teamID == "" || memberID == "" {
		return storedName
	}
	for _, member := range squads[teamID] {
		if member.ID == memberID && member.Name != "" {
			return member.Name
		}
	}
	return storedName
}

// boutFigures is what a bout's row prints beside its fighters, in side order:
// each side's score string and result mark, and the one closed-set middle
// mark. Every bout-detail sheet reads them here, attributed, marked and
// maru-filled by the same domain owners as the main sheets' bout rows
// (export's writeTeamSubMatchScores), so a bout reads the same on all of them;
// IpponsScore drops placeholder dots and the Ht mark from a recorded score.
func boutFigures(m *state.MatchResult, sub state.SubMatchResult) (scoreA, scoreB, markA, markB, middle string) {
	att := domain.SubBoutAttributionForTeamRow(sub.Attribution(), m.SideA, m.SideB)
	markA, markB = domain.SideMarksAB(sub.Decision, sub.HanteiDecided(), att)
	scoreA, scoreB = domain.DefaultWinMaruAB(
		domain.IpponsScore(sub.IpponsA), domain.IpponsScore(sub.IpponsB),
		sub.Decision, sub.Encho.On(), att)
	return scoreA, scoreB, markA, markB, domain.MiddleMark(sub.Decision, sub.Encho.On())
}

// buildKachinukiDetail converts a single state.MatchResult into the
// helper-layer detail struct. positions is the match's own position lookup
// (lineupRule.positionsForMatch).
func buildKachinukiDetail(m *state.MatchResult, label string, positions map[string]string, teamNumbers map[string]string, squads map[string][]domain.TeamMember) helper.KachinukiMatchDetail {
	resolvePos := func(team, memberID, player string) string {
		return resolveKachinukiBoutPosition(positions, team, memberID, player)
	}
	bouts := make([]helper.KachinukiBout, 0, len(m.SubResults))
	for _, sub := range m.SubResults {
		scoreA, scoreB, markA, markB, middle := boutFigures(m, sub)
		bouts = append(bouts, helper.KachinukiBout{
			Position:   sub.Position,
			SideAName:  resolveKachinukiDisplayName(squads, m.SideAID, sub.SideAMemberID, sub.SideA),
			SideALabel: resolveKachinukiMemberLabel(teamNumbers, squads, m.SideAID, sub.SideAMemberID),
			SideAPos:   resolvePos(m.SideA, sub.SideAMemberID, sub.SideA),
			ScoreA:     scoreA,
			SideBName:  resolveKachinukiDisplayName(squads, m.SideBID, sub.SideBMemberID, sub.SideB),
			SideBLabel: resolveKachinukiMemberLabel(teamNumbers, squads, m.SideBID, sub.SideBMemberID),
			SideBPos:   resolvePos(m.SideB, sub.SideBMemberID, sub.SideB),
			ScoreB:     scoreB,
			Middle:     middle,
			MarkA:      markA,
			MarkB:      markB,
		})
	}

	return helper.KachinukiMatchDetail{
		Label:     label,
		SideATeam: m.SideA,
		SideBTeam: m.SideB,
		Bouts:     bouts,
	}
}

// lineupKey is the composite key used to look up a player's lineup
// position in a match's position lookup. Both team name and player name
// are needed because the two teams of a match may field players with the
// same name in different positions.
func lineupKey(team, player string) string {
	return team + "\x00" + player
}

// memberKey is the player half of a lineup key for a position held by
// MEMBER ID rather than by name (bc-dnst): a fighter fielded by squad number
// and not yet named has an id and an empty name, so a name-keyed lookup
// could never find its position. The NUL-framed marker keeps the id
// namespace apart from names, which never contain NUL.
func memberKey(memberID string) string {
	return "\x00id\x00" + memberID
}

// positionsForMatch answers, for a match's section of the export, the lineup
// position each fighter held there. The position is read from the lineup in
// force for the fighter's team at THAT match (lineup_in_force.go), never from
// whichever saved lineup holds the fighter's name, and the team is its
// participant id, as on every other read of the rule: a side with no id labels
// nothing. The empty rule, &lineupRule{}, labels nothing, and positions then
// render as empty strings, which the renderer handles.
//
// The result is keyed by lineupKey(team name, fighter): by name, and by member
// id (memberKey) as well, since a fighter fielded by number alone has an id and
// no name.
func (r *lineupRule) positionsForMatch(m *state.MatchResult) map[string]string {
	out := map[string]string{}
	for _, side := range []struct{ id, name string }{{m.SideAID, m.SideA}, {m.SideBID, m.SideB}} {
		if in := r.inForce(side.id, m.ID); in.Found {
			indexLineupPositions(out, side.name, in.Lineup)
		}
	}
	return out
}

// indexLineupPositions adds each position a lineup fills to out, under
// lineupKey(team, fighter), by name and by member id.
//
// Both are indexed over the positions in the order a lineup is fielded
// (domain.ComparePositions), first write wins, because one fighter can still hold
// two positions of a lineup: the duplicate guard is new, and rows written before
// it are live data repaired by hand. Both loops compute the same map key for such
// a fighter, so a plain range let Go's randomised map order decide which position
// label survived, and the same competition exported "Senpo" on one run and
// "Chuken" on the next. The winner is the first position fielded, the one the
// roster (TeamLineup.OrderedMembers) fields the fighter at; a text order of the
// position names gave "Chuken" here for a fighter the roster fields as "Senpo",
// and "10" ahead of "2".
func indexLineupPositions(out map[string]string, team string, lineup domain.TeamLineup) {
	indexFirstPositionHeld(out, team, lineup.Positions, func(name string) string { return name })
	// Every position held by id is indexed under the id as well, so a
	// nameless fighter (bc-dnst) still resolves; resolveKachinukiBoutPosition
	// tries the id first.
	indexFirstPositionHeld(out, team, lineup.MemberIDs, memberKey)
}

// indexFirstPositionHeld indexes held (position to a fighter's name or member
// id, empty for a vacant position) into out under lineupKey(team, key(held)),
// walking the positions in the order a lineup is fielded so a fighter held twice
// keeps the label of the first.
func indexFirstPositionHeld(out map[string]string, team string, held map[domain.Position]string, key func(string) string) {
	indexed := make(map[string]struct{}, len(held))
	for _, pos := range slices.SortedFunc(maps.Keys(held), domain.ComparePositions) {
		fighter := held[pos]
		if fighter == "" {
			continue
		}
		if _, dup := indexed[fighter]; dup {
			continue
		}
		indexed[fighter] = struct{}{}
		out[lineupKey(team, key(fighter))] = pos.Label()
	}
}

// resolveKachinukiPosition returns the position label for (team, player) in
// a match's position lookup.
func resolveKachinukiPosition(positions map[string]string, team, player string) string {
	return positions[lineupKey(team, player)]
}

// resolveKachinukiBoutPosition resolves a bout side's position by its
// MEMBER ID first (the identity, present for every squad-era row and the
// only handle a nameless fighter has), then by name for legacy rows that
// carry no id.
func resolveKachinukiBoutPosition(positions map[string]string, team, memberID, player string) string {
	if memberID != "" {
		if label := resolveKachinukiPosition(positions, team, memberKey(memberID)); label != "" {
			return label
		}
	}
	return resolveKachinukiPosition(positions, team, player)
}

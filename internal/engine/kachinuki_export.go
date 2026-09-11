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
	"fmt"
	"strings"

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

// collectKachinukiMatches returns the bout-by-bout detail for every
// kachinuki match in the competition that has at least one bout. Only
// invoked for competitions where comp.IsKachinuki() is true; returns an
// empty slice for fixed team or individual competitions.
//
// The function is read-only: load pool matches, bracket, and team lineups,
// flatten into helper.KachinukiMatchDetail. The order is pool matches in
// persisted order, then bracket matches round-by-round.
func (e *Engine) collectKachinukiMatches(compID string, comp *state.Competition) ([]helper.KachinukiMatchDetail, error) {
	if !comp.IsKachinuki() {
		return nil, nil
	}

	// Lookup table: teamName → position → playerName, used to derive a
	// per-bout lineup-position annotation. Built once per export; lineups
	// may be missing entirely (Slice 7 lineup integration is partial), in
	// which case positions render as empty strings.
	positionByPlayer := e.buildKachinukiPositionMap(compID, comp)

	// Squad member labels (bc-pnum: "make a team member's label available
	// to the public surfaces" -- the printed record is one of the
	// surfaces the operator ruling names). Both lookups are tolerant of
	// every read failure the same way positionByPlayer is above: a
	// missing/corrupt squads.yaml, roster, pools.csv or bracket.json must
	// degrade to blank labels, not fail the whole Kachinuki Detail export.
	teamNumbers := e.buildKachinukiTeamNumbers(compID, comp)
	squads := e.buildKachinukiSquads(compID)

	var out []helper.KachinukiMatchDetail

	// Pool matches first.
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	for i := range poolMatches {
		m := &poolMatches[i]
		if len(m.SubResults) == 0 {
			continue
		}
		out = append(out, buildKachinukiDetail(m, fmt.Sprintf("Pool Match %d", i+1), positionByPlayer, teamNumbers, squads))
	}

	// Bracket matches: read real SubResults appended by MaybeAdvanceKachinuki.
	// A bracket match with no SubResults is skipped (renderer guard); one
	// with SubResults is fed directly to buildKachinukiDetail regardless of
	// the match decision (exhaustion, daihyosen, fought, etc.).
	bracket, err := e.store.LoadBracket(compID)
	if err == nil && bracket != nil {
		for rIdx, round := range bracket.Rounds {
			for mIdx, bm := range round {
				if len(bm.SubResults) == 0 {
					continue
				}
				detail := buildKachinukiDetail(bracketMatchToTeamResult(bm), fmt.Sprintf("Bracket R%d-M%d", rIdx+1, mIdx+1), positionByPlayer, teamNumbers, squads)
				if len(detail.Bouts) > 0 {
					out = append(out, detail)
				}
			}
		}
		// The 3rd-place match is a sibling of bracket.Rounds; same treatment.
		if bm := bracket.ThirdPlaceMatch; bm != nil && len(bm.SubResults) > 0 {
			detail := buildKachinukiDetail(bracketMatchToTeamResult(*bm), "3rd Place Match", positionByPlayer, teamNumbers, squads)
			if len(detail.Bouts) > 0 {
				out = append(out, detail)
			}
		}
	}

	return out, nil
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
// buildKachinukiPositionMap already applies to a missing/corrupt lineups
// file, a few lines below in this file.
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

// buildKachinukiSquads loads compID's squads.yaml (internal/state/squad.go),
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

// buildKachinukiDetail converts a single state.MatchResult into the
// helper-layer detail struct, including eliminations and the final
// decision.
func buildKachinukiDetail(m *state.MatchResult, label string, positions map[string]string, teamNumbers map[string]string, squads map[string][]domain.TeamMember) helper.KachinukiMatchDetail {
	resolvePos := func(team, player string) string {
		return resolveKachinukiPosition(positions, m.ID, team, player)
	}
	bouts := make([]helper.KachinukiBout, 0, len(m.SubResults))
	for _, sub := range m.SubResults {
		bouts = append(bouts, helper.KachinukiBout{
			Position:   sub.Position,
			SideAName:  sub.SideA,
			SideALabel: resolveKachinukiMemberLabel(teamNumbers, squads, m.SideAID, sub.SideAMemberID),
			SideAPos:   resolvePos(m.SideA, sub.SideA),
			ScoreA:     strings.Join(sub.IpponsA, ""),
			SideBName:  sub.SideB,
			SideBLabel: resolveKachinukiMemberLabel(teamNumbers, squads, m.SideBID, sub.SideBMemberID),
			SideBPos:   resolvePos(m.SideB, sub.SideB),
			ScoreB:     strings.Join(sub.IpponsB, ""),
			Winner:     sub.Winner,
			Decision:   sub.Decision,
		})
	}

	elimA, elimB := tallyKachinukiEliminations(m)

	return helper.KachinukiMatchDetail{
		Label:        label,
		SideATeam:    m.SideA,
		SideBTeam:    m.SideB,
		Bouts:        bouts,
		Winner:       m.Winner,
		Decision:     m.Decision,
		EliminationA: elimA,
		EliminationB: elimB,
	}
}

// tallyKachinukiEliminations returns the number of retired (eliminated)
// players per side. It delegates to RetiredPlayersFromBoutLog so the
// retirement rule (hikiwake retires both sides, otherwise the loser retires)
// lives in exactly one place: len of the per-side retired-NAME set equals the
// elimination count for valid play (each player retires at most once, and
// RetiredMemberSet.retire always records the name when the bout row names
// one, member id or not, so Names is the count regardless of which rows
// happen to be id-repaired). `a` is SideA eliminations, `b` is SideB.
func tallyKachinukiEliminations(m *state.MatchResult) (a, b int) {
	retiredA, retiredB := RetiredPlayersFromBoutLog(m.SubResults, m.SideA, m.SideB)
	return len(retiredA.Names), len(retiredB.Names)
}

// lineupKey is the composite key used to look up a player's lineup
// position from a ROUND-scoped lineup. Both team name and player name
// are needed because two teams may field players with the same name in
// different positions.
func lineupKey(team, player string) string {
	return team + "\x00" + player
}

// matchLineupKey is the composite key for a MATCH-scoped lineup
// position (mp-825): the same player may occupy a different position in
// successive encounters, so the match ID is part of the key.
func matchLineupKey(matchID, team, player string) string {
	return matchID + "\x00" + team + "\x00" + player
}

// buildKachinukiPositionMap loads team lineups for the competition and
// flattens them into a position lookup. Two namespaces share one map:
// match-scoped entries (mp-825) keyed by matchLineupKey, and
// round-scoped (legacy) entries keyed by lineupKey as the fallback.
// resolveKachinukiPosition consults match-scoped first. Missing lineups
// yield an empty map, positions render as empty strings, the renderer
// handles it.
func (e *Engine) buildKachinukiPositionMap(compID string, comp *state.Competition) map[string]string {
	out := map[string]string{}
	if comp == nil {
		return out
	}
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil || len(lineups) == 0 {
		return out
	}
	// The lineup editor keys lineups by the team PARTICIPANT ID
	// (player.id, a UUID) while match sides, and therefore
	// resolveKachinukiPosition's team argument, carry the team display
	// NAME. Index every entry under BOTH keys ("match on id OR name") so
	// UI-saved lineups resolve. A participant load failure only loses the
	// id-to-name translation; raw TeamID keys are still emitted.
	idToName := map[string]string{}
	if participants, perr := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName()); perr == nil {
		for _, p := range participants {
			if p.ID != "" && p.Name != "" {
				idToName[p.ID] = p.Name
			}
		}
	}
	for _, lineup := range lineups {
		teamKeys := []string{lineup.TeamID}
		if name, ok := idToName[lineup.TeamID]; ok && name != lineup.TeamID {
			teamKeys = append(teamKeys, name)
		}
		for pos, playerName := range lineup.Positions {
			if playerName == "" {
				continue
			}
			label := formatPositionLabel(pos)
			for _, teamKey := range teamKeys {
				if lineup.MatchID != "" {
					out[matchLineupKey(lineup.MatchID, teamKey, playerName)] = label
				} else {
					out[lineupKey(teamKey, playerName)] = label
				}
			}
		}
	}
	return out
}

// resolveKachinukiPosition returns the position label for (team, player)
// in the given match, preferring a match-scoped lineup and falling back
// to the round-scoped entry.
func resolveKachinukiPosition(positions map[string]string, matchID, team, player string) string {
	if matchID != "" {
		if label, ok := positions[matchLineupKey(matchID, team, player)]; ok {
			return label
		}
	}
	return positions[lineupKey(team, player)]
}

// formatPositionLabel turns a domain.Position wire value into a
// title-cased label suitable for the Excel cell (e.g. "senpo" → "Senpo").
// Numeric positions ("1", "2", …) pass through unchanged.
func formatPositionLabel(p domain.Position) string {
	s := string(p)
	if s == "" {
		return ""
	}
	// Numeric positions stay numeric.
	if s[0] >= '0' && s[0] <= '9' {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

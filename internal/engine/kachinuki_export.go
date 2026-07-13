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
// invoked for competitions with comp.TeamMatchType == TeamMatchTypeKachinuki;
// returns an empty slice for fixed team or individual competitions.
//
// The function is read-only: load pool matches, bracket, and team lineups,
// flatten into helper.KachinukiMatchDetail. The order is pool matches in
// persisted order, then bracket matches round-by-round.
func (e *Engine) collectKachinukiMatches(compID string, comp *state.Competition) ([]helper.KachinukiMatchDetail, error) {
	if comp == nil || comp.TeamMatchType != state.TeamMatchTypeKachinuki || comp.TeamSize < 2 {
		return nil, nil
	}

	// Lookup table: teamName → position → playerName, used to derive a
	// per-bout lineup-position annotation. Built once per export; lineups
	// may be missing entirely (Slice 7 lineup integration is partial), in
	// which case positions render as empty strings.
	positionByPlayer := e.buildKachinukiPositionMap(compID, comp)

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
		out = append(out, buildKachinukiDetail(m, fmt.Sprintf("Pool Match %d", i+1), positionByPlayer))
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
				mr := state.MatchResult{
					ID:         bm.ID,
					SideA:      bm.SideA,
					SideB:      bm.SideB,
					Winner:     bm.Winner,
					Status:     bm.Status,
					Decision:   bm.Decision,
					SubResults: bm.SubResults,
				}
				detail := buildKachinukiDetail(&mr, fmt.Sprintf("Bracket R%d-M%d", rIdx+1, mIdx+1), positionByPlayer)
				if len(detail.Bouts) > 0 {
					out = append(out, detail)
				}
			}
		}
		// The 3rd-place match is a sibling of bracket.Rounds; same treatment.
		if bm := bracket.ThirdPlaceMatch; bm != nil && len(bm.SubResults) > 0 {
			mr := state.MatchResult{
				ID:         bm.ID,
				SideA:      bm.SideA,
				SideB:      bm.SideB,
				Winner:     bm.Winner,
				Status:     bm.Status,
				Decision:   bm.Decision,
				SubResults: bm.SubResults,
			}
			detail := buildKachinukiDetail(&mr, "3rd Place Match", positionByPlayer)
			if len(detail.Bouts) > 0 {
				out = append(out, detail)
			}
		}
	}

	return out, nil
}

// buildKachinukiDetail converts a single state.MatchResult into the
// helper-layer detail struct, including eliminations and the final
// decision.
func buildKachinukiDetail(m *state.MatchResult, label string, positions map[string]string) helper.KachinukiMatchDetail {
	bouts := make([]helper.KachinukiBout, 0, len(m.SubResults))
	for _, sub := range m.SubResults {
		bouts = append(bouts, helper.KachinukiBout{
			Position:  sub.Position,
			SideAName: sub.SideA,
			SideAPos:  resolveKachinukiPosition(positions, m.ID, m.SideA, sub.SideA),
			ScoreA:    strings.Join(sub.IpponsA, ""),
			SideBName: sub.SideB,
			SideBPos:  resolveKachinukiPosition(positions, m.ID, m.SideB, sub.SideB),
			ScoreB:    strings.Join(sub.IpponsB, ""),
			Winner:    sub.Winner,
			Decision:  sub.Decision,
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

// tallyKachinukiEliminations counts retired players per team based on
// the bout log. A hikiwake retires both sides; a per-bout winner retires
// the loser.
func tallyKachinukiEliminations(m *state.MatchResult) (a, b int) {
	for _, sub := range m.SubResults {
		if state.IsDraw(sub.Decision) {
			if sub.SideA != "" {
				a++
			}
			if sub.SideB != "" {
				b++
			}
			continue
		}
		switch sub.Winner {
		case sub.SideA, m.SideA:
			// SideA wins → SideB player retired.
			if sub.SideB != "" {
				b++
			}
		case sub.SideB, m.SideB:
			if sub.SideA != "" {
				a++
			}
		}
	}
	return a, b
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

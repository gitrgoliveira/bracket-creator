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

	// Bracket matches.
	bracket, err := e.store.LoadBracket(compID)
	if err == nil && bracket != nil {
		for rIdx, round := range bracket.Rounds {
			for mIdx, bm := range round {
				// BracketMatch carries no SubResults; bout logs only
				// live on pool MatchResults today (see the TODO in
				// MaybeAdvanceKachinuki). When future work adds bouts to
				// the bracket store, this loop will pick them up via the
				// same buildKachinukiDetail helper. For now we render
				// a single-row section only when the bracket match has
				// already been finalized via kachinuki-exhaustion, a
				// summary stub so operators at least see the outcome.
				if bm.Decision != string(domain.DecisionKachinukiExhaustion) {
					continue
				}
				stub := state.MatchResult{
					ID:       bm.ID,
					SideA:    bm.SideA,
					SideB:    bm.SideB,
					Winner:   bm.Winner,
					Status:   bm.Status,
					Decision: bm.Decision,
				}
				detail := buildKachinukiDetail(&stub, fmt.Sprintf("Bracket R%d-M%d", rIdx+1, mIdx+1), positionByPlayer)
				// No bouts on the bracket stub, skip rather than emit
				// an empty section (renderer also guards against this).
				if len(detail.Bouts) == 0 {
					continue
				}
				out = append(out, detail)
			}
		}
		// The bronze (3rd-place) match is a sibling of bracket.Rounds; render
		// it with the same exhaustion-stub treatment as the Rounds matches.
		if bm := bracket.ThirdPlaceMatch; bm != nil && bm.Decision == string(domain.DecisionKachinukiExhaustion) {
			stub := state.MatchResult{
				ID:       bm.ID,
				SideA:    bm.SideA,
				SideB:    bm.SideB,
				Winner:   bm.Winner,
				Status:   bm.Status,
				Decision: bm.Decision,
			}
			detail := buildKachinukiDetail(&stub, "3rd Place Match", positionByPlayer)
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
	if err != nil || lineups == nil {
		return out
	}
	for _, lineup := range lineups {
		teamName := lineup.TeamID
		for pos, playerName := range lineup.Positions {
			if playerName == "" {
				continue
			}
			label := formatPositionLabel(pos)
			if lineup.MatchID != "" {
				out[matchLineupKey(lineup.MatchID, teamName, playerName)] = label
			} else {
				out[lineupKey(teamName, playerName)] = label
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

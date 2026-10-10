package engine

// representative_export.go converts a fixed-order team competition's
// representative bouts into the helper-layer KachinukiMatchDetail shape the
// "Representative Bouts" sheet is drawn from. The sheet exists because the
// main sheets' team block lists the numbered bouts only (it skips position -1,
// so its height, page breaks and IV/PW ranges stay TeamBoutRows' alone), which
// left a representative bout, and the representatives, out of every
// workbook. The renderer is the Kachinuki Detail sheet's
// (internal/helper/excel_kachinuki.go), and the matches are walked by the same
// detailDraw.sections, so the two sheets order a draw alike.

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RepresentativeBoutMatches returns one section per match of a fixed-order
// team competition that holds a representative bout, or an empty slice for
// any other competition. Exported, like KachinukiDetailMatches, so the
// "Download results" path (internal/export.BuildResultsWorkbook) emits the
// sheet without duplicating the collection logic ExportCompetitionXlsx uses.
func (e *Engine) RepresentativeBoutMatches(id string) ([]helper.KachinukiMatchDetail, error) {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return nil, err
	}
	return e.collectRepresentativeBouts(id, comp)
}

// collectRepresentativeBouts returns the Representative Bouts sections of a
// fixed-order team competition's draw, nil for any other competition (a
// kachinuki one has no representative bout: AddDaihyosen refuses it), in the
// order the Kachinuki Detail sheet lists a draw. A match without the bout has
// no section. It is read-only and, like the Kachinuki Detail read, tolerant of
// a missing or unreadable team-members.yaml, roster or bracket.json: the
// representatives then print as the bout label alone.
func (e *Engine) collectRepresentativeBouts(compID string, comp *state.Competition) ([]helper.KachinukiMatchDetail, error) {
	if !comp.IsTeam() || comp.IsKachinuki() {
		return nil, nil
	}
	draw, err := e.loadDetailDraw("engine.collectRepresentativeBouts", compID)
	if err != nil {
		return nil, err
	}
	teamNumbers := e.buildKachinukiTeamNumbers(compID, comp)
	squads := e.buildKachinukiSquads(compID)
	return draw.sections(comp, func(m *state.MatchResult, label string) (helper.KachinukiMatchDetail, bool) {
		return buildRepresentativeBoutDetail(m, label, teamNumbers, squads)
	}), nil
}

// buildRepresentativeBoutDetail converts m's representative bout (the row at
// state.DaihyosenSubPosition) into a one-bout section titled label, or reports
// false when m holds none. The bout's score, result marks and centre come from
// boutFigures, the Kachinuki Detail sheet's own, so the two sheets cannot
// disagree on a bout; the centre reads (DH) through domain.MiddleMark.
//
// The row keeps the TEAM names in SideA/SideB by rule (the result mark is
// placed on the winner's side by comparing against them), so its fighters come
// from the member ids alone, via representativeOf. A representative has no
// lineup position, so none is printed.
func buildRepresentativeBoutDetail(m *state.MatchResult, label string, teamNumbers map[string]string, squads map[string][]domain.TeamMember) (helper.KachinukiMatchDetail, bool) {
	i := state.DaihyosenSubIndex(m.SubResults)
	if i < 0 {
		return helper.KachinukiMatchDetail{}, false
	}
	sub := m.SubResults[i]
	scoreA, scoreB, markA, markB, middle := boutFigures(m, sub)
	nameA, labelA := representativeOf(teamNumbers, squads, m.SideAID, sub.SideAMemberID)
	nameB, labelB := representativeOf(teamNumbers, squads, m.SideBID, sub.SideBMemberID)
	return helper.KachinukiMatchDetail{
		Label:     label,
		SideATeam: m.SideA,
		SideBTeam: m.SideB,
		Bouts: []helper.KachinukiBout{{
			Position:   sub.Position,
			SideAName:  nameA,
			SideALabel: labelA,
			ScoreA:     scoreA,
			SideBName:  nameB,
			SideBLabel: labelB,
			ScoreB:     scoreB,
			Middle:     middle,
			MarkA:      markA,
			MarkB:      markB,
		}},
	}, true
}

// representativeOf names the representative a side picked: the current name
// and "T1.3"-style label of the member memberID in teamID's team members.
// Both are empty for a side that picked nobody (no member id) or whose pick
// does not resolve to a member of that team (a member since removed, a team
// without an id, no team-members file): unlike a numbered bout's row, the
// representative's row stores the team's name where a fighter's would be, so
// the stored name is never a fallback here, and the bout prints its label
// alone, as the viewer shows "-".
func representativeOf(teamNumbers map[string]string, squads map[string][]domain.TeamMember, teamID, memberID string) (name, label string) {
	if teamID == "" || memberID == "" {
		return "", ""
	}
	for _, member := range squads[teamID] {
		if member.ID == memberID {
			return member.Name, resolveKachinukiMemberLabel(teamNumbers, squads, teamID, memberID)
		}
	}
	return "", ""
}

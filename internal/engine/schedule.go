package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func (e *Engine) GenerateSchedule(compID string) error {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return err
	}
	if comp == nil {
		return fmt.Errorf("competition %s not found", compID)
	}

	var entries []state.ScheduleEntry

	if comp.Format == "pools" {
		matches, err := e.store.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		for _, m := range matches {
			entries = append(entries, state.ScheduleEntry{
				MatchType: "pool",
				MatchRef:  m.ID,
				Court:     m.Court,
				Status:    string(m.Status),
			})
		}
	} else {
		bracket, err := e.store.LoadBracket(compID)
		if err != nil {
			return err
		}
		if bracket != nil {
			for rIdx, round := range bracket.Rounds {
				for _, m := range round {
					entries = append(entries, state.ScheduleEntry{
						MatchType: "bracket",
						MatchRef:  fmt.Sprintf("R%d-M%s", rIdx+1, m.ID),
						Court:     "A", // Default court
						Status:    string(m.Status),
					})
				}
			}
		}
	}

	return e.store.SaveSchedule(compID, entries)
}

package engine

import (
	"fmt"
)

func (e *Engine) StartCompetition(id string) error {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return err
	}
	if comp == nil {
		return fmt.Errorf("competition %s not found", id)
	}

	if comp.Status != "setup" && comp.Status != "" {
		return fmt.Errorf("competition %s already started", id)
	}

	players, err := e.store.LoadParticipants(id, comp.WithZekkenName)
	if err != nil {
		return err
	}
	if len(players) == 0 {
		return fmt.Errorf("no participants found for competition %s", id)
	}

	seeds, err := e.store.LoadSeeds(id)
	if err != nil {
		return err
	}

	// Generate Pools or Bracket
	if comp.Format == "pools" {
		if err := e.generatePools(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = "pools"
	} else {
		if err := e.generatePlayoffs(comp, players, seeds); err != nil {
			return err
		}
		comp.Status = "playoffs"
	}

	if err := e.store.SaveCompetition(comp); err != nil {
		return err
	}

	return e.GenerateSchedule(id)
}

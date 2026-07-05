package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LeagueStandings returns a league competition's standings as a single
// rank-ordered slice, mirroring SwissStandings. A league runs one round-robin
// group, so this returns that group's PlayerStandings (already rank-ordered by
// CalculatePoolStandings, with daihyosen / tie-break / override state applied).
//
// Leagues are NOT pools: this dedicated read is what the league standings
// surface consumes, so the league UI never routes through the pool path. A
// non-league competition yields a NotFoundError so the endpoint 404s rather
// than leaking pool standings.
func (e *Engine) LeagueStandings(compID string) ([]state.PlayerStanding, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.Format != state.CompFormatLeague {
		return nil, notFoundErrorf("competition %q is not a league", compID)
	}
	byPool, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}
	// A league has exactly one round-robin group; return it. Iterating the map
	// is safe here because there is a single key (an empty slice before the
	// draw, when CalculatePoolStandings returns no groups).
	for _, group := range byPool {
		return group, nil
	}
	return []state.PlayerStanding{}, nil
}

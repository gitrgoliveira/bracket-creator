package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LeagueStandingsBeforeDraw is LeagueStandings as the same league answers
// before its draw: no group, so an empty slice. A public read of a draft whose
// draw is hidden from its caller answers it (bc-drpb). The refusals are
// LeagueStandings' own.
func (e *Engine) LeagueStandingsBeforeDraw(compID string) ([]state.PlayerStanding, error) {
	if err := e.requireLeague(compID); err != nil {
		return nil, err
	}
	return []state.PlayerStanding{}, nil
}

// requireLeague answers an unknown competition, or one that is not a league,
// with the NotFoundError the league standings reads give.
func (e *Engine) requireLeague(compID string) error {
	comp, err := e.loadExistingCompetition(compID)
	if err != nil {
		return err
	}
	if comp.Format != state.CompFormatLeague {
		return notFoundErrorf("competition %q is not a league", compID)
	}
	return nil
}

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
	if err := e.requireLeague(compID); err != nil {
		return nil, err
	}
	byPool, err := e.CalculatePoolStandings(compID)
	if err != nil {
		return nil, err
	}
	// A league has exactly one round-robin group; return it. Iterating the map
	// is safe here because there is a single key. With no group the answer is
	// an empty slice, as LeagueStandingsBeforeDraw gives.
	for _, group := range byPool {
		return group, nil
	}
	return []state.PlayerStanding{}, nil
}

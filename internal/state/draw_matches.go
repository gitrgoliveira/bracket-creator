package state

import (
	"errors"
	"fmt"
)

// DrawMatch is the part of a stored match that places it in the draw and says
// who is seated: its id, the two participant ids, and where a knockout match
// sits. It exists so a reader that needs only that (the lineup rule, which
// orders a team's matches) does not pay for the deep clone of every score,
// ippon slice and group stamp that LoadPoolMatches and LoadBracket make.
type DrawMatch struct {
	ID               string
	SideAID, SideBID string
	// Knockout is true for a bracket match, the 3rd-place match included. A
	// pool, league or Swiss match is not: its order is read from its id.
	Knockout bool
	// Round is a knockout match's bracket round index, len(Rounds) for the
	// 3rd-place match; 0 for any other match.
	Round int
	// Index is a knockout match's position within its round, 0 for the
	// 3rd-place match; 0 for any other match.
	Index int
	// Hidden is true for a structural bye: a bracket match nobody plays, which
	// the draw can still seat a team in (a bye's winner is seated at once).
	Hidden bool
	// Bye is true for a pool, league or Swiss match with a side that has neither
	// a name nor a participant id: a Swiss round with an odd number of teams
	// stores its odd team out as a completed match against nobody. A bracket's
	// byes are Hidden instead, and a knockout match is never a Bye.
	Bye bool
	// PoolRound is a pool or league match's stored Round: its circle-method
	// round, or -1 for a pool drawn without rounds. 0 for a knockout match, and
	// for a Swiss match, which never stored one.
	PoolRound int
}

// DrawMatchesFrom projects loaded matches into DrawMatch values: every pool
// match, then each bracket round's matches, then the 3rd-place match, all in
// stored order. It keeps no reference into its arguments, so it is as safe over
// the store's cached parse as over a caller's own copy.
func DrawMatchesFrom(poolMatches []MatchResult, bracket *Bracket) []DrawMatch {
	n := len(poolMatches)
	if bracket != nil {
		for _, round := range bracket.Rounds {
			n += len(round)
		}
		if bracket.ThirdPlaceMatch != nil {
			n++
		}
	}
	out := make([]DrawMatch, 0, n)
	for i := range poolMatches {
		m := &poolMatches[i]
		bye := m.SideA == "" && m.SideAID == "" || m.SideB == "" && m.SideBID == ""
		out = append(out, DrawMatch{ID: m.ID, SideAID: m.SideAID, SideBID: m.SideBID, PoolRound: m.Round, Bye: bye})
	}
	if bracket == nil {
		return out
	}
	for round := range bracket.Rounds {
		for index := range bracket.Rounds[round] {
			bm := &bracket.Rounds[round][index]
			out = append(out, DrawMatch{ID: bm.ID, SideAID: bm.SideAID, SideBID: bm.SideBID, Knockout: true, Round: round, Index: index, Hidden: bm.Hidden})
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil {
		out = append(out, DrawMatch{ID: bm.ID, SideAID: bm.SideAID, SideBID: bm.SideBID, Knockout: true, Round: len(bracket.Rounds), Hidden: bm.Hidden})
	}
	return out
}

// DrawMatches is DrawMatchesFrom over the store's CACHED parse of
// pool-matches.csv and bracket.json. It reads that cache without copying, as
// MatchSidesByID does, and adds none of its own, so the cache refresh every
// writer already performs covers it. Like LoadPoolMatches and LoadBracket,
// which it replaces for a reader of ids alone, it runs EnsureLegacyUpgraded
// first: the side ids it returns are what that upgrade stamps onto a legacy
// draw.
//
// A file that cannot be read contributes no matches and is named in the
// returned error, while the matches of the other file are still returned: a
// caller whose answer survives half a draw logs the error and carries on, and
// one that does not returns it.
func (s *Store) DrawMatches(compID string) ([]DrawMatch, error) {
	if err := ValidateCompetitionID(compID); err != nil {
		return nil, err
	}
	s.EnsureLegacyUpgraded(compID)
	poolMatches, poolErr := s.cachedPoolMatches(compID)
	if poolErr != nil {
		poolErr = fmt.Errorf("pool-matches.csv: %w", poolErr)
	}
	bracket, bracketErr := s.cachedBracket(compID)
	if bracketErr != nil {
		bracketErr = fmt.Errorf("bracket.json: %w", bracketErr)
	}
	return DrawMatchesFrom(poolMatches, bracket), errors.Join(poolErr, bracketErr)
}

// TeamMatches is TeamMatches over DrawMatches, with the competition's own
// format deciding whether its bracket is a knockout: the format is read from
// the cached config.md without copying it, so the call adds no parse of its own.
// A competition with no config.md plays a knockout as far as this goes, so no
// bracket match is dropped for want of a record. A file that cannot be read is
// named in the returned error and contributes nothing, as in DrawMatches.
func (s *Store) TeamMatches(compID string) ([]TeamMatch, error) {
	draw, drawErr := s.DrawMatches(compID)
	comp, compErr := s.cachedCompetition(compID)
	if compErr != nil {
		compErr = fmt.Errorf("config.md: %w", compErr)
	}
	return TeamMatches(draw, comp == nil || comp.IsKnockoutEnabled()), errors.Join(drawErr, compErr)
}

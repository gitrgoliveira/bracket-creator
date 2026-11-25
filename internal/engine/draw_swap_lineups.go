package engine

import (
	"errors"
	"fmt"
	"log"
	"maps"
	"slices"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A swapped team takes its lineups with it (bc-drev FR6, decision D3 (b)):
// each lineup saved for one of the team's matches moves to the match the team
// plays in the same playing order at its new place, the k-th team match of
// the old place to the k-th of the new one, in match order (state.TeamMatches:
// a pool's bout order, a knockout's first match). A lineup whose playing order
// has no match at the new place (a move to a smaller pool) is removed and
// counted. Opponents' lineups, round lineups and starting lineups stay.

// lineupSlotWrite is one lineup slot (a team's own lineup of one match) as the
// swap leaves it: every record of it, the live one and the removed ones, or
// none when the slot is emptied.
type lineupSlotWrite struct {
	teamID, matchID string
	records         []domain.TeamLineup
}

// swapLineups is the lineup part of a swap's plan: the slots it writes, the
// competition's RoundLineupsGiven after it (given, set when givenChanged),
// and the teams whose lineups moved, whose carried lineups are worked out
// again once the slots are written.
type swapLineups struct {
	slots        []lineupSlotWrite
	given        map[string][]string
	givenChanged bool
	teams        []string
}

// teamMatchMap maps each of a team's matches at its old place (old, in match
// order) to the match it plays in the same playing order at its new place
// (new, in match order), "" when there is none. A match the team plays both
// before and after (a swap inside one pool, or of two knockout opponents,
// where one match holds both teams) keeps its lineup where it is and is left
// out of the map, and so out of the pairing: the remaining matches pair up in
// order, so a swap inside one pool only reorders the bouts around it.
func teamMatchMap(old, new []string) map[string]string {
	plays := map[string]bool{}
	for _, id := range new {
		plays[id] = true
	}
	played := map[string]bool{}
	for _, id := range old {
		played[id] = true
	}
	var to []string
	for _, id := range new {
		if !played[id] {
			to = append(to, id)
		}
	}
	out := map[string]string{}
	k := 0
	for _, id := range old {
		if plays[id] {
			continue
		}
		out[id] = ""
		if k < len(to) {
			out[id] = to[k]
		}
		k++
	}
	return out
}

// seatedMatches lists the ids of the matches of matches that seat teamID, in
// the order given.
func seatedMatches(matches []state.TeamMatch, teamID string) []string {
	var out []string
	for _, m := range matches {
		if m.Seats(teamID) {
			out = append(out, m.ID)
		}
	}
	return out
}

// moveLineups fills the plan's lineup part for a swap of the teams a and b,
// from the draw before (snap) and after (the plan's own pools, pool matches
// and bracket, so it runs after a seeded swap's rebuild). It is pure, like
// the rest of the plan: the dry run answers the same counts. Nothing for an
// individual competition.
func (p *swapPlan) moveLineups(snap swapSnapshot, a, b string) {
	comp := snap.comp
	if !comp.IsTeam() {
		return
	}
	knockout := comp.IsKnockoutEnabled()
	before := state.TeamMatches(state.DrawMatchesFrom(snap.poolMatches, snap.bracket), knockout)
	after := state.TeamMatches(state.DrawMatchesFrom(p.poolMatches, p.bracket), knockout)

	// The records of each slot, sorted by storage order so the plan is the
	// same on every run.
	slotRecords := map[[2]string][]domain.TeamLineup{}
	keys := slices.Sorted(maps.Keys(snap.lineups))
	for _, k := range keys {
		l := snap.lineups[k]
		if l.MatchID != "" && (l.TeamID == a || l.TeamID == b) {
			slot := [2]string{l.TeamID, l.MatchID}
			slotRecords[slot] = append(slotRecords[slot], l)
		}
	}

	given := snap.comp.RoundLineupsGiven
	var nextGiven map[string][]string
	for _, team := range []string{a, b} {
		moves := teamMatchMap(seatedMatches(before, team), seatedMatches(after, team))
		moved := false
		for _, from := range slices.Sorted(maps.Keys(moves)) {
			records := slotRecords[[2]string{team, from}]
			if len(records) == 0 {
				continue
			}
			moved = true
			to := moves[from]
			live := slices.ContainsFunc(records, func(l domain.TeamLineup) bool { return !l.IsRemoved() })
			p.lineups.slots = append(p.lineups.slots, lineupSlotWrite{teamID: team, matchID: from})
			if to == "" {
				if live {
					p.answer.LineupsRemoved++
				}
				continue
			}
			if live {
				p.answer.LineupsMoved++
			}
			out := make([]domain.TeamLineup, 0, len(records))
			for _, l := range records {
				l = l.Clone()
				l.MatchID = to
				// A carried lineup names the team's earlier match it carries
				// from; that match moves with the team too.
				if mapped, ok := moves[l.CopiedFrom]; ok && l.CopiedFrom != "" {
					l.CopiedFrom = mapped
				}
				if mapped, ok := moves[l.StartedFrom]; ok && l.StartedFrom != "" {
					l.StartedFrom = mapped
				}
				out = append(out, l)
			}
			p.lineups.slots = append(p.lineups.slots, lineupSlotWrite{teamID: team, matchID: to, records: out})
		}
		if moved {
			p.lineups.teams = append(p.lineups.teams, team)
		}

		// The round-lineup settlement's settled pairs move the same way, so it
		// neither gives the team a lineup twice nor withholds one: a pair whose
		// match has no counterpart goes, and a match of the new place with no
		// counterpart is settled at commit like any newly seated one.
		pairs, ok := given[team]
		if !ok {
			continue
		}
		remapped := make([]string, 0, len(pairs))
		for _, id := range pairs {
			to, mapped := moves[id]
			switch {
			case !mapped:
				remapped = append(remapped, id)
			case to != "":
				remapped = append(remapped, to)
			}
		}
		sort.Strings(remapped)
		remapped = slices.Compact(remapped)
		if slices.Equal(remapped, pairs) {
			continue
		}
		if nextGiven == nil {
			nextGiven = make(map[string][]string, len(given))
			for k, v := range given {
				nextGiven[k] = slices.Clone(v)
			}
		}
		if len(remapped) == 0 {
			delete(nextGiven, team)
		} else {
			nextGiven[team] = remapped
		}
	}
	if nextGiven != nil {
		p.lineups.given, p.lineups.givenChanged = nextGiven, true
	}
}

// saveLineups stages the plan's lineup part in tx, after the draw files: the
// slots, then the competition's settled pairs, then each moved team's carried
// lineups worked out again over the new match order (FollowLineupSave), so a
// carried lineup reads the lineup in force where the team now plays. A
// carried lineup the domain refuses as worked out stays as stored and is
// logged; any other failure fails the swap, so every lineup stays where it
// was.
func (p *swapPlan) saveLineups(e *Engine, tx state.StoreTx, comp *state.Competition) error {
	for _, w := range p.lineups.slots {
		if err := tx.ReplaceTeamLineupSlot(comp.ID, w.teamID, w.matchID, 0, w.records, comp.TeamSize); err != nil {
			return fmt.Errorf("moving team %s's lineup of %s: %w", w.teamID, w.matchID, err)
		}
	}
	if p.lineups.givenChanged {
		next := *comp
		next.RoundLineupsGiven = p.lineups.given
		if err := tx.SaveCompetition(&next); err != nil {
			return fmt.Errorf("saving the settled round-lineup pairs: %w", err)
		}
	}
	for _, team := range p.lineups.teams {
		if _, err := e.FollowLineupSave(tx, comp, domain.TeamLineup{TeamID: team}, nil); err != nil {
			var derived *DerivedLineupError
			if !errors.As(err, &derived) {
				return err
			}
			log.Printf("WARN engine: competition %s: after a draw swap: %v", comp.ID, err)
		}
	}
	return nil
}

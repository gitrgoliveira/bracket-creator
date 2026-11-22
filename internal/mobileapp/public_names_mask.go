package mobileapp

import (
	"crypto/subtle"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The name mask of hidden public names: what one competition's names read as,
// built from its roster, the draw's numbers and its team members (bc-hnam).

// nameMask is what one competition's names read as, built from its roster,
// the draw's numbers and its team members. Immutable once built, so views
// share it across goroutines.
type nameMask struct {
	team bool
	// numbers maps a participant id to its number ("" before the draw).
	numbers map[string]string
	// byNameDojo maps a (name, dojo) key to the one participant id holding
	// it; a key two participants share maps to "".
	byNameDojo map[string]string
	// members maps a member id to its team's participant id.
	members map[string]string
	// memberLabels maps a member id to its label ("T10.1"), "" when the
	// team has no number yet.
	memberLabels map[string]string
	// squads holds each team's members, for the by-name lookups.
	squads map[string][]domain.TeamMember
	// teamDojo maps a team id to its dojo.
	teamDojo map[string]string
	// competitorKeys holds each participant's own reveal key when the
	// competition gives per-person links.
	competitorKeys []competitorKey
}

type competitorKey struct {
	id  string
	key string
}

// buildNameMask builds a competition's mask from its roster (numbers already
// filled from the draw) and its team members.
func buildNameMask(comp *state.Competition, players []domain.Player, squads map[string][]domain.TeamMember) *nameMask {
	m := &nameMask{
		team:         comp.IsTeam(),
		numbers:      make(map[string]string, len(players)),
		byNameDojo:   make(map[string]string, len(players)),
		members:      map[string]string{},
		memberLabels: map[string]string{},
		squads:       squads,
		teamDojo:     map[string]string{},
	}
	perPerson := comp.PublicNames == state.PublicNamesPerson
	for _, p := range players {
		if p.ID == "" {
			continue
		}
		m.numbers[p.ID] = p.Number
		m.teamDojo[p.ID] = p.Dojo
		k := helper.CompetitorKey("", p.Name, p.Dojo)
		if _, dup := m.byNameDojo[k]; dup {
			m.byNameDojo[k] = ""
		} else {
			m.byNameDojo[k] = p.ID
		}
		if perPerson {
			if key := state.CompetitorRevealKey(comp, p.ID); key != "" {
				m.competitorKeys = append(m.competitorKeys, competitorKey{id: p.ID, key: key})
			}
		}
	}
	for teamID, ms := range squads {
		for _, tm := range ms {
			if tm.ID == "" {
				continue
			}
			m.members[tm.ID] = teamID
			m.memberLabels[tm.ID] = domain.SquadMemberLabel(m.numbers[teamID], tm.Index)
		}
	}
	return m
}

// failClosedMask is the mask of a competition whose files could not be
// read: it knows no one, so every hidden name reads "?" (or the number the
// payload itself carries).
func failClosedMask(comp *state.Competition) *nameMask {
	return buildNameMask(comp, nil, nil)
}

// memberTeam is the participant id of the team member memberID belongs to.
func (m *nameMask) memberTeam(memberID string) string { return m.members[memberID] }

// memberIDByName is the id of the one member of teamID named name, or "".
func (m *nameMask) memberIDByName(teamID, name string) string {
	found := ""
	for _, tm := range m.squads[teamID] {
		if tm.Name == name && tm.ID != "" {
			if found != "" {
				return ""
			}
			found = tm.ID
		}
	}
	return found
}

// memberByNameDojo finds the one member named name in a team of dojo (any
// team when dojo is empty), or "", "".
func (m *nameMask) memberByNameDojo(name, dojo string) (teamID, memberID string) {
	for tid, ms := range m.squads {
		if dojo != "" && !strings.EqualFold(strings.TrimSpace(m.teamDojo[tid]), strings.TrimSpace(dojo)) {
			continue
		}
		for _, tm := range ms {
			if tm.Name != name || tm.ID == "" {
				continue
			}
			if memberID != "" {
				return "", ""
			}
			teamID, memberID = tid, tm.ID
		}
	}
	return teamID, memberID
}

// competitorForKey is the participant whose own reveal key is key, compared
// in constant time against every one, or "".
func (m *nameMask) competitorForKey(key string) string {
	if m == nil {
		return ""
	}
	found := ""
	for _, ck := range m.competitorKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(ck.key)) == 1 {
			found = ck.id
		}
	}
	return found
}

package engine

import (
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// This file is the ONE reader of who a kachinuki team fields at a match (bc-kno1,
// operator decision K1 (a) and round bc-kno1-2): the lineup the match fields, its
// empty places filled by member number. Number order is worked out on every read
// and never stored, because carrying a lineup rebuilds every position of a pin
// (carryInto), so a stored fill would be wiped. The advance's queues
// (kachinukiRosterQueues), and through them the roster read, ask it.

// The two sentences the kachinuki roster read (KachinukiRoster.Warning) tells
// the operator when a file it needed could not be read; the read carries on and
// warns (operator decisions Q5 (a) and L1). Both end the same way, saying what
// to do: a side that fields nobody gets no next pairing from Record bout.
const (
	// kachinukiMembersUnreadWarning: team-members.yaml could not be read, so
	// the empty places of a lineup are not filled by member number.
	kachinukiMembersUnreadWarning = "Team members could not be read, so fighters are not seated by member number. Add each bout's pairing by hand."
	// kachinukiLineupsUnreadWarning: lineups.yaml could not be read, so no
	// side fields anyone and no queue is worked out.
	kachinukiLineupsUnreadWarning = "The lineups could not be read, so who fights next is not worked out. Add each bout's pairing by hand."
)

// kachinukiMembers is a LAZY read of a competition's team members: the file is
// read only the first time a fielded lineup leaves an empty place, and once per
// reader, so a match whose lineups name every place pays no load. A nil reader
// knows no members, so it fills nothing.
type kachinukiMembers struct {
	load   func(compID string) (map[string][]domain.TeamMember, error)
	compID string
	caller string

	read   bool
	byTeam map[string][]domain.TeamMember
	unread bool
}

// readKachinukiMembers makes the lazy reader over load (the store's LoadSquads,
// or a transaction's). caller names the reader in the ERROR line a load error
// logs.
func readKachinukiMembers(load func(string) (map[string][]domain.TeamMember, error), compID, caller string) *kachinukiMembers {
	return &kachinukiMembers{load: load, compID: compID, caller: caller}
}

// ensureRead reads the file on first use and reports whether it could be read.
// A file that cannot be read is logged once, as an ERROR naming it, and the
// reader then reports unread. A nil reader knows no members.
func (k *kachinukiMembers) ensureRead() bool {
	if k == nil || k.load == nil {
		return false
	}
	if !k.read {
		k.read = true
		byTeam, err := k.load(k.compID)
		if err != nil {
			log.Printf("engine: ERROR: %s compId=%s: team-members.yaml load error: %v; fighters are not seated by member number", k.caller, k.compID, err)
			k.unread = true
		} else {
			k.byTeam = byTeam
		}
	}
	return !k.unread
}

// forTeam answers the members of the team with participant id teamID, reading
// the file on first use. ok is false when the file could not be read.
func (k *kachinukiMembers) forTeam(teamID string) ([]domain.TeamMember, bool) {
	if !k.ensureRead() {
		return nil, false
	}
	return k.byTeam[teamID], true
}

// all answers every team's members, reading team-members.yaml now if nothing
// has read it yet. A file that cannot be read is logged once, as an ERROR naming
// it (ensureRead), and answers an empty map, so the labels degrade to blank
// rather than failing the export. A missing file is already "no members" from
// LoadSquads itself (state.parseSquadsFile's contract).
func (k *kachinukiMembers) all() map[string][]domain.TeamMember {
	if !k.ensureRead() {
		return map[string][]domain.TeamMember{}
	}
	return k.byTeam
}

// warning is the sentence to tell the operator, empty unless a read was needed
// and the file could not be read.
func (k *kachinukiMembers) warning() string {
	if k != nil && k.unread {
		return kachinukiMembersUnreadWarning
	}
	return ""
}

// inForceReader is what kachinukiFielded asks of the lineups: the lineup in
// force for a team at a match, and whether lineups.yaml could not be read (then
// nothing is filled by number, since a lineup may exist that was not read).
type inForceReader interface {
	inForce(teamID, matchID string) InForceLineup
	lineupsUnread() bool
}

// kachinukiLineups is the lineup rule a kachinuki read fields from, with whether
// lineups.yaml could not be read. A rule built over lineups that were read has
// unread false.
type kachinukiLineups struct {
	*lineupRule
	unread bool
}

func (r kachinukiLineups) lineupsUnread() bool { return r.unread }

// readKachinukiLineups builds the lineup rule for a kachinuki read that holds
// the pool matches and the bracket and keeps working without lineups (the
// advance's queues and the Kachinuki Detail export). knockout is whether the
// competition plays a knockout stage. A lineups.yaml that cannot be read is
// logged as an ERROR naming the file and caller, and answers the rule over no
// lineup marked unread, so nobody is fielded and no fighter is seated by member
// number over a lineup that may exist.
func (e *Engine) readKachinukiLineups(caller, compID string, knockout bool, poolMatches []state.MatchResult, bracket *state.Bracket) kachinukiLineups {
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		return unreadKachinukiLineups(caller, compID, err)
	}
	return kachinukiLineups{lineupRule: newLineupRuleFrom(lineups, poolMatches, bracket, knockout)}
}

// unreadKachinukiLineups is the rule a kachinuki read fields from when
// lineups.yaml could not be read (err): it logs the ERROR naming the file and
// caller, and answers no lineup marked unread. The one shape for every reader
// (readKachinukiLineups, and KachinukiRoster over its cached read).
func unreadKachinukiLineups(caller, compID string, err error) kachinukiLineups {
	log.Printf("engine: ERROR: %s compId=%s: lineups.yaml load error: %v; resolving no lineup, so fighters are not seated by member number", caller, compID, err)
	return kachinukiLineups{lineupRule: &lineupRule{}, unread: true}
}

// kachinukiFielded is the lineup match m fields for the team with participant id
// teamID: the match's own lineup (an operator's save, or the one it started
// with, its pin); else, while the match has not started (hasStarted), the
// lineup in force there by the carry rule; else an empty lineup (a match that
// started with nothing of its own never follows a carried lineup, K1 (c)). Its
// empty places are then filled by member number
// (domain.TeamLineup.FilledByMemberNumber: members 1..teamSize, named or not,
// the lowest number not placed elsewhere). The fill is skipped when lineups.yaml
// could not be read or the team members could not be read (Q5). An empty teamID,
// a side with no id, fields nobody. The answer may share maps with a lineup the
// rule holds, so it must not be modified.
func kachinukiFielded(rule inForceReader, members *kachinukiMembers, teamSize int, m *state.MatchResult, teamID string) domain.TeamLineup {
	if teamID == "" || m == nil {
		return domain.TeamLineup{}
	}
	var lineup domain.TeamLineup
	if in := rule.inForce(teamID, m.ID); in.Found && (in.Source.MatchID == m.ID || !hasStarted(m)) {
		lineup = in.Lineup
	}
	if rule.lineupsUnread() || len(lineup.OrderedMembers(teamSize)) >= teamSize {
		return lineup
	}
	teamMembers, ok := members.forTeam(teamID)
	if !ok {
		return lineup
	}
	return lineup.FilledByMemberNumber(teamSize, teamMembers)
}

// formInForce answers the lineup form of one team at one match as that match's
// own lineup, and every other question as the rule it wraps does, so
// kachinukiFielded reads the form exactly as it would read the lineup once the
// form is saved.
type formInForce struct {
	inForceReader
	teamID, matchID string
	form            domain.TeamLineup
}

func (f formInForce) inForce(teamID, matchID string) InForceLineup {
	if teamID == f.teamID && matchID == f.matchID {
		return InForceLineup{Lineup: f.form, Source: LineupSource{MatchID: matchID}, Found: true}
	}
	return f.inForceReader.inForce(teamID, matchID)
}

// KachinukiFieldedPlacesBeforeDraw is KachinukiFieldedPlaces as the same
// competition answers in setup, where no match is held: a competition that is
// not kachinuki fills nothing, and a kachinuki one still refuses a form
// position its team size has no place for, then fills nothing for the match it
// does not hold. A public read of a draft whose draw is hidden from its caller
// answers it (bc-drpb), so the hidden draw shows in no difference.
func (e *Engine) KachinukiFieldedPlacesBeforeDraw(compID string, form domain.TeamLineup) (map[domain.Position]string, error) {
	_, places, err := e.kachinukiFieldedSetup(compID, form)
	return places, err
}

// kachinukiFieldedSetup is the part of the fielded read that needs no match:
// the competition (an unknown one is a NotFoundError), the empty map every
// answer without a place to fill gives, and, for a kachinuki competition, the
// refusal of a form position its team size has no place for. The map is nil
// with an error.
func (e *Engine) kachinukiFieldedSetup(compID string, form domain.TeamLineup) (*state.Competition, map[domain.Position]string, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return nil, nil, err
	}
	if comp == nil {
		return nil, nil, notFoundErrorf("competition %s not found", compID)
	}
	if comp.IsKachinuki() {
		if err := form.ValidatePositions(comp.TeamSize); err != nil {
			return nil, nil, err
		}
	}
	return comp, map[domain.Position]string{}, nil
}

// KachinukiFieldedPlaces answers, for the lineup panel (bc-kno1, operator
// decisions D2 and D3), the places of form that the server would fill by member
// number if form were the lineup of the team with participant id teamID at
// match matchID: each place form leaves empty (no name and no member id) mapped
// to the id of the member kachinukiFielded seats there. The form is the panel's
// current positions, saved or not; nothing is written.
//
// It answers an EMPTY map, never nil, for a competition that is not kachinuki,
// an unknown match, a team that is not a side of the match (checked by id),
// a form with no empty place, and a team-members.yaml that cannot be read
// (logged as an ERROR naming the file, as every kachinuki reader logs it). A
// form position the team size has no place for is the domain's own refusal
// (domain.TeamLineup.ValidatePositions). An unknown competition is a
// NotFoundError. A lineups.yaml that cannot be read, or a draw file the match
// lookup cannot read, is returned as an error: over lineups it could not read
// the server fills nothing, so an answer would promise a fill it never makes.
func (e *Engine) KachinukiFieldedPlaces(compID, teamID, matchID string, form domain.TeamLineup) (map[domain.Position]string, error) {
	comp, places, err := e.kachinukiFieldedSetup(compID, form)
	if err != nil || !comp.IsKachinuki() {
		return places, err
	}
	located, err := e.findTeamMatch(compID, matchID)
	if err != nil {
		return nil, err
	}
	if located == nil || located.Result == nil {
		return places, nil
	}
	m := located.Result
	if teamID == "" || (teamID != m.SideAID && teamID != m.SideBID) {
		return places, nil
	}
	members := readKachinukiMembers(e.store.LoadSquads, compID, "engine.KachinukiFieldedPlaces")
	err = e.readLineupRule(compID, comp.IsKnockoutEnabled(), located.PoolMatches, located.Bracket, func(rule *lineupRule) {
		reader := formInForce{inForceReader: kachinukiLineups{lineupRule: rule}, teamID: teamID, matchID: m.ID, form: form}
		for pos, id := range kachinukiFielded(reader, members, comp.TeamSize, m, teamID).MemberIDs {
			if id != "" && form.Positions[pos] == "" && form.MemberIDs[pos] == "" {
				places[pos] = id
			}
		}
	})
	if err != nil {
		return nil, err
	}
	return places, nil
}

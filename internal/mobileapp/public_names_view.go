package mobileapp

import (
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The mask view of hidden public names: a payload copy rewritten so each hidden
// name reads as the competitor's number or the member's label (bc-hnam).

// maskView answers for one competition and one audience. The zero view (the
// organiser, a competition link's holder, or a competition that shows names)
// changes nothing; a view with a mask rewrites the copies it is given.
type maskView struct {
	m *nameMask
	// reveal holds the participant ids the caller's own links reveal.
	reveal map[string]bool
}

// namesHidden reports whether payloads through this view are masked, the
// value of their namesHidden marker.
func (v maskView) namesHidden() bool { return v.m != nil }

// namesHiddenMark is embedded in a payload struct to carry the namesHidden
// marker: true on every masked payload, so the public pages can tell a number
// shown in a name's place from a name.
type namesHiddenMark struct {
	NamesHidden bool `json:"namesHidden,omitempty"`
}

// mark is the namesHidden marker for a payload built through v.
func (v maskView) mark() namesHiddenMark { return namesHiddenMark{NamesHidden: v.namesHidden()} }

// revealed reports whether the caller's own links reveal participant id.
func (v maskView) revealed(id string) bool { return id != "" && v.reveal[id] }

// side is what a match side (or winner) named name with participant id reads
// as: the name for a team (team names stay) or a revealed competitor, the
// competitor's number otherwise. An id with no number, or a name with no id,
// reads "?". An empty side (a bye) and the draw's placeholders ("Winner of
// ...", "Pool A-1st") are not competitors and stay as they are.
func (v maskView) side(name, id string) string {
	reading, _ := v.sideReading(name, id)
	return reading
}

// sideReading is side's answer and whether the side was masked at all, so a
// caller never has to compare the reading with the name to tell: a real name
// that happens to equal its own mask ("?" or the competitor's number) reads
// the same either way.
func (v maskView) sideReading(name, id string) (string, bool) {
	if v.m == nil || v.m.team || v.revealed(id) {
		return name, false
	}
	if id != "" {
		if n := v.m.numbers[id]; n != "" {
			return n, true
		}
		return maskedUnknown, true
	}
	if name == "" || helper.IsWinnerOfPlaceholder(name) || helper.IsPoolFinalistPlaceholder(name) {
		return name, false
	}
	return maskedUnknown, true
}

// players masks a roster copy in place. An individual's Name and DisplayName
// (zekken name) read as the number, "?" (maskedUnknown) before the draw gives
// one, as a side does; a revealed competitor's stay. A team keeps its name
// and loses Metadata, where a legacy roster held member names.
func (v maskView) players(ps []domain.Player) {
	if v.m == nil {
		return
	}
	for i := range ps {
		v.player(&ps[i])
	}
}

func (v maskView) player(p *domain.Player) {
	if v.m == nil {
		return
	}
	if v.m.team {
		if !v.revealed(p.ID) {
			p.Metadata = nil
		}
		return
	}
	if v.revealed(p.ID) {
		return
	}
	n := v.m.numbers[p.ID]
	if p.Number != "" {
		n = p.Number
	}
	if n == "" {
		n = maskedUnknown
	}
	p.Name = n
	p.DisplayName = n
}

// standings masks standings rows in place (their Player, as players).
func (v maskView) standings(rows []state.PlayerStanding) {
	if v.m == nil {
		return
	}
	for i := range rows {
		v.player(&rows[i].Player)
	}
}

// pools masks pools in place: each pool's players and the players of its
// pairings. A pairing's sides are pointers, so each is replaced by a masked
// copy rather than written through.
func (v maskView) pools(ps []helper.Pool) {
	if v.m == nil {
		return
	}
	for i := range ps {
		v.players(ps[i].Players)
		pairings := ps[i].Matches
		for j := range pairings {
			pm := &pairings[j]
			pm.SideA = v.playerCopy(pm.SideA)
			pm.SideB = v.playerCopy(pm.SideB)
		}
	}
}

func (v maskView) playerCopy(p *domain.Player) *domain.Player {
	if p == nil {
		return nil
	}
	cp := *p
	cp.Metadata = append([]string(nil), p.Metadata...)
	v.player(&cp)
	return &cp
}

// matches masks pool or league matches in place. Pass a copy
// (LoadPoolMatches gives one); bout rows are cloned before they are changed.
func (v maskView) matches(ms []state.MatchResult) {
	if v.m == nil {
		return
	}
	for i := range ms {
		v.match(&ms[i])
	}
}

// match masks one pool or league match in place: sides and winner, the
// representative-bout fighters and the bout rows. The team summary (IV/PW) is
// fixed first, from the names as stored, so the public one is the organiser's.
func (v maskView) match(m *state.MatchResult) {
	if v.m == nil || m == nil {
		return
	}
	m.HoldTeamResult()
	teamA, teamB := m.SideA, m.SideB
	m.SubResults = v.bouts(m.SubResults, m.SideAID, m.SideBID, teamA, teamB)
	m.RepPlayerA = v.memberByName(m.SideAID, m.RepPlayerA)
	m.RepPlayerB = v.memberByName(m.SideBID, m.RepPlayerB)
	m.SideA = v.side(m.SideA, m.SideAID)
	m.SideB = v.side(m.SideB, m.SideBID)
	m.Winner = v.side(m.Winner, m.WinnerID)
}

// bracket masks a bracket in place: every round and the bronze match. Pass
// a copy (LoadBracket gives one).
func (v maskView) bracket(b *state.Bracket) {
	if v.m == nil || b == nil {
		return
	}
	for ri := range b.Rounds {
		for j := range b.Rounds[ri] {
			v.bracketMatch(&b.Rounds[ri][j])
		}
	}
	if b.ThirdPlaceMatch != nil {
		bronze := *b.ThirdPlaceMatch
		v.bracketMatch(&bronze)
		b.ThirdPlaceMatch = &bronze
	}
}

// bracketMatch masks one knockout match in place.
func (v maskView) bracketMatch(m *state.BracketMatch) {
	if v.m == nil || m == nil {
		return
	}
	m.HoldTeamResult()
	m.SubResults = v.bouts(m.SubResults, m.SideAID, m.SideBID, m.SideA, m.SideB)
	m.Winner = v.bracketWinner(m)
	m.SideA = v.side(m.SideA, m.SideAID)
	m.SideB = v.side(m.SideB, m.SideBID)
}

// bracketWinner is what a knockout match's winner reads as, from the names as
// stored. A winner with an id reads as side reads it. A winner with a name and
// no id is a legacy row a repair has not reached, one of the id-less shapes a
// BracketMatch may hold: it reads as the side whose stored name it is, so the
// public bracket shows that side's number rather than "?". A name both sides
// share, or neither holds, still reads "?".
func (v maskView) bracketWinner(m *state.BracketMatch) string {
	if m.WinnerID == "" && m.Winner != "" && m.SideA != m.SideB {
		switch m.Winner {
		case m.SideA:
			return v.side(m.SideA, m.SideAID)
		case m.SideB:
			return v.side(m.SideB, m.SideBID)
		}
	}
	return v.side(m.Winner, m.WinnerID)
}

// bouts is a masked copy of a team match's bout rows. teamAID/teamBID are
// the match's team ids, teamA/teamB its team names (which stay: a quick-score
// row names the two teams).
func (v maskView) bouts(subs []state.SubMatchResult, teamAID, teamBID, teamA, teamB string) []state.SubMatchResult {
	if v.m == nil || len(subs) == 0 {
		return subs
	}
	out := make([]state.SubMatchResult, 0, len(subs))
	for _, s := range state.CloneSubResults(subs) {
		orig := s
		s.SideA = v.member(teamAID, s.SideAMemberID, orig.SideA, teamA)
		s.SideB = v.member(teamBID, s.SideBMemberID, orig.SideB, teamB)
		switch {
		case orig.Winner == "" || orig.Winner == teamA || orig.Winner == teamB:
		case orig.WinnerMemberID != "":
			s.Winner = v.member(v.m.memberTeam(orig.WinnerMemberID), orig.WinnerMemberID, orig.Winner, "")
		default:
			switch state.SubBoutWinnerSide(orig, teamA, teamB) {
			case domain.MatchSideA:
				s.Winner = s.SideA
			case domain.MatchSideB:
				s.Winner = s.SideB
			default:
				s.Winner = maskedUnknown
			}
		}
		out = append(out, s)
	}
	return out
}

// member is what a team member's name reads as: the name when the member's
// team is revealed (E7) or when no member id is recorded and it is the team's
// own name (a legacy side that holds the team, whose name stays), the
// member's label ("T10.1") otherwise, or "?" when the member is not known by
// id. A member with an id is masked even when their name equals the team's.
// An empty name stays empty.
func (v maskView) member(teamID, memberID, name, teamName string) string {
	if v.m == nil || name == "" {
		return name
	}
	if memberID == "" && teamName != "" && name == teamName {
		return name
	}
	if memberID != "" {
		if owner := v.m.memberTeam(memberID); owner != "" {
			teamID = owner
		}
	}
	if v.revealed(teamID) {
		return name
	}
	if memberID == "" {
		return maskedUnknown
	}
	if l := v.m.memberLabels[memberID]; l != "" {
		return l
	}
	return maskedUnknown
}

// memberByName is a representative-bout fighter's masked name: the fighter
// is recorded by name alone (MatchResult.RepPlayerA/B carry no id), so the
// name is looked up among that team's own members, and reads "?" unless it
// names exactly one of them.
func (v maskView) memberByName(teamID, name string) string {
	if v.m == nil || name == "" || v.revealed(teamID) {
		return name
	}
	return v.member(teamID, v.m.memberIDByName(teamID, name), name, "")
}

// teamMembers is a masked copy of one team's members: each named member's
// name reads as their label, unless the team is revealed.
func (v maskView) teamMembers(teamID string, ms []domain.TeamMember) []domain.TeamMember {
	if v.m == nil || ms == nil {
		return ms
	}
	out := append([]domain.TeamMember(nil), ms...)
	for i := range out {
		out[i].Name = v.member(teamID, out[i].ID, out[i].Name, "")
	}
	return out
}

// squads is a masked copy of every team's members, keyed by team id.
func (v maskView) squads(sq map[string][]domain.TeamMember) map[string][]domain.TeamMember {
	if v.m == nil || sq == nil {
		return sq
	}
	out := make(map[string][]domain.TeamMember, len(sq))
	for teamID, ms := range sq {
		out[teamID] = v.teamMembers(teamID, ms)
	}
	return out
}

// lineup masks a lineup in place: each position's name reads as its
// member's label. The Positions map is replaced, never written through.
func (v maskView) lineup(l *domain.TeamLineup) {
	if v.m == nil || l == nil || len(l.Positions) == 0 {
		return
	}
	positions := make(map[domain.Position]string, len(l.Positions))
	for pos, name := range l.Positions {
		positions[pos] = v.member(l.TeamID, l.MemberIDs[pos], name, "")
	}
	l.Positions = positions
}

// awards is a masked copy of the fighting-spirit awards. A recipient is
// recorded by name and dojo alone (FightingSpiritAward carries no id), so it
// is matched to the roster by (name, dojo), or in a team competition to a
// member by name within a team of that dojo, and reads "?" unless exactly one
// matches.
func (v maskView) awards(as []state.FightingSpiritAward) []state.FightingSpiritAward {
	if v.m == nil || as == nil {
		return as
	}
	out := append([]state.FightingSpiritAward(nil), as...)
	for i := range out {
		out[i].RecipientName = v.awardRecipient(out[i].RecipientName, out[i].RecipientDojo)
	}
	return out
}

func (v maskView) awardRecipient(name, dojo string) string {
	if name == "" {
		return name
	}
	if v.m.team {
		teamID, memberID := v.m.memberByNameDojo(name, dojo)
		if teamID == "" {
			return maskedUnknown
		}
		return v.member(teamID, memberID, name, "")
	}
	id := v.m.byNameDojo[helper.CompetitorKey("", name, dojo)]
	if id == "" {
		return maskedUnknown
	}
	return v.side(name, id)
}

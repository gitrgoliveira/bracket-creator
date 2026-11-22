package mobileapp

import (
	"crypto/sha256"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Hiding competitor names from the public pages (bc-hnam, bc-anon).
//
// Two halves, both server-side so a name a spectator may not see never leaves
// the server:
//
//   - The AUDIENCE JUDGE (publicNames.judge) decides, per request, who a public
//     read is answered for: the organiser (a valid X-Tournament-Password), a
//     reveal scope (valid X-Reveal-Key entries: a competition's link, which
//     reveals every name in it, or competitors' own links, which reveal those
//     competitors alone), or the public. A password sent but wrong is answered
//     401, never the masked view. A reveal key unlocks names on reads only: it
//     never passes AuthMiddleware, so it can never reach a write or an
//     organiser-only read.
//   - The NAME MASK (nameMask, through maskView) rewrites a COPY of a payload
//     so each hidden name reads as the competitor's number. It is built per
//     competition from the roster, the draw's numbers and the team members,
//     and cached until one of those files (or the competition's config)
//     changes. A named side with no id fails closed as "?".
//
// What a masked payload keeps: ids, numbers, dojos (Q3 b) and team names
// (Q-teams); "Winner of ..." and "Pool A-1st" placeholders. What it replaces:
// an individual's name and zekken name (an engi pair is one participant),
// match sides and winners, team members' names (with the member's label, the
// team's number and the member's index, "T10.1"), lineup positions,
// representative-bout fighters and fighting-spirit recipients.

// revealKeyHeader carries the reveal keys a phone holds:
// "<compId>:<key>[,<compId>:<key>...]".
const revealKeyHeader = "X-Reveal-Key"

// maskedUnknown is what a hidden name reads as when the mask cannot say which
// competitor it is (a named side with no id, an id with no number): fail
// closed rather than show the name.
const maskedUnknown = "?"

// Bounds on the X-Reveal-Key header. A phone holds a few keys (one per
// competition, or one per child it follows); anything past these is dropped
// unread, so a long header costs nothing.
const (
	maxRevealHeaderBytes = 8 << 10
	maxRevealKeys        = 32
	revealKeyHexLen      = 2 * sha256.Size
)

// Audience classes, the first part of an audience's key.
const (
	audienceOrganiser = "organiser"
	audiencePublic    = "public"
	audienceReveal    = "reveal"
)

// audience is who a public read is answered for. The zero value is the public.
type audience struct {
	organiser bool
	// competitions holds the competitions whose competition link the caller
	// holds: every name in them is revealed.
	competitions map[string]bool
	// competitors holds, per competition, the participant ids whose own link
	// the caller holds: those competitors' names are revealed (a team's link
	// reveals its members' names, E7).
	competitors map[string]map[string]bool
}

// organiserAudience is the signed-in organiser's audience.
func organiserAudience() audience { return audience{organiser: true} }

// isOrganiser reports whether the caller is the signed-in organiser.
func (a audience) isOrganiser() bool { return a.organiser }

// revealsAll reports whether the caller sees every name in compID: the
// organiser, or the holder of that competition's link.
func (a audience) revealsAll(compID string) bool {
	return a.organiser || a.competitions[compID]
}

// revealedCompetitors is the set of participant ids in compID whose own link
// the caller holds (nil when none). Read only.
func (a audience) revealedCompetitors(compID string) map[string]bool {
	return a.competitors[compID]
}

// key names the whole audience, per-competitor links included, sorted: two
// callers with the same key are answered the same bytes, so it is the part a
// single-flight key adds.
func (a audience) key() string {
	if a.organiser {
		return audienceOrganiser
	}
	parts := a.competitionParts()
	compIDs := make([]string, 0, len(a.competitors))
	for id, set := range a.competitors {
		if len(set) > 0 {
			compIDs = append(compIDs, id)
		}
	}
	sort.Strings(compIDs)
	for _, id := range compIDs {
		pids := make([]string, 0, len(a.competitors[id]))
		for pid := range a.competitors[id] {
			pids = append(pids, pid)
		}
		sort.Strings(pids)
		parts = append(parts, "p:"+id+"="+strings.Join(pids, "+"))
	}
	if len(parts) == 0 {
		return audiencePublic
	}
	return audienceReveal + "|" + strings.Join(parts, ",")
}

func (a audience) competitionParts() []string {
	parts := make([]string, 0, len(a.competitions))
	for id, ok := range a.competitions {
		if ok {
			parts = append(parts, "c:"+id)
		}
	}
	sort.Strings(parts)
	return parts
}

// publicNames owns the audience judge and the per-competition name masks for
// one router: one instance is shared by every public read and the event
// stream, so a mask is built once for all of them.
type publicNames struct {
	store    *state.Store
	verifier PasswordVerifier
	masks    *nameMaskCache
	now      func() time.Time
}

// newPublicNames builds the judge and mask cache. In locked mode the verifier
// is wrapped with a digest cache (cachedVerifier), because a bcrypt
// comparison on every public read would cost far more than the read.
func newPublicNames(store *state.Store, verifier PasswordVerifier) *publicNames {
	if verifier == nil {
		verifier = NewFileVerifier(store)
	}
	if cacheableVerifier(verifier) {
		verifier = newCachedVerifier(verifier, time.Now)
	}
	return &publicNames{
		store:    store,
		verifier: verifier,
		masks:    newNameMaskCache(store),
		now:      time.Now,
	}
}

// judge works out who this read is answered for. Every answer it judges
// varies by audience, so it is marked no-store (and Vary on both
// credentials) before anything else is written. ok is false when the
// response has been written: 401 for a password sent but wrong, 500 when the
// password could not be checked.
//
// Reveal keys are judged only for a caller that is not the organiser. An
// invalid, expired or wrong-kind key is dropped here, before any key is
// formed from the audience, so it folds into the public.
func (p *publicNames) judge(c *gin.Context) (audience, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "X-Tournament-Password, "+revealKeyHeader)

	if password := c.GetHeader("X-Tournament-Password"); password != "" {
		valid, err := p.verifier.Verify(password)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
			return audience{}, false
		}
		if !valid {
			respondInvalidPassword(c)
			return audience{}, false
		}
		return organiserAudience(), true
	}
	return p.revealAudience(c.GetHeader(revealKeyHeader)), true
}

// revealAudience judges an X-Reveal-Key header. Only a key that reveals
// something right now counts: its competition hides names, its links are
// open (state.RevealOpen: the kind gives links and the expiry has not
// passed), and the key is the competition's key (a competition link) or one
// of its competitors' keys (per-person links), compared in constant time.
// Anything else is dropped without a word.
func (p *publicNames) revealAudience(header string) audience {
	entries := parseRevealKeys(header)
	if len(entries) == 0 {
		return audience{}
	}
	// A missing tournament (nil, no tournament.md yet) is no refusal: a
	// competition's own RevealUntil still opens its links (state.RevealOpen).
	t, err := p.store.LoadTournament()
	if err != nil {
		return audience{}
	}
	now := p.now()
	var aud audience
	for _, e := range entries {
		comp, err := p.store.LoadCompetition(e.compID)
		if err != nil || comp == nil || !comp.HidesNames() || !state.RevealOpen(comp, t, now) {
			continue
		}
		switch comp.PublicNames {
		case state.PublicNamesCompetition:
			want := state.CompetitionRevealKey(comp)
			if want != "" && subtle.ConstantTimeCompare([]byte(e.key), []byte(want)) == 1 {
				if aud.competitions == nil {
					aud.competitions = map[string]bool{}
				}
				aud.competitions[comp.ID] = true
			}
		case state.PublicNamesPerson:
			m, err := p.masks.get(comp.ID)
			if err != nil {
				// The mask fails closed and knows no key: this key
				// reveals nothing until the files read again.
				slog.Warn("mobile-app: reveal key not judged", "competition", comp.ID, "err", err)
			}
			if pid := m.competitorForKey(e.key); pid != "" {
				if aud.competitors == nil {
					aud.competitors = map[string]map[string]bool{}
				}
				if aud.competitors[comp.ID] == nil {
					aud.competitors[comp.ID] = map[string]bool{}
				}
				aud.competitors[comp.ID][pid] = true
			}
		}
	}
	return aud
}

// revealKeyEntry is one "<compId>:<key>" entry of an X-Reveal-Key header.
type revealKeyEntry struct {
	compID string
	key    string
}

// parseRevealKeys reads an X-Reveal-Key header into its well-formed entries:
// a valid competition id, a colon and a 64-character lowercase hex key. A
// header past maxRevealHeaderBytes is ignored whole; entries past
// maxRevealKeys, malformed entries and repeats are dropped.
func parseRevealKeys(header string) []revealKeyEntry {
	header = strings.TrimSpace(header)
	if header == "" || len(header) > maxRevealHeaderBytes {
		return nil
	}
	var out []revealKeyEntry
	seen := map[revealKeyEntry]bool{}
	for _, raw := range strings.Split(header, ",") {
		if len(out) == maxRevealKeys {
			break
		}
		compID, key, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok || state.ValidateCompetitionID(compID) != nil || !isLowerHex(key, revealKeyHexLen) {
			continue
		}
		e := revealKeyEntry{compID: compID, key: key}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// canonicalRevealKeys is a header's well-formed entries alone, joined as
// the header writes them, or "" when it has none. What an event-stream
// ticket keeps, so a malformed header earns no ticket and a stored one never
// holds more than maxRevealKeys entries.
func canonicalRevealKeys(header string) string {
	entries := parseRevealKeys(header)
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.compID + ":" + e.key
	}
	return strings.Join(parts, ",")
}

// isLowerHex reports whether s is exactly n lowercase hex characters, the
// shape every derived reveal key has.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

// maskView answers for one competition and one audience. The zero view (the
// organiser, a competition link's holder, or a competition that shows names)
// changes nothing; a view with a mask rewrites the copies it is given.
type maskView struct {
	m *nameMask
	// reveal holds the participant ids the caller's own links reveal.
	reveal map[string]bool
}

// hides reports whether the public pages hide comp's names right now: its
// setting hides them and the tournament is not self-run. A self-run
// tournament always shows names, whatever a competition's setting says (a
// restore can leave one that hides them), because its competitors score their
// own matches and need them; the competition keeps its setting, so it hides
// names again if the tournament is switched back to officiated. A tournament
// that cannot be read counts as officiated, so the names stay hidden.
func (p *publicNames) hides(comp *state.Competition) bool {
	if comp == nil || !comp.HidesNames() {
		return false
	}
	t, err := p.store.LoadTournament()
	return err != nil || !t.IsSelfRun()
}

// view is the mask view for comp and aud. The error is the mask's build
// error (a file that could not be read); the view is still usable and fails
// closed, so a caller logs the error and serves the view.
func (p *publicNames) view(comp *state.Competition, aud audience) (maskView, error) {
	if !p.hides(comp) || aud.revealsAll(comp.ID) {
		return maskView{}, nil
	}
	m, err := p.masks.get(comp.ID)
	return maskView{m: m, reveal: aud.revealedCompetitors(comp.ID)}, err
}

// namesHidden reports whether payloads through this view are masked, the
// value of their namesHidden marker.
func (v maskView) namesHidden() bool { return v.m != nil }

// namesHiddenMark is embedded in a payload struct to carry the namesHidden
// marker: true on every masked payload, so an organiser surface can refuse to
// save a masked label as a name.
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
// (zekken name) read as the number, empty before the draw gives one; a
// revealed competitor's stay. A team keeps its name and loses Metadata, where
// a legacy roster held member names.
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
// representative-bout fighters and the bout rows.
func (v maskView) match(m *state.MatchResult) {
	if v.m == nil || m == nil {
		return
	}
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
	m.SubResults = v.bouts(m.SubResults, m.SideAID, m.SideBID, m.SideA, m.SideB)
	m.SideA = v.side(m.SideA, m.SideAID)
	m.SideB = v.side(m.SideB, m.SideBID)
	m.Winner = v.side(m.Winner, m.WinnerID)
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
// team is revealed (E7) or when it is the team's own name (team names stay),
// the member's label ("T10.1") otherwise, or "?" when the member is not known
// by id. An empty name stays empty.
func (v maskView) member(teamID, memberID, name, teamName string) string {
	if v.m == nil || name == "" {
		return name
	}
	if teamName != "" && name == teamName {
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

// nameMaskFiles are the competition files a mask is built from; a write to
// any of them invalidates it.
var nameMaskFiles = [...]string{"config.md", "participants.csv", "pools.csv", "bracket.json", "team-members.yaml"}

type nameMaskToken [len(nameMaskFiles)]struct {
	mtime   int64
	version uint64
}

type nameMaskEntry struct {
	token nameMaskToken
	mask  *nameMask
}

// nameMaskCache holds one mask per competition, valid while the files it was
// built from are unchanged (Store.FileToken: the in-process write counter and
// the mtime, for an edit made outside the app).
type nameMaskCache struct {
	store   *state.Store
	mu      sync.Mutex
	entries map[string]nameMaskEntry
}

func newNameMaskCache(store *state.Store) *nameMaskCache {
	return &nameMaskCache{store: store, entries: map[string]nameMaskEntry{}}
}

func (c *nameMaskCache) token(compID string) nameMaskToken {
	var t nameMaskToken
	for i, f := range nameMaskFiles {
		t[i].mtime, t[i].version = c.store.FileToken(compID, f)
	}
	return t
}

// get is compID's mask, built when the cached one is out of date. The token
// is sampled BEFORE the files are read, so a write landing during the build
// leaves an entry whose token is already stale and is rebuilt next time. On a
// read error the mask fails closed (failClosedMask, or what could be read)
// and is not cached, so the next read tries again.
func (c *nameMaskCache) get(compID string) (*nameMask, error) {
	tok := c.token(compID)
	c.mu.Lock()
	e, ok := c.entries[compID]
	c.mu.Unlock()
	if ok && e.token == tok {
		return e.mask, nil
	}
	m, err := c.build(compID)
	if err != nil {
		return m, err
	}
	c.mu.Lock()
	c.entries[compID] = nameMaskEntry{token: tok, mask: m}
	c.mu.Unlock()
	return m, nil
}

func (c *nameMaskCache) build(compID string) (*nameMask, error) {
	comp, err := c.store.LoadCompetition(compID)
	if err != nil || comp == nil {
		if err == nil {
			err = fmt.Errorf("competition %q not found", compID)
		}
		return failClosedMask(&state.Competition{ID: compID}), fmt.Errorf("name mask: %w", err)
	}
	players, err := c.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		return failClosedMask(comp), fmt.Errorf("name mask: participants: %w", err)
	}
	var bracket *state.Bracket
	if needsBracket, ok := numberingApplies(comp); ok && needsBracket {
		if bracket, err = c.store.LoadBracket(compID); err != nil {
			return failClosedMask(comp), fmt.Errorf("name mask: bracket: %w", err)
		}
	}
	if err := numbersFromDrawWithBracket(c.store, comp, players, bracket); err != nil {
		return failClosedMask(comp), fmt.Errorf("name mask: numbers: %w", err)
	}
	var squads map[string][]domain.TeamMember
	if comp.IsTeam() {
		if squads, err = c.store.LoadSquads(compID); err != nil {
			// Members unknown: every member name reads "?".
			return buildNameMask(comp, players, nil), fmt.Errorf("name mask: team members: %w", err)
		}
	}
	return buildNameMask(comp, players, squads), nil
}

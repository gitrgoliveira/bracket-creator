package engine

import (
	"slices"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's swap suggestions (bc-drev): swaps of two unseeded
// competitors that keep dojo-mates apart for longer. Nothing is stored; the
// answer is recomputed from the stored draw on every read, like the dojo
// separation summary it improves on.
//
// A suggestion never trades one dojo's pairing for another's: it is made only
// when no dojo is worse off after it and at least one is better off, each
// dojo judged as a whole (judgeDojos). Seeded competitors are never suggested:
// seeds keep their places in the draw (SwapSeeded). With every seed in place
// the bracket keeps its shape, so a swap changes only the meetings of the
// pairs that hold one of the two competitors, and each candidate is scored on
// those pairs alone.

// maxSwapSuggestions is how many suggestions an answer holds at most.
const maxSwapSuggestions = 5

// SuggestedSide is one competitor of a suggested swap and the place they hold
// now. ID and Place are what the swap request takes; the rest names them.
type SuggestedSide struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName,omitempty"`
	Number      string    `json:"number,omitempty"`
	Dojo        string    `json:"dojo,omitempty"`
	Place       DrawPlace `json:"place"`
}

// SwapImprovement is one pair of dojo-mates a suggestion keeps apart for
// longer: their summary row before the swap and the row the summary would
// show after it.
type SwapImprovement struct {
	Dojo   string      `json:"dojo"`
	Before DojoMeeting `json:"before"`
	After  DojoMeeting `json:"after"`
}

// SwapSuggestion is one suggested swap. A is the earlier of the two in the
// draw. Improves lists every pair that meets later after the swap, the
// earliest meeting first; every other pair meets where it did. Byes lists the
// first-round byes the swap hands over, as the swap's own answer does, so the
// card can say so before the organiser asks for the dry run.
type SwapSuggestion struct {
	A        SuggestedSide     `json:"a"`
	B        SuggestedSide     `json:"b"`
	Improves []SwapImprovement `json:"improves"`
	Byes     []SwapBye         `json:"byes,omitempty"`
}

// SwapSuggestions is the answer of GET .../draw/swap-suggestions.
type SwapSuggestions struct {
	Suggestions []SwapSuggestion `json:"suggestions"`
}

// meetingRank says how late a pair of dojo-mates can first meet, larger is
// later: depth is how far from the end of the bracket the earliest meeting
// is, counted down to 0 for the final (-1 the semi-finals, and so on, by the
// match's display round when it carries one, as the bracket names it), below
// every round for a shared pool and above every round when they cannot meet;
// winners is the same for the if-both-win meeting, which breaks a tie. A
// pair with an earliest meeting and no if-both-win one (a knockout-only draw,
// or a pool whose winner the first round does not hold) breaks no tie:
// winners is its depth. This is the browser's meetingLateness
// (admin_draw_swap_check.jsx) exactly, so the two sort a dojo's meetings
// alike, and it reads every meeting on its own bracket's rounds, so a swap
// that rebuilds the bracket compares meetings in two brackets fairly.
type meetingRank struct {
	depth, winners int
}

// sharedPoolDepth and noMeetingDepth rank a shared pool below, and a pair
// that cannot meet above, every round of any bracket.
const (
	sharedPoolDepth = -1000
	noMeetingDepth  = 1000
)

func (r meetingRank) compare(o meetingRank) int {
	if r.depth != o.depth {
		return r.depth - o.depth
	}
	return r.winners - o.winners
}

// meetingDepth is minus how many rounds before the final m is (0 = the
// final): by its display round when it carries one, else by its index in its
// bracket's rounds.
func meetingDepth(m *BracketMeeting) int {
	if m.DisplayRound > 0 {
		return -(m.DisplayRound - 1)
	}
	return -(m.Rounds - 1 - m.Round)
}

// rankMeeting ranks a summary row.
func rankMeeting(dm DojoMeeting) meetingRank {
	switch {
	case dm.SamePool != "":
		return meetingRank{depth: sharedPoolDepth, winners: sharedPoolDepth}
	case dm.Earliest == nil:
		return meetingRank{depth: noMeetingDepth, winners: noMeetingDepth}
	}
	r := meetingRank{depth: meetingDepth(dm.Earliest)}
	r.winners = r.depth
	if dm.IfBothWin != nil {
		r.winners = meetingDepth(dm.IfBothWin)
	}
	return r
}

// dojoTally is one dojo's pairs of dojo-mates in a swap: where each can first
// meet before it and after it, in any order (one entry per pair in each).
// name is the dojo as the draw spells it, for the dojos a candidate names.
type dojoTally struct {
	key, name     string
	before, after []meetingRank
}

// tallyIndex is the index of the dojo key's tally in tallies, added (with the
// dojo's name) when it holds none.
func tallyIndex(tallies *[]dojoTally, key, name string) int {
	for i := range *tallies {
		if (*tallies)[i].key == key {
			return i
		}
	}
	*tallies = append(*tallies, dojoTally{key: key, name: name})
	return len(*tallies) - 1
}

// shift is how the dojo fares in the swap, judged as a whole (operator
// decision C1): its meetings before and after, each sorted earliest first,
// are compared from the earliest, and the first difference decides. Positive
// is better off (that meeting is later after the swap), negative worse off,
// zero unchanged. Pairs that meet where they did weigh nothing, so a tally
// may hold only the pairs the swap can move. A swap between two of the dojo's
// own competitors that only trades their meetings changes nothing.
func (d *dojoTally) shift() int {
	slices.SortFunc(d.before, meetingRank.compare)
	slices.SortFunc(d.after, meetingRank.compare)
	for i := 0; i < len(d.before) && i < len(d.after); i++ {
		if c := d.after[i].compare(d.before[i]); c != 0 {
			return c
		}
	}
	return 0
}

// judgeDojos is the ONE rule that calls a swap better, worse or the same
// (operator decision C1), for the suggestions and the swap candidates alike:
// worse when any dojo is worse off, even if another is better off; better when
// none is worse off and at least one is better off; the same otherwise.
// better says, tally by tally, which dojos are better off. dojos names, sorted,
// the dojos the verdict is about: those worse off for worse, those better off
// for better, none for the same (operator decision C9: a mark names the dojos
// it affects, never the competitor's own).
func judgeDojos(tallies []dojoTally) (verdict string, better []bool, dojos []string) {
	better = make([]bool, len(tallies))
	var worseOff, betterOff []string
	for i := range tallies {
		switch c := tallies[i].shift(); {
		case c < 0:
			worseOff = append(worseOff, tallies[i].name)
		case c > 0:
			better[i] = true
			betterOff = append(betterOff, tallies[i].name)
		}
	}
	switch {
	case len(worseOff) > 0:
		slices.Sort(worseOff)
		return SwapVerdictWorse, better, worseOff
	case len(betterOff) > 0:
		slices.Sort(betterOff)
		return SwapVerdictBetter, better, betterOff
	}
	return SwapVerdictSame, better, nil
}

// suggestEntrant is one drawn competitor as the suggestions see them: the
// summary's own record of them, their dojo key, seed and place, their index
// in draw order, the group a swap must leave (pool, or first-round match)
// and the place key their meetings are read from (pool, or leaf).
type suggestEntrant struct {
	c     drawnCompetitor
	key   string
	seed  int
	place DrawPlace
	order int
	group int
	at    int
}

// swapSuggester holds one draw's entrants, indexed by dojo, and the rank of
// every pair of place keys it has read, so the search reads each meeting of
// two places once.
type swapSuggester struct {
	entrants []suggestEntrant
	byDojo   map[string][]int
	meet     func(a, b drawnCompetitor) DojoMeeting
	ranks    map[[2]int]meetingRank
	before   map[[2]string]DojoMeeting
	// groupSize is how many drawn competitors each first-round match holds, in
	// a knockout-only draw: one means a bye. Nil for a pools draw.
	groupSize map[int]int
}

// pairIDs is the unordered key of a pair of competitors.
func pairIDs(a, b string) [2]string {
	if b < a {
		a, b = b, a
	}
	return [2]string{a, b}
}

// SuggestSwaps is the suggestions for comp's draw, from the draw's own
// records: pools serves a pools draw and roster a knockout-only draw, as for
// DojoSeparation. Only a draw-ready competition whose format draws a bracket
// has any; every other answers empty lists.
//
// A candidate is two unseeded competitors in different pools (pools draw) or
// different first-round matches (knockout only). It is suggested when
// judgeDojos calls it better: no dojo is worse off and at least one is better
// off, where a shared pool is the earliest meeting of all, then earlier rounds
// before later ones, with the if-both-win meeting breaking a tie. Its
// improvements are the pairs of the dojos better off that meet later.
// Suggestions are ranked by the earliest meeting they fix (a shared pool
// first), then by how much later it becomes, then by how many pairs improve,
// then by the two ids, and at most maxSwapSuggestions are returned.
func SuggestSwaps(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, roster []domain.Player) SwapSuggestions {
	out := SwapSuggestions{Suggestions: []SwapSuggestion{}}
	if comp == nil || comp.Status != state.CompStatusDrawReady || !CompetitionDrawsBracket(comp.EffectiveFormat()) {
		return out
	}
	summary := DojoSeparation(comp, pools, bracket, roster)
	s := newSwapSuggester(comp, pools, bracket, roster, summary)
	if s == nil {
		return out
	}

	type ranked struct {
		suggestion SwapSuggestion
		fixed      meetingRank
		gain       meetingRank
	}
	var found []ranked
	n := len(s.entrants)
	for x := 0; x < n; x++ {
		for y := x + 1; y < n; y++ {
			improves, ok := s.evaluate(x, y)
			if !ok {
				continue
			}
			fixed, gain := improves[0].rankBefore, improves[0].gain
			sug := SwapSuggestion{A: s.side(x), B: s.side(y), Improves: make([]SwapImprovement, len(improves)), Byes: s.byes(x, y)}
			for i, imp := range improves {
				sug.Improves[i] = imp.SwapImprovement
			}
			found = append(found, ranked{suggestion: sug, fixed: fixed, gain: gain})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if c := a.fixed.compare(b.fixed); c != 0 {
			return c < 0
		}
		if c := a.gain.compare(b.gain); c != 0 {
			return c > 0
		}
		if la, lb := len(a.suggestion.Improves), len(b.suggestion.Improves); la != lb {
			return la > lb
		}
		if a.suggestion.A.ID != b.suggestion.A.ID {
			return a.suggestion.A.ID < b.suggestion.A.ID
		}
		return a.suggestion.B.ID < b.suggestion.B.ID
	})
	for i := 0; i < len(found) && i < maxSwapSuggestions; i++ {
		out.Suggestions = append(out.Suggestions, found[i].suggestion)
	}
	return out
}

// newSwapSuggester lists comp's drawn competitors in draw order, with the
// place each holds, beside summary, the draw's DojoSeparation, whose rows
// the suggestions quote as the before of each pair; nil for a draw that has
// no source of places.
func newSwapSuggester(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, roster []domain.Player, summary []DojoMeeting) *swapSuggester {
	s := &swapSuggester{
		byDojo: map[string][]int{},
		ranks:  map[[2]int]meetingRank{},
		before: make(map[[2]string]DojoMeeting, len(summary)),
	}
	for _, dm := range summary {
		s.before[pairIDs(dm.A.ID, dm.B.ID)] = dm
	}
	var drawn []drawnCompetitor
	seeds := map[string]int{}
	places := map[string]DrawPlace{}
	groups := map[string]int{}
	switch DrawSourceFor(comp) {
	case DrawInPools:
		drawn = poolCompetitors(pools, comp.EffectiveNumberPrefix() != "")
		s.meet = poolMeeting(pools, bracket)
		for pi, p := range pools {
			for i, pl := range p.Players {
				if pl.ID != "" {
					seeds[pl.ID] = pl.Seed
					places[pl.ID] = DrawPlace{Pool: p.PoolName, Position: i + 1}
					groups[pl.ID] = pi
				}
			}
		}
	case DrawInBracket:
		drawn = leafCompetitors(comp, bracket, roster)
		s.meet = func(a, b drawnCompetitor) DojoMeeting {
			return DojoMeeting{A: a.side, B: b.side, Earliest: leafMeeting(bracket, a.slot, b.slot)}
		}
		for _, p := range roster {
			if p.ID != "" {
				seeds[p.ID] = p.Seed
			}
		}
		for _, c := range drawn {
			slot := c.slot
			places[c.side.ID] = DrawPlace{Slot: &slot}
			groups[c.side.ID] = slot / 2
		}
		s.groupSize = map[int]int{}
		for _, g := range groups {
			s.groupSize[g]++
		}
	default:
		return nil
	}
	for i, c := range drawn {
		e := suggestEntrant{
			c:     c,
			key:   helper.DojoKey(c.dojo),
			seed:  seeds[c.side.ID],
			place: places[c.side.ID],
			order: i,
			group: groups[c.side.ID],
			at:    groups[c.side.ID],
		}
		if DrawSourceFor(comp) == DrawInBracket {
			e.at = c.slot
		}
		s.entrants = append(s.entrants, e)
		if e.key != "" {
			s.byDojo[e.key] = append(s.byDojo[e.key], i)
		}
	}
	return s
}

// view is entrant i as the swap of x and y leaves them: x takes y's place
// (pool or leaf, the number it carries, its place in draw order) and y takes
// x's. Everyone else stays.
func (s *swapSuggester) view(i, x, y int) suggestEntrant {
	e := s.entrants[i]
	var from int
	switch i {
	case x:
		from = y
	case y:
		from = x
	default:
		return e
	}
	o := s.entrants[from]
	e.c.side.Pool, e.c.side.Number, e.c.slot = o.c.side.Pool, o.c.side.Number, o.c.slot
	e.place, e.order, e.group, e.at = o.place, o.order, o.group, o.at
	return e
}

// rank is how late two entrants can first meet, read from their place keys
// (a pools draw's meeting depends on the two pools only, a knockout's on the
// two leaves), each pair of keys read once.
func (s *swapSuggester) rank(a, b suggestEntrant) meetingRank {
	k := [2]int{a.at, b.at}
	if k[1] < k[0] {
		k[0], k[1] = k[1], k[0]
	}
	if r, ok := s.ranks[k]; ok {
		return r
	}
	r := rankMeeting(s.meet(a.c, b.c))
	s.ranks[k] = r
	return r
}

// row is the summary row of entrants i and j after the swap of x and y, as
// DojoSeparation would write it on the swapped draw: the earlier in draw
// order is A, and the dojo reads under its first member's spelling.
func (s *swapSuggester) row(i, j, x, y int) DojoMeeting {
	a, b := s.view(i, x, y), s.view(j, x, y)
	if b.order < a.order {
		a, b = b, a
	}
	dm := s.meet(a.c, b.c)
	first := a
	for _, m := range s.byDojo[a.key] {
		if v := s.view(m, x, y); v.order < first.order {
			first = v
		}
	}
	dm.Dojo = strings.TrimSpace(first.c.dojo)
	return dm
}

// laterPair is a pair of dojo-mates, entrants i and j, that meets later
// after a swap: before and after rank its first meeting.
type laterPair struct {
	i, j          int
	before, after meetingRank
}

// pairShifts judges the swap of entrants x and y by judgeDojos, from the
// first meeting of every pair of dojo-mates that holds x or y before and
// after the swap of the two (no other pair's meeting can change): verdict is
// the swap's, gained lists the pairs that meet later from the dojos better
// off, and dojos names the dojos the verdict is about (judgeDojos). Valid
// because a swap moves no seed, so the bracket keeps its shape; any two
// unseeded entrants, in one group or two.
func (s *swapSuggester) pairShifts(x, y int) (gained []laterPair, verdict string, dojos []string) {
	ex, ey := s.entrants[x], s.entrants[y]
	type pair struct {
		i, j  int
		tally int
	}
	var pairs []pair
	// At most two dojos: x's and y's.
	tallies := make([]dojoTally, 0, 2)
	// A dojo is named as its first competitor in draw order spells it.
	add := func(i, j int, key string) {
		name := strings.TrimSpace(s.entrants[s.byDojo[key][0]].c.dojo)
		pairs = append(pairs, pair{i, j, tallyIndex(&tallies, key, name)})
	}
	for _, m := range s.byDojo[ex.key] {
		if m != x {
			add(x, m, ex.key)
		}
	}
	for _, m := range s.byDojo[ey.key] {
		if m != y && m != x {
			add(y, m, ey.key)
		}
	}
	var moved []laterPair
	var movedTally []int
	for _, p := range pairs {
		before := s.rank(s.entrants[p.i], s.entrants[p.j])
		after := s.rank(s.view(p.i, x, y), s.view(p.j, x, y))
		t := &tallies[p.tally]
		t.before = append(t.before, before)
		t.after = append(t.after, after)
		if after.compare(before) > 0 {
			moved = append(moved, laterPair{p.i, p.j, before, after})
			movedTally = append(movedTally, p.tally)
		}
	}
	verdict, better, dojos := judgeDojos(tallies)
	for k, g := range moved {
		if better[movedTally[k]] {
			gained = append(gained, g)
		}
	}
	return gained, verdict, dojos
}

// improvement is a SwapImprovement with the ranks the search sorts by.
type improvement struct {
	SwapImprovement
	rankBefore meetingRank
	gain       meetingRank
}

// evaluate scores the swap of entrants x and y. ok is true when it is a
// candidate (both unseeded, in different groups) that judgeDojos calls
// better; improves then lists the pairs of the dojos better off that meet
// later, the earliest meeting first, with the largest gain
// first among equals.
func (s *swapSuggester) evaluate(x, y int) (improves []improvement, ok bool) {
	ex, ey := s.entrants[x], s.entrants[y]
	if ex.seed > 0 || ey.seed > 0 || ex.group == ey.group {
		return nil, false
	}
	gained, verdict, _ := s.pairShifts(x, y)
	if verdict != SwapVerdictBetter || len(gained) == 0 {
		return nil, false
	}
	improves = make([]improvement, len(gained))
	for k, g := range gained {
		ids := pairIDs(s.entrants[g.i].c.side.ID, s.entrants[g.j].c.side.ID)
		before := s.before[ids]
		improves[k] = improvement{
			SwapImprovement: SwapImprovement{Dojo: before.Dojo, Before: before, After: s.row(g.i, g.j, x, y)},
			rankBefore:      g.before,
			gain:            meetingRank{depth: g.after.depth - g.before.depth, winners: g.after.winners - g.before.winners},
		}
	}
	sort.SliceStable(improves, func(i, j int) bool {
		a, b := improves[i], improves[j]
		if c := a.rankBefore.compare(b.rankBefore); c != 0 {
			return c < 0
		}
		if c := a.gain.compare(b.gain); c != 0 {
			return c > 0
		}
		if a.Dojo != b.Dojo {
			return a.Dojo < b.Dojo
		}
		if a.Before.A.ID != b.Before.A.ID {
			return a.Before.A.ID < b.Before.A.ID
		}
		return a.Before.B.ID < b.Before.B.ID
	})
	return improves, true
}

// byes is the first-round byes the swap of entrants x and y hands over in a
// knockout-only draw, as SwapResult.Byes would list them: a competitor takes
// the bye of the match they move to, so both change hands exactly when one of
// the two matches holds one competitor and the other two.
func (s *swapSuggester) byes(x, y int) []SwapBye {
	if s.groupSize == nil {
		return nil
	}
	ex, ey := s.entrants[x], s.entrants[y]
	toX, toY := s.groupSize[ey.group] == 1, s.groupSize[ex.group] == 1
	if toX == toY {
		return nil
	}
	return []SwapBye{
		{ID: ex.c.side.ID, Name: ex.c.side.Name, Bye: toX},
		{ID: ey.c.side.ID, Name: ey.c.side.Name, Bye: toY},
	}
}

// side names entrant i at the place they hold now.
func (s *swapSuggester) side(i int) SuggestedSide {
	e := s.entrants[i]
	return SuggestedSide{
		ID: e.c.side.ID, Name: e.c.side.Name, DisplayName: e.c.side.DisplayName,
		Number: e.c.side.Number, Dojo: strings.TrimSpace(e.c.dojo), Place: clonePlace(e.place),
	}
}

// SwapSuggestionsFor loads comp's draw and answers SuggestSwaps for it. It
// takes the record, as DojoSeparationFor does, because its caller has just
// loaded it to decide between 200 and 404. A draw that cannot be read is an
// error, never an empty answer, which would say there is nothing to improve.
func (e *Engine) SwapSuggestionsFor(comp *state.Competition) (*SwapSuggestions, error) {
	empty := &SwapSuggestions{Suggestions: []SwapSuggestion{}}
	if comp == nil || comp.Status != state.CompStatusDrawReady || !CompetitionDrawsBracket(comp.EffectiveFormat()) {
		return empty, nil
	}
	var (
		pools  []helper.Pool
		roster []domain.Player
		err    error
	)
	switch DrawSourceFor(comp) {
	case DrawInPools:
		if pools, err = e.store.LoadPools(comp.ID); err != nil {
			return nil, err
		}
	case DrawInBracket:
		if roster, err = e.store.LoadParticipants(comp.ID, comp.EffectiveWithZekkenName()); err != nil {
			return nil, err
		}
	default:
		return empty, nil
	}
	bracket, err := e.store.LoadBracket(comp.ID)
	if err != nil {
		return nil, err
	}
	answer := SuggestSwaps(comp, pools, bracket, roster)
	return &answer, nil
}

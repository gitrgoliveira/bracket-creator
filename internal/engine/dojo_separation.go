package engine

import (
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's dojo separation summary (bc-drev): for every two
// competitors of one dojo in a drawn competition, the earliest bracket match
// where they can meet. Nothing is stored. The answer is a pure function of the
// stored pools, bracket and roster, recomputed on every read like the seed
// warnings, so it can never go stale against a draw that was changed.

// DojoSide is one competitor of a DojoMeeting, named by participant id.
// Pool is the pool label pools.csv holds, set for a pools draw only. Number
// is the competitor number of the side's place, set only when the
// competition numbers (it carries a number prefix): pools.csv's Number for a
// pools draw, the number NumberKnockoutParticipants composes from the
// bracket's DrawOrder for a knockout.
type DojoSide struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	Number      string `json:"number,omitempty"`
	Pool        string `json:"pool,omitempty"`
}

// BracketMeeting is the bracket match where two first-round paths join.
// Round is the index into Bracket.Rounds (0 = the first round) and Rounds the
// bracket's round count: the pair bracketRoundLabel (bracket.jsx) names a
// round from, beside DisplayRound, which it prefers when set. PlaceA and
// PlaceB are the qualifying places ("Pool A-1st") the two competitors must
// finish at for this meeting, set for a pools draw only.
type BracketMeeting struct {
	MatchID      string `json:"matchId"`
	DisplayRound int    `json:"displayRound,omitempty"`
	Round        int    `json:"round"`
	Rounds       int    `json:"rounds"`
	MatchNumber  int    `json:"matchNumber,omitempty"`
	PlaceA       string `json:"placeA,omitempty"`
	PlaceB       string `json:"placeB,omitempty"`
}

// DojoMeeting is one pair of competitors of the same dojo.
//
//   - Same pool: SamePool is the pool's label, and Earliest is nil, since they
//     meet in the pool.
//   - Different pools: IfBothWin is the meeting of the two pools' 1st places,
//     and Earliest the earliest meeting over every pair of qualifying places
//     the bracket holds for the two pools (equal to IfBothWin when nothing
//     earlier exists). Extra qualifiers count, since the places are read from
//     the bracket's own draw-time labels.
//   - Knockout only: Earliest is the meeting of their two first-round leaves.
//
// A or B with no place in the bracket (a pools draw whose bracket holds no
// qualifying place for one of the pools) leaves both meetings nil.
type DojoMeeting struct {
	Dojo      string          `json:"dojo"`
	A         DojoSide        `json:"a"`
	B         DojoSide        `json:"b"`
	SamePool  string          `json:"samePool,omitempty"`
	IfBothWin *BracketMeeting `json:"ifBothWin,omitempty"`
	Earliest  *BracketMeeting `json:"earliest,omitempty"`
}

// DrawPlace is where a competitor sits in a drawn competition: a pool place
// (Pool, as pools.csv holds the label, and Position, 1-based) for a pools
// draw, or a first-round leaf (Slot, 0-based, side A of first-round match m
// is 2m and side B 2m+1) for a knockout-only draw. Slot is a pointer because
// 0 is a real slot.
type DrawPlace struct {
	Pool     string `json:"pool,omitempty"`
	Position int    `json:"position,omitempty"`
	Slot     *int   `json:"slot,omitempty"`
}

// PoolPlaceOf returns the pool place that holds participant id. An empty id
// is in no place (a record with an id field is resolved by id only).
func PoolPlaceOf(pools []helper.Pool, id string) (DrawPlace, bool) {
	if id == "" {
		return DrawPlace{}, false
	}
	for _, p := range pools {
		for i, pl := range p.Players {
			if pl.ID == id {
				return DrawPlace{Pool: p.PoolName, Position: i + 1}, true
			}
		}
	}
	return DrawPlace{}, false
}

// LeafSlotOf returns the first-round leaf that holds participant id, by the
// side ids a knockout-only draw stamps on its first round. An empty id is in
// no leaf.
func LeafSlotOf(bracket *state.Bracket, id string) (DrawPlace, bool) {
	if id == "" || bracket == nil || len(bracket.Rounds) == 0 {
		return DrawPlace{}, false
	}
	for mi, m := range bracket.Rounds[0] {
		for side, sideID := range [2]string{m.SideAID, m.SideBID} {
			if sideID == id {
				slot := 2*mi + side
				return DrawPlace{Slot: &slot}, true
			}
		}
	}
	return DrawPlace{}, false
}

// FirstRoundLeaf returns the first-round match and side that slot names
// (side A of match m is slot 2m, side B 2m+1). ok is false for a slot the
// bracket does not hold.
func FirstRoundLeaf(bracket *state.Bracket, slot int) (m *state.BracketMatch, side domain.MatchSide, ok bool) {
	if bracket == nil || len(bracket.Rounds) == 0 || slot < 0 || slot/2 >= len(bracket.Rounds[0]) {
		return nil, domain.MatchSideNone, false
	}
	side = domain.MatchSideA
	if slot%2 == 1 {
		side = domain.MatchSideB
	}
	return &bracket.Rounds[0][slot/2], side, true
}

// leafMeeting is the match where first-round leaves i and j meet: the bracket
// is position encoded (the winner of Rounds[r][m] goes to Rounds[r+1][m/2]),
// so their paths join in Rounds[k-1][i>>k] for the first k >= 1 at which
// i>>k == j>>k. nil for one leaf twice or a slot the bracket does not hold.
func leafMeeting(bracket *state.Bracket, i, j int) *BracketMeeting {
	if bracket == nil || i == j || i < 0 || j < 0 || len(bracket.Rounds) == 0 {
		return nil
	}
	if leaves := 2 * len(bracket.Rounds[0]); i >= leaves || j >= leaves {
		return nil
	}
	for k := 1; k <= len(bracket.Rounds); k++ {
		if i>>k != j>>k {
			continue
		}
		r, mi := k-1, i>>k
		if mi >= len(bracket.Rounds[r]) {
			return nil
		}
		m := &bracket.Rounds[r][mi]
		return &BracketMeeting{
			MatchID:      m.ID,
			DisplayRound: m.DisplayRound,
			Round:        r,
			Rounds:       len(bracket.Rounds),
			MatchNumber:  m.MatchNumber,
		}
	}
	return nil
}

// compareMeetings orders two meetings earliest first: by round, then by match
// number, then by match id, with nil last.
func compareMeetings(a, b *BracketMeeting) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	case a.Round != b.Round:
		return a.Round - b.Round
	case a.MatchNumber != b.MatchNumber:
		return a.MatchNumber - b.MatchNumber
	}
	return strings.Compare(a.MatchID, b.MatchID)
}

// drawnCompetitor is one competitor in draw order, with the dojo the
// summary groups them by and the place the meeting is read from.
type drawnCompetitor struct {
	side DojoSide
	dojo string
	slot int // knockout only: the first-round leaf
}

// qualifierLeaf is one qualifying place a pools draw's bracket holds: the
// first-round leaf and its draw-time label ("Pool A-2nd").
type qualifierLeaf struct {
	slot  int
	label string
}

// DojoSeparation is the summary for comp's draw, from the draw's own records,
// so the swap's dry run can call it on the draw it would write. Only a format
// that draws a bracket (pools + knockout, knockout) has one: a league, a Swiss
// and a competition with no draw yet answer an empty list. pools serves a
// pools draw and roster a knockout-only draw, whose first-round leaves name
// their competitors by id only.
//
// Competitors are grouped by helper.DojoKey, the rule the draw placed them by,
// and a blank or whitespace dojo forms no group. Within a group the pairs
// follow draw order (A is the earlier in the draw), and the list is sorted:
// same-pool pairs first, then the earliest meeting (round, match number), then
// dojo, then A's id, then B's id.
func DojoSeparation(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, roster []domain.Player) []DojoMeeting {
	out := []DojoMeeting{}
	if comp == nil || !CompetitionDrawsBracket(comp.EffectiveFormat()) {
		return out
	}
	var drawn []drawnCompetitor
	var meet func(a, b drawnCompetitor) DojoMeeting
	numbered := comp.EffectiveNumberPrefix() != ""
	switch DrawSourceFor(comp) {
	case DrawInPools:
		drawn = poolCompetitors(pools, numbered)
		meet = poolMeeting(pools, bracket)
	case DrawInBracket:
		drawn = leafCompetitors(comp, bracket, roster)
		meet = func(a, b drawnCompetitor) DojoMeeting {
			return DojoMeeting{A: a.side, B: b.side, Earliest: leafMeeting(bracket, a.slot, b.slot)}
		}
	default:
		return out
	}

	groups := map[string][]drawnCompetitor{}
	var keys []string
	for _, c := range drawn {
		key := helper.DojoKey(c.dojo)
		if key == "" {
			continue
		}
		if _, seen := groups[key]; !seen {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], c)
	}
	for _, key := range keys {
		members := groups[key]
		// The group reads under the spelling of its first member in the draw.
		dojo := strings.TrimSpace(members[0].dojo)
		for i := 0; i < len(members); i++ {
			for j := i + 1; j < len(members); j++ {
				dm := meet(members[i], members[j])
				dm.Dojo = dojo
				out = append(out, dm)
			}
		}
	}

	sort.SliceStable(out, func(x, y int) bool {
		a, b := out[x], out[y]
		if (a.SamePool != "") != (b.SamePool != "") {
			return a.SamePool != ""
		}
		if c := compareMeetings(a.Earliest, b.Earliest); c != 0 {
			return c < 0
		}
		if a.Dojo != b.Dojo {
			return a.Dojo < b.Dojo
		}
		if a.A.ID != b.A.ID {
			return a.A.ID < b.A.ID
		}
		return a.B.ID < b.B.ID
	})
	return out
}

// poolCompetitors lists a pools draw's competitors in pool order, then
// position. A row with no id is left out: a competitor is identified by id,
// and an id-less pool row is a fault the load repair reports, not a person
// this summary can name. numbered says the competition numbers, so each side
// carries its row's stored Number (the number belongs to the place).
func poolCompetitors(pools []helper.Pool, numbered bool) []drawnCompetitor {
	var out []drawnCompetitor
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.ID == "" {
				continue
			}
			side := DojoSide{ID: pl.ID, Name: pl.Name, DisplayName: pl.DisplayName, Pool: p.PoolName}
			if numbered {
				side.Number = pl.Number
			}
			out = append(out, drawnCompetitor{
				side: side,
				dojo: pl.Dojo,
			})
		}
	}
	return out
}

// leafCompetitors lists a knockout-only draw's competitors in first-round
// slot order, each read from the roster by the leaf's side id. A leaf whose
// id the roster does not hold names nobody the summary could group. The
// number is composed from the bracket's DrawOrder by
// NumberKnockoutParticipants, the one derivation of a knockout number, over a
// copy of the roster with any number it carried cleared, so a competition
// with no prefix, or a bracket that recorded no DrawOrder, names no number.
func leafCompetitors(comp *state.Competition, bracket *state.Bracket, roster []domain.Player) []drawnCompetitor {
	if bracket == nil || len(bracket.Rounds) == 0 {
		return nil
	}
	numbered := make([]domain.Player, len(roster))
	copy(numbered, roster)
	for i := range numbered {
		numbered[i].Number = ""
	}
	NumberKnockoutParticipants(comp, bracket.DrawOrder, numbered)
	byID := make(map[string]domain.Player, len(numbered))
	for _, p := range numbered {
		if p.ID != "" {
			byID[p.ID] = p
		}
	}
	var out []drawnCompetitor
	for mi, m := range bracket.Rounds[0] {
		for side, id := range [2]string{m.SideAID, m.SideBID} {
			p, ok := byID[id]
			if id == "" || !ok {
				continue
			}
			out = append(out, drawnCompetitor{
				side: DojoSide{ID: p.ID, Name: p.Name, DisplayName: p.DisplayName, Number: p.Number},
				dojo: p.Dojo,
				slot: 2*mi + side,
			})
		}
	}
	return out
}

// poolMeeting returns the meeting of two competitors of a pools draw. The
// qualifying places are the first-round leaves whose draw-time label
// (PlaceholderA/B, written once at the draw and never rewritten as the pools
// resolve) names their pool, so the answer is the same before and after the
// pools finish.
func poolMeeting(pools []helper.Pool, bracket *state.Bracket) func(a, b drawnCompetitor) DojoMeeting {
	byPool := map[string][]qualifierLeaf{}
	if bracket != nil && len(bracket.Rounds) > 0 {
		for mi, m := range bracket.Rounds[0] {
			for side, label := range [2]string{m.PlaceholderA, m.PlaceholderB} {
				if !helper.IsPoolFinalistPlaceholder(label) {
					continue
				}
				pool := qualifierLabelPool(label)
				byPool[pool] = append(byPool[pool], qualifierLeaf{slot: 2*mi + side, label: label})
			}
		}
	}
	at := func(m *BracketMeeting, la, lb qualifierLeaf) *BracketMeeting {
		if m == nil {
			return nil
		}
		m.PlaceA, m.PlaceB = la.label, lb.label
		return m
	}
	return func(a, b drawnCompetitor) DojoMeeting {
		dm := DojoMeeting{A: a.side, B: b.side}
		if a.side.Pool == b.side.Pool {
			dm.SamePool = a.side.Pool
			return dm
		}
		first := helper.GetOrdinal(1)
		for _, la := range byPool[a.side.Pool] {
			for _, lb := range byPool[b.side.Pool] {
				m := at(leafMeeting(bracket, la.slot, lb.slot), la, lb)
				if m == nil {
					continue
				}
				if la.label == a.side.Pool+"-"+first && lb.label == b.side.Pool+"-"+first {
					winners := *m
					dm.IfBothWin = &winners
				}
				if compareMeetings(m, dm.Earliest) < 0 {
					dm.Earliest = m
				}
			}
		}
		return dm
	}
}

// DojoSeparationFor loads comp's draw and answers DojoSeparation for it. It
// takes the record, as SeedWarningsFor does, because its caller has just
// loaded it to decide between 200 and 404. A draw that cannot be read is an
// error, never an empty summary: an empty one tells the organiser that no
// two competitors of a dojo can meet early.
func (e *Engine) DojoSeparationFor(comp *state.Competition) ([]DojoMeeting, error) {
	if comp == nil || !CompetitionDrawsBracket(comp.EffectiveFormat()) {
		return []DojoMeeting{}, nil
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
		return []DojoMeeting{}, nil
	}
	bracket, err := e.store.LoadBracket(comp.ID)
	if err != nil {
		return nil, err
	}
	return DojoSeparation(comp, pools, bracket, roster), nil
}

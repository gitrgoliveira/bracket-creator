package engine

import (
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The draw review's swap (bc-drev): two competitors of a drawn competition
// that has not started trade places. The competitor moves; the place keeps its
// position, its number, its court and its time. One request is one
// WithTransaction, and the change is worked out by a pure planSwap over what
// the transaction read, so the dry run ("If you swap them") answers exactly
// what the real swap would do: it is the same plan, not saved.

// Swap refusal codes, carried by SwapRefusedError.Code. The API answers the
// first three 400 and the rest 409.
const (
	SwapInvalid          = "invalid"
	SwapSameCompetitor   = "same_competitor"
	SwapNotInDraw        = "not_in_draw"
	SwapNoDraw           = "no_draw"
	SwapDrawStarted      = "draw_started"
	SwapNotSupported     = "swap_not_supported"
	SwapDrawChanged      = "draw_changed"
	SwapAlreadySwapped   = "already_swapped"
	swapDrawStartedHuman = "The competition has started, so the draw can no longer change."
)

// SwapRefusedError is SwapDrawPlaces' refusal: Code is one of the Swap*
// codes above and Message the sentence the organiser reads. Places is set for
// SwapDrawChanged only: where the two competitors are now, so the page can
// refetch and say so.
type SwapRefusedError struct {
	Code    string
	Message string
	Places  *SwapPlaces
}

func (e *SwapRefusedError) Error() string { return e.Message }

func swapRefused(code, format string, args ...any) *SwapRefusedError {
	return &SwapRefusedError{Code: code, Message: fmt.Sprintf(format, args...)}
}

// SwapSide is one competitor of a swap request: their participant id and the
// place the page saw them at. The place is a pool place ({pool, position})
// for a pools draw or a first-round leaf ({slot}) for a knockout-only draw.
type SwapSide struct {
	ID    string    `json:"id"`
	Place DrawPlace `json:"place"`
}

// SwapRequest is the body of POST .../draw/swap. DryRun computes the answer
// and saves nothing.
type SwapRequest struct {
	A      SwapSide `json:"a"`
	B      SwapSide `json:"b"`
	DryRun bool     `json:"dryRun,omitempty"`
}

// SwapPlaces is where the two competitors of a request are: after the swap
// in an answer, now in a SwapDrawChanged refusal.
type SwapPlaces struct {
	A DrawPlace `json:"a"`
	B DrawPlace `json:"b"`
}

// SwapMove says where one competitor goes.
type SwapMove struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	DisplayName string    `json:"displayName,omitempty"`
	From        DrawPlace `json:"from"`
	To          DrawPlace `json:"to"`
}

// SwapNumber is the number a competitor leaves and the one they take: a
// number belongs to its place, so the two trade numbers (their tags). Listed
// only when the competition numbers.
type SwapNumber struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	From string `json:"from"`
	To   string `json:"to"`
}

// SwapBye is a competitor of a knockout-only draw whose first-round bye the
// swap gives (Bye true) or takes away (Bye false).
type SwapBye struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Bye  bool   `json:"bye"`
}

// SwapSeedWarning says a seeded competitor leaves their pool (or their
// knockout place): their seed and where they go. Filled by the seeded swap.
type SwapSeedWarning struct {
	ID   string    `json:"id"`
	Name string    `json:"name"`
	Seed int       `json:"seed"`
	To   DrawPlace `json:"to"`
}

// SwapSeparation is the dojo separation summary's rows for the dojos the swap
// touches, before and after: the two competitors' dojos and any other dojo
// whose meetings change.
type SwapSeparation struct {
	Before []DojoMeeting `json:"before"`
	After  []DojoMeeting `json:"after"`
}

// SwapResult is the answer of a swap or of its dry run. Applied says this
// request changed the draw (false for a dry run and for a resend, whose
// Reason is SwapAlreadySwapped). BracketRebuilt, TimesRescheduled and
// SeededWarning belong to a seeded swap, LineupsMoved and LineupsRemoved to a
// team swap.
type SwapResult struct {
	Applied          bool              `json:"applied"`
	DryRun           bool              `json:"dryRun,omitempty"`
	Reason           string            `json:"reason,omitempty"`
	Places           SwapPlaces        `json:"places"`
	Moves            []SwapMove        `json:"moves"`
	Numbers          []SwapNumber      `json:"numbers,omitempty"`
	Byes             []SwapBye         `json:"byes,omitempty"`
	BracketRebuilt   bool              `json:"bracketRebuilt"`
	TimesRescheduled bool              `json:"timesRescheduled"`
	SeededWarning    []SwapSeedWarning `json:"seededWarning,omitempty"`
	LineupsMoved     int               `json:"lineupsMoved"`
	LineupsRemoved   int               `json:"lineupsRemoved"`
	Separation       SwapSeparation    `json:"separation"`
}

// swapSnapshot is what the swap's transaction read: the competition, its draw
// and its roster. fought names a match of a draw-ready competition that holds
// something fought (bout rows, a score, a history or a score-log file), or ""
// when none does. lineups is a team competition's stored lineups, the
// records of lineups taken away included (nil for an individual one), which
// move with a swapped team (moveLineups). rebuild builds a pools + knockout competition's knockout
// preview for a seeded swap; it is nil for every other format.
type swapSnapshot struct {
	comp        *state.Competition
	pools       []helper.Pool
	poolMatches []state.MatchResult
	bracket     *state.Bracket
	roster      []domain.Player
	lineups     map[string]domain.TeamLineup
	fought      string
	rebuild     previewBuilder
}

// previewBuilder builds a pools + knockout competition's knockout preview from
// its pools and pool matches (buildPoolPreviewBracket), on the tournament and
// court floor read before the swap's transaction took the lock.
type previewBuilder func(comp *state.Competition, pools []helper.Pool, poolMatches []state.MatchResult) (*state.Bracket, error)

// swapPlan is a swap worked out on copies of the snapshot's draw: the new
// pools, pool matches and bracket, which of them changed, the lineups that
// move with a swapped team, and the answer. save stages the changed draw
// files in the transaction, saveLineups the lineups after them.
type swapPlan struct {
	pools              []helper.Pool
	poolMatches        []state.MatchResult
	bracket            *state.Bracket
	poolsChanged       bool
	poolMatchesChanged bool
	bracketChanged     bool
	lineups            swapLineups
	answer             SwapResult
}

// SwapDrawPlaces trades the places of two competitors of a draw-ready
// competition, in ONE WithTransaction: every refusal is judged under the
// competition's lock, and pools.csv, pool-matches.csv and bracket.json land
// together or not at all. A dry run (req.DryRun) is refused exactly as the
// swap would be and answers the same plan without saving it. A resend of a
// swap that landed (each competitor at the other's named place) answers
// Applied false, Reason SwapAlreadySwapped. No match history line is written:
// nothing was fought, and the competitor-status records and check-ins are
// keyed by participant id, so they stay with the competitor.
func (e *Engine) SwapDrawPlaces(compID string, req SwapRequest) (*SwapResult, error) {
	rebuild := e.swapPreviewBuilder(compID)
	var answer SwapResult
	err := e.store.WithTransaction(compID, func(tx state.StoreTx) error {
		snap, err := loadSwapSnapshot(tx, compID)
		if err != nil {
			return err
		}
		snap.rebuild = rebuild
		plan, err := planSwap(snap, req)
		if err != nil {
			return err
		}
		answer = plan.answer
		if req.DryRun || !plan.answer.Applied {
			return nil
		}
		if err := plan.save(tx, compID); err != nil {
			return err
		}
		return plan.saveLineups(e, tx, snap.comp)
	})
	if err != nil {
		return nil, err
	}
	return &answer, nil
}

// swapPreviewBuilder reads what a seeded swap's knockout rebuild needs beyond
// the competition's own files, before the swap's transaction takes the lock,
// as the draw reads them: the tournament (its schedule) and the court floor
// the other same-day competitions leave (courtFloorFor). A read that fails
// refuses only a swap that rebuilds the knockout; an unseeded swap never asks.
// nil for a competition that is not pools + knockout, which has no knockout
// preview to rebuild.
func (e *Engine) swapPreviewBuilder(compID string) previewBuilder {
	comp, err := e.store.LoadCompetition(compID)
	if err == nil && (comp == nil || comp.EffectiveFormat() != state.CompFormatMixed) {
		return nil
	}
	var tournament *state.Tournament
	var floor map[string]time.Time
	if err == nil {
		tournament, err = e.store.LoadTournament()
	}
	if err == nil {
		floor, err = e.courtFloorFor(comp, tournament)
	}
	return func(c *state.Competition, pools []helper.Pool, poolMatches []state.MatchResult) (*state.Bracket, error) {
		if err != nil {
			return nil, err
		}
		return e.buildPoolPreviewBracket(c, pools, poolMatches, floor, tournament)
	}
}

// loadSwapSnapshot reads what planSwap judges, under the transaction's lock.
// The matches' history and score-log files are read only for a draw-ready
// competition, the one state a swap can apply in.
func loadSwapSnapshot(tx state.StoreTx, compID string) (swapSnapshot, error) {
	var snap swapSnapshot
	comp, err := tx.LoadCompetition(compID)
	if err != nil {
		return snap, err
	}
	if comp == nil {
		return snap, notFoundErrorf("competition %s not found", compID)
	}
	snap.comp = comp
	if snap.pools, err = tx.LoadPools(compID); err != nil {
		return snap, fmt.Errorf("loading pools: %w", err)
	}
	if snap.poolMatches, err = tx.LoadPoolMatches(compID); err != nil {
		return snap, fmt.Errorf("loading pool matches: %w", err)
	}
	if snap.bracket, err = tx.LoadBracket(compID); err != nil {
		return snap, fmt.Errorf("loading bracket: %w", err)
	}
	if snap.roster, err = tx.LoadParticipants(compID, comp.EffectiveWithZekkenName()); err != nil {
		return snap, fmt.Errorf("loading participants: %w", err)
	}
	if comp.IsTeam() {
		if snap.lineups, err = tx.LoadTeamLineupsWithRemoved(compID); err != nil {
			return snap, fmt.Errorf("loading lineups: %w", err)
		}
	}
	if comp.Status == state.CompStatusDrawReady {
		if snap.fought, err = foughtMatch(tx, compID, snap); err != nil {
			return snap, err
		}
	}
	return snap, nil
}

// foughtMatch returns the id of the first match that holds something fought
// although the competition is still draw-ready: bout rows, a score, or a
// history or score-log file. A swap must never relabel a side under rows that
// name the old competitor's team members, and any start moves the
// competition out of draw-ready, so this is a guard that should never fire.
func foughtMatch(tx state.StoreTx, compID string, snap swapSnapshot) (string, error) {
	var ids []string
	for i := range snap.poolMatches {
		m := &snap.poolMatches[i]
		if len(m.SubResults) > 0 || m.HoldsScore() {
			return m.ID, nil
		}
		ids = append(ids, m.ID)
	}
	if b := snap.bracket; b != nil {
		for ri := range b.Rounds {
			for mi := range b.Rounds[ri] {
				m := &b.Rounds[ri][mi]
				if len(m.SubResults) > 0 || m.HoldsScore() {
					return m.ID, nil
				}
				ids = append(ids, m.ID)
			}
		}
		if bm := b.ThirdPlaceMatch; bm != nil {
			if len(bm.SubResults) > 0 || bm.HoldsScore() {
				return bm.ID, nil
			}
			ids = append(ids, bm.ID)
		}
	}
	for _, id := range ids {
		history, err := tx.LoadMatchHistory(compID, id)
		if err != nil {
			return "", fmt.Errorf("loading the history of %s: %w", id, err)
		}
		if len(history) > 0 {
			return id, nil
		}
		lines, err := tx.LoadScoreLog(compID, id)
		if err != nil {
			return "", fmt.Errorf("loading the score log of %s: %w", id, err)
		}
		if len(lines) > 0 {
			return id, nil
		}
	}
	return "", nil
}

// planSwap works the swap out on copies of snap's draw. It is pure: the
// transaction saves what it returns, the dry run only answers it. Refusals,
// in order: SwapNotSupported, SwapNoDraw, SwapDrawStarted, SwapInvalid,
// SwapSameCompetitor, SwapNotInDraw, SwapDrawChanged.
func planSwap(snap swapSnapshot, req SwapRequest) (swapPlan, error) {
	comp := snap.comp
	if !CompetitionDrawsBracket(comp.EffectiveFormat()) {
		return swapPlan{}, swapRefused(SwapNotSupported, "A league or Swiss competition has no draw places to swap.")
	}
	source := DrawSourceFor(comp)
	if source == DrawNone || (source == DrawInPools && len(snap.pools) == 0) ||
		(source == DrawInBracket && (snap.bracket == nil || len(snap.bracket.Rounds) == 0)) {
		return swapPlan{}, swapRefused(SwapNoDraw, "There is no draw to change yet.")
	}
	if comp.Status != state.CompStatusDrawReady || recordedResultIn(snap.poolMatches, snap.bracket) || snap.fought != "" {
		return swapPlan{}, swapRefused(SwapDrawStarted, swapDrawStartedHuman)
	}
	for _, side := range []SwapSide{req.A, req.B} {
		if err := checkSwapPlace(side, source); err != nil {
			return swapPlan{}, err
		}
	}
	if req.A.ID == req.B.ID {
		return swapPlan{}, swapRefused(SwapSameCompetitor, "Pick two different competitors to swap.")
	}
	if samePlace(req.A.Place, req.B.Place) {
		return swapPlan{}, swapRefused(SwapInvalid, "Two competitors cannot be at the same place: pick the place each of them is at.")
	}

	placeOf := func(id string) (DrawPlace, bool) {
		if source == DrawInPools {
			return PoolPlaceOf(snap.pools, id)
		}
		return LeafSlotOf(snap.bracket, id)
	}
	atA, okA := placeOf(req.A.ID)
	atB, okB := placeOf(req.B.ID)
	for _, c := range []struct {
		id string
		ok bool
	}{{req.A.ID, okA}, {req.B.ID, okB}} {
		if !c.ok {
			return swapPlan{}, notInDrawRefusal(snap.roster, c.id)
		}
	}

	after := SwapPlaces{A: req.B.Place, B: req.A.Place}
	plan := swapPlan{}
	switch {
	case samePlace(atA, req.A.Place) && samePlace(atB, req.B.Place):
		plan = exchangePlaces(snap, req.A.ID, req.B.ID, source)
		if err := plan.seededSwap(snap, req, source); err != nil {
			return swapPlan{}, err
		}
		plan.moveLineups(snap, req.A.ID, req.B.ID)
		plan.answer.Applied = true
	case samePlace(atA, req.B.Place) && samePlace(atB, req.A.Place):
		// A resend of a swap that landed: nothing to do, and nothing changes.
		plan = swapPlan{pools: snap.pools, poolMatches: snap.poolMatches, bracket: snap.bracket}
		plan.answer.Reason = SwapAlreadySwapped
	default:
		refusal := swapRefused(SwapDrawChanged, "The draw has changed since you opened it. Look at it again, then pick the swap again.")
		refusal.Places = &SwapPlaces{A: clonePlace(atA), B: clonePlace(atB)}
		return swapPlan{}, refusal
	}
	plan.answer.DryRun = req.DryRun
	if req.DryRun {
		plan.answer.Applied = false
	}
	plan.answer.Places = SwapPlaces{A: clonePlace(after.A), B: clonePlace(after.B)}
	plan.describe(snap, req, source)
	return plan, nil
}

// checkSwapPlace refuses a place that is not exactly one shape, or a shape
// the draw does not have: a pool place names a pool and a 1-based position
// in a pools draw, a slot is a 0-based first-round leaf of a knockout-only
// draw.
func checkSwapPlace(side SwapSide, source DrawSource) error {
	if side.ID == "" {
		return swapRefused(SwapInvalid, "Each competitor of a swap needs an id.")
	}
	p := side.Place
	poolShape := p.Pool != "" || p.Position != 0
	slotShape := p.Slot != nil
	switch {
	case poolShape && slotShape:
		return swapRefused(SwapInvalid, "A place is a pool place or a bracket slot, not both.")
	case !poolShape && !slotShape:
		return swapRefused(SwapInvalid, "Each competitor of a swap needs the place they are at.")
	case poolShape && source != DrawInPools:
		return swapRefused(SwapInvalid, "This draw has no pools: name a bracket slot.")
	case slotShape && source != DrawInBracket:
		return swapRefused(SwapInvalid, "This draw is in pools: name a pool and a position.")
	case poolShape && (p.Pool == "" || p.Position < 1):
		return swapRefused(SwapInvalid, "A pool place needs the pool and a position from 1.")
	case slotShape && *p.Slot < 0:
		return swapRefused(SwapInvalid, "A bracket slot is a number from 0.")
	}
	return nil
}

// notInDrawRefusal names a competitor the draw does not hold and why: not in
// the roster at all, or in it but not drawn (not checked in when the draw
// was made, or added after it).
func notInDrawRefusal(roster []domain.Player, id string) *SwapRefusedError {
	for _, p := range roster {
		if p.ID == id {
			return swapRefused(SwapNotInDraw, "%s is not in the draw: they were not checked in when the draw was made, or were added after it.", p.Name)
		}
	}
	return swapRefused(SwapNotInDraw, "Competitor %s is not in this competition's roster, so they are not in the draw.", id)
}

// samePlace compares two places by value (a slot by the leaf it names).
func samePlace(a, b DrawPlace) bool {
	if a.Pool != b.Pool || a.Position != b.Position || (a.Slot == nil) != (b.Slot == nil) {
		return false
	}
	return a.Slot == nil || *a.Slot == *b.Slot
}

// clonePlace copies a place, its slot included, so an answer never aliases a
// request's or a snapshot's slot.
func clonePlace(p DrawPlace) DrawPlace {
	if p.Slot != nil {
		slot := *p.Slot
		p.Slot = &slot
	}
	return p
}

// exchangePlaces applies the ONE two-way id map {a->b, b->a} to copies of the
// draw in a single pass, so neither id collapses onto the other:
//
//   - pools.csv: the two rows trade the competitor (id, name, display name,
//     dojo, seed, metadata) and each keeps its position and Number;
//   - pool-matches.csv: every side and winner naming either id takes the
//     other's id and name;
//   - bracket.json: every slot (forEachBracketSlot); sides A and B through
//     seatBracketSide, which keeps the representative bout row in step, and
//     the Winner (a bye the draw resolved) written directly, name and id;
//     DrawOrder trades the two ids, so the numbers composed from it stay with
//     the place.
//
// Courts, times, match ids and placeholder labels do not move. Resolved by id
// only: every draw-ready side carries its participant id.
func exchangePlaces(snap swapSnapshot, a, b string, source DrawSource) swapPlan {
	plan := swapPlan{
		pools:       clonePools(snap.pools),
		poolMatches: clonePoolMatches(snap.poolMatches),
		bracket:     cloneBracket(snap.bracket),
	}
	other := map[string]string{a: b, b: a}

	if source == DrawInPools {
		ai, aj, _ := poolRowOf(plan.pools, a)
		bi, bj, _ := poolRowOf(plan.pools, b)
		pa, pb := &plan.pools[ai].Players[aj], &plan.pools[bi].Players[bj]
		numA, posA, numB, posB := pa.Number, pa.PoolPosition, pb.Number, pb.PoolPosition
		*pa, *pb = *pb, *pa
		pa.Number, pa.PoolPosition, pb.Number, pb.PoolPosition = numA, posA, numB, posB
		plan.poolsChanged = true
	}

	rowNames := map[string]string{}
	for _, p := range snap.pools {
		for _, pl := range p.Players {
			if pl.ID == a || pl.ID == b {
				rowNames[pl.ID] = pl.Name
			}
		}
	}
	for _, p := range snap.roster {
		if _, ok := rowNames[p.ID]; !ok && (p.ID == a || p.ID == b) {
			rowNames[p.ID] = p.Name
		}
	}

	// Each file names a competitor its own way, so the name a competitor takes
	// into a slot is the one that file already gives them.
	matchNames := maps.Clone(rowNames)
	for _, m := range snap.poolMatches {
		for _, s := range [][2]string{{m.SideAID, m.SideA}, {m.SideBID, m.SideB}} {
			if (s[0] == a || s[0] == b) && s[1] != "" {
				matchNames[s[0]] = s[1]
			}
		}
	}
	for i := range plan.poolMatches {
		m := &plan.poolMatches[i]
		for _, f := range [][2]*string{{&m.SideAID, &m.SideA}, {&m.SideBID, &m.SideB}, {&m.WinnerID, &m.Winner}} {
			if to, ok := other[*f[0]]; ok {
				*f[0], *f[1] = to, matchNames[to]
				plan.poolMatchesChanged = true
			}
		}
	}

	if plan.bracket != nil {
		bracketNames := maps.Clone(rowNames)
		if snap.bracket != nil {
			for _, m := range snap.bracket.Rounds {
				for _, bm := range m {
					for _, s := range [][2]string{{bm.SideAID, bm.SideA}, {bm.SideBID, bm.SideB}} {
						if (s[0] == a || s[0] == b) && s[1] != "" {
							bracketNames[s[0]] = s[1]
						}
					}
				}
			}
		}
		forEachBracketSlot(plan.bracket, func(m *state.BracketMatch, slot domain.MatchSide, name, id *string) {
			to, ok := other[*id]
			if !ok {
				return
			}
			plan.bracketChanged = true
			if slot == domain.MatchSideNone {
				// The Winner is not a side: written directly, name and id.
				*name, *id = bracketNames[to], to
				return
			}
			seatBracketSide(m, slot, bracketNames[to], to)
		})
		for i, id := range plan.bracket.DrawOrder {
			if to, ok := other[id]; ok {
				plan.bracket.DrawOrder[i] = to
				plan.bracketChanged = true
			}
		}
	}
	return plan
}

// seededSwap fills a seeded swap's part of the plan (FR5): a warning for each
// seeded competitor the swap moves out of their pool (or out of their
// knockout place), and, in a pools + knockout draw, the knockout preview
// rebuilt from the swapped pools with the draw's own builder, since a pool's
// seed decides where its qualifiers sit in the bracket. A swap inside one
// pool moves nobody out of it, and a swap of two unseeded competitors leaves
// every seed where it was, so neither rebuilds anything. A knockout-only draw
// is never rebuilt: its first round holds the competitors themselves, which
// exchangePlaces has already traded.
func (p *swapPlan) seededSwap(snap swapSnapshot, req SwapRequest, source DrawSource) error {
	if source == DrawInPools && req.A.Place.Pool == req.B.Place.Pool {
		return nil
	}
	for _, mv := range []struct {
		id string
		to DrawPlace
	}{{req.A.ID, req.B.Place}, {req.B.ID, req.A.Place}} {
		if c := swapCompetitor(snap, mv.id); c.Seed > 0 {
			p.answer.SeededWarning = append(p.answer.SeededWarning, SwapSeedWarning{ID: c.ID, Name: c.Name, Seed: c.Seed, To: clonePlace(mv.to)})
		}
	}
	if len(p.answer.SeededWarning) == 0 || source != DrawInPools ||
		snap.comp.EffectiveFormat() != state.CompFormatMixed || snap.bracket == nil {
		return nil
	}
	if snap.rebuild == nil {
		return fmt.Errorf("competition %s: no builder for its knockout preview", snap.comp.ID)
	}
	rebuilt, err := snap.rebuild(snap.comp, p.pools, p.poolMatches)
	if err != nil {
		return err
	}
	if rebuilt == nil {
		return fmt.Errorf("competition %s: the swapped pools build no knockout", snap.comp.ID)
	}
	p.answer.TimesRescheduled = keepBracketSchedule(snap.bracket, rebuilt)
	p.bracket, p.bracketChanged = rebuilt, true
	p.answer.BracketRebuilt = true
	return nil
}

// keepBracketSchedule carries the stored bracket's schedule onto the rebuilt
// one, by match id, and reports whether it could not (rescheduled true).
//
// A swap never changes a pool's size, so the rebuilt tree has the stored
// tree's match ids, but a seed changing pool can move which of them is a bye.
// When every match id is a match to play (or a bye) both before and after,
// each match keeps its stored court, time and queue place, so an organiser's
// court and time moves stand. When any id is a bye on one side and a match on
// the other, or an id is new or gone, the builder's whole new schedule is
// taken: never a mix, which could put two matches on one court at one time.
// Either way a match keeps its stored change stamps, which only grow.
func keepBracketSchedule(stored, rebuilt *state.Bracket) (rescheduled bool) {
	before := map[string]*state.BracketMatch{}
	for _, m := range bracketMatches(stored) {
		before[m.ID] = m
	}
	after := bracketMatches(rebuilt)
	same := len(after) == len(before)
	for _, m := range after {
		if old, ok := before[m.ID]; !ok || toPlay(old) != toPlay(m) {
			same = false
		}
	}
	for _, m := range after {
		old, ok := before[m.ID]
		if !ok {
			continue
		}
		m.ModifiedAt, m.GroupStamps = old.ModifiedAt, maps.Clone(old.GroupStamps)
		if same {
			m.Court, m.ScheduledAt, m.QueuePosition = old.Court, old.ScheduledAt, old.QueuePosition
		}
	}
	return !same
}

// toPlay says a draw-ready bracket match is one to fight rather than a bye or
// a dead slot, which the build marks completed.
func toPlay(m *state.BracketMatch) bool {
	return m.Status == state.MatchStatusScheduled
}

// bracketMatches lists every match of b, its rounds and the bronze, for
// writing in place.
func bracketMatches(b *state.Bracket) []*state.BracketMatch {
	if b == nil {
		return nil
	}
	var out []*state.BracketMatch
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			out = append(out, &b.Rounds[ri][mi])
		}
	}
	if b.ThirdPlaceMatch != nil {
		out = append(out, b.ThirdPlaceMatch)
	}
	return out
}

// describe fills the answer's account of what the plan does: where each
// competitor goes, the numbers they trade, the byes that change hands and
// the separation rows that change. snap is the draw before the plan.
func (p *swapPlan) describe(snap swapSnapshot, req SwapRequest, source DrawSource) {
	comp := snap.comp
	who := map[string]domain.Player{}
	for _, side := range []SwapSide{req.A, req.B} {
		who[side.ID] = swapCompetitor(snap, side.ID)
	}
	afterPlace := map[string]DrawPlace{req.A.ID: p.answer.Places.A, req.B.ID: p.answer.Places.B}
	beforePlace := map[string]DrawPlace{req.A.ID: p.answer.Places.B, req.B.ID: p.answer.Places.A}
	if p.answer.Reason == SwapAlreadySwapped {
		beforePlace = afterPlace
	}

	p.answer.Moves = []SwapMove{}
	for _, side := range []SwapSide{req.A, req.B} {
		c := who[side.ID]
		p.answer.Moves = append(p.answer.Moves, SwapMove{
			ID: c.ID, Name: c.Name, DisplayName: c.DisplayName,
			From: clonePlace(beforePlace[side.ID]), To: clonePlace(afterPlace[side.ID]),
		})
	}

	if comp.EffectiveNumberPrefix() != "" {
		before := placeNumbers(comp, snap.pools, snap.bracket, source)
		after := placeNumbers(comp, p.pools, p.bracket, source)
		for _, side := range []SwapSide{req.A, req.B} {
			from, to := before[side.ID], after[side.ID]
			if p.answer.Reason == SwapAlreadySwapped {
				from = to
			}
			if from == "" && to == "" {
				continue
			}
			p.answer.Numbers = append(p.answer.Numbers, SwapNumber{ID: side.ID, Name: who[side.ID].Name, From: from, To: to})
		}
	}

	if source == DrawInBracket {
		had, has := byeHolders(snap.bracket), byeHolders(p.bracket)
		for _, side := range []SwapSide{req.A, req.B} {
			if had[side.ID] != has[side.ID] {
				p.answer.Byes = append(p.answer.Byes, SwapBye{ID: side.ID, Name: who[side.ID].Name, Bye: has[side.ID]})
			}
		}
	}

	before := DojoSeparation(comp, snap.pools, snap.bracket, snap.roster)
	after := DojoSeparation(comp, p.pools, p.bracket, snap.roster)
	touched := map[string]bool{}
	for _, side := range []SwapSide{req.A, req.B} {
		if key := helper.DojoKey(who[side.ID].Dojo); key != "" {
			touched[key] = true
		}
	}
	beforeByDojo, afterByDojo := meetingsByDojo(before), meetingsByDojo(after)
	for key := range afterByDojo {
		if !slices.Equal(beforeByDojo[key], afterByDojo[key]) {
			touched[key] = true
		}
	}
	for key := range beforeByDojo {
		if !slices.Equal(beforeByDojo[key], afterByDojo[key]) {
			touched[key] = true
		}
	}
	keep := func(rows []DojoMeeting) []DojoMeeting {
		out := []DojoMeeting{}
		for _, r := range rows {
			if touched[helper.DojoKey(r.Dojo)] {
				out = append(out, r)
			}
		}
		return out
	}
	p.answer.Separation = SwapSeparation{Before: keep(before), After: keep(after)}
}

// swapCompetitor is the competitor id names, as the draw writes them: the
// pools.csv row for a pools draw, else the roster.
func swapCompetitor(snap swapSnapshot, id string) domain.Player {
	if i, j, ok := poolRowOf(snap.pools, id); ok {
		return snap.pools[i].Players[j]
	}
	for _, p := range snap.roster {
		if p.ID == id {
			return p
		}
	}
	return domain.Player{ID: id}
}

// placeNumbers maps each drawn competitor's id to the number of the place
// they hold: the pools.csv row's Number for a pools draw, the number composed
// from DrawOrder's position for a knockout-only draw.
func placeNumbers(comp *state.Competition, pools []helper.Pool, bracket *state.Bracket, source DrawSource) map[string]string {
	out := map[string]string{}
	switch source {
	case DrawInPools:
		for _, p := range pools {
			for _, pl := range p.Players {
				if pl.ID != "" {
					out[pl.ID] = pl.Number
				}
			}
		}
	case DrawInBracket:
		if bracket == nil {
			return out
		}
		prefix := comp.EffectiveNumberPrefix()
		for id, pos := range drawPositions(bracket.DrawOrder) {
			out[id] = helper.CompetitorNumber(prefix, pos+1)
		}
	}
	return out
}

// byeHolders is the set of competitors whose first-round match of a
// knockout-only draw has nobody on the other side.
func byeHolders(bracket *state.Bracket) map[string]bool {
	out := map[string]bool{}
	if bracket == nil || len(bracket.Rounds) == 0 {
		return out
	}
	for _, m := range bracket.Rounds[0] {
		switch {
		case m.SideAID != "" && m.SideB == "" && m.SideBID == "":
			out[m.SideAID] = true
		case m.SideBID != "" && m.SideA == "" && m.SideAID == "":
			out[m.SideBID] = true
		}
	}
	return out
}

// meetingsByDojo keys each summary row by its dojo and reduces it to what the
// separation is: the pair (unordered) and where they meet. A dojo whose list
// differs before and after is one the swap changed.
func meetingsByDojo(rows []DojoMeeting) map[string][]string {
	out := map[string][]string{}
	matchOf := func(m *BracketMeeting) string {
		if m == nil {
			return ""
		}
		return m.MatchID
	}
	for _, r := range rows {
		a, b := r.A.ID, r.B.ID
		if b < a {
			a, b = b, a
		}
		key := helper.DojoKey(r.Dojo)
		out[key] = append(out[key], fmt.Sprintf("%s|%s|%s|%s|%s", a, b, r.SamePool, matchOf(r.Earliest), matchOf(r.IfBothWin)))
	}
	for k := range out {
		slices.Sort(out[k])
	}
	return out
}

// poolRowOf finds the pools.csv row holding id.
func poolRowOf(pools []helper.Pool, id string) (pool, row int, ok bool) {
	if id == "" {
		return 0, 0, false
	}
	for i := range pools {
		for j := range pools[i].Players {
			if pools[i].Players[j].ID == id {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// save stages the files the plan changed in tx.
func (p *swapPlan) save(tx state.StoreTx, compID string) error {
	if p.poolsChanged {
		if err := tx.SavePools(compID, p.pools); err != nil {
			return fmt.Errorf("saving pools: %w", err)
		}
	}
	if p.poolMatchesChanged {
		if err := tx.SavePoolMatches(compID, p.poolMatches); err != nil {
			return fmt.Errorf("saving pool matches: %w", err)
		}
	}
	if p.bracketChanged && p.bracket != nil {
		if err := tx.SaveBracket(compID, p.bracket); err != nil {
			return fmt.Errorf("saving bracket: %w", err)
		}
	}
	return nil
}

func clonePools(pools []helper.Pool) []helper.Pool {
	out := make([]helper.Pool, len(pools))
	for i, p := range pools {
		out[i] = p
		out[i].Players = slices.Clone(p.Players)
	}
	return out
}

func clonePoolMatches(matches []state.MatchResult) []state.MatchResult {
	out := slices.Clone(matches)
	for i := range out {
		out[i].GroupStamps = maps.Clone(out[i].GroupStamps)
	}
	return out
}

// cloneBracket copies a bracket deep enough for exchangePlaces to write its
// sides, winners, stamps and DrawOrder without touching the original.
// seatBracketSide replaces a match's SubResults rather than editing it, so
// the rows may stay shared.
func cloneBracket(b *state.Bracket) *state.Bracket {
	if b == nil {
		return nil
	}
	out := *b
	out.Rounds = make([][]state.BracketMatch, len(b.Rounds))
	for i, round := range b.Rounds {
		out.Rounds[i] = slices.Clone(round)
		for j := range out.Rounds[i] {
			out.Rounds[i][j].GroupStamps = maps.Clone(out.Rounds[i][j].GroupStamps)
		}
	}
	if b.ThirdPlaceMatch != nil {
		bm := *b.ThirdPlaceMatch
		bm.GroupStamps = maps.Clone(bm.GroupStamps)
		out.ThirdPlaceMatch = &bm
	}
	out.DrawOrder = slices.Clone(b.DrawOrder)
	return &out
}

package engine

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"sort"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The blank layout: the workbook a competition with no draw gets from
// ExportCompetitionXlsx, so the "Download blank template" button and the
// Export PDFs prints have something to print before the draw (operator
// decisions E2 and E3, 2026-11-18). It is the layout the draw would print for
// the same number of places, with every place empty: no name, no dojo, no seed,
// no number. Nothing is written to the store.
//
// How many places is blankPlaceCount's. Their shape is the draw's own: the
// places go through the same pool, pool-match and knockout generators the draw
// runs (blankDraw), and the workbook through the same sheet pipeline the stored
// draw prints through (renderWorkbook, in its blank mode), then finishTemplate.

// ErrBlankLayoutTooFew is returned by ExportCompetitionXlsx for a competition
// with no draw and fewer than two places to print: no expected number of
// participants above that, and fewer than two registered. It is one of the
// IsUnexportable sentinels, so the export door answers it as a refusal naming
// the fix and Export PDFs leaves the competition out with it. Its text is
// operator copy.
var ErrBlankLayoutTooFew = errors.New("no draw yet, and fewer than 2 places to print: set the expected number of participants in this competition's Settings, or add participants")

// ErrBlankLayoutUnbuildable is returned, wrapped with the number of places and
// the reason, when the competition's settings cannot form a draw for the places
// blankPlaceCount gives (a pools + knockout competition that would form one
// pool, a pool smaller than the number it sends on). The draw would refuse the
// same settings. Also an IsUnexportable sentinel, and also operator copy.
var ErrBlankLayoutUnbuildable = errors.New("no draw yet, and this competition's settings cannot form a draw")

// minBlankPlaces is the fewest places a blank layout prints: the fewest a draw
// can pair.
const minBlankPlaces = 2

// blankPlaceCount is how many empty places the blank layout prints: the
// schedule estimate's count rule (countForEstimate), the larger of the
// organiser's expected number and everyone registered. Everyone registered,
// checked in or not, even where check-in applies (operator decision Q4 (a),
// 2026-11-18): the template is printed ahead of the day, so it may be larger
// than the schedule estimate, which counts check-ins. No seeds are read: the
// layout places nobody. Fewer than minBlankPlaces is ErrBlankLayoutTooFew
// (operator decision Q1 (a)).
func (e *Engine) blankPlaceCount(comp *state.Competition) (int, error) {
	players, err := e.store.LoadParticipants(comp.ID, comp.EffectiveWithZekkenName())
	if err != nil {
		return 0, err
	}
	n, _ := countForEstimate(comp.ExpectedParticipants, len(players))
	if n < minBlankPlaces {
		return 0, ErrBlankLayoutTooFew
	}
	return n, nil
}

// blankLayoutXlsx renders the blank layout of a competition with no draw.
// tourn is the caller's strict load (it may be nil when there is no
// tournament.md), read for the competition's shiaijo by name.
//
// No Names to Print sheet and no Tags sheet: there is nobody to name.
func (e *Engine) blankLayoutXlsx(comp *state.Competition, tourn *state.Tournament) ([]byte, error) {
	n, err := e.blankPlaceCount(comp)
	if err != nil {
		return nil, err
	}
	courts := CompetitionCourts(comp, tourn)
	layout, err := blankDraw(comp, courts, n)
	if err != nil {
		return nil, err
	}

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("engine: blank layout %q: closing the workbook failed: %v", comp.ID, err)
		}
	}()

	if _, _, err := renderWorkbook(f, workbookInput{
		comp:        layout.comp,
		pools:       layout.pools,
		courts:      courts,
		courtOfPool: PoolCourtByName(layout.poolMatches),
		draw:        layout.draw,
		roster:      layout.roster,
		blank:       true,
	}); err != nil {
		return nil, err
	}
	if err := helper.BlankCellsHolding(f, placeTexts(layout.places)); err != nil {
		return nil, err
	}
	finishTemplate(f, layout.comp, layout.pools, layout.draw, len(courts))

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// blankLayout is a draw of empty places, held in memory.
type blankLayout struct {
	// comp is the copy of the competition the draw ran on: its shiaijo
	// materialised, no number prefix, and the draw's own defaults applied (a
	// team competition's team size, a league's single round-robin pool).
	comp *state.Competition
	// places are the empty places, in the order they were drawn from.
	places []helper.Player
	// pools carry their matches (AttachPoolMatches), as the stored-draw
	// export's do; poolMatches are those matches as the draw would store them.
	pools       []helper.Pool
	poolMatches []state.MatchResult
	draw        *helper.KnockoutDraw
	// roster is a knockout-only draw's places, top of the bracket to the
	// bottom: its Data sheet rows, which the first round refers to.
	roster []helper.Player
}

// blankDraw draws n empty places over comp's settings, the way runDrawPipeline
// draws a roster, and stores nothing.
func blankDraw(comp *state.Competition, courts []string, n int) (*blankLayout, error) {
	c := *comp
	c.Courts = courts
	c.NumberPrefix = ""
	if c.IsTeam() && c.TeamSize == 0 {
		c.TeamSize = 5 // runDrawPipeline's default for a team competition
	}
	numCourts := max(len(courts), 1)
	places := blankPlaces(&c, n)
	layout := &blankLayout{comp: &c, places: places}

	switch c.Format {
	case state.CompFormatMixed, state.CompFormatLeague:
		if c.Format == state.CompFormatLeague {
			// runDrawPipeline's single-pool invariant for a league.
			c.PoolSize = n
			c.RoundRobin = true
		}
		pools, drawCourts, hasRounds, err := blankPoolPhase(&c, places, numCourts)
		if err != nil {
			return nil, err
		}
		layout.poolMatches = poolMatchResults(&c, pools, drawCourts, hasRounds)
		AttachPoolMatches(pools, layout.poolMatches)
		layout.pools = pools
		layout.draw = EliminationDraw(&c, pools, nil, len(courts))
	default:
		// A standalone knockout, as runDrawPipeline's default case draws one
		// (generateKnockout): seeding order, the balanced tree, then the pow2
		// slot list it stores (buildBracketFromDraw), printed as
		// EliminationDraw prints a stored bracket's first round.
		seeded := helper.StandardSeeding(places)
		names := make([]string, len(seeded))
		keyOf := make(map[string]string, len(seeded))
		for i, p := range seeded {
			names[i] = p.Name
			keyOf[p.Name] = helper.PlayerKey(p)
		}
		stored := helper.NewKnockoutDraw(helper.CreateBalancedTree(names), len(courts))
		if stored == nil || stored.Root == nil {
			return nil, blankUnbuildable(n, "the knockout could not be drawn")
		}
		leaves := helper.SlotArray(stored.Root)
		keys := make([]string, len(leaves))
		for i, leaf := range leaves {
			keys[i] = keyOf[leaf]
		}
		layout.draw = knockoutDrawFromLeaves(leaves, keys, len(courts))
		layout.roster = seeded
	}
	return layout, nil
}

// blankPlaces makes n empty places: each a stand-in with a distinct id, name
// and dojo (and display name when the competition prints one), so the
// generators, which refuse a blank name or dojo and read an empty bracket side
// as a bye, draw them as competitors, and no two share a dojo, so the dojo
// spread has nothing to move. No seed, no number. The stand-in text never
// reaches the workbook: helper.BlankCellsHolding blanks every cell that prints
// it (placeTexts).
func blankPlaces(comp *state.Competition, n int) []helper.Player {
	places := make([]helper.Player, n)
	for i := range places {
		places[i] = helper.Player{
			ID:           "blank-" + strconv.Itoa(i+1),
			Name:         fmt.Sprintf("Blank place %d", i+1),
			Dojo:         fmt.Sprintf("Blank dojo %d", i+1),
			PoolPosition: int64(i),
		}
		if comp.EffectiveWithZekkenName() {
			places[i].DisplayName = fmt.Sprintf("Blank display %d", i+1)
		}
	}
	return places
}

// placeTexts is every stand-in text the places carry, the set
// helper.BlankCellsHolding blanks.
func placeTexts(places []helper.Player) map[string]bool {
	texts := make(map[string]bool, 3*len(places))
	for _, p := range places {
		for _, t := range []string{p.Name, p.Dojo, p.DisplayName} {
			if t != "" {
				texts[t] = true
			}
		}
	}
	return texts
}

// blankUnbuildable is ErrBlankLayoutUnbuildable for n places, with the reason.
func blankUnbuildable(n int, reason string) error {
	return fmt.Errorf("%w for %d places: %s; change the expected number of participants or the pool settings in this competition's Settings", ErrBlankLayoutUnbuildable, n, reason)
}

// blankPoolPhase forms the pool phase over the places with the generators and
// refusals generatePools applies, and stores nothing: the pool count and sizes
// (helper.BuildPoolPhaseFillBracket or helper.BuildPoolPhaseTreeAwareWithMode
// by the extra-qualifier mode), the pools + knockout refusals, the pool matches
// (partial, round robin or full) and the one-pool court check. No numbers.
// hasRounds reports whether the matches carry a round.
func blankPoolPhase(c *state.Competition, places []helper.Player, numCourts int) (pools []helper.Pool, drawCourts int, hasRounds bool, err error) {
	n := len(places)
	if c.PoolSize <= 0 {
		return nil, 0, false, blankUnbuildable(n, "the pool size is not set")
	}
	poolFedKnockout := c.Format == state.CompFormatMixed
	if poolFedKnockout {
		if verr := state.ValidateExtraQualifiers(c.ExtraQualifiers, c.PoolSizeMode, c.EffectivePoolWinners()); verr != nil {
			return nil, 0, false, blankUnbuildable(n, verr.Error())
		}
	}
	if poolFedKnockout && c.ExtraQualifiers == state.ExtraQualifiersFillBracket {
		pools, drawCourts, err = helper.BuildPoolPhaseFillBracket(places, c.PoolSize, numCourts)
	} else {
		extraQualifiers := state.ExtraQualifiersNone
		if poolFedKnockout {
			extraQualifiers = c.ExtraQualifiers
		}
		pools, drawCourts, err = helper.BuildPoolPhaseTreeAwareWithMode(places, c.PoolSize, c.PoolSizeMode == "max", numCourts, c.EffectivePoolWinners(), extraQualifiers)
	}
	if err != nil {
		return nil, 0, false, blankUnbuildable(n, err.Error())
	}
	if poolFedKnockout {
		if len(pools) < 2 {
			return nil, 0, false, blankUnbuildable(n, fmt.Sprintf("a pools + knockout competition needs at least 2 pools, and a pool size of %d makes %d", c.PoolSize, len(pools)))
		}
		winners := c.EffectivePoolWinners()
		for _, p := range pools {
			if len(p.Players) < winners {
				return nil, 0, false, blankUnbuildable(n, fmt.Sprintf("pool %s would have %d place(s) but %d go on to the knockout", p.PoolName, len(p.Players), winners))
			}
		}
	}
	switch c.PoolFormat {
	case state.PoolFormatPartial:
		helper.CreatePartialPoolMatches(pools)
		hasRounds = true
	default:
		if c.RoundRobin {
			helper.CreatePoolRoundRobinMatches(pools)
			hasRounds = true
		} else {
			helper.CreatePoolMatches(pools)
		}
	}
	if len(pools) == 1 && numCourts > 1 {
		if verr := ValidateCourtCount(n, numCourts); verr != nil {
			return nil, 0, false, blankUnbuildable(n, verr.Error())
		}
	}
	return pools, drawCourts, hasRounds, nil
}

// poolMatchResults lays the pools' matches out as generatePools stores them,
// without the times: each with its pool's shiaijo (a one-pool competition's
// spread over every shiaijo it was given, a league's by the league scheduler),
// numbered in playing order (numberPoolMatchesInPlayingOrder), so the Pool
// Matches sheet prints them in the order and under the shiaijo the draw would.
func poolMatchResults(c *state.Competition, pools []helper.Pool, drawCourts int, hasRounds bool) []state.MatchResult {
	courtAssign := helper.AssignPoolsToCourts(len(pools), drawCourts)
	var results []state.MatchResult
	for pi, p := range pools {
		poolCourts := []string{""}
		if len(c.Courts) > 0 {
			poolCourts = []string{c.Courts[courtAssign[pi]]}
			if len(pools) == 1 && len(c.Courts) > 1 {
				poolCourts = c.Courts
			}
		}
		for i, m := range p.Matches {
			round := m.Round
			if !hasRounds {
				round = -1
			}
			results = append(results, state.MatchResult{
				ID:      p.PoolName + "-" + strconv.Itoa(i),
				SideA:   m.SideA.Name,
				SideB:   m.SideB.Name,
				SideAID: m.SideA.ID,
				SideBID: m.SideB.ID,
				Status:  state.MatchStatusScheduled,
				Court:   poolCourts[i%len(poolCourts)],
				Round:   round,
			})
		}
	}
	switch {
	case c.Format == state.CompFormatLeague:
		results, _ = scheduleLeagueSlots(results, c.Courts)
	case len(pools) == 1 && len(c.Courts) > 1:
		sort.SliceStable(results, func(i, j int) bool { return results[i].Round < results[j].Round })
		roundStart, currentRound := 0, -1
		for i := range results {
			if results[i].Round != currentRound {
				currentRound, roundStart = results[i].Round, 0
			}
			results[i].Court = c.Courts[roundStart%len(c.Courts)]
			roundStart++
		}
	}
	numberPoolMatchesInPlayingOrder(results)
	return results
}

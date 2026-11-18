package engine

import (
	"bytes"
	"errors"
	"fmt"
	"log"
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
	players, err := e.store.LoadParticipantsOpt(comp.ID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: false, HasIDs: comp.ParticipantIDsHint()})
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
	// The real draw's own court rule (generateDraw refuses the same count).
	if err := ValidateCompetitionShiaijoCount(courts, comp.Format); err != nil {
		return nil, blankUnbuildable(n, err.Error())
	}
	c := *comp
	c.Courts = courts
	c.NumberPrefix = ""
	applyDefaultTeamSize(&c)
	places := blankPlaces(&c, n)
	layout := &blankLayout{comp: &c, places: places}

	switch c.Format {
	case state.CompFormatMixed, state.CompFormatLeague:
		applyLeagueSinglePool(&c, n)
		// The draw's own pool phase (formPools, createPoolMatches,
		// layoutPoolMatches), refusals included, minus the numbering, the start
		// times and the store.
		pools, drawCourts, err := formPools(&c, places)
		if err != nil {
			return nil, blankUnbuildable(n, err.Error())
		}
		hasRounds := createPoolMatches(&c, pools)
		layout.poolMatches, _ = layoutPoolMatches(&c, pools, drawCourts, hasRounds)
		numberPoolMatchesInPlayingOrder(layout.poolMatches)
		AttachPoolMatches(pools, layout.poolMatches)
		layout.pools = pools
		if c.Format == state.CompFormatMixed {
			// The draw's knockout, refused as generatePoolPreviewBracket
			// refuses it: never a workbook without the Tree sheets.
			draw, outOfScope, reason := buildPoolFedDraw(&c, pools, len(courts))
			if outOfScope || draw == nil {
				if reason == "" {
					reason = blankKnockoutUndrawable
				}
				return nil, blankUnbuildable(n, reason)
			}
			layout.draw = draw
		}
	default:
		// A standalone knockout, drawn as generateKnockout draws one
		// (seededKnockoutDraw), then the pow2 slot list it stores
		// (buildBracketFromDraw), printed as EliminationDraw prints a stored
		// bracket's first round.
		seeded, stored := seededKnockoutDraw(places, len(courts))
		keyOf := make(map[string]string, len(seeded))
		for _, p := range seeded {
			keyOf[p.Name] = helper.PlayerKey(p)
		}
		if stored == nil || stored.Root == nil {
			return nil, blankUnbuildable(n, blankKnockoutUndrawable)
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

const blankKnockoutUndrawable = "the knockout could not be drawn; change the expected number of participants or the pool settings in this competition's Settings"

// blankUnbuildable is ErrBlankLayoutUnbuildable for n places, with the reason.
// The reason is operator wording that already names its own remedy (the draw's
// refusals do); blankKnockoutUndrawable is the one that needs this file's.
func blankUnbuildable(n int, reason string) error {
	return fmt.Errorf("%w for %d places: %s", ErrBlankLayoutUnbuildable, n, reason)
}

package engine

import (
	"bytes"
	"log"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ExportCompetitionXlsx renders the competition's template with no results: the
// export behind GET /api/competitions/:id/export (the app's "Download blank
// template") and the PDF print export (ExportTournamentWorkbooks). It decides
// which template a competition gets:
//
//   - With a draw (any status but setup), its stored draw (pools.csv,
//     pool-matches.csv, bracket.json), so the template, the printed PDFs and
//     the matches the app runs come from the one stored draw.
//   - With no draw (state.CanGenerateDraw: setup, or a legacy record with no
//     status), the blank layout (blankLayoutXlsx): the layout the draw would
//     print, its places empty, as many as blankPlaceCount gives. Nothing on
//     disk is read as a draw then: a setup competition's pools.csv or
//     bracket.json, if any, is the leftover of a draw that was refused or
//     discarded. ErrBlankLayoutTooFew or ErrBlankLayoutUnbuildable when it
//     cannot be sized or laid out.
//
// Its pool counting formulas (W/L/T/IV/PW/PL/RANK) are live: AttachPoolMatches
// points each match's sides INTO its own pool's Players slice, the pointer
// identity helper.PrintPoolMatches links a player's formulas by (mp-x0u9 was
// the stored pools' matches pointing at another slice, which printed "=0").
// The finish /create gives its workbook is given here too (finishTemplate), so
// the CLI's template and the app's differ only in the draw they print.
func (e *Engine) ExportCompetitionXlsx(id string) ([]byte, error) {
	comp, err := e.store.LoadCompetition(id)
	if err != nil {
		return nil, err
	}
	if comp == nil {
		return nil, notFoundErrorf("competition %s not found", id)
	}

	// Swiss has no pools and no static bracket (results are per-round pairings
	// and a running standings table), so there is nothing to render into the
	// pool/tree layout this function produces. Block it explicitly, before any
	// rendering work, rather than emitting a workbook whose sheets are
	// structurally present but hold no participant data. Matches the guard in
	// internal/export.BuildResultsWorkbook. A dedicated Swiss sheet is tracked
	// as follow-up work (mp-4n9n).
	if comp.Format == state.CompFormatSwiss {
		return nil, ErrSwissExportUnsupported
	}

	if state.CanGenerateDraw(comp.Status) {
		tourn, err := e.store.LoadTournament()
		if err != nil {
			return nil, err
		}
		return e.blankLayoutXlsx(comp, tourn)
	}

	pools, err := e.store.LoadPools(id)
	if err != nil {
		return nil, err
	}

	// bc-pnum review: report, never fail, an unnumbered POOLED
	// competitor under a numbered competition. AddPoolDataToSheet /
	// AddPlayerDataToSheet degrade silently (that is D1's own
	// report-over-fabricate rule -- no fabricated number is ever printed),
	// so nothing downstream would otherwise say WHY a competitor's Player
	// Number / tag / Names-to-Print cell came out blank. An empty
	// comp.EffectiveNumberPrefix() is excluded on purpose: a Swiss
	// competition is unnumbered by design (see NumberPools/
	// AssignPlayerNumbers's own doc comments), so an empty Number there is
	// normal, not a gap to report.
	if effectiveNumberPrefix := comp.EffectiveNumberPrefix(); effectiveNumberPrefix != "" {
		if name, dojo, ok := firstUnnumberedPooledCompetitor(pools); ok {
			log.Printf("engine: ExportCompetitionXlsx compId=%s: competitor %q (dojo %q) has no Number under prefix %q; its tag/Player-Number/Names-to-Print cells will print blank",
				id, name, dojo, effectiveNumberPrefix)
		}
	}

	// The Pool Matches grid comes from the stored results, as it does in the
	// results export (AttachPoolMatches): pools.csv records membership only, and
	// the matches the draw left in the store's cache are gone after a restart,
	// which printed a template with no match blocks. Strict like the bracket
	// load below, so a corrupt pool-matches.csv fails the export rather than
	// print one; a missing file (no draw yet) is not an error.
	poolMatches, err := e.store.LoadPoolMatches(id)
	if err != nil {
		return nil, err
	}
	AttachPoolMatches(pools, poolMatches)
	// Where each pool is ACTUALLY being fought, so it bands under that shiaijo.
	courtOfPool := PoolCourtByName(poolMatches)

	// The tournament, loaded ONCE and strictly (mp-yuy8 criterion 6): both the
	// shiaijo list below and the Tags sheet's publicURL near the end of this
	// function read from this single load, so the two can never disagree and a
	// corrupt tournament.md aborts the export instead of silently printing
	// positional court labels on one sheet. A MISSING tournament.md is not an
	// error -- LoadTournament returns (nil, nil) for that (state/tournament.go)
	// -- so a competition with no tournament record yet still exports.
	tourn, err := e.store.LoadTournament()
	if err != nil {
		return nil, err
	}

	// The shiaijo BY NAME, for every sheet that prints one. The count is read
	// off the same list rather than derived a second time, so the two can never
	// disagree; CompetitionCourts owns the inheritance and the single-court fallback.
	courts := CompetitionCourts(comp, tourn)
	numCourts := len(courts)

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return nil, err
	}
	defer func() {
		// The workbook's bytes are already taken by then, so a close failure
		// (a temp file excelize could not remove) is logged, not returned.
		if err := f.Close(); err != nil {
			log.Printf("engine: export %q: closing the workbook failed: %v", id, err)
		}
	}()

	// Load the stored bracket ONCE, unconditionally, strictly (mp-yuy8 criterion
	// 4). It used to load only for naginata/pure-knockout and otherwise fall
	// through best-effort on a load error, silently continuing with a nil
	// bracket -- but the bracket carries the LIVE court of every bout (the
	// operator reassigns matches between shiaijo as the day runs), which is the
	// only correct source for the elimination sheet's bands; a nil bracket bands
	// by the draw's regions instead, i.e. prints score sheets under the wrong
	// court rather than failing. A MISSING bracket.json is not an error --
	// parseBracketFile returns an empty non-nil bracket for a not-yet-drawn
	// competition (state/bracket.go) -- so league/swiss/mixed competitions that
	// never had a bracket are unaffected; only a corrupt/unreadable file fails.
	bracket, err := e.store.LoadBracket(id)
	if err != nil {
		return nil, err
	}

	// Elimination leaves for the knockout phase, shared with the results workbook
	// (EliminationDraw) so both exports of one competition render the identical
	// bracket: pool winners for pooled formats, or the stored bracket's leaves for
	// a pure knockout competition (mp-ndfu, mp-0yd8). RenderCompetitionWorkbook's
	// own gate then drops the phantom bracket a league's placeholder finals imply.
	draw := EliminationDraw(e.store, comp, pools, bracket, numCourts)

	// Kachinuki Detail sheet's bout log. Opt-in: WriteKachinukiDetailSheet
	// no-ops on empty input, so this is safe for a fixed-format competition.
	kachinukiMatches, err := e.collectKachinukiMatches(id, comp)
	if err != nil {
		return nil, err
	}

	// Representative Bouts sheet's bouts. Opt-in the same way: only a
	// fixed-order team competition whose draw holds a representative bout has
	// any.
	representativeBouts, err := e.collectRepresentativeBouts(id, comp)
	if err != nil {
		return nil, err
	}

	// The shared sheet pipeline (mp-yuy8): Data, Pool Draw, Pool Matches,
	// knockout, Tree cleanup, Names to Print, Kachinuki Detail, Representative
	// Bouts -- identical
	// steps and order to internal/export.BuildResultsWorkbook.
	// RenderCompetitionWorkbook derives namesToPrintPlayers via
	// KnockoutNamesToPrint (numbering.go, bc-pnum A8: a
	// knockout-only competition never has a pools.csv, so feeding it the
	// empty pools slice alone would make its Data and Names-to-Print steps
	// no-ops) -- internal/export.BuildResultsWorkbook resolves through the
	// SAME derivation, so the two exports of one competition agree on
	// whether that sheet exists at all.
	_, namesToPrintPlayers, err := e.RenderCompetitionWorkbook(f, comp, pools, bracket, courts, courtOfPool, draw, kachinukiMatches, representativeBouts)
	if err != nil {
		return nil, err
	}

	// Tags sheet, the stored-draw export's own extra: pass publicURL so numbered
	// tags get an embedded QR code. tourn (loaded once, strictly, above) may
	// legitimately be nil for a competition with no tournament record yet,
	// which simply omits QR codes without aborting the export. CreateTagsSheet
	// errors (e.g. Excel write failures) still propagate.
	var publicURL string
	if tourn != nil {
		publicURL = tourn.PublicURL
	}
	// namesToPrintPlayers is the SAME numbered roster the Names-to-Print
	// sheet above just used (RenderCompetitionWorkbook's second return
	// value), not re-derived here: CreateTagsSheet needs it for the
	// identical knockout-only shape, and deriving it a second time would
	// just be a second KnockoutNamesToPrint call over the same inputs.
	tagsPools := pools
	if namesToPrintPlayers != nil {
		// Same numbered roster as the Names-to-Print sheet above (bc-pnum A8):
		// CreateTagsSheet only reads player.Number as a literal value, so a
		// single synthetic pool wrapping the roster is enough; PoolName is
		// never read by this sheet.
		tagsPools = []helper.Pool{{Players: namesToPrintPlayers}}
	}
	if err := helper.CreateTagsSheet(f, tagsPools, publicURL, comp.EffectiveNumberPrefix()); err != nil {
		return nil, err
	}
	finishTemplate(f, comp, pools, draw, numCourts)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// finishTemplate gives the stored-draw workbook the finish /create gives its
// blank template (cmd/create-pools.go, cmd/create-knockout.go): the Time
// Estimator sheet filled in for this draw, and every sheet but the editable
// ones protected against an accidental edit of a formula
// (helper.ProtectAllSheets). Neither changes a printed page: the PDF groups
// print no Time Estimator, and a protected sheet prints as it is.
//
// The counts are the ones /create passes: the pools and the matches the Pool
// Matches sheet prints (AttachPoolMatches has filled pool.Matches), the team
// size, and n-1 knockout matches for the n entrants of the draw this workbook
// prints, none for a competition with no knockout (a league).
func finishTemplate(f *excelize.File, comp *state.Competition, pools []helper.Pool, draw *helper.KnockoutDraw, numCourts int) {
	var poolMatches int
	for _, p := range pools {
		poolMatches += len(p.Matches)
	}
	var knockoutMatches int
	if draw != nil && comp.IsKnockoutEnabled() {
		knockoutMatches = max(helper.CountLeaves(draw.Root)-1, 0)
	}
	helper.FillEstimations(f, int64(len(pools)), int64(poolMatches), int64(comp.TeamSize), int64(knockoutMatches), numCourts)
	helper.ProtectAllSheets(f)
}

// firstUnnumberedPooledCompetitor returns the first competitor across every
// pool, in pool then in-pool order, whose Number is blank -- for
// ExportCompetitionXlsx's bc-pnum review report-gap log line above. ok is
// false when pools is empty or every competitor carries a Number.
func firstUnnumberedPooledCompetitor(pools []helper.Pool) (name, dojo string, ok bool) {
	for _, pool := range pools {
		for _, p := range pool.Players {
			if p.Number == "" {
				return p.Name, p.Dojo, true
			}
		}
	}
	return "", "", false
}

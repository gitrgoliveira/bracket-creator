package engine

import (
	"bytes"

	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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

	pools, err := e.store.LoadPools(id)
	if err != nil {
		return nil, err
	}

	// Where each pool is ACTUALLY being fought. Best-effort for the same reason
	// the bracket load below is: a competition with no pool matches on disk
	// simply bands by the drawn allocation, which is what this did before.
	var courtOfPool map[string]string
	if poolMatches, poolErr := e.store.LoadPoolMatches(id); poolErr == nil {
		courtOfPool = PoolCourtByName(poolMatches)
	}

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
		_ = f.Close()
	}()

	// Load the stored bracket ONCE, unconditionally, strictly (mp-yuy8 criterion
	// 4). It used to load only for naginata/pure-playoffs and otherwise fall
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
	// a pure playoffs competition (mp-ndfu, mp-0yd8). RenderCompetitionWorkbook's
	// own gate then drops the phantom bracket a league's placeholder finals imply.
	draw := EliminationDraw(e.store, comp, pools, bracket, numCourts)

	// Kachinuki Detail sheet's bout log. Opt-in: WriteKachinukiDetailSheet
	// no-ops on empty input, so this is safe for a fixed-format competition.
	kachinukiMatches, err := e.collectKachinukiMatches(id, comp)
	if err != nil {
		return nil, err
	}

	// The shared sheet pipeline (mp-yuy8): Data, Pool Draw, Pool Matches,
	// knockout, Tree cleanup, Names to Print, Kachinuki Detail -- identical
	// steps and order to internal/export.BuildResultsWorkbook.
	if _, err := RenderCompetitionWorkbook(f, comp, pools, bracket, courts, courtOfPool, draw, kachinukiMatches); err != nil {
		return nil, err
	}

	// bc-pnum A8: a playoffs-only competition never has a pools.csv (its
	// numbers are participant order under the prefix, composed on read, see
	// NumberedParticipantsFor), so the pipeline above fed CreateNamesWithPoolToPrint
	// (its step 6) an empty pools slice, which is a no-op that deletes the
	// template Names-to-Print sheet and creates nothing. Below feeds the SAME
	// two auxiliary sheets (Names to Print and, further down, Tags) the
	// numbered roster instead, so a knockout-only competition's printed
	// materials are not silently blank. The Data sheet also needs real rows
	// for these players FIRST, because CreateNamesToPrint links its position
	// cell to one by FORMULA (the same mechanism the pooled path uses); the
	// pipeline's own step 1 (AddPoolDataToSheet) wrote only the column
	// headers for this competition, since pools was empty, so there is
	// nothing here to conflict with re-populating it via the CLI's own
	// flat-roster writer.
	var namesToPrintPlayers []helper.Player
	if comp.EffectiveFormat() == state.CompFormatPlayoffs && len(pools) == 0 && comp.NumberPrefix != "" {
		numbered, npErr := e.NumberedParticipantsFor(comp)
		if npErr != nil {
			return nil, npErr
		}
		namesToPrintPlayers = numbered
		playerCoords := helper.AddPlayerDataToSheet(f, numbered, comp.EffectiveWithZekkenName(), comp.Name)
		helper.CreateNamesToPrint(f, numbered, comp.EffectiveWithZekkenName(), courts, playerCoords)
	}

	// Tags sheet, blank-template-export-only extra: pass publicURL so numbered
	// tags get an embedded QR code. tourn (loaded once, strictly, above) may
	// legitimately be nil for a competition with no tournament record yet,
	// which simply omits QR codes without aborting the export. CreateTagsSheet
	// errors (e.g. Excel write failures) still propagate.
	var publicURL string
	if tourn != nil {
		publicURL = tourn.PublicURL
	}
	tagsPools := pools
	if namesToPrintPlayers != nil {
		// Same numbered roster as the Names-to-Print sheet above (bc-pnum A8):
		// CreateTagsSheet only reads player.Number as a literal value, so a
		// single synthetic pool wrapping the roster is enough; PoolName is
		// never read by this sheet.
		tagsPools = []helper.Pool{{Players: namesToPrintPlayers}}
	}
	if err := helper.CreateTagsSheet(f, tagsPools, publicURL); err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

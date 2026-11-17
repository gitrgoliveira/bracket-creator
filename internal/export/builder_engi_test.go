package export

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_EngiPairedNameInDataSheet verifies that for an engi
// competition (Engi=true, WithZekkenName=false) the combined pair name
// ("Name 1 - Name 2") is written to the Data sheet name column; engi does not
// alter the CSV/data-sheet layout.
func TestBuildResultsWorkbook_EngiPairedNameInDataSheet(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Mark competition as engi (WithZekkenName remains false).
	markCompAsEngi(t, store, compID)

	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// The Name column (B, index 1) must carry the combined pair name; engi does
	// not populate the zekken column (WithZekkenName stays false).
	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	assert.True(t, columnContains(rows, 1, "Member One A - Member Two A"),
		"Data sheet col B must contain the combined pair name for an engi competition")
}

// TestBuildResultsWorkbook_NonEngiWithZekkenStillWorks verifies additivity:
// a non-engi competition with WithZekkenName=true still writes DisplayName
// to col D (existing behaviour is not broken by the engi fix).
func TestBuildResultsWorkbook_NonEngiWithZekkenStillWorks(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Standard competition: no engi, but WithZekkenName=true.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.WithZekkenName = true
	require.NoError(t, store.SaveCompetition(comp))

	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	zekkenPool := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "p1", Name: "Name One", DisplayName: "ZEKKEN ONE", Dojo: "DojoX"},
			{ID: "p2", Name: "Name Two", DisplayName: "ZEKKEN TWO", Dojo: "DojoY"},
		},
	}
	zekkenPool.Matches = []helper.Match{{SideA: &zekkenPool.Players[0], SideB: &zekkenPool.Players[1]}}
	pools := []helper.Pool{zekkenPool}
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	assert.True(t, columnContains(rows, 3, "ZEKKEN ONE"),
		"Data sheet col D must contain 'ZEKKEN ONE' for WithZekkenName=true competition")
}

// TestBuildResultsWorkbook_EngiPoolFlagScoreCells verifies that for an engi pool
// match, the Pool Matches sheet renders the referee flag counts as literal numbers,
// not ippon letters. Previously broken because overlayPoolScores called
// domain.IpponsScore(mr.IpponsA) which returned "" (engi matches have no ippons).
// Both the 3-2 case and the 5-0 shutout are exercised: the loser's "0" must be
// written explicitly to distinguish a clean shutout from a kiken/fusenpai.
func TestBuildResultsWorkbook_EngiPoolFlagScoreCells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		flagsA     int
		flagsB     int
		wantLeft   string
		wantRight  string
		wantStands string // expected value in the Flags standings column
	}{
		// FlagsA is Aka's (SideA, right), FlagsB Shiro's (SideB, left).
		{
			name: "3-2", flagsA: 3, flagsB: 2,
			wantLeft: "2", wantRight: "3", wantStands: "3",
		},
		{
			// 5-0 shutout: the loser's "0" is a real score, distinguishing it from
			// a kiken/fusenpai where no flags were recorded.
			name: "5-0 shutout", flagsA: 5, flagsB: 0,
			wantLeft: "0", wantRight: "5", wantStands: "5",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			markCompAsEngi(t, store, compID)

			pools := makeEngiPools()
			require.NoError(t, store.SavePools(compID, pools))

			// SideAID/SideBID/WinnerID are required for computeEngiStandings so the
			// standings overlay can also write the accumulated flag total.
			results := []state.MatchResult{
				{
					ID:       "Pool A-0",
					SideA:    "Member One A",
					SideAID:  "pair1",
					SideB:    "Member One B",
					SideBID:  "pair2",
					FlagsA:   tc.flagsA,
					FlagsB:   tc.flagsB,
					Decision: "fought",
					Status:   state.MatchStatusCompleted,
					Winner:   "Member One A",
					WinnerID: "pair1",
				},
			}
			require.NoError(t, store.SavePoolMatches(compID, results))

			data, err := BuildResultsWorkbook(store, eng, compID)
			require.NoError(t, err)

			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer f.Close()

			rows, err := f.GetRows(helper.SheetPoolMatches)
			require.NoError(t, err)

			// Assert the MATCH ROW score cells specifically: left score (FlagsB) at
			// bandStart+1 and right score (FlagsA) at bandStart+5 (vs is at bandStart+3).
			// Using containsCell alone would be satisfied by the standings overlay alone.
			matchRow := firstPoolMatchScoreRow(rows, 0)
			require.NotNil(t, matchRow, "match score row must exist (White/Red header must be present)")
			require.Greater(t, len(matchRow), 5, "match score row must have at least 6 columns")
			assert.Equal(t, tc.wantLeft, matchRow[1],
				"left score cell (2 before vs at col 3) must be %q", tc.wantLeft)
			assert.Equal(t, tc.wantRight, matchRow[5],
				"right score cell (2 after vs at col 3) must be %q", tc.wantRight)

			// The winner's accumulated flag total also appears in the standings column.
			assert.True(t, columnHasValueUnderHeader(rows, helper.ColHeaderFlags, tc.wantStands),
				"Pool Matches 'Flags' standings column must carry %q for the engi winner", tc.wantStands)
		})
	}
}

// TestBuildResultsWorkbook_EngiNonEngiPoolScoreUnchanged verifies that a non-engi
// pool match still renders ippon letters (not flag counts) after the engi fix.
func TestBuildResultsWorkbook_EngiNonEngiPoolScoreUnchanged(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Non-engi competition.
	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideAID:  "Alice",
			SideB:    "Bob",
			SideBID:  "Bob",
			IpponsA:  []string{"M"},
			IpponsB:  []string{},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
			WinnerID: "Alice",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	assert.True(t, sheetContainsCell(rows, "M"),
		"Pool Matches sheet must still contain 'M' (ippon) for a non-engi match")
}

// TestBuildResultsWorkbook_EngiBracketFlagScoreCells verifies that for an engi
// elimination bracket match with FlagsA=3 and FlagsB=2, the Elimination Matches
// sheet renders the flag counts ("3"/"2") in the victory cells, NOT the ippon
// letters carried in IpponsA/IpponsB (which do not apply to engi). The assertion is
// column-precise (it reads the exact victory cell under the match header) so an
// incidental "3"/"2" elsewhere cannot mask a regression. FlagsA is Aka's
// (SideA), so it lands in the RIGHT victory column and FlagsB in the LEFT.
//
// Previously broken because overlayBracketScores used ScoreA/ScoreB directly,
// which for engi are the (inapplicable) ippon letters, and never consulted
// FlagsA/FlagsB.
func TestBuildResultsWorkbook_EngiBracketFlagScoreCells(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name              string
		flagsA            int
		flagsB            int
		ipponsA           []string
		ipponsB           []string
		wantLeft          string
		wantRight         string
		forbiddenIpponVal string
	}{
		// The left column (White/SideB) carries FlagsB=2, the right column
		// (Red/SideA) carries FlagsA=3.
		{name: "3-2", flagsA: 3, flagsB: 2, ipponsA: []string{"M", "K"}, ipponsB: []string{"M"}, wantLeft: "2", wantRight: "3", forbiddenIpponVal: "MK"},
		// 5-0 shutout: the loser's cell must be "0", not blank (pairwise write rule).
		{name: "5-0 shutout", flagsA: 5, flagsB: 0, ipponsA: []string{"M", "K"}, wantLeft: "0", wantRight: "5", forbiddenIpponVal: "MK"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			// Engi + Mixed format so an elimination bracket sheet is produced.
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Format = state.CompFormatMixed
			comp.Engi = true
			require.NoError(t, store.SaveCompetition(comp))

			pools := makeEngiPools()
			require.NoError(t, store.SavePools(compID, pools))
			require.NoError(t, store.SavePoolMatches(compID, nil))

			// Engi bracket match: pair1 beats pair2 on referee flags. IpponsA/IpponsB
			// carry ippon letters that MUST NOT render for engi; if the engi branch
			// is skipped, the ippon value would leak into the left victory cell.
			bracket := &state.Bracket{
				Rounds: [][]state.BracketMatch{
					{
						{
							ID:          "B1",
							SideA:       "Member One A",
							SideB:       "Member One B",
							Winner:      "Member One A",
							Status:      state.MatchStatusCompleted,
							FlagsA:      tc.flagsA,
							FlagsB:      tc.flagsB,
							IpponsA:     tc.ipponsA,
							IpponsB:     tc.ipponsB,
							Decision:    "fought",
							MatchNumber: 1,
						},
					},
				},
			}
			require.NoError(t, store.SaveBracket(compID, bracket))

			data, err := BuildResultsWorkbook(store, eng, compID)
			require.NoError(t, err)

			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer f.Close()

			rows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)

			left, right := bracketVictoryCells(t, rows, "Round 1 - Match 1")
			assert.Equal(t, tc.wantLeft, left,
				"left victory cell must render the engi flag count, not an ippon letter")
			assert.Equal(t, tc.wantRight, right,
				"right victory cell must render the engi flag count, not an ippon letter")

			// The ippon-letter score ("MK") must never leak into any cell:
			// engi is flag-scored, so the ippon path must be fully bypassed.
			assert.False(t, sheetContainsCell(rows, tc.forbiddenIpponVal),
				"elimination sheet must NOT contain ippon letters (%q) for an engi bracket", tc.forbiddenIpponVal)
		})
	}
}

// TestBuildResultsWorkbook_EngiStandingsHeadersRelabeled verifies that for an
// engi competition the Pool Matches standings section uses W/L/Flags/Rank headers
// (not W/L/T/PW/PL/Rank) and that the accumulated own-side flag count is overlaid
// as a literal value under the "Flags" header.
//
// Previously broken because PrintPoolMatches had no engi parameter and
// printIndividualResultsTableSection always wrote "T"/"PW"/"PL" headers, and
// overlayPoolStandings wrote to those non-existent-for-engi columns.
func TestBuildResultsWorkbook_EngiStandingsHeadersRelabeled(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Mark competition as engi (WithZekkenName remains false; EffectiveWithZekkenName() handles it).
	markCompAsEngi(t, store, compID)

	// Two engi pairs in one pool.
	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))

	// Engi match: pair1 wins 3-2 on referee flags.
	// SideAID/SideBID must match the player IDs in makeEngiPools so that
	// computeEngiStandings can resolve the standings row via engiPlayerKey.
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Member One A",
			SideAID:  "pair1",
			SideB:    "Member One B",
			SideBID:  "pair2",
			FlagsA:   3,
			FlagsB:   2,
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Member One A",
			WinnerID: "pair1",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// The standings table must have a "Flags" header.
	assert.True(t, sheetContainsCell(rows, "Flags"),
		"Pool Matches standings header must be 'Flags' for an engi competition")

	// The winner's accumulated flag count (3) must be overlaid as a literal
	// under the "Flags" header.
	assert.True(t, columnHasValueUnderHeader(rows, "Flags", "3"),
		"Pool Matches 'Flags' column must contain literal '3' for the engi winner")

	// "PW" and "PL" must NOT appear in the standings header for engi
	// (they are kendo-specific ippon-count columns).
	assert.False(t, sheetContainsCell(rows, "PW"),
		"Pool Matches must NOT contain 'PW' header for an engi competition")
	assert.False(t, sheetContainsCell(rows, "PL"),
		"Pool Matches must NOT contain 'PL' header for an engi competition")

	// "L" must NOT appear in the standings header for engi:
	// engi rankings do not record losses, only wins and flags.
	assert.False(t, sheetContainsCell(rows, "L"),
		"Pool Matches must NOT contain 'L' header for an engi competition")
}

// TestBuildResultsWorkbook_NonEngiStandingsHeadersUnchanged proves additivity:
// a non-engi competition still has the W/L/T/PW/PL/Rank standings header after
// the engi relabeling fix (engi=false must be a no-op).
func TestBuildResultsWorkbook_NonEngiStandingsHeadersUnchanged(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	// Standard non-engi competition with a single pool match.
	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))

	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alice",
			SideB:    "Bob",
			IpponsA:  []string{"M"},
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Alice",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Non-engi must still have the classic kendo standings headers.
	assert.True(t, sheetContainsCell(rows, "PW"),
		"Non-engi Pool Matches must still contain 'PW' header")
	assert.True(t, sheetContainsCell(rows, "PL"),
		"Non-engi Pool Matches must still contain 'PL' header")
	assert.True(t, sheetContainsCell(rows, "T"),
		"Non-engi Pool Matches must still contain 'T' header")
	// "Flags" must NOT appear in the non-engi standings.
	assert.False(t, sheetContainsCell(rows, "Flags"),
		"Non-engi Pool Matches must NOT contain 'Flags' header")
}

// TestBuildResultsWorkbook_EngiKikenMarkPlacement characterizes the result-mark
// placement for a kiken-voluntary engi match: "Kiken" rides in the WITHDRAWING
// pair's score cell (the loser, SideB, left of the vs column), the winner's
// score cell stays blank (FlagsScorePair returns ("", "") when neither side
// scored flags), and the middle cell keeps its template "vs" (kiken is a
// result, not a middle mark).
func TestBuildResultsWorkbook_EngiKikenMarkPlacement(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	pools := makeEngiPools()
	require.NoError(t, store.SavePools(compID, pools))

	// kiken-voluntary: pair1 wins; no flag score is recorded (FlagsScorePair -> "", "").
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Member One A",
			SideAID:  "pair1",
			SideB:    "Member One B",
			SideBID:  "pair2",
			FlagsA:   0,
			FlagsB:   0,
			Decision: "kiken-voluntary",
			Status:   state.MatchStatusCompleted,
			Winner:   "Member One A",
			WinnerID: "pair1",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// The withdrawing pair's score cell must carry "Kiken".
	assert.True(t, sheetContainsCell(rows, "Kiken"),
		"the withdrawing pair's score cell must render 'Kiken' for a kiken-voluntary engi match")

	// Geometry: the loser (SideB, Shiro) sits LEFT of the vs column, so from
	// the Kiken cell the middle is 2 columns right (template "vs") and the
	// winner's score cell 4 columns right (blank: no flags were scored).
	found := 0
	for _, row := range rows {
		for j, cell := range row {
			if cell != "Kiken" {
				continue
			}
			found++
			// GetRows trims trailing empty cells, so a blank cell to the
			// right may be past the row's end: read it as "".
			at := func(c int) string {
				if c < len(row) {
					return row[c]
				}
				return ""
			}
			assert.Equal(t, 1, j%helper.CourtsColumnsPerCourt,
				"Kiken must sit in the band's left score cell (start column + 1)")
			assert.Contains(t, []string{"", "vs"}, at(j+2),
				"the middle cell must stay untouched (template text): kiken is a result, not a middle mark")
			assert.Equal(t, "", at(j+4),
				"the winner's score cell must be blank for kiken with FlagsA=0")
		}
	}
	assert.Equal(t, 1, found, "exactly one Kiken mark")
}

// TestBuildResultsWorkbook_EngiPartialPoolScoring characterizes partial scoring:
// two engi pools, only pool A is scored (FlagsA=3, FlagsB=2). Pool B is left
// untouched. The export must succeed and the scored flags must appear in the Pool
// Matches sheet under the "Flags" standings column.
func TestBuildResultsWorkbook_EngiPartialPoolScoring(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	// Two engi pools of two pairs each; pool B is unscored. Each pair carries
	// both member names combined in Name (the canonical engi model).
	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	partialPoolA := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "ep1", Name: "Spark A - Spark B", Dojo: "DojoA"},
			{ID: "ep2", Name: "Flame A - Flame B", Dojo: "DojoB"},
		},
	}
	partialPoolA.Matches = []helper.Match{{SideA: &partialPoolA.Players[0], SideB: &partialPoolA.Players[1]}}

	partialPoolB := helper.Pool{
		PoolName: "Pool B",
		Players: []helper.Player{
			{ID: "ep3", Name: "Wave A - Wave B", Dojo: "DojoC"},
			{ID: "ep4", Name: "Stone A - Stone B", Dojo: "DojoD"},
		},
	}
	partialPoolB.Matches = []helper.Match{{SideA: &partialPoolB.Players[0], SideB: &partialPoolB.Players[1]}}

	pools := []helper.Pool{partialPoolA, partialPoolB}
	require.NoError(t, store.SavePools(compID, pools))

	// Score pool A only; pool B has no result entry (partial scoring).
	results := []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Spark A - Spark B",
			SideAID:  "ep1",
			SideB:    "Flame A - Flame B",
			SideBID:  "ep2",
			FlagsA:   3,
			FlagsB:   2,
			Decision: "fought",
			Status:   state.MatchStatusCompleted,
			Winner:   "Spark A - Spark B",
			WinnerID: "ep1",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, results))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	// Assert the pool A MATCH ROW score cells specifically: left score (FlagsB,
	// Shiro) at bandStart+1 and right score (FlagsA, Aka) at bandStart+5 (vs is
	// at bandStart+3). containsCell alone is vacuous: it would be satisfied by
	// the standings overlay.
	matchRow := firstPoolMatchScoreRow(rows, 0)
	require.NotNil(t, matchRow, "pool A match score row must exist (White/Red header must be present)")
	require.Greater(t, len(matchRow), 5, "match score row must have at least 6 columns")
	assert.Equal(t, "2", matchRow[1],
		"left score cell (2 before vs) must be '2' (FlagsB=2 for pool A)")
	assert.Equal(t, "3", matchRow[5],
		"right score cell (2 after vs) must be '3' (FlagsA=3 for pool A)")

	// The winner's accumulated flag total must appear under the "Flags" standings header.
	assert.True(t, columnHasValueUnderHeader(rows, helper.ColHeaderFlags, "3"),
		"Pool Matches 'Flags' standings column must carry '3' for the pool A winner")
}

// TestBuildResultsWorkbook_EngiMultiCourtStandingsColumns is the engi analog of
// TestBuildResultsWorkbook_MultiCourtStandingsColumns: four engi pools across two
// courts; only the court-B pool is scored. The "Flags" header appears once per
// court band; the court-B Flags column must hold the scored value while the
// court-A Flags column must remain clean (no cross-band bleed).
func TestBuildResultsWorkbook_EngiMultiCourtStandingsColumns(t *testing.T) {
	t.Parallel()
	dir, err := os.MkdirTemp("", "export-test-mc-engi-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "mc-engi"
	comp := &state.Competition{ID: compID, Name: "MC Engi", Courts: []string{"A", "B"}, Engi: true}
	require.NoError(t, store.SaveCompetition(comp))

	// makePair builds an engi pair: both member names combined in Name (the
	// canonical model); DisplayName is not used for engi.
	makePair := func(id, n1, n2, dojo string) helper.Player {
		return helper.Player{ID: id, Name: n1 + " - " + n2, Dojo: dojo}
	}

	// makeEngiPool creates a pool with SideA/SideB pointing into pool.Players.
	makeEngiPool := func(name string, p1, p2 helper.Player) helper.Pool {
		pool := helper.Pool{PoolName: name, Players: []helper.Player{p1, p2}}
		pool.Matches = []helper.Match{{SideA: &pool.Players[0], SideB: &pool.Players[1]}}
		return pool
	}

	// Four pools: [A,B] -> court A; [C,D] -> court B (contiguous assignment).
	pools := []helper.Pool{
		makeEngiPool("Pool A", makePair("pa1", "AOne-A", "AOne-B", "DojoPA"), makePair("pa2", "ATwo-A", "ATwo-B", "DojoPA")),
		makeEngiPool("Pool B", makePair("pb1", "BOne-A", "BOne-B", "DojoPB"), makePair("pb2", "BTwo-A", "BTwo-B", "DojoPB")),
		makeEngiPool("Pool C", makePair("pc1", "COne-A", "COne-B", "DojoPC"), makePair("pc2", "CTwo-A", "CTwo-B", "DojoPC")),
		makeEngiPool("Pool D", makePair("pd1", "DOne-A", "DOne-B", "DojoPD"), makePair("pd2", "DTwo-A", "DTwo-B", "DojoPD")),
	}
	require.NoError(t, store.SavePools(compID, pools))

	// Score only Pool C (court B): pc1 wins 3-2 on flags.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool C-0", SideA: "COne-A - COne-B", SideAID: "pc1",
			SideB: "CTwo-A - CTwo-B", SideBID: "pc2",
			FlagsA: 3, FlagsB: 2,
			Decision: "fought", Status: state.MatchStatusCompleted,
			Winner: "COne-A - COne-B", WinnerID: "pc1",
		},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetPoolMatches)
	require.NoError(t, err)

	courtAFlags := headerColInBand(rows, helper.ColHeaderFlags, 0, helper.CourtsColumnsPerCourt)
	courtBFlags := headerColInBand(rows, helper.ColHeaderFlags, helper.CourtsColumnsPerCourt, 2*helper.CourtsColumnsPerCourt)
	require.GreaterOrEqual(t, courtAFlags, 0, "court A 'Flags' header must exist in pool matches for an engi competition")
	require.GreaterOrEqual(t, courtBFlags, 0, "court B 'Flags' header must exist in pool matches for an engi competition")

	assert.True(t, columnContains(rows, courtBFlags, "3"),
		"court B's Flags column must carry the winner's accumulated flag count (=3)")
	assert.False(t, columnContains(rows, courtAFlags, "3"),
		"court A pools are unscored: '3' in court A's Flags column means court B's score leaked (multi-court colMap bug)")
}

// TestBuildResultsWorkbook_EngiUnicodeAndCommaNames characterizes that the export
// correctly handles engi pair names containing unicode characters and commas.
// The combined pair name (both members joined in the Name field) must appear in
// the Data sheet exactly, without corruption or splitting on comma or multibyte
// boundaries.
func TestBuildResultsWorkbook_EngiUnicodeAndCommaNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	markCompAsEngi(t, store, compID)

	// Unicode names (Japanese) and names containing commas.
	// SideA/SideB point into pool.Players so the playerMatchRows map resolves correctly.
	unicodePool := helper.Pool{
		PoolName: "Pool A",
		Players: []helper.Player{
			{ID: "uni1", Name: "結城 由紀 - 田中 花子", Dojo: "東京道場"},
			{ID: "com1", Name: "O'Brien, Sean - Smith, Jane", Dojo: "New York, NY"},
		},
	}
	unicodePool.Matches = []helper.Match{{SideA: &unicodePool.Players[0], SideB: &unicodePool.Players[1]}}
	// Keep local variable aliases for name assertions below.
	uniPair := unicodePool.Players[0]
	comPair := unicodePool.Players[1]
	pools := []helper.Pool{unicodePool}
	require.NoError(t, store.SavePools(compID, pools))
	// No match results; we are only verifying Data-sheet name rendering.
	require.NoError(t, store.SavePoolMatches(compID, nil))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	dataRows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)

	// Both combined pair names must appear as exact cell values in the Data sheet.
	names := []string{uniPair.Name, comPair.Name}
	for _, name := range names {
		assert.True(t, sheetContainsCell(dataRows, name),
			"Data sheet must contain member name %q exactly (unicode/comma safe)", name)
	}
}

// TestBuildResultsWorkbook_EngiEliminationHeaderFlags verifies that when a
// competition has Engi=true, BuildResultsWorkbook writes "Fl" in the lV/rV
// columns of the Elimination Matches header row. The lV column for court 1 is
// B (startCol+1); rV is F (startCol+5). The first match title is at row 2, so
// the header row is at row 3.
func TestBuildResultsWorkbook_EngiEliminationHeaderFlags(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatMixed
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.Engi = true
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "M1", Dojo: "D"}, {Name: "M2", Dojo: "D"}, {Name: "M3", Dojo: "D"},
		{Name: "M4", Dojo: "D"}, {Name: "M5", Dojo: "D"}, {Name: "M6", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// Match title at row 2, header row at row 3. lV=B, rV=F for court 1.
	lV, err := f.GetCellValue(helper.SheetEliminationMatches, "B3")
	require.NoError(t, err)
	rV, err := f.GetCellValue(helper.SheetEliminationMatches, "F3")
	require.NoError(t, err)
	assert.Equal(t, "Fl", lV, "engi elimination match header lV cell (B3) must be 'Fl'")
	assert.Equal(t, "Fl", rV, "engi elimination match header rV cell (F3) must be 'Fl'")
}

// TestBuildResultsWorkbook_EngiPairLabelsInBracketEntrants verifies that engi
// bracket entrant name cells render the full pair as "Member1 - Member2".
// Under the combined-name model the bracket match SideA/SideB fields hold the
// full "Pair1A - Pair1B" string (both members are joined in Player.Name at
// registration). overlayKnockoutBracketNames writes these directly into the
// entrant cells; no DisplayName or roster lookup occurs. Covers both the
// knockout entrant overwrite (overlayKnockoutBracketNames) and the 3rd Place
// block entrant writes (overlayBracketScores).
func TestBuildResultsWorkbook_EngiPairLabelsInBracketEntrants(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, true)
	bracket := startNaginataWith4Players(t, store, eng, compID, true)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2)
	for _, m := range sf {
		_, err := eng.RecordMatchResultWithIneligibility(compID, m.ID, &state.MatchResult{
			FlagsA: 3, FlagsB: 2, Status: state.MatchStatusCompleted,
		})
		require.NoError(t, err)
	}

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Collect every non-empty name cell (court cols A and G) below a match or
	// 3rd Place header. Each populated entrant must carry the dash pair label.
	var entrants []string
	for i, row := range rows {
		for _, cell := range row {
			if cell != helper.ThirdPlaceLabel && parseRoundMatchLabel(cell) <= 0 {
				continue
			}
			if i+2 >= len(rows) {
				continue
			}
			nameRow := rows[i+2]
			for _, col := range []int{0, 6} {
				if col < len(nameRow) && strings.HasPrefix(nameRow[col], "Pair") {
					entrants = append(entrants, nameRow[col])
				}
			}
		}
	}
	require.NotEmpty(t, entrants, "expected populated bracket entrant name cells")
	for _, name := range entrants {
		assert.Regexp(t, `^Pair\dA - Pair\dB$`, name,
			"engi entrant must render as 'Member1 - Member2', got %q", name)
	}
}

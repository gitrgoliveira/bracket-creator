package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The four SENIOR events of the 34th EKC 2026 (Podgorica) drawings, decoded
// 2026-08-18, plus the 33rd EKC 2025 (Leiden) Men Team draw decoded the same
// day. draw_ekc_test.go covers the three Junior events; together the two files
// are all seven 2026 events plus the one 2025 sheet that discriminates the
// R6(c) block-layout rule.
//
// Seeding across all seven is exactly the previous year's medallists (33rd EKC
// 2025), and every seeded pool is its shiaijo's FIRST pool -- which is the
// structural reason criteria 1 and 3 of R6 agree on real data (spec R6(b)).
// Source links are in the spec's Sources section.
//
// Two of these four are NOT reproducible today, and each is pinned below by the
// narrowest test that states the gap:
//
//   - Men Team: blocks of 6 hit the R6(c) layout defect (the sheet's
//     sub-block template vs our single 4+2 block). Its shiaijo assignment IS
//     correct and is asserted; its bye allocation is asserted in tests that
//     skip until R6(c) is fixed (2026 and 2025 sheets).
//   - Men Individual and Ladies Individual: both use PER-POOL qualifier counts
//     (a 4-person pool sends 2, a 3-person pool sends 1), which the uniform
//     poolWinners parameter cannot express at all.

// TestEKCLadiesTeam is reference draw D: 8 pools, 2 qualifiers, courts A(1,2)
// B(3,4) C(5,6) D(7,8), seeds on pools 1, 3, 5 and 7 (Poland, France, Italy,
// Netherlands -- the 2025 placings 3rd, 1st, 2nd, 3rd).
//
// It is the cleanest reference case in the suite: 16 occupants over four blocks
// of four, so layer 1 owes NO byes anywhere and the whole sheet is R3 + R4
// crossing. That makes it the control for R6(c) -- same event family and same
// qualifier count as Men Team, differing only in block size.
func TestEKCLadiesTeam(t *testing.T) {
	pools := ekcPools(8, 1, 3, 5, 7)

	assignment, err := AssignPoolsToCourts(8, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 1, 1, 2, 2, 3, 3}, assignment,
		"the sheet's court blocks A(1,2) B(3,4) C(5,6) D(7,8)")

	draw := BuildKnockoutDraw(pools, 2, 4)

	assertEKCRegions(t, draw, []ekcRegion{
		{court: "A", matches: []string{"Pool 1-1st v Pool 5-2nd", "Pool 2-1st v Pool 6-2nd"}, byes: []string{}},
		{court: "B", matches: []string{"Pool 3-1st v Pool 7-2nd", "Pool 4-1st v Pool 8-2nd"}, byes: []string{}},
		{court: "C", matches: []string{"Pool 5-1st v Pool 1-2nd", "Pool 6-1st v Pool 2-2nd"}, byes: []string{}},
		{court: "D", matches: []string{"Pool 7-1st v Pool 3-2nd", "Pool 8-1st v Pool 4-2nd"}, byes: []string{}},
	})

	// F1-F2 on A, F4-F5 on B, F7-F8 on C, F10-F11 on D; then F3/F6/F9/F12 one
	// per shiaijo, F13 on B and F14 on C, and the final F15 on B.
	assert.Equal(t, [][]string{
		{"A", "A", "B", "B", "C", "C", "D", "D"},
		{"A", "B", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Ladies Team sheet")

	// Every pool's two qualifiers in opposite halves (R5), on a draw where no
	// bye can be masking a misplacement.
	first, second := halfLeaves(draw.Root)
	for p := 1; p <= 8; p++ {
		a, b := poolLabel(p, "1st"), poolLabel(p, "2nd")
		assert.NotEqual(t, holdsLabel(first, a), holdsLabel(first, b),
			"pool %d's two qualifiers must sit in opposite halves (R5)", p)
	}
	assert.Len(t, first, 8)
	assert.Len(t, second, 8)
}

// TestEKCMenTeamShiaijoAssignment is the half of the Men Team sheet we DO
// reproduce: 12 pools, 2 qualifiers, courts A(1,2,3) B(4,5,6) C(7,8,9)
// D(10,11,12), seeds on pools 1, 4, 7 and 10 (Italy, France, Switzerland,
// Spain -- the 2025 placings 3rd, 1st, 2nd, 3rd).
//
// Every bout runs on the shiaijo the sheet prints, including the closing bouts
// on the middle courts. Asserting this separately from the regions matters:
// R6(c) is a defect in WHICH occupant sits where inside a block, not in the
// block structure or the court derivation, and keeping the correct half pinned
// stops a fix for R6(c) from regressing it.
func TestEKCMenTeamShiaijoAssignment(t *testing.T) {
	assignment, err := AssignPoolsToCourts(12, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3, 3}, assignment,
		"the sheet's court blocks A(1-3) B(4-6) C(7-9) D(10-12)")

	draw := BuildKnockoutDraw(ekcPools(12, 1, 4, 7, 10), 2, 4)

	// F1-F2 A, F6-F7 B, F11-F12 C, F16-F17 D; F3-F4 A, F8-F9 B, F13-F14 C,
	// F18-F19 D; F5 A, F10 B, F15 C, F20 D; F21 B, F22 C; final F23 B.
	assert.Equal(t, [][]string{
		{"A", "A", "B", "B", "C", "C", "D", "D"},
		{"A", "A", "B", "B", "C", "C", "D", "D"},
		{"A", "B", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Men Team sheet")

	// R4 crossing at 2 qualifiers, partners A<->C and B<->D, which the sheet
	// prints and which is independent of the R6(c) split.
	first, _ := halfLeaves(draw.Root)
	for p := 1; p <= 12; p++ {
		a, b := poolLabel(p, "1st"), poolLabel(p, "2nd")
		assert.NotEqual(t, holdsLabel(first, a), holdsLabel(first, b),
			"pool %d's two qualifiers must sit in opposite halves (R5)", p)
	}
}

// TestEKCMenTeamByes is the OTHER half, and it currently fails: see spec R6(c).
//
// Each shiaijo holds 6 occupants (3 home 1sts + 3 crossed-in 2nds). The sheet
// lays a 6-occupant block out as TWO SUB-BLOCKS, each headed by a home 1st who
// byes into the sub-block final (R6(c)'s template, mirrored on the second court
// of each half). We build one 4+2 block instead, so the byes land on P3#1 and
// P9#2 while the seeded pool plays.
//
// The expectations below are the SHEET, which is the definition of record. When
// R6(c) is fixed, delete the t.Skip and this test verifies the fix. Do NOT
// "fix" the test by writing our current output into it.
func TestEKCMenTeamByes(t *testing.T) {
	t.Skip("known defect R6(c): even-sized blocks (here 6) misallocate byes; see specs/007-ekc-draw/spec.md")

	draw := BuildKnockoutDraw(ekcPools(12, 1, 4, 7, 10), 2, 4)

	assertEKCRegions(t, draw, []ekcRegion{
		{
			court:   "A",
			matches: []string{"Pool 7-2nd v Pool 8-2nd", "Pool 3-1st v Pool 9-2nd"},
			byes:    []string{"Pool 1-1st", "Pool 2-1st"},
		},
		{
			court:   "B",
			matches: []string{"Pool 10-2nd v Pool 5-1st", "Pool 11-2nd v Pool 12-2nd"},
			byes:    []string{"Pool 4-1st", "Pool 6-1st"},
		},
		{
			court:   "C",
			matches: []string{"Pool 1-2nd v Pool 2-2nd", "Pool 9-1st v Pool 3-2nd"},
			byes:    []string{"Pool 7-1st", "Pool 8-1st"},
		},
		{
			court:   "D",
			matches: []string{"Pool 4-2nd v Pool 11-1st", "Pool 5-2nd v Pool 6-2nd"},
			byes:    []string{"Pool 10-1st", "Pool 12-1st"},
		},
	})
}

// TestEKCIndividualEventsUsePerPoolQualifierCounts pins the second gap the full
// sheet exposed, and it is a MODELLING gap rather than a layout one.
//
// Men Individual and Ladies Individual do not send a fixed number of qualifiers
// per pool. A 4-person pool sends 2 and a 3-person pool sends 1, so the
// occupant count is neither the pool count nor twice it:
//
//	Ladies Individual  34 pools, 2 of them 4-person -> 36 occupants (final F35)
//	Men Individual     45 pools, 2 of them 4-person -> 47 occupants (final F46)
//
// The sheet corroborates the rule directly: Ladies pool 16 (CZE-4, IRL-3,
// ISR-2, PRT-3) is one of the two 4-person pools, and P16 #2 appears in shiaijo
// A's block having crossed over from B. Every 3-person pool sends only its 1st.
//
// BuildKnockoutDraw takes ONE poolWinners for the whole competition, so neither
// value can produce the sheet. This test states that as arithmetic rather than
// as a comment, so it starts failing the moment per-pool qualifiers are
// supported and the event becomes expressible.
func TestEKCIndividualEventsUsePerPoolQualifierCounts(t *testing.T) {
	cases := []struct {
		name            string
		numPools        int
		sheetOccupants  int
		fourPersonPools int
	}{
		{"Ladies Individual", 34, 36, 2},
		{"Men Individual", 45, 47, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.numPools+tc.fourPersonPools, tc.sheetOccupants,
				"the sheet's occupant count is one per pool plus one per 4-person pool")

			atOne := len(TreeLeafLabels(BuildKnockoutDraw(ekcPools(tc.numPools), 1, 4).Root))
			atTwo := len(TreeLeafLabels(BuildKnockoutDraw(ekcPools(tc.numPools), 2, 4).Root))

			assert.Equal(t, tc.numPools, atOne, "one qualifier per pool")
			assert.Equal(t, 2*tc.numPools, atTwo, "two qualifiers per pool")
			assert.NotEqual(t, tc.sheetOccupants, atOne,
				"a uniform 1 qualifier cannot reach the sheet's %d occupants", tc.sheetOccupants)
			assert.NotEqual(t, tc.sheetOccupants, atTwo,
				"a uniform 2 qualifiers cannot reach the sheet's %d occupants", tc.sheetOccupants)
		})
	}
}

// TestEKC2025MenTeamByes replays the 33rd EKC 2025 (Leiden) Men Team draw: 11
// pools, 2 qualifiers, courts A(1,2,3) B(4,5,6) C(7,8,9) D(10,11), seeds on
// pools 1, 4, 7 and 10 (POL, FRA, BEL, ESP -- the blue rows on the pools page).
// Skipped for the same defect as TestEKCMenTeamByes, and worth having as well
// as it because its five-occupant courts pin what a plain 3+3 reading cannot:
//
//   - Courts A and C (6 occupants) print bout-for-bout IDENTICALLY to the 34th
//     EKC 2026 draw, which is what makes the template a rule rather than one
//     sheet's arrangement.
//   - Courts B and D (5 occupants) are the template with a VACANCY: the missing
//     occupant's playing slot stays empty and its would-be opponent byes, so
//     each prints THREE named byes and ONE round-1 match. F7 and F16 sit in the
//     sheet's round-2 column -- column position is round depth.
//   - Both vacancy byes land on the WEAKEST crossed 2nd, each passing over a
//     SEEDED pool's 2nd (P11#2 over Spain's P10#2; P6#2 over France's P4#2).
//
// The D expectations pin one further sheet detail on a single cell: after the
// crossed bye is granted, the remaining playing pair fills weakest-first
// (P5#2 above P4#2). That side order is aka/shiro-cosmetic and low-confidence;
// if a future sheet contradicts it, re-read spec R6(c) before touching it.
//
// The knockout rounds beyond the court finals (F19-F21) were NOT decoded from
// the sheet, so this test deliberately asserts regions only, not courtsByRound.
func TestEKC2025MenTeamByes(t *testing.T) {
	t.Skip("known defect R6(c): >4-occupant blocks at 2+ qualifiers use the wrong layout; see specs/007-ekc-draw/spec.md")

	assignment, err := AssignPoolsToCourts(11, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{0, 0, 0, 1, 1, 1, 2, 2, 2, 3, 3}, assignment,
		"the sheet's court blocks A(1-3) B(4-6) C(7-9) D(10,11)")

	draw := BuildKnockoutDraw(ekcPools(11, 1, 4, 7, 10), 2, 4)

	assertEKCRegions(t, draw, []ekcRegion{
		{
			court:   "A",
			matches: []string{"Pool 7-2nd v Pool 8-2nd", "Pool 3-1st v Pool 9-2nd"},
			byes:    []string{"Pool 1-1st", "Pool 2-1st"},
		},
		{
			court:   "B",
			matches: []string{"Pool 10-2nd v Pool 5-1st"},
			byes:    []string{"Pool 4-1st", "Pool 6-1st", "Pool 11-2nd"},
		},
		{
			court:   "C",
			matches: []string{"Pool 1-2nd v Pool 2-2nd", "Pool 9-1st v Pool 3-2nd"},
			byes:    []string{"Pool 7-1st", "Pool 8-1st"},
		},
		{
			court:   "D",
			matches: []string{"Pool 5-2nd v Pool 4-2nd"},
			byes:    []string{"Pool 10-1st", "Pool 6-2nd", "Pool 11-1st"},
		},
	})
}

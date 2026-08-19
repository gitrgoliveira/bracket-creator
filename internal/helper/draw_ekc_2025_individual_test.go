package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The 33rd EKC 2025 (Leiden) Ladies Individual and Men Individual knockout
// draws, decoded 2026-08-19 from the official PDF pages (li2025-08.png,
// mi2025-12.png). Both are single-qualifier (poolWinners=1), 4-court events
// whose per-court block sizes are far larger than anything draw_ekc_test.go
// or draw_ekc_senior_test.go replay: Ladies runs 10/10/9/9 occupants per
// court and Men runs 12/12/12/11. TestEKC2025MenTeamByes already showed the
// production layout tops out at a 6-occupant block (two 3-occupant
// sub-blocks); these two sheets are the next size up and are expected to fail
// for the same reason, so both tests are committed red and skipped, exactly
// as TestEKCMenTeamByes was before its fix landed (bc-qual phase LP-1a; the
// big-block templates are LP-2).
//
// Every court in both sheets shares one recurring shape: SOME pools sit in a
// genuine round-1 match (two pool winners meeting head-on), while the
// remaining pools -- and any round-1 winners still without a round-2
// opponent -- are drawn as "round-2 column" entries: either a leaf-leaf pair
// that skips round 1 entirely (both pools wait and meet each other one round
// late) or a single pool paired against a round-1 winner. This is the same
// per-round grouping TestEKC2025MenTeamByes's 5-occupant blocks already pin
// (one round-1 match, then a leaf-leaf pair plus a mixed pair in round 2);
// these two sheets just apply it inside bigger blocks with two or three
// round-1 matches instead of one.
//
// Verified against the images down to aka/shiro slot order (regionRounds
// prints Left before Right, so a side flip fails the same way
// TestEKC2025MenTeamByes's comment warns about): every match transcribed here
// was cross-checked pixel-by-pixel, including a 3x zoom crop of the Men
// Individual court D block and the semifinal/final wiring on both sheets, and
// no discrepancy was found against the task's source tables.

// TestEKC2025LadiesIndividual replays the 33rd EKC 2025 Ladies Individual
// draw: 38 pools, 1 qualifier each, 4 courts, blocks A(1-10) B(11-20)
// C(21-29) D(30-38) -- AssignPoolsToCourts' front-loaded-remainder split.
//
// Courts A and B (10 occupants) are built from two 5-occupant sub-blocks
// apiece, each shaped like TestEKC2025MenTeamByes's 5-occupant regions (one
// round-1 match, then a leaf-leaf pair and a mixed pair in round 2). Courts C
// and D (9 occupants) are one 4-occupant sub-block (two leaf-leaf round-2
// pairs) plus one 5-occupant sub-block, again matching the same one-round-1
// -match template.
func TestEKC2025LadiesIndividual(t *testing.T) {
	t.Skip("pending big-block templates, bc-qual LP-2")

	assignment, err := AssignPoolsToCourts(38, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		2, 2, 2, 2, 2, 2, 2, 2, 2,
		3, 3, 3, 3, 3, 3, 3, 3, 3,
	}, assignment, "the sheet's court blocks A(1-10) B(11-20) C(21-29) D(30-38)")

	draw := BuildKnockoutDraw(ekcPools(38), 1, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)

	// The shiaijo printed on every bout of the sheet, round by round: two
	// round-1 blocks (A, B) of 2 matches and two (C, D) of 1; a round-2 layer
	// of 4 matches per court; a round-3 layer of 2 matches per court; the four
	// block finals A/B/C/D; the semifinals A+B on B and C+D on C; the final
	// on B.
	assert.Equal(t, [][]string{
		{"A", "A", "B", "B", "C", "D"},
		{"A", "A", "A", "A", "B", "B", "B", "B", "C", "C", "C", "C", "D", "D", "D", "D"},
		{"A", "A", "B", "B", "C", "C", "D", "D"},
		{"A", "B", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Ladies Individual sheet")

	assert.Equal(t, [][]string{
		{"Pool 3-1st v Pool 4-1st", "Pool 7-1st v Pool 8-1st"},                                      // F1, F2
		{"Pool 1-1st v Pool 2-1st", "W v Pool 5-1st", "Pool 6-1st v W", "Pool 9-1st v Pool 10-1st"}, // F3-F6
		{"W v W", "W v W"}, // F7, F8
		{"W v W"},          // F9
	}, regionRounds(draw.Regions[0]), "shiaijo A")

	assert.Equal(t, [][]string{
		{"Pool 13-1st v Pool 14-1st", "Pool 17-1st v Pool 18-1st"},                                       // F10, F11
		{"Pool 11-1st v Pool 12-1st", "W v Pool 15-1st", "Pool 16-1st v W", "Pool 19-1st v Pool 20-1st"}, // F12-F15
		{"W v W", "W v W"}, // F16, F17
		{"W v W"},          // F18
	}, regionRounds(draw.Regions[1]), "shiaijo B")

	assert.Equal(t, [][]string{
		{"Pool 26-1st v Pool 27-1st"}, // F19
		{"Pool 21-1st v Pool 22-1st", "Pool 23-1st v Pool 24-1st", "Pool 25-1st v W", "Pool 28-1st v Pool 29-1st"}, // F20-F23
		{"W v W", "W v W"}, // F24, F25
		{"W v W"},          // F26
	}, regionRounds(draw.Regions[2]), "shiaijo C")

	assert.Equal(t, [][]string{
		{"Pool 35-1st v Pool 36-1st"}, // F27
		{"Pool 30-1st v Pool 31-1st", "Pool 32-1st v Pool 33-1st", "Pool 34-1st v W", "Pool 37-1st v Pool 38-1st"}, // F28-F31
		{"W v W", "W v W"}, // F32, F33
		{"W v W"},          // F34
	}, regionRounds(draw.Regions[3]), "shiaijo D")
}

// TestEKC2025MenIndividual replays the 33rd EKC 2025 Men Individual draw: 47
// pools, 1 qualifier each, 4 courts, blocks A(1-12) B(13-24) C(25-36)
// D(37-47).
//
// Courts A, B and C are the same 12-occupant shape three times over (shifted
// by 12 pools each): four round-1 matches, four round-2 matches each pairing
// a round-1 winner against a lone pool, two round-3 matches, one block final.
// Court D (11 occupants) is the odd one out: three round-1 matches instead of
// four, so its round-2 layer picks up one leaf-leaf pair (the two pools that
// never got a round-1 opponent) alongside three mixed pairs.
func TestEKC2025MenIndividual(t *testing.T) {
	t.Skip("pending big-block templates, bc-qual LP-2")

	assignment, err := AssignPoolsToCourts(47, 4)
	require.NoError(t, err)
	assert.Equal(t, []int{
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
		3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
	}, assignment, "the sheet's court blocks A(1-12) B(13-24) C(25-36) D(37-47)")

	draw := BuildKnockoutDraw(ekcPools(47), 1, 4)
	require.NotNil(t, draw)
	require.Len(t, draw.Regions, 4)

	// Round 1 is 4+4+4+3 (D is short one pair), every other round and the
	// semis/final match TestEKC2025LadiesIndividual's shape one-for-one:
	// round 2 is 4 per court, round 3 is 2 per court, the four block finals,
	// the two semifinals (A+B on B, C+D on C) and the final on B.
	assert.Equal(t, [][]string{
		{"A", "A", "A", "A", "B", "B", "B", "B", "C", "C", "C", "C", "D", "D", "D"},
		{"A", "A", "A", "A", "B", "B", "B", "B", "C", "C", "C", "C", "D", "D", "D", "D"},
		{"A", "A", "B", "B", "C", "C", "D", "D"},
		{"A", "B", "C", "D"},
		{"B", "C"},
		{"B"},
	}, courtsByRound(draw), "the shiaijo printed on every bout of the Men Individual sheet")

	assert.Equal(t, [][]string{
		{"Pool 2-1st v Pool 3-1st", "Pool 4-1st v Pool 5-1st", "Pool 8-1st v Pool 9-1st", "Pool 10-1st v Pool 11-1st"}, // F1-F4
		{"Pool 1-1st v W", "W v Pool 6-1st", "Pool 7-1st v W", "W v Pool 12-1st"},                                      // F5-F8
		{"W v W", "W v W"}, // F9, F10
		{"W v W"},          // F11
	}, regionRounds(draw.Regions[0]), "shiaijo A")

	assert.Equal(t, [][]string{
		{"Pool 14-1st v Pool 15-1st", "Pool 16-1st v Pool 17-1st", "Pool 20-1st v Pool 21-1st", "Pool 22-1st v Pool 23-1st"}, // F12-F15
		{"Pool 13-1st v W", "W v Pool 18-1st", "Pool 19-1st v W", "W v Pool 24-1st"},                                         // F16-F19
		{"W v W", "W v W"}, // F20, F21
		{"W v W"},          // F22
	}, regionRounds(draw.Regions[1]), "shiaijo B")

	assert.Equal(t, [][]string{
		{"Pool 26-1st v Pool 27-1st", "Pool 28-1st v Pool 29-1st", "Pool 32-1st v Pool 33-1st", "Pool 34-1st v Pool 35-1st"}, // F23-F26
		{"Pool 25-1st v W", "W v Pool 30-1st", "Pool 31-1st v W", "W v Pool 36-1st"},                                         // F27-F30
		{"W v W", "W v W"}, // F31, F32
		{"W v W"},          // F33
	}, regionRounds(draw.Regions[2]), "shiaijo C")

	assert.Equal(t, [][]string{
		{"Pool 39-1st v Pool 40-1st", "Pool 43-1st v Pool 44-1st", "Pool 45-1st v Pool 46-1st"}, // F34-F36
		{"Pool 37-1st v Pool 38-1st", "W v Pool 41-1st", "Pool 42-1st v W", "W v Pool 47-1st"},  // F37-F40
		{"W v W", "W v W"}, // F41, F42
		{"W v W"},          // F43
	}, regionRounds(draw.Regions[3]), "shiaijo D")
}

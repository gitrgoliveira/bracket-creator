package state_test

// bracket_rounds_test.go pins Bracket.RestampRoundsFromFeeders, the load-time
// correction for stored brackets whose rounds or match numbers differ from the
// rule generation applies (bc-tmfn): v2.0.0 and v2.1.0 put a pair drawn beside
// an empty pair one round early, and older releases broke a tie inside a round
// by a different order. The restamp recomputes each real match's round as its
// distance from the final along the stored Feeders and renumbers; on a bracket
// nobody has started it also puts each court's times in match-number order
// (bracket_rounds_schedule_test.go), and it changes nothing else.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// v21FiveEntrantFixture is a five-entrant knockout on one shiaijo exactly as a
// v2.1.0 binary wrote it through its API (generate-draw, start, then P4 beat P5
// M-K), bytes unchanged. P1 v P2 (m-r1-0) sits beside an empty pair: v2.1.0
// stored it as round 3, Match 1; its distance from the final makes it a
// semifinal, round 2, and so Match 2 after P4 v P5.
const v21FiveEntrantFixture = "testdata/bracket_v2.1.0_knockout5.json"

func loadV21FiveEntrant(t *testing.T) *state.Bracket {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(v21FiveEntrantFixture))
	require.NoError(t, err)
	var b state.Bracket
	require.NoError(t, json.Unmarshal(raw, &b))
	// A bronze the restamp must neither read nor write. v2.1.0 draws one only
	// for a format that needs a single 3rd place; adding it here keeps the
	// "bronze untouched" rule under test without a second fixture.
	b.ThirdPlaceMatch = &state.BracketMatch{
		ID:           state.BronzeMatchID,
		Status:       state.MatchStatusScheduled,
		Court:        "A",
		ScheduledAt:  "09:15",
		DisplayRound: -1,
	}
	return &b
}

func matchByID(t *testing.T, b *state.Bracket, id string) *state.BracketMatch {
	t.Helper()
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			if b.Rounds[ri][mi].ID == id {
				return &b.Rounds[ri][mi]
			}
		}
	}
	require.Failf(t, "no such match", "%s", id)
	return nil
}

func TestRestampRoundsFromFeeders_CorrectsTheV21FiveEntrantBracket(t *testing.T) {
	got := loadV21FiveEntrant(t)
	require.Equal(t, 3, matchByID(t, got, "m-r1-0").DisplayRound, "fixture must carry the v2.1 classification")

	changes, err := got.RestampRoundsFromFeeders()
	require.NoError(t, err)
	// P4 v P5 carries a recorded result, so the times stay where v2.1.0 put
	// them even though the two bouts swap numbers.
	assert.Equal(t, []state.BracketRoundChange{
		{ID: "m-r1-0", OldRound: 3, NewRound: 2, OldNumber: 1, NewNumber: 2, OldScheduledAt: "09:00", NewScheduledAt: "09:00"},
		{ID: "m-r1-3", OldRound: 3, NewRound: 3, OldNumber: 2, NewNumber: 1, OldScheduledAt: "09:05", NewScheduledAt: "09:05"},
	}, changes)

	// The whole bracket equals the stored one with exactly these four fields
	// set, so sides, ids, winners, results, courts, scheduled times, Hidden
	// rows, feeders, the draw order and the bronze are all proved untouched.
	want := loadV21FiveEntrant(t)
	for id, rn := range map[string][2]int{
		"m-r1-0": {2, 2}, // P1 v P2: a semifinal, fought after P4 v P5
		"m-r1-3": {3, 1}, // P4 v P5: the one first-round bout
		"m-r2-1": {2, 3}, // P3 v W(P4 v P5)
		"m-r3-0": {1, 4}, // the final
	} {
		m := matchByID(t, want, id)
		m.DisplayRound, m.MatchNumber = rn[0], rn[1]
	}
	assert.Equal(t, want, got)
}

func TestRestampRoundsFromFeeders_SecondCallChangesNothing(t *testing.T) {
	b := loadV21FiveEntrant(t)
	_, err := b.RestampRoundsFromFeeders()
	require.NoError(t, err)

	once, err := json.Marshal(b)
	require.NoError(t, err)
	changes, err := b.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Empty(t, changes)
	twice, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, string(once), string(twice))
}

// A bracket from before the display metadata carries no Feeders anywhere. It
// predates the misclassification too, so it is left exactly as stored, match
// numbers included.
func TestRestampRoundsFromFeeders_NoFeedersLeavesTheBracketAlone(t *testing.T) {
	b := loadV21FiveEntrant(t)
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			b.Rounds[ri][mi].Feeders = nil
		}
	}
	before, err := json.Marshal(b)
	require.NoError(t, err)

	changes, err := b.RestampRoundsFromFeeders()
	require.ErrorIs(t, err, state.ErrBracketNoFeeders)
	assert.Nil(t, changes)
	after, err := json.Marshal(b)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after))
}

// Feeders that do not form the tree generation writes are not guessed at: the
// bracket is left exactly as stored, rounds and numbers alike.
func TestRestampRoundsFromFeeders_UnwalkableFeedersLeaveTheBracketAlone(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(t *testing.T, b *state.Bracket)
	}{
		{"a real match the walk does not reach", func(t *testing.T, b *state.Bracket) {
			// m-r1-1 made a real bout that no match names as a feeder.
			m := matchByID(t, b, "m-r1-1")
			m.SideA, m.SideB, m.Hidden = "X", "Y", false
		}},
		{"a feeder naming a missing match", func(t *testing.T, b *state.Bracket) {
			matchByID(t, b, "m-r2-1").Feeders = []string{"", "m-r9-9"}
		}},
		{"a feeder naming a hidden match", func(t *testing.T, b *state.Bracket) {
			matchByID(t, b, "m-r2-1").Feeders = []string{"m-r1-2", "m-r1-3"}
		}},
		{"a match fed into twice", func(t *testing.T, b *state.Bracket) {
			matchByID(t, b, "m-r2-1").Feeders = []string{"m-r1-0", "m-r1-3"}
		}},
		{"a last round that is not one final", func(t *testing.T, b *state.Bracket) {
			last := len(b.Rounds) - 1
			b.Rounds[last] = append(b.Rounds[last], state.BracketMatch{ID: "m-r3-1", Hidden: true})
		}},
		{"a duplicated match id", func(t *testing.T, b *state.Bracket) {
			// A Hidden row sharing a later real bout's id: the walk itself
			// would succeed, so only the duplicate check can refuse it.
			matchByID(t, b, "m-r1-1").ID = "m-r1-3"
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := loadV21FiveEntrant(t)
			tc.mutate(t, b)
			before, err := json.Marshal(b)
			require.NoError(t, err)

			changes, err := b.RestampRoundsFromFeeders()
			require.ErrorIs(t, err, state.ErrBracketFeedersUnwalkable)
			assert.Nil(t, changes)
			after, err := json.Marshal(b)
			require.NoError(t, err)
			assert.Equal(t, string(before), string(after))
		})
	}
}

// Nothing to walk is not an error: an empty bracket, and a one-entrant bracket
// whose only match is a Hidden bye (generation stamps no round on it).
func TestRestampRoundsFromFeeders_NothingToWalk(t *testing.T) {
	var nilBracket *state.Bracket
	changes, err := nilBracket.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Nil(t, changes)

	changes, err = (&state.Bracket{}).RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Nil(t, changes)

	lone := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideA: "P1", Winner: "P1", Status: state.MatchStatusCompleted, Hidden: true, Feeders: []string{"", ""}},
	}}}
	changes, err = lone.RestampRoundsFromFeeders()
	require.NoError(t, err)
	assert.Nil(t, changes)
	assert.Zero(t, lone.Rounds[0][0].DisplayRound)
	assert.Zero(t, lone.Rounds[0][0].MatchNumber)
}

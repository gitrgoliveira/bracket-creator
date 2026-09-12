package state

// submatch_member_id_roundtrip_test.go pins the wire/storage round trip for
// SubMatchResult's bc-tmid pass 2 id triple (SideAMemberID/SideBMemberID/
// WinnerMemberID) through BOTH bout-log stores: pool-matches.csv (via
// MatchResult.SubResults, JSON-encoded into one CSV cell) and bracket.json
// (via BracketMatch.SubResults, plain JSON). A file written before these
// fields existed -- no member ids anywhere in the bout log -- must still
// parse cleanly, matching every other omitempty id field in this package.

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolMatchesSubResultMemberIDs_RoundTrip: a pool match's bout log
// carrying the id triple survives a save/load cycle intact, and a
// pool-matches.csv row with a legacy-shaped (no member ids) SubResults cell
// still parses.
func TestPoolMatchesSubResultMemberIDs_RoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID = "pool-subresult-member-ids"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "T"}))

	matches := []MatchResult{
		{
			ID:    "P1-0",
			SideA: "TeamA",
			SideB: "TeamB",
			SubResults: []SubMatchResult{
				{
					Position:       1,
					SideA:          "Sato",
					SideAMemberID:  "id-sato",
					SideB:          "Tanaka",
					SideBMemberID:  "id-tanaka",
					Winner:         "Sato",
					WinnerMemberID: "id-sato",
					IpponsA:        []string{"M"},
					IpponsB:        []string{},
				},
				{
					// A second, legacy-shaped bout with no member ids at all,
					// in the SAME file, alongside the repaired one above.
					Position: 2,
					SideA:    "Legacy A",
					SideB:    "Legacy B",
				},
			},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	require.Len(t, loaded[0].SubResults, 2)

	repaired := loaded[0].SubResults[0]
	assert.Equal(t, "id-sato", repaired.SideAMemberID)
	assert.Equal(t, "id-tanaka", repaired.SideBMemberID)
	assert.Equal(t, "id-sato", repaired.WinnerMemberID)

	legacy := loaded[0].SubResults[1]
	assert.Empty(t, legacy.SideAMemberID, "a legacy-shaped bout row parses with no id, not an invented one")
	assert.Empty(t, legacy.SideBMemberID)
	assert.Empty(t, legacy.WinnerMemberID)
	assert.Equal(t, "Legacy A", legacy.SideA, "names are untouched by the round trip")
}

// TestBracketSubResultMemberIDs_RoundTrip: the bracket.json twin of the
// pool-matches test above -- a BracketMatch's bout log carrying the id
// triple survives a save/load cycle, and a legacy-shaped bout with no
// member ids in the SAME file still parses.
func TestBracketSubResultMemberIDs_RoundTrip(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID = "bracket-subresult-member-ids"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "T"}))

	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{
				{
					ID:    "M1",
					SideA: "Team A",
					SideB: "Team B",
					SubResults: []SubMatchResult{
						{
							Position:       1,
							SideA:          "Sato",
							SideAMemberID:  "id-sato",
							SideB:          "Tanaka",
							SideBMemberID:  "id-tanaka",
							Winner:         "Tanaka",
							WinnerMemberID: "id-tanaka",
						},
						{
							Position: 2,
							SideA:    "Legacy A",
							SideB:    "Legacy B",
						},
					},
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, loaded.Rounds[0][0].SubResults, 2)

	repaired := loaded.Rounds[0][0].SubResults[0]
	assert.Equal(t, "id-sato", repaired.SideAMemberID)
	assert.Equal(t, "id-tanaka", repaired.SideBMemberID)
	assert.Equal(t, "id-tanaka", repaired.WinnerMemberID)

	legacy := loaded.Rounds[0][0].SubResults[1]
	assert.Empty(t, legacy.SideAMemberID, "a legacy-shaped bout row parses with no id, not an invented one")
	assert.Empty(t, legacy.SideBMemberID)
	assert.Empty(t, legacy.WinnerMemberID)
}

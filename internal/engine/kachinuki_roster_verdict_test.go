package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The roster read answers the advance's own verdict (decideKachinukiAdvance):
// NextPairing is what Record bout would then do, and Exhausted names a side a
// recorded win left with nobody, on the knowledge of a lineup or member number
// order only (a side that fields nobody has an empty queue and is never
// exhausted). Each case reads the roster, then taps Record bout, and pins the
// read against what the tap did.
func TestKachinukiRoster_AnswersTheAdvancesVerdict(t *testing.T) {
	cases := []struct {
		name             string
		size             int
		aNames, bNames   []string
		lineupA, lineupB bool
		// bouts builds the stored log from the seeded teams' members.
		bouts func(rt rosterTeams) []state.SubMatchResult
		// recordFirst taps Record bout once before the read, so the last bout
		// is stored as recorded.
		recordFirst bool
		// membersUnreadable breaks team-members.yaml once the match is stored.
		membersUnreadable              bool
		wantNext                       bool
		wantExhaustedA, wantExhaustedB bool
	}{
		{
			name: "an unrecorded lead with fighters behind appends", size: 3,
			aNames: []string{"A1", "A2", "A3"}, bNames: []string{"B1", "B2", "B3"}, lineupA: true, lineupB: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false)}
			},
			wantNext: true,
		},
		{
			name: "a recorded win that leaves the loser's lineup with nobody", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{
					rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
					rosterBout(2, rt.a[0], rt.b[1], &rt.a[0], false),
				}
			},
			recordFirst: true, wantExhaustedB: true,
		},
		{
			name: "an unrecorded win at exhaustion is no result yet", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{
					rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
					rosterBout(2, rt.a[0], rt.b[1], &rt.a[0], false),
				}
			},
		},
		{
			// No lineup: the side fields its members by number, so a recorded
			// win over its last member leaves it with nobody.
			name: "a recorded win over a side with no lineup exhausts it by member number", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: false,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{
					rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
					rosterBout(2, rt.a[0], rt.b[1], &rt.a[0], false),
				}
			},
			recordFirst: true, wantExhaustedB: true,
		},
		{
			// No lineup and team members that cannot be read: the side fields
			// nobody and its queue is empty, so the empty queue says nothing
			// about who it has left: nobody is exhausted.
			name: "a recorded win over a side that fields nobody exhausts nobody", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: false,
			membersUnreadable: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{
					rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false),
					rosterBout(2, rt.a[0], rt.b[1], &rt.a[0], false),
				}
			},
			recordFirst: true,
		},
		{
			name: "a recorded draw with both sides out exhausts nobody", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{
					rosterBout(1, rt.a[0], rt.b[0], nil, true),
					rosterBout(2, rt.a[1], rt.b[1], nil, true),
				}
			},
			recordFirst: true,
		},
		{
			name: "a bout with no outcome appends nothing", size: 2,
			aNames: []string{"A1", "A2"}, bNames: []string{"B1", "B2"}, lineupA: true, lineupB: true,
			bouts: func(rt rosterTeams) []state.SubMatchResult {
				return []state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], nil, false)}
			},
		},
	}
	for _, tc := range cases {
		for _, bracket := range []bool{false, true} {
			t.Run(tc.name+"/"+branchName(bracket), func(t *testing.T) {
				eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-verdict", tc.size,
					func(c *state.Competition) { c.Format = state.CompFormatMixed })
				rt := seedRosterTeams(t, store, comp.ID, tc.size, tc.aNames, tc.bNames, tc.lineupA, tc.lineupB)
				matchID := saveRosterMatch(t, store, comp.ID, rt, tc.bouts(rt), bracket)
				if tc.membersUnreadable {
					makeTeamMembersUnreadable(t, store, comp.ID)
				}
				if tc.recordFirst {
					appended, _, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
					require.NoError(t, err)
					require.False(t, appended, "the fixture's recorded bout stays last")
				}

				got, err := eng.KachinukiRoster(comp.ID, matchID)
				require.NoError(t, err)
				assert.Equal(t, tc.wantNext, got.NextPairing, "nextPairing")
				assert.Equal(t, tc.wantExhaustedA, got.SideA.Exhausted, "sideA.exhausted")
				assert.Equal(t, tc.wantExhaustedB, got.SideB.Exhausted, "sideB.exhausted")
				if tc.membersUnreadable {
					assert.Equal(t, kachinukiMembersUnreadWarning, got.Warning)
				} else {
					assert.Empty(t, got.Warning)
				}

				// The pin: the read's verdict is what Record bout then does.
				appended, _, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
				require.NoError(t, err)
				assert.Equal(t, got.NextPairing, appended, "the read said what Record bout does")
			})
		}
	}
}

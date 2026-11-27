package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestSwapCandidates_ARefusedSwapIsMarkedNotFatal: in a fill-bracket draw whose
// drafted 2nd places come from the seeded pools (33 entrants, 5 seeds, the
// exact supply), a swap that moves a seed so a seeded pool is lost cannot
// rebuild the knockout ("seed more pools"). The candidates answer must still
// succeed, mark that candidate refused with no dojos, and give the rest their
// normal verdicts, and the real swap must refuse the same pair.
func TestSwapCandidates_ARefusedSwapIsMarkedNotFatal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-cand-refused"
	drawSeededComp(t, eng, store, compID, state.CompFormatMixed, 33, 5, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A", "B"}
	})
	roster, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)

	var pickID string
	var refused, other *SwapCandidate
	var answer *SwapCandidates
	for _, p := range roster {
		got, err := eng.SwapCandidatesFor(compID, p.ID)
		require.NoError(t, err, "a refused swap must never fail the whole answer")
		for i := range got.Candidates {
			if got.Candidates[i].Verdict == SwapVerdictRefused && refused == nil {
				pickID, refused, answer = p.ID, &got.Candidates[i], got
			}
		}
		if refused != nil {
			break
		}
	}
	require.NotNil(t, refused, "the 33-entrant, 5-seed fill-bracket draw must hold a refused swap")
	assert.Empty(t, refused.Dojos, "a refused candidate names no dojos")
	for i := range answer.Candidates {
		if answer.Candidates[i].Verdict != SwapVerdictRefused {
			other = &answer.Candidates[i]
			break
		}
	}
	require.NotNil(t, other, "the pick's other candidates keep a normal verdict")
	assert.Contains(t, []string{SwapVerdictWorse, SwapVerdictBetter, SwapVerdictSame}, other.Verdict)

	pick, ok := candidatePlace(answer, pickID)
	require.True(t, ok)
	_, err = eng.SwapDrawPlaces(compID, SwapRequest{
		A:      SwapSide{ID: pickID, Place: pick},
		B:      SwapSide{ID: refused.ID, Place: refused.Place},
		DryRun: true,
	})
	var ve *ValidationError
	require.ErrorAs(t, err, &ve, "the real swap answers the refusal the mark stands for")
}

// candidatePlace is the place the answer gives the pick.
func candidatePlace(a *SwapCandidates, id string) (DrawPlace, bool) {
	if a.Pick.ID != id {
		return DrawPlace{}, false
	}
	return a.Pick.Place, true
}

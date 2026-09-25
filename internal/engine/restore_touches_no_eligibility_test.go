package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A correction the server refuses is rolled back by replaying the match's
// prior result (rollbackMatchResultTx, matchWriteRestore). That replay used to
// re-derive eligibility from the snapshot, so a snapshot holding a withdrawal
// re-barred its loser: a kiken-injury competitor the doctor had cleared, whose
// match the operator then tried to correct, was made ineligible again by a
// correction that never landed. The first attempt of the confirm flow is
// always refused (downstream_knockout_played) and the score door commits the
// transaction on that refusal, so the re-bar reached disk every time.
func TestRefusedCorrectionRollbackKeepsAReinstatedCompetitorEligible(t *testing.T) {
	f := newRQFixture(t, "rq-restore-elig", 1, [][]string{{"A1", "A2"}, {"B1", "B2"}})

	// A2 withdraws injured from Pool A-0 (A1 is aka, A2 shiro), so A1 wins
	// the pool; B1 wins Pool B; A1 then beats B1 in the knockout.
	_, st, err := f.eng.RecordDecision(f.compID, "Pool A-0", "kiken-injury", "shiro", "", nil, false)
	require.NoError(t, err)
	require.NotNil(t, st)
	require.Equal(t, rqID("A2"), st.PlayerID)
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	ko, _ := f.slot("Pool A-1st")
	require.NoError(t, f.scoreKO(ko.ID, "A1"))

	// The doctor clears A2.
	reinstated, err := f.eng.ReinstateCompetitor(f.compID, rqID("A2"))
	require.NoError(t, err)
	require.True(t, reinstated.Eligible)

	statusPath := filepath.Join(f.store.GetFolder(), "competitions", f.compID, "competitor-status.yaml")
	before, err := os.ReadFile(statusPath)
	require.NoError(t, err)

	// The operator corrects Pool A-0 to a fought win for A2 (a decided
	// result, so it replaces the withdrawal rather than keeping it). A2 then
	// tops Pool A, moving a qualifier out of a knockout match already fought:
	// refused until confirmed, and the pool row is rolled back.
	correction := f.poolResult("Pool A-0", "A2")
	correction.Decision = "fought"
	correction.CorrectionReason = "the withdrawal was recorded on the wrong match"
	err = f.write("Pool A-0", correction)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)

	assert.Equal(t, "kiken-injury", loadPoolMatchByID(t, f.store, f.compID, "Pool A-0").Decision,
		"the refused correction is rolled back")
	statuses, err := f.store.LoadCompetitorStatus(f.compID)
	require.NoError(t, err)
	assert.True(t, statuses[rqID("A2")].Eligible, "the rollback must not re-bar the reinstated competitor")
	after, err := os.ReadFile(statusPath)
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "a refused write leaves competitor-status.yaml untouched")
}

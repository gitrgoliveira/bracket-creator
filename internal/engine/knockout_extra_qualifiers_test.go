package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The extra-qualifier modes put "Pool X-2nd" placeholders into the LIVE
// bracket while forcing PoolWinners to 1, and ResolveQualifiedPools used to
// build its label resolver only for ranks 1..EffectivePoolWinners(). The -2nd
// keys therefore never existed, those slots stayed placeholders after every
// pool finished, their round-1 matches never became playable, and the
// competition sat in pools status forever -- the whole mode was unusable in
// the app one screen past the draw the UAT stopped at (code-review round,
// cross-file tracer).
//
// The fix shares state.Competition.MatchWinnerRanksNeeded() with the Excel
// export, which hit the IDENTICAL bug earlier (a crossed 2nd rendered as
// inert text) and already owns "how many ranks can a placeholder reference".
//
// Both non-standard modes are pinned end to end through the REAL pipeline:
// StartCompetition builds the draw, every pool match is completed, and
// MaybeAutoCompletePools must leave a bracket with no pool placeholder on any
// side. Fault injection (verified, reverted): restoring the resolver's rank
// bound to EffectivePoolWinners() turns both red with a surviving "-2nd" side.

// completeAllPoolMatches marks every pool match completed with the winner
// chosen by POOL ORDER (the earlier player in the pool's roster wins), which
// yields a strict ranking in every pool -- no ties, so no tiebreaker
// injection can hold the pool phase open.
func completeAllPoolMatches(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	order := map[string]int{}
	for _, p := range pools {
		for i, pl := range p.Players {
			order[pl.Name] = i
		}
	}
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, matches)
	for i := range matches {
		m := &matches[i]
		m.Status = state.MatchStatusCompleted
		m.Winner = m.SideA
		m.IpponsA = []string{"M"}
		if order[m.SideB] < order[m.SideA] {
			m.Winner = m.SideB
			m.IpponsA = nil
			m.IpponsB = []string{"M"}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	_, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
}

// assertBracketFullyResolved fails on any bracket side still holding a pool
// placeholder, naming the slot; -2nd slots are the regression's signature.
func assertBracketFullyResolved(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, b)
	sawSecond := false
	for ri, round := range b.Rounds {
		for mi, m := range round {
			if strings.HasSuffix(m.PlaceholderA, "-2nd") || strings.HasSuffix(m.PlaceholderB, "-2nd") {
				sawSecond = true
			}
			for _, s := range [...]struct{ side, v string }{{"A", m.SideA}, {"B", m.SideB}} {
				assert.Falsef(t, helper.IsPoolFinalistPlaceholder(s.v),
					"r%d m%d side %s still reads %q after every pool completed: the resolver never built a key for it and this match can never become playable",
					ri, mi, s.side, s.v)
			}
		}
	}
	require.True(t, sawSecond,
		"fixture must actually seat a drafted/crossed 2nd, or this test does not exercise the -2nd resolver path")
}

func startExtraQualifiersComp(t *testing.T, eng *Engine, store *state.Store, compID, mode string, entrants int, courts []string) {
	t.Helper()
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = mode
		c.Courts = courts
	})
	names := make([]string, entrants)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))
}

func TestFillBracketDraftedSecondResolvesWhenPoolsComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-resolves-2nd"

	// 11 entrants at minimum 3: 3 pools (one of 3, two of 4), 1 drafted 2nd
	// from an oversized pool -- the docs' own worked example.
	startExtraQualifiersComp(t, eng, store, compID, state.ExtraQualifiersFillBracket, 11, []string{"A"})
	completeAllPoolMatches(t, eng, store, compID)
	assertBracketFullyResolved(t, store, compID)
}

// TestFillBracketGappedSeedSurvivorsStillDraw is the end-to-end pin for
// FillBracketPoolCount's rule-4 rank filter, through the one pipeline that
// actually produces gapped ranks: check-in. The operator seeds a CONTIGUOUS
// 1..14 (LoadSeeds validates that); the players holding ranks 3..12 are
// no-shows, so dropSeedAssignments leaves survivors with raw ranks
// {1, 2, 13, 14} among 38 checked-in entrants.
//
// A scalar seeded-player count read that as four seeded pools and formation
// promised 12 pools with 4 drafted 2nds -- but ranks 13 and 14 exceed the pool
// count and wrap into already-seeded pools, so the cut held only two seeded
// pools plus two oversized, and SelectFillBracketDrafts refused at draw time
// with "seed more pools", to an operator who had seeded fourteen. Counting
// only ranks that can land their own pool (r <= P), formation now cuts the
// two-live-seed shape (10 pools, 6 drafts from 8 oversized) and the draw
// builds and resolves.
//
// Fault injection (verified, reverted): restoring the scalar count (supplied
// reading len(ranks) instead of ranks <= p) turns this red at
// StartCompetition with the selection refusal above.
func TestFillBracketGappedSeedSurvivorsStillDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "fill-bracket-gapped-seed-survivors"

	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolSizeMode = "min"
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		c.Courts = []string{"A"}
		c.CheckInEnabled = true
	})

	// 48 registered, 38 checked in: every seeded player holding ranks 3..12
	// is a no-show.
	players := make([]domain.Player, 48)
	var seeds []domain.SeedAssignment
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Player%02d", i), Dojo: fmt.Sprintf("Dojo%02d", i)}
		if i < 14 {
			seeds = append(seeds, domain.SeedAssignment{Name: players[i].Name, SeedRank: i + 1})
		}
		checkedIn := i == 0 || i == 1 || i == 12 || i == 13 || i >= 14
		players[i].CheckedIn = checkedIn
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SaveSeeds(compID, seeds))

	require.NoError(t, eng.StartCompetition(compID),
		"gapped seed survivors must re-cut to a shape their pools can supply, not refuse with \"seed more pools\"")

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	assert.Len(t, pools, 10,
		"the two-live-seed cut for 38 entrants: ranks 13 and 14 cannot land their own pool and must not be counted as draft supply")

	completeAllPoolMatches(t, eng, store, compID)
	assertBracketFullyResolved(t, store, compID)
}

func TestLargerPoolsCrossedSecondResolvesWhenPoolsComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "larger-pools-resolves-2nd"

	// 104 entrants at minimum 3 on 4 shiaijo: 34 pools, two oversized, each
	// crossed 2nd seated on the neighbour shiaijo -- the docs' worked example
	// and the shape the PR's browser UAT built through this same pipeline.
	// (Small counts like 13 on 2 shiaijo are legitimately out of scope for
	// the per-pool builder and refuse at StartCompetition.)
	startExtraQualifiersComp(t, eng, store, compID, state.ExtraQualifiersLargerPools, 104, []string{"A", "B", "C", "D"})
	completeAllPoolMatches(t, eng, store, compID)
	assertBracketFullyResolved(t, store, compID)
}

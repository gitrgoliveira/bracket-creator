package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// setupTeamLeagueComp sets up a team-league competition with four teams
// (Alpha, Beta, Gamma, Delta) in a single pool. All six round-robin matches
// are pre-saved in the provided standings configuration:
//
//   - "topTie":    Alpha and Beta share 1st (tied on all 8 criteria); Gamma 3rd; Delta 4th.
//   - "noTie":     Alpha 1st, Beta 2nd, Gamma 3rd, Delta 4th (clear standings).
//   - "bottomTie": Alpha 1st, Beta 2nd; Gamma and Delta share 3rd (tied at bottom).
//   - "threeWay":  Alpha, Beta, Gamma all tied at the top; Delta last.
//   - "belowBand": Alpha 1st, Beta 2nd; Gamma and Delta share 5th (more than 4 teams needed
//     for this, use sixTeamLeagueComp for that scenario instead).
func setupTeamLeagueComp(t *testing.T, compID string, scenario string, opts ...func(*state.Competition)) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	comp := &state.Competition{
		ID:       compID,
		Name:     "Team League Test",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		TeamSize: 2,
		Kind:     "team",
	}
	for _, opt := range opts {
		opt(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))

	teams := []string{"Alpha", "Beta", "Gamma", "Delta"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: func() []helper.Player {
			ps := make([]helper.Player, len(teams))
			for i, n := range teams {
				ps[i] = helper.Player{Name: n}
			}
			return ps
		}()},
	}))

	var matches []state.MatchResult
	switch scenario {
	case "topTie":
		// Alpha and Beta both beat Gamma and Delta, but drew each other →
		// W=2, L=1, T=1 for both (tie at the top). Gamma beat Delta.
		// Alpha vs Beta: draw
		// Alpha vs Gamma: Alpha wins
		// Alpha vs Delta: Alpha wins
		// Beta vs Gamma: Beta wins
		// Beta vs Delta: Beta wins
		// Gamma vs Delta: Gamma wins
		matches = teamLeagueMatchSet([]teamLeagueResult{
			{"Pool A-0", "Alpha", "Beta", "", domain.DecisionHikiwake, [][]string{{"Alpha", "Beta", "", "hikiwake"}, {"Alpha", "Beta", "", "hikiwake"}}},
			{"Pool A-1", "Alpha", "Gamma", "Alpha", "", [][]string{{"Alpha", "Gamma", "Alpha", ""}, {"Alpha", "Gamma", "Alpha", ""}}},
			{"Pool A-2", "Alpha", "Delta", "Alpha", "", [][]string{{"Alpha", "Delta", "Alpha", ""}, {"Alpha", "Delta", "Alpha", ""}}},
			{"Pool A-3", "Beta", "Gamma", "Beta", "", [][]string{{"Beta", "Gamma", "Beta", ""}, {"Beta", "Gamma", "Beta", ""}}},
			{"Pool A-4", "Beta", "Delta", "Beta", "", [][]string{{"Beta", "Delta", "Beta", ""}, {"Beta", "Delta", "Beta", ""}}},
			{"Pool A-5", "Gamma", "Delta", "Gamma", "", [][]string{{"Gamma", "Delta", "Gamma", ""}, {"Gamma", "Delta", "Gamma", ""}}},
		})
	case "noTie":
		// Alpha > Beta > Gamma > Delta (clear hierarchy)
		matches = teamLeagueMatchSet([]teamLeagueResult{
			{"Pool A-0", "Alpha", "Beta", "Alpha", "", [][]string{{"Alpha", "Beta", "Alpha", ""}, {"Alpha", "Beta", "Alpha", ""}}},
			{"Pool A-1", "Alpha", "Gamma", "Alpha", "", [][]string{{"Alpha", "Gamma", "Alpha", ""}, {"Alpha", "Gamma", "Alpha", ""}}},
			{"Pool A-2", "Alpha", "Delta", "Alpha", "", [][]string{{"Alpha", "Delta", "Alpha", ""}, {"Alpha", "Delta", "Alpha", ""}}},
			{"Pool A-3", "Beta", "Gamma", "Beta", "", [][]string{{"Beta", "Gamma", "Beta", ""}, {"Beta", "Gamma", "Beta", ""}}},
			{"Pool A-4", "Beta", "Delta", "Beta", "", [][]string{{"Beta", "Delta", "Beta", ""}, {"Beta", "Delta", "Beta", ""}}},
			{"Pool A-5", "Gamma", "Delta", "Gamma", "", [][]string{{"Gamma", "Delta", "Gamma", ""}, {"Gamma", "Delta", "Gamma", ""}}},
		})
		// Make standings different by giving Alpha one extra win over everyone else.
		// The match set above already achieves this: Alpha(3W) > Beta(2W) > Gamma(1W) > Delta(0W).
	case "bottomTie":
		// Alpha 1st, Beta 2nd; Gamma and Delta share 3rd (both 0 wins, all losses, drew each other).
		matches = teamLeagueMatchSet([]teamLeagueResult{
			{"Pool A-0", "Alpha", "Beta", "Alpha", "", [][]string{{"Alpha", "Beta", "Alpha", ""}, {"Alpha", "Beta", "Alpha", ""}}},
			{"Pool A-1", "Alpha", "Gamma", "Alpha", "", [][]string{{"Alpha", "Gamma", "Alpha", ""}, {"Alpha", "Gamma", "Alpha", ""}}},
			{"Pool A-2", "Alpha", "Delta", "Alpha", "", [][]string{{"Alpha", "Delta", "Alpha", ""}, {"Alpha", "Delta", "Alpha", ""}}},
			{"Pool A-3", "Beta", "Gamma", "Beta", "", [][]string{{"Beta", "Gamma", "Beta", ""}, {"Beta", "Gamma", "Beta", ""}}},
			{"Pool A-4", "Beta", "Delta", "Beta", "", [][]string{{"Beta", "Delta", "Beta", ""}, {"Beta", "Delta", "Beta", ""}}},
			// Gamma and Delta draw each other → both have 0W, 2L, 1T.
			{"Pool A-5", "Gamma", "Delta", "", domain.DecisionHikiwake, [][]string{{"Gamma", "Delta", "", "hikiwake"}, {"Gamma", "Delta", "", "hikiwake"}}},
		})
	case "threeWay":
		// Alpha, Beta, Gamma all have identical records: 1W, 1L, 1T.
		// Delta loses all.
		matches = teamLeagueMatchSet([]teamLeagueResult{
			{"Pool A-0", "Alpha", "Beta", "Alpha", "", [][]string{{"Alpha", "Beta", "Alpha", ""}, {"Alpha", "Beta", "Alpha", ""}}},
			// Beta beats Gamma
			{"Pool A-1", "Beta", "Gamma", "Beta", "", [][]string{{"Beta", "Gamma", "Beta", ""}, {"Beta", "Gamma", "Beta", ""}}},
			// Gamma beats Alpha (so Alpha: 1W 1L; Beta: 1W 1L; Gamma: 1W 1L, all tied with each other;
			// they all beat Delta)
			{"Pool A-2", "Gamma", "Alpha", "Gamma", "", [][]string{{"Gamma", "Alpha", "Gamma", ""}, {"Gamma", "Alpha", "Gamma", ""}}},
			{"Pool A-3", "Alpha", "Delta", "Alpha", "", [][]string{{"Alpha", "Delta", "Alpha", ""}, {"Alpha", "Delta", "Alpha", ""}}},
			{"Pool A-4", "Beta", "Delta", "Beta", "", [][]string{{"Beta", "Delta", "Beta", ""}, {"Beta", "Delta", "Beta", ""}}},
			{"Pool A-5", "Gamma", "Delta", "Gamma", "", [][]string{{"Gamma", "Delta", "Gamma", ""}, {"Gamma", "Delta", "Gamma", ""}}},
		})
	default:
		t.Fatalf("unknown scenario %q", scenario)
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// teamLeagueResult is a compact fixture descriptor for a completed team match.
type teamLeagueResult struct {
	id       string
	sideA    string
	sideB    string
	winner   string
	decision domain.Decision
	// subs: each sub-entry is [sideA, sideB, winner, decision]
	subs [][]string
}

// teamLeagueMatchSet converts a slice of teamLeagueResult into MatchResult entries.
func teamLeagueMatchSet(results []teamLeagueResult) []state.MatchResult {
	out := make([]state.MatchResult, len(results))
	for i, r := range results {
		subs := make([]state.SubMatchResult, len(r.subs))
		for j, s := range r.subs {
			subs[j] = state.SubMatchResult{
				Position: j + 1,
				SideA:    s[0],
				SideB:    s[1],
				Winner:   s[2],
				Decision: s[3],
			}
		}
		out[i] = state.MatchResult{
			ID:         r.id,
			SideA:      r.sideA,
			SideB:      r.sideB,
			Status:     state.MatchStatusCompleted,
			Winner:     r.winner,
			Decision:   string(r.decision),
			Court:      "A",
			SubResults: subs,
		}
	}
	return out
}

// --- LeagueTiebreakCandidates tests ---

// TestLeagueTiebreakCandidates_NonLeague verifies that non-league formats return
// no candidates (the function is a no-op for mixed/playoffs/swiss).
func TestLeagueTiebreakCandidates_NonLeague(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "non-league",
		Format:   state.CompFormatMixed,
		Status:   state.CompStatusPools,
		TeamSize: 2,
		Kind:     "team",
	}))
	candidates, err := eng.LeagueTiebreakCandidates("non-league")
	require.NoError(t, err)
	assert.Empty(t, candidates, "non-league should return no candidates")
}

// TestLeagueTiebreakCandidates_IndividualLeague verifies that an individual-format
// league (TeamSize==0) returns no candidates, league playoff is team-only.
func TestLeagueTiebreakCandidates_IndividualLeague(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "ind-league",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		TeamSize: 0,
		Kind:     "individual",
	}))
	candidates, err := eng.LeagueTiebreakCandidates("ind-league")
	require.NoError(t, err)
	assert.Empty(t, candidates, "individual league should return no candidates")
}

// TestLeagueTiebreakCandidates_NoTie verifies that a team-league with distinct
// standings (no ties) returns no candidates.
func TestLeagueTiebreakCandidates_NoTie(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-notie", "noTie")
	candidates, err := eng.LeagueTiebreakCandidates("league-notie")
	require.NoError(t, err)
	assert.Empty(t, candidates, "no-tie league should return no candidates")
}

// TestLeagueTiebreakCandidates_TopTie verifies that two teams tied at 1st/2nd
// produce one consequential group (intersects [1..3]).
func TestLeagueTiebreakCandidates_TopTie(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-toptie", "topTie")

	candidates, err := eng.LeagueTiebreakCandidates("league-toptie")
	require.NoError(t, err)
	require.Len(t, candidates, 1, "one consequential tied group at positions 1–2")

	g := candidates[0]
	assert.Equal(t, 1, g.MinPosition)
	assert.Equal(t, 2, g.MaxPosition)
	assert.Len(t, g.Teams, 2)

	names := []string{g.Teams[0].Player.Name, g.Teams[1].Player.Name}
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, names)
}

// TestLeagueTiebreakCandidates_ThreeWayTopTie verifies that three teams tied at
// the top (positions 1–3) produce one consequential group of 3.
func TestLeagueTiebreakCandidates_ThreeWayTopTie(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-threeway", "threeWay")

	candidates, err := eng.LeagueTiebreakCandidates("league-threeway")
	require.NoError(t, err)
	require.Len(t, candidates, 1, "one consequential group for 3-way top tie")

	g := candidates[0]
	assert.Equal(t, 1, g.MinPosition)
	assert.Len(t, g.Teams, 3, "all three tied teams in the group")
}

// TestLeagueTiebreakCandidates_BottomTieTwoThirdsFalse verifies that a tie at
// positions 3–4 IS consequential when LeagueTwoThirdPlaces is false (default)
// and TopN >= 3.
func TestLeagueTiebreakCandidates_BottomTieTwoThirdsFalse(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-bottomtie-default", "bottomTie",
		func(c *state.Competition) {
			c.LeagueTwoThirdPlaces = false
			c.LeagueTiebreakTopN = 3
		})

	candidates, err := eng.LeagueTiebreakCandidates("league-bottomtie-default")
	require.NoError(t, err)
	require.Len(t, candidates, 1, "3rd/4th tie is consequential when TwoThirdPlaces=false")

	g := candidates[0]
	assert.Equal(t, 3, g.MinPosition, "tie starts at 3rd")
	assert.Equal(t, 4, g.MaxPosition, "tie ends at 4th")
}

// TestLeagueTiebreakCandidates_BottomTieTwoThirdsTrue verifies that a tie at
// positions 3–4 is NOT consequential when LeagueTwoThirdPlaces is true, both
// teams share 3rd place and no decider is needed.
func TestLeagueTiebreakCandidates_BottomTieTwoThirdsTrue(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-bottomtie-twothirds", "bottomTie",
		func(c *state.Competition) {
			c.LeagueTwoThirdPlaces = true
			c.LeagueTiebreakTopN = 3
		})

	candidates, err := eng.LeagueTiebreakCandidates("league-bottomtie-twothirds")
	require.NoError(t, err)
	assert.Empty(t, candidates,
		"3rd/4th tie is non-consequential when LeagueTwoThirdPlaces=true (both share 3rd)")
}

// TestLeagueTiebreakCandidates_BelowBandWithTopN4 verifies that a tie at positions
// 5–6 is NOT consequential when TopN=4, those teams sit outside the tie-break band.
// Uses a 6-team league where Alpha/Beta/Gamma/Delta occupy the top 4 and
// Epsilon/Zeta tie at 5th.
func TestLeagueTiebreakCandidates_BelowBandWithTopN4(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "league-below-band"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   compID,
		Format:               state.CompFormatLeague,
		Status:               state.CompStatusPools,
		Courts:               []string{"A"},
		TeamSize:             2,
		Kind:                 "team",
		LeagueTiebreakTopN:   4,
		LeagueTwoThirdPlaces: false,
	}))

	// 6 teams: Alpha > Beta > Gamma > Delta, then Epsilon and Zeta tied at 5th.
	teams := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: func() []helper.Player {
			ps := make([]helper.Player, len(teams))
			for i, n := range teams {
				ps[i] = helper.Player{Name: n}
			}
			return ps
		}()},
	}))

	// Build a match set where Alpha wins everything, Beta wins all except Alpha,
	// Gamma wins all except Alpha/Beta, Delta wins all except Alpha/Beta/Gamma,
	// and Epsilon/Zeta draw each other and lose to all others.
	var matches []state.MatchResult
	idx := 0
	for i, ta := range teams {
		for _, tb := range teams[i+1:] {
			m := state.MatchResult{
				ID:     fmt.Sprintf("Pool A-%d", idx),
				SideA:  ta,
				SideB:  tb,
				Status: state.MatchStatusCompleted,
				Court:  "A",
			}
			switch {
			case ta == "Epsilon" && tb == "Zeta":
				// Draw: both share 5th (0W 4L 1T each)
				m.Winner = ""
				m.Decision = string(domain.DecisionHikiwake)
				m.SubResults = []state.SubMatchResult{
					{Position: 1, SideA: ta, SideB: tb, Winner: "", Decision: string(domain.DecisionHikiwake)},
					{Position: 2, SideA: ta, SideB: tb, Winner: "", Decision: string(domain.DecisionHikiwake)},
				}
			case tb == "Epsilon" || tb == "Zeta":
				// ta beats the bottom team
				m.Winner = ta
				m.SubResults = []state.SubMatchResult{
					{Position: 1, SideA: ta, SideB: tb, Winner: ta},
					{Position: 2, SideA: ta, SideB: tb, Winner: ta},
				}
			default:
				// Top-4 play: the alphabetically earlier team (higher in the list)
				// wins to produce a strict ordering: Alpha > Beta > Gamma > Delta.
				m.Winner = ta
				m.SubResults = []state.SubMatchResult{
					{Position: 1, SideA: ta, SideB: tb, Winner: ta},
					{Position: 2, SideA: ta, SideB: tb, Winner: ta},
				}
			}
			matches = append(matches, m)
			idx++
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	candidates, err := eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, candidates,
		"5th/6th tie is not consequential when TopN=4")
}

// --- MaybeAutoCompletePools team-league tests ---

// TestMaybeAutoCompletePools_TeamLeague_TopTie_AwaitingPlayoff verifies that a
// team-league competition with a consequential top-position tie (e.g. 1st/2nd)
// returns AutoCompleteAwaitingLeagueTiebreak and does NOT auto-inject any DH
// matches and does NOT transition to completed.
func TestMaybeAutoCompletePools_TeamLeague_TopTie_AwaitingPlayoff(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "league-await-playoff", "topTie")

	outcome, err := eng.MaybeAutoCompletePools("league-await-playoff")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome,
		"top-position tie should block with AwaitingLeagueTiebreak")

	// Verify no DH matches were auto-injected.
	allMatches, err := store.LoadPoolMatches("league-await-playoff")
	require.NoError(t, err)
	for _, m := range allMatches {
		assert.False(t, IsPoolDaihyosenMatchID(m.ID),
			"MaybeAutoCompletePools must NOT inject DH matches for team-league (match %s)", m.ID)
	}

	// Competition must not have transitioned to completed.
	comp, err := store.LoadCompetition("league-await-playoff")
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status,
		"competition must remain in pools status when awaiting playoff")
}

// TestMaybeAutoCompletePools_TeamLeague_NoTie_Completes verifies that a team-league
// with no consequential ties completes normally with shared ranks.
func TestMaybeAutoCompletePools_TeamLeague_NoTie_Completes(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-notie-complete", "noTie")

	outcome, err := eng.MaybeAutoCompletePools("league-notie-complete")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome,
		"no-tie team-league should complete normally")
}

// TestMaybeAutoCompletePools_TeamLeague_BelowBand_Completes verifies that a
// team-league where the ONLY tie is below the consequential band (positions 3–4
// when TopN=3 and LeagueTwoThirdPlaces=true) completes normally with shared 3rd.
func TestMaybeAutoCompletePools_TeamLeague_BelowBand_Completes(t *testing.T) {
	eng, _ := setupTeamLeagueComp(t, "league-twothirds-complete", "bottomTie",
		func(c *state.Competition) {
			c.LeagueTwoThirdPlaces = true
			c.LeagueTiebreakTopN = 3
		})

	outcome, err := eng.MaybeAutoCompletePools("league-twothirds-complete")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome,
		"3rd/4th tie with TwoThirdPlaces=true should complete (shared 3rd, no tie-breaker)")
}

// TestMaybeAutoCompletePools_TeamLeague_ThreeWayTie_AwaitingPlayoff verifies that
// three teams tied at the top (positions 1–3) return AwaitingLeagueTiebreak and
// no DH is auto-injected.
func TestMaybeAutoCompletePools_TeamLeague_ThreeWayTie_AwaitingPlayoff(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "league-threeway-await", "threeWay")

	outcome, err := eng.MaybeAutoCompletePools("league-threeway-await")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome,
		"3-team top tie should block with AwaitingLeagueTiebreak")

	// No DH auto-injected.
	allMatches, err := store.LoadPoolMatches("league-threeway-await")
	require.NoError(t, err)
	for _, m := range allMatches {
		assert.False(t, IsPoolDaihyosenMatchID(m.ID),
			"no DH must be auto-injected for team-league (match %s)", m.ID)
	}
}

// TestMaybeAutoCompletePools_TeamLeague_DHCompletedAfterOperatorInject verifies
// the full operator-inject path: after the engine returns AwaitingLeagueTiebreak,
// the operator (via Phase 3b) calls GenerateLeagueTiebreakMatches, plays the DH
// matches, and then MaybeAutoCompletePools transitions to completed.
func TestMaybeAutoCompletePools_TeamLeague_DHCompletedAfterOperatorInject(t *testing.T) {
	eng, store := setupTeamLeagueComp(t, "league-operator-dh", "topTie")

	// First call: engine detects tie, returns AwaitingLeagueTiebreak.
	outcome, err := eng.MaybeAutoCompletePools("league-operator-dh")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome)

	// Operator injects DH for the tied group (Phase 3b path).
	injected, injErr := eng.GenerateLeagueTiebreakMatches("league-operator-dh", []string{"Alpha", "Beta"})
	require.NoError(t, injErr)
	require.Len(t, injected, 1, "one DH match for a two-way tie")

	// Second call: DH is pending → still no completion.
	outcome, err = eng.MaybeAutoCompletePools("league-operator-dh")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome,
		"pending DH match should block auto-completion")

	// Operator scores the DH match (Alpha wins).
	allMatches, err := store.LoadPoolMatches("league-operator-dh")
	require.NoError(t, err)
	for i := range allMatches {
		if IsPoolDaihyosenMatchID(allMatches[i].ID) {
			allMatches[i].Status = state.MatchStatusCompleted
			allMatches[i].Winner = allMatches[i].SideA
		}
	}
	require.NoError(t, store.SavePoolMatches("league-operator-dh", allMatches))

	// Third call: all done, should transition.
	outcome, err = eng.MaybeAutoCompletePools("league-operator-dh")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome,
		"completed DH should allow transition to complete")
}

// TestIsConsequentialTie covers the boundary logic for the two-thirds exemption
// and band intersection.
func TestIsConsequentialTie(t *testing.T) {
	team := func(name string) state.PlayerStanding {
		return state.PlayerStanding{Player: domain.Player{Name: name}}
	}

	tests := []struct {
		name       string
		minPos     int
		maxPos     int
		topN       int
		twoThirds  bool
		wantConseq bool
	}{
		// Within band, no exemption
		{"1st-2nd tie, TopN=3, noTwoThirds", 1, 2, 3, false, true},
		{"1st-3rd tie, TopN=3, noTwoThirds", 1, 3, 3, false, true},
		// Below band entirely
		{"4th-5th, TopN=3", 4, 5, 3, false, false},
		{"5th-6th, TopN=4", 5, 6, 4, false, false},
		// At the band edge but within
		{"3rd-4th, TopN=4, noTwoThirds", 3, 4, 4, false, true},
		{"3rd-4th, TopN=3, noTwoThirds", 3, 4, 3, false, true},
		// Two-thirds exemption: group entirely at pos>=3
		{"3rd-4th, TopN=3, twoThirds", 3, 4, 3, true, false},
		{"3rd-4th, TopN=4, twoThirds", 3, 4, 4, true, false},
		// Two-thirds exemption does NOT fire when group crosses 2nd/3rd boundary
		{"2nd-3rd, TopN=3, twoThirds", 2, 3, 3, true, true},
		{"1st-3rd, TopN=3, twoThirds", 1, 3, 3, true, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Build a group with MinPosition/MaxPosition as specified.
			teams := make([]state.PlayerStanding, tc.maxPos-tc.minPos+1)
			for i := range teams {
				teams[i] = team(fmt.Sprintf("Team%d", i))
			}
			g := TiedGroup{
				Teams:       teams,
				MinPosition: tc.minPos,
				MaxPosition: tc.maxPos,
			}
			comp := &state.Competition{
				LeagueTiebreakTopN:   tc.topN,
				LeagueTwoThirdPlaces: tc.twoThirds,
			}
			got := isConsequentialTie(g, comp)
			assert.Equal(t, tc.wantConseq, got,
				"isConsequentialTie(%d-%d, topN=%d, twoThirds=%v)", tc.minPos, tc.maxPos, tc.topN, tc.twoThirds)
		})
	}
}

// TestEffectiveTopN verifies the default-3 fallback and explicit values.
func TestEffectiveTopN(t *testing.T) {
	assert.Equal(t, 3, effectiveTopN(&state.Competition{LeagueTiebreakTopN: 0}), "zero defaults to 3")
	assert.Equal(t, 3, effectiveTopN(&state.Competition{LeagueTiebreakTopN: 3}))
	assert.Equal(t, 4, effectiveTopN(&state.Competition{LeagueTiebreakTopN: 4}))
}

// TestIsConsequentialTie_BandBoundaryEdgeCases focuses on the exact boundary
// where MinPosition == TopN (still consequential, within the band) vs
// MinPosition == TopN+1 (NOT consequential, one step outside), for both
// TopN=3 and TopN=4.
func TestIsConsequentialTie_BandBoundaryEdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		minPos     int
		maxPos     int
		topN       int
		twoThirds  bool
		wantConseq bool
	}{
		// TopN=3 boundary: minPos=3 is within [1..3] → consequential
		{"minPos==TopN3, no exemption", 3, 4, 3, false, true},
		// TopN=3 boundary: minPos=4 is outside [1..3] → NOT consequential
		{"minPos==TopN3+1, no exemption", 4, 5, 3, false, false},
		// TopN=4 boundary: minPos=4 is within [1..4] → consequential
		{"minPos==TopN4, no exemption", 4, 5, 4, false, true},
		// TopN=4 boundary: minPos=5 is outside [1..4] → NOT consequential
		{"minPos==TopN4+1, no exemption", 5, 6, 4, false, false},
		// Two-thirds exemption: tie at exactly 3–4 with topN=3 and twoThirds=true
		// → NOT consequential (all positions >= 3)
		{"minPos==TopN3 with TwoThirds", 3, 4, 3, true, false},
		// Two-thirds exemption does NOT fire when minPos < 3 (tie spans 2nd/3rd)
		{"minPos<3 with TwoThirds, topN=3", 2, 3, 3, true, true},
		// Default TopN=0 treated as 3: minPos=3 → consequential (within default band)
		{"minPos==3, TopN=0 defaults to 3", 3, 4, 0, false, true},
		// Default TopN=0 treated as 3: minPos=4 → NOT consequential
		{"minPos==4, TopN=0 defaults to 3", 4, 5, 0, false, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			teams := make([]state.PlayerStanding, tc.maxPos-tc.minPos+1)
			for i := range teams {
				teams[i] = state.PlayerStanding{Player: domain.Player{Name: fmt.Sprintf("T%d", i)}}
			}
			g := TiedGroup{Teams: teams, MinPosition: tc.minPos, MaxPosition: tc.maxPos}
			comp := &state.Competition{
				LeagueTiebreakTopN:   tc.topN,
				LeagueTwoThirdPlaces: tc.twoThirds,
			}
			got := isConsequentialTie(g, comp)
			assert.Equal(t, tc.wantConseq, got,
				"isConsequentialTie(%d-%d, topN=%d, twoThirds=%v)", tc.minPos, tc.maxPos, tc.topN, tc.twoThirds)
		})
	}
}

// TestLeagueTiebreakCandidates_LargeTieSpanningBand verifies that a single
// large tied group spanning positions 1–4 (all four teams perfectly tied)
// returns exactly one consequential candidate with the correct MinPosition and
// team count, and that the engine does NOT split it into multiple groups.
func TestLeagueTiebreakCandidates_LargeTieSpanningBand(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "league-large-tie"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Large Tie League",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		TeamSize: 2,
		Kind:     "team",
		// Default TopN=0 → effective 3; large tie spans positions 1–4 so even
		// just MinPosition=1 makes it consequential.
	}))

	teams := []string{"Alpha", "Beta", "Gamma", "Delta"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: func() []helper.Player {
			ps := make([]helper.Player, len(teams))
			for i, n := range teams {
				ps[i] = helper.Player{Name: n}
			}
			return ps
		}()},
	}))

	// All teams draw every match: everyone ends with 0 wins, 0 losses, 3 draws →
	// perfectly tied across all criteria at positions 1–4.
	var matches []state.MatchResult
	idx := 0
	for i, ta := range teams {
		for _, tb := range teams[i+1:] {
			matches = append(matches, state.MatchResult{
				ID:       fmt.Sprintf("Pool A-%d", idx),
				SideA:    ta,
				SideB:    tb,
				Status:   state.MatchStatusCompleted,
				Court:    "A",
				Decision: string(domain.DecisionHikiwake),
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: ta, SideB: tb, Decision: string(domain.DecisionHikiwake)},
					{Position: 2, SideA: ta, SideB: tb, Decision: string(domain.DecisionHikiwake)},
				},
			})
			idx++
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	candidates, err := eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	require.Len(t, candidates, 1, "all-tied 4-team league must produce exactly one candidate group")
	g := candidates[0]
	assert.Equal(t, 1, g.MinPosition, "4-way tie starts at position 1")
	assert.Equal(t, 4, g.MaxPosition, "4-way tie ends at position 4")
	assert.Len(t, g.Teams, 4, "all four teams in the single tied group")
}

// TestLeagueTiebreakCandidates_BelowBandSharedRanks verifies that a tie sitting
// entirely below the consequential band returns zero candidates (the league
// completes with shared ranks, no tiebreaker needed). Tested for both TopN=3
// and TopN=4 using a 6-team league where only positions 5–6 are tied.
func TestLeagueTiebreakCandidates_BelowBandSharedRanks(t *testing.T) {
	for _, topN := range []int{3, 4} {
		topN := topN
		t.Run(fmt.Sprintf("TopN=%d", topN), func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := fmt.Sprintf("league-below-band-shared-%d", topN)

			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID:                   compID,
				Format:               state.CompFormatLeague,
				Status:               state.CompStatusPools,
				Courts:               []string{"A"},
				TeamSize:             2,
				Kind:                 "team",
				LeagueTiebreakTopN:   topN,
				LeagueTwoThirdPlaces: false,
			}))

			teams := []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"}
			require.NoError(t, store.SavePools(compID, []helper.Pool{
				{PoolName: "Pool A", Players: func() []helper.Player {
					ps := make([]helper.Player, len(teams))
					for i, n := range teams {
						ps[i] = helper.Player{Name: n}
					}
					return ps
				}()},
			}))

			// Top 4 have clear ordering (each beats all below); Epsilon/Zeta draw each
			// other and lose all others → both stuck at position 5 (below the band).
			var ms []state.MatchResult
			idx := 0
			for i, ta := range teams {
				for _, tb := range teams[i+1:] {
					m := state.MatchResult{
						ID:     fmt.Sprintf("Pool A-%d", idx),
						SideA:  ta,
						SideB:  tb,
						Status: state.MatchStatusCompleted,
						Court:  "A",
					}
					if ta == "Epsilon" && tb == "Zeta" {
						m.Decision = string(domain.DecisionHikiwake)
						m.SubResults = []state.SubMatchResult{
							{Position: 1, SideA: ta, SideB: tb, Decision: string(domain.DecisionHikiwake)},
							{Position: 2, SideA: ta, SideB: tb, Decision: string(domain.DecisionHikiwake)},
						}
					} else {
						m.Winner = ta
						m.SubResults = []state.SubMatchResult{
							{Position: 1, SideA: ta, SideB: tb, Winner: ta},
							{Position: 2, SideA: ta, SideB: tb, Winner: ta},
						}
					}
					ms = append(ms, m)
					idx++
				}
			}
			require.NoError(t, store.SavePoolMatches(compID, ms))

			candidates, err := eng.LeagueTiebreakCandidates(compID)
			require.NoError(t, err)
			assert.Empty(t, candidates,
				"5th/6th tie is below topN=%d band, league should complete with shared ranks", topN)
		})
	}
}

// TestLeagueTiebreakCandidates_NonTeamAndNonLeague exercises the early-exit
// paths for competitions that do not require a tiebreaker at all.
func TestLeagueTiebreakCandidates_NonTeamAndNonLeague(t *testing.T) {
	t.Run("non-league format (swiss)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:       "swiss-comp",
			Format:   state.CompFormatSwiss,
			Status:   state.CompStatusPools,
			TeamSize: 2,
			Kind:     "team",
		}))
		cands, err := eng.LeagueTiebreakCandidates("swiss-comp")
		require.NoError(t, err)
		assert.Empty(t, cands, "swiss format must return no tiebreak candidates")
	})

	t.Run("league format but TeamSize==0 (individual)", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:       "ind-league-2",
			Format:   state.CompFormatLeague,
			Status:   state.CompStatusPools,
			TeamSize: 0,
			Kind:     "individual",
		}))
		cands, err := eng.LeagueTiebreakCandidates("ind-league-2")
		require.NoError(t, err)
		assert.Empty(t, cands, "individual league (TeamSize=0) must return no tiebreak candidates")
	})
}

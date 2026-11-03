package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// populatedTournament holds the router, store, engine, data folder, and
// entity identifiers for a populated tournament fixture used by backup
// and restore round-trip tests.
type populatedTournament struct {
	Router             *gin.Engine
	Store              *state.Store
	Eng                *engine.Engine
	Dir                string
	Password           string
	MixedCompID        string
	KnockoutCompID     string
	TeamCompID         string
	KachinukiCompID    string
	SelfRunCompID      string
	MixedScoredMatchID string
	MixedEventsMatchID string
	KnockoutMatchID    string
	TeamMatchID        string
	SelfRunMatchID     string
	TeamIDs            []string
	KachinukiTeamIDs   []string
	IneligiblePlayerID string
	OverriddenPoolName string
}

func populatedServeJSON(t *testing.T, r http.Handler, password, method, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	var bodyReader *bytes.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(t, err)
		bodyReader = bytes.NewReader(raw)
	} else {
		bodyReader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, bodyReader)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// buildPopulatedTournament builds a tournament in a temporary directory and
// populates all ten required record kinds primarily through the HTTP API.
func buildPopulatedTournament(t *testing.T) *populatedTournament {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir := t.TempDir()
	password := "secret"

	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Populated Fixture Taikai",
		Date:         "10-05-2026",
		Venue:        "Central Budokan",
		Courts:       []string{"A", "B"},
		Password:     password,
		DurationDays: 1,
		Mode:         state.TournamentModeOfficiated,
	}))

	eng := engine.New(store)
	mockFS := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html/>")},
	}
	res := resources.NewResources(nil, mockFS)
	router, hub, limiter := NewRouter(store, eng, res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)

	nowMs := time.Now().UnixMilli()

	pt := &populatedTournament{
		Router:          router,
		Store:           store,
		Eng:             eng,
		Dir:             dir,
		Password:        password,
		MixedCompID:     "ind-mixed",
		KnockoutCompID:  "ind-knockout",
		TeamCompID:      "team-fixed",
		KachinukiCompID: "k1",
		SelfRunCompID:   "ind-self-run",
	}

	// Kind 1 (individual mixed competition) and Kind 10 (officiated competition
	// with check-in enabled and admin-scored results).
	w := populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions", map[string]any{
		"id":             pt.MixedCompID,
		"name":           "Individual Mixed",
		"kind":           "individual",
		"format":         state.CompFormatMixed,
		"poolSize":       3,
		"poolWinners":    2,
		"courts":         []string{"A", "B"},
		"checkInEnabled": true,
		"players": []map[string]any{
			{"name": "Sato Ken", "dojo": "Tokyo Dojo", "seed": 1},
			{"name": "Suzuki Ren", "dojo": "Osaka Dojo", "seed": 2},
			{"name": "Takahashi Kai", "dojo": "Kyoto Dojo"},
			{"name": "Tanaka Ryu", "dojo": "Nagoya Dojo"},
			{"name": "Watanabe Shin", "dojo": "Fukuoka Dojo"},
			{"name": "Ito Haru", "dojo": "Sapporo Dojo"},
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	mixedPlayers, err := store.LoadParticipants(pt.MixedCompID, false)
	require.NoError(t, err)
	require.Len(t, mixedPlayers, 6)
	mixedPlayerIDs := make([]string, len(mixedPlayers))
	for i, p := range mixedPlayers {
		mixedPlayerIDs[i] = p.ID
	}

	// Check in all participants so checkInEnabled draws the full 6-player roster.
	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.MixedCompID+"/participants/checkin-bulk", map[string]any{
		"participantIds": mixedPlayerIDs,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.MixedCompID+"/start", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	poolMatches, err := store.LoadPoolMatches(pt.MixedCompID)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(poolMatches), 3)

	m0 := poolMatches[0]
	pt.MixedScoredMatchID = m0.ID
	escapedM0 := url.PathEscape(m0.ID)

	// Record first pool match score via the score API.
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.MixedCompID+"/matches/"+escapedM0+"/score", map[string]any{
		"id":         m0.ID,
		"sideA":      m0.SideA,
		"sideB":      m0.SideB,
		"sideAId":    m0.SideAID,
		"sideBId":    m0.SideBID,
		"winner":     m0.SideA,
		"winnerId":   m0.SideAID,
		"ipponsA":    []string{"M"},
		"ipponsB":    []string{},
		"status":     string(state.MatchStatusCompleted),
		"modifiedAt": nowMs - 20000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 5 (history): rescore the completed pool match with a correctionReason
	// so history/<matchId>.jsonl records the initial write and the correction.
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.MixedCompID+"/matches/"+escapedM0+"/score", map[string]any{
		"id":               m0.ID,
		"sideA":            m0.SideA,
		"sideB":            m0.SideB,
		"sideAId":          m0.SideAID,
		"sideBId":          m0.SideBID,
		"winner":           m0.SideA,
		"winnerId":         m0.SideAID,
		"ipponsA":          []string{"M", "K"},
		"ipponsB":          []string{"D"},
		"status":           string(state.MatchStatusCompleted),
		"correctionReason": "Scoring error: added second ippon",
		"modifiedAt":       nowMs - 15000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 6 (events): start a second pool match, POST a tap event to
	// /score-events so competitions/<id>/events/<matchId>.jsonl has an event line,
	// then finish the match so no live running match remains on the court.
	m1 := poolMatches[1]
	pt.MixedEventsMatchID = m1.ID
	escapedM1 := url.PathEscape(m1.ID)

	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.MixedCompID+"/matches/"+escapedM1+"/score", map[string]any{
		"id":         m1.ID,
		"sideA":      m1.SideA,
		"sideB":      m1.SideB,
		"sideAId":    m1.SideAID,
		"sideBId":    m1.SideBID,
		"status":     string(state.MatchStatusRunning),
		"startOnly":  true,
		"modifiedAt": nowMs - 12000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.MixedCompID+"/matches/"+escapedM1+"/score-events", map[string]any{
		"session": "fixture-session",
		"events": []map[string]any{
			{
				"id":     "fixture-ev-1",
				"seq":    1,
				"stamp":  nowMs - 11000,
				"kind":   "ippon.add",
				"side":   "a",
				"letter": "M",
			},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.MixedCompID+"/matches/"+escapedM1+"/score", map[string]any{
		"id":         m1.ID,
		"sideA":      m1.SideA,
		"sideB":      m1.SideB,
		"sideAId":    m1.SideAID,
		"sideBId":    m1.SideBID,
		"winner":     m1.SideA,
		"winnerId":   m1.SideAID,
		"ipponsA":    []string{"M"},
		"ipponsB":    []string{},
		"status":     string(state.MatchStatusCompleted),
		"modifiedAt": nowMs - 10000,
		"writeId":    "fixture-fin-1",
		"session":    "fixture-session",
		"basis":      map[string]any{"seen": 2, "seq": 1},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 7 (competitor statuses): record a withdrawn competitor through
	// POST /api/competitions/:id/competitor-status.
	mLast := poolMatches[len(poolMatches)-1]
	pt.IneligiblePlayerID = mLast.SideBID
	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.MixedCompID+"/competitor-status", map[string]any{
		"playerId": pt.IneligiblePlayerID,
		"eligible": false,
		"reason":   "kiken-injury: shoulder strain",
		"matchId":  mLast.ID,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 8 (override): save a pool rank override through
	// PUT /api/competitions/:id/pools/:poolId/override-rank so overrides.json exists.
	pools, err := store.LoadPools(pt.MixedCompID)
	require.NoError(t, err)
	require.NotEmpty(t, pools)
	require.GreaterOrEqual(t, len(pools[0].Players), 2)
	pt.OverriddenPoolName = pools[0].PoolName
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.MixedCompID+"/pools/"+url.PathEscape(pt.OverriddenPoolName)+"/override-rank", map[string]any{
		"ranks": []map[string]any{
			{"playerId": pools[0].Players[0].ID, "rank": 1},
			{"playerId": pools[0].Players[1].ID, "rank": 2},
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 2 (individual knockout competition with a generated bracket and
	// at least one recorded bracket result).
	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions", map[string]any{
		"id":     pt.KnockoutCompID,
		"name":   "Individual Knockout",
		"kind":   "individual",
		"format": state.CompFormatKnockout,
		"courts": []string{"A", "B"},
		"players": []map[string]any{
			{"name": "Yamamoto Jin", "dojo": "Kobe Dojo", "seed": 1},
			{"name": "Nakamura Sora", "dojo": "Sendai Dojo", "seed": 2},
			{"name": "Kobayashi Ren", "dojo": "Hiroshima Dojo"},
			{"name": "Kato Shin", "dojo": "Yokohama Dojo"},
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.KnockoutCompID+"/start", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	bracket, err := store.LoadBracket(pt.KnockoutCompID)
	require.NoError(t, err)
	require.NotNil(t, bracket)
	require.NotEmpty(t, bracket.Rounds)
	require.NotEmpty(t, bracket.Rounds[0])

	bm0 := bracket.Rounds[0][0]
	pt.KnockoutMatchID = bm0.ID
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.KnockoutCompID+"/matches/"+url.PathEscape(bm0.ID)+"/score", map[string]any{
		"id":         bm0.ID,
		"sideA":      bm0.SideA,
		"sideB":      bm0.SideB,
		"sideAId":    bm0.SideAID,
		"sideBId":    bm0.SideBID,
		"winner":     bm0.SideA,
		"winnerId":   bm0.SideAID,
		"ipponsA":    []string{"M", "K"},
		"ipponsB":    []string{"D"},
		"status":     string(state.MatchStatusCompleted),
		"modifiedAt": nowMs - 9000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 3 (team competition with team members added via the team-members route,
	// a saved starting lineup via PUT .../lineups/0, and a per-match lineup via
	// PUT .../match-lineups/:matchId).
	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions", map[string]any{
		"id":       pt.TeamCompID,
		"name":     "Team Fixed",
		"kind":     "team",
		"teamSize": 3,
		"format":   state.CompFormatLeague,
		"courts":   []string{"A", "B"},
		"players": []map[string]any{
			{"name": "Aoi Team", "dojo": "Aoi Dojo"},
			{"name": "Midori Team", "dojo": "Midori Dojo"},
			{"name": "Akane Team", "dojo": "Akane Dojo"},
			{"name": "Shiro Team", "dojo": "Shiro Dojo"},
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	teamPlayers, err := store.LoadParticipants(pt.TeamCompID, false)
	require.NoError(t, err)
	require.Len(t, teamPlayers, 4)
	for _, tp := range teamPlayers {
		pt.TeamIDs = append(pt.TeamIDs, tp.ID)
	}

	team0ID := pt.TeamIDs[0]
	var team0Members []domain.TeamMember
	for _, fighterName := range []string{"Aoi Senpo", "Aoi Chuken", "Aoi Taisho"} {
		w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.TeamCompID+"/teams/"+team0ID+"/members", map[string]any{
			"name": fighterName,
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var added domain.TeamMember
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
		team0Members = append(team0Members, added)
	}

	team1ID := pt.TeamIDs[1]
	var team1Members []domain.TeamMember
	for _, fighterName := range []string{"Midori Senpo", "Midori Chuken", "Midori Taisho"} {
		w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.TeamCompID+"/teams/"+team1ID+"/members", map[string]any{
			"name": fighterName,
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var added domain.TeamMember
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
		team1Members = append(team1Members, added)
	}

	// Save starting (round 0) lineups for team 0 and team 1.
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.TeamCompID+"/teams/"+team0ID+"/lineups/0", map[string]any{
		"positions": map[string]string{
			"1": team0Members[0].Name,
			"2": team0Members[1].Name,
			"3": team0Members[2].Name,
		},
		"memberIds": map[string]string{
			"1": team0Members[0].ID,
			"2": team0Members[1].ID,
			"3": team0Members[2].ID,
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.TeamCompID+"/teams/"+team1ID+"/lineups/0", map[string]any{
		"positions": map[string]string{
			"1": team1Members[0].Name,
			"2": team1Members[1].Name,
			"3": team1Members[2].Name,
		},
		"memberIds": map[string]string{
			"1": team1Members[0].ID,
			"2": team1Members[1].ID,
			"3": team1Members[2].ID,
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.TeamCompID+"/start", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	teamMatches, err := store.LoadPoolMatches(pt.TeamCompID)
	require.NoError(t, err)
	require.NotEmpty(t, teamMatches)
	for _, tm := range teamMatches {
		if tm.SideAID == team0ID || tm.SideBID == team0ID {
			pt.TeamMatchID = tm.ID
			break
		}
	}
	require.NotEmpty(t, pt.TeamMatchID)

	// Save a per-match lineup for team 0 on its first pool match.
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.TeamCompID+"/teams/"+team0ID+"/match-lineups/"+url.PathEscape(pt.TeamMatchID), map[string]any{
		"positions": map[string]string{
			"1": team0Members[2].Name,
			"2": team0Members[1].Name,
			"3": team0Members[0].Name,
		},
		"memberIds": map[string]string{
			"1": team0Members[2].ID,
			"2": team0Members[1].ID,
			"3": team0Members[0].ID,
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 4 (kachinuki team competition): reuse seedKachinukiRosterComp to seed
	// competition "k1" with TeamMatchType kachinuki, team members, and lineup,
	// then add the second team's members and lineup through the API and revert
	// its seeded running match back to the queue so no court stays busy.
	seedKachinukiRosterComp(t, store)
	kPlayers, err := store.LoadParticipants(pt.KachinukiCompID, false)
	require.NoError(t, err)
	require.Len(t, kPlayers, 2)
	for _, kp := range kPlayers {
		pt.KachinukiTeamIDs = append(pt.KachinukiTeamIDs, kp.ID)
	}
	toraID := pt.KachinukiTeamIDs[1]
	var toraMembers []domain.TeamMember
	for _, fighterName := range []string{"B1", "B2"} {
		w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.KachinukiCompID+"/teams/"+toraID+"/members", map[string]any{
			"name": fighterName,
		})
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		var added domain.TeamMember
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
		toraMembers = append(toraMembers, added)
	}
	w = populatedServeJSON(t, router, password, http.MethodPut, "/api/competitions/"+pt.KachinukiCompID+"/teams/"+toraID+"/lineups/0", map[string]any{
		"positions": map[string]string{
			"1": toraMembers[0].Name,
			"2": toraMembers[1].Name,
		},
		"memberIds": map[string]string{
			"1": toraMembers[0].ID,
			"2": toraMembers[1].ID,
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions/"+pt.KachinukiCompID+"/matches/"+url.PathEscape("Pool A-0")+"/revert-to-queue", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Kind 9 (branding logo and sponsor image): upload through the branding
	// and sponsors routes reusing buildBrandingUpload, buildSponsorUpload,
	// postSponsor, and tinyPNG.
	logoBody, logoCT := buildBrandingUpload(t, "logo.png", tinyPNG)
	logoReq := httptest.NewRequest(http.MethodPost, "/api/branding/logo", logoBody)
	logoReq.Header.Set("Content-Type", logoCT)
	logoReq.Header.Set("X-Tournament-Password", password)
	logoW := httptest.NewRecorder()
	router.ServeHTTP(logoW, logoReq)
	require.Equal(t, http.StatusOK, logoW.Code, logoW.Body.String())

	sponsorBody, sponsorCT := buildSponsorUpload(t, "Kendo Gear Co", "https://example.com", "sponsor.png", tinyPNG)
	sponsorW := postSponsor(t, router, sponsorBody, sponsorCT, password)
	require.Equal(t, http.StatusCreated, sponsorW.Code, sponsorW.Body.String())

	// Kind 10 (self-run competition vs officiated competition):
	// Mode ("officiated" vs "self-run") is a tournament-level field on
	// state.Tournament rather than state.Competition, and PUT /api/tournament
	// enforces errModeImmutable after creation. To exercise the self-run HTTP
	// flow on a dedicated competition and distinguish it on disk from the
	// officiated competitions (which have checkInEnabled=true and matches with
	// resultSource="admin"), we create SelfRunCompID with checkInEnabled=false,
	// temporarily flip Tournament.Mode to self-run through the store, start and
	// score a match anonymously via serveJSON (no X-Tournament-Password header)
	// so the match is persisted with resultSource="self-reported", and then
	// restore Tournament.Mode to officiated through the store.
	w = populatedServeJSON(t, router, password, http.MethodPost, "/api/competitions", map[string]any{
		"id":             pt.SelfRunCompID,
		"name":           "Individual Self Run",
		"kind":           "individual",
		"format":         state.CompFormatKnockout,
		"courts":         []string{"A", "B"},
		"checkInEnabled": false,
		"players": []map[string]any{
			{"name": "Mori Jin", "dojo": "Chiba Dojo"},
			{"name": "Ikeda Ren", "dojo": "Saitama Dojo"},
			{"name": "Hashimoto Kai", "dojo": "Kanagawa Dojo"},
			{"name": "Yamashita Ryu", "dojo": "Shizuoka Dojo"},
		},
	})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	require.NotNil(t, tourn)
	tourn.Mode = state.TournamentModeSelfRun
	require.NoError(t, store.SaveTournament(tourn))

	// Anonymous start and score in self-run mode (no X-Tournament-Password).
	w = serveJSON(router, http.MethodPost, "/api/competitions/"+pt.SelfRunCompID+"/start", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	srBracket, err := store.LoadBracket(pt.SelfRunCompID)
	require.NoError(t, err)
	require.NotNil(t, srBracket)
	require.NotEmpty(t, srBracket.Rounds)
	require.NotEmpty(t, srBracket.Rounds[0])
	srMatch := srBracket.Rounds[0][0]
	pt.SelfRunMatchID = srMatch.ID

	w = serveJSON(router, http.MethodPut, "/api/competitions/"+pt.SelfRunCompID+"/matches/"+url.PathEscape(srMatch.ID)+"/score", map[string]any{
		"id":         srMatch.ID,
		"sideA":      srMatch.SideA,
		"sideB":      srMatch.SideB,
		"sideAId":    srMatch.SideAID,
		"sideBId":    srMatch.SideBID,
		"winner":     srMatch.SideA,
		"winnerId":   srMatch.SideAID,
		"ipponsA":    []string{"M", "D"},
		"ipponsB":    []string{},
		"status":     string(state.MatchStatusCompleted),
		"modifiedAt": nowMs - 5000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	// Restore Tournament.Mode to officiated so subsequent authenticated admin
	// calls and round-trip tests see the standard officiated posture.
	tourn, err = store.LoadTournament()
	require.NoError(t, err)
	tourn.Mode = state.TournamentModeOfficiated
	require.NoError(t, store.SaveTournament(tourn))

	// Settle any lazy load-time migrations (such as assigning a numberPrefix to
	// seedKachinukiRosterComp's "k1" competition and EnsureLegacyUpgraded flags)
	// so on-disk files do not mutate on a subsequent GET.
	w = populatedServeJSON(t, router, password, http.MethodGet, "/api/competitions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(router, http.MethodGet, "/api/viewer/competitions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	return pt
}

func TestPopulatedFixture_EveryKindExists(t *testing.T) {
	pt := buildPopulatedTournament(t)

	// 1. Individual MIXED competition with generated draw and recorded pool result.
	mixedComp, err := pt.Store.LoadCompetition(pt.MixedCompID)
	require.NoError(t, err, "kind 1 (individual mixed): load competition")
	require.NotNil(t, mixedComp, "kind 1 (individual mixed): competition must exist")
	assert.Equal(t, "individual", mixedComp.Kind, "kind 1 (individual mixed): kind")
	assert.Equal(t, state.CompFormatMixed, mixedComp.Format, "kind 1 (individual mixed): format")
	pools, err := pt.Store.LoadPools(pt.MixedCompID)
	require.NoError(t, err, "kind 1 (individual mixed): load pools")
	assert.NotEmpty(t, pools, "kind 1 (individual mixed): pools.csv must contain pools")
	poolMatches, err := pt.Store.LoadPoolMatches(pt.MixedCompID)
	require.NoError(t, err, "kind 1 (individual mixed): load pool matches")
	assert.NotEmpty(t, poolMatches, "kind 1 (individual mixed): pool-matches.csv must contain matches")
	var completedPoolMatch bool
	for _, pm := range poolMatches {
		if pm.ID == pt.MixedScoredMatchID && pm.Status == state.MatchStatusCompleted {
			completedPoolMatch = true
			break
		}
	}
	assert.True(t, completedPoolMatch, "kind 1 (individual mixed): must have at least one completed pool match")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "pools.csv"))
	assert.NoError(t, err, "kind 1 (individual mixed): pools.csv must exist on disk")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "pool-matches.csv"))
	assert.NoError(t, err, "kind 1 (individual mixed): pool-matches.csv must exist on disk")

	// 2. Individual KNOCKOUT competition with generated bracket and recorded bracket result.
	koComp, err := pt.Store.LoadCompetition(pt.KnockoutCompID)
	require.NoError(t, err, "kind 2 (individual knockout): load competition")
	require.NotNil(t, koComp, "kind 2 (individual knockout): competition must exist")
	assert.Equal(t, "individual", koComp.Kind, "kind 2 (individual knockout): kind")
	assert.Equal(t, state.CompFormatKnockout, koComp.Format, "kind 2 (individual knockout): format")
	koBracket, err := pt.Store.LoadBracket(pt.KnockoutCompID)
	require.NoError(t, err, "kind 2 (individual knockout): load bracket")
	require.NotNil(t, koBracket, "kind 2 (individual knockout): bracket must exist")
	var completedBracketMatch bool
	for _, rnd := range koBracket.Rounds {
		for _, bm := range rnd {
			if bm.ID == pt.KnockoutMatchID && bm.Status == state.MatchStatusCompleted && bm.Winner != "" {
				completedBracketMatch = true
			}
		}
	}
	assert.True(t, completedBracketMatch, "kind 2 (individual knockout): bracket.json must have a completed match")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.KnockoutCompID, "bracket.json"))
	assert.NoError(t, err, "kind 2 (individual knockout): bracket.json must exist on disk")

	// 3. TEAM competition with team members, round lineup, and per-match lineup.
	teamComp, err := pt.Store.LoadCompetition(pt.TeamCompID)
	require.NoError(t, err, "kind 3 (team competition): load competition")
	require.NotNil(t, teamComp, "kind 3 (team competition): competition must exist")
	assert.Equal(t, "team", teamComp.Kind, "kind 3 (team competition): kind")
	assert.Equal(t, 3, teamComp.TeamSize, "kind 3 (team competition): teamSize")
	w := populatedServeJSON(t, pt.Router, pt.Password, http.MethodGet, "/api/competitions/"+pt.TeamCompID+"/team-members", nil)
	require.Equal(t, http.StatusOK, w.Code, "kind 3 (team competition): GET team-members")
	var teamMembersBody struct {
		TeamMembers map[string][]domain.TeamMember `json:"teamMembers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &teamMembersBody), "kind 3 (team competition): parse team-members response")
	teamMembersByTeam := teamMembersBody.TeamMembers
	assert.NotEmpty(t, teamMembersByTeam[pt.TeamIDs[0]], "kind 3 (team competition): team 0 must have team members")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.TeamCompID, "team-members.yaml"))
	assert.NoError(t, err, "kind 3 (team competition): team-members.yaml must exist on disk")

	teamLineups, err := pt.Store.LoadTeamLineups(pt.TeamCompID)
	require.NoError(t, err, "kind 3 (team competition): load lineups")
	var hasRoundLineup, hasMatchLineup bool
	for _, lu := range teamLineups {
		if lu.TeamID == pt.TeamIDs[0] && lu.MatchID == "" && lu.Round == 0 {
			hasRoundLineup = true
		}
		if lu.TeamID == pt.TeamIDs[0] && lu.MatchID == pt.TeamMatchID {
			hasMatchLineup = true
		}
	}
	assert.True(t, hasRoundLineup, "kind 3 (team competition): must have a saved round 0 lineup")
	assert.True(t, hasMatchLineup, "kind 3 (team competition): must have a saved per-match lineup")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.TeamCompID, "lineups.yaml"))
	assert.NoError(t, err, "kind 3 (team competition): lineups.yaml must exist on disk")

	// 4. KACHINUKI team competition with team members and a lineup.
	kComp, err := pt.Store.LoadCompetition(pt.KachinukiCompID)
	require.NoError(t, err, "kind 4 (kachinuki competition): load competition")
	require.NotNil(t, kComp, "kind 4 (kachinuki competition): competition must exist")
	assert.Equal(t, "team", kComp.Kind, "kind 4 (kachinuki competition): kind")
	assert.Equal(t, state.TeamMatchTypeKachinuki, kComp.TeamMatchType, "kind 4 (kachinuki competition): teamMatchType")
	w = populatedServeJSON(t, pt.Router, pt.Password, http.MethodGet, "/api/competitions/"+pt.KachinukiCompID+"/team-members", nil)
	require.Equal(t, http.StatusOK, w.Code, "kind 4 (kachinuki competition): GET team-members")
	var kMembersBody struct {
		TeamMembers map[string][]domain.TeamMember `json:"teamMembers"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &kMembersBody), "kind 4 (kachinuki competition): parse team-members response")
	kMembersByTeam := kMembersBody.TeamMembers
	assert.NotEmpty(t, kMembersByTeam[pt.KachinukiTeamIDs[0]], "kind 4 (kachinuki competition): team 0 must have team members")
	assert.NotEmpty(t, kMembersByTeam[pt.KachinukiTeamIDs[1]], "kind 4 (kachinuki competition): team 1 must have team members")
	kLineups, err := pt.Store.LoadTeamLineups(pt.KachinukiCompID)
	require.NoError(t, err, "kind 4 (kachinuki competition): load lineups")
	assert.NotEmpty(t, kLineups, "kind 4 (kachinuki competition): must have saved lineups")

	// 5. History: at least one match has a history/<matchId>.jsonl file with rescore lines.
	historyEntries, err := pt.Store.LoadMatchHistory(pt.MixedCompID, pt.MixedScoredMatchID)
	require.NoError(t, err, "kind 5 (history): load match history")
	assert.GreaterOrEqual(t, len(historyEntries), 2, "kind 5 (history): rescored match must have at least 2 history entries")
	historyFiles, err := filepath.Glob(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "history", "*.jsonl"))
	require.NoError(t, err, "kind 5 (history): glob history/*.jsonl")
	assert.NotEmpty(t, historyFiles, "kind 5 (history): history/*.jsonl must exist on disk")

	// 6. Events: at least one competitions/<id>/events/<matchId>.jsonl score-event log.
	scoreLog, err := pt.Store.LoadScoreLog(pt.MixedCompID, pt.MixedEventsMatchID)
	require.NoError(t, err, "kind 6 (events): load score event log")
	var hasTapEvent bool
	for _, line := range scoreLog {
		if line.Event != nil && line.Event.ID == "fixture-ev-1" {
			hasTapEvent = true
			break
		}
	}
	assert.True(t, hasTapEvent, "kind 6 (events): score log must contain the POSTed tap event")
	eventFiles, err := filepath.Glob(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "events", "*.jsonl"))
	require.NoError(t, err, "kind 6 (events): glob events/*.jsonl")
	assert.NotEmpty(t, eventFiles, "kind 6 (events): events/*.jsonl must exist on disk")

	// 7. Competitor statuses: at least one withdrawn or barred competitor recorded.
	statuses, err := pt.Store.LoadCompetitorStatus(pt.MixedCompID)
	require.NoError(t, err, "kind 7 (competitor statuses): load competitor status")
	st, ok := statuses[pt.IneligiblePlayerID]
	require.True(t, ok, "kind 7 (competitor statuses): withdrawn competitor status must exist")
	assert.False(t, st.Eligible, "kind 7 (competitor statuses): competitor must be marked ineligible")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "competitor-status.yaml"))
	assert.NoError(t, err, "kind 7 (competitor statuses): competitor-status.yaml must exist on disk")

	// 8. Override: a pool rank override saved in overrides.json.
	overrides, err := pt.Store.LoadOverrides(pt.MixedCompID)
	require.NoError(t, err, "kind 8 (override): load overrides")
	require.NotNil(t, overrides, "kind 8 (override): overrides must not be nil")
	assert.NotEmpty(t, overrides.PoolRanks[pt.OverriddenPoolName], "kind 8 (override): pool rank override must exist for pool")
	_, err = os.Stat(filepath.Join(pt.Dir, "competitions", pt.MixedCompID, "overrides.json"))
	assert.NoError(t, err, "kind 8 (override): overrides.json must exist on disk")

	// 9. Branding logo and sponsor image: branding/ and sponsors/ are non-empty.
	brandingEntries, err := os.ReadDir(filepath.Join(pt.Dir, "branding"))
	require.NoError(t, err, "kind 9 (branding and sponsors): read branding dir")
	assert.NotEmpty(t, brandingEntries, "kind 9 (branding and sponsors): branding/ directory must be non-empty")
	sponsorEntries, err := os.ReadDir(filepath.Join(pt.Dir, "sponsors"))
	require.NoError(t, err, "kind 9 (branding and sponsors): read sponsors dir")
	assert.NotEmpty(t, sponsorEntries, "kind 9 (branding and sponsors): sponsors/ directory must be non-empty")
	tourn, err := pt.Store.LoadTournament()
	require.NoError(t, err, "kind 9 (branding and sponsors): load tournament")
	require.NotNil(t, tourn.Theme, "kind 9 (branding and sponsors): tournament theme must be set")
	assert.NotEmpty(t, tourn.Theme.LogoPath, "kind 9 (branding and sponsors): tournament logo path must be set")
	assert.NotEmpty(t, tourn.Sponsors, "kind 9 (branding and sponsors): tournament sponsors list must be non-empty")

	// 10. SELF-RUN competition and OFFICIATED competition told apart by competition.
	srComp, err := pt.Store.LoadCompetition(pt.SelfRunCompID)
	require.NoError(t, err, "kind 10 (self-run vs officiated): load self-run competition")
	require.NotNil(t, srComp, "kind 10 (self-run vs officiated): self-run competition must exist")
	assert.NotEqual(t, pt.MixedCompID, pt.SelfRunCompID, "kind 10 (self-run vs officiated): self-run and officiated competitions must be distinct")
	assert.True(t, mixedComp.CheckInEnabled, "kind 10 (self-run vs officiated): officiated competition has checkInEnabled=true")
	assert.False(t, srComp.CheckInEnabled, "kind 10 (self-run vs officiated): self-run competition has checkInEnabled=false")

	var officiatedResultSource string
	for _, pm := range poolMatches {
		if pm.ID == pt.MixedScoredMatchID {
			officiatedResultSource = pm.ResultSource
			break
		}
	}
	assert.Equal(t, "admin", officiatedResultSource, "kind 10 (self-run vs officiated): officiated competition match must have resultSource=admin")

	srBracket, err := pt.Store.LoadBracket(pt.SelfRunCompID)
	require.NoError(t, err, "kind 10 (self-run vs officiated): load self-run bracket")
	var selfRunResultSource string
	for _, rnd := range srBracket.Rounds {
		for _, bm := range rnd {
			if bm.ID == pt.SelfRunMatchID {
				selfRunResultSource = bm.ResultSource
			}
		}
	}
	assert.Equal(t, "self-reported", selfRunResultSource, "kind 10 (self-run vs officiated): self-run competition match must have resultSource=self-reported")
	assert.False(t, strings.EqualFold(pt.MixedCompID, pt.SelfRunCompID), "kind 10 (self-run vs officiated): distinct competition IDs")
}

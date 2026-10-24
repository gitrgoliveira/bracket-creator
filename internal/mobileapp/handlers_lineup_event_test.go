package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// payloadBroadcaster records what each broadcast carried, which
// recordingBroadcaster leaves out: the data of a lineup event is what a
// listener decides whether to refetch by.
type payloadBroadcaster struct {
	events []broadcastEvent
}

type broadcastEvent struct {
	typ  EventType
	data any
}

func (b *payloadBroadcaster) Broadcast(t EventType, data any) {
	b.events = append(b.events, broadcastEvent{typ: t, data: data})
}

// last is the data of the most recent lineup event, failing when none was sent.
func (b *payloadBroadcaster) last(t *testing.T) any {
	t.Helper()
	require.NotEmpty(t, b.events, "no event was broadcast")
	ev := b.events[len(b.events)-1]
	require.Equal(t, EventLineupUpdated, ev.typ)
	return ev.data
}

// TestLineupUpdatedEvent_NamesTheTeamAndTheMatch: every writer of the lineup
// event says whose lineups changed, so a listener that only reads one team's
// lineup need not refetch for another's. The competition stays in the payload,
// which is what a listener filters by first. A match's lineup names the match as
// well; the team's starting lineup does not, and nor does a member rename or
// clear, which can reach the team's lineups at several matches.
func TestLineupUpdatedEvent_NamesTheTeamAndTheMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "lineup-event-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}}))
	teams, err := store.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, teams, 1)
	teamID := teams[0].ID
	sato, err := store.AddTeamMember("c1", teamID, "Sato")
	require.NoError(t, err)

	hub := &payloadBroadcaster{}
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLineupHandlers(admin, store, store, store, hub, store, NewFileVerifier(store))
	RegisterSquadHandlers(admin, store, store, hub, store, NewFileVerifier(store))

	send := func(method, path string, body any) {
		t.Helper()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, squadJSONReq(method, path, "secret", body))
		require.Contains(t, []int{http.StatusOK, http.StatusNoContent}, w.Code, "%s %s: %s", method, path, w.Body.String())
	}
	lineup := LineupRequest{Positions: map[domain.Position]string{domain.PositionNumbered(1): "Sato"}}
	team := "/api/competitions/c1/teams/" + teamID

	send(http.MethodPut, team+"/lineups/0", lineup)
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID}, hub.last(t), "PUT of the starting lineup")

	send(http.MethodDelete, team+"/lineups/0", nil)
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID}, hub.last(t), "DELETE of the starting lineup")

	send(http.MethodPut, team+"/match-lineups/Pool%20A-0", lineup)
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID, "matchId": "Pool A-0"}, hub.last(t), "PUT of a match's lineup")

	send(http.MethodDelete, team+"/match-lineups/Pool%20A-0", nil)
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID, "matchId": "Pool A-0"}, hub.last(t), "DELETE of a match's lineup")

	send(http.MethodPut, team+"/members/"+sato.ID, map[string]any{"name": "Sato Kenji"})
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID}, hub.last(t), "a member renamed")

	send(http.MethodDelete, team+"/members/"+sato.ID, nil)
	assert.Equal(t, gin.H{"competitionId": "c1", "teamId": teamID}, hub.last(t), "a member's name cleared")
}

// Operator decision K1 (c): a kachinuki encounter's bout 1 is whatever the
// lineup says now, so a lineup write renames bout 1 of the team's running
// match on every device: the server seats it (engine.ReseatKachinukiOpeners)
// and announces the match, read fresh, after the lineup event. Its points stay
// on bout 1.
func TestLineupWrite_RenamesKachinukiBoutOneAndAnnouncesTheMatch(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "secret"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "k1", Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki}))
	require.NoError(t, store.SaveParticipants("k1", []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}, {Name: "Ryu", Dojo: "Ryu Dojo"}}))
	teams, err := store.LoadParticipants("k1", false)
	require.NoError(t, err)
	require.Len(t, teams, 2)
	a, b := teams[0], teams[1]
	require.NoError(t, store.SavePoolMatches("k1", []state.MatchResult{{
		ID: "Pool A-0", SideA: a.Name, SideAID: a.ID, SideB: b.Name, SideBID: b.ID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "Sato", SideB: "Ito", IpponsA: []string{"M"}, IpponsB: []string{}, Winner: "Sato"}},
	}}))
	eng := engine.New(store)
	_, err = eng.RecordScoreEvents("k1", "Pool A-0", engine.ScoreEventsRequest{Session: "s", Events: []domain.ScoreEvent{
		{ID: "e1", Session: "s", Seq: 1, Stamp: time.Now().UnixMilli() - 1000, Kind: domain.ScoreIpponAdd, Pos: 1, Side: domain.ScoreSideA, Letter: "K"},
	}})
	require.NoError(t, err)

	hub := &payloadBroadcaster{}
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLineupHandlers(admin, store, store, store, hub, store, NewFileVerifier(store), eng)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, squadJSONReq(http.MethodPut, "/api/competitions/k1/teams/"+a.ID+"/match-lineups/Pool%20A-0",
		"secret", LineupRequest{Positions: map[domain.Position]string{domain.PositionNumbered(1): "Ota"}}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	require.Len(t, hub.events, 2, "the lineup event, then the match it renamed")
	assert.Equal(t, EventLineupUpdated, hub.events[0].typ)
	assert.Equal(t, EventMatchUpdated, hub.events[1].typ)
	pushed, ok := hub.events[1].data.(gin.H)
	require.True(t, ok)
	assert.Equal(t, "Pool A-0", pushed["matchId"])
	assert.NotNil(t, pushed["result"], "the push carries the match as stored now")

	stored, err := store.LoadPoolMatches("k1")
	require.NoError(t, err)
	row := stored[0].SubResults[0]
	assert.Equal(t, "Ota", row.SideA, "bout 1 is the lineup's fighter now")
	assert.Equal(t, []string{"M", "K"}, row.IpponsA, "its points stay on bout 1")
	assert.Equal(t, "Ito", row.SideB, "the other team's fighter is left as it was")
}

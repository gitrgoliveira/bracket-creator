package mobileapp

// The event stream's contract with every page: an event is sent only after
// the write it announces has committed, so a page that reads again on the
// event sees the write; and a write a page shows is followed by an event, so
// a page that skips the events its copy already covers is never left stale.
// These tests pin both directions for the doors the broadcast audit named.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// readAtBroadcast is a Broadcaster that, at the moment a handler sends the
// event `on`, reads the store the way a page's refetch would and records
// whether that read already holds the write. The read runs on its own
// goroutine with a deadline, so a broadcast made while the write still holds
// the competition's lock reads as "not visible" instead of hanging the test.
type readAtBroadcast struct {
	on   EventType
	read func() (bool, error)

	mu     sync.Mutex
	events []EventType
	seen   []bool
}

func (b *readAtBroadcast) Broadcast(t EventType, _ any) {
	b.mu.Lock()
	b.events = append(b.events, t)
	b.mu.Unlock()
	if t != b.on {
		return
	}
	done := make(chan bool, 1)
	go func() {
		ok, err := b.read()
		done <- err == nil && ok
	}()
	visible := false
	select {
	case visible = <-done:
	case <-time.After(2 * time.Second):
	}
	b.mu.Lock()
	b.seen = append(b.seen, visible)
	b.mu.Unlock()
}

func (b *readAtBroadcast) snapshot() ([]EventType, []bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.events), slices.Clone(b.seen)
}

func newCommitTestStore(t *testing.T) *state.Store {
	t.Helper()
	dir, err := os.MkdirTemp("", "broadcast-after-commit-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	return store
}

// TestSeedsPut_AnnouncesTheSavedSeedsAfterTheyCommit: PUT .../seeds wrote
// seeds.csv and announced nothing, so a page showing the seeding (merged onto
// each participant) kept the old one. It now sends participants_updated, and
// a read made at that moment already holds the new seeding.
func TestSeedsPut_AnnouncesTheSavedSeedsAfterTheyCommit(t *testing.T) {
	store := newCommitTestStore(t)
	const compID = "seeds-commit"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Seeds", Kind: "individual", Format: state.CompFormatKnockout,
		Courts: []string{"A"}, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo A"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo B"},
	}))

	b := &readAtBroadcast{on: EventParticipantsUpdated, read: func() (bool, error) {
		seeds, err := store.LoadSeedsRaw(compID)
		return len(seeds) == 1 && seeds[0].Name == "Alice", err
	}}
	r := gin.New()
	RegisterParticipantHandlers(r.Group("/api"), store, engine.New(store), b)

	body, _ := json.Marshal([]domain.SeedAssignment{{Name: "Alice", Dojo: "Dojo A", SeedRank: 1}})
	req := httptest.NewRequest(http.MethodPut, "/api/competitions/"+compID+"/seeds", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	events, seen := b.snapshot()
	assert.Equal(t, []EventType{EventParticipantsUpdated}, events, "the seeds save is announced once, as a roster change")
	assert.Equal(t, []bool{true}, seen, "a read made as the event is sent already holds the new seeding")
}

// TestDaihyosenAdd_BroadcastsAfterTheBoutCommits: the representative-bout add
// announces its match only once the bout is stored, so a page that refetches
// on that event finds the bout.
func TestDaihyosenAdd_BroadcastsAfterTheBoutCommits(t *testing.T) {
	store := newCommitTestStore(t)
	const compID = "dh-commit"
	seedTiedTeamKnockout(t, store, compID)

	b := &readAtBroadcast{on: EventMatchUpdated, read: func() (bool, error) {
		br, err := store.LoadBracket(compID)
		if err != nil || br == nil {
			return false, err
		}
		return state.DaihyosenSubIndex(br.Rounds[0][0].SubResults) >= 0, nil
	}}
	r := gin.New()
	RegisterDaihyosenHandlers(r.Group("/api"), engine.New(store), store, b, store, NewFileVerifier(store))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/B1/daihyosen", nil))
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	_, seen := b.snapshot()
	require.NotEmpty(t, seen, "the add announces its match")
	for i, ok := range seen {
		assert.Truef(t, ok, "match_updated #%d was sent before a read could see the added bout", i+1)
	}
}

func seedTiedTeamKnockout(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: "11111111-1111-4111-1111-111111111111", Name: "Alice", Dojo: "A"},
	}))
	// Empty SubResults: IV 0-0, PW 0-0, the tie a representative bout breaks.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "B1", SideA: "TeamA", SideB: "TeamB", Status: state.MatchStatusRunning}},
		},
	}))
}

// outcomeAutoEngine is the real engine with its after-write auto-complete
// pass answering a fixed outcome, so a door's handling of each outcome is
// pinned while every other call it makes stays production code.
type outcomeAutoEngine struct {
	*engine.Engine
	outcome engine.AutoCompleteOutcome
	err     error
}

func (e outcomeAutoEngine) MaybeAutoCompletePoolsAfterWrite(string, ...state.MatchResult) (engine.AutoCompleteOutcome, error) {
	return e.outcome, e.err
}

// autoCompleteAnnouncements is what every outcome of the auto-complete pass
// is announced with, the error arm included: a failed pass may have
// committed part of its work and cannot say which.
var autoCompleteAnnouncements = []struct {
	name    string
	outcome engine.AutoCompleteOutcome
	err     error
	want    []EventType
}{
	{"no change", engine.AutoCompleteNoChange, nil, nil},
	{"completed", engine.AutoCompleteTransitioned, nil, []EventType{EventCompetitionCompleted}},
	{"tie-break injected", engine.AutoCompleteTiebreakInjected, nil, []EventType{EventMatchUpdated, EventScheduleUpdated}},
	{"knockout started", engine.AutoCompleteKnockoutStarted, nil, []EventType{EventCompetitionStarted, EventScheduleUpdated}},
	{"started", engine.AutoCompleteStarted, nil, []EventType{EventCompetitionStarted, EventScheduleUpdated}},
	{"pools resolved", engine.AutoCompletePoolsResolved, nil, []EventType{EventMatchUpdated, EventScheduleUpdated}},
	{"league tie-break awaited", engine.AutoCompleteAwaitingLeagueTiebreak, nil, []EventType{EventMatchUpdated, EventScheduleUpdated}},
	{"the pass failed", engine.AutoCompleteNoChange, errors.New("disk refused"), []EventType{EventMatchUpdated, EventScheduleUpdated}},
}

// TestAnswerAutoComplete_AnnouncesEveryOutcome pins the one tail every door
// asks: each outcome that writes is followed by its events, and the error
// arm sets the sanitized header and still announces a refresh.
func TestAnswerAutoComplete_AnnouncesEveryOutcome(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range autoCompleteAnnouncements {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			hub := &recordingBroadcaster{}
			answerAutoComplete(c, hub, "comp", tc.outcome, tc.err)
			assert.Equal(t, tc.want, hub.events)
			if tc.err != nil {
				assert.Equal(t, AutoCompleteErrorValue, w.Header().Get(AutoCompleteErrorHeader))
			} else {
				assert.Empty(t, w.Header().Get(AutoCompleteErrorHeader))
			}
		})
	}
}

// TestDaihyosenAdd_AnnouncesEveryAutoCompleteOutcome: the add's own switch
// announced three of the outcomes and dropped the knockout start, the pools
// seated and the league waiting on its tie-breaker, each a commit with no
// event. It now answers through answerAutoComplete like the score doors.
func TestDaihyosenAdd_AnnouncesEveryAutoCompleteOutcome(t *testing.T) {
	for _, tc := range autoCompleteAnnouncements {
		t.Run(tc.name, func(t *testing.T) {
			store := newCommitTestStore(t)
			const compID = "dh-outcomes"
			seedTiedTeamKnockout(t, store, compID)

			hub := &recordingBroadcaster{}
			r := gin.New()
			eng := outcomeAutoEngine{Engine: engine.New(store), outcome: tc.outcome, err: tc.err}
			RegisterDaihyosenHandlers(r.Group("/api"), eng, store, hub, store, NewFileVerifier(store))

			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/B1/daihyosen", nil))
			require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

			require.NotEmpty(t, hub.events)
			assert.Equal(t, EventMatchUpdated, hub.events[0], "the added bout's own match is announced first")
			assert.Equal(t, len(tc.want), len(hub.events)-1, "then the auto-complete outcome, as the score doors announce it: %v", hub.events)
			if len(tc.want) > 0 {
				assert.Equal(t, tc.want, hub.events[1:], "then the auto-complete outcome, as the score doors announce it")
			}
		})
	}
}

// TestMaybeAutoCompletePools_EngiHealAloneIsReported: an engi league holding
// a tie-break row the tie rule does not hold, with regular bouts still to
// fight, is healed (the row removed and saved) and nothing else happens. The
// pass reported AutoCompleteNoChange, so no door announced the removed row.
// It now reports AutoCompletePoolsResolved, which the doors announce.
func TestMaybeAutoCompletePools_EngiHealAloneIsReported(t *testing.T) {
	store := newCommitTestStore(t)
	eng := engine.New(store)
	const compID = "engi-heal-alone"
	player := func(name string) helper.Player {
		return helper.Player{ID: "id-" + name, Name: name, Dojo: "Dojo " + name}
	}
	alice, bob, carol := player("Alice"), player("Bob"), player("Carol")
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Engi Heal", Kind: "individual", Format: state.CompFormatLeague,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 1, RoundRobin: true,
		Courts: []string{"A"}, StartTime: "09:00", Status: state.CompStatusPools, Engi: true,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{alice, bob, carol}},
	}))
	side := func(id string, a, b helper.Player) state.MatchResult {
		return state.MatchResult{
			ID: id, SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID,
			Status: state.MatchStatusScheduled, Court: "A",
		}
	}
	fought := side("Pool A-0", alice, bob)
	fought.Status, fought.FlagsA, fought.FlagsB = state.MatchStatusCompleted, 3, 2
	fought.Winner, fought.WinnerID = alice.Name, alice.ID
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		fought,
		side("Pool A-1", alice, carol),
		side("Pool A-2", bob, carol),
		// Alice beat Bob: nothing ties them, so this row is an old engine's.
		side("Pool A-TB-0", alice, bob),
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, engine.AutoCompletePoolsResolved, outcome,
		"a heal that removed a row is a write, reported so the doors announce it")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		assert.Falsef(t, engine.IsTiebreakerMatchID(m.ID), "the orphaned tie-break row %s must be healed away", m.ID)
	}
}

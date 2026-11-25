package mobileapp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The event stream keeps a hidden draft's draw off every stream but the
// organiser's (bc-drpb): an event carrying draw content or the draw's shape
// is sent with no data, judged when it is sent, before the names option or a
// reveal link is asked.

// drawEvents broadcasts each event type that carries the juniors draw, and
// is the list the catalogue must flag (TestSSEEventCatalogue_DrawEvents).
var drawEvents = map[EventType]func(h *Hub){
	EventMatchUpdated: broadcastJuniorsResult,
	EventCompetitorStatusUpdated: func(h *Hub) {
		h.Broadcast(EventCompetitorStatusUpdated, gin.H{"competitionId": "juniors", "status": gin.H{
			"playerId": pnAlice, "status": "withdrawn", "matchId": "Pool A-0",
		}})
	},
	EventLineupUpdated: func(h *Hub) {
		h.Broadcast(EventLineupUpdated, lineupUpdatedPayload("juniors", "team-1", "Pool A-0"))
	},
	EventSwissRoundGenerated: func(h *Hub) {
		h.Broadcast(EventSwissRoundGenerated, gin.H{"competitionId": "juniors", "swissCurrentRound": 1, "matchCount": 2})
	},
}

func TestSSEEventCatalogue_DrawEvents(t *testing.T) {
	for et, spec := range sseEventCatalogue {
		_, want := drawEvents[et]
		assert.Equal(t, want, spec.draw, "event type %q", et)
	}
	visibility, ok := sseEventCatalogue[EventDrawVisibilityChanged]
	require.True(t, ok, "draw_visibility_changed is classified")
	assert.Equal(t, sseEventPasses, visibility.class, "it names a competition and whether its draw is hidden, nothing more")
	assert.False(t, visibility.draw, "the public pages must be told the draw was hidden or published")
}

// A hidden draft's draw events reach a public stream with no data whatever
// the names option, and reach a stream holding a competition link or a
// competitor's own link with no data too: a reveal key unlocks names, never
// a hidden draw. The organiser's stream is sent each as broadcast.
func TestHubProject_HiddenDraftWithholdsDrawEvents(t *testing.T) {
	for _, option := range []string{state.PublicNamesShown, state.PublicNamesHidden, state.PublicNamesCompetition, state.PublicNamesPerson} {
		t.Run("names "+option, func(t *testing.T) {
			f := newPNFixture(t, option)
			h := hubWithNames(f)
			setDraft(t, &f, true)
			streams := map[string]*sseSubscriber{
				"public":            {},
				"competition link":  {aud: audience{competitions: map[string]bool{"juniors": true}}},
				"competitor's link": {aud: audience{competitors: map[string]map[string]bool{"juniors": {pnAlice: true}}}},
			}
			for et, broadcast := range drawEvents {
				broadcast(h)
				e := lastEntry(t, h)
				require.Equal(t, et, e.typ)
				assert.Equal(t, "juniors", e.compID, "%s records the competition it names", et)
				for name, sub := range streams {
					assert.Equal(t, withoutData(e), h.project(e, sub), "%s on the %s stream", et, name)
				}
				assert.Equal(t, e.payload, h.project(e, &sseSubscriber{aud: organiserAudience()}), "%s on the organiser's stream", et)
			}
		})
	}
}

// A competitor status or a lineup that names no match (a withdrawal before the
// draw starts, a team's starting lineup, a team member added or renamed)
// carries nothing of the draw, so it reaches the public as broadcast while the
// draft is hidden, as the public reads of it still answer.
func TestHubProject_HiddenDraftPassesMatchlessStatusAndLineup(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	h := hubWithNames(f)
	setDraft(t, &f, true)
	matchless := map[string]func(){
		"status": func() {
			h.Broadcast(EventCompetitorStatusUpdated, gin.H{"competitionId": "juniors", "status": gin.H{
				"playerId": pnAlice, "status": "withdrawn",
			}})
		},
		"lineup": func() {
			h.Broadcast(EventLineupUpdated, lineupUpdatedPayload("juniors", "team-1", ""))
		},
	}
	for name, broadcast := range matchless {
		broadcast()
		e := lastEntry(t, h)
		assert.False(t, e.draws, name)
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "matchless %s passes", name)
	}
}

// Events that carry no draw still reach the public as broadcast while the
// draft is hidden, draw_visibility_changed among them (the public pages
// refetch on it), and are never judged on the competition.
func TestHubProject_HiddenDraftPassesTheRest(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	h := hubWithNames(f)
	setDraft(t, &f, true)
	for _, et := range []EventType{EventDrawVisibilityChanged, EventParticipantsUpdated, EventDrawGenerated, EventCompetitionStarted} {
		h.Broadcast(et, gin.H{"competitionId": "juniors", "hidden": true})
		e := lastEntry(t, h)
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "%s passes", et)
		assert.Empty(t, e.compID, "%s is judged on no competition", et)
		assert.Nil(t, e.masked, et)
		assert.Nil(t, e.selfRun, "%s needs no self-run judgement", et)
	}
	for et, broadcast := range drawEvents {
		broadcast(h)
		e := lastEntry(t, h)
		assert.NotNil(t, e.masked, "%s memoises its competition", et)
		assert.NotNil(t, e.selfRun, "%s carries the shared self-run judgement", et)
	}
}

// A published draft's draw events reach the public; hidden again, the same
// stored events are withheld when they are next sent (a replay), and sent
// again once it is published. Visibility is judged when an event is sent,
// never when it was broadcast.
func TestHubProject_DrawVisibilityJudgedWhenSent(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	h := hubWithNames(f)
	setDraft(t, &f, false)
	var entries []*historyEntry
	for et, broadcast := range drawEvents {
		broadcast(h)
		e := lastEntry(t, h)
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "%s of a published draft", et)
		entries = append(entries, e)
	}

	setDraft(t, &f, true)
	for _, e := range entries {
		assert.Equal(t, withoutData(e), h.project(e, &sseSubscriber{}), "%s once hidden again", e.typ)
	}

	setDraft(t, &f, false)
	for _, e := range entries {
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "%s published again", e.typ)
	}
}

// A self-run tournament shows its draw as soon as it is made, and a started
// competition is public whatever is stored: neither withholds a draw event.
func TestHubProject_DrawShownInSelfRunAndOnceStarted(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	h := hubWithNames(f)
	setDraft(t, &f, true)
	setTournamentMode(t, f, state.TournamentModeSelfRun)
	for et, broadcast := range drawEvents {
		broadcast(h)
		e := lastEntry(t, h)
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "%s in a self-run tournament", et)
	}

	setTournamentMode(t, f, state.TournamentModeOfficiated)
	comp, err := f.store.LoadCompetition(f.comp.ID)
	require.NoError(t, err)
	comp.Status = state.CompStatusPools
	require.True(t, comp.DrawHidden, "the stored flag is left as it was")
	require.NoError(t, f.store.SaveCompetition(comp))
	for et, broadcast := range drawEvents {
		broadcast(h)
		e := lastEntry(t, h)
		assert.Equal(t, e.payload, h.project(e, &sseSubscriber{}), "%s of a started competition", et)
	}
}

// A draw event that cannot be judged is sent with no data, never as
// broadcast: one naming no competition, one naming a competition that cannot
// be read.
func TestHubProject_DrawEventFailsClosed(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	h := hubWithNames(f)
	h.Broadcast(EventLineupUpdated, gin.H{"teamId": "team-1", "matchId": "Pool A-0"})
	e := lastEntry(t, h)
	assert.Equal(t, withoutData(e), h.project(e, &sseSubscriber{}))
	assert.Equal(t, e.payload, h.project(e, &sseSubscriber{aud: organiserAudience()}))

	h.Broadcast(EventSwissRoundGenerated, gin.H{"competitionId": "ghost", "matchCount": 2})
	e = lastEntry(t, h)
	assert.Equal(t, withoutData(e), h.project(e, &sseSubscriber{}))
}

// Through the stream itself: live, a public stream is sent a hidden draft's
// result with no data while the organiser's stream carries it; replayed after
// a publish, the same event reaches the public, and replayed after the draft
// is hidden again it is withheld again.
func TestHandleEvents_HiddenDraftLiveAndReplay(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesShown)
	setDraft(t, &f, true)
	r, h := ticketRouter(f)
	// A first event, so the result's replay asks from a Last-Event-ID above 0.
	h.Broadcast(EventTournamentUpdated, gin.H{})

	open := func(query string) (*mockResponseWriter, context.CancelFunc, chan struct{}) {
		ctx, cancel := context.WithCancel(context.Background())
		w := &mockResponseWriter{ResponseRecorder: httptest.NewRecorder(), closeChan: make(chan bool)}
		req := httptest.NewRequest(http.MethodGet, "/api/events"+query, nil).WithContext(ctx)
		done := make(chan struct{})
		go func() {
			r.ServeHTTP(w, req)
			close(done)
		}()
		return w, cancel, done
	}

	tk, _, err := h.issueTicket(0, sseSubscriber{aud: organiserAudience()})
	require.NoError(t, err)
	orgW, orgCancel, orgDone := open("?ticket=" + tk)
	pubW, pubCancel, pubDone := open("")
	require.Eventually(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		return len(h.clients) == 2
	}, 2*time.Second, 5*time.Millisecond)

	broadcastJuniorsResult(h)
	e := lastEntry(t, h)
	waitForFrame(t, orgW, "Alice Arai")
	waitForFrame(t, pubW, fmt.Sprintf(`{"type":"match_updated","data":null,"seq":%d}`, e.seq))
	orgCancel()
	pubCancel()
	<-orgDone
	<-pubDone
	assert.NotContains(t, pubW.BodyString(), "Alice Arai")

	replay := func() string {
		w, cancel, done := open(fmt.Sprintf("?lastEventId=%d", e.seq-1))
		waitForFrame(t, w, fmt.Sprintf(`"seq":%d`, e.seq))
		cancel()
		<-done
		return w.BodyString()
	}

	setDraft(t, &f, false)
	assert.Contains(t, replay(), "Alice Arai", "published, the replay carries the result")

	setDraft(t, &f, true)
	body := replay()
	assert.NotContains(t, body, "Alice Arai", "hidden again, the replay withholds it")
	assert.Contains(t, body, fmt.Sprintf(`{"type":"match_updated","data":null,"seq":%d}`, e.seq))
}

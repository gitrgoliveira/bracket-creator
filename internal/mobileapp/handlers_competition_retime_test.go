package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// retimeFixture draws a mixed competition on day 1 at 09:00 and returns its
// router, store, hub and the settled settings version.
func retimeFixture(t *testing.T, format string) (*gin.Engine, *state.Store, *Hub, string, string) {
	t.Helper()
	r, store, _, hub, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Retime Cup", Date: "10-06-2026", DurationDays: 2, Courts: []string{"A", "B"},
	}))
	_, _ = cvSeed(t, r, store, "rt", func(c *state.Competition) {
		c.Format = format
		if format == state.CompFormatLeague {
			c.PoolSize, c.PoolWinners, c.PoolSizeMode = 0, 0, ""
		}
		c.Date = "10-06-2026"
		c.StartTime = "09:00"
	})
	players := make([]domain.Player, 8)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("Fighter %02d", i+1), Dojo: fmt.Sprintf("Dojo %d", i%4)}
	}
	require.NoError(t, store.SaveParticipants("rt", players))
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/competitions/rt/generate-draw", nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "generate-draw: %s", w.Body.String())
	stored, err := store.LoadCompetition("rt")
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, stored.Status)
	return r, store, hub, state.ConfigVersion(stored), "rt"
}

func retimeTimes(t *testing.T, store *state.Store, cid string) map[string]string {
	t.Helper()
	out := map[string]string{}
	pool, err := store.LoadPoolMatches(cid)
	require.NoError(t, err)
	for _, m := range pool {
		out[m.ID] = m.ScheduledAt
	}
	bracket, err := store.LoadBracket(cid)
	require.NoError(t, err)
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for _, m := range round {
				out[m.ID] = m.ScheduledAt
			}
		}
	}
	return out
}

type retimeAnswer struct {
	StartTime     string                    `json:"startTime"`
	Date          string                    `json:"date"`
	ConfigVersion string                    `json:"configVersion"`
	PreviousTimes *[]engine.MatchTimeChange `json:"previousTimes"`
	NotFreshCount *int                      `json:"notFreshCount"`
	TimesChanged  *bool                     `json:"timesChanged"`
	Code          string                    `json:"code"`
	Error         string                    `json:"error"`
}

func retimePut(t *testing.T, r *gin.Engine, store *state.Store, cid, version string, edits map[string]any) (*httptest.ResponseRecorder, retimeAnswer) {
	t.Helper()
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	w := cvPut(t, r, cid, cvSettingsBody(t, stored, version, edits))
	var ans retimeAnswer
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ans))
	return w, ans
}

func TestPutRetime_MovingAGivenStartMovesTheMatchesAndBroadcasts(t *testing.T) {
	r, store, hub, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "11:00"})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	require.NotNil(t, ans.PreviousTimes)
	assert.NotEmpty(t, *ans.PreviousTimes)
	require.NotNil(t, ans.TimesChanged)
	assert.True(t, *ans.TimesChanged)
	require.NotNil(t, ans.NotFreshCount)
	assert.Zero(t, *ans.NotFreshCount)

	after := retimeTimes(t, store, cid)
	for _, ch := range *ans.PreviousTimes {
		assert.Equal(t, before[ch.MatchID], ch.Was)
		assert.Equal(t, after[ch.MatchID], ch.Now)
	}
	assert.NotEqual(t, before, after)

	seen := map[EventType]bool{}
	timeout := time.After(time.Second)
	for !seen[EventTournamentUpdated] || !seen[EventScheduleUpdated] {
		select {
		case msg := <-ch:
			seen[decodeHubEvent(t, msg).Type] = true
		case <-timeout:
			t.Fatalf("expected tournament_updated and schedule_updated, saw %v", seen)
		}
	}
}

func TestPutRetime_UndoPutsTheMatchesBackExactly(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)
	w, moved := retimePut(t, r, store, cid, version, map[string]any{"startTime": "11:00"})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	require.NotNil(t, moved.PreviousTimes)

	w, undone := retimePut(t, r, store, cid, moved.ConfigVersion, map[string]any{
		"startTime":         "09:00",
		"restoreMatchTimes": *moved.PreviousTimes,
	})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	assert.Equal(t, "09:00", undone.StartTime)
	assert.Equal(t, before, retimeTimes(t, store, cid))
	assert.Nil(t, undone.PreviousTimes, "a restore is not a re-time")
}

func TestPutRetime_UndoIsRefusedWhenAMatchChangedSince(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	w, moved := retimePut(t, r, store, cid, version, map[string]any{"startTime": "11:00"})
	require.Equal(t, http.StatusOK, w.Code)

	pool, err := store.LoadPoolMatches(cid)
	require.NoError(t, err)
	pool[0].ScheduledAt = "23:40"
	require.NoError(t, store.SavePoolMatches(cid, pool))
	afterEdit := retimeTimes(t, store, cid)

	w, undone := retimePut(t, r, store, cid, moved.ConfigVersion, map[string]any{
		"startTime":         "09:00",
		"restoreMatchTimes": *moved.PreviousTimes,
	})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "undo_stale", undone.Code)
	assert.Equal(t, "Changed since the move; Undo not applied.", undone.Error)
	assert.Equal(t, afterEdit, retimeTimes(t, store, cid), "nothing is written")
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "11:00", stored.StartTime, "the settings are not written either")
}

func TestPutRetime_RestoreNeedsAVersionAndKnownMatches(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)

	w, _ := retimePut(t, r, store, cid, "", map[string]any{"restoreMatchTimes": []engine.MatchTimeChange{}})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w, _ = retimePut(t, r, store, cid, version, map[string]any{
		"restoreMatchTimes": []engine.MatchTimeChange{{MatchID: "no-such-match", Was: "09:00", Now: "10:00"}},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Equal(t, before, retimeTimes(t, store, cid))
}

func TestPutRetime_AnotherDayAtTheSameStartAnswersAnEmptyListAndUndoesWithOne(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)
	w, moved := retimePut(t, r, store, cid, version, map[string]any{"date": "11-06-2026"})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	require.NotNil(t, moved.PreviousTimes, "present even when no match time changed")
	assert.Empty(t, *moved.PreviousTimes)
	require.NotNil(t, moved.TimesChanged)
	assert.False(t, *moved.TimesChanged)

	w, undone := retimePut(t, r, store, cid, moved.ConfigVersion, map[string]any{
		"date":              "10-06-2026",
		"restoreMatchTimes": []engine.MatchTimeChange{},
	})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	assert.Equal(t, "10-06-2026", undone.Date)
	assert.Equal(t, before, retimeTimes(t, store, cid))
}

func TestPutRetime_AStaleVersionMovesNothing(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)
	w, _ := retimePut(t, r, store, cid, version, map[string]any{"startTime": "10:00"})
	require.Equal(t, http.StatusOK, w.Code)
	moved := retimeTimes(t, store, cid)

	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "12:00"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "config_changed", ans.Code)
	assert.Equal(t, moved, retimeTimes(t, store, cid))
	assert.NotEqual(t, before, moved)
}

func TestPutRetime_ALeagueThatCannotBeReadBackKeepsTheSettingsAndTheTimes(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatLeague)
	pool, err := store.LoadPoolMatches(cid)
	require.NoError(t, err)
	pool[1].Court = "Z"
	require.NoError(t, store.SavePoolMatches(cid, pool))
	before := retimeTimes(t, store, cid)

	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "11:00"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "retime_unavailable", ans.Code, w.Body.String())
	assert.Equal(t, before, retimeTimes(t, store, cid))
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "09:00", stored.StartTime, "the start is not saved when the matches cannot move")
}

func TestPutRetime_ASetupCompetitionJustSavesTheStart(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	_, version := cvSeed(t, r, store, "plain", nil)
	w, ans := retimePut(t, r, store, "plain", version, map[string]any{"startTime": "10:30"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "10:30", ans.StartTime)
	assert.Nil(t, ans.PreviousTimes)

	w, ans = retimePut(t, r, store, "plain", ans.ConfigVersion, map[string]any{
		"startTime":         "09:00",
		"restoreMatchTimes": []engine.MatchTimeChange{},
	})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "undo_stale", ans.Code)
}

// A league stores the pool size it drew with while the request's is zeroed on
// ingest; a drawn league's settings save was refused for that, so its start
// could never move.
func TestPutRetime_ADrawnLeagueMovesWithItsStartAndKeepsItsDrawnPoolSize(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatLeague)
	drawn, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "11:00"})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	require.NotNil(t, ans.PreviousTimes)
	assert.NotEmpty(t, *ans.PreviousTimes)
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, drawn.PoolSize, stored.PoolSize)
	assert.Equal(t, "11:00", stored.StartTime)
}

func calendarOnlyRouter(store *state.Store) *gin.Engine {
	r := gin.New()
	RegisterCalendarHandlers(r.Group("/api"), store, engine.New(store))
	return r
}

func TestCalendarPreview_ADrawnCompetitionCarriesTheRetimeDryRun(t *testing.T) {
	_, store, _, _, cid := retimeFixture(t, state.CompFormatMixed)
	r := calendarOnlyRouter(store)
	before := retimeTimes(t, store, cid)

	w := cvGet(t, r, "/api/schedule/calendar/preview?competitionId="+cid+"&date=10-06-2026&startTime=11:00&courts=A,B")
	var ans struct {
		Previewed bool `json:"previewed"`
		Retime    *struct {
			AnyChange     bool `json:"anyChange"`
			MatchCount    int  `json:"matchCount"`
			NotFreshCount int  `json:"notFreshCount"`
			Courts        []struct {
				Court    string `json:"court"`
				OldStart int    `json:"oldStart"`
				NewStart int    `json:"newStart"`
				OldEnd   int    `json:"oldEnd"`
				NewEnd   int    `json:"newEnd"`
			} `json:"courts"`
		} `json:"retime"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &ans))
	require.NotNil(t, ans.Retime, w.Body.String())
	assert.False(t, ans.Previewed, "a drawn competition's placement is where it sits now")
	assert.True(t, ans.Retime.AnyChange)
	assert.NotZero(t, ans.Retime.MatchCount)
	assert.Zero(t, ans.Retime.NotFreshCount)
	require.NotEmpty(t, ans.Retime.Courts)
	for _, c := range ans.Retime.Courts {
		assert.Equal(t, 120, c.NewStart-c.OldStart, "shiaijo %s", c.Court)
		assert.Equal(t, 120, c.NewEnd-c.OldEnd, "shiaijo %s", c.Court)
	}
	assert.Equal(t, before, retimeTimes(t, store, cid), "the dry run writes nothing")

	same := cvGet(t, r, "/api/schedule/calendar/preview?competitionId="+cid+"&date=10-06-2026&startTime=09:00&courts=A,B")
	require.NoError(t, json.Unmarshal(same.Body.Bytes(), &ans))
	require.NotNil(t, ans.Retime)
	assert.False(t, ans.Retime.AnyChange)
}

func TestCalendarPreview_ASetupCompetitionHasNoRetime(t *testing.T) {
	settings, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Date: "10-06-2026", DurationDays: 1, Courts: []string{"A", "B"}}))
	_, _ = cvSeed(t, settings, store, "plain", nil)
	r := calendarOnlyRouter(store)
	w := cvGet(t, r, "/api/schedule/calendar/preview?competitionId=plain&date=10-06-2026&startTime=10:00&courts=A")
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	_, has := raw["retime"]
	assert.False(t, has)
}

// A competition saved before twoThirdPlaces was stored resolves it from its
// format; the page sends the resolved value back on every settings save, and a
// drawn competition's save must not be refused for that.
func TestPutRetime_ADrawnCompetitionThatNeverStoredTwoThirdPlacesCanStillMove(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	stored.TwoThirdPlaces = nil
	require.NoError(t, store.SaveCompetition(stored))
	w, _ := retimePut(t, r, store, cid, "", map[string]any{"startTime": "11:00", "twoThirdPlaces": stored.EffectiveTwoThirdPlaces()})
	require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
	_ = version
}

// A match time is a bare "HH:MM": a start that carries the competition's own
// matches past midnight is refused with a code the client can branch on, and
// neither the settings nor the times are written.
func TestPutRetime_AStartThatRunsPastMidnightIs409WithACode(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)

	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "23:50"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "retime_past_midnight", ans.Code)
	assert.Contains(t, ans.Error, "past midnight")
	assert.Equal(t, before, retimeTimes(t, store, cid))
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "09:00", stored.StartTime, "the start is not saved when the matches cannot move")
}

// The time an Undo puts back is written as it comes, so it must be a clock.
func TestPutRetime_ARestoreWithAMalformedTimeIs400AndWritesNothing(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	before := retimeTimes(t, store, cid)
	var id string
	for matchID := range before {
		id = matchID
		break
	}

	w, ans := retimePut(t, r, store, cid, version, map[string]any{
		"restoreMatchTimes": []engine.MatchTimeChange{{MatchID: id, Was: "later", Now: before[id]}},
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, ans.Error, "not HH:MM")
	assert.Equal(t, before, retimeTimes(t, store, cid))
}

// Every refusal of a move names its cause in a `code`, on the preview as on
// the save: a start past midnight, and a shiaijo another competition holds
// until midnight or later.
func TestCalendarPreview_RefusalsOfAMoveCarryACode(t *testing.T) {
	_, store, _, _, cid := retimeFixture(t, state.CompFormatMixed)
	r := calendarOnlyRouter(store)

	w := getCalendarPreview(t, r, "competitionId="+cid+"&date=10-06-2026&startTime=23:50", "")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "retime_past_midnight", body["code"])

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "late", Name: "Late Open", Format: state.CompFormatLeague,
		Date: "10-06-2026", StartTime: "21:00", Courts: []string{"A"},
		PoolMatchDurationSeconds: 180, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("late", []state.MatchResult{
		{ID: "Pool A-0", Court: "A", ScheduledAt: "23:58", Status: state.MatchStatusScheduled},
	}))
	w = getCalendarPreview(t, r, "competitionId="+cid+"&date=10-06-2026&startTime=22:00", "")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	body = nil
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "court_past_midnight", body["code"])
	assert.Equal(t, "late", body["competitionId"])
	assert.NotEmpty(t, body["court"])
}

// A drawn competition keeps the shiaijo of its draw, so its preview asks for
// no `courts`; one not yet drawn still must name them.
func TestCalendarPreview_ADrawnCompetitionNeedsNoCourts(t *testing.T) {
	_, store, _, _, cid := retimeFixture(t, state.CompFormatMixed)
	r := calendarOnlyRouter(store)
	w := cvGet(t, r, "/api/schedule/calendar/preview?competitionId="+cid+"&date=10-06-2026&startTime=11:00")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// A stale save is answered config_changed, whatever a re-time of it would have
// met: the version is the save's first refusal, so the reader reloads instead
// of being told about a shiaijo past midnight it may no longer be moving onto.
func TestPutRetime_AStaleVersionIsConfigChangedBeforeACourtPastMidnight(t *testing.T) {
	r, store, _, version, cid := retimeFixture(t, state.CompFormatMixed)
	w, _ := retimePut(t, r, store, cid, version, map[string]any{"startTime": "10:00"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "late", Name: "Late Open", Format: state.CompFormatLeague,
		Date: "10-06-2026", StartTime: "21:00", Courts: []string{"A"},
		PoolMatchDurationSeconds: 180, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("late", []state.MatchResult{
		{ID: "Pool A-0", Court: "A", ScheduledAt: "23:58", Status: state.MatchStatusScheduled},
	}))

	w, ans := retimePut(t, r, store, cid, version, map[string]any{"startTime": "22:00"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "config_changed", ans.Code)
}

// putOmitting sends the stored settings with the given keys left out of the body.
func putOmitting(t *testing.T, r *gin.Engine, store *state.Store, cid string, keys ...string) *httptest.ResponseRecorder {
	t.Helper()
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(cvSettingsBody(t, stored, "", nil), &m))
	for _, k := range keys {
		delete(m, k)
	}
	b, err := json.Marshal(m)
	require.NoError(t, err)
	return cvPut(t, r, cid, b)
}

// A settings save that leaves twoThirdPlaces out keeps the stored value, as an
// omitted teamMatchType does: before the start it is not wiped, and after the
// start the save is not refused for a change it never asked for.
func TestPutSettings_AnOmittedTwoThirdPlacesKeepsTheStoredValue(t *testing.T) {
	off := false
	for _, status := range []state.CompetitionStatus{state.CompStatusSetup, state.CompStatusPools} {
		t.Run(string(status), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			_, _ = cvSeed(t, r, store, "ttp", func(c *state.Competition) {
				c.TwoThirdPlaces = &off // the format's own default is true
			})
			stored, err := store.LoadCompetition("ttp")
			require.NoError(t, err)
			stored.Status = status
			require.NoError(t, store.SaveCompetition(stored))

			w := putOmitting(t, r, store, "ttp", "twoThirdPlaces")
			require.Equalf(t, http.StatusOK, w.Code, "%s", w.Body.String())
			after, err := store.LoadCompetition("ttp")
			require.NoError(t, err)
			require.NotNil(t, after.TwoThirdPlaces)
			assert.False(t, *after.TwoThirdPlaces)
		})
	}
}

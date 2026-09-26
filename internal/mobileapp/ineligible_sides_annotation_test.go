// Package mobileapp, ineligible_sides_annotation_test.go pins bc-cse's
// read-only ineligibleSides annotation: which SCHEDULED matches carry a
// barred side, and how that feeds the queue-position derivation ("Next up"
// skips a match nobody can start).
package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAnyScheduledMatchHasBothSides(t *testing.T) {
	t.Run("no matches", func(t *testing.T) {
		assert.False(t, anyScheduledMatchHasBothSides(nil, nil))
	})
	t.Run("scheduled pool match with both ids", func(t *testing.T) {
		ms := []state.MatchResult{{Status: state.MatchStatusScheduled, SideAID: "a", SideBID: "b"}}
		assert.True(t, anyScheduledMatchHasBothSides(ms, nil))
	})
	t.Run("scheduled pool match missing one id", func(t *testing.T) {
		ms := []state.MatchResult{{Status: state.MatchStatusScheduled, SideAID: "a"}}
		assert.False(t, anyScheduledMatchHasBothSides(ms, nil))
	})
	t.Run("running match with both ids does not count", func(t *testing.T) {
		ms := []state.MatchResult{{Status: state.MatchStatusRunning, SideAID: "a", SideBID: "b"}}
		assert.False(t, anyScheduledMatchHasBothSides(ms, nil))
	})
	t.Run("bracket round match", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{{
			{Status: state.MatchStatusScheduled, SideAID: "a", SideBID: "b"},
		}}}
		assert.True(t, anyScheduledMatchHasBothSides(nil, b))
	})
	t.Run("bronze match", func(t *testing.T) {
		b := &state.Bracket{ThirdPlaceMatch: &state.BracketMatch{
			Status: state.MatchStatusScheduled, SideAID: "a", SideBID: "b",
		}}
		assert.True(t, anyScheduledMatchHasBothSides(nil, b))
	})
}

func TestAnnotateIneligibleSides(t *testing.T) {
	statuses := map[string]domain.CompetitorStatus{
		"alice": {PlayerID: "alice", Eligible: false, MatchID: "Pool A-0", Reason: "kiken-voluntary at Pool A-0"},
	}

	t.Run("no statuses: no-op, whatever the matches", func(t *testing.T) {
		ms := []state.MatchResult{{ID: "Pool A-1", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "carol"}}
		annotateEligibility(ms, nil, nil)
		assert.Nil(t, ms[0].IneligibleSides)
	})

	t.Run("scheduled match with a barred side A is stamped", func(t *testing.T) {
		ms := []state.MatchResult{{ID: "Pool A-1", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "carol"}}
		annotateEligibility(ms, nil, statuses)
		require.NotNil(t, ms[0].IneligibleSides)
		assert.Equal(t, "kiken-voluntary", ms[0].IneligibleSides.A)
		assert.Empty(t, ms[0].IneligibleSides.B)
	})

	t.Run("scheduled match with a barred side B is stamped", func(t *testing.T) {
		ms := []state.MatchResult{{ID: "Pool A-1", Status: state.MatchStatusScheduled, SideAID: "carol", SideBID: "alice"}}
		annotateEligibility(ms, nil, statuses)
		require.NotNil(t, ms[0].IneligibleSides)
		assert.Empty(t, ms[0].IneligibleSides.A)
		assert.Equal(t, "kiken-voluntary", ms[0].IneligibleSides.B)
	})

	t.Run("the undo path: a match barred by ITS OWN prior decision is not stamped", func(t *testing.T) {
		ms := []state.MatchResult{{ID: "Pool A-0", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "bob"}}
		annotateEligibility(ms, nil, statuses)
		assert.Nil(t, ms[0].IneligibleSides, "Pool A-0 recorded alice's own status; it must not bar itself")
	})

	t.Run("running/completed matches are never stamped", func(t *testing.T) {
		ms := []state.MatchResult{
			{ID: "Pool A-2", Status: state.MatchStatusRunning, SideAID: "alice", SideBID: "carol"},
			{ID: "Pool A-3", Status: state.MatchStatusCompleted, SideAID: "alice", SideBID: "carol"},
		}
		annotateEligibility(ms, nil, statuses)
		assert.Nil(t, ms[0].IneligibleSides)
		assert.Nil(t, ms[1].IneligibleSides)
	})

	t.Run("no barred side: not stamped", func(t *testing.T) {
		ms := []state.MatchResult{{ID: "Pool A-4", Status: state.MatchStatusScheduled, SideAID: "bob", SideBID: "carol"}}
		annotateEligibility(ms, nil, statuses)
		assert.Nil(t, ms[0].IneligibleSides)
	})

	t.Run("a bracket match is stamped the same way", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "carol"},
		}}}
		annotateEligibility(nil, b, statuses)
		require.NotNil(t, b.Rounds[0][0].IneligibleSides)
		assert.Equal(t, "kiken-voluntary", b.Rounds[0][0].IneligibleSides.A)
	})

	t.Run("the bronze match is stamped too", func(t *testing.T) {
		b := &state.Bracket{ThirdPlaceMatch: &state.BracketMatch{
			ID: "m-bronze", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "carol",
		}}
		annotateEligibility(nil, b, statuses)
		require.NotNil(t, b.ThirdPlaceMatch.IneligibleSides)
	})

	t.Run("bc-cse: a barred side whose reason has no parseable decision leaves the annotation nil", func(t *testing.T) {
		// A status set directly (e.g. via POST /competitor-status) carries
		// free text with no " at <matchID>" suffix, so SplitStatusReason's
		// ok is false. Before the fix, out.A was stamped "" anyway (the
		// struct was still allocated because a != nil), producing a non-nil
		// annotation with BOTH fields empty: Go's nil check reads that as
		// barred, but the JSON (A/B are `omitempty`) omits both keys, so a
		// client reading a/b directly reads it as NOT barred.
		freeTextStatuses := map[string]domain.CompetitorStatus{
			"dave": {PlayerID: "dave", Eligible: false, Reason: "disqualified for unsporting conduct"},
		}
		ms := []state.MatchResult{{ID: "Pool A-9", Status: state.MatchStatusScheduled, SideAID: "dave", SideBID: "carol"}}
		annotateEligibility(ms, nil, freeTextStatuses)
		assert.Nil(t, ms[0].IneligibleSides, "an unparseable barring reason must leave the annotation nil, not an empty {}")
	})

	t.Run("bc-cse: one side parses and the other does not, only the parsed side is stamped", func(t *testing.T) {
		mixedStatuses := map[string]domain.CompetitorStatus{
			"alice": {PlayerID: "alice", Eligible: false, MatchID: "Pool A-0", Reason: "kiken-voluntary at Pool A-0"},
			"dave":  {PlayerID: "dave", Eligible: false, Reason: "disqualified for unsporting conduct"},
		}
		ms := []state.MatchResult{{ID: "Pool A-9", Status: state.MatchStatusScheduled, SideAID: "alice", SideBID: "dave"}}
		annotateEligibility(ms, nil, mixedStatuses)
		require.NotNil(t, ms[0].IneligibleSides)
		assert.Equal(t, "kiken-voluntary", ms[0].IneligibleSides.A)
		assert.Empty(t, ms[0].IneligibleSides.B, "dave's reason does not parse, so his side is not stamped")
	})
}

// TestQueuePositions_SkipBarredScheduledMatch pins the "position 0" part of
// bc-cse: a stamped ineligibleSides match is not counted toward its court's
// queue, in both the pool (state.DeriveQueuePositions) and bracket
// (annotateBracketQueuePositions) derivations.
func TestQueuePositions_SkipBarredScheduledMatch(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		ms := []state.MatchResult{
			{ID: "Pool A-0", Court: "A", Status: state.MatchStatusScheduled, IneligibleSides: &state.IneligibleSidesAnnotation{A: "kiken-voluntary"}},
			{ID: "Pool A-1", Court: "A", Status: state.MatchStatusScheduled},
		}
		positions := state.DeriveQueuePositions(ms)
		assert.Equal(t, 0, positions[0], "the barred match is not queued")
		assert.Equal(t, 1, positions[1], "the next real match becomes position 1")
	})

	t.Run("bracket", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", Court: "A", Status: state.MatchStatusScheduled, IneligibleSides: &state.IneligibleSidesAnnotation{B: "fusenpai"}},
			{ID: "m-r1-1", Court: "A", Status: state.MatchStatusScheduled},
		}}}
		annotateBracketQueuePositions(b)
		assert.Equal(t, 0, b.Rounds[0][0].QueuePosition)
		assert.Equal(t, 1, b.Rounds[0][1].QueuePosition)
	})
}

// TestViewerCompetitionDetail_IneligibleSides_RemovedAfterReinstate is the
// end-to-end HTTP check: a competitor withdrawn via kiken-injury bars her
// next scheduled match on the viewer detail payload (with queuePosition 0),
// and reinstating her removes the annotation and restores the queue
// position, on the very next GET.
func TestViewerCompetitionDetail_IneligibleSides_RemovedAfterReinstate(t *testing.T) {
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "ineligible-sides-viewer"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Court: "A", Status: state.MatchStatusCompleted,
			Decision: "kiken-injury", DecisionBy: "aka", Winner: "Bob"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	// The kiken-injury status itself (RecordDecision's normal write path,
	// exercised directly here for a smaller fixture than a full /decision
	// HTTP round trip).
	_, _, err = eng.RecordDecision(compID, "Pool A-0", "kiken-injury", "aka", "test", nil, false)
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	viewer := r.Group("/api/viewer")
	RegisterViewerHandlers(viewer, store, eng)

	get := func() map[string]any {
		w := httptest.NewRecorder()
		req, rerr := http.NewRequest(http.MethodGet, "/api/viewer/competitions/"+compID, nil)
		require.NoError(t, rerr)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}
	findMatch := func(body map[string]any, id string) map[string]any {
		for _, raw := range body["poolMatches"].([]any) {
			m := raw.(map[string]any)
			if m["id"] == id {
				return m
			}
		}
		t.Fatalf("match %s not found in poolMatches", id)
		return nil
	}

	before := findMatch(get(), "Pool A-1")
	ineligibleSides, ok := before["ineligibleSides"].(map[string]any)
	require.True(t, ok, "Pool A-1 must carry ineligibleSides while Alice is barred, got %+v", before)
	assert.Equal(t, "kiken-injury", ineligibleSides["a"])
	// queuePosition is `omitempty`; a 0 (not queued) value is OMITTED from the
	// wire entirely, exactly as an ordinary non-barred running/completed
	// match's queuePosition already is.
	assert.Nil(t, before["queuePosition"], "a barred match is not queued")

	_, err = eng.ReinstateCompetitor(compID, aliceID)
	require.NoError(t, err)

	after := findMatch(get(), "Pool A-1")
	assert.Nil(t, after["ineligibleSides"], "the annotation must be gone once Alice is reinstated")
	assert.Equal(t, float64(1), after["queuePosition"], "the match rejoins the queue")
}

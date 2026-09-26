package mobileapp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// annotateEligibility stamps, on a COMPLETED match a withdrawal or default win
// decided, where the withdrawn side's competitor status stands now, so the
// editor's clear control can word its consequence without fetching the
// statuses itself.
func TestAnnotateEligibility_WithdrawnStatus(t *testing.T) {
	statuses := map[string]domain.CompetitorStatus{
		"alice": {PlayerID: "alice", Eligible: false, MatchID: "Pool A-0", Reason: "kiken-voluntary at Pool A-0"},
		"bob":   {PlayerID: "bob", Eligible: false, MatchID: "Pool A-3", Reinstateable: true},
		"carol": {PlayerID: "carol", Eligible: true, MatchID: "m-r1-0"},
	}
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideAID: "alice", SideBID: "dave", Status: state.MatchStatusCompleted, Decision: "kiken-voluntary", DecisionBy: "aka"},
		{ID: "Pool A-1", SideAID: "erin", SideBID: "bob", Status: state.MatchStatusCompleted, Decision: "fusensho", DecisionBy: "shiro"},
		{ID: "Pool A-2", SideAID: "alice", SideBID: "erin", Status: state.MatchStatusCompleted, WinnerID: "erin"},
		{ID: "Pool A-4", SideAID: "alice", SideBID: "frank", Status: state.MatchStatusScheduled},
	}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideAID: "carol", SideBID: "gina", Status: state.MatchStatusCompleted, Decision: "fusenpai", WinnerID: "gina"},
	}}}

	annotateEligibility(pool, bracket, statuses)

	require.NotNil(t, pool[0].WithdrawnStatus, "a kiken names the side that withdrew")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: false, MatchID: "Pool A-0"}, *pool[0].WithdrawnStatus)
	require.NotNil(t, pool[1].WithdrawnStatus, "a default win names the barred side")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: false, MatchID: "Pool A-3", Reinstateable: true}, *pool[1].WithdrawnStatus)
	assert.Nil(t, pool[2].WithdrawnStatus, "a fought match withdrew nobody")
	assert.Nil(t, pool[3].WithdrawnStatus, "a scheduled match is not decided")
	assert.NotNil(t, pool[3].IneligibleSides, "and keeps its own barred-side stamp")
	b := bracket.Rounds[0][0]
	require.NotNil(t, b.WithdrawnStatus, "with no decisionBy, the side that did not win")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: true, MatchID: "m-r1-0"}, *b.WithdrawnStatus)
}

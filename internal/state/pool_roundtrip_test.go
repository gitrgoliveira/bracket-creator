package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// notPersistedInPoolCSV lists the MatchResult fields a pool/league match does
// NOT keep on disk, each with the reason it is legitimately transient. Every
// other field must survive a save/reload, and TestPoolMatchRoundTripIsComplete
// fails on any field that is neither.
//
// This exists because the two match stores persist by different mechanisms and
// only one of them fails loudly. bracket.json is json.Marshal over the struct,
// so a new BracketMatch field is persisted the moment it is declared. A pool
// match is a hand-written column list in three places (the header, the row, and
// a rec[N] read per column), so a new MatchResult field is persisted only if
// someone remembers, and NOTHING breaks when they do not.
//
// That is not hypothetical: DecisionBy, DecisionReason, Encho and ModifiedAt
// were all lost on restart for pool matches while the bracket kept them. The
// withdrawal audit trail (who called the kiken, and why) survived until the
// next restart and no further; a match fought on in overtime came back with no
// record of it; and ModifiedAt's absence is why the timestamp last-write-wins
// guard could not run on the pool path at all, since it had no stored stamp to
// compare against.
//
// Adding a field to MatchResult now forces a decision here, in the open.
var notPersistedInPoolCSV = map[string]string{
	"Rev": "per-session write counter: orders writes WITHIN one client session " +
		"to discard an out-of-order reconnect flush. It is meaningless once the " +
		"result has landed, and meaningless to a different session.",
	"RevSession": "identifies the client session Rev counts within; same reason.",
	"QueuePosition": "derived on read from court and scheduled time " +
		"(DeriveQueuePositions), never authored, so persisting it would create a " +
		"second source of truth that could disagree with the schedule.",
	"WinnerSide": "derived: the winner name compared against SideA/SideB.",
}

// TestPoolMatchRoundTripIsComplete sweeps every exported MatchResult field,
// gives it a distinctive value, and checks it survives a save and a reload from
// a FRESH store (a reload through the same store would read the write-through
// cache and prove nothing).
func TestPoolMatchRoundTripIsComplete(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// The fully-populated fixture is shared with the golden-bytes test, which
	// documents the "every persisted field non-zero" contract both rely on:
	// one fixture to update when a column is appended, not two that must agree.
	in := poolMatchesGoldenInput()[0]
	require.NoError(t, s.SavePoolMatches("c", []MatchResult{in}))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	got := loaded[0]

	// sweepFields (file_roundtrip_guards_test.go) is the shared sweep every CSV
	// guard uses, and it also asserts the allow-list names only real fields, so
	// a rename that strands an entry fails here rather than silently narrowing
	// what this test covers.
	sweepFields(t, "pool-matches.csv", in, got, notPersistedInPoolCSV)
}

// Files written before the columns existed must still load. The parser reads
// each trailing column behind a length guard, so an older row simply leaves the
// newer fields at their zero values rather than failing to parse.
func TestPoolMatchesReadsPreColumnFiles(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// A 25-column row: the layout before DecisionBy/DecisionReason/Encho/ModifiedAt.
	legacy := "PoolName,MatchIdx,SideA,SideB,Winner,IpponsA,IpponsB,HansokuA,HansokuB," +
		"Decision,Status,Court,SubResults,ScheduledAt,ResultSource,Round,SideAID,SideBID," +
		"WinnerID,CorrectionReason,RepPlayerA,RepPlayerB,FlagsA,FlagsB,ReopenPending\n" +
		"Pool A,1,Kyoto,Osaka,Kyoto,M,K,0,0,,completed,B,,09:45,admin,3,id-a,id-b,id-a,,,,0,0,false\n"
	require.NoError(t, s.atomicWrite(s.compPath("c", "pool-matches.csv"), []byte(legacy), 0600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Kyoto", loaded[0].Winner, "the pre-existing columns still parse")
	assert.Empty(t, loaded[0].DecisionBy)
	assert.Nil(t, loaded[0].Encho)
	assert.Zero(t, loaded[0].ModifiedAt,
		"an unstamped legacy row must read as 0, which ApplyByTimestamp treats as always-applies")
}

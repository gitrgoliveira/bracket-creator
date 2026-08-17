package state

import (
	"reflect"
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

	// A match with every persisted field set to something distinguishable from
	// its zero value. Hantei rides in the ippons (encodeHanteiIntoIppons), so a
	// tied scoreline plus a named winner is the shape that carries it.
	in := MatchResult{
		ID: "Pool A-1", SideA: "Kyoto", SideB: "Osaka", Winner: "Kyoto",
		SideAID: "id-a", SideBID: "id-b", WinnerID: "id-a",
		IpponsA: []string{"M"}, IpponsB: []string{"K"},
		HansokuA: 1, HansokuB: 2,
		Decision: "fought", DecisionBy: "Referee Tanaka", DecisionReason: "call recorded",
		Status: MatchStatusCompleted, Court: "B", Round: 3,
		ScheduledAt: "09:45",
		SubResults: []SubMatchResult{{
			Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"D"}, Winner: "K1",
		}},
		Encho:            &EnchoMetadata{PeriodCount: 2},
		DecidedByHantei:  HanteiPtr(true),
		ResultSource:     "admin",
		CorrectionReason: "scoreboard misread",
		RepPlayerA:       "Rep A", RepPlayerB: "Rep B",
		FlagsA: 2, FlagsB: 1,
		ReopenPending: true,
		ModifiedAt:    1737000000000,
	}
	require.NoError(t, s.SavePoolMatches("c", []MatchResult{in}))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	got := loaded[0]

	inV, gotV := reflect.ValueOf(in), reflect.ValueOf(got)
	typ := inV.Type()
	for i := range typ.NumField() {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		if reason, skip := notPersistedInPoolCSV[f.Name]; skip {
			assert.NotEmptyf(t, reason, "%s needs a reason, not an empty string", f.Name)
			continue
		}
		want, have := inV.Field(i).Interface(), gotV.Field(i).Interface()
		// Pointer fields compare by value: the round trip rebuilds them.
		switch f.Name {
		case "Encho":
			require.NotNilf(t, got.Encho, "Encho was not persisted")
			assert.Equal(t, in.Encho.PeriodCount, got.Encho.PeriodCount)
			continue
		case "DecidedByHantei":
			require.NotNilf(t, got.DecidedByHantei, "the hantei verdict was not persisted")
			assert.True(t, *got.DecidedByHantei)
			continue
		}
		assert.Equalf(t, want, have,
			"MatchResult.%s did not survive a pool save/reload. Either persist it in "+
				"savePoolMatchesLocked + parsePoolMatchesRecords, or add it to "+
				"notPersistedInPoolCSV with the reason it is transient.", f.Name)
	}
}

// The allow-list must name real fields: a rename that leaves a stale entry
// behind would silently stop covering the field it was meant to exempt.
func TestNotPersistedListNamesRealFields(t *testing.T) {
	typ := reflect.TypeOf(MatchResult{})
	for name := range notPersistedInPoolCSV {
		_, ok := typ.FieldByName(name)
		assert.Truef(t, ok, "notPersistedInPoolCSV names %q, which is not a MatchResult field", name)
	}
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

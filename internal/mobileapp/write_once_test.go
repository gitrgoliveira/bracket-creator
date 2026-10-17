package mobileapp

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// bc-evts stage 3 review 11, Y8: a request the outbox gave up on at its
// deadline can still land after its resend and after the match's or team's
// next write. The doors the server orders by arrival (a court move, a time
// change, a lineup save, a member's rename or cleared name) take the write's
// writeId and answer one they have seen as recorded without applying it
// again, so a late duplicate never puts an older value back.

func (f teamWritesFixture) storedMatchField(t *testing.T, field string) string {
	t.Helper()
	matches, err := f.store.LoadPoolMatches("c1")
	require.NoError(t, err)
	for _, m := range matches {
		if m.ID == "PoolA-0" {
			if field == "court" {
				return m.Court
			}
			return m.ScheduledAt
		}
	}
	t.Fatal("PoolA-0 not found")
	return ""
}

func TestLateDuplicate_CourtMoveIsAnsweredAsRecorded(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	path := "/api/competitions/c1/matches/PoolA-0/court"
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "B", "writeId": "w-court-1"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "C", "writeId": "w-court-2"}).Code)
	w := f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "B", "writeId": "w-court-1"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "C", f.storedMatchField(t, "court"), "the late duplicate puts nothing back")
}

func TestLateDuplicate_TimeChangeIsAnsweredAsRecorded(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	path := "/api/competitions/c1/matches/PoolA-0/time"
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"scheduledAt": "10:00", "writeId": "w-time-1"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"scheduledAt": "11:00", "writeId": "w-time-2"}).Code)
	w := f.send(http.MethodPut, path, "main-pw", map[string]string{"scheduledAt": "10:00", "writeId": "w-time-1"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "11:00", f.storedMatchField(t, "time"), "the late duplicate puts nothing back")
}

func TestLateDuplicate_LineupSaveIsAnsweredAsRecorded(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	changed := []domain.Position{domain.PosSenpo}
	save := func(name, writeID string) LineupRequest {
		return LineupRequest{Positions: map[domain.Position]string{domain.PosSenpo: name}, Changed: &changed, WriteID: writeID}
	}
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", save("Ito", "w-lineup-1")).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", save("Kato", "w-lineup-2")).Code)
	w := f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", save("Ito", "w-lineup-1"))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Kato", "answered with the lineup as stored")
	l, ok := f.savedLineup(t, "PoolA-0")
	require.True(t, ok)
	assert.Equal(t, "Kato", l.Positions[domain.PosSenpo], "the late duplicate puts nothing back")
}

func TestLateDuplicate_MemberRenameIsAnsweredAsRecorded(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	path := f.membersPath() + "/" + f.namedA
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Ren", "writeId": "w-member-1"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Rei", "writeId": "w-member-2"}).Code)
	w := f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Ren", "writeId": "w-member-1"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Rei", "answered with the member as stored")
	assert.Equal(t, "Rei", f.memberName(t, f.namedA), "the late duplicate puts nothing back")
}

func TestLateDuplicate_MemberRenameAfterAClearedName(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	path := f.membersPath() + "/" + f.namedA
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Ren", "writeId": "w-member-1"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodDelete, path, "main-pw", map[string]string{"writeId": "w-member-2"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Ren", "writeId": "w-member-1"}).Code)
	assert.Equal(t, "", f.memberName(t, f.namedA), "the late rename does not undo the cleared name")
	// A late duplicate of the clear is answered as recorded too.
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"name": "Rio", "writeId": "w-member-3"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodDelete, path, "main-pw", map[string]string{"writeId": "w-member-2"}).Code)
	assert.Equal(t, "Rio", f.memberName(t, f.namedA), "the late clear does not undo the later name")
}

// A write with no writeId (an older client) is applied every time, as before.
func TestLateDuplicate_NoWriteIDAppliesAsBefore(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	path := "/api/competitions/c1/matches/PoolA-0/court"
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "B"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "C"}).Code)
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, path, "main-pw", map[string]string{"court": "B"}).Code)
	assert.Equal(t, "B", f.storedMatchField(t, "court"))
}

// A write the store refused is not recorded: its resend is judged again, and
// applies once what refused it has gone.
func TestLateDuplicate_ARefusedWriteIsJudgedAgain(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	ito, err := f.store.AddTeamMember("c1", f.teamA, "Ito")
	require.NoError(t, err)
	path := f.membersPath() + "/" + f.namedA
	body := map[string]string{"name": "Ito", "writeId": "w-member-9"}
	require.Equal(t, http.StatusConflict, f.send(http.MethodPut, path, "main-pw", body).Code, "another member has the name")
	_, err = f.store.RenameTeamMember("c1", f.teamA, ito.ID, "Ito Ren")
	require.NoError(t, err)
	w := f.send(http.MethodPut, path, "main-pw", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "Ito", f.memberName(t, f.namedA), "the resend applied")
}

// The record is bounded: an id is forgotten writeOnceKeep after it was
// applied, and past writeOnceMax ids the oldest goes first.
func TestWriteOnce_ForgetsByAgeAndBound(t *testing.T) {
	g := newWriteOnce()
	now := time.Unix(1000, 0)
	g.now = func() time.Time { return now }
	ran := 0
	apply := func() bool { ran++; return true }
	require.True(t, g.do("m", "w1", apply))
	require.False(t, g.do("m", "w1", apply), "a duplicate runs nothing")
	require.True(t, g.do("other", "w1", apply), "the same id at another target is another write")
	now = now.Add(writeOnceKeep)
	require.True(t, g.do("m", "w1", apply), "forgotten once its time is over")
	assert.Equal(t, 3, ran)

	for i := 0; i < writeOnceMax+5; i++ {
		now = now.Add(time.Millisecond)
		g.remember("bulk", fmt.Sprintf("w%d", i))
	}
	assert.LessOrEqual(t, len(g.seen), writeOnceMax)
	assert.False(t, g.seenAt("bulk", "w0"), "the oldest went first")
	assert.True(t, g.seenAt("bulk", fmt.Sprintf("w%d", writeOnceMax+4)))
	assert.False(t, g.seenAt("bulk", ""), "no id is never seen")
}

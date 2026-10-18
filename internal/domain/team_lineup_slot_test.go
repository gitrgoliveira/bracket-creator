package domain_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

// A removal ordered by its own stamp (stage 6 review 8, m1) splits the lineup
// it takes away: its changes made before the stamp kept as the removed
// record, those made at or after it staying, each change in one record only
// (m3), and a removal of nothing kept as a marker of its stamp.
func TestRemoveFromSlot_SplitsAtTheRemovalsStamp(t *testing.T) {
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	copyAt := func() domain.TeamLineup {
		return domain.TeamLineup{TeamID: "t", MatchID: "M", CopiedAt: 10,
			Positions: map[domain.Position]string{p1: "Kato", p2: "Ueda"},
			Stamps:    map[domain.Position]int64{p1: 30, p2: 5},
			Own:       []domain.Position{p1},
			History:   map[domain.Position][]domain.PositionChange{p1: {{Stamp: 10, Name: "Mori"}}}}
	}

	// Between its two own changes: Mori stays removed at 20, Kato is the
	// live lineup's own, made at 30; the carried position goes from the
	// record.
	out, removed, changed := domain.RemoveFromSlot([]domain.TeamLineup{copyAt()}, "t", "M", 0, 20)
	require.True(t, removed)
	require.True(t, changed)
	require.Len(t, out, 2)
	var rec, live domain.TeamLineup
	for _, l := range out {
		if l.IsRemoved() {
			rec = l
		} else {
			live = l
		}
	}
	assert.Equal(t, int64(20), rec.RemovedAt)
	assert.Equal(t, map[domain.Position]string{p1: "Mori"}, rec.Positions, "its own position alone")
	assert.Equal(t, int64(10), rec.CopiedAt)
	assert.Empty(t, rec.History)
	assert.Equal(t, "Kato", live.Positions[p1])
	assert.Equal(t, int64(30), live.CopiedAt)
	assert.Equal(t, []domain.Position{p1}, live.Own)
	assert.Empty(t, live.History, "Mori is the removed record's, never kept twice")

	// The same stamp again is that removal: nothing changes.
	again, removed, changed := domain.RemoveFromSlot(out, "t", "M", 0, 20)
	assert.False(t, removed)
	assert.False(t, changed)
	assert.Equal(t, out, again)

	// Before every change: nothing taken away, a marker kept; a change at
	// 15 then belongs to the epoch the marker at 20 closes.
	out, removed, _ = domain.RemoveFromSlot([]domain.TeamLineup{copyAt()}, "t", "M", 0, 5)
	assert.False(t, removed)
	require.Len(t, out, 2)
	idx := domain.SlotEpochAt(out, 3)
	require.GreaterOrEqual(t, idx, 0)
	assert.True(t, out[idx].IsRemovalMarker())
	assert.Equal(t, -1, domain.SlotEpochAt(out, 5), "a change at the removal's own stamp comes after it")

	// A removal of a slot with nothing in it is a marker; one stamped before a
	// pin's start leaves the pin.
	out, removed, _ = domain.RemoveFromSlot(nil, "t", "M", 0, 7)
	assert.False(t, removed)
	assert.Equal(t, []domain.TeamLineup{domain.RemovalMarker("t", "M", 0, 7)}, out)
	pin := domain.TeamLineup{TeamID: "t", MatchID: "M", StartedAt: 50, Positions: map[domain.Position]string{p1: "Sato"}}
	out, removed, _ = domain.RemoveFromSlot([]domain.TeamLineup{pin}, "t", "M", 0, 40)
	assert.False(t, removed)
	assert.Len(t, out, 2)
	out, removed, _ = domain.RemoveFromSlot([]domain.TeamLineup{pin}, "t", "M", 0, 60)
	assert.True(t, removed, "a pin taken away at or after its start")
	require.Len(t, out, 1)
	assert.True(t, out[0].IsRemovalMarker())

	// A removed record split again keeps its epoch's later part removed at
	// its own stamp, or a marker of it.
	out, _, _ = domain.RemoveFromSlot([]domain.TeamLineup{copyAt()}, "t", "M", 0, 100)
	out, removed, _ = domain.RemoveFromSlot(out, "t", "M", 0, 20)
	assert.True(t, removed)
	stamps := map[int64]bool{}
	for _, l := range out {
		stamps[l.RemovedAt] = true
	}
	assert.Equal(t, map[int64]bool{20: true, 100: true}, stamps)
}

// A starting lineup, and a match lineup a migration gave (no CopiedAt), split
// the same way: the part after the removal of a match lineup is a carried
// copy made at its first change.
func TestTeamLineupSplitAt_LineupsThatAreNotCopies(t *testing.T) {
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	start := domain.TeamLineup{TeamID: "t",
		Positions: map[domain.Position]string{p1: "Aoki", p2: "Ueda"},
		Stamps:    map[domain.Position]int64{p1: 40, p2: 5},
		History:   map[domain.Position][]domain.PositionChange{p1: {{Stamp: 0}, {Stamp: 5, Name: "Sato"}}}}
	before, after, hasBefore, hasAfter := start.SplitAt(20)
	require.True(t, hasBefore)
	require.True(t, hasAfter)
	assert.Equal(t, "Sato", before.Positions[p1])
	assert.Equal(t, "Ueda", before.Positions[p2])
	assert.Equal(t, int64(20), before.RemovedAt)
	assert.Equal(t, map[domain.Position]string{p1: "Aoki"}, after.Positions)
	assert.Zero(t, after.CopiedAt, "a starting lineup stays one")

	migrated := domain.TeamLineup{TeamID: "t", MatchID: "M", Positions: map[domain.Position]string{p1: "Hara"},
		Stamps: map[domain.Position]int64{p1: 50}}
	_, after, hasBefore, hasAfter = migrated.SplitAt(20)
	assert.False(t, hasBefore)
	require.True(t, hasAfter)
	assert.Equal(t, int64(50), after.CopiedAt, "a copy from its first change after the removal")
	assert.Equal(t, []domain.Position{p1}, after.Own)

	// The empty value every first change keeps is no lineup held.
	fresh := domain.TeamLineup{TeamID: "t", Positions: map[domain.Position]string{p1: "Mori"},
		Stamps: map[domain.Position]int64{p1: 40}, History: map[domain.Position][]domain.PositionChange{p1: {{Stamp: 0}}}}
	_, _, hasBefore, _ = fresh.SplitAt(20)
	assert.False(t, hasBefore)
}

// OwnPositionsOnly keeps a carried copy's own positions alone; any other
// lineup is returned as it is.
func TestTeamLineupOwnPositionsOnly(t *testing.T) {
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	c := domain.TeamLineup{TeamID: "t", MatchID: "M", CopiedAt: 10, Own: []domain.Position{p1},
		Positions: map[domain.Position]string{p1: "Mori", p2: "Ueda"},
		MemberIDs: map[domain.Position]string{p2: "u"}, Stamps: map[domain.Position]int64{p1: 10, p2: 3},
		History: map[domain.Position][]domain.PositionChange{p2: {{Stamp: 1}}}}
	got := c.OwnPositionsOnly()
	assert.Equal(t, map[domain.Position]string{p1: "Mori"}, got.Positions)
	assert.Nil(t, got.MemberIDs)
	assert.Equal(t, map[domain.Position]int64{p1: 10}, got.Stamps)
	assert.Nil(t, got.History)
	assert.Equal(t, "Ueda", c.Positions[p2], "the lineup itself is not changed")
	start := domain.TeamLineup{TeamID: "t", Positions: map[domain.Position]string{p2: "Ueda"}}
	assert.Equal(t, start, start.OwnPositionsOnly())
}

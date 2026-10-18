package domain_test

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A position holds its latest change BY STAMP (operator decision L1): an
// earlier-stamped change arriving later only joins its History, where a read
// at a stamp between the two finds it.
func TestApplyStampedChanges_APositionHoldsItsLatestChangeByStamp(t *testing.T) {
	p := domain.PositionNumbered(1)
	start := domain.TeamLineup{Positions: map[domain.Position]string{p: "Sato"}, Stamps: map[domain.Position]int64{p: 10}}
	save := func(l domain.TeamLineup, name string, at int64) domain.TeamLineup {
		out, err := l.ApplyStampedChanges([]domain.Position{p}, map[domain.Position]string{p: name}, map[domain.Position]string{p: "m-" + name}, map[domain.Position]int64{p: at}, 1_000)
		require.NoError(t, err)
		return out
	}
	inTime := save(save(start, "Kato", 20), "Aoki", 30)
	late := save(save(start, "Aoki", 30), "Kato", 20)
	for name, l := range map[string]domain.TeamLineup{"in time": inTime, "the earlier change arriving last": late} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "Aoki", l.Positions[p])
			assert.Equal(t, int64(30), l.Stamps[p])
			for at, want := range map[int64]string{5: "", 15: "Sato", 25: "Kato", 35: "Aoki"} {
				held, ok := l.HeldAt(p, at)
				if want == "" {
					assert.False(t, ok, "nothing before %d", at)
					assert.Empty(t, held.Name, "a value set later is never answered for an earlier stamp")
					continue
				}
				assert.True(t, ok)
				assert.Equal(t, want, held.Name, "at %d", at)
			}
		})
	}
	_, err := start.ApplyStampedChanges(nil, nil, nil, nil, 0)
	require.ErrorIs(t, err, domain.ErrLineupNoChangedPositions)
	_, err = start.ApplyStampedChanges([]domain.Position{p}, map[domain.Position]string{}, nil, map[domain.Position]int64{p: 5}, 1_000)
	require.ErrorIs(t, err, domain.ErrLineupChangedPositionMissing, "a stale change still names its value")
}

// On a carried copy a changed position joins Own, its earlier History is not
// the copy's (a carried position's value is the source's), and a change made
// before the copy's point moves the point back to it.
func TestApplyStampedChanges_ACarriedCopyKeepsItsOwnPositions(t *testing.T) {
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	copyOf := domain.TeamLineup{MatchID: "P1-1", CopiedAt: 50, Positions: map[domain.Position]string{p1: "Sato", p2: "Ueda"},
		Stamps: map[domain.Position]int64{p1: 10, p2: 10}, Own: []domain.Position{p2}}
	assert.True(t, copyOf.IsCarriedCopy())
	assert.False(t, domain.TeamLineup{StartedAt: 5, CopiedAt: 5}.IsCarriedCopy(), "a pin is not a copy")
	out, err := copyOf.ApplyStampedChanges([]domain.Position{p1}, map[domain.Position]string{p1: "Kato"}, nil, map[domain.Position]int64{p1: 40}, 1_000)
	require.NoError(t, err)
	assert.Equal(t, []domain.Position{p1, p2}, out.Own)
	assert.Equal(t, int64(40), out.CopiedAt, "an earlier change moves the copy's point back")
	assert.Empty(t, out.History[p1], "a carried value is the source's, not the copy's history")
	assert.Equal(t, map[domain.Position]bool{p1: true, p2: true}, out.OwnSet())
	_, ok := out.HeldAt(p1, 30)
	assert.False(t, ok, "before its own change the copy carried the position")

	// The copy shares nothing with what it was made from.
	c := out.Clone()
	c.Own[0], c.Positions[p1] = p2, "x"
	assert.Equal(t, p1, out.Own[0])
	assert.Equal(t, "Kato", out.Positions[p1])
}

// History keeps every stamped change of a position, with no bound by count
// (stage 6 review 8, m2), and Clone copies it deeply.
func TestApplyStampedChanges_HistoryKeepsEveryChange(t *testing.T) {
	const changes = 40
	p := domain.PositionNumbered(1)
	l := domain.TeamLineup{Positions: map[domain.Position]string{}}
	for i := 1; i <= changes; i++ {
		var err error
		l, err = l.ApplyStampedChanges([]domain.Position{p}, map[domain.Position]string{p: fmt.Sprint("n", i)}, nil, map[domain.Position]int64{p: int64(i * 10)}, 10_000)
		require.NoError(t, err)
	}
	// Nothing is dropped: the empty value from the beginning and every value
	// before the current one, so a read at any stamp finds what was held.
	require.Len(t, l.History[p], changes)
	for at, want := range map[int64]string{5: "", 15: "n1", 35: "n3", 125: "n12", int64(changes * 10): fmt.Sprint("n", changes-1), 10_000: fmt.Sprint("n", changes)} {
		held, ok := l.HeldAt(p, at)
		require.True(t, ok)
		assert.Equal(t, want, held.Name, "at %d", at)
	}
	c := l.Clone()
	c.History[p][0].Name = "changed"
	assert.NotEqual(t, "changed", l.History[p][0].Name)
	assert.Equal(t, domain.PositionChange{Stamp: int64(changes * 10), Name: fmt.Sprint("n", changes)}, l.Held(p))
}

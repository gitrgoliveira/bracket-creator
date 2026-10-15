package domain_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

func TestScoreEventValidate(t *testing.T) {
	ok := domain.ScoreEvent{ID: "e1", Stamp: 1, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "M"}
	assert.NoError(t, ok.Validate())
	for _, c := range []struct {
		name string
		edit func(*domain.ScoreEvent)
	}{
		{"no id", func(e *domain.ScoreEvent) { e.ID = "" }},
		{"long id", func(e *domain.ScoreEvent) { e.ID = strings.Repeat("x", domain.MaxScoreEventIDLen+1) }},
		{"long name", func(e *domain.ScoreEvent) { e.Name = strings.Repeat("x", domain.MaxScoreEventFieldLen+1) }},
		{"no stamp", func(e *domain.ScoreEvent) { e.Stamp = 0 }},
		{"negative seq", func(e *domain.ScoreEvent) { e.Seq = -1 }},
		{"bad pos", func(e *domain.ScoreEvent) { e.Pos = -2 }},
		{"hantei is not a strike", func(e *domain.ScoreEvent) { e.Letter = domain.HanteiMark }},
		{"maru is not a strike", func(e *domain.ScoreEvent) { e.Letter = domain.DefaultWinIppon }},
		{"two letters", func(e *domain.ScoreEvent) { e.Letter = "MK" }},
		{"no side", func(e *domain.ScoreEvent) { e.Side = "" }},
		{"unknown kind", func(e *domain.ScoreEvent) { e.Kind = "ippon.toggle" }},
		{"removal without target", func(e *domain.ScoreEvent) { e.Kind = domain.ScoreIpponRemove }},
		{"bout decision on the match", func(e *domain.ScoreEvent) {
			e.Kind, e.Value, e.Side = domain.ScoreBoutDecision, domain.BoutDecisionHikiwake, ""
		}},
		{"bout decision value", func(e *domain.ScoreEvent) { e.Kind, e.Value, e.Pos = domain.ScoreBoutDecision, "draw", 1 }},
		{"verdict off the representative bout", func(e *domain.ScoreEvent) { e.Kind, e.Pos = domain.ScoreDaihyosenVerdict, 1 }},
		{"verdict value", func(e *domain.ScoreEvent) { e.Kind, e.Pos, e.Value = domain.ScoreDaihyosenVerdict, -1, "x" }},
		{"encho side", func(e *domain.ScoreEvent) { e.Kind, e.Side = domain.ScoreEnchoAdd, "c" }},
	} {
		t.Run(c.name, func(t *testing.T) {
			e := ok
			c.edit(&e)
			assert.Error(t, e.Validate())
		})
	}
	for _, e := range []domain.ScoreEvent{
		{ID: "r", Stamp: 1, Kind: domain.ScoreIpponRemove, Target: "e1"},
		{ID: "x", Stamp: 1, Kind: domain.ScoreEnchoRemove, Pos: 2},
		{ID: "d", Stamp: 1, Kind: domain.ScoreBoutDecision, Pos: 2, Value: domain.BoutDecisionClear},
		{ID: "f", Stamp: 1, Kind: domain.ScoreBoutDecision, Pos: 2, Side: "b", Value: domain.BoutDecisionFusensho},
		{ID: "v", Stamp: 1, Kind: domain.ScoreDaihyosenVerdict, Pos: -1, Value: domain.BoutDecisionClear},
		{ID: "p", Stamp: 1, Kind: domain.ScoreRepPick, Pos: -1, Side: "a", MemberID: "m"},
		{ID: "h", Stamp: 1, Kind: domain.ScoreIpponAdd, Side: "b", Letter: "H"},
	} {
		assert.NoError(t, e.Validate(), e.ID)
	}
}

func TestApplyFoulIncrement(t *testing.T) {
	f, h := domain.ApplyFoulIncrement(0, 0, 0)
	assert.Equal(t, 1, f)
	assert.False(t, h)
	f, h = domain.ApplyFoulIncrement(1, 1, 0)
	assert.Equal(t, 0, f)
	assert.True(t, h)
	f, h = domain.ApplyFoulIncrement(1, 2, 0)
	assert.Equal(t, 0, f)
	assert.False(t, h, "the opponent already holds two")
	_, h = domain.ApplyFoulIncrement(1, 0, 2)
	assert.False(t, h, "the bout is already decided")
}

func TestBoutWinnerSide(t *testing.T) {
	assert.Equal(t, "a", domain.BoutWinnerSide(1, 1, false, "", "a"))
	assert.Equal(t, "", domain.BoutWinnerSide(1, 0, true, "", ""))
	assert.Equal(t, "b", domain.BoutWinnerSide(0, 0, false, "b", ""))
	assert.Equal(t, "a", domain.BoutWinnerSide(2, 1, false, "", ""))
	assert.Equal(t, "b", domain.BoutWinnerSide(0, 1, false, "", ""))
	assert.Equal(t, "", domain.BoutWinnerSide(1, 1, false, "", ""))
	assert.Equal(t, "a", domain.BoutWinnerSide(2, 1, false, "", "b"), "a hantei only on a tie")
}

func TestAdjustFlags(t *testing.T) {
	n, out := domain.AdjustFlags(domain.MaxEngiFlags, 1)
	assert.Equal(t, domain.MaxEngiFlags, n)
	assert.Equal(t, domain.ScoreOutcomeCapped, out)
	n, out = domain.AdjustFlags(0, -1)
	assert.Equal(t, 0, n)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, out)
	n, out = domain.AdjustFlags(2, 1)
	assert.Equal(t, 3, n)
	assert.Equal(t, domain.ScoreOutcomeApplied, out)
}

func TestStruckIppons(t *testing.T) {
	assert.Equal(t, []string{"M", "K"}, domain.StruckIppons([]string{"○", "M", domain.HanteiMark, domain.IpponPlaceholder, "", "K"}))
	assert.Nil(t, domain.StruckIppons(nil))
}

func TestBoutScoreFold(t *testing.T) {
	var b domain.BoutScore
	assert.False(t, b.Played())
	assert.Equal(t, domain.ScoreOutcomeApplied, b.AddIppon("a", "M", "e1"))
	assert.Equal(t, domain.ScoreOutcomeApplied, b.AddFoul("b", "f1"))
	assert.True(t, b.Played())
	assert.Equal(t, domain.ScoreOutcomeApplied, b.AddFoul("b", "f2"))
	assert.Equal(t, []string{"M", "H"}, b.IpponsA)
	assert.Equal(t, []string{"e1", "f2"}, b.MarksA)
	assert.Equal(t, domain.ScoreOutcomeCapped, b.AddIppon("b", "K", "e2"))
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, b.SetFusensho("b", "z"), "a has already won the bout")
	assert.Equal(t, domain.ScoreOutcomeApplied, b.RemoveMark("f2"))
	assert.Equal(t, domain.ScoreOutcomeNoTarget, b.RemoveMark("f2"))
	assert.Equal(t, domain.ScoreOutcomeNoTarget, b.RemoveFoul("b"))

	// A placeholder slot is filled first, and its mark beside it.
	p := domain.BoutScore{IpponsA: []string{domain.IpponPlaceholder, "K"}}
	p.AddIppon("a", "M", "e")
	assert.Equal(t, []string{"M", "K"}, p.IpponsA)
	assert.Equal(t, []string{"e", ""}, p.MarksA)

	// Fusensho in overtime gives one maru; a Tie ends it; awarding the same
	// side again changes nothing.
	f := domain.BoutScore{IpponsB: []string{"D"}, MarksB: []string{"d"}, Encho: 1}
	assert.Equal(t, domain.ScoreOutcomeApplied, f.SetFusensho("a", "z"))
	assert.Equal(t, []string{domain.DefaultWinIppon}, f.IpponsA)
	assert.Equal(t, domain.ScoreOutcomeApplied, f.SetFusensho("a", "z2"))
	assert.Equal(t, []string{"z"}, f.MarksA)
	assert.Equal(t, domain.ScoreOutcomeNotAllowed, f.RemoveMark("z"), "the maru is not a point to remove")
	assert.Equal(t, domain.ScoreOutcomeApplied, f.SetDraw())
	assert.True(t, f.Draw)
	assert.Empty(t, f.Fusensho)
	assert.Empty(t, f.IpponsA)
	assert.Equal(t, domain.ScoreOutcomeApplied, f.ClearBoutDecision())
	assert.False(t, f.Draw)
	// A strike ends a fusensho; the circles never survive as points.
	g := domain.BoutScore{}
	g.SetFusensho("b", "z")
	g.AddIppon("a", "M", "m")
	assert.Empty(t, g.Fusensho)
	assert.Empty(t, g.IpponsB)
	assert.Equal(t, []string{"M"}, g.IpponsA)
	// Clearing a fusensho a baseline brought (no snapshot) keeps the struck
	// points only.
	c := domain.ClearFusensho(domain.BoutScore{IpponsA: []string{"○", "○"}, Fusensho: "a"})
	assert.Empty(t, c.IpponsA)
	e := domain.BoutScore{Fusensho: "a", IpponsA: []string{"○", "○"}}
	e.ClearBoutDecision()
	assert.Empty(t, e.IpponsA)
	assert.Equal(t, domain.ScoreOutcomeNoTarget, (&domain.BoutScore{}).AdjustEncho(-1))
	assert.True(t, domain.FusenshoAllowed(domain.BoutScore{Fusensho: "a"}, "a"))
}

// The cap counts struck points on both sides: a fusensho winner's circles
// are no strikes, so a strike on that side lands and ends the fusensho.
func TestAddIpponOnAFusenshoWinnersSide(t *testing.T) {
	b := domain.BoutScore{}
	if !assert.Equal(t, domain.ScoreOutcomeApplied, b.SetFusensho(domain.ScoreSideA, "f")) {
		return
	}
	assert.Equal(t, []string{domain.DefaultWinIppon, domain.DefaultWinIppon}, b.IpponsA)
	assert.Equal(t, domain.ScoreOutcomeApplied, b.AddIppon(domain.ScoreSideA, "M", "e"))
	assert.Empty(t, b.Fusensho, "the strike ends the fusensho")
	assert.Equal(t, []string{"M"}, b.IpponsA)
	assert.Equal(t, []string{"e"}, b.MarksA)

	b = domain.BoutScore{IpponsA: []string{"M", "K"}}
	assert.Equal(t, domain.ScoreOutcomeCapped, b.AddIppon(domain.ScoreSideA, "D", "x"), "two struck points still cap")
}

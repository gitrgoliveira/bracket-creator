package state

import "testing"

// ClearDerivedScoreFields drops everything only the score replay derives, the
// per-session event seqs included (stage 3 review 11, Y9): a client binding a
// whole MatchResult must not be able to claim which taps the log holds.
func TestClearDerivedScoreFields_DropsEveryDerivedField(t *testing.T) {
	m := MatchResult{
		MarkIDs:      &MarkIDs{A: []string{"x"}},
		ScoreVersion: 4,
		EventSeqs:    map[string]int64{"dev": 9},
		SubResults:   []SubMatchResult{{Position: 1, MarkIDs: &MarkIDs{B: []string{"y"}}}},
	}
	m.ClearDerivedScoreFields()
	if m.MarkIDs != nil || m.ScoreVersion != 0 || m.EventSeqs != nil || m.SubResults[0].MarkIDs != nil {
		t.Fatalf("derived fields kept: marks %v, version %d, seqs %v, bout marks %v",
			m.MarkIDs, m.ScoreVersion, m.EventSeqs, m.SubResults[0].MarkIDs)
	}
}

package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// DrawHiddenIn is the one rule for "the public may not see this draw now":
// draw-ready, stored hidden, and an officiated tournament. Every other
// combination shows the draw.
func TestDrawHiddenIn(t *testing.T) {
	officiated := &Tournament{Mode: TournamentModeOfficiated}
	selfRun := &Tournament{Mode: TournamentModeSelfRun}
	for _, tc := range []struct {
		name   string
		comp   *Competition
		tourn  *Tournament
		hidden bool
	}{
		{"hidden draft, officiated", &Competition{Status: CompStatusDrawReady, DrawHidden: true}, officiated, true},
		{"hidden draft, mode unset", &Competition{Status: CompStatusDrawReady, DrawHidden: true}, &Tournament{}, true},
		{"hidden draft, no tournament", &Competition{Status: CompStatusDrawReady, DrawHidden: true}, nil, true},
		{"published draft", &Competition{Status: CompStatusDrawReady}, officiated, false},
		{"hidden draft, self-run", &Competition{Status: CompStatusDrawReady, DrawHidden: true}, selfRun, false},
		{"setup with a stale flag", &Competition{Status: CompStatusSetup, DrawHidden: true}, officiated, false},
		{"started pools with the flag", &Competition{Status: CompStatusPools, DrawHidden: true}, officiated, false},
		{"started knockout with the flag", &Competition{Status: CompStatusKnockout, DrawHidden: true}, officiated, false},
		{"complete with the flag", &Competition{Status: CompStatusComplete, DrawHidden: true}, officiated, false},
		{"nil competition", nil, officiated, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.hidden, tc.comp.DrawHiddenIn(tc.tourn))
		})
	}
}

// An absent draw_hidden key reads as published, so a competition drawn
// before the field existed stays public; a stored true round-trips, and a
// false is not written at all.
func TestDrawHidden_StoredShape(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	cfg := filepath.Join(dir, "competitions", "drawn", "config.md")

	require.NoError(t, s.SaveCompetition(&Competition{ID: "drawn", Name: "Drawn", Status: CompStatusDrawReady}))
	raw, err := os.ReadFile(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "draw_hidden", "a published draft writes no key")
	c, err := s.LoadCompetition("drawn")
	require.NoError(t, err)
	assert.False(t, c.DrawHidden, "absent reads as published")
	assert.False(t, c.DrawHiddenIn(&Tournament{}))

	_, err = s.UpdateCompetitionChanged("drawn", func(cur *Competition) (*Competition, error) {
		cur.DrawHidden = true
		return cur, nil
	})
	require.NoError(t, err)
	raw, err = os.ReadFile(cfg)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(raw), "draw_hidden: true"))
	reopened, err := NewStore(dir)
	require.NoError(t, err)
	c, err = reopened.LoadCompetition("drawn")
	require.NoError(t, err)
	assert.True(t, c.DrawHidden)
	assert.True(t, c.DrawHiddenIn(&Tournament{}))
}

// DrawHidden is server-managed, not a setting: a publish or unpublish never
// makes an open Settings page stale.
func TestConfigVersion_IgnoresDrawHidden(t *testing.T) {
	c := &Competition{ID: "c", Name: "C", Status: CompStatusDrawReady, Courts: []string{"A"}}
	before := ConfigVersion(c)
	c.DrawHidden = true
	assert.Equal(t, before, ConfigVersion(c))
}

// A publish or unpublish changes config.md, so the scheduled backup's
// fingerprint moves with it.
func TestFingerprint_MovesOnADrawVisibilityFlip(t *testing.T) {
	s := fingerprintTestStore(t)
	const comp = "fp-draw-hidden"
	require.NoError(t, s.SaveCompetition(&Competition{ID: comp, Name: "C", Status: CompStatusDrawReady, DrawHidden: true}))
	snap0, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)

	_, err = s.UpdateCompetitionChanged(comp, func(cur *Competition) (*Competition, error) {
		cur.DrawHidden = false
		return cur, nil
	})
	require.NoError(t, err)
	snap1, err := s.Snapshot(ScopeCompetition, comp)
	require.NoError(t, err)
	assert.NotEqual(t, Fingerprint(snap0.Files), Fingerprint(snap1.Files))
}

// A backup keeps the draw's visibility: a hidden draft restored from its
// archive is hidden again.
func TestBackup_RoundTripKeepsDrawHidden(t *testing.T) {
	s, _ := newRestoreTestStore(t)
	require.NoError(t, s.SaveCompetition(&Competition{
		ID: "men", Name: "Men", Date: "03-11-2026", Status: CompStatusDrawReady,
		NumberPrefix: "M", Courts: []string{"A"}, DrawHidden: true,
	}))
	a := archiveOf(t, s, ScopeCompetition, "men")

	_, err := s.UpdateCompetitionChanged("men", func(cur *Competition) (*Competition, error) {
		cur.DrawHidden = false
		return cur, nil
	})
	require.NoError(t, err)

	_, err = s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	require.NoError(t, err)
	c, err := s.LoadCompetition("men")
	require.NoError(t, err)
	assert.True(t, c.DrawHidden, "the restored draft is hidden as it was when backed up")
}

package mobileapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// setDraft stores f's competition as a draft (draw-ready) whose draw is stored
// hidden or published, and reloads it into f.
func setDraft(t *testing.T, f *pnFixture, hidden bool) {
	t.Helper()
	f.comp.Status = state.CompStatusDrawReady
	f.comp.DrawHidden = hidden
	require.NoError(t, f.store.SaveCompetition(f.comp))
	loaded, err := f.store.LoadCompetition(f.comp.ID)
	require.NoError(t, err)
	f.comp = loaded
}

// makeSelfRun turns f's tournament self-run.
func makeSelfRun(t *testing.T, f *pnFixture) {
	t.Helper()
	stored, err := f.store.LoadTournament()
	require.NoError(t, err)
	selfRun := *stored
	selfRun.Mode = state.TournamentModeSelfRun
	require.NoError(t, f.store.SaveTournament(&selfRun))
}

// The draw judgement per audience: only the organiser sees a hidden draft's
// draw; a reveal key unlocks names, never the draw; a published draft, a
// started competition and a self-run tournament hide nothing. The judgement
// reaches the view whether or not the competition hides names.
func TestPublicDrawHidden_PerAudience(t *testing.T) {
	cases := []struct {
		name        string
		publicNames string
		hidden      bool
		started     bool
		selfRun     bool
		headers     func(f pnFixture) map[string]string
		want        bool
	}{
		{name: "organiser sees a hidden draft", publicNames: state.PublicNamesHidden, hidden: true,
			headers: func(pnFixture) map[string]string { return map[string]string{"X-Tournament-Password": "secret-pw"} }},
		{name: "public does not see a hidden draft", publicNames: state.PublicNamesHidden, hidden: true, want: true},
		{name: "public does not see a hidden draft whose names are shown", publicNames: "", hidden: true, want: true},
		{name: "a competition reveal key shows names, not the draw", publicNames: state.PublicNamesCompetition, hidden: true, want: true,
			headers: func(f pnFixture) map[string]string {
				return map[string]string{revealKeyHeader: f.comp.ID + ":" + state.CompetitionRevealKey(f.comp)}
			}},
		{name: "public sees a published draft", publicNames: state.PublicNamesHidden},
		{name: "public sees a started competition whatever is stored", publicNames: state.PublicNamesHidden, hidden: true, started: true},
		{name: "self-run shows the draw as soon as it is made", publicNames: state.PublicNamesHidden, hidden: true, selfRun: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPNFixture(t, tc.publicNames)
			setDraft(t, &f, tc.hidden)
			if tc.started {
				f.comp.Status = state.CompStatusPools
				require.NoError(t, f.store.SaveCompetition(f.comp))
			}
			if tc.selfRun {
				makeSelfRun(t, &f)
			}
			var hdr map[string]string
			if tc.headers != nil {
				hdr = tc.headers(f)
			}
			aud, ok, w := judgeRequest(t, f.pn, hdr)
			require.True(t, ok, w.Body.String())
			if tc.headers != nil && tc.publicNames == state.PublicNamesCompetition {
				require.True(t, aud.revealsAll(f.comp.ID), "the key is valid: it reveals the names")
			}

			comp, err := f.store.LoadCompetition(f.comp.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.want, f.pn.drawHidden(comp, aud), "judgement")
			v, err := f.pn.view(comp, aud)
			require.NoError(t, err)
			assert.Equal(t, tc.want, v.drawHidden, "the view carries the judgement")
		})
	}
}

// A nil competition hides nothing (the read answers its own 404).
func TestPublicDrawHidden_NilCompetition(t *testing.T) {
	f := newPNFixture(t, state.PublicNamesHidden)
	assert.False(t, f.pn.drawHidden(nil, audience{}))
}

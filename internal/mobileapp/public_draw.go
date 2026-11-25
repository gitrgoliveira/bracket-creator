package mobileapp

import "github.com/gitrgoliveira/bracket-creator/internal/state"

// Keeping a draft draw off the public pages until it is published (bc-drpb).
//
// The rule of the draw itself is state.Competition.DrawHiddenIn (draw-ready,
// stored as hidden, the tournament officiated). This file holds the server's
// one judgement of it per caller, publicNames.drawHidden, which publicNames.view
// puts on the mask view (maskView.drawHidden), so every public read and the
// event stream that already ask the view for names ask it for the draw too: a
// hidden draft is answered to the public as the same competition in setup.

// drawHidden reports whether aud may not see comp's draw right now: the draw
// is hidden in the tournament as it stands (Competition.DrawHiddenIn) and the
// caller is not the organiser. A reveal key unlocks names only, never a
// hidden draw: its holder is answered as the public is. A self-run tournament
// shows its draw as soon as it is made (DrawHiddenIn), so every caller there
// reads it, the judge answering everyone as the public. The tournament is read
// only for a competition whose draw is stored as hidden; one that cannot be
// read counts as officiated, so the draw stays hidden.
func (p *publicNames) drawHidden(comp *state.Competition, aud audience) bool {
	if aud.isOrganiser() || comp == nil || !comp.DrawHidden {
		return false
	}
	return comp.DrawHiddenIn(p.tournament())
}

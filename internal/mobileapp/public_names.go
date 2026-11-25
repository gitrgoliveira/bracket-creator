package mobileapp

import (
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Hiding competitor names from the public pages (bc-hnam, bc-anon).
//
// Two halves, both server-side so a name a spectator may not see never leaves
// the server:
//
//   - The AUDIENCE JUDGE (publicNames.judge) decides, per request, who a public
//     read is answered for: the organiser (a valid X-Tournament-Password), a
//     reveal scope (valid X-Reveal-Key entries: a competition's link, which
//     reveals every name in it, or competitors' own links, which reveal those
//     competitors alone), or the public. A password sent but wrong is answered
//     401, never the masked view. A reveal key unlocks names on reads only: it
//     never passes AuthMiddleware, so it can never reach a write or an
//     organiser-only read.
//   - The NAME MASK (nameMask, through maskView) rewrites a COPY of a payload
//     so each hidden name reads as the competitor's number. It is built per
//     competition from the roster, the draw's numbers and the team members,
//     and cached until one of those files (or the competition's config)
//     changes. A named side with no id fails closed as "?".
//
// What a masked payload keeps: ids, numbers, dojos (Q3 b) and team names
// (Q-teams); "Winner of ..." and "Pool A-1st" placeholders. What it replaces:
// an individual's name and zekken name (an engi side, solo or pair, is one participant),
// match sides and winners, team members' names (with the member's label, the
// team's number and the member's index, "T10.1"), lineup positions,
// representative-bout fighters and fighting-spirit recipients.
//
// The mask view carries one judgement besides names (bc-drpb): whether the
// audience may see the competition's draw (maskView.drawHidden, judged by
// publicNames.drawHidden in public_draw.go). It rides here because the view
// already reaches every public read and the event stream, so the view is "what
// this audience may read of one competition", not only its names.
//
// The files: this one holds publicNames (the owner of both halves) and whether a
// competition hides its names; public_names_audience.go the audience judge and
// the reveal keys; public_names_view.go the mask view over payloads;
// public_names_mask.go the mask a competition's names are built into;
// public_names_cache.go the cache of masks and the files it is keyed by;
// public_draw.go whether the audience may see a draft draw.

// maskedUnknown is what a hidden name reads as when the mask cannot say which
// competitor it is (a named side with no id, an id with no number): fail
// closed rather than show the name.
const maskedUnknown = "?"

// publicNames owns the audience judge and the per-competition name masks for
// one router: one instance is shared by every public read and the event
// stream, so a mask is built once for all of them.
type publicNames struct {
	store    *state.Store
	verifier PasswordVerifier
	masks    *nameMaskCache
	now      func() time.Time
}

// newPublicNames builds the judge and mask cache. In locked mode the verifier
// is wrapped with a digest cache (cachedVerifier), because a bcrypt
// comparison on every public read would cost far more than the read.
func newPublicNames(store *state.Store, verifier PasswordVerifier) *publicNames {
	if verifier == nil {
		verifier = NewFileVerifier(store)
	}
	if cacheableVerifier(verifier) {
		verifier = newCachedVerifier(verifier, time.Now)
	}
	return &publicNames{
		store:    store,
		verifier: verifier,
		masks:    newNameMaskCache(store),
		now:      time.Now,
	}
}

// hides reports whether the public pages hide comp's names right now
// (Competition.HidesNamesIn the tournament as it stands). A self-run
// tournament always shows names, whatever a competition's setting says (a
// restore can leave one that hides them); the competition keeps its setting,
// so it hides names again if it is restored into an officiated tournament
// (the mode of a tournament cannot change once it is made). The tournament is
// read only for a competition whose setting hides names.
func (p *publicNames) hides(comp *state.Competition) bool {
	return comp.HidesNames() && comp.HidesNamesIn(p.tournament())
}

// selfRun reports whether the tournament is self-run. A tournament that cannot
// be read counts as officiated.
func (p *publicNames) selfRun() bool {
	return p.tournament().IsSelfRun()
}

// tournament is the tournament names are judged in, nil when it cannot be
// read (it then counts as officiated, so the names stay hidden).
func (p *publicNames) tournament() *state.Tournament {
	t, err := p.store.LoadTournament()
	if err != nil {
		return nil
	}
	return t
}

// view is the mask view for comp and aud: the names aud may read, and whether
// aud may read comp's draw (readsAsSetup, judged whether or not names are
// hidden). The error is the mask's build error (a file that could not be
// read); the view is still usable and fails closed, so a caller logs the
// error and serves the view.
func (p *publicNames) view(comp *state.Competition, aud audience) (maskView, error) {
	draw := p.readsAsSetup(comp, aud)
	if !p.hides(comp) || aud.revealsAll(comp.ID) {
		return maskView{drawHidden: draw}, nil
	}
	m, err := p.masks.get(comp.ID)
	return maskView{m: m, reveal: aud.revealedCompetitors(comp.ID), drawHidden: draw}, err
}

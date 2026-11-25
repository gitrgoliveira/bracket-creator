package state

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// The poster rules shared with the browser through
// testdata/poster_rules.json (TestPosterRules_SharedTable pins the table to
// these values, and the Posters page's counters and defaults read the table).
const (
	// MaxPosterHeadingLen and MaxPosterCallToActionLen are counted in
	// characters (runes), after trimming.
	MaxPosterHeadingLen      = 80
	MaxPosterCallToActionLen = 120

	// PosterLogoCentre puts the logo in the middle of the code (encoded at
	// level H), PosterLogoAbove above it. "" is the default: centre when the
	// address fits level H, else above.
	PosterLogoCentre = "centre"
	PosterLogoAbove  = "above"

	// MaxPosterCourtEntries bounds the courts map: twice the court limit, so
	// the entries of courts since removed are kept without the block growing
	// without end.
	MaxPosterCourtEntries = 2 * helper.MaxCourts

	// PosterViewerHeading is the viewer home poster's default heading, and a
	// court poster's default is PosterCourtHeadingPrefix plus its label.
	PosterViewerHeading      = "Follow the tournament live"
	PosterCourtHeadingPrefix = "Shiaijo "
)

// PosterLogoValues lists every logo value a save may send, the default first.
var PosterLogoValues = []string{"", PosterLogoCentre, PosterLogoAbove}

// PosterChoices is what the organiser chose for one poster. Every field is
// omitted when empty, so an untouched poster stores nothing.
type PosterChoices struct {
	Heading      string `yaml:"heading,omitempty" json:"heading,omitempty"`
	CallToAction string `yaml:"call_to_action,omitempty" json:"callToAction,omitempty"`
	Logo         string `yaml:"logo,omitempty" json:"logo,omitempty"`
	HideSponsors bool   `yaml:"hide_sponsors,omitempty" json:"hideSponsors,omitempty"`
}

// Posters holds the choices of the viewer home poster and of each court's
// poster, keyed by court label. An entry for a court the tournament no
// longer has is kept (the poster comes back with the court) and not shown.
// An absent block reads as every default, so no load-time migration exists.
type Posters struct {
	Viewer PosterChoices            `yaml:"viewer,omitempty" json:"viewer,omitzero"`
	Courts map[string]PosterChoices `yaml:"courts,omitempty" json:"courts,omitempty"`
}

// isEmpty reports whether c holds no choice after trimming.
func (c PosterChoices) isEmpty() bool {
	return trimPosterChoices(c) == PosterChoices{}
}

// IsEmpty reports whether the block holds no choice at all. A nil block is
// empty.
func (p *Posters) IsEmpty() bool {
	if p == nil {
		return true
	}
	if !p.Viewer.isEmpty() {
		return false
	}
	for _, c := range p.Courts {
		if !c.isEmpty() {
			return false
		}
	}
	return true
}

func trimPosterChoices(c PosterChoices) PosterChoices {
	c.Heading = strings.TrimSpace(c.Heading)
	c.CallToAction = strings.TrimSpace(c.CallToAction)
	c.Logo = strings.TrimSpace(c.Logo)
	return c
}

// NormalizePosters returns a trimmed copy of p with the empty court entries
// dropped, or nil when nothing is left, so an empty block is never written
// to tournament.md. p itself is not changed.
func NormalizePosters(p *Posters) *Posters {
	if p == nil {
		return nil
	}
	out := &Posters{Viewer: trimPosterChoices(p.Viewer)}
	for key, c := range p.Courts {
		if c.isEmpty() {
			continue
		}
		if out.Courts == nil {
			out.Courts = make(map[string]PosterChoices, len(p.Courts))
		}
		out.Courts[key] = trimPosterChoices(c)
	}
	if out.IsEmpty() {
		return nil
	}
	return out
}

// IsCourtLabel is the one rule of what a court label is: a single character
// that is not blank. The tournament's court list and the posters' court keys
// both ask it.
func IsCourtLabel(label string) bool {
	return strings.TrimSpace(label) != "" && utf8.RuneCountInString(label) == 1
}

// posterName names a poster as the Posters page shows it.
func posterName(court string) string {
	if court == "" {
		return "viewer home"
	}
	return PosterCourtHeadingPrefix + court
}

// PosterError is a refused posters block: Message is one plain sentence for
// the operator, answered as it is.
type PosterError struct {
	Message string
}

func (e *PosterError) Error() string { return e.Message }

func posterErrorf(format string, args ...any) error {
	return &PosterError{Message: fmt.Sprintf(format, args...)}
}

// ValidatePosters refuses a block the server would not store, with one plain
// sentence naming the poster and the box in the operator's words (the PUT and
// the POST answer it as it is). The lengths are judged after trimming. An
// entry for a court the tournament no longer has is valid: it is kept. Only
// non-empty court entries count toward MaxPosterCourtEntries, because
// NormalizePosters drops empty ones before storage.
func ValidatePosters(p *Posters) error {
	if p == nil {
		return nil
	}
	if err := validatePosterChoices("", p.Viewer); err != nil {
		return err
	}
	kept := 0
	for _, c := range p.Courts {
		if !c.isEmpty() {
			kept++
		}
	}
	if kept > MaxPosterCourtEntries {
		return posterErrorf("Poster choices can be kept for at most %d shiaijo; this save holds %d.", MaxPosterCourtEntries, kept)
	}
	for _, key := range slices.Sorted(maps.Keys(p.Courts)) {
		if !IsCourtLabel(key) {
			return posterErrorf("The poster choices name a shiaijo %q, which is not a shiaijo label: a label is one letter or character.", key)
		}
		if err := validatePosterChoices(key, p.Courts[key]); err != nil {
			return err
		}
	}
	return nil
}

func validatePosterChoices(court string, c PosterChoices) error {
	c = trimPosterChoices(c)
	name := posterName(court)
	if n := utf8.RuneCountInString(c.Heading); n > MaxPosterHeadingLen {
		return posterErrorf("The heading on the %s poster is longer than %d characters.", name, MaxPosterHeadingLen)
	}
	if n := utf8.RuneCountInString(c.CallToAction); n > MaxPosterCallToActionLen {
		return posterErrorf("The call to action on the %s poster is longer than %d characters.", name, MaxPosterCallToActionLen)
	}
	if !slices.Contains(PosterLogoValues, c.Logo) {
		return posterErrorf("The logo place on the %s poster must be in the centre of the code or above it.", name)
	}
	return nil
}

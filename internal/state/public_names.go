package state

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// What the public pages show for a competition's competitors
// (Competition.PublicNames, Tournament.DefaultPublicNames). One kind of
// reveal link at a time: per person, per competition, or none.
const (
	// PublicNamesShown shows names, as before the setting existed.
	PublicNamesShown = ""
	// PublicNamesPerson shows numbers; each competitor's own link (their
	// tag QR, or the organiser's Copy link) reveals their own name, and a
	// team's link reveals its members' names.
	PublicNamesPerson = "person"
	// PublicNamesCompetition shows numbers; one competition link reveals
	// every name in the competition.
	PublicNamesCompetition = "competition"
	// PublicNamesHidden shows numbers and has no link: only the signed-in
	// organiser sees names.
	PublicNamesHidden = "hidden"
)

// RevealUntilLayout is Competition.RevealUntil's layout: a local date and
// time, read in the server's time zone like every schedule time.
const RevealUntilLayout = "2006-01-02T15:04"

// revealSecretBytes is the length of a competition's reveal secret before
// it is hex-encoded.
const revealSecretBytes = 32

// Domain-separation labels for the keys derived from a reveal secret, so a
// competition key can never equal a competitor key.
const (
	revealLabelCompetition = "competition"
	revealLabelCompetitor  = "competitor:"
)

// ValidPublicNames reports whether v is one of the PublicNames values.
func ValidPublicNames(v string) bool {
	switch v {
	case PublicNamesShown, PublicNamesPerson, PublicNamesCompetition, PublicNamesHidden:
		return true
	}
	return false
}

// PublicNamesHidesNames reports whether v shows numbers instead of names
// on the public pages. Every value but "shown" hides, a value no setting has
// (a hand-edited config.md) included: a privacy setting that cannot be read
// fails closed, as HidesNamesIn does for a tournament it cannot read, and the
// organiser sets it again from the setup page.
func PublicNamesHidesNames(v string) bool {
	return v != PublicNamesShown
}

// PublicNamesHasLinks reports whether v gives reveal links (per person or
// per competition), so a reveal secret is needed.
func PublicNamesHasLinks(v string) bool {
	return v == PublicNamesPerson || v == PublicNamesCompetition
}

// HidesNames reports whether the public pages show this competition's
// competitors by number. A nil competition hides nothing.
func (c *Competition) HidesNames() bool {
	return c != nil && PublicNamesHidesNames(c.PublicNames)
}

// HidesNamesIn reports whether the public pages of tournament t hide this
// competition's names: its option hides them (HidesNames) and t is not
// self-run. A self-run tournament shows every name whatever a competition's
// stored setting says, because its competitors score their own matches from
// the public page; the setting itself is left as it is. A nil tournament (one
// that cannot be read) counts as officiated, so the names stay hidden. The one
// owner of that rule: the public pages and the event stream
// (mobileapp's publicNames.hides) and the Tags sheet's reveal links ask it.
func (c *Competition) HidesNamesIn(t *Tournament) bool {
	return c.HidesNames() && !t.IsSelfRun()
}

// NewCompetitionPublicNames is the public-names option a new competition of
// format starts with in tournament t when its maker does not choose one: the
// tournament default, except for a Swiss competition (not offered there yet)
// and in a self-run tournament (officiated only), which start with names
// shown, as does a competition made before any tournament exists (t nil).
func NewCompetitionPublicNames(t *Tournament, format string) string {
	if t == nil || t.IsSelfRun() || format == CompFormatSwiss {
		return PublicNamesShown
	}
	return t.DefaultPublicNames
}

// ValidRevealUntil reports whether v is a RevealUntil value: "" (the end
// of the tournament) or a date and time in RevealUntilLayout.
func ValidRevealUntil(v string) bool {
	if v == "" {
		return true
	}
	_, err := time.Parse(RevealUntilLayout, v)
	return err == nil
}

// NewRevealSecret mints a competition's reveal secret: revealSecretBytes
// random bytes as lowercase hex.
func NewRevealSecret() (string, error) {
	b := make([]byte, revealSecretBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("minting reveal secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// CompetitionRevealKey is the key of the competition's one reveal link:
// hex HMAC-SHA256(secret, "competition"). It is "" (no key) unless the
// competition gives a competition link and holds a well-formed secret.
// Expiry is not judged here; ask RevealOpen.
func CompetitionRevealKey(c *Competition) string {
	if c == nil || c.PublicNames != PublicNamesCompetition {
		return ""
	}
	return revealKey(c.RevealSecret, revealLabelCompetition)
}

// CompetitorRevealKey is the key of one competitor's own reveal link:
// hex HMAC-SHA256(secret, "competitor:" + participantID). It is "" (no
// key) unless the competition gives per-person links, holds a well-formed
// secret and participantID is not empty. Expiry is not judged here; ask
// RevealOpen. The key follows the participant id, which a substitute taking
// the place of a competitor keeps, so the earlier link then reveals the
// substitute's name; the organiser guide says so.
func CompetitorRevealKey(c *Competition, participantID string) string {
	if c == nil || c.PublicNames != PublicNamesPerson || participantID == "" {
		return ""
	}
	return revealKey(c.RevealSecret, revealLabelCompetitor+participantID)
}

// revealKey derives the hex HMAC-SHA256 of label under the hex secret, or
// "" when the secret is not revealSecretBytes of hex (fails closed on a
// hand-edited config.md).
func revealKey(secretHex, label string) string {
	secret, err := hex.DecodeString(secretHex)
	if err != nil || len(secret) != revealSecretBytes {
		return ""
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label)) // hash.Hash.Write never returns an error
	return hex.EncodeToString(mac.Sum(nil))
}

// RevealExpiry is when the competition's reveal links stop revealing, read
// in loc (the server's time zone): RevealUntil when set; otherwise one hour
// after the tournament's Closing time on its last day, or 01:00 the
// morning after the last day when no Closing time reads as HH:MM. ok is
// false when no time can be worked out (a RevealUntil that does not read,
// or no tournament date), and the links then reveal nothing.
func RevealExpiry(c *Competition, t *Tournament, loc *time.Location) (time.Time, bool) {
	if c == nil {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.Local
	}
	if c.RevealUntil != "" {
		at, err := time.ParseInLocation(RevealUntilLayout, c.RevealUntil, loc)
		if err != nil {
			return time.Time{}, false
		}
		return at, true
	}
	if t == nil {
		return time.Time{}, false
	}
	first, err := time.ParseInLocation("02-01-2006", strings.TrimSpace(t.Date), loc)
	if err != nil {
		return time.Time{}, false
	}
	days := t.DurationDays
	if days < 1 {
		days = 1
	}
	y, m, d := first.AddDate(0, 0, days-1).Date()
	if closing, err := time.Parse("15:04", strings.TrimSpace(t.ClosingTime)); err == nil {
		return time.Date(y, m, d, closing.Hour(), closing.Minute(), 0, 0, loc).Add(time.Hour), true
	}
	return time.Date(y, m, d+1, 1, 0, 0, 0, loc), true
}

// RevealOpen reports whether the competition's reveal links reveal names
// at now: the competition gives links (PublicNamesHasLinks) and now is
// before RevealExpiry, read in now's time zone, and the competition holds a
// well-formed reveal secret (without one no key can be derived, so nothing
// could reveal a name). Judged on every read, so
// switching links off, or an expiry passing, stops every link at once, and
// switching back or moving the expiry later makes the same links work
// again.
func RevealOpen(c *Competition, t *Tournament, now time.Time) bool {
	if c == nil || !PublicNamesHasLinks(c.PublicNames) || revealKey(c.RevealSecret, revealLabelCompetition) == "" {
		return false
	}
	expiry, ok := RevealExpiry(c, t, now.Location())
	return ok && now.Before(expiry)
}

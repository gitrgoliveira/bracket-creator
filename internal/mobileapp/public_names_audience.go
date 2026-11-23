package mobileapp

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The audience judge of hidden public names: who a public read is answered for,
// from the password and the reveal keys it carries (bc-hnam).

// revealKeyHeader carries the reveal keys a phone holds:
// "<compId>:<key>[,<compId>:<key>...]".
const revealKeyHeader = "X-Reveal-Key"

// Bounds on the X-Reveal-Key header. A phone holds a few keys (one per
// competition, or one per child it follows); anything past these is dropped
// unread, so a long header costs nothing.
const (
	maxRevealHeaderBytes = 8 << 10
	maxRevealKeys        = 32
	revealKeyHexLen      = 2 * sha256.Size
)

// Audience classes, the first part of an audience's key.
const (
	audienceOrganiser = "organiser"
	audiencePublic    = "public"
	audienceReveal    = "reveal"
)

// audience is who a public read is answered for. The zero value is the public.
type audience struct {
	organiser bool
	// competitions holds the competitions whose competition link the caller
	// holds: every name in them is revealed.
	competitions map[string]bool
	// competitors holds, per competition, the participant ids whose own link
	// the caller holds: those competitors' names are revealed (a team's link
	// reveals its members' names, E7).
	competitors map[string]map[string]bool
}

// organiserAudience is the signed-in organiser's audience.
func organiserAudience() audience { return audience{organiser: true} }

// isOrganiser reports whether the caller is the signed-in organiser.
func (a audience) isOrganiser() bool { return a.organiser }

// revealsAll reports whether the caller sees every name in compID: the
// organiser, or the holder of that competition's link.
func (a audience) revealsAll(compID string) bool {
	return a.organiser || a.competitions[compID]
}

// revealedCompetitors is the set of participant ids in compID whose own link
// the caller holds (nil when none). Read only.
func (a audience) revealedCompetitors(compID string) map[string]bool {
	return a.competitors[compID]
}

// key names the whole audience, per-competitor links included, sorted: two
// callers with the same key are answered the same bytes, so it is the part a
// single-flight key adds.
func (a audience) key() string {
	if a.organiser {
		return audienceOrganiser
	}
	parts := a.competitionParts()
	compIDs := make([]string, 0, len(a.competitors))
	for id, set := range a.competitors {
		if len(set) > 0 {
			compIDs = append(compIDs, id)
		}
	}
	sort.Strings(compIDs)
	for _, id := range compIDs {
		pids := make([]string, 0, len(a.competitors[id]))
		for pid := range a.competitors[id] {
			pids = append(pids, pid)
		}
		sort.Strings(pids)
		parts = append(parts, "p:"+id+"="+strings.Join(pids, "+"))
	}
	if len(parts) == 0 {
		return audiencePublic
	}
	return audienceReveal + "|" + strings.Join(parts, ",")
}

// keyFor names the audience as one competition's answer sees it: the organiser,
// the holder of that competition's link, the holders of the same set of
// competitors' own links in it, or the public (every link that opens another
// competition counts for nothing here). It is the part a single-flight key adds
// for a read of that one competition.
func (a audience) keyFor(compID string) string {
	if a.organiser {
		return audienceOrganiser
	}
	if a.competitions[compID] {
		return audienceReveal + "|c:" + compID
	}
	pids := make([]string, 0, len(a.competitors[compID]))
	for pid := range a.competitors[compID] {
		pids = append(pids, pid)
	}
	if len(pids) == 0 {
		return audiencePublic
	}
	sort.Strings(pids)
	return audienceReveal + "|p:" + compID + "=" + strings.Join(pids, "+")
}

func (a audience) competitionParts() []string {
	parts := make([]string, 0, len(a.competitions))
	for id, ok := range a.competitions {
		if ok {
			parts = append(parts, "c:"+id)
		}
	}
	sort.Strings(parts)
	return parts
}

// judge works out who a public read is answered for. Every answer it judges
// varies by audience, so it is marked no-store (and Vary on both
// credentials) before anything else is written. ok is false when the
// response has been written: 401 for a password sent but wrong, 500 when the
// password could not be checked.
//
// A self-run tournament hides no names (hides), so a password that is wrong
// there is not refused: a device still holding an old one reads the public
// pages as it did before names could be hidden, and is answered the public
// view. A password that is right is the organiser's in either mode.
//
// Reveal keys are judged only for a caller that is not the organiser. An
// invalid, expired or wrong-kind key is dropped here, before any key is
// formed from the audience, so it folds into the public.
func (p *publicNames) judge(c *gin.Context) (audience, bool) {
	return p.judgeCredentials(c, false)
}

// judgeTicket judges the credentials of an event-stream ticket request. The
// ticket exists for the credentials it is asked with, so a wrong password is
// refused in every mode: the page is told to ask for the password again.
func (p *publicNames) judgeTicket(c *gin.Context) (audience, bool) {
	return p.judgeCredentials(c, true)
}

// judgeCredentials is the judge behind both; refuseSelfRun says whether a
// wrong password is refused in a self-run tournament too.
func (p *publicNames) judgeCredentials(c *gin.Context, refuseSelfRun bool) (audience, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Vary", "X-Tournament-Password, "+revealKeyHeader)

	if password := c.GetHeader("X-Tournament-Password"); password != "" {
		valid, err := p.verifier.Verify(password)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
			return audience{}, false
		}
		if valid {
			return organiserAudience(), true
		}
		if refuseSelfRun || !p.selfRun() {
			respondInvalidPassword(c)
			return audience{}, false
		}
	}
	return p.revealAudience(c.GetHeader(revealKeyHeader)), true
}

// revealAudience judges an X-Reveal-Key header. Only a key that reveals
// something right now counts: its competition hides names, its links are
// open (state.RevealOpen: the kind gives links and the expiry has not
// passed), and the key is the competition's key (a competition link) or one
// of its competitors' keys (per-person links), compared in constant time.
// Anything else is dropped without a word.
func (p *publicNames) revealAudience(header string) audience {
	entries := parseRevealKeys(header)
	if len(entries) == 0 {
		return audience{}
	}
	// A missing tournament (nil, no tournament.md yet) is no refusal: a
	// competition's own RevealUntil still opens its links (state.RevealOpen).
	t, err := p.store.LoadTournament()
	if err != nil {
		return audience{}
	}
	now := p.now()
	var aud audience
	for _, e := range entries {
		comp, err := p.store.LoadCompetition(e.compID)
		if err != nil || comp == nil || !comp.HidesNames() || !state.RevealOpen(comp, t, now) {
			continue
		}
		switch comp.PublicNames {
		case state.PublicNamesCompetition:
			want := state.CompetitionRevealKey(comp)
			if want != "" && subtle.ConstantTimeCompare([]byte(e.key), []byte(want)) == 1 {
				if aud.competitions == nil {
					aud.competitions = map[string]bool{}
				}
				aud.competitions[comp.ID] = true
			}
		case state.PublicNamesPerson:
			m, err := p.masks.get(comp.ID)
			if err != nil {
				// The mask fails closed and knows no key: this key
				// reveals nothing until the files read again.
				slog.Warn("mobile-app: reveal key not judged", "competition", comp.ID, "err", err)
			}
			if pid := m.competitorForKey(e.key); pid != "" {
				if aud.competitors == nil {
					aud.competitors = map[string]map[string]bool{}
				}
				if aud.competitors[comp.ID] == nil {
					aud.competitors[comp.ID] = map[string]bool{}
				}
				aud.competitors[comp.ID][pid] = true
			}
		}
	}
	return aud
}

// revealKeyEntry is one "<compId>:<key>" entry of an X-Reveal-Key header.
type revealKeyEntry struct {
	compID string
	key    string
}

// parseRevealKeys reads an X-Reveal-Key header into its well-formed entries:
// a valid competition id, a colon and a 64-character lowercase hex key. A
// header past maxRevealHeaderBytes is ignored whole; entries past
// maxRevealKeys, malformed entries and repeats are dropped.
func parseRevealKeys(header string) []revealKeyEntry {
	header = strings.TrimSpace(header)
	if header == "" || len(header) > maxRevealHeaderBytes {
		return nil
	}
	var out []revealKeyEntry
	seen := map[revealKeyEntry]bool{}
	for _, raw := range strings.Split(header, ",") {
		if len(out) == maxRevealKeys {
			break
		}
		compID, key, ok := strings.Cut(strings.TrimSpace(raw), ":")
		if !ok || state.ValidateCompetitionID(compID) != nil || !isLowerHex(key, revealKeyHexLen) {
			continue
		}
		e := revealKeyEntry{compID: compID, key: key}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// canonicalRevealKeys is a header's well-formed entries alone, joined as
// the header writes them, or "" when it has none. What an event-stream
// ticket keeps, so a malformed header earns no ticket and a stored one never
// holds more than maxRevealKeys entries.
func canonicalRevealKeys(header string) string {
	entries := parseRevealKeys(header)
	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = e.compID + ":" + e.key
	}
	return strings.Join(parts, ",")
}

// isLowerHex reports whether s is exactly n lowercase hex characters, the
// shape every derived reveal key has.
func isLowerHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return false
		}
	}
	return true
}

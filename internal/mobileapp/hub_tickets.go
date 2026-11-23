package mobileapp

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// codeEventTicketInvalid: /api/events was opened with a ticket that is
// spent, expired or unknown (401). The client asks for a new ticket.
const codeEventTicketInvalid = "event_ticket_invalid"

// codeEventTicketNotNeeded: a ticket was asked for with no password and no
// reveal key (400). The public stream needs none.
const codeEventTicketNotNeeded = "event_ticket_not_needed"

// eventTicketTTL is how long a ticket waits to be used.
const eventTicketTTL = 60 * time.Second

// maxEventTickets bounds the tickets waiting to be used. Past it, expired
// ones are dropped; while every one is still live, no new ticket is given
// (503), so a flood of ticket requests cannot grow the map without bound.
const maxEventTickets = 10000

// organiserTicketReserve is the share of maxEventTickets only an organiser's
// ticket may take: a caller holding a reveal key can fill the rest, but
// never the room the organiser's console needs to open its stream.
const organiserTicketReserve = 1000

// maxIdleEventTickets bounds the waiting tickets of callers whose reveal keys
// reveal nothing now (a key of a closed or expired link, or one made up: a
// well-formed key is ticketed whether or not it is the competition's, since a
// later change of an option is judged against it). Past it, such a caller is
// refused, so an anonymous flood of invented keys cannot take the room phones
// holding a real key need.
const maxIdleEventTickets = 1000

// eventTicketBytes is the size of a ticket's random part (hex on the wire).
const eventTicketBytes = 32

// eventTickets holds the event-stream tickets handed out and not yet used.
type eventTickets struct {
	mu sync.Mutex
	m  map[string]eventTicket
	// passwordEpoch counts the admin password changes. An organiser's
	// ticket carries the count read before its password was judged and is
	// redeemed only while that is still the count, so a ticket judged
	// against a password that has since changed never opens a stream.
	passwordEpoch uint64
	// now is the clock tickets are timed by; nil is time.Now.
	now func() time.Time
}

type eventTicket struct {
	sub     sseSubscriber
	expires time.Time
	// idle: the subscriber is no organiser and holds no key that reveals
	// anything now (counted against maxIdleEventTickets).
	idle bool
	// epoch is the passwordEpoch read before the caller was judged.
	epoch uint64
}

// epochNow is the current passwordEpoch, read before a caller is judged.
func (t *eventTickets) epochNow() uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.passwordEpoch
}

// passwordChanged counts a change of the admin password and drops the
// organiser tickets still waiting: each was judged against the old password.
// Called from Broadcast's EventPasswordReset, beside ending the organiser
// streams already open.
func (t *eventTickets) passwordChanged() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.passwordEpoch++
	for k, e := range t.m {
		if e.sub.aud.isOrganiser() {
			delete(t.m, k)
		}
	}
}

// idleTickets counts the waiting tickets that reveal nothing. Call it with
// the lock held.
func (t *eventTickets) idleTickets() int {
	n := 0
	for _, e := range t.m {
		if e.idle {
			n++
		}
	}
	return n
}

func (t *eventTickets) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// errTooManyEventTickets: maxEventTickets live tickets are waiting.
var errTooManyEventTickets = errors.New("too many event-stream tickets waiting")

// issueTicket hands out a single-use ticket for sub, valid eventTicketTTL.
// judgedAt is the passwordEpoch read before sub's credentials were judged.
func (h *Hub) issueTicket(judgedAt uint64, sub sseSubscriber) (string, time.Time, error) {
	var b [eventTicketBytes]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", time.Time{}, fmt.Errorf("event ticket: %w", err)
	}
	ticket := hex.EncodeToString(b[:])
	t := &h.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.clock()
	if t.m == nil {
		t.m = map[string]eventTicket{}
	}
	limit := maxEventTickets
	if !sub.aud.isOrganiser() {
		limit -= organiserTicketReserve
	}
	idle := !sub.aud.isOrganiser() && sub.aud.key() == audiencePublic
	expires := now.Add(eventTicketTTL)
	full := func() bool {
		return len(t.m) >= limit || (idle && t.idleTickets() >= maxIdleEventTickets)
	}
	if full() {
		for k, old := range t.m {
			if !now.Before(old.expires) {
				delete(t.m, k)
			}
		}
		if full() {
			return "", time.Time{}, errTooManyEventTickets
		}
	}
	t.m[ticket] = eventTicket{sub: sub, expires: expires, idle: idle, epoch: judgedAt}
	return ticket, expires, nil
}

// redeemTicket spends ticket: the subscriber it was handed out for, and
// whether it was waiting and unexpired, and, for an organiser's, judged
// against the admin password in force now. A ticket is gone once asked for,
// whatever the answer.
func (h *Hub) redeemTicket(ticket string) (sseSubscriber, bool) {
	t := &h.tickets
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[ticket]
	if !ok {
		return sseSubscriber{}, false
	}
	delete(t.m, ticket)
	if !t.clock().Before(e.expires) {
		return sseSubscriber{}, false
	}
	if e.sub.aud.isOrganiser() && e.epoch != t.passwordEpoch {
		return sseSubscriber{}, false
	}
	return e.sub, true
}

// respondTicketNotNeeded answers 400 event_ticket_not_needed: the caller's
// stream is the public one, opened with no ticket.
func respondTicketNotNeeded(c *gin.Context) {
	c.JSON(http.StatusBadRequest, gin.H{
		"error": "A stream with no password or reveal key needs no ticket. Open /api/events without one.",
		"code":  codeEventTicketNotNeeded,
	})
}

// HandleEventTicket is POST /api/events/ticket: the caller's credentials,
// sent as headers (X-Tournament-Password, X-Reveal-Key), are judged as for a
// public read, and a single-use ticket bound to that audience is answered,
// {ticket, expiresAt}, to open /api/events?ticket=... within eventTicketTTL.
// A password sent but wrong is 401, as on every public read. A reveal key
// unlocks names on the stream only, never a write. A caller with neither is
// the public, which opens the stream with no ticket: 400. So is every caller
// of a self-run tournament, which hides no names (publicNames.hides): its
// console opens the public stream as it always did, and is never refused a
// ticket for a stale password or a full ticket map.
func (h *Hub) HandleEventTicket() gin.HandlerFunc {
	return func(c *gin.Context) {
		names := h.names
		if names == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "event-stream tickets are not available"})
			return
		}
		if names.selfRun() {
			respondTicketNotNeeded(c)
			return
		}
		// Read before the password is judged: a change landing between the
		// two leaves this ticket with the old count, and it is refused.
		judgedAt := h.tickets.epochNow()
		aud, ok := names.judge(c)
		if !ok {
			return
		}
		sub := sseSubscriber{aud: aud}
		if !aud.isOrganiser() {
			// Kept even when it reveals nothing now: a later change of a
			// competition's option is judged against it again.
			sub.reveal = canonicalRevealKeys(c.GetHeader(revealKeyHeader))
			if sub.reveal == "" {
				// A caller with no credential, or with a header holding no
				// well-formed reveal key, is the public, and the public
				// stream is opened with no ticket. Handing one out would let
				// anyone fill the waiting tickets and lock the organiser out.
				respondTicketNotNeeded(c)
				return
			}
		}
		ticket, expires, err := h.issueTicket(judgedAt, sub)
		if errors.Is(err, errTooManyEventTickets) {
			c.Header("Retry-After", "5")
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Too many event-stream tickets are waiting; retry shortly"})
			return
		}
		if err != nil {
			slog.Error("mobile-app: event-stream ticket not issued", "err", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"ticket": ticket, "expiresAt": expires.UnixMilli()})
	}
}

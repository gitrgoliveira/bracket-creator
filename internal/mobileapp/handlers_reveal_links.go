package mobileapp

import (
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// revealLinksResponse is GET /api/competitions/:id/reveal-links: what the
// organiser's settings need to show the competition link. Keys are derived
// from the competition's secret, which never leaves the server. A
// competitor's own key is not in it: the Participants tab's Reveal link reads
// it from GET .../reveal-links/:participantId when the organiser taps it, so
// the server logs each one it hands out.
type revealLinksResponse struct {
	// PublicNames is the competition's option.
	PublicNames string `json:"publicNames"`
	// RevealUntil is the stored expiry ("" for the end of the tournament).
	RevealUntil string `json:"revealUntil"`
	// ExpiresAt is the expiry in force, in state.RevealUntilLayout and the
	// server's time zone; "" when none can be worked out (no tournament
	// date), and the links then reveal nothing.
	ExpiresAt string `json:"expiresAt"`
	// Open reports whether the links reveal names right now.
	Open bool `json:"open"`
	// CompetitionKey is the key of the one competition link; "" unless the
	// option is per competition.
	CompetitionKey string `json:"competitionKey"`
}

// revealLinkCompetitorResponse is GET
// /api/competitions/:id/reveal-links/:participantId: one competitor's own
// key, with the expiry the Reveal link sheet shows beside its QR code.
type revealLinkCompetitorResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Key       string `json:"key"`
	ExpiresAt string `json:"expiresAt"`
	Open      bool   `json:"open"`
}

// codeNoRevealLink answers a competitor's link read when the competition has
// no link per person, or holds no competitor with that id.
const codeNoRevealLink = "no_reveal_link"

// revealExpiryText is the expiry in force in state.RevealUntilLayout, "" when
// none can be worked out (no tournament date).
func revealExpiryText(comp *state.Competition, tourn *state.Tournament, now time.Time) string {
	if expiry, ok := state.RevealExpiry(comp, tourn, now.Location()); ok {
		return expiry.Format(state.RevealUntilLayout)
	}
	return ""
}

// RegisterRevealLinkHandlers registers the organiser's reads of a
// competition's reveal links. They are admin routes (the password gate), and
// in self-run they stay organiser-only (isSelfRunMainGatedConfigRoute): the
// keys they answer unlock names on the public pages.
func RegisterRevealLinkHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/competitions/:id/reveal-links", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, ok := loadExistingCompetition(c, store, id)
		if !ok {
			return
		}
		tourn, err := store.LoadTournament()
		if err != nil {
			internalError(c, err)
			return
		}
		now := time.Now()
		// The key unlocks names: never kept by a cache on the way.
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, revealLinksResponse{
			PublicNames:    comp.PublicNames,
			RevealUntil:    comp.RevealUntil,
			ExpiresAt:      revealExpiryText(comp, tourn, now),
			Open:           state.RevealOpen(comp, tourn, now),
			CompetitionKey: state.CompetitionRevealKey(comp),
		})
	})

	// One competitor's own link, read when the organiser taps Reveal link
	// beside their name. Each read is logged, so the server's log says when a
	// competitor's link was shown, and to which address.
	r.GET("/competitions/:id/reveal-links/:participantId", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, ok := loadExistingCompetition(c, store, id)
		if !ok {
			return
		}
		tourn, err := store.LoadTournament()
		if err != nil {
			internalError(c, err)
			return
		}
		pid := c.Param("participantId")
		var key, name string
		if comp.PublicNames == state.PublicNamesPerson {
			players, err := store.LoadParticipantsOpt(id, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
			if err != nil {
				internalError(c, err)
				return
			}
			for _, p := range players {
				if p.ID == pid {
					key, name = state.CompetitorRevealKey(comp, p.ID), p.Name
					break
				}
			}
		}
		if key == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "this competition has no reveal link for that competitor", "code": codeNoRevealLink})
			return
		}
		now := time.Now()
		log.Printf("mobileapp: reveal link shown for competitor %q (%s) of competition %q, to %s", name, pid, id, c.Request.RemoteAddr)
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, revealLinkCompetitorResponse{
			ID:        pid,
			Name:      name,
			Key:       key,
			ExpiresAt: revealExpiryText(comp, tourn, now),
			Open:      state.RevealOpen(comp, tourn, now),
		})
	})
}

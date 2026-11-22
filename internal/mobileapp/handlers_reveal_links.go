package mobileapp

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// revealLinksResponse is GET /api/competitions/:id/reveal-links: what the
// organiser's settings and participant rows need to show the reveal QR codes
// and Copy link buttons. Keys are derived from the competition's secret,
// which never leaves the server.
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
	// Competitors holds each competitor's own key; empty unless the option
	// is per person. A competitor with no participant id has no key and is
	// left out.
	Competitors []revealLinkCompetitor `json:"competitors"`
}

// revealLinkCompetitor is one competitor's own reveal key.
type revealLinkCompetitor struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dojo string `json:"dojo"`
	Key  string `json:"key"`
}

// RegisterRevealLinkHandlers registers the organiser's read of a
// competition's reveal links. It is an admin route (the password gate), and
// in self-run it stays organiser-only (isSelfRunMainGatedConfigRoute): the
// keys it answers unlock names on the public pages.
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
		answer := revealLinksResponse{
			PublicNames:    comp.PublicNames,
			RevealUntil:    comp.RevealUntil,
			Open:           state.RevealOpen(comp, tourn, now),
			CompetitionKey: state.CompetitionRevealKey(comp),
			Competitors:    []revealLinkCompetitor{},
		}
		if expiry, ok := state.RevealExpiry(comp, tourn, now.Location()); ok {
			answer.ExpiresAt = expiry.Format(state.RevealUntilLayout)
		}
		if comp.PublicNames == state.PublicNamesPerson {
			players, err := store.LoadParticipantsOpt(id, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{HasIDs: comp.ParticipantIDsHint()})
			if err != nil {
				internalError(c, err)
				return
			}
			for _, p := range players {
				key := state.CompetitorRevealKey(comp, p.ID)
				if key == "" {
					continue
				}
				answer.Competitors = append(answer.Competitors, revealLinkCompetitor{ID: p.ID, Name: p.Name, Dojo: p.Dojo, Key: key})
			}
		}
		// The keys unlock names: never kept by a cache on the way.
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, answer)
	})
}

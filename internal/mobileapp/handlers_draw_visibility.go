package mobileapp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The refusals of Publish draw and Unpublish (bc-drpb). Each body carries the
// code the page branches on and the sentence it shows as it is.
const (
	// drawNotReadyCode: the competition is not waiting to start (setup has
	// no draw; a started, completed or invalid competition follows the
	// public rules of a running one), so there is no draft to show or hide.
	drawNotReadyCode    = "draw_not_ready"
	drawNotReadyMessage = "The draw can be published or hidden only while it waits to start."

	// drawVisibilitySelfRunCode: a self-run tournament has no hidden drafts
	// (state.Competition.DrawHiddenIn judges the mode at read time), so a
	// flip there would store a state nobody can see.
	drawVisibilitySelfRunCode    = "draw_visibility_self_run"
	drawVisibilitySelfRunMessage = "A self-run tournament shows its draw as soon as it is made."
)

// registerDrawVisibilityHandlers registers Publish draw and Unpublish, the two
// organiser routes that flip whether a draft's draw is kept off the public
// pages (state.Competition.DrawHidden). Both take no body and answer 200 with
// the competition record as stored. A competition already in the asked state
// is answered 200 with nothing written and nothing broadcast, so a repeated
// tap is harmless. Not draw-ready: 409 draw_not_ready. A self-run tournament:
// 409 draw_visibility_self_run for the organiser; an anonymous self-run caller
// never gets this far, because both routes are in
// isSelfRunMainGatedConfigRoute and the gate answers 401. They are direct
// organiser writes like Generate and Discard draw, not outbox doors: two
// devices racing, the later arrival wins.
func registerDrawVisibilityHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	r.POST("/competitions/:id/publish-draw", func(c *gin.Context) {
		setDrawVisibility(c, store, eng, hub, false)
	})
	r.POST("/competitions/:id/unpublish-draw", func(c *gin.Context) {
		setDrawVisibility(c, store, eng, hub, true)
	})
}

// setDrawVisibility is the body both routes share: hidden true is Unpublish,
// false is Publish. The write lands (engine.SetDrawHidden, one
// UpdateCompetitionChanged) before the answer, and only a write that changed
// the stored config broadcasts draw_visibility_changed, which the public
// pages refetch the named competition on.
func setDrawVisibility(c *gin.Context, store *state.Store, eng *engine.Engine, hub *Hub, hidden bool) {
	id, ok := requireValidCompID(c)
	if !ok {
		return
	}
	t, err := store.LoadTournament()
	if err != nil {
		internalError(c, err)
		return
	}
	if t.IsSelfRun() {
		c.JSON(http.StatusConflict, gin.H{"code": drawVisibilitySelfRunCode, "error": drawVisibilitySelfRunMessage})
		return
	}
	changed, err := eng.SetDrawHidden(id, hidden)
	if err != nil {
		if errors.Is(err, engine.ErrDrawNotReady) {
			c.JSON(http.StatusConflict, gin.H{"code": drawNotReadyCode, "error": drawNotReadyMessage})
			return
		}
		respondEngineError(c, err)
		return
	}
	comp, err := store.LoadCompetition(id)
	if err != nil {
		internalError(c, err, "draw visibility saved but failed to load updated state")
		return
	}
	if comp == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "draw visibility saved but competition no longer exists"})
		return
	}
	if changed {
		hub.Broadcast(EventDrawVisibilityChanged, gin.H{"competitionId": id, "hidden": hidden})
	}
	c.JSON(http.StatusOK, comp)
}

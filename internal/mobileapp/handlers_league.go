package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
)

// RegisterPublicLeagueHandlers registers the public league standings read.
//
// Leagues are distinct from pools: this dedicated endpoint feeds the league
// standings UI so it never consumes the pool standings path (the mislabelled
// "pools" tab). Mirrors RegisterPublicSwissHandlers (public read, no auth).
//
//	GET /competitions/:id/league/standings
func RegisterPublicLeagueHandlers(r *gin.RouterGroup, eng *engine.Engine) {
	r.GET("/competitions/:id/league/standings", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		standings, err := eng.LeagueStandings(id)
		if err != nil {
			respondEngineError(c, err)
			return
		}
		c.JSON(http.StatusOK, standings)
	})
}

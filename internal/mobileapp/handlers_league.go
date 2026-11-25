package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterPublicLeagueHandlers registers the public league standings read.
//
// Leagues are distinct from pools: this dedicated endpoint feeds the league
// standings UI so it never consumes the pool standings path (the mislabelled
// "pools" tab). Mirrors RegisterPublicSwissHandlers (public read, no auth).
//
//	GET /competitions/:id/league/standings
//
// This form mounts the read with no judge, every caller reading the names as
// stored (tests mount it alone this way); NewRouterWithHub calls
// registerPublicLeagueHandlers with the router's shared judge.
func RegisterPublicLeagueHandlers(r *gin.RouterGroup, eng *engine.Engine) {
	registerPublicLeagueHandlers(r, eng, nil)
}

// registerPublicLeagueHandlers wires the league standings read. It judges its
// caller first (publicReadView: a password sent but wrong is 401, the answer is
// no-store) and answers for that audience: in a competition that hides names
// (bc-hnam) the public reads each competitor's number where the name is (a
// competitor whose own link the caller holds keeps theirs), and a team keeps
// its name. The answer stays a bare array of rows, so it carries no
// namesHidden marker: a masked row reads as a number, never as a name to save.
// A draft whose draw is hidden from the caller (maskView.drawHidden, bc-drpb)
// is answered as the same competition in setup: no standings.
func registerPublicLeagueHandlers(r *gin.RouterGroup, eng *engine.Engine, names *publicNames) {
	r.GET("/competitions/:id/league/standings", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		v, comp, ok := publicReadViewOf(c, names, id)
		if !ok {
			return
		}
		if v.drawHidden && comp.Format == state.CompFormatLeague {
			// A hidden draw is answered as setup: a league before its draw
			// has no standings. Any other competition is the engine's 404.
			c.JSON(http.StatusOK, []state.PlayerStanding{})
			return
		}
		standings, err := eng.LeagueStandings(id)
		if err != nil {
			respondEngineError(c, err)
			return
		}
		if v.namesHidden() {
			// A copy: the engine's rows are left as they were.
			standings = append([]state.PlayerStanding(nil), standings...)
			v.standings(standings)
		}
		c.JSON(http.StatusOK, standings)
	})
}

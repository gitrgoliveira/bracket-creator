package mobileapp

import (
	"net/http"
	"sort"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterMatchHistoryHandler serves a match's history (bc-mrgc, operator
// ruling 2026-10-03: "Nothing should be dropped. All events must be
// ordered."): every write that reached the match, applied or not, with the
// values of any change a newer one to the same group kept from being applied.
// The score editors show it in a closed-by-default "History" disclosure.
//
// GET /api/competitions/:id/matches/:mid/history answers the entries in stamp
// order, oldest first (matchHistoryInStampOrder), [] when the match has none,
// and 404 for an unknown competition or match. It is an organiser view: in a
// self-run tournament it stays behind the main password
// (isSelfRunMainGatedConfigRoute).
func RegisterMatchHistoryHandler(r *gin.RouterGroup, store MatchHistoryStore) {
	r.GET("/competitions/:id/matches/:mid/history", func(c *gin.Context) {
		id, mid := c.Param("id"), c.Param("mid")
		if err := state.ValidateCompetitionID(id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err)
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		_, found, err := store.MatchStatusByID(id, mid)
		if err != nil {
			internalError(c, err)
			return
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "match not found"})
			return
		}
		entries, err := store.LoadMatchHistory(id, mid)
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, matchHistoryInStampOrder(entries))
	})
}

// matchHistoryInStampOrder orders a match's history by when each change was
// MADE (its stamp), oldest first, the order the merge applied them in; the
// file holds them in arrival order. An unstamped write has no time of its own
// and is placed at the time the server took it. Ties keep arrival order.
// Never nil, so an empty history answers [].
func matchHistoryInStampOrder(entries []state.MatchHistoryEntry) []state.MatchHistoryEntry {
	out := make([]state.MatchHistoryEntry, len(entries))
	copy(out, entries)
	at := func(e state.MatchHistoryEntry) int64 {
		if e.Stamp > 0 {
			return e.Stamp
		}
		return e.ReceivedAt
	}
	sort.SliceStable(out, func(i, j int) bool { return at(out[i]) < at(out[j]) })
	return out
}

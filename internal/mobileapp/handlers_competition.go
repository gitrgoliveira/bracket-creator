package mobileapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// slugifyID derives a valid competition ID from a name: lowercase, non-alphanumeric
// runs become a single hyphen, leading/trailing hyphens stripped, max 64 chars.
func slugifyID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var sb strings.Builder
	prevHyphen := true
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			sb.WriteRune('-')
			prevHyphen = true
		}
	}
	result := strings.TrimRight(sb.String(), "-")
	if len(result) > 64 {
		result = strings.TrimRight(result[:64], "-")
	}
	return result
}

// saveCompetitionWithPlayers persists the competition config and, when players
// are present, saves participants and extracts seed assignments.
// Returns (true, nil) when the on-disk content changed, so callers can decide
// whether to broadcast.
func saveCompetitionWithPlayers(comp *state.Competition, store *state.Store) (bool, error) {
	if len(comp.Players) > 0 {
		comp.HasParticipantIDs = true // participants.csv always written with UUID IDs
	}
	changed, err := store.SaveCompetitionChanged(comp)
	if err != nil {
		return false, err
	}
	if len(comp.Players) == 0 {
		return changed, nil
	}
	if err := store.SaveParticipants(comp.ID, comp.Players); err != nil {
		return false, fmt.Errorf("failed to save participants: %w", err)
	}
	assignments := extractSeeds(comp.Players)
	if err := store.SaveSeeds(comp.ID, assignments); err != nil {
		fmt.Printf("Warning: failed to save seeds: %v\n", err)
	}
	return changed, nil
}

func extractSeeds(players []helper.Player) []domain.SeedAssignment {
	var out []domain.SeedAssignment
	for _, p := range players {
		if p.Seed > 0 {
			out = append(out, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
		}
	}
	return out
}

func checkUniqueCompName(store *state.Store, name, excludeID string) error {
	ids, _ := store.ListCompetitions()
	for _, existingID := range ids {
		if existingID == excludeID {
			continue
		}
		existing, err := store.LoadCompetition(existingID)
		if err == nil && existing != nil && strings.EqualFold(existing.Name, name) {
			return fmt.Errorf("competition name %q already exists", name)
		}
	}
	return nil
}

func RegisterCompetitionHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	r.GET("/competitions", func(c *gin.Context) {
		ids, err := store.ListCompetitions()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		comps := make([]*state.Competition, 0)
		for _, id := range ids {
			comp, err := store.LoadCompetition(id)
			if err == nil && comp != nil {
				comps = append(comps, comp)
			}
		}
		c.JSON(http.StatusOK, comps)
	})

	r.POST("/competitions", func(c *gin.Context) {
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		comp.Name = strings.TrimSpace(comp.Name)
		// Trim NumberPrefix too so untrimmed input from the SETTINGS edit
		// path can't land as "  A" / participants becoming "  A1" / etc.
		// Mirrors the comp.Name trim above (and the frontend trim in
		// admin_competition.jsx saveNow + admin_setup.jsx create).
		comp.NumberPrefix = strings.TrimSpace(comp.NumberPrefix)
		if err := checkUniqueCompName(store, comp.Name, ""); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if comp.ID == "" {
			comp.ID = slugifyID(comp.Name)
			if comp.ID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "competition ID is required (could not derive one from name)"})
				return
			}
		}

		if _, err := saveCompetitionWithPlayers(&comp, store); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, comp)
	})

	r.GET("/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		c.JSON(http.StatusOK, comp)
	})

	r.PUT("/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		comp.ID = id // ensure ID matches URL
		comp.Name = strings.TrimSpace(comp.Name)
		// See POST handler comment — same trim is needed here so the
		// SETTINGS edit path can't persist whitespace-padded prefixes.
		comp.NumberPrefix = strings.TrimSpace(comp.NumberPrefix)

		if err := checkUniqueCompName(store, comp.Name, id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		changed, err := saveCompetitionWithPlayers(&comp, store)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, comp)
	})

	r.DELETE("/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		// If the config loads cleanly, gate on status. If it doesn't load
		// (corrupt or unparseable config.md), fall through to delete so the
		// operator can recover from a broken competition.
		if comp, err := store.LoadCompetition(id); err == nil && comp != nil {
			switch comp.Status {
			case state.CompStatusPools, state.CompStatusPlayoffs:
				c.JSON(http.StatusConflict, gin.H{"error": "competition is in progress; mark it invalid before deleting"})
				return
			}
		}
		if err := store.DeleteCompetition(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})

	r.POST("/competitions/:id/invalidate", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if comp.Status != state.CompStatusPools && comp.Status != state.CompStatusPlayoffs {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("only in-progress competitions can be invalidated (current status: %q)", comp.Status)})
			return
		}
		comp.Status = state.CompStatusInvalid
		if _, err := saveCompetitionWithPlayers(comp, store); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, comp)
	})

	r.GET("/competitions/:id/reserved-slots", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		slots, err := store.LoadReservedSlots(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, slots)
	})

	r.POST("/competitions/:id/reserved-slots", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var req struct {
			SourceCompID string `json:"sourceCompID"`
			SourceRank   int    `json:"sourceRank"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.SourceCompID == "" || req.SourceRank < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sourceCompID and sourceRank (>= 1) are required"})
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		slot, err := store.AddReservedSlot(id, req.SourceCompID, req.SourceRank, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, slot)
	})

	r.DELETE("/competitions/:id/reserved-slots/:slotID", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		slotID := c.Param("slotID")
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if err := store.RemoveReservedSlot(id, slotID, comp.WithZekkenName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})

	r.POST("/competitions/:id/start", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if err := eng.StartCompetition(id); err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			switch {
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validation):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			}
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "competition started but failed to load updated state: " + err.Error()})
			return
		}

		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": id})

		// A pools competition that generated zero matches (e.g. single
		// participant) has nothing to score, so trip the auto-complete check
		// at start time. The non-zero case will trip via score handlers.
		// Same sanitized-header contract as tryAutoCompletePools — see
		// AutoCompleteErrorHeader/Value in hub.go.
		if autoCompleted, err := eng.MaybeAutoCompletePools(id); err != nil {
			log.Printf("MaybeAutoCompletePools(%s) after start: %v", id, err)
			c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		} else if autoCompleted {
			hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": id})
			// Reflect the auto-complete in the response body so the caller doesn't
			// see a stale "pools" status. The persisted file is already updated.
			comp.Status = state.CompStatusComplete
		}

		c.JSON(http.StatusOK, comp)
	})

	r.POST("/competitions/:id/complete", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if comp.Status != state.CompStatusPools && comp.Status != state.CompStatusPlayoffs {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("competition cannot be completed from status %q", comp.Status)})
			return
		}
		comp.Status = state.CompStatusComplete
		if _, err := saveCompetitionWithPlayers(comp, store); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, comp)
	})

	r.GET("/competitions/:id/export", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		data, err := eng.ExportCompetitionXlsx(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		filename := fmt.Sprintf("bracket-%s.xlsx", id)
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	})

	r.PUT("/competitions/:id/pools/:poolId/override-rank", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		poolId := c.Param("poolId")
		var req struct {
			PlayerName string `json:"playerName"`
			Rank       int    `json:"rank"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Defense-in-depth: the JS client already guards isNaN/<=0, but a stale
		// or hand-crafted request could persist garbage rank values. Reject
		// non-positive ranks (and anything implausibly large — no real pool
		// has 1000+ participants). Trim whitespace from the player name so
		// "   " doesn't slip through the empty check and so padded names
		// don't create keys that miss later lookups.
		playerName := strings.TrimSpace(req.PlayerName)
		if playerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "playerName is required"})
			return
		}
		if req.Rank <= 0 || req.Rank > 1000 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rank must be a positive integer ≤ 1000"})
			return
		}

		changed, err := store.SaveRankOverrideChanged(id, poolId, playerName, req.Rank)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/schedule", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var entries []state.ScheduleEntry
		if err := c.ShouldBindJSON(&entries); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		changed, err := store.SaveScheduleChanged(id, entries)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventScheduleUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	r.POST("/competitions/:id/playoffs", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		src, err := store.LoadCompetition(id)
		if err != nil || src == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "source competition not found"})
			return
		}

		if src.Format != state.CompFormatPools {
			c.JSON(http.StatusBadRequest, gin.H{"error": "source competition must use pools format"})
			return
		}

		// Calculate number of pools to determine how many reserved slots we need.
		parts, _ := store.LoadParticipants(id, src.WithZekkenName)
		poolSize := src.PoolSize
		if poolSize <= 0 {
			poolSize = 3 // default
		}
		numPools := (len(parts) + poolSize - 1) / poolSize
		winnersPerPool := src.PoolWinners
		if winnersPerPool <= 0 {
			winnersPerPool = 2 // default
		}
		totalWinners := numPools * winnersPerPool

		playoff := state.Competition{
			Name:           src.Name + " - Playoffs",
			Format:         state.CompFormatPlayoffs,
			Courts:         src.Courts,
			WithZekkenName: src.WithZekkenName,
			NumberPrefix:   src.NumberPrefix,
			StartTime:      src.StartTime,
			Status:         state.CompStatusSetup,
		}
		playoff.ID = slugifyID(playoff.Name)

		if _, err := store.SaveCompetitionChanged(&playoff); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Link reserved slots (this will also add placeholder participants)
		for i := 1; i <= totalWinners; i++ {
			if _, err := store.AddReservedSlot(playoff.ID, id, i, playoff.WithZekkenName); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to add reserved slot %d: %v", i, err)})
				return
			}
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, playoff)
	})

	r.DELETE("/competitions/:id/overrides", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		changed, err := store.ResetOverridesChanged(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusNoContent)
	})
}

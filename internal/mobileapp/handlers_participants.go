package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterParticipantHandlers(r *gin.RouterGroup, store *state.Store, hub Broadcaster) {
	r.GET("/competitions/:id/participants", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		players, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, players)
	})

	r.POST("/competitions/:id/participants", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Status != state.CompStatusSetup && comp.Status != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify participants after competition has started"})
			return
		}

		var req struct {
			Players []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Dojo        string   `json:"dojo"`
				Metadata    []string `json:"metadata"`
				Tag         string   `json:"tag"`
			} `json:"players"`
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Dojo        string   `json:"dojo"`
			Metadata    []string `json:"metadata"`
			DanGrade    string   `json:"danGrade"`
			Tag         string   `json:"tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Reject an empty body up front so it can't fall through to the
		// roster-replace path below and silently wipe the participants list.
		// Each branch needs at least its own discriminator: Players[] (batch)
		// or Name (single-add).
		if len(req.Players) == 0 && req.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "request must include either a non-empty 'players' array or a 'name' field"})
			return
		}

		if len(req.Players) == 0 && req.Name != "" {
			// Single player add workflow
			name := strings.TrimSpace(req.Name)
			dojo := strings.TrimSpace(req.Dojo)
			if name == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be blank"})
				return
			}
			if dojo == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "dojo must not be blank"})
				return
			}
			metadata := req.Metadata
			if len(metadata) == 0 && req.DanGrade != "" {
				metadata = []string{req.DanGrade}
			}

			// Default to "manual" so rows added via this UI carry the same
			// provenance marker as rows the operator added by hand to the
			// paste-box import — keeps tag-filter buckets coherent.
			tag := req.Tag
			if tag == "" {
				tag = "manual"
			}

			// Strip displayName for non-zekken competitions. Otherwise
			// saveParticipantsNoLock writes a 3-column row (Name,DisplayName,Dojo)
			// that LoadParticipants(withZekkenName=false) then mis-parses —
			// displayName takes column 2, the real Dojo gets pushed into
			// Metadata. Store.AddParticipant re-derives via SanitizeName(Name)
			// when DisplayName is empty.
			displayName := req.DisplayName
			if !comp.WithZekkenName {
				displayName = ""
			}

			if err := validatePlayerLengths(name, displayName, dojo, tag, metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			player := domain.Player{
				Name:        name,
				DisplayName: displayName,
				Dojo:        dojo,
				Metadata:    metadata,
				Tag:         tag,
			}

			addedPlayer, err := store.AddParticipant(id, player, comp.WithZekkenName)
			if err != nil {
				if errors.Is(err, state.ErrDuplicateName) || errors.Is(err, state.ErrCompetitionNotInSetup) {
					c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
					return
				}
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add participant: " + err.Error()})
				return
			}

			hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
			c.JSON(http.StatusOK, addedPlayer)
			return
		}

		// Per-player length caps — defense-in-depth against unbounded
		// participants.csv inflation. Reject the whole batch on the
		// first offender (matches the all-or-nothing semantics
		// SaveParticipants already enforces on write).
		for i, p := range req.Players {
			if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Tag, p.Metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
				return
			}
		}

		// Load existing participants so we can preserve check-in state for
		// players that survive the edit (matched by name). A full roster
		// replacement via this endpoint must not silently clear check-ins
		// that were already recorded.
		existing, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load participants: " + err.Error()})
			return
		}
		checkedInByName := make(map[string]bool, len(existing))
		for _, ep := range existing {
			checkedInByName[strings.ToLower(strings.TrimSpace(ep.Name))] = ep.CheckedIn
		}

		players := make([]domain.Player, 0, len(req.Players))
		for i, p := range req.Players {
			// Mirror the single-add/replace strip: a non-empty DisplayName on
			// a non-zekken competition produces a 3-column CSV row that
			// LoadParticipants(_, false) mis-parses on the next read. Force "".
			displayName := p.DisplayName
			if !comp.WithZekkenName {
				displayName = ""
			}
			players = append(players, domain.Player{
				Name:         p.Name,
				DisplayName:  displayName,
				Dojo:         p.Dojo,
				Metadata:     p.Metadata,
				Tag:          p.Tag,
				PoolPosition: int64(i),
				CheckedIn:    checkedInByName[strings.ToLower(strings.TrimSpace(p.Name))],
			})
		}

		if err := store.SaveParticipants(id, players); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save participants: " + err.Error()})
			return
		}

		// Reload from disk so the response reflects the persisted roster —
		// SaveParticipants mints UUID IDs when p.ID is empty, so the request-
		// derived `players` slice lacks the persisted IDs and would force
		// clients to round-trip GET /participants to learn them.
		saved, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to reload participants: " + err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, saved)
	})

	r.PUT("/competitions/:id/participants/:pid", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		var req struct {
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Dojo        string   `json:"dojo"`
			Metadata    []string `json:"metadata"`
			DanGrade    string   `json:"danGrade"`
			Tag         string   `json:"tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		name := strings.TrimSpace(req.Name)
		dojo := strings.TrimSpace(req.Dojo)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be blank"})
			return
		}
		if dojo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dojo must not be blank"})
			return
		}

		metadata := req.Metadata
		if len(metadata) == 0 && req.DanGrade != "" {
			metadata = []string{req.DanGrade}
		}

		// Run the status check and participant write under one lock acquire so
		// a concurrent start-competition cannot flip status between the check
		// and the file write (TOCTOU, mp-0lc).
		var (
			updatedPlayer *domain.Player
			httpStatus    = http.StatusInternalServerError
			httpMsg       string
		)
		txErr := store.WithTransaction(id, func(tx state.StoreTx) error {
			comp, err := tx.LoadCompetition(id)
			if err != nil {
				httpMsg = err.Error()
				return err
			}
			if comp == nil {
				httpStatus = http.StatusNotFound
				httpMsg = "competition not found"
				return fmt.Errorf("competition not found")
			}
			if comp.Status != state.CompStatusSetup && comp.Status != "" {
				httpStatus = http.StatusConflict
				httpMsg = "cannot modify participants after competition has started"
				return state.ErrCompetitionNotInSetup
			}

			// Strip displayName for non-zekken competitions — same CSV-corruption
			// guard as the single-add path: a 3-column row written here would be
			// mis-parsed on the next LoadParticipants(withZekkenName=false) read,
			// shifting Dojo into Metadata. Empty DisplayName triggers SanitizeName
			// re-derivation in saveParticipantsNoLock.
			displayName := req.DisplayName
			if !comp.WithZekkenName {
				displayName = ""
			}

			if err := validatePlayerLengths(name, displayName, dojo, req.Tag, metadata); err != nil {
				httpStatus = http.StatusBadRequest
				httpMsg = err.Error()
				return err
			}

			p, err := tx.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
				p.Name = name
				p.DisplayName = displayName
				p.Dojo = dojo
				p.Metadata = metadata
				p.Tag = req.Tag
				return nil
			})
			if err != nil {
				switch {
				case errors.Is(err, state.ErrParticipantNotFound):
					httpStatus = http.StatusNotFound
				case errors.Is(err, state.ErrDuplicateName):
					httpStatus = http.StatusConflict
				}
				httpMsg = err.Error()
				return err
			}
			updatedPlayer = p
			return nil
		})
		if txErr != nil {
			msg := httpMsg
			if msg == "" {
				msg = txErr.Error()
			}
			c.JSON(httpStatus, gin.H{"error": msg})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})

	r.GET("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		seeds, err := store.LoadSeeds(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, seeds)
	})

	r.PUT("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var assignments []domain.SeedAssignment
		if err := c.ShouldBindJSON(&assignments); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Cross-file guard symmetry with handlers_import.go's seed
		// validation: reject oversized names so seeds.csv can't grow
		// unbounded.
		for i, sa := range assignments {
			if err := validateMaxLen(fmt.Sprintf("seeds[%d].name", i), sa.Name, MaxLenSeedAssignmentName); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		if err := store.SaveSeeds(id, assignments); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, assignments)
	})

	r.PUT("/competitions/:id/participants/:pid/checkin", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		updatedPlayer, err := store.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})

		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrParticipantNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})

	r.POST("/competitions/:id/participants/checkin-bulk", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var req struct {
			ParticipantIDs []string `json:"participantIds"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.ParticipantIDs) > MaxBulkCheckInIDs {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("participantIds must not exceed %d entries", MaxBulkCheckInIDs)})
			return
		}
		for i, pid := range req.ParticipantIDs {
			if err := validateMaxLen(fmt.Sprintf("participantIds[%d]", i), pid, MaxLenEntityID); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		result, err := store.BulkCheckIn(id, req.ParticipantIDs)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		if result.CheckedIn > 0 {
			hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		}

		c.JSON(http.StatusOK, result)
	})

	r.DELETE("/competitions/:id/participants/:pid/checkin", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		updatedPlayer, err := store.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
			p.CheckedIn = false
			return nil
		})

		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrParticipantNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})
}

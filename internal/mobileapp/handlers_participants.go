package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterParticipantHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub Broadcaster, elevated ElevatedVerifier) {
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

	r.POST("/competitions/:id/participants", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Status == state.CompStatusDrawReady {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify participants while a draw is pending; discard the draw first"})
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
			if len(metadata) == 0 {
				if dg := strings.TrimSpace(req.DanGrade); dg != "" {
					metadata = []string{dg}
				}
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
				Tag:         helper.CanonicalParticipantTag(tag),
			}

			addedPlayer, err := store.AddParticipant(id, player, comp.WithZekkenName)
			if err != nil {
				if errors.Is(err, state.ErrDuplicateName) {
					c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
					return
				}
				if errors.Is(err, state.ErrReservedName) {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				if errors.Is(err, state.ErrCompetitionNotInSetup) {
					// Reload to distinguish draw-ready from a fully-started competition
					// under TOCTOU: status could have flipped between our check above
					// and AddParticipant acquiring the per-comp lock.
					msg := "cannot modify participants after competition has started"
					if reloaded, _ := store.LoadCompetition(id); reloaded != nil && reloaded.Status == state.CompStatusDrawReady {
						msg = "cannot modify participants while a draw is pending; discard the draw first"
					}
					c.JSON(http.StatusConflict, gin.H{"error": msg})
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

		// Tier-1: Reject perfect duplicates (normalizedName, normalizedDojo).
		// Uses name+dojo so "John Smith / Wakaba" and "John Smith / Tora" are
		// treated as distinct competitors (different clubs) while
		// "Müller / Wakaba" vs "muller / wakaba" are rejected.
		entries := make([][2]string, len(req.Players))
		for i, p := range req.Players {
			entries[i] = [2]string{p.Name, p.Dojo}
		}
		if dupes := helper.CheckDuplicateEntriesByNameDojo(entries); len(dupes) > 0 {
			c.JSON(http.StatusConflict, gin.H{"error": fmt.Sprintf("duplicate participant(s) in request: %s", strings.Join(dupes, "; "))})
			return
		}

		// Near-duplicate (Tier-2) warnings are surfaced authoritatively by the
		// PUT /competitions/:id roster path (the SPA's primary import flow);
		// this endpoint stays a plain array response to keep one shape.

		// Load existing participants so we can preserve check-in state for
		// players that survive the edit (matched by normalizedName+normalizedDojo).
		// A full roster replacement via this endpoint must not silently clear
		// check-ins that were already recorded.
		existing, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load participants: " + err.Error()})
			return
		}
		// Key by (normalizedName, normalizedDojo) — NOT name alone. Tier-1
		// dedup allows two same-named competitors from different dojos, so a
		// name-only key would transfer check-in state between distinct people.
		checkInKey := func(name, dojo string) string {
			return helper.NormalizeParticipantName(name) + "|" + helper.NormalizeParticipantName(dojo)
		}
		checkedInByKey := make(map[string]bool, len(existing))
		for _, ep := range existing {
			checkedInByKey[checkInKey(ep.Name, ep.Dojo)] = ep.CheckedIn
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
				Tag:          helper.CanonicalParticipantTag(p.Tag),
				PoolPosition: int64(i),
				CheckedIn:    checkedInByKey[checkInKey(p.Name, p.Dojo)],
			})
		}

		if err := store.SaveParticipants(id, players); err != nil {
			// Defense-in-depth: saveParticipantsNoLock also enforces the
			// Tier-1 (name, dojo) guard, so map that to 409 rather than 500
			// in case the pre-check above ever diverges from the write layer.
			if errors.Is(err, state.ErrDuplicateName) {
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
				return
			}
			if errors.Is(err, state.ErrReservedName) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
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

	r.PUT("/competitions/:id/participants/:pid", RequireElevatedPassword(elevated), func(c *gin.Context) {
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

		if comp.Status != state.CompStatusDrawReady && comp.Status != state.CompStatusSetup && comp.Status != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify participants after competition has started"})
			return
		}

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
		if len(metadata) == 0 {
			if dg := strings.TrimSpace(req.DanGrade); dg != "" {
				metadata = []string{dg}
			}
		}

		// Run the status check and participant write under one lock acquire so
		// a concurrent start-competition cannot flip status between the check
		// and the file write (TOCTOU, mp-0lc).
		var (
			updatedPlayer  *domain.Player
			oldName        string
			oldDojo        string
			oldDisplayName string
			isDrawReady    bool
			httpStatus     = http.StatusInternalServerError
			httpMsg        string
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
			if comp.Status != state.CompStatusDrawReady && comp.Status != state.CompStatusSetup && comp.Status != "" {
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
				// Capture old values before mutation for draw cascade.
				// The transform callback receives the pre-mutation player,
				// so this avoids a separate LoadParticipants scan.
				if comp.Status == state.CompStatusDrawReady {
					oldName = p.Name
					oldDojo = p.Dojo
					oldDisplayName = p.DisplayName
					isDrawReady = true
				}
				p.Name = name
				p.DisplayName = displayName
				p.Dojo = dojo
				p.Metadata = metadata
				p.Tag = helper.CanonicalParticipantTag(req.Tag)
				return nil
			})
			if err != nil {
				switch {
				case errors.Is(err, state.ErrParticipantNotFound):
					httpStatus = http.StatusNotFound
				case errors.Is(err, state.ErrDuplicateName):
					httpStatus = http.StatusConflict
				case errors.Is(err, state.ErrReservedName):
					httpStatus = http.StatusBadRequest
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

		var warnings []string
		if isDrawReady && oldName != "" {
			// Cascade the name/dojo change through draw artifacts outside the
			// transaction — WithTransaction's per-comp lock is released above
			// (non-reentrant mutex), so the cascade function can acquire it.
			// Use updatedPlayer.DisplayName (the canonical post-save value) so
			// auto-derived display names propagate correctly into pools.csv.
			// For non-zekken competitions UpdateParticipant returns DisplayName=""
			// (not persisted), but pools.csv carries the auto-derived SanitizeName form.
			// Match what saveParticipantsNoLock writes so the cascade doesn't blank it.
			cascadeDisplayName := updatedPlayer.DisplayName
			if cascadeDisplayName == "" {
				cascadeDisplayName = helper.SanitizeName(updatedPlayer.Name)
			}
			w, cascadeErr := eng.ReplaceParticipantInDraw(id, oldName, oldDojo, oldDisplayName, updatedPlayer.Name, updatedPlayer.Dojo, cascadeDisplayName)
			if cascadeErr != nil {
				// participants.csv (and seeds.csv) were already updated — broadcast and
				// return 200 with the updated player so the client keeps its local state.
				// Include any warnings collected before the failure (e.g. dojo conflicts
				// from pools) and a cascadeError field for operator visibility.
				hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
				type playerWithCascadeError struct {
					domain.Player
					Warnings     []string `json:"warnings,omitempty"`
					CascadeError string   `json:"cascadeError"`
				}
				c.JSON(http.StatusOK, playerWithCascadeError{
					Player:       *updatedPlayer,
					Warnings:     w,
					CascadeError: cascadeErr.Error(),
				})
				return
			}
			warnings = w
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		if len(warnings) > 0 {
			type playerWithWarnings struct {
				domain.Player
				Warnings []string `json:"warnings"`
			}
			c.JSON(http.StatusOK, playerWithWarnings{
				Player:   *updatedPlayer,
				Warnings: warnings,
			})
			return
		}
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
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if comp.Status == state.CompStatusDrawReady {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify seeds while a draw is pending; discard the draw first"})
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

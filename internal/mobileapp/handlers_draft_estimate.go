package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// DraftWarning represents a warning for a draft estimate calculation.
type DraftWarning struct {
	Code         string `json:"code"`
	Participants int    `json:"participants"`
	Usable       int    `json:"usable"`
	Chosen       int    `json:"chosen"`
}

// draftEstimateResponse wraps the engine's DraftEstimate and adds a warnings array.
type draftEstimateResponse struct {
	engine.DraftEstimate
	Warnings []DraftWarning `json:"warnings"`
}

func parseDraftQueryBool(c *gin.Context, key string) (*bool, bool) {
	raw := c.Query(key)
	if raw == "" {
		return nil, true
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%s must be a boolean", key)})
		return nil, false
	}
	return &v, true
}

func parseDraftQueryOptionalInt(c *gin.Context, key string) (int, bool, bool) {
	raw := c.Query(key)
	if raw == "" {
		return 0, false, true
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%s must be an integer", key)})
		return 0, false, false
	}
	if v < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("%s must be non-negative", key)})
		return 0, false, false
	}
	return v, true, true
}

// RegisterDraftEstimateHandlers registers the draft schedule estimate endpoint.
func RegisterDraftEstimateHandlers(r gin.IRoutes, store TournamentLoader, eng DraftEstimator) {
	r.GET("/schedule/draft-estimate", func(c *gin.Context) {
		participants, ok := parseRequiredBoundedInt(c, "participants", 1, engine.MaxEstimateParticipants)
		if !ok {
			return
		}

		tournament, err := store.LoadTournament()
		if err != nil {
			internalError(c, err)
			return
		}

		var comp state.Competition

		rawCourts := c.Query("courts")
		if rawCourts != "" {
			courtCount, err := strconv.Atoi(rawCourts)
			if err != nil || courtCount < 1 || courtCount > engine.MaxCourts {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("courts must be between 1 and %d", engine.MaxCourts)})
				return
			}
			labels := make([]string, courtCount)
			for i := 0; i < courtCount; i++ {
				labels[i] = helper.CourtLabel(i)
			}
			comp.Courts = labels
		} else {
			comp.Courts = engine.InheritedDrawCourts(nil, tournament)
		}

		rawDate := strings.TrimSpace(c.Query("date"))
		if rawDate != "" {
			if err := validateDateDMY(rawDate); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			comp.Date = rawDate
			if err := validateCompetitionDateInTournament(&comp, tournament); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		} else {
			defaultCompetitionDate(&comp, tournament)
		}

		rawStartTime := strings.TrimSpace(c.Query("startTime"))
		if rawStartTime != "" {
			if err := validateMaxLen("startTime", rawStartTime, MaxLenCompetitionStartTime); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if _, err := time.Parse("15:04", rawStartTime); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "startTime must be HH:MM"})
				return
			}
			comp.StartTime = rawStartTime
		}

		comp.Format = strings.TrimSpace(c.Query("format"))

		comp.Kind = strings.TrimSpace(c.Query("kind"))

		if v, set, ok := parseDraftQueryOptionalInt(c, "teamSize"); !ok {
			return
		} else if set {
			comp.TeamSize = v
		}

		comp.TeamMatchType = state.TeamMatchType(strings.TrimSpace(c.Query("teamMatchType")))

		if v, set, ok := parseDraftQueryOptionalInt(c, "poolSize"); !ok {
			return
		} else if set {
			comp.PoolSize = v
		}

		comp.PoolSizeMode = strings.TrimSpace(c.Query("poolSizeMode"))

		if v, set, ok := parseDraftQueryOptionalInt(c, "poolWinners"); !ok {
			return
		} else if set {
			comp.PoolWinners = v
		}

		comp.PoolFormat = strings.TrimSpace(c.Query("poolFormat"))

		if b, ok := parseDraftQueryBool(c, "roundRobin"); !ok {
			return
		} else if b != nil {
			comp.RoundRobin = *b
		}

		comp.ExtraQualifiers = strings.TrimSpace(c.Query("extraQualifiers"))

		if b, ok := parseDraftQueryBool(c, "twoThirdPlaces"); !ok {
			return
		} else if b != nil {
			comp.TwoThirdPlaces = b
		}

		if b, ok := parseDraftQueryBool(c, "naginata"); !ok {
			return
		} else if b != nil {
			comp.Naginata = *b
		}

		if v, set, ok := parseDraftQueryOptionalInt(c, "swissRounds"); !ok {
			return
		} else if set {
			comp.SwissRounds = v
		}

		if v, set, ok := parseDraftQueryOptionalInt(c, "poolMatchDurationSeconds"); !ok {
			return
		} else if set {
			comp.PoolMatchDurationSeconds = v
		}

		if v, set, ok := parseDraftQueryOptionalInt(c, "knockoutMatchDurationSeconds"); !ok {
			return
		} else if set {
			comp.KnockoutMatchDurationSeconds = v
		}

		// The create POST's own chain, so a draft is judged as the competition
		// it describes would be.
		if err := validateNewCompetition(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		estimate, err := eng.EstimateDraft(&comp, tournament, participants)
		if err != nil {
			var valErr *engine.ValidationError
			if errors.As(err, &valErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": valErr.Error()})
				return
			}
			internalError(c, err)
			return
		}

		warnings := []DraftWarning{}
		maxUseful := engine.MaxUsefulCourts(participants)
		if len(comp.Courts) > maxUseful {
			warnings = append(warnings, DraftWarning{
				Code:         "too_many_shiaijo",
				Participants: participants,
				Usable:       maxUseful,
				Chosen:       len(comp.Courts),
			})
		}

		c.JSON(http.StatusOK, draftEstimateResponse{
			DraftEstimate: estimate,
			Warnings:      warnings,
		})
	})
}

package mobileapp

import (
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The refusal codes of the public-names option (Competition.PublicNames,
// Tournament.DefaultPublicNames). Each answer also carries a sentence the
// organiser reads.
const (
	// codePublicNamesSelfRun: a hiding option in a self-run tournament
	// (409). Self-run tournaments are run by the competitors from the
	// public page, so the option is for officiated tournaments only.
	codePublicNamesSelfRun = "public_names_self_run"
	// codePublicNamesSwiss: a hiding option on a Swiss competition, or a
	// hiding competition switched to Swiss (400). A Swiss draw gives
	// competitors no numbers to show instead of their names.
	codePublicNamesSwiss = "public_names_swiss"
	// codePublicNamesApplyConfirm: a tournament save that changes the
	// default would change these competitions (409); the save is sent again
	// with confirmPublicNames once the organiser has seen the list.
	codePublicNamesApplyConfirm = "public_names_apply_confirm"
	// codePublicNamesSelfRunSwitch: a tournament being created self-run
	// over competitions already in the data folder that hide names (409);
	// the save is sent again with confirmShowNames once the organiser has
	// seen the list, and those competitions then show names.
	codePublicNamesSelfRunSwitch = "public_names_self_run_switch"
)

const (
	msgPublicNamesSelfRun = "Showing numbers instead of names is only for officiated tournaments: in a self-run tournament the competitors run their own matches from the public page, so they need to see the names."
	msgPublicNamesSwiss   = "Showing numbers instead of names is not available for Swiss competitions yet, because a Swiss draw gives competitors no numbers to show."
	msgPublicNamesApply   = "Saving this default also changes what the public pages show for the competitions listed. Confirm to change them all; each one can still be changed on its own afterwards."
	msgPublicNamesSwitch  = "The competitions listed show numbers instead of names, which a self-run tournament does not offer: the competitors run their own matches from the public page, so they need to see the names. Confirm to show their names."
)

// validatePublicNamesValue answers the 400 sentence for a publicNames value
// that is not one of the options, or "" when it is one.
func validatePublicNamesValue(field, v string) string {
	if state.ValidPublicNames(v) {
		return ""
	}
	return fmt.Sprintf("%s must be one of %q (names shown), %q, %q or %q", field,
		state.PublicNamesShown, state.PublicNamesPerson, state.PublicNamesCompetition, state.PublicNamesHidden)
}

// validateRevealUntilValue answers the 400 sentence for a revealUntil value
// that does not read, or "" when it reads.
func validateRevealUntilValue(v string) string {
	if state.ValidRevealUntil(v) {
		return ""
	}
	return "revealUntil must be a date and time like 2026-05-12T18:00, or empty for the end of the tournament"
}

// setPublicNames gives comp the public-names option v and mints its reveal
// secret the first time v gives reveal links. The secret is kept for the
// competition's whole life (E3): switching links off leaves it, so switching
// them back on gives back the very links already printed. It is the one
// writer of Competition.RevealSecret; nothing a client sends reaches it.
func setPublicNames(comp *state.Competition, v string) error {
	comp.PublicNames = v
	if state.PublicNamesHasLinks(v) && comp.RevealSecret == "" {
		secret, err := state.NewRevealSecret()
		if err != nil {
			return err
		}
		comp.RevealSecret = secret
	}
	return nil
}

// publicNamesRefusal judges a competition's public-names option as a write
// would store it: hiding names is refused while the tournament is self-run
// (409) and on a Swiss competition (400). changed says whether the write
// moves the option, formatChanged whether it moves the format: a write
// answers only for what it introduces, so a stored value it leaves alone is
// never refused (a hand-edited config.md stays editable). It answers the
// status, code and sentence, or status 0 when the write may land.
func publicNamesRefusal(publicNames, format string, changed, formatChanged bool, tourn *state.Tournament) (int, string, string) {
	if !state.PublicNamesHidesNames(publicNames) {
		return 0, "", ""
	}
	if changed && tourn.IsSelfRun() {
		return http.StatusConflict, codePublicNamesSelfRun, msgPublicNamesSelfRun
	}
	if (changed || formatChanged) && format == state.CompFormatSwiss {
		return http.StatusBadRequest, codePublicNamesSwiss, msgPublicNamesSwiss
	}
	return 0, "", ""
}

// publicNamesTarget is a competition a new tournament default would change,
// as the confirmation lists it.
type publicNamesTarget struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	PublicNames string `json:"publicNames"`
}

// publicNamesDefaultTargets lists the competitions that taking the
// tournament default v would change: every one whose option differs, Swiss
// left out (E2). Sorted as the store lists them.
func publicNamesDefaultTargets(comps []*state.Competition, v string) []publicNamesTarget {
	targets := []publicNamesTarget{}
	for _, comp := range comps {
		if comp == nil || comp.Format == state.CompFormatSwiss || comp.PublicNames == v {
			continue
		}
		targets = append(targets, publicNamesTarget{ID: comp.ID, Name: comp.Name, PublicNames: comp.PublicNames})
	}
	return targets
}

// applyPublicNamesDefault sets every competition in targets to the
// public-names option v (the tournament default, or "" to show names), each
// under its own lock, re-judged there (a competition switched to Swiss
// meanwhile is not given a hiding option). It answers whether
// any competition changed and the ids it could not change; a failure on one
// competition does not stop the others, and a save of the same default with
// confirmPublicNames again retries the rest.
func applyPublicNamesDefault(store *state.Store, targets []publicNamesTarget, v string) (bool, []string) {
	anyChanged := false
	var failed []string
	for _, target := range targets {
		changed, err := store.UpdateCompetitionChanged(target.ID, func(current *state.Competition) (*state.Competition, error) {
			if current == nil || current.PublicNames == v || (current.Format == state.CompFormatSwiss && state.PublicNamesHidesNames(v)) {
				return nil, nil
			}
			if err := setPublicNames(current, v); err != nil {
				return nil, err
			}
			return current, nil
		})
		if err != nil {
			log.Printf("mobileapp: tournament default public names: competition %s: %v", target.ID, err)
			failed = append(failed, target.ID)
			continue
		}
		anyChanged = anyChanged || changed
	}
	return anyChanged, failed
}

// judgePublicNamesDefault judges a tournament save's default public-names
// option (stored is the tournament as it stands, nil before the first save)
// and answers the competitions the save is to set to it once it lands. A
// value that is not an option is 400; a hiding default in a self-run
// tournament is 409 public_names_self_run (judged on a change only, so a
// stored value the save leaves alone never locks the form, and is never
// applied to the competitions either); a moved default
// that would change competitions is 409 public_names_apply_confirm listing
// them until confirmed. ok is false when the answer has been written.
func judgePublicNamesDefault(c *gin.Context, store *state.Store, stored, t *state.Tournament, confirmed bool) ([]publicNamesTarget, bool) {
	if msg := validatePublicNamesValue("defaultPublicNames", t.DefaultPublicNames); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": msg})
		return nil, false
	}
	if stored == nil {
		// The first save (a PUT may create the tournament too): no
		// competition has a tournament to follow yet, and the mode is the
		// one this save chooses.
		if state.PublicNamesHidesNames(t.DefaultPublicNames) && t.IsSelfRun() {
			c.JSON(http.StatusConflict, gin.H{"error": msgPublicNamesSelfRun, "code": codePublicNamesSelfRun})
			return nil, false
		}
		return nil, true
	}
	moved := t.DefaultPublicNames != stored.DefaultPublicNames
	// The mode cannot change on a save (errModeImmutable), so the stored
	// one is the mode this default lands in.
	if moved && state.PublicNamesHidesNames(t.DefaultPublicNames) && stored.IsSelfRun() {
		c.JSON(http.StatusConflict, gin.H{"error": msgPublicNamesSelfRun, "code": codePublicNamesSelfRun})
		return nil, false
	}
	if !moved && !confirmed {
		return nil, true
	}
	// A hiding default a self-run tournament already holds (hand-edited, or
	// brought in by a restore) is left alone, as the refusal above leaves it
	// on a save that does not move it, and so is never applied to the
	// competitions: public names are for officiated tournaments only.
	if stored.IsSelfRun() && state.PublicNamesHidesNames(t.DefaultPublicNames) {
		return nil, true
	}
	comps, err := loadAllCompetitions(store)
	if err != nil {
		internalError(c, err)
		return nil, false
	}
	targets := publicNamesDefaultTargets(comps, t.DefaultPublicNames)
	if len(targets) > 0 && !confirmed {
		c.JSON(http.StatusConflict, gin.H{
			"error":        msgPublicNamesApply,
			"code":         codePublicNamesApplyConfirm,
			"competitions": targets,
		})
		return nil, false
	}
	return targets, true
}

// publicNamesHidingTargets lists the competitions that hide names, Swiss
// included (a hand-edited config.md can hold one), sorted as the store
// lists them.
func publicNamesHidingTargets(comps []*state.Competition) []publicNamesTarget {
	targets := []publicNamesTarget{}
	for _, comp := range comps {
		if comp.HidesNames() {
			targets = append(targets, publicNamesTarget{ID: comp.ID, Name: comp.Name, PublicNames: comp.PublicNames})
		}
	}
	return targets
}

// showNamesForSelfRun is the self-run switch (public_names_self_run_switch).
// The mode is chosen once, when the tournament is created (errModeImmutable
// after that), so the one save that turns a tournament self-run is the
// first one (stored nil), and it can find competitions already in the data
// folder that hide names. Such a save is answered 409 with the list until it
// carries confirmShowNames; confirmed, those competitions are set to show
// names BEFORE the tournament is written, so a failure part way leaves no
// self-run tournament with a competition hiding names, and sending the save
// again finishes the rest. Each competition keeps its reveal secret (E3).
// ok is false when the answer has been written; the caller broadcasts
// tournament_updated once the tournament lands.
func showNamesForSelfRun(c *gin.Context, store *state.Store, stored, t *state.Tournament, confirmed bool) bool {
	if stored != nil || !t.IsSelfRun() {
		return true
	}
	comps, err := loadAllCompetitions(store)
	if err != nil {
		internalError(c, err)
		return false
	}
	targets := publicNamesHidingTargets(comps)
	if len(targets) == 0 {
		return true
	}
	if !confirmed {
		c.JSON(http.StatusConflict, gin.H{
			"error":        msgPublicNamesSwitch,
			"code":         codePublicNamesSelfRunSwitch,
			"competitions": targets,
		})
		return false
	}
	if _, failed := applyPublicNamesDefault(store, targets, state.PublicNamesShown); len(failed) > 0 {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":        fmt.Sprintf("The tournament was not saved: these competitions could not be changed to show names: %s. Save again to retry.", strings.Join(failed, ", ")),
			"competitions": failed,
		})
		return false
	}
	return true
}

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

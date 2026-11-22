package mobileapp

// The public-names option's rules: its refusal codes and sentences, the
// judgement of a competition's or the tournament default's value, and the
// applying of a default and of the self-run switch to competitions. The
// reveal-links read is in handlers_reveal_links.go.

import (
	"fmt"
	"log"
	"net/http"
	"strings"

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

// publicNamesDefaultChanges reports whether the tournament default v changes
// comp: its option differs, and v is not a hiding option on a Swiss one (E2:
// Swiss never hides names, but one left hiding by a hand edit or a restore is
// brought back to showing them).
func publicNamesDefaultChanges(comp *state.Competition, v string) bool {
	return comp != nil && comp.PublicNames != v && (comp.Format != state.CompFormatSwiss || !state.PublicNamesHidesNames(v))
}

// publicNamesDefaultTargets lists the competitions that taking the
// tournament default v would change (publicNamesDefaultChanges). Sorted as
// the store lists them.
func publicNamesDefaultTargets(comps []*state.Competition, v string) []publicNamesTarget {
	targets := []publicNamesTarget{}
	for _, comp := range comps {
		if !publicNamesDefaultChanges(comp, v) {
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
			if !publicNamesDefaultChanges(current, v) {
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

// publicNamesAnswer is the refusal or failure a judgement hands back: the
// status and the body the handler writes. Judging never writes an answer
// itself, so each judgement can be read and tested apart from gin.
type publicNamesAnswer struct {
	status int
	body   gin.H
}

// answerPublicNames writes a judgement's refusal, or the 500 for its error,
// and says whether it answered (the handler then returns).
func answerPublicNames(c *gin.Context, refusal *publicNamesAnswer, err error) bool {
	if err != nil {
		internalError(c, err)
		return true
	}
	if refusal != nil {
		c.JSON(refusal.status, refusal.body)
		return true
	}
	return false
}

// publicNamesSelfRunAnswer is the 409 for a hiding default in a self-run
// tournament.
func publicNamesSelfRunAnswer() *publicNamesAnswer {
	return &publicNamesAnswer{http.StatusConflict, gin.H{"error": msgPublicNamesSelfRun, "code": codePublicNamesSelfRun}}
}

// publicNamesDefaultFailedAnswer is the 500 for a tournament save that landed
// while some competitions could not be set to its default.
func publicNamesDefaultFailedAnswer(failed []string) *publicNamesAnswer {
	return &publicNamesAnswer{http.StatusInternalServerError, gin.H{
		"error":        fmt.Sprintf("The tournament settings were saved, but these competitions could not be changed to the new default: %s. Save again to retry.", strings.Join(failed, ", ")),
		"competitions": failed,
	}}
}

// judgePublicNamesDefault judges a tournament save's default public-names
// option (stored is the tournament as it stands, nil before the first save)
// and answers the competitions the save is to set to it once it lands. A
// value that is not an option is 400; a hiding default in a self-run
// tournament is 409 public_names_self_run (judged on a change only, so a
// stored value the save leaves alone never locks the form, and is never
// applied to the competitions either); a moved default
// that would change competitions is 409 public_names_apply_confirm listing
// them until confirmed. A first save counts as moving the default from
// showing names, and takes the mode it chooses, so it too sets the
// competitions already in the data folder.
func judgePublicNamesDefault(store *state.Store, stored, t *state.Tournament, confirmed bool) ([]publicNamesTarget, *publicNamesAnswer, error) {
	if msg := validatePublicNamesValue("defaultPublicNames", t.DefaultPublicNames); msg != "" {
		return nil, &publicNamesAnswer{http.StatusBadRequest, gin.H{"error": msg}}, nil
	}
	// The mode cannot change on a later save (errModeImmutable), so the
	// stored one is the mode this default lands in.
	storedDefault, selfRun := "", t.IsSelfRun()
	if stored != nil {
		storedDefault, selfRun = stored.DefaultPublicNames, stored.IsSelfRun()
	}
	hides := state.PublicNamesHidesNames(t.DefaultPublicNames)
	moved := t.DefaultPublicNames != storedDefault
	if moved && hides && selfRun {
		return nil, publicNamesSelfRunAnswer(), nil
	}
	if !moved && !confirmed {
		return nil, nil, nil
	}
	// A hiding default a self-run tournament already holds (hand-edited, or
	// brought in by a restore) is left alone, as the refusal above leaves it
	// on a save that does not move it, and so is never applied to the
	// competitions: public names are for officiated tournaments only.
	if hides && selfRun {
		return nil, nil, nil
	}
	comps, err := loadAllCompetitions(store)
	if err != nil {
		return nil, nil, err
	}
	targets := publicNamesDefaultTargets(comps, t.DefaultPublicNames)
	if len(targets) > 0 && !confirmed {
		return nil, &publicNamesAnswer{http.StatusConflict, gin.H{
			"error":        msgPublicNamesApply,
			"code":         codePublicNamesApplyConfirm,
			"competitions": targets,
		}}, nil
	}
	return targets, nil, nil
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

// judgeSelfRunSwitch judges the self-run switch (public_names_self_run_switch).
// The mode is chosen once, when the tournament is created (errModeImmutable
// after that), so the one save that turns a tournament self-run is the
// first one (stored nil), and it can find competitions already in the data
// folder that hide names. Such a save is answered 409 with the list until it
// carries confirmShowNames; confirmed, it answers the competitions
// applySelfRunSwitch is to set to show names.
func judgeSelfRunSwitch(store *state.Store, stored, t *state.Tournament, confirmed bool) ([]publicNamesTarget, *publicNamesAnswer, error) {
	if stored != nil || !t.IsSelfRun() {
		return nil, nil, nil
	}
	comps, err := loadAllCompetitions(store)
	if err != nil {
		return nil, nil, err
	}
	targets := publicNamesHidingTargets(comps)
	if len(targets) > 0 && !confirmed {
		return nil, &publicNamesAnswer{http.StatusConflict, gin.H{
			"error":        msgPublicNamesSwitch,
			"code":         codePublicNamesSelfRunSwitch,
			"competitions": targets,
		}}, nil
	}
	return targets, nil, nil
}

// applySelfRunSwitch sets the competitions judgeSelfRunSwitch listed to show
// names, BEFORE the tournament is written, so a failure part way leaves no
// self-run tournament with a competition hiding names, and sending the save
// again finishes the rest. Each competition keeps its reveal secret (E3). It
// answers the 500 naming the competitions it could not change, nil when all
// changed; the caller broadcasts tournament_updated once the tournament lands, or defers tellSwitchUnlessSaved
// until it does.
//
// The listing was read before the tournament's own lock, which the lock
// order (competitions, then the tournament) forbids holding here. A second
// first save landing in between leaves these competitions showing names and
// this save refused as stale, which is the safe direction: the retry finds
// the tournament that was created and no competition is left hiding under a
// self-run one.
func applySelfRunSwitch(hub *Hub, store *state.Store, targets []publicNamesTarget) *publicNamesAnswer {
	if len(targets) == 0 {
		return nil
	}
	changed, failed := applyPublicNamesDefault(store, targets, state.PublicNamesShown)
	if len(failed) == 0 {
		return nil
	}
	// The competitions that did change are already showing names: tell
	// open pages now, as the tournament save's own failure path does.
	if changed {
		hub.Broadcast(EventTournamentUpdated, nil)
	}
	return &publicNamesAnswer{http.StatusInternalServerError, gin.H{
		"error":        fmt.Sprintf("The tournament was not saved: these competitions could not be changed to show names: %s. Save again to retry.", strings.Join(failed, ", ")),
		"competitions": failed,
	}}
}

// tellSwitchUnlessSaved is deferred by a tournament save that ran
// applySelfRunSwitch: when the save then fails (saved is still false), the
// competitions are already showing names while the tournament is not
// self-run, so open pages and streams are told now, as they are when the
// switch itself fails part way. A save that lands broadcasts for itself.
func tellSwitchUnlessSaved(hub *Hub, targets []publicNamesTarget, saved *bool) {
	if len(targets) > 0 && !*saved {
		hub.Broadcast(EventTournamentUpdated, nil)
	}
}

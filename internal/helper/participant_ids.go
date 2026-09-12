package helper

import (
	"errors"
	"fmt"
	"strings"
)

// ErrMissingParticipantIDsInDraw is the sentinel identifying a draw refused
// because the roster still has one or more rows with an empty id (bc-pnum
// ruling 1c). Match it with errors.Is; the returned error's message
// additionally names the affected competitors so the operator knows what to
// do, via MissingParticipantIDsMessage.
//
// Ids are the roster's stable identity for every downstream write (match
// results' SideAID/SideBID, pools.csv's ID column, sub-bout winner
// attribution): drawing from an id-less roster would stamp those columns
// with blanks the moment the draw runs, permanently, since the draw is a
// one-time snapshot of the roster at that instant.
var ErrMissingParticipantIDsInDraw = errors.New("cannot draw: every competitor must have an id")

// ParticipantIDMissing reports whether id counts as "no id" -- blank, or
// whitespace-only. The ONE predicate the two notices below AND the load-time
// legacy repair (internal/state/legacy_upgrade.go) test a PARTICIPANT id
// against: the three used to disagree (both notices trimmed whitespace, the
// repair's own "does this row need work" scan did not), so a whitespace-only
// id was named by a notice the repair would never actually attempt to fix.
//
// Deliberately NOT the owner for the pool-matches notice: that one asks a
// different question, whether a MATCH ROW is missing a side or winner id,
// and it shares state.MatchResult.MissingSideOrWinnerID with its own half of
// the repair, so those two agree with each other rather than with this.
func ParticipantIDMissing(id string) bool {
	return strings.TrimSpace(id) == ""
}

// MissingParticipantIDsMessage names the id-less rows in players and states
// the remedy (a re-save mints one, participants.csv's one write chokepoint
// -- see internal/state/participants.go marshalParticipantsCSV), or returns
// "" when every row already has an id.
//
// Shared by ValidateNoMissingParticipantIDs below (the draw pre-flight) and
// the operator console's data-issues banner (missingIDsIssue,
// internal/mobileapp/handlers_viewer.go) so the two surfaces describe the
// exact same condition with the exact same words: one is a hard refusal, the
// other is advance warning before the operator ever tries to draw, and they
// must not drift into two different accounts of the same defect.
func MissingParticipantIDsMessage(players []Player) string {
	count := 0
	var labels []string
	for _, p := range players {
		if ParticipantIDMissing(p.ID) {
			count++
			if len(labels) < MaxNamedRows {
				labels = append(labels, playerLabel(p))
			}
		}
	}
	return NamedLabelsMessage(TruncatedLabels(count, labels, "competitors"), "no id on file. Save the roster once and the ids are assigned.")
}

// MaxNamedRows is the most affected rows any missing-id/no-id notice names
// individually before folding the rest into a "N <noun>, including" count.
const MaxNamedRows = 3

// playerLabel is the "Name (Dojo)" (or bare "Name" when Dojo is blank)
// label every player-identified notice names an affected row by.
func playerLabel(p Player) string {
	if p.Dojo != "" {
		return fmt.Sprintf("%s (%s)", p.Name, p.Dojo)
	}
	return p.Name
}

// TruncatedLabels collapses a count/first-three-labels pair into the
// []string NamedLabelsMessage expects: labels verbatim when count already
// fits within them, or a single pre-composed "N <noun>, including <label>,
// <label>, <label>" element when count exceeds len(labels) -- collapsing
// here (rather than inside NamedLabelsMessage) is what lets each caller use
// its own noun ("competitors" here, "match(es)" for
// engine.PoolMatchesMissingSideIDsMessage) while still sharing the same
// join-and-append-tail primitive. Returns nil when count is 0.
func TruncatedLabels(count int, labels []string, noun string) []string {
	if count == 0 {
		return nil
	}
	if count <= len(labels) {
		return labels
	}
	return []string{fmt.Sprintf("%d %s, including %s", count, noun, strings.Join(labels, ", "))}
}

// NamedLabelsMessage composes the "<who>: <tail>" sentence shared by every
// missing-id/no-id notice in this codebase -- participants.csv and
// pools.csv's collectors below (via TruncatedLabels), plus
// engine.PoolMatchesMissingSideIDsMessage (which maps its own affected rows
// to "SideA vs SideB" labels and collapses them the same way, with its own
// noun). Returns "" when labels is empty.
func NamedLabelsMessage(labels []string, tail string) string {
	if len(labels) == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %s", strings.Join(labels, ", "), tail)
}

// PoolsMissingParticipantIDsMessage names pools.csv rows (drawn pool
// members) that carry no participant id, and states the consequence and the
// residual remedy, or returns "" when every member already has one.
//
// A pools.csv row (helper.Pool.Players, column 8 on disk) is a record that
// carries an id field, so downstream consumers -- the numberPrefix-derived
// competitor number, standings, scoring, eligibility -- resolve it by id
// only (operator ruling bc-pnum): a member missing here gets no number and
// contributes nothing to any of those. The draw pipeline stamps ids on every
// drawn row and refuses to run over an id-less roster
// (ValidateNoMissingParticipantIDs), so a legacy pools.csv predating the id
// column is now repaired automatically at load time
// (state.upgradePoolParticipantIDsLocked resolves each row against
// participants.csv by an exact name+dojo match when the row's own dojo is
// non-empty, or the unique-bare-name fallback when it is blank -- see that
// function's own doc comment). This message therefore only ever names the
// residue that repair could not resolve: a row whose name/dojo does not
// match any current roster entry -- a hand edit, or a participant no longer
// on the roster -- not the whole legacy population. The remedy that still
// works for that residue is the same one that produced the ids in the first
// place: regenerate the draw while the competition is still draw-ready.
func PoolsMissingParticipantIDsMessage(pools []Pool) string {
	count := 0
	var labels []string
	for _, p := range pools {
		for _, pl := range p.Players {
			// pl.Name != "" matches the load-time repair's own
			// poolMemberMissingID gate (internal/state/legacy_upgrade.go):
			// a nameless row is an empty slot, not a competitor missing an
			// id, so it is neither attempted by the repair nor named here.
			if pl.Name != "" && ParticipantIDMissing(pl.ID) {
				count++
				if len(labels) < MaxNamedRows {
					labels = append(labels, playerLabel(pl))
				}
			}
		}
	}
	return NamedLabelsMessage(TruncatedLabels(count, labels, "competitors"), "no id in the pool draw and could not be matched to a participant automatically. No player number is assigned; regenerate the draw while it is still draw-ready to fix it.")
}

// ValidateNoMissingParticipantIDs is the draw pre-flight for bc-pnum ruling
// 1c: refuses to draw while any player in players has an empty id. Returns
// nil when every row already has one.
//
// Mirrors ValidateNoBlankDojo's shape and calling convention (both are
// roster pre-flights the engine's runDrawPipeline runs ahead of the format
// switch, so every format -- pools, playoffs, league, Swiss -- is covered by
// one check rather than by the pool distributor alone). Unlike blank dojo,
// there is no participant-SAVE-time floor to distinguish this from: every
// write path mints an id for an id-less row (marshalParticipantsCSV), so the
// only way a loaded roster still has one is a legacy participants.csv that
// predates that write and has never been re-saved -- the draw is the one
// place that must still catch it, because it is a one-time snapshot that
// would otherwise stamp blank ids into pools.csv / bracket.json forever.
func ValidateNoMissingParticipantIDs(players []Player) error {
	msg := MissingParticipantIDsMessage(players)
	if msg == "" {
		return nil
	}
	return fmt.Errorf("%w: %s", ErrMissingParticipantIDsInDraw, msg)
}

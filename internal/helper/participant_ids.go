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

// MissingParticipantIDsMessage names the id-less rows in players and states
// the remedy (a re-save mints one, participants.csv's one write chokepoint
// -- see internal/state/participants.go marshalParticipantsCSV), or returns
// "" when every row already has an id.
//
// Shared by ValidateNoMissingParticipantIDs below (the draw pre-flight) and
// the operator console's data-issues banner (missingParticipantIDsIssue,
// internal/mobileapp/handlers_viewer.go) so the two surfaces describe the
// exact same condition with the exact same words: one is a hard refusal, the
// other is advance warning before the operator ever tries to draw, and they
// must not drift into two different accounts of the same defect.
func MissingParticipantIDsMessage(players []Player) string {
	var missing []Player
	for _, p := range players {
		if strings.TrimSpace(p.ID) == "" {
			missing = append(missing, p)
		}
	}
	return namedPlayersMessage(missing, "no id on file. Save the roster once and the ids are assigned.")
}

// namedPlayersMessage composes the "<who>: <consequenceAndRemedy>" sentence
// shared by every missing-id notice (participants.csv, pools.csv, and
// pool-matches.csv's own composer in internal/engine): a competition's
// data-issues banner and the draw pre-flight must describe the same
// condition with the same words, so the naming/truncation rule lives in ONE
// place. Returns "" when missing is empty.
//
// Names at most the first three affected rows, followed by the total count,
// for a large roster: an operator doesn't need every name to understand what
// happened, and a wall of names would bury the remedy.
func namedPlayersMessage(missing []Player, consequenceAndRemedy string) string {
	const maxNamed = 3
	if len(missing) == 0 {
		return ""
	}
	label := func(p Player) string {
		if p.Dojo != "" {
			return fmt.Sprintf("%s (%s)", p.Name, p.Dojo)
		}
		return p.Name
	}
	named := missing
	if len(named) > maxNamed {
		named = named[:maxNamed]
	}
	names := make([]string, len(named))
	for i, p := range named {
		names[i] = label(p)
	}
	who := strings.Join(names, ", ")
	if len(missing) > maxNamed {
		who = fmt.Sprintf("%d competitors, including %s", len(missing), who)
	}
	return fmt.Sprintf("%s: %s", who, consequenceAndRemedy)
}

// PoolsMissingParticipantIDsMessage names pools.csv rows (drawn pool
// members) that carry no participant id, and states the consequence and
// remedy, or returns "" when every member already has one.
//
// A pools.csv row (helper.Pool.Players, column 8 on disk) is a record that
// carries an id field, so downstream consumers -- the numberPrefix-derived
// competitor number, standings, scoring, eligibility -- resolve it by id
// only (operator ruling bc-pnum): a member missing here gets no number and
// contributes nothing to any of those. The draw pipeline stamps ids on every
// drawn row and refuses to run over an id-less roster
// (ValidateNoMissingParticipantIDs), so a pools.csv row missing one is
// leftover data from before that fix, or a hand-edited file -- the remedy is
// to regenerate the draw while the competition is still draw-ready, not a
// participants.csv re-save (which does not touch pools.csv at all).
func PoolsMissingParticipantIDsMessage(pools []Pool) string {
	var missing []Player
	for _, p := range pools {
		for _, pl := range p.Players {
			if strings.TrimSpace(pl.ID) == "" {
				missing = append(missing, pl)
			}
		}
	}
	return namedPlayersMessage(missing, "no id in the pool draw. No player number is assigned; regenerate the draw while it is still draw-ready.")
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

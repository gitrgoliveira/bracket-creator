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
//
// Names at most the first three affected rows, followed by the total count,
// for a large roster: an operator doesn't need every name to understand what
// happened, and a wall of names would bury the remedy.
func MissingParticipantIDsMessage(players []Player) string {
	const maxNamed = 3
	var missing []Player
	for _, p := range players {
		if strings.TrimSpace(p.ID) == "" {
			missing = append(missing, p)
		}
	}
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
	return fmt.Sprintf("%s: no id on file. Save the roster once and the ids are assigned.", who)
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

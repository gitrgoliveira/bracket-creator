package domain

import "fmt"

// TeamMember is one person on a team's squad (bc-tmid): the actual kendoka
// who steps onto the court, as distinct from the team itself, which is the
// PARTICIPANT (it carries the stable id and competitor number every other
// entity in this codebase keys on). Before this type existed a member was
// only a free-floating string, either a lineup slot's bare NAME
// (TeamLineup.Positions) or an untyped trailing column on the team's own
// participant row (Player.Metadata, an array shared -- ambiguously -- with
// an individual's dan grade at index 0). Neither survives a rename, and
// neither can be told apart from a different member sharing the same name
// once one of them has already fought.
type TeamMember struct {
	// ID is a minted UUID, stable for the member's life. A member may be
	// renamed (Name changes) but never re-identified: everything that
	// needs to remember "this specific person, regardless of what they are
	// called today" -- bout logs, kachinuki retirement, standings -- keys
	// on this field, never on Name.
	ID string `json:"id" yaml:"id"`

	// Index is the 1-based DISPLAY index, minted once when the member is
	// added (or seeded, see below) and never reused: the ENTRY itself is
	// never removed, so no index is ever freed. "Removal" in the operator's
	// own words (bc-pnum ruling) is CLEARING Name back to "" -- a bout
	// already fought refers to a position by this index, and the label
	// (e.g. "T10.4") must keep meaning what it always meant, which a freed
	// or reused index would break.
	//
	// It is stored rather than derived from the member's position in the
	// squad slice because the label an operator reads is the team's
	// competitor number plus this index -- e.g. "T10.1" -- and a
	// competitor number is MUTABLE: changing a competition's number prefix
	// renumbers every competitor (engine.RenumberCompetitors), so the LABEL
	// regenerates on demand from the team's CURRENT number, exactly like
	// every other competitor number, while the member's own identity (this
	// Index, paired with ID) stays fixed underneath it. Deriving Index from
	// slice position instead would silently renumber every later member
	// the moment an earlier one was removed -- moot today (an entry is
	// never removed), but the reason this field is persisted data rather
	// than a computed len()-based value is that "no entry removal" is an
	// operator RULING, not a language guarantee: the stored index does not
	// need that ruling to hold forever in order to stay correct.
	//
	// Do NOT add a "next index" counter alongside this. With no entry
	// removal, the next index is always max(existing Index)+1: a stored
	// counter would be a second, DERIVABLE representation of the same fact,
	// and a derivable value kept in two places is a value that can disagree
	// with itself the moment one of the two writes is missed.
	Index int `json:"index" yaml:"index"`

	// Name is the member's display name. It may be changed by
	// RenameTeamMember without affecting ID or Index, and CLEARED back to ""
	// by ClearTeamMemberName -- the operator's "removal" -- which is
	// likewise refused from touching ID or Index and, per the same ruling,
	// refused outright once the competition has started (state.CanStart).
	// A blank Name is a normal, expected state, not an absence: a team's
	// squad is SEEDED with the competition's TeamSize members on load
	// (state.upgradeSquadsFromMetadataLocked), each already carrying its ID
	// and Index, with Name blank until filled in.
	Name string `json:"name" yaml:"name"`
}

// SquadMemberLabel composes a squad member's visible label (bc-pnum, "make
// a team member's label available to the public surfaces") from the team's
// competitor NUMBER and the member's stable display INDEX, e.g. "T10.1".
// This is the Go twin of web-mobile/js/squad_member_label.jsx's
// squadMemberLabel, the JS side's own single owner of the identical
// composition; the two must never drift apart on the "number, dot, index"
// format. internal/engine/kachinuki_export.go (the Kachinuki Detail Excel
// sheet) is this function's one caller on the Go side, so the printed
// record and the live app compose the same label rather than two
// hand-rolled copies of the same format string.
//
// Returns "" when teamNumber is empty (pre-draw, or a competitor excluded
// from the draw -- there is no meaningful label for a member of a team
// that itself has no number) or when memberIndex is not a real 1-based
// index (<= 0; every real member is minted with one starting at 1, so this
// branch is defensive only, mirroring the JS side's own defensive check).
func SquadMemberLabel(teamNumber string, memberIndex int) string {
	if teamNumber == "" || memberIndex <= 0 {
		return ""
	}
	return fmt.Sprintf("%s.%d", teamNumber, memberIndex)
}

// squad_member_label.jsx: the ONE place that composes a squad member's
// visible label (bc-tmid pass 3) from the team's competitor NUMBER and the
// member's stable display INDEX (domain.TeamMember.Index), e.g. "T10.1".
//
// The competitor-number CHIP has its own owner, NumberedName
// (numbered_name.jsx); this module composes the member LABEL
// "<number>.<index>" and is not a second chip renderer. It is a leaf with no
// imports and no window.* dependency, so every surface that shows a member
// label ES-imports squadMemberLabel directly rather than restating the
// "<number>.<index>" composition itself, per this repo's rule that a
// display contract lives in one primitive.
//
// The label REGENERATES from the team's CURRENT competitor number on every
// call; it is never stored. A competition's number prefix is mutable after
// the draw (engine.RenumberCompetitors renumbers every competitor when the
// operator changes it), so a persisted label would go stale the moment a
// renumber happened, while the member's real identity -- TeamMember.ID
// paired with TeamMember.Index -- does not change underneath it. Computing
// this on every render is what keeps the label honest across a renumber;
// caching or storing it would silently drift the first time the operator
// renumbers.
//
// Returns "" when the team has no number assigned yet (pre-draw, or a
// competitor excluded from the draw) -- a spectator surface shows a bare
// name then, never an operator's slot handle -- and also when the member
// has no index (defensive; every real member is minted with one).
export function squadMemberLabel(teamNumber, memberIndex) {
  if (!teamNumber) return "";
  if (memberIndex === null || memberIndex === undefined || memberIndex === "") return "";
  return `${teamNumber}.${memberIndex}`;
}

// squadSlotLabel is the OPERATOR's handle for a squad slot on the surfaces
// used before the draw (the Lineups page, the Settings squad editor), where
// squadMemberLabel is "" and a blank slot otherwise rendered as an empty
// option, an empty label column and an aria-label of "Name for " (bc-dnst):
// the numbered label once the team has a number, else the member's index
// alone, "Slot 3", the one stable handle a member always has. Owned here,
// beside the label it falls back from, rather than as a per-surface `||`
// tail, so every operator surface names a slot the same way; spectator
// surfaces keep calling squadMemberLabel and stay bare before the draw.
export function squadSlotLabel(teamNumber, memberIndex) {
  const numbered = squadMemberLabel(teamNumber, memberIndex);
  if (numbered) return numbered;
  if (memberIndex === null || memberIndex === undefined || memberIndex === "") return "";
  return `Slot ${memberIndex}`;
}

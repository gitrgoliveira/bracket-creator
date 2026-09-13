// squad_member_label.jsx: the ONE place that composes a squad member's
// visible label (bc-tmid pass 3) from the team's competitor NUMBER and the
// member's stable display INDEX (domain.TeamMember.Index), e.g. "T10.1".
//
// There is no existing shared competitor-number renderer in this codebase
// (the "<span className=num-prefix>{p.number}</span>" idiom is repeated
// inline at roughly ten call sites, none of them a function); this module
// is deliberately NOT one more of those. It is a leaf with no imports and
// no window.* dependency, so every surface that shows a member label
// ES-imports squadMemberLabel directly rather than restating the
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
// competitor excluded from the draw) -- there is no meaningful label for a
// member of a team that itself has no number -- and also when the member
// has no index (defensive; every real member is minted with one).
export function squadMemberLabel(teamNumber, memberIndex) {
  if (!teamNumber) return "";
  if (memberIndex === null || memberIndex === undefined || memberIndex === "") return "";
  return `${teamNumber}.${memberIndex}`;
}

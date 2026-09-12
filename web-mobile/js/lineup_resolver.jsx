// lineup_resolver.js: shared helpers for resolving per-match and round-scoped
// team lineups across all consumer surfaces (admin scoring modal, viewer,
// TvDisplay, StreamingOverlay).
//
// Do NOT import from admin_lineup.jsx; that module is an admin input panel
// and may not be loaded on public/viewer surfaces.
//
// API shape expected:
//   API.fetchMatchLineup(compId, teamId, matchId) → lineup | null
//   API.fetchTeamLineup(compId, teamId, round)    → lineup | null
//
// Lineup shape:
//   { teamId, positions: { [posKey]: playerName }, memberIds: { [posKey]: memberId } }
// where posKey is a named FIK position ("senpo", "jiho", ...) for 5-person
// teams, or a numeric string "1".."N" for other sizes. `memberIds` is the
// squad member id half of a lineup pick (bc-tmid pass 3), keyed by the SAME
// posKey as `positions`; a lineup saved before squads existed simply omits it.

import { squadMemberLabel } from './squad_member_label.jsx';

// resolveMatchLineup: prefer the per-match lineup endpoint (GET
// match-lineups/:matchId); fall back to the round lineup when no per-match
// entry exists (404 → null → round lookup). Network errors on either
// endpoint are swallowed so the caller degrades gracefully.
//
// The round step passes { fallback: true }: match-scoring surfaces are the
// client-side twin of AMENDMENT 1, so when the match's own round has no
// saved lineup the server resolves the closest saved round instead of 404
// (operators typically save one round-0 lineup for the whole day; without
// this, a knockout final at round index 1 got no names and kachinuki bout 1
// was submitted with empty sides). The lineup EDITOR calls fetchTeamLineup
// directly without the flag, so its exact + 404 semantics are unchanged.
//
// mp-bkg regression guard: the per-match endpoint must win when it returns a
// non-null result (the whole point of the per-match API). This function is
// tested directly in scoring_modal_match_lineup.test.jsx.
export async function resolveMatchLineup(compId, teamId, matchId, round, { fetchMatchLineup, fetchTeamLineup }) {
  try {
    const matchLineup = await fetchMatchLineup(compId, teamId, matchId);
    if (matchLineup !== null) return matchLineup;
  } catch (_e) { /* network: fall through */ }
  try {
    return await fetchTeamLineup(compId, teamId, round, { fallback: true });
  } catch (_e) { /* 404 / network: ignore */ }
  return null;
}

// resolveLineupTeamId maps a match-side key to the participant id that
// lineups are stored under. A match side's `id`, once resolved
// (api_serializers.resolveSide), is EITHER the participant's real id (a
// UUID) or "" -- resolveSide never invents an id from the display name.
// Callers build `sideKey` via sideLookupKey(side) (competitor_identity.jsx),
// so an unresolved side (id "") still falls through to its NAME here, and
// TeamLineups are keyed server-side by whatever team key was used when the
// lineup was saved; in practice, that's the participant's real id. Passing
// a bare name straight through can make the lineup GET 404 and the
// per-match (and round) lineup never reaches the scoring grid. We look the
// side up in the competition's participant list by id OR name and return
// its real id, falling back to the original key when unmatched.
//
// bc-pnum: callers pass sideLookupKey(side) deliberately -- the name arm
// recovers a real id for an id-less side. No object overload (YAGNI).
export function resolveLineupTeamId(sideKey, players) {
  if (!sideKey) return "";
  const list = Array.isArray(players) ? players : [];
  const p = list.find(pl => pl
    && (pl.id === sideKey || pl.ID === sideKey || pl.name === sideKey || pl.Name === sideKey));
  return (p && (p.id || p.ID)) || sideKey;
}

// FIK named position KEYS for 5-person teams (index 0=senpo … 4=taisho).
// Used by pickFromLineup to look up positions[key] in the lineup object.
export const POS_KEYS_5 = ["senpo", "jiho", "chuken", "fukusho", "taisho"];
// Title-case labels for display (Senpo, Jiho, ...), derived from POS_KEYS_5.
// Consumed by admin_scoring_team.jsx and streaming_overlay.jsx (single source).
export const POS_LABELS_5 = POS_KEYS_5.map((s) => s.charAt(0).toUpperCase() + s.slice(1));

// resolveBoutSideName: which name identifies one side of a sub-bout row.
// KACHINUKI numbered bouts are SERVER-FIRST: the engine appended the
// pairing via winner-stays advancement (bout 5 is "winner of bout 4 vs
// next in queue", NOT "taisho vs taisho"), so an existing SubResult name
// must never be overwritten by a lineup-position lookup. The lineup only
// seeds the bootstrapped bout 1 before the first submit. Fixed-format
// matches and the daihyosen row stay LINEUP-FIRST: lineups are always
// editable and drive fixed position-vs-position pairings.
export function resolveBoutSideName({ isKachinuki, isDaihyosen, existingName, lineupName }) {
  if (isKachinuki && !isDaihyosen) return existingName || lineupName || "";
  return lineupName || existingName || "";
}

// kachinukiHidesLineupPosition: for a kachinuki NUMBERED bout past the
// bootstrap (index 0), the lineup position no longer identifies who fights
// (winner-stays advancement determines the pairing), so display surfaces must
// suppress the position-lineup name and fall back to the bout number. The
// daihyosen (isDaihyosen) is an operator-chosen rep bout and stays lineup-first.
// Shared by the display surfaces (match_scoreboard, streaming_overlay) so the
// bootstrap rule lives in one place.
export function kachinukiHidesLineupPosition(isKachinuki, isDaihyosen, index) {
  return !!isKachinuki && !isDaihyosen && index !== 0;
}

// pickFromLineup: resolves the player name at a given bout index from a
// lineup object. 5-person teams use named position keys; other sizes use
// the numeric string "1".."N". Returns "" when the lineup has no entry for
// that position.
export function pickFromLineup(lineup, index, teamSize) {
  if (!lineup || !lineup.positions) return "";
  if (teamSize === 5 && index >= 0 && index < 5) {
    const named = lineup.positions[POS_KEYS_5[index]];
    if (named) return named;
  }
  const numeric = lineup.positions[String(index + 1)];
  if (numeric) return numeric;
  return "";
}

// pickMemberIdFromLineup: the memberIds twin of pickFromLineup, resolving the
// squad MEMBER ID pinned at a lineup position rather than the name. Mirrors
// the SAME posKey5/numeric priority so the id can only ever pair with the
// name pickFromLineup resolves from the SAME position for the SAME row.
// Returns "" when the lineup carries no memberIds map (a lineup saved before
// squads existed) or no entry at that position.
export function pickMemberIdFromLineup(lineup, index, teamSize) {
  if (!lineup || !lineup.memberIds) return "";
  if (teamSize === 5 && index >= 0 && index < 5) {
    const named = lineup.memberIds[POS_KEYS_5[index]];
    if (named) return named;
  }
  const numeric = lineup.memberIds[String(index + 1)];
  if (numeric) return numeric;
  return "";
}

// resolveBoutSideMemberId: which squad MEMBER ID identifies one side of a
// sub-bout row (bc-pnum: extend the squad member label -- squadMemberLabel,
// squad_member_label.jsx -- to the team scoring surfaces). MIRRORS
// resolveBoutSideName's kachinuki-vs-fixed priority exactly: the id must
// come from the SAME source tier the name was resolved from (a kachinuki
// numbered bout's pairing is server-bout-log first, the SubMatchResult's own
// sideAMemberId/sideBMemberId -- backfilled from squads.yaml by the
// legacy-upgrade repair -- so the lineup position's id must never outrank
// it; fixed-format and the daihyosen row stay lineup-first). Callers pass
// existingMemberId/lineupMemberId from the SAME existing/lineup objects
// resolveBoutSideName was given for the SAME row, and must independently
// block an operator's free-typed override (which has no valid lineup key to
// resolve an id from at all) before ever reaching this function -- see
// admin_scoring_team.jsx's playerNamesForBout for that guard.
export function resolveBoutSideMemberId({ isKachinuki, isDaihyosen, existingMemberId, lineupMemberId }) {
  if (isKachinuki && !isDaihyosen) return existingMemberId || lineupMemberId || "";
  return lineupMemberId || existingMemberId || "";
}

// resolveSquadMember: the squad member a bout side's (memberId, name) pair
// identifies -- id first, an exact NAME match only when no id resolved (a
// manually-typed/free bout, or a row the legacy-upgrade repair has not
// reached yet). Squad member names are unique WITHIN one team
// (server-enforced, bc-tmdup), so the name fallback cannot be ambiguous and
// needs no second key the way a cross-team lookup would.
//
// Returns null when nothing in `squad` matches: an empty/not-yet-loaded
// squad, or a name that names no squad member at all.
// squadMemberIdForUniqueName: the id of the ONE squad member carrying `name`,
// or "" when nobody does or more than one does.
//
// Deliberately stricter than resolveSquadMember below, which takes the first
// name match. That looseness is right for a LABEL -- showing "T10.1" beside
// the wrong twin is a cosmetic slip on a row the operator can read -- and
// wrong for anything WRITTEN, where the id becomes the record of who fought.
// Members of one team may share a display name in rosters that predate the
// uniqueness rule, so the gate is not theoretical.
export function squadMemberIdForUniqueName(squad, name) {
  const wanted = (name || "").trim();
  if (!wanted) return "";
  const hits = (Array.isArray(squad) ? squad : []).filter(mem => mem && mem.name === wanted);
  return hits.length === 1 ? (hits[0].id || "") : "";
}

export function resolveSquadMember(squad, memberId, name) {
  const list = Array.isArray(squad) ? squad : [];
  if (memberId) {
    const byId = list.find(mem => mem && mem.id === memberId);
    if (byId) return byId;
  }
  if (!name) return null;
  return list.find(mem => mem && mem.name === name) || null;
}

// resolveBoutSideSquadLabel: the squad member label riding beside a bout
// row's fighter name on the PUBLIC surfaces (operator ruling: "the label
// must be visible everywhere, together with the name"). Composes the same
// chain every consumer must use, so the composition is stated once: first
// resolveBoutSideMemberId (the member id, through the SAME kachinuki/
// fixed-format tier resolveBoutSideName used for the name -- an id must
// never label a different fighter than the name shown beside it), then
// resolveSquadMember (id first, an exact name fallback -- squad member
// names are unique within one team, so the fallback needs no second key),
// then squadMemberLabel (the one "number.index" composer). Returns "" when
// the fighter matches no squad member or the team carries no competitor
// number yet -- never a stray separator.
export function resolveBoutSideSquadLabel({ isKachinuki, isDaihyosen, existingMemberId, lineupMemberId, squad, name, teamNumber }) {
  const memberId = resolveBoutSideMemberId({ isKachinuki, isDaihyosen, existingMemberId, lineupMemberId });
  const member = resolveSquadMember(squad, memberId, name);
  return member ? squadMemberLabel(teamNumber, member.index) : "";
}

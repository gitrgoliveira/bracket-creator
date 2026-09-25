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

import { squadMemberLabel, squadSlotLabel } from './squad_member_label.jsx';

// squadRosterEntries: the ONE builder of a lineup picker's list for a team
// (bc-dnst), shared by the score sheet's per-row picker (admin_scoring_team
// .jsx rosterForSide) and the Up Next "Enter lineup" panel
// (admin_schedule_lineup.jsx): the squad's members first, every one of them
// in index order as `{id, index, name, label}` objects so a still-blank slot
// is offered by its number alone (operator ruling 2026-09-15). The LEGACY
// names (the pre-squad metadata roster, plus whatever
// `mergeRosterWithAssigned` re-adds for the lineup's own assigned names) are
// appended ONLY when the squad itself is empty: the fallback exists for
// exactly two "no squad" cases, a legacy team that predates squads
// altogether (no id at all) and a squad fetch that failed, and a squad that
// IS present already lists every member the legacy roster could offer, so
// mixing the two would only reintroduce the stale pre-rename names
// resolveBoutSideDisplayName exists to stop showing. Reads
// mergeRosterWithAssigned off window.AdminLineupHelpers (this module must
// not import admin_lineup.jsx, see the header) and copes with its absence.
export function squadRosterEntries({ teamNumber, squad, legacyNames, lineup }) {
  const squadEntries = (Array.isArray(squad) ? squad : [])
    .slice()
    .sort((x, y) => (x?.index || 0) - (y?.index || 0))
    .map(mem => ({
      id: mem?.id || "",
      index: mem?.index || 0,
      name: String(mem?.name || "").trim(),
      // squadSlotLabel, not squadMemberLabel: before the draw a team has no
      // number, so the numbered form is "" and every seeded blank slot rendered
      // as an identical unlabelled "no name yet" row -- the operator could not
      // tell which slot they had picked, nor which they had already used, while
      // the Lineups page showed "Slot 1".."Slot 7" for the same members. These
      // are all OPERATOR pickers, which is exactly what squadSlotLabel is for;
      // spectator surfaces keep squadMemberLabel and stay bare (bc-dnst).
      label: squadSlotLabel(teamNumber, mem?.index),
    }));
  if (squadEntries.length > 0) {
    // A name the LINEUP already holds that NO squad member carries is
    // appended, as a plain string: the same shape the no-squad branch below
    // returns, and one every consumer already handles.
    //
    // This is not defensive. It is the documented mint-failure path:
    // resolveMemberIdsForPositions saves a substitute BY NAME with no member
    // id when addTeamMember fails, and deliberately never blocks the write for
    // it. Such a fighter is in neither the squad nor the legacy metadata, so
    // without this tail they are in NO list at all. The score sheet applied
    // mergeRosterWithAssigned for exactly this before the squad list arrived;
    // this restores that parity rather than adding something new.
    //
    // Scope, stated precisely because it is narrower than it looks: on the
    // FILTERED pickers rosterWithoutPlacedElsewhere then drops this entry at
    // every position except the one already holding the name, which is
    // correct, since one fighter holds one position. Where the tail actually
    // shows is the UNFILTERED list a kachinuki row past the first offers,
    // where the fighter who stayed on legitimately repeats.
    const known = new Set(squadEntries.map(e => e.name.toLowerCase()).filter(Boolean));
    const assigned = [];
    for (const raw of Object.values(lineup?.positions || {})) {
      const name = String(raw == null ? "" : raw).trim();
      const key = name.toLowerCase();
      if (!key || known.has(key)) continue;
      known.add(key);
      assigned.push(name);
    }
    return assigned.length ? [...squadEntries, ...assigned] : squadEntries;
  }
  const seen = new Set();
  const merge = (typeof window !== "undefined" && window.AdminLineupHelpers?.mergeRosterWithAssigned)
    ? window.AdminLineupHelpers.mergeRosterWithAssigned
    : (names) => names;
  const legacyEntries = merge(Array.isArray(legacyNames) ? legacyNames : [], lineup).filter(n => {
    const key = String(n).trim().toLowerCase();
    if (!key || seen.has(key)) return false;
    seen.add(key);
    return true;
  });
  return legacyEntries;
}

// memberPlacedElsewhere: the ONE predicate behind "a member holds one
// position per lineup" (bc-dnst): the position key at which `memberIds`
// already holds `id` other than `posKey`, or "" when it holds it nowhere
// else. Every lineup writer asks it before writing (the score sheet's
// buildInlineLineupWrite, the Up Next panel's save, the Lineups page's add
// path) and rosterWithoutPlacedElsewhere below is the same answer rendered
// as a list filter, so the offer and the refusal can never disagree; the
// server's own ErrLineupDuplicateMember stays an unreachable backstop.
//
// rosterWithoutPlacedElsewhere: a fixed-order lineup fields each fighter
// once, so the list offered for `posKey` drops every entry already placed at
// ANOTHER position of `lineup` (bc-dnst; a click test once caught the list
// offering a fighter twice and letting him be placed on two rows). An object
// entry is dropped when its id appears in lineup.memberIds elsewhere, or its
// name matches a NON-BLANK name elsewhere; a blank-named object entry is
// compared by id only, since every blank slot shares the same empty name
// and matching on it would hide every other blank slot the moment one of
// them is placed. A plain legacy string is compared by name. Shared by the
// same two pickers as squadRosterEntries.
export function memberPlacedElsewhere(memberIds, posKey, id) {
  if (!id) return "";
  const hit = Object.entries(memberIds || {}).find(([key, other]) => key !== posKey && other === id);
  return hit ? hit[0] : "";
}

export function rosterWithoutPlacedElsewhere(roster, lineup, posKey) {
  const otherNames = new Set(Object.entries(lineup?.positions || {})
    .filter(([key, name]) => key !== posKey && String(name || "").trim())
    .map(([, name]) => String(name).trim().toLowerCase()));
  return roster.filter(entry => {
    if (typeof entry === "string") return !otherNames.has(entry.trim().toLowerCase());
    // An entry that CARRIES an id is judged by that id alone: "a record that
    // carries an id field is resolved by id only" (CLAUDE.md, bc-pnum). A name
    // arm beside the id arm hid an UNPLACED member from every picker whenever a
    // teammate placed elsewhere shared their display name -- same-name
    // teammates exist on rosters predating the uniqueness rule, as
    // squadMemberIdForUniqueName's own comment says -- while
    // memberPlacedElsewhere, the write-time predicate this list must agree
    // with, is id-only and would have allowed them (bc-dnst).
    //
    // So ASK that predicate rather than re-deriving its answer from a local
    // Set: "the offer and the refusal can never disagree" then holds by
    // construction instead of by discipline, and a squad is single digits.
    if (entry.id) return !memberPlacedElsewhere(lineup?.memberIds, posKey, entry.id);
    // No id: the legacy shape, placed by name, so judged by name.
    if (!entry.name) return true;
    return !otherNames.has(entry.name.trim().toLowerCase());
  });
}

// mergeLineupIdsForPosition composes the WHOLE memberIds map an inline
// lineup write sends: carries `existingIds` forward untouched, then either
// sets `posKey` to `resolvedId` or CLEARS it -- clearing happens both when
// the operator cleared the position (no name, so nothing to resolve) and
// when a name was typed/picked but resolution/minting failed (offline venue
// wifi). Either way a stale id must never survive under a position it no
// longer names: kachinuki retirement keys on the member id (CLAUDE.md §
// Team Lineups & Kachinuki), so a stale id left behind would attribute a
// bout to whoever used to occupy that slot. Exported so this exact rule is
// pinned without mounting TeamScoreEditorModal.
export function mergeLineupIdsForPosition(existingIds, posKey, resolvedId) {
  const updated = { ...existingIds };
  if (resolvedId) updated[posKey] = resolvedId;
  else delete updated[posKey];
  return updated;
}

// buildInlineLineupWrite computes exactly what the inline lineup picker
// (submitInlineLineup, inside TeamScoreEditorModal in admin_scoring_team.jsx)
// sends to putMatchLineup: the WHOLE positions map (existing + the one
// changed position) and its memberIds counterpart, merged via
// mergeLineupIdsForPosition above. Exported (and pulled out of the
// component) so this exact value-in/body-out contract -- including "a mint
// failure never blocks the write" -- is pinned directly, without mounting
// TeamScoreEditorModal, which vitest's hook stubs cannot drive through a
// full interaction (see tie_button_no_term.test.jsx). It lives here, not in
// the scoring module, because it IS the lineup-write authority every other
// writer (the Up Next panel's save, the Lineups page's add path) already
// pointed at in prose -- a resolver pointing at a scoring component module
// for its own contract was backwards (bc-rvfx).
//
// member (bc-dnst) is the squad-member object LineupNameInput hands back
// when the operator picked one of the row's numbered entries, rather than
// typing/"+ Add"-ing a free name. When member carries an id, this WRITES
// BY ID directly -- positions[posKey] = member.name || "" and
// memberIds[posKey] = member.id -- and never calls the resolver: the
// picked entry already names its own member, so resolving by name would be
// redundant at best and wrong at worst (two blank members share the same
// "" name). This is also why a picked member's position is kept even when
// its name is empty: picking a blank slot is a real placement (the row
// shows that member's number and an empty box to type the name into), not
// a clear. A falsy `value` with NO member is the only thing that clears
// the position, matching the pre-bc-dnst contract exactly.
//
// Without a member, the pre-existing name-resolution path runs: a name is
// only resolved when `value` is truthy (a cleared position has no name to
// look up, and mergeLineupIdsForPosition's own clear-on-falsy-id branch
// handles it), via resolveMemberIdsForPositions (admin_lineup.jsx, reached
// here via window.AdminLineupHelpers -- see this file's own header for why
// this stays a window lookup rather than an ES import).
//
// bc-cse gap closure: also returns `failures` (the resolver's own, or []
// when the resolver is unavailable/threw, or never invoked because member
// already answered) so submitInlineLineup can warn the operator on a
// successful save without ever blocking this one.
//
// bc-dnst duplicate guard: a member can only ever occupy ONE position at a
// time, so once the id for `posKey` is known (picked directly, or resolved
// from a typed name that matched an existing member), this checks the
// lineup's OWN memberIds -- the state BEFORE this write -- for that same id
// under a DIFFERENT position. A hit means the operator just tried to place
// someone who is already fighting elsewhere in this same encounter; the
// write is refused entirely (no positions/memberIds returned, nothing is
// sent to the server) and the caller is told which position already holds
// them via `refused`, so it can leave the box exactly as it was and tell
// the operator rather than silently doubling the member up server-side
// (internal/domain/team_lineup.go's ValidatePositions rejects the same
// case, but refusing here means the operator never round-trips to the
// server to find out).
export async function buildInlineLineupWrite(compId, teamId, lineup, squad, posKey, value, password, member) {
  const existing = lineup?.positions || {};
  const positions = { ...existing };
  const pickedMemberId = member && member.id ? member.id : null;
  if (pickedMemberId) positions[posKey] = member.name || "";
  else if (value) positions[posKey] = value;
  else delete positions[posKey];

  let resolvedId = pickedMemberId;
  let nextSquad = Array.isArray(squad) ? squad : [];
  let failures = [];
  if (!pickedMemberId && value) {
    try {
      const resolver = window.AdminLineupHelpers?.resolveMemberIdsForPositions;
      if (typeof resolver === "function") {
        // currentIds: a name typed into a slot PICKED by number (its
        // memberId already set on this position) renames that same member
        // rather than falling back to the position's index default -- see
        // resolveMemberIdsForPositions' own doc comment.
        const resolved = await resolver(compId, teamId, { [posKey]: value }, squad, password, lineup?.memberIds);
        nextSquad = resolved.squad;
        resolvedId = resolved.memberIds[posKey] || null;
        failures = resolved.failures || [];
      }
    } catch (_e) {
      // On top of the helper's own per-position mint catch, because this
      // runs on the live scoring path: an operator swapping a fighter
      // mid-encounter must not lose the swap because identity resolution
      // failed. The NAME is the load-bearing half of a lineup slot and the
      // id is an enhancement over it, so the write proceeds either way;
      // mergeLineupIdsForPosition below clears this position's id, the same
      // as any other unresolved name, and the load-time repair fills it in
      // later from the squad.
    }
  }

  const otherPosKey = memberPlacedElsewhere(lineup?.memberIds, posKey, resolvedId);
  if (otherPosKey) {
    return { refused: { position: otherPosKey, name: value || (member && (member.name || member.label)) || "" } };
  }

  const memberIds = mergeLineupIdsForPosition(lineup?.memberIds, posKey, resolvedId);
  return { positions, memberIds, squad: nextSquad, failures };
}

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
//
// teamNameA/teamNameB close a trap that only shows with NO lineup set
// (bc-dnst). A FIXED-ORDER bout settles at the match level. Rows written
// before that rule put the TEAM's name into sideA/sideB, and the server's
// attribution reads it (state.SubBoutWinnerSide matches sub.Winner against
// the match-level names). That stored value is therefore not a fighter's
// name, and falling back to it dressed the team's own name up as the person
// fighting: score one mark with no lineup and every position, untouched ones
// included, came back named after the team. A lineup hid it, because the
// lineup name wins.
//
// BOTH team names, not just the row's own side: that is the check the TV
// board carried locally for the quick-score shape (match_scoreboard's
// subSideName, now deleted in favour of this), and narrowing it to one side
// would have quietly dropped coverage for a row whose sides are crossed by
// hand-edited data. Passing them is how a caller opts in; a caller with no
// team names to give (none today) simply gets the old behaviour.
//
// The team-name test applies to KACHINUKI too, and must run BEFORE its
// server-first return. Today's engine writes real fighter names per pairing, so
// a team name should never land in that field -- but rows that already carry
// one exist (the quick-score synth shape state.SubBoutWinnerSide's own team
// arms answer for, a hand-edited bracket.json, an imported tournament), and the
// TV board filtered them for years before this rule moved here. Scoping the
// test below the kachinuki return dropped exactly that coverage and printed the
// team's own name in the fighter slot of every bout row.
// THE DAIHYOSEN ROW IS EXEMPT, and that is not a detail. It is the one bout
// whose sideA/sideB ARE the two team names by rule: buildPatch keeps them
// there so placeHt can put the hantei mark on the winner's side, and blanking
// them drops the mark from the wire. Filtering them here blanked both of that
// row's name cells, and the rep bout is the ONE row with no fallback label
// (its lineup key is the literal "daihyosen", which no lineup carries, and it
// offers no roster or typeable box), so the editor printed "-" against both
// teams where the two names used to be.
export function resolveBoutSideName({ isKachinuki, isDaihyosen, existingName, lineupName, teamNameA, teamNameB }) {
  const namesATeam = existingName === teamNameA || existingName === teamNameB;
  const stored = namesATeam && !isDaihyosen ? "" : existingName;
  if (isKachinuki && !isDaihyosen) return stored || lineupName || "";
  return lineupName || stored || "";
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


// lineupTaisho / kachinukiTaishoPairing (bc-kten, operator ruling 2026-09-25):
// in a kachinuki encounter only the very last bout, taisho against taisho,
// may go to encho. JS twins of domain.TeamLineup.Taisho and
// domain.KachinukiTaishoPairing; both are pinned by the shared table
// internal/domain/testdata/kachinuki_taisho.json. The position walk mirrors
// Go's canonicalPositionOrder EXACTLY (the five FIK names for a 5-person team,
// else "1".."N"), so a 5-person lineup saved under numeric keys reads as
// empty here as it does to the server: unlike pickFromLineup, no numeric
// fallback, or the editor would offer what the server refuses.
function taishoPositionKeys(teamSize) {
  if (teamSize === 5) return POS_KEYS_5;
  return Array.from({ length: Math.max(0, teamSize || 0) }, (_, i) => String(i + 1));
}

// The lineup's taisho: its LAST occupied position ({name, memberId}), a
// position being occupied when it carries a name OR a member id. null when
// there is no lineup or nothing is placed.
export function lineupTaisho(lineup, teamSize) {
  if (!lineup) return null;
  const positions = lineup.positions || {};
  const ids = lineup.memberIds || {};
  let last = null;
  for (const key of taishoPositionKeys(teamSize)) {
    const name = positions[key] || "";
    const memberId = ids[key] || "";
    if (!name && !memberId) continue;
    last = { name, memberId };
  }
  return last;
}

function taishoHolds(slot, fighter) {
  if (fighter.memberId && slot.memberId) return fighter.memberId === slot.memberId;
  return !!fighter.name && fighter.name === slot.name;
}

// Whether a kachinuki bout between fighters a (Aka) and b (Shiro) is taisho
// against taisho. known is false when either side has no lineup or an empty
// one: the app then cannot tell who the taisho is and must not withhold
// Encho, since a tied knockout bout already has End match held back.
export function kachinukiTaishoPairing({ teamSize, lineupA, lineupB, a, b }) {
  const ta = lineupTaisho(lineupA, teamSize);
  const tb = lineupTaisho(lineupB, teamSize);
  if (!ta || !tb) return { taisho: false, known: false };
  return { taisho: taishoHolds(ta, a || {}) && taishoHolds(tb, b || {}), known: true };
}

// resolveBoutSideMemberId: which squad MEMBER ID identifies one side of a
// sub-bout row (bc-pnum: extend the squad member label -- squadMemberLabel,
// squad_member_label.jsx -- to the team scoring surfaces). MIRRORS
// resolveBoutSideName's kachinuki-vs-fixed priority exactly: the id must
// come from the SAME source tier the name was resolved from (a kachinuki
// numbered bout's pairing is server-bout-log first, the SubMatchResult's own
// sideAMemberId/sideBMemberId -- backfilled from team-members.yaml by the
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

// resolveBoutSideDisplayName: the ONE rule for what name a bout side shows
// on screen (bc-dnst, operator ruling 2026-09-15). A rename reaches every
// bout that member has already fought -- the STORED SubMatchResult text
// (sub.sideA/sub.sideB) stays frozen forever (it is not rewritten, and
// nothing on the wire changes when a member is renamed), but every render
// site resolves the member's CURRENT name by id and shows that instead.
// Resolution is id-only, exactly like resolveSquadMember: a name can never
// re-attach a bout to a different member than the id it was recorded with.
//
// This is DISPLAY-ONLY. Nothing that WRITES a bout (buildPatch's
// playerNamesForBout, the kachinuki bout log, a lineup PUT) may pass a name
// through this function; doing so would let a stale render silently rewrite
// the stored record. Every call site below is a render, never a writer.
//
// storedName is returned verbatim (falling back to "" for a nullish/absent
// value) whenever memberId resolves to nothing, or resolves to a squad
// member whose own name is still blank (an unnamed seeded slot has nothing
// newer to show).
export function resolveBoutSideDisplayName({ squad, memberId, storedName }) {
  const list = Array.isArray(squad) ? squad : [];
  const member = memberId ? list.find(mem => mem && mem.id === memberId) : null;
  const currentName = member ? String(member.name || "").trim() : "";
  if (currentName) return currentName;
  return storedName || "";
}

// boutSideView: the resolve-then-display sequence every read-only bout-row
// surface repeats for ONE side (bc-rvfx) -- the TV/viewer scoreboard and the
// OBS overlay both derive a side's name this way. resolveBoutSideName picks
// the base identity (kachinuki-vs-fixed priority, the team-name trap
// filtered out), `fallback` stands in when that resolves to nothing (a bare
// bout number or FIK position label, exactly as every call site already
// applied via `|| boutNum` / `|| ovlFallback`), then resolveBoutSideDisplayName
// swaps in the squad member's CURRENT name by id -- a rename reaches a bout
// already fought without the stored record ever being touched.
//
// Returns both `name` (the frozen base identity, post-fallback) and
// `displayName`: a caller that also feeds the base name to
// resolveBoutSideSquadLabel (as match_scoreboard.jsx does) or to the
// display name (as streaming_overlay.jsx does -- the two surfaces disagree
// on which, and that disagreement predates this helper) keeps reading
// whichever field it already used.
//
// admin_scoring_team.jsx's bout rows do NOT go through this: their base
// identity comes from playerNamesForBout, which layers a manual-override
// short-circuit (a picked or typed fighter beats the lineup/server
// priority below) ahead of resolveBoutSideName, so folding it in here would
// either reach into that editor's local per-row override state from this
// shared leaf, or add a bypass parameter unused by every other caller.
// Those two sites call resolveBoutSideDisplayName directly instead.
export function boutSideView({ isKachinuki, isDaihyosen, existingName, lineupName, teamNameA, teamNameB, fallback, squad, memberId }) {
  const name = resolveBoutSideName({ isKachinuki, isDaihyosen, existingName, lineupName, teamNameA, teamNameB }) || fallback;
  const displayName = resolveBoutSideDisplayName({ squad, memberId, storedName: name });
  return { name, displayName };
}

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
//   { teamId, positions: { [posKey]: playerName } }
// where posKey is a named FIK position ("senpo", "jiho", ...) for 5-person
// teams, or a numeric string "1".."N" for other sizes.

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

// resolveLineupTeamId maps a match-side to the participant id that lineups
// are stored under. Accepts EITHER the resolved side object ({id, name}, as
// produced by api_serializers.jsx) or a bare name/id string for legacy
// callers that only ever had one value. TeamLineups are keyed server-side by
// the participant's real id; passing a bare name straight through can make
// the lineup GET 404 and the per-match (and round) lineup never reaches the
// scoring grid, so this maps a name-keyed side to its real id.
//
// bc-pnum: an id decides whenever the side carries one -- matched against
// the roster by id ONLY, never falling back to a name compare that could
// resolve a DIFFERENT participant/team sharing the same display name (a
// documented restore-hole lets two teams collide on a name; two players
// always can). Name is the fallback only for a bare string argument (a
// caller that only ever had a name, not the resolved side object) or a side
// object with no id at all (e.g. a bracket row, by design).
export function resolveLineupTeamId(side, players) {
  if (!side) return "";
  const list = Array.isArray(players) ? players : [];
  const isObj = side && typeof side === "object";
  const id = isObj ? (side.id || "") : "";
  const name = isObj ? (side.name || "") : side;
  if (id) {
    const p = list.find(pl => pl && (pl.id === id || pl.ID === id));
    return (p && (p.id || p.ID)) || id;
  }
  if (!name) return "";
  const p = list.find(pl => pl && (pl.name === name || pl.Name === name));
  return (p && (p.id || p.ID)) || name;
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

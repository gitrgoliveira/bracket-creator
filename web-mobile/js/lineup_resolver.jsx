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

// resolveLineupTeamId maps a match-side key to the participant id that
// lineups are stored under. Depending on the API path, a match side's `id`
// may be EITHER the participant's real id (a UUID) OR the team NAME (some
// serializers set id = name). TeamLineups are keyed server-side by whatever
// team key was used when the lineup was saved; in practice, that's the participant's
// real id. Passing a bare name straight through can make the lineup GET
// 404 and the per-match (and round) lineup never reaches the scoring grid.
// We look the side up in the competition's participant list by id OR name and
// return its real id, falling back to the original key when unmatched.
export function resolveLineupTeamId(sideKey, players) {
  if (!sideKey) return "";
  const list = Array.isArray(players) ? players : [];
  const p = list.find(pl => pl
    && (pl.id === sideKey || pl.ID === sideKey || pl.name === sideKey || pl.Name === sideKey));
  return (p && (p.id || p.ID)) || sideKey;
}

// FIK named positions for 5-person teams (index 0=senpo … 4=taisho).
// Mirrors POS_LABELS_BY_INDEX_5 in admin_scoring_modal.jsx.
const POS_LABELS_5 = ["senpo", "jiho", "chuken", "fukusho", "taisho"];

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

// pickFromLineup: resolves the player name at a given bout index from a
// lineup object. 5-person teams use named position keys; other sizes use
// the numeric string "1".."N". Returns "" when the lineup has no entry for
// that position.
export function pickFromLineup(lineup, index, teamSize) {
  if (!lineup || !lineup.positions) return "";
  if (teamSize === 5 && index >= 0 && index < 5) {
    const named = lineup.positions[POS_LABELS_5[index]];
    if (named) return named;
  }
  const numeric = lineup.positions[String(index + 1)];
  if (numeric) return numeric;
  return "";
}

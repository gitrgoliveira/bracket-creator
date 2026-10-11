// kachinuki_advisory.jsx: the advisory line a kachinuki team score sheet shows
// above the live bout (bc-kfnl, operator ruling 2026-09-24): each side's
// fighter on the court, how many fighters its lineup has left, and who comes
// next, e.g.
//
//   Shiro: T1.1 Taki on, 2 left (T1.2 Ueda, T1.3 Sato) · Aka: T2.5 Kudo on, last fighter
//
// The first name in the brackets is next. Every fighter carries their
// team-member number exactly as the bout rows show one (squadMemberLabel over
// resolveSquadMember, then the member's current name by id through
// resolveBoutSideDisplayName); a member with no name reads by number alone.
//
// The split is the SERVER's (GET .../kachinuki-roster, engine.KachinukiRoster):
// each side's fighter on, and the queue behind them, built and filtered the way
// the advance appends the next pairing, so the line never names a different
// next fighter from the one the app then appends. Which queue entry is the
// fighter on is decided there, by the engine's identity rule, never here: this
// file only shapes that answer for display.
//
// ADVISORY ONLY: kachinuki is operator-led and the roster is advisory (team
// sizes are unregulated, a vacancy is legitimate). Nothing here ends the
// encounter, arms End match or holds a button back, and the line never asks
// the operator to enter or complete a lineup: a side with no lineup shows its
// fighter on alone, and with no lineup on either side there is no line.
import { squadMemberLabel } from './squad_member_label.jsx';
import { resolveSquadMember, resolveBoutSideDisplayName } from './lineup_resolver.jsx';
// The client's one draw test (the twin of state.IsDraw). The team editor
// already loads api_serializers.jsx through admin_scoring_autosave.jsx, so this
// adds no module to its graph.
import { isHikiwake } from './api_serializers.jsx';

// The separator between the two sides of the line.
export const ADVISORY_SEPARATOR = " · ";

// kachinukiFighterLabel names one fighter the way a bout row does: the
// team-member number (e.g. "T1.2"), then the member's current name. "" when
// neither is known.
export function kachinukiFighterLabel({ squad, teamNumber, memberId, name }) {
  const member = resolveSquadMember(squad, memberId, name);
  const number = member ? squadMemberLabel(teamNumber, member.index) : "";
  const shown = resolveBoutSideDisplayName({ squad, memberId, storedName: name });
  return [number, shown].filter(Boolean).join(" ");
}

// kachinukiFighterKnown is the ONE client test of "the roster names a fighter
// here": a member id or a name (a member picked by number and not named yet
// carries only the id), the twin of engine.kachinukiFighterKnown.
export function kachinukiFighterKnown(f) {
  return !!f && !!(f.memberId || f.name);
}

// kachinukiAdvisorySide shapes one side of the server's answer: the fighter on
// (side.on, which the SERVER picked out of the queue by the engine's identity
// rule) and, with a lineup, the fighters left behind them (side.remaining,
// which never holds the fighter on; the first is next). Returns null when
// there is nothing to say for the side.
export function kachinukiAdvisorySide({ side, squad, teamNumber }) {
  if (!side) return null;
  const on = kachinukiFighterKnown(side.on) ? side.on : null;
  const onLabel = on ? kachinukiFighterLabel({ squad, teamNumber, memberId: on.memberId, name: on.name }) : "";
  if (!side.lineupFound) {
    return onLabel ? { on: onLabel, left: null, text: `${onLabel} on` } : null;
  }
  const remaining = Array.isArray(side.remaining) ? side.remaining : [];
  const left = remaining.map(f => kachinukiFighterLabel({ squad, teamNumber, memberId: f.memberId, name: f.name }) || "-");
  const head = onLabel ? `${onLabel} on` : "";
  let tail;
  if (left.length === 0) tail = onLabel ? "last fighter" : "no fighters left";
  else tail = `${left.length} left (${left.join(", ")})`;
  return { on: onLabel, left, text: head ? `${head}, ${tail}` : tail };
}

// kachinukiAdvisoryModel builds the whole line from the server's roster.
// roster.sideA is Aka (the match's side A, squadA), roster.sideB is Shiro; the
// line reads Shiro first, then Aka (left to right, as the sheet). Returns null
// when the roster is missing or neither side has a lineup: then there is no
// line at all.
export function kachinukiAdvisoryModel({ roster, squadA, squadB, teamNumberA, teamNumberB }) {
  if (!roster || !roster.sideA || !roster.sideB) return null;
  if (!roster.sideA.lineupFound && !roster.sideB.lineupFound) return null;
  const shiro = kachinukiAdvisorySide({ side: roster.sideB, squad: squadB, teamNumber: teamNumberB });
  const aka = kachinukiAdvisorySide({ side: roster.sideA, squad: squadA, teamNumber: teamNumberA });
  const parts = [shiro && `Shiro: ${shiro.text}`, aka && `Aka: ${aka.text}`].filter(Boolean);
  if (parts.length === 0) return null;
  return { shiro, aka, text: parts.join(ADVISORY_SEPARATOR) };
}

// bc-kbrc: the one line the team sheet shows in place of the live bout once
// Record bout has recorded the last bout as a win and appended no pairing. It
// names the side the SERVER's Record bout decision says has nobody left
// (engine.KachinukiRoster: `nextPairing` false and that side `exhausted`, which
// the server sets only for the loser of a recorded win whose lineup has nobody
// behind them, at most one side). null otherwise (no roster read, the read
// failed, a pairing would follow, or no lineup says so): the sheet then keeps
// the bout live, as it always did, and shows no line (coordinator decision
// 2026-10-11).
export function kachinukiNoPairingText(roster) {
  if (!roster || roster.nextPairing !== false) return null;
  const shiro = !!(roster.sideB && roster.sideB.exhausted);
  const aka = !!(roster.sideA && roster.sideA.exhausted);
  if (!shiro && !aka) return null;
  return `${shiro ? "Shiro" : "Aka"} has no fighters left. End match finishes the encounter.`;
}

// lastNumberedBout is the bout log's last numbered bout, the row the server
// reads as the live bout (the JS twin of engine.lastNumberedBout): numbered
// means position > 0 (the representative bout, -1, is not one), and last means
// the highest position, whatever the array's order (the first of two rows
// sharing it), exactly as the Go function decides. null when there is none.
// The ONE client owner of "the stored last numbered bout": the roster key, the
// recorded-last key and the closing of a recorded last bout all ask it.
export function lastNumberedBout(subResults) {
  let last = null;
  for (const s of Array.isArray(subResults) ? subResults : []) {
    if (s && s.position > 0 && (!last || s.position > last.position)) last = s;
  }
  return last;
}

// kachinukiRecordedBoutCloses reports whether the last numbered bout is one
// Record bout recorded as a WIN, the only recorded bout that may close the
// live row (bc-kbrc). A recorded DRAW still last keeps both fighters on, in
// every phase (engine.KachinukiRoster): nothing followed it because both sides
// had nobody left, and whether the pair fights on in encho is the operator's
// call, so the bout stays live with Encho offered. End match decides the rest.
export function kachinukiRecordedBoutCloses(subResults) {
  const last = lastNumberedBout(subResults);
  return !!last && last.recorded === true && !isHikiwake(last.decision);
}

// kachinukiRosterKey is the VALUE the roster read is keyed on (never the match
// object, which every broadcast re-creates): the match id, every numbered
// bout's fighters and recorded mark, and the RESULT (winner, winner member id,
// decision) of every bout the server reads one from (engine.KachinukiRoster):
// each bout before the last, and the last once Record bout has recorded it.
// The last bout while it is being fought keys on its fighters alone: the
// server keeps both of them on whatever its score (the running autosave
// writes a one-point lead's winner and a 1-1's hikiwake there, and neither is
// a result), so a live tap reads nothing, and Record bout, which marks the
// bout recorded with or without a next pairing, reads once. The roster's
// verdicts (`nextPairing`, `exhausted`) are asked of a RECORDED last bout
// only, whose result this key carries, so leaving the live score out never
// lets a stale verdict decide anything. Every reader keys
// the same way on the same stored log, so a fresh sheet and a second device
// show the same line.
export function kachinukiRosterKey(matchId, subResults) {
  const rows = (Array.isArray(subResults) ? subResults : [])
    // The numbered rows, by lastNumberedBout's own rule (position > 0); filter
    // already made a new array, so the sort touches no caller's.
    .filter(s => s && s.position > 0)
    .sort((x, y) => (x.position || 0) - (y.position || 0));
  const live = lastNumberedBout(rows);
  const parts = rows.map((s) => {
    const fighters = [s.position, s.sideAMemberId || "", s.sideA || "", s.sideBMemberId || "", s.sideB || "", s.recorded ? "R" : ""];
    if (s === live && !s.recorded) return fighters.join("~");
    const winner = s.winner && typeof s.winner === "object" ? JSON.stringify(s.winner) : (s.winner || "");
    return [...fighters, winner, s.winnerMemberId || "", s.decision || ""].join("~");
  });
  return `${matchId || ""}|${parts.join("/")}`;
}

// storedBoutRecorded reports whether the match holds the numbered bout at
// position as recorded (SubMatchResult.recorded, which only the server
// writes: Record bout sets it). The team sheet asks it to disable Record bout
// on a recorded bout; it never sends the mark back.
export function storedBoutRecorded(match, position) {
  const subs = match && Array.isArray(match.subResults) ? match.subResults : [];
  return subs.some(s => s && s.position === position && s.recorded === true);
}

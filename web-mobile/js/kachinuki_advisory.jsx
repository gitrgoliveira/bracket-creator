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
// The queue is the SERVER's (GET .../kachinuki-roster, engine.KachinukiRoster),
// read through the same call that appends the next pairing, so the line never
// names a different next fighter from the one the app then appends. This file
// only shapes that answer for display.
//
// ADVISORY ONLY: kachinuki is operator-led and the roster is advisory (team
// sizes are unregulated, a vacancy is legitimate). Nothing here ends the
// encounter, arms End match or holds a button back, and the line never asks
// the operator to enter or complete a lineup: a side with no lineup shows its
// fighter on alone, and with no lineup on either side there is no line.
import { squadMemberLabel } from './squad_member_label.jsx';
import { resolveSquadMember, resolveBoutSideDisplayName } from './lineup_resolver.jsx';

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

// sameFighter: by member id when both carry one, else by a non-empty name.
function sameFighter(f, on) {
  if (!f || !on) return false;
  if (f.memberId && on.memberId) return f.memberId === on.memberId;
  if (f.memberId || on.memberId) return false;
  return !!f.name && f.name === on.name;
}

// kachinukiAdvisorySide shapes one side: the fighter on, and with a lineup the
// fighters left behind them (the server's queue minus the fighter on). Returns
// null when there is nothing to say for the side.
export function kachinukiAdvisorySide({ side, on, squad, teamNumber }) {
  const onKnown = !!(on && (on.memberId || on.name));
  const onLabel = onKnown ? kachinukiFighterLabel({ squad, teamNumber, memberId: on.memberId, name: on.name }) : "";
  if (!side || !side.lineupFound) {
    return onLabel ? { on: onLabel, left: null, text: `${onLabel} on` } : null;
  }
  const remaining = Array.isArray(side.remaining) ? side.remaining : [];
  const left = remaining
    .filter(f => !(onKnown && sameFighter(f, on)))
    .map(f => kachinukiFighterLabel({ squad, teamNumber, memberId: f.memberId, name: f.name }) || "-");
  const head = onLabel ? `${onLabel} on` : "";
  let tail;
  if (left.length === 0) tail = onLabel ? "last fighter" : "0 left";
  else tail = `${left.length} left (${left.join(", ")})`;
  return { on: onLabel, left, text: head ? `${head}, ${tail}` : tail };
}

// kachinukiAdvisoryModel builds the whole line from the server's roster and
// the live bout's two fighters. roster.sideA is Aka (the match's side A, squadA),
// roster.sideB is Shiro; the line reads Shiro first, then Aka (left to right,
// as the sheet). Returns null when the roster is missing or neither side has a
// lineup: then there is no line at all.
export function kachinukiAdvisoryModel({ roster, onA, onB, squadA, squadB, teamNumberA, teamNumberB }) {
  if (!roster || !roster.sideA || !roster.sideB) return null;
  if (!roster.sideA.lineupFound && !roster.sideB.lineupFound) return null;
  const shiro = kachinukiAdvisorySide({ side: roster.sideB, on: onB, squad: squadB, teamNumber: teamNumberB });
  const aka = kachinukiAdvisorySide({ side: roster.sideA, on: onA, squad: squadA, teamNumber: teamNumberA });
  const parts = [shiro && `Shiro: ${shiro.text}`, aka && `Aka: ${aka.text}`].filter(Boolean);
  if (parts.length === 0) return null;
  return { shiro, aka, text: parts.join(ADVISORY_SEPARATOR) };
}

// kachinukiRosterKey is the VALUE the roster read is keyed on (never the match
// object, which every broadcast re-creates): the match id and every numbered
// bout's fighters and outcome, leaving out the outcome of the last numbered
// bout, the live one, so a point scored on it does not re-read. Who has retired
// changes only when a bout's outcome is recorded, which appends the next bout
// and so changes the key.
export function kachinukiRosterKey(matchId, subResults) {
  const rows = (Array.isArray(subResults) ? subResults : [])
    .filter(s => s && s.position !== -1)
    .slice()
    .sort((x, y) => (x.position || 0) - (y.position || 0));
  const lastIdx = rows.length - 1;
  const parts = rows.map((s, i) => {
    const who = [s.position, s.sideAMemberId || "", s.sideA || "", s.sideBMemberId || "", s.sideB || ""];
    const winner = s.winner && typeof s.winner === "object" ? JSON.stringify(s.winner) : (s.winner || "");
    const outcome = i === lastIdx ? [] : [winner, s.winnerMemberId || "", s.decision || ""];
    return [...who, ...outcome].join("~");
  });
  return `${matchId || ""}|${parts.join("/")}`;
}

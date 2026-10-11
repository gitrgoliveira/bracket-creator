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

// kachinukiAdvisorySide shapes one side of the server's answer: the fighter on
// (side.on, which the SERVER picked out of the queue by the engine's identity
// rule) and, with a lineup, the fighters left behind them (side.remaining,
// which never holds the fighter on; the first is next). Returns null when
// there is nothing to say for the side.
export function kachinukiAdvisorySide({ side, squad, teamNumber }) {
  if (!side) return null;
  const on = side.on && (side.on.memberId || side.on.name) ? side.on : null;
  const onLabel = on ? kachinukiFighterLabel({ squad, teamNumber, memberId: on.memberId, name: on.name }) : "";
  if (!side.lineupFound) {
    return onLabel ? { on: onLabel, left: null, text: `${onLabel} on` } : null;
  }
  const remaining = Array.isArray(side.remaining) ? side.remaining : [];
  const left = remaining.map(f => kachinukiFighterLabel({ squad, teamNumber, memberId: f.memberId, name: f.name }) || "-");
  const head = onLabel ? `${onLabel} on` : "";
  let tail;
  if (left.length === 0) tail = onLabel ? "last fighter" : "0 left";
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

// kachinukiRosterKey is the VALUE the roster read is keyed on (never the match
// object, which every broadcast re-creates): the match id, every numbered
// bout's fighters, and the outcome of every bout but the live one (the last
// numbered bout), so a point scored on the live bout does not re-read.
//
// recordedThrough is the highest bout position this sheet saw RECORDED (Record
// bout). Recording usually appends the next bout, which changes the key, but
// when a side has nobody left it appends nothing; so once the live bout is
// recorded the key carries that fact and the live bout's outcome, and the read
// is made again (with recordedThrough, see API.fetchKachinukiRoster) to take
// its loser off. Returns { key, recordedThrough }: the recorded position the
// key stands for, 0 while the live bout is still being fought.
export function kachinukiRosterKey(matchId, subResults, recordedThrough = 0) {
  const rows = (Array.isArray(subResults) ? subResults : [])
    .filter(s => s && s.position !== -1)
    .slice()
    .sort((x, y) => (x.position || 0) - (y.position || 0));
  const lastIdx = rows.length - 1;
  const livePos = lastIdx >= 0 ? (rows[lastIdx].position || 0) : 0;
  const liveRecorded = lastIdx >= 0 && recordedThrough > 0 && recordedThrough >= livePos;
  const parts = rows.map((s, i) => {
    const who = [s.position, s.sideAMemberId || "", s.sideA || "", s.sideBMemberId || "", s.sideB || ""];
    const winner = s.winner && typeof s.winner === "object" ? JSON.stringify(s.winner) : (s.winner || "");
    const outcome = i === lastIdx && !liveRecorded ? [] : [winner, s.winnerMemberId || "", s.decision || ""];
    return [...who, ...outcome].join("~");
  });
  const recorded = liveRecorded ? livePos : 0;
  return { key: `${matchId || ""}|${parts.join("/")}|${recorded}`, recordedThrough: recorded };
}

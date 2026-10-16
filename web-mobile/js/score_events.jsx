// score_events.jsx: the score-event leaf (bc-evts). A LEAF: it imports nothing
// and reads no window global, so the outbox, the editors and the tests can all
// import it without pulling the app in.
//
// It is the JS twin of the Go fold and replay:
//   - the kinds, sides and outcomes: internal/domain/score_event.go;
//   - the fold rules (AddIppon, RemoveMark, AddFoul/ApplyFoulIncrement,
//     RemoveFoul, AdjustEncho, AdjustFlags): the same file;
//   - foldEvent's shape gates, markID, assignMarks and the replay
//     (replayScoring, the closed-match rule, BasisContains):
//     internal/engine/score_replay.go.
// The replay is pinned against Go by the shared golden table
// internal/engine/testdata/score_replay.json (score_replay_golden.test.jsx).
//
// Scope (stage 3): position 0 of an individual or engi match. A team match
// (numbered bouts, the representative bout, fusensho per bout) folds from
// stage 5, and until then foldEvent THROWS on one rather than misfold it. The
// replay folds a baseline only as the FIRST line to set its groups (a log's
// head): a screen copies taps on top of the server's last confirmed match and
// never folds a baseline, so the rival and echo rules for a later baseline
// stay Go only, and replayHead throws on one.

// The kinds (domain.ScoreEventKind).
export const IPPON_ADD = 'ippon.add';
export const IPPON_REMOVE = 'ippon.remove';
export const FOUL_ADD = 'foul.add';
export const FOUL_REMOVE = 'foul.remove';
export const ENCHO_ADD = 'encho.add';
export const ENCHO_REMOVE = 'encho.remove';
export const FLAG_ADD = 'flag.add';
export const FLAG_REMOVE = 'flag.remove';
export const BOUT_DECISION = 'bout.decision';
export const BOUT_FIGHTER = 'bout.fighter';
export const REP_PICK = 'rep.pick';
export const POOL_REP = 'pool.rep';
export const DAIHYOSEN_VERDICT = 'daihyosen.verdict';

// The sides: a is SideA (Aka), b is SideB (Shiro).
export const SIDE_A = 'a';
export const SIDE_B = 'b';

// The representative bout's position (domain.DaihyosenBoutPos).
export const DAIHYOSEN_POS = -1;

// The outcomes the replay gives a LOGGED event (they may change on a later
// replay) ...
export const APPLIED = 'applied';
export const RECORDED = 'recorded';
export const DUPLICATE = 'duplicate';
export const CAPPED = 'capped';
export const BEFORE_BASELINE = 'beforeBaseline';
export const MATCH_CLOSED = 'matchClosed';
export const NO_TARGET = 'noTarget';
export const NOT_ALLOWED = 'notAllowed';
// ... and the ones the score-events door gives an event it does NOT log.
// clock_skew is an OUTCOME (with serverNowMs/aheadMs beside it): the device
// re-stamps the event and sends it again with the same id.
export const NOT_STARTED = 'notStarted';
export const REFUSED = 'refused';
export const CLOCK_SKEW = 'clock_skew';

// The outcomes that need no word to the operator: the tap is in the match.
export const QUIET_OUTCOMES = new Set([APPLIED, RECORDED, DUPLICATE]);

// domain.MaxEngiFlags and domain.MaxIpponsPerSide.
export const MAX_FLAGS = 5;
const MAX_IPPONS_PER_SIDE = 2;

// The non-strike entries an ippon slice can hold, spelled as result_slot.jsx
// and internal/domain spell them (not imported: this file is a leaf).
const MARU = '○'; // a decision's awarded point (domain.DefaultWinIppon)
const PLACEHOLDER = '•'; // an unfilled slot (domain.IpponPlaceholder)
const HANTEI = 'Ht'; // the judges' decision (domain.HanteiMark)

// makeEvent is one tap without the fields the outbox adds (id, session, seq,
// stamp). Only the fields given are kept, so the wire stays small.
export function makeEvent({ kind, pos = 0, side, letter, target, value, memberId, name }) {
  const e = { kind, pos };
  if (side) e.side = side;
  if (letter) e.letter = letter;
  if (target) e.target = target;
  if (value) e.value = value;
  if (memberId) e.memberId = memberId;
  if (name) e.name = name;
  return e;
}

// ---------------------------------------------------------------------------
// Mark ids
// ---------------------------------------------------------------------------

// markID is the id a baseline-minted mark takes (engine.markID).
export function markID(pos, side, letter, n) {
  return `b:${pos}:${side}:${letter}:${n}`;
}

// assignMarks names the marks a baseline brings (engine.assignMarks): the
// marks held before keep their ids by a longest common subsequence of values,
// the earliest copy first; a new mark is minted markID with the first ordinal
// of its letter on that side that is not already taken; a placeholder has none.
export function assignMarks(prev, prevMarks, next, pos, side) {
  prev = prev || [];
  prevMarks = prevMarks || [];
  next = next || [];
  const isMark = (v) => v !== '' && v != null && v !== PLACEHOLDER;
  const same = (i, j) => isMark(next[j]) && prev[i] === next[j];
  const lcs = Array.from({ length: prev.length + 1 }, () => Array.from({ length: next.length + 1 }, () => 0));
  for (let i = prev.length - 1; i >= 0; i--) {
    for (let j = next.length - 1; j >= 0; j--) {
      lcs[i][j] = same(i, j) ? lcs[i + 1][j + 1] + 1 : Math.max(lcs[i + 1][j], lcs[i][j + 1]);
    }
  }
  const out = Array.from({ length: next.length }, () => '');
  for (let i = 0, j = 0; j < next.length; ) {
    if (i < prev.length && same(i, j) && lcs[i][j] === lcs[i + 1][j + 1] + 1) {
      if (i < prevMarks.length) out[j] = prevMarks[i] || '';
      i++;
      j++;
    } else if (i < prev.length && lcs[i + 1][j] >= lcs[i][j + 1]) {
      i++;
    } else {
      j++;
    }
  }
  const taken = new Set(out.filter((id) => id !== ''));
  const ordinal = {};
  next.forEach((v, j) => {
    if (!isMark(v)) return;
    let n = ordinal[v] || 0;
    ordinal[v] = n + 1;
    if (out[j] !== '') return;
    let id = markID(pos, side, v, n);
    while (taken.has(id)) id = markID(pos, side, v, ++n);
    taken.add(id);
    out[j] = id;
  });
  return out;
}

// alignTo is marks cut or padded with '' to the ippons' length.
function alignTo(ippons, marks) {
  return ippons.map((_, i) => (marks && marks[i]) || '');
}

// markIdsOf is the stored form (engine.markIDsOf): null when neither side
// holds a mark, and a side with none is left out.
function markIdsOf(a, b, ma, mb) {
  if (a.length === 0 && b.length === 0) return null;
  const ids = {};
  if (a.length > 0) ids.a = alignTo(a, ma);
  if (b.length > 0) ids.b = alignTo(b, mb);
  return ids;
}

// ---------------------------------------------------------------------------
// The fold (domain.BoutScore at position 0)
// ---------------------------------------------------------------------------

const isScoringIppon = (v) => v != null && v !== '' && v !== PLACEHOLDER && v !== HANTEI;
const countScoring = (ippons) => ippons.filter(isScoringIppon).length;
// The points a side actually struck: no maru, placeholder or hantei mark.
const countStruck = (ippons) => ippons.filter((v) => isScoringIppon(v) && v !== MARU).length;

// appendMark puts entry in the first free slot ('' or the placeholder), else
// at the end, with id beside it (domain.appendMark over domain.AppendIppon).
function appendMark(ippons, marks, entry, id) {
  const outI = [...ippons];
  const outM = alignTo(ippons, marks);
  const free = outI.findIndex((v) => v === '' || v === PLACEHOLDER);
  if (free >= 0) {
    outI[free] = entry;
    outM[free] = id;
  } else {
    outI.push(entry);
    outM.push(id);
  }
  return [outI, outM];
}

// applyFoulIncrement is the one hansoku rule (domain.ApplyFoulIncrement): the
// second foul lands an H for the opponent and the count goes back to 0, no H
// once either side holds the points that end the bout.
function applyFoulIncrement(fouls, opponentPoints, ownPoints) {
  const next = fouls + 1;
  if (next < 2) return [next, false];
  if (opponentPoints >= MAX_IPPONS_PER_SIDE || ownPoints >= MAX_IPPONS_PER_SIDE) return [0, false];
  return [0, true];
}

const other = (side) => (side === SIDE_A ? SIDE_B : SIDE_A);

// Each method of score works on a bout score {ippons: {a, b}, marks: {a, b},
// fouls: {a, b}} and returns the outcome. A bout's fusensho rule
// (domain.ApplyBoutScoreEdit) never touches position 0 of an individual match,
// which carries no fusensho; stage 5 adds it with the team bouts.
const score = {
  addIppon(s, side, letter, id) {
    if (countStruck(s.ippons[side]) >= MAX_IPPONS_PER_SIDE || countStruck(s.ippons[other(side)]) >= MAX_IPPONS_PER_SIDE) {
      return CAPPED;
    }
    [s.ippons[side], s.marks[side]] = appendMark(s.ippons[side], s.marks[side], letter, id);
    return APPLIED;
  },
  removeMark(s, target) {
    for (const side of [SIDE_A, SIDE_B]) {
      const marks = alignTo(s.ippons[side], s.marks[side]);
      const i = marks.indexOf(target);
      if (i < 0 || !target) continue;
      if (s.ippons[side][i] === MARU) return NOT_ALLOWED;
      s.ippons[side] = s.ippons[side].filter((_, k) => k !== i);
      s.marks[side] = marks.filter((_, k) => k !== i);
      return APPLIED;
    }
    return NO_TARGET;
  },
  addFoul(s, side, id) {
    const o = other(side);
    const [f, landsH] = applyFoulIncrement(s.fouls[side], countScoring(s.ippons[o]), countScoring(s.ippons[side]));
    s.fouls[side] = f;
    if (landsH) [s.ippons[o], s.marks[o]] = appendMark(s.ippons[o], s.marks[o], 'H', id);
    return APPLIED;
  },
  removeFoul(s, side) {
    if (s.fouls[side] <= 0) return NO_TARGET;
    s.fouls[side]--;
    return APPLIED;
  },
};

const sideOf = (e) => (e.side === SIDE_B ? SIDE_B : SIDE_A);

// foldScoring is engine.foldScoring for the match-level strike and foul kinds.
function foldScoring(s, e) {
  switch (e.kind) {
    case IPPON_ADD: return score.addIppon(s, sideOf(e), e.letter, e.id);
    case IPPON_REMOVE: return score.removeMark(s, e.target);
    case FOUL_ADD: return score.addFoul(s, sideOf(e), e.id);
    case FOUL_REMOVE: return score.removeFoul(s, sideOf(e));
  }
  return NOT_ALLOWED;
}

const copy = (m) => JSON.parse(JSON.stringify(m));
const enchoCount = (m) => (m.encho && m.encho.periodCount > 0 ? m.encho.periodCount : 0);

// foldMatchLevel is engine.foldMatchLevel for an individual match; m is a copy
// it may change.
function foldMatchLevel(m, e) {
  if (e.kind === ENCHO_ADD || e.kind === ENCHO_REMOVE) {
    const next = enchoCount(m) + (e.kind === ENCHO_REMOVE ? -1 : 1);
    if (next < 0) return NO_TARGET;
    m.encho = next > 0 ? { periodCount: next } : null;
    return APPLIED;
  }
  if (e.kind === BOUT_DECISION || e.kind === BOUT_FIGHTER || e.kind === DAIHYOSEN_VERDICT) return NOT_ALLOWED;
  const s = {
    ippons: { a: [...(m.ipponsA || [])], b: [...(m.ipponsB || [])] },
    marks: { a: [...((m.markIds && m.markIds.a) || [])], b: [...((m.markIds && m.markIds.b) || [])] },
    fouls: { a: m.hansokuA || 0, b: m.hansokuB || 0 },
  };
  const out = foldScoring(s, e);
  if (out !== APPLIED) return out;
  m.ipponsA = s.ippons.a;
  m.ipponsB = s.ippons.b;
  m.hansokuA = s.fouls.a;
  m.hansokuB = s.fouls.b;
  const ids = markIdsOf(s.ippons.a, s.ippons.b, s.marks.a, s.marks.b);
  if (ids) m.markIds = ids;
  else delete m.markIds;
  return APPLIED;
}

// foldEvent folds one event onto a copy of match (engine.foldEvent) and returns
// {match, outcome}; the match given is never changed, and on any outcome but
// applied the copy equals it. opts: {engi} for an engi match; {team} or
// {kachinuki} throws until stage 5 ports the team fold.
export function foldEvent(match, e, opts = {}) {
  if (opts.team || opts.kachinuki) {
    throw new Error('score_events: a team match is folded from stage 5 (team bouts are not ported yet)');
  }
  const m = copy(match);
  let outcome;
  switch (e.kind) {
    case FLAG_ADD:
    case FLAG_REMOVE: {
      if (!opts.engi) { outcome = NOT_ALLOWED; break; }
      const key = e.side === SIDE_B ? 'flagsB' : 'flagsA';
      const next = (m[key] || 0) + (e.kind === FLAG_REMOVE ? -1 : 1);
      if (next > MAX_FLAGS) outcome = CAPPED;
      else if (next < 0) outcome = NO_TARGET;
      else { m[key] = next; outcome = APPLIED; }
      break;
    }
    case POOL_REP:
      // A rep player is a TEAM's fighter for a pool representative bout.
      outcome = NOT_ALLOWED;
      break;
    case REP_PICK:
      // Only a team match holds the representative bout this names.
      outcome = NO_TARGET;
      break;
    default:
      if (opts.engi || (e.pos || 0) !== 0) outcome = NOT_ALLOWED;
      else outcome = foldMatchLevel(m, e);
  }
  return { match: outcome === APPLIED ? m : copy(match), outcome };
}

// ---------------------------------------------------------------------------
// What a board shows
// ---------------------------------------------------------------------------

const markIdSet = (m) => new Set([...((m.markIds && m.markIds.a) || []), ...((m.markIds && m.markIds.b) || [])]);

// applyPending is what this device's board shows: a copy of the server's last
// confirmed match (which carries markIds and scoreVersion) with the device's
// still-pending taps folded on top, in order, by the same fold. outcomes is
// {id: outcome} as the fold reads each tap here: a tap on a finished match is
// matchClosed and one on a scheduled match notStarted (both change nothing),
// and a tap whose mark the server match already carries (its id is a mark id)
// is a duplicate, so an echo that lands before the outbox's answer does not
// show the point twice. scoreVersion and modifiedAt stay the server's: the
// board is not a newer copy of the match than the one the server confirmed.
// Neither argument is changed.
export function applyPending(serverMatch, pending, opts = {}) {
  let match = copy(serverMatch);
  const outcomes = {};
  for (const e of pending || []) {
    if (match.status === 'completed') { outcomes[e.id] = MATCH_CLOSED; continue; }
    if (match.status === 'scheduled') { outcomes[e.id] = NOT_STARTED; continue; }
    if ((e.kind === IPPON_ADD || e.kind === FOUL_ADD) && markIdSet(match).has(e.id)) {
      outcomes[e.id] = DUPLICATE;
      continue;
    }
    const r = foldEvent(match, e, opts);
    match = r.match;
    outcomes[e.id] = r.outcome;
  }
  return { match, markIds: match.markIds || null, outcomes };
}

// ---------------------------------------------------------------------------
// The replay (engine.replayScoring), for a log whose baselines are its head
// ---------------------------------------------------------------------------

const GROUP_POINTS = 'points';
const GROUP_RESULT = 'result';
const GROUP_ENCHO = 'encho';
const GROUP_FLAGS = 'flags';
const GROUP_REP = 'rep';
const GROUP_REP_PICK_A = 'repPickA';
const GROUP_REP_PICK_B = 'repPickB';
const GROUP_RECORDED = 'recorded';
const isBoutGroup = (g) => g.startsWith('bout:');
const isScoringGroup = (g) => g === GROUP_POINTS || g === GROUP_FLAGS || isBoutGroup(g);

// The decisions that close a match without a fought scoreline (kiken in any
// form, fusenpai, fusensho): domain.IsDefaultWinDecisionStr.
const ENDED_WITHOUT_FIGHT = new Set(['kiken', 'kiken-voluntary', 'kiken-injury', 'fusenpai', 'fusensho']);

// The doors whose baseline the replay treats by a rule of its own
// (keepLoserPoints, repairFillsIDs): Go only.
const GO_ONLY_DOORS = new Set(['decision', 'repair']);

// ScoreEventGroup: the change group an event changes.
function groupOf(e) {
  switch (e.kind) {
    case REP_PICK: return e.side === SIDE_B ? GROUP_REP_PICK_B : GROUP_REP_PICK_A;
    case POOL_REP: return GROUP_REP;
    case FLAG_ADD: case FLAG_REMOVE: return GROUP_FLAGS;
  }
  if ((e.pos || 0) !== 0) return `bout:${e.pos}`;
  if (e.kind === ENCHO_ADD || e.kind === ENCHO_REMOVE) return GROUP_ENCHO;
  return GROUP_POINTS;
}

const groupStamp = (b, g) => (b.stamps && b.stamps[g] != null ? b.stamps[g] : b.stamp);

function finishesFought(b) {
  const r = b.values && b.values[GROUP_RESULT];
  return !!r && r.status === 'completed' && !ENDED_WITHOUT_FIGHT.has(r.decision || '');
}

function groupOrder(values) {
  const rank = (g) => {
    if (g === GROUP_RECORDED) return 3;
    if (g === GROUP_REP_PICK_A || g === GROUP_REP_PICK_B) return 2;
    return isBoutGroup(g) ? 1 : 0;
  };
  return Object.keys(values).sort((x, y) => rank(x) - rank(y) || (x < y ? -1 : x > y ? 1 : 0));
}

// baselineItems: a baseline replays each group at that group's own stamp, and
// a fought finish dates its scoring and overtime no earlier than its result.
function baselineItems(line, b) {
  const resultAt = finishesFought(b) ? groupStamp(b, GROUP_RESULT) : 0;
  const byStamp = new Map();
  for (const g of groupOrder(b.values || {})) {
    let s = groupStamp(b, g);
    if (resultAt > 0 && (isScoringGroup(g) || g === GROUP_ENCHO) && s < resultAt) s = resultAt;
    if (!byStamp.has(s)) byStamp.set(s, []);
    byStamp.get(s).push(g);
  }
  return [...byStamp].map(([s, groups]) => ({ line, stamp: s, baseline: true, id: b.id, groups }));
}

function compareItems(x, y) {
  if (x.stamp !== y.stamp) return x.stamp < y.stamp ? -1 : 1;
  if (x.baseline !== y.baseline) return x.baseline ? 1 : -1;
  if (x.id !== y.id) return x.id < y.id ? -1 : 1;
  return x.line - y.line;
}

const RESULT_FIELDS = ['status', 'winner', 'winnerId', 'decision', 'decisionBy', 'decisionReason', 'correctionReason', 'resultSource'];

// setGroup is state.SetGroupFromValue for the groups of an individual or engi
// match.
function setGroup(m, g, v) {
  switch (g) {
    case GROUP_POINTS:
      m.ipponsA = [...((v && v.ipponsA) || [])];
      m.ipponsB = [...((v && v.ipponsB) || [])];
      m.hansokuA = (v && v.hansokuA) || 0;
      m.hansokuB = (v && v.hansokuB) || 0;
      return;
    case GROUP_RESULT:
      for (const k of RESULT_FIELDS) m[k] = (v && v[k]) || '';
      return;
    case GROUP_ENCHO:
      m.encho = v ? { periodCount: v.periodCount || 0 } : null;
      return;
    case GROUP_FLAGS:
      m.flagsA = (v && v.flagsA) || 0;
      m.flagsB = (v && v.flagsB) || 0;
      return;
    case GROUP_REP:
      m.repPlayerA = (v && v.repPlayerA) || '';
      m.repPlayerB = (v && v.repPlayerB) || '';
      return;
    case GROUP_REP_PICK_A:
    case GROUP_REP_PICK_B:
    case GROUP_RECORDED:
      // They live on a team match's bout rows; a match without any has
      // nothing to set (only the group's stamp moves).
      if ((m.subResults || []).length > 0) throw new Error(`score_events: group ${g} on a match with bouts is folded from stage 5`);
      return;
  }
  throw new Error(`score_events: group ${g} is folded from stage 5`);
}

// replayHead is engine.replayScoring for a log of individual or engi lines:
// `match` is the stored match (the cache, the match AFTER every line) and the
// log is head followed by lines, in that order of arrival (a line's index in
// the log is what a baseline's basis.seen counts). Lines are folded in (stamp,
// id) order; a baseline must be the first line to set each of its groups (the
// log's head) and must not land on a finished match. Returns {match,
// outcomes}, outcomes being [{id, line, outcome}] for every event line in log
// order, so an id logged twice has two (the second a duplicate). Neither
// argument is changed.
export function replayHead(match, head, lines, opts = {}) {
  const log = [...(head || []), ...(lines || [])];
  if (log.length === 0) return { match: copy(match), outcomes: [] };
  let cur = copy(match);
  const stamps = { ...match.groupStamps };
  const govern = {};
  const applied = {};
  const covered = {};
  const early = {};
  const outcome = {};
  const firstLine = {};
  const items = [];
  log.forEach((l, i) => {
    if (l.baseline) {
      if (GO_ONLY_DOORS.has(l.baseline.door)) throw new Error(`score_events: a ${l.baseline.door} baseline is replayed in Go only`);
      items.push(...baselineItems(i, l.baseline));
      for (const g of Object.keys(l.baseline.values || {})) covered[g] = true;
      return;
    }
    items.push({ line: i, stamp: l.event.stamp, baseline: false, id: l.event.id });
    if (!(l.event.id in firstLine)) firstLine[l.event.id] = i;
  });
  items.sort(compareItems);

  const closed = () => GROUP_RESULT in govern && cur.status === 'completed';
  const basisContains = (b, i, e) => {
    const basis = b.basis || {};
    if (i < (basis.seen || 0) || (basis.own || []).includes(e.id)) return true;
    return !!b.session && e.session === b.session && basis.seq > 0 && e.seq > 0 && e.seq <= basis.seq;
  };
  const contains = (b, i) => {
    const e = log[i].event;
    if (e.stamp > groupStamp(b, groupOf(e))) return false;
    return basisContains(b, i, e);
  };
  const absorb = (b, i) => { outcome[i] = contains(b, i) ? RECORDED : BEFORE_BASELINE; };

  const applyBaseline = (line, b, groups) => {
    if (closed()) throw new Error('score_events: a baseline over a finished match is replayed in Go only');
    for (const g of groups) {
      if (g in govern) throw new Error(`score_events: a second baseline on group ${g} is replayed in Go only`);
      setGroup(cur, g, b.values[g]);
      if (g === GROUP_POINTS) {
        // The first baseline to set the points mints its own ids: the
        // cache's are the match after every line, not this point's.
        const ids = markIdsOf(cur.ipponsA, cur.ipponsB,
          assignMarks([], [], cur.ipponsA, 0, SIDE_A), assignMarks([], [], cur.ipponsB, 0, SIDE_B));
        if (ids) cur.markIds = ids;
        else delete cur.markIds;
      }
      const s = groupStamp(b, g);
      if (s > 0) stamps[g] = s;
      else delete stamps[g];
      for (const i of applied[g] || []) absorb(b, i);
      for (const i of early[g] || []) absorb(b, i);
      applied[g] = [];
      early[g] = [];
      govern[g] = line;
    }
  };

  const applyEvent = (line, e) => {
    const g = groupOf(e);
    if (closed()) { outcome[line] = MATCH_CLOSED; return; }
    const governed = g in govern;
    if (!governed && covered[g]) { (early[g] = early[g] || []).push(line); return; }
    if (governed && contains(log[govern[g]].baseline, line)) { outcome[line] = RECORDED; return; }
    const r = foldEvent(cur, e, opts);
    outcome[line] = r.outcome;
    if (r.outcome === APPLIED) {
      cur = r.match;
      stamps[g] = Math.max(stamps[g] || 0, e.stamp);
      (applied[g] = applied[g] || []).push(line);
    }
  };

  for (const it of items) {
    const l = log[it.line];
    if (l.baseline) applyBaseline(it.line, l.baseline, it.groups);
    else if (firstLine[l.event.id] !== it.line) outcome[it.line] = DUPLICATE;
    else applyEvent(it.line, l.event);
  }
  // An early event whose baseline never set its group is still older than it.
  for (const g of Object.keys(early)) for (const i of early[g]) outcome[i] = BEFORE_BASELINE;

  const keys = Object.keys(stamps);
  if (keys.length > 0) {
    cur.groupStamps = stamps;
    cur.modifiedAt = Math.max(...keys.map((k) => stamps[k]));
  } else {
    delete cur.groupStamps;
  }
  cur.scoreVersion = log.length;
  const outcomes = [];
  log.forEach((l, i) => { if (l.event) outcomes.push({ id: l.event.id, line: i, outcome: outcome[i] }); });
  return { match: cur, outcomes };
}

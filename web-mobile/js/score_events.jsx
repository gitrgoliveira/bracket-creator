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
// Scope: position 0 of an individual or engi match (stage 3), and a team
// match's bouts (stage 5): the numbered bouts and the representative bout
// (engine.foldBout over domain.BoutScore: the scoring, the per-bout decision
// with its fusensho snapshot, a kachinuki bout's fighter, the representative
// verdict), the representative picks, a team's encounter overtime and a pool
// representative bout's rep players. The fold's shape is its options:
// {engi}, {team} for a fixed-order team, {team, kachinuki} for kachinuki. The
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
// The reason, and the sentence, the server gives a tap on a bout a removal
// took off (engine.EventReasonBoutRemoved, EventMessageBoutRemoved): the
// board says the same as soon as the removal is on screen (applyPending).
export const BOUT_REMOVED = 'bout_removed';
export const BOUT_REMOVED_MESSAGE = "Not applied: the bout it was made on was removed, so it was kept in the match's history. Pick the bout's fighters to add it again.";
// Which of them need no word to the operator has ONE owner, write_result.jsx
// (tapOutcomeQuiet), which every reader asks (stage 3 review 11, Y2).

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
function foldMatchLevel(m, e, opts = {}) {
  // A pool representative or tie-break bout is ONE individual bout even in a
  // team competition (state.IsPoolSupplementaryMatchID).
  const singleBout = isSupplementaryId(m.id);
  if (e.kind === ENCHO_ADD || e.kind === ENCHO_REMOVE) {
    // A kachinuki encounter never carries a match-level overtime
    // (state.Competition.ClearKachinukiEncounterEncho).
    if (opts.kachinuki && !singleBout) return NOT_ALLOWED;
    const next = enchoCount(m) + (e.kind === ENCHO_REMOVE ? -1 : 1);
    if (next < 0) return NO_TARGET;
    m.encho = next > 0 ? { periodCount: next } : null;
    return APPLIED;
  }
  if (e.kind === BOUT_DECISION || e.kind === BOUT_FIGHTER || e.kind === DAIHYOSEN_VERDICT) return NOT_ALLOWED;
  // A team encounter scores on its bouts, never at match level.
  if (opts.team && !singleBout) return NOT_ALLOWED;
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

// ---------------------------------------------------------------------------
// A team match's bouts (stage 5): domain.BoutScore and engine.foldBout,
// boutScoreOf and writeBoutRow
// ---------------------------------------------------------------------------

const DAIHYOSEN_POS = -1; // state.DaihyosenSubPosition
// state.IsPoolSupplementaryMatchID: "<pool>-DH-<n>" or "<pool>-TB-<n>", the
// digits after the LAST marker (hasNumericSuffixAfter).
const isSupplementaryId = (id) => typeof id === 'string' && /-(?:DH|TB)-\d+$/.test(id);
// A side's name: the wire carries a string, a client copy the side object.
const sideName = (side) => (side && typeof side === 'object' ? side.name || '' : side || '');
const setOrDelete = (o, k, v) => { if (v) o[k] = v; else delete o[k]; };

// A bout's score as the fold holds it (domain.BoutScore): each side's ippons
// with their mark ids, its outstanding fouls, the overtime periods, a
// hikiwake, the side a per-bout fusensho was awarded to, and `pre`, the score
// before that fusensho (replay-internal, never stored: a fusensho a stored
// row brought ends through clearFusensho).
function cloneBout(b) {
  return {
    ippons: { a: [...b.ippons.a], b: [...b.ippons.b] },
    marks: { a: [...b.marks.a], b: [...b.marks.b] },
    fouls: { a: b.fouls.a, b: b.fouls.b },
    encho: b.encho, draw: b.draw, fusensho: b.fusensho,
    pre: b.pre ? cloneBout(b.pre) : null,
  };
}

// boutScoreOf: a stored bout row as the fold reads it (engine.boutScoreOf).
function boutScoreOf(row) {
  const ia = [...(row.ipponsA || [])];
  const ib = [...(row.ipponsB || [])];
  const b = {
    ippons: { a: ia, b: ib },
    marks: { a: alignTo(ia, row.markIds && row.markIds.a), b: alignTo(ib, row.markIds && row.markIds.b) },
    fouls: { a: row.hansokuA || 0, b: row.hansokuB || 0 },
    encho: row.encho && row.encho.periodCount > 0 ? row.encho.periodCount : 0,
    draw: row.decision === 'hikiwake',
    fusensho: '',
    pre: null,
  };
  if (row.decision === 'fusensho') {
    if (ia.includes(MARU)) b.fusensho = SIDE_A;
    else if (ib.includes(MARU)) b.fusensho = SIDE_B;
  }
  return b;
}

// struckWithMarks: the points a side struck (no maru, no placeholder, no Ht)
// with their mark ids (domain.struckWithMarks).
function struckWithMarks(ippons, marks) {
  const m = alignTo(ippons, marks);
  const outI = [];
  const outM = [];
  ippons.forEach((v, i) => {
    if (isScoringIppon(v) && v !== MARU) { outI.push(v); outM.push(m[i]); }
  });
  return [outI, outM];
}

// clearFusensho ends a per-bout fusensho: the awarded side keeps only what it
// struck, the other side's points stay (domain.ClearFusensho).
function clearFusensho(b) {
  const out = cloneBout(b);
  out.pre = null;
  if (!b.fusensho) return out;
  [out.ippons[b.fusensho], out.marks[b.fusensho]] = struckWithMarks(out.ippons[b.fusensho], out.marks[b.fusensho]);
  out.fusensho = '';
  return out;
}

const fusenshoBase = (b) => (b.pre ? cloneBout(b.pre) : clearFusensho(b));

// fusenshoAllowed: a fusensho for `side` is refused against a side that
// already won the bout (domain.FusenshoAllowed).
function fusenshoAllowed(b, side) {
  if (b.fusensho === side) return true;
  return countStruck(fusenshoBase(b).ippons[other(side)]) < MAX_IPPONS_PER_SIDE;
}

function removesLosingSideMark(prev, next) {
  if (!prev.fusensho || next.fusensho !== prev.fusensho) return false;
  const won = prev.fusensho;
  const lost = other(won);
  const same = prev.ippons[won].length === next.ippons[won].length && prev.ippons[won].every((v, i) => v === next.ippons[won][i]);
  return same && prev.fouls.a === next.fouls.a && prev.fouls.b === next.fouls.b
    && next.ippons[lost].length < prev.ippons[lost].length;
}

// applyBoutScoreEdit: a fought score ends the bout's fusensho and its draw,
// except taking a mark off the side the fusensho went against, a correction
// that keeps it (domain.ApplyBoutScoreEdit).
function applyBoutScoreEdit(prev, next) {
  if (removesLosingSideMark(prev, next)) {
    if (next.pre) {
      const p = cloneBout(next.pre);
      const lost = other(prev.fusensho);
      p.ippons[lost] = [...next.ippons[lost]];
      p.marks[lost] = [...next.marks[lost]];
      next.pre = p;
    }
    return next;
  }
  const out = clearFusensho(next);
  out.draw = false;
  return out;
}

const bout = {
  // The scoring ops are the match level's (score above) on a copy, then the
  // edit rule; overtime moves alone (domain.BoutScore.AdjustEncho).
  scoring(b, e) {
    if (e.kind === ENCHO_ADD || e.kind === ENCHO_REMOVE) {
      const next = b.encho + (e.kind === ENCHO_REMOVE ? -1 : 1);
      if (next < 0) return [b, NO_TARGET];
      const out = cloneBout(b);
      out.encho = next;
      return [out, APPLIED];
    }
    const next = cloneBout(b);
    const out = foldScoring(next, e);
    return out === APPLIED ? [applyBoutScoreEdit(b, next), APPLIED] : [b, out];
  },
  setFusensho(b, side, id) {
    if (b.fusensho === side) return [b, APPLIED];
    if (!fusenshoAllowed(b, side)) return [b, NOT_ALLOWED];
    const snap = fusenshoBase(b);
    const next = cloneBout(b);
    next.fouls = { a: 0, b: 0 };
    next.draw = false;
    next.pre = snap;
    const maru = b.encho > 0 ? [MARU] : [MARU, MARU];
    next.ippons[side] = maru;
    next.marks[side] = maru.map((_, i) => (i > 0 ? `${id}:${i}` : id));
    const o = other(side);
    [next.ippons[o], next.marks[o]] = struckWithMarks(snap.ippons[o], snap.marks[o]);
    next.fusensho = side;
    return [next, APPLIED];
  },
  clearDecision(b) {
    let out = b;
    if (b.fusensho) {
      if (b.pre) {
        out = cloneBout(b.pre);
        out.encho = b.encho;
      } else {
        out = clearFusensho(b);
      }
    } else {
      out = cloneBout(b);
    }
    out.draw = false;
    out.pre = null;
    return out;
  },
  setDraw(b) {
    const out = clearFusensho(b);
    out.draw = true;
    return out;
  },
};

const boutPlayed = (b) => b.ippons.a.length > 0 || b.ippons.b.length > 0 || b.fouls.a > 0 || b.fouls.b > 0
  || !!b.fusensho || b.draw || b.encho > 0;

// boutWinnerSide: a hantei on a level score, else no winner on a draw, else
// the fusensho side, else the higher count (domain.BoutWinnerSide).
function boutWinnerSide(aCount, bCount, draw, fusensho, hanteiSide) {
  if ((hanteiSide === SIDE_A || hanteiSide === SIDE_B) && aCount === bCount) return hanteiSide;
  if (draw) return '';
  if (fusensho === SIDE_A || fusensho === SIDE_B) return fusensho;
  if (aCount > bCount) return SIDE_A;
  if (bCount > aCount) return SIDE_B;
  return '';
}

// rowWinnerSide: the side a bout row's winner names, ids first, then the
// fighters' names, then the teams' (state.SubBoutWinnerSide).
function rowWinnerSide(row, teamA, teamB) {
  const winner = row.winner || '';
  const same = !!row.sideA && row.sideA === row.sideB;
  const a = same ? '' : row.sideA || '';
  const b = same ? '' : row.sideB || '';
  const wid = row.winnerMemberId || '';
  const aid = row.sideAMemberId || '';
  const bid = row.sideBMemberId || '';
  if (wid) {
    if (aid && wid === aid) return SIDE_A;
    if (bid && wid === bid) return SIDE_B;
  }
  if (!(wid && aid && bid) && winner) {
    if (a && winner === a) return SIDE_A;
    if (b && winner === b) return SIDE_B;
  }
  if (!winner || same) return '';
  if (winner === teamA || (row.sideA && winner === row.sideA)) return SIDE_A;
  if (winner === teamB || (row.sideB && winner === row.sideB)) return SIDE_B;
  return '';
}

// boutResultChanged: a different winner side or decision for the bout, every
// draw read as one and "fought" as none (engine.boutResultChanged).
function boutResultChanged(before, row, teamA, teamB) {
  const decision = (sub) => (sub.decision === 'hikiwake' ? 'hikiwake' : sub.decision === 'fought' ? '' : sub.decision || '');
  return rowWinnerSide(before, teamA, teamB) !== rowWinnerSide(row, teamA, teamB) || decision(before) !== decision(row);
}

const rowEncho = (row) => (row.encho && row.encho.periodCount > 0 ? row.encho.periodCount : 0);

// writeBoutRow: the bout row the score makes (engine.writeBoutRow): its
// ippons, fouls, overtime and mark ids, the decision (daihyosen on the
// representative bout, fusensho, a hikiwake for a played bout with no
// winner), the winner (the team; a kachinuki bout's fighter), and the winner's
// member id on a kachinuki bout or the representative bout.
function writeBoutRow(row, b, m, opts) {
  row.ipponsA = [...b.ippons.a];
  row.ipponsB = [...b.ippons.b];
  row.hansokuA = b.fouls.a;
  row.hansokuB = b.fouls.b;
  if (b.encho > 0) row.encho = { periodCount: b.encho };
  else delete row.encho;
  const ids = markIdsOf(b.ippons.a, b.ippons.b, b.marks.a, b.marks.b);
  if (ids) row.markIds = ids;
  else delete row.markIds;
  const hantei = b.ippons.a.includes(HANTEI) ? SIDE_A : b.ippons.b.includes(HANTEI) ? SIDE_B : '';
  const w = boutWinnerSide(countScoring(b.ippons.a), countScoring(b.ippons.b), b.draw, b.fusensho, hantei);
  const isRep = row.position === DAIHYOSEN_POS;
  if (isRep) row.decision = 'daihyosen';
  else if (b.fusensho) row.decision = 'fusensho';
  else if (!w && boutPlayed(b)) row.decision = 'hikiwake';
  else row.decision = '';
  const team = w === SIDE_A ? sideName(m.sideA) : w === SIDE_B ? sideName(m.sideB) : '';
  row.winner = team;
  if (opts.kachinuki && !isRep) {
    if (w === SIDE_A) row.winner = row.sideA || team;
    else if (w === SIDE_B) row.winner = row.sideB || team;
  }
  let wid = '';
  if (opts.kachinuki || isRep) wid = w === SIDE_A ? row.sideAMemberId : w === SIDE_B ? row.sideBMemberId : '';
  setOrDelete(row, 'winnerMemberId', wid);
  // A client copy's derived verdict flag is the reader's (normalizeMatch).
  delete row.decidedByHantei;
}

// keepEntries: the ippons `keep` holds, each with its mark id.
function keepEntries(ippons, marks, keep) {
  const m = alignTo(ippons, marks);
  const outI = [];
  const outM = [];
  ippons.forEach((v, i) => { if (keep(v)) { outI.push(v); outM.push(m[i]); } });
  return [outI, outM];
}

// reconcileWinnerMemberId: the representative row's winner id follows its
// picks (state.SubMatchResult.ReconcileWinnerMemberID): the side its winner
// names by name, that side's pick.
function reconcileWinnerMemberId(row) {
  delete row.winnerMemberId;
  if (!row.winner || (row.sideA && row.sideA === row.sideB)) return;
  if (row.winner === row.sideA) setOrDelete(row, 'winnerMemberId', row.sideAMemberId);
  else if (row.winner === row.sideB) setOrDelete(row, 'winnerMemberId', row.sideBMemberId);
}

// foldBout folds a tap on bout `e.pos` of a team match (engine.foldBout).
// `ctx.bouts` keeps each bout's score between taps of one fold, the
// fusensho snapshot with it; `ctx.stamps` is the replay's group stamps.
// createsMissingRow says an event on bout pos may make the row when the match
// holds none (engine.ReplayShape.createsMissingRow): a numbered bout of a
// fixed-order team match, within its team size (`opts.teamSize`), and any
// numbered bout of a kachinuki encounter (its first bout and a pairing added
// by hand), so no device writes a bout row ahead of its taps.
export function createsMissingRow(opts, pos) {
  if (!opts.team) return false;
  if (opts.kachinuki) return pos >= 1;
  return pos >= 1 && pos <= (Number(opts.teamSize) || 0);
}

// fighterIs: whether a row's side holds the fighter a server fighter event's
// target names, "id:<member id>" or "name:<name>" (engine.fighterIs).
function fighterIs(row, e) {
  const name = (e.side === SIDE_B ? row.sideB : row.sideA) || '';
  const member = (e.side === SIDE_B ? row.sideBMemberId : row.sideAMemberId) || '';
  if (e.target.startsWith('id:')) return member === e.target.slice(3);
  const n = e.target.startsWith('name:') ? e.target.slice(5) : e.target;
  return member === '' && name === n;
}

// serverFighterEvent: the server's own fighter events pass the closed-match
// rule (engine.serverFighterEvent): they name who fought, never a score.
const serverFighterEvent = (e) => e.session === 'server' && e.kind === BOUT_FIGHTER;

// withEmptyBoutRow is rows with an empty row at pos, the numbered rows in
// position order ahead of the representative bout (engine.withEmptyBoutRow).
function withEmptyBoutRow(rows, pos) {
  const empty = { position: pos, sideA: '', sideB: '', ipponsA: [], ipponsB: [], hansokuA: 0, hansokuB: 0, winner: '', decision: '' };
  const at = rows.findIndex((r) => r && (r.position < 0 || r.position > pos));
  return at < 0 ? [...rows, empty] : [...rows.slice(0, at), empty, ...rows.slice(at)];
}

function foldBout(m, e, opts, ctx) {
  let rows = m.subResults || [];
  let idx = rows.findIndex((r) => r && r.position === e.pos);
  if (idx < 0) {
    // A numbered bout the match holds no row for yet (createsMissingRow: a
    // fixed-order bout of a match started from the queue, a kachinuki first
    // bout, a pairing added by hand): the row is made, and kept only if the
    // event applies (m.subResults is set only then).
    // A bout whose row a line removed keeps its stamp, a tombstone: an
    // event stamped at or before the removal does not make it again; after
    // it only a fighter picked there does (the pairing added again), since a
    // bare strike would make a bout with no fighters (engine.foldBout).
    const tomb = ctx.stamps[`bout:${e.pos}`] || 0;
    if (!createsMissingRow(opts, e.pos) || tomb >= (e.stamp || 0) || (tomb > 0 && opts.kachinuki && e.kind !== BOUT_FIGHTER)) {
      // The reason the server gives such a tap (engine.EventReasonBoutRemoved).
      if (tomb > 0 && createsMissingRow(opts, e.pos)) ctx.reason = BOUT_REMOVED;
      return NO_TARGET;
    }
    rows = withEmptyBoutRow(rows, e.pos);
    idx = rows.findIndex((r) => r && r.position === e.pos);
  }
  const before = rows[idx];
  const row = { ...before };
  let b = ctx.bouts.has(e.pos) ? cloneBout(ctx.bouts.get(e.pos)) : boutScoreOf(before);
  let out;
  switch (e.kind) {
    case BOUT_DECISION:
      if (e.value === 'hikiwake') { b = bout.setDraw(b); out = APPLIED; }
      else if (e.value === 'fusensho') {
        if (e.pos === DAIHYOSEN_POS) return NOT_ALLOWED;
        [b, out] = bout.setFusensho(b, sideOf(e), e.id);
      } else { b = bout.clearDecision(b); out = APPLIED; }
      break;
    case BOUT_FIGHTER:
      if (!opts.kachinuki || e.pos <= 0) return NOT_ALLOWED;
      // On the first bout too: the lineup's fighters reach it as the server's
      // own bout.fighter events (engine.openerSeatEvents), ordered by stamp
      // like every tap. One that carries the stayer on names, in target, the
      // fighter it replaces: a side holding another keeps it (engine.fighterIs).
      if (e.target && !fighterIs(row, e)) return NOT_ALLOWED;
      if (e.side === SIDE_B) { row.sideB = e.name || ''; setOrDelete(row, 'sideBMemberId', e.memberId); }
      else { row.sideA = e.name || ''; setOrDelete(row, 'sideAMemberId', e.memberId); }
      out = APPLIED;
      break;
    case DAIHYOSEN_VERDICT: {
      if (e.value !== 'clear' && countScoring(b.ippons.a) !== countScoring(b.ippons.b)) return NOT_ALLOWED;
      const notHt = (v) => v !== HANTEI;
      [b.ippons.a, b.marks.a] = keepEntries(b.ippons.a, b.marks.a, notHt);
      [b.ippons.b, b.marks.b] = keepEntries(b.ippons.b, b.marks.b, notHt);
      if (e.value !== 'clear') {
        const s = sideOf(e);
        [b.ippons[s], b.marks[s]] = appendMark(b.ippons[s], b.marks[s], HANTEI, e.id);
      }
      out = APPLIED;
      break;
    }
    default:
      [b, out] = bout.scoring(b, e);
  }
  if (out !== APPLIED) return out;
  ctx.bouts.set(e.pos, b);
  writeBoutRow(row, b, m, opts);
  if (row.recorded && (rowEncho(row) > rowEncho(before) || boutResultChanged(before, row, sideName(m.sideA), sideName(m.sideB)))) {
    // The bout is being fought again: it is no longer recorded
    // (mergeHold.unrecordReopenedBouts' rule).
    delete row.recorded;
    ctx.stamps[GROUP_RECORDED] = Math.max(ctx.stamps[GROUP_RECORDED] || 0, e.stamp || 0);
  }
  m.subResults = rows.map((r, i) => (i === idx ? row : r));
  return APPLIED;
}

// foldEvent folds one event onto a copy of match (engine.foldEvent) and returns
// {match, outcome}; the match given is never changed, and on any outcome but
// applied the copy equals it. opts: {engi} for an engi match, {team} for a
// fixed-order team match, {team, kachinuki} for a kachinuki one. `ctx` carries
// a fold's state from one event to the next (each bout's score with its
// fusensho snapshot, and the replay's group stamps); omitted, the event is
// folded on its own.
export function foldEvent(match, e, opts = {}, ctx = null) {
  const c = ctx || { bouts: new Map(), stamps: {} };
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
      // A rep player is the fighter a TEAM fields for a pool representative
      // or tie-break bout, the only match the replay takes one on.
      if (!opts.team || !isSupplementaryId(m.id)) { outcome = NOT_ALLOWED; break; }
      if (e.side === SIDE_B) m.repPlayerB = e.name || '';
      else m.repPlayerA = e.name || '';
      outcome = APPLIED;
      break;
    case REP_PICK: {
      // Only a team match's representative bout holds the pick this names.
      const rows = m.subResults || [];
      const idx = rows.findIndex((r) => r && r.position === DAIHYOSEN_POS);
      if (idx < 0 || e.pos !== DAIHYOSEN_POS) { outcome = NO_TARGET; break; }
      const row = { ...rows[idx] };
      setOrDelete(row, e.side === SIDE_B ? 'sideBMemberId' : 'sideAMemberId', e.memberId);
      reconcileWinnerMemberId(row);
      m.subResults = rows.map((r, i) => (i === idx ? row : r));
      outcome = APPLIED;
      break;
    }
    default:
      if (opts.engi) outcome = NOT_ALLOWED;
      else if ((e.pos || 0) === 0) outcome = foldMatchLevel(m, e, opts);
      else if (!opts.team) outcome = NOT_ALLOWED;
      else outcome = foldBout(m, e, opts, c);
  }
  return { match: outcome === APPLIED ? m : copy(match), outcome };
}

// ---------------------------------------------------------------------------
// What a board shows
// ---------------------------------------------------------------------------

const markIdSet = (m) => new Set([...((m && m.markIds && m.markIds.a) || []), ...((m && m.markIds && m.markIds.b) || [])]);
// markHolder: what carries the marks a tap at e.pos names, the match or the
// bout row (a team bout's marks are its row's).
const markHolder = (m, e) => ((e.pos || 0) === 0 ? m : (m.subResults || []).find((r) => r && r.position === e.pos));

// applyPending is what this device's board shows: a copy of the server's last
// confirmed match (which carries markIds and scoreVersion) with the device's
// still-pending taps folded on top, in order, by the same fold. outcomes is
// {id: outcome} as the fold reads each tap here: a tap on a finished match is
// matchClosed and one on a scheduled match notStarted (both change nothing),
// and a tap whose mark the server match already carries (its id is a mark id)
// is a duplicate, so an echo that lands before the outbox's answer does not
// show the point twice. A tap that leaves no mark id (a foul that lands no H,
// an overtime period, a removal) is told apart by its seq: the server copy
// names, per event session, the highest seq its log holds (eventSeqs), and a
// tap at or below its own session's is in the copy already, whatever the
// server made of it. scoreVersion and modifiedAt stay the server's: the
// board is not a newer copy of the match than the one the server confirmed.
// Neither argument is changed.
export function applyPending(serverMatch, pending, opts = {}) {
  let match = withMintedMarkIds(copy(serverMatch));
  const outcomes = {};
  // `reasons`: why a tap is not applied, where the server gives one too (a tap
  // on a bout the match's copy shows removed: BOUT_REMOVED).
  const reasons = {};
  const held = (serverMatch && serverMatch.eventSeqs) || {};
  // The copy's bout tombstones (a removed bout keeps its `bout:N` stamp), so a
  // pending tap on a bout another device removed folds as the server's replay
  // folds it (stage 6 review 7): not applied once the removal is on screen.
  const stamps = {};
  for (const [g, s] of Object.entries((serverMatch && serverMatch.groupStamps) || {})) {
    if (g.startsWith('bout:')) stamps[g] = s;
  }
  const ctx = { bouts: new Map(), stamps };
  for (const e of pending || []) {
    if (match.status === 'completed' && !boutsStayOpen(match, groupOf(e), opts)) { outcomes[e.id] = MATCH_CLOSED; continue; }
    if (match.status === 'scheduled') { outcomes[e.id] = NOT_STARTED; continue; }
    if (e.session && e.seq > 0 && Number(held[e.session]) >= e.seq) {
      outcomes[e.id] = DUPLICATE;
      continue;
    }
    if ((e.kind === IPPON_ADD || e.kind === FOUL_ADD) && markIdSet(markHolder(match, e)).has(e.id)) {
      outcomes[e.id] = DUPLICATE;
      continue;
    }
    ctx.reason = undefined;
    const r = foldEvent(match, e, opts, ctx);
    match = r.match;
    outcomes[e.id] = r.outcome;
    if (r.outcome !== APPLIED && ctx.reason) reasons[e.id] = ctx.reason;
  }
  return { match, markIds: match.markIds || null, outcomes, reasons };
}

// withMintedMarkIds: a copy that names no mark ids (for the match's points, or
// for a bout row that holds marks) is one whose score log has not begun: the
// server's first baseline mints each mark's id from what the mark is
// (engine.MigrationBaseline: b:<pos>:<side>:<letter>:<n>), so the board names
// them the same way, and a removal made against them folds here as it will
// there. A copy that names ids keeps them. The match given is changed.
function withMintedMarkIds(m) {
  const mint = (holder, pos) => {
    if (holder.markIds) return;
    const ia = holder.ipponsA || [];
    const ib = holder.ipponsB || [];
    const ids = markIdsOf(ia, ib, assignMarks([], [], ia, pos, SIDE_A), assignMarks([], [], ib, pos, SIDE_B));
    if (ids) holder.markIds = ids;
  };
  if (!m) return m;
  mint(m, 0);
  for (const r of m.subResults || []) if (r && r.position) mint(r, r.position);
  return m;
}

// boutsStayOpen: a fixed-order team match closed by a match-level kiken,
// fusenpai or fusensho keeps its numbered bouts open, merged by bout (T1,
// engine replayer.boutsStayOpen): a bout a device that never saw the decision
// scores keeps its score. Every other finished match is closed whole.
function boutsStayOpen(m, g, opts) {
  if (!isBoutGroup(g) || Number(g.slice(5)) < 1) return false;
  return !!opts.team && !opts.kachinuki && m.status === 'completed' && ENDED_WITHOUT_FIGHT.has(m.decision || '');
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
    case GROUP_REP_PICK_B: {
      // A side's representative pick, on the representative bout's row; a
      // match without one has nothing to set (state.withRepPick).
      const key = g === GROUP_REP_PICK_A ? 'sideAMemberId' : 'sideBMemberId';
      m.subResults = (m.subResults || []).map((r) => {
        if (!r || r.position !== DAIHYOSEN_POS) return r;
        const row = { ...r };
        setOrDelete(row, key, v && v[key]);
        return row;
      });
      return;
    }
    case GROUP_RECORDED: {
      // Only a numbered bout can be recorded (state.setRecordedPositions).
      const positions = (v && v.positions) || [];
      m.subResults = (m.subResults || []).map((r) => {
        if (!r) return r;
        const row = { ...r };
        if (row.position > 0 && positions.includes(row.position)) row.recorded = true;
        else delete row.recorded;
        return row;
      });
      return;
    }
  }
  if (isBoutGroup(g)) {
    // A bout row (state.SetGroupFromValue through CopyGroup): the value's
    // row, keeping the recorded mark and the representative picks the row
    // already holds (those are their own groups); no row removes it.
    const pos = Number(g.slice(5));
    const rows = m.subResults || [];
    const dst = rows.find((r) => r && r.position === pos);
    let row = null;
    if (v) {
      row = { ...copy(v), position: pos };
      delete row.markIds;
      if (dst && dst.recorded) row.recorded = true;
      else delete row.recorded;
      if (pos === DAIHYOSEN_POS && dst) {
        setOrDelete(row, 'sideAMemberId', dst.sideAMemberId);
        setOrDelete(row, 'sideBMemberId', dst.sideBMemberId);
      }
    }
    m.subResults = setSubAt(rows, pos, row);
    return;
  }
  throw new Error(`score_events: ${g} is not a group`);
}

// setSubAt: rows with the row at `pos` replaced by `row`, removed when row is
// null, or inserted in bout order (numbered ascending, the representative
// bout after) when none is there (state.setSubAt).
function setSubAt(rows, pos, row) {
  const order = (p) => (p < 0 ? (1 << 30) - p : p);
  const out = [];
  let placed = false;
  for (const r of rows) {
    if (r && r.position === pos) {
      if (row && !placed) { out.push(row); placed = true; }
      continue;
    }
    out.push(r);
  }
  if (row && !placed) {
    out.push(row);
    out.sort((x, y) => order(x.position) - order(y.position));
  }
  return out;
}

// logEventSeqs mirrors engine.ScoreLogEventSeqs: per event session, the
// highest seq the log holds, whatever each tap's outcome; null when none, and
// past MAX_EVENT_SEQ_SESSIONS the sessions logged latest are kept.
const MAX_EVENT_SEQ_SESSIONS = 32;
function logEventSeqs(log) {
  const top = {};
  const last = {};
  log.forEach((l, i) => {
    const e = l && l.event;
    if (!e || !(e.seq > 0) || !e.session) return;
    top[e.session] = Math.max(top[e.session] || 0, e.seq);
    last[e.session] = i;
  });
  const sessions = Object.keys(top);
  if (sessions.length === 0) return null;
  if (sessions.length > MAX_EVENT_SEQ_SESSIONS) {
    sessions.sort((a, b) => last[b] - last[a]);
    for (const s of sessions.slice(MAX_EVENT_SEQ_SESSIONS)) delete top[s];
  }
  return top;
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
  const ctx = { bouts: new Map(), stamps };
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
  // engine.replayer.startMadeRowsEmpty: a bout an event may make the row of
  // (createsMissingRow) that no baseline of the log sets starts from no row,
  // since its row in the match can only be one these events made, and they
  // are folded again (each would count twice).
  const madeEmpty = {};
  for (const l of log) {
    const e = l.event;
    if (!e || !createsMissingRow(opts, e.pos)) continue;
    const g = groupOf(e);
    if (g !== `bout:${e.pos}` || covered[g] || madeEmpty[g]) continue;
    madeEmpty[g] = true;
    cur.subResults = (cur.subResults || []).filter((r) => !r || r.position !== e.pos);
    delete stamps[g];
  }
  items.sort(compareItems);
  // engine.closingResults: per event line, the baseline line of the first
  // result after it in stamp order that finishes the match.
  const closer = {};
  let nextCloser = -1;
  for (let k = items.length - 1; k >= 0; k--) {
    const it = items[k];
    const l = log[it.line];
    if (l.baseline) {
      if ((it.groups || []).includes(GROUP_RESULT)) {
        const r = (l.baseline.values || {})[GROUP_RESULT];
        if (r && r.status === 'completed') nextCloser = it.line;
      }
      continue;
    }
    if (nextCloser >= 0) closer[it.line] = nextCloser;
  }

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
      } else if (isBoutGroup(g)) {
        // A bout row's marks likewise (b:<pos>:<side>:<letter>:<n>), and the
        // bout's folded score starts again from the row.
        const pos = Number(g.slice(5));
        ctx.bouts.delete(pos);
        cur.subResults = (cur.subResults || []).map((r) => {
          if (!r || r.position !== pos) return r;
          const row = { ...r };
          const ia = row.ipponsA || [];
          const ib = row.ipponsB || [];
          const rowIds = markIdsOf(ia, ib, assignMarks([], [], ia, pos, SIDE_A), assignMarks([], [], ib, pos, SIDE_B));
          if (rowIds) row.markIds = rowIds;
          else delete row.markIds;
          return row;
        });
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

  // engine.replayer.closedBefore (D5): a finish or a decision that sent a
  // basis governs every scoring group and the overtime at its stamp, so an
  // event stamped before it that it did not have in view is turned away.
  const closedBefore = (line, e, g) => {
    if (!(line in closer) || serverFighterEvent(e)) return false;
    if (!(isScoringGroup(g) || g === 'encho')) return false;
    const b = log[closer[line]].baseline;
    if (g in (b.values || {})) return false;
    if (boutsStayOpen((b.values || {})[GROUP_RESULT] || {}, g, opts)) return false;
    return !!b.basisSent && !basisContains(b, line, e);
  };

  const applyEvent = (line, e) => {
    const g = groupOf(e);
    if (closed() && !boutsStayOpen(cur, g, opts) && !serverFighterEvent(e)) { outcome[line] = MATCH_CLOSED; return; }
    if (closedBefore(line, e, g)) { outcome[line] = BEFORE_BASELINE; return; }
    const governed = g in govern;
    if (!governed && covered[g]) { (early[g] = early[g] || []).push(line); return; }
    if (governed && contains(log[govern[g]].baseline, line)) { outcome[line] = RECORDED; return; }
    const r = foldEvent(cur, e, opts, ctx);
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
  const seqs = logEventSeqs(log);
  if (seqs) cur.eventSeqs = seqs;
  else delete cur.eventSeqs;
  const outcomes = [];
  log.forEach((l, i) => { if (l.event) outcomes.push({ id: l.event.id, line: i, outcome: outcome[i] }); });
  return { match: cur, outcomes };
}

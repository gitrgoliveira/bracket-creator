// draft_taps.jsx: the ONE owner of "a draft kept when another device started
// the match is saved as the taps that would have made it" (operator decision
// D1 (a), 2026-10-17, bc-evts stage 5).
//
// A score editor holds a draft while the match is not running (the Scores
// tab's Score on a scheduled match). When another device starts the match the
// draft is kept on screen with Save and Discard; Save sends the draft's
// changes as the scoring events the same gestures send on a running match,
// through the outbox, in order. The changes are the draft against the copy it
// was typed over (`base`), never against the board, so a point the other
// device scored is not taken away; one the other device also scored can count
// twice and is taken back by hand (accepted with the decision).
//
// Every function here is pure and answers events without `pos` for a match
// (pos 0) and with it for a team bout; the caller hands each to its own tap
// door (emitTap, emitBoutTap).
import {
  IPPON_ADD, IPPON_REMOVE, FOUL_ADD, FOUL_REMOVE, ENCHO_ADD, ENCHO_REMOVE,
  FLAG_ADD, FLAG_REMOVE, BOUT_DECISION, REP_PICK, POOL_REP,
} from './score_events.jsx';

// A mark a scoring tap adds or takes away: a letter, never a default-win maru,
// a hantei mark or an empty slot.
const scoring = (l) => typeof l === 'string' && l !== '' && l !== '○' && l !== 'Ht' && l !== '•';

const at = (pos) => (pos ? { pos } : {});

// letterChanges: the letters the draft holds beyond the base (in the draft's
// order), and the base's letters the draft no longer holds.
export function letterChanges(base = [], draft = []) {
  const left = (base || []).filter(scoring);
  const add = [];
  for (const l of (draft || []).filter(scoring)) {
    const i = left.indexOf(l);
    if (i >= 0) left.splice(i, 1);
    else add.push(l);
  }
  return { add, remove: left };
}

// pointTaps: one side's ippon.remove events (each naming the mark the board
// shows for that letter, the first not yet named; a letter the board no
// longer shows sends nothing) and then its ippon.add events.
export function pointTaps({ pos = 0, side, base, draft, boardLetters = [], boardIds = [] }) {
  const { add, remove } = letterChanges(base, draft);
  const named = new Set();
  const out = [];
  for (const l of remove) {
    const i = boardLetters.findIndex((b, j) => b === l && !named.has(j) && boardIds[j]);
    if (i < 0) continue;
    named.add(i);
    out.push({ kind: IPPON_REMOVE, ...at(pos), side, target: boardIds[i] });
  }
  for (const letter of add) out.push({ kind: IPPON_ADD, ...at(pos), side, letter });
  return out;
}

// countTaps: the add or remove events that move a count from `from` to `to`.
export function countTaps({ pos = 0, side, from, to, addKind, removeKind }) {
  const a = Number(from) || 0;
  const b = Number(to) || 0;
  const one = { kind: b > a ? addKind : removeKind, ...at(pos), ...(side ? { side } : {}) };
  return Array.from({ length: Math.abs(b - a) }, () => ({ ...one }));
}

// individualDraftTaps: an individual match's draft ({aPts, bPts, aFouls,
// bFouls, encho, repA, repB}) against its base, with the board's letters and
// mark ids per side ({a: {letters, ids}, b: {...}}): each side's points, then
// each side's fouls, then the overtime, then the pool representatives.
export function individualDraftTaps(base, draft, board = {}) {
  const out = [];
  for (const side of ['a', 'b']) {
    const k = side === 'a' ? 'aPts' : 'bPts';
    out.push(...pointTaps({ side, base: base[k], draft: draft[k], boardLetters: board[side]?.letters, boardIds: board[side]?.ids }));
  }
  for (const side of ['a', 'b']) {
    const k = side === 'a' ? 'aFouls' : 'bFouls';
    out.push(...countTaps({ side, from: base[k], to: draft[k], addKind: FOUL_ADD, removeKind: FOUL_REMOVE }));
  }
  out.push(...countTaps({ from: base.encho, to: draft.encho, addKind: ENCHO_ADD, removeKind: ENCHO_REMOVE }));
  for (const side of ['a', 'b']) {
    const k = side === 'a' ? 'repA' : 'repB';
    if ((draft[k] || '') !== (base[k] || '')) out.push({ kind: POOL_REP, side, name: draft[k] || '' });
  }
  return out;
}

// engiDraftTaps: an engi match's flag counts ({a, b}) against the base.
export function engiDraftTaps(base, draft) {
  return ['a', 'b'].flatMap((side) => countTaps({ side, from: base[side], to: draft[side], addKind: FLAG_ADD, removeKind: FLAG_REMOVE }));
}

// teamDraftTaps: a team sheet's bout rows (the editor's own row shape:
// {_pos, aPts, bPts, aFouls, bFouls, fusensho, draw, encho,
// aMemberIdOverride, bMemberIdOverride}) against the base rows, bout by bout
// in position order, with the board's rows ({[pos]: {a: {letters, ids}, b}})
// for removals, then the encounter's overtime (`encho`, at `enchoPos`). In a
// bout: the representative picks, the points, the fouls, its overtime, then
// its result (a fusensho or a tie, or clearing one).
export function teamDraftTaps(base, draft, board = {}) {
  const out = [];
  const baseAt = new Map((base.rows || []).map((r) => [r._pos, r]));
  const rows = [...(draft.rows || [])].sort((x, y) => (x._pos < 0) - (y._pos < 0) || x._pos - y._pos);
  for (const d of rows) {
    const pos = d._pos;
    const b = baseAt.get(pos) || {};
    for (const side of ['a', 'b']) {
      const k = `${side}MemberIdOverride`;
      if (pos < 0 && (d[k] || '') !== (b[k] || '')) out.push({ kind: REP_PICK, pos, side, memberId: d[k] || '' });
    }
    for (const side of ['a', 'b']) {
      const k = side === 'a' ? 'aPts' : 'bPts';
      out.push(...pointTaps({ pos, side, base: b[k], draft: d[k], boardLetters: board[pos]?.[side]?.letters, boardIds: board[pos]?.[side]?.ids }));
    }
    for (const side of ['a', 'b']) {
      const k = side === 'a' ? 'aFouls' : 'bFouls';
      out.push(...countTaps({ pos, side, from: b[k], to: d[k], addKind: FOUL_ADD, removeKind: FOUL_REMOVE }));
    }
    out.push(...countTaps({ pos, from: b.encho, to: d.encho, addKind: ENCHO_ADD, removeKind: ENCHO_REMOVE }));
    if ((d.fusensho || '') !== (b.fusensho || '')) {
      out.push(d.fusensho
        ? { kind: BOUT_DECISION, pos, side: d.fusensho, value: 'fusensho' }
        : { kind: BOUT_DECISION, pos, value: 'clear' });
    } else if (!!d.draw !== !!b.draw && !d.fusensho) {
      out.push({ kind: BOUT_DECISION, pos, value: d.draw ? 'hikiwake' : 'clear' });
    }
  }
  out.push(...countTaps({ pos: draft.enchoPos || 0, from: base.encho, to: draft.encho, addKind: ENCHO_ADD, removeKind: ENCHO_REMOVE }));
  return out;
}

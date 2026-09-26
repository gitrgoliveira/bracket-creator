import { describe, it, expect } from 'vitest';
import {
  barredSides, isBarredMatch, awaitedDefaultWin, defaultWinDecisionBody,
  barredNote, defaultWinActionLabel, bothBarredDrawable, bothBarredDrawAction,
  sideBarredByDecision, involvesCompetitor,
} from '../ineligible_match.jsx';
// Sets window.isKikenDecision, which sideBarredByDecision reads at call time.
import '../api_serializers.jsx';

// The server stamps `ineligibleSides` on scheduled matches only; the sides
// arrive normalized ({id, name}) on every surface that reads them.
const match = (over = {}) => ({
  id: 'Pool A-2', compId: 'c1', status: 'scheduled',
  sideA: { id: 'u', name: 'Umi E' }, sideB: { id: 'y', name: 'Yama C' },
  ...over,
});

describe('ineligible_match', () => {
  it('reads nothing from a match with no stamp', () => {
    const m = match();
    expect(isBarredMatch(m)).toBe(false);
    expect(awaitedDefaultWin(m)).toBeNull();
    expect(defaultWinDecisionBody(m)).toBeNull();
    expect(barredNote(m)).toBe('');
  });

  it('ignores a stamp on a match that is not scheduled', () => {
    for (const status of ['running', 'completed']) {
      const m = match({ status, ineligibleSides: { b: 'kiken-voluntary' } });
      expect(barredSides(m)).toEqual({ a: '', b: '' });
      expect(isBarredMatch(m)).toBe(false);
    }
  });

  it('records the default win for the opponent as fusensho naming the barred side', () => {
    const onB = match({ ineligibleSides: { b: 'kiken-voluntary' } });
    expect(isBarredMatch(onB)).toBe(true);
    expect(defaultWinDecisionBody(onB)).toEqual({
      decision: 'fusensho', decisionBy: 'shiro', decisionReason: 'auto: Yama C withdrawn',
    });
    expect(defaultWinActionLabel(onB)).toBe('Record default win for Umi E');

    const onA = match({ ineligibleSides: { a: 'fusenpai' } });
    expect(defaultWinDecisionBody(onA).decisionBy).toBe('aka');
    expect(defaultWinActionLabel(onA)).toBe('Record default win for Yama C');
  });

  it('says who cannot fight and what to do, by the decision that barred them', () => {
    expect(barredNote(match({ ineligibleSides: { b: 'kiken-voluntary' } })))
      .toBe('Yama C withdrew: record the default win.');
    expect(barredNote(match({ ineligibleSides: { b: 'kiken' } })))
      .toBe('Yama C withdrew: record the default win.');
    expect(barredNote(match({ ineligibleSides: { b: 'fusenpai' } })))
      .toBe('Yama C did not appear earlier: record the default win.');
    const injured = match({ ineligibleSides: { b: 'kiken-injury' } });
    expect(barredNote(injured)).toBe('Yama C withdrew injured: reinstate them or record the default win.');
    expect(awaitedDefaultWin(injured).reinstateable).toBe(true);
  });

  it('offers no default win when both sides are barred', () => {
    const m = match({ ineligibleSides: { a: 'fusenpai', b: 'kiken-voluntary' } });
    expect(isBarredMatch(m)).toBe(true);
    expect(awaitedDefaultWin(m)).toBeNull();
    expect(defaultWinDecisionBody(m)).toBeNull();
    expect(defaultWinActionLabel(m)).toBe('');
    expect(barredNote(m)).toBe('Both withdrew earlier: neither can fight this match.');
  });

  // bc-cse: "If both competitors withdraw, neither receives a win or
  // points" -- a pool/league match can be RECORDED as drawn; a knockout
  // bracket match cannot (it needs a winner), so it names the problem
  // instead and offers no action.
  describe('both sides barred: the draw action (bc-cse)', () => {
    const both = (over = {}) => match({ ineligibleSides: { a: 'fusenpai', b: 'kiken-voluntary' }, ...over });

    // The server accepts the draw for a pool or league id only
    // (engine.IsPoolMatchID: "Pool " prefix), so the offer follows the id.
    it('bothBarredDrawable is true for a pool/league id, false for a knockout or Swiss id', () => {
      expect(bothBarredDrawable(match({ phase: 'pool' }))).toBe(true);
      expect(bothBarredDrawable(match({ id: 'm-r1-0', phase: 'bracket' }))).toBe(false);
      expect(bothBarredDrawable(match({ id: 'Swiss-R2-0', phase: 'pool' }))).toBe(false);
    });

    it('offers the draw action for a pool/league match, naming both sides in the reason', () => {
      const action = bothBarredDrawAction(both({ phase: 'pool' }));
      expect(action).toEqual({
        label: 'Record as drawn (neither can fight)',
        body: { decision: 'hikiwake', decisionReason: 'auto: Umi E and Yama C withdrawn' },
      });
    });

    it('offers no draw action for a knockout bracket match, and the note says why', () => {
      const m = both({ id: 'm-r1-0', phase: 'bracket' });
      expect(bothBarredDrawAction(m)).toBeNull();
      expect(barredNote(m)).toBe('Neither can fight: correct the earlier withdrawal or the draw.');
    });

    it('offers no draw action when only one side is barred', () => {
      expect(bothBarredDrawAction(match({ ineligibleSides: { b: 'kiken-voluntary' }, phase: 'pool' }))).toBeNull();
    });
  });
});

// The advance after a decision skips the competitor it barred, because the
// court list shows their matches barred only after it refreshes.
describe('sideBarredByDecision', () => {
  // What /decision answers with: the stored match, sides as bare names.
  const stored = (over) => ({ id: 'Pool A-1', sideA: 'Umi E', sideB: 'Yama C', sideAId: 'u', sideBId: 'y', status: 'completed', ...over });
  const open = match({ id: 'Pool A-1', status: 'running' });

  it('names the side a withdrawal or no-show bars, from the caller\'s own match', () => {
    expect(sideBarredByDecision(stored({ decision: 'kiken-voluntary', decisionBy: 'aka', winner: 'Yama C' }), open)).toBe(open.sideA);
    expect(sideBarredByDecision(stored({ decision: 'kiken-injury', decisionBy: 'shiro', winner: 'Umi E' }), open)).toBe(open.sideB);
    expect(sideBarredByDecision(stored({ decision: 'fusenpai', decisionBy: 'shiro', winner: 'Umi E' }), open)).toBe(open.sideB);
  });

  it('bars nobody for any other decision, or for a write that has not come back', () => {
    expect(sideBarredByDecision(stored({ decision: 'fought', winner: 'Umi E' }), open)).toBeNull();
    expect(sideBarredByDecision(stored({ decision: 'fusensho', decisionBy: 'shiro', winner: 'Umi E' }), open)).toBeNull();
    expect(sideBarredByDecision({ queued: true }, open)).toBeNull();
  });

  it('involvesCompetitor finds the side on either side of a match, and nothing for no side', () => {
    const later = match({ id: 'Pool A-3', sideA: { id: 'k', name: 'Kawa D' }, sideB: { id: 'u', name: 'Umi E' } });
    expect(involvesCompetitor(later, open.sideA)).toBe(true);
    expect(involvesCompetitor(later, open.sideB)).toBe(false);
    expect(involvesCompetitor(later, null)).toBe(false);
  });
});

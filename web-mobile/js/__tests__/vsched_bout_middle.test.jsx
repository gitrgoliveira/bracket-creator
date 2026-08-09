// Regression (mp-u37s): the VSchedItem bout MIDDLE never renders a dash.
//
// Contract (CLAUDE.md § Match Decision Types): the middle slot of a bout may
// read ONLY "vs" / "X" / "(E)" / "(DH)". A dash is a CELL value, never a
// middle. The single source is boutMiddle (bracket.jsx).
//
// The bug: VSchedItem hand-rolled its no-score fallback as
//   scoreStr ? <score> : m.status === "completed" ? "-" : "vs"
// so a COMPLETED match whose score string comes back empty rendered "-" in the
// middle slot. matchScoreStr/formatIpponsScore return "" whenever both score
// cells are empty and the match is not a bye/draw/default-win, which is the
// everyday shape of a match finished with no ippon recorded (and of a match
// stored with decision "fought" but no cells).
//
// These tests mount the real VSchedItem against the REAL window.matchScoreStr
// and window.boutMiddle (bracket.jsx is imported for its side-effect window.*
// publication, which is how viewer_match.jsx reaches those helpers in the
// browser), so the empty-score-string case is produced by production code
// rather than asserted against a stub.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findInTree, hasClass, collectText } from './helpers/vdom.js';

const realReact = global.React;

describe('mp-u37s: VSchedItem bout middle is never a dash', () => {
  let runtime, VSchedItem;

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.queueLabelCompact = null;
    vi.resetModules();
    // Side-effect import: publishes the real window.matchScoreStr /
    // window.boutMiddle that viewer_match.jsx calls.
    await import('../bracket.jsx');
    ({ VSchedItem } = await import('../viewer_match.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.queueLabelCompact;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // The centre cell of the players row: either the score string
  // (vsched-item__score) or the bout middle (vsched-item__vs).
  const centreText = (m) => {
    const tree = runtime.mount(VSchedItem, { m, tweaks: {} });
    const node = findInTree(tree, n =>
      hasClass(n, 'vsched-item__vs') || hasClass(n, 'vsched-item__score'));
    expect(node).not.toBeNull();
    return collectText(node);
  };

  const base = {
    id: 'm-1',
    court: 'A',
    phase: 'bracket',
    round: 'Final',
    sideA: { id: 'a', name: 'Ryu' },
    sideB: { id: 'b', name: 'Phoenix' },
  };

  // THE bug case: completed, no score cells → matchScoreStr returns "".
  it('completed match with no recorded score renders "vs", not "-"', () => {
    const mid = centreText({ ...base, status: 'completed' });
    expect(mid).not.toBe('-');
    expect(mid).not.toContain('-');
    expect(mid).toBe('vs');
  });

  it('completed match with a winner but no recorded score renders "vs", not "-"', () => {
    const mid = centreText({ ...base, status: 'completed', winner: { id: 'a', name: 'Ryu' } });
    expect(mid).not.toContain('-');
    expect(mid).toBe('vs');
  });

  it('completed match decided "fought" with no recorded score renders "vs", not "-"', () => {
    const mid = centreText({ ...base, status: 'completed', decision: 'fought' });
    expect(mid).not.toContain('-');
    expect(mid).toBe('vs');
  });

  it('running match with no score yet still renders "vs"', () => {
    expect(centreText({ ...base, status: 'running' })).toBe('vs');
  });

  it('scheduled match renders "vs"', () => {
    expect(centreText({ ...base, status: 'scheduled' })).toBe('vs');
  });

  // Non-plain middles reach this surface INSIDE the score string, not through
  // the fallback: matchScoreStr already returns "X"/"(E)"/"(DH)" for these
  // payloads, so scoreStr is truthy and the vsched-item__score span wins. These
  // cases therefore pin the score-string path (that formatIpponsScore keeps
  // surfacing the marks), NOT the fallback. The fallback's own handling of the
  // same marks is pinned separately below.
  it.each([
    ['hikiwake tie', { decision: 'hikiwake' }, 'X'],
    ['encho', { encho: { periodCount: 1 } }, '(E)'],
    ['daihyosen', { decision: 'daihyosen' }, '(DH)'],
  ])('completed %s renders its mark in the centre, never a dash', (_label, extra, want) => {
    const mid = centreText({ ...base, status: 'completed', ...extra });
    expect(mid).toContain(want);
    expect(mid).not.toContain('-');
  });

  // Directly pin the FALLBACK branch for the non-"vs" marks. Only
  // matchScoreStr is stubbed (to the empty string the bye case produces);
  // boutMiddle stays REAL, so this asserts what the new call actually returns
  // rather than what a stub was told to say. Without this, every non-"vs"
  // assertion above would still pass with the fallback entirely broken.
  describe('fallback branch with an empty score string', () => {
    let realMatchScoreStr;

    beforeEach(() => {
      realMatchScoreStr = global.window.matchScoreStr;
      global.window.matchScoreStr = () => '';
    });

    afterEach(() => {
      global.window.matchScoreStr = realMatchScoreStr;
    });

    it.each([
      ['hikiwake tie', { decision: 'hikiwake' }, 'X'],
      ['encho', { encho: { periodCount: 1 } }, '(E)'],
      ['daihyosen', { decision: 'daihyosen' }, '(DH)'],
      ['no decision', {}, 'vs'],
    ])('completed %s falls back to its boutMiddle mark, never a dash', (_label, extra, want) => {
      const mid = centreText({ ...base, status: 'completed', ...extra });
      expect(mid).toBe(want);
      expect(mid).not.toContain('-');
    });
  });

  // Degradation: a SCHEDULED row must render without bracket.jsx loaded at all.
  // This branch renders for scheduled matches, whereas matchScoreStr above is
  // gated to completed/running, so an unguarded call would have made bracket.jsx
  // a hard dependency of an up-next list that previously needed no helper. Two
  // fixtures (viewer.test.jsx, vsched_winner_cue.test.jsx) had to grow a
  // boutMiddle stub before this guard existed; both are back to their original
  // form, and this test is what keeps them that way.
  describe('without bracket.jsx loaded', () => {
    let saved;

    beforeEach(() => {
      saved = { boutMiddle: global.window.boutMiddle, matchScoreStr: global.window.matchScoreStr };
      delete global.window.boutMiddle;
      delete global.window.matchScoreStr;
    });

    afterEach(() => {
      global.window.boutMiddle = saved.boutMiddle;
      global.window.matchScoreStr = saved.matchScoreStr;
    });

    it('a scheduled row still renders "vs" instead of throwing', () => {
      expect(() => centreText({ ...base, status: 'scheduled' })).not.toThrow();
      expect(centreText({ ...base, status: 'scheduled' })).toBe('vs');
    });
  });
});

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

// The three special middles (label, payload, expected mark), shared by the
// score-string-path block and the fallback block so the two stay in sync.
const MARK_CASES = [
  ['hikiwake tie', { decision: 'hikiwake' }, 'X'],
  ['encho', { encho: { periodCount: 1 } }, '(E)'],
  ['daihyosen', { decision: 'daihyosen' }, '(DH)'],
];

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

  // THE bug case and its near variants: completed, no score cells →
  // matchScoreStr returns "" and the middle must read "vs", never "-".
  it.each([
    ['no extra fields', {}],
    ['a winner', { winner: { id: 'a', name: 'Ryu' } }],
    ['decision "fought"', { decision: 'fought' }],
  ])('completed match with no recorded score and %s renders "vs", not "-"', (_label, extra) => {
    expect(centreText({ ...base, status: 'completed', ...extra })).toBe('vs');
  });

  it('running match with no score yet still renders "vs"', () => {
    expect(centreText({ ...base, status: 'running' })).toBe('vs');
  });

  it('scheduled match renders "vs"', () => {
    expect(centreText({ ...base, status: 'scheduled' })).toBe('vs');
  });

  // These payloads make matchScoreStr truthy ("X"/"(E)"/"(DH)" ride inside the
  // score string), so they pin the score-string span, NOT the fallback — the
  // fallback's own handling of the same marks is pinned below.
  it.each(MARK_CASES)('completed %s renders its mark in the centre, never a dash', (_label, extra, want) => {
    const mid = centreText({ ...base, status: 'completed', ...extra });
    expect(mid).toContain(want);
    expect(mid).not.toContain('-');
  });

  // Directly pin the FALLBACK branch. Only matchScoreStr is stubbed (to the
  // empty string the bye case produces); boutMiddle stays REAL, so this asserts
  // what the new call actually returns rather than what a stub was told to say.
  // Without this, every non-"vs" assertion above would still pass with the
  // fallback entirely broken. No restore needed: the outer beforeEach re-imports
  // bracket.jsx per test, re-publishing the real window.matchScoreStr.
  describe('fallback branch with an empty score string', () => {
    beforeEach(() => {
      global.window.matchScoreStr = () => '';
    });

    it.each([
      ...MARK_CASES,
      ['no decision', {}, 'vs'],
    ])('completed %s falls back to its boutMiddle mark, never a dash', (_label, extra, want) => {
      expect(centreText({ ...base, status: 'completed', ...extra })).toBe(want);
    });
  });
});

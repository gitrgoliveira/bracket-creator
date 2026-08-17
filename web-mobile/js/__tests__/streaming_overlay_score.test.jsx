import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { collectText } from './helpers/vdom.js';

// The OBS lower-third read "0 - 0" through a scored KNOCKOUT bout while the
// board beside it read "K D vs M".
//
// Cause: a bracket match persists its running score as the formatted
// scoreA/scoreB STRING, not the ippon arrays a pool match carries (the same
// split that made the rollback snapshot lose its score, engine-side). The
// overlay read only the arrays. IndividualScore already had the
// ipponsFromScore fallback; this surface did not.
//
// It also showed a DIGIT for an empty side, which the score-cell contract
// forbids outright — a cell with no points reads "-", never "0", so a kendo
// score never reads "M - 0".
describe('StreamingOverlay individual-match score', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'matchMiddleMark', 'Term', 'ipponsFromScore'];

  // A bracket match as the court endpoint serves it: score STRINGS, no arrays.
  const bracketMatch = (scoreA, scoreB) => ({
    id: 'm-r1-0', court: 'A', status: 'running',
    sideA: { name: 'Alice' }, sideB: { name: 'Bob' },
    scoreA, scoreB, subResults: [],
  });
  const compWith = (match) => [{
    id: 'c1', name: 'Cup', kind: 'individual', teamSize: 0, withZekkenName: false,
    poolMatches: [], bracket: { rounds: [[match]] },
  }];

  const scoreText = (tree) => {
    // The score line renders as `{ipponsB} - {ipponsA}`.
    const all = collectText(tree);
    return all;
  };

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.matchMiddleMark = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    // The real one (bracket.jsx): "MK" -> ["M","K"].
    global.window.ipponsFromScore = (s) => String(s || '').split('').filter(Boolean);
    vi.resetModules();
    ({ StreamingOverlay } = await import('../streaming_overlay.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    STUBBED.forEach(k => {
      if (savedGlobals[k]?.had) global.window[k] = savedGlobals[k].val;
      else delete global.window[k];
    });
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('reads a bracket match score from the score string, not just the arrays', () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(bracketMatch('M', 'KD')),
    });
    const text = scoreText(tree);
    expect(text).toContain('KD');
    expect(text).toContain('M');
    // The bug's signature: a scored bout reading as zeroes.
    expect(text).not.toContain('0 - 0');
  });

  it('shows a dash, never a digit, for a side with no points', () => {
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(bracketMatch('', 'M')),
    });
    const text = scoreText(tree);
    expect(text).toContain('M');
    // "M - 0" is the reading the cell contract forbids.
    expect(text).not.toMatch(/\b0\b/);
  });

  it('still prefers the ippon arrays when the match carries them (pool matches)', () => {
    const m = bracketMatch('SHOULD-NOT-BE-USED', 'ALSO-NOT');
    m.ipponsA = ['D'];
    m.ipponsB = ['T'];
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(m),
    });
    const text = scoreText(tree);
    expect(text).toContain('D');
    expect(text).toContain('T');
    expect(text).not.toContain('SHOULD-NOT-BE-USED');
  });

  it('drops the unfilled-slot placeholder rather than printing it', () => {
    const m = bracketMatch('', '');
    m.ipponsA = ['M', '•'];
    m.ipponsB = ['•'];
    const tree = runtime.mount(StreamingOverlay, {
      court: 'A', position: 'bottom', competitions: compWith(m),
    });
    const text = scoreText(tree);
    expect(text).not.toContain('•');
    expect(text).toContain('M');
  });
});

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

function collectText(node) {
  if (node == null) return '';
  if (typeof node === 'string' || typeof node === 'number') return String(node);
  if (Array.isArray(node)) return node.map(collectText).join('');
  if (node.children) return collectText(node.children);
  if (node.props?.children) return collectText(node.props.children);
  return '';
}
function findAll(node, pred, acc = []) {
  if (node == null || typeof node !== 'object') return acc;
  if (Array.isArray(node)) { node.forEach(k => findAll(k, pred, acc)); return acc; }
  if (pred(node)) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, pred, acc));
  return acc;
}

// The broadcast overlay must signal a daihyosen bout on court. isPoolDaihyosenBout
// (the "…-DH-N" rep-bout id) drives the DAIHYOSEN chip regardless of how the bout
// renders, so an individual-kind fixture exercises the detection without the team
// lineup fetch / QR path.
describe('StreamingOverlay DH signal', () => {
  const realReact = global.React;
  let runtime;
  let StreamingOverlay;
  const savedGlobals = {};
  const STUBBED = ['isHikiwake', 'decisionSuffix', 'Term'];

  const runningMatch = (id) => ({
    id, court: 'A', status: 'running',
    sideA: { name: 'Alpha' }, sideB: { name: 'Charlie' },
    ipponsA: [], ipponsB: [], subResults: [],
  });
  const comp = (matchId) => [{
    id: 'c1', name: 'Pool Cup', kind: 'individual', teamSize: 0, withZekkenName: false,
    poolMatches: [runningMatch(matchId)], bracket: { rounds: [] },
  }];

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] } : { had: false };
    });
    global.window.isHikiwake = () => false;
    global.window.decisionSuffix = () => '';
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
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

  it('shows a DAIHYOSEN chip when the running match is a pool daihyosen rep bout', () => {
    const tree = runtime.mount(StreamingOverlay, { court: 'A', position: 'bottom', competitions: comp('Pool A-DH-0') });
    const chips = findAll(tree, n => n.props && n.props['data-testid'] === 'overlay-dh-badge');
    expect(chips).toHaveLength(1);
    expect(collectText(chips[0])).toContain('DAIHYOSEN');
  });

  it('shows NO DH chip for a regular running pool match', () => {
    const tree = runtime.mount(StreamingOverlay, { court: 'A', position: 'bottom', competitions: comp('Pool A-1') });
    const chips = findAll(tree, n => n.props && n.props['data-testid'] === 'overlay-dh-badge');
    expect(chips).toHaveLength(0);
  });

  it('shows NO DH chip when nothing is running on the court', () => {
    const tree = runtime.mount(StreamingOverlay, { court: 'B', position: 'bottom', competitions: comp('Pool A-DH-0') });
    const chips = findAll(tree, n => n.props && n.props['data-testid'] === 'overlay-dh-badge');
    expect(chips).toHaveLength(0);
  });
});

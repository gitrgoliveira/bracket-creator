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

describe('PoolsViewer league standings label (mp-mnwu)', () => {
  const realReact = global.React;
  let runtime;
  let PoolsViewer;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact'];

  const leagueComp = (status) => ({
    format: 'league',
    kind: 'individual',
    teamSize: 0,
    status,
  });

  const pool = { poolName: 'League', players: [] };
  const baseStandings = { League: [{ player: { name: 'P1' }, wins: 1, losses: 0, draws: 0, pointsWon: 2, pointsLost: 0 }] };
  const tweaks = { showDojo: false };

  function makeMatches(completed, scheduled) {
    return [
      ...Array.from({ length: completed }, (_, i) => ({ id: `League-c${i}`, status: 'completed' })),
      ...Array.from({ length: scheduled }, (_, i) => ({ id: `League-s${i}`, status: 'scheduled' })),
    ];
  }

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    STUBBED.forEach(k => {
      savedGlobals[k] = Object.prototype.hasOwnProperty.call(global.window, k)
        ? { had: true, val: global.window[k] }
        : { had: false };
    });
    global.window.Term = function Term(props) { return { type: 'span', props, children: props?.children }; };
    global.window.isHikiwake = () => false;
    global.window.formatIpponsScore = () => '';
    global.window.ipponsFromScore = () => [];
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    vi.resetModules();
    ({ PoolsViewer } = await import('../viewer.jsx'));
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

  it('shows "Standings" when league competition is in progress', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings: baseStandings,
      poolMatches: makeMatches(2, 4),
      tweaks,
      competition: leagueComp('pools'),
    });
    const text = collectText(tree);
    expect(text).toContain('Standings');
    expect(text).not.toContain('Final standings');
  });

  it('shows "Final standings" when all league matches are complete', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings: baseStandings,
      poolMatches: makeMatches(6, 0),
      tweaks,
      competition: leagueComp('completed'),
    });
    const text = collectText(tree);
    expect(text).toContain('Final standings');
  });

  it('shows pool name (not "Standings") for non-league competitions', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [{ poolName: 'PoolA', players: [] }],
      standings: { PoolA: [] },
      poolMatches: makeMatches(2, 3),
      tweaks,
      competition: { format: 'mixed', kind: 'individual', teamSize: 0, status: 'pools' },
    });
    const text = collectText(tree);
    expect(text).toContain('PoolA');
    expect(text).not.toContain('Final standings');
    expect(text).not.toContain('Standings');
  });
});

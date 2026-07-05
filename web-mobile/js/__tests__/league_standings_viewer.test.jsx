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

describe('LeagueStandingsViewer (mp-dunx)', () => {
  const realReact = global.React;
  let runtime;
  let LeagueStandingsViewer;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'teamIVScore', 'matchScoreStr', 'matchStateCell', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact', 'API', 'LoadingSpinner', 'EmptyState'];

  // Standings: fetched rank-ordered from the API. P3 rank 1, P1 rank 2, P4 rank 3, P2 rank 4.
  const mockStandings = [
    { player: { name: 'P3', dojo: 'Dojo3', id: 'id3' }, rank: 1, wins: 3, losses: 0, draws: 0, ipponsGiven: 6, ipponsTaken: 0, isOverridden: false },
    { player: { name: 'P1', dojo: 'Dojo1', id: 'id1' }, rank: 2, wins: 2, losses: 1, draws: 0, ipponsGiven: 4, ipponsTaken: 2, isOverridden: false },
    { player: { name: 'P4', dojo: 'Dojo4', id: 'id4' }, rank: 3, wins: 1, losses: 2, draws: 0, ipponsGiven: 2, ipponsTaken: 4, isOverridden: false },
    { player: { name: 'P2', dojo: 'Dojo2', id: 'id2' }, rank: 4, wins: 0, losses: 3, draws: 0, ipponsGiven: 0, ipponsTaken: 6, isOverridden: false },
  ];

  const comp = { id: 'league-1', format: 'league', kind: 'individual', teamSize: 0, status: 'pools' };
  const tweaks = { showDojo: false };

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
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    global.window.matchStateCell = (m, ippB, ippA) =>
      m?.status === 'completed' ? (global.window.matchScoreStr(m, ippB, ippA) || '-')
      : m?.status === 'running' ? 'vs' : '–';
    global.window.ipponsFromScore = () => [];
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    global.window.LoadingSpinner = function LoadingSpinner({ text }) { return { type: 'div', props: { className: 'loading' }, children: text }; };
    global.window.EmptyState = function EmptyState({ title }) { return { type: 'div', props: {}, children: title }; };
    global.window.API = {
      leagueStandings: vi.fn().mockResolvedValue(mockStandings),
    };
    vi.resetModules();
    ({ LeagueStandingsViewer } = await import('../viewer.jsx'));
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

  it('calls window.API.leagueStandings with the comp id', async () => {
    // The reactive runtime runs useEffect synchronously during mount.
    // The promise resolves async, but we can verify the call was made.
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledWith('league-1');
  });

  // NOTE: the reactive_react useEffect runs synchronously, but the fetch promise
  // resolves asynchronously. The initial render shows loading state.
  // We flush the promise queue with a tick to let the standings arrive,
  // then trigger a rerender via the state setter captured in the reactive runtime.
  //
  // Since the reactive runtime's setState triggers synchronous rerenders,
  // we await a microtask tick so the mockResolvedValue settles:
  it('renders rows in fetched (rank) order after standings resolve', async () => {
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    // Flush the resolved promise so the setState callbacks fire and rerender.
    await Promise.resolve();
    // updateProps re-renders with the same props but preserves hook slots
    // (including the standings state set by the resolved fetch).
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const text = collectText(finalTree);
    // P3 (rank 1) must appear before P1 (rank 2)
    const p3Idx = text.indexOf('P3');
    const p1Idx = text.indexOf('P1');
    expect(p3Idx).toBeGreaterThanOrEqual(0);
    expect(p1Idx).toBeGreaterThanOrEqual(0);
    expect(p3Idx).toBeLessThan(p1Idx);
  });

  it('shows ranks 1,2,3,4 in the # column after standings resolve', async () => {
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    await Promise.resolve();
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const drawPosCells = findAll(finalTree, n => {
      const cls = n.props?.className;
      return n.type === 'td' && typeof cls === 'string' && cls.includes('pool-standings__draw-pos');
    });
    expect(drawPosCells.length).toBeGreaterThan(0);
    const texts = drawPosCells.map(c => collectText(c));
    expect(texts).toEqual(['1', '2', '3', '4']);
  });

  it('shows no rank-badge spans (ranks live in # column only)', async () => {
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    await Promise.resolve();
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const badges = findAll(finalTree, n => {
      const cls = n.props?.className;
      return typeof cls === 'string' && cls.split(' ').includes('rank-badge');
    });
    expect(badges).toHaveLength(0);
  });

  it('caption text is "Ranked by standings"', async () => {
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    await Promise.resolve();
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const text = collectText(finalTree);
    expect(text).toContain('Ranked by standings');
  });

  // A scored bout changes poolMatchesSig, which re-triggers the fetch effect
  // (setLoading(true)). Before the fix, the render gate was `if (loading)`,
  // which blanked the table with a full-page spinner on every background
  // refresh - much more visible here than on SwissStandingsViewer since the
  // per-bout signature fires far more often than Swiss's per-round one.
  it('keeps showing standings during a background re-fetch, no spinner flicker', async () => {
    let resolveSecond;
    global.window.API.leagueStandings = vi.fn()
      .mockResolvedValueOnce(mockStandings)
      .mockImplementationOnce(() => new Promise(resolve => { resolveSecond = resolve; }));

    const firstMatches = [{ id: 'Pool A-1', status: 'scheduled' }];
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: firstMatches, tweaks });
    await Promise.resolve();
    let tree = runtime.updateProps({ competition: comp, poolMatches: firstMatches, tweaks });
    expect(collectText(tree)).toContain('P3'); // initial load resolved: table visible

    // A different match signature (the bout completed) re-triggers the fetch
    // while it's still pending (the second mock never resolves yet).
    const secondMatches = [{ id: 'Pool A-1', status: 'completed' }];
    tree = runtime.updateProps({ competition: comp, poolMatches: secondMatches, tweaks });
    const text = collectText(tree);
    expect(text).not.toContain('Loading standings');
    expect(text).toContain('P3'); // last-known standings stay on screen

    resolveSecond(mockStandings);
    await Promise.resolve();
    runtime.updateProps({ competition: comp, poolMatches: secondMatches, tweaks });
  });
});

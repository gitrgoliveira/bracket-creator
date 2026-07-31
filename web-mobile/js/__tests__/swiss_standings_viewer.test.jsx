import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// mp-8pba: SwissStandingsViewer must dispatch its columns on the scoring
// paradigm (engi flags / team sub-bouts), mirroring the pool/league standings
// table, so the tie-break data the backend now tallies is actually visible.
// Before the fix it hardcoded the individual W/L/D/PW/PL shape, which rendered
// engi flags and team IV/PW as zeros.

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

describe('SwissStandingsViewer (mp-8pba)', () => {
  const realReact = global.React;
  let runtime;
  let SwissStandingsViewer;
  const savedGlobals = {};
  const STUBBED = ['API', 'LoadingSpinner', 'engiPairParts'];

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
    global.window.LoadingSpinner = function LoadingSpinner({ text }) { return { type: 'div', props: { className: 'loading' }, children: text }; };
    global.window.engiPairParts = (name) => {
      const i = (name || '').indexOf(' - ');
      return i < 0 ? [name, ''] : [name.slice(0, i), name.slice(i + 3)];
    };
    vi.resetModules();
    ({ SwissStandingsViewer } = await import('../viewer_standings.jsx'));
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

  describe('engi swiss', () => {
    const comp = { id: 'engi-1', format: 'swiss', kind: 'individual', engi: true, teamSize: 0, status: 'pools', swissRounds: 2, swissCurrentRound: 1 };
    // Charlie 2W/8 flags, Bravo 1W/7 flags, Alpha 1W/5 flags: Bravo outranks
    // Alpha purely on flags despite equal wins.
    const standings = [
      { player: { name: 'Charlie One - Charlie Two', dojo: 'Chiba', id: 'c' }, rank: 1, wins: 2, flags: 8, scoreSummary: 'W:2 Flags:8' },
      { player: { name: 'Bravo One - Bravo Two', dojo: 'Bara', id: 'b' }, rank: 2, wins: 1, flags: 7, scoreSummary: 'W:1 Flags:7' },
      { player: { name: 'Alpha One - Alpha Two', dojo: 'Aoi', id: 'a' }, rank: 3, wins: 1, flags: 5, scoreSummary: 'W:1 Flags:5' },
    ];

    beforeEach(() => {
      global.window.API = { swissStandings: vi.fn().mockResolvedValue(standings) };
    });

    it('renders the accumulated flags value in a cell', async () => {
      runtime.mount(SwissStandingsViewer, { competition: comp, poolMatches: [], tweaks });
      await Promise.resolve();
      const tree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
      const numCells = findAll(tree, n => n.type === 'td' && n.props?.className === 'num').map(collectText);
      // Charlie's 8 flags and Bravo's 7 flags must appear as rendered cells.
      expect(numCells).toContain('8');
      expect(numCells).toContain('7');
    });

    it('keeps rank order (Bravo above Alpha on flags tie-break)', async () => {
      runtime.mount(SwissStandingsViewer, { competition: comp, poolMatches: [], tweaks });
      await Promise.resolve();
      const tree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
      const text = collectText(tree);
      expect(text.indexOf('Bravo')).toBeLessThan(text.indexOf('Alpha'));
    });

    it('caption ranks by flags, not PW', async () => {
      runtime.mount(SwissStandingsViewer, { competition: comp, poolMatches: [], tweaks });
      await Promise.resolve();
      const tree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
      expect(collectText(tree)).toContain('total flags');
    });
  });

  describe('team swiss', () => {
    const comp = { id: 'team-1', format: 'swiss', kind: 'team', engi: false, teamSize: 5, status: 'pools', swissRounds: 2, swissCurrentRound: 1 };
    const standings = [
      { player: { name: 'TeamC', dojo: 'Chiba', id: 'c' }, rank: 1, wins: 1, losses: 0, draws: 0, individualWins: 5, individualLosses: 0, individualDraws: 0, pointsWon: 10, pointsLost: 0 },
      { player: { name: 'TeamA', dojo: 'Aoi', id: 'a' }, rank: 2, wins: 1, losses: 0, draws: 0, individualWins: 3, individualLosses: 2, individualDraws: 0, pointsWon: 6, pointsLost: 4 },
    ];

    beforeEach(() => {
      global.window.API = { swissStandings: vi.fn().mockResolvedValue(standings) };
    });

    it('renders individual victories and points-won from the team fields', async () => {
      runtime.mount(SwissStandingsViewer, { competition: comp, poolMatches: [], tweaks });
      await Promise.resolve();
      const tree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
      const numCells = findAll(tree, n => n.type === 'td' && n.props?.className === 'num').map(collectText);
      // TeamC: IV 5, PW 10 must be present (not zeros from the old ippon path).
      expect(numCells).toContain('5');
      expect(numCells).toContain('10');
    });

    it('caption ranks by team wins → IV → PW', async () => {
      runtime.mount(SwissStandingsViewer, { competition: comp, poolMatches: [], tweaks });
      await Promise.resolve();
      const tree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
      const text = collectText(tree);
      expect(text).toContain('IV');
      expect(text).toContain('PW');
    });
  });
});

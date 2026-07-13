// T4 regression suite: kachinuki (winner-stays) rendering in TeamScoreboard
// and BoutSubRow (match_scoreboard.jsx).
//
// Kachinuki invariants:
//   - rowCount = number of recorded bouts, minimum 1 (no teamSize padding)
//   - kachinuki prop is threaded to each BoutSubRow
//   - Name resolution is server-bout-first; lineup is bootstrap-only (index 0)
//   - Later rows (index > 0) never show lineup position names
//
// TeamScoreboard is not fully expanded by makeReactive; boutRows() collects
// the BoutSubRow VNODE PROPS to verify count and the kachinuki flag.
// BoutSubRow is mounted directly to verify name resolution.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { boutRows, findInTree, collectText } from './helpers/vdom.js';

const realReact = global.React;

// Shared setup/teardown for both describe blocks: same globals, same module,
// only the exported symbol consumed differs per block.
async function setupSuite() {
  const runtime = makeReactive();
  global.React = runtime.React;
  global.window = global.window || {};
  global.window.isHikiwake = vi.fn((t) => t === 'hikiwake');
  global.window.ipponsFromScore = vi.fn(() => []);
  vi.resetModules();
  const mod = await import('../match_scoreboard.jsx');
  return { runtime, mod };
}

function teardownSuite(runtime) {
  runtime.unmount();
  global.React = realReact;
  delete global.window.isHikiwake;
  delete global.window.ipponsFromScore;
  vi.restoreAllMocks();
  vi.resetModules();
}

// Minimal lineup shape for teamSize=5. pickFromLineup uses POS_LABELS_5
// position keys (lowercase: "senpo", "jiho", ...) for a 5-person team.
const linupA = { positions: { senpo: 'AkaSenpo', jiho: 'AkaJiho', chuken: 'AkaChuken', fukusho: 'AkaFukusho', taisho: 'AkaTaisho' } };
const linupB = { positions: { senpo: 'ShiroSenpo', jiho: 'ShiroJiho', chuken: 'ShiroChuken', fukusho: 'ShiroFukusho', taisho: 'ShiroTaisho' } };

describe('T4 kachinuki: TeamScoreboard row count', () => {
  let runtime, TeamScoreboard;

  beforeEach(async () => {
    let mod;
    ({ runtime, mod } = await setupSuite());
    TeamScoreboard = mod.TeamScoreboard;
  });
  afterEach(() => teardownSuite(runtime));

  it('kachinuki + 0 bouts: exactly 1 row (senpo bootstrap), kachinuki=true passed to BoutSubRow', () => {
    const tree = runtime.mount(TeamScoreboard, {
      subResults: [], lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true, shiroName: 'ShiroTeam', akaName: 'AkaTeam',
    });
    const rows = boutRows(tree);
    expect(rows).toHaveLength(1);
    expect(rows[0].kachinuki).toBe(true);
    expect(rows[0].index).toBe(0);
  });

  it('kachinuki + 3 bouts: 3 rows, not padded to teamSize', () => {
    const subResults = [
      { position: 1, sideA: 'WinnerA1', sideB: 'WinnerB1', ipponsB: ['M'], ipponsA: [] },
      { position: 2, sideA: 'WinnerA2', sideB: 'WinnerB2', ipponsB: [], ipponsA: ['K'] },
      { position: 3, sideA: 'WinnerA3', sideB: 'WinnerB3', ipponsB: ['D'], ipponsA: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, {
      subResults, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true, shiroName: 'ShiroTeam', akaName: 'AkaTeam',
    });
    const rows = boutRows(tree);
    expect(rows).toHaveLength(3);
    expect(rows.every(r => r.kachinuki === true)).toBe(true);
  });

  it('kachinuki + 7 bouts (teamSize=5): 7 rows (exceeds teamSize)', () => {
    const subResults = Array.from({ length: 7 }, (_, i) => ({
      position: i + 1,
      sideA: `A${i}`, sideB: `B${i}`,
      ipponsA: [], ipponsB: [],
    }));
    const tree = runtime.mount(TeamScoreboard, {
      subResults, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true, shiroName: 'ShiroTeam', akaName: 'AkaTeam',
    });
    expect(boutRows(tree)).toHaveLength(7);
  });

  it('fixed (kachinuki=false) + 3 bouts: padded to teamSize=5', () => {
    const subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: 2, ipponsB: [], ipponsA: ['K'] },
      { position: 3, ipponsB: [], ipponsA: [] },
    ];
    const tree = runtime.mount(TeamScoreboard, {
      subResults, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: false, shiroName: 'ShiroTeam', akaName: 'AkaTeam',
    });
    expect(boutRows(tree)).toHaveLength(5);
  });
});

describe('T4 kachinuki: BoutSubRow name resolution', () => {
  let runtime, BoutSubRow;

  beforeEach(async () => {
    let mod;
    ({ runtime, mod } = await setupSuite());
    BoutSubRow = mod.BoutSubRow;
  });
  afterEach(() => teardownSuite(runtime));

  it('kachinuki bout index 0 with server names: uses server names, not lineup', () => {
    // Even at index 0, if the server recorded per-bout sides, use those.
    const sub = { position: 1, sideA: 'ServerAka', sideB: 'ServerShiro', ipponsB: ['M'], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true,
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(collectText(shiro)).toBe('ServerShiro');
    expect(collectText(aka)).toBe('ServerAka');
  });

  it('kachinuki bout index 0 with empty sub sides: falls back to lineup senpo (bootstrap)', () => {
    const sub = { position: 1, ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 0, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true,
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(collectText(shiro)).toBe('ShiroSenpo');
    expect(collectText(aka)).toBe('AkaSenpo');
  });

  it('kachinuki bout index 1 with winner-stays names: uses server names (not lineup position 1)', () => {
    // Bout 2: the winner of bout 1 stayed on, so sideB is NOT the Jiho player.
    const sub = { position: 2, sideA: 'WinnerAka', sideB: 'WinnerShiro', ipponsB: [], ipponsA: ['M'] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 1, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true,
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    // Must NOT be the lineup Jiho player ("ShiroJiho" / "AkaJiho").
    expect(collectText(shiro)).toBe('WinnerShiro');
    expect(collectText(aka)).toBe('WinnerAka');
    expect(collectText(shiro)).not.toBe('ShiroJiho');
    expect(collectText(aka)).not.toBe('AkaJiho');
  });

  it('kachinuki bout index 1 with empty sub sides: falls back to bout number, not lineup', () => {
    // Index > 0 with empty sub: must show the bout number, NEVER lineup position name.
    const sub = { position: 2, ipponsB: [], ipponsA: [] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 1, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: true,
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    // sub.position=2 so boutNum="#2"; must not show lineup Jiho names.
    expect(collectText(shiro)).toBe('#2');
    expect(collectText(aka)).toBe('#2');
    expect(collectText(shiro)).not.toBe('ShiroJiho');
  });

  it('fixed (kachinuki=false) index 1: lineup position takes precedence over server names', () => {
    const sub = { position: 2, sideA: 'ServerAka2', sideB: 'ServerShiro2', ipponsB: [], ipponsA: ['K'] };
    const tree = runtime.mount(BoutSubRow, {
      sub, index: 1, lineupA: linupA, lineupB: linupB, teamSize: 5,
      kachinuki: false,
    });
    const shiro = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const aka   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    // Fixed order: lineup Jiho player wins over server name.
    expect(collectText(shiro)).toBe('ShiroJiho');
    expect(collectText(aka)).toBe('AkaJiho');
  });
});

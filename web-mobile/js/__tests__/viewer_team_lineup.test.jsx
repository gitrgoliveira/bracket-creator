// mp-13y: Per-match lineup rendering — unit tests for shared logic and
// view-layer components added in the lineup-rendering PR.
//
// Covers:
//   - lineup_resolver.jsx: resolveLineupTeamId, pickFromLineup, resolveMatchLineup
//   - match_scoreboard.jsx: boutHansokuMark, BoutSubRow canonical layout
//     (hansoku marking moved here from display.jsx's removed boutHansokuMarkD)
//   - display.jsx: findCurrentBoutIndex

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';

// ──────────────────────────────────────────────────
// lineup_resolver.jsx: pure utility functions
// ──────────────────────────────────────────────────

describe('lineup_resolver: resolveLineupTeamId', () => {
  let resolveLineupTeamId;

  beforeEach(async () => {
    vi.resetModules();
    ({ resolveLineupTeamId } = await import('../lineup_resolver.jsx'));
  });

  it('returns the UUID when sideKey matches player id', () => {
    const players = [{ id: 'uuid-1', name: 'Team Alpha' }];
    expect(resolveLineupTeamId('uuid-1', players)).toBe('uuid-1');
  });

  it('maps a name-keyed sideKey to the participant UUID', () => {
    const players = [{ id: 'uuid-99', name: 'Team Alpha' }];
    // api_serializers sets match side id = team name; this resolves it to UUID.
    expect(resolveLineupTeamId('Team Alpha', players)).toBe('uuid-99');
  });

  it('falls back to sideKey when no matching player found', () => {
    const players = [{ id: 'uuid-1', name: 'Team Alpha' }];
    expect(resolveLineupTeamId('Team Beta', players)).toBe('Team Beta');
  });

  it('returns empty string for falsy sideKey', () => {
    const players = [{ id: 'uuid-1', name: 'Team Alpha' }];
    expect(resolveLineupTeamId('', players)).toBe('');
    expect(resolveLineupTeamId(null, players)).toBe('');
    expect(resolveLineupTeamId(undefined, players)).toBe('');
  });

  it('handles PascalCase id/name fields (Go API normalisation)', () => {
    const players = [{ ID: 'uuid-2', Name: 'Team Beta' }];
    expect(resolveLineupTeamId('Team Beta', players)).toBe('uuid-2');
  });

  it('handles non-array / null players gracefully', () => {
    expect(resolveLineupTeamId('any-key', null)).toBe('any-key');
    expect(resolveLineupTeamId('any-key', undefined)).toBe('any-key');
  });
});

describe('lineup_resolver: pickFromLineup', () => {
  let pickFromLineup;

  beforeEach(async () => {
    vi.resetModules();
    ({ pickFromLineup } = await import('../lineup_resolver.jsx'));
  });

  const lineup5 = {
    positions: {
      senpo: 'Alice', jiho: 'Bob', chuken: 'Carol',
      fukusho: 'Dave', taisho: 'Eve',
    },
  };

  const lineupNumeric = {
    positions: { '1': 'Player1', '2': 'Player2', '3': 'Player3' },
  };

  it('picks by named key for 5-person lineups (senpo, jiho, chuken, fukusho, taisho)', () => {
    expect(pickFromLineup(lineup5, 0, 5)).toBe('Alice');  // senpo
    expect(pickFromLineup(lineup5, 1, 5)).toBe('Bob');    // jiho
    expect(pickFromLineup(lineup5, 2, 5)).toBe('Carol');  // chuken
    expect(pickFromLineup(lineup5, 3, 5)).toBe('Dave');   // fukusho
    expect(pickFromLineup(lineup5, 4, 5)).toBe('Eve');    // taisho
  });

  it('falls through to numeric key when teamSize != 5', () => {
    const mixed = { positions: { '1': 'Alpha', '2': 'Beta', senpo: 'Wrong' } };
    expect(pickFromLineup(mixed, 0, 3)).toBe('Alpha');
    expect(pickFromLineup(mixed, 1, 3)).toBe('Beta');
  });

  it('picks by numeric key for non-5-person lineups', () => {
    expect(pickFromLineup(lineupNumeric, 0, 3)).toBe('Player1');
    expect(pickFromLineup(lineupNumeric, 2, 3)).toBe('Player3');
  });

  it('returns empty string when lineup is null', () => {
    expect(pickFromLineup(null, 0, 5)).toBe('');
    expect(pickFromLineup(undefined, 0, 5)).toBe('');
  });

  it('returns empty string when positions key is missing', () => {
    expect(pickFromLineup({}, 0, 5)).toBe('');
    expect(pickFromLineup({ positions: {} }, 0, 5)).toBe('');
  });

  it('returns empty string for out-of-range indices', () => {
    expect(pickFromLineup(lineup5, 5, 5)).toBe('');
    expect(pickFromLineup(lineup5, -1, 5)).toBe('');
  });
});

describe('lineup_resolver: resolveMatchLineup', () => {
  let resolveMatchLineup;

  beforeEach(async () => {
    vi.resetModules();
    ({ resolveMatchLineup } = await import('../lineup_resolver.jsx'));
  });

  it('returns match-specific lineup when fetchMatchLineup succeeds', async () => {
    const matchLineup = { positions: { senpo: 'A' } };
    const fetchers = {
      fetchMatchLineup: vi.fn().mockResolvedValue(matchLineup),
      fetchTeamLineup: vi.fn().mockResolvedValue({ positions: { senpo: 'B' } }),
    };
    const result = await resolveMatchLineup('c1', 't1', 'm1', 0, fetchers);
    expect(result).toBe(matchLineup);
    expect(fetchers.fetchTeamLineup).not.toHaveBeenCalled();
  });

  it('falls through to round-based lineup when fetchMatchLineup returns null', async () => {
    const roundLineup = { positions: { '1': 'C' } };
    const fetchers = {
      fetchMatchLineup: vi.fn().mockResolvedValue(null),
      fetchTeamLineup: vi.fn().mockResolvedValue(roundLineup),
    };
    const result = await resolveMatchLineup('c1', 't1', 'm1', 2, fetchers);
    expect(result).toBe(roundLineup);
    expect(fetchers.fetchTeamLineup).toHaveBeenCalledWith('c1', 't1', 2);
  });

  it('returns null when both fetchers throw/reject', async () => {
    const fetchers = {
      fetchMatchLineup: vi.fn().mockRejectedValue(new Error('network')),
      fetchTeamLineup: vi.fn().mockRejectedValue(new Error('404')),
    };
    const result = await resolveMatchLineup('c1', 't1', 'm1', 0, fetchers);
    expect(result).toBeNull();
  });
});

// ──────────────────────────────────────────────────
// viewer.jsx: boutHansokuMark
// ──────────────────────────────────────────────────

describe('viewer: boutHansokuMark', () => {
  let boutHansokuMark;

  beforeEach(async () => {
    vi.resetModules();
    // viewer.jsx uses global React; provide a minimal stub so the module loads.
    global.React = { createElement: vi.fn(() => ({})), useState: vi.fn(() => [null, vi.fn()]), useEffect: vi.fn(), useMemo: (f) => f(), useRef: vi.fn(() => ({ current: null })), useCallback: (f) => f, memo: (c) => c };
    ({ boutHansokuMark } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('returns "▲" for an odd foul count (outstanding hansoku)', () => {
    expect(boutHansokuMark(1)).toBe('▲');
    expect(boutHansokuMark(3)).toBe('▲');
    expect(boutHansokuMark(5)).toBe('▲');
  });

  it('returns "" for an even foul count (converted to ippon)', () => {
    expect(boutHansokuMark(0)).toBe('');
    expect(boutHansokuMark(2)).toBe('');
    expect(boutHansokuMark(4)).toBe('');
  });

  it('returns "" for null/undefined (no fouls recorded)', () => {
    expect(boutHansokuMark(null)).toBe('');
    expect(boutHansokuMark(undefined)).toBe('');
  });
});

// ──────────────────────────────────────────────────
// viewer.jsx: BoutSubRow canonical layout
// ──────────────────────────────────────────────────

describe('viewer: BoutSubRow canonical layout (mp-13y)', () => {
  const realReact = global.React;
  let runtime;
  let BoutSubRow;

  function collectText(node) {
    if (node == null) return '';
    if (typeof node === 'string' || typeof node === 'number') return String(node);
    if (Array.isArray(node)) return node.map(collectText).join('');
    if (node.children) return collectText(node.children);
    if (node.props?.children) return collectText(node.props.children);
    return '';
  }

  function findInTree(node, predicate) {
    if (!node || typeof node !== 'object') return null;
    if (Array.isArray(node)) {
      for (const k of node) {
        const found = findInTree(k, predicate);
        if (found) return found;
      }
      return null;
    }
    if (predicate(node)) return node;
    const kids = node.children || node.props?.children || [];
    for (const k of [].concat(kids)) {
      const found = findInTree(k, predicate);
      if (found) return found;
    }
    return null;
  }

  beforeEach(async () => {
    runtime = makeReactive();
    global.React = runtime.React;
    global.window = global.window || {};
    global.window.isHikiwake = vi.fn(() => false);
    vi.resetModules();
    ({ BoutSubRow } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    runtime.unmount();
    global.React = realReact;
    delete global.window.isHikiwake;
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('renders the DH row with the sub-row-dh testid (position -1)', () => {
    // The "DAIHYOSEN" banner is rendered by TeamScoreboard, not the bout row;
    // the rep-bout row itself carries data-testid="sub-row-dh".
    const sub = { position: -1, ipponsA: ['K'], ipponsB: [], decidedByHantei: false };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 3, isDH: true });
    expect(collectText(tree)).not.toContain('Match -1');
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-dh')).toBeTruthy();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-hantei')).toBeNull();
  });

  it('renders the "Ht" hantei marker when decidedByHantei=true', () => {
    const sub = { position: -1, ipponsA: ['K'], ipponsB: [], decidedByHantei: true };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 3, isDH: true });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'sub-row-hantei')).toBeTruthy();
  });

  it('falls back to bout number when no lineup provided (individual index + 1)', () => {
    const sub = { position: 2, ipponsA: ['M'], ipponsB: [], decidedByHantei: false };
    const tree = runtime.mount(BoutSubRow, { sub, index: 1, lineupA: null, lineupB: null, teamSize: 3, isDH: false });
    const text = collectText(tree);
    // No lineup: boutNum = String(position) = "2".
    expect(text).toContain('2');
  });

  it('injects lineup names when lineup is provided (5-person named keys)', () => {
    const lineupA = { positions: { senpo: 'AkaPlayer', jiho: 'AkaTwo', chuken: 'AkaThree', fukusho: 'AkaFour', taisho: 'AkaFive' } };
    const lineupB = { positions: { senpo: 'ShiroPlayer', jiho: 'ShiroTwo', chuken: 'ShiroThree', fukusho: 'ShiroFour', taisho: 'ShiroFive' } };
    const sub = { position: 1, ipponsA: ['M'], ipponsB: [], decidedByHantei: false };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA, lineupB, teamSize: 5, isDH: false });
    const text = collectText(tree);
    // Shiro (left, sideB/lineupB) senpo = ShiroPlayer; Aka (right, sideA/lineupA) senpo = AkaPlayer.
    expect(text).toContain('ShiroPlayer');
    expect(text).toContain('AkaPlayer');
    // data-testids for name cells.
    const shiroName = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-shiro-name');
    const akaName   = findInTree(tree, n => n?.props?.['data-testid'] === 'sub-aka-name');
    expect(shiroName).toBeTruthy();
    expect(akaName).toBeTruthy();
  });

  it('renders hansoku ▲ in centre marks for odd foul counts', () => {
    const sub = { position: 1, ipponsA: ['M'], ipponsB: [], hansokuA: 1, hansokuB: 0, decidedByHantei: false };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 3, isDH: false });
    const text = collectText(tree);
    expect(text).toContain('▲');
    const foulA = findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-a');
    expect(foulA).toBeTruthy();
  });

  it('does not render hansoku ▲ when foul count is even', () => {
    const sub = { position: 1, ipponsA: ['M'], ipponsB: [], hansokuA: 2, hansokuB: 2, decidedByHantei: false };
    const tree = runtime.mount(BoutSubRow, { sub, index: 0, lineupA: null, lineupB: null, teamSize: 3, isDH: false });
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-a')).toBeNull();
    expect(findInTree(tree, n => n?.props?.['data-testid'] === 'foul-mark-b')).toBeNull();
  });
});

// ──────────────────────────────────────────────────
// display.jsx: boutHansokuMarkD + findCurrentBoutIndex
// ──────────────────────────────────────────────────

describe('shared: boutHansokuMark (mp-13y)', () => {
  let boutHansokuMark;

  beforeEach(async () => {
    vi.resetModules();
    ({ boutHansokuMark } = await import('../match_scoreboard.jsx'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('returns "▲" for an odd foul count', () => {
    expect(boutHansokuMark(1)).toBe('▲');
    expect(boutHansokuMark(3)).toBe('▲');
  });

  it('returns "" for an even foul count or null', () => {
    expect(boutHansokuMark(0)).toBe('');
    expect(boutHansokuMark(2)).toBe('');
    expect(boutHansokuMark(null)).toBe('');
    expect(boutHansokuMark(undefined)).toBe('');
  });
});

describe('display: findCurrentBoutIndex (mp-13y)', () => {
  let findCurrentBoutIndex;

  beforeEach(async () => {
    vi.resetModules();
    global.React = { createElement: vi.fn(() => ({})), useState: vi.fn(() => [null, vi.fn()]), useEffect: vi.fn(), useMemo: (f) => f(), useRef: vi.fn(() => ({ current: null })), useCallback: (f) => f, memo: (c) => c };
    global.window = global.window || {};
    global.window.renderQR = vi.fn();
    ({ findCurrentBoutIndex } = await import('../display.jsx'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  // findCurrentBoutIndex returns the index of the FIRST UNSCORED regular bout
  // (the bout currently in progress). When all regular bouts are complete, it
  // returns regularSubs.length (signals DH/done). Aligns with TeamScoreboard's
  // currentIdx logic.

  it('returns 0 when no subs have been scored yet', () => {
    expect(findCurrentBoutIndex([{ position: 1 }, { position: 2 }])).toBe(0);
  });

  it('returns the first unscored bout after scored ones', () => {
    const subs = [
      { position: 1, ipponsA: ['M'], ipponsB: [] },
      { position: 2, ipponsA: [], ipponsB: ['D'] },
      { position: 3, ipponsA: [], ipponsB: [] },
    ];
    // Subs 0,1 scored; sub 2 is the first unscored → returns 2.
    expect(findCurrentBoutIndex(subs)).toBe(2);
  });

  it('returns the first unscored bout after hansoku data', () => {
    const subs = [
      { position: 1, ipponsA: ['M'], ipponsB: [], hansokuA: 0 },
      { position: 2, ipponsA: [], ipponsB: [], hansokuB: 1 },
      { position: 3, ipponsA: [], ipponsB: [] },
    ];
    // Subs 0,1 scored; sub 2 is the first unscored → returns 2.
    expect(findCurrentBoutIndex(subs)).toBe(2);
  });

  it('returns regularSubs.length when all subs have been scored', () => {
    const subs = [
      { position: 1, ipponsA: ['M'], ipponsB: [] },
      { position: 2, ipponsA: [], ipponsB: ['K'] },
    ];
    // Both scored; all done → returns 2 (subs.length).
    expect(findCurrentBoutIndex(subs)).toBe(2);
  });

  it('skips "•" placeholder ippons (not real data)', () => {
    const subs = [
      { position: 1, ipponsA: ['•'], ipponsB: ['•'] },
      { position: 2, ipponsA: [], ipponsB: [] },
    ];
    // "•" is excluded; no real data, so returns 0.
    expect(findCurrentBoutIndex(subs)).toBe(0);
  });

  it('returns 0 for empty / null input', () => {
    expect(findCurrentBoutIndex([])).toBe(0);
    expect(findCurrentBoutIndex(null)).toBe(0);
    expect(findCurrentBoutIndex(undefined)).toBe(0);
  });
});

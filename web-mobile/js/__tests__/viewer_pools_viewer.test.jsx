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

// Walk a vnode tree and collect all vnodes matching a predicate.
function findAll(node, pred, acc = []) {
  if (node == null || typeof node !== 'object') return acc;
  if (Array.isArray(node)) { node.forEach(k => findAll(k, pred, acc)); return acc; }
  if (pred(node)) acc.push(node);
  const kids = node.children || node.props?.children || [];
  [].concat(kids).forEach(k => findAll(k, pred, acc));
  return acc;
}

// ------------------------------------------------------------------
// mp-938b: Draw-order standings table and rank badges
// ------------------------------------------------------------------
describe('PoolsViewer draw-order standings (mp-938b)', () => {
  const realReact = global.React;
  let runtime;
  let PoolsViewer;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'teamIVScore', 'matchScoreStr', 'matchStateCell', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact'];

  const baseComp = { kind: 'individual', teamSize: 0, format: 'mixed', poolWinners: 2 };
  const tweaks = { showDojo: false };

  // Pool of 4: draw order is P1, P2, P3, P4.
  // Standings (rank order): P3 (1st), P1 (2nd), P4 (3rd), P2 (4th).
  // With poolWinners=2, advancing rows should be P3 and P1, NOT the first two draw rows.
  const pool = {
    poolName: 'Pool A',
    players: [
      { name: 'P1', dojo: 'Dojo1' },
      { name: 'P2', dojo: 'Dojo2' },
      { name: 'P3', dojo: 'Dojo3' },
      { name: 'P4', dojo: 'Dojo4' },
    ],
  };

  // standings.player must include dojo to match the real API contract and the
  // composite id-or-name||dojo lookup key used by PoolsViewer.
  const standings = {
    'Pool A': [
      { player: { name: 'P3', dojo: 'Dojo3' }, wins: 3, losses: 0, draws: 0, ipponsGiven: 6, ipponsTaken: 0 },
      { player: { name: 'P1', dojo: 'Dojo1' }, wins: 2, losses: 1, draws: 0, ipponsGiven: 4, ipponsTaken: 2 },
      { player: { name: 'P4', dojo: 'Dojo4' }, wins: 1, losses: 2, draws: 0, ipponsGiven: 2, ipponsTaken: 4 },
      { player: { name: 'P2', dojo: 'Dojo2' }, wins: 0, losses: 3, draws: 0, ipponsGiven: 0, ipponsTaken: 6 },
    ],
  };

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
    // Mirror the real matchStateCell (bracket.jsx): completed → score||"—",
    // running → "vs", scheduled → "–".
    global.window.matchStateCell = (m, ippB, ippA) =>
      m?.status === 'completed' ? (global.window.matchScoreStr(m, ippB, ippA) || '—')
      : m?.status === 'running' ? 'vs' : '–';
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

  it('renders rows in draw-position order (pool.players order), not rank order', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    const text = collectText(tree);
    // P1 should appear before P3 in the output (draw position 1 < draw position 3)
    const p1Idx = text.indexOf('P1');
    const p3Idx = text.indexOf('P3');
    expect(p1Idx).toBeGreaterThanOrEqual(0);
    expect(p3Idx).toBeGreaterThanOrEqual(0);
    expect(p1Idx).toBeLessThan(p3Idx);
  });

  it('applies advancing class by looked-up rank, not row index', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    // Find all tr vnodes with advancing class
    const advancingRows = findAll(tree, n => {
      const cls = n.props?.className;
      return n.type === 'tr' && typeof cls === 'string' && cls.includes('advancing');
    });
    // P3 (draw pos 3, rank 1) and P1 (draw pos 1, rank 2) should advance.
    // P2 (draw pos 2, rank 4) and P4 (draw pos 4, rank 3) should not.
    expect(advancingRows).toHaveLength(2);
    const advancingText = advancingRows.map(r => collectText(r)).join(' ');
    expect(advancingText).toContain('P3');
    expect(advancingText).toContain('P1');
    expect(advancingText).not.toContain('P2');
    expect(advancingText).not.toContain('P4');
  });

  it('shows rank badges (1st/2nd/3rd/4th) for each player', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    const text = collectText(tree);
    expect(text).toContain('1st');
    expect(text).toContain('2nd');
    expect(text).toContain('3rd');
    expect(text).toContain('4th');
  });

  it('advancing rank badges have rank-badge--adv class, others do not', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    // Collect all rank-badge spans
    const advBadges = findAll(tree, n => {
      const cls = n.props?.className;
      return typeof cls === 'string' && cls.includes('rank-badge--adv');
    });
    const allBadges = findAll(tree, n => {
      const cls = n.props?.className;
      return typeof cls === 'string' && cls.includes('rank-badge') && !cls.includes('rank-badge--adv');
    });
    // 2 advancing (P3=1st, P1=2nd), 2 non-advancing (P4=3rd, P2=4th)
    expect(advBadges).toHaveLength(2);
    const advText = advBadges.map(b => collectText(b)).join(' ');
    expect(advText).toContain('1st');
    expect(advText).toContain('2nd');
    // Non-advancing badges contain 3rd and 4th
    expect(allBadges).toHaveLength(2);
    const otherText = allBadges.map(b => collectText(b)).join(' ');
    expect(otherText).toContain('3rd');
    expect(otherText).toContain('4th');
  });

  it('shows draw position in # column (1..N), not rank', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    // Assert on pool-standings__draw-pos cells specifically, not substring
    // matching (which could match digits in player names like "P1", "P2").
    const drawPosCells = findAll(tree, n => {
      const cls = n.props?.className;
      return n.type === 'td' && typeof cls === 'string' && cls.includes('pool-standings__draw-pos');
    });
    expect(drawPosCells).toHaveLength(4);
    const drawPosTexts = drawPosCells.map(cell => collectText(cell));
    // Pool players are in draw order P1→P2→P3→P4, so positions are 1,2,3,4
    expect(drawPosTexts).toEqual(['1', '2', '3', '4']);
  });

  it('renders numbered match list when matches are present', () => {
    const matches = [
      { id: 'Pool A-0', sideA: { name: 'P1' }, sideB: { name: 'P2' }, status: 'scheduled' },
      { id: 'Pool A-1', sideA: { name: 'P3' }, sideB: { name: 'P4' }, status: 'scheduled' },
    ];
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings,
      poolMatches: matches,
      tweaks,
      competition: baseComp,
    });
    const text = collectText(tree);
    expect(text).toContain('Matches');
    // Assert on the PoolNumberedMatchRow component vnodes and their num props.
    // The test framework doesn't call sub-components, so we check the vnode
    // props rather than the rendered DOM output (which would be opaque here).
    const matchRows = findAll(tree, n => {
      // React.memo wraps the inner function; displayName may be on the wrapper
      // directly or on n.type.type (the inner fn). Handle both.
      const displayName = n.type?.displayName || n.type?.type?.displayName;
      return displayName === 'PoolNumberedMatchRow';
    });
    expect(matchRows).toHaveLength(2);
    // num={idx+1} so first match gets num=1, second gets num=2
    expect(matchRows.map(r => r.props?.num)).toEqual([1, 2]);
  });

  it('renders pre-match (no standings) rows in draw order with dashes', () => {
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool],
      standings: {},
      poolMatches: [],
      tweaks,
      competition: baseComp,
    });
    const text = collectText(tree);
    // All 4 players appear
    expect(text).toContain('P1');
    expect(text).toContain('P2');
    expect(text).toContain('P3');
    expect(text).toContain('P4');
    // No rank badges when no standings
    expect(text).not.toContain('1st');
    expect(text).not.toContain('2nd');
  });

  // A pool whose standings exist but are all 0-0-0 (no bout decided yet) must
  // NOT show rank badges or the green "advancing" highlight — the rank is just
  // the seed/draw fallback and asserting placement/qualification pre-scoring is
  // misleading. Provisional ranks appear only once a result exists.
  it('suppresses rank badges + advancing highlight until the pool has a result', () => {
    const zeroStandings = {
      'Pool A': [
        { player: { name: 'P1', dojo: 'Dojo1' }, rank: 1, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
        { player: { name: 'P2', dojo: 'Dojo2' }, rank: 2, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
        { player: { name: 'P3', dojo: 'Dojo3' }, rank: 3, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
        { player: { name: 'P4', dojo: 'Dojo4' }, rank: 4, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
      ],
    };
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool], standings: zeroStandings, poolMatches: [], tweaks, competition: baseComp,
    });
    const advancingRows = findAll(tree, n => {
      const cls = n.props?.className;
      return n.type === 'tr' && typeof cls === 'string' && cls.includes('advancing');
    });
    const rankBadges = findAll(tree, n => {
      const cls = n.props?.className;
      return typeof cls === 'string' && cls.split(' ').includes('rank-badge');
    });
    expect(advancingRows).toHaveLength(0);
    expect(rankBadges).toHaveLength(0);
    // The standings rows themselves still render (with their 0 stats).
    const text = collectText(tree);
    expect(text).toContain('P1');
    expect(text).toContain('P4');
  });

  // Counterpart: once a single bout is decided (any non-zero W/L/D in
  // standings), provisional ranks + advancing highlight come back.
  it('shows rank badges + advancing once any standing has a result', () => {
    const partialStandings = {
      'Pool A': [
        { player: { name: 'P1', dojo: 'Dojo1' }, rank: 1, wins: 1, losses: 0, draws: 0, ipponsGiven: 1, ipponsTaken: 0 },
        { player: { name: 'P2', dojo: 'Dojo2' }, rank: 2, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
        { player: { name: 'P3', dojo: 'Dojo3' }, rank: 3, wins: 0, losses: 1, draws: 0, ipponsGiven: 0, ipponsTaken: 1 },
        { player: { name: 'P4', dojo: 'Dojo4' }, rank: 4, wins: 0, losses: 0, draws: 0, ipponsGiven: 0, ipponsTaken: 0 },
      ],
    };
    const tree = runtime.mount(PoolsViewer, {
      pools: [pool], standings: partialStandings, poolMatches: [], tweaks, competition: baseComp,
    });
    const rankBadges = findAll(tree, n => {
      const cls = n.props?.className;
      return typeof cls === 'string' && cls.split(' ').includes('rank-badge');
    });
    const advancingRows = findAll(tree, n => {
      const cls = n.props?.className;
      return n.type === 'tr' && typeof cls === 'string' && cls.includes('advancing');
    });
    expect(rankBadges).toHaveLength(4);
    expect(advancingRows).toHaveLength(2);
  });
});

// ------------------------------------------------------------------

describe('PoolsViewer league standings label (mp-mnwu)', () => {
  const realReact = global.React;
  let runtime;
  let PoolsViewer;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'teamIVScore', 'matchScoreStr', 'matchStateCell', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact'];

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
    global.window.teamIVScore = () => null;
    global.window.matchScoreStr = (m, ippB, ippA) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(ippB, ippA, m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    // Mirror the real matchStateCell (bracket.jsx): completed → score||"—",
    // running → "vs", scheduled → "–".
    global.window.matchStateCell = (m, ippB, ippA) =>
      m?.status === 'completed' ? (global.window.matchScoreStr(m, ippB, ippA) || '—')
      : m?.status === 'running' ? 'vs' : '–';
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

// ------------------------------------------------------------------
// mp-o4xl: PoolNumberedMatchRow shows IV aggregate for team matches
// ------------------------------------------------------------------
describe('PoolNumberedMatchRow team IV score (mp-o4xl)', () => {
  const realReact = global.React;
  let runtime;
  let PoolNumberedMatchRow;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'teamIVScore', 'matchScoreStr', 'matchStateCell', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact'];

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
    // Mirror the real matchStateCell (bracket.jsx): completed → score||"—",
    // running → "vs", scheduled → "–".
    global.window.matchStateCell = (m, ippB, ippA) =>
      m?.status === 'completed' ? (global.window.matchScoreStr(m, ippB, ippA) || '—')
      : m?.status === 'running' ? 'vs' : '–';
    global.window.ipponsFromScore = () => [];
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    vi.resetModules();
    ({ PoolNumberedMatchRow } = await import('../viewer.jsx'));
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

  it('renders the IV string for a completed team match with subResults (not "—")', () => {
    // Stub teamIVScore to return the IV aggregate (as it would for a real team match)
    global.window.teamIVScore = () => '2–1';

    const m = {
      id: 'Pool A-0',
      sideA: { name: 'TeamA' },
      sideB: { name: 'TeamB' },
      status: 'completed',
      subResults: [
        { position: 0, winner: 'TeamB', sideA: 'P1', sideB: 'P2' },
        { position: 1, winner: 'TeamA', sideA: 'P3', sideB: 'P4' },
        { position: 2, winner: 'TeamB', sideA: 'P5', sideB: 'P6' },
      ],
    };

    const tree = runtime.mount(PoolNumberedMatchRow, { m, num: 1 });
    const text = collectText(tree);
    // Should show "2–1" from teamIVScore, not "—" (the fallback for empty score)
    expect(text).toContain('2–1');
    expect(text).not.toContain('—');
  });

  it('renders "—" for a completed individual match with no subResults when formatIpponsScore returns empty', () => {
    // teamIVScore returns null for individual matches (no subResults)
    global.window.teamIVScore = () => null;
    global.window.formatIpponsScore = () => ''; // also empty

    const m = {
      id: 'Pool A-1',
      sideA: { name: 'Alice' },
      sideB: { name: 'Bob' },
      status: 'completed',
      ipponsA: [],
      ipponsB: [],
    };

    const tree = runtime.mount(PoolNumberedMatchRow, { m, num: 2 });
    const text = collectText(tree);
    expect(text).toContain('—');
  });

  it('falls back to formatIpponsScore when teamIVScore returns null', () => {
    global.window.teamIVScore = () => null;
    global.window.formatIpponsScore = () => 'M–·';

    const m = {
      id: 'Pool B-0',
      sideA: { name: 'Alice' },
      sideB: { name: 'Bob' },
      status: 'completed',
      ipponsA: ['M'],
      ipponsB: [],
    };

    const tree = runtime.mount(PoolNumberedMatchRow, { m, num: 3 });
    const text = collectText(tree);
    expect(text).toContain('M–·');
  });

  // Proposal 1: the visible Shiro/Aka text badge was dropped (the hatched/red
  // side fill encodes side); the label survives only as an sr-only span so
  // assistive tech still announces it, and names no longer ellipsis-clip.
  it('encodes side via sr-only labels, not visible cbadge text badges', () => {
    const m = {
      id: 'Pool A-0',
      sideA: { name: 'Aaron Thompson' },
      sideB: { name: 'Jane Austen' },
      status: 'scheduled',
    };

    const tree = runtime.mount(PoolNumberedMatchRow, { m, num: 1 });

    const hasClass = (cls) => (n) =>
      typeof n.props?.className === 'string' && n.props.className.split(' ').includes(cls);

    // No visible chip badges remain.
    expect(findAll(tree, hasClass('cbadge'))).toHaveLength(0);

    // Both side labels survive as sr-only spans, in Shiro/Aka order.
    const srLabels = findAll(tree, hasClass('sr-only')).map(collectText);
    expect(srLabels).toEqual(['Shiro: ', 'Aka: ']);

    // Names render in full (no truncation markup couples to the assertion;
    // both competitor names are present in the tree text).
    const text = collectText(tree);
    expect(text).toContain('Jane Austen');
    expect(text).toContain('Aaron Thompson');
  });
});

// ------------------------------------------------------------------
// mp-8rc9 Phase 1: poolLabel format-aware label helper
// ------------------------------------------------------------------
describe('poolLabel — format-aware phase label (mp-8rc9)', () => {
  let poolLabel;

  beforeEach(async () => {
    vi.resetModules();
    ({ poolLabel } = await import('../viewer.jsx'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('returns "League table" for a league-format match', () => {
    const m = { compFormat: 'league', poolName: 'Pool A', compName: 'City League' };
    expect(poolLabel(m)).toBe('League table');
  });

  it('returns poolName for a mixed-format match', () => {
    const m = { compFormat: 'mixed', poolName: 'Pool B', compName: 'Open Cup' };
    expect(poolLabel(m)).toBe('Pool B');
  });

  it('returns poolName for a playoffs-format match', () => {
    const m = { compFormat: 'playoffs', poolName: 'Pool A', compName: 'Open Cup' };
    expect(poolLabel(m)).toBe('Pool A');
  });

  it('returns poolName when compFormat is absent (legacy match)', () => {
    const m = { poolName: 'Pool C', compName: 'Old Cup' };
    expect(poolLabel(m)).toBe('Pool C');
  });

  it('never returns the competition name for league (would be redundant in eyebrow)', () => {
    const m = { compFormat: 'league', poolName: 'Pool A', compName: 'City League' };
    expect(poolLabel(m)).not.toBe('City League');
  });

  // The core terminology rule: a LEAGUE surface must never render the word
  // "Pool" — leagues are a single table shown as "League table". Pool/mixed
  // surfaces DO render the pool name. poolLabel is the eyebrow path; this
  // guards it against the "Pool" word leaking back in for leagues.
  it('league poolLabel never contains the word "Pool" even when poolName is "Pool A"', () => {
    const m = { compFormat: 'league', poolName: 'Pool A', compName: 'City League' };
    expect(poolLabel(m)).toBe('League table');
    expect(poolLabel(m)).not.toMatch(/pool/i);
  });

  it('pool/mixed poolLabel DOES surface the pool name (the word "Pool" is correct here)', () => {
    expect(poolLabel({ compFormat: 'mixed', poolName: 'Pool B' })).toMatch(/pool/i);
    expect(poolLabel({ compFormat: 'playoffs', poolName: 'Pool A' })).toMatch(/pool/i);
  });
});

// ------------------------------------------------------------------
// mp-8rc9: leagueAwareLabel — single source of truth for the
// league-vs-pool heading. Every admin/viewer surface routes its
// pool-heading through this helper (poolLabel, poolDisplayLabel, the
// shiaijo phase label, the scoring eyebrow). Pinning the boundary here
// guards ALL of them: a league must read "League table" and must NEVER
// contain "Pool"; a pool/mixed surface must surface the pool name.
// ------------------------------------------------------------------
describe('leagueAwareLabel — league/pool terminology boundary (mp-8rc9)', () => {
  let leagueAwareLabel;

  beforeEach(async () => {
    vi.resetModules();
    ({ leagueAwareLabel } = await import('../viewer_utils.jsx'));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.resetModules();
  });

  it('league → "League table", never "Pool" (even with a pool-shaped poolName)', () => {
    expect(leagueAwareLabel('league', 'Pool A')).toBe('League table');
    expect(leagueAwareLabel('league', 'Pool A')).not.toMatch(/pool/i);
  });

  it('league ignores the non-league fallback and still never says "Pool"', () => {
    // admin_shiaijo passes "Pool" as the non-league fallback; a league must
    // not fall through to it.
    expect(leagueAwareLabel('league', '', 'Pool')).toBe('League table');
    expect(leagueAwareLabel('league', '', 'Pool')).not.toMatch(/pool/i);
  });

  it('mixed/pools → the pool name (the word "Pool" belongs here)', () => {
    expect(leagueAwareLabel('mixed', 'Pool A')).toBe('Pool A');
    expect(leagueAwareLabel('playoffs', 'Pool B')).toBe('Pool B');
    expect(leagueAwareLabel('swiss', 'Pool C')).toBe('Pool C');
  });

  it('non-league with empty poolName uses the supplied fallback', () => {
    expect(leagueAwareLabel('mixed', '', 'Pool')).toBe('Pool');
    expect(leagueAwareLabel('mixed', '')).toBe('');
  });
});

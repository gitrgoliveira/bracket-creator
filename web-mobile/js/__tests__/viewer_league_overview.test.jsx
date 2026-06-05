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

describe('ViewerOverview league standings (mp-ldnr)', () => {
  const realReact = global.React;
  let runtime;
  let ViewerOverview;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'ipponsFromScore', 'queueLabel', 'queueLabelCompact', 'teamIVScore', 'matchScoreStr'];

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
    global.window.ipponsFromScore = () => [];
    global.window.queueLabel = () => '';
    global.window.queueLabelCompact = () => null;
    vi.resetModules();
    ({ ViewerOverview } = await import('../viewer.jsx'));
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

  const baseProps = {
    myPlayer: null,
    myUpcoming: null,
    currentMatch: null,
    liveMatches: [],
    upcomingMatches: [],
    recentMatches: [],
    tweaks: {},
  };

  function makeStandings(count) {
    const rows = [];
    for (let i = 0; i < count; i++) {
      rows.push({
        player: { id: `p${i}`, name: `Player ${i + 1}`, dojo: `Dojo ${i + 1}` },
        wins: count - i,
        losses: i,
        draws: 0,
        ipponsGiven: (count - i) * 2,
        ipponsTaken: i,
      });
    }
    return rows;
  }

  function makeTeamStandings(count) {
    const rows = [];
    for (let i = 0; i < count; i++) {
      rows.push({
        player: { id: `t${i}`, name: `Team ${i + 1}`, dojo: '' },
        wins: count - i,
        losses: i,
        draws: 0,
        individualWins: (count - i) * 3,
        individualLosses: i * 2,
        pointsWon: (count - i) * 5,
        pointsLost: i * 3,
      });
    }
    return rows;
  }

  const leagueComp = (status = 'pools') => ({
    format: 'league',
    status,
    kind: 'individual',
    teamSize: 0,
  });

  const teamLeagueComp = (status = 'pools') => ({
    format: 'league',
    status,
    kind: 'team',
    teamSize: 5,
  });

  const pools = [{ poolName: 'League', players: [] }];

  function completedMatches(n) {
    return Array.from({ length: n }, (_, i) => ({ id: `League-${i}`, status: 'completed' }));
  }

  function mixedMatches(completed, scheduled) {
    return [
      ...Array.from({ length: completed }, (_, i) => ({ id: `League-${i}`, status: 'completed' })),
      ...Array.from({ length: scheduled }, (_, i) => ({ id: `League-s${i}`, status: 'scheduled' })),
    ];
  }

  it('running league shows standings header and top-5 rows', () => {
    const standings = { League: makeStandings(8) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('pools'),
      standings,
      pools,
      poolMatches: mixedMatches(3, 5),
    });
    const text = collectText(tree);
    expect(text).toContain('Standings');
    expect(text).toContain('Player 1');
    expect(text).toContain('Player 5');
    expect(text).not.toContain('Player 6');
    expect(text).toContain('Showing top 5 of 8');
    expect(text).not.toContain('Final standings');
  });

  it('completed league shows winner badge and final standings', () => {
    const standings = { League: makeStandings(4) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('completed'),
      standings,
      pools,
      poolMatches: completedMatches(6),
    });
    const text = collectText(tree);
    expect(text).toContain('Final standings');
    expect(text).toContain('Player 4');
    expect(text).not.toContain('Showing top 5');
    const winnerBadge = findInTree(tree, n =>
      typeof n?.type === 'function' && n.props?.testId === 'league-overview-winner'
    );
    expect(winnerBadge).not.toBeNull();
    expect(winnerBadge.props.name).toBe('Player 1');
  });

  it('non-league format does NOT show standings section', () => {
    const standings = { PoolA: makeStandings(4) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: { format: 'mixed', status: 'pools', kind: 'individual', teamSize: 0 },
      standings,
      pools: [{ poolName: 'PoolA', players: [] }],
      poolMatches: mixedMatches(2, 4),
    });
    const text = collectText(tree);
    expect(text).not.toContain('Standings');
    expect(text).not.toContain('Player 1');
  });

  it('view-full-standings button calls onSwitchTab("pools")', () => {
    const onSwitchTab = vi.fn();
    const standings = { League: makeStandings(3) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('pools'),
      standings,
      pools,
      poolMatches: mixedMatches(1, 2),
      onSwitchTab,
    });
    const btn = findInTree(tree, n =>
      n?.props?.['data-testid'] === 'league-overview-view-all'
    );
    expect(btn).not.toBeNull();
    btn.props.onClick();
    expect(onSwitchTab).toHaveBeenCalledWith('pools');
  });

  it('team league shows correct column headers (W/L/T/IV/IL/PW/PL)', () => {
    const standings = { League: makeTeamStandings(3) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: teamLeagueComp('pools'),
      standings,
      pools,
      poolMatches: mixedMatches(1, 2),
    });
    const text = collectText(tree);
    expect(text).toContain('Team');
    expect(text).toContain('IV');
    expect(text).toContain('IL');
    expect(text).toContain('Team 1');
  });

  it('running league with no matches scored shows no standings section', () => {
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('pools'),
      standings: { League: [] },
      pools,
      poolMatches: [],
    });
    const text = collectText(tree);
    expect(text).not.toContain('Standings');
  });

  it('running league does NOT show winner badge', () => {
    const standings = { League: makeStandings(4) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('pools'),
      standings,
      pools,
      poolMatches: mixedMatches(3, 3),
    });
    const winnerBadge = findInTree(tree, n =>
      typeof n?.type === 'function' && n.props?.testId === 'league-overview-winner'
    );
    expect(winnerBadge).toBeNull();
  });

  it('completed league shows match progress as N/N', () => {
    const standings = { League: makeStandings(3) };
    const tree = runtime.mount(ViewerOverview, {
      ...baseProps,
      c: leagueComp('completed'),
      standings,
      pools,
      poolMatches: completedMatches(3),
    });
    const text = collectText(tree);
    expect(text).toContain('3/3 matches');
  });
});

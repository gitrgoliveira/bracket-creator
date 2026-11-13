import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { makeReactive } from './helpers/reactive_react.js';
import { findAll, collectText, expandNamed } from './helpers/vdom.js';


describe('LeagueStandingsViewer (mp-dunx)', () => {
  const realReact = global.React;
  let runtime;
  let LeagueStandingsViewer;
  const savedGlobals = {};
  const STUBBED = ['Term', 'isHikiwake', 'formatIpponsScore', 'teamIVScore', 'matchScoreStr', 'matchStateCell', 'queueLabel', 'queueLabelCompact', 'API', 'LoadingSpinner', 'EmptyState'];

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
    global.window.matchScoreStr = (m) =>
      (global.window.teamIVScore(m)) ||
      global.window.formatIpponsScore(m?.ipponsB || [], m?.ipponsA || [], m?.score, m?.decision, m?.encho, m?.decidedByHantei);
    global.window.matchStateCell = (m) =>
      m?.status === 'completed' ? (global.window.matchScoreStr(m) || 'vs') : 'vs';
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
    const text = collectText(finalTree, expandNamed('NumberedName'));
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
    const texts = drawPosCells.map(c => collectText(c, expandNamed('NumberedName')));
    expect(texts).toEqual(['1', '2', '3', '4']);
  });

  // bc-rvfx: collectText's NumberedName-expansion branch above (added when
  // the outer-side number rendering landed) was never exercised by a
  // fixture that carries a `number` -- mockStandings never sets one, so the
  // branch was pinned by nothing. This is a rank-ordered, single-column
  // standings row (no left/right pairing), so the number sits BEFORE the
  // name on every row (operator ruling 2026-09-14, bc-dnst).
  it('places the competitor number before the name in the standings row', async () => {
    const numberedStandings = [
      { player: { name: 'P3', dojo: 'Dojo3', id: 'id3', number: 'K11' }, rank: 1, wins: 3, losses: 0, draws: 0, ipponsGiven: 6, ipponsTaken: 0, isOverridden: false },
    ];
    global.window.API.leagueStandings = vi.fn().mockResolvedValue(numberedStandings);
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    await Promise.resolve();
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const nameCell = findAll(finalTree, n => typeof n?.props?.className === 'string' && n.props.className.includes('pool__player-name'))[0];
    expect(nameCell).toBeTruthy();
    const text = collectText(nameCell, expandNamed('NumberedName'));
    expect(text).toContain('K11');
    expect(text).toContain('P3');
    expect(text.indexOf('K11')).toBeLessThan(text.indexOf('P3'));
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
    const text = collectText(finalTree, expandNamed('NumberedName'));
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
    expect(collectText(tree, expandNamed('NumberedName'))).toContain('P3'); // initial load resolved: table visible

    // A different match signature (the bout completed) re-triggers the fetch
    // while it's still pending (the second mock never resolves yet).
    const secondMatches = [{ id: 'Pool A-1', status: 'completed' }];
    tree = runtime.updateProps({ competition: comp, poolMatches: secondMatches, tweaks });
    const text = collectText(tree, expandNamed('NumberedName'));
    expect(text).not.toContain('Loading standings');
    expect(text).toContain('P3'); // last-known standings stay on screen

    resolveSecond(mockStandings);
    await Promise.resolve();
    runtime.updateProps({ competition: comp, poolMatches: secondMatches, tweaks });
  });

  // N7: a correction that keeps the encounter's winner still moves the table
  // (IV/PW, ippons), so the table must be fetched again.
  it('refetches after a correction that keeps the encounter winner', () => {
    const team = { ...comp, kind: 'team', teamSize: 2, status: 'completed' };
    const bout = (winner, ipponsA) => ({ winner, ipponsA, ipponsB: [], decision: '' });
    const encounter = (subs) => [{ id: 'Pool A-0', status: 'completed', winner: 'Alpha', subResults: subs }];
    const before = encounter([bout('Alpha', ['M']), bout('Alpha', ['K'])]);
    runtime.mount(LeagueStandingsViewer, { competition: team, poolMatches: before, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(1);

    // Same winner, one bout corrected from a win to a draw: IV and PW change.
    const corrected = encounter([bout('Alpha', ['M', 'K']), { winner: '', ipponsA: [], ipponsB: [], decision: 'hikiwake' }]);
    runtime.updateProps({ competition: team, poolMatches: corrected, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(2);
  });

  it('does not refetch on a live mark of a match not yet finished', () => {
    const running = (ipponsA) => [{ id: 'Pool A-0', status: 'running', winner: '', ipponsA, ipponsB: [] }];
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: running([]), tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(1);
    runtime.updateProps({ competition: comp, poolMatches: running(['M']), tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(1);
  });

  // N2: Accept shared ranks and its Undo change the competition's status, not
  // a match, yet change the ranks the table reads, so the table is fetched
  // again on a status change with the same matches.
  it('refetches when the competition status changes with the matches unchanged (Accept, Undo)', () => {
    const played = [{ id: 'Pool A-0', status: 'completed', winner: 'P3', ipponsA: ['M'], ipponsB: [] }];
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: played, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(1);

    // Accept: the league finishes, no match moves.
    runtime.updateProps({ competition: { ...comp, status: 'completed' }, poolMatches: played, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(2);

    // Undo: back to match play, still no match moves.
    runtime.updateProps({ competition: { ...comp, status: 'pools' }, poolMatches: played, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(3);

    // A re-render with nothing changed fetches nothing.
    runtime.updateProps({ competition: { ...comp, status: 'pools' }, poolMatches: played, tweaks });
    expect(global.window.API.leagueStandings).toHaveBeenCalledTimes(3);
  });

  // N2a/N2b: the table prints the place the server gives, so an accepted
  // shared 1st reads 1, 1, 3 and a tie below the band 4, 4.
  it('shows a shared place as the server ranks it', async () => {
    const shared = mockStandings.map((s, i) => ({ ...s, rank: [1, 1, 3, 3][i] }));
    global.window.API.leagueStandings = vi.fn().mockResolvedValue(shared);
    runtime.mount(LeagueStandingsViewer, { competition: comp, poolMatches: [], tweaks });
    await Promise.resolve();
    const finalTree = runtime.updateProps({ competition: comp, poolMatches: [], tweaks });
    const cells = findAll(finalTree, n => n.type === 'td' && typeof n.props?.className === 'string' && n.props.className.includes('pool-standings__draw-pos'));
    expect(cells.map(c => collectText(c, expandNamed('NumberedName')))).toEqual(['1', '1', '3', '3']);
  });
});

describe('poolMatchesSignature (N7): the one key the league table and admin Pools refetch on', () => {
  let poolMatchesSignature;
  beforeEach(async () => {
    vi.resetModules();
    ({ poolMatchesSignature } = await import('../viewer_standings.jsx'));
  });

  const done = (over) => ({ id: 'Pool A-0', status: 'completed', winner: 'A', ipponsA: ['M'], ipponsB: [], decision: '', ...over });

  it('changes when a correction keeps the winner but changes the score', () => {
    expect(poolMatchesSignature([done({ ipponsA: ['M', 'K'] })])).not.toBe(poolMatchesSignature([done()]));
  });

  it('changes when a team bout is corrected under the same encounter winner', () => {
    const subs = (d) => [{ winner: 'A', ipponsA: ['M'], ipponsB: [], decision: '' }, { winner: d ? '' : 'A', ipponsA: [], ipponsB: [], decision: d }];
    expect(poolMatchesSignature([done({ subResults: subs('hikiwake') })])).not.toBe(poolMatchesSignature([done({ subResults: subs('') })]));
  });

  it('changes when engi flags change', () => {
    expect(poolMatchesSignature([done({ flagsA: 3 })])).not.toBe(poolMatchesSignature([done({ flagsA: 2 })]));
  });

  it('reads a winner given as a name or as {name} alike', () => {
    expect(poolMatchesSignature([done({ winner: { name: 'A' } })])).toBe(poolMatchesSignature([done()]));
  });

  it('ignores the score of a match not yet finished', () => {
    const live = (ipponsA) => [{ id: 'Pool A-0', status: 'running', winner: '', ipponsA, ipponsB: [] }];
    expect(poolMatchesSignature(live(['M']))).toBe(poolMatchesSignature(live([])));
  });

  it('is empty for no matches', () => {
    expect(poolMatchesSignature(undefined)).toBe('');
    expect(poolMatchesSignature([])).toBe('');
  });
});

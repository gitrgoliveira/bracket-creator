import { describe, it, expect } from 'vitest';
import { overlayPositionLabel, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, poolNameOf } from '../display.jsx';
import { TeamScoreboard, IndividualScore } from '../match_scoreboard.jsx';

// mp-13y: white TvDisplay board. The board is TV CHROME (court header, team-name
// row, NEXT, sponsor) that delegates the scoreboard body to the SHARED
// match_scoreboard.jsx components (TeamScoreboard / IndividualScore) — the same
// ones the viewer card uses. The scoreboard's own rendering (slots, IV/PW
// summary, DH banner) is covered by match_scoreboard.test.jsx.

describe('overlayPositionLabel — FIK names only for 5-person teams', () => {
  it('returns Senpo..Taisho for a 5-person team', () => {
    expect(overlayPositionLabel(5, 0, {})).toBe('Senpo');
    expect(overlayPositionLabel(5, 2, {})).toBe('Chuken');
    expect(overlayPositionLabel(5, 4, {})).toBe('Taisho');
  });
  it('falls back to the bare bout number for non-5 teams (3, 7, kachinuki 11/15)', () => {
    expect(overlayPositionLabel(3, 0, {})).toBe('1');
    expect(overlayPositionLabel(3, 2, {})).toBe('3');
    expect(overlayPositionLabel(11, 9, {})).toBe('10');
    expect(overlayPositionLabel(15, 14, {})).toBe('15');
  });
  it('returns Daihyosen for the rep bout (position === -1)', () => {
    expect(overlayPositionLabel(5, 0, { position: -1 })).toBe('Daihyosen');
  });
  it('uses an explicit string position verbatim when present', () => {
    expect(overlayPositionLabel(7, 0, { position: 'Jiho' })).toBe('Jiho');
  });
});

function teamPromoted(promotedKind = 'live') {
  return {
    kind: promotedKind,
    match: {
      id: 'm1', round: 'Round 1',
      sideA: { name: 'Red Team' }, sideB: { name: 'White Team' },
      subResults: [
        { position: 1, ipponsB: ['M'], ipponsA: [] },
        { position: 2, ipponsB: [], ipponsA: [] },
      ],
    },
    competition: { id: 'c1', name: 'Teams', kind: 'team', teamSize: 5 },
    isBracket: false,
  };
}

function render(props) { return JSON.stringify(TvWhiteBoard(props)); }

// Depth-first search for a vnode matching the predicate (TvWhiteBoard delegates
// the body to a child component vnode, so we assert on its type + props).
function findVnode(node, pred) {
  if (!node || typeof node !== 'object') return null;
  if (Array.isArray(node)) { for (const k of node) { const f = findVnode(k, pred); if (f) return f; } return null; }
  if (pred(node)) return node;
  const kids = node.children || node.props?.children || [];
  for (const k of [].concat(kids)) { const f = findVnode(k, pred); if (f) return f; }
  return null;
}

describe('TvWhiteBoard', () => {
  const base = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };

  it('renders a white board for a live team match, delegating to TeamScoreboard, NO "LIVE"', () => {
    const p = teamPromoted();
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const str = render(props);
    expect(str).toContain('tvd--white');
    expect(str).toContain('tvd-team-bouts');
    expect(str).toContain('White Team');
    expect(str).toContain('Red Team');
    expect(str).not.toContain('LIVE');
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.variant).toBe('tv');
    expect(sb.props.subResults.length).toBe(2);
  });

  it('delegates an individual match to IndividualScore (no team bout grid)', () => {
    const p = {
      kind: 'live',
      match: { id: 'i1', round: 'Round 1', sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
        ipponsB: ['K'], ipponsA: ['M'], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: false, subResults: [], teamSize: 0 };
    const str = render(props);
    expect(str).toContain('tvd--white');
    expect(str).not.toContain('tvd-team-bouts');
    expect(str).toContain('Shiro P');
    expect(str).toContain('Aka P');
    expect(str).not.toContain('LIVE');
    expect(findVnode(TvWhiteBoard(props), n => n.type === IndividualScore)).toBeTruthy();
    expect(findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard)).toBeNull();
  });

  it('passes showDH to TeamScoreboard when a DH sub exists', () => {
    const p = teamPromoted();
    p.match.subResults = [
      { position: 1, ipponsB: ['M'], ipponsA: [] },
      { position: -1, ipponsB: ['M'], ipponsA: [] },
    ];
    const props = { ...base, promoted: p, promotedKind: 'live', isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5, showDH: true };
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.showDH).toBe(true);
  });

  it('an up-next team match renders the TeamScoreboard (numbered rows), no "Starts soon" / "up next" badge', () => {
    // mp-13y #6/#9: up-next now shows the real scoreboard (TeamScoreboard
    // renders teamSize numbered rows when subResults is empty), and the
    // "↑ up next" badge was dropped.
    const p = teamPromoted('upnext');
    p.match.subResults = [];
    const props = { ...base, promoted: p, promotedKind: 'upnext', isTeamMatch: true, subResults: [], teamSize: 5 };
    const str = render(props);
    expect(str).not.toContain('Starts soon');
    expect(str).not.toContain('up next');
    expect(findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard)).toBeTruthy();
  });
});

// mp-13y: individual TV board lists the whole pool (pool phase) / round
// (knockout) as a bottom-anchored feed with the current match LAST.
describe('poolNameOf', () => {
  it('derives the pool name from a "<Pool>-<idx>" id', () => {
    expect(poolNameOf('Pool A-0')).toBe('Pool A');
    expect(poolNameOf('Pool B-12')).toBe('Pool B');
  });
  it('returns "" when the id has no "<name>-<digits>" tail', () => {
    expect(poolNameOf('')).toBe('');
    expect(poolNameOf(undefined)).toBe('');
    expect(poolNameOf('Pool A')).toBe('');     // no trailing -<digits>
    expect(poolNameOf('Pool A-x')).toBe('');   // trailing token not digits
    // Note: a bracket id "m-r1-0" looks pool-shaped to this helper (last - is
    // followed by digits), so it would return "m-r1". That's harmless because
    // gatherIndividualGroup only calls poolNameOf on the pool-phase branch.
  });
});

describe('gatherIndividualGroup', () => {
  const poolComp = {
    poolMatches: [
      { id: 'Pool A-0', sideA: 'Tanaka', sideB: 'Suzuki', status: 'running', scheduledAt: '09:00' },
      { id: 'Pool A-1', sideA: 'Yamada', sideB: 'Mori', status: 'completed', scheduledAt: '09:10' },
      { id: 'Pool A-2', sideA: 'Tanaka', sideB: 'Yamada', status: 'completed', scheduledAt: '09:20' },
      { id: 'Pool B-0', sideA: 'X', sideB: 'Y', status: 'completed', scheduledAt: '09:00' }, // other pool
      { id: 'Pool A-3', sideA: 'Suzuki', sideB: 'Mori', status: 'scheduled', scheduledAt: '09:30' }, // not started
    ],
  };
  it('gathers the same pool, completed first, current (running) LAST', () => {
    const promoted = { competition: poolComp, match: poolComp.poolMatches[0], isBracket: false };
    const rows = gatherIndividualGroup(promoted);
    expect(rows.map(m => m.id)).toEqual(['Pool A-1', 'Pool A-2', 'Pool A-0']); // running last; Pool B + scheduled excluded
    expect(rows[rows.length - 1].status).toBe('running');
  });
  it('gathers the same bracket round, current LAST', () => {
    const comp = { bracket: { rounds: [
      [ { id: 'm-r1-0', status: 'completed', sideA: 'A', sideB: 'B', scheduledAt: '10:00' },
        { id: 'm-r1-1', status: 'running', sideA: 'C', sideB: 'D', scheduledAt: '10:00' } ],
      [ { id: 'm-r2-0', status: 'scheduled', sideA: '', sideB: '' } ],
    ] } };
    const promoted = { competition: comp, match: comp.bracket.rounds[0][1], isBracket: true, roundIndex: 0 };
    const rows = gatherIndividualGroup(promoted);
    expect(rows.map(m => m.id)).toEqual(['m-r1-0', 'm-r1-1']);
    expect(rows[rows.length - 1].id).toBe('m-r1-1'); // running at the bottom
  });
});

describe('TvIndividualBoard', () => {
  const base = { tournament: { name: 'Cup' }, court: 'B', connected: true, zekken: false, queueMatches: [] };
  const comp = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
    { id: 'Pool A-0', sideA: 'Tanaka', sideB: 'Suzuki', status: 'running', ipponsA: ['M'], ipponsB: [], scheduledAt: '09:00' },
    { id: 'Pool A-1', sideA: 'Yamada', sideB: 'Mori', status: 'completed', ipponsA: ['M'], ipponsB: ['D'], scheduledAt: '09:10' },
  ] };
  it('caps visible rows at TV_INDIV_MAX_VISIBLE (10) — oldest completed drop off the top, current stays', () => {
    // 15 completed rows + 1 running → 16 total; tail 10 = 9 completed + the running one.
    const many = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      ...Array.from({ length: 15 }, (_, i) => ({
        id: `Pool A-${i+1}`, sideA: `A${i+1}`, sideB: `B${i+1}`, status: 'completed',
        ipponsA: ['M'], ipponsB: [], scheduledAt: `09:${String(10+i).padStart(2,'0')}`,
      })),
      { id: 'Pool A-0', sideA: 'Cur', sideB: 'Run', status: 'running', ipponsA: [], ipponsB: ['K'], scheduledAt: '11:00' },
    ] };
    const promoted = { competition: many, match: many.poolMatches[many.poolMatches.length - 1], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const scores = [];
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === IndividualScore) scores.push(n);
      const k=n.children||n.props?.children||[]; [].concat(k).forEach(walk); })(tree);
    expect(scores.length).toBe(10);
    // Current match is the LAST visible row (running, status running).
    expect(scores[scores.length - 1].props.match.status).toBe('running');
    // First visible row is one of the LATER completed matches (oldest 6 dropped).
    expect(scores[0].props.match.id).toBe('Pool A-7'); // 15 - (10-1) = 7
    const str = JSON.stringify(tree);
    expect(str).toContain('"data-dropped":6'); // 16 total - 10 visible
  });

  it('renders one IndividualScore row per pool match, current highlighted, in the top-anchored group', () => {
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const scores = [];
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === IndividualScore) scores.push(n);
      const k=n.children||n.props?.children||[]; [].concat(k).forEach(walk); })(tree);
    expect(scores.length).toBe(2);
    // every row delegates to the shared IndividualScore with showNames
    expect(scores.every(s => s.props.showNames)).toBe(true);
    const str = JSON.stringify(tree);
    expect(str).toContain('tvd-indiv-group');
    expect(str).toContain('tvd-indiv-row-now'); // the running match is flagged current
  });
});

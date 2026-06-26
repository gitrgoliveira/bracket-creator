import { describe, it, expect } from 'vitest';
import { overlayPositionLabel, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, findNextPoolOnCourt, phaseProgressOnCourt, poolNameOf, sideLabel } from '../display.jsx';
import { phaseLabel } from '../display_helpers.jsx';
import { TeamScoreboard, IndividualScore } from '../match_scoreboard.jsx';

// mp-13y: white TvDisplay board. The board is TV CHROME (court header, team-name
// row, NEXT, sponsor) that delegates the scoreboard body to the SHARED
// match_scoreboard.jsx components (TeamScoreboard / IndividualScore) — the same
// ones the viewer card uses. The scoreboard's own rendering (slots, IV/PW
// summary, DH banner) is covered by match_scoreboard.test.jsx.

describe('sideLabel — numberPrefix + zekken', () => {
  it('returns the bare name when there is no number', () => {
    expect(sideLabel({ name: 'Tanaka' })).toBe('Tanaka');
  });
  it('prepends the assigned number when set (numberPrefix support)', () => {
    expect(sideLabel({ name: 'Tanaka', number: 'K1' })).toBe('K1 Tanaka');
  });
  it('honours the zekken displayName when withZekkenName=true and includes the number', () => {
    expect(sideLabel({ name: 'Tanaka Kenji', displayName: 'TANAKA', number: 'K1' }, true)).toBe('K1 TANAKA');
  });
  it('returns "TBD" for null sides', () => {
    expect(sideLabel(null)).toBe('TBD');
    expect(sideLabel(undefined, true)).toBe('TBD');
  });
});

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

function teamPromoted(promotedKind = 'running') {
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

  it('league board header shows just the competition name — no dangling " · " separator', () => {
    // phaseLabel returns "" for league; the subtitle must not render "Name · ".
    const p = teamPromoted();
    p.competition = { id: 'c1', name: 'Veterans League', kind: 'team', teamSize: 5, format: 'league' };
    const props = { ...base, promoted: p, isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const subtitle = findVnode(TvWhiteBoard(props), n =>
      n.type === 'span' && JSON.stringify(n).includes('Veterans League'));
    const text = [].concat(subtitle.children ?? subtitle.props?.children ?? []).join('');
    expect(text).toBe('Veterans League');
    expect(text).not.toContain('·');
  });

  it('renders a white board for a running team match, delegating to TeamScoreboard, NO "LIVE"', () => {
    const p = teamPromoted();
    const props = { ...base, promoted: p, isTeamMatch: true,
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
      kind: 'running',
      match: { id: 'i1', round: 'Round 1', sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
        ipponsB: ['K'], ipponsA: ['M'], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const props = { ...base, promoted: p, isTeamMatch: false, subResults: [], teamSize: 0 };
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
    const props = { ...base, promoted: p, isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5, showDH: true };
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.showDH).toBe(true);
  });

  it('threads shiroName/akaName into TeamScoreboard so DH team-name winner resolves (tri-review #1)', () => {
    // Without this, centreMarks falls back to centre Ht on a daihyosen result
    // persisted with the team name as the winner — the round-5 win-mark fix
    // never reaches the TV display path.
    const p = teamPromoted();
    const props = { ...base, promoted: p, isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const sb = findVnode(TvWhiteBoard(props), n => n.type === TeamScoreboard);
    expect(sb).toBeTruthy();
    expect(sb.props.shiroName).toBe('White Team');
    expect(sb.props.akaName).toBe('Red Team');
  });

  it('threads withZekkenName to IndividualScore so zekken-mode shows displayName (tri-review #2)', () => {
    const p = {
      kind: 'running',
      match: { id: 'i1', round: 'Round 1',
        sideA: { name: 'Aka Player', displayName: 'AKA' },
        sideB: { name: 'Shiro Player', displayName: 'SHI' },
        ipponsB: [], ipponsA: [], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const props = { ...base, promoted: p, isTeamMatch: false,
      subResults: [], teamSize: 0, zekken: true };
    const is = findVnode(TvWhiteBoard(props), n => n.type === IndividualScore);
    expect(is).toBeTruthy();
    expect(is.props.withZekkenName).toBe(true);
  });

  it('an up-next team match renders the TeamScoreboard (numbered rows), no "Starts soon" / "up next" badge', () => {
    // mp-13y #6/#9: up-next now shows the real scoreboard (TeamScoreboard
    // renders teamSize numbered rows when subResults is empty), and the
    // "↑ up next" badge was dropped.
    const p = teamPromoted('upnext');
    p.match.subResults = [];
    const props = { ...base, promoted: p, isTeamMatch: true, subResults: [], teamSize: 5 };
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
  it('strips DH/TB supplementary-bout suffixes to the base pool', () => {
    // Backend ids: "Pool X-DH-N" (daihyosen), "Pool X-TB-N" (tiebreaker).
    expect(poolNameOf('Pool A-DH-0')).toBe('Pool A');
    expect(poolNameOf('Pool A-TB-1')).toBe('Pool A');
    expect(poolNameOf('Pool B-DH-2')).toBe('Pool B');
  });
  it('keeps hyphenated pool names intact (non-greedy capture)', () => {
    expect(poolNameOf('Pool A-East-0')).toBe('Pool A-East');
    expect(poolNameOf('Pool A-East-DH-0')).toBe('Pool A-East');
  });
  it('returns "" when the id has no "<name>-<digits>" tail', () => {
    expect(poolNameOf('')).toBe('');
    expect(poolNameOf(undefined)).toBe('');
    expect(poolNameOf('Pool A')).toBe('');     // no trailing -<digits>
    expect(poolNameOf('Pool A-x')).toBe('');   // trailing token not digits
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
  it('gathers the whole pool — completed first, current next, scheduled LAST; other pools excluded', () => {
    // Pool-phase per-court board shows the WHOLE pool so spectators see the
    // pool's full progression (past → present → future) on one screen.
    // Status sort: completed → current → scheduled. Pool B is excluded.
    const promoted = { competition: poolComp, match: poolComp.poolMatches[0], isBracket: false };
    const rows = gatherIndividualGroup(promoted);
    expect(rows.map(m => m.id)).toEqual(['Pool A-1', 'Pool A-2', 'Pool A-0', 'Pool A-3']);
    // Status order check: completed, completed, running, scheduled.
    expect(rows.map(m => m.status)).toEqual(['completed', 'completed', 'running', 'scheduled']);
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
  it('filters bracket round to the promoted court — cross-court matches excluded', () => {
    const comp = { bracket: { rounds: [
      [ { id: 'm-r1-0', court: 'A', status: 'completed', sideA: 'A', sideB: 'B', scheduledAt: '10:00' },
        { id: 'm-r1-1', court: 'B', status: 'running', sideA: 'C', sideB: 'D', scheduledAt: '10:00' },
        { id: 'm-r1-2', court: 'A', status: 'running', sideA: 'E', sideB: 'F', scheduledAt: '10:05' } ],
    ] } };
    // Court A display: should see m-r1-0 + m-r1-2, NOT m-r1-1 (court B).
    const promoted = { competition: comp, match: comp.bracket.rounds[0][2], isBracket: true, roundIndex: 0 };
    const rows = gatherIndividualGroup(promoted, 'A');
    expect(rows.map(m => m.id)).toEqual(['m-r1-0', 'm-r1-2']);
  });
  it('orders by NUMERIC match index in an untimed pool (no scheduledAt) — not lexicographic id', () => {
    // 12 completed bouts + a running one, no scheduledAt / queuePosition.
    // Lexicographic id sort would put "Pool A-10" before "Pool A-2"; the numeric
    // tiebreak keeps 2 < 10. The running match sorts last (current slot).
    const comp = { poolMatches: [
      ...Array.from({ length: 12 }, (_, i) => ({ id: `Pool A-${i}`, sideA: `A${i}`, sideB: `B${i}`, status: 'completed' })),
      { id: 'Pool A-99', sideA: 'Run', sideB: 'Ner', status: 'running' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[comp.poolMatches.length - 1], isBracket: false };
    const rows = gatherIndividualGroup(promoted);
    expect(rows.map(m => m.id)).toEqual([
      ...Array.from({ length: 12 }, (_, i) => `Pool A-${i}`),
      'Pool A-99',
    ]);
  });
});

describe('findNextPoolOnCourt', () => {
  // Two pools both routed to court A; Pool A is current.
  const comp = { poolMatches: [
    { id: 'Pool A-0', court: 'A', sideA: 'Eduardo', sideB: 'Carol', status: 'running',   scheduledAt: '09:00' },
    { id: 'Pool A-1', court: 'A', sideA: 'Eduardo', sideB: 'Erin',  status: 'scheduled', scheduledAt: '09:05' },
    { id: 'Pool A-2', court: 'A', sideA: 'Carol',   sideB: 'Erin',  status: 'scheduled', scheduledAt: '09:10' },
    { id: 'Pool B-0', court: 'A', sideA: 'Philippe',sideB: 'Dave',  status: 'scheduled', scheduledAt: '09:15' },
    { id: 'Pool B-1', court: 'A', sideA: 'Philippe',sideB: 'Frank', status: 'scheduled', scheduledAt: '09:20' },
    { id: 'Pool B-2', court: 'A', sideA: 'Dave',    sideB: 'Frank', status: 'scheduled', scheduledAt: '09:25' },
  ] };
  it('returns the next pool on this court with its roster (first-seen order)', () => {
    const res = findNextPoolOnCourt(comp, 'Pool A', 'A');
    expect(res).not.toBeNull();
    expect(res.name).toBe('Pool B');
    // Each name carries its STARTING colour (first bout, scheduled order).
    // Order is Shiro-first within each match to match the left-dark/right-red
    // convention on the match board:
    // B-0 Dave(sideB→shiro) then Philippe(sideA→aka); B-1 …Frank(sideB→shiro).
    expect(res.players).toEqual([
      { name: 'Dave', side: 'shiro' },
      { name: 'Philippe', side: 'aka' },
      { name: 'Frank', side: 'shiro' },
    ]);
  });
  it('returns null when there is no next pool on this court', () => {
    expect(findNextPoolOnCourt(comp, 'Pool B', 'A')).toBeNull();
  });
  it('colours the roster by NUMERIC match order in an untimed pool (not lexicographic id)', () => {
    // No scheduledAt / queuePosition. Lexicographic id sort puts "Pool B-10"
    // before "Pool B-2", which would mis-attribute the starting colour. The
    // numeric tiebreak keeps B-2 first → "EarlyShiro" is seen first as Shiro
    // (sideB-first ordering so Shiro appears before Aka within each match).
    const c = { poolMatches: [
      { id: 'Pool A-0',  court: 'A', sideA: 'X',    sideB: 'Y',          status: 'running' },
      { id: 'Pool B-10', court: 'A', sideA: 'Late', sideB: 'LateShiro',  status: 'scheduled' },
      { id: 'Pool B-2',  court: 'A', sideA: 'Early',sideB: 'EarlyShiro', status: 'scheduled' },
    ] };
    const res = findNextPoolOnCourt(c, 'Pool A', 'A');
    expect(res.name).toBe('Pool B');
    expect(res.players[0]).toEqual({ name: 'EarlyShiro', side: 'shiro' });
  });
  it('ignores pools on other courts', () => {
    const c2 = { poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running',  scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'B', sideA: 'P', sideB: 'Q', status: 'scheduled', scheduledAt: '09:05' },
    ] };
    expect(findNextPoolOnCourt(c2, 'Pool A', 'A')).toBeNull();
  });
  it('orders the next pool by queuePosition first (untimed schedule)', () => {
    // No scheduledAt; queuePosition is the real per-court order. Pool C has the
    // lower queue position than Pool B, so C is next even though B sorts first
    // alphabetically (the old scheduledAt-only sort would have picked B).
    const c = { poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running',   queuePosition: 1 },
      { id: 'Pool B-0', court: 'A', sideA: 'P', sideB: 'Q', status: 'scheduled', queuePosition: 9 },
      { id: 'Pool C-0', court: 'A', sideA: 'M', sideB: 'N', status: 'scheduled', queuePosition: 5 },
    ] };
    expect(findNextPoolOnCourt(c, 'Pool A', 'A').name).toBe('Pool C');
  });
  it('picks earliest scheduledAt across pools; alphabetical tiebreak', () => {
    const c3 = { poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running',   scheduledAt: '09:00' },
      // Pool C first match at 10:00, Pool B first match at 10:00 → Pool B wins (alphabetical).
      { id: 'Pool C-0', court: 'A', sideA: 'M', sideB: 'N', status: 'scheduled', scheduledAt: '10:00' },
      { id: 'Pool B-0', court: 'A', sideA: 'P', sideB: 'Q', status: 'scheduled', scheduledAt: '10:00' },
    ] };
    expect(findNextPoolOnCourt(c3, 'Pool A', 'A').name).toBe('Pool B');
  });
  it('roster honours number prefix + zekken displayName via sideLabel', () => {
    // Object sides with number + displayName; withZekkenName true → the roster
    // must match the rest of the TV surface (e.g. "K1 Ryu", not "Tanaka").
    const c = { withZekkenName: true, poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running', scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'A', status: 'scheduled', scheduledAt: '09:30',
        sideA: { name: 'Tanaka', displayName: 'Ryu', number: 'K1' },
        sideB: { name: 'Suzuki', displayName: 'Sho', number: 'K2' } },
    ] };
    const res = findNextPoolOnCourt(c, 'Pool A', 'A');
    expect(res.players).toEqual([
      { name: 'K2 Sho', side: 'shiro' },
      { name: 'K1 Ryu', side: 'aka' },
    ]);
  });
  it('surfaces team names for team competitions (sideA/sideB ARE team names)', () => {
    const team = { kind: 'team', poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'Team Alpha', sideB: 'Team Beta',  status: 'running',   scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'A', sideA: 'Team Gamma', sideB: 'Team Delta', status: 'scheduled', scheduledAt: '09:30' },
      { id: 'Pool B-1', court: 'A', sideA: 'Team Gamma', sideB: 'Team Epsilon', status: 'scheduled', scheduledAt: '09:40' },
    ] };
    const res = findNextPoolOnCourt(team, 'Pool A', 'A');
    expect(res.name).toBe('Pool B');
    expect(res.players).toEqual([
      { name: 'Team Delta', side: 'shiro' },
      { name: 'Team Gamma', side: 'aka' },
      { name: 'Team Epsilon', side: 'shiro' },
    ]);
  });
  it('excludes a pool already started on ANOTHER court (matches can move courts)', () => {
    // Pool B is routed to court A (scheduled here) but already has a COMPLETED
    // match on court C — it has begun elsewhere, so it must not surface as the
    // future "UP NEXT" pool on court A.
    const c = { poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running',   scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'A', sideA: 'P', sideB: 'Q', status: 'scheduled', scheduledAt: '09:15' },
      { id: 'Pool B-1', court: 'C', sideA: 'P', sideB: 'R', status: 'completed', scheduledAt: '08:50' },
    ] };
    expect(findNextPoolOnCourt(c, 'Pool A', 'A')).toBeNull();
  });
});

describe('TvIndividualBoard', () => {
  const base = { tournament: { name: 'Cup' }, court: 'B', connected: true, zekken: false, queueMatches: [] };
  const comp = { name: 'Indiv', kind: 'individual', teamSize: 0, format: 'mixed', poolMatches: [
    { id: 'Pool A-0', court: 'B', sideA: 'Tanaka', sideB: 'Suzuki', status: 'running', ipponsA: ['M'], ipponsB: [], scheduledAt: '09:00' },
    { id: 'Pool A-1', court: 'B', sideA: 'Yamada', sideB: 'Mori', status: 'completed', ipponsA: ['M'], ipponsB: ['D'], scheduledAt: '09:10' },
  ] };
  it('caps visible rows at TV_INDIV_MAX_VISIBLE (10) — oldest completed drop off the top, current stays', () => {
    // 15 completed rows + 1 running → 16 total; tail 10 = 9 completed + the running one.
    const many = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      ...Array.from({ length: 15 }, (_, i) => ({
        id: `Pool A-${i+1}`, court: 'B', sideA: `A${i+1}`, sideB: `B${i+1}`, status: 'completed',
        ipponsA: ['M'], ipponsB: [], scheduledAt: `09:${String(10+i).padStart(2,'0')}`,
      })),
      { id: 'Pool A-0', court: 'B', sideA: 'Cur', sideB: 'Run', status: 'running', ipponsA: [], ipponsB: ['K'], scheduledAt: '11:00' },
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

  it('keeps the running row visible when many scheduled matches follow it (windowed, not tail-sliced)', () => {
    // Pool phase order is completed → current → scheduled. With 1 completed +
    // 1 running + 20 scheduled, a blind tail slice would show only scheduled
    // rows and DROP the running match. windowAroundCurrent must keep it on screen.
    const many = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'D1', sideB: 'D2', status: 'completed', ipponsA: ['M'], ipponsB: [], scheduledAt: '09:00' },
      { id: 'Pool A-1', court: 'B', sideA: 'Cur', sideB: 'Run', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:05' },
      ...Array.from({ length: 20 }, (_, i) => ({
        id: `Pool A-${i + 2}`, court: 'B', sideA: `S${i}`, sideB: `T${i}`, status: 'scheduled',
        ipponsA: [], ipponsB: [], scheduledAt: `10:${String(i).padStart(2,'0')}`,
      })),
    ] };
    const promoted = { competition: many, match: many.poolMatches[1], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const scores = [];
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === IndividualScore) scores.push(n);
      const k=n.children||n.props?.children||[]; [].concat(k).forEach(walk); })(tree);
    expect(scores.length).toBe(10);
    expect(scores.some(s => s.props.match.status === 'running')).toBe(true);
  });

  it('renders one IndividualScore row per pool match, current highlighted, in the space-evenly group', () => {
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

  it('NOW row uses navy treatment (accent-soft background only); amber #fef3c7 is absent', () => {
    // mp-pa6s: running row uses the DESIGN.md §3 navy running signal — a quiet
    // var(--accent-soft) BACKGROUND only (no spine/border, no transform, no
    // pulse) — and must NOT use the old amber background. The per-court board
    // also omits the inline "NOW" dot/label badge: every row is the same size
    // and the bg tint alone marks the live row.
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const str = JSON.stringify(TvIndividualBoard({ ...base, promoted }));
    expect(str).toContain('tvd-indiv-row-now');
    expect(str).not.toContain('#fef3c7');
  });

  it('completed (non-running) rows do NOT get the navy NOW treatment', () => {
    // mp-pa6s: only the live row gets the var(--accent-soft) background tint.
    // Completed rows keep the grey #f9fafb background.
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    // Walk the vnode tree and collect the wrapper div for each row by testid.
    const rows = [];
    (function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (Array.isArray(n)) { n.forEach(walk); return; }
      const tid = n.props?.['data-testid'];
      if (tid === 'tvd-indiv-row' || tid === 'tvd-indiv-row-now') rows.push(n);
      const k = n.children || n.props?.children || [];
      [].concat(k).forEach(walk);
    })(TvIndividualBoard({ ...base, promoted }));
    // We have 2 rows total (poolMatches has 2 entries).
    expect(rows.length).toBe(2);
    const nowRow = rows.find(r => r.props['data-testid'] === 'tvd-indiv-row-now');
    const doneRow = rows.find(r => r.props['data-testid'] === 'tvd-indiv-row');
    // NOW row: navy soft bg as the live signal (no spine, no transform).
    expect(nowRow.props.style.background).toBe('var(--accent-soft)');
    expect(nowRow.props.style.borderLeft).toBeUndefined();
    // Completed row: grey bg.
    expect(doneRow.props.style.background).toBe('#f9fafb');
    expect(doneRow.props.style.borderLeft).toBeUndefined();
  });

  it('all rows have the same padding and no transform — the live row is signalled by bg only', () => {
    // Per user constraint: rows must be uniform size on /display?court=A.
    // No transform, no spine, no asymmetric padding between live and queue —
    // the bg tint alone carries the live signal. Text scales globally via
    // --msb-scale based on row count (asserted separately below).
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const rows = [];
    (function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (Array.isArray(n)) { n.forEach(walk); return; }
      const tid = n.props?.['data-testid'];
      if (tid === 'tvd-indiv-row' || tid === 'tvd-indiv-row-now') rows.push(n);
      const k = n.children || n.props?.children || [];
      [].concat(k).forEach(walk);
    })(TvIndividualBoard({ ...base, promoted }));
    expect(rows.length).toBe(2);
    const padding = new Set(rows.map(r => r.props.style.padding));
    expect(padding.size).toBe(1); // every row carries the same padding
    // No row carries a transform; the IndividualScore is rendered directly
    // (not wrapped in a scaled <div>).
    for (const r of rows) {
      const kids = [].concat(r.props?.children || r.children || []);
      for (const k of kids) {
        if (k && typeof k === 'object' && k.type === 'div') {
          expect(k.props?.style?.transform).toBeUndefined();
        }
      }
    }
  });

  it('body container sets --msb-scale based on row count (text adapts to available room)', () => {
    // Few rows → big text (scale toward the 2.4 cap); many rows → smaller text
    // (scale toward the 0.85 floor). The CSS .msb--tv rules read this variable.
    const fewRows = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'A', sideB: 'B', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promotedFew = { competition: fewRows, match: fewRows.poolMatches[0], isBracket: false };
    const strFew = JSON.stringify(TvIndividualBoard({ ...base, promoted: promotedFew }));
    // 1 row → scale = clamp(0.85, 9/1, 2.4) = 2.4
    expect(strFew).toContain('"--msb-scale":2.4');

    // Build a full pool with many matches so the row count grows.
    const many = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      ...Array.from({ length: 9 }, (_, i) => ({
        id: `Pool A-${i + 1}`, court: 'B', sideA: `A${i+1}`, sideB: `B${i+1}`,
        status: 'completed', ipponsA: ['M'], ipponsB: [], scheduledAt: `09:${String(10+i).padStart(2,'0')}`,
      })),
      { id: 'Pool A-0', court: 'B', sideA: 'Cur', sideB: 'Run', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '11:00' },
    ] };
    const promotedMany = { competition: many, match: many.poolMatches[many.poolMatches.length - 1], isBracket: false };
    const strMany = JSON.stringify(TvIndividualBoard({ ...base, promoted: promotedMany }));
    // 10 rows → scale = clamp(0.85, 9/10, 2.4) = 0.9
    expect(strMany).toContain('"--msb-scale":0.9');
  });

  it('caps a LEAGUE board at 6 visible rows (windowed around the current match)', () => {
    // 28-match round-robin all on court B → must show only 6, including the running row.
    const league = { name: 'League', kind: 'individual', teamSize: 0, format: 'league', poolMatches: [
      ...Array.from({ length: 12 }, (_, i) => ({
        id: `Pool A-${i}`, court: 'B', sideA: `A${i}`, sideB: `B${i}`, status: 'completed',
        ipponsA: ['M'], ipponsB: [], scheduledAt: `09:${String(i).padStart(2,'0')}`,
      })),
      { id: 'Pool A-12', court: 'B', sideA: 'Run', sideB: 'Cur', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '10:00' },
      ...Array.from({ length: 15 }, (_, i) => ({
        id: `Pool A-${i+13}`, court: 'B', sideA: `S${i}`, sideB: `T${i}`, status: 'scheduled',
        ipponsA: [], ipponsB: [], scheduledAt: `10:${String(i+1).padStart(2,'0')}`,
      })),
    ] };
    const promoted = { competition: league, match: league.poolMatches[12], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const scores = [];
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === IndividualScore) scores.push(n);
      const k=n.children||n.props?.children||[]; [].concat(k).forEach(walk); })(tree);
    expect(scores.length).toBe(6);
    expect(scores.some(s => s.props.match.status === 'running')).toBe(true);
  });

  it('renders the "UP NEXT" pool strip with name + roster when another pool follows on this court', () => {
    const multiPool = { name: 'Indiv', kind: 'individual', teamSize: 0, format: 'mixed', poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'Eduardo', sideB: 'Carol',  status: 'running',   scheduledAt: '09:00' },
      { id: 'Pool A-1', court: 'B', sideA: 'Eduardo', sideB: 'Erin',   status: 'scheduled', scheduledAt: '09:05' },
      { id: 'Pool B-0', court: 'B', sideA: 'Philippe',sideB: 'Dave',   status: 'scheduled', scheduledAt: '09:30' },
      { id: 'Pool B-1', court: 'B', sideA: 'Philippe',sideB: 'Frank',  status: 'scheduled', scheduledAt: '09:35' },
      { id: 'Pool B-2', court: 'B', sideA: 'Dave',    sideB: 'Frank',  status: 'scheduled', scheduledAt: '09:40' },
    ] };
    const promoted = { competition: multiPool, match: multiPool.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const str = JSON.stringify(tree);
    expect(str).toContain('tvd-next-pool');
    expect(str).toContain('UP NEXT');
    expect(str).toContain('Pool B');
    expect(str).toContain('Philippe');
    expect(str).toContain('Dave');
    expect(str).toContain('Frank');
    // Each roster name is wrapped in a span coloured by its starting side:
    // Philippe is sideA (Aka) in B-0 → red; Dave/Frank are sideB → dark #111.
    const nameSpans = [];
    const kidsOf = n => (n.children != null ? n.children : n.props?.children);
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === 'span') {
        const c = kidsOf(n);
        const text = typeof c === 'string' ? c : (Array.isArray(c) && c.length === 1 && typeof c[0] === 'string' ? c[0] : '');
        if (['Philippe','Dave','Frank'].includes(text)) nameSpans.push({ text, color: n.props?.style?.color });
      }
      [].concat(kidsOf(n) || []).forEach(walk); })(tree);
    const byName = Object.fromEntries(nameSpans.map(s => [s.text, s.color]));
    expect(byName['Philippe']).toBe('var(--red, #b91c1c)');
    expect(byName['Dave']).toBe('#111');
    expect(byName['Frank']).toBe('#111');
    // The roster container must be a wrappable flex row so long rosters
    // wrap to a second line rather than clipping or ellipsizing.
    const kidsOf2 = n => (n.children != null ? n.children : n.props?.children);
    const rosterDiv = (function find(n){ if(!n||typeof n!=='object') return null; if(Array.isArray(n)){for(const k of n){const r=find(k); if(r) return r;} return null;}
      if(n.type==='div' && n.props?.style?.flexWrap==='wrap') return n;
      const k=kidsOf2(n)||[]; for(const c of [].concat(k)){const r=find(c); if(r) return r;} return null; })(tree);
    expect(rosterDiv).toBeTruthy();
    expect(rosterDiv.props.style.flexWrap).toBe('wrap');
  });

  it('does NOT render the UP NEXT pool strip when there is no following pool on this court', () => {
    // The base fixture has only Pool A on court B; no next pool.
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const str = JSON.stringify(TvIndividualBoard({ ...base, promoted }));
    expect(str).not.toContain('tvd-next-pool');
  });

  it('does NOT render the UP NEXT pool strip for a Swiss competition (poolNameOf treats Swiss ids as pools)', () => {
    // Swiss matches live in poolMatches with ids "Swiss-R{n}-{i}"; the strip is
    // gated to format === "mixed" so a Swiss board never floods it with the next
    // round's roster.
    const swiss = { name: 'Swiss Open', kind: 'individual', teamSize: 0, format: 'swiss', poolMatches: [
      { id: 'Swiss-R1-0', court: 'B', sideA: 'A', sideB: 'B', status: 'running',   scheduledAt: '09:00' },
      { id: 'Swiss-R2-0', court: 'B', sideA: 'C', sideB: 'D', status: 'scheduled', scheduledAt: '10:00' },
    ] };
    const promoted = { competition: swiss, match: swiss.poolMatches[0], isBracket: false };
    const str = JSON.stringify(TvIndividualBoard({ ...base, promoted }));
    expect(str).not.toContain('tvd-next-pool');
  });

  it('passes match sides with .number through to IndividualScore (numberPrefix support)', () => {
    // mp-13y: when a competition has numberPrefix configured, the assigned
    // number (e.g. "K1") rides on match.sideA.number / match.sideB.number
    // — TvIndividualBoard must pass the full side object through so the
    // shared IndividualScore can render "K1 Tanaka".
    const numbered = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', status: 'running',
        sideA: { name: 'Suzuki', number: 'K2' },
        sideB: { name: 'Tanaka', number: 'K1' },
        ipponsA: ['M'], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promoted = { competition: numbered, match: numbered.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ ...base, promoted });
    const scores = [];
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if(n.type === IndividualScore) scores.push(n);
      const k=n.children||n.props?.children||[]; [].concat(k).forEach(walk); })(tree);
    expect(scores.length).toBe(1);
    expect(scores[0].props.match.sideA.number).toBe('K2');
    expect(scores[0].props.match.sideB.number).toBe('K1');
  });
});

describe('phaseProgressOnCourt + phase strip', () => {
  // Shared vnode walker used throughout this file.
  function findAll(node, pred) {
    const found = [];
    (function walk(n) {
      if (!n || typeof n !== 'object') return;
      if (Array.isArray(n)) { n.forEach(walk); return; }
      if (pred(n)) found.push(n);
      const kids = n.children || n.props?.children || [];
      [].concat(kids).forEach(walk);
    })(node);
    return found;
  }
  function findOne(node, pred) { return findAll(node, pred)[0] || null; }

  // a) Pool phase per-court
  it('pool phase: counts only matches of the current pool on the requested court', () => {
    const competition = { poolMatches: [
      { id: 'Pool A-0', court: 'A', status: 'completed' },
      { id: 'Pool A-1', court: 'A', status: 'running' },
      { id: 'Pool A-2', court: 'A', status: 'scheduled' },
      { id: 'Pool A-3', court: 'B', status: 'completed' },
      { id: 'Pool A-4', court: 'B', status: 'completed' },
      { id: 'Pool A-5', court: 'B', status: 'completed' },
    ] };
    const promoted = { competition, isBracket: false, match: { id: 'Pool A-0' } };
    const result = phaseProgressOnCourt(promoted, 'A');
    expect(result).not.toBeNull();
    expect(result.done).toBe(1);
    expect(result.total).toBe(3);
  });

  // a2) A DH/TB supplementary bout promoted → still counts under its base pool
  it('pool phase: a promoted DH/TB bout counts under the base pool (suffix stripped)', () => {
    const competition = { poolMatches: [
      { id: 'Pool A-0', court: 'A', status: 'completed' },
      { id: 'Pool A-1', court: 'A', status: 'completed' },
      { id: 'Pool A-DH-0', court: 'A', status: 'running' },
    ] };
    const promoted = { competition, isBracket: false, match: { id: 'Pool A-DH-0' } };
    const result = phaseProgressOnCourt(promoted, 'A');
    expect(result).toEqual({ done: 2, total: 3 });
  });

  // b) Bracket round per-court (resolved sides required to count)
  it('bracket phase: counts matches in roundIndex on the requested court only', () => {
    const S = (a, b) => ({ sideA: { name: a }, sideB: { name: b } });
    const competition = { bracket: { rounds: [
      [ { id: 'm-r1-0', court: 'A', status: 'completed', ...S('A1', 'A2') },
        { id: 'm-r1-1', court: 'A', status: 'scheduled', ...S('A3', 'A4') },
        { id: 'm-r1-2', court: 'B', status: 'completed', ...S('B1', 'B2') },
        { id: 'm-r1-3', court: 'B', status: 'completed', ...S('B3', 'B4') } ],
    ] } };
    const promoted = { competition, isBracket: true, roundIndex: 0, match: { id: 'm-r1-0' } };
    const result = phaseProgressOnCourt(promoted, 'A');
    expect(result).not.toBeNull();
    expect(result.done).toBe(1);
    expect(result.total).toBe(2);
  });

  // b2) Bracket placeholders ("Winner of …" / "Pool X-1st") excluded from total
  it('bracket phase: excludes unresolved placeholder matches from the count', () => {
    const competition = { bracket: { rounds: [
      [ { id: 'm-r1-0', court: 'A', status: 'completed', sideA: { name: 'A1' }, sideB: { name: 'A2' } },
        { id: 'm-r1-1', court: 'A', status: 'scheduled', sideA: { name: 'Winner of r0-m0' }, sideB: { name: 'A4' } },
        { id: 'm-r1-2', court: 'A', status: 'scheduled', sideA: { name: 'Pool A-1st' }, sideB: { name: 'Pool B-2nd' } } ],
    ] } };
    const promoted = { competition, isBracket: true, roundIndex: 0, match: { id: 'm-r1-0' } };
    // Only the resolved match counts — the two placeholders are not runnable yet.
    expect(phaseProgressOnCourt(promoted, 'A')).toEqual({ done: 1, total: 1 });
  });

  // c) League single pool — ids shaped "League-0", "League-1", …
  it('league single pool: large match set — returns correct done/total for court', () => {
    const total = 45;
    const doneCount = 12;
    const poolMatches = Array.from({ length: total }, (_, i) => ({
      id: `League-${i}`,
      court: 'A',
      status: i < doneCount ? 'completed' : 'scheduled',
    }));
    const competition = { poolMatches };
    const promoted = { competition, isBracket: false, match: { id: 'League-0' } };
    const result = phaseProgressOnCourt(promoted, 'A');
    expect(result).not.toBeNull();
    expect(result.done).toBe(12);
    expect(result.total).toBe(45);
  });

  // d) No group → null
  it('returns null when poolMatches is empty and isBracket is false', () => {
    const competition = { poolMatches: [] };
    const promoted = { competition, isBracket: false, match: { id: 'Pool A-0' } };
    expect(phaseProgressOnCourt(promoted, 'A')).toBeNull();
  });

  it('returns null when promoted.competition is null', () => {
    const promoted = { competition: null, isBracket: false, match: { id: 'Pool A-0' } };
    expect(phaseProgressOnCourt(promoted, 'A')).toBeNull();
  });

  // e) Render-level: phase strip and progress counter appear in TvIndividualBoard
  it('renders tvd-phase-strip with groupLabel and tvd-phase-progress showing "1 / 3"', () => {
    // phaseLabel derives the pool name from the pool-shaped id via poolNameOf
    // (e.g. "Pool A-1" → "Pool A"); round is incidental here.
    const comp = { name: 'Ind', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'A', round: -1, sideA: 'Tanaka', sideB: 'Suzuki', status: 'completed', ipponsA: ['M'], ipponsB: [], scheduledAt: '09:00' },
      { id: 'Pool A-1', court: 'A', round: -1, sideA: 'Yamada', sideB: 'Mori',   status: 'running',   ipponsA: [], ipponsB: ['D'],  scheduledAt: '09:10' },
      { id: 'Pool A-2', court: 'A', round: -1, sideA: 'Tanaka', sideB: 'Yamada', status: 'scheduled', ipponsA: [], ipponsB: [],    scheduledAt: '09:20' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[1], isBracket: false };
    const tree = TvIndividualBoard({ tournament: { name: 'Cup' }, court: 'A', connected: true, zekken: false, queueMatches: [], promoted });
    const str = JSON.stringify(tree);

    // Strip container is present
    expect(str).toContain('tvd-phase-strip');

    // groupLabel text ("Pool A") appears inside the strip
    const strip = findOne(tree, n => n.props?.['data-testid'] === 'tvd-phase-strip');
    expect(strip).not.toBeNull();
    const stripStr = JSON.stringify(strip);
    expect(stripStr).toContain('Pool A');

    // Progress counter node
    const progress = findOne(tree, n => n.props?.['data-testid'] === 'tvd-phase-progress');
    expect(progress).not.toBeNull();
    const progressText = JSON.stringify(progress.props?.children ?? progress.children ?? '');
    expect(progressText).toContain('1');
    expect(progressText).toContain('3');
  });

  // f) Header subtitle no longer carries the phase label
  it('top-right header span shows competition name only — no phase label', () => {
    const comp = { name: 'MyComp', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ tournament: { name: 'Cup' }, court: 'A', connected: true, zekken: false, queueMatches: [], promoted });
    // Find the span that carries the competition name. It sits inside the
    // top-right flex div that also holds the RECONNECTING badge.
    // We look for a span whose serialised text contains "MyComp" — and assert
    // it does NOT contain "Pool A" (the phase label must have moved to the strip).
    const spans = findAll(tree, n => n.type === 'span' && JSON.stringify(n).includes('MyComp'));
    expect(spans.length).toBeGreaterThan(0);
    for (const sp of spans) {
      const text = JSON.stringify(sp.props?.children ?? sp.children ?? '');
      expect(text).not.toContain('Pool A');
    }
  });
});

// A league is a single round-robin table. The match carries a positive,
// per-match round-robin round number (4, 5, 6, 0…) — meaningless to a
// spectator and visibly inconsistent across the feed. phaseLabel must
// suppress it for format === 'league' so only the completed/total counter
// conveys progress (the round number leaked through as the phase label
// before this fix — "4 · 10 / 28 MATCHES").
describe('phaseLabel — league suppresses the round-robin round number', () => {
  it('returns "" for a league match instead of String(round)', () => {
    const m = { id: 'Pool A-3', round: 4, status: 'running' };
    expect(phaseLabel(m, false, undefined, undefined, 'league')).toBe('');
  });

  it('renders the bare round number for a non-pool, non-bracket match (back-compat)', () => {
    // No pool-shaped id → falls through to the round-number fallback.
    const m = { round: 4, status: 'running' };
    expect(phaseLabel(m, false, undefined, undefined)).toBe('4');
  });

  it('pool (mixed) derives the pool name from the match id via poolNameOf', () => {
    const m = { id: 'Pool A-1', round: -1, status: 'scheduled' };
    expect(phaseLabel(m, false, undefined, undefined, 'mixed')).toBe('Pool A');
  });

  it('labels a pool DH/TB supplementary bout as its base pool, not "0"', () => {
    // The engine leaves Round at 0 for DH/TB bouts; without id-derivation this
    // rendered a bogus "0". poolNameOf strips the -DH-/-TB- suffix.
    expect(phaseLabel({ id: 'Pool A-DH-0', round: 0 }, false, undefined, undefined, 'mixed')).toBe('Pool A');
    expect(phaseLabel({ id: 'Pool A-TB-0', round: 0 }, false, undefined, undefined, 'mixed')).toBe('Pool A');
  });
});

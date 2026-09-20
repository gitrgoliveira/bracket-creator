import { describe, it, expect } from 'vitest';
import { overlayPositionLabel, TvWhiteBoard, TvIndividualBoard, gatherIndividualGroup, findNextPoolOnCourt, phaseProgressOnCourt, poolNameOf, sideLabel } from '../display.jsx';
import { phaseLabel } from '../display_helpers.jsx';
import { NumberedName } from '../numbered_name.jsx';
import { TeamScoreboard, IndividualScore } from '../match_scoreboard.jsx';
import { findInTree as findVnode, hasClass, collectText } from './helpers/vdom.js';

// mp-13y: white TvDisplay board. The board is TV CHROME (court header, team-name
// row, NEXT, sponsor) that delegates the scoreboard body to the SHARED
// match_scoreboard.jsx components (TeamScoreboard / IndividualScore): the same
// ones the viewer card uses. The scoreboard's own rendering (slots, IV/PW
// summary, DH banner) is covered by match_scoreboard.test.jsx.

describe('sideLabel: numberPrefix + zekken', () => {
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
  it('passes the colour through to withNumber (Aka number after the name)', () => {
    expect(sideLabel({ name: 'Tanaka', number: 'K1' }, false, 'aka')).toBe('Tanaka K1');
  });
});

describe('overlayPositionLabel: FIK names only for 5-person teams', () => {
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

describe('TvWhiteBoard', () => {
  const base = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };

  it('header centre is a PLAIN vs: the middle mark lives only in the row centre', () => {
    // Operator ruling: X/(E)/(DH) appear in the FIK row centre rendered by the
    // shared scoreboard below, and NOWHERE else on this surface. The header
    // chip used to duplicate the mark ~10cm above the row; assert on an
    // overtime match that the header carries no (E) while the row (which owns
    // the mark via matchMiddleMark) is still mounted.
    //
    // The stub is LOAD-BEARING: this file's import graph never reaches
    // bracket.jsx (the only module that assigns window.matchMiddleMark) and
    // vitest isolates per file, so without it the removed line would evaluate
    // to "" on main too and this test could not fail. Same stub the sibling
    // match_scoreboard / streaming_overlay suites install.
    const priorMiddleMark = window.matchMiddleMark;
    window.matchMiddleMark = (m) => (m?.encho?.periodCount > 0 ? '(E)' : '');
    try {
    const p = teamPromoted();
    p.match = {
      id: 'm2', round: 'Round 1',
      sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
      ipponsA: ['M'], ipponsB: [], winner: { name: 'Aka P' },
      encho: { periodCount: 1 },
    };
    p.competition = { id: 'c2', name: 'Singles', kind: 'individual' };
    const props = { ...base, promoted: p, isTeamMatch: false, subResults: [] };
    const headerCentre = findVnode(TvWhiteBoard(props), n =>
      n?.props?.style?.fontSize === '2.4vh');
    expect(headerCentre).toBeTruthy();
    expect(JSON.stringify(headerCentre)).not.toContain('(E)');
    expect(JSON.stringify(headerCentre)).toContain('vs');
    expect(findVnode(TvWhiteBoard(props), n => n.type === IndividualScore)).toBeTruthy();
    } finally {
      // Scoped: leaving the stub installed would make every later test in this
      // file run with a middle mark defined, so one asserting "no mark here"
      // could pass for the wrong reason and results become order-dependent.
      if (priorMiddleMark === undefined) delete window.matchMiddleMark;
      else window.matchMiddleMark = priorMiddleMark;
    }
  });

  it('league board header shows just the competition name, no dangling " · " separator', () => {
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

  it("a TEAM match shows each side's IV/PW in the headline row, never in the centre (bc-lbty Change 3)", () => {
    const p = teamPromoted();
    const props = { ...base, promoted: p, isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5 };
    const tree = TvWhiteBoard(props);
    // ABSOLUTE CONSTRAINT: the centre stays a bare "vs". The closed-set rule
    // (vs/X/(E)/(DH)) never admits a per-side figure like IV/PW.
    const headerCentre = findVnode(tree, n => n?.props?.style?.fontSize === '2.4vh');
    expect(headerCentre).toBeTruthy();
    expect(collectText(headerCentre)).toBe('vs');
    // Each side's OWN cell carries its IV/PW instead (teamIVPWFrom, falling
    // back to teamIVPW here since teamPromoted()'s match carries no
    // teamResult): sideB (White Team/shiro) won the one scored bout 1-0.
    const shiroBlock = findVnode(tree, n => n?.props?.['data-testid'] === 'headline-ivpw-shiro');
    const akaBlock = findVnode(tree, n => n?.props?.['data-testid'] === 'headline-ivpw-aka');
    expect(shiroBlock).toBeTruthy();
    expect(akaBlock).toBeTruthy();
    expect(collectText(shiroBlock)).toBe('IV 1PW 1');
    expect(collectText(akaBlock)).toBe('PW 0IV 0');
  });

  it('an INDIVIDUAL match renders no headline IV/PW readout', () => {
    const p = {
      kind: 'running',
      match: { id: 'i1', round: 'Round 1', sideA: { name: 'Aka P' }, sideB: { name: 'Shiro P' },
        ipponsB: ['K'], ipponsA: ['M'], subResults: [] },
      competition: { id: 'c2', name: 'Ind', teamSize: 0 }, isBracket: false,
    };
    const props = { ...base, promoted: p, isTeamMatch: false, subResults: [], teamSize: 0 };
    const tree = TvWhiteBoard(props);
    expect(findVnode(tree, n => n?.props?.['data-testid'] === 'headline-ivpw-shiro')).toBeNull();
    expect(findVnode(tree, n => n?.props?.['data-testid'] === 'headline-ivpw-aka')).toBeNull();
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

  it('shows the daihyosen rep player headline + team sub-label when rep names are set (mp-62vr)', () => {
    // A pool DH rep bout: SideA/SideB are TEAM names; repPlayerA/repPlayerB are
    // the fighters the operator recorded. Shiro = sideB → repPlayerB, Aka =
    // sideA → repPlayerA.
    const p = {
      kind: 'running',
      match: { id: 'Pool A-DH-0', round: -1,
        sideA: { name: 'Kyoto Dojo' }, sideB: { name: 'Tokyo Dojo' },
        repPlayerA: 'Sato Ren', repPlayerB: 'Yamada Taro',
        ipponsA: ['M'], ipponsB: [], subResults: [] },
      competition: { id: 'cT', name: 'Team Cup', kind: 'team', teamSize: 5 }, isBracket: false,
    };
    const props = { ...base, promoted: p, isTeamMatch: false, subResults: [], teamSize: 0 };
    const str = render(props);
    // Rep players are the headline; team names appear as the sub-labels.
    expect(str).toContain('Sato Ren');
    expect(str).toContain('Yamada Taro');
    expect(str).toContain('rep-shiro-team');
    expect(str).toContain('rep-aka-team');
    expect(str).toContain('Tokyo Dojo');
    expect(str).toContain('Kyoto Dojo');
  });

  it('falls back to team-name-only headline when rep names are NOT set', () => {
    const p = {
      kind: 'running',
      match: { id: 'Pool A-DH-0', round: -1,
        sideA: { name: 'Kyoto Dojo' }, sideB: { name: 'Tokyo Dojo' },
        ipponsA: ['M'], ipponsB: [], subResults: [] },
      competition: { id: 'cT', name: 'Team Cup', kind: 'team', teamSize: 5 }, isBracket: false,
    };
    const props = { ...base, promoted: p, isTeamMatch: false, subResults: [], teamSize: 0 };
    const str = render(props);
    expect(str).toContain('Tokyo Dojo');
    expect(str).toContain('Kyoto Dojo');
    // No rep sub-label rows when rep names are absent.
    expect(str).not.toContain('rep-shiro-team');
    expect(str).not.toContain('rep-aka-team');
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
    // persisted with the team name as the winner. The round-5 win-mark fix
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
  it('gathers the whole pool: completed first, current next, scheduled LAST; other pools excluded', () => {
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
  it('filters bracket round to the promoted court: cross-court matches excluded', () => {
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
  it('orders by NUMERIC match index in an untimed pool (no scheduledAt): not lexicographic id', () => {
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
  // bc-lbty: findNextPoolOnCourt now returns each bout's shiro/aka as a
  // NumberedName chip element (Change 1: the NEXT strip chips its number
  // everywhere for visual consistency with the main row), not a plain
  // string. Flattening back to {id, shiro, aka} NAME strings here lets the
  // tests below keep asserting on this function's OWN name/pairing/ordering
  // logic without re-testing chip rendering (that is covered separately, see
  // "bout labels honour the outer-side number..." below and the "NEXT strip
  // renders NumberedName chips" describe further down this file).
  const boutNames = (bouts) => bouts.map(b => ({ id: b.id, shiro: b.shiro.props.name, aka: b.aka.props.name }));
  it('returns the next pool on this court with its bouts as Shiro/Aka pairs in run order', () => {
    const res = findNextPoolOnCourt(comp, 'Pool A', 'A');
    expect(res).not.toBeNull();
    expect(res.name).toBe('Pool B');
    // The strip shows WHICH BOUTS come next, as a group (operator ruling
    // 2026-09-14, bc-dnst), not a roster: each entry is one match, sideB as
    // Shiro (left, dark) and sideA as Aka (right, red), in run order.
    expect(boutNames(res.bouts)).toEqual([
      { id: 'Pool B-0', shiro: 'Dave', aka: 'Philippe' },
      { id: 'Pool B-1', shiro: 'Frank', aka: 'Philippe' },
      { id: 'Pool B-2', shiro: 'Frank', aka: 'Dave' },
    ]);
  });
  it('returns null when there is no next pool on this court', () => {
    expect(findNextPoolOnCourt(comp, 'Pool B', 'A')).toBeNull();
  });
  it('orders the bouts by NUMERIC match order in an untimed pool (not lexicographic id)', () => {
    // No scheduledAt / queuePosition. Lexicographic id sort puts "Pool B-10"
    // before "Pool B-2", which would list the bouts out of run order. The
    // numeric tiebreak keeps B-2 first.
    const c = { poolMatches: [
      { id: 'Pool A-0',  court: 'A', sideA: 'X',    sideB: 'Y',          status: 'running' },
      { id: 'Pool B-10', court: 'A', sideA: 'Late', sideB: 'LateShiro',  status: 'scheduled' },
      { id: 'Pool B-2',  court: 'A', sideA: 'Early',sideB: 'EarlyShiro', status: 'scheduled' },
    ] };
    const res = findNextPoolOnCourt(c, 'Pool A', 'A');
    expect(res.name).toBe('Pool B');
    expect(res.bouts.map(b => b.id)).toEqual(['Pool B-2', 'Pool B-10']);
    expect(boutNames(res.bouts)[0]).toEqual({ id: 'Pool B-2', shiro: 'EarlyShiro', aka: 'Early' });
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
  it('bout labels honour the outer-side number + zekken displayName via NumberedName chips', () => {
    // Object sides with number + displayName; withZekkenName true: the bouts
    // must read like the rows above them, Shiro's number BEFORE the name and
    // Aka's AFTER it (operator ruling 2026-09-14, bc-dnst) -- now via a
    // NumberedName chip's own side/name/number props (bc-lbty), not a
    // composed string.
    const c = { withZekkenName: true, poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running', scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'A', status: 'scheduled', scheduledAt: '09:30',
        sideA: { name: 'Tanaka', displayName: 'Ryu', number: 'K1' },
        sideB: { name: 'Suzuki', displayName: 'Sho', number: 'K2' } },
    ] };
    const res = findNextPoolOnCourt(c, 'Pool A', 'A');
    expect(res.bouts).toHaveLength(1);
    const [bout] = res.bouts;
    expect(bout.id).toBe('Pool B-0');
    expect(bout.shiro.type).toBe(NumberedName);
    expect(bout.shiro.props).toMatchObject({ side: 'shiro', name: 'Sho', number: 'K2' });
    expect(bout.aka.type).toBe(NumberedName);
    expect(bout.aka.props).toMatchObject({ side: 'aka', name: 'Ryu', number: 'K1' });
  });
  it('surfaces team names for team competitions (sideA/sideB ARE team names)', () => {
    const team = { kind: 'team', poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'Team Alpha', sideB: 'Team Beta',  status: 'running',   scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'A', sideA: 'Team Gamma', sideB: 'Team Delta', status: 'scheduled', scheduledAt: '09:30' },
      { id: 'Pool B-1', court: 'A', sideA: 'Team Gamma', sideB: 'Team Epsilon', status: 'scheduled', scheduledAt: '09:40' },
    ] };
    const res = findNextPoolOnCourt(team, 'Pool A', 'A');
    expect(res.name).toBe('Pool B');
    expect(boutNames(res.bouts)).toEqual([
      { id: 'Pool B-0', shiro: 'Team Delta', aka: 'Team Gamma' },
      { id: 'Pool B-1', shiro: 'Team Epsilon', aka: 'Team Gamma' },
    ]);
  });
  it('excludes a pool already started on ANOTHER court (matches can move courts)', () => {
    // Pool B is routed to court A (scheduled here) but already has a COMPLETED
    // match on court C: it has begun elsewhere, so it must not surface as the
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
  it('caps visible rows at TV_INDIV_MAX_VISIBLE (10): oldest completed drop off the top, current stays', () => {
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
    // mp-pa6s: running row uses the DESIGN.md §3 navy running signal: a quiet
    // var(--accent-soft) BACKGROUND only (no spine/border, no transform, no
    // pulse) and must NOT use the old amber background. The per-court board
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

  it('all rows have the same padding and no transform: the live row is signalled by bg only', () => {
    // Per user constraint: rows must be uniform size on /display?court=A.
    // No transform, no spine, no asymmetric padding between live and queue.
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
    // Few rows → bigger text (scale toward the 1.5 cap, lowered from 2.4 on
    // operator feedback that a 3-bout pool read as too large); many rows →
    // smaller text (scale toward the 0.85 floor). The CSS .msb--tv rules read
    // this variable.
    const fewRows = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'A', sideB: 'B', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promotedFew = { competition: fewRows, match: fewRows.poolMatches[0], isBracket: false };
    const strFew = JSON.stringify(TvIndividualBoard({ ...base, promoted: promotedFew }));
    // 1 row → scale = clamp(0.85, 7/1, 1.5) = 1.5
    expect(strFew).toContain('"--msb-scale":1.5');

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
    // 10 rows → scale = clamp(0.85, 7/10, 2.4) = 0.85
    expect(strMany).toContain('"--msb-scale":0.85');
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

  it('renders the "UP NEXT" pool strip with the pool name and its bouts as pairs when another pool follows on this court', () => {
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
    // One pair per bout of the next pool, in run order (3 bouts here).
    expect(str.split('"tvd-next-bout"').length - 1).toBe(3);
    expect(str).toContain('3 bouts');
    // Each name is wrapped in a span coloured by its side IN THAT BOUT: Philippe
    // is sideA (Aka) in B-0 and B-1 → red both times; Frank is sideB (Shiro) in
    // B-1 and B-2 → dark both times; Dave is Shiro in B-0 and Aka in B-2, so
    // he is named twice with a different colour each time, as the bouts will
    // be fought.
    const nameSpans = [];
    const kidsOf = n => (n.children != null ? n.children : n.props?.children);
    // chipName: a span's child is either a plain string (pre-chip form) or a
    // NumberedName chip element (bc-lbty) -- and kidsOf's own top-level
    // `.children` alias is ALWAYS an array (even for a single child, per the
    // mock createElement in reactive_react.js / vitest.setup.js), so a single
    // chip child arrives here as a one-element array, not the bare element.
    const chipName = (c) => {
      const el = Array.isArray(c) && c.length === 1 ? c[0] : c;
      return (el && typeof el === 'object' && el.type === NumberedName) ? el.props.name : null;
    };
    // Each bout pair is now rendered via the shared NextPair component
    // (display_scoreboard.jsx), not inline spans: expand any function-typed
    // node (NextPair included) by invoking it with its own props, mirroring
    // what a real renderer would do, so the walk still reaches the coloured
    // name spans NextPair produces internally. NumberedName (bc-lbty: the
    // shiro/aka props are now chips, not strings) is the one exception left
    // UNEXPANDED: its rendered text lives in its own inner span, so expanding
    // it would separate the name text from the colour NextPair's wrapping
    // span (matched below) applies. Reading the chip's `.props.name` via
    // chipName() instead keeps the colour and the name on the SAME span,
    // exactly as the pre-chip plain-string form did.
    (function walk(n){ if(!n||typeof n!=='object') return; if(Array.isArray(n)){n.forEach(walk);return;}
      if (typeof n.type === 'function' && n.type !== NumberedName) { walk(n.type(n.props)); return; }
      if(n.type === 'span') {
        const c = kidsOf(n);
        const text = typeof c === 'string' ? c
          : chipName(c)
          || (Array.isArray(c) && c.length === 1 && typeof c[0] === 'string' ? c[0] : '');
        if (['Philippe','Dave','Frank'].includes(text)) nameSpans.push({ text, color: n.props?.style?.color });
      }
      [].concat(kidsOf(n) || []).forEach(walk); })(tree);
    const coloursOf = (name) => nameSpans.filter(s => s.text === name).map(s => s.color);
    expect(coloursOf('Philippe')).toEqual(['var(--red)', 'var(--red)']);
    expect(coloursOf('Frank')).toEqual(['var(--ink-1)', 'var(--ink-1)']);
    expect(coloursOf('Dave')).toEqual(['var(--ink-1)', 'var(--red)']);
    // The bouts container must be a wrappable flex row so a big pool's bouts
    // wrap to further lines rather than clipping or ellipsizing.
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
    // TvIndividualBoard must pass the full side object through so the
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
    // Only the resolved match counts. The two placeholders are not runnable yet.
    expect(phaseProgressOnCourt(promoted, 'A')).toEqual({ done: 1, total: 1 });
  });

  // c) League single pool: ids shaped "League-0", "League-1", ...
  it('league single pool: large match set, returns correct done/total for court', () => {
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
  it('top-right header span shows competition name only, no phase label', () => {
    const comp = { name: 'MyComp', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'A', sideA: 'X', sideB: 'Y', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ tournament: { name: 'Cup' }, court: 'A', connected: true, zekken: false, queueMatches: [], promoted });
    // Find the span that carries the competition name. It sits inside the
    // top-right flex div that also holds the RECONNECTING badge.
    // We look for a span whose serialised text contains "MyComp" and assert
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
// per-match round-robin round number (4, 5, 6, 0...) is meaningless to a
// spectator and visibly inconsistent across the feed. phaseLabel must
// suppress it for format === 'league' so only the completed/total counter
// conveys progress (the round number leaked through as the phase label
// before this fix: "4 · 10 / 28 MATCHES").
describe('phaseLabel: league suppresses the round-robin round number', () => {
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

  it('does NOT derive a pool-like label from a bracket id when bracketRoundLabel is unavailable', () => {
    // poolNameOf matches any "*-<digits>" shape, so a bracket id "m-r1-0" would
    // wrongly yield "m-r1". The !isBracket guard prevents that: a bracket match
    // with no round-label helper falls through to the numeric round (or "").
    const saved = window.bracketRoundLabel;
    window.bracketRoundLabel = undefined; // simulate bracket.jsx not loaded
    try {
      expect(phaseLabel({ id: 'm-r1-0', round: 0 }, true, 0, 3, 'knockout')).toBe('0');
      expect(phaseLabel({ id: 'm-r1-0' }, true, 0, 3, 'knockout')).toBe('');
    } finally {
      window.bracketRoundLabel = saved;
    }
  });
});

// bc-rvfx: the TV headline cells ELLIPSISE, and sideLabel's string form puts
// Aka's number LAST, so a long team name truncated Aka's number away while
// Shiro's leading number always survived: the two sides degraded differently
// from the same data.
//
// MEASURED on the real board at 1920x1080 before converting (the bead required
// a measurement, not an argument): the headline cell offers 811px at 54px
// Archivo-800, so "Musashi Dojo Thunderbolts T108" (886px) lost its T108
// outright. The cells now render NumberedName's clip mode, which puts the chip
// in its own flex child so only the NAME ellipsises.
//
// The non-clipping rows (NextPair's NEXT line and UP NEXT list, the overlay's
// individual lines) were measured at the same viewport, do NOT clip, and keep
// the plain string form -- there is nothing there to protect.
describe('TvWhiteBoard: the headline number cannot be truncated away (bc-rvfx)', () => {
  function findAllVnodes(node, pred, out = []) {
    if (!node || typeof node !== 'object') return out;
    if (Array.isArray(node)) { node.forEach(k => findAllVnodes(k, pred, out)); return out; }
    if (pred(node)) out.push(node);
    const kids = node.children || node.props?.children || [];
    [].concat(kids).forEach(k => findAllVnodes(k, pred, out));
    return out;
  }

  // Names far past the measured ~28-character threshold, both numbered.
  function longNamedTeams() {
    return {
      kind: 'running',
      match: {
        id: 'm9', round: 'Final',
        sideA: { name: 'Kenshinkan Kendo Renshinkan Melbourne', number: 'T7' },  // AKA
        sideB: { name: 'Musashi Dojo Thunderbolts Alpha', number: 'T25' },       // SHIRO
        subResults: [],
      },
      competition: { id: 'c1', name: 'Teams', kind: 'team', teamSize: 5 },
      isBracket: true,
    };
  }

  // This block's own chrome props: `base` above is scoped to its own describe.
  const chrome = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, queueMatches: [], zekken: false,
  };
  const propsFor = (p) => ({ ...chrome, promoted: p, isTeamMatch: true, subResults: [], teamSize: 5 });

  it('renders each headline name through NumberedName in clip mode', () => {
    const chips = findAllVnodes(TvWhiteBoard(propsFor(longNamedTeams())),
      n => n.type === NumberedName);
    expect(chips.length).toBeGreaterThanOrEqual(2);
    // clip is what keeps the chip out of the ellipsised run; without it the
    // wrapper is display:contents and the cell truncates the chip again.
    chips.forEach(c => expect(c.props.clip).toBeTruthy());
  });

  it('keeps each number on its OUTER side: Shiro before, Aka after', () => {
    const chips = findAllVnodes(TvWhiteBoard(propsFor(longNamedTeams())),
      n => n.type === NumberedName);
    const shiro = chips.find(c => c.props.side === 'shiro');
    const aka = chips.find(c => c.props.side === 'aka');
    expect(shiro).toBeTruthy();
    expect(aka).toBeTruthy();
    expect(shiro.props.number).toBe('T25');
    expect(aka.props.number).toBe('T7');
    // The name is handed over separately from the number, which is the whole
    // point: the cell can ellipsise the one without touching the other.
    expect(shiro.props.name).toBe('Musashi Dojo Thunderbolts Alpha');
    expect(aka.props.name).toBe('Kenshinkan Kendo Renshinkan Melbourne');
  });

  it('still renders a numberless side as a bare name, with no empty chip', () => {
    const p = longNamedTeams();
    delete p.match.sideA.number;
    const aka = findAllVnodes(TvWhiteBoard(propsFor(p)),
      n => n.type === NumberedName).find(c => c.props.side === 'aka');
    expect(aka).toBeTruthy();
    expect(aka.props.number).toBe('');
  });
});

// bc-rvfx / PR #428 audit: sideLabel's outer-side number placement (Shiro's
// number BEFORE the name, Aka's AFTER it) is wired at several
// display_scoreboard.jsx call sites. The two promoted-headline cells are
// covered above (they went through the later NumberedName/clip conversion).
// These NON-clipping NEXT rows used to call sideLabel directly as a plain
// string and were left unpinned by that same audit; as of bc-lbty
// (2026-09-19, Change 1) they render the SAME NumberedName chip (in its
// plain, non-clip form) off sideLabelParts, so these tests now assert on the
// chip's side/name/number PROPS rather than a composed string. NOTE: `base`
// above is scoped to the `TvWhiteBoard`/`TvIndividualBoard` describes above,
// so this block defines its own chrome props rather than reaching for either.
describe('TvWhiteBoard NEXT line: competitor number sits on the outer side (bc-rvfx)', () => {
  const chrome = {
    tournament: { name: 'Cup' }, court: 'A', connected: true,
    lineupA: null, lineupB: null, showDH: false, zekken: false,
  };

  // findNextPairProps: NextPair is not exported by display_scoreboard.jsx, so
  // its element is matched here by shape (a node carrying both `shiro` and
  // `aka` props) rather than by importing the component. A generic
  // findAll/findInTree walk never reaches `shiro`/`aka` on its own: they are
  // custom props holding the two chip elements directly, not `children`,
  // which is the only thing those helpers descend into.
  const findNextPairProps = (tree) =>
    findVnode(tree, n => n && n.props && n.props.shiro !== undefined && n.props.aka !== undefined)?.props;

  it("puts Shiro's number before the name and Aka's after it", () => {
    const p = teamPromoted();
    const props = {
      ...chrome, promoted: p, isTeamMatch: true,
      subResults: p.match.subResults, teamSize: 5,
      queueMatches: [{
        sideA: { name: 'Yamada', number: 'K9' },
        sideB: { name: 'Tanaka', number: 'K5' },
        _comp: { withZekkenName: false },
      }],
    };
    const pair = findNextPairProps(TvWhiteBoard(props));
    expect(pair).toBeTruthy();
    expect(pair.shiro.type).toBe(NumberedName);
    expect(pair.aka.type).toBe(NumberedName);
    // sideB (Tanaka) is Shiro: number leads. sideA (Yamada) is Aka: number trails.
    expect(pair.shiro.props).toMatchObject({ side: 'shiro', name: 'Tanaka', number: 'K5' });
    expect(pair.aka.props).toMatchObject({ side: 'aka', name: 'Yamada', number: 'K9' });
  });
});

describe('TvIndividualBoard: competitor number sits on the outer side (bc-rvfx)', () => {
  const chrome = { tournament: { name: 'Cup' }, court: 'B', connected: true, zekken: false };
  const findNextPairProps = (tree) =>
    findVnode(tree, n => n && n.props && n.props.shiro !== undefined && n.props.aka !== undefined)?.props;

  it("NEXT line puts Shiro's number before the name and Aka's after it", () => {
    const comp = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'X', sideB: 'Y', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    // A queued match not already in the body's single row, and no format
    // "mixed" competing UP NEXT pool strip, so this exercises TvIndividualBoard's
    // OWN "Next match line" (display_scoreboard.jsx, gated on `next && !nextPool`).
    const queueMatches = [{
      id: 'Pool A-9',
      sideA: { name: 'Yamada', number: 'K9' },
      sideB: { name: 'Tanaka', number: 'K5' },
      _comp: { withZekkenName: false },
    }];
    const tree = TvIndividualBoard({ ...chrome, promoted, queueMatches });
    expect(JSON.stringify(tree)).not.toContain('tvd-next-pool'); // sanity: not exercising the pool strip below
    const pair = findNextPairProps(tree);
    expect(pair).toBeTruthy();
    expect(pair.shiro.type).toBe(NumberedName);
    expect(pair.aka.type).toBe(NumberedName);
    expect(pair.shiro.props).toMatchObject({ side: 'shiro', name: 'Tanaka', number: 'K5' });
    expect(pair.aka.props).toMatchObject({ side: 'aka', name: 'Yamada', number: 'K9' });
  });

  it("UP NEXT pool bout strip puts each bout's number on the outer side", () => {
    // Mirrors the existing "renders the UP NEXT pool strip..." fixture above,
    // but with NUMBERED sides: that test's fixture uses bare name strings, so
    // it never exercises the number-placement behaviour at this render site
    // (findNextPoolOnCourt's OWN unit test covers the computed shiro/aka
    // chip elements directly; this covers those elements actually reaching
    // the rendered NextPair unmangled).
    const multiPool = { name: 'Indiv', kind: 'individual', teamSize: 0, format: 'mixed', poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'Eduardo', sideB: 'Carol', status: 'running', scheduledAt: '09:00' },
      { id: 'Pool B-0', court: 'B', status: 'scheduled', scheduledAt: '09:30',
        sideA: { name: 'Yamada', number: 'K9' },
        sideB: { name: 'Tanaka', number: 'K5' } },
    ] };
    const promoted = { competition: multiPool, match: multiPool.poolMatches[0], isBracket: false };
    const tree = TvIndividualBoard({ ...chrome, promoted, queueMatches: [] });
    expect(JSON.stringify(tree)).toContain('tvd-next-pool');
    const pair = findNextPairProps(tree);
    expect(pair).toBeTruthy();
    expect(pair.shiro.type).toBe(NumberedName);
    expect(pair.aka.type).toBe(NumberedName);
    expect(pair.shiro.props).toMatchObject({ side: 'shiro', name: 'Tanaka', number: 'K5' });
    expect(pair.aka.props).toMatchObject({ side: 'aka', name: 'Yamada', number: 'K9' });
  });
});

// bc-lbty (Change 1): the NEXT strip's NumberedName conversion. Both
// TvWhiteBoard's own team "Next line" (a running/up-next TEAM match) and
// TvIndividualBoard's own individual "Next match line" now render the queued
// match's sides as NumberedName chips (a `.num-prefix` span holding the
// number) rather than a bare "T14 Renshin Slate" string, so the NEXT strip
// reads consistently with the chipped main row above it (operator decision
// 2026-09-19). These tests go one step further than the "outer side" describe
// blocks above: they actually CALL the found chip element (NumberedName is a
// pure, hookless function, safe to invoke directly) and assert a real
// `.num-prefix` span comes out, proving the strip renders a chip and not just
// that chip-shaped props were handed to NextPair.
describe('NEXT strip renders NumberedName chips, not bare strings (bc-lbty)', () => {
  const findNextPairProps = (tree) =>
    findVnode(tree, n => n && n.props && n.props.shiro !== undefined && n.props.aka !== undefined)?.props;

  // numPrefixText: render the chip and read the text out of its `.num-prefix`
  // span specifically (not just anywhere in the chip), so a number that leaked
  // into the plain name span instead would not be mistaken for a real chip.
  const numPrefixText = (rendered) => {
    const span = findVnode(rendered, n => hasClass(n, 'num-prefix'));
    if (!span) return null;
    const c = span.children != null ? span.children : span.props?.children;
    return Array.isArray(c) ? c.join('') : c;
  };

  it("TvWhiteBoard's team NEXT line renders a .num-prefix chip for both sides", () => {
    const p = teamPromoted();
    const props = {
      tournament: { name: 'Cup' }, court: 'A', connected: true,
      lineupA: null, lineupB: null, showDH: false, zekken: false,
      promoted: p, isTeamMatch: true, subResults: p.match.subResults, teamSize: 5,
      queueMatches: [{
        sideA: { name: 'Yamada', number: 'K9' },
        sideB: { name: 'Tanaka', number: 'K5' },
        _comp: { withZekkenName: false },
      }],
    };
    const pair = findNextPairProps(TvWhiteBoard(props));
    expect(pair).toBeTruthy();
    // Not a bare string: the NEXT strip hands NextPair a NumberedName element.
    expect(typeof pair.shiro).not.toBe('string');
    expect(typeof pair.aka).not.toBe('string');
    expect(pair.shiro.type).toBe(NumberedName);
    expect(pair.aka.type).toBe(NumberedName);
    // NextPair rows don't clip: the plain (non-clip) chip form is used.
    expect(pair.shiro.props.clip).toBeFalsy();
    expect(pair.aka.props.clip).toBeFalsy();
    // Rendering each chip actually produces a `.num-prefix` span carrying the
    // number as ITS OWN element, for BOTH sides -- not a number glued onto
    // the name text the way the old bare-string form read.
    expect(numPrefixText(pair.shiro.type(pair.shiro.props))).toBe('K5');
    expect(numPrefixText(pair.aka.type(pair.aka.props))).toBe('K9');
  });

  it("TvIndividualBoard's NEXT line renders a .num-prefix chip for both sides", () => {
    const comp = { name: 'Indiv', kind: 'individual', teamSize: 0, poolMatches: [
      { id: 'Pool A-0', court: 'B', sideA: 'X', sideB: 'Y', status: 'running', ipponsA: [], ipponsB: [], scheduledAt: '09:00' },
    ] };
    const promoted = { competition: comp, match: comp.poolMatches[0], isBracket: false };
    const queueMatches = [{
      id: 'Pool A-9',
      sideA: { name: 'Yamada', number: 'K9' },
      sideB: { name: 'Tanaka', number: 'K5' },
      _comp: { withZekkenName: false },
    }];
    const tree = TvIndividualBoard({
      tournament: { name: 'Cup' }, court: 'B', connected: true, zekken: false,
      promoted, queueMatches,
    });
    const pair = findNextPairProps(tree);
    expect(pair).toBeTruthy();
    expect(typeof pair.shiro).not.toBe('string');
    expect(typeof pair.aka).not.toBe('string');
    expect(pair.shiro.type).toBe(NumberedName);
    expect(pair.aka.type).toBe(NumberedName);
    expect(pair.shiro.props.clip).toBeFalsy();
    expect(pair.aka.props.clip).toBeFalsy();
    expect(numPrefixText(pair.shiro.type(pair.shiro.props))).toBe('K5');
    expect(numPrefixText(pair.aka.type(pair.aka.props))).toBe('K9');
  });
});
